package afhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/omniaura/go-kit/errs"
)

func TestWantsEventStream(t *testing.T) {
	for accept, want := range map[string]bool{
		"text/event-stream":                          true,
		"application/json, Text/Event-Stream;q=0.9":  true,
		"text/event-stream; charset=utf-8":           true,
		"text/event-stream;q=0":                      false,
		"text/event-stream; Q=0.0, application/json": false,
		"text/event-stream;q=x":                      false,
		"text/event-stream;q=0, text/event-stream":   true,
		"application/json":                           false,
		"":                                           false,
	} {
		r := httptest.NewRequest("POST", "/", nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		if got := WantsEventStream(r); got != want {
			t.Errorf("%q: got %v", accept, got)
		}
	}
}

func TestEventStream(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewEventStream(w)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Send("tick", map[string]int{"n": i})
		}()
	}
	wg.Wait()
	ctx := errs.WithRef(context.Background(), "r1")
	s.SendError(ctx, ErrModelFailed.New(ctx).AddError(errors.New("secret cause")))
	s.SendError(ctx, errors.New("secret internal"))

	if w.Code != 200 || w.Header().Get("Content-Type") != "text/event-stream" || !w.Flushed {
		t.Fatalf("code %d, headers %v, flushed %v", w.Code, w.Header(), w.Flushed)
	}
	body := w.Body.String()
	// Concurrent sends never interleave: every event is whole.
	events := strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n")
	if len(events) != 22 {
		t.Fatalf("%d events:\n%s", len(events), body)
	}
	for _, e := range events[:20] {
		var n int
		if _, err := fmt.Sscanf(e, "event: tick\ndata: {\"n\":%d}", &n); err != nil {
			t.Errorf("malformed event %q", e)
		}
	}
	if events[20] != `event: error`+"\n"+`data: {"message":"the model could not answer","code":"model_failed","ref":"r1","action":"retry","status":502,"retryable":true}` ||
		events[21] != `event: error`+"\n"+`data: {"message":"unknown error","code":"internal_error","ref":"r1","status":500}` {
		t.Errorf("errors: %q %q", events[20], events[21])
	}
	if strings.Contains(body, "secret") {
		t.Error("an error cause reached the client")
	}
}
