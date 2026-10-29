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
		{"3", "tool cancel_order is not available in this conversation", true},
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

type interruptedShop struct {
	shop
	stop error
}

func (s *interruptedShop) LookupOrder(context.Context, LookupOrderArgs) (string, error) {
	return "", s.stop
}

func TestGeneratedAgentInterruptsToolBatch(t *testing.T) {
	stop := errors.New("approval needed")
	model, seen := script(t, afagent.Message{ToolCalls: []afagent.ToolCall{
		call("1", "lookup_order", `{"order_id":"A-1"}`),
		call("2", "offer_refund", `{"order_id":"A-1","reason":"late"}`),
	}})
	s := &interruptedShop{stop: stop}
	a := &SupportAgent{Model: model, Tools: s, Concurrency: 2, InterruptTool: func(err error) bool { return errors.Is(err, stop) }}
	res, err := a.Run(t.Context(), &Support{}, afagent.Message{Role: afagent.User, Content: "help"})
	if !errors.Is(err, stop) || len(*seen) != 1 || len(s.refunds) != 0 || res.ToolCalls != 1 || len(res.Messages) != 2 {
		t.Fatalf("generated agent did not preserve interrupted call: result=%+v refunds=%v err=%v", res, s.refunds, err)
	}
}

func TestGeneratedAgentDynamicToolsAreGated(t *testing.T) {
	model, seen := script(t, afagent.Message{ToolCalls: []afagent.ToolCall{
		call("1", "connected", `{"query":"x"}`),
		call("2", "denied", `{}`),
	}}, afagent.Message{Content: "done"})
	var dispatched []string
	a := &SupportAgent{Model: model, Tools: &shop{}, AdditionalTools: []afagent.ToolSpec{
		{Name: "connected", Schema: []byte(`{"type":"object"}`)}, {Name: "denied", Schema: []byte(`{"type":"object"}`)},
	}, Allow: func(_ context.Context, name string) bool { return name != "denied" },
		DispatchAdditional: func(_ context.Context, c afagent.ToolCall) (string, error) {
			dispatched = append(dispatched, c.Name)
			return "data", nil
		},
	}
	res, err := a.Run(t.Context(), &Support{}, afagent.Message{Role: afagent.User, Content: "go"})
	if err != nil || res.Final != "done" || strings.Join(dispatched, ",") != "connected" || len(*seen) != 2 {
		t.Fatalf("result=%+v dispatched=%v err=%v", res, dispatched, err)
	}
	msgs := (*seen)[1].Messages
	if len(msgs) != 4 || msgs[2].Content != "data" || !msgs[3].IsError {
		t.Fatalf("tool results: %+v", msgs)
	}
}

