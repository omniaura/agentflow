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

// PartKind is the kind of content in a multimodal Part.
type PartKind string

const (
	PartText  PartKind = "text"
	PartImage PartKind = "image"
	PartFile  PartKind = "file"
)

// Part is one piece of multimodal content in a Message: text, an image, or a
// file such as a PDF. Messages that carry only text leave Parts nil and use
// Content. EstimatedTokens, when positive, lets callers that know a file or
// image's model-input size (such as a parsed PDF page count) override default
// compaction heuristics.
type Part struct {
	Kind            PartKind `json:"kind"`
	Text            string   `json:"text,omitempty"`
	URL             string   `json:"url,omitempty"`
	Data            []byte   `json:"data,omitempty"`
	MIMEType        string   `json:"mime_type,omitempty"`
	Filename        string   `json:"filename,omitempty"`
	EstimatedTokens int      `json:"estimated_tokens,omitempty"`
}

// Message is one message of a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`
	// Parts carries multimodal content (text, images, files) alongside or in
	// place of Content. Providers that support multimodal messages send Parts;
	// text-only messages leave Parts nil.
	Parts []Part `json:"parts,omitempty"`
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
	// Source says who wrote a user message the user didn't, such as
	// SourceLoop, so a transcript shown to the user can leave it out. The
	// model sees it as any user message.
	Source string `json:"source,omitempty"`
}

// SourceLoop is the Source of the loop's own messages, such as asking the
// model again after Check rejected its answer.
const SourceLoop = "loop"

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
	// NoToolCalls means the model must answer this turn without calling a
	// tool. Adapters still send Tools, with tool choice none: providers
	// reject a conversation holding tool calls when no tools are defined.
	NoToolCalls bool
	// PromptCacheKey, when set, names a stable prompt-cache bucket for
	// providers that route cache affinity by key. Providers that do not use
	// cache keys ignore it.
	PromptCacheKey string
	// OnText, when set, receives the answer's text as the model writes
	// it. Adapters that stream call it; others may never call it. Call it
	// from one goroutine at a time, in the order of the text, and only
	// before Complete returns: text after that is dropped.
	OnText func(text string)
	// OnReasoning, when set, receives the model's reasoning, or a summary
	// of it, as the model writes it, for adapters that stream it. It is
	// not the answer. The same rules as OnText apply.
	OnReasoning func(text string)
}

// OutputSpec describes a structured final answer.
type OutputSpec struct {
	Name        string
	Description string
	// Schema is a JSON Schema for the final answer. By default adapters request
	// strict adherence (every property required, additionalProperties false);
	// set NonStrict for open schemas or schemas with optional fields.
	Schema    json.RawMessage
	NonStrict bool
}

// Usage counts the tokens of model calls.
type Usage struct {
	// InputTokens counts all input tokens, CachedInputTokens the ones
	// read from the provider's prompt cache, and CacheCreationInputTokens the
	// ones written to the prompt cache where reported by the provider.
	InputTokens              int `json:"input_tokens"`
	CachedInputTokens        int `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens"`
}

func (u *Usage) add(o Usage) {
	u.InputTokens += o.InputTokens
	u.CachedInputTokens += o.CachedInputTokens
	u.CacheCreationInputTokens += o.CacheCreationInputTokens
	u.OutputTokens += o.OutputTokens
}

// Response is a model's answer: an assistant message.
type Response struct {
	Message Message
	Usage   Usage
	// FinishReason is the provider's stop/finish reason (such as "stop",
	// "tool_calls", or "end_turn") when reported.
	FinishReason string
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
	// ReasoningDelta carries the model's reasoning, or a summary of it, in
	// Text, as the model writes it, when the adapter streams it. It is not
	// part of the answer.
	ReasoningDelta EventKind = "reasoning_delta"
)

