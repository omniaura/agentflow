package afcompact_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afcompact"
)

func TestEstimateMultimodalPartsBounded(t *testing.T) {
	// A 2 MiB PDF attached as raw bytes should estimate at FileTokens, not 500k tokens.
	pdf := make([]byte, 2<<20)
	msgs := []afagent.Message{{
		Role: afagent.User,
		Parts: []afagent.Part{
			{Kind: afagent.PartText, Text: "read invoice"},
			{Kind: afagent.PartImage, URL: "https://example.com/a.png"},
			{Kind: afagent.PartFile, Data: pdf, MIMEType: "application/pdf", Filename: "big.pdf"},
		},
	}}
	est := afcompact.Estimate(msgs)
	if est < afcompact.ImageTokens+afcompact.FileTokens || est > afcompact.ImageTokens+afcompact.FileTokens+100 {
		t.Fatalf("est = %d, want ~%d", est, afcompact.ImageTokens+afcompact.FileTokens)
	}

	// A large multi-page PDF or large text file scales beyond the fixed 2000-token floor
	// and trips BeforeTurn over-budget checks instead of slipping through uncompacted.
	multiPagePDF := []byte("%PDF-1.4\n" + strings.Repeat("1 0 obj << /Type /Page >> endobj\n", 10))
	pdfMsgs := []afagent.Message{{
		Role:  afagent.User,
		Parts: []afagent.Part{{Kind: afagent.PartFile, Data: multiPagePDF, MIMEType: "application/pdf", Filename: "ten.pdf"}},
	}}
	if got := afcompact.Estimate(pdfMsgs); got < 10*afcompact.FileTokens {
		t.Fatalf("10-page PDF est = %d, want >= %d", got, 10*afcompact.FileTokens)
	}
	bigTextMsgs := []afagent.Message{{
		Role:  afagent.User,
		Parts: []afagent.Part{{Kind: afagent.PartFile, Data: []byte(strings.Repeat("word ", 12_000)), MIMEType: "text/plain", Filename: "report.txt"}},
	}}
	if got := afcompact.Estimate(bigTextMsgs); got < 14_000 {
		t.Fatalf("60KB text file est = %d, want >= 14000", got)
	}
	comp := &afcompact.Compactor{Budget: 5000, At: 1.0}
	if _, err := comp.BeforeTurn(context.Background(), 1, bigTextMsgs); !errors.Is(err, afcompact.ErrOverBudget) {
		t.Fatalf("BeforeTurn on 60KB text PartFile err = %v, want ErrOverBudget", err)
	}
	// Explicit Part.EstimatedTokens overrides default heuristic.
	hintedMsgs := []afagent.Message{{
		Role:  afagent.User,
		Parts: []afagent.Part{{Kind: afagent.PartFile, URL: "https://example.com/huge.pdf", EstimatedTokens: 12_000}},
	}}
	if got := afcompact.Estimate(hintedMsgs); got < 12_000 {
		t.Fatalf("hinted file est = %d, want >= 12000", got)
	}

	// Compressed PDF object stream (/ObjStm + FlateDecode) with 8 page objects
	// must estimate multiple pages and trip BeforeTurn over-budget checks.
	var objStm bytes.Buffer
	zw := zlib.NewWriter(&objStm)
	_, _ = zw.Write([]byte(strings.Repeat("<< /Type /Page /Parent 2 0 R >>\n", 8)))
	_ = zw.Close()
	compressedPDF := append([]byte("%PDF-1.5\n1 0 obj << /Type /ObjStm /Filter /FlateDecode >>\nstream\n"), objStm.Bytes()...)
	compressedPDF = append(compressedPDF, []byte("\nendstream\nendobj\n")...)
	compPDFMsgs := []afagent.Message{{
		Role:  afagent.User,
		Parts: []afagent.Part{{Kind: afagent.PartFile, Data: compressedPDF, MIMEType: "application/pdf", Filename: "compressed.pdf"}},
	}}
	if got := afcompact.Estimate(compPDFMsgs); got < 8*afcompact.FileTokens {
		t.Fatalf("compressed 8-page PDF est = %d, want >= %d", got, 8*afcompact.FileTokens)
	}
	if _, err := comp.BeforeTurn(context.Background(), 1, compPDFMsgs); !errors.Is(err, afcompact.ErrOverBudget) {
		t.Fatalf("BeforeTurn on compressed 8-page PDF err = %v, want ErrOverBudget", err)
	}
}

