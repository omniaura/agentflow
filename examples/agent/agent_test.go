package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
)

type shop struct {
	refunds []OfferRefundArgs
}

func (s *shop) LookupOrder(ctx context.Context, args LookupOrderArgs) (string, error) {
	if args.OrderId != "A-1" {
		return "", fmt.Errorf("no order %s", args.OrderId)
	}
	return `{"status":"delivered late","total":"$20"}`, nil
}

func (s *shop) OfferRefund(ctx context.Context, args OfferRefundArgs) (string, error) {
	s.refunds = append(s.refunds, args)
	return "offer shown", nil
}

// script is a fake model that answers each turn with the next message.
func script(t *testing.T, turns ...afagent.Message) (afagent.Model, *[]afagent.Request) {
	var seen []afagent.Request
	return afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		seen = append(seen, req)
		if len(seen) > len(turns) {
			t.Fatalf("unexpected turn %d", len(seen))
		}
		return afagent.Response{Message: turns[len(seen)-1], Usage: afagent.Usage{InputTokens: 10, OutputTokens: 2}}, nil
	}), &seen
}

func call(id, name, args string) afagent.ToolCall {
	return afagent.ToolCall{ID: id, Name: name, Arguments: args}
}

func TestSupportAgent(t *testing.T) {
	model, seen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{
			call("1", "lookup_order", `{"order_id":"A-1"}`),
			call("2", "offer_refund", `{"order_id":"A-1","reason":"changed my mind"}`),
			call("3", "cancel_order", `{}`),
		}},
		afagent.Message{ToolCalls: []afagent.ToolCall{call("4", "offer_refund", `{"order_id":"A-1","reason":"late"}`)}},
		afagent.Message{Content: "I've offered you a refund for the late order."},
	)
	s := &shop{}
	var events []string
	agent := &SupportAgent{Model: model, Tools: s, OnEvent: func(e afagent.Event) {
		events = append(events, fmt.Sprintf("%d:%s", e.Turn, e.Kind))
	}}
	res, err := agent.Run(context.Background(), &Support{Shop: "Acme"}, afagent.Message{Role: afagent.User, Content: "My order A-1 was late."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "I've offered you a refund for the late order." || res.Turns != 3 || res.Usage.InputTokens != 30 {
		t.Fatalf("result = %+v", res)
	}
	if len(s.refunds) != 1 || s.refunds[0].Reason != OfferRefundReasonLate {
		t.Fatalf("refunds = %+v", s.refunds)
	}

	// The model saw the system prompt, both tools, and every result.
	first := (*seen)[0]
	if !strings.HasPrefix(first.System, "You are a support agent for Acme.") || len(first.Tools) != 2 || first.Tools[1].Name != "offer_refund" {
		t.Fatalf("first request = %+v", first)
	}
	results := (*seen)[1].Messages[2:]
	want := []struct {
		id, content string
		isErr       bool
	}{
		{"1", `{"status":"delivered late","total":"$20"}`, false},
		{"2", `offer_refund: invalid arguments: reason: "changed my mind" is not one of damaged, late`, true},
		{"3", `there is no tool named "cancel_order"`, true},
	}
	if len(results) != len(want) {
		t.Fatalf("results = %+v", results)
	}
	for i, w := range want {
		r := results[i]
		if r.Role != afagent.Tool || r.ToolCallID != w.id || r.Content != w.content || r.IsError != w.isErr {
			t.Errorf("result %d = %+v, want %+v", i, r, w)
		}
	}
	if got := strings.Join(events, " "); !strings.HasPrefix(got, "1:model_responded 1:tool_called 1:tool_returned") || !strings.HasSuffix(got, "3:model_responded") {
		t.Fatalf("events = %s", got)
	}
}

func TestSupportAgentMaxTurns(t *testing.T) {
	loop := afagent.Message{ToolCalls: []afagent.ToolCall{call("x", "lookup_order", `{"order_id":"nope"}`)}}
	turns := make([]afagent.Message, 6)
	for i := range turns {
		turns[i] = loop
	}
	model, _ := script(t, turns...)
	res, err := (&SupportAgent{Model: model, Tools: &shop{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
	// .maxturns 6 is the default.
	if !errors.Is(err, afagent.ErrMaxTurns) || res.Turns != 6 {
		t.Fatalf("got %v after %d turns", err, res.Turns)
	}
	if last := res.Messages[len(res.Messages)-1]; last.Content != "no order nope" || !last.IsError {
		t.Fatalf("last message = %+v", last)
	}
}
