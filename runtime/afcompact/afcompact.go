// Package afcompact keeps an agent's conversation within its model's
// context window. It works in stages, cheapest first:
//
//   - Microcompact clears the bodies of older tool results down to a short
//     head. The calls and their IDs stay, so the transcript stays well
//     formed. It is deterministic and costs nothing.
//   - Digest condenses large tool results in place: it splits each into
//     chunks and has a model condense them in parallel, so a short
//     conversation with huge results keeps what they said.
//   - Fold has a model summarize the older rounds into one message, keeping
//     the first request and the most recent rounds as they are.
//
// Compactor runs them as an agent's BeforeTurn, only as far as the
// conversation needs, and fails with ErrOverBudget when even that is not
// enough. Sizes are estimated from characters (Estimate), conservatively:
// they are not a provider's exact token count.
package afcompact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// ErrOverBudget means the conversation doesn't fit the budget even after
// compacting.
var ErrOverBudget = errors.New("afcompact: the conversation is over its context budget")

// CharsPerToken is how Estimate turns characters into tokens.
const CharsPerToken = 4

// Estimate returns the approximate tokens of msgs: their text, or their
// provider-native payload when that is larger (it holds the text, and may
// hold more, such as thinking blocks), and tool call names and arguments,
// by CharsPerToken.
func Estimate(msgs []afagent.Message) int {
	chars := 0
	for _, m := range msgs {
		text := utf8.RuneCountInString(m.Content)
		if m.Native != nil {
			text = max(text, utf8.RuneCount(m.Native.Data))
		}
		chars += text + 4
		for _, c := range m.ToolCalls {
			chars += utf8.RuneCountInString(c.Name) + utf8.RuneCountInString(c.Arguments) + 4
		}
	}
	return (chars + CharsPerToken - 1) / CharsPerToken
}

// Microcompact returns msgs with the bodies of tool results cleared down to
// their first head characters, except the last keep results, which stay
// whole. It changes nothing else, and never the msgs passed in.
func Microcompact(msgs []afagent.Message, keep, head int) []afagent.Message {
	out := append([]afagent.Message(nil), msgs...)
	seen := 0
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != afagent.Tool {
			continue
		}
		if seen++; seen <= keep {
			continue
		}
		if r := []rune(out[i].Content); len(r) > head {
			out[i].Content = string(r[:head]) + fmt.Sprintf(" … [%d characters cleared to save context]", len(r)-head)
		}
	}
	return out
}

// Summarizer writes the summary Fold folds older messages into. Summarize
// turns a model into one.
type Summarizer func(ctx context.Context, msgs []afagent.Message) (string, error)

// SummaryPrompt is the system prompt Summarize sends with the messages to
// summarize.
const SummaryPrompt = `You compact an AI agent's working conversation so it can carry on. Summarize the messages: the user's goal, what has been done and found (keep IDs, names, numbers and errors exactly), what is still to do, and any constraints the user set. Write it for the agent, not the user. Leave out pleasantries.`

// Summarize returns a Summarizer that asks model, without tools.
func Summarize(model afagent.Model) Summarizer {
	return func(ctx context.Context, msgs []afagent.Message) (string, error) {
		var b strings.Builder
		for _, m := range msgs {
			fmt.Fprintf(&b, "[%s] %s\n", m.Role, m.Content)
			for _, c := range m.ToolCalls {
				fmt.Fprintf(&b, "  called %s(%s)\n", c.Name, c.Arguments)
			}
		}
		res, err := model.Complete(ctx, afagent.Request{System: SummaryPrompt, Messages: []afagent.Message{{Role: afagent.User, Content: b.String()}}})
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(res.Message.Content) == "" {
			return "", errors.New("afcompact: the summary is empty")
		}
		return res.Message.Content, nil
	}
}

// SummaryPrefix starts the message Fold puts in place of what it folded.
const SummaryPrefix = "[Summary of the earlier conversation, compacted to save context]\n"

