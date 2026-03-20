package coarse

import (
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
)

func TestConvertHandlesMalformedDirectiveTokens(t *testing.T) {
	input := []byte("<!")
	tokens := token.Slice{
		{Kind: kind.OpenBracket, Start: 0, End: 1},
		{Kind: kind.DirectiveVar, Start: 1, End: 2},
	}

	got := Convert(tokens, input)

	if len(got) != 1 {
		t.Fatalf("expected 1 token, got %d", len(got))
	}
	if got[0].Kind != Text {
		t.Fatalf("expected malformed directive to fall back to text, got %v", got[0].Kind)
	}
	if got[0].Start != 0 || got[0].End != 1 {
		t.Fatalf("expected fallback token to cover opening bracket, got [%d:%d]", got[0].Start, got[0].End)
	}
}

func TestConvertGroupsValidDirectiveTokens(t *testing.T) {
	input := []byte("<!user.name>")
	tokens := token.Slice{
		{Kind: kind.OpenBracket, Start: 0, End: 1},
		{Kind: kind.DirectiveVar, Start: 1, End: 2},
		{Kind: kind.VarName, Start: 2, End: 11},
		{Kind: kind.CloseBracket, Start: 11, End: 12},
	}

	got := Convert(tokens, input)
	want := []Token{{Kind: Var, Start: 2, End: 11}}

	if len(got) != len(want) {
		t.Fatalf("expected %d tokens, got %d", len(want), len(got))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("token %d mismatch: want %+v got %+v", i, want[i], got[i])
		}
	}

	info := got[0].GetVar(input, map[string]string{})
	if string(info.Path[0]) != "user" || string(info.Path[1]) != "name" {
		t.Fatalf("unexpected variable path: %#v", info.Path)
	}
	if info.Type != "string" {
		t.Fatalf("expected default type string, got %q", info.Type)
	}

	require.NoError(t, nil)
}