func TestSupportAgentMaxTurns(t *testing.T) {
	loop := afagent.Message{ToolCalls: []afagent.ToolCall{call("x", "lookup_order", `{"order_id":"nope"}`)}}
	turns := make([]afagent.Message, 6)
	for i := range turns {
		turns[i] = loop
	}
	// .maxturns 6 is the default. The last turn may not call tools, so the
	// model answers with what it has.
	turns[5] = afagent.Message{Content: "I couldn't find order nope."}
	model, seen := script(t, turns...)
	res, err := (&SupportAgent{Model: model, Tools: &shop{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
	if err != nil || res.Turns != 6 || res.Final != "I couldn't find order nope." || !res.OutOfTurns {
		t.Fatalf("got %v after %d turns: %q (out of turns: %v)", err, res.Turns, res.Final, res.OutOfTurns)
	}
	for i, req := range *seen {
		// Tools are still sent on the last turn; the adapter turns them off.
		if req.NoToolCalls != (i == 5) || len(req.Tools) == 0 {
			t.Errorf("turn %d: NoToolCalls = %v with %d tools", i+1, req.NoToolCalls, len(req.Tools))
		}
	}

	// A model that calls tools anyway ends the run; the calls don't run.
	turns[5] = loop
	model, _ = script(t, turns...)
	res, err = (&SupportAgent{Model: model, Tools: &shop{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
	if !errors.Is(err, afagent.ErrMaxTurns) || res.Turns != 6 || res.OutOfTurns {
		t.Fatalf("got %v after %d turns", err, res.Turns)
	}
	if last := res.Messages[len(res.Messages)-1]; last.Role != afagent.Assistant || len(last.ToolCalls) != 1 {
		t.Fatalf("last message = %+v", last)
	}
}

func TestTriagerReturnsTypedOutput(t *testing.T) {
	model, seen := script(t,
		afagent.Message{Content: "This is about a late order."},
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "lookup_order", `{"order_id":"A-1"}`)}},
		afagent.Message{Content: `{"category":"refund","order_id":"A-1","urgent":true}`},
	)
	var rejected int
	agent := &TriagerAgent{Model: model, Tools: &shop{}, OnEvent: func(e afagent.Event) {
		if e.Kind == afagent.AnswerRejected {
			rejected++
		}
	}}
	res, err := agent.Run(context.Background(), &Support{Shop: "Acme"}, afagent.Message{Role: afagent.User, Content: "A-1 came late, I want my money back"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output.Category != TriageCategoryRefund || res.Output.OrderId != "A-1" || !res.Output.Urgent {
		t.Errorf("output = %+v", res.Output)
	}
	if rejected != 1 || res.Turns != 3 {
		t.Errorf("rejected %d, turns %d", rejected, res.Turns)
	}
	req := (*seen)[0]
	if req.Output == nil || req.Output.Name != "triage" || !strings.Contains(string(req.Output.Schema), `"additionalProperties":false`) {
		t.Errorf("output spec = %+v", req.Output)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "lookup_order" {
		t.Errorf("tools = %+v", req.Tools)
	}
}

func TestTriagerGivesUpOnInvalidAnswers(t *testing.T) {
	model, _ := script(t, afagent.Message{Content: "{}"}, afagent.Message{Content: "{}"}, afagent.Message{Content: "{}"})
	res, err := (&TriagerAgent{Model: model, Tools: &shop{}}).Run(context.Background(), &Support{Shop: "Acme"})
	if !errors.Is(err, afagent.ErrMaxTurns) || res.Turns != 3 {
		t.Fatalf("got %v after %d turns", err, res.Turns)
	}
}

// Check vets what the schema can't: here, that a refund names its order.
// The model sees the rejection in Check's words and answers again.
func TestTriagerCheck(t *testing.T) {
	model, seen := script(t,
		afagent.Message{Content: `{"category":"refund","order_id":null,"urgent":false}`},
		afagent.Message{Content: `{"category":"refund","order_id":"A-1","urgent":false}`},
	)
	var ctxs int
	type key struct{}
	agent := &TriagerAgent{Model: model, Tools: &shop{}, Check: func(ctx context.Context, out TriageOutput) error {
		if ctx.Value(key{}) == "run" {
			ctxs++
		}
		if out.Category == TriageCategoryRefund && out.OrderId == "" {
			return errors.New("a refund needs the order_id")
		}
		return nil
	}}
	ctx := context.WithValue(context.Background(), key{}, "run")
	res, err := agent.Run(ctx, &Support{Shop: "Acme"}, afagent.Message{Role: afagent.User, Content: "refund please"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output.OrderId != "A-1" || res.Turns != 2 || ctxs != 2 {
		t.Errorf("output %+v after %d turns, %d checks with the run's context", res.Output, res.Turns, ctxs)
	}
	if last := (*seen)[1].Messages; !strings.Contains(last[len(last)-1].Content, "a refund needs the order_id") {
		t.Errorf("model was told %q", last[len(last)-1].Content)
	}

	// Answers Check keeps rejecting end the run with its reason.
	model, _ = script(t, afagent.Message{Content: `{"category":"refund","order_id":null,"urgent":false}`},
		afagent.Message{Content: `{"category":"refund","order_id":null,"urgent":false}`},
		afagent.Message{Content: `{"category":"refund","order_id":null,"urgent":false}`})
	agent.Model = model
	if _, err := agent.Run(ctx, &Support{Shop: "Acme"}); !errors.Is(err, afagent.ErrMaxTurns) || !strings.Contains(err.Error(), "a refund needs the order_id") {
		t.Fatalf("got %v", err)
	}
}

// Allow picks a run's tools: offer_refund is neither offered nor run.
func TestSupportAgentAllow(t *testing.T) {
	model, seen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "offer_refund", `{"order_id":"A-1","reason":"late"}`)}},
		afagent.Message{Content: "I can't offer refunds here."},
	)
	s := &shop{}
	type key struct{}
	agent := &SupportAgent{Model: model, Tools: s, Allow: func(ctx context.Context, tool string) bool {
		// Allow sees the run's context, as with a workspace's settings.
		return ctx.Value(key{}) == "workspace-1" && tool != "offer_refund"
	}}
	ctx := context.WithValue(context.Background(), key{}, "workspace-1")
	res, err := agent.Run(ctx, &Support{}, afagent.Message{Role: afagent.User, Content: "refund A-1"})
	if err != nil {
		t.Fatal(err)
	}
	if tools := (*seen)[0].Tools; len(tools) != 1 || tools[0].Name != "lookup_order" {
		t.Errorf("offered %+v", tools)
	}
	if refused := res.Messages[2]; !refused.IsError || refused.Content != "tool offer_refund is not available in this conversation" || len(s.refunds) != 0 {
		t.Errorf("refused call = %+v, refunds %v", refused, s.refunds)
	}
}

// Allow is asked once per run: a tool it starts allowing mid-run still
// isn't run, because it wasn't offered.
func TestSupportAgentAllowOncePerRun(t *testing.T) {
	model, _ := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "offer_refund", `{"order_id":"A-1","reason":"late"}`)}},
		afagent.Message{Content: "ok"},
	)
	s := &shop{}
	asked := map[string]int{}
	agent := &SupportAgent{Model: model, Tools: s, Allow: func(ctx context.Context, tool string) bool {
		asked[tool]++
		return tool != "offer_refund" || asked[tool] > 1
	}}
	res, err := agent.Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "refund A-1"})
	if err != nil {
		t.Fatal(err)
	}
	if refused := res.Messages[2]; !refused.IsError || len(s.refunds) != 0 || asked["offer_refund"] != 1 {
		t.Errorf("refused = %+v, refunds %v, asked %v", refused, s.refunds, asked)
	}
}

