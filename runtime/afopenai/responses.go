package afopenai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// NativeResponses is the Native provider name of messages from the
// Responses API: their data is the response's output items, as JSON.
const NativeResponses = "openai-responses"

// ResponsesModel runs agents on OpenAI's Responses API, for models such as
// reasoning models that are best served there. It implements
// afagent.Model:
//
//	model := &afopenai.ResponsesModel{Model: "o3", Reasoning: shared.ReasoningParam{Effort: shared.ReasoningEffortMedium, Summary: shared.ReasoningSummaryAuto}}
//
// It keeps no state on OpenAI's side (store is false): each assistant
// message keeps the response's output items, reasoning included, in
// Message.Native, and the next request sends them back. Reasoning summaries
// stream to Request.OnReasoning, the answer to Request.OnText.
type ResponsesModel struct {
	// Client is the API client. When nil, Complete uses one built from
	// Options, which reads OPENAI_API_KEY from the environment.
	Client  *openai.Client
	Options []option.RequestOption
	// Model is the model ID. It is required.
	Model string
	// MaxOutputTokens caps each answer, reasoning included; 0 leaves it to
	// the model.
	MaxOutputTokens int64
	// Reasoning sets a reasoning model's effort and summaries.
	Reasoning shared.ReasoningParam
	// Params, when set, adjusts each request before it is sent.
	Params func(*responses.ResponseNewParams)
}

var _ afagent.Model = (*ResponsesModel)(nil)

// Complete sends one turn of the conversation to the model.
func (m *ResponsesModel) Complete(ctx context.Context, req afagent.Request) (afagent.Response, error) {
	if m.Model == "" {
		return afagent.Response{}, errors.New("afopenai: ResponsesModel.Model is empty")
	}
	params, err := m.params(req)
	if err != nil {
		return afagent.Response{}, err
	}
	client := m.Client
	if client == nil {
		c := openai.NewClient(m.Options...)
		client = &c
	}
	if req.OnText == nil && req.OnReasoning == nil {
		r, err := client.Responses.New(ctx, params)
		if err != nil {
			return afagent.Response{}, apiError(err)
		}
		return responsesResult(r)
	}
	// Someone is listening: stream, and take the final response from the
	// event that ends the stream.
	stream := client.Responses.NewStreaming(ctx, params)
	defer stream.Close()
	var final *responses.Response
	for stream.Next() {
		e := stream.Current()
		switch e.Type {
		case "response.output_text.delta":
			if req.OnText != nil {
				req.OnText(e.AsResponseOutputTextDelta().Delta)
			}
		case "response.reasoning_summary_text.delta":
			if req.OnReasoning != nil {
				req.OnReasoning(e.AsResponseReasoningSummaryTextDelta().Delta)
			}
		case "response.completed":
			r := e.AsResponseCompleted().Response
			final = &r
		case "response.incomplete":
			r := e.AsResponseIncomplete().Response
			final = &r
		case "response.failed":
			r := e.AsResponseFailed().Response
			return afagent.Response{}, fmt.Errorf("afopenai: response failed: %s", r.Error.Message)
		case "error":
			return afagent.Response{}, fmt.Errorf("afopenai: stream error: %s", e.AsError().Message)
		}
	}
	if err := stream.Err(); err != nil {
		return afagent.Response{}, apiError(err)
	}
	if final == nil {
		return afagent.Response{}, errors.New("afopenai: the stream ended without a response")
	}
	return responsesResult(final)
}

