package afcompact_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if len(out) != 4 || out[0].Content != "find the bug" || !strings.HasPrefix(out[1].Content, afcompact.SummaryPrefix) || out[2].ToolCalls[0].ID != "4" || out[3].ToolCallID != "4" {
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
