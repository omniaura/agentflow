package afthread_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afhttp"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// newServer serves the store's threads for the user named in X-User, with
// a worker running agent.
func newServer(t *testing.T, s *afthread.Memory, agent afthread.Agent) (*afthread.Server, *http.ServeMux) {
	srv := &afthread.Server{
		Store:  s,
		Worker: &afthread.Worker{Store: s, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) { return agent, nil }},
		Owner: func(r *http.Request) (string, error) {
			if u := r.Header.Get("X-User"); u != "" {
				return u, nil
			}
			return "", afhttp.Unauthorized(r.Context(), errors.New("no user"))
		},
	}
	mux := http.NewServeMux()
	srv.Register(mux, "/threads/")
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	return srv, mux
}

// settle waits for the thread's messages to be answered.
func settle(t *testing.T, s *afthread.Memory, owner, thread string) {
	t.Helper()
	for range 200 {
		busy := false
		for _, in := range s.Inbox(owner, thread) {
			busy = busy || in.Status == afthread.Queued || in.Status == afthread.Started
		}
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("messages still waiting: %+v", s.Inbox(owner, thread))
}

func do(mux *http.ServeMux, method, path, user, body string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		r.Header.Set("X-User", user)
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// toolThenAnswer looks something up, then answers.
func toolThenAnswer() afthread.Agent {
	return func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		turn := 0
		model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
			if turn++; turn == 1 {
				return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: `{"secret":"arg"}`}}}}, nil
			}
			if req.OnText != nil {
				req.OnText("It arrives ")
				req.OnText("Tuesday.")
			}
			return afagent.Response{Message: afagent.Message{Content: "It arrives Tuesday."}}, nil
		})
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, BeforeTurn: before,
			Dispatch: func(context.Context, afagent.ToolCall) (string, error) { return "secret result", nil }}
		return l.Run(ctx, msgs)
	}
}

func TestServerSendsAndReads(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	_, mux := newServer(t, s, toolThenAnswer())

	w := do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"Where is A-1?","key":"k1"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	var sent afthread.SentMessage
	json.Unmarshal(w.Body.Bytes(), &sent)
	if sent.ID == 0 || sent.Key != "k1" || sent.Status != afthread.Queued {
		t.Errorf("sent = %+v", sent)
	}
	// The agent answers in the background.
	settle(t, s, "ann", th.ID)
	w = do(mux, "GET", "/threads/"+th.ID+"/messages", "ann", "")
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	// Tool calls and results stay on the server.
	if got := strings.TrimSpace(w.Body.String()); got != `{"messages":[{"role":"user","content":"Where is A-1?"},{"role":"assistant","content":"It arrives Tuesday."}]}` {
		t.Errorf("messages = %s", got)
	}

	// The same key again is the same message, now answered: no new run.
	w = do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"Where is A-1?"}`, "Idempotency-Key", "k1")
	json.Unmarshal(w.Body.Bytes(), &sent)
	if w.Code != http.StatusAccepted || sent.Status != afthread.Done {
		t.Errorf("resend: %d %+v", w.Code, sent)
	}
	settle(t, s, "ann", th.ID)
	if in := s.Inbox("ann", th.ID); len(in) != 1 {
		t.Errorf("inbox = %+v", in)
	}
	// A key of the server's own when the client gives none.
	w = do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"thanks"}`)
	json.Unmarshal(w.Body.Bytes(), &sent)
	if sent.Key == "" {
		t.Errorf("no key: %+v", sent)
	}
}

