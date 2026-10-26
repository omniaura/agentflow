package afopenai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// fakeResponses answers the Responses API with each scripted response in
// turn, or streams the scripted events, and records the request bodies.
type fakeResponses struct {
	mu      sync.Mutex
	answers []string
	events  []string // Streamed to requests with "stream": true.
	bodies  []map[string]any
}

func (f *fakeResponses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/responses" {
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.bodies = append(f.bodies, body)
	if body["stream"] == true {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range f.events {
			io.WriteString(w, "data: "+e+"\n\n")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(f.bodies) > len(f.answers) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"no more answers","type":"invalid_request_error"}}`)
		return
	}
	io.WriteString(w, f.answers[len(f.bodies)-1])
}

// responseJSON is a Responses API response with the given status and output
// items.
func responseJSON(status, output string) string {
	return `{"id":"resp_1","object":"response","created_at":1,"status":"` + status + `","model":"m","output":[` + output + `],"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":64},"output_tokens":20,"output_tokens_details":{"reasoning_tokens":8},"total_tokens":120}}`
}

func newResponsesModel(t *testing.T, f *fakeResponses) *ResponsesModel {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := openai.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	return &ResponsesModel{Client: &c, Model: "test-model"}
}

const (
	reasoningItem = `{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"Look both up."}],"encrypted_content":"enc-1"}`
	messageItem   = `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A-1 is late.","annotations":[]}]}`
)

// inputTypes lists the input items' types, or roles for the easy messages
// that have no type.
func inputTypes(input []any) string {
	var types []string
	for _, it := range input {
		m := it.(map[string]any)
		s, _ := m["type"].(string)
		if s == "" {
			s, _ = m["role"].(string)
		}
		types = append(types, s)
	}
	return strings.Join(types, ",")
}

