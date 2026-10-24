// Package afthread keeps agents' conversations in a database: threads, their
// transcripts, and an inbox of messages waiting for a thread's agent, sent by
// users or by other agents. Everything an agent run needs survives a restart,
// so any server can pick a thread up.
//
// Store is the interface to the database. Bring your own: an adapter maps it
// onto an existing schema (a threads table holding a transcript, a queue of
// incoming messages), so agentflow assumes no data model of its own. Memory
// is an in-memory Store for tests, and afthreadtest checks that an adapter
// behaves like it.
//
// Run is one step of a worker: it claims a thread's queued messages, runs the
// agent on the transcript, hands it messages that arrive while it runs,
// saves the conversation before every turn and marks the messages done.
package afthread

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// ErrNotFound means there is no thread with the ID, for the owner.
var ErrNotFound = errors.New("afthread: no such thread")

// Thread is a conversation an agent continues.
type Thread struct {
	ID string
	// Owner is whose thread it is, such as a user ID: stores scope every
	// read by it.
	Owner string
	// Agent names the agent that answers on the thread.
	Agent    string
	Title    string
	Metadata map[string]string
	Created  time.Time
	Updated  time.Time
}

// Status is where an incoming message is in its life.
type Status string

const (
	Queued  Status = "queued"
	Started Status = "started"
	Done    Status = "done"
	Failed  Status = "failed"
)

// Incoming is a message waiting for a thread's agent.
type Incoming struct {
	// ID orders the thread's messages; the store assigns it.
	ID int64
	// Key makes sending idempotent: sending a key the thread has seen
	// returns the first message instead of queueing another, as a client
	// retrying a request would.
	Key string
	// From is "" for the thread's user, or the name of the agent that sent
	// the message.
	From    string
	Content string
	Status  Status
	// Error is why a Failed message failed.
	Error string
	At    time.Time
	// Claimed is when Claim last started it.
	Claimed time.Time
}

// Store keeps threads in a database. Methods take the owner, and must not
// reveal or change another owner's thread: they return ErrNotFound for it.
type Store interface {
	// Create stores a new thread and returns it, with an ID when it had
	// none.
	Create(ctx context.Context, t Thread) (Thread, error)
	Get(ctx context.Context, owner, id string) (Thread, error)
	// Transcript returns the thread's conversation, oldest first.
	Transcript(ctx context.Context, owner, id string) ([]afagent.Message, error)
	// Append adds messages to the end of the transcript.
	Append(ctx context.Context, owner, id string, msgs ...afagent.Message) error
	// Replace sets the whole transcript, as compaction does when it folds
	// old messages into a summary.
	Replace(ctx context.Context, owner, id string, msgs []afagent.Message) error
	// Send queues a message for the thread's agent. A key already sent
	// returns the message it sent, unchanged.
	Send(ctx context.Context, owner, id string, in Incoming) (Incoming, error)
	// Claim marks the thread's queued messages started and returns them,
	// oldest first. Concurrent claims never return the same message twice.
	Claim(ctx context.Context, owner, id string) ([]Incoming, error)
	// Finish marks started messages done, or failed with err.
	Finish(ctx context.Context, owner, id string, ids []int64, err error) error
	// Reclaim puts back in the queue the thread's messages claimed before
	// before and never finished, as a worker that crashed mid-run leaves
	// them, and returns how many. Call it with a lease longer than any run,
	// such as when a worker starts.
	Reclaim(ctx context.Context, owner, id string, before time.Time) (int, error)
}

// Message is what the agent sees of an incoming message: its text, marked
// with the sender when another agent sent it.
func (in Incoming) Message() afagent.Message {
	if in.From == "" {
		return afagent.Message{Role: afagent.User, Content: in.Content}
	}
	return afagent.Message{Role: afagent.User, Content: fmt.Sprintf("[message from the %s agent]\n%s", in.From, in.Content)}
}

// Agent runs an agent on a conversation with before as its BeforeTurn.
// A generated agent fits in a closure; to compact as well, compact first
// and call before last, so what is saved is what the model sees:
//
//	func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
//		agent := *support // a copy, so runs don't share BeforeTurn
//		agent.BeforeTurn = func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
//			msgs, err := compact(ctx, msgs)
//			if err != nil {
//				return nil, err
//			}
//			return before(ctx, turn, msgs)
//		}
//		return agent.Run(ctx, system, msgs...)
//	}
type Agent func(ctx context.Context, msgs []afagent.Message, before BeforeTurn) (afagent.Result, error)