// Event reports progress, for logs, metrics and streaming to a client.
type Event struct {
	Kind EventKind
	// Turn counts model calls from 1.
	Turn     int
	Response *Response
	Call     *ToolCall
	// Result, Deferred, and Err are a tool's outcome. Deferred is true when
	// DeferTool matched the call's error so the host can suspend and resume
	// without presenting the call as failed.
	Result   string
	Deferred bool
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
	// OutOfTurns means the answer came on the last turn, with tools
	// withheld by AnswerOnLastTurn: the model may have stopped short of
	// what it meant to do.
	OutOfTurns bool
	// ToolCalls counts the tool calls run, and OutOfToolCalls means the
	// answer came with tools withheld because MaxToolCalls were run.
	ToolCalls      int
	OutOfToolCalls bool
	// Deferred holds tool calls left unanswered because DeferTool matched
	// their dispatch error. Result.Messages retains the assistant's call batch
	// and all completed results; the host must supply tool messages for every
	// deferred call before resuming the conversation.
	Deferred []ToolCall
}

// ErrDeferred means one or more tool calls in the batch were deferred by
// DeferTool. Result.Deferred lists the unanswered calls and Result.Messages
// holds the conversation to resume once the host resolves them.
var ErrDeferred = errors.New("afagent: one or more tool calls were deferred")

// AnswerFeedback is an error Check may return to send a custom follow-up
// message to the model instead of the default rejection wrapper. When
// NoToolCalls is true, the retry turn withholds tools; when ExtraTurn is true
// and the run is on its last turn, one extra turn is granted for that retry
// (at most once per run).
type AnswerFeedback struct {
	Message     string
	NoToolCalls bool
	ExtraTurn   bool
}

func (f *AnswerFeedback) Error() string { return f.Message }

// Model adapters wrap these errors for answers the loop can't use.
var (
	// ErrTruncated means the answer hit the output token limit or the
	// context window, so its text or tool calls may be cut off.
	ErrTruncated = errors.New("afagent: the model's answer was cut off")
	// ErrRefused means the model declined to answer.
	ErrRefused = errors.New("afagent: the model refused")
)

// Messenger carries messages between agents: what the built-in
// send_message and list_agents tools call. afthread.Messenger is one.
type Messenger interface {
	// Send delivers text to the agent named to and returns what the
	// sender is told.
	Send(ctx context.Context, to, text string) (string, error)
	// List returns the agents the sender may message.
	List(ctx context.Context) (string, error)
}

// ErrMaxTurns means the model kept calling tools, or giving answers Check
// rejected, for MaxTurns model calls. After a rejected last answer the
// error also wraps Check's. With AnswerOnLastTurn, only a rejected last
// answer, or tool calls the model made anyway, end the loop with it.
var ErrMaxTurns = errors.New("afagent: the agent did not finish within its turn limit")

// MaxDepth is how deep subagents may nest: an agent calling an agent
// calling an agent, and so on.
const MaxDepth = 8

// ErrMaxDepth means subagents nested deeper than MaxDepth, as agents that
// delegate to each other would.
var ErrMaxDepth = errors.New("afagent: subagents nest too deep")

type depthKey struct{}

// Nest returns ctx one subagent deeper, or ErrMaxDepth past MaxDepth. An
// agent run as another agent's tool calls it first.
func Nest(ctx context.Context) (context.Context, error) {
	d, _ := ctx.Value(depthKey{}).(int)
	if d >= MaxDepth {
		return ctx, fmt.Errorf("%w: %d levels", ErrMaxDepth, d)
	}
	return context.WithValue(ctx, depthKey{}, d+1), nil
}

type eventsKey struct{}

// observer is a WithEvents or WithStream func, for the loops at its
// depth.
type observer struct {
	depth int
	fn    func(Event)
	// text is whether fn wants streamed text and reasoning.
	text bool
}

// WithEvents returns ctx with fn also seeing the events of the loops run
// with it: model responses, tool calls and results, and rejected answers.
// Streamed text and reasoning are left out, so adapters needn't stream for
// it. Subagents nested in those loops (see Nest) are runs of their own,
// which fn doesn't see. A store recording runs uses it, so the agents it
// runs need no wiring. Like OnEvent, fn must be safe for concurrent use.
func WithEvents(ctx context.Context, fn func(Event)) context.Context {
	return withObserver(ctx, fn, false)
}

// WithStream is WithEvents with streamed text and reasoning too, for
// streaming a run to a client: adapters stream for it.
func WithStream(ctx context.Context, fn func(Event)) context.Context {
	return withObserver(ctx, fn, true)
}

