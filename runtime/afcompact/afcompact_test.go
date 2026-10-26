package afcompact_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afcompact"
)

func user(s string) afagent.Message { return afagent.Message{Role: afagent.User, Content: s} }

// round is a tool call and its result.
func round(id, result string) []afagent.Message {
	return []afagent.Message{
		{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: id, Name: "search", Arguments: `{"q":"x"}`}}},
		{Role: afagent.Tool, ToolCallID: id, Content: result},
	}
}

func TestEstimate(t *testing.T) {
	small := afcompact.Estimate([]afagent.Message{user("hi")})
	big := afcompact.Estimate(append([]afagent.Message{user("hi")}, round("1", strings.Repeat("x", 4000))...))
	if small <= 0 || big < 1000 || big <= small {
		t.Fatalf("small %d, big %d", small, big)
	}
}

// A native payload, such as Claude's content blocks with thinking, counts
// when it is larger than the text.
func TestEstimateNative(t *testing.T) {
	plain := afagent.Message{Role: afagent.Assistant, Content: "ok"}
	native := plain
	native.Native = &afagent.Native{Provider: "anthropic", Data: []byte(`[{"type":"thinking","thinking":"` + strings.Repeat("t", 4000) + `"}]`)}
	if a, b := afcompact.Estimate([]afagent.Message{plain}), afcompact.Estimate([]afagent.Message{native}); b < a+900 {
		t.Fatalf("plain %d, native %d", a, b)
	}
}

// At above 1 still compacts before declaring the conversation over budget.
func TestCompactorAtAboveOne(t *testing.T) {
	long := strings.Repeat("x", 2000)
	msgs := []afagent.Message{user("go")}
	for _, id := range []string{"1", "2", "3"} {
		msgs = append(msgs, round(id, long)...)
	}
	c := &afcompact.Compactor{Budget: afcompact.Estimate(msgs) - 10, At: 1.5}
	if _, err := c.BeforeTurn(context.Background(), 1, msgs); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestMicrocompact(t *testing.T) {
	long := strings.Repeat("é", 500)
	msgs := []afagent.Message{user("go")}
	for _, id := range []string{"1", "2", "3"} {
		msgs = append(msgs, round(id, long)...)
	}
	out := afcompact.Microcompact(msgs, 1, 10)
	// The last result stays whole; older ones keep a head and a note.
	if out[6].Content != long || !strings.HasPrefix(out[2].Content, strings.Repeat("é", 10)+" … [490 characters cleared") || out[4].Content == long {
		t.Fatalf("got %q / %q / %q", out[2].Content[:40], out[4].Content[:20], out[6].Content[:10])
	}
	// Calls and ids are untouched, and so is the input.
	if out[1].ToolCalls[0].ID != "1" || out[2].ToolCallID != "1" || msgs[2].Content != long {
		t.Fatal("structure or input changed")
	}
}

func TestFold(t *testing.T) {
	msgs := []afagent.Message{user("find the bug")}
	for _, id := range []string{"1", "2", "3", "4"} {
		msgs = append(msgs, round(id, "result "+id)...)
	}
	var folded []afagent.Message
	summarize := func(ctx context.Context, ms []afagent.Message) (string, error) {
		folded = ms
		return "searched 1-3, nothing yet", nil
	}
	// keep 1 would cut between call 4 and its result: the tail moves back
	// to the call.
	out, err := afcompact.Fold(context.Background(), msgs, 1, summarize)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 || out[0].Content != "find the bug" || !strings.HasPrefix(out[1].Content, afcompact.SummaryPrefix) || out[1].Source != afcompact.SourceSummary || out[2].ToolCalls[0].ID != "4" || out[3].ToolCallID != "4" {
		t.Fatalf("got %+v", out)
	}
	if len(folded) != 6 || folded[0].ToolCalls[0].ID != "1" {
		t.Fatalf("folded %+v", folded)
	}
	// Nothing to fold: unchanged, and no summary asked.
	short := []afagent.Message{user("hi"), {Role: afagent.Assistant, Content: "hello"}}
	if out, err := afcompact.Fold(context.Background(), short, 6, func(context.Context, []afagent.Message) (string, error) {
		t.Fatal("summarized")
		return "", nil
	}); err != nil || len(out) != 2 {
		t.Fatalf("short: %+v, %v", out, err)
	}
}

func TestSummarize(t *testing.T) {
	var got afagent.Request
	model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		got = req
		return afagent.Response{Message: afagent.Message{Content: "the summary"}}, nil
	})
	s, err := afcompact.Summarize(model)(context.Background(), append([]afagent.Message{user("find it")}, round("1", "found nothing")...))
	if err != nil || s != "the summary" {
		t.Fatalf("got %q, %v", s, err)
	}
	// One request, no tools, with the transcript as text.
	if got.System != afcompact.SummaryPrompt || len(got.Tools) != 0 || !strings.Contains(got.Messages[0].Content, `called search({"q":"x"})`) || !strings.Contains(got.Messages[0].Content, "found nothing") {
		t.Fatalf("request = %+v", got)
	}
	empty := afagent.ModelFunc(func(context.Context, afagent.Request) (afagent.Response, error) { return afagent.Response{}, nil })
	if _, err := afcompact.Summarize(empty)(context.Background(), []afagent.Message{user("x")}); err == nil {
		t.Fatal("empty summary accepted")
	}
}

