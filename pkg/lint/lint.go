/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package lint

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
	"github.com/peyton-spencer/caseconv/bytcase"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Diagnostic struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
}

type frame struct {
	path    string
	tok     token.T
	sawElse bool
	loop    bool
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var symbolicConditionalPattern = regexp.MustCompile(`<\?[^\n>]*(>=|<=|==|!=)`)

func Lint(name string, src []byte) ([]Diagnostic, error) {
	tokens, err := token.Tokenize(src)
	if err != nil {
		return nil, err
	}

	l := &linter{name: name, src: src, lineStarts: lineStarts(src)}
	l.checkRawSymbolicOperators()
	l.check(tokens)
	return l.diagnostics, nil
}

func (l *linter) checkRawSymbolicOperators() {
	for _, loc := range symbolicConditionalPattern.FindAllIndex(l.src, -1) {
		l.add(token.T{Start: loc[0], End: loc[1]}, SeverityError, "AF009", "invalid symbolic comparison operator")
	}
}

type linter struct {
	name        string
	src         []byte
	lineStarts  []int
	diagnostics []Diagnostic
	stack       []frame
	types       map[string]string
	titles      map[string]token.T
	sections    map[string]bool
	// titleTexts, tools and agents are checked against each other once
	// the whole file is read.
	titleTexts []string
	tools      []string
	toolDecls  []*decl.Tool
	outputs    []string
	agents     []*decl.Agent
	hasTitle   bool
	hasContent bool
}

func (l *linter) check(tokens token.Slice) {
	l.types = make(map[string]string)
	l.titles = make(map[string]token.T)

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch tok.Kind {
		case kind.TitleDirective:
			i = l.checkTitle(tokens, i)
		case kind.Text:
			if strings.TrimSpace(string(tok.Get(l.src))) != "" {
				l.hasContent = true
			}
		case kind.OpenBracket:
			i = l.checkDirective(tokens, i)
		case kind.Section:
			l.checkSection(tok)
		}
	}

	for _, f := range l.stack {
		if f.loop {
			l.add(f.tok, SeverityError, "AF003", fmt.Sprintf("unclosed loop <*%s>", f.path))
		} else {
			l.add(f.tok, SeverityError, "AF003", fmt.Sprintf("unclosed conditional <?%s>", f.path))
		}
	}
	if !l.hasTitle && l.hasContent {
		l.add(token.T{Start: 0, End: 0}, SeverityError, "AF001", "file has content but no .title section")
	}
	for _, a := range l.agents {
		l.checkAgent(a)
	}
}

// checkAgent reports an agent whose system prompt, tools or output are not
// in the file.
func (l *linter) checkAgent(a *decl.Agent) {
	quoted := make([]string, len(l.titleTexts))
	for i, t := range l.titleTexts {
		quoted[i] = strconv.Quote(t)
	}
	switch {
	case a.System != "" && !slices.Contains(l.titleTexts, a.System):
		off := l.directiveOffset(a, ".system")
		l.add(token.T{Start: off, End: off}, SeverityError, "AF011", fmt.Sprintf("agent %s: no prompt titled %q in this file (have %s)", a.Name, a.System, strings.Join(quoted, ", ")))
	case a.System == "" && len(l.titleTexts) > 1:
		l.add(token.T{Start: a.Start, End: a.Start}, SeverityError, "AF011", fmt.Sprintf("agent %s: the file has %d prompts; name the system prompt with .system <title>", a.Name, len(l.titleTexts)))
	}
	if a.Returns != "" && !slices.Contains(l.outputs, a.Returns) {
		have := "it declares none"
		if len(l.outputs) > 0 {
			have = "have " + strings.Join(l.outputs, ", ")
		}
		off := l.directiveOffset(a, ".returns")
		l.add(token.T{Start: off, End: off}, SeverityError, "AF011", fmt.Sprintf("agent %s: no output %s in this file (%s)", a.Name, a.Returns, have))
	}
	if a.Tools == nil {
		return
	}
	// .tools may name tools of the other .af files of the package.
	siblings := ast.SiblingTools(l.name)
	for _, name := range a.Tools {
		if slices.Contains(l.tools, name) && !slices.ContainsFunc(siblings, func(s ast.SiblingTool) bool { return s.Tool.Name == name }) {
			continue
		}
		if _, err := ast.FindTool(l.toolDecls, siblings, name); err != nil {
			off := l.directiveOffset(a, ".tools")
			l.add(token.T{Start: off, End: off}, SeverityError, "AF011", fmt.Sprintf("agent %s: %v", a.Name, err))
		}
	}
}

// directiveOffset returns the offset of a directive line in an agent
// section, or the section's start.
func (l *linter) directiveOffset(a *decl.Agent, directive string) int {
	off := a.Start
	for off < a.End {
		end := bytes.IndexByte(l.src[off:a.End], '\n')
		if end < 0 {
			end = a.End - off
		}
		if line := l.src[off : off+end]; bytes.HasPrefix(line, []byte(directive)) && (len(line) == len(directive) || line[len(directive)] == ' ' || line[len(directive)] == '\t') {
			return off
		}
		off += end + 1
	}
	return a.Start
}

