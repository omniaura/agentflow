package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/aftool"
)

type courier struct{ shop }

func (c *courier) TrackShipment(ctx context.Context, args TrackShipmentArgs) (string, error) {
	return "in transit, arriving Tuesday: " + args.OrderId, nil
}

// The tracker agent's track_shipment tool comes from shipping.af, another
// file of the package: its schema and decoder are generated there.
func TestTrackerUsesAnotherFilesTool(t *testing.T) {
	model, seen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "track_shipment", `{"order_id":"A-1"}`)}},
		afagent.Message{Content: "Your parcel arrives Tuesday."},
	)
	res, err := (&TrackerAgent{Model: model, Tools: &courier{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "where is A-1?"})
	if err != nil || res.Final != "Your parcel arrives Tuesday." {
		t.Fatalf("got %v: %+v", err, res)
	}
	if got := res.Messages[2].Content; got != "in transit, arriving Tuesday: A-1" {
		t.Errorf("tool result = %q", got)
	}
	var names []string
	for _, spec := range (*seen)[0].Tools {
		names = append(names, spec.Name)
	}
	if len(names) != 2 || names[1] != "track_shipment" || string((*seen)[0].Tools[1].Schema) != string(TrackShipmentTool.Schema) {
		t.Errorf("tools = %v", names)
	}
	// Arguments go through shipping.af's strict decoder.
	model, seen = script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "track_shipment", `{"order":"A-1"}`)}},
		afagent.Message{Content: "ok"},
	)
	if _, err := (&TrackerAgent{Model: model, Tools: &courier{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if bad := (*seen)[1].Messages[2]; !bad.IsError {
		t.Errorf("unknown argument accepted: %+v", bad)
	}
}

// .maxcalls 3: after three tool calls the tracker answers without tools.
func TestTrackerMaxCalls(t *testing.T) {
	track := afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "track_shipment", `{"order_id":"A-1"}`), call("2", "track_shipment", `{"order_id":"A-2"}`)}}
	model, seen := script(t, track, track, afagent.Message{Content: "A-1 arrives Tuesday."})
	res, err := (&TrackerAgent{Model: model, Tools: &courier{}}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "where are my parcels?"})
	if err != nil || res.ToolCalls != 3 || !res.OutOfToolCalls || res.Final != "A-1 arrives Tuesday." {
		t.Fatalf("got %v: %d calls, out %v, %q", err, res.ToolCalls, res.OutOfToolCalls, res.Final)
	}
	if !(*seen)[2].NoToolCalls {
		t.Error("tools offered after the limit")
	}
	// The fourth call was refused, not run.
	if last := res.Messages[len(res.Messages)-2]; !last.IsError || !strings.Contains(last.Content, "limit of 3 tool calls") {
		t.Errorf("fourth call = %+v", last)
	}
	// MaxToolCalls overrides .maxcalls.
	model, _ = script(t, track, track, track, afagent.Message{Content: "ok"})
	if res, err := (&TrackerAgent{Model: model, Tools: &courier{}, MaxToolCalls: 10}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"}); err != nil || res.ToolCalls != 6 {
		t.Fatalf("MaxToolCalls 10: %v, %d calls", err, res.ToolCalls)
	}
}

// The concierge hands tracking to the tracker agent, which runs as a
// subagent: its own model, tools and turns, seeing only the task.
func TestConciergeDelegatesToTracker(t *testing.T) {
	trackerModel, trackerSeen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("t1", "track_shipment", `{"order_id":"A-1"}`)}},
		afagent.Message{Content: "A-1 arrives Tuesday."},
	)
	tracker := &TrackerAgent{Model: trackerModel, Tools: &courier{}}
	conciergeModel, conciergeSeen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("c1", "tracker", `{"task":"Where is order A-1?"}`)}},
		afagent.Message{Content: "Your parcel arrives Tuesday."},
	)
	concierge := &ConciergeAgent{Model: conciergeModel, Tools: &shop{}, Tracker: tracker.AsTool(&Support{})}
	res, err := concierge.Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "where's my stuff?"})
	if err != nil || res.Final != "Your parcel arrives Tuesday." {
		t.Fatalf("got %v: %+v", err, res)
	}
	// The concierge sees the tracker as a tool described by its .agent text.
	var offered []string
	for _, spec := range (*conciergeSeen)[0].Tools {
		offered = append(offered, spec.Name)
	}
	if strings.Join(offered, ",") != "lookup_order,tracker" || !strings.Contains(TrackerAgentTool.Description, "where their parcel is") {
		t.Errorf("offered %v, description %q", offered, TrackerAgentTool.Description)
	}
	// The tracker got only the task, and its answer came back as the result.
	if first := (*trackerSeen)[0].Messages; len(first) != 1 || first[0].Content != "Where is order A-1?" {
		t.Errorf("tracker's messages = %+v", first)
	}
	if got := res.Messages[2]; got.IsError || got.Content != "A-1 arrives Tuesday." {
		t.Errorf("tracker result = %+v", got)
	}
}

