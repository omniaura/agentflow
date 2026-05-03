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
	"fmt"
	"regexp"
	"strings"

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
	hasTitle    bool
	hasContent  bool
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
		}
	}

	for _, f := range l.stack {
		l.add(f.tok, SeverityError, "AF003", fmt.Sprintf("unclosed conditional <?%s>", f.path))
	}
	if !l.hasTitle && l.hasContent {
		l.add(token.T{Start: 0, End: 0}, SeverityError, "AF001", "file has content but no .title section")
	}
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
		if isSymbolicOperator(parts[1]) || !isOperator(parts[1]) {
			l.add(tokens[start], SeverityError, "AF009", fmt.Sprintf("invalid comparison operator %q", parts[1]))
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