func TestCompactor(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("x", 2000)
	msgs := []afagent.Message{user("go")}
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		msgs = append(msgs, round(id, long)...)
	}
	size := afcompact.Estimate(msgs) // about 3000 tokens
	var stages []string
	report := func(stage string, before, after int) {
		if after >= before {
			t.Errorf("%s grew %d -> %d", stage, before, after)
		}
		stages = append(stages, stage)
	}

	// Under the threshold: untouched.
	c := &afcompact.Compactor{Budget: size * 2, OnCompact: report}
	if out, err := c.BeforeTurn(ctx, 1, msgs); err != nil || len(stages) != 0 || &out[0] != &msgs[0] {
		t.Fatalf("under: %v, %v", stages, err)
	}

	// Microcompact is enough.
	c = &afcompact.Compactor{Budget: size * 3 / 4, KeepResults: 1, OnCompact: report, Summarize: func(context.Context, []afagent.Message) (string, error) {
		t.Fatal("folded when microcompact was enough")
		return "", nil
	}}
	out, err := c.BeforeTurn(ctx, 1, msgs)
	if err != nil || strings.Join(stages, ",") != "microcompact" || len(out) != len(msgs) {
		t.Fatalf("microcompact: %v, %v", stages, err)
	}

	// Results that are all recent, so only folding helps.
	stages = nil
	recent := []afagent.Message{user("go")}
	for i := range 6 {
		recent = append(recent, afagent.Message{Role: afagent.Assistant, Content: long}, user(string(rune('a'+i))))
	}
	c = &afcompact.Compactor{Budget: afcompact.Estimate(recent) / 2, KeepMessages: 2, OnCompact: report, Summarize: func(context.Context, []afagent.Message) (string, error) { return "short", nil }}
	out, err = c.BeforeTurn(ctx, 2, recent)
	if err != nil || strings.Join(stages, ",") != "fold" || len(out) != 4 {
		t.Fatalf("fold: %v, %d messages, %v", stages, len(out), err)
	}

	// Still too big, or no summarizer: over budget.
	c = &afcompact.Compactor{Budget: 10}
	if _, err := c.BeforeTurn(ctx, 1, recent); !errors.Is(err, afcompact.ErrOverBudget) {
		t.Fatalf("over budget: %v", err)
	}
	// A failing summarizer fails the turn.
	c = &afcompact.Compactor{Budget: afcompact.Estimate(recent) / 2, Summarize: func(context.Context, []afagent.Message) (string, error) { return "", errors.New("summarizer down") }}
	if _, err := c.BeforeTurn(ctx, 1, recent); err == nil || err.Error() != "summarizer down" {
		t.Fatalf("summarizer error: %v", err)
	}
}

// In a loop, the compactor keeps every request within budget.
func TestCompactorInLoop(t *testing.T) {
	long := strings.Repeat("y", 3000)
	c := &afcompact.Compactor{Budget: 1200, Summarize: func(context.Context, []afagent.Message) (string, error) { return "earlier searches found nothing", nil }}
	turn := 0
	model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		if size := afcompact.Estimate(req.Messages); size > c.Budget {
			t.Errorf("turn %d sent %d tokens", turn, size)
		}
		if turn++; turn < 6 {
			return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: string(rune('0' + turn)), Name: "search", Arguments: "{}"}}}}, nil
		}
		return afagent.Response{Message: afagent.Message{Content: "done"}}, nil
	})
	l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "search"}}, BeforeTurn: c.BeforeTurn, Dispatch: func(context.Context, afagent.ToolCall) (string, error) { return long, nil }}
	if res, err := l.Run(context.Background(), []afagent.Message{user("search")}); err != nil || res.Final != "done" {
		t.Fatalf("got %v, %+v", err, res)
	}
}

// Without a summarizer, microcompact escalates to keeping only the latest
// result when keeping KeepResults isn't enough.
func TestCompactorMicrocompactEscalates(t *testing.T) {
	long := strings.Repeat("z", 3000)
	msgs := []afagent.Message{user("go")}
	for _, id := range []string{"1", "2", "3"} {
		msgs = append(msgs, round(id, long)...)
	}
	var stages []string
	c := &afcompact.Compactor{Budget: 1200, OnCompact: func(stage string, _, _ int) { stages = append(stages, stage) }}
	out, err := c.BeforeTurn(context.Background(), 1, msgs)
	if err != nil || out[6].Content != long || out[2].Content == long || out[4].Content == long {
		t.Fatalf("got %v, %v", err, stages)
	}
}

