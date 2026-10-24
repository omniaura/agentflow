package decl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseTool(t *testing.T) {
	src := `.title System
Hello.

.tool manage_billing
.maxbytes 4096
.param view enum(overview, plans,topup) Which control to show.
.param note string?
.param filter object? Narrow the usage.
.param filter.since string ISO date.
.param items []object
.param items.qty int?	How many.

Show a billing control.
  Indented line.

.tool ping
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(f.Tools))
	}
	tool := f.Tools[0]
	if tool.Name != "manage_billing" || tool.MaxBytes != 4096 {
		t.Fatalf("got name %q maxbytes %d", tool.Name, tool.MaxBytes)
	}
	if want := "Show a billing control.\n  Indented line."; tool.Description != want {
		t.Fatalf("description = %q, want %q", tool.Description, want)
	}
	if got := src[tool.Start:tool.End]; !strings.HasPrefix(got, ".tool manage_billing\n") || !strings.HasSuffix(got, "Indented line.\n\n") {
		t.Fatalf("section = %q", got)
	}
	type flat struct {
		Name, Desc string
		Type       Type
		Optional   bool
		Children   int
	}
	var got []flat
	for _, p := range tool.Params {
		got = append(got, flat{p.Name, p.Description, p.Type, p.Optional, len(p.Children)})
	}
	want := []flat{
		{"view", "Which control to show.", Type{Kind: Enum, Enum: []string{"overview", "plans", "topup"}}, false, 0},
		{"note", "", Type{Kind: String}, true, 0},
		{"filter", "Narrow the usage.", Type{Kind: Object}, true, 1},
		{"items", "", Type{Kind: Object, Array: true}, false, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params:\n got %+v\nwant %+v", got, want)
	}
	qty := tool.Params[3].Children[0]
	if qty.Name != "qty" || qty.Type.Kind != Int || !qty.Optional || qty.Description != "How many." {
		t.Fatalf("items.qty = %+v", qty)
	}
	if ping := f.Tools[1]; ping.Name != "ping" || ping.Description != "" || len(ping.Params) != 0 {
		t.Fatalf("ping = %+v", ping)
	}
}

func TestParseToolErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"missing name", ".tool\n", `tool name ""`},
		{"bad name", ".tool a b\n", `tool name "a b"`},
		{"missing param name", ".tool x\n.param\n", "expected .param <name>"},
		{"bad path", ".tool x\n.param a-b string\n", `parameter name "a-b"`},
		{"misplaced brackets", ".tool x\n.param a[]b.c\n", `[] goes right before a '.'`},
		{"trailing brackets", ".tool x\n.param a[] int\n", `write a []object instead of a[]`},
		{"scalar parent", ".tool x\n.param a int\n.param a.b\n", `inside "a", which is int, not an object`},
		{"scalar after children", ".tool x\n.param a.b\n.param a int\n", `must be object or []object, not int`},
		{"array mismatch", ".tool x\n.param a[].b\n.param a object\n", `declare it []object`},
		{"duplicate param", ".tool x\n.param a string\n.param a int\n", `parameter "a" is declared twice`},
		{"empty enum value", ".tool x\n.param a enum(b,,c)\n", "empty value"},
		{"repeated enum value", ".tool x\n.param a enum(b,b)\n", `repeats "b"`},
		{"unclosed enum", ".tool x\n.param a enum(b, c\n", "missing ')'"},
		{"bad maxbytes", ".tool x\n.maxbytes lots\n", ".maxbytes needs a positive integer"},
		{"duplicate tool", ".tool x\n.tool x\n", `tool "x" is declared twice`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src))
			var de *Error
			if !errors.As(err, &de) || !strings.Contains(de.Msg, tt.msg) {
				t.Fatalf("got %v, want error containing %q", err, tt.msg)
			}
			if de.Offset < 0 || de.Offset > len(tt.src) {
				t.Fatalf("offset %d out of range", de.Offset)
			}
		})
	}
}

// Like prompt variables, tool parameters need no up-front declarations: the
// type defaults to string, and dotted paths imply their objects.
func TestParseToolInference(t *testing.T) {
	src := `.tool search
.param query What to search for.
.param limit? int At most this many.
.param filter.since ISO date.
.param filter.tags? []string
.param hits[].url
.param hits[].score? float
.param hits []object? Earlier hits to rerank.
.param opts.verbose? bool
.param strng is a typo
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	var walk func(prefix string, ps []*Param)
	walk = func(prefix string, ps []*Param) {
		for _, p := range ps {
			opt := ""
			if p.Optional {
				opt = "?"
			}
			got = append(got, prefix+p.Name+opt+" "+p.Type.String()+" "+p.Description)
			walk(prefix+p.Name+".", p.Children)
		}
	}
	walk("", f.Tools[0].Params)
	want := []string{
		"query string What to search for.",
		"limit? int At most this many.",
		"filter object ",
		"filter.since string ISO date.",
		"filter.tags? []string ",
		"hits? []object Earlier hits to rerank.",
		"hits.url string ",
		"hits.score? float ",
		"opts? object ",
		"opts.verbose? bool ",
		"strng string is a typo",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("params:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if w := f.Tools[0].Warnings; len(w) != 0 {
		t.Fatalf("unexpected warnings %v", w)
	}
	f, err = Parse([]byte(".tool x\n.param a strng The value.\n.param b Strings to match.\n.param c in UTC.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if w := f.Tools[0].Warnings; len(w) != 1 || !strings.Contains(w[0].Msg, "did you mean type string") {
		t.Fatalf("warnings = %v", w)
	}
}

func TestSectionBounds(t *testing.T) {
	src := []byte("text\n.tool a\nbody\n.toolbox is text\n.title T\nx\n")
	if IsSectionStart(src, 0) || !IsSectionStart(src, 5) {
		t.Fatal("IsSectionStart")
	}
	// ".toolbox" is not a .tool line, so the section runs to .title.
	if end := SectionEnd(src, 5); string(src[end:]) != ".title T\nx\n" {
		t.Fatalf("SectionEnd = %d (%q)", end, src[end:])
	}
	f, err := Parse(src)
	if err != nil || len(f.Tools) != 1 || f.Tools[0].Description != "body\n.toolbox is text" {
		t.Fatalf("got %+v, %v", f.Tools, err)
	}
}

func TestParseSQLTool(t *testing.T) {
	src := ".tool pin_note\n.param note_id int Which note.\n.auth owner_id\nPins a note.\n.sql :execrows\nUPDATE notes SET pinned = true\nWHERE id = @note_id AND owner_id = @owner_id\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tool := f.Tools[0]
	if tool.SQLKind != ":execrows" || tool.SQL != "UPDATE notes SET pinned = true\nWHERE id = @note_id AND owner_id = @owner_id" || tool.Description != "Pins a note." || len(tool.Auth) != 1 || tool.Auth[0] != "owner_id" {
		t.Fatalf("tool = %+v", tool)
	}
	for _, tt := range []struct{ src, msg string }{
		{".tool t\n.auth owner_id\nDoes t.\n", ".auth names arguments of the tool's query"},
		{".tool t\n.param owner_id\n.auth owner_id\nDoes t.\n.sql :one\nSELECT 1\n", "owner_id comes from the caller (.auth)"},
		{".tool t\nDoes t.\n.sql :batchone\nSELECT 1\n", ".sql needs the query's sqlc command"},
		{".tool t\nDoes t.\n.sql :one\n", "write the query on the lines after .sql"},
		{".tool t\nDoes t.\n.sql :one\nSELECT 1\n.auth owner_id\n", ".auth must come before .sql"},
		{".tool t\nDoes t.\n.sql :one\nSELECT @x\n.param x int\n", ".param must come before .sql"},
		{".route GET /a A\n.sql :one\nSELECT 1\n.public\n", ".public must come before .sql"},
		{".tool t\nDoes t.\n.sql :one\nSELECT 1\n.auth,owner_id\n", "malformed .auth directive"},
		{".tool t\nDoes t.\n.sql :one\nSELECT 1\n.sql:many\n", "malformed .sql directive"},
	} {
		if _, err := Parse([]byte(tt.src)); err == nil || !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("%q: got %v, want %q", tt.src, err, tt.msg)
		}
	}
	// An .output has no query.
	if f, err := Parse([]byte(".output o\n.field x\n.sql :one\n")); err == nil && f.Outputs[0].SQL != "" {
		t.Error(".output accepted .sql")
	}
}
