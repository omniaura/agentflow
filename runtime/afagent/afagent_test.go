package afagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

func TestLoopCheckRejectsAnswer(t *testing.T) {
	spec := &OutputSpec{Name: "verdict", Schema: json.RawMessage(`{"type":"object"}`)}
	model, reqs := script(t, Message{Content: "prose"}, calls("a"), Message{Content: `{"ok":true}`})
	var rejected []error
	l := Loop{
		Model:    model,
		Output:   spec,
		Dispatch: func(context.Context, ToolCall) (string, error) { return "", nil },
		Check: func(answer string) error {
			if !strings.HasPrefix(answer, "{") {
				return errors.New("not JSON")
			}
			return nil
		},
		OnEvent: func(e Event) {
			if e.Kind == AnswerRejected {
				rejected = append(rejected, e.Err)
			}
		},
	}
	res, err := l.Run(context.Background(), []Message{{Role: User, Content: "judge"}})
	if err != nil || res.Final != `{"ok":true}` || res.Turns != 3 {
		t.Fatalf("got %v, %+v", err, res)
	}
	if len(rejected) != 1 || rejected[0].Error() != "not JSON" {
		t.Errorf("rejected = %v", rejected)
	}
	// The rejection goes back as a user message after the bad answer.
	if m := res.Messages[2]; m.Role != User || m.Content != "Your answer was rejected: not JSON. Answer again." {
		t.Errorf("message = %+v", m)
	}
	for i, r := range *reqs {
		if r.Output != spec {
			t.Errorf("request %d has no output spec", i)
		}
	}
}

func TestLoopCheckGivesUpAtMaxTurns(t *testing.T) {
	model, _ := script(t, Message{Content: "no"}, Message{Content: "no"})
	l := Loop{Model: model, MaxTurns: 2, Check: func(string) error { return errors.New("bad") }}
	if _, err := l.Run(context.Background(), nil); !errors.Is(err, ErrMaxTurns) {
		t.Fatalf("got %v", err)
	}
}

func TestLoopStreamsText(t *testing.T) {
	model := ModelFunc(func(ctx context.Context, req Request) (Response, error) {
		if req.OnText == nil {
			t.Fatal("OnText is unset while OnEvent listens")
		}
		req.OnText("Hel")
		req.OnText("lo")
		return Response{Message: Message{Content: "Hello"}}, nil
	})
	var got []string
	l := Loop{Model: model, OnEvent: func(e Event) {
		if e.Kind == TextDelta {
			got = append(got, fmt.Sprintf("%d:%s", e.Turn, e.Text))
		}
	}}
	if _, err := l.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "1:Hel,1:lo" {
		t.Errorf("deltas = %v", got)
	}
	// Nobody listening: adapters needn't stream.
	quiet := ModelFunc(func(ctx context.Context, req Request) (Response, error) {
		if req.OnText != nil {
			t.Error("OnText set without OnEvent")
		}
		return Response{}, nil
	})
	(&Loop{Model: quiet}).Run(context.Background(), nil)
}

func TestClientEventKeepsToolDataOnTheServer(t *testing.T) {
	call := &ToolCall{ID: "1", Name: "lookup_order", Arguments: `{"order_id":"secret-arg"}`}
	for _, tt := range []struct {
		e    Event
		name string
		data string
	}{
		{Event{Kind: TextDelta, Text: "hi"}, "text", `{"text":"hi"}`},
		{Event{Kind: ToolCalled, Call: call}, "tool_called", `{"name":"lookup_order"}`},
		{Event{Kind: ToolReturned, Call: call, Result: "secret-result"}, "tool_returned", `{"failed":false,"name":"lookup_order"}`},
		{Event{Kind: ToolReturned, Call: call, Err: errors.New("secret-error")}, "tool_returned", `{"failed":true,"name":"lookup_order"}`},
		{Event{Kind: AnswerRejected, Err: errors.New("secret-check")}, "answer_rejected", `{}`},
	} {
		name, data, ok := ClientEvent(tt.e)
		b, _ := json.Marshal(data)
		if !ok || name != tt.name || string(b) != tt.data {
			t.Errorf("%s: got %s %s %v", tt.e.Kind, name, b, ok)
		}
	}
	if _, _, ok := ClientEvent(Event{Kind: ModelResponded}); ok {
		t.Error("model responses are not client events")
	}
	for _, k := range []EventKind{ToolCalled, ToolReturned} {
		if _, _, ok := ClientEvent(Event{Kind: k}); ok {
			t.Errorf("%s without a call is a client event", k)
		}
	}
}

