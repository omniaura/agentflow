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
	"slices"
	"strconv"
	"strings"
)

// SectionDirectives are the directives that start a declaration section.
var SectionDirectives = []string{".tool", ".route", ".output", ".agent"}

// ToolDirectives are the directives allowed inside a tool section.
var ToolDirectives = []string{".param", ".maxbytes", ".auth", ".sql"}

// OutputDirectives are the directives allowed inside an output section.
var OutputDirectives = []string{".field", ".maxbytes"}

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
	// declared is false for an object implied by its properties' paths.
	declared bool
}

// Tool is a parsed .tool section.
type Tool struct {
	Name        string
	Description string
	MaxBytes    int
	Params      []*Param
	// Start and End delimit the section in the source.
	Start, End int
	// Output marks an .output section.
	Output bool
	// SQL is the query that implements the tool, written after a .sql
	// line, and SQLKind its sqlc command; both are empty for a tool you
	// implement in Go. SQLOffset is the .sql line's byte offset.
	SQL, SQLKind string
	SQLOffset    int
	// Auth names the query arguments that come from the caller, not from
	// the model.
	Auth []string
	// Warnings flag likely mistakes that still parse, such as a misspelled
	// type read as the start of a description.
	Warnings []*Error
}

// File holds the declaration sections of one .af file.
type File struct {
	Tools  []*Tool
	Routes []*Route
	// Outputs are .output sections: structured outputs, parsed like tools
	// with .field for .param.
	Outputs []*Tool
	Agents  []*Agent
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
		if hasDirective(src[i:], ".agent") {
			a, err := ParseAgent(src, i, end)
			if err != nil {
				return f, err
			}
			for _, prev := range f.Agents {
				if prev.Name == a.Name {
					return f, &Error{Offset: i, Msg: fmt.Sprintf("agent %q is declared twice", a.Name)}
				}
			}
			f.Agents = append(f.Agents, a)
			i = end
			continue
		}
		if hasDirective(src[i:], ".route") {
			r, err := ParseRoute(src, i, end)
			if err != nil {
				return f, err
			}
			for _, prev := range f.Routes {
				if prev.Method == r.Method && prev.Path == r.Path {
					return f, &Error{Offset: i, Msg: fmt.Sprintf("route %s %s is declared twice", r.Method, r.Path)}
				}
				if r.SQL != "" && prev.SQL != "" && prev.Query == r.Query {
					return f, &Error{Offset: r.SQLOffset, Msg: fmt.Sprintf("query %s is written twice; write it once and name it from both routes", r.Query)}
				}
			}
			f.Routes = append(f.Routes, r)
			i = end
			continue
		}
		t, err := ParseTool(src, i, end)
		if err != nil {
			return f, err
		}
		list, what := &f.Tools, "tool"
		if t.Output {
			list, what = &f.Outputs, "output"
		}
		for _, prev := range *list {
			if prev.Name == t.Name {
				return f, &Error{Offset: i, Msg: fmt.Sprintf("%s %q is declared twice", what, t.Name)}
			}
		}
		*list = append(*list, t)
		i = end
	}
	return f, nil
}

// ParseSection parses the declaration section src[start:end] and returns a
// *Tool, a *Route or an *Agent.
func ParseSection(src []byte, start, end int) (any, error) {
	if hasDirective(src[start:], ".agent") {
		return ParseAgent(src, start, end)
	}
	if hasDirective(src[start:], ".route") {
		return ParseRoute(src, start, end)
	}
	return ParseTool(src, start, end)
}

