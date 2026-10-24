package gogen

import (
	"errors"
	"strings"
	"testing"
)

const findSQL = "\n.sql :many\nSELECT id, body, pinned FROM notes WHERE owner_id = @owner_id AND body ILIKE '%' || @text || '%'\n"

func TestSQLTools(t *testing.T) {
	code, err := genRoutesFile(t, ".title A\nHi.\n\n.tool find_notes\n.param text What to find.\n.auth owner_id\nFinds notes."+findSQL)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type ApiSQLTools struct",
		"Caller func(ctx context.Context) (Caller, error)",
		"func (t *ApiSQLTools) FindNotes(ctx context.Context, args FindNotesArgs) (string, error) {",
		"arg.OwnerID = caller.OwnerID",
		`afhttp.Parse("text", args.Text, &arg.Text)`,
		// The model sees text, with its description, and not owner_id.
		`"text":{"type":"string","description":"What to find."}`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(code, `"owner_id"`) {
		t.Error("owner_id is in the tool's schema")
	}
}

func TestSQLToolInfersParams(t *testing.T) {
	code, err := genRoutesFile(t, ".title A\nHi.\n\n.tool pin_note\n.auth owner_id\nPins.\n.sql :execrows\nUPDATE notes SET pinned = true WHERE id = @note_id AND owner_id = @owner_id\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"note_id":{"type":"integer"}`, `"required":["note_id"]`, "arg.NoteID = int64(args.NoteId)"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSQLToolErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"stray param", ".tool find_notes\n.param words\n.auth owner_id\nFinds." + findSQL, "parameter words matches no argument of query FindNotesTool (ownerId, text)"},
		{"stray auth", ".tool find_notes\n.auth owner_id, team_id\nFinds." + findSQL, ".auth team_id matches no argument"},
		{"wrong type", ".tool find_notes\n.param text bool\n.auth owner_id\nFinds." + findSQL, "parameter text is bool, but query argument text is pgtype.Text"},
		{"object", ".tool find_notes\n.param text.words\n.auth owner_id\nFinds." + findSQL, "parameter text is an object"},
		{"not generated", ".tool nope\nNope.\n.sql :one\nSELECT 1\n", "no sqlc query NopeTool"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genRoutesFile(t, ".title A\nHi.\n\n"+tt.src)
			var de *DeclError
			if !errors.As(err, &de) || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("got %v, want %q", err, tt.msg)
			}
		})
	}
}
