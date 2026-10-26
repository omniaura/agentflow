package afthread

import (
	"context"
	"encoding/json"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// Runs records agent runs and what happened in them, so a run's progress
// is in the database: for a client to follow, an operator to inspect, and
// to see where a crashed run stopped. A Store implements it optionally;
// Run records into it when it does.
type Runs interface {
	// StartRun records a run starting on the thread.
	StartRun(ctx context.Context, owner, thread string) (RunRecord, error)
	// AddEvent appends an event to the run, numbering it.
	AddEvent(ctx context.Context, owner, run string, e RunEvent) (RunEvent, error)
	// Events returns the run's events numbered after after, in order.
	Events(ctx context.Context, owner, run string, after int64) ([]RunEvent, error)
	// EndRun records how the run ended.
	EndRun(ctx context.Context, owner, run string, end RunEnd) error
	// GetRun returns the run.
	GetRun(ctx context.Context, owner, run string) (RunRecord, error)
}

// RunStatus is where a run is.
type RunStatus string

const (
	Running   RunStatus = "running"
	Succeeded RunStatus = "succeeded"
	RunFailed RunStatus = "failed"
)

// RunRecord is a run of a thread's agent.
type RunRecord struct {
	ID, Owner, Thread string
	Status            RunStatus
	Started           time.Time
	// The rest is set when the run ends.
	Ended time.Time
	RunEnd
}

// RunEnd is how a run ended.
type RunEnd struct {
	Turns, ToolCalls int
	Usage            afagent.Usage
	// Error is why it failed, or "".
	Error string
}

// RunEvent is one thing that happened in a run.
type RunEvent struct {
	// Seq numbers the run's events from 1; the store assigns it.
	Seq int64
	// Kind is an afagent.EventKind, or another name for events recorded
	// with Record, such as "compacted".
	Kind string
	Data json.RawMessage
	At   time.Time
}

type recorderKey struct{}

type recorder struct {
	runs       Runs
	owner, run string
}

// Record adds an event to the run that ctx belongs to, when Run started
// one; otherwise it does nothing. data is stored as JSON.
func Record(ctx context.Context, kind string, data any) error {
	r, ok := ctx.Value(recorderKey{}).(*recorder)
	if !ok {
		return nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = r.runs.AddEvent(ctx, r.owner, r.run, RunEvent{Kind: kind, Data: b})
	return err
}

// Recorder returns an afagent OnEvent func that records the run's model
// responses (with usage), tool calls, tool results and rejected answers.
// Streamed text is left out: the answer is saved to the transcript. Run
// sets it up itself, with afagent.WithEvents, so the agent's loop records
// without wiring; subagents' loops are runs of their own and aren't
// recorded.
func Recorder(ctx context.Context) func(afagent.Event) {
	return func(e afagent.Event) {
		var data any
		switch e.Kind {
		case afagent.ToolCalled:
			data = map[string]any{"turn": e.Turn, "call": e.Call}
		case afagent.ToolReturned:
			d := map[string]any{"turn": e.Turn, "call": e.Call, "result": e.Result}
			if e.Err != nil {
				d["error"] = e.Err.Error()
			}
			data = d
		case afagent.AnswerRejected:
			data = map[string]any{"turn": e.Turn, "error": errString(e.Err)}
		case afagent.ModelResponded:
			if e.Response == nil {
				return
			}
			data = map[string]any{"turn": e.Turn, "usage": e.Response.Usage}
		default:
			return
		}
		// Recording is best effort: a lost event must not fail the run.
		_ = Record(ctx, string(e.Kind), data)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
