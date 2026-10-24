// Package afagent is the runtime for agents declared with .agent in .af
// files: a provider-neutral model interface and the tool-calling loop.
//
// The loop sends the conversation to a Model, runs the tool calls the model
// asks for through a dispatcher, feeds the results back, and stops when the
// model answers without calling a tool. The package has no dependencies;
// adapters for provider SDKs implement Model.
package afagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Role is who wrote a message.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	// Tool messages carry a tool call's result back to the model.
	Tool Role = "tool"
)

// Message is one message of a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`
	// ToolCalls are the calls an assistant message asks for.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID is the call a tool message answers, and IsError marks a
	// failed call.
	ToolCallID string `json:"tool_call_id,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	// Native is the provider's own encoding of an assistant message. Its
	// adapter sends it back as is, keeping what Message can't express,
	// such as thinking blocks; other providers ignore it.
	Native *Native `json:"native,omitempty"`
}

// Native is a message as a provider encodes it.
type Native struct {
	Provider string          `json:"provider"`
	Data     json.RawMessage `json:"data"`
}

// ToolCall is a model's request to run a tool.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments is the JSON the model sent.
	Arguments string `json:"arguments"`
}

// ToolSpec describes a tool to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// Request is one model call.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
	// Output, when set, is the JSON the final answer must be. Adapters
	// pass it as the provider's structured output format; the model may
	// still call tools before answering.
	Output *OutputSpec
	// OnText, when set, receives the answer's text as the model writes
	// it. Adapters that stream call it; others may never call it. Call it
	// from one goroutine at a time, in the order of the text, and only
	// before Complete returns: text after that is dropped.
	OnText func(text string)
}

// OutputSpec describes a structured final answer.
type OutputSpec struct {
	Name        string
	Description string
	// Schema is a strict JSON Schema: every property required, optional
	// ones nullable, no other properties allowed.
	Schema json.RawMessage
}

// Usage counts the tokens of model calls.
type Usage struct {
	// InputTokens counts all input tokens, CachedInputTokens the ones
	// read from the provider's prompt cache.
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens"`
}

func (u *Usage) add(o Usage) {
	u.InputTokens += o.InputTokens
	u.CachedInputTokens += o.CachedInputTokens
	u.OutputTokens += o.OutputTokens
}

// Response is a model's answer: an assistant message.
type Response struct {
	Message Message
	Usage   Usage
}

// Model is a chat model that can call tools.
type Model interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// ModelError is a model call the provider answered with an HTTP error
// status. Adapters return it, so wrappers such as those in afmodel can
// tell a failure worth trying again (rate limited, overloaded) from one
// that isn't (a bad request).
type ModelError struct {
	Status int
	// RetryAfter is how long the provider asked to wait, or 0.
	RetryAfter time.Duration
	Err        error
}

func (e *ModelError) Error() string { return e.Err.Error() }

func (e *ModelError) Unwrap() error { return e.Err }

// duration is n units, saturating at the longest Duration instead of
// overflowing into a negative one.
func duration(n float64, unit time.Duration) time.Duration {
	if ns := n * float64(unit); ns < float64(math.MaxInt64) {
		return time.Duration(ns)
	}
	return math.MaxInt64
}

// RetryAfter reads how long a provider's response asks to wait before
// trying again: retry-after-ms, or Retry-After in seconds or as a date.
// It returns 0 when h says nothing usable.
func RetryAfter(h http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(h.Get("Retry-After-Ms"), 64); err == nil && ms > 0 {
		return duration(ms, time.Millisecond)
	}
	v := h.Get("Retry-After")
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return duration(secs, time.Second)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(t))
	}
	return 0
}

// ModelFunc adapts a function to a Model.
type ModelFunc func(ctx context.Context, req Request) (Response, error)

func (f ModelFunc) Complete(ctx context.Context, req Request) (Response, error) { return f(ctx, req) }

// EventKind is what happened in the loop.
type EventKind string