func withObserver(ctx context.Context, fn func(Event), text bool) context.Context {
	d, _ := ctx.Value(depthKey{}).(int)
	obs, _ := ctx.Value(eventsKey{}).([]observer)
	return context.WithValue(ctx, eventsKey{}, append(obs[:len(obs):len(obs)], observer{depth: d, fn: fn, text: text}))
}

// observers returns the WithEvents and WithStream funcs for a loop run
// with ctx.
func observers(ctx context.Context) []observer {
	d, _ := ctx.Value(depthKey{}).(int)
	obs, _ := ctx.Value(eventsKey{}).([]observer)
	var out []observer
	for _, o := range obs {
		if o.depth == d {
			out = append(out, o)
		}
	}
	return out
}

// ErrMaxToolCalls means the model kept calling tools after MaxToolCalls
// were run, when the tools were withheld.
var ErrMaxToolCalls = errors.New("afagent: the agent called tools beyond its tool-call limit")

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
	// InterruptTool, when set, classifies a dispatch error that must end the
	// run immediately instead of becoming a tool result. Calls run sequentially
	// in this mode, even when Concurrency is higher: no later call may start
	// after an interruption. The returned conversation retains the assistant's
	// full call batch and completed results, but has no result for the
	// interrupted or unstarted calls. The host must resolve all outstanding
	// calls before passing that conversation to a provider again.
	InterruptTool func(error) bool
	// DeferTool, when set, classifies a dispatch error that leaves the call
	// unanswered for later resolution (such as human approval) while still
	// processing the rest of the turn's batch so sibling calls can run or
	// defer together. Calls run sequentially in this mode. After the batch
	// finishes, if any call was deferred, Run stops without calling the model
	// again and returns Result (with Deferred populated) and ErrDeferred.
	DeferTool func(error) bool
	// CountToolCall, when set, reports whether a call counts against
	// MaxToolCalls. Calls returning false (such as internal workspace readers)
	// are not refused when MaxToolCalls is reached and do not increment
	// Result.ToolCalls. Nil counts every call.
	CountToolCall func(ToolCall) bool
	// FormatToolError, when set, formats recoverable tool dispatch errors into
	// the Tool message content sent back to the model (default err.Error()).
	// ToolReturned.Err and Message.IsError remain set.
	FormatToolError func(error) string
	// Output, when set, asks the model for a structured final answer.
	Output *OutputSpec
	// Check, when set, vets a final answer. If it fails, the error goes
	// back to the model as a user message and the loop goes on, within
	// MaxTurns. Returning *AnswerFeedback sends its message verbatim and can
	// optionally withhold tools or grant one extra turn for the retry.
	Check func(answer string) error
	// MaxTurns bounds the model calls; 0 means DefaultMaxTurns.
	MaxTurns int
	// BeforeTurn, when set, is called before each model call with the
	// turn about to start (1 for the first) and the conversation so far.
	// The messages it returns are the conversation from then on, so it
	// can compact a long one, or add messages that arrived during the run.
	// Its error ends the run.
	BeforeTurn func(ctx context.Context, turn int, messages []Message) ([]Message, error)
	// BeforeTools can stop a tool batch after the model's response and usage
	// have been recorded, before any call starts. It is not called for a final
	// answer or for calls the loop has already refused. On error, Result keeps
	// the response and usage; the host must resolve its unanswered calls before
	// resuming the conversation.
	BeforeTools func(ctx context.Context, turn int, response Response, usage Usage) error
	// AfterTools, when set, runs after a tool batch completes without
	// interruption or deferral, before the next turn. It may append follow-up
	// messages (such as attached files or screenshots produced by tools) or
	// adjust the conversation. Its error ends the run.
	AfterTools func(ctx context.Context, turn int, messages []Message) ([]Message, error)
	// MaxToolCalls, when above 0, bounds the tool calls of a run. Calls a
	// turn makes beyond it are refused, and later turns withhold the
	// tools, so the model answers with what it has; Result.OutOfToolCalls
	// reports such an answer.
	MaxToolCalls int
	// AnswerOnLastTurn withholds the tools on the last turn, so the model
	// answers with what it has instead of the loop ending in ErrMaxTurns.
	// Result.OutOfTurns reports such an answer.
	AnswerOnLastTurn bool
	// LastTurnMessage, ToolLimitMessage, and ToolLimitResult optionally
	// customize the SourceLoop messages appended when AnswerOnLastTurn or
	// MaxToolCalls withholds tools and the error text for over-limit calls.
	LastTurnMessage  string
	ToolLimitMessage string
	ToolLimitResult  string
	// PromptCacheKey, when set, is passed on every Request.PromptCacheKey.
	PromptCacheKey string
	// Concurrency is how many tool calls of one turn run at once; 0 or 1
	// runs them in order.
	Concurrency int
	// OnEvent, if set, is called from the tools' goroutines when
	// Concurrency is above 1, so it must be safe for concurrent use.
	OnEvent func(Event)
}