func (m *ResponsesModel) params(req afagent.Request) (responses.ResponseNewParams, error) {
	p := responses.ResponseNewParams{Model: shared.ResponsesModel(m.Model), Store: openai.Bool(false), Reasoning: m.Reasoning}
	if m.MaxOutputTokens > 0 {
		p.MaxOutputTokens = openai.Int(m.MaxOutputTokens)
	}
	if req.System != "" {
		p.Instructions = openai.String(req.System)
	}
	if m.Reasoning.Effort != "" || m.Reasoning.Summary != "" {
		// Without stored state, reasoning travels back encrypted.
		p.Include = append(p.Include, responses.ResponseIncludableReasoningEncryptedContent)
	}
	var input responses.ResponseInputParam
	for i, msg := range req.Messages {
		switch msg.Role {
		case afagent.User:
			input = append(input, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleUser))
		case afagent.Assistant:
			items, err := assistantItems(msg)
			if err != nil {
				return p, fmt.Errorf("afopenai: message %d: %w", i, err)
			}
			input = append(input, items...)
		case afagent.Tool:
			content := msg.Content
			if msg.IsError {
				content = errorPrefix + content
			}
			out := responses.ResponseInputItemParamOfFunctionCallOutput(content)
			out.OfFunctionCallOutput.CallID = openai.String(msg.ToolCallID)
			input = append(input, out)
		default:
			return p, fmt.Errorf("afopenai: message %d has unknown role %q", i, msg.Role)
		}
	}
	p.Input.OfInputItemList = input
	for _, t := range req.Tools {
		var schema map[string]any
		if err := json.Unmarshal(t.Schema, &schema); err != nil {
			return p, fmt.Errorf("afopenai: tool %s: schema: %w", t.Name, err)
		}
		// Not strict: optional parameters aren't in required, which
		// strict mode rejects. The tool's decoder checks the arguments.
		tool := responses.ToolParamOfFunction(t.Name, schema, false)
		if t.Description != "" {
			tool.OfFunction.Description = openai.String(t.Description)
		}
		p.Tools = append(p.Tools, tool)
	}
	if req.NoToolCalls && len(p.Tools) > 0 {
		p.ToolChoice.OfToolChoiceMode = param.NewOpt(responses.ToolChoiceOptionsNone)
	}
	if req.Output != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.Output.Schema, &schema); err != nil {
			return p, fmt.Errorf("afopenai: output %s: schema: %w", req.Output.Name, err)
		}
		format := &responses.ResponseFormatTextJSONSchemaConfigParam{Name: req.Output.Name, Schema: schema, Strict: openai.Bool(true)}
		if req.Output.Description != "" {
			format.Description = openai.String(req.Output.Description)
		}
		p.Text.Format.OfJSONSchema = format
	}
	if m.Params != nil {
		m.Params(&p)
	}
	return p, nil
}

// assistantItems are the input items of an assistant message: the output
// items the Responses API gave it, as they were, or else ones built from
// its text and tool calls, as a message from another provider needs.
func assistantItems(msg afagent.Message) ([]responses.ResponseInputItemUnionParam, error) {
	if n := msg.Native; n != nil && n.Provider == NativeResponses {
		var raw []json.RawMessage
		if err := json.Unmarshal(n.Data, &raw); err != nil {
			return nil, fmt.Errorf("native output items: %w", err)
		}
		items := make([]responses.ResponseInputItemUnionParam, 0, len(raw))
		for _, r := range raw {
			var item struct {
				Type             string  `json:"type"`
				EncryptedContent *string `json:"encrypted_content"`
			}
			if err := json.Unmarshal(r, &item); err != nil {
				return nil, fmt.Errorf("native output item: %w", err)
			}
			// Nothing is stored on OpenAI's side, so reasoning can only go
			// back encrypted; without that, the API rejects its ID.
			if item.Type == "reasoning" && item.EncryptedContent == nil {
				continue
			}
			items = append(items, param.Override[responses.ResponseInputItemUnionParam](r))
		}
		return items, nil
	}
	var items []responses.ResponseInputItemUnionParam
	if msg.Content != "" {
		items = append(items, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleAssistant))
	}
	for _, c := range msg.ToolCalls {
		args := c.Arguments
		if args == "" {
			args = "{}"
		}
		items = append(items, responses.ResponseInputItemParamOfFunctionCall(args, c.ID, c.Name))
	}
	return items, nil
}

func responsesResult(r *responses.Response) (afagent.Response, error) {
	resp := afagent.Response{Usage: afagent.Usage{
		InputTokens:       int(r.Usage.InputTokens),
		CachedInputTokens: int(r.Usage.InputTokensDetails.CachedTokens),
		OutputTokens:      int(r.Usage.OutputTokens),
	}}
	if r.Status == responses.ResponseStatusIncomplete {
		reason := r.IncompleteDetails.Reason
		if reason == "content_filter" {
			return resp, fmt.Errorf("afopenai: incomplete: %s: %w", reason, afagent.ErrRefused)
		}
		return resp, fmt.Errorf("afopenai: incomplete: %s: %w", reason, afagent.ErrTruncated)
	}
	msg := afagent.Message{Role: afagent.Assistant}
	var text strings.Builder
	raw := make([]json.RawMessage, 0, len(r.Output))
	for _, item := range r.Output {
		raw = append(raw, json.RawMessage(item.RawJSON()))
		switch item.Type {
		case "message":
			for _, c := range item.AsMessage().Content {
				switch c.Type {
				case "output_text":
					text.WriteString(c.Text)
				case "refusal":
					return resp, fmt.Errorf("afopenai: %w: %s", afagent.ErrRefused, c.Refusal)
				}
			}
		case "function_call":
			fc := item.AsFunctionCall()
			msg.ToolCalls = append(msg.ToolCalls, afagent.ToolCall{ID: fc.CallID, Name: fc.Name, Arguments: fc.Arguments})
		}
	}
	msg.Content = text.String()
	data, err := json.Marshal(raw)
	if err != nil {
		return resp, err
	}
	msg.Native = &afagent.Native{Provider: NativeResponses, Data: data}
	resp.Message = msg
	return resp, nil
}
