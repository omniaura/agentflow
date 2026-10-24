package survey

import (
	"fmt"
	"testing"

	"github.com/omniaura/agentflow/pkg/sqlcgo"
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
		Routes:       {"api/api.go:14 GetUser → GetUser"},
		Checks:       {"api/api.go:30 Ban → GetUser, BanUser"},
		Outputs:      {"api/api.go:67 openai.ResponseFormatJSONSchemaParam"},
		Prompts:      {"api/api.go:69 "},
		GenAgentflow: {"api/api_af.go:1 "},
		GenSqlc:      {"db/q.sql.go:1 "},
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

func TestWrites(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind sqlcgo.Kind
		want bool
	}{
		{"GetUser", sqlcgo.One, false},
		{"ListUsers", sqlcgo.Many, false},
		{"CreateUser", sqlcgo.One, true},
		{"Settings", sqlcgo.One, false},
		{"SetName", sqlcgo.One, true},
		{"Update", sqlcgo.Many, true},
		{"Touch", sqlcgo.Exec, true},
		{"CountUsers", sqlcgo.ExecRows, true},
	} {
		if got := writes(&sqlcgo.Query{Name: tt.name, Kind: tt.kind}); got != tt.want {
			t.Errorf("%s %s: got %v", tt.name, tt.kind, got)
		}
	}
}
