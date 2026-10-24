package gogen

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/token/coarse"
	"github.com/peyton-spencer/caseconv/bytcase"
)

// AfagentImport is the runtime package generated agent code imports.
const AfagentImport = "github.com/omniaura/agentflow/runtime/afagent"

// promptName returns the Go type of prompt p in f, as GenFile names it.
func promptName(f ast.File, p ast.Prompt) string {
	if p.Title.Kind == coarse.Unset {
		return string(bytcase.ToCamel([]byte(f.Name)))
	}
	return string(bytcase.ToCamel(p.Title.Get(f.Content)))
}

// genAgents writes, per .agent section, a <Name>Tools interface with a
// method per tool, a <Name>Agent struct whose Run drives the
// afagent.Loop, and the dispatcher that decodes each call's arguments
// with the tool's strict decoder before calling the method.
func genAgents(buf *bytes.Buffer, f ast.File, imports map[string]bool) error {
	for _, a := range f.Agents {
		system, err := agentSystem(f, a)
		if err != nil {
			return &DeclError{Offset: a.Start, Err: err}
		}
		tools, err := agentTools(f, a)
		if err != nil {
			return &DeclError{Offset: a.Start, Err: err}
		}
		imports["context"] = true
		imports[AfagentImport] = true
		name := camel(a.Name)

		if len(tools) > 0 {
			fmt.Fprintf(buf, "// %sTools runs the tools of the %s agent. Each method gets the call's\n// decoded arguments; its result or error goes back to the model.\n", name, a.Name)
			fmt.Fprintf(buf, "type %sTools interface {\n", name)
			for _, t := range tools {
				tn := camel(t.Name)
				if desc := firstLine(t.Description); desc != "" {
					fmt.Fprintf(buf, "// %s: %s\n", tn, desc)
				}
				fmt.Fprintf(buf, "%s(ctx context.Context, args %sArgs) (string, error)\n", tn, tn)
			}
			buf.WriteString("}\n\n")
		}

		fmt.Fprintf(buf, "// %sAgent is the %s agent.", name, a.Name)
		if a.Description != "" {
			buf.WriteString("\n//\n")
			for _, l := range strings.Split(a.Description, "\n") {
				fmt.Fprintf(buf, "// %s\n", strings.TrimRight(l, " \t"))
			}
		} else {
			buf.WriteString("\n")
		}
		fmt.Fprintf(buf, "type %sAgent struct {\n", name)
		buf.WriteString("Model afagent.Model\n")
		if len(tools) > 0 {
			fmt.Fprintf(buf, "Tools %sTools\n", name)
		}
		buf.WriteString("// MaxTurns bounds the model calls of a run")
		if a.MaxTurns > 0 {
			fmt.Fprintf(buf, "; 0 means %d.\n", a.MaxTurns)
		} else {
			buf.WriteString("; 0 means afagent.DefaultMaxTurns.\n")
		}
		buf.WriteString("MaxTurns int\n")
		buf.WriteString("// Concurrency is how many tool calls of a turn run at once.\nConcurrency int\n")
		buf.WriteString("// OnEvent, when set, sees each model response and tool call. With\n// Concurrency above 1 it is called concurrently.\nOnEvent func(afagent.Event)\n")
		buf.WriteString("}\n\n")

		fmt.Fprintf(buf, "// %sAgentTools describes the tools of the %s agent to the model.\n", name, a.Name)
		fmt.Fprintf(buf, "var %sAgentTools = []afagent.ToolSpec{\n", name)
		for _, t := range tools {
			tn := camel(t.Name)
			fmt.Fprintf(buf, "{Name: %sTool.Name, Description: %sTool.Description, Schema: %sTool.Schema},\n", tn, tn, tn)
		}
		buf.WriteString("}\n\n")

		params := "ctx context.Context, messages ...afagent.Message"
		systemExpr := `""`
		if system != "" {
			params = "ctx context.Context, system *" + system + ", messages ...afagent.Message"
			systemExpr = "system.String()"
		}
		fmt.Fprintf(buf, "// Run continues the conversation in messages until the model answers\n// without calling a tool, and returns the answer and the conversation.\n")
		fmt.Fprintf(buf, "func (a *%sAgent) Run(%s) (afagent.Result, error) {\n", name, params)
		buf.WriteString("maxTurns := a.MaxTurns\n")
		if a.MaxTurns > 0 {
			fmt.Fprintf(buf, "if maxTurns == 0 {\nmaxTurns = %d\n}\n", a.MaxTurns)
		}
		fmt.Fprintf(buf, "loop := afagent.Loop{\nModel: a.Model,\nSystem: %s,\nTools: %sAgentTools,\nMaxTurns: maxTurns,\nConcurrency: a.Concurrency,\nOnEvent: a.OnEvent,\n", systemExpr, name)
		if len(tools) > 0 {
			buf.WriteString("Dispatch: a.dispatch,\n")
		}
		buf.WriteString("}\nreturn loop.Run(ctx, messages)\n}\n\n")

		if len(tools) > 0 {
			imports["fmt"] = true
			fmt.Fprintf(buf, "func (a *%sAgent) dispatch(ctx context.Context, call afagent.ToolCall) (string, error) {\n", name)
			buf.WriteString("switch call.Name {\n")
			for _, t := range tools {
				tn := camel(t.Name)
				fmt.Fprintf(buf, "case %sTool.Name:\nargs, err := %sTool.Decode(call.Arguments)\nif err != nil {\nreturn \"\", err\n}\nreturn a.Tools.%s(ctx, args)\n", tn, tn, tn)
			}
			buf.WriteString("}\nreturn \"\", fmt.Errorf(\"there is no tool named %q\", call.Name)\n}\n\n")
		}
	}
	return nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// agentSystem returns the Go type of the agent's system prompt, or "" for
// none.
func agentSystem(f ast.File, a *decl.Agent) (string, error) {
	if a.System != "" {
		var titles []string
		for _, p := range f.Prompts {
			if p.Title.Kind == coarse.Unset {
				continue
			}
			title := strings.TrimSpace(string(p.Title.Get(f.Content)))
			if title == a.System {
				return promptName(f, p), nil
			}
			titles = append(titles, strconv.Quote(title))
		}
		return "", fmt.Errorf("agent %s: no prompt titled %q in this file (have %s)", a.Name, a.System, strings.Join(titles, ", "))
	}
	switch len(f.Prompts) {
	case 0:
		return "", nil
	case 1:
		return promptName(f, f.Prompts[0]), nil
	}
	return "", fmt.Errorf("agent %s: the file has %d prompts; name the system prompt with .system <title>", a.Name, len(f.Prompts))
}

// agentTools returns the tools the agent can call.
func agentTools(f ast.File, a *decl.Agent) ([]*decl.Tool, error) {
	if a.Tools == nil {
		return f.Tools, nil
	}
	var out []*decl.Tool
	for _, name := range a.Tools {
		i := slices.IndexFunc(f.Tools, func(t *decl.Tool) bool { return t.Name == name })
		if i < 0 {
			var have []string
			for _, t := range f.Tools {
				have = append(have, t.Name)
			}
			return nil, fmt.Errorf("agent %s: no tool %s in this file (have %s)", a.Name, name, strings.Join(have, ", "))
		}
		out = append(out, f.Tools[i])
	}
	return out, nil
}
