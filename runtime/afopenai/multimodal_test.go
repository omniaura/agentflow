package afopenai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/omniaura/agentflow/runtime/afagent"
)

func TestOpenAIMultimodalPartsNonStrictAndPromptCacheKey(t *testing.T) {
	f := &fakeOpenAI{answers: []string{
		completion("stop", `{"role":"assistant","content":"{\"ok\":true}","refusal":null}`),
	}}
	m := newModel(t, f)
	resp, err := m.Complete(context.Background(), afagent.Request{
		PromptCacheKey: "workflow-step-1",
		Output: &afagent.OutputSpec{
			Name:      "step_output",
			Schema:    json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
			NonStrict: true,
		},
		Messages: []afagent.Message{{
			Role:    afagent.User,
			Content: "Inspect this screenshot and invoice:",
			Parts: []afagent.Part{
				{Kind: afagent.PartImage, URL: "https://example.com/screen.png"},
				{Kind: afagent.PartImage, Data: []byte("pngbytes"), MIMEType: "image/png"},
				{Kind: afagent.PartFile, Data: []byte("%PDF-1.4"), MIMEType: "application/pdf", Filename: "invoice.pdf"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", resp.FinishReason)
	}
	body := f.bodies[0]
	if body["prompt_cache_key"] != "workflow-step-1" {
		t.Fatalf("prompt_cache_key = %v", body["prompt_cache_key"])
	}
	rf := body["response_format"].(map[string]any)["json_schema"].(map[string]any)
	if rf["strict"] != false {
		t.Fatalf("strict = %v, want false", rf["strict"])
	}
	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("content parts = %d, want 4: %v", len(content), content)
	}
	if content[1].(map[string]any)["type"] != "image_url" {
		t.Errorf("part 1 = %v", content[1])
	}
	img2 := content[2].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(img2, "data:image/png;base64,") {
		t.Errorf("part 2 url = %q", img2)
	}
	file3 := content[3].(map[string]any)["file"].(map[string]any)
	if file3["filename"] != "invoice.pdf" || !strings.HasPrefix(file3["file_data"].(string), "data:application/pdf;base64,") {
		t.Errorf("part 3 file = %v", file3)
	}
}

func TestResponsesModelMultimodalPartsNonStrictAndCacheKey(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"ok\":true}"}]}],"usage":{"input_tokens":10,"output_tokens":5,"input_tokens_details":{"cached_tokens":4}}}`)
	}))
	t.Cleanup(srv.Close)
	c := openai.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	m := &ResponsesModel{Client: &c, Model: "gpt-5"}
	resp, err := m.Complete(context.Background(), afagent.Request{
		PromptCacheKey: "resp-cache-1",
		Output: &afagent.OutputSpec{
			Name:      "step_output",
			Schema:    json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
			NonStrict: true,
		},
		Messages: []afagent.Message{{
			Role: afagent.User,
			Parts: []afagent.Part{
				{Kind: afagent.PartText, Text: "Look"},
				{Kind: afagent.PartImage, URL: "https://example.com/s.png"},
				{Kind: afagent.PartFile, Data: []byte("%PDF-1.4"), MIMEType: "application/pdf", Filename: "doc.pdf"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if gotBody["prompt_cache_key"] != "resp-cache-1" {
		t.Fatalf("prompt_cache_key = %v", gotBody["prompt_cache_key"])
	}
	format := gotBody["text"].(map[string]any)["format"].(map[string]any)
	if format["strict"] != false {
		t.Fatalf("strict = %v", format["strict"])
	}
	content := gotBody["input"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 3 || content[1].(map[string]any)["type"] != "input_image" || content[2].(map[string]any)["type"] != "input_file" {
		t.Fatalf("input content = %v", content)
	}
}

func TestOpenAIChatRejectsRemoteFileURLAndResponsesFinishReasons(t *testing.T) {
	f := &fakeOpenAI{answers: []string{
		completion("stop", `{"role":"assistant","content":"ok","refusal":null}`),
	}}
	m := newModel(t, f)
	// data: URL is accepted in Chat Completions.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartFile, URL: "data:application/pdf;base64,JVBERg==", Filename: "a.pdf"}},
		}},
	}); err != nil {
		t.Fatalf("data URI rejected: %v", err)
	}
	// Remote https:// URL is rejected locally in Chat Completions.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartFile, URL: "https://example.com/a.pdf", Filename: "a.pdf"}},
		}},
	}); err == nil {
		t.Fatal("expected error for remote file URL in Chat Completions")
	}

	// Responses API sets FinishReason="tool_calls" for function calls and "" on refusal.
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	c := openai.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	rm := &ResponsesModel{Client: &c, Model: "gpt-5"}

	reply = `{"id":"resp_tc","object":"response","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":5,"output_tokens":3}}`
	resp, err := rm.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}},
	})
	if err != nil || resp.FinishReason != "tool_calls" {
		t.Fatalf("tool_calls resp = %+v, err = %v", resp, err)
	}

	reply = `{"id":"resp_ref","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"nope"}]}],"usage":{"input_tokens":5,"output_tokens":3}}`
	resp, err = rm.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{Role: afagent.User, Content: "hi"}},
	})
	if err == nil || resp.FinishReason != "" {
		t.Fatalf("refusal resp.FinishReason = %q, err = %v", resp.FinishReason, err)
	}

	// Both Chat Completions and Responses reject user messages whose parts
	// produce zero content items.
	emptyPartsMsg := []afagent.Message{{Role: afagent.User, Parts: []afagent.Part{{Kind: afagent.PartText, Text: ""}}}}
	if _, err := m.Complete(context.Background(), afagent.Request{Messages: emptyPartsMsg}); err == nil {
		t.Fatal("expected Chat Completions error for empty user parts")
	}
	if _, err := rm.Complete(context.Background(), afagent.Request{Messages: emptyPartsMsg}); err == nil {
		t.Fatal("expected Responses error for empty user parts")
	}

	// Omitted file MIMEType defaults to application/pdf in both Chat Completions
	// and Responses adapters.
	fDef := &fakeOpenAI{answers: []string{completion("stop", `{"role":"assistant","content":"ok","refusal":null}`)}}
	mDef := newModel(t, fDef)
	defaultPDF := []afagent.Message{{Role: afagent.User, Parts: []afagent.Part{{Kind: afagent.PartFile, Data: []byte("%PDF-1.4"), Filename: "doc.pdf"}}}}
	if _, err := mDef.Complete(context.Background(), afagent.Request{Messages: defaultPDF}); err != nil {
		t.Fatalf("Chat default PDF MIME: %v", err)
	}
	chatFile := fDef.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["file"].(map[string]any)
	if !strings.HasPrefix(chatFile["file_data"].(string), "data:application/pdf;base64,") {
		t.Fatalf("Chat file_data = %v, want application/pdf data URI", chatFile["file_data"])
	}

	var respDefBody map[string]any
	srvDef := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &respDefBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_def","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	}))
	t.Cleanup(srvDef.Close)
	cDef := openai.NewClient(option.WithBaseURL(srvDef.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	rmDef := &ResponsesModel{Client: &cDef, Model: "gpt-5"}
	if _, err := rmDef.Complete(context.Background(), afagent.Request{Messages: defaultPDF}); err != nil {
		t.Fatalf("Responses default PDF MIME: %v", err)
	}
	respFile := respDefBody["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(respFile["file_data"].(string), "data:application/pdf;base64,") {
		t.Fatalf("Responses file_data = %v, want application/pdf data URI", respFile["file_data"])
	}
}
