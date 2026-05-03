package lint

import (
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
		{"inconsistent type", ".title Test\n<!count int> <!count bool>\n", "AF101"},
		{"duplicate title", ".title Hello User\nOne\n.title hello user\nTwo\n", "AF102"},
		{"empty title", ".title   \nBody\n", "AF103"},
		{"empty directive", ".title Test\n<!>\n", "AF104"},
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
