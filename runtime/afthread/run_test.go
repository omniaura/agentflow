package afthread_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// scripted is a model answering each call with the next message.
func scripted(t *testing.T, turns ...afagent.Message) (afagent.Model, *[]afagent.Request) {
	var seen []afagent.Request
	return afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		seen = append(seen, req)
		if len(seen) > len(turns) {
			t.Fatalf("unexpected model call %d", len(seen))
		}
		m := turns[len(seen)-1]
		if m.Content == "FAIL" {
			return afagent.Response{}, errors.New("model down")
		}
		return afagent.Response{Message: m}, nil
	}), &seen
}

func loopAgent(model afagent.Model, dispatch func(context.Context, afagent.ToolCall) (string, error)) afthread.Agent {
	return func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, Dispatch: dispatch, BeforeTurn: before}
		return l.Run(ctx, msgs)
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	s.Append(ctx, "ann", th.ID, afagent.Message{Role: afagent.User, Content: "earlier"}, afagent.Message{Role: afagent.Assistant, Content: "earlier answer"})

	// Nothing queued: nothing runs.
	if res, err := afthread.Run(ctx, s, "ann", th.ID, nil); err != nil || res.Turns != 0 {
		t.Fatalf("empty inbox: %+v, %v", res, err)
	}

	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "where is A-1?"})
	model, seen := scripted(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: "{}"}}},
		afagent.Message{Content: "It arrives Tuesday."},
	)
	dispatch := func(ctx context.Context, call afagent.ToolCall) (string, error) {
		// While the tool runs, the tracker agent sends the thread a note,
		// and the transcript already holds the question: it was saved before
		// turn 1.
		s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m2", From: "tracker", Content: "carrier says Tuesday"})
		if got, _ := s.Transcript(ctx, "ann", th.ID); len(got) != 3 || got[2].Content != "where is A-1?" {
			t.Errorf("transcript mid-run = %+v", got)
		}
		return "in transit", nil
	}
	res, err := afthread.Run(ctx, s, "ann", th.ID, loopAgent(model, dispatch))
	if err != nil || res.Final != "It arrives Tuesday." {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	// The model saw the history and the question, then the note on turn 2.
	first, second := (*seen)[0].Messages, (*seen)[1].Messages
	if len(first) != 3 || first[0].Content != "earlier" || first[2].Content != "where is A-1?" {
		t.Errorf("turn 1 = %+v", first)
	}
	if last := second[len(second)-1]; !strings.Contains(last.Content, "[message from the tracker agent]\ncarrier says Tuesday") {
		t.Errorf("turn 2 ends with %+v", last)
	}
	// The thread holds the whole conversation, and both messages are done.
	got, _ := s.Transcript(ctx, "ann", th.ID)
	if len(got) != 7 || got[3].ToolCalls[0].Name != "look" || got[4].Content != "in transit" || got[6].Content != "It arrives Tuesday." {
		t.Errorf("transcript = %+v", got)
	}
	for _, in := range s.Inbox("ann", th.ID) {
		if in.Status != afthread.Done {
			t.Errorf("%s is %s", in.Key, in.Status)
		}
	}
	// A client retrying m1 doesn't make it run again.
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "where is A-1?"})
	if res, err := afthread.Run(ctx, s, "ann", th.ID, nil); err != nil || res.Turns != 0 {
		t.Fatalf("retried key ran: %+v, %v", res, err)
	}
}

func TestRunFailure(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "hi"})
	model, _ := scripted(t, afagent.Message{Content: "FAIL"})
	_, err := afthread.Run(ctx, s, "ann", th.ID, loopAgent(model, nil))
	if err == nil || !strings.Contains(err.Error(), "model down") {
		t.Fatalf("got %v", err)
	}
	// The question was saved before the model failed; the message failed.
	if got, _ := s.Transcript(ctx, "ann", th.ID); len(got) != 1 || got[0].Content != "hi" {
		t.Errorf("transcript = %+v", got)
	}
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Failed || !strings.Contains(in[0].Error, "model down") {
		t.Errorf("inbox = %+v", in)
	}
}

// Compaction rewrites the conversation: the thread is replaced, not
// appended to.
func TestRunCompaction(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	for range 3 {
		s.Append(ctx, "ann", th.ID, afagent.Message{Role: afagent.User, Content: "old"}, afagent.Message{Role: afagent.Assistant, Content: "old answer"})
	}
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "new question"})
	model, _ := scripted(t, afagent.Message{Content: "answer"})
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		l := afagent.Loop{Model: model, BeforeTurn: func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
			// Compact first, then let afthread save what the model sees.
			compacted := []afagent.Message{{Role: afagent.User, Content: "summary of 6 messages"}, msgs[len(msgs)-1]}
			return before(ctx, turn, compacted)
		}}
		return l.Run(ctx, msgs)
	}
	if _, err := afthread.Run(ctx, s, "ann", th.ID, agent); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Transcript(ctx, "ann", th.ID)
	if len(got) != 3 || got[0].Content != "summary of 6 messages" || got[1].Content != "new question" || got[2].Content != "answer" {
		t.Errorf("transcript = %+v", got)
	}
}

