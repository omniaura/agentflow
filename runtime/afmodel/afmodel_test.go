package afmodel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/go-kit/errs"
)

// scripted answers each call with the next of its results.
type scripted struct {
	name    string
	results []error
	calls   int
	stream  bool
	reason  bool
	// delay is how long each call takes.
	delay time.Duration
}

func (s *scripted) Complete(ctx context.Context, req afagent.Request) (afagent.Response, error) {
	i := s.calls
	s.calls++
	time.Sleep(s.delay)
	if s.stream && req.OnText != nil {
		req.OnText("Hel")
	}
	if s.reason && req.OnReasoning != nil {
		req.OnReasoning("Hmm")
	}
	if i < len(s.results) && s.results[i] != nil {
		return afagent.Response{}, s.results[i]
	}
	return afagent.Response{Message: afagent.Message{Content: s.name}, Usage: afagent.Usage{InputTokens: 10, OutputTokens: 2}}, nil
}

func status(code int) error {
	return &afagent.ModelError{Status: code, Err: fmt.Errorf("provider: %d", code)}
}

func TestTransient(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want bool
	}{
		{status(429), true},
		{status(503), true},
		// Anthropic's overloaded.
		{fmt.Errorf("wrapped: %w", status(529)), true},
		{status(400), false},
		{status(401), false},
		{&net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{fmt.Errorf("stream: %w", io.ErrUnexpectedEOF), true},
		{context.Canceled, false},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), false},
		{errors.New("afanthropic: tool x: schema: bad"), false},
		{nil, false},
	} {
		if got := Transient(tt.err); got != tt.want {
			t.Errorf("%v: got %v", tt.err, got)
		}
	}
}

func TestRetry(t *testing.T) {
	policy := errs.FixedRetry(3, time.Millisecond)
	ctx := context.Background()

	m := &scripted{name: "ok", results: []error{status(429), status(503)}}
	resp, err := Retry(m, policy).Complete(ctx, afagent.Request{})
	if err != nil || resp.Message.Content != "ok" || m.calls != 3 {
		t.Fatalf("two transient failures: %v, %d calls", err, m.calls)
	}
	// Out of attempts: the last error.
	m = &scripted{results: []error{status(429), status(429), status(502)}}
	if _, err := Retry(m, policy).Complete(ctx, afagent.Request{}); err == nil || m.calls != 3 || err.Error() != "provider: 502" {
		t.Fatalf("out of attempts: %v, %d calls", err, m.calls)
	}
	// Not transient: no retry.
	m = &scripted{results: []error{status(400)}}
	if _, err := Retry(m, policy).Complete(ctx, afagent.Request{}); err == nil || m.calls != 1 {
		t.Fatalf("bad request: %v, %d calls", err, m.calls)
	}
	// Text already streamed: no retry, so the client never sees it twice.
	m = &scripted{stream: true, results: []error{status(503)}}
	if _, err := Retry(m, policy).Complete(ctx, afagent.Request{OnText: func(string) {}}); err == nil || m.calls != 1 {
		t.Fatalf("after streaming: %v, %d calls", err, m.calls)
	}
	// So is reasoning.
	m = &scripted{reason: true, results: []error{status(503)}}
	if _, err := Retry(m, policy).Complete(ctx, afagent.Request{OnReasoning: func(string) {}}); err == nil || m.calls != 1 {
		t.Fatalf("after streaming reasoning: %v, %d calls", err, m.calls)
	}
	// Streaming without text yet is retried.
	m = &scripted{results: []error{status(503)}}
	if _, err := Retry(m, policy).Complete(ctx, afagent.Request{OnText: func(string) {}}); err != nil || m.calls != 2 {
		t.Fatalf("streaming request, no text: %v, %d calls", err, m.calls)
	}
}

func TestRetryAfter(t *testing.T) {
	ctx := context.Background()
	limited := &afagent.ModelError{Status: 429, RetryAfter: 40 * time.Millisecond, Err: errors.New("slow down")}
	m := &scripted{results: []error{limited}}
	start := time.Now()
	if _, err := Retry(m, errs.FixedRetry(2, time.Millisecond)).Complete(ctx, afagent.Request{}); err != nil || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("Retry-After not honoured: %v after %v", err, time.Since(start))
	}
	// A hint beyond MaxRetryAfter ends the retries, so the caller can pass
	// it on instead of holding the request.
	p := errs.FixedRetry(2, time.Millisecond)
	p.MaxRetryAfter = 10 * time.Millisecond
	m = &scripted{results: []error{limited}}
	if _, err := Retry(m, p).Complete(ctx, afagent.Request{}); !errors.Is(err, limited) || m.calls != 1 {
		t.Fatalf("long hint: %v, %d calls", err, m.calls)
	}
	// A canceled context stops the wait.
	cctx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	m = &scripted{results: []error{&afagent.ModelError{Status: 503, RetryAfter: time.Minute, Err: errors.New("down")}}}
	if _, err := Retry(m, errs.FixedRetry(2, 0)).Complete(cctx, afagent.Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled wait: %v", err)
	}
}

func TestFallback(t *testing.T) {
	ctx := context.Background()
	primary := &scripted{name: "claude", results: []error{status(529), status(503)}}
	secondary := &scripted{name: "gpt"}
	m := Fallback(primary, secondary)
	resp, err := m.Complete(ctx, afagent.Request{})
	if err != nil || resp.Message.Content != "gpt" || primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("got %q, %v", resp.Message.Content, err)
	}
	// A bad request would fail everywhere: no fallback.
	primary = &scripted{results: []error{status(400)}}
	secondary = &scripted{name: "gpt"}
	if _, err := Fallback(primary, secondary).Complete(ctx, afagent.Request{}); err == nil || secondary.calls != 0 {
		t.Fatalf("bad request fell back: %v", err)
	}
	// All fail: the last error.
	primary = &scripted{results: []error{status(503)}}
	secondary = &scripted{results: []error{status(502)}}
	if _, err := Fallback(primary, secondary).Complete(ctx, afagent.Request{}); err == nil || err.Error() != "provider: 502" {
		t.Fatalf("all failed: %v", err)
	}
	// After streaming text: no fallback.
	primary = &scripted{stream: true, results: []error{status(503)}}
	secondary = &scripted{name: "gpt"}
	if _, err := Fallback(primary, secondary).Complete(ctx, afagent.Request{OnText: func(string) {}}); err == nil || secondary.calls != 0 {
		t.Fatalf("fell back after streaming: %v", err)
	}
	if _, err := Fallback().Complete(ctx, afagent.Request{}); err == nil {
		t.Fatal("no models: no error")
	}
}

func TestMeter(t *testing.T) {
	var calls []Call
	record := func(ctx context.Context, c Call) { calls = append(calls, c) }
	primary := &scripted{name: "claude", results: []error{status(503)}}
	// A call that takes no time may measure 0 on a coarse clock.
	secondary := &scripted{name: "gpt", delay: time.Millisecond}
	// Meter each provider, inside the fallback.
	m := Fallback(Meter(primary, record), Meter(secondary, record))
	if _, err := m.Complete(context.Background(), afagent.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Err == nil || calls[1].Err != nil || calls[1].Response.Usage.InputTokens != 10 || calls[1].Duration <= 0 {
		t.Fatalf("calls = %+v", calls)
	}
}