const (
	// ModelResponded follows each model call.
	ModelResponded EventKind = "model_responded"
	// ToolCalled precedes running a tool, and ToolReturned follows it.
	ToolCalled   EventKind = "tool_called"
	ToolReturned EventKind = "tool_returned"
	// AnswerRejected follows a final answer that failed Check; Err says
	// why, and the model is asked again.
	AnswerRejected EventKind = "answer_rejected"
	// TextDelta carries text as the model writes it, in Text, when the
	// model streams. ModelResponded still follows with the whole message.
	// With Check set, a final answer's text is held until it passes, and
	// dropped if it fails: clients never see a rejected answer.
	TextDelta EventKind = "text_delta"
)

// Event reports progress, for logs, metrics and streaming to a client.
type Event struct {
	Kind EventKind
	// Turn counts model calls from 1.
	Turn     int
	Response *Response
	Call     *ToolCall
	// Result and Err are a tool's outcome.
	Result string
	// Text is a TextDelta's text.
	Text string
	Err  error
}

// Result is a finished run.
type Result struct {
	// Final is the model's last answer.
	Final string
	// Messages is the whole conversation, ending with the final answer.
	Messages []Message
	Turns    int
	Usage    Usage
}

// Model adapters wrap these errors for answers the loop can't use.
var (
	// ErrTruncated means the answer hit the output token limit or the
	// context window, so its text or tool calls may be cut off.
	ErrTruncated = errors.New("afagent: the model's answer was cut off")
	// ErrRefused means the model declined to answer.
	ErrRefused = errors.New("afagent: the model refused")
)

// ErrMaxTurns means the model kept calling tools, or giving answers Check
// rejected, for MaxTurns model calls. After a rejected last answer the
// error also wraps Check's.
var ErrMaxTurns = errors.New("afagent: the agent did not finish within its turn limit")

// DefaultMaxTurns bounds a loop whose MaxTurns is 0.
const DefaultMaxTurns = 10

// Loop runs a tool-calling agent.
type Loop struct {
	Model  Model
	System string
	Tools  []ToolSpec
	// Dispatch runs one tool call. An error goes back to the model as a
	// failed result, so it can recover; it does not stop the loop.
	Dispatch func(ctx context.Context, call ToolCall) (string, error)
	// Output, when set, asks the model for a structured final answer.
	Output *OutputSpec
	// Check, when set, vets a final answer. If it fails, the error goes
	// back to the model as a user message and the loop goes on, within
	// MaxTurns.
	Check func(answer string) error
	// MaxTurns bounds the model calls; 0 means DefaultMaxTurns.
	MaxTurns int
	// Concurrency is how many tool calls of one turn run at once; 0 or 1
	// runs them in order.
	Concurrency int
	// OnEvent, if set, is called from the tools' goroutines when
	// Concurrency is above 1, so it must be safe for concurrent use.
	OnEvent func(Event)
}

