package afthread_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

func TestMessenger(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	support, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support", Title: "Answers customers."})
	tracker, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "tracker", Title: "Tracks parcels."})
	s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "billing", Title: "Handles invoices."})
	s.Create(ctx, afthread.Thread{Owner: "bob", Agent: "intruder"})
	var notified []string
	m := &afthread.Messenger{Store: s, Directory: s, Owner: "ann", From: "support",
		CanMessage: func(ctx context.Context, from, to string) bool { return to != "billing" },
		Notify:     func(ctx context.Context, owner, thread string) error { notified = append(notified, thread); return nil }}

	got, err := m.Send(ctx, "tracker", "where is A-1?")
	if err != nil || !strings.Contains(got, "Sent to the tracker agent") {
		t.Fatalf("Send = %q, %v", got, err)
	}
	in := s.Inbox("ann", tracker.ID)
	if len(in) != 1 || in[0].From != "support" || in[0].Content != "where is A-1?" || len(notified) != 1 || notified[0] != tracker.ID {
		t.Fatalf("inbox %+v, notified %v", in, notified)
	}
	for to, want := range map[string]string{
		"":         "name the agent",
		"support":  "can't message itself",
		"billing":  "may not message the billing agent",
		"intruder": `no agent named "intruder"`, // bob's agent is invisible to ann's
		"nobody":   `no agent named "nobody"`,
	} {
		if _, err := m.Send(ctx, to, "hi"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Send to %q: %v, want %q", to, err, want)
		}
	}
	if _, err := m.Send(ctx, "tracker", "  "); err == nil {
		t.Error("empty message sent")
	}
	// List leaves out the sender and whom it may not message.
	if list, err := m.List(ctx); err != nil || list != `[{"name":"tracker","description":"Tracks parcels."}]` {
		t.Fatalf("List = %s, %v", list, err)
	}
	// A failing Notify fails the send, so the caller knows.
	m.Notify = func(context.Context, string, string) error { return errors.New("queue down") }
	if _, err := m.Send(ctx, "tracker", "again"); err == nil || err.Error() != "queue down" {
		t.Fatalf("Notify error: %v", err)
	}
	_ = support
}

// Two agents converse through their threads: support asks tracker, which
// answers on its own run, and support sees the answer in its next run.
func TestMessengerConversation(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	support, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	tracker, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "tracker"})
	messenger := func(from string) *afthread.Messenger {
		return &afthread.Messenger{Store: s, Directory: s, Owner: "ann", From: from}
	}
	// An agent whose model sends one message, then answers.
	agent := func(from, to, text string) afthread.Agent {
		return func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
			sent := false
			model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
				if !sent && to != "" {
					sent = true
					return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "send_message", Arguments: `{}`}}}}, nil
				}
				return afagent.Response{Message: afagent.Message{Content: "ok"}}, nil
			})
			l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "send_message"}}, BeforeTurn: before,
				Dispatch: func(ctx context.Context, call afagent.ToolCall) (string, error) { return messenger(from).Send(ctx, to, text) }}
			return l.Run(ctx, msgs)
		}
	}
	s.Send(ctx, "ann", support.ID, afthread.Incoming{Key: "u1", Content: "where is A-1?"})
	if _, err := afthread.Run(ctx, s, "ann", support.ID, agent("support", "tracker", "Where is order A-1?")); err != nil {
		t.Fatal(err)
	}
	if _, err := afthread.Run(ctx, s, "ann", tracker.ID, agent("tracker", "support", "A-1 arrives Tuesday.")); err != nil {
		t.Fatal(err)
	}
	if _, err := afthread.Run(ctx, s, "ann", support.ID, agent("support", "", "")); err != nil {
		t.Fatal(err)
	}
	trackerSaw, _ := s.Transcript(ctx, "ann", tracker.ID)
	supportSaw, _ := s.Transcript(ctx, "ann", support.ID)
	if !strings.Contains(trackerSaw[0].Content, "[message from the support agent]\nWhere is order A-1?") {
		t.Errorf("tracker saw %q", trackerSaw[0].Content)
	}
	last := supportSaw[len(supportSaw)-2]
	if !strings.Contains(last.Content, "[message from the tracker agent]\nA-1 arrives Tuesday.") {
		t.Errorf("support's reply message = %q", last.Content)
	}
}
