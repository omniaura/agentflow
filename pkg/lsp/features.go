package lsp

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/format"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"github.com/omniaura/agentflow/pkg/lint"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
	"github.com/peyton-spencer/caseconv/bytcase"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (r Range) protocol() protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: protocol.UInteger(r.Start.Line), Character: protocol.UInteger(r.Start.Character)},
		End:   protocol.Position{Line: protocol.UInteger(r.End.Line), Character: protocol.UInteger(r.End.Character)},
	}
}

func (d *Document) rangeOf(start, end int) Range {
	return Range{Start: d.byteOffsetToPosition(start), End: d.byteOffsetToPosition(end)}
}

// Diagnostics returns the parse errors and af lint diagnostics of the
// document, with their codes.
func (d *Document) Diagnostics() []protocol.Diagnostic {
	out := []protocol.Diagnostic{}
	for _, e := range d.Errors {
		if e.Code == "AF011" {
			continue // af lint reports declaration errors too.
		}
		out = append(out, diagnostic(e.Range, protocol.DiagnosticSeverityError, e.Code, e.Message))
	}
	ds, err := lint.Lint(extractFilename(d.URI), d.Content)
	if err != nil {
		return out
	}
	starts := lineStartOffsets(d.Content)
	for _, ld := range ds {
		if ld.Line < 1 || ld.Line > len(starts) {
			continue
		}
		start := min(len(d.Content), starts[ld.Line-1]+ld.Column-1)
		// Underline the word at the diagnostic, or the rest of its line.
		end := start
		for end < len(d.Content) && d.Content[end] != '\n' && d.Content[end] != ' ' {
			end++
		}
		if end == start {
			end = lineEnd(d.Content, start, len(d.Content))
		}
		severity := protocol.DiagnosticSeverityError
		if ld.Severity == lint.SeverityWarning {
			severity = protocol.DiagnosticSeverityWarning
		}
		out = append(out, diagnostic(d.rangeOf(start, end), severity, ld.Code, ld.Message))
	}
	return out
}

func diagnostic(r Range, severity protocol.DiagnosticSeverity, code, msg string) protocol.Diagnostic {
	src := "agentflow"
	dg := protocol.Diagnostic{Range: r.protocol(), Severity: &severity, Source: &src, Message: msg}
	if code != "" {
		dg.Code = &protocol.IntegerOrString{Value: code}
	}
	return dg
}

// lineEnd returns the offset of the first newline in src[start:end], or end.
func lineEnd(src []byte, start, end int) int {
	if i := bytes.IndexByte(src[start:end], '\n'); i >= 0 {
		return start + i
	}
	return end
}

func lineStartOffsets(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// FormatEdits returns the edits that make the document af fmt formatted:
// none when it already is.
func (d *Document) FormatEdits() ([]protocol.TextEdit, error) {
	formatted, err := format.Format(extractFilename(d.URI), d.Content)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(formatted, d.Content) {
		return []protocol.TextEdit{}, nil
	}
	return []protocol.TextEdit{{Range: d.rangeOf(0, len(d.Content)).protocol(), NewText: string(formatted)}}, nil
}

// loop is a <*path []T as alias> ... </path> block.
type loop struct {
	alias                string
	aliasStart, aliasEnd int
	// The alias is in scope from the loop's tag to its closing tag.
	start, end int
}

var aliasRE = regexp.MustCompile(`\s+as\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`)

func (d *Document) loops() []loop {
	var out []loop
	toks := d.Tokens
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].Kind != kind.DirectiveLoop || toks[i+1].Kind != kind.VarName {
			continue
		}
		src := string(toks[i+1].Get(d.Content))
		closeIdx := -1
		for j := i + 1; j < len(toks); j++ {
			if toks[j].Kind == kind.CloseBracket {
				closeIdx = j
				break
			}
		}
		if closeIdx < 0 {
			continue
		}
		tagText := d.Content[toks[i+1].End:toks[closeIdx].Start]
		m := aliasRE.FindSubmatchIndex(tagText)
		if m == nil {
			continue
		}
		l := loop{
			alias:      string(tagText[m[2]:m[3]]),
			aliasStart: toks[i+1].End + m[2],
			aliasEnd:   toks[i+1].End + m[3],
			start:      toks[i].Start,
			end:        len(d.Content),
		}
		for j := closeIdx; j+1 < len(toks); j++ {
			if toks[j].Kind == kind.DirectiveEnd && toks[j+1].Kind == kind.VarName && string(toks[j+1].Get(d.Content)) == src {
				l.end = toks[j+1].End
				break
			}
		}
		out = append(out, l)
	}
	return out
}

