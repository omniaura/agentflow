package afagent

import (
	"context"
	"strings"
	"testing"
)

func TestToolCatalogSnapshotsGatesAndRoutes(t *testing.T) {
	base := []ToolSpec{{Name: "declared", Schema: []byte(`{"type":"object"}`)}}
	extra := []ToolSpec{{Name: "z", Schema: []byte(`{"type":"object"}`)}, {Name: "a", Schema: []byte(`{"type":"object"}`)}, {Name: "denied", Schema: []byte(`{}`)}}
	c, err := PrepareTools(t.Context(), base, extra, func(_ context.Context, name string) bool { return name != "denied" })
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 3 || c.Tools[0].Name != "declared" || c.Tools[1].Name != "a" || c.Tools[2].Name != "z" {
		t.Fatalf("catalog: %+v", c.Tools)
	}
	extra[1].Schema[0] = 'x'
	if string(c.Tools[1].Schema) != `{"type":"object"}` {
		t.Fatal("catalog shares mutable schemas")
	}
	var calls []string
	declared := func(_ context.Context, c ToolCall) (string, error) {
		calls = append(calls, "declared:"+c.Name)
		return "ok", nil
	}
	dynamic := func(_ context.Context, c ToolCall) (string, error) {
		calls = append(calls, "dynamic:"+c.Name)
		return "ok", nil
	}
	for _, name := range []string{"declared", "a", "denied", "unknown"} {
		_, err := c.Dispatch(t.Context(), ToolCall{Name: name}, declared, dynamic)
		if (name == "denied" || name == "unknown") != (err != nil) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if strings.Join(calls, ",") != "declared:declared,dynamic:a" {
		t.Fatalf("unexpected dispatch: %v", calls)
	}
}

func TestToolCatalogRejectsCollisionsAndMalformedSchema(t *testing.T) {
	for _, extra := range [][]ToolSpec{
		{{Name: "base", Schema: []byte(`{}`)}},
		{{Name: "same", Schema: []byte(`{}`)}, {Name: "same", Schema: []byte(`{}`)}},
		{{Name: "", Schema: []byte(`{}`)}},
		{{Name: "broken", Schema: []byte(`[]`)}},
	} {
		if _, err := PrepareTools(t.Context(), []ToolSpec{{Name: "base"}}, extra, nil); err == nil {
			t.Fatalf("accepted invalid catalog: %+v", extra)
		}
	}
}

func TestToolCatalogDoesNotValidateDeniedToolSchema(t *testing.T) {
	c, err := PrepareTools(t.Context(), nil, []ToolSpec{{Name: "denied", Schema: []byte(`bad`)}}, func(context.Context, string) bool { return false })
	if err != nil || len(c.Tools) != 0 || c.HasAdditional() {
		t.Fatalf("denied tool must not block catalog: %+v %v", c, err)
	}
}
