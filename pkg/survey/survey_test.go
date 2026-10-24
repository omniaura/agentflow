package survey

import (
	"fmt"
	"testing"
)

func TestRun(t *testing.T) {
	r, err := Run("testdata/mod")
	if err != nil {
		t.Fatal(err)
	}
	got := map[Category][]string{}
	for c, sites := range r.Sites {
		for _, s := range sites {
			got[c] = append(got[c], fmt.Sprintf("%s:%d %s", s.File, s.Line, s.Name))
		}
	}
	want := map[Category][]string{
		Routes:       {"api/api.go:15 GetUser → GetUser"},
		Checks:       {"api/api.go:98 Fallthrough → GetUser, BanUser", "api/api.go:31 Ban → GetUser, BanUser", "api/api.go:67 Audited → GetUser, GetUser", "api/api.go:91 Guarded → GetUser, BanUser"},
		Outputs:      {"api/api.go:133 openai.ResponseFormatJSONSchemaParam"},
		Prompts:      {"api/api.go:135 "},
		GenAgentflow: {"api/api_af.go:1 "},
		GenSqlc:      {"db/q.sql.go:1 ", "audit/q.sql.go:1 "},
		Agents:       {"api/agent.go:11 RunAgent"},
	}
	for c, w := range want {
		if fmt.Sprint(got[c]) != fmt.Sprint(w) {
			t.Errorf("%s: got %v, want %v", c, got[c], w)
		}
	}
	// The tool literal counts with its declaration, the dynamic one with
	// its statement, and the map nested in the literal not again.
	if len(r.Sites[Tools]) != 2 || r.Sites[Tools][0].Name != "openai.FunctionDefinitionParam" || r.Sites[Tools][0].Lines != 7 || r.Sites[Tools][1].Lines != 4 {
		t.Errorf("tools = %+v", r.Sites[Tools])
	}
	if r.AFLines != 2 || r.Lines != r.HandWritten+r.LinesOf(GenAgentflow)+r.LinesOf(GenSqlc) {
		t.Errorf("lines = %d, hand-written %d, af %d", r.Lines, r.HandWritten, r.AFLines)
	}
}