// Run continues the conversation in messages until the model answers
// without calling a tool. On ErrMaxTurns, a model error or a canceled
// context, the Result holds the conversation so far.
func (l *Loop) Run(ctx context.Context, messages []Message) (Result, error) {
	res := Result{Messages: append([]Message(nil), messages...)}
	maxTurns := l.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	var rejected error
	for res.Turns < maxTurns {
		rejected = nil
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Turns++
		req := Request{System: l.System, Messages: res.Messages, Tools: l.Tools, Output: l.Output}
		// held is the turn's text while Check may still reject it. The
		// mutex keeps a Model that breaks OnText's contract from racing.
		var (
			mu     sync.Mutex
			held   []Event
			closed bool
		)
		if l.OnEvent != nil {
			turn := res.Turns
			req.OnText = func(text string) {
				e := Event{Kind: TextDelta, Turn: turn, Text: text}
				if l.Check != nil {
					mu.Lock()
					if !closed {
						held = append(held, e)
					}
					mu.Unlock()
					return
				}
				l.emit(e)
			}
		}
		resp, err := l.Model.Complete(ctx, req)
		mu.Lock()
		closed = true
		mu.Unlock()
		if err != nil {
			return res, fmt.Errorf("afagent: turn %d: %w", res.Turns, err)
		}
		resp.Message.Role = Assistant
		res.Usage.add(resp.Usage)
		res.Messages = append(res.Messages, resp.Message)
		final := len(resp.Message.ToolCalls) == 0
		if final && l.Check != nil {
			rejected = l.Check(resp.Message.Content)
		}
		if rejected == nil {
			for _, e := range held {
				l.emit(e)
			}
		}
		l.emit(Event{Kind: ModelResponded, Turn: res.Turns, Response: &resp})
		if final {
			if rejected != nil {
				l.emit(Event{Kind: AnswerRejected, Turn: res.Turns, Err: rejected})
				res.Messages = append(res.Messages, Message{Role: User, Content: fmt.Sprintf("Your answer was rejected: %v. Answer again.", rejected)})
				continue
			}
			res.Final = resp.Message.Content
			return res, nil
		}
		res.Messages = append(res.Messages, l.runTools(ctx, res.Turns, resp.Message.ToolCalls)...)
	}
	if rejected != nil {
		return res, fmt.Errorf("%w: the last answer was rejected: %w", ErrMaxTurns, rejected)
	}
	return res, ErrMaxTurns
}

func (l *Loop) emit(e Event) {
	if l.OnEvent != nil {
		l.OnEvent(e)
	}
}

// runTools runs the calls of one turn and returns their results in call
// order, since providers expect them so.
func (l *Loop) runTools(ctx context.Context, turn int, calls []ToolCall) []Message {
	results := make([]Message, len(calls))
	run := func(i int) {
		call := calls[i]
		l.emit(Event{Kind: ToolCalled, Turn: turn, Call: &call})
		out, err := l.dispatch(ctx, call)
		msg := Message{Role: Tool, ToolCallID: call.ID, Content: out}
		if err != nil {
			msg.Content, msg.IsError = err.Error(), true
		}
		results[i] = msg
		l.emit(Event{Kind: ToolReturned, Turn: turn, Call: &call, Result: msg.Content, Err: err})
	}
	n := l.Concurrency
	if n <= 1 || len(calls) == 1 {
		for i := range calls {
			run(i)
		}
		return results
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run(i)
		}()
	}
	wg.Wait()
	return results
}

func (l *Loop) dispatch(ctx context.Context, call ToolCall) (out string, err error) {
	if l.Dispatch == nil {
		return "", fmt.Errorf("no tools are available")
	}
	defer func() {
		// A panicking tool fails its call, not the server.
		if p := recover(); p != nil {
			out, err = "", fmt.Errorf("tool %s failed: %v", call.Name, p)
		}
	}()
	return l.Dispatch(ctx, call)
}

// ClientEvent is what of e is safe to show the end user, as a server-sent
// event: its name and data. Tool arguments, results and errors stay on the
// server; ok is false for events with nothing to show.
//
//	text            {"text": "..."}          the answer as it is written
//	tool_called     {"name": "lookup_order"}
//	tool_returned   {"name": "lookup_order", "failed": false}
//	answer_rejected {}                       the model is asked again
func ClientEvent(e Event) (name string, data any, ok bool) {
	switch e.Kind {
	case TextDelta:
		return "text", map[string]string{"text": e.Text}, true
	case ToolCalled:
		if e.Call == nil {
			return "", nil, false
		}
		return "tool_called", map[string]string{"name": e.Call.Name}, true
	case ToolReturned:
		if e.Call == nil {
			return "", nil, false
		}
		return "tool_returned", map[string]any{"name": e.Call.Name, "failed": e.Err != nil}, true
	case AnswerRejected:
		return "answer_rejected", struct{}{}, true
	}
	return "", nil, false
}