// A compactor that rewrites a message's provider payload replaces the
// transcript rather than appending to stale data.
func TestRunNativeChange(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	s.Append(ctx, "ann", th.ID, afagent.Message{Role: afagent.Assistant, Content: "a", Native: &afagent.Native{Provider: "anthropic", Data: []byte(`"old blocks"`)}})
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "q"})
	model, _ := scripted(t, afagent.Message{Content: "answer"})
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		l := afagent.Loop{Model: model, BeforeTurn: func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
			msgs = append([]afagent.Message(nil), msgs...)
			msgs[0].Native = &afagent.Native{Provider: "anthropic", Data: []byte(`"new blocks"`)}
			return before(ctx, turn, msgs)
		}}
		return l.Run(ctx, msgs)
	}
	if _, err := afthread.Run(ctx, s, "ann", th.ID, agent); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Transcript(ctx, "ann", th.ID); len(got) != 3 || string(got[0].Native.Data) != `"new blocks"` {
		t.Fatalf("transcript = %+v", got)
	}
}

// With a store that records runs, Run records the run, its events and how
// it ended.
func TestRunRecordsRun(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "look it up"})
	model := afagent.ModelFunc(func() func(context.Context, afagent.Request) (afagent.Response, error) {
		n := 0
		return func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
			n++
			if n == 1 {
				return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: `{"id":7}`}}}, Usage: afagent.Usage{InputTokens: 50, OutputTokens: 5}}, nil
			}
			return afagent.Response{Message: afagent.Message{Content: "found"}, Usage: afagent.Usage{InputTokens: 60, OutputTokens: 3}}, nil
		}
	}())
	var runID string
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		// Other code records with the run's ctx too, as a compactor would.
		afthread.Record(ctx, "compacted", map[string]int{"before": 900, "after": 400})
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, BeforeTurn: before, OnEvent: afthread.Recorder(ctx),
			Dispatch: func(context.Context, afagent.ToolCall) (string, error) { return "row 7", nil }}
		return l.Run(ctx, msgs)
	}
	if _, err := afthread.Run(ctx, s, "ann", th.ID, agent); err != nil {
		t.Fatal(err)
	}
	runID = "run-1"
	r, err := s.GetRun(ctx, "ann", runID)
	if err != nil || r.Status != afthread.Succeeded || r.Thread != th.ID || r.Turns != 2 || r.ToolCalls != 1 || r.Usage.InputTokens != 110 {
		t.Fatalf("run = %+v, %v", r, err)
	}
	events, _ := s.Events(ctx, "ann", runID, 0)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	// No text deltas; tool events carry the call and result.
	if strings.Join(kinds, ",") != "compacted,model_responded,tool_called,tool_returned,model_responded" {
		t.Fatalf("events = %v", kinds)
	}
	if !strings.Contains(string(events[3].Data), `"result":"row 7"`) || !strings.Contains(string(events[2].Data), `"arguments":"{\"id\":7}"`) {
		t.Errorf("tool events = %s / %s", events[2].Data, events[3].Data)
	}

	// A failed run is recorded as failed, with the error.
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m2", Content: "again"})
	failing, _ := scripted(t, afagent.Message{Content: "FAIL"})
	afthread.Run(ctx, s, "ann", th.ID, loopAgent(failing, nil))
	if r, _ := s.GetRun(ctx, "ann", "run-2"); r.Status != afthread.RunFailed || !strings.Contains(r.Error, "model down") {
		t.Fatalf("failed run = %+v", r)
	}
}

// Without a recorded run, Record and Recorder do nothing.
func TestRecordWithoutRun(t *testing.T) {
	if err := afthread.Record(context.Background(), "x", 1); err != nil {
		t.Fatal(err)
	}
	afthread.Recorder(context.Background())(afagent.Event{Kind: afagent.ToolCalled, Call: &afagent.ToolCall{}})
}

// failingStore fails one method, to check the run record on every path.
type failingStore struct {
	*afthread.Memory
	transcript, finish bool
}

func (f failingStore) Transcript(ctx context.Context, owner, id string) ([]afagent.Message, error) {
	if f.transcript {
		return nil, errors.New("transcript unavailable")
	}
	return f.Memory.Transcript(ctx, owner, id)
}

func (f failingStore) Finish(ctx context.Context, owner, id string, ids []int64, err error) error {
	if f.finish {
		return errors.New("finish unavailable")
	}
	return f.Memory.Finish(ctx, owner, id, ids, err)
}

func TestRunRecordsEveryFailure(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		store failingStore
		want  string
	}{
		"transcript": {failingStore{Memory: &afthread.Memory{}, transcript: true}, "transcript unavailable"},
		"finish":     {failingStore{Memory: &afthread.Memory{}, finish: true}, "finish unavailable"},
	} {
		s := c.store
		th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
		s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "hi"})
		model, _ := scripted(t, afagent.Message{Content: "hello"})
		_, err := afthread.Run(ctx, s, "ann", th.ID, loopAgent(model, nil))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: got %v", name, err)
		}
		// The run doesn't stay running, and its error says why.
		if r, err := s.GetRun(ctx, "ann", "run-1"); err != nil || r.Status != afthread.RunFailed || !strings.Contains(r.Error, c.want) {
			t.Errorf("%s: run = %+v, %v", name, r, err)
		}
	}
}
