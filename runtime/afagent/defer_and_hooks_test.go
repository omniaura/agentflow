package afagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLoopDeferToolCollectsMultipleParkedCallsAndRunsSiblings(t *testing.T) {
	parked := errors.New("approval required")
	ordinaryErr := errors.New("boom")
	model, reqs := script(t, calls("approve_1", "read_ok", "fail_tool", "approve_2"))
	var called []string
	var returnedEvents []Event
	afterCalled := false
	l := Loop{
		Model:       model,
		Concurrency: 4,
		DeferTool:   func(err error) bool { return errors.Is(err, parked) },
		OnEvent: func(e Event) {
			if e.Kind == ToolReturned {
				returnedEvents = append(returnedEvents, e)
			}
		},
		AfterTools: func(_ context.Context, _ int, msgs []Message) ([]Message, error) {
			afterCalled = true
			return msgs, nil
		},
		Dispatch: func(_ context.Context, c ToolCall) (string, error) {
			called = append(called, c.Name)
			switch {
			case strings.HasPrefix(c.Name, "approve_"):
				return "", parked
			case c.Name == "fail_tool":
				return "", ordinaryErr
			default:
				return "read result", nil
			}
		},
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if !errors.Is(err, ErrDeferred) {
		t.Fatalf("err = %v, want ErrDeferred", err)
	}
	if afterCalled {
		t.Fatal("AfterTools ran on a deferred batch")
	}
	if len(*reqs) != 1 || strings.Join(called, ",") != "approve_1,read_ok,fail_tool,approve_2" {
		t.Fatalf("requests=%d called=%v", len(*reqs), called)
	}
	if len(res.Deferred) != 2 || res.Deferred[0].ID != "c0" || res.Deferred[1].ID != "c3" {
		t.Fatalf("deferred = %+v", res.Deferred)
	}
	// Deferred ToolReturned events must report deferred=true and failed=false in
	// ClientEvent, while ordinary dispatch errors still report failed=true.
	if len(returnedEvents) != 4 {
		t.Fatalf("returnedEvents = %d, want 4", len(returnedEvents))
	}
	if name, data, ok := ClientEvent(returnedEvents[0]); !ok || name != "tool_returned" {
		t.Fatalf("deferred ClientEvent = %q %v %v", name, data, ok)
	} else if m := data.(map[string]any); m["failed"] != false || m["deferred"] != true {
		t.Fatalf("deferred ClientEvent data = %v, want failed=false deferred=true", m)
	}
	if _, data, ok := ClientEvent(returnedEvents[2]); !ok {
		t.Fatal("ordinary failure ClientEvent missing")
	} else if m := data.(map[string]any); m["failed"] != true || m["deferred"] != nil {
		t.Fatalf("ordinary failure ClientEvent data = %v, want failed=true", m)
	}
	// Transcript holds user, assistant with all 4 calls, and only the completed read_ok + fail_tool results.
	if len(res.Messages) != 4 || res.Messages[2].ToolCallID != "c1" || res.Messages[2].Content != "read result" || res.Messages[3].ToolCallID != "c2" || !res.Messages[3].IsError {
		t.Fatalf("messages = %+v", res.Messages)
	}
	// Resume by injecting the two deferred tool results.
	resumed := append(res.Messages,
		Message{Role: Tool, ToolCallID: "c0", Content: "approved 1"},
		Message{Role: Tool, ToolCallID: "c3", Content: "approved 2"},
	)
	model2, reqs2 := script(t, Message{Content: "all done"})
	l.Model = model2
	res2, err := l.Run(t.Context(), resumed)
	if err != nil || res2.Final != "all done" || len(*reqs2) != 1 || len(called) != 4 {
		t.Fatalf("resume: res=%+v err=%v called=%v", res2, err, called)
	}
}

func TestLoopInterruptOverridesDeferImmediately(t *testing.T) {
	parked := errors.New("parked")
	abort := errors.New("revoked")
	model, _ := script(t, calls("approve_1", "abort_now", "never_run"))
	var called []string
	l := Loop{
		Model:         model,
		DeferTool:     func(err error) bool { return errors.Is(err, parked) },
		InterruptTool: func(err error) bool { return errors.Is(err, abort) },
		Dispatch: func(_ context.Context, c ToolCall) (string, error) {
			called = append(called, c.Name)
			switch c.Name {
			case "approve_1":
				return "", parked
			case "abort_now":
				return "", abort
			default:
				return "ok", nil
			}
		},
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if !errors.Is(err, abort) {
		t.Fatalf("err = %v, want abort", err)
	}
	if strings.Join(called, ",") != "approve_1,abort_now" {
		t.Fatalf("called = %v, want abort before never_run", called)
	}
	if len(res.Deferred) != 1 || res.Deferred[0].ID != "c0" {
		t.Fatalf("deferred = %+v", res.Deferred)
	}
}

func TestLoopCountToolCallExemptAndToolLimitTurn(t *testing.T) {
	model, reqs := script(t, calls("ext", "read_file", "ext"), Message{Content: "final summary"})
	var ran []string
	l := Loop{
		Model:            model,
		MaxTurns:         2,
		MaxToolCalls:     1,
		AnswerOnLastTurn: true,
		ToolLimitResult:  `{"error":"limit"}`,
		ToolLimitMessage: "Write your answer now.",
		Tools:            []ToolSpec{{Name: "ext"}, {Name: "read_file"}},
		CountToolCall:    func(c ToolCall) bool { return c.Name != "read_file" },
		Dispatch: func(_ context.Context, c ToolCall) (string, error) {
			ran = append(ran, c.Name)
			return "ok:" + c.Name, nil
		},
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Final != "final summary" || !res.OutOfToolCalls || res.ToolCalls != 1 {
		t.Fatalf("res = %+v", res)
	}
	// First ext ran (counted=1), read_file ran (exempt), second ext refused.
	if strings.Join(ran, ",") != "ext,read_file" {
		t.Fatalf("ran = %v", ran)
	}
	if len(*reqs) != 2 || !(*reqs)[1].NoToolCalls {
		t.Fatalf("reqs = %d, turn 2 NoToolCalls = %v", len(*reqs), len(*reqs) > 1 && (*reqs)[1].NoToolCalls)
	}
	// Tool limit message and custom limit result present.
	msgs := (*reqs)[1].Messages
	if msgs[len(msgs)-2].Content != `{"error":"limit"}` || msgs[len(msgs)-1].Content != "Write your answer now." || msgs[len(msgs)-1].Source != SourceLoop {
		t.Fatalf("tail messages = %+v", msgs[len(msgs)-3:])
	}
}

func TestLoopAfterToolsFormatToolErrorAndAnswerFeedback(t *testing.T) {
	model, reqs := script(t,
		calls("browser"),
		Message{Content: "Would you like me to continue?"},
		Message{Content: ""},
		Message{Content: "Finished report."},
	)
	var returnedErr error
	nudged := false
	emptyNudged := false
	l := Loop{
		Model:          model,
		MaxTurns:       3,
		PromptCacheKey: "wf-cache-1",
		Tools:          []ToolSpec{{Name: "browser"}},
		FormatToolError: func(err error) string {
			b, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(b)
		},
		Dispatch: func(_ context.Context, c ToolCall) (string, error) {
			return "", errors.New("selector timeout")
		},
		AfterTools: func(_ context.Context, _ int, msgs []Message) ([]Message, error) {
			return append(msgs, Message{
				Role: User,
				Parts: []Part{
					{Kind: PartText, Text: "Screenshot attached"},
					{Kind: PartImage, URL: "https://example.com/shot.png"},
				},
			}), nil
		},
		Check: func(answer string) error {
			if strings.Contains(answer, "Would you like") && !nudged {
				nudged = true
				return &AnswerFeedback{Message: "Do not ask; finish now."}
			}
			if strings.TrimSpace(answer) == "" && !emptyNudged {
				emptyNudged = true
				return &AnswerFeedback{Message: "Write the final summary now.", NoToolCalls: true, ExtraTurn: true}
			}
			return nil
		},
		OnEvent: func(e Event) {
			if e.Kind == ToolReturned {
				returnedErr = e.Err
			}
		},
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "check site"}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Final != "Finished report." || res.Turns != 4 {
		t.Fatalf("res = %+v", res)
	}
	if returnedErr == nil || returnedErr.Error() != "selector timeout" {
		t.Fatalf("ToolReturned.Err = %v", returnedErr)
	}
	if (*reqs)[0].PromptCacheKey != "wf-cache-1" {
		t.Fatalf("PromptCacheKey = %q", (*reqs)[0].PromptCacheKey)
	}
	// Turn 2 saw formatted JSON error + multimodal AfterTools user message.
	turn2 := (*reqs)[1].Messages
	if turn2[2].Content != `{"error":"selector timeout"}` || !turn2[2].IsError {
		t.Fatalf("tool msg = %+v", turn2[2])
	}
	if len(turn2[3].Parts) != 2 || turn2[3].Parts[1].URL != "https://example.com/shot.png" {
		t.Fatalf("after tools msg = %+v", turn2[3])
	}
	// Turn 3 saw verbatim feedback (not wrapped in "Your answer was rejected:").
	turn3 := (*reqs)[2].Messages
	if turn3[len(turn3)-1].Content != "Do not ask; finish now." || (*reqs)[2].NoToolCalls {
		t.Fatalf("turn 3 = %+v, NoToolCalls=%v", turn3[len(turn3)-1], (*reqs)[2].NoToolCalls)
	}
	// Turn 4 got the extra turn with NoToolCalls=true.
	if !(*reqs)[3].NoToolCalls || (*reqs)[3].Messages[len((*reqs)[3].Messages)-1].Content != "Write the final summary now." {
		t.Fatalf("turn 4 = NoToolCalls:%v tail:%+v", (*reqs)[3].NoToolCalls, (*reqs)[3].Messages[len((*reqs)[3].Messages)-1])
	}
}

func TestLoopLastTurnMessage(t *testing.T) {
	model, reqs := script(t, calls("search"), Message{Content: "answer"})
	l := Loop{
		Model:            model,
		MaxTurns:         2,
		Tools:            []ToolSpec{{Name: "search"}},
		AnswerOnLastTurn: true,
		LastTurnMessage:  "Last turn: summarize now.",
		Dispatch:         func(context.Context, ToolCall) (string, error) { return "hit", nil },
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "find"}})
	if err != nil || res.Final != "answer" || !res.OutOfTurns {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	turn2 := (*reqs)[1].Messages
	if !(*reqs)[1].NoToolCalls || turn2[len(turn2)-1].Content != "Last turn: summarize now." || turn2[len(turn2)-1].Source != SourceLoop {
		t.Fatalf("turn 2 = %+v", turn2)
	}
}

func TestLoopAnswerFeedbackEmptyMessageAndForcedNoToolsViolation(t *testing.T) {
	// Empty AnswerFeedback.Message falls back to default rejection wrapper, and
	// a model calling tools on a forced-no-tools retry before the last turn fails
	// without wrapping ErrMaxTurns.
	model, reqs := script(t,
		Message{Content: "bad"},
		calls("search"),
	)
	dispatched := 0
	checkCalls := 0
	l := Loop{
		Model:    model,
		MaxTurns: 4,
		Tools:    []ToolSpec{{Name: "search"}},
		Dispatch: func(context.Context, ToolCall) (string, error) {
			dispatched++
			return "ok", nil
		},
		Check: func(string) error {
			checkCalls++
			if checkCalls == 1 {
				return &AnswerFeedback{NoToolCalls: true}
			}
			return nil
		},
	}
	_, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if err == nil {
		t.Fatal("expected error when model called tools on forced no-tools retry")
	}
	if errors.Is(err, ErrMaxTurns) {
		t.Fatalf("err wrapped ErrMaxTurns before turn limit: %v", err)
	}
	if dispatched != 0 {
		t.Fatalf("dispatched = %d, want 0", dispatched)
	}
	turn2 := (*reqs)[1]
	if !turn2.NoToolCalls {
		t.Fatal("turn 2 should have NoToolCalls=true")
	}
	tail := turn2.Messages[len(turn2.Messages)-1]
	if tail.Content == "" || !strings.Contains(tail.Content, "rejected") {
		t.Fatalf("empty feedback produced %q, want default rejection wrapper", tail.Content)
	}
}

func TestLoopNoToolsRepeatedToolCallsTerminates(t *testing.T) {
	// With no tools configured, repeated tool calls beyond MaxToolCalls must
	// still terminate at MaxTurns rather than extending maxTurns forever.
	model, _ := script(t, calls("ghost", "ghost"), calls("ghost"))
	l := Loop{
		Model:            model,
		MaxTurns:         2,
		MaxToolCalls:     1,
		AnswerOnLastTurn: true,
		Dispatch:         func(context.Context, ToolCall) (string, error) { return "ok", nil },
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if !errors.Is(err, ErrMaxTurns) || res.Turns != 2 {
		t.Fatalf("res.Turns=%d err=%v, want turns=2 ErrMaxTurns", res.Turns, err)
	}
}

func TestLoopCheckTypedNilAnswerFeedbackNormalized(t *testing.T) {
	model, _ := script(t, Message{Content: "bad"}, Message{Content: "good"})
	calls := 0
	var seenRejected error
	l := Loop{
		Model:    model,
		MaxTurns: 3,
		Check: func(string) error {
			calls++
			if calls == 1 {
				var fb *AnswerFeedback
				return fb
			}
			return nil
		},
		OnEvent: func(e Event) {
			if e.Kind == AnswerRejected {
				seenRejected = e.Err
			}
		},
	}
	res, err := l.Run(t.Context(), []Message{{Role: User, Content: "go"}})
	if err != nil || res.Final != "good" || seenRejected == nil {
		t.Fatalf("res=%+v err=%v seenRejected=%v", res, err, seenRejected)
	}
}
