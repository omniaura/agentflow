// Package decl parses the declaration sections of .af files.
//
// A prompt section starts at a .title line and holds a template. A
// declaration section starts at a line naming a section directive, such as
// .tool, and declares something other than a prompt: its body is a series of
// directive lines and free text. Sections run until the next .title or
// section line.
//
// A tool section declares an LLM tool:
//
//	.tool manage_billing
//	.maxbytes 4096
//	.param view enum(overview,plans,topup) Which billing control to show.
//	.param note string? A note to show next to the control.
//	Show an inline billing control for the user's workspace.
//
// The text that is not a directive is the tool's description.
package decl

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SectionDirectives are the directives that start a declaration section.
var SectionDirectives = []string{".tool"}

// ToolDirectives are the directives allowed inside a tool section.
var ToolDirectives = []string{".param", ".maxbytes"}

// Kind is the type of a tool parameter.
type Kind uint8

const (
	String Kind = iota + 1
	Int
	Float
	Bool
	Enum
	Object
	Any
)

// Type is the declared type of a parameter.
type Type struct {
	Kind  Kind
	Array bool
	// Enum holds the allowed values of an Enum.
	Enum []string
}

// Param is one declared tool parameter.
type Param struct {
	Name        string
	Type        Type
	Optional    bool
	Description string
	// Children are the declared properties of an Object.
	Children []*Param
	// Offset is the byte offset of the .param line.
	Offset int
}

// Tool is a parsed .tool section.
type Tool struct {
	Name        string
	Description string
	MaxBytes    int
	Params      []*Param
	// Start and End delimit the section in the source.
	Start, End int
}

// File holds the declaration sections of one .af file.
type File struct {
	Tools []*Tool
}