// Fold replaces the messages between the first one, the request, and the
// last keep messages with one user message holding their summary. The
// tail starts at a round boundary, never between a tool call and its
// result, so it may keep more than keep messages. It returns msgs as they
// are when there is nothing to fold.
func Fold(ctx context.Context, msgs []afagent.Message, keep int, summarize Summarizer) ([]afagent.Message, error) {
	start := len(msgs) - keep
	// Move the cut back to a message that doesn't answer a tool call.
	for start > 1 && msgs[start].Role == afagent.Tool {
		start--
	}
	if start <= 1 {
		return msgs, nil
	}
	summary, err := summarize(ctx, msgs[1:start])
	if err != nil {
		return nil, err
	}
	out := []afagent.Message{msgs[0], {Role: afagent.User, Content: SummaryPrefix + summary}}
	return append(out, msgs[start:]...), nil
}

// ChunkSummarizer summarizes one chunk of a tool result: part of parts,
// from the given call. SummarizeChunk turns a model into one.
type ChunkSummarizer func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error)

// ChunkPrompt is the system prompt SummarizeChunk sends.
const ChunkPrompt = `You condense one part of a large tool result so an AI agent can keep working with it. Keep every fact the agent may need: IDs, names, numbers, dates, errors, and anything that answers what the tool was called for. Drop repetition and boilerplate. Reply with the condensed text only.`

// SummarizeChunk returns a ChunkSummarizer that asks model, without tools.
func SummarizeChunk(model afagent.Model) ChunkSummarizer {
	return func(ctx context.Context, call afagent.ToolCall, part, parts int, chunk string) (string, error) {
		prompt := fmt.Sprintf("Tool: %s\nArguments: %s\nPart %d of %d of its result:\n\n%s", call.Name, call.Arguments, part, parts, chunk)
		res, err := model.Complete(ctx, afagent.Request{System: ChunkPrompt, Messages: []afagent.Message{{Role: afagent.User, Content: prompt}}})
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(res.Message.Content) == "" {
			return "", errors.New("afcompact: the chunk summary is empty")
		}
		return res.Message.Content, nil
	}
}

// DigestPrefix starts a result Digest condensed.
const DigestPrefix = "[Condensed from a %d-character result, in %d parts]\n"

