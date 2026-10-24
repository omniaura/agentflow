package afanthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// fakeClaude streams each scripted answer in turn as server-sent events and
// records the request bodies.
type fakeClaude struct {
	mu      sync.Mutex
	answers [][]string // Each answer is a list of SSE event payloads.
	bodies  []map[string]any
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/v1/messages" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.bodies = append(f.bodies, body)
	if len(f.bodies) > len(f.answers) {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"no more answers"}}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range f.answers[len(f.bodies)-1] {
		var typ struct{ Type string }
		json.Unmarshal([]byte(ev), &typ)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, ev)
	}
}

// answer builds the events of one streamed message from content blocks,
// given as JSON objects with their full content.
func answer(stop string, blocks ...string) []string {
	evs := []string{`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":100,"cache_read_input_tokens":40,"cache_creation_input_tokens":10,"output_tokens":1}}}`}
	for i, b := range blocks {
		var block map[string]any
		json.Unmarshal([]byte(b), &block)
		start := map[string]any{"type": block["type"]}
		var delta map[string]any
		switch block["type"] {
		case "text":
			start["text"] = ""
			delta = map[string]any{"type": "text_delta", "text": block["text"]}
		case "thinking":
			start["thinking"], start["signature"] = "", ""
			delta = map[string]any{"type": "thinking_delta", "thinking": block["thinking"]}
		case "tool_use":
			start["id"], start["name"], start["input"] = block["id"], block["name"], map[string]any{}
			in, _ := json.Marshal(block["input"])
			delta = map[string]any{"type": "input_json_delta", "partial_json": string(in)}
		}
		s, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": i, "content_block": start})
		d, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": i, "delta": delta})
		evs = append(evs, string(s), string(d))
		if block["type"] == "thinking" {
			sig, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": block["signature"]}})
			evs = append(evs, string(sig))
		}
		evs = append(evs, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
	}
	return append(evs,
		fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":20}}`, stop),
		`{"type":"message_stop"}`)
}

func newModel(t *testing.T, f *fakeClaude) *Model {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	return &Model{Client: &c}
}

var lookupSchema = json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`)

func TestAgentLoopOnClaude(t *testing.T) {
	f := &fakeClaude{answers: [][]string{
		answer("tool_use",
			`{"type":"thinking","thinking":"Look both orders up.","signature":"sig-1"}`,
			`{"type":"text","text":"Checking."}`,
			`{"type":"tool_use","id":"toolu_1","name":"lookup_order","input":{"order_id":"A-1"}}`,
			`{"type":"tool_use","id":"toolu_2","name":"lookup_order","input":{"order_id":"B-2"}}`),
		answer("end_turn", `{"type":"text","text":"A-1 is late; B-2 doesn't exist."}`),
	}}
	loop := afagent.Loop{
		Model:  newModel(t, f),
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
	if res.Final != "A-1 is late; B-2 doesn't exist." {
		t.Errorf("final = %q", res.Final)
	}
	if res.Usage != (afagent.Usage{InputTokens: 300, CachedInputTokens: 80, OutputTokens: 40}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	first := res.Messages[1]
	if first.Content != "Checking." || len(first.ToolCalls) != 2 || first.ToolCalls[0].Arguments != `{"order_id":"A-1"}` {
		t.Errorf("first answer = %+v", first)
	}

	req := f.bodies[0]
	if req["model"] != DefaultModel || req["max_tokens"] != float64(DefaultMaxTokens) || req["stream"] != true {
		t.Errorf("model, max_tokens, stream = %v, %v, %v", req["model"], req["max_tokens"], req["stream"])
	}
	if _, ok := req["cache_control"]; !ok {
		t.Error("request does not turn on prompt caching")
	}
	tool := req["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if tool["description"] != "Load an order." || schema["additionalProperties"] != false || schema["required"].([]any)[0] != "order_id" {
		t.Errorf("tool = %v", tool)
	}
	if sys := req["system"].([]any)[0].(map[string]any); sys["text"] != "You are support." {
		t.Errorf("system = %v", sys)
	}

	// The second turn sends Claude's blocks back as they were, thinking and
	// signature included, and both tool results in one user message.
	if testing.Verbose() {
		b, _ := json.MarshalIndent(f.bodies[1]["messages"], "", " ")
		t.Logf("%s", b)
	}
	msgs := f.bodies[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("second request has %d messages: %v", len(msgs), msgs)
	}
	blocks := msgs[1].(map[string]any)["content"].([]any)
	thinking := blocks[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["signature"] != "sig-1" || thinking["thinking"] != "Look both orders up." {
		t.Errorf("thinking block = %v", thinking)
	}
	results := msgs[2].(map[string]any)
	if results["role"] != "user" || len(results["content"].([]any)) != 2 {
		t.Fatalf("tool results = %v", results)
	}
	r2 := results["content"].([]any)[1].(map[string]any)
	if r2["type"] != "tool_result" || r2["tool_use_id"] != "toolu_2" || r2["is_error"] != true {
		t.Errorf("second result = %v", r2)
	}
}

func TestForeignAssistantMessages(t *testing.T) {
	// A conversation started on another provider has no Claude blocks: its
	// assistant messages are rebuilt from text and tool calls.
	msgs, err := messages([]afagent.Message{
		{Role: afagent.User, Content: "hi"},
		{Role: afagent.Assistant, Content: "Looking.", ToolCalls: []afagent.ToolCall{{ID: "c1", Name: "look", Arguments: ""}},
			Native: &afagent.Native{Provider: "openai", Data: json.RawMessage(`{"bogus":true}`)}},
		{Role: afagent.Tool, ToolCallID: "c1", Content: "found"},
		{Role: afagent.User, Content: "thanks"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(msgs)
	for _, want := range []string{
		`{"text":"Looking.","type":"text"}`,
		`{"id":"c1","input":{},"name":"look","type":"tool_use"}`,
		`"tool_use_id":"c1"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	if len(msgs) != 4 {
		t.Errorf("got %d messages, want 4: a user message after tool results stays separate", len(msgs))
	}
}