// Run continues the conversation in messages until the model answers
// without calling a tool. On ErrMaxTurns, ErrDeferred, a model error or a
// canceled context, the Result holds the conversation so far.
func (l *Loop) Run(ctx context.Context, messages []Message) (Result, error) {
	res := Result{Messages: append([]Message(nil), messages...)}
	// Streamed text goes to OnEvent and WithStream funcs, not WithEvents
	// funcs: the model streams only when someone wants it.
	streaming := l.OnEvent != nil
	if obs := observers(ctx); len(obs) > 0 {
		copied, own := *l, l.OnEvent
		copied.OnEvent = func(e Event) {
			if own != nil {
				own(e)
			}
			text := e.Kind == TextDelta || e.Kind == ReasoningDelta
			for _, o := range obs {
				if o.text || !text {
					o.fn(e)
				}
			}
		}
		l = &copied
		for _, o := range obs {
			streaming = streaming || o.text
		}
	}
	maxTurns := l.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	var (
		forceNoTools    bool
		extraTurnUsed   bool
		lastTurnNudged  bool
		toolLimitNudged bool
	)
	for res.Turns < maxTurns {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Turns++
		spent := l.MaxToolCalls > 0 && res.ToolCalls >= l.MaxToolCalls
		last := l.AnswerOnLastTurn && res.Turns == maxTurns
		if spent && !toolLimitNudged && l.ToolLimitMessage != "" && len(l.Tools) > 0 {
			toolLimitNudged = true
			res.Messages = append(res.Messages, Message{Role: User, Content: l.ToolLimitMessage, Source: SourceLoop})
		} else if last && !spent && !forceNoTools && !lastTurnNudged && l.LastTurnMessage != "" && len(l.Tools) > 0 && res.Turns > 1 && res.ToolCalls > 0 {
			lastTurnNudged = true
			res.Messages = append(res.Messages, Message{Role: User, Content: l.LastTurnMessage, Source: SourceLoop})
		}
		if l.BeforeTurn != nil {
			msgs, err := l.BeforeTurn(ctx, res.Turns, res.Messages)
			if err != nil {
				return res, err
			}
			res.Messages = msgs
		}
		req := Request{
			System:         l.System,
			Messages:       res.Messages,
			Tools:          l.Tools,
			Output:         l.Output,
			PromptCacheKey: l.PromptCacheKey,
		}
		turnForcedNoTools := forceNoTools
		forceNoTools = false
		req.NoToolCalls = len(l.Tools) > 0 && (spent || last || turnForcedNoTools)
		// held is the turn's text while Check may still reject it. The
		// mutex keeps a Model that breaks OnText's contract from racing.
		var (
			mu     sync.Mutex
			held   []Event
			closed bool
		)
		if streaming {
			turn := res.Turns
			// Emitting under mu keeps an event from reaching OnEvent after
			// the turn closed, and so after Run returned.
			req.OnText = func(text string) {
				e := Event{Kind: TextDelta, Turn: turn, Text: text}
				mu.Lock()
				defer mu.Unlock()
				switch {
				case closed:
				case l.Check != nil:
					held = append(held, e)
				default:
					l.emit(e)
				}
			}
			// Reasoning isn't the answer: Check doesn't hold it back.
			req.OnReasoning = func(text string) {
				mu.Lock()
				defer mu.Unlock()
				if !closed {
					l.emit(Event{Kind: ReasoningDelta, Turn: turn, Text: text})
				}
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
		var rejected error
		if final && l.Check != nil {
			rejected = l.Check(resp.Message.Content)
			var nilFB *AnswerFeedback
			if errors.As(rejected, &nilFB) && nilFB == nil {
				rejected = errors.New("afagent: answer rejected")
			}
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
				var fb *AnswerFeedback
				isFB := errors.As(rejected, &fb)
				if isFB && fb.ExtraTurn && res.Turns == maxTurns && !extraTurnUsed {
					maxTurns++
					extraTurnUsed = true
				}
				if res.Turns == maxTurns {
					// No turn is left to ask again: end on the rejected answer.
					return res, fmt.Errorf("%w: the last answer was rejected: %w", ErrMaxTurns, rejected)
				}
				if isFB {
					message := fb.Message
					if message == "" {
						message = fmt.Sprintf("Your answer was rejected: %v. Answer again.", rejected)
					}
					res.Messages = append(res.Messages, Message{Role: User, Content: message, Source: SourceLoop})
					if fb.NoToolCalls {
						forceNoTools = true
					}
				} else {
					res.Messages = append(res.Messages, Message{Role: User, Content: fmt.Sprintf("Your answer was rejected: %v. Answer again.", rejected), Source: SourceLoop})
				}
				continue
			}
			res.Final = resp.Message.Content
			// Both limits may hold on the last turn; each reports itself.
			res.OutOfToolCalls = req.NoToolCalls && spent
			res.OutOfTurns = req.NoToolCalls && last
			return res, nil
		}
		if req.NoToolCalls && spent {
			return res, fmt.Errorf("%w: the model called tools after running %d", ErrMaxToolCalls, res.ToolCalls)
		}
		if req.NoToolCalls && last {
			// The model called tools it was told it couldn't; running them
			// would leave their results unanswered.
			return res, fmt.Errorf("%w: the model called tools on its last turn, when it was to answer", ErrMaxTurns)
		}
		if req.NoToolCalls {
			return res, errors.New("afagent: the model called tools when it was told not to")
		}
		if l.BeforeTools != nil {
			if err := l.BeforeTools(ctx, res.Turns, resp, res.Usage); err != nil {
				return res, err
			}
		}
		calls := resp.Message.ToolCalls
		limitRefused := make([]bool, len(calls))
		limitHit := false
		if l.MaxToolCalls > 0 {
			remaining := max(0, l.MaxToolCalls-res.ToolCalls)
			for i, call := range calls {
				if l.CountToolCall != nil && !l.CountToolCall(call) {
					continue
				}
				if remaining > 0 {
					remaining--
				} else {
					limitRefused[i] = true
					limitHit = true
				}
			}
		}
		results, attempted, deferred, interrupt := l.runTools(ctx, res.Turns, calls, limitRefused)
		res.Messages = append(res.Messages, results...)
		res.ToolCalls += attempted
		if len(deferred) > 0 {
			res.Deferred = deferred
		}
		if interrupt != nil {
			return res, interrupt
		}
		if len(deferred) > 0 {
			return res, ErrDeferred
		}
		if l.AfterTools != nil {
			msgs, err := l.AfterTools(ctx, res.Turns, res.Messages)
			if err != nil {
				return res, err
			}
			res.Messages = msgs
		}
		if limitHit && res.Turns < maxTurns && len(l.Tools) > 0 && l.ToolLimitMessage != "" && !toolLimitNudged {
			toolLimitNudged = true
			res.Messages = append(res.Messages, Message{Role: User, Content: l.ToolLimitMessage, Source: SourceLoop})
		}
	}
	return res, ErrMaxTurns
}

func (l *Loop) emit(e Event) {
	if l.OnEvent != nil {
		l.OnEvent(e)
	}
}

func (l *Loop) limitResultText() string {
	if l.ToolLimitResult != "" {
		return l.ToolLimitResult
	}
	return fmt.Sprintf("not run: the limit of %d tool calls is reached; answer with what you have", l.MaxToolCalls)
}

func (l *Loop) formatToolErr(err error) string {
	if l.FormatToolError != nil {
		return l.FormatToolError(err)
	}
	return err.Error()
}

// runTools runs the calls of one turn and returns their results in call
// order, since providers expect them so.
func (l *Loop) runTools(ctx context.Context, turn int, calls []ToolCall, limitRefused []bool) ([]Message, int, []ToolCall, error) {
	sequential := l.InterruptTool != nil || l.DeferTool != nil || l.Concurrency <= 1 || len(calls) == 1
	if sequential {
		results := make([]Message, 0, len(calls))
		var deferred []ToolCall
		counted := 0
		for i, call := range calls {
			if limitRefused[i] {
				results = append(results, Message{Role: Tool, ToolCallID: call.ID, Content: l.limitResultText(), IsError: true})
				continue
			}
			if (l.InterruptTool != nil || l.DeferTool != nil) && ctx.Err() != nil {
				return results, counted, deferred, ctx.Err()
			}
			l.emit(Event{Kind: ToolCalled, Turn: turn, Call: &call})
			out, err := l.dispatch(ctx, call)
			interrupted := err != nil && l.InterruptTool != nil && l.InterruptTool(err)
			isDeferred := !interrupted && err != nil && l.DeferTool != nil && l.DeferTool(err)
			msg := Message{Role: Tool, ToolCallID: call.ID, Content: out}
			if err != nil && !isDeferred {
				msg.Content, msg.IsError = l.formatToolErr(err), true
			}
			if isDeferred {
				l.emit(Event{Kind: ToolReturned, Turn: turn, Call: &call, Deferred: true, Err: err})
			} else {
				l.emit(Event{Kind: ToolReturned, Turn: turn, Call: &call, Result: msg.Content, Err: err})
			}
			if l.CountToolCall == nil || l.CountToolCall(call) {
				counted++
			}
			if interrupted {
				return results, counted, deferred, err
			}
			if isDeferred {
				deferred = append(deferred, call)
				continue
			}
			results = append(results, msg)
		}
		return results, counted, deferred, nil
	}

	results := make([]Message, len(calls))
	counted := 0
	sem := make(chan struct{}, l.Concurrency)
	var wg sync.WaitGroup
	for i, call := range calls {
		if limitRefused[i] {
			results[i] = Message{Role: Tool, ToolCallID: call.ID, Content: l.limitResultText(), IsError: true}
			continue
		}
		if l.CountToolCall == nil || l.CountToolCall(call) {
			counted++
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, call ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			l.emit(Event{Kind: ToolCalled, Turn: turn, Call: &call})
			out, err := l.dispatch(ctx, call)
			msg := Message{Role: Tool, ToolCallID: call.ID, Content: out}
			if err != nil {
				msg.Content, msg.IsError = l.formatToolErr(err), true
			}
			l.emit(Event{Kind: ToolReturned, Turn: turn, Call: &call, Result: msg.Content, Err: err})
			results[i] = msg
		}(i, call)
	}
	wg.Wait()
	return results, counted, nil, nil
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
//	reasoning       {"text": "..."}          the model's reasoning summary
//	tool_called     {"name": "lookup_order"}
//	tool_returned   {"name": "lookup_order", "failed": false}
//	answer_rejected {}                       the model is asked again
func ClientEvent(e Event) (name string, data any, ok bool) {
	switch e.Kind {
	case TextDelta:
		return "text", map[string]string{"text": e.Text}, true
	case ReasoningDelta:
		return "reasoning", map[string]string{"text": e.Text}, true
	case ToolCalled:
		if e.Call == nil {
			return "", nil, false
		}
		return "tool_called", map[string]string{"name": e.Call.Name}, true
	case ToolReturned:
		if e.Call == nil {
			return "", nil, false
		}
		if e.Deferred {
			return "tool_returned", map[string]any{"name": e.Call.Name, "failed": false, "deferred": true}, true
		}
		return "tool_returned", map[string]any{"name": e.Call.Name, "failed": e.Err != nil}, true
	case AnswerRejected:
		return "answer_rejected", struct{}{}, true
	}
	return "", nil, false
}