// Digest condenses each tool result longer than over characters in place:
// it splits the result into chunks of at most chunk characters, at line
// breaks where it can, summarizes the chunks concurrently, at most
// concurrency at a time, and joins their summaries in order as the
// result's content. Calls, IDs and every other message stay as they are,
// so the agent keeps what the results said, unlike Microcompact. A chunk
// that fails fails the whole digest.
func Digest(ctx context.Context, msgs []afagent.Message, over, chunk, concurrency int, summarize ChunkSummarizer) ([]afagent.Message, error) {
	if chunk <= 0 {
		return nil, fmt.Errorf("afcompact: Digest needs a positive chunk size, got %d", chunk)
	}
	calls := map[string]afagent.ToolCall{}
	type job struct {
		msg, part int
		text      string
	}
	var jobs []job
	parts := map[int][]string{}
	for i, m := range msgs {
		for _, c := range m.ToolCalls {
			calls[c.ID] = c
		}
		// Size alone decides: a condensed result is short, so it isn't
		// condensed again unless it is still over.
		if m.Role != afagent.Tool || utf8.RuneCountInString(m.Content) <= over {
			continue
		}
		chunks := split(m.Content, chunk)
		parts[i] = make([]string, len(chunks))
		for p, text := range chunks {
			jobs = append(jobs, job{msg: i, part: p, text: text})
		}
	}
	if len(jobs) == 0 {
		return msgs, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The first error wins and stops the rest.
	var failed error
	var once sync.Once
	fail := func(err error) {
		once.Do(func() {
			failed = err
			cancel()
		})
	}
	// A fixed pool of workers, however many chunks there are.
	queue := make(chan job)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(max(1, concurrency), len(jobs)) {
		wg.Go(func() {
			for j := range queue {
				out, err := summarize(ctx, calls[msgs[j.msg].ToolCallID], j.part+1, len(parts[j.msg]), j.text)
				if err != nil {
					fail(err)
					continue
				}
				mu.Lock()
				parts[j.msg][j.part] = out
				mu.Unlock()
			}
		})
	}
feed:
	for _, j := range jobs {
		select {
		case queue <- j:
		case <-ctx.Done():
			fail(ctx.Err())
			break feed
		}
	}
	close(queue)
	wg.Wait()
	if failed != nil {
		return nil, failed
	}
	out := append([]afagent.Message(nil), msgs...)
	for i, ps := range parts {
		out[i].Content = fmt.Sprintf(DigestPrefix, utf8.RuneCountInString(msgs[i].Content), len(ps)) + strings.Join(ps, "\n")
	}
	return out, nil
}

// split cuts s into pieces of at most n characters, at a line break in the
// second half of a piece when there is one.
func split(s string, n int) []string {
	r := []rune(s)
	var out []string
	for len(r) > n {
		cut := n
		for i := n - 1; i >= n/2; i-- {
			if r[i] == '\n' {
				cut = i + 1
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}

// Compactor keeps a conversation within Budget tokens, as an agent's
// BeforeTurn.
type Compactor struct {
	// Budget is the tokens the conversation may take, below the model's
	// window by room for the system prompt, tools and answer.
	Budget int
	// At is the share of Budget past which it compacts; 0 means 0.8, and
	// it is at most 1.
	At float64
	// KeepResults is how many tool results Microcompact leaves whole, and
	// KeepMessages how many recent messages Fold keeps, at first; 0 means 4
	// and 6. When that is not enough, they drop to the latest one.
	KeepResults, KeepMessages int
	// Head is how much of a cleared tool result stays; 0 means 200.
	Head int
	// Summarize writes Fold's summaries; nil skips Fold.
	Summarize Summarizer
	// SummarizeChunk condenses large tool results with Digest; nil skips
	// it. DigestOver is the size, in characters, past which a result is
	// condensed (0 means 8000), Chunk the size of its chunks (0 means
	// 6000), and Concurrency how many chunks are condensed at once (0
	// means 4).
	SummarizeChunk    ChunkSummarizer
	DigestOver, Chunk int
	Concurrency       int
	// OnCompact, when set, is told each stage that ran, as "microcompact"
	// or "fold", with the estimate before and after.
	OnCompact func(stage string, before, after int)
}

// BeforeTurn compacts msgs if they are past the threshold, in stages that
// each run only if the ones before were not enough: Microcompact keeping
// KeepResults results whole; Digest of the results still large; Microcompact
// keeping only the latest; Fold keeping KeepMessages messages, then only
// the latest round. It returns
// ErrOverBudget when the result still exceeds Budget.
func (c *Compactor) BeforeTurn(ctx context.Context, turn int, msgs []afagent.Message) ([]afagent.Message, error) {
	at := c.At
	switch {
	case at <= 0:
		at = 0.8
	case at > 1:
		// Past Budget is over budget: compact by then at the latest.
		at = 1
	}
	limit := int(float64(c.Budget) * at)
	size := Estimate(msgs)
	if size <= limit {
		return msgs, nil
	}
	keepResults, keepMessages, head := or(c.KeepResults, 4), or(c.KeepMessages, 6), or(c.Head, 200)
	compacted := msgs
	try := func(stage string, next []afagent.Message) {
		if after := Estimate(next); after < size {
			c.report(stage, size, after)
			compacted, size = next, after
		}
	}
	if size > limit {
		try("microcompact", Microcompact(compacted, keepResults, head))
	}
	// Large results the agent still needs, the latest included: condense
	// them in parallel chunks rather than clearing them.
	if size > limit && c.SummarizeChunk != nil {
		digested, err := Digest(ctx, compacted, or(c.DigestOver, 8000), or(c.Chunk, 6000), or(c.Concurrency, 4), c.SummarizeChunk)
		if err != nil {
			return nil, err
		}
		try("digest", digested)
	}
	if size > limit {
		try("microcompact", Microcompact(compacted, 1, head))
	}
	if c.Summarize != nil {
		for _, keep := range []int{keepMessages, 1} {
			if size <= limit {
				break
			}
			folded, err := Fold(ctx, compacted, keep, c.Summarize)
			if err != nil {
				return nil, err
			}
			try("fold", folded)
		}
	}
	if size > c.Budget {
		return nil, fmt.Errorf("%w: about %d tokens after compacting, budget %d", ErrOverBudget, size, c.Budget)
	}
	return compacted, nil
}

func (c *Compactor) report(stage string, before, after int) {
	if c.OnCompact != nil {
		c.OnCompact(stage, before, after)
	}
}

func or(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}