func TestStopReasons(t *testing.T) {
	for _, tt := range []struct {
		stop string
		want error
	}{
		{"max_tokens", afagent.ErrTruncated},
		{"model_context_window_exceeded", afagent.ErrTruncated},
		{"refusal", afagent.ErrRefused},
	} {
		f := &fakeClaude{answers: [][]string{answer(tt.stop, `{"type":"text","text":"partial"}`)}}
		_, err := newModel(t, f).Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.stop, err, tt.want)
		}
	}
}

func TestParamsHookAndAPIError(t *testing.T) {
	f := &fakeClaude{}
	m := newModel(t, f)
	m.Model, m.MaxTokens = "claude-sonnet-5-5", 512
	m.Params = func(p *anthropic.MessageNewParams) {
		p.CacheControl = anthropic.CacheControlEphemeralParam{}
		p.Metadata.UserID = anthropic.String("u1")
	}
	_, err := m.Complete(context.Background(), afagent.Request{Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "no more answers") {
		t.Fatalf("got %v, want the API error", err)
	}
	req := f.bodies[0]
	if req["model"] != "claude-sonnet-5-5" || req["max_tokens"] != float64(512) || req["metadata"].(map[string]any)["user_id"] != "u1" {
		t.Errorf("request = %v", req)
	}
	if _, ok := req["cache_control"]; ok {
		t.Error("Params could not turn caching off")
	}
}

func TestBadToolSchema(t *testing.T) {
	_, err := (&Model{}).Complete(context.Background(), afagent.Request{Tools: []afagent.ToolSpec{{Name: "x", Schema: json.RawMessage(`nope`)}}})
	if err == nil || !strings.Contains(err.Error(), "tool x: schema") {
		t.Fatalf("got %v", err)
	}
}

func TestStructuredOutput(t *testing.T) {
	f := &fakeClaude{answers: [][]string{answer("end_turn", `{"type":"text","text":"{\"ok\":true}"}`)}}
	resp, err := newModel(t, f).Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{Role: afagent.User, Content: "judge"}},
		Output:   &afagent.OutputSpec{Name: "verdict", Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)},
	})
	if err != nil || resp.Message.Content != `{"ok":true}` {
		t.Fatalf("got %v, %+v", err, resp)
	}
	format := f.bodies[0]["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"].(map[string]any)["additionalProperties"] != false {
		t.Errorf("format = %v", format)
	}
}

func TestStreamsText(t *testing.T) {
	f := &fakeClaude{answers: [][]string{answer("end_turn",
		`{"type":"thinking","thinking":"private","signature":"s"}`,
		`{"type":"text","text":"Hello there"}`)}}
	var deltas []string
	resp, err := newModel(t, f).Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}},
		OnText:   func(s string) { deltas = append(deltas, s) },
	})
	if err != nil || resp.Message.Content != "Hello there" {
		t.Fatalf("got %v, %+v", err, resp)
	}
	// Text only: thinking stays out of the stream.
	if strings.Join(deltas, "") != "Hello there" {
		t.Errorf("deltas = %q", deltas)
	}
}
