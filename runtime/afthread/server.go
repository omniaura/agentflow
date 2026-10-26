package afthread

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
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

// Register adds to mux, under prefix such as "/threads":
//
//	POST {prefix}/{thread}/messages  queues a SendRequest for the thread's agent: 202 and the SentMessage
//	GET  {prefix}/{thread}/messages  the conversation: 200 and ThreadMessages
//
// With a Worker, the agent answers in the background. A client sending
// Accept: text/event-stream instead gets the run as server-sent events:
// "queued" with the SentMessage, the agent's events as afagent.ClientEvent
// names them ("text", "tool_called", ...), then "done" with the
// SentMessage as it ended. When another worker holds the thread, "done"
// comes at once with the message still queued: that worker answers it.
func (s *Server) Register(mux *http.ServeMux, prefix string) {
	if s.Owner == nil {
		panic("afthread: Server.Owner is not set")
	}
	prefix = strings.TrimSuffix(prefix, "/")
	mux.HandleFunc("POST "+prefix+"/{thread}/messages", s.send)
	mux.HandleFunc("GET "+prefix+"/{thread}/messages", s.messages)
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
	maxBytes := s.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	var body SendRequest
	if err := afhttp.DecodeBody(w, r, maxBytes, &body); err != nil {
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
	stream.WriteTimeout = s.WriteTimeout
	if stream.WriteTimeout <= 0 {
		stream.WriteTimeout = 10 * time.Second
	}
	// The run goes on in the background when the client goes away or stops
	// reading; this request then ends, and the stream writes no more.
	var gone sync.Once
	broken := make(chan struct{})
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