// checkSection reports declaration errors in a section such as .tool or
// .route.
func (l *linter) checkSection(tok token.T) {
	sec, err := decl.ParseSection(l.src, tok.Start, tok.End)
	if err != nil {
		off := tok.Start
		var de *decl.Error
		if errors.As(err, &de) {
			off = de.Offset
		}
		l.add(token.T{Start: off, End: off}, SeverityError, "AF011", err.Error())
		return
	}
	if l.sections == nil {
		l.sections = map[string]bool{}
	}
	key := ""
	switch s := sec.(type) {
	case *decl.Tool:
		for _, w := range s.Warnings {
			l.add(token.T{Start: w.Offset, End: w.Offset}, SeverityWarning, "AF012", w.Msg)
		}
		key = fmt.Sprintf("tool %q", s.Name)
		if s.Output {
			key = fmt.Sprintf("output %q", s.Name)
			l.outputs = append(l.outputs, s.Name)
		} else {
			l.tools = append(l.tools, s.Name)
			l.toolDecls = append(l.toolDecls, s)
		}
	case *decl.Route:
		key = "route " + s.Method + " " + s.Path
	case *decl.Agent:
		key = fmt.Sprintf("agent %q", s.Name)
		l.agents = append(l.agents, s)
		if s.ServePath != "" {
			// An agent's route shares the mux with .route sections.
			route := "route " + s.ServeMethod + " " + s.ServePath
			if l.sections[route] {
				off := l.directiveOffset(s, ".serve")
				l.add(token.T{Start: off, End: off}, SeverityError, "AF011", route+" is declared twice")
			}
			l.sections[route] = true
		}
	}
	if l.sections[key] {
		l.add(tok, SeverityError, "AF011", key+" is declared twice")
	}
	l.sections[key] = true
}

func (l *linter) checkTitle(tokens token.Slice, start int) int {
	l.hasTitle = true
	i := start + 1
	if i < len(tokens) && tokens[i].Kind == kind.Whitespace && !strings.Contains(string(tokens[i].Get(l.src)), "\n") {
		i++
	}

	if i >= len(tokens) || tokens[i].Kind != kind.TitleText || strings.TrimSpace(string(tokens[i].Get(l.src))) == "" {
		l.add(tokens[start], SeverityWarning, "AF103", "empty title")
		return start
	}

	title := strings.TrimSpace(string(tokens[i].Get(l.src)))
	l.titleTexts = append(l.titleTexts, title)
	generated := string(bytcase.ToCamel([]byte(title)))
	if first, ok := l.titles[generated]; ok {
		_ = first
		l.add(tokens[i], SeverityWarning, "AF102", fmt.Sprintf("duplicate title %q generates %s", title, generated))
	} else {
		l.titles[generated] = tokens[i]
	}
	return i
}

func (l *linter) checkDirective(tokens token.Slice, start int) int {
	if start+1 >= len(tokens) {
		return start
	}
	switch tokens[start+1].Kind {
	case kind.DirectiveLoop:
		return l.checkLoop(tokens, start)
	case kind.DirectiveVar:
		return l.checkVar(tokens, start)
	case kind.DirectiveCond:
		return l.checkCond(tokens, start)
	case kind.DirectiveEnd:
		return l.checkEnd(tokens, start)
	case kind.DirectiveElse:
		return l.checkElse(tokens, start)
	default:
		return start
	}
}

func (l *linter) checkVar(tokens token.Slice, start int) int {
	end, content := l.directiveContent(tokens, start)
	l.hasContent = true
	parts := strings.Fields(content)
	if len(parts) == 0 {
		l.add(tokens[start], SeverityWarning, "AF104", "empty variable directive")
		return end
	}
	l.checkPath(tokens[start], parts[0])
	if len(parts) > 1 {
		l.checkType(tokens[start], parts[0], parts[1])
	}
	return end
}

func (l *linter) checkCond(tokens token.Slice, start int) int {
	end, content := l.directiveContent(tokens, start)
	l.hasContent = true
	parts := strings.Fields(content)
	if len(parts) == 0 {
		l.add(tokens[start], SeverityWarning, "AF104", "empty conditional directive")
		return end
	}

	path := parts[0]
	l.checkPath(tokens[start], path)
	if len(parts) == 2 {
		if isSymbolicOperator(parts[1]) {
			l.add(tokens[start], SeverityError, "AF009", fmt.Sprintf("invalid comparison operator %q", parts[1]))
		} else {
			l.checkType(tokens[start], path, parts[1])
		}
	} else if len(parts) >= 3 {
		op := parts[1]
		// <?path type op value> declares the type before the comparison.
		if len(parts) >= 4 && !isOperator(op) && !isSymbolicOperator(op) {
			l.checkType(tokens[start], path, op)
			op = parts[2]
		}
		if isSymbolicOperator(op) || !isOperator(op) {
			l.add(tokens[start], SeverityError, "AF009", fmt.Sprintf("invalid comparison operator %q", op))
		}
	}
	l.stack = append(l.stack, frame{path: path, tok: tokens[start]})
	return end
}

