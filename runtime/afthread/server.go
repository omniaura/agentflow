package afthread

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afhttp"
	"github.com/omniaura/go-kit/errs"
)

// Server serves threads over HTTP, as a chat client uses them: it sends a
// message to a thread's agent, which answers in the background, and reads
// the conversation back. Create threads with Store.Create, such as when a
// user starts a chat.
//
//	srv := &afthread.Server{Store: store, Worker: worker, Owner: userID}
//	srv.Register(mux, "/threads")
//	...
//	httpServer.Shutdown(ctx)
//	srv.Shutdown(ctx)
type Server struct {
	Store Store
	// Worker answers sent messages. Nil leaves them queued, for workers
	// of your own.
	Worker *Worker
	// Agents, when set, are the agents a client may start a thread with:
	// POST {prefix} creates one. Nil leaves creating threads to you, with
	// Store.Create.
	Agents []string
	// Owner authenticates the request and returns whose threads it may
	// use. Its error fails the request as it is when it is an
	// *errs.Error; wrap authentication failures with afhttp.Unauthorized.
	Owner func(r *http.Request) (string, error)
	// MaxBytes caps the request body; 0 means 1 MiB.
	MaxBytes int64
	// OnError writes a failed request's error; nil means afhttp.WriteError.
	OnError func(w http.ResponseWriter, r *http.Request, err error)
	// OnDrainError, when set, gets the error of a background Drain: a run
	// that failed, or a store that did.
	OnDrainError func(ctx context.Context, owner, thread string, err error)
	// PollInterval is how often a client following a run checks for its
	// new events. 0 means half a second.
	PollInterval time.Duration
	// FollowTimeout bounds how long one request follows a run, so a run
	// left running by a crashed worker can't hold it forever. 0 means 30
	// minutes.
	FollowTimeout time.Duration
	// WriteTimeout bounds each event's write to a streaming client, so one
	// that stops reading can't hold up the run: past it, the client gets
	// no more events and the run goes on. 0 means 10 seconds.
	WriteTimeout time.Duration

	wg       sync.WaitGroup
	once     sync.Once
	life     context.Context
	shutdown context.CancelFunc
	mu       sync.Mutex
	closing  bool
}

// errShuttingDown refuses messages once Shutdown has begun.
var errShuttingDown = errs.NewFactory(http.StatusServiceUnavailable, "the server is shutting down", errs.WithCode("shutting_down"), errs.WithAction(errs.ActionRetry))

// NewThread is the body of POST {prefix}.
type NewThread struct {
	// Agent is one of the server's Agents.
	Agent string `json:"agent"`
	Title string `json:"title,omitempty"`
}

