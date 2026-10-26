package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// A generated agent answers a thread's messages through afthread: its own
// BeforeTurn (a compactor, here clearing old tool results) runs before the
// thread saves, so the thread keeps what the model saw, and the run's
// events are recorded without wiring.
func TestSupportOnAThread(t *testing.T) {
	ctx := context.Background()
	store := &afthread.Memory{}
	th, _ := store.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	model, seen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "lookup_order", `{"order_id":"A-1"}`)}},
		afagent.Message{Content: "A-1 arrived late."},
	)
	// When turn 2 starts, the thread holds what the model sees.
	checked := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		if len(req.Messages) == 3 {
			saved, _ := store.Transcript(ctx, "ann", th.ID)
			if len(saved) != 3 || saved[2].Content != "[cleared]" {
				t.Errorf("saved before turn 2: %+v", saved)
			}
		}
		return model.Complete(ctx, req)
	})
	support := &SupportAgent{Model: checked, Tools: &shop{},
		BeforeTurn: func(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
			out := append([]afagent.Message(nil), msgs...)
			for i := range out {
				if out[i].Role == afagent.Tool {
					out[i].Content = "[cleared]"
				}
			}
			return out, nil
		}}
	w := &afthread.Worker{Store: store, Agent: func(ctx context.Context, t afthread.Thread) (afthread.Agent, error) {
		return support.ForThread(&Support{Shop: "Acme"}), nil
	}}
	store.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "Where is A-1?"})
	if err := w.Drain(ctx, "ann", th.ID); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[1].Messages[2].Content; got != "[cleared]" {
		t.Errorf("the model saw the tool result %q", got)
	}
	transcript, _ := store.Transcript(ctx, "ann", th.ID)
	if len(transcript) != 4 || transcript[2].Content != "[cleared]" || transcript[3].Content != "A-1 arrived late." {
		t.Errorf("transcript = %+v", transcript)
	}
	events, _ := store.Events(ctx, "ann", "run-1", 0)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "model_responded,tool_called,tool_returned,model_responded" {
		t.Errorf("events = %v", kinds)
	}

	// An agent with .returns runs on a thread too.
	triage, _ := script(t, afagent.Message{Content: `{"category":"account","order_id":null,"urgent":false}`})
	var agent afthread.Agent = (&TriagerAgent{Model: triage, Tools: &shop{}}).ForThread(&Support{Shop: "Acme"})
	store.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "I can't log in"})
	res, err := afthread.Run(ctx, store, "ann", th.ID, agent)
	if err != nil || !strings.Contains(res.Final, `"account"`) {
		t.Fatalf("Run = %+v, %v", res, err)
	}
}
