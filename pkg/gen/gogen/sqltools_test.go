package gogen

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

const findSQL = "\n.sql :many\nSELECT id, body, pinned FROM notes WHERE owner_id = @owner_id AND strpos(lower(body), lower(@text)) > 0\n"

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
		"arg.Text = args.Text",
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
		{"wrong type", ".tool find_notes\n.param text bool\n.auth owner_id\nFinds." + findSQL, "parameter text is bool, but query argument text is string"},
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

func TestConvert(t *testing.T) {
	field := func(expr string) sqlcgo.Field {
		return sqlcgo.Field{Name: "X", JSON: "x", Type: sqlcgo.Type{Expr: expr}}
	}
	param := func(kind decl.Kind, array bool) *decl.Param {
		p := &decl.Param{Name: "x"}
		p.Type.Kind, p.Type.Array = kind, array
		return p
	}
	for _, tt := range []struct {
		name string
		p    *decl.Param
		to   string
		want string
	}{
		{"int64", param(decl.Int, false), "int64", "arg.X = int64(args.X)"},
		{"int32 range-checked", param(decl.Int, false), "int32", `afhttp.Parse("x", strconv.Itoa(args.X), &arg.X)`},
		// float32(1e300) would be +Inf: parse instead, which rejects it.
		{"float32 range-checked", param(decl.Float, false), "float32", `afhttp.Parse("x", strconv.FormatFloat(args.X, 'g', -1, 64), &arg.X)`},
		{"[]int64", param(decl.Int, true), "[]int64", "arg.X = make([]int64, len(args.X))\nfor i, v := range args.X {\narg.X[i] = int64(v)\n}"},
		{"[]int32", param(decl.Int, true), "[]int32", `afhttp.Parse("x", strconv.Itoa(v), &arg.X[i])`},
		{"[]string", param(decl.String, true), "[]string", "arg.X = args.X"},
		{"database type", param(decl.String, false), "pgtype.UUID", "if err := afhttp.Parse(\"x\", args.X, &arg.X); err != nil {"},
		// Left out, an optional value stays NULL.
		{"optional database type", &decl.Param{Name: "x", Optional: true}, "pgtype.Text", "if args.X != \"\" {\nif err := afhttp.Parse(\"x\", args.X, &arg.X)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := convert(&buf, map[string]bool{}, tt.p, field(tt.to), "arg.X", "args.X"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("got\n%s\nwant %s", buf.String(), tt.want)
			}
		})
	}
	// An optional list still parses every element: only the list may be
	// left out.
	var list bytes.Buffer
	optional := &decl.Param{Name: "x", Optional: true}
	optional.Type.Array = true
	if err := convert(&list, map[string]bool{}, optional, field("[]pgtype.UUID"), "arg.X", "args.X"); err != nil || strings.Contains(list.String(), `if v != ""`) || !strings.Contains(list.String(), `afhttp.Parse("x", v, &arg.X[i])`) {
		t.Errorf("optional list (%v):\n%s", err, list.String())
	}
	var buf bytes.Buffer
	if err := convert(&buf, map[string]bool{}, param(decl.Bool, true), field("[]int64"), "a", "b"); err == nil {
		t.Error("a list of bools converted to []int64")
	}
	// Inference accepts integer lists, which convert handles, and refuses
	// lists of database types.
	if p, err := inferParam(field("[]int64")); err != nil || p.Type.Kind != decl.Int || !p.Type.Array {
		t.Errorf("[]int64: %+v, %v", p, err)
	}
	if _, err := inferParam(field("[]pgtype.UUID")); err == nil {
		t.Error("[]pgtype.UUID inferred")
	}
}
