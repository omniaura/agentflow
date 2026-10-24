package afhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWantsEventStream(t *testing.T) {
	for accept, want := range map[string]bool{
		"text/event-stream":                         true,
		"application/json, Text/Event-Stream;q=0.9": true,
		"application/json":                          false,
		"":                                          false,
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
	s.SendError(&Error{Status: http.StatusBadGateway, Msg: "the model could not answer", Err: errors.New("secret cause")})
	s.SendError(errors.New("secret internal"))

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
	if events[20] != `event: error`+"\n"+`data: {"error":"the model could not answer"}` || events[21] != `event: error`+"\n"+`data: {"error":"internal error"}` {
		t.Errorf("errors: %q %q", events[20], events[21])
	}
	if strings.Contains(body, "secret") {
		t.Error("an error cause reached the client")
	}
}
