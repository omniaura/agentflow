package afhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

// flaky fails its first write, then accepts writes again.
type flaky struct {
	httptest.ResponseRecorder
	writes int
}

func (f *flaky) Write(b []byte) (int, error) {
	if f.writes++; f.writes == 1 {
		return 0, errors.New("write timeout")
	}
	return f.ResponseRecorder.Write(b)
}

// After a failed write, a stream writes nothing more: a client must never
// get a frame after a broken one.
func TestEventStreamStopsAfterAFailedWrite(t *testing.T) {
	w := &flaky{ResponseRecorder: *httptest.NewRecorder()}
	s := NewEventStream(w)
	s.WriteTimeout = time.Second
	if err := s.Send("text", map[string]string{"text": "a"}); err == nil {
		t.Fatal("the failed write returned nil")
	}
	if err := s.Send("text", map[string]string{"text": "b"}); err == nil || !strings.Contains(err.Error(), "write timeout") {
		t.Fatalf("Send after the failure = %v", err)
	}
	if body := w.Body.String(); body != "" {
		t.Errorf("wrote %q after the failure", body)
	}
}

// deadlineless can't set write deadlines, though it says it can.
type deadlineless struct{ httptest.ResponseRecorder }

func (d *deadlineless) SetWriteDeadline(time.Time) error { return errors.New("conn closed") }

func TestEventStreamDeadlineFailure(t *testing.T) {
	w := &deadlineless{ResponseRecorder: *httptest.NewRecorder()}
	s := NewEventStream(w)
	s.WriteTimeout = time.Second
	if err := s.Send("text", "a"); err == nil || !strings.Contains(err.Error(), "conn closed") {
		t.Fatalf("Send = %v", err)
	}
	if w.Body.Len() != 0 {
		t.Errorf("wrote %q without a deadline", w.Body)
	}
}

func TestEventStreamClose(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewEventStream(w)
	s.Close()
	if err := s.Send("text", "a"); err == nil || w.Body.Len() != 0 {
		t.Errorf("Send after Close = %v, wrote %q", err, w.Body)
	}
}

// deadlines records the write deadlines set on it.
type deadlines struct {
	httptest.ResponseRecorder
	set []time.Time
}

func (d *deadlines) SetWriteDeadline(t time.Time) error {
	d.set = append(d.set, t)
	return nil
}

// Each event's write has a deadline, cleared after it, so a connection
// serving another request later has none.
func TestEventStreamClearsTheDeadline(t *testing.T) {
	w := &deadlines{ResponseRecorder: *httptest.NewRecorder()}
	s := NewEventStream(w)
	s.WriteTimeout = time.Second
	if err := s.Send("text", "a"); err != nil {
		t.Fatal(err)
	}
	if len(w.set) != 2 || w.set[0].IsZero() || !w.set[1].IsZero() {
		t.Errorf("deadlines set: %v", w.set)
	}
}