func TestServerErrors(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	srv, mux := newServer(t, s, toolThenAnswer())
	for _, tt := range []struct {
		name, method, path, user, body string
		status                         int
	}{
		{"no user", "POST", "/threads/" + th.ID + "/messages", "", `{"message":"hi"}`, 401},
		{"another owner's thread", "POST", "/threads/" + th.ID + "/messages", "bob", `{"message":"hi"}`, 404},
		{"another owner reads", "GET", "/threads/" + th.ID + "/messages", "bob", "", 404},
		{"no thread", "GET", "/threads/nope/messages", "ann", "", 404},
		{"empty message", "POST", "/threads/" + th.ID + "/messages", "ann", `{"message":"  "}`, 400},
		{"unknown field", "POST", "/threads/" + th.ID + "/messages", "ann", `{"message":"hi","history":[]}`, 400},
	} {
		if w := do(mux, tt.method, tt.path, tt.user, tt.body); w.Code != tt.status {
			t.Errorf("%s: %d %s", tt.name, w.Code, w.Body)
		}
	}
	if in := s.Inbox("ann", th.ID); len(in) != 0 {
		t.Errorf("a failed request queued %+v", in)
	}

	// A background run's failure reaches OnDrainError.
	failed := make(chan error, 1)
	srv.OnDrainError = func(ctx context.Context, owner, thread string, err error) { failed <- err }
	srv.Worker.Agent = func(context.Context, afthread.Thread) (afthread.Agent, error) { return nil, errors.New("no agent") }
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"hi"}`)
	select {
	case err := <-failed:
		if !strings.Contains(err.Error(), "no agent") {
			t.Errorf("OnDrainError got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("OnDrainError wasn't called")
	}

	// Without a worker, messages wait for workers of your own.
	srv.Worker = nil
	if w := do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"later"}`); w.Code != http.StatusAccepted {
		t.Errorf("without a worker: %d %s", w.Code, w.Body)
	}
	if in := s.Inbox("ann", th.ID); in[len(in)-1].Status != afthread.Queued {
		t.Errorf("inbox = %+v", in)
	}
}

// events reads a server-sent event stream as "name data" lines.
func events(body string) []string {
	var out []string
	var name string
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			out = append(out, name+" "+strings.TrimPrefix(line, "data: "))
		}
	}
	return out
}

func TestServerStreams(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	_, mux := newServer(t, s, toolThenAnswer())
	w := do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"Where is A-1?","key":"k1"}`, "Accept", "text/event-stream")
	got := strings.Join(events(w.Body.String()), "\n")
	want := strings.Join([]string{
		`queued {"id":1,"key":"k1","status":"queued"}`,
		`tool_called {"name":"look"}`,
		`tool_returned {"failed":false,"name":"look"}`,
		`text {"text":"It arrives "}`,
		`text {"text":"Tuesday."}`,
		`done {"id":1,"key":"k1","status":"done"}`,
	}, "\n")
	if got != want {
		t.Errorf("stream\n got %s\nwant %s", got, want)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Error("the stream leaked tool data")
	}

	// When another worker holds the thread, the message waits for it.
	s.Lock(ctx, "ann", th.ID, "elsewhere", time.Minute)
	w = do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"and B-2?","key":"k2"}`, "Accept", "text/event-stream")
	ev := events(w.Body.String())
	if len(ev) != 2 || ev[1] != `done {"id":2,"key":"k2","status":"queued"}` {
		t.Errorf("stream while leased = %v", ev)
	}

	// Sending a key again for a message already answered streams no run.
	w = do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"Where is A-1?","key":"k1"}`, "Accept", "text/event-stream")
	if ev := events(w.Body.String()); len(ev) != 1 || ev[0] != `done {"id":1,"key":"k1","status":"done"}` {
		t.Errorf("stream of an answered message = %v", ev)
	}
}

// A client that disconnects mid-stream doesn't stop the run.
func TestServerRunOutlivesTheClient(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	var canceled atomic.Bool
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		time.Sleep(50 * time.Millisecond)
		canceled.Store(ctx.Err() != nil)
		answer := afagent.Message{Role: afagent.Assistant, Content: "ok"}
		return afagent.Result{Turns: 1, Messages: append(msgs, answer)}, nil
	}
	srv, mux := newServer(t, s, agent)
	reqCtx, cancel := context.WithCancel(ctx)
	r := httptest.NewRequestWithContext(reqCtx, "POST", "/threads/"+th.ID+"/messages", strings.NewReader(`{"message":"hi"}`))
	r.Header.Set("X-User", "ann")
	r.Header.Set("Accept", "text/event-stream")
	time.AfterFunc(10*time.Millisecond, cancel)
	start := time.Now()
	mux.ServeHTTP(httptest.NewRecorder(), r)
	if took := time.Since(start); took > 40*time.Millisecond {
		t.Errorf("the request lasted %v, waiting on the run", took)
	}
	// The request ended with the client; the run didn't.
	srv.Shutdown(ctx)
	if canceled.Load() {
		t.Error("the run's ctx was canceled with the request")
	}
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Done {
		t.Errorf("message = %+v", in[0])
	}
}

// Shutdown waits for background runs, and cancels them once its ctx ends.
func TestServerShutdown(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	release := make(chan struct{})
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return afagent.Result{}, ctx.Err()
		}
		answer := afagent.Message{Role: afagent.Assistant, Content: "ok"}
		return afagent.Result{Turns: 1, Messages: append(msgs, answer)}, nil
	}
	srv, mux := newServer(t, s, agent)
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"one"}`)
	time.AfterFunc(20*time.Millisecond, func() { close(release) })
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Done {
		t.Fatalf("Shutdown didn't wait: %+v", in[0])
	}

	// A run that won't end is canceled when Shutdown's ctx does.
	srv2, mux2 := newServer(t, s, func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		<-ctx.Done()
		return afagent.Result{}, ctx.Err()
	})
	do(mux2, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"two"}`)
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := srv2.Shutdown(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v", err)
	}
	// The canceled run stops soon after.
	settle(t, s, "ann", th.ID)
	in := s.Inbox("ann", th.ID)
	if in[1].Status != afthread.Failed {
		t.Errorf("message = %+v", in[1])
	}
	// Shut down, the server takes no more messages.
	if w := do(mux2, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"three"}`); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"action":"retry"`) {
		t.Errorf("send after Shutdown: %d %s", w.Code, w.Body)
	}
	// The client never sees why a message failed.
	_, mux3 := newServer(t, s, nil)
	w := do(mux3, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"two"}`, "Idempotency-Key", in[1].Key)
	if !strings.Contains(w.Body.String(), `"status":"failed"`) || strings.Contains(w.Body.String(), "context") {
		t.Errorf("resend of a failed message = %s", w.Body)
	}
}