func TestStripEncodedAndMicrocompactRounds(t *testing.T) {
	blob := strings.Repeat("QUJDRA==", 200) // 1600 base64 chars
	msgs := []afagent.Message{
		{Role: afagent.User, Content: "email body: " + blob + " end"},
		{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "r1a", Name: "s"}, {ID: "r1b", Name: "s"}}},
		{Role: afagent.Tool, ToolCallID: "r1a", Content: strings.Repeat("a", 1200)},
		{Role: afagent.Tool, ToolCallID: "r1b", Content: strings.Repeat("b", 1200)},
		{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "r2a", Name: "s"}, {ID: "r2b", Name: "s"}}},
		{Role: afagent.Tool, ToolCallID: "r2a", Content: strings.Repeat("c", 1200)},
		{Role: afagent.Tool, ToolCallID: "r2b", Content: strings.Repeat("d", 1200)},
	}
	stripped, ok := afcompact.StripEncoded(msgs, 1024)
	if !ok || !strings.Contains(stripped[0].Content, "[1600 characters of encoded data omitted]") {
		t.Fatalf("stripped = %q, ok=%v", stripped[0].Content, ok)
	}
	// Caller-specified minChars below 256 must be honored too.
	shortMsgs := []afagent.Message{{Role: afagent.User, Content: "token: " + strings.Repeat("QUJD", 40) + " short: " + strings.Repeat("AB", 20)}}
	shortStripped, shortOK := afcompact.StripEncoded(shortMsgs, 128)
	if !shortOK || !strings.Contains(shortStripped[0].Content, "[160 characters of encoded data omitted]") || !strings.Contains(shortStripped[0].Content, strings.Repeat("AB", 20)) {
		t.Fatalf("short stripped = %q, ok=%v", shortStripped[0].Content, shortOK)
	}
	// Line-wrapped 76-char MIME base64 blocks must be stripped as one run without
	// swallowing a short trailing prose line like "Summary".
	wrappedLine := strings.Repeat("QUJD", 19) // 76 chars
	mimeBlock := strings.Repeat(wrappedLine+"\r\n", 18) + wrappedLine
	mimeMsgs := []afagent.Message{{Role: afagent.User, Content: "attachment:\r\n" + mimeBlock + "\r\nSummary"}}
	mimeStripped, mimeOK := afcompact.StripEncoded(mimeMsgs, 1024)
	if !mimeOK || !strings.Contains(mimeStripped[0].Content, "characters of encoded data omitted]") || !strings.HasSuffix(mimeStripped[0].Content, "\r\nSummary") {
		t.Fatalf("mime stripped = %q, ok=%v", mimeStripped[0].Content, mimeOK)
	}
	if msgs[0].Content != "email body: "+blob+" end" {
		t.Fatal("StripEncoded mutated input")
	}
	// Encoded text inside PartText parts must also be stripped without mutating input.
	partMsgs := []afagent.Message{{
		Role: afagent.User,
		Parts: []afagent.Part{
			{Kind: afagent.PartText, Text: "part body: " + blob + " end"},
		},
	}}
	partStripped, partOK := afcompact.StripEncoded(partMsgs, 1024)
	if !partOK || !strings.Contains(partStripped[0].Parts[0].Text, "[1600 characters of encoded data omitted]") {
		t.Fatalf("part stripped = %q, ok=%v", partStripped[0].Parts[0].Text, partOK)
	}
	if partMsgs[0].Parts[0].Text != "part body: "+blob+" end" {
		t.Fatal("StripEncoded mutated input Parts slice")
	}
	// Keep 1 round intact (both r2a and r2b stay whole), clear round 1 (r1a, r1b),
	// including results between head and 2*head characters.
	msgs[2].Content = strings.Repeat("a", 150)
	mc := afcompact.MicrocompactRounds(msgs, 1, 100)
	if mc[5].Content != strings.Repeat("c", 1200) || mc[6].Content != strings.Repeat("d", 1200) {
		t.Fatal("newest round was cleared")
	}
	if !strings.Contains(mc[2].Content, "50 characters cleared to save context") || !strings.Contains(mc[3].Content, "cleared to save context") {
		t.Fatalf("older round not cleared: %q / %q", mc[2].Content, mc[3].Content)
	}
	// MicrocompactRounds also truncates large PartText entries on older tool
	// results without mutating the caller's Parts slice.
	origPartText := strings.Repeat("p", 500)
	partToolMsgs := []afagent.Message{
		{Role: afagent.User, Content: "go"},
		{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "p1", Name: "s"}}},
		{Role: afagent.Tool, ToolCallID: "p1", Parts: []afagent.Part{{Kind: afagent.PartText, Text: origPartText}}},
		{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "p2", Name: "s"}}},
		{Role: afagent.Tool, ToolCallID: "p2", Parts: []afagent.Part{{Kind: afagent.PartText, Text: origPartText}}},
	}
	mcParts := afcompact.MicrocompactRounds(partToolMsgs, 1, 100)
	if !strings.Contains(mcParts[2].Parts[0].Text, "400 characters cleared to save context") {
		t.Fatalf("older PartText tool result not cleared: %q", mcParts[2].Parts[0].Text)
	}
	if mcParts[4].Parts[0].Text != origPartText {
		t.Fatal("newest PartText tool result was cleared")
	}
	if partToolMsgs[2].Parts[0].Text != origPartText {
		t.Fatal("MicrocompactRounds mutated input Parts slice")
	}
}

