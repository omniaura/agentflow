package afagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// script is a model that answers with its responses in order.
func script(t *testing.T, responses ...Message) (Model, *[]Request) {
	var reqs []Request
	return ModelFunc(func(ctx context.Context, req Request) (Response, error) {
		reqs = append(reqs, req)
		if len(reqs) > len(responses) {
			t.Fatalf("unexpected model call %d", len(reqs))
		}
		return Response{Message: responses[len(reqs)-1], Usage: Usage{InputTokens: 10, OutputTokens: 2}}, nil
	}), &reqs
}

func calls(names ...string) Message {
	m := Message{}
	for i, n := range names {
		m.ToolCalls = append(m.ToolCalls, ToolCall{ID: fmt.Sprint("c", i), Name: n, Arguments: "{}"})
	}
	return m
}

func TestLoopRunsToolsUntilAnswer(t *testing.T) {
	model, reqs := script(t, calls("a", "b"), Message{Content: "done"})
	var events []EventKind
	l := Loop{
		Model:  model,
		System: "sys",
		Tools:  []ToolSpec{{Name: "a"}, {Name: "b"}},
		Dispatch: func(ctx context.Context, c ToolCall) (string, error) {
			if c.Name == "b" {
				return "", errors.New("b broke")
			}
			return "A!", nil
		},
		OnEvent: func(e Event) { events = append(events, e.Kind) },
	}
	res, err := l.Run(context.Background(), []Message{{Role: User, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "done" || res.Turns != 2 || res.Usage != (Usage{InputTokens: 20, OutputTokens: 4}) {
		t.Fatalf("res = %+v", res)
	}
	// user, assistant calls, two tool results, final answer.
	if len(res.Messages) != 5 || res.Messages[1].Role != Assistant || res.Messages[4].Role != Assistant {
		t.Fatalf("messages = %+v", res.Messages)
	}
	if r := res.Messages[2]; r.Role != Tool || r.ToolCallID != "c0" || r.Content != "A!" || r.IsError {
		t.Errorf("result a = %+v", r)
	}
	if r := res.Messages[3]; r.ToolCallID != "c1" || r.Content != "b broke" || !r.IsError {
		t.Errorf("result b = %+v", r)
	}
	if (*reqs)[1].System != "sys" || len((*reqs)[1].Messages) != 4 || len((*reqs)[1].Tools) != 2 {
		t.Errorf("second request = %+v", (*reqs)[1])
	}
	want := []EventKind{ModelResponded, ToolCalled, ToolReturned, ToolCalled, ToolReturned, ModelResponded}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Errorf("events = %v, want %v", events, want)
	}
}

func TestLoopMaxTurns(t *testing.T) {
	model, _ := script(t, calls("a"), calls("a"))
	l := Loop{Model: model, MaxTurns: 2, Dispatch: func(context.Context, ToolCall) (string, error) { return "", nil }}
	res, err := l.Run(context.Background(), nil)
	if !errors.Is(err, ErrMaxTurns) || res.Turns != 2 || len(res.Messages) != 4 {
		t.Fatalf("got %v, %+v", err, res)
	}
}

func TestLoopConcurrentToolsKeepCallOrder(t *testing.T) {
	model, _ := script(t, calls("slow", "fast", "mid"), Message{Content: "ok"})
	var running, peak atomic.Int32
	delay := map[string]time.Duration{"slow": 30 * time.Millisecond, "fast": 0, "mid": 10 * time.Millisecond}
	l := Loop{Model: model, Concurrency: 3, Dispatch: func(ctx context.Context, c ToolCall) (string, error) {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(delay[c.Name])
		return c.Name, nil
	}}
	res, err := l.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range res.Messages[1:4] { // After the assistant message.
		got = append(got, m.Content)
	}
	if strings.Join(got, ",") != "slow,fast,mid" {
		t.Errorf("results out of call order: %v", got)
	}
	if peak.Load() < 2 {
		t.Errorf("tools did not run concurrently (peak %d)", peak.Load())
	}
}

func TestLoopToolPanicFailsTheCall(t *testing.T) {
	model, _ := script(t, calls("boom"), Message{Content: "recovered"})
	l := Loop{Model: model, Dispatch: func(context.Context, ToolCall) (string, error) { panic("kaboom") }}
	res, err := l.Run(context.Background(), nil)
	if err != nil || res.Final != "recovered" {
		t.Fatalf("got %v, %+v", err, res)
	}
	if r := res.Messages[1]; !r.IsError || !strings.Contains(r.Content, "tool boom failed: kaboom") {
		t.Errorf("result = %+v", r)
	}
}

func TestLoopWithoutDispatch(t *testing.T) {
	model, _ := script(t, calls("a"), Message{Content: "ok"})
	res, err := (&Loop{Model: model}).Run(context.Background(), nil)
	if err != nil || !res.Messages[1].IsError || res.Messages[1].Content != "no tools are available" {
		t.Fatalf("got %v, %+v", err, res)
	}
}

func TestLoopModelError(t *testing.T) {
	boom := errors.New("rate limited")
	l := Loop{Model: ModelFunc(func(context.Context, Request) (Response, error) { return Response{}, boom })}
	in := []Message{{Role: User, Content: "hi"}}
	res, err := l.Run(context.Background(), in)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "turn 1") || len(res.Messages) != 1 {
		t.Fatalf("got %v, %+v", err, res)
	}
}

func TestLoopCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := ModelFunc(func(context.Context, Request) (Response, error) { return Response{Message: calls("a")}, nil })
	l := Loop{Model: model, Dispatch: func(context.Context, ToolCall) (string, error) {
		cancel()
		return "", nil
	}}
	res, err := l.Run(ctx, nil)
	if !errors.Is(err, context.Canceled) || res.Turns != 1 {
		t.Fatalf("got %v, %+v", err, res)
	}
}

func TestLoopDoesNotMutateInput(t *testing.T) {
	model, _ := script(t, Message{Content: "ok"})
	in := make([]Message, 1, 8)
	in[0] = Message{Role: User, Content: "hi"}
	if _, err := (&Loop{Model: model}).Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if in[:2][1].Content != "" {
		t.Error("Run wrote into the caller's slice")
	}
}