// A streaming client that stops reading doesn't hold up the run.
func TestServerStalledClient(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	chunk := strings.Repeat("x", 64<<10)
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
			// Far more than the connection's buffers hold.
			for range 400 {
				req.OnText(chunk)
			}
			return afagent.Response{Message: afagent.Message{Content: "long"}}, nil
		})
		return (&afagent.Loop{Model: model, BeforeTurn: before}).Run(ctx, msgs)
	}
	srv, mux := newServer(t, s, agent)
	srv.WriteTimeout = 100 * time.Millisecond
	hs := httptest.NewServer(mux)
	defer hs.Close()
	conn, err := net.Dial("tcp", hs.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body := `{"message":"write a lot"}`
	fmt.Fprintf(conn, "POST /threads/%s/messages HTTP/1.1\r\nHost: x\r\nX-User: ann\r\nAccept: text/event-stream\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", th.ID, len(body), body)
	// The client never reads. The run still finishes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if in := s.Inbox("ann", th.ID); len(in) == 1 && in[0].Status == afthread.Done {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the run is stuck behind the client: %+v", s.Inbox("ann", th.ID))
}

// panicky panics when a worker leases its threads.
type panicky struct{ *afthread.Memory }

func (p panicky) Lock(context.Context, string, string, string, time.Duration) (bool, error) {
	panic("store bug")
}