func TestCompactorStripEncodedAndDigestUserWithEstimator(t *testing.T) {
	ctx := context.Background()
	head := "GOAL: extract every invoice total.\n"
	bigInput := head + strings.Repeat("Invoice #12345 from Acme Corp for $42.00\n", 400)
	msgs := []afagent.Message{{Role: afagent.User, Content: bigInput}}
	var stages []string
	var calls int
	c := &afcompact.Compactor{
		Budget:          1000,
		At:              1.0,
		StripEncodedMin: 1024,
		DigestUser:      true,
		DigestHead:      len(head),
		DigestOver:      2000,
		Chunk:           4000,
		Concurrency:     1,
		Estimator: func(ms []afagent.Message) int {
			return afcompact.Estimate(ms)
		},
		SummarizeChunk: func(_ context.Context, _ afagent.ToolCall, part, parts int, _ string) (string, error) {
			calls++
			return "Acme: $42.00", nil
		},
		OnCompact: func(stage string, _, _ int) { stages = append(stages, stage) },
	}
	out, err := c.BeforeTurn(ctx, 1, msgs)
	if err != nil {
		t.Fatalf("BeforeTurn: %v", err)
	}
	if calls == 0 || !strings.HasPrefix(out[0].Content, head) || !strings.Contains(out[0].Content, "Acme: $42.00") {
		t.Fatalf("out = %q, calls=%d stages=%v", out[0].Content, calls, stages)
	}
	// Budget/cost admission error stops serial user-digest immediately.
	budgetErr := errors.New("budget_exhausted")
	c.SummarizeChunk = func(context.Context, afagent.ToolCall, int, int, string) (string, error) {
		return "", budgetErr
	}
	if _, err := c.BeforeTurn(ctx, 1, msgs); !errors.Is(err, budgetErr) {
		t.Fatalf("err = %v, want budgetErr", err)
	}
}

