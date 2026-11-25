package afanthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
)

func TestAnthropicMultimodalPartsAndCacheCreationUsage(t *testing.T) {
	f := &fakeClaude{answers: [][]string{
		answer("end_turn", `{"type":"text","text":"I see the screenshot and PDF."}`),
	}}
	m := newModel(t, f)
	resp, err := m.Complete(context.Background(), afagent.Request{
		System: "Inspect attachments.",
		Messages: []afagent.Message{
			{
				Role:    afagent.User,
				Content: "Here are two files:",
				Parts: []afagent.Part{
					{Kind: afagent.PartImage, URL: "https://example.com/screen.png"},
					{Kind: afagent.PartFile, Data: []byte("%PDF-1.4"), MIMEType: "application/pdf", Filename: "invoice.pdf"},
				},
			},
			{
				Role:      afagent.Assistant,
				ToolCalls: []afagent.ToolCall{{ID: "toolu_1", Name: "computer_browser", Arguments: `{"action":"screenshot"}`}},
			},
			{
				Role:       afagent.Tool,
				ToolCallID: "toolu_1",
				Content:    `{"ok":true}`,
				Parts:      []afagent.Part{{Kind: afagent.PartImage, Data: []byte("png"), MIMEType: "image/png"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.FinishReason != "end_turn" {
		t.Fatalf("FinishReason = %q", resp.FinishReason)
	}
	if resp.Usage.CacheCreationInputTokens != 10 || resp.Usage.CachedInputTokens != 40 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	msgs := f.bodies[0]["messages"].([]any)
	userContent := msgs[0].(map[string]any)["content"].([]any)
	if len(userContent) != 3 {
		t.Fatalf("user content len = %d: %v", len(userContent), userContent)
	}
	if userContent[1].(map[string]any)["type"] != "image" || userContent[2].(map[string]any)["type"] != "document" {
		t.Fatalf("user parts = %v", userContent)
	}
	toolBlock := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	toolContent := toolBlock["content"].([]any)
	if len(toolContent) != 2 || toolContent[0].(map[string]any)["type"] != "text" || toolContent[1].(map[string]any)["type"] != "image" {
		t.Fatalf("tool content = %v", toolContent)
	}
}

func TestAnthropicFileDataURIMimeAndToolDocumentFallback(t *testing.T) {
	f := &fakeClaude{answers: [][]string{
		answer("end_turn", `{"type":"text","text":"ok"}`),
	}}
	m := newModel(t, f)
	_, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{
			{
				Role: afagent.User,
				Parts: []afagent.Part{
					{Kind: afagent.PartFile, URL: "data:text/plain;base64,SGVsbG8=", Filename: "note.txt"},
				},
			},
			{
				Role:      afagent.Assistant,
				ToolCalls: []afagent.ToolCall{{ID: "toolu_doc", Name: "fetch"}},
			},
			{
				Role:       afagent.Tool,
				ToolCallID: "toolu_doc",
				Parts: []afagent.Part{
					{Kind: afagent.PartFile, Data: []byte("%PDF-1.4"), MIMEType: "application/pdf", Filename: "doc.pdf"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	msgs := f.bodies[0]["messages"].([]any)
	doc := msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	src := doc["source"].(map[string]any)
	if src["type"] != "text" || src["data"] != "Hello" {
		t.Fatalf("text data URI source = %v, want plain text Hello", src)
	}
	toolBlock := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	toolPart := toolBlock["content"].([]any)[0].(map[string]any)
	if toolPart["type"] != "text" {
		t.Fatalf("tool document part = %v, want text placeholder", toolPart)
	}

	// Non-base64 data URI must fail locally instead of being sent as a PDF URL.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartFile, URL: "data:text/plain,hello"}},
		}},
	}); err == nil {
		t.Fatal("expected error for non-base64 file data URI")
	}

	// Non-base64 image data URI must also fail locally.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartImage, URL: "data:image/png,notbase64"}},
		}},
	}); err == nil {
		t.Fatal("expected error for non-base64 image data URI")
	}

	// User message whose parts are all empty must fail locally instead of sending empty content.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartText, Text: ""}},
		}},
	}); err == nil {
		t.Fatal("expected error for user message with no content blocks")
	}

	// Tool result whose parts are all empty must fail locally.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{
			{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "t1", Name: "f"}}},
			{Role: afagent.Tool, ToolCallID: "t1", Parts: []afagent.Part{{Kind: afagent.PartText, Text: ""}}},
		},
	}); err == nil {
		t.Fatal("expected error for tool result with no content blocks")
	}

	// Unsupported image MIME type fails locally; image/jpg normalizes to image/jpeg;
	// URL image inside tool_result degrades to a text placeholder.
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role:  afagent.User,
			Parts: []afagent.Part{{Kind: afagent.PartImage, Data: []byte("svg"), MIMEType: "image/svg+xml"}},
		}},
	}); err == nil {
		t.Fatal("expected error for unsupported image MIME type")
	}
	f2 := &fakeClaude{answers: [][]string{answer("end_turn", `{"type":"text","text":"ok"}`)}}
	m2 := newModel(t, f2)
	if _, err := m2.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{
			{Role: afagent.User, Parts: []afagent.Part{{Kind: afagent.PartImage, Data: []byte("jpg"), MIMEType: "image/jpg"}}},
			{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "t2", Name: "shot"}}},
			{Role: afagent.Tool, ToolCallID: "t2", Parts: []afagent.Part{{Kind: afagent.PartImage, URL: "https://example.com/a.png"}}},
		},
	}); err != nil {
		t.Fatalf("normalized jpg + tool URL image failed: %v", err)
	}
	m2Msgs := f2.bodies[0]["messages"].([]any)
	imgSrc := m2Msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any)["source"].(map[string]any)
	if imgSrc["media_type"] != "image/jpeg" {
		t.Fatalf("media_type = %v, want image/jpeg", imgSrc["media_type"])
	}
	t2Part := m2Msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if t2Part["type"] != "text" {
		t.Fatalf("tool URL image part = %v, want text placeholder", t2Part)
	}

	// Binary / invalid UTF-8 non-PDF files must be rejected locally (raw bytes and data URI).
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role: afagent.User,
			Parts: []afagent.Part{{
				Kind:     afagent.PartFile,
				Data:     []byte{0xff, 0xfe, 0x00, 0x80},
				MIMEType: "application/octet-stream",
				Filename: "blob.bin",
			}},
		}},
	}); err == nil {
		t.Fatal("expected error for invalid UTF-8 non-PDF file bytes")
	}
	if _, err := m.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role: afagent.User,
			Parts: []afagent.Part{{
				Kind:     afagent.PartFile,
				URL:      "data:application/octet-stream;base64,//4AgA==",
				Filename: "blob.bin",
			}},
		}},
	}); err == nil {
		t.Fatal("expected error for invalid UTF-8 non-PDF file data URI")
	}

	// Empty MIMEType with plain-text data becomes a text document; %PDF- bytes
	// become a PDF document; URL-backed tool document omits "0 bytes".
	f3 := &fakeClaude{answers: [][]string{answer("end_turn", `{"type":"text","text":"ok"}`)}}
	m3 := newModel(t, f3)
	if _, err := m3.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{
			{
				Role: afagent.User,
				Parts: []afagent.Part{
					{Kind: afagent.PartFile, Data: []byte("plain notes"), Filename: "notes.txt"},
					{Kind: afagent.PartFile, Data: []byte("%PDF-1.7\n"), Filename: "scan.pdf"},
				},
			},
			{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "t3", Name: "doc"}}},
			{Role: afagent.Tool, ToolCallID: "t3", Parts: []afagent.Part{{Kind: afagent.PartFile, URL: "https://example.com/report.pdf", Filename: "report.pdf"}}},
		},
	}); err != nil {
		t.Fatalf("empty MIME / URL tool doc failed: %v", err)
	}
	m3Msgs := f3.bodies[0]["messages"].([]any)
	m3User := m3Msgs[0].(map[string]any)["content"].([]any)
	if src := m3User[0].(map[string]any)["source"].(map[string]any); src["type"] != "text" || src["data"] != "plain notes" {
		t.Fatalf("empty-MIME plain text source = %v, want text", src)
	}
	if src := m3User[1].(map[string]any)["source"].(map[string]any); src["type"] != "base64" || src["media_type"] != "application/pdf" {
		t.Fatalf("empty-MIME PDF source = %v, want base64 application/pdf", src)
	}
	t3Text := m3Msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(t3Text, "0 bytes") {
		t.Fatalf("URL-backed tool doc placeholder reported 0 bytes: %q", t3Text)
	}

	// Parameterized data URIs (e.g. ;charset=... or ;name=...) strip parameters
	// before classifying image and PDF media types.
	f4 := &fakeClaude{answers: [][]string{answer("end_turn", `{"type":"text","text":"ok"}`)}}
	m4 := newModel(t, f4)
	if _, err := m4.Complete(context.Background(), afagent.Request{
		Messages: []afagent.Message{{
			Role: afagent.User,
			Parts: []afagent.Part{
				{Kind: afagent.PartImage, URL: "data:image/png;charset=utf-8;base64,iVBORw0KGgo="},
				{Kind: afagent.PartFile, URL: "data:application/pdf;name=invoice.pdf;base64,JVBERi0xLjQK", Filename: "invoice.pdf"},
			},
		}},
	}); err != nil {
		t.Fatalf("parameterized data URIs failed: %v", err)
	}
	m4User := f4.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if src := m4User[0].(map[string]any)["source"].(map[string]any); src["media_type"] != "image/png" {
		t.Fatalf("parameterized image media_type = %v, want image/png", src["media_type"])
	}
	if src := m4User[1].(map[string]any)["source"].(map[string]any); src["type"] != "base64" || src["media_type"] != "application/pdf" {
		t.Fatalf("parameterized PDF source = %v, want base64 application/pdf", src)
	}
}
