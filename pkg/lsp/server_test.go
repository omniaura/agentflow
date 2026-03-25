package lsp

import "testing"

func TestParseChangeEventFromMapRequiresStringText(t *testing.T) {
	changeEvent, ok := parseChangeEventFromMap(map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": 0.0, "character": 0.0},
			"end":   map[string]any{"line": 0.0, "character": 1.0},
		},
	}, "file:///tmp/test.af", 0)
	if ok {
		t.Fatalf("expected missing text field to be rejected, got %+v", changeEvent)
	}
}

func TestParseChangeEventFromMapParsesSafeFallbackPayload(t *testing.T) {
	changeEvent, ok := parseChangeEventFromMap(map[string]any{
		"text": "hello",
		"range": map[string]any{
			"start": map[string]any{"line": 1.0, "character": 2.0},
			"end":   map[string]any{"line": 3.0, "character": 4.0},
		},
		"rangeLength": 5.0,
	}, "file:///tmp/test.af", 1)
	if !ok {
		t.Fatal("expected valid fallback payload to be accepted")
	}
	if changeEvent.Text != "hello" {
		t.Fatalf("expected text to be preserved, got %q", changeEvent.Text)
	}
	if changeEvent.Range == nil {
		t.Fatal("expected range to be parsed")
	}
	if changeEvent.Range.Start.Line != 1 || changeEvent.Range.Start.Character != 2 {
		t.Fatalf("unexpected start range: %+v", changeEvent.Range.Start)
	}
	if changeEvent.Range.End.Line != 3 || changeEvent.Range.End.Character != 4 {
		t.Fatalf("unexpected end range: %+v", changeEvent.Range.End)
	}
	if changeEvent.RangeLength == nil || *changeEvent.RangeLength != 5 {
		t.Fatalf("expected range length 5, got %+v", changeEvent.RangeLength)
	}
}
