package gogen

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/ast"
)

func genAgentFile(t *testing.T, src string) (string, error) {
	t.Helper()
	f, err := ast.NewFile("bot.af", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = GenFile(&buf, f, "bot")
	return buf.String(), err
}

func TestGenAgents(t *testing.T) {
	code, err := genAgentFile(t, ".title A\nHi.\n\n.title B\nYo <!name>.\n\n.tool look\n.param id\n\nLooks.\n\n.tool skip\nSkips.\n\n.agent helper\n.system B\n.tools look\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type HelperTools interface {",
		"Look(ctx context.Context, args LookArgs) (string, error)",
		"func (a *HelperAgent) Run(ctx context.Context, system *B, messages ...afagent.Message) (afagent.Result, error)",
		"case LookTool.Name:",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	// .tools limits the agent to the tools it lists.
	if strings.Contains(code, "Skip(ctx") || strings.Contains(code, "case SkipTool.Name") {
		t.Errorf("agent got a tool it did not list:\n%s", code)
	}
}

func TestGenAgentWithoutPrompts(t *testing.T) {
	code, err := genAgentFile(t, ".tool look\nLooks.\n\n.agent helper\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "func (a *HelperAgent) Run(ctx context.Context, messages ...afagent.Message)") {
		t.Errorf("Run should take no system prompt:\n%s", code)
	}
}

func TestGenAgentErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"unknown system", ".title A\nHi.\n\n.agent x\n.system Nope\n", "no prompt titled \"Nope\" in this file (have \"A\")"},
		{"ambiguous system", ".title A\nHi.\n\n.title B\nYo.\n\n.agent x\n", "the file has 2 prompts; name the system prompt with .system <title>"},
		{"unknown tool", ".title A\nHi.\n\n.tool look\nLooks.\n\n.agent x\n.tools find\n", "no tool find in this file (have look)"},
		{"unknown output", ".title A\nHi.\n\n.output o\n.field x\n\n.agent x\n.returns p\n", "no output p in this file (have o)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genAgentFile(t, tt.src)
			var de *DeclError
			if !errors.As(err, &de) || !strings.Contains(de.Error(), tt.msg) {
				t.Fatalf("got %v, want DeclError containing %q", err, tt.msg)
			}
			if de.Offset != strings.Index(tt.src, ".agent") {
				t.Fatalf("offset %d does not point at the agent", de.Offset)
			}
		})
	}
}
