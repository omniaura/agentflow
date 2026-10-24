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

	"github.com/omniaura/agentflow/runtime/afagent"
)

// fakeOpenAI answers chat completions with each scripted response in turn
// and records the request bodies.
type fakeOpenAI struct {
	mu      sync.Mutex
	answers []string
	bodies  []map[string]any
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/chat/completions" {
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
	w.Header().Set("Content-Type", "application/json")
	if len(f.bodies) > len(f.answers) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"no more answers","type":"invalid_request_error"}}`)
		return
	}
	io.WriteString(w, f.answers[len(f.bodies)-1])
}

func completion(finish, message string) string {
	return `{"id":"cc_1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"` + finish + `","logprobs":null,"message":` + message + `}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":64}}}`
}

func newModel(t *testing.T, f *fakeOpenAI) *Model {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := openai.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	return &Model{Client: &c, Model: "test-model"}
}

var lookupSchema = json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`)

func TestAgentLoopOnOpenAI(t *testing.T) {
	f := &fakeOpenAI{answers: []string{
		completion("tool_calls", `{"role":"assistant","content":null,"refusal":null,"tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"lookup_order","arguments":"{\"order_id\":\"A-1\"}"}},
			{"id":"call_2","type":"function","function":{"name":"lookup_order","arguments":"{\"order_id\":\"B-2\"}"}}]}`),
		completion("stop", `{"role":"assistant","content":"A-1 is late.","refusal":null}`),
	}}
	m := newModel(t, f)
	m.MaxCompletionTokens = 1000
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

	req := f.bodies[0]
	if req["model"] != "test-model" || req["max_completion_tokens"] != float64(1000) {
		t.Errorf("request = %v", req)
	}
	fn := req["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "lookup_order" || fn["description"] != "Load an order." || fn["parameters"].(map[string]any)["additionalProperties"] != false {
		t.Errorf("tool = %v", fn)
	}

	msgs := f.bodies[1]["messages"].([]any)
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool,tool" {
		t.Fatalf("roles = %v", roles)
	}
	calls := msgs[2].(map[string]any)["tool_calls"].([]any)
	if len(calls) != 2 || calls[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{"order_id":"A-1"}` {
		t.Errorf("assistant tool calls = %v", calls)
	}
	if _, ok := msgs[2].(map[string]any)["content"]; ok {
		t.Errorf("assistant message without text sent content: %v", msgs[2])
	}
	if r := msgs[4].(map[string]any); r["tool_call_id"] != "call_2" || r["content"] != "Error: no order B-2" {
		t.Errorf("failed result = %v", r)
	}
}

func TestFinishReasons(t *testing.T) {
	for _, tt := range []struct {
		finish, message string
		want            error
	}{
		{"length", `{"role":"assistant","content":"part","refusal":null}`, afagent.ErrTruncated},
		{"content_filter", `{"role":"assistant","content":"","refusal":null}`, afagent.ErrRefused},
		{"stop", `{"role":"assistant","content":null,"refusal":"I can't help with that."}`, afagent.ErrRefused},
	} {
		f := &fakeOpenAI{answers: []string{completion(tt.finish, tt.message)}}
		_, err := newModel(t, f).Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.finish, err, tt.want)
		}
	}
}

func TestParamsHookAndErrors(t *testing.T) {
	f := &fakeOpenAI{}
	m := newModel(t, f)
	m.Params = func(p *openai.ChatCompletionNewParams) { p.ParallelToolCalls = openai.Bool(false) }
	_, err := m.Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
	// The SDK's error stays reachable, with the status and message.
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.Message != "no more answers" {
		t.Fatalf("got %v, want the API error", err)
	}
	if f.bodies[0]["parallel_tool_calls"] != false {
		t.Errorf("Params not applied: %v", f.bodies[0])
	}
	if _, err := (&Model{}).Complete(context.Background(), afagent.Request{}); err == nil || !strings.Contains(err.Error(), "Model.Model is empty") {
		t.Errorf("empty model: %v", err)
	}
	if _, err := m.Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: "system"}}}); err == nil || !strings.Contains(err.Error(), `unknown role "system"`) {
		t.Errorf("bad role: %v", err)
	}
}
