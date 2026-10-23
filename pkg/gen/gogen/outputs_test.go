package gogen

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/decl"
)

// checkStrictSchema enforces the strict structured-output rules OpenAI and
// Anthropic share: every object lists all its properties as required and
// sets additionalProperties to false, and no unsupported keyword appears.
func checkStrictSchema(t *testing.T, path string, v any) {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok {
			for i, x := range arr {
				checkStrictSchema(t, fmt.Sprintf("%s[%d]", path, i), x)
			}
		}
		return
	}
	for _, k := range []string{"minimum", "maximum", "multipleOf", "minLength", "maxLength", "minItems", "maxItems", "pattern"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s: unsupported keyword %s", path, k)
		}
	}
	if m["type"] == "object" {
		props, _ := m["properties"].(map[string]any)
		if m["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false", path)
		}
		var req []string
		for _, r := range m["required"].([]any) {
			req = append(req, r.(string))
		}
		for k := range props {
			if !slices.Contains(req, k) {
				t.Errorf("%s: property %s is not required", path, k)
			}
		}
	}
	for k, x := range m {
		checkStrictSchema(t, path+"."+k, x)
	}
}

func TestOutputSchemasAreStrict(t *testing.T) {
	src := ".output everything\n.field s\n.field n? int\n.field f float\n.field b? bool\n.field e? enum(a, b)\n.field es? []enum(x, y)\n.field list? []string\n.field obj.inner? int\n.field objs[].name\n.field objs[].deep.k bool\n"
	f, err := decl.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	schema := strictObjectSchema(f.Outputs[0].Params, "")
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	checkStrictSchema(t, "$", v)
	for _, want := range []string{
		`"n":{"anyOf":[{"type":"integer"},{"type":"null"}]}`,
		`"e":{"anyOf":[{"type":"string","enum":["a","b"]},{"type":"null"}]}`,
		`"obj":{"anyOf":[{"type":"object","properties":{"inner":{"anyOf":[{"type":"integer"},{"type":"null"}]}},"required":["inner"],"additionalProperties":false},{"type":"null"}]}`,
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema is missing %s:\n%s", want, schema)
		}
	}
}

func TestOutputStrictErrors(t *testing.T) {
	for _, tt := range []struct{ src, msg string }{
		{".output o\n.field x any\n", "cannot hold any"},
		{".output o\n.field x object\n", "declare the object's fields"},
		{".output o\n.field a.b []object\n", `field "a.b": declare the object's fields`},
		{".output o\n.param x\n", ""},
	} {
		_, err := decl.Parse([]byte(tt.src))
		if tt.msg == "" {
			// .param is not an .output directive: it is description text.
			if err != nil {
				t.Errorf("%q: %v", tt.src, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("%q: got %v, want %q", tt.src, err, tt.msg)
		}
	}
}
