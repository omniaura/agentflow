package gogen_test

import (
	"bytes"
	"fmt"
	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopsCompileAndRender(t *testing.T) {
	tests := []struct{ name, source, fixture string }{
		{"TestLoopsCompileAndRender", ".title system\n<*extensions []Extension as extension><!extension.name>:<!extension.detail.text>;<*extension.scores []int as score><!score>,</extension.scores></extensions>\n.title user\n<*names []string as name><!name>;</names><*counts []int as count><!count>;</counts>", "package main\n\ntype Detail struct {Text string}; type Extension struct {Name string; Detail Detail; Scores []int}\nfunc main() {\ns:=System{Extensions:[]Extension{{Name:\"writing\",Detail:Detail{Text:\"concise\"},Scores:[]int{1,2}}}};if got:=s.String();got!=\"writing:concise;1,2,\" {panic(got)}\nu:=User{Names:[]string{\"A\",\"B\"},Counts:[]int{0,-2}};if got:=u.String();got!=\"A;B;0;-2;\" {panic(got)};u=User{};if got:=u.String();got!=\"\" {panic(got)}\n}\n"},
		{"TestArbitraryValuesAndNestedSlices", ".title values\n<*values []any as value><!value>;</values><*matrix [][]int as row><*row []int as number><!number>,</row>;</matrix>", "package main\n\nfunc main(){v:=Values{Values:[]any{\"text\",42,struct{Name string}{\"struct\"},nil},Matrix:[][]int{{1,2},nil,{3}}};if got:=v.String();got!=\"text;42;{struct};<nil>;1,2,;;3,;\"{panic(got)}}"},
		{"TestLoopsWithImplicitObjectsAndLexicalConditions", ".title mixed\n<!account.name>:<*items []Item as item><?item.enabled bool><!item.name><*item.tags []string as tag><!item.name>=<!tag>;</item.tags><else>off</item.enabled></items><!account.count int>", "package main\n\ntype Item struct{Name string; Enabled bool; Tags []string}\nfunc main(){v:=Mixed{Items:[]Item{{Name:\"A\",Enabled:true,Tags:[]string{\"x\",\"y\"}},{Name:\"B\"}}};v.Account.Name=\"owner\";v.Account.Count=2;if got:=v.String();got!=\"owner:AA=x;A=y;off2\"{panic(got)}}"},
		{"TestLoopImplicitObjectConditional", ".title conditional\n<*items []string as item><!item></items><?account><!account.name><else>empty</account>", "package main\n\nfunc main(){v:=Conditional{Items:[]string{\"A\"}};if got:=v.String();got!=\"Aempty\"{panic(got)};v.Account.Name=\"owner\";if got:=v.String();got!=\"Aowner\"{panic(got)}}"},
		{"alias_struct_condition", ".title alias\n<*items []Item as item><?item.detail Detail><!item.detail.text><else>empty</item.detail></items>", "package main\ntype Detail struct{Text string};type Item struct{Detail Detail}\nfunc main(){v:=Alias{Items:[]Item{{},{Detail:Detail{Text:\"yes\"}}}};if got:=v.String();got!=\"emptyyes\"{panic(got)}}"},
		{"malformed_loop_literal", ".title malformed\nA<*>B", "package main\nfunc main(){if got:=(&Malformed{}).String();got!=\"A<*>B\"{panic(got)}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ast.NewFile("prompts.af", []byte(tt.source))
			require.NoError(t, err)
			var b bytes.Buffer
			require.NoError(t, gogen.GenFile(&b, f, "main"))
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": "module example\ngo 1.25.0\n", "prompts.go": b.String(), "main.go": tt.fixture} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			out, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s\n%s", err, out, b.String())
			}
			require.NoError(t, err)
		})
	}
}
func TestInvalidLoops(t *testing.T) {
	tests := []struct{ name, source string }{
		{"scalar collection", "<*items int as item></items>"},
		{"missing element", "<*items [] as item></items>"},
		{"out of scope", "<*items []string as item></items><!item>"},
		{"invalid expression", "<*items []string as item><!value string;panic()></items>"},
		{"wrong close", "<*items []string as item></wrong>"},
		{"unclosed", "<*items []string as item>"},
		{"else on loop", "<*items []string as item><else></items>"},
		{"shadowing", "<*items []string as item><*others []int as item></others></items>"},
		{"invalid alias", "<*items []string as item;panic></items>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ast.NewFile("invalid.af", []byte(tt.source))
			if err == nil {
				var b bytes.Buffer
				err = gogen.GenFile(&b, f, "example")
			}
			var check error
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				check = fmt.Errorf("expected invalid source error, got %v", err)
			}
			require.NoError(t, check)
		})
	}
}