func TestConciergeSubagentErrors(t *testing.T) {
	ask := afagent.Message{ToolCalls: []afagent.ToolCall{call("c1", "tracker", `{"task":"Where is A-1?"}`)}}
	for name, c := range map[string]struct {
		tracker func(context.Context, string) (string, error)
		args    string
		want    string
	}{
		"unset":       {nil, `{"task":"x"}`, "the tracker agent is not set up"},
		"failed":      {func(context.Context, string) (string, error) { return "", errors.New("model down") }, `{"task":"x"}`, "model down"},
		"no task":     {func(context.Context, string) (string, error) { return "ok", nil }, `{}`, "task"},
		"extra field": {func(context.Context, string) (string, error) { return "ok", nil }, `{"task":"x","agent":"y"}`, "agent"},
	} {
		ask.ToolCalls[0].Arguments = c.args
		model, _ := script(t, ask, afagent.Message{Content: "sorry"})
		res, err := (&ConciergeAgent{Model: model, Tools: &shop{}, Tracker: c.tracker}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
		if err != nil || !res.Messages[2].IsError || !strings.Contains(res.Messages[2].Content, c.want) {
			t.Errorf("%s: %v, %+v", name, err, res.Messages[2])
		}
	}
}

// An agent with .returns, called as a tool, returns its output as JSON.
func TestTriagerAsTool(t *testing.T) {
	model, _ := script(t, afagent.Message{Content: `{"category":"order","order_id":null,"urgent":false}`})
	got, err := (&TriagerAgent{Model: model, Tools: &shop{}}).AsTool(&Support{})(context.Background(), "my order is late")
	if err != nil || got != `{"category":"order","urgent":false}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

type fakeMessenger struct{ sent []string }

func (f *fakeMessenger) Send(ctx context.Context, to, text string) (string, error) {
	f.sent = append(f.sent, to+": "+text)
	return "sent to " + to, nil
}

func (f *fakeMessenger) List(ctx context.Context) (string, error) {
	return `[{"name":"tracker"}]`, nil
}

// The coordinator's send_message and list_agents are built in: their
// schemas come from aftool and their calls go to Messenger.
func TestCoordinatorMessages(t *testing.T) {
	model, seen := script(t,
		afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "list_agents", `{}`)}},
		afagent.Message{ToolCalls: []afagent.ToolCall{call("2", "send_message", `{"to":"tracker","text":"Where is A-1?"}`)}},
		afagent.Message{Content: "Asked the tracker."},
	)
	m := &fakeMessenger{}
	res, err := (&CoordinatorAgent{Model: model, Messenger: m}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "find A-1"})
	if err != nil || res.Final != "Asked the tracker." {
		t.Fatalf("got %v: %+v", err, res)
	}
	if tools := (*seen)[0].Tools; len(tools) != 2 || string(tools[1].Schema) != string(aftool.SendMessage.Schema) {
		t.Errorf("tools = %+v", tools)
	}
	if res.Messages[2].Content != `[{"name":"tracker"}]` || res.Messages[4].Content != "sent to tracker" || len(m.sent) != 1 || m.sent[0] != "tracker: Where is A-1?" {
		t.Errorf("messages = %+v, sent %v", res.Messages, m.sent)
	}
	// Without a Messenger, or with bad arguments, the call fails back to
	// the model.
	for _, c := range []struct {
		messenger afagent.Messenger
		args, want string
	}{
		{nil, `{"to":"tracker","text":"x"}`, "messaging other agents is not set up"},
		{&fakeMessenger{}, `{"to":"tracker"}`, "text"},
	} {
		model, _ := script(t, afagent.Message{ToolCalls: []afagent.ToolCall{call("1", "send_message", c.args)}}, afagent.Message{Content: "ok"})
		res, err := (&CoordinatorAgent{Model: model, Messenger: c.messenger}).Run(context.Background(), &Support{}, afagent.Message{Role: afagent.User, Content: "hi"})
		if err != nil || !res.Messages[2].IsError || !strings.Contains(res.Messages[2].Content, c.want) {
			t.Errorf("%s: %v, %+v", c.args, err, res.Messages[2])
		}
	}
}