var (
	toolNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	identRE    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// ParseTool parses the tool or output section src[start:end], whose first
// line is its .tool or .output line.
func ParseTool(src []byte, start, end int) (*Tool, error) {
	t := &Tool{Start: start, End: end, Output: hasDirective(src[start:], ".output")}
	header, i := line(src, start, end)
	what, directive, param := "tool", ".tool", ".param"
	if t.Output {
		what, directive, param = "output", ".output", ".field"
	}
	name := strings.TrimSpace(strings.TrimPrefix(header, directive))
	if !toolNameRE.MatchString(name) {
		return nil, &Error{Offset: start, Msg: fmt.Sprintf("%s name %q must be 1-64 letters, digits, '_' or '-'", what, name)}
	}
	t.Name = name
	params := map[string]*Param{}
	var desc []string
	for i < end {
		off := i
		var l string
		l, i = line(src, i, end)
		switch {
		case hasDirective([]byte(l), param):
			p, err := parseParam(strings.TrimSpace(strings.TrimPrefix(l, param)), off)
			if err != nil {
				return nil, err
			}
			if err := t.addParam(params, p); err != nil {
				return nil, err
			}
			if w, _, _ := strings.Cut(p.Description, " "); p.Type.Kind == String {
				if typ, ok := misspelledType(w); ok {
					t.Warnings = append(t.Warnings, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q: %q starts the description; did you mean type %s?", p.Name, w, typ)})
				}
			}
		case hasDirective([]byte(l), ".maxbytes"):
			v := strings.TrimSpace(strings.TrimPrefix(l, ".maxbytes"))
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".maxbytes needs a positive integer, got %q", v)}
			}
			t.MaxBytes = n
		case !t.Output && hasDirective([]byte(l), ".sql"):
			// The rest of the section is the query.
			kind := strings.TrimSpace(strings.TrimPrefix(l, ".sql"))
			if !slices.Contains(SQLKinds, kind) {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".sql needs the query's sqlc command, one of %s; got %q", strings.Join(SQLKinds, ", "), kind)}
			}
			t.SQLKind, t.SQLOffset = kind, off
			sql, err := sqlBody(src, i, end, ToolDirectives)
			if err != nil {
				return nil, err
			}
			t.SQL = sql
			i = end
		case !t.Output && hasDirective([]byte(l), ".auth"):
			for _, name := range strings.FieldsFunc(strings.TrimPrefix(l, ".auth"), func(r rune) bool { return r == ' ' || r == '\t' || r == ',' }) {
				if !identRE.MatchString(name) {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf(".auth name %q must be an identifier", name)}
				}
				if !slices.Contains(t.Auth, name) {
					t.Auth = append(t.Auth, name)
				}
			}
		default:
			desc = append(desc, l)
		}
	}
	if len(t.Auth) > 0 && t.SQL == "" {
		return nil, &Error{Offset: start, Msg: ".auth names arguments of the tool's query: add the query after a .sql line"}
	}
	for _, name := range t.Auth {
		for _, p := range t.Params {
			if p.Name == name {
				return nil, &Error{Offset: p.Offset, Msg: fmt.Sprintf("%s comes from the caller (.auth), so the model cannot set it", name)}
			}
		}
	}
	t.Description = strings.TrimRight(trimLeadingBlankLines(strings.Join(desc, "\n")), " \t\n")
	for _, p := range t.Params {
		inferOptional(p)
	}
	if t.Output {
		if err := checkStrict(t.Params, ""); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// addParam inserts p, whose Name is still its full path, into the tool.
// Like variables in prompts, objects need no declaration of their own: a
// path such as filter.since makes filter an object, and items[].sku makes
// items an array of objects. Declaring the object too, before or after its
// properties, only adds its description or makes it optional.
func (t *Tool) addParam(params map[string]*Param, p *Param) error {
	segs := strings.Split(p.Name, ".")
	siblings := &t.Params
	path := ""
	for i, seg := range segs {
		array := strings.HasSuffix(seg, "[]")
		seg = strings.TrimSuffix(seg, "[]")
		path = join(path, seg)
		last := i == len(segs)-1
		node, exists := params[path]
		if last && exists && node.declared {
			return &Error{Offset: p.Offset, Msg: fmt.Sprintf("parameter %q is declared twice", path)}
		}
		if last && array {
			return &Error{Offset: p.Offset, Msg: fmt.Sprintf("parameter %q: write %s []object instead of %s[]", path, path, path)}
		}
		if !exists {
			node = &Param{Name: seg, Type: Type{Kind: Object, Array: array}, Offset: p.Offset}
			params[path] = node
			*siblings = append(*siblings, node)
		}
		if !last {
			if node.Type.Kind != Object {
				return &Error{Offset: p.Offset, Msg: fmt.Sprintf("parameter %q is inside %q, which is %s, not an object", p.Name, path, node.Type)}
			}
			if array {
				node.Type.Array = true
			}
			siblings = &node.Children
			continue
		}
		if exists {
			// The object was implied by its properties; keep them.
			if p.Type.Kind != Object {
				return &Error{Offset: p.Offset, Msg: fmt.Sprintf("parameter %q has properties, so it must be object or []object, not %s", path, p.Type)}
			}
			if node.Type.Array && !p.Type.Array {
				return &Error{Offset: p.Offset, Msg: fmt.Sprintf("parameter %q is used as %s[], so declare it []object", path, path)}
			}
			node.Type, node.Optional, node.Description, node.Offset = p.Type, p.Optional, p.Description, p.Offset
			node.declared = true
			return nil
		}
		node.Type, node.Optional, node.Description = p.Type, p.Optional, p.Description
		node.declared = true
	}
	return nil
}

// checkStrict rejects what strict structured outputs cannot express: values
// of any type and objects without declared fields.
func checkStrict(params []*Param, prefix string) error {
	for _, p := range params {
		path := join(prefix, p.Name)
		switch {
		case p.Type.Kind == Any:
			return &Error{Offset: p.Offset, Msg: fmt.Sprintf("field %q: an .output field needs a type; strict structured outputs cannot hold any", path)}
		case p.Type.Kind == Object && len(p.Children) == 0:
			return &Error{Offset: p.Offset, Msg: fmt.Sprintf("field %q: declare the object's fields (%s.name ...); strict structured outputs reject free-form objects", path, path)}
		}
		if err := checkStrict(p.Children, path); err != nil {
			return err
		}
	}
	return nil
}

// inferOptional makes an undeclared object optional when none of its
// properties is required.
func inferOptional(p *Param) {
	if p.Type.Kind != Object || len(p.Children) == 0 {
		return
	}
	required := false
	for _, c := range p.Children {
		inferOptional(c)
		required = required || !c.Optional
	}
	if !p.declared {
		p.Optional = !required
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func (t Type) String() string {
	s := ""
	switch t.Kind {
	case String:
		s = "string"
	case Int:
		s = "int"
	case Float:
		s = "float"
	case Bool:
		s = "bool"
	case Enum:
		s = "enum(" + strings.Join(t.Enum, ", ") + ")"
	case Object:
		s = "object"
	case Any:
		s = "any"
	}
	if t.Array {
		return "[]" + s
	}
	return s
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

// parseParam parses the text after .param: a name, an optional type, and a
// description. The type defaults to string, as it does for <!var>, and a
// trailing ? on the name or the type makes the parameter optional.
func parseParam(s string, off int) (*Param, error) {
	name, rest := s, ""
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		name, rest = s[:i], strings.TrimSpace(s[i:])
	}
	if name == "" {
		return nil, &Error{Offset: off, Msg: "expected .param <name> [type] [description]"}
	}
	p := &Param{Offset: off, Type: Type{Kind: String}}
	if strings.HasSuffix(name, "?") {
		p.Optional = true
		name = strings.TrimSuffix(name, "?")
	}
	p.Name = name
	for _, part := range strings.Split(strings.ReplaceAll(name, "[]", ""), ".") {
		if !identRE.MatchString(part) {
			return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter name %q must be identifiers separated by '.'", name)}
		}
	}
	if strings.Contains(strings.ReplaceAll(name, "[].", "."), "[]") && !strings.HasSuffix(name, "[]") {
		return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter name %q: [] goes right before a '.'", name)}
	}
	// The type runs to the first space, or to the closing parenthesis of an
	// enum, so enum(a, b) may contain spaces. A first word that is not a type
	// starts the description.
	typEnd := strings.IndexAny(rest, " \t")
	if typEnd < 0 {
		typEnd = len(rest)
	}
	if open := strings.IndexByte(rest, '('); open >= 0 && open < typEnd && isEnumPrefix(rest[:open]) {
		closing := strings.IndexByte(rest, ')')
		if closing < 0 {
			return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q: enum is missing ')'", name)}
		}
		typEnd = closing + 1
		if typEnd < len(rest) && rest[typEnd] == '?' {
			typEnd++
		}
	}
	typ := rest[:typEnd]
	optional := strings.HasSuffix(typ, "?")
	t, err := parseType(strings.TrimSuffix(typ, "?"))
	switch {
	case err == nil:
		p.Type = t
		p.Optional = p.Optional || optional
		rest = rest[typEnd:]
	case isEnumPrefix(strings.TrimSuffix(typ, "?")) || strings.HasPrefix(typ, "enum("):
		return nil, &Error{Offset: off, Msg: fmt.Sprintf("parameter %q: %v", name, err)}
	}
	p.Description = strings.TrimSpace(rest)
	return p, nil
}

var typeNames = []string{"string", "integer", "float", "float64", "number", "bool", "boolean", "object"}

// misspelledType reports whether w is one or two edits away from a type name.
// Short names such as int and any are left out: too many words are near them.
func misspelledType(w string) (string, bool) {
	w = strings.TrimPrefix(strings.TrimSuffix(w, "?"), "[]")
	// Descriptions start with a capital; types never do.
	if len(w) < 4 || strings.ToLower(w) != w {
		return "", false
	}
	for _, typ := range typeNames {
		if d := editDistance(w, typ); d > 0 && d <= 2 && len(typ) > 4 || d == 1 {
			return typ, true
		}
	}
	return "", false
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// IsType reports whether s, such as "[]int?" or "enum(a, b)", is a
// parameter type.
func IsType(s string) bool {
	_, err := parseType(strings.TrimSuffix(s, "?"))
	return err == nil
}

func isEnumPrefix(s string) bool {
	return s == "enum" || s == "[]enum"
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

// sqlBody returns the query that follows a .sql line: the rest of the
// section, src[start:end]. A section directive in it was meant to come
// before .sql, so it is an error rather than SQL.
func sqlBody(src []byte, start, end int, directives []string) (string, error) {
	for i := start; i < end; {
		off := i
		var l string
		l, i = line(src, i, end)
		for _, d := range directives {
			switch {
			case !strings.HasPrefix(l, d), len(l) > len(d) && isIdentByte(l[len(d)]):
				// Not this directive: SQL such as the .authentic of a
				// qualified name split across lines.
			case !hasDirective([]byte(l), d):
				// Such as .auth, or .sql:many: no SQL starts like this.
				return "", &Error{Offset: off, Msg: fmt.Sprintf("malformed %s directive in the query", d)}
			case d == ".sql":
				return "", &Error{Offset: off, Msg: "a section has one query: only one .sql line"}
			default:
				return "", &Error{Offset: off, Msg: fmt.Sprintf("%s must come before .sql: the lines after .sql are the query", d)}
			}
		}
	}
	sql := strings.TrimSpace(string(src[start:end]))
	if sql == "" {
		return "", &Error{Offset: start, Msg: "write the query on the lines after .sql"}
	}
	return sql, nil
}

func isIdentByte(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
