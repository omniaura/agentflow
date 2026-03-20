package lsp

import (
	"fmt"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestDocumentManagerLimitsOpenDocuments(t *testing.T) {
	dm := NewDocumentManager()

	for i := 0; i < maxOpenDocuments; i++ {
		_, err := dm.OpenDocument(protocol.TextDocumentItem{
			URI:        fmt.Sprintf("file:///tmp/%d.af", i),
			LanguageID: "agentflow",
			Version:    1,
			Text:       ".title Prompt\nHello <!name>",
		})
		require.NoError(t, err)
	}

	_, err := dm.OpenDocument(protocol.TextDocumentItem{
		URI:        "file:///tmp/overflow.af",
		LanguageID: "agentflow",
		Version:    1,
		Text:       ".title Prompt\nHello",
	})
	require.EqualErr(t, ErrTooManyOpenDocuments, err)
}

func TestDocumentCompletionIncludesVariablesAndTypes(t *testing.T) {
	doc := mustOpenTestDocument(t, ".title Prompt\nHello <!user.name string>\n<!")

	items := doc.GetCompletionItems(Position{Line: 2, Character: 2})

	labels := map[string]bool{}
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["user.name"] {
		t.Fatalf("expected completion items to include existing variable")
	}
}

func TestDocumentCompletionSuggestsTypesInsideVariable(t *testing.T) {
	doc := mustOpenTestDocument(t, ".title Prompt\n<!user ")

	items := doc.GetCompletionItems(Position{Line: 1, Character: 7})

	labels := map[string]bool{}
	for _, item := range items {
		labels[item.Label] = true
	}

	for _, typ := range []string{"string", "int", "bool", "float32", "float64"} {
		if !labels[typ] {
			t.Fatalf("expected completion items to include type %q", typ)
		}
	}
}

func TestParseRangeFromMapRejectsInvalidNumbers(t *testing.T) {
	if _, ok := parseRangeFromMap(map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": -1.0, "character": 0.0},
			"end":   map[string]any{"line": 0.0, "character": 1.0},
		},
	}); ok {
		t.Fatal("expected negative positions to be rejected")
	}

	if _, ok := parseRangeFromMap(map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": 1.5, "character": 0.0},
			"end":   map[string]any{"line": 2.0, "character": 1.0},
		},
	}); ok {
		t.Fatal("expected fractional positions to be rejected")
	}
}

func mustOpenTestDocument(t *testing.T, text string) *Document {
	t.Helper()

	dm := NewDocumentManager()
	doc, err := dm.OpenDocument(protocol.TextDocumentItem{
		URI:        "file:///tmp/test.af",
		LanguageID: "agentflow",
		Version:    1,
		Text:       text,
	})
	require.NoError(t, err)
	return doc
}