func (l *linter) checkEnd(tokens token.Slice, start int) int {
	end, content := l.directiveContent(tokens, start)
	l.hasContent = true
	parts := strings.Fields(content)
	if len(parts) == 0 {
		l.add(tokens[start], SeverityWarning, "AF104", "empty end directive")
		return end
	}
	path := parts[0]
	l.checkPath(tokens[start], path)
	if len(l.stack) == 0 {
		l.add(tokens[start], SeverityError, "AF002", fmt.Sprintf("unmatched end tag </%s>", path))
		return end
	}
	top := l.stack[len(l.stack)-1]
	l.stack = l.stack[:len(l.stack)-1]
	if top.path != path {
		l.add(tokens[start], SeverityError, "AF004", fmt.Sprintf("mismatched conditional close: opened <?%s> but closed </%s>", top.path, path))
	}
	return end
}

func (l *linter) checkElse(tokens token.Slice, start int) int {
	end, _ := l.directiveContent(tokens, start)
	l.hasContent = true
	if len(l.stack) == 0 {
		l.add(tokens[start], SeverityError, "AF006", "else outside conditional")
		return end
	}
	top := &l.stack[len(l.stack)-1]
	if top.loop {
		l.add(tokens[start], SeverityError, "AF006", "else is not supported on a loop")
		return end
	}
	if top.sawElse {
		l.add(tokens[start], SeverityError, "AF005", fmt.Sprintf("duplicate else in conditional <?%s>", top.path))
	}
	top.sawElse = true
	return end
}

func (l *linter) directiveContent(tokens token.Slice, start int) (int, string) {
	end := start
	for end < len(tokens) && tokens[end].Kind != kind.CloseBracket {
		end++
	}
	if end >= len(tokens) {
		return start, ""
	}
	contentStart := tokens[start+2].Start
	contentEnd := tokens[end].Start
	if contentEnd < contentStart {
		contentEnd = contentStart
	}
	return end, string(l.src[contentStart:contentEnd])
}

func (l *linter) checkPath(tok token.T, path string) {
	if !validPath(path) {
		l.add(tok, SeverityError, "AF007", fmt.Sprintf("invalid variable path %q", path))
	}
}

func (l *linter) checkType(tok token.T, path, typ string) {
	if !validType(typ) {
		l.add(tok, SeverityError, "AF008", fmt.Sprintf("invalid type annotation %q", typ))
		return
	}
	if previous, ok := l.types[path]; ok && previous != typ {
		l.add(tok, SeverityWarning, "AF101", fmt.Sprintf("variable %q has inconsistent type annotations %q and %q", path, previous, typ))
		return
	}
	l.types[path] = typ
}

func (l *linter) add(tok token.T, severity Severity, code, message string) {
	line, column := position(l.lineStarts, tok.Start)
	l.diagnostics = append(l.diagnostics, Diagnostic{File: l.name, Line: line, Column: column, Severity: severity, Code: code, Message: message})
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// Position returns the 1-based line and column of a byte offset in src.
func Position(src []byte, offset int) (line, column int) {
	return position(lineStarts(src), offset)
}

func position(starts []int, offset int) (int, int) {
	line := 0
	for i := range starts {
		if starts[i] > offset {
			break
		}
		line = i
	}
	return line + 1, offset - starts[line] + 1
}

func validPath(path string) bool {
	if path == "" || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if !identifierPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func validType(typ string) bool {
	switch typ {
	case "string", "int", "bool", "float32", "float64":
		return true
	default:
		return false
	}
}

func isOperator(op string) bool {
	switch op {
	case "eq", "ne", "gt", "lt", "gte", "lte":
		return true
	default:
		return false
	}
}

func isSymbolicOperator(op string) bool {
	return bytes.ContainsAny([]byte(op), "=<>!")
}

func (l *linter) checkLoop(tokens token.Slice, start int) int {
	end, content := l.directiveContent(tokens, start)
	parts := strings.Fields(content)
	if len(parts) != 4 || parts[2] != "as" || !identifierPattern.MatchString(parts[3]) || (!strings.HasPrefix(parts[1], "[]") || len(parts[1]) == 2) {
		l.add(tokens[start], SeverityError, "AF010", "expected <*path []Type as alias>")
		return end
	}
	l.checkPath(tokens[start], parts[0])
	l.stack = append(l.stack, frame{path: parts[0], tok: tokens[start], loop: true})
	return end
}