// Error is a declaration error at a byte offset in the source.
type Error struct {
	Offset int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// IsSectionStart reports whether the line starting at src[i:] opens a
// declaration section. i must be at the start of a line.
func IsSectionStart(src []byte, i int) bool {
	for _, d := range SectionDirectives {
		if hasDirective(src[i:], d) {
			return true
		}
	}
	return false
}

// SectionEnd returns the end of the declaration section that starts at
// src[start:]: the start of the next .title or section line, or len(src).
func SectionEnd(src []byte, start int) int {
	i := lineEnd(src, start)
	for i < len(src) {
		if hasDirective(src[i:], ".title") || IsSectionStart(src, i) {
			return i
		}
		i = lineEnd(src, i)
	}
	return len(src)
}

// lineEnd returns the start of the line after the one at src[i:].
func lineEnd(src []byte, i int) int {
	if j := bytes.IndexByte(src[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(src)
}

func hasDirective(line []byte, d string) bool {
	if !bytes.HasPrefix(line, []byte(d)) {
		return false
	}
	return len(line) == len(d) || line[len(d)] == ' ' || line[len(d)] == '\t' || line[len(d)] == '\n' || line[len(d)] == '\r'
}

// Parse parses every declaration section in src.
func Parse(src []byte) (File, error) {
	var f File
	for i := 0; i < len(src); {
		if !IsSectionStart(src, i) {
			i = lineEnd(src, i)
			continue
		}
		end := SectionEnd(src, i)
		t, err := ParseTool(src, i, end)
		if err != nil {
			return f, err
		}
		for _, prev := range f.Tools {
			if prev.Name == t.Name {
				return f, &Error{Offset: i, Msg: fmt.Sprintf("tool %q is declared twice", t.Name)}
			}
		}
		f.Tools = append(f.Tools, t)
		i = end
	}
	return f, nil
}

var (
	toolNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	identRE    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// ParseTool parses the tool section src[start:end], whose first line is its
// .tool line.
func ParseTool(src []byte, start, end int) (*Tool, error) {
	t := &Tool{Start: start, End: end}
	header, i := line(src, start, end)
	name := strings.TrimSpace(strings.TrimPrefix(header, ".tool"))
	if !toolNameRE.MatchString(name) {
		return nil, &Error{Offset: start, Msg: fmt.Sprintf("tool name %q must be 1-64 letters, digits, '_' or '-'", name)}
	}
	t.Name = name
	params := map[string]*Param{}
	var desc []string
	for i < end {
		off := i
		var l string
		l, i = line(src, i, end)
		switch {
		case hasDirective([]byte(l), ".param"):
			p, err := parseParam(strings.TrimSpace(strings.TrimPrefix(l, ".param")), off)
			if err != nil {
				return nil, err
			}
			if _, dup := params[p.Name]; dup {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q is declared twice", p.Name)}
			}
			if j := strings.LastIndexByte(p.Name, '.'); j >= 0 {
				parent, leaf := p.Name[:j], p.Name[j+1:]
				pp, ok := params[parent]
				if !ok {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q needs %q declared before it as object or []object", p.Name, parent)}
				}
				if pp.Type.Kind != Object {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q is inside %q, which is not an object", p.Name, parent)}
				}
				params[p.Name] = p
				p.Name = leaf
				pp.Children = append(pp.Children, p)
				break
			}
			params[p.Name] = p
			t.Params = append(t.Params, p)
		case hasDirective([]byte(l), ".maxbytes"):
			v := strings.TrimSpace(strings.TrimPrefix(l, ".maxbytes"))
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".maxbytes needs a positive integer, got %q", v)}
			}
			t.MaxBytes = n
		default:
			desc = append(desc, l)
		}
	}
	t.Description = strings.TrimRight(trimLeadingBlankLines(strings.Join(desc, "\n")), " \t\n")
	return t, nil
}

func trimLeadingBlankLines(s string) string {
	for {
		l, rest, ok := strings.Cut(s, "\n")
		if !ok || strings.TrimSpace(l) != "" {
			return s
		}
		s = rest
	}
}

// line returns the line at src[i:] without its newline, and the start of the
// next line.
func line(src []byte, i, end int) (string, int) {
	j := bytes.IndexByte(src[i:end], '\n')
	if j < 0 {
		return strings.TrimRight(string(src[i:end]), "\r"), end
	}
	return strings.TrimRight(string(src[i:i+j]), "\r"), i + j + 1
}

func parseParam(s string, off int) (*Param, error) {
	name, rest := s, ""
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		name, rest = s[:i], strings.TrimSpace(s[i:])
	}
	// The type runs to the first space, or to the closing parenthesis of an
	// enum, so enum(a, b) may contain spaces.
	typEnd := strings.IndexAny(rest, " \t")
	if open := strings.IndexByte(rest, '('); open >= 0 && (typEnd < 0 || open < typEnd) {
		closing := strings.IndexByte(rest, ')')
		if closing < 0 {
			return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q: enum is missing ')'", name)}
		}
		typEnd = closing + 1
		if typEnd < len(rest) && rest[typEnd] == '?' {
			typEnd++
		}
	}
	if typEnd < 0 {
		typEnd = len(rest)
	}
	typ, desc := rest[:typEnd], strings.TrimSpace(rest[typEnd:])
	if name == "" || typ == "" {
		return nil, &Error{Offset: off, Msg: "expected .param <name> <type> [description]"}
	}
	p := &Param{Name: name, Description: desc, Offset: off}
	for _, part := range strings.Split(p.Name, ".") {
		if !identRE.MatchString(part) {
			return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter name %q must be identifiers separated by '.'", p.Name)}
		}
	}
	if strings.HasSuffix(typ, "?") {
		p.Optional = true
		typ = strings.TrimSuffix(typ, "?")
	}
	t, err := parseType(typ)
	if err != nil {
		return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q: %v", name, err)}
	}
	p.Type = t
	return p, nil
}

func parseType(s string) (Type, error) {
	var t Type
	if strings.HasPrefix(s, "[]") {
		t.Array = true
		s = s[2:]
	}
	if inner, ok := strings.CutPrefix(s, "enum("); ok && strings.HasSuffix(inner, ")") {
		t.Kind = Enum
		for _, v := range strings.Split(strings.TrimSuffix(inner, ")"), ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				return t, fmt.Errorf("enum %q has an empty value", s)
			}
			for _, prev := range t.Enum {
				if prev == v {
					return t, fmt.Errorf("enum %q repeats %q", s, v)
				}
			}
			t.Enum = append(t.Enum, v)
		}
		return t, nil
	}
	switch s {
	case "string":
		t.Kind = String
	case "int", "integer":
		t.Kind = Int
	case "float", "float64", "number":
		t.Kind = Float
	case "bool", "boolean":
		t.Kind = Bool
	case "object":
		t.Kind = Object
	case "any":
		t.Kind = Any
	default:
		return t, fmt.Errorf("unknown parameter type %q; want string, int, float, bool, enum(a,b), object, any, or [] of one", s)
	}
	return t, nil
}
