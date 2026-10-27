package outputs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	got, err := TaskValueCheckSchema.Decode(`{"repeatable":true,"confidence":0.9,"why":"They said every Monday.","title":null,"cadence":"weekly","sources":[{"name":"GitHub","kind":"app"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Repeatable || got.Confidence != 0.9 || got.Title != "" || got.Cadence != TaskValueCheckCadenceWeekly || got.Sources[0].Kind != "app" {
		t.Fatalf("got %+v", got)
	}
	// Strict outputs send null for an optional field, never omit it.
	_, err = TaskValueCheckSchema.Decode(`{"repeatable":true,"confidence":1,"why":"","cadence":null,"sources":[{"name":"x","kind":"web"}]}`)
	if err == nil || err.Error() != "task_value_check: invalid output: title: missing; send null when there is no value" {
		t.Fatalf("got %v", err)
	}
	_, err = TaskValueCheckSchema.Decode(`{"repeatable":"yes","confidence":1,"why":"","title":null,"cadence":null,"sources":[]}`)
	if err == nil || err.Error() != "task_value_check: invalid output: repeatable: expected a boolean, got a string" {
		t.Fatalf("got %v", err)
	}
}

func TestStrictSchema(t *testing.T) {
	var schema struct {
		Required             []string                   `json:"required"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(TaskValueCheckSchema.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	// Strict mode: every field is required, optional ones are nullable.
	if strings.Join(schema.Required, ",") != "repeatable,confidence,why,title,cadence,sources" || schema.AdditionalProperties {
		t.Fatalf("schema = %s", TaskValueCheckSchema.Schema)
	}
	if !strings.Contains(string(schema.Properties["title"]), `{"type":"null"}`) || strings.Contains(string(schema.Properties["why"]), "null") {
		t.Fatalf("nullability: title %s, why %s", schema.Properties["title"], schema.Properties["why"])
	}
	var format struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(TaskValueCheckSchema.OpenAI(), &format); err != nil || format.Type != "json_schema" || format.JSONSchema.Name != "task_value_check" || !format.JSONSchema.Strict {
		t.Fatalf("openai = %s (%v)", TaskValueCheckSchema.OpenAI(), err)
	}
	var anthropic struct {
		Type   string          `json:"type"`
		Schema json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal(TaskValueCheckSchema.Anthropic(), &anthropic); err != nil || anthropic.Type != "json_schema" || string(anthropic.Schema) != string(TaskValueCheckSchema.Schema) {
		t.Fatalf("anthropic = %s (%v)", TaskValueCheckSchema.Anthropic(), err)
	}
}

func TestFieldsInPrompt(t *testing.T) {
	prompt := (&Judge{Fields: TaskValueCheckSchema.Fields}).String()
	for _, want := range []string{
		`- "repeatable" (true or false): Would the person plausibly want this exact task done again, on new data?`,
		`- "title" (string, or null): A short name for the task, at most 60 characters.`,
		`- "cadence" (one of "daily", "weekly", "monthly", or null): How often the task would run.`,
		`- "sources" (list of object)`,
		`- "sources[].kind" (one of "app", "web", "file")`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, prompt)
		}
	}
}