// Without Allow, dispatch still runs only the tools offered: a caller that
// narrows SupportAgentTools doesn't leave the others callable.
func TestSupportAgentRunsOnlyOfferedTools(t *testing.T) {
	saved := SupportAgentTools
	defer func() { SupportAgentTools = saved }()
	SupportAgentTools = saved[:1]
	model, _ := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "offer_refund", `{"order_id":"A-1","reason":"late"}`)}},
		afagent.Message{Content: "ok"},
	)
	s := &shop{}
	res, err := (&SupportAgent{Model: model, Tools: s}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
	if err != nil || !res.Messages[2].IsError || len(s.refunds) != 0 {
		t.Fatalf("got %v, %+v, refunds %v", err, res.Messages[2], s.refunds)
	}
}

// BeforeTurn compacts a long conversation before the model sees it.
func TestSupportAgentBeforeTurn(t *testing.T) {
	look := afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "lookup_order", `{"order_id":"A-1"}`)}}
	model, seen := script(t, look, look, afagent.Message{Content: "Delivered late."})
	agent := &SupportAgent{Model: model, Tools: &shop{}, BeforeTurn: func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
		if len(msgs) > 3 {
			// Keep the question; summarize the rest.
			return []afagent.Message{msgs[0], {Role: afagent.User, Content: "(summary: A-1 was delivered late)"}}, nil
		}
		return msgs, nil
	}}
	res, err := agent.Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "where is A-1?"})
	if err != nil || res.Final != "Delivered late." {
		t.Fatalf("got %v: %+v", err, res)
	}
	if third := (*seen)[2].Messages; len(third) != 2 || !strings.Contains(third[1].Content, "summary") {
		t.Errorf("third request = %+v", third)
	}
}