// ThreadView is a thread, as a client sees it.
type ThreadView struct {
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`
	Title   string    `json:"title,omitempty"`
	Created time.Time `json:"created"`
}

// SendRequest is the body of POST {prefix}/{thread}/messages.
type SendRequest struct {
	// Message is the user's new message.
	Message string `json:"message"`
	// Key makes sending idempotent, as a client retrying a request needs:
	// a key sent before returns that message. The Idempotency-Key header
	// sets it too.
	Key string `json:"key,omitempty"`
}

// SentMessage is a queued message, as POST {prefix}/{thread}/messages
// answers. Why a failed message failed stays on the server: it may hold a
// provider's or the database's error.
type SentMessage struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Status Status `json:"status"`
}

// ThreadMessage is a message of the conversation, as a client sees it.
type ThreadMessage struct {
	// Role is "user" or "assistant".
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ThreadMessages is the answer to GET {prefix}/{thread}/messages.
type ThreadMessages struct {
	Messages []ThreadMessage `json:"messages"`
}

// RunView is a run of a thread's agent, as a client sees it. Why a failed
// run failed stays on the server.
type RunView struct {
	ID        string        `json:"id"`
	Status    RunStatus     `json:"status"`
	Started   time.Time     `json:"started"`
	Ended     *time.Time    `json:"ended,omitempty"`
	Turns     int           `json:"turns"`
	ToolCalls int           `json:"tool_calls"`
	Usage     afagent.Usage `json:"usage"`
}

// ThreadRunList is the answer to GET {prefix}/{thread}/runs.
type ThreadRunList struct {
	Runs []RunView `json:"runs"`
}

// RunEventView is a recorded event of a run, as a client sees it: what
// afagent.ClientEvent would show of it. Seq numbers it, to resume after.
type RunEventView struct {
	Seq  int64          `json:"seq"`
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

// RunEvents is the answer to GET {prefix}/{thread}/runs/{run}/events
// without Accept: text/event-stream.
type RunEvents struct {
	Run    RunView        `json:"run"`
	Events []RunEventView `json:"events"`
}

// Register adds to mux, under prefix such as "/threads":
//
//	POST {prefix}                             with Agents: creates the caller's thread for a NewThread: 201 and the ThreadView
//	POST {prefix}/{thread}/messages           queues a SendRequest for the thread's agent: 202 and the SentMessage
//	GET  {prefix}/{thread}/messages           the conversation: 200 and ThreadMessages
//	GET  {prefix}/{thread}/runs               the thread's latest runs, newest first: ThreadRunList
//	GET  {prefix}/{thread}/runs/{run}/events  a run's events after ?after=seq: RunEvents
//
// With a Worker, the agent answers in the background. A client sending
// Accept: text/event-stream instead gets the run as server-sent events:
// "queued" with the SentMessage, the agent's events as afagent.ClientEvent
// names them ("text", "tool_called", ...), then "done" with the
// SentMessage as it ended. When another worker holds the thread, "done"
// comes at once with the message still queued: that worker answers it.
// A key sent before, for a message already answered, gets only "done".
//
// The run endpoints follow a run from the database, as a client that got
// a 202, or lost its stream, does; they need a Store that implements Runs
// and ThreadRuns. With Accept: text/event-stream, the events endpoint
// streams the run's events, each with its seq as the event ID, until the
// run ends, then "done" with the RunView (a crashed worker's run ends
// when the next worker reclaims the thread). A client that reconnects with
// Last-Event-ID resumes after it. Text isn't recorded, so read the answer
// from the messages once the run is done.
func (s *Server) Register(mux *http.ServeMux, prefix string) {
	if s.Owner == nil {
		panic("afthread: Server.Owner is not set")
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if len(s.Agents) > 0 {
		mux.HandleFunc("POST "+prefix, s.create)
	}
	mux.HandleFunc("POST "+prefix+"/{thread}/messages", s.send)
	mux.HandleFunc("GET "+prefix+"/{thread}/messages", s.messages)
	mux.HandleFunc("GET "+prefix+"/{thread}/runs", s.runs)
	mux.HandleFunc("GET "+prefix+"/{thread}/runs/{run}/events", s.events)
}

// Shutdown waits for the runs the server started to finish, like
// http.Server.Shutdown: call it after that, so no request starts another.
// If ctx ends first, it cancels them, which fails their messages, and
// returns ctx's error without waiting for them to stop.
func (s *Server) Shutdown(ctx context.Context) error {
	s.init()
	// No request may start a run once Wait may be waiting.
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			// Both were ready: the runs finished in time.
			return nil
		default:
		}
		// Cancellation is cooperative: a run may take a while to stop, and
		// Shutdown doesn't wait for it past ctx.
		s.shutdown()
		return ctx.Err()
	}
}

// enter counts a request that may start a run, unless Shutdown has begun.
func (s *Server) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *Server) writeTimeout() time.Duration {
	if s.WriteTimeout > 0 {
		return s.WriteTimeout
	}
	return 10 * time.Second
}

func (s *Server) init() {
	s.once.Do(func() { s.life, s.shutdown = context.WithCancel(context.Background()) })
}

// detach returns a ctx for a run started by a request: it keeps the
// request's values but not its cancellation, since a client that goes away
// mustn't stop the run, and Shutdown can cancel it.
func (s *Server) detach(ctx context.Context) (context.Context, context.CancelFunc) {
	s.init()
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(s.life, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		err = afhttp.ErrNotFound.New(r.Context()).AddError(err)
	}
	if s.OnError != nil {
		s.OnError(w, r, err)
		return
	}
	afhttp.WriteError(w, r, err)
}

func (s *Server) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return 1 << 20
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner, err := s.Owner(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body NewThread
	if err := afhttp.DecodeBody(w, r, s.maxBytes(), &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if !slices.Contains(s.Agents, body.Agent) {
		s.fail(w, r, afhttp.ErrInvalidRequest.New(ctx).Field("agent", "must be one of "+strings.Join(s.Agents, ", ")))
		return
	}
	t, err := s.Store.Create(ctx, Thread{Owner: owner, Agent: body.Agent, Title: body.Title})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	afhttp.WriteJSON(w, r, http.StatusCreated, ThreadView{ID: t.ID, Agent: t.Agent, Title: t.Title, Created: t.Created})
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.enter() {
		s.fail(w, r, errShuttingDown.New(ctx))
		return
	}
	defer s.wg.Done()
	owner, err := s.Owner(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body SendRequest
	if err := afhttp.DecodeBody(w, r, s.maxBytes(), &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		s.fail(w, r, afhttp.ErrInvalidRequest.New(ctx).Field("message", "is required"))
		return
	}
	key := body.Key
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	if key == "" {
		// A key of our own, to look the message up once it is answered.
		key = rand.Text()
	}
	thread := r.PathValue("thread")
	in, err := s.Store.Send(ctx, owner, thread, Incoming{Key: key, Content: body.Message})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Worker == nil || !afhttp.WantsEventStream(r) {
		if s.Worker != nil {
			s.drain(ctx, owner, thread)
		}
		afhttp.WriteJSON(w, r, http.StatusAccepted, sent(in))
		return
	}
	stream := afhttp.NewEventStream(w)
	stream.WriteTimeout = s.writeTimeout()
	// The run goes on in the background when the client goes away or stops
	// reading; this request then ends, and the stream writes no more.
	var gone sync.Once
	broken := make(chan struct{})
	if in.Status == Done || in.Status == Failed {
		// A key sent before, for a message already answered: there is no
		// run to stream. One still started may be a crashed worker's,
		// which draining reclaims.
		stream.Send("done", sent(in))
		return
	}
	if stream.Send("queued", sent(in)) != nil {
		gone.Do(func() { close(broken) })
	}
	run, cancel := s.detach(ctx)
	run = afagent.WithStream(run, func(e afagent.Event) {
		if name, data, ok := afagent.ClientEvent(e); ok {
			if stream.Send(name, data) != nil {
				gone.Do(func() { close(broken) })
			}
		}
	})
	done := make(chan struct{})
	s.wg.Go(func() {
		defer close(done)
		defer cancel()
		s.drainNow(run, owner, thread)
	})
	select {
	case <-done:
	case <-ctx.Done():
		stream.Close()
		return
	case <-broken:
		stream.Close()
		return
	}
	// Sending the key again looks the message up.
	in, err = s.Store.Send(context.WithoutCancel(ctx), owner, thread, Incoming{Key: key, Content: body.Message})
	if err != nil {
		stream.SendError(ctx, err)
		return
	}
	stream.Send("done", sent(in))
}

// drain answers the thread's messages in the background.
func (s *Server) drain(ctx context.Context, owner, thread string) {
	ctx, cancel := s.detach(ctx)
	s.wg.Go(func() {
		defer cancel()
		s.drainNow(ctx, owner, thread)
	})
}

// drainNow runs Worker.Drain in a goroutine of the server's, where a panic
// would end the process: it reports one as an error instead.
func (s *Server) drainNow(ctx context.Context, owner, thread string) {
	defer func() {
		if p := recover(); p != nil {
			s.reportDrain(ctx, owner, thread, fmt.Errorf("afthread: draining the thread panicked: %v", p))
		}
	}()
	if err := s.Worker.Drain(ctx, owner, thread); err != nil {
		s.reportDrain(ctx, owner, thread, err)
	}
}

// reportDrain hands err to OnDrainError, ignoring a panic in it.
func (s *Server) reportDrain(ctx context.Context, owner, thread string, err error) {
	if s.OnDrainError == nil {
		return
	}
	defer func() { _ = recover() }()
	s.OnDrainError(context.WithoutCancel(ctx), owner, thread, err)
}

func sent(in Incoming) SentMessage {
	return SentMessage{ID: in.ID, Key: in.Key, Status: in.Status}
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	owner, err := s.Owner(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	msgs, err := s.Store.Transcript(r.Context(), owner, r.PathValue("thread"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	afhttp.WriteJSON(w, r, http.StatusOK, ThreadMessages{Messages: clientView(msgs)})
}

// clientView is the conversation a client sees: the user's messages and
// the agent's answers. Tool calls and results stay on the server, as
// afagent.ClientEvent keeps them, and so do user messages the user didn't
// write (a Source): the loop's prompts, other agents' messages, summaries.
func clientView(msgs []afagent.Message) []ThreadMessage {
	out := []ThreadMessage{}
	for _, m := range msgs {
		switch {
		case m.Role == afagent.User && m.Source == "":
			out = append(out, ThreadMessage{Role: "user", Content: m.Content})
		case m.Role == afagent.Assistant && m.Content != "":
			out = append(out, ThreadMessage{Role: "assistant", Content: m.Content})
		}
	}
	return out
}

// runStores returns the Store as Runs and ThreadRuns, if it is both.
func (s *Server) runStores() (Runs, ThreadRuns, bool) {
	runs, ok := s.Store.(Runs)
	list, ok2 := s.Store.(ThreadRuns)
	return runs, list, ok && ok2
}

func runView(r RunRecord) RunView {
	v := RunView{ID: r.ID, Status: r.Status, Started: r.Started, Turns: r.Turns, ToolCalls: r.ToolCalls, Usage: r.Usage}
	if !r.Ended.IsZero() {
		ended := r.Ended
		v.Ended = &ended
	}
	return v
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	_, list, ok := s.runStores()
	if !ok {
		s.fail(w, r, afhttp.ErrNotFound.New(r.Context()))
		return
	}
	owner, err := s.Owner(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := list.ThreadRuns(r.Context(), owner, r.PathValue("thread"), 20)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := ThreadRunList{Runs: []RunView{}}
	for _, rec := range recs {
		out.Runs = append(out.Runs, runView(rec))
	}
	afhttp.WriteJSON(w, r, http.StatusOK, out)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runs, _, ok := s.runStores()
	if !ok {
		s.fail(w, r, afhttp.ErrNotFound.New(ctx))
		return
	}
	owner, err := s.Owner(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rec, err := runs.GetRun(ctx, owner, r.PathValue("run"))
	if err == nil && rec.Thread != r.PathValue("thread") {
		err = ErrNotFound
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	after := r.Header.Get("Last-Event-ID")
	if q := r.URL.Query().Get("after"); q != "" {
		after = q
	}
	var seq int64
	if after != "" {
		if err := afhttp.Parse("after", after, &seq); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if !afhttp.WantsEventStream(r) {
		events, err := runs.Events(ctx, owner, rec.ID, seq)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if rec.Status == Running {
			// It may have ended since: then show it ended, with the events
			// it recorded meanwhile.
			if rec, err = runs.GetRun(ctx, owner, rec.ID); err == nil && rec.Status != Running {
				from := seq
				if len(events) > 0 {
					from = events[len(events)-1].Seq
				}
				var more []RunEvent
				more, err = runs.Events(ctx, owner, rec.ID, from)
				events = append(events, more...)
			}
			if err != nil {
				s.fail(w, r, err)
				return
			}
		}
		out := RunEvents{Run: runView(rec), Events: []RunEventView{}}
		for _, e := range events {
			if v, ok := clientRunEvent(e); ok {
				out.Events = append(out.Events, v)
			}
		}
		afhttp.WriteJSON(w, r, http.StatusOK, out)
		return
	}
	stream := afhttp.NewEventStream(w)
	stream.WriteTimeout = s.writeTimeout()
	poll := s.PollInterval
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	follow := s.FollowTimeout
	if follow <= 0 {
		follow = 30 * time.Minute
	}
	// A run whose worker crashed stays running until a worker reclaims the
	// thread: past FollowTimeout, or on Shutdown, the stream ends without
	// "done", and the client reconnects with Last-Event-ID.
	s.init()
	deadline := time.After(follow)
	for {
		// Read the run before its events: once it has ended, the events
		// read after hold all of them.
		rec, err := runs.GetRun(ctx, owner, rec.ID)
		if err != nil {
			stream.SendError(ctx, err)
			return
		}
		events, err := runs.Events(ctx, owner, rec.ID, seq)
		if err != nil {
			stream.SendError(ctx, err)
			return
		}
		for _, e := range events {
			seq = e.Seq
			if v, ok := clientRunEvent(e); ok {
				if stream.SendEvent(strconv.FormatInt(e.Seq, 10), v.Kind, v.Data) != nil {
					return
				}
			}
		}
		if rec.Status != Running {
			stream.Send("done", runView(rec))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-s.life.Done():
			return
		case <-deadline:
			return
		case <-time.After(poll):
		}
	}
}

// clientRunEvent is what a client sees of a recorded event, as
// afagent.ClientEvent shows a live one: a tool's name and whether it
// failed, never its arguments or result. Other events stay on the server.
func clientRunEvent(e RunEvent) (RunEventView, bool) {
	var d struct {
		Call  *afagent.ToolCall `json:"call"`
		Error string            `json:"error"`
	}
	switch afagent.EventKind(e.Kind) {
	case afagent.ToolCalled, afagent.ToolReturned:
		if json.Unmarshal(e.Data, &d) != nil || d.Call == nil {
			return RunEventView{}, false
		}
		data := map[string]any{"name": d.Call.Name}
		if afagent.EventKind(e.Kind) == afagent.ToolReturned {
			data["failed"] = d.Error != ""
		}
		return RunEventView{Seq: e.Seq, Kind: e.Kind, Data: data}, true
	case afagent.AnswerRejected:
		return RunEventView{Seq: e.Seq, Kind: e.Kind, Data: map[string]any{}}, true
	}
	return RunEventView{}, false
}
