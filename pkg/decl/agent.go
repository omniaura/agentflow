package decl

import (
	"fmt"
	"strconv"
	"strings"
)

// AgentDirectives are the directives allowed inside an agent section.
var AgentDirectives = []string{".system", ".tools", ".maxturns", ".returns"}

// Agent is a parsed .agent section: a tool-calling loop over the file's
// prompts and tools.
//
//	.agent support
//	.system Support Agent
//	.tools lookup_order, offer_refund
//	.maxturns 8
//	Answers customer questions about their orders.
//
// Without .system, the system prompt is the file's only .title prompt, if it
// has exactly one. Without .tools, the agent can call every tool declared
// in the file. With .returns, its final answer is the named .output.
type Agent struct {
	Name string
	// System is the title of the system prompt, or "" when inferred.
	System string
	// Tools are the tool names, or nil for every tool in the file.
	Tools    []string
	MaxTurns int
	// Returns names the .output the final answer must be, or "" for text.
	Returns     string
	Description string
	Start, End  int
}

// ParseAgent parses the agent section src[start:end], whose first line is
// its .agent line.
func ParseAgent(src []byte, start, end int) (*Agent, error) {
	a := &Agent{Start: start, End: end}
	header, i := line(src, start, end)
	a.Name = strings.TrimSpace(strings.TrimPrefix(header, ".agent"))
	if !toolNameRE.MatchString(a.Name) {
		return nil, &Error{Offset: start, Msg: fmt.Sprintf("agent name %q must be 1-64 letters, digits, '_' or '-'", a.Name)}
	}
	var desc []string
	for i < end {
		off := i
		var l string
		l, i = line(src, i, end)
		b := []byte(l)
		switch {
		case hasDirective(b, ".system"):
			a.System = strings.TrimSpace(strings.TrimPrefix(l, ".system"))
			if a.System == "" {
				return nil, &Error{Offset: off, Msg: ".system needs the title of a prompt in this file"}
			}
		case hasDirective(b, ".tools"):
			names := strings.FieldsFunc(strings.TrimPrefix(l, ".tools"), func(r rune) bool { return r == ' ' || r == '\t' || r == ',' })
			if len(names) == 0 {
				return nil, &Error{Offset: off, Msg: ".tools needs tool names; leave it out to use every tool in the file"}
			}
			for _, n := range names {
				if !toolNameRE.MatchString(n) {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf(".tools: %q is not a tool name", n)}
				}
				for _, prev := range a.Tools {
					if prev == n {
						return nil, &Error{Offset: off, Msg: fmt.Sprintf(".tools lists %s twice", n)}
					}
				}
				a.Tools = append(a.Tools, n)
			}
		case hasDirective(b, ".returns"):
			a.Returns = strings.TrimSpace(strings.TrimPrefix(l, ".returns"))
			if !toolNameRE.MatchString(a.Returns) {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".returns needs the name of an .output in this file, got %q", a.Returns)}
			}
		case hasDirective(b, ".maxturns"):
			v := strings.TrimSpace(strings.TrimPrefix(l, ".maxturns"))
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".maxturns needs a positive integer, got %q", v)}
			}
			a.MaxTurns = n
		default:
			desc = append(desc, l)
		}
	}
	a.Description = strings.TrimRight(trimLeadingBlankLines(strings.Join(desc, "\n")), " \t\n")
	return a, nil
}
