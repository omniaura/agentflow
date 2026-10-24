package gogen

import (
	"bytes"
	"fmt"
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
func genAgents(buf *bytes.Buffer, f ast.File, siblings []ast.SiblingTool, imports map[string]bool) error {
	for _, a := range f.Agents {
		system, err := agentSystem(f, a)
		if err != nil {
			return &DeclError{Offset: a.Start, Err: err}
		}
		tools, err := agentTools(f, a, siblings)
		if err != nil {
			return &DeclError{Offset: a.Start, Err: err}
		}
		returns, err := agentReturns(f, a)
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
			fmt.Fprintf(buf, "; 0 or less means %d.\n", a.MaxTurns)
		} else {
			buf.WriteString("; 0 or less means afagent.DefaultMaxTurns.\n")
		}
		buf.WriteString("MaxTurns int\n")
		buf.WriteString("// Concurrency is how many tool calls of a turn run at once.\nConcurrency int\n")
		if len(tools) > 0 {
			buf.WriteString("// Allow, when set, picks the tools of a run, such as the ones a\n// workspace enabled: only tools it allows are offered to the model,\n// and a call to another is refused. Nil allows every tool.\nAllow func(ctx context.Context, tool string) bool\n")
		}
		buf.WriteString("// OnEvent, when set, sees each model response and tool call. With\n// Concurrency above 1 it is called concurrently.\nOnEvent func(afagent.Event)\n")
		if returns != nil {
			fmt.Fprintf(buf, "// Check, when set, vets a decoded answer beyond its schema, such as\n// that it covers every input. Its error goes back to the model as\n// written, and the model answers again within MaxTurns.\nCheck func(ctx context.Context, out %sOutput) error\n", camel(returns.Name))
		}
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
		result := "afagent.Result"
		if returns != nil {
			result = name + "AgentResult"
			out := camel(returns.Name)
			fmt.Fprintf(buf, "// %s is a finished run of the %s agent: its answer, decoded, and\n// the run.\n", result, a.Name)
			fmt.Fprintf(buf, "type %s struct {\nOutput %sOutput\nafagent.Result\n}\n\n", result, out)
			fmt.Fprintf(buf, "// Run continues the conversation in messages until the model answers\n// without calling a tool, with a valid %s output, which it decodes.\n", returns.Name)
		} else {
			fmt.Fprintf(buf, "// Run continues the conversation in messages until the model answers\n// without calling a tool, and returns the answer and the conversation.\n")
		}
		fmt.Fprintf(buf, "func (a *%sAgent) Run(%s) (%s, error) {\n", name, params, result)
		buf.WriteString("maxTurns := a.MaxTurns\n")
		if a.MaxTurns > 0 {
			fmt.Fprintf(buf, "if maxTurns <= 0 {\nmaxTurns = %d\n}\n", a.MaxTurns)
		}
		toolsExpr := name + "AgentTools"
		if len(tools) > 0 {
			// Allow is asked once per run: dispatch then enforces the very
			// tools offered, even if Allow would answer differently later.
			buf.WriteString("tools := a.tools(ctx)\n")
			toolsExpr = "tools"
		}
		fmt.Fprintf(buf, "loop := afagent.Loop{\nModel: a.Model,\nSystem: %s,\nTools: %s,\nMaxTurns: maxTurns,\nConcurrency: a.Concurrency,\nOnEvent: a.OnEvent,\n", systemExpr, toolsExpr)
		if len(tools) > 0 {
			// The last turn answers with what the tools found, rather than
			// failing with ErrMaxTurns.
			imports["slices"] = true
			buf.WriteString("Dispatch: func(ctx context.Context, call afagent.ToolCall) (string, error) {\nif !slices.ContainsFunc(tools, func(t afagent.ToolSpec) bool { return t.Name == call.Name }) {\n// The model may name a tool it wasn't offered.\nreturn \"\", fmt.Errorf(\"tool %s is not available in this conversation\", call.Name)\n}\nreturn a.dispatch(ctx, call)\n},\nAnswerOnLastTurn: true,\n")
		}
		if returns == nil {
			buf.WriteString("}\nreturn loop.Run(ctx, messages)\n}\n\n")
		} else {
			schema := camel(returns.Name) + "Schema"
			fmt.Fprintf(buf, "Output: &afagent.OutputSpec{Name: %s.Name, Description: %s.Description, Schema: %s.Schema},\n", schema, schema, schema)
			fmt.Fprintf(buf, "Check: func(answer string) error {\nout, err := %s.Decode(answer)\nif err == nil && a.Check != nil {\nerr = a.Check(ctx, out)\n}\nreturn err\n},\n", schema)
			fmt.Fprintf(buf, "}\nres, err := loop.Run(ctx, messages)\nout := %s{Result: res}\nif err != nil {\nreturn out, err\n}\nout.Output, err = %s.Decode(res.Final)\nreturn out, err\n}\n\n", result, schema)
		}

		if len(tools) > 0 {
			imports["fmt"] = true
			fmt.Fprintf(buf, "// tools are the tools offered in a run: those Allow allows.\nfunc (a *%sAgent) tools(ctx context.Context) []afagent.ToolSpec {\nif a.Allow == nil {\nreturn %sAgentTools\n}\nvar out []afagent.ToolSpec\nfor _, t := range %sAgentTools {\nif a.Allow(ctx, t.Name) {\nout = append(out, t)\n}\n}\nreturn out\n}\n\n", name, name, name)
			fmt.Fprintf(buf, "func (a *%sAgent) dispatch(ctx context.Context, call afagent.ToolCall) (string, error) {\n", name)
			buf.WriteString("switch call.Name {\n")
			for _, t := range tools {
				tn := camel(t.Name)
				fmt.Fprintf(buf, "case %sTool.Name:\nargs, err := %sTool.Decode(call.Arguments)\nif err != nil {\nreturn \"\", err\n}\nreturn a.Tools.%s(ctx, args)\n", tn, tn, tn)
			}
			buf.WriteString("}\nreturn \"\", fmt.Errorf(\"there is no tool named %q\", call.Name)\n}\n\n")
		}
		if a.ServePath != "" {
			genAgentServer(buf, a, name, system, returns, imports)
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
// agentReturns returns the .output an agent answers with, or nil.
func agentReturns(f ast.File, a *decl.Agent) (*decl.Tool, error) {
	if a.Returns == "" {
		return nil, nil
	}
	var have []string
	for _, o := range f.Outputs {
		if o.Name == a.Returns {
			return o, nil
		}
		have = append(have, o.Name)
	}
	list := "it declares none"
	if len(have) > 0 {
		list = "have " + strings.Join(have, ", ")
	}
	return nil, fmt.Errorf("agent %s: no output %s in this file (%s)", a.Name, a.Returns, list)
}

func agentTools(f ast.File, a *decl.Agent, siblings []ast.SiblingTool) ([]*decl.Tool, error) {
	if a.Tools == nil {
		return f.Tools, nil
	}
	var out []*decl.Tool
	for _, name := range a.Tools {
		t, err := ast.FindTool(f.Tools, siblings, name)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// genAgentServer writes <Name>AgentServer, which serves an agent with
// .serve over HTTP. The client sends only its new message: the system
// prompt's values and the conversation so far come from Prepare, on the
// server, so a client can't forge the model's or a tool's messages.
func genAgentServer(buf *bytes.Buffer, a *decl.Agent, name, system string, returns *decl.Tool, imports map[string]bool) {
	imports["net/http"] = true
	imports["errors"] = true
	imports[AfhttpImport] = true
	route := a.ServeMethod + " " + a.ServePath
	server := name + "AgentServer"

	fmt.Fprintf(buf, "// %sAgentRequest is the body of %s.\n", name, route)
	fmt.Fprintf(buf, "type %sAgentRequest struct {\n// Message is the user's new message.\nMessage string `json:\"message\"`\n}\n\n", name)
	fmt.Fprintf(buf, "// %sAgentResponse is the answer to %s.\n", name, route)
	fmt.Fprintf(buf, "type %sAgentResponse struct {\n", name)
	if returns != nil {
		fmt.Fprintf(buf, "Output %sOutput `json:\"output\"`\n", camel(returns.Name))
	} else {
		buf.WriteString("Answer string `json:\"answer\"`\n")
	}
	buf.WriteString("Usage afagent.Usage `json:\"usage\"`\n}\n\n")

	result := "afagent.Result"
	if returns != nil {
		result = name + "AgentResult"
	}
	fmt.Fprintf(buf, "// %s serves the %s agent at %s. Set its fields, then call\n// Register.\n", server, a.Name, route)
	fmt.Fprintf(buf, "type %s struct {\nAgent *%sAgent\n", server, name)
	buf.WriteString("// Prepare authenticates the request and returns ")
	if system != "" {
		buf.WriteString("the system prompt and ")
	}
	buf.WriteString("the\n// conversation so far, which the new message continues. An error\n// fails the request as it is when it is an *errs.Error, or with 500;\n// wrap authentication failures with afhttp.Unauthorized.\n")
	if system != "" {
		fmt.Fprintf(buf, "Prepare func(r *http.Request) (*%s, []afagent.Message, error)\n", system)
	} else {
		buf.WriteString("Prepare func(r *http.Request) ([]afagent.Message, error)\n")
	}
	fmt.Fprintf(buf, "// Done, when set, runs after a successful run, to store the\n// conversation (res.Messages) for the next request.\nDone func(r *http.Request, res %s) error\n", result)
	buf.WriteString("// MaxBytes caps the request body; 0 means 1 MiB.\nMaxBytes int64\n")
	buf.WriteString("// OnError writes a failed request's error; nil means afhttp.WriteError.\nOnError func(w http.ResponseWriter, r *http.Request, err error)\n}\n\n")

	fmt.Fprintf(buf, "// Register adds %s to mux.\nfunc (s *%s) Register(mux *http.ServeMux) {\nmux.HandleFunc(%q, s.serve)\n}\n\n", route, server, route)

	fmt.Fprintf(buf, "func (s *%s) fail(w http.ResponseWriter, r *http.Request, err error) {\nif s.OnError != nil {\ns.OnError(w, r, err)\nreturn\n}\nafhttp.WriteError(w, r, err)\n}\n\n", server)

	fmt.Fprintf(buf, "func (s *%s) serve(w http.ResponseWriter, r *http.Request) {\n", server)
	if system != "" {
		buf.WriteString("system, history, err := s.Prepare(r)\n")
	} else {
		buf.WriteString("history, err := s.Prepare(r)\n")
	}
	buf.WriteString("if err != nil {\ns.fail(w, r, err)\nreturn\n}\n")
	fmt.Fprintf(buf, "maxBytes := s.MaxBytes\nif maxBytes <= 0 {\nmaxBytes = 1 << 20\n}\nvar body %sAgentRequest\nif err := afhttp.DecodeBody(w, r, maxBytes, &body); err != nil {\ns.fail(w, r, err)\nreturn\n}\n", name)
	buf.WriteString("if body.Message == \"\" {\ns.fail(w, r, afhttp.ErrInvalidRequest.New(r.Context()).Field(\"message\", \"is required\"))\nreturn\n}\n")
	buf.WriteString("messages := append(history[:len(history):len(history)], afagent.Message{Role: afagent.User, Content: body.Message})\n")
	// A client that accepts text/event-stream gets the run as it happens.
	// The agent is copied per request, so concurrent streams don't share
	// an OnEvent.
	buf.WriteString("agent := s.Agent\nvar stream *afhttp.EventStream\nif afhttp.WantsEventStream(r) {\nstream = afhttp.NewEventStream(w)\ncopied := *s.Agent\nprev := copied.OnEvent\ncopied.OnEvent = func(e afagent.Event) {\nif prev != nil {\nprev(e)\n}\nif name, data, ok := afagent.ClientEvent(e); ok {\nstream.Send(name, data)\n}\n}\nagent = &copied\n}\n")
	if system != "" {
		buf.WriteString("res, err := agent.Run(r.Context(), system, messages...)\n")
	} else {
		buf.WriteString("res, err := agent.Run(r.Context(), messages...)\n")
	}
	buf.WriteString("if err == nil && s.Done != nil {\nerr = s.Done(r, res)\n} else if errors.Is(err, afagent.ErrMaxTurns) || errors.Is(err, afagent.ErrTruncated) || errors.Is(err, afagent.ErrRefused) {\n")
	buf.WriteString("// The model, not the server, failed.\nerr = afhttp.ErrModelFailed.New(r.Context()).AddError(err)\n}\n")
	buf.WriteString("if err != nil {\nif stream != nil {\n// The status is sent: report the error as an event.\nstream.SendError(r.Context(), err)\nreturn\n}\ns.fail(w, r, err)\nreturn\n}\n")
	if returns != nil {
		fmt.Fprintf(buf, "resp := %sAgentResponse{Output: res.Output, Usage: res.Usage}\n", name)
	} else {
		fmt.Fprintf(buf, "resp := %sAgentResponse{Answer: res.Final, Usage: res.Usage}\n", name)
	}
	buf.WriteString("if stream != nil {\nstream.Send(\"done\", resp)\nreturn\n}\nafhttp.WriteJSON(w, r, http.StatusOK, resp)\n}\n\n")
}
