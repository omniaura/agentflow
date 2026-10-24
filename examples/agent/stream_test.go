package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

type sseEvent struct{ name, data string }

// readEvents reads server-sent events from resp as they arrive.
func readEvents(t *testing.T, resp *http.Response) <-chan sseEvent {
	ch := make(chan sseEvent)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		var e sseEvent
		for sc.Scan() {
			switch line := sc.Text(); {
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			case line == "":
				ch <- e
				e = sseEvent{}
			}
		}
	}()
	return ch
}

func postStream(t *testing.T, url, body string) *http.Response {
	req, _ := http.NewRequest("POST", url+"/triage", strings.NewReader(body))
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestServeStreams(t *testing.T) {
	// The model writes its answer in two pieces and waits in between until
	// the client has seen the first: events must reach it as they happen.
	seen := make(chan struct{})
	turn := 0
	model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		turn++
		if turn == 1 {
			return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "lookup_order", `{"order_id":"A-1"}`)}}}, nil
		}
		req.OnText(`{"category":"refund",`)
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			return afagent.Response{}, fmt.Errorf("the client never saw the first text event: the stream is buffered")
		}
		req.OnText(`"order_id":"A-1","urgent":true}`)
		return afagent.Response{Message: afagent.Message{Content: `{"category":"refund","order_id":"A-1","urgent":true}`}, Usage: afagent.Usage{InputTokens: 7}}, nil
	})
	var hooked atomic.Int32
	agent := &TriagerAgent{Model: model, Tools: &shop{}, OnEvent: func(afagent.Event) { hooked.Add(1) }}
	mux := http.NewServeMux()
	(&TriagerAgentServer{
		Agent:   agent,
		Prepare: func(r *http.Request) (*Support, []afagent.Message, error) { return &Support{Shop: "Acme"}, nil, nil },
	}).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp := postStream(t, srv.URL, `{"message":"A-1 was late"}`)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var got []string
	for e := range readEvents(t, resp) {
		got = append(got, e.name+" "+e.data)
		if e.name == "text" && len(got) > 0 && strings.Contains(e.data, "category") {
			close(seen)
		}
		if e.name == "done" {
			var done TriagerAgentResponse
			if err := json.Unmarshal([]byte(e.data), &done); err != nil || done.Output.Category != TriageCategoryRefund || done.Usage.InputTokens != 7 {
				t.Errorf("done = %s (%v)", e.data, err)
			}
		}
	}
	want := []string{
		`tool_called {"name":"lookup_order"}`,
		`tool_returned {"failed":false,"name":"lookup_order"}`,
		`text {"text":"{\"category\":\"refund\","}`,
		`text {"text":"\"order_id\":\"A-1\",\"urgent\":true}"}`,
	}
	if len(got) != 5 || strings.Join(got[:4], "\n") != strings.Join(want, "\n") || !strings.HasPrefix(got[4], "done ") {
		t.Errorf("events:\n%s", strings.Join(got, "\n"))
	}
	// The agent's own OnEvent still ran, and the shared agent is untouched.
	if hooked.Load() == 0 {
		t.Error("the agent's OnEvent was not called")
	}
	if agent.OnEvent == nil {
		t.Error("streaming replaced the shared agent's OnEvent")
	}
}

func TestServeStreamsErrors(t *testing.T) {
	refused := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		return afagent.Response{}, fmt.Errorf("adapter: secret detail: %w", afagent.ErrRefused)
	})
	mux := http.NewServeMux()
	(&TriagerAgentServer{
		Agent:   &TriagerAgent{Model: refused, Tools: &shop{}},
		Prepare: func(r *http.Request) (*Support, []afagent.Message, error) { return &Support{Shop: "Acme"}, nil, nil },
	}).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A failed run, after the stream started: an error event.
	var got []sseEvent
	for e := range readEvents(t, postStream(t, srv.URL, `{"message":"x"}`)) {
		got = append(got, e)
	}
	if len(got) != 1 || got[0].name != "error" || got[0].data != `{"error":"the model could not answer"}` {
		t.Errorf("events = %+v", got)
	}
	// A bad request fails before the stream starts: a plain 400.
	resp := postStream(t, srv.URL, `{}`)
	if resp.StatusCode != 400 || resp.Header.Get("Content-Type") == "text/event-stream" {
		t.Errorf("bad request: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