// BeforeTurn is afagent.Loop's BeforeTurn.
type BeforeTurn = func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error)

// Run answers a thread's queued messages. It claims them and runs agent on
// the transcript with them. Before each turn it adds the messages queued
// since (from the second turn on) and saves the conversation so far:
// appending what is new, or replacing the transcript when compaction
// rewrote it. A run that stops halfway leaves the thread as of its last
// turn. Last, it saves the answer and marks every claimed message done,
// or failed with the run's error. It returns the run, or a zero Result and
// nil when nothing was queued.
//
// When the Store implements Runs, Run records the run and how it ended,
// and Recorder and Record add events to it from the ctx agent gets.
//
// Run one worker per thread at a time: claims are safe to race, but the
// transcript is written by whoever runs.
func Run(ctx context.Context, s Store, owner, id string, agent Agent) (afagent.Result, error) {
	claimed, err := s.Claim(ctx, owner, id)
	if err != nil || len(claimed) == 0 {
		return afagent.Result{}, err
	}
	// With a Store that records runs, the run and its events go to the
	// database too.
	var run *recorder
	if runs, ok := s.(Runs); ok {
		rec, err := runs.StartRun(ctx, owner, id)
		if err != nil {
			return afagent.Result{}, errors.Join(err, finish(ctx, s, owner, id, claimed, err))
		}
		run = &recorder{runs: runs, owner: owner, run: rec.ID}
		ctx = context.WithValue(ctx, recorderKey{}, run)
	}
	saved, err := s.Transcript(ctx, owner, id)
	if err != nil {
		return afagent.Result{}, errors.Join(err, finish(ctx, s, owner, id, claimed, err))
	}
	// save writes msgs, the conversation the model sees, to the thread.
	save := func(ctx context.Context, msgs []afagent.Message) error {
		var err error
		switch {
		case len(msgs) >= len(saved) && sameStart(msgs, saved):
			if added := msgs[len(saved):]; len(added) > 0 {
				err = s.Append(ctx, owner, id, added...)
			}
		default:
			err = s.Replace(ctx, owner, id, msgs)
		}
		if err == nil {
			saved = append([]afagent.Message(nil), msgs...)
		}
		return err
	}
	msgs := append([]afagent.Message(nil), saved...)
	for _, in := range claimed {
		msgs = append(msgs, in.Message())
	}
	before := func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
		if turn > 1 {
			in, err := s.Claim(ctx, owner, id)
			if err != nil {
				return nil, err
			}
			claimed = append(claimed, in...)
			for _, m := range in {
				msgs = append(msgs, m.Message())
			}
		}
		return msgs, save(ctx, msgs)
	}
	res, runErr := agent(ctx, msgs, before)
	var saveErr error
	if len(res.Messages) > 0 {
		saveErr = save(context.WithoutCancel(ctx), res.Messages)
	}
	failure := errors.Join(runErr, saveErr)
	var endErr error
	if run != nil {
		end := RunEnd{Turns: res.Turns, ToolCalls: res.ToolCalls, Usage: res.Usage, Error: errString(failure)}
		endErr = run.runs.EndRun(context.WithoutCancel(ctx), owner, run.run, end)
	}
	return res, errors.Join(failure, finish(ctx, s, owner, id, claimed, failure), endErr)
}

func finish(ctx context.Context, s Store, owner, id string, claimed []Incoming, failure error) error {
	ids := make([]int64, len(claimed))
	for i, in := range claimed {
		ids[i] = in.ID
	}
	// Record the outcome even if the run's context was canceled.
	return s.Finish(context.WithoutCancel(ctx), owner, id, ids, failure)
}

// sameStart reports whether msgs begins with prefix, provider-native
// payloads included.
func sameStart(msgs, prefix []afagent.Message) bool {
	for i := range prefix {
		if !reflect.DeepEqual(msgs[i], prefix[i]) {
			return false
		}
	}
	return true
}
