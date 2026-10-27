package api

import (
	"context"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// RunAgent is a hand-written tool-calling loop: an .agent candidate.
func RunAgent(ctx context.Context, c anthropic.Client, msgs []anthropic.MessageParam) error {
	for turn := 0; turn < 10; turn++ {
		turnCtx, cancel := context.WithTimeout(ctx, time.Minute)
		resp, err := c.Messages.New(turnCtx, anthropic.MessageNewParams{Messages: msgs})
		cancel()
		if err != nil {
			return err
		}
		msgs = append(msgs, resp.ToParam())
		var results []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				results = append(results, anthropic.NewToolResultBlock(tu.ID, dispatch(tu.Name), false))
			}
		}
		if len(results) == 0 {
			return nil
		}
		msgs = append(msgs, anthropic.NewUserMessage(results...))
	}
	return nil
}

func dispatch(name string) string { return name }

// LogCalls reads tool calls but answers none: not an agent loop.
func LogCalls(resp struct{ ToolCalls []string }) {
	for _, c := range resp.ToolCalls {
		println(c)
	}
}

type msg struct {
	Role       string
	ToolCalls  []string
	ToolCallID string
}

// convert translates a transcript with tool calls and results, but calls
// no model: not an agent loop.
func convert(in []msg) []map[string]any {
	var out []map[string]any
	for _, m := range in {
		if len(m.ToolCalls) > 0 {
			out = append(out, map[string]any{"role": "assistant", "tool_calls": m.ToolCalls})
		}
		if m.ToolCallID != "" {
			out = append(out, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID})
		}
	}
	return out
}
