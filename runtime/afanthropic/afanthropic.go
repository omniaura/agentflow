// Package afanthropic runs agentflow agents on Claude through the official
// Anthropic Go SDK. Model implements afagent.Model:
//
//	agent := &SupportAgent{Model: &afanthropic.Model{}, Tools: impl}
//
// Requests stream, so long answers don't time out, and use automatic prompt
// caching, so each turn of an agent reuses the conversation before it. A
// structured output (Request.Output) is sent as output_config.format.
// Assistant messages keep Claude's own content blocks in Message.Native, so
// thinking blocks go back to Claude unchanged on the next turn.
package afanthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// Provider names this adapter in afagent.Native.
const Provider = "anthropic"

// DefaultModel and DefaultMaxTokens apply when Model leaves them zero.
const (
	DefaultModel     = "claude-opus-5-5"
	DefaultMaxTokens = 16000
)

// Model is a Claude model.
type Model struct {
	// Client is the API client. When nil, Complete uses one built from
	// Options, which reads ANTHROPIC_API_KEY from the environment.
	Client  *anthropic.Client
	Options []option.RequestOption
	// Model is the model ID; DefaultModel when empty.
	Model string
	// MaxTokens caps each answer; DefaultMaxTokens when 0.
	MaxTokens int64
	// Params, when set, adjusts each request before it is sent: to set
	// thinking, effort, metadata or tool_choice, or to turn caching off.
	Params func(*anthropic.MessageNewParams)
}

// Complete sends one turn of the conversation to Claude.
func (m *Model) Complete(ctx context.Context, req afagent.Request) (afagent.Response, error) {
	params, err := m.params(req)
	if err != nil {
		return afagent.Response{}, err
	}
	client := m.Client
	if client == nil {
		c := anthropic.NewClient(m.Options...)
		client = &c
	}
	stream := client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return afagent.Response{}, fmt.Errorf("afanthropic: %w", err)
		}
		if d, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			switch t := d.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if req.OnText != nil {
					req.OnText(t.Text)
				}
			case anthropic.ThinkingDelta:
				if req.OnReasoning != nil {
					req.OnReasoning(t.Thinking)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return afagent.Response{}, apiError(err)
	}
	return response(msg)
}

func (m *Model) params(req afagent.Request) (anthropic.MessageNewParams, error) {
	p := anthropic.MessageNewParams{
		Model:        anthropic.Model(m.Model),
		MaxTokens:    m.MaxTokens,
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}
	if p.Model == "" {
		p.Model = DefaultModel
	}
	if p.MaxTokens == 0 {
		p.MaxTokens = DefaultMaxTokens
	}
	if req.System != "" {
		p.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	for _, t := range req.Tools {
		tool, err := toolParam(t)
		if err != nil {
			return p, err
		}
		p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	if req.NoToolCalls && len(p.Tools) > 0 {
		p.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
	}
	msgs, err := messages(req.Messages)
	if err != nil {
		return p, err
	}
	p.Messages = msgs
	if req.Output != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.Output.Schema, &schema); err != nil {
			return p, fmt.Errorf("afanthropic: output %s: schema: %w", req.Output.Name, err)
		}
		p.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: schema}
	}
	if m.Params != nil {
		m.Params(&p)
	}
	return p, nil
}

// toolParam converts a tool's JSON Schema into Claude's input_schema, which
// spells out properties and required and passes the rest through.
func toolParam(t afagent.ToolSpec) (anthropic.ToolParam, error) {
	var schema map[string]any
	if err := json.Unmarshal(t.Schema, &schema); err != nil {
		return anthropic.ToolParam{}, fmt.Errorf("afanthropic: tool %s: schema: %w", t.Name, err)
	}
	in := anthropic.ToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				in.Required = append(in.Required, s)
			}
		}
	}
	for k, v := range schema {
		if k != "type" && k != "properties" && k != "required" {
			in.ExtraFields[k] = v
		}
	}
	tool := anthropic.ToolParam{Name: t.Name, InputSchema: in}
	if t.Description != "" {
		tool.Description = anthropic.String(t.Description)
	}
	return tool, nil
}

// messages converts a conversation. Consecutive tool results go back in one
// user message, as Claude expects.
func messages(in []afagent.Message) ([]anthropic.MessageParam, error) {
	var out []anthropic.MessageParam
	for i, m := range in {
		switch m.Role {
		case afagent.User:
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case afagent.Assistant:
			msg, err := assistant(m)
			if err != nil {
				return nil, fmt.Errorf("afanthropic: message %d: %w", i, err)
			}
			out = append(out, msg)
		case afagent.Tool:
			block := anthropic.NewToolResultBlock(m.ToolCallID, m.Content, m.IsError)
			if n := len(out); n > 0 && i > 0 && in[i-1].Role == afagent.Tool {
				out[n-1].Content = append(out[n-1].Content, block)
				continue
			}
			out = append(out, anthropic.NewUserMessage(block))
		default:
			return nil, fmt.Errorf("afanthropic: message %d has unknown role %q", i, m.Role)
		}
	}
	return out, nil
}

func assistant(m afagent.Message) (anthropic.MessageParam, error) {
	if m.Native != nil && m.Native.Provider == Provider {
		var p anthropic.MessageParam
		if err := json.Unmarshal(m.Native.Data, &p); err != nil {
			return p, err
		}
		return p, nil
	}
	var blocks []anthropic.ContentBlockParamUnion
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, c := range m.ToolCalls {
		args := json.RawMessage(c.Arguments)
		if strings.TrimSpace(c.Arguments) == "" {
			args = json.RawMessage("{}")
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(c.ID, args, c.Name))
	}
	return anthropic.NewAssistantMessage(blocks...), nil
}

func response(msg anthropic.Message) (afagent.Response, error) {
	u := msg.Usage
	resp := afagent.Response{Usage: afagent.Usage{
		InputTokens:       int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens),
		CachedInputTokens: int(u.CacheReadInputTokens),
		OutputTokens:      int(u.OutputTokens),
	}}
	switch msg.StopReason {
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return resp, fmt.Errorf("afanthropic: stop reason %s: %w", msg.StopReason, afagent.ErrTruncated)
	case anthropic.StopReasonRefusal:
		err := afagent.ErrRefused
		if d := msg.StopDetails.Explanation; d != "" {
			err = fmt.Errorf("%w: %s", err, d)
		}
		return resp, fmt.Errorf("afanthropic: %w", err)
	}
	var text []string
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text = append(text, b.Text)
		case anthropic.ToolUseBlock:
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, afagent.ToolCall{ID: b.ID, Name: b.Name, Arguments: string(b.Input)})
		}
	}
	resp.Message.Role = afagent.Assistant
	resp.Message.Content = strings.Join(text, "")
	native, err := json.Marshal(msg.ToParam())
	if err != nil {
		return resp, fmt.Errorf("afanthropic: %w", err)
	}
	resp.Message.Native = &afagent.Native{Provider: Provider, Data: native}
	return resp, nil
}

var _ afagent.Model = (*Model)(nil)

// apiError wraps a failed request, as an *afagent.ModelError with the
// status and Retry-After hint when the API answered with an error.
func apiError(err error) error {
	err = fmt.Errorf("afanthropic: %w", err)
	var e *anthropic.Error
	if !errors.As(err, &e) {
		return err
	}
	me := &afagent.ModelError{Status: e.StatusCode, Err: err}
	if e.Response != nil {
		me.RetryAfter = afagent.RetryAfter(e.Response.Header)
	}
	return me
}