func TestDigest(t *testing.T) {
	ctx := context.Background()
	var lines []string
	for i := range 300 {
		lines = append(lines, strings.Repeat("r", 40)+string(rune('a'+i%26)))
	}
	big := strings.Join(lines, "\n") // about 12,600 characters
	msgs := []afagent.Message{user("go")}
	msgs = append(msgs, round("1", "small result")...)
	msgs = append(msgs, round("2", big)...)
	var mu sync.Mutex
	running, peak := 0, 0
	var seen []string
	summarize := func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error) {
		mu.Lock()
		running++
		peak = max(peak, running)
		seen = append(seen, call.ID)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		// Chunks end at a line break and stay within the size.
		if utf8.RuneCountInString(chunk) > 3000 || !strings.HasSuffix(chunk, "\n") && part < parts {
			t.Errorf("part %d of %d: %d characters", part, parts, utf8.RuneCountInString(chunk))
		}
		return fmt.Sprintf("<part %d/%d>", part, parts), nil
	}
	out, err := afcompact.Digest(ctx, msgs, 8000, 3000, 2, summarize)
	if err != nil {
		t.Fatal(err)
	}
	// The big result is condensed in order; the small one, calls and IDs
	// stay; the input isn't changed.
	got := out[4].Content
	if !strings.HasPrefix(got, "[Condensed from a 12599-character result, in 5 parts]\n<part 1/5>\n<part 2/5>") || !strings.HasSuffix(got, "<part 5/5>") {
		t.Fatalf("digest = %q", got)
	}
	if out[2].Content != "small result" || out[4].ToolCallID != "2" || out[3].ToolCalls[0].ID != "2" || msgs[4].Content != big {
		t.Fatal("changed more than the big result")
	}
	// Chunks ran concurrently, at most 2 at a time, knowing their call.
	if peak != 2 || len(seen) != 5 || seen[0] != "2" {
		t.Errorf("peak %d, calls %v", peak, seen)
	}
	// A condensed result is short: under the threshold, it isn't
	// condensed again. A raw result that merely looks condensed is.
	if again, _ := afcompact.Digest(ctx, out, 8000, 3000, 2, summarize); again[4].Content != got {
		t.Error("condensed twice")
	}
	spoof := append([]afagent.Message(nil), msgs...)
	spoof[4].Content = "[Condensed from a 1-character result, in 1 parts]\n" + big
	if out, _ := afcompact.Digest(ctx, spoof, 8000, 3000, 2, summarize); !strings.Contains(out[4].Content, "<part 1/") {
		t.Error("trusted the marker in a raw result")
	}
	// A chunk size that can't split is an error, not a hang.
	for _, n := range []int{0, -1} {
		if _, err := afcompact.Digest(ctx, msgs, 8000, n, 2, summarize); err == nil {
			t.Errorf("chunk %d accepted", n)
		}
	}
	// One failing chunk fails the digest.
	failing := func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error) {
		if part == 3 {
			return "", errors.New("chunk failed")
		}
		return "ok", nil
	}
	if _, err := afcompact.Digest(ctx, msgs, 8000, 3000, 4, failing); err == nil || err.Error() != "chunk failed" {
		t.Fatalf("failing chunk: %v", err)
	}
	// After a failure, no further chunk is summarized, even by a
	// summarizer that ignores cancellation.
	var calls int
	var cmu sync.Mutex
	failFirst := func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error) {
		cmu.Lock()
		calls++
		cmu.Unlock()
		return "", errors.New("down")
	}
	if _, err := afcompact.Digest(ctx, msgs, 8000, 3000, 1, failFirst); err == nil || calls != 1 {
		t.Fatalf("after a failure: %v, %d calls", err, calls)
	}
	// Canceled midway: an error, never a digest with parts missing.
	cctx, cancelRun := context.WithCancel(ctx)
	cancelling := func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error) {
		cancelRun()
		return "ok", nil
	}
	if out, err := afcompact.Digest(cctx, msgs, 8000, 3000, 1, cancelling); !errors.Is(err, context.Canceled) || out != nil {
		t.Fatalf("canceled: %v, %v", err, out != nil)
	}
}

// A short conversation with one huge result: the compactor condenses it
// instead of clearing it.
func TestCompactorDigests(t *testing.T) {
	huge := strings.Repeat(strings.Repeat("d", 99)+"\n", 400) // 40,000 characters
	msgs := append([]afagent.Message{user("summarize the logs")}, round("1", huge)...)
	var stages []string
	c := &afcompact.Compactor{
		Budget: 4000,
		SummarizeChunk: func(context.Context, afagent.ToolCall, int, int, string) (string, error) {
			return "errors at 10:02 and 10:07", nil
		},
		OnCompact: func(stage string, _, _ int) { stages = append(stages, stage) },
		Summarize: func(context.Context, []afagent.Message) (string, error) {
			t.Fatal("folded")
			return "", nil
		},
	}
	out, err := c.BeforeTurn(context.Background(), 1, msgs)
	if err != nil || strings.Join(stages, ",") != "digest" || !strings.Contains(out[2].Content, "errors at 10:02") {
		t.Fatalf("got %v, %v, %q", err, stages, out[2].Content[:60])
	}
}
