package lint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestLintValidFile(t *testing.T) {
	diagnostics := lintString(t, `.title System Prompt
Hello <!user.name>.

<?user.premium bool>
Premium.
<else>
Default.
</user.premium>
`)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLintAcceptsTypedComparison(t *testing.T) {
	diagnostics := lintString(t, ".title Test\n<?age int gte 18>\nAdult\n<else>\nMinor\n</age>\n")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLintRules(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{"no title", "Hello <!user.name>\n", "AF001"},
		{"unmatched end", ".title Test\n</user.premium>\n", "AF002"},
		{"unclosed conditional", ".title Test\n<?user.premium>\n", "AF003"},
		{"mismatched close", ".title Test\n<?user.premium>\n</user.admin>\n", "AF004"},
		{"duplicate else", ".title Test\n<?user.premium>\n<else>\n<else>\n</user.premium>\n", "AF005"},
		{"else outside", ".title Test\n<else>\n", "AF006"},
		{"invalid var path", ".title Test\n<!user..name>\n", "AF007"},
		{"invalid conditional path", ".title Test\n<?.premium>\n</.premium>\n", "AF007"},
		{"invalid end path", ".title Test\n</user.>\n", "AF007"},
		{"invalid type", ".title Test\n<!count float>\n", "AF008"},
		{"symbolic operator", ".title Test\n<?count >= 3>\n</count>\n", "AF009"},
		{"typed symbolic operator", ".title Test\n<?count int >= 3>\n</count>\n", "AF009"},
		{"typed comparison with invalid type", ".title Test\n<?count float gte 3>\n</count>\n", "AF008"},
		{"inconsistent type", ".title Test\n<!count int> <!count bool>\n", "AF101"},
		{"duplicate title", ".title Hello User\nOne\n.title hello user\nTwo\n", "AF102"},
		{"empty title", ".title   \nBody\n", "AF103"},
		{"empty directive", ".title Test\n<!>\n", "AF104"},
		{"bad tool name", ".tool bad name\nDoes x.\n", "AF011"},
		{"bad param path", ".tool x\n.param a-b\nDoes x.\n", "AF011"},
		{"scalar parent", ".tool x\n.param a int\n.param a.b\nDoes x.\n", "AF011"},
		{"output any", ".output o\n.field x any\n", "AF011"},
		{"duplicate output", ".output o\nOne.\n.output o\nTwo.\n", "AF011"},
		{"bad route", ".route GET /x getx\n", "AF011"},
		{"duplicate route", ".route GET /x Q\n\n.route GET /x R\n", "AF011"},
		{"misspelled param type", ".tool x\n.param a strng The a.\nDoes x.\n", "AF012"},
		{"duplicate tool", ".tool x\nOne.\n.tool x\nTwo.\n", "AF011"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := lintString(t, tt.src)
			if !hasCode(diagnostics, tt.code) {
				t.Fatalf("expected %s, got %#v", tt.code, diagnostics)
			}
		})
	}
}

func TestLintToolSection(t *testing.T) {
	diagnostics := lintString(t, ".tool search\n.param query string What to find.\nSearch the web.\n\n.title System\nUse <!tool_name>.\n")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
	diagnostics = lintString(t, ".title System\nHi.\n\n.tool x\n.param a int\n.param a int\nDoes x.\n")
	if len(diagnostics) != 1 || diagnostics[0].Code != "AF011" || diagnostics[0].Line != 6 {
		t.Fatalf("expected AF011 on line 6, got %#v", diagnostics)
	}
}

func lintString(t *testing.T, src string) []Diagnostic {
	t.Helper()
	diagnostics, err := Lint("test.af", []byte(src))
	require.NoError(t, err)
	return diagnostics
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestLoopDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, source, code, message string }{
		{"missing element", ".title Test\n<*items [] as item></items>", "AF010", "expected <*path []Type as alias>"},
		{"empty directive", ".title Test\nA<*>B", "AF010", "expected <*path []Type as alias>"},
		{"unclosed loop", ".title Test\n<*items []Item as item>", "AF003", "unclosed loop <*items>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ds := lintString(t, tt.source)
			found := false
			for _, d := range ds {
				if d.Code == tt.code && d.Message == tt.message {
					found = true
				}
			}
			var err error
			if !found {
				err = fmt.Errorf("missing %s %q in %#v", tt.code, tt.message, ds)
			}
			require.NoError(t, err)
		})
	}
}

func TestLintAgentReferences(t *testing.T) {
	const ok = ".title A\nHi.\n\n.title B\nYo.\n\n.tool look\nLooks.\n\n.output o\n.field x\n\n.agent helper\n.system B\n.tools look\n"
	if d := lintString(t, ok); len(d) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", d)
	}
	for _, tt := range []struct {
		name, src, msg string
		line           int
	}{
		{"unknown system", ".title A\nHi.\n\n.agent x\n.tools\n", "", 0},
		{"unknown system title", ".title A\nHi.\n\n.agent x\n.maxturns 3\n.system Nope\n", `no prompt titled "Nope" in this file (have "A")`, 6},
		{"ambiguous system", ".title A\nHi.\n\n.title B\nYo.\n\n.agent x\n", "the file has 2 prompts", 7},
		{"unknown tool", ".title A\nHi.\n\n.tool look\nLooks.\n\n.agent x\n.tools look, find\n", "no tool find in this file (have look)", 8},
		{"output is not a tool", ".title A\nHi.\n\n.output o\n.field x\n\n.agent x\n.tools o\n", "no tool o in this file (it declares none)", 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := lintString(t, tt.src)
			if len(d) != 1 || d[0].Code != "AF011" || !strings.Contains(d[0].Message, tt.msg) || (tt.line != 0 && d[0].Line != tt.line) {
				t.Fatalf("want one AF011 %q on line %d, got %#v", tt.msg, tt.line, d)
			}
		})
	}
}