// aliasAt returns the innermost loop whose alias name is in scope at off.
func aliasAt(loops []loop, name string, off int) *loop {
	var best *loop
	for i := range loops {
		l := &loops[i]
		if l.alias == name && off >= l.start && off <= l.end && (best == nil || l.start > best.start) {
			best = l
		}
	}
	return best
}

// segmentAt returns the index and byte range of the dotted path segment of
// tok that contains off.
func segmentAt(src []byte, tok token.T, off int) (int, int, int) {
	start := tok.Start
	for i, seg := range strings.Split(string(tok.Get(src)), ".") {
		end := start + len(seg)
		if off <= end {
			return i, start, end
		}
		start = end + 1
	}
	return -1, 0, 0
}

func (d *Document) tokenAt(off int, kinds ...kind.Kind) (token.T, bool) {
	for _, t := range d.Tokens {
		if off >= t.Start && off <= t.End && slices.Contains(kinds, t.Kind) {
			return t, true
		}
	}
	return token.T{}, false
}

func (d *Document) path() string {
	u, err := url.Parse(d.URI)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// Definition returns where the symbol at pos is defined:
//   - a variable: its first typed use, or its first use; a loop alias: the
//     loop that binds it
//   - a .title or .tool name: the Go type or variable generated for it
//   - a .route query: the sqlc method that runs it
func (d *Document) Definition(pos Position) []protocol.Location {
	off := d.positionToOffset(pos)
	if tok, ok := d.tokenAt(off, kind.VarName); ok {
		return d.variableDefinition(tok, off)
	}
	if tok, ok := d.tokenAt(off, kind.TitleText); ok {
		return d.goDefinition(`type ` + string(bytcase.ToCamel(tok.Get(d.Content))) + ` struct`)
	}
	if tok, ok := d.tokenAt(off, kind.Section); ok {
		lineEnd := lineEnd(d.Content, tok.Start, tok.End)
		if off > lineEnd {
			return nil
		}
		sec, err := decl.ParseSection(d.Content, tok.Start, tok.End)
		if err != nil {
			return nil
		}
		switch s := sec.(type) {
		case *decl.Tool:
			name := string(bytcase.ToCamel([]byte(strings.ReplaceAll(s.Name, "-", "_"))))
			if s.Output {
				return d.goDefinition(`var ` + name + `Schema = `)
			}
			return d.goDefinition(`var ` + name + `Tool = `)
		case *decl.Route:
			if off < s.QueryOffset {
				return nil
			}
			return d.queryDefinition(s.Query)
		case *decl.Agent:
			return d.goDefinition(`type ` + string(bytcase.ToCamel([]byte(strings.ReplaceAll(s.Name, "-", "_")))) + `Agent struct`)
		}
	}
	return nil
}

func (d *Document) variableDefinition(tok token.T, off int) []protocol.Location {
	seg, segStart, segEnd := segmentAt(d.Content, tok, off)
	if seg < 0 {
		return nil
	}
	segs := strings.Split(string(tok.Get(d.Content)), ".")
	if seg == 0 {
		if l := aliasAt(d.loops(), segs[0], tok.Start); l != nil {
			return []protocol.Location{{URI: d.URI, Range: d.rangeOf(l.aliasStart, l.aliasEnd).protocol()}}
		}
	}
	prefix := strings.Join(segs[:seg+1], ".")
	var first *token.T
	for i, t := range d.Tokens {
		if t.Kind != kind.VarName {
			continue
		}
		p := string(t.Get(d.Content))
		if p != prefix && !strings.HasPrefix(p, prefix+".") {
			continue
		}
		if first == nil {
			first = &d.Tokens[i]
		}
		if p == prefix && i+2 < len(d.Tokens) && d.Tokens[i+1].Kind == kind.Whitespace && d.Tokens[i+2].Kind == kind.TypeName {
			first = &d.Tokens[i]
			break
		}
	}
	if first == nil {
		first = &tok
	}
	start := first.Start + (segStart - tok.Start)
	return []protocol.Location{{URI: d.URI, Range: d.rangeOf(start, start+(segEnd-segStart)).protocol()}}
}

// goDefinition finds the line starting with prefix in the generated
// _af.go next to the document.
func (d *Document) goDefinition(prefix string) []protocol.Location {
	p := d.path()
	if p == "" {
		return nil
	}
	gen := strings.TrimSuffix(p, ".af") + "_af.go"
	b, err := os.ReadFile(gen)
	if err != nil {
		return nil
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, prefix) {
			name := strings.Fields(prefix)[1]
			col := strings.Index(line, name)
			return []protocol.Location{{URI: fileURI(gen), Range: Range{Start: Position{Line: i, Character: col}, End: Position{Line: i, Character: col + len(name)}}.protocol()}}
		}
	}
	return nil
}

