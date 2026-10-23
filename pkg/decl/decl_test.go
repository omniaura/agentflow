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
		{"missing type", ".tool x\n.param a\n", "expected .param <name> <type>"},
		{"unknown type", ".tool x\n.param a str\n", `unknown parameter type "str"`},
		{"bad path", ".tool x\n.param a-b string\n", `parameter name "a-b"`},
		{"undeclared parent", ".tool x\n.param a.b string\n", `needs "a" declared before it`},
		{"scalar parent", ".tool x\n.param a string\n.param a.b string\n", `which is not an object`},
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