// A panic in a background run is an error to report, never a crash.
func TestServerSurvivesPanics(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	failed := make(chan error, 4)
	panicking := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		panic("agent bug")
	}
	srv, mux := newServer(t, s, panicking)
	srv.OnDrainError = func(ctx context.Context, owner, thread string, err error) {
		failed <- err
		panic("reporter bug too")
	}
	wait := func(want string) {
		t.Helper()
		select {
		case err := <-failed:
			if !strings.Contains(err.Error(), want) {
				t.Errorf("OnDrainError got %v, want %q", err, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("OnDrainError wasn't called for %q", want)
		}
	}
	// The agent: the run fails like any other.
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"a"}`)
	wait("the agent panicked: agent bug")
	settle(t, s, "ann", th.ID)
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Failed {
		t.Errorf("message = %+v", in[0])
	}
	// The worker's Agent: the thread isn't left leased.
	srv.Worker.Agent = func(context.Context, afthread.Thread) (afthread.Agent, error) { panic("lookup bug") }
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"b"}`, "Accept", "text/event-stream")
	wait("panic: lookup bug")
	if held, _ := s.Lock(ctx, "ann", th.ID, "next", time.Minute); !held {
		t.Error("the thread is still leased")
	}
	s.Unlock(ctx, "ann", th.ID, "next")
	// The store, outside any run.
	srv.Store, srv.Worker.Store = panicky{s}, panicky{s}
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"c"}`)
	wait("panic: store bug")

	// A server without Owner can't authenticate anyone.
	defer func() {
		if p := recover(); p == nil || !strings.Contains(fmt.Sprint(p), "Owner is not set") {
			t.Errorf("Register without Owner: %v", p)
		}
	}()
	(&afthread.Server{Store: s}).Register(http.NewServeMux(), "/x")
}

// failingWriter fails every write, as a connection that is gone does.
type failingWriter struct{ header http.Header }

func (f *failingWriter) Header() http.Header       { return f.header }
func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func (f *failingWriter) WriteHeader(int)           {}

// A stream that fails from its first event ends the request at once.
func TestServerStreamFailsAtOnce(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	release := make(chan struct{})
	srv, mux := newServer(t, s, func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		<-release
		return afagent.Result{Turns: 1, Messages: append(msgs, afagent.Message{Role: afagent.Assistant, Content: "ok"})}, nil
	})
	r := httptest.NewRequest("POST", "/threads/"+th.ID+"/messages", strings.NewReader(`{"message":"hi"}`))
	r.Header.Set("X-User", "ann")
	r.Header.Set("Accept", "text/event-stream")
	returned := make(chan struct{})
	go func() {
		mux.ServeHTTP(&failingWriter{header: http.Header{}}, r)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Error("the request waits on the run")
	}
	close(release)
	srv.Shutdown(ctx)
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Done {
		t.Errorf("message = %+v", in[0])
	}
}

// The client sees what the user wrote and the agent answered: not the
// loop's prompts, other agents' messages or summaries.
func TestServerShowsOnlyTheUsersMessages(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	s.Append(ctx, "ann", th.ID,
		afagent.Message{Role: afagent.User, Content: "hi"},
		afagent.Message{Role: afagent.User, Content: "[Summary]", Source: "summary"},
		afagent.Message{Role: afagent.Assistant, Content: "{bad}"},
		afagent.Message{Role: afagent.User, Content: "Your answer was rejected", Source: afagent.SourceLoop},
		afagent.Message{Role: afagent.User, Content: "[message from the tracker agent]", Source: "agent:tracker"},
		afagent.Message{Role: afagent.Assistant, Content: "hello"},
	)
	_, mux := newServer(t, s, nil)
	w := do(mux, "GET", "/threads/"+th.ID+"/messages", "ann", "")
	if got := strings.TrimSpace(w.Body.String()); got != `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"{bad}"},{"role":"assistant","content":"hello"}]}` {
		t.Errorf("messages = %s", got)
	}
}

// Shutdown returns when its ctx ends, even if a run ignores cancellation.
func TestServerShutdownDoesNotWaitPastItsContext(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	release := make(chan struct{})
	defer close(release)
	srv, mux := newServer(t, s, func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		<-release // Deaf to ctx.
		return afagent.Result{}, nil
	})
	do(mux, "POST", "/threads/"+th.ID+"/messages", "ann", `{"message":"hi"}`)
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(short); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 500*time.Millisecond {
		t.Errorf("Shutdown = %v after %v", err, time.Since(start))
	}
}
