package agent

import (
	"context"
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
