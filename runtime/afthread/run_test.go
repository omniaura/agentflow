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