func TestResultWorkspaceAdmitReadIsolationAndLoopIntegration(t *testing.T) {
	ws := afcompact.NewResultWorkspace(afcompact.WorkspaceOptions{
		InlineMax:       128,
		AlwaysInlineMax: 32,
		InlineBudget:    256,
		PreviewBytes:    64,
		MaxBytes:        100_000,
		MaxFiles:        4,
	})
	// Small result stays inline.
	if got := ws.Admit("search", "small"); got != "small" {
		t.Fatalf("small = %q", got)
	}
	// Large JSON result spills and is pretty-printed.
	bigJSON, _ := json.Marshal(map[string]any{"items": strings.Split(strings.Repeat("row,", 80), ",")})
	desc := ws.Admit("gmail_fetch", string(bigJSON))
	var parsed struct {
		Saved struct {
			Path string `json:"path"`
		} `json:"saved_file"`
	}
	if err := json.Unmarshal([]byte(desc), &parsed); err != nil || parsed.Saved.Path != "/results/001-gmail-fetch.json" {
		t.Fatalf("desc = %s (err=%v)", desc, err)
	}
	// Read slice via Dispatch inside a Loop with CountToolCall exemption.
	turn := 0
	model := afagent.ModelFunc(func(_ context.Context, req afagent.Request) (afagent.Response, error) {
		turn++
		switch turn {
		case 1:
			return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{
				ID: "c1", Name: ws.ToolName(), Arguments: `{"path":"/results/001-gmail-fetch.json","offset":1,"limit":5}`,
			}}}}, nil
		default:
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "1\t{") {
				t.Errorf("unexpected read output: %s", req.Messages[len(req.Messages)-1].Content)
			}
			return afagent.Response{Message: afagent.Message{Content: "done"}}, nil
		}
	})
	loop := afagent.Loop{
		Model:         model,
		Tools:         []afagent.ToolSpec{ws.ToolSpec()},
		MaxToolCalls:  1,
		CountToolCall: func(c afagent.ToolCall) bool { return c.Name != ws.ToolName() },
		Dispatch:      ws.Dispatch,
	}
	res, err := loop.Run(context.Background(), []afagent.Message{user("inspect")})
	if err != nil || res.Final != "done" || res.ToolCalls != 0 {
		t.Fatalf("loop res=%+v err=%v", res, err)
	}

	// Isolation: a second workspace cannot see the first workspace's files.
	other := afcompact.NewResultWorkspace(afcompact.WorkspaceOptions{})
	if _, err := other.Read(parsed.Saved.Path, 1, 10); err == nil {
		t.Fatal("second workspace read first workspace's file")
	}

	// Workspace-full reports explicit truncation notice instead of silent drop.
	tiny := afcompact.NewResultWorkspace(afcompact.WorkspaceOptions{
		InlineMax:       64,
		AlwaysInlineMax: 16,
		MaxBytes:        32,
		PreviewBytes:    20,
	})
	fullOut := tiny.Admit("huge", strings.Repeat("x", 500))
	if !strings.Contains(fullOut, `"truncated":true`) || !strings.Contains(fullOut, `"workspace":"full"`) {
		t.Fatalf("full workspace output = %s", fullOut)
	}

	// Read reports exact omitted byte count and never claims past-end when a
	// single requested line exceeds ReadMaxBytes.
	tight := afcompact.NewResultWorkspace(afcompact.WorkspaceOptions{
		InlineMax:        16,
		AlwaysInlineMax:  8,
		ReadLineMaxBytes: 100,
		ReadMaxBytes:     60,
	})
	tightDesc := tight.Admit("log", strings.Repeat("a", 250)+"\nline2")
	var tightParsed struct {
		Saved struct {
			Path string `json:"path"`
		} `json:"saved_file"`
	}
	if err := json.Unmarshal([]byte(tightDesc), &tightParsed); err != nil {
		t.Fatalf("tight desc = %s (%v)", tightDesc, err)
	}
	gotTight, err := tight.Read(tightParsed.Saved.Path, 1, 10)
	if err != nil {
		t.Fatalf("tight Read failed: %v", err)
	}
	if !strings.Contains(gotTight, "[more: continue with offset 2]") || !strings.Contains(gotTight, "line truncated to fit response") {
		t.Fatalf("tight Read = %q", gotTight)
	}
	wide := afcompact.NewResultWorkspace(afcompact.WorkspaceOptions{
		InlineMax:        16,
		AlwaysInlineMax:  8,
		ReadLineMaxBytes: 100,
		ReadMaxBytes:     4096,
	})
	wideDesc := wide.Admit("log", strings.Repeat("a", 250))
	var wideParsed struct {
		Saved struct {
			Path string `json:"path"`
		} `json:"saved_file"`
	}
	_ = json.Unmarshal([]byte(wideDesc), &wideParsed)
	gotWide, err := wide.Read(wideParsed.Saved.Path, 1, 10)
	if err != nil || !strings.Contains(gotWide, "[150 more bytes on this line]") {
		t.Fatalf("wide Read = %q (err=%v), want 150 more bytes", gotWide, err)
	}

	// Binary check must detect NUL bytes even past the first 8 KiB.
	lateNul := strings.Repeat("a", 10<<10) + "\x00tail"
	lateDesc := wide.Admit("blob", lateNul)
	var lateParsed struct {
		Saved struct {
			Path string `json:"path"`
		} `json:"saved_file"`
	}
	if err := json.Unmarshal([]byte(lateDesc), &lateParsed); err != nil {
		t.Fatalf("lateDesc = %s (%v)", lateDesc, err)
	}
	if _, err := wide.Read(lateParsed.Saved.Path, 1, 10); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("late NUL Read err = %v, want binary file error", err)
	}
}
