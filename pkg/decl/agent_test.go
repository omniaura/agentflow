package decl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseAgent(t *testing.T) {
	src := ".agent support\n.system Support Agent\n.tools lookup_order, offer_refund\n.maxturns 8\n.returns verdict\nAnswers questions.\n\n.agent bare\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := Agent{Name: "support", System: "Support Agent", Tools: []string{"lookup_order", "offer_refund"}, MaxTurns: 8, Returns: "verdict", Description: "Answers questions.", End: strings.Index(src, ".agent bare")}
	if len(f.Agents) != 2 || !reflect.DeepEqual(*f.Agents[0], want) {
		t.Fatalf("got %+v\nwant %+v", f.Agents[0], want)
	}
	if b := f.Agents[1]; b.Name != "bare" || b.Tools != nil || b.System != "" {
		t.Fatalf("bare = %+v", b)
	}
	sec, err := ParseSection([]byte(src), 0, want.End)
	if a, ok := sec.(*Agent); !ok || err != nil || a.Name != "support" {
		t.Fatalf("ParseSection = %#v, %v", sec, err)
	}
}

func TestParseAgentErrors(t *testing.T) {
	for _, tt := range []struct{ src, msg string }{
		{".agent\n", `agent name ""`},
		{".agent a\n.system\n", ".system needs the title"},
		{".agent a\n.tools\n", ".tools needs tool names"},
		{".agent a\n.tools x, x\n", ".tools lists x twice"},
		{".agent a\n.maxturns 0\n", ".maxturns needs a positive integer"},
		{".agent a\n.returns\n", ".returns needs the name of an .output"},
		{".agent a\n.returns two words\n", ".returns needs the name of an .output"},
		{".agent a\n.agent a\n", `agent "a" is declared twice`},
	} {
		_, err := Parse([]byte(tt.src))
		var de *Error
		if !errors.As(err, &de) || !strings.Contains(de.Msg, tt.msg) {
			t.Errorf("%q: got %v, want %q", tt.src, err, tt.msg)
		}
	}
}