// With Check set, an answer's text reaches OnEvent only once it passes:
// a rejected answer is never shown. Text beside tool calls isn't an
// answer, so it isn't held back by Check.
func TestLoopHoldsTextUntilCheckPasses(t *testing.T) {
	turns := []struct {
		deltas []string
		msg    Message
	}{
		{[]string{"Looking"}, Message{Content: "Looking", ToolCalls: []ToolCall{{ID: "1", Name: "noop", Arguments: "{}"}}}},
		{[]string{"bad ", "answer"}, Message{Content: "bad answer"}},
		{[]string{"good"}, Message{Content: "good"}},
	}
	n := 0
	model := ModelFunc(func(ctx context.Context, req Request) (Response, error) {
		tt := turns[n]
		n++
		for _, d := range tt.deltas {
			req.OnText(d)
		}
		return Response{Message: tt.msg}, nil
	})
	var got []string
	l := Loop{
		Model:    model,
		Dispatch: func(context.Context, ToolCall) (string, error) { return "ok", nil },
		Check: func(answer string) error {
			if answer != "good" {
				return errors.New("not good")
			}
			return nil
		},
		OnEvent: func(e Event) {
			switch e.Kind {
			case TextDelta:
				got = append(got, fmt.Sprintf("%d:%s", e.Turn, e.Text))
			case ModelResponded, AnswerRejected:
				got = append(got, fmt.Sprintf("%d:%s", e.Turn, e.Kind))
			}
		},
	}
	res, err := l.Run(context.Background(), nil)
	if err != nil || res.Final != "good" {
		t.Fatalf("final %q, err %v", res.Final, err)
	}
	want := "1:Looking,1:model_responded,2:model_responded,2:answer_rejected,3:good,3:model_responded"
	if strings.Join(got, ",") != want {
		t.Errorf("events\n got %s\nwant %s", strings.Join(got, ","), want)
	}
}

// A Model that calls OnText from several goroutines doesn't race the held
// text, and text after Complete returns is dropped. Run with -race.
func TestLoopHeldTextIsSafe(t *testing.T) {
	var late func(string)
	model := ModelFunc(func(ctx context.Context, req Request) (Response, error) {
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req.OnText("x")
			}()
		}
		wg.Wait()
		late = req.OnText
		return Response{Message: Message{Content: "ok"}}, nil
	})
	var deltas atomic.Int32
	l := Loop{Model: model, Check: func(string) error { return nil }, OnEvent: func(e Event) {
		if e.Kind == TextDelta {
			deltas.Add(1)
		}
	}}
	if _, err := l.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	late("too late")
	if n := deltas.Load(); n != 20 {
		t.Errorf("%d deltas, want 20", n)
	}
}

func TestRetryAfter(t *testing.T) {
	for _, tt := range []struct {
		h    http.Header
		want time.Duration
	}{
		{http.Header{"Retry-After": {"3"}}, 3 * time.Second},
		{http.Header{"Retry-After": {"0.5"}}, 500 * time.Millisecond},
		{http.Header{"Retry-After-Ms": {"1500"}, "Retry-After": {"9"}}, 1500 * time.Millisecond},
		{http.Header{"Retry-After": {"soon"}}, 0},
		{http.Header{"Retry-After": {"-2"}}, 0},
		{http.Header{"Retry-After": {time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)}}, 0},
		{http.Header{}, 0},
	} {
		if got := RetryAfter(tt.h); got != tt.want {
			t.Errorf("%v: got %v", tt.h, got)
		}
	}
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	if got := RetryAfter(http.Header{"Retry-After": {future}}); got < 50*time.Second || got > time.Minute {
		t.Errorf("date: got %v", got)
	}
	e := &ModelError{Status: 429, Err: context.Canceled}
	if e.Error() != context.Canceled.Error() || !errors.Is(e, context.Canceled) {
		t.Error("ModelError does not wrap its cause")
	}
}