func TestAgentLoopOnResponses(t *testing.T) {
	f := &fakeResponses{answers: []string{
		responseJSON("completed", reasoningItem+`,
			{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"lookup_order","arguments":"{\"order_id\":\"A-1\"}"},
			{"id":"fc_2","type":"function_call","status":"completed","call_id":"call_2","name":"lookup_order","arguments":"{\"order_id\":\"B-2\"}"}`),
		responseJSON("completed", messageItem),
	}}
	m := newResponsesModel(t, f)
	m.MaxOutputTokens = 1000
	m.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffortMedium, Summary: shared.ReasoningSummaryAuto}
	loop := afagent.Loop{
		Model:  m,
		System: "You are support.",
		Tools:  []afagent.ToolSpec{{Name: "lookup_order", Description: "Load an order.", Schema: lookupSchema}},
		Dispatch: func(ctx context.Context, c afagent.ToolCall) (string, error) {
			if strings.Contains(c.Arguments, "B-2") {
				return "", errors.New("no order B-2")
			}
			return "late", nil
		},
	}
	res, err := loop.Run(context.Background(), []afagent.Message{{Role: afagent.User, Content: "Where are A-1 and B-2?"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "A-1 is late." || res.Usage != (afagent.Usage{InputTokens: 200, CachedInputTokens: 128, OutputTokens: 40}) {
		t.Errorf("res = %+v", res)
	}
	if c := res.Messages[1].ToolCalls; len(c) != 2 || c[1].ID != "call_2" || c[1].Arguments != `{"order_id":"B-2"}` {
		t.Errorf("tool calls = %+v", c)
	}
	if n := res.Messages[1].Native; n == nil || n.Provider != NativeResponses {
		t.Errorf("native = %+v", n)
	}

	req := f.bodies[0]
	if req["model"] != "test-model" || req["max_output_tokens"] != float64(1000) || req["store"] != false || req["instructions"] != "You are support." {
		t.Errorf("request = %v", req)
	}
	if inc := req["include"].([]any); len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
		t.Errorf("include = %v", inc)
	}
	if r := req["reasoning"].(map[string]any); r["effort"] != "medium" || r["summary"] != "auto" {
		t.Errorf("reasoning = %v", r)
	}
	tool := req["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "lookup_order" || tool["description"] != "Load an order." || tool["strict"] != false || tool["parameters"].(map[string]any)["additionalProperties"] != false {
		t.Errorf("tool = %v", tool)
	}

	// The second turn sends the output items back as they were, reasoning
	// included, then each tool's output.
	input := f.bodies[1]["input"].([]any)
	if got := inputTypes(input); got != "user,reasoning,function_call,function_call,function_call_output,function_call_output" {
		t.Fatalf("input = %v", got)
	}
	if r := input[1].(map[string]any); r["encrypted_content"] != "enc-1" || r["id"] != "rs_1" {
		t.Errorf("reasoning item = %v", r)
	}
	if o := input[5].(map[string]any); o["call_id"] != "call_2" || o["output"] != "Error: no order B-2" {
		t.Errorf("failed output = %v", o)
	}
}

func TestResponsesReplay(t *testing.T) {
	// Reasoning without encrypted content can't go back, since nothing is
	// stored; a message from another provider is rebuilt from its text and
	// tool calls.
	native, _ := json.Marshal([]json.RawMessage{
		json.RawMessage(`{"id":"rs_1","type":"reasoning","summary":[]}`),
		json.RawMessage(messageItem),
	})
	f := &fakeResponses{answers: []string{responseJSON("completed", messageItem)}}
	_, err := newResponsesModel(t, f).Complete(context.Background(), afagent.Request{Messages: []afagent.Message{
		{Role: afagent.User, Content: "hi"},
		{Role: afagent.Assistant, Native: &afagent.Native{Provider: NativeResponses, Data: native}},
		{Role: afagent.User, Content: "and B-2?"},
		{Role: afagent.Assistant, Content: "Checking.", ToolCalls: []afagent.ToolCall{{ID: "toolu_1", Name: "lookup_order"}},
			Native: &afagent.Native{Provider: "anthropic", Data: json.RawMessage(`[]`)}},
		{Role: afagent.Tool, ToolCallID: "toolu_1", Content: "late"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := f.bodies[0]["input"].([]any)
	if got := inputTypes(input); got != "user,message,user,assistant,function_call,function_call_output" {
		t.Fatalf("input = %v", got)
	}
	if fc := input[4].(map[string]any); fc["call_id"] != "toolu_1" || fc["arguments"] != "{}" || fc["name"] != "lookup_order" {
		t.Errorf("rebuilt call = %v", fc)
	}
	if a := input[3].(map[string]any); a["role"] != "assistant" || a["content"] != "Checking." {
		t.Errorf("rebuilt text = %v", a)
	}
	if _, ok := f.bodies[0]["include"]; ok {
		t.Errorf("include set without reasoning: %v", f.bodies[0]["include"])
	}
}

func TestResponsesStatuses(t *testing.T) {
	for _, tt := range []struct {
		name, answer string
		want         error
	}{
		{"truncated", `{"id":"r","object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`, afagent.ErrTruncated},
		{"filtered", `{"id":"r","object":"response","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, afagent.ErrRefused},
		{"failed", `{"id":"r","object":"response","status":"failed","error":{"code":"server_error","message":"boom"},"output":[]}`, nil},
		{"cancelled", `{"id":"r","object":"response","status":"cancelled","output":[]}`, nil},
		{"refusal", responseJSON("completed", `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I can't help with that."}]}`), afagent.ErrRefused},
	} {
		f := &fakeResponses{answers: []string{tt.answer}}
		_, err := newResponsesModel(t, f).Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestResponsesStreams(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 5, "response": json.RawMessage(responseJSON("completed", reasoningItem+","+messageItem))})
	f := &fakeResponses{events: []string{
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
		`{"type":"response.reasoning_summary_text.delta","sequence_number":1,"item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Look both"}`,
		`{"type":"response.reasoning_summary_text.delta","sequence_number":2,"item_id":"rs_1","output_index":0,"summary_index":0,"delta":" up."}`,
		`{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","output_index":1,"content_index":0,"delta":"A-1 is"}`,
		`{"type":"response.output_text.delta","sequence_number":4,"item_id":"msg_1","output_index":1,"content_index":0,"delta":" late."}`,
		string(completed),
	}}
	var events []string
	loop := afagent.Loop{
		Model: newResponsesModel(t, f),
		OnEvent: func(e afagent.Event) {
			if name, data, ok := afagent.ClientEvent(e); ok {
				events = append(events, name+":"+data.(map[string]string)["text"])
			}
		},
	}
	res, err := loop.Run(context.Background(), []afagent.Message{{Role: afagent.User, Content: "where is A-1"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "reasoning:Look both|reasoning: up.|text:A-1 is|text: late."
	if strings.Join(events, "|") != want || res.Final != "A-1 is late." {
		t.Errorf("events %q, final %q", events, res.Final)
	}
	if res.Usage != (afagent.Usage{InputTokens: 100, CachedInputTokens: 64, OutputTokens: 20}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	if f.bodies[0]["stream"] != true {
		t.Errorf("request = %v", f.bodies[0])
	}

	for _, tt := range []struct{ event, want string }{
		{`{"type":"response.failed","sequence_number":1,"response":{"id":"r","object":"response","status":"failed","error":{"code":"server_error","message":"boom"},"output":[]}}`, "response failed: boom"},
		// A gateway may leave the error out.
		{`{"type":"response.failed","sequence_number":1,"response":{"id":"r","object":"response","status":"failed","output":[]}}`, "response failed: no reason given"},
		{`{"type":"error","sequence_number":1,"code":"server_error","message":"boom","param":null}`, "stream error: boom"},
	} {
		f := &fakeResponses{events: []string{tt.event}}
		_, err := newResponsesModel(t, f).Complete(context.Background(), afagent.Request{
			Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}},
			OnText:   func(string) {},
		})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("got %v, want %q", err, tt.want)
		}
	}
	f = &fakeResponses{}
	if _, err := newResponsesModel(t, f).Complete(context.Background(), afagent.Request{OnReasoning: func(string) {}}); err == nil || !strings.Contains(err.Error(), "without a response") {
		t.Errorf("empty stream: %v", err)
	}
}

func TestResponsesRequestShape(t *testing.T) {
	tools := []afagent.ToolSpec{{Name: "x", Schema: json.RawMessage(`{"type":"object","properties":{}}`)}}
	f := &fakeResponses{answers: []string{responseJSON("completed", `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}`)}}
	m := newResponsesModel(t, f)
	m.Params = func(p *responses.ResponseNewParams) { p.ParallelToolCalls = openai.Bool(false) }
	resp, err := m.Complete(context.Background(), afagent.Request{
		Messages:    []afagent.Message{{Role: afagent.User, Content: "judge"}},
		Tools:       tools,
		NoToolCalls: true,
		Output:      &afagent.OutputSpec{Name: "verdict", Description: "The verdict.", Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)},
	})
	if err != nil || resp.Message.Content != `{"ok":true}` {
		t.Fatalf("got %v, %+v", err, resp)
	}
	body := f.bodies[0]
	format := body["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "verdict" || format["strict"] != true || format["description"] != "The verdict." {
		t.Errorf("format = %v", format)
	}
	if body["tool_choice"] != "none" || body["parallel_tool_calls"] != false {
		t.Errorf("body = %v", body)
	}

	// Errors: the API's, with its status; an empty model; an unknown role.
	_, err = m.Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
	var me *afagent.ModelError
	if !errors.As(err, &me) || me.Status != http.StatusBadRequest {
		t.Errorf("got %v, want the API error", err)
	}
	if _, err := (&ResponsesModel{}).Complete(context.Background(), afagent.Request{}); err == nil || !strings.Contains(err.Error(), "Model is empty") {
		t.Errorf("empty model: %v", err)
	}
	if _, err := m.Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: "system"}}}); err == nil || !strings.Contains(err.Error(), `unknown role "system"`) {
		t.Errorf("bad role: %v", err)
	}
}
