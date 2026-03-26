package coarse

import (
	"fmt"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
)

func TestTokenHelpers(t *testing.T) {
	input := []byte("hello world")
	tok := Token{Kind: Text, Start: 0, End: 5}

	if got := string(tok.Get(input)); got != "hello" {
		t.Fatalf("Get() = %q, want %q", got, "hello")
	}

	want := fmt.Sprintf("%v: [%d:%d] %q", Text, 0, 5, []byte("hello"))
	if got := tok.Stringify(input); got != want {
		t.Fatalf("Stringify() = %q, want %q", got, want)
	}
}

func TestTokenGetVarUsesTypeCache(t *testing.T) {
	input := []byte("user.age int")
	cache := map[string]string{}

	explicit := Token{Kind: Var, Start: 0, End: len(input)}.GetVar(input, cache)
	if got := explicit.Type; got != "int" {
		t.Fatalf("explicit type = %q, want int", got)
	}
	if got := cache["user.age"]; got != "int" {
		t.Fatalf("cached type = %q, want int", got)
	}

	implicitInput := []byte("user.age")
	implicit := Token{Kind: Var, Start: 0, End: len(implicitInput)}.GetVar(implicitInput, cache)
	if got := implicit.Type; got != "int" {
		t.Fatalf("implicit cached type = %q, want int", got)
	}
}

func TestTokenGetVarParsesConditionals(t *testing.T) {
	input := []byte("user.age gte 30")
	cache := map[string]string{}

	info := Token{Kind: OptionalBlock, Start: 0, End: len(input)}.GetVar(input, cache)

	if got := string(info.Path[0]); got != "user" {
		t.Fatalf("path[0] = %q, want user", got)
	}
	if got := string(info.Path[1]); got != "age" {
		t.Fatalf("path[1] = %q, want age", got)
	}
	if got := info.Operator; got != "gte" {
		t.Fatalf("operator = %q, want gte", got)
	}
	if got := info.Type; got != "int" {
		t.Fatalf("type = %q, want int", got)
	}
	if got := info.Operand; got != "30" {
		t.Fatalf("operand = %q, want 30", got)
	}
}

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

func TestConvertGroupsConditionalElseAndEndTokens(t *testing.T) {
	input := []byte("<?user.active><else></user.active>")
	tokens := token.Slice{
		{Kind: kind.OpenBracket, Start: 0, End: 1},
		{Kind: kind.DirectiveCond, Start: 1, End: 2},
		{Kind: kind.VarName, Start: 2, End: 13},
		{Kind: kind.CloseBracket, Start: 13, End: 14},
		{Kind: kind.OpenBracket, Start: 14, End: 15},
		{Kind: kind.DirectiveElse, Start: 15, End: 19},
		{Kind: kind.CloseBracket, Start: 19, End: 20},
		{Kind: kind.OpenBracket, Start: 20, End: 21},
		{Kind: kind.DirectiveEnd, Start: 21, End: 22},
		{Kind: kind.VarName, Start: 22, End: 33},
		{Kind: kind.CloseBracket, Start: 33, End: 34},
	}

	got := Convert(tokens, input)
	want := []Token{
		{Kind: OptionalBlock, Start: 2, End: 13},
		{Kind: ElseBlock, Start: 14, End: 20},
		{Kind: EndTag, Start: 22, End: 33},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d tokens, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d mismatch: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestConvertGroupsTitleAndTrimsInterPromptWhitespace(t *testing.T) {
	input := []byte(".title First\nbody\n\n.title Second\n")
	tokens := token.Slice{
		{Kind: kind.TitleDirective, Start: 0, End: 6},
		{Kind: kind.Whitespace, Start: 6, End: 7},
		{Kind: kind.TitleText, Start: 7, End: 12},
		{Kind: kind.Whitespace, Start: 12, End: 13},
		{Kind: kind.Text, Start: 13, End: 17},
		{Kind: kind.Whitespace, Start: 17, End: 19},
		{Kind: kind.TitleDirective, Start: 19, End: 25},
		{Kind: kind.Whitespace, Start: 25, End: 26},
		{Kind: kind.TitleText, Start: 26, End: 32},
		{Kind: kind.Whitespace, Start: 32, End: 33},
	}

	got := Convert(tokens, input)
	want := []Token{
		{Kind: Title, Start: 7, End: 12},
		{Kind: Text, Start: 13, End: 17},
		{Kind: Title, Start: 26, End: 32},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d tokens, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d mismatch: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestConvertTreatsStandaloneBracketAsText(t *testing.T) {
	input := []byte("<")
	tokens := token.Slice{{Kind: kind.OpenBracket, Start: 0, End: 1}}

	got := Convert(tokens, input)

	if len(got) != 1 {
		t.Fatalf("expected 1 token, got %d", len(got))
	}
	if got[0] != (Token{Kind: Text, Start: 0, End: 1}) {
		t.Fatalf("unexpected token: %+v", got[0])
	}
}

func TestGroupingHelpersValidateRanges(t *testing.T) {
	tokens := token.Slice{
		{Kind: kind.OpenBracket, Start: 0, End: 1},
		{Kind: kind.DirectiveVar, Start: 1, End: 2},
		{Kind: kind.VarName, Start: 2, End: 6},
		{Kind: kind.CloseBracket, Start: 6, End: 7},
	}

	if end := skipToClosingBracket(tokens, 0); end != 3 {
		t.Fatalf("skipToClosingBracket() = %d, want 3", end)
	}

	if !validGroupedRange(tokens, 0, 3) {
		t.Fatal("validGroupedRange() = false, want true")
	}

	grouped, ok := groupVariableTokens(tokens, 0)
	if !ok {
		t.Fatal("groupVariableTokens() = false, want true")
	}
	if grouped != (Token{Kind: Var, Start: 2, End: 6}) {
		t.Fatalf("unexpected grouped token: %+v", grouped)
	}

	truncated := tokens[:3]
	if end := skipToClosingBracket(truncated, 0); end != 2 {
		t.Fatalf("skipToClosingBracket() truncated = %d, want 2", end)
	}
	if validGroupedRange(truncated, 0, 2) {
		t.Fatal("validGroupedRange() truncated = true, want false")
	}
	if _, ok := groupVariableTokens(truncated, 0); ok {
		t.Fatal("groupVariableTokens() truncated = true, want false")
	}
	if _, ok := groupConditionalTokens(truncated, 0); ok {
		t.Fatal("groupConditionalTokens() truncated = true, want false")
	}
	if _, ok := groupEndTagTokens(truncated, 0); ok {
		t.Fatal("groupEndTagTokens() truncated = true, want false")
	}
}
