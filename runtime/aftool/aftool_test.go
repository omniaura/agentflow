package aftool_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/aftool"
)

type filter struct {
	Since  string   `json:"since"`
	Models []string `json:"models,omitempty"`
}

type item struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
}

type args struct {
	View   string          `json:"view"`
	Note   string          `json:"note,omitempty"`
	Filter *filter         `json:"filter,omitempty"`
	Items  []item          `json:"items,omitempty"`
	Ratio  float64         `json:"ratio,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Extra  json.RawMessage `json:"extra,omitempty"`
	Meta   map[string]any  `json:"meta,omitempty"`
}

var def = &aftool.Def[args]{
	Name:        "manage_billing",
	Description: "Show a <billing> control.",
	Schema:      []byte(`{"type":"object","properties":{},"additionalProperties":false}`),
	MaxBytes:    512,
	Shape: aftool.Shape{Kind: aftool.Object, Fields: []aftool.Field{
		{Name: "view", Required: true, Shape: aftool.Shape{Kind: aftool.String, Enum: []string{"overview", "plans"}}},
		{Name: "note", Shape: aftool.Shape{Kind: aftool.String}},
		{Name: "filter", Shape: aftool.Shape{Kind: aftool.Object, Fields: []aftool.Field{
			{Name: "since", Required: true, Shape: aftool.Shape{Kind: aftool.String}},
			{Name: "models", Shape: aftool.Shape{Kind: aftool.Array, Items: &aftool.Shape{Kind: aftool.String}}},
		}}},
		{Name: "items", Shape: aftool.Shape{Kind: aftool.Array, Items: &aftool.Shape{Kind: aftool.Object, Fields: []aftool.Field{
			{Name: "sku", Required: true, Shape: aftool.Shape{Kind: aftool.String}},
			{Name: "qty", Required: true, Shape: aftool.Shape{Kind: aftool.Integer}},
		}}}},
		{Name: "ratio", Shape: aftool.Shape{Kind: aftool.Number}},
		{Name: "ok", Shape: aftool.Shape{Kind: aftool.Boolean}},
		{Name: "extra", Shape: aftool.Shape{Kind: aftool.Any}},
		{Name: "meta", Shape: aftool.Shape{Kind: aftool.Object}},
	}},
}

func TestDecode(t *testing.T) {
	got, err := def.Decode(` {"view":"plans","filter":{"since":"2026-01-01","models":["a","b"]},
		"items":[{"sku":"x","qty":2}],"ratio":0.5,"ok":true,"extra":[1,{"k":null}],"meta":{"any":"thing"},"note":null} `)
	if err != nil {
		t.Fatal(err)
	}
	want := args{
		View:   "plans",
		Filter: &filter{Since: "2026-01-01", Models: []string{"a", "b"}},
		Items:  []item{{Sku: "x", Qty: 2}},
		Ratio:  0.5,
		OK:     true,
		Extra:  json.RawMessage(`[1,{"k":null}]`),
		Meta:   map[string]any{"any": "thing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	for _, tt := range []struct{ name, in, path, msg string }{
		{"empty", ``, "view", "required"},
		{"missing required", `{}`, "view", "required"},
		{"null required", `{"view":null}`, "view", "required"},
		{"not in enum", `{"view":"topup"}`, "view", `"topup" is not one of overview, plans`},
		{"wrong type", `{"view":3}`, "view", "expected a string, got a number"},
		{"unknown property", `{"view":"plans","amount":5}`, "amount", "unknown property"},
		{"nested required", `{"view":"plans","filter":{}}`, "filter.since", "required"},
		{"nested unknown", `{"view":"plans","filter":{"since":"x","until":"y"}}`, "filter.until", "unknown property"},
		{"array item", `{"view":"plans","items":[{"sku":"a","qty":1},{"sku":"b","qty":1.5}]}`, "items[1].qty", "expected an integer, got 1.5"},
		{"array type", `{"view":"plans","items":{}}`, "items", "expected an array, got an object"},
		{"array elem type", `{"view":"plans","filter":{"since":"x","models":[1]}}`, "filter.models[0]", "expected a string"},
		{"object type", `{"view":"plans","meta":[]}`, "meta", "expected an object, got an array"},
		{"bool type", `{"view":"plans","ok":"yes"}`, "ok", "expected a boolean"},
		{"number type", `{"view":"plans","ratio":"1"}`, "ratio", "expected a number"},
		{"not an object", `[]`, "", "expected an object, got an array"},
		{"null arguments", `null`, "", "expected an object, got null"},
		{"null array item", `{"view":"plans","items":[null]}`, "items[0]", "expected an object, got null"},
		{"null string item", `{"view":"plans","filter":{"since":"x","models":[null]}}`, "filter.models[0]", "expected a string, got null"},
		{"invalid json", `{"view":`, "", "not valid JSON"},
		{"trailing data", `{"view":"plans"} {}`, "", "unexpected data after the JSON object"},
		{"too large", `{"note":"` + strings.Repeat("x", 600) + `"}`, "", "the limit is 512"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := def.Decode(tt.in)
			var ae *aftool.ArgsError
			if !errors.As(err, &ae) {
				t.Fatalf("got %v, want *ArgsError", err)
			}
			if ae.Tool != "manage_billing" || ae.Path != tt.path || !strings.Contains(ae.Msg, tt.msg) {
				t.Fatalf("got path %q msg %q, want path %q msg containing %q", ae.Path, ae.Msg, tt.path, tt.msg)
			}
			if !strings.HasPrefix(err.Error(), "manage_billing: invalid arguments: ") {
				t.Fatalf("error = %q", err)
			}
		})
	}
}

func TestDefaultMaxBytes(t *testing.T) {
	d := &aftool.Def[map[string]any]{Name: "free", Shape: aftool.Shape{Kind: aftool.Object}}
	if _, err := d.Decode(`{"a":"` + strings.Repeat("x", aftool.DefaultMaxBytes) + `"}`); err == nil {
		t.Fatal("want an error over DefaultMaxBytes")
	}
	got, err := d.Decode(`{"a":1}`)
	if err != nil || got["a"] != 1.0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestProviderFormats(t *testing.T) {
	var openai struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	raw := def.OpenAI()
	if err := json.Unmarshal(raw, &openai); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	if openai.Type != "function" || openai.Function.Name != def.Name || openai.Function.Description != def.Description || string(openai.Function.Parameters) != string(def.Schema) {
		t.Fatalf("openai = %s", raw)
	}
	if !strings.Contains(string(raw), "Show a <billing>") {
		t.Fatalf("description is HTML-escaped: %s", raw)
	}
	var anthropic struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	raw = def.Anthropic()
	if err := json.Unmarshal(raw, &anthropic); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	if anthropic.Name != def.Name || anthropic.Description != def.Description || string(anthropic.InputSchema) != string(def.Schema) {
		t.Fatalf("anthropic = %s", raw)
	}
}