func (d *Document) queryDefinition(query string) []protocol.Location {
	p := d.path()
	if p == "" {
		return nil
	}
	repo, err := sqlcgo.Find(filepath.Dir(p))
	if err != nil {
		return nil
	}
	_, q, err := repo.Lookup(query)
	if err != nil || q.File == "" {
		return nil
	}
	line := max(0, q.Line-1)
	return []protocol.Location{{URI: fileURI(q.File), Range: Range{Start: Position{Line: line}, End: Position{Line: line}}.protocol()}}
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ErrNoSymbol means there is nothing to rename at a position.
var ErrNoSymbol = errors.New("no variable or loop alias here")

// Rename renames the path segment of the variable at pos, everywhere it
// appears with the same prefix, or the loop alias at pos within its loop.
func (d *Document) Rename(pos Position, newName string) (*protocol.WorkspaceEdit, error) {
	if !identRE.MatchString(newName) {
		return nil, fmt.Errorf("%q is not an identifier", newName)
	}
	off := d.positionToOffset(pos)
	loops := d.loops()
	var target *loop
	var prefix []string
	seg := 0
	if tok, ok := d.tokenAt(off, kind.VarName); ok {
		var segStart int
		seg, segStart, _ = segmentAt(d.Content, tok, off)
		segs := strings.Split(string(tok.Get(d.Content)), ".")
		if seg < 0 {
			return nil, ErrNoSymbol
		}
		_ = segStart
		if seg == 0 {
			target = aliasAt(loops, segs[0], tok.Start)
		}
		prefix = segs[:seg+1]
	} else {
		for i := range loops {
			if off >= loops[i].aliasStart && off <= loops[i].aliasEnd {
				target = &loops[i]
			}
		}
		if target == nil {
			return nil, ErrNoSymbol
		}
	}

	var edits []protocol.TextEdit
	replace := func(start, end int) {
		edits = append(edits, protocol.TextEdit{Range: d.rangeOf(start, end).protocol(), NewText: newName})
	}
	if target != nil {
		replace(target.aliasStart, target.aliasEnd)
	}
	for _, t := range d.Tokens {
		if t.Kind != kind.VarName {
			continue
		}
		segs := strings.Split(string(t.Get(d.Content)), ".")
		bound := aliasAt(loops, segs[0], t.Start)
		if target != nil {
			if bound != target {
				continue
			}
			replace(t.Start, t.Start+len(segs[0]))
			continue
		}
		// A path whose head is a loop alias names the alias, not the
		// variable being renamed.
		if bound != nil || len(segs) <= seg || !slices.Equal(segs[:seg+1], prefix) {
			continue
		}
		start := t.Start + len(strings.Join(segs[:seg], "."))
		if seg > 0 {
			start++
		}
		replace(start, start+len(segs[seg]))
	}
	return &protocol.WorkspaceEdit{Changes: map[protocol.DocumentUri][]protocol.TextEdit{d.URI: edits}}, nil
}

// SectionSymbols returns a symbol per .tool and .route section.
func (d *Document) SectionSymbols() []protocol.DocumentSymbol {
	var out []protocol.DocumentSymbol
	for _, t := range d.Tokens {
		if t.Kind != kind.Section {
			continue
		}
		sec, err := decl.ParseSection(d.Content, t.Start, t.End)
		if err != nil {
			continue
		}
		header := lineEnd(d.Content, t.Start, t.End)
		sym := protocol.DocumentSymbol{
			Range:          d.rangeOf(t.Start, len(bytes.TrimRight(d.Content[:t.End], " \t\r\n"))).protocol(),
			SelectionRange: d.rangeOf(t.Start, header).protocol(),
		}
		switch s := sec.(type) {
		case *decl.Tool:
			sym.Name, sym.Kind = s.Name, protocol.SymbolKindFunction
			detail := "tool"
			if s.Output {
				sym.Kind, detail = protocol.SymbolKindStruct, "output"
			}
			sym.Detail = &detail
			for _, p := range s.Params {
				sym.Children = append(sym.Children, paramSymbol(d, p))
			}
		case *decl.Route:
			sym.Name, sym.Kind = s.Method+" "+s.Path, protocol.SymbolKindMethod
			detail := "route → " + s.Query
			sym.Detail = &detail
		case *decl.Agent:
			sym.Name, sym.Kind = s.Name, protocol.SymbolKindClass
			detail := "agent"
			if s.Tools != nil {
				detail += " → " + strings.Join(s.Tools, ", ")
			}
			sym.Detail = &detail
		}
		out = append(out, sym)
	}
	return out
}

func paramSymbol(d *Document, p *decl.Param) protocol.DocumentSymbol {
	lineEnd := lineEnd(d.Content, p.Offset, len(d.Content))
	detail := p.Type.String()
	if p.Optional {
		detail += "?"
	}
	sym := protocol.DocumentSymbol{
		Name: p.Name, Detail: &detail, Kind: protocol.SymbolKindField,
		Range: d.rangeOf(p.Offset, lineEnd).protocol(), SelectionRange: d.rangeOf(p.Offset, lineEnd).protocol(),
	}
	for _, c := range p.Children {
		sym.Children = append(sym.Children, paramSymbol(d, c))
	}
	return sym
}

// sectionDirectiveAt returns the directive that opened the declaration
// section containing off, such as ".tool", or "" outside sections. It reads
// the header, so it works while the section does not parse yet.
func (d *Document) sectionDirectiveAt(off int) string {
	for _, t := range d.Tokens {
		if t.Kind == kind.Section && off >= t.Start && off <= t.End {
			for _, dir := range decl.SectionDirectives {
				if bytes.HasPrefix(d.Content[t.Start:], []byte(dir)) {
					return dir
				}
			}
		}
	}
	return ""
}

var paramTypeRE = regexp.MustCompile(`^\.(?:param|field)\s+[A-Za-z_][A-Za-z0-9_.\[\]]*\??\s+\S*$`)

// directiveCompletions suggests directives at the start of a line, and
// types after a .param name.
func (d *Document) directiveCompletions(pos Position, beforeCursor []byte) []CompletionItem {
	var items []CompletionItem
	if sec := d.sectionDirectiveAt(d.positionToOffset(pos)); paramTypeRE.Match(beforeCursor) && (sec == ".tool" || sec == ".output") {
		types := []string{"string", "int", "float", "bool", "enum()", "object", "any", "[]string", "[]int", "[]object"}
		if sec == ".output" {
			// Strict outputs have no free-form values.
			types = slices.DeleteFunc(types, func(t string) bool { return t == "any" || t == "object" || t == "[]object" })
		}
		for _, typ := range types {
			items = append(items, CompletionItem{Label: typ, Kind: protocol.CompletionItemKindTypeParameter, Detail: "parameter type"})
		}
		return items
	}
	trimmed := bytes.TrimSpace(beforeCursor)
	if len(beforeCursor) != len(trimmed) || (len(trimmed) > 0 && trimmed[0] != '.') || bytes.ContainsAny(trimmed, " \t") {
		return nil
	}
	add := func(label, detail string) {
		items = append(items, CompletionItem{Label: label, Kind: protocol.CompletionItemKindKeyword, Detail: detail, InsertText: label + " "})
	}
	// The line start is inside the section the line would continue.
	lineStart := d.positionToOffset(Position{Line: pos.Line})
	switch d.sectionDirectiveAt(max(0, lineStart-1)) {
	case ".tool":
		add(".param", "tool parameter: .param name [type] [description]")
		add(".maxbytes", "cap the tool's arguments")
	case ".agent":
		add(".system", "the system prompt's title (default: the file's only prompt)")
		add(".tools", "the tools it may call (default: every tool in the file)")
		add(".maxturns", "bound the model calls of a run")
	case ".output":
		add(".field", "output field: .field name [type] [description]")
		add(".maxbytes", "cap the output's size")
	case ".route":
		add(".auth", "arguments read from the caller")
		add(".public", "serve without authentication")
		add(".status", "success status code")
		add(".maxbytes", "cap the request body")
		add(".sql", "write the query here: .sql :one|:many|:exec|:execrows, then the SQL")
	}
	add(".title", "start a prompt")
	add(".tool", "declare an LLM tool: .tool name")
	add(".route", "serve a sqlc query: .route METHOD /path Query")
	add(".output", "declare a structured output: .output name")
	add(".agent", "declare a tool-calling agent: .agent name")
	return items
}

// SectionHover describes the .tool or .route header at pos in Markdown:
// a tool's parameters, or how a route binds its query's arguments.
func (d *Document) SectionHover(pos Position) string {
	off := d.positionToOffset(pos)
	tok, ok := d.tokenAt(off, kind.Section)
	if !ok {
		return ""
	}
	header := lineEnd(d.Content, tok.Start, tok.End)
	if off > header {
		return ""
	}
	sec, err := decl.ParseSection(d.Content, tok.Start, tok.End)
	if err != nil {
		return ""
	}
	var b strings.Builder
	switch s := sec.(type) {
	case *decl.Tool:
		what := "tool"
		if s.Output {
			what = "output"
		}
		fmt.Fprintf(&b, "**%s %s**\n\n", what, s.Name)
		var walk func(indent string, ps []*decl.Param)
		walk = func(indent string, ps []*decl.Param) {
			for _, p := range ps {
				opt := ""
				if p.Optional {
					opt = "?"
				}
				fmt.Fprintf(&b, "%s- `%s%s` %s", indent, p.Name, opt, p.Type)
				if p.Description != "" {
					b.WriteString(": " + p.Description)
				}
				b.WriteString("\n")
				walk(indent+"  ", p.Children)
			}
		}
		walk("", s.Params)
		if len(s.Params) == 0 {
			b.WriteString("Nothing declared.\n")
		}
	case *decl.Route:
		fmt.Fprintf(&b, "**%s %s** → `%s`\n\n", s.Method, s.Path, s.Query)
		p := d.path()
		if p == "" {
			break
		}
		repo, err := sqlcgo.Find(filepath.Dir(p))
		if err != nil {
			fmt.Fprintf(&b, "%v\n", err)
			break
		}
		pkg, q, err := repo.Lookup(s.Query)
		if err != nil {
			fmt.Fprintf(&b, "%v\n", err)
			break
		}
		bindings, err := gogen.Bind(s, pkg, q)
		if err != nil {
			fmt.Fprintf(&b, "%v\n", err)
			break
		}
		fmt.Fprintf(&b, "`%s.%s` is `%s`.\n\n", pkg.Name, q.Name, q.Kind)
		for _, bd := range bindings {
			from := map[string]string{"path": "path", "auth": "caller", "query": "query string", "body": "JSON body"}[bd.Source]
			fmt.Fprintf(&b, "- `%s` %s ← %s\n", bd.Arg.JSON, bd.Arg.Type.Expr, from)
		}
		if !s.Public {
			b.WriteString("\nRequires authentication.\n")
		}
	}
	return b.String()
}
