// Package afopenai runs agentflow agents on OpenAI's Chat Completions API,
// or any gateway that speaks it, through the official OpenAI Go SDK. Model
// implements afagent.Model:
//
//	agent := &SupportAgent{Model: &afopenai.Model{Model: modelID}, Tools: impl}
//
// Point Options at another base URL (option.WithBaseURL) to use an
// OpenAI-compatible gateway. A structured output (Request.Output) is sent as
// a strict json_schema response_format.
package afopenai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// Model is a chat model.
type Model struct {
	// Client is the API client. When nil, Complete uses one built from
	// Options, which reads OPENAI_API_KEY from the environment.
	Client  *openai.Client
	Options []option.RequestOption
	// Model is the model ID. It is required.
	Model string
	// MaxCompletionTokens caps each answer; 0 leaves it to the model.
	MaxCompletionTokens int64
	// Params, when set, adjusts each request before it is sent: to set
	// reasoning effort, tool_choice or parallel tool calls, for example.
	Params func(*openai.ChatCompletionNewParams)
}

// errorPrefix marks failed tool results, which Chat Completions has no
// field for.
const errorPrefix = "Error: "

// Complete sends one turn of the conversation to the model.
func (m *Model) Complete(ctx context.Context, req afagent.Request) (afagent.Response, error) {
	if m.Model == "" {
		return afagent.Response{}, errors.New("afopenai: Model.Model is empty")
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
	if req.OnText == nil {
		cc, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return afagent.Response{}, apiError(err)
		}
		return response(cc)
	}
	// Someone is listening: stream, and accumulate the same completion.
	params.StreamOptions.IncludeUsage = openai.Bool(true)
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return afagent.Response{}, errors.New("afopenai: the stream sent an inconsistent chunk")
		}
		for _, c := range chunk.Choices {
			if c.Index == 0 && c.Delta.Content != "" {
				req.OnText(c.Delta.Content)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return afagent.Response{}, apiError(err)
	}
	return response(&acc.ChatCompletion)
}

func (m *Model) params(req afagent.Request) (openai.ChatCompletionNewParams, error) {
	p := openai.ChatCompletionNewParams{Model: shared.ChatModel(m.Model)}
	if m.MaxCompletionTokens > 0 {
		p.MaxCompletionTokens = openai.Int(m.MaxCompletionTokens)
	}
	if req.System != "" {
		p.Messages = append(p.Messages, openai.SystemMessage(req.System))
	}
	for i, msg := range req.Messages {
		switch msg.Role {
		case afagent.User:
			p.Messages = append(p.Messages, openai.UserMessage(msg.Content))
		case afagent.Assistant:
			p.Messages = append(p.Messages, assistant(msg))
		case afagent.Tool:
			content := msg.Content
			if msg.IsError {
				content = errorPrefix + content
			}
			p.Messages = append(p.Messages, openai.ToolMessage(content, msg.ToolCallID))
		default:
			return p, fmt.Errorf("afopenai: message %d has unknown role %q", i, msg.Role)
		}
	}
	for _, t := range req.Tools {
		var schema shared.FunctionParameters
		if err := json.Unmarshal(t.Schema, &schema); err != nil {
			return p, fmt.Errorf("afopenai: tool %s: schema: %w", t.Name, err)
		}
		fn := shared.FunctionDefinitionParam{Name: t.Name, Parameters: schema}
		if t.Description != "" {
			fn.Description = openai.String(t.Description)
		}
		p.Tools = append(p.Tools, openai.ChatCompletionFunctionTool(fn))
	}
	if req.NoToolCalls && len(p.Tools) > 0 {
		p.ToolChoice.OfAuto = openai.String(string(openai.ChatCompletionToolChoiceOptionAutoNone))
	}
	if req.Output != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.Output.Schema, &schema); err != nil {
			return p, fmt.Errorf("afopenai: output %s: schema: %w", req.Output.Name, err)
		}
		format := shared.ResponseFormatJSONSchemaJSONSchemaParam{Name: req.Output.Name, Schema: schema, Strict: openai.Bool(true)}
		if req.Output.Description != "" {
			format.Description = openai.String(req.Output.Description)
		}
		p.ResponseFormat.OfJSONSchema = &shared.ResponseFormatJSONSchemaParam{JSONSchema: format}
	}
	if m.Params != nil {
		m.Params(&p)
	}
	return p, nil
}

func assistant(msg afagent.Message) openai.ChatCompletionMessageParamUnion {
	var a openai.ChatCompletionAssistantMessageParam
	if msg.Content != "" {
		a.Content.OfString = openai.String(msg.Content)
	}
	for _, c := range msg.ToolCalls {
		args := c.Arguments
		if args == "" {
			args = "{}"
		}
		a.ToolCalls = append(a.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID:       c.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{Name: c.Name, Arguments: args},
			},
		})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &a}
}

func response(cc *openai.ChatCompletion) (afagent.Response, error) {
	u := cc.Usage
	resp := afagent.Response{Usage: afagent.Usage{
		InputTokens:       int(u.PromptTokens),
		CachedInputTokens: int(u.PromptTokensDetails.CachedTokens),
		OutputTokens:      int(u.CompletionTokens),
	}}
	if len(cc.Choices) == 0 {
		return resp, errors.New("afopenai: the response has no choices")
	}
	choice := cc.Choices[0]
	switch {
	case choice.FinishReason == "length":
		return resp, fmt.Errorf("afopenai: finish reason length: %w", afagent.ErrTruncated)
	case choice.FinishReason == "content_filter":
		return resp, fmt.Errorf("afopenai: finish reason content_filter: %w", afagent.ErrRefused)
	case choice.Message.Refusal != "":
		return resp, fmt.Errorf("afopenai: %w: %s", afagent.ErrRefused, choice.Message.Refusal)
	}
	resp.Message = afagent.Message{Role: afagent.Assistant, Content: choice.Message.Content}
	for _, c := range choice.Message.ToolCalls {
		if c.Type != "function" {
			return resp, fmt.Errorf("afopenai: unsupported %s tool call %s", c.Type, c.ID)
		}
		resp.Message.ToolCalls = append(resp.Message.ToolCalls, afagent.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	return resp, nil
}

var _ afagent.Model = (*Model)(nil)

// apiError wraps a failed request, as an *afagent.ModelError with the
// status and Retry-After hint when the API answered with an error.
func apiError(err error) error {
	err = fmt.Errorf("afopenai: %w", err)
	var e *openai.Error
	if !errors.As(err, &e) {
		return err
	}
	me := &afagent.ModelError{Status: e.StatusCode, Err: err}
	if e.Response != nil {
		me.RetryAfter = afagent.RetryAfter(e.Response.Header)
	}
	return me
}
