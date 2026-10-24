package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
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
