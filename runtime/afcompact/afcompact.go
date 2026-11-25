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
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
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

// ImageTokens and FileTokens are the conservative token estimates Estimate
// assigns to multimodal image and file parts when counting prompt size.
const (
	ImageTokens = 1000
	FileTokens  = 2000
)

// DefaultStripEncodedMin is the shortest run of encoded data StripEncoded
// replaces when minChars is <= 0.
const DefaultStripEncodedMin = 1024

// encodedRun matches candidate base64/encoded runs (including MIME line-wrapped
// base64 blocks of >= 60 chars per line); the exact minChars floor is enforced
// in StripEncoded.
var encodedRun = regexp.MustCompile(`[A-Za-z0-9+/_=-]{60,}(?:\r?\n[A-Za-z0-9+/_=-]{60,})*|[A-Za-z0-9+/_=-]+`)

// pdfPagePattern matches PDF page object dictionaries (`/Type /Page`
// but not `/Type /Pages`) for multi-page PDF token estimation.
var pdfPagePattern = regexp.MustCompile(`/Type\s*/Page\b`)

// pdfPagesCountPattern matches `/Type /Pages` dictionaries carrying a `/Count N`
// entry (in either field order) to recover total pages even when individual page
// objects are compressed or split across object streams.
var (
	pdfPagesCountAfter  = regexp.MustCompile(`/Type\s*/Pages\b[^<>]{0,256}/Count\s+(\d+)`)
	pdfPagesCountBefore = regexp.MustCompile(`/Count\s+(\d+)[^<>]{0,256}/Type\s*/Pages\b`)
)

const (
	maxPDFInflateBytes  = 16 << 20
	maxPDFPagesEstimate = 10_000
)

// Estimate returns the approximate tokens of msgs: their text, multimodal
// parts, or their provider-native payload when that is larger (it holds the
// text, and may hold more, such as thinking blocks), and tool call names and
// arguments, by CharsPerToken.
func Estimate(msgs []afagent.Message) int {
	chars := 0
	for _, m := range msgs {
		text := utf8.RuneCountInString(m.Content)
		for _, p := range m.Parts {
			switch p.Kind {
			case afagent.PartText:
				text += utf8.RuneCountInString(p.Text)
			case afagent.PartImage:
				if p.EstimatedTokens > 0 {
					text += p.EstimatedTokens * CharsPerToken
				} else {
					text += ImageTokens * CharsPerToken
				}
			case afagent.PartFile:
				text += filePartChars(p)
			}
		}
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

func filePartChars(p afagent.Part) int {
	if p.EstimatedTokens > 0 {
		return p.EstimatedTokens * CharsPerToken
	}
	raw := p.Data
	mime := strings.ToLower(strings.TrimSpace(p.MIMEType))
	if len(raw) == 0 && strings.HasPrefix(p.URL, "data:") {
		if rest, ok := strings.CutPrefix(p.URL, "data:"); ok {
			if meta, b64, ok := strings.Cut(rest, ","); ok {
				if media, ok := strings.CutSuffix(meta, ";base64"); ok {
					if semi := strings.IndexByte(media, ';'); semi >= 0 {
						media = media[:semi]
					}
					if mime == "" {
						mime = strings.ToLower(strings.TrimSpace(media))
					}
					if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
						raw = decoded
					}
				}
			}
		}
	}
	if len(raw) == 0 {
		return FileTokens * CharsPerToken
	}
	isPDF := mime == "application/pdf" || bytes.HasPrefix(raw, []byte("%PDF-"))
	if isPDF {
		if pages := estimatePDFPages(raw); pages > 0 {
			return pages * FileTokens * CharsPerToken
		}
		return FileTokens * CharsPerToken
	}
	// Non-PDF text files are passed to the model as text, so count their runes.
	if utf8.Valid(raw) {
		return max(utf8.RuneCount(raw), FileTokens*CharsPerToken)
	}
	return FileTokens * CharsPerToken
}

func estimatePDFPages(raw []byte) int {
	pageCount := len(pdfPagePattern.FindAll(raw, -1))
	maxTreeCount := scanPDFPagesCount(raw)

	// Inflate FlateDecode / ObjStm streams within a strict byte budget so PDFs
	// with compressed page objects or page trees are counted accurately.
	remaining := maxPDFInflateBytes
	pos := 0
	for remaining > 0 && pos < len(raw) {
		idx := bytes.Index(raw[pos:], []byte("stream"))
		if idx < 0 {
			break
		}
		start := pos + idx + len("stream")
		if start < len(raw) && raw[start] == '\r' {
			start++
		}
		if start >= len(raw) || raw[start] != '\n' {
			pos += idx + len("stream")
			continue
		}
		start++
		endIdx := bytes.Index(raw[start:], []byte("endstream"))
		if endIdx < 0 {
			break
		}
		streamData := bytes.TrimRight(raw[start:start+endIdx], "\r\n")
		pos = start + endIdx + len("endstream")
		if zr, err := zlib.NewReader(bytes.NewReader(streamData)); err == nil {
			inflated, _ := io.ReadAll(io.LimitReader(zr, int64(remaining)))
			_ = zr.Close()
			remaining -= len(inflated)
			if len(inflated) > 0 {
				pageCount += len(pdfPagePattern.FindAll(inflated, -1))
				if c := scanPDFPagesCount(inflated); c > maxTreeCount {
					maxTreeCount = c
				}
			}
		}
	}

	pages := max(pageCount, maxTreeCount)
	return min(pages, maxPDFPagesEstimate)
}

func scanPDFPagesCount(b []byte) int {
	best := 0
	for _, pat := range []*regexp.Regexp{pdfPagesCountAfter, pdfPagesCountBefore} {
		for _, m := range pat.FindAllSubmatch(b, -1) {
			if len(m) < 2 {
				continue
			}
			if n, err := strconv.Atoi(string(m[1])); err == nil && n > best {
				best = n
			}
		}
	}
	return best
}

func truncateToolMessage(m afagent.Message, head int) (afagent.Message, bool) {
	changed := false
	if r := []rune(m.Content); len(r) > head {
		m.Content = string(r[:head]) + fmt.Sprintf(" … [%d characters cleared to save context]", len(r)-head)
		changed = true
	}
	if len(m.Parts) > 0 {
		parts := m.Parts
		partsCopied := false
		for j, p := range m.Parts {
			if p.Kind != afagent.PartText {
				continue
			}
			if r := []rune(p.Text); len(r) > head {
				if !partsCopied {
					parts = append([]afagent.Part(nil), m.Parts...)
					partsCopied = true
				}
				parts[j].Text = string(r[:head]) + fmt.Sprintf(" … [%d characters cleared to save context]", len(r)-head)
				changed = true
			}
		}
		if partsCopied {
			m.Parts = parts
		}
	}
	return m, changed
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
		if next, changed := truncateToolMessage(out[i], head); changed {
			out[i] = next
		}
	}
	return out
}

// MicrocompactRounds clears tool results older than the newest keepRounds
// assistant tool-call rounds down to their first head characters, keeping
// complete recent parallel rounds intact. It never mutates msgs.
func MicrocompactRounds(msgs []afagent.Message, keepRounds, head int) []afagent.Message {
	if keepRounds <= 0 {
		return Microcompact(msgs, 0, head)
	}
	rounds := 0
	keepFrom := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == afagent.Assistant && len(msgs[i].ToolCalls) > 0 {
			rounds++
			if rounds == keepRounds {
				keepFrom = i
				break
			}
		}
	}
	if rounds < keepRounds {
		return msgs
	}
	out := append([]afagent.Message(nil), msgs...)
	changed := false
	for i := range keepFrom {
		if out[i].Role != afagent.Tool {
			continue
		}
		if next, did := truncateToolMessage(out[i], head); did {
			out[i] = next
			changed = true
		}
	}
	if !changed {
		return msgs
	}
	return out
}

// StripEncoded replaces long runs of encoded data (such as base64 attachments,
// MIME bodies, or inline blobs) of at least minChars characters in User and
// Tool messages (both Content and PartText Parts) with a short placeholder. It
// never mutates msgs and reports whether any message changed.
func StripEncoded(msgs []afagent.Message, minChars int) ([]afagent.Message, bool) {
	if minChars <= 0 {
		minChars = DefaultStripEncodedMin
	}
	replace := func(text string) string {
		if len(text) < minChars {
			return text
		}
		return encodedRun.ReplaceAllStringFunc(text, func(run string) string {
			if len(run) < minChars {
				return run
			}
			return fmt.Sprintf("[%d characters of encoded data omitted]", len(run))
		})
	}
	out := msgs
	copied := false
	for i, m := range msgs {
		if m.Role != afagent.User && m.Role != afagent.Tool {
			continue
		}
		stripped := replace(m.Content)
		parts := m.Parts
		partsCopied := false
		for j, p := range m.Parts {
			if p.Kind != afagent.PartText {
				continue
			}
			strippedPart := replace(p.Text)
			if len(strippedPart) >= len(p.Text) {
				continue
			}
			if !partsCopied {
				parts = append([]afagent.Part(nil), m.Parts...)
				partsCopied = true
			}
			parts[j].Text = strippedPart
		}
		contentChanged := len(stripped) < len(m.Content)
		if contentChanged || partsCopied {
			if !copied {
				out, copied = append([]afagent.Message(nil), msgs...), true
			}
			if contentChanged {
				out[i].Content = stripped
			}
			if partsCopied {
				out[i].Parts = parts
			}
		}
	}
	return out, copied
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

// SourceSummary is the Source of that message: the user didn't write it.
const SourceSummary = "summary"

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
	out := []afagent.Message{msgs[0], {Role: afagent.User, Content: SummaryPrefix + summary, Source: SourceSummary}}
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
				if err := ctx.Err(); err != nil {
					// Failed or canceled: drain without calling, and make
					// sure a cancellation is the result.
					fail(err)
					continue
				}
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
		if ctx.Err() != nil {
			fail(ctx.Err())
			break
		}
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
	// Canceled after the last chunk was taken: never a partial digest.
	if err := ctx.Err(); err != nil {
		return nil, err
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
	// Estimator, when set, replaces Estimate when measuring conversation size
	// (for example, to include tool schemas or custom tokenization density).
	Estimator func(msgs []afagent.Message) int
	// KeepResults is how many tool results Microcompact leaves whole, and
	// KeepMessages how many recent messages Fold keeps, at first; 0 means 4
	// and 6. When that is not enough, they drop to the latest one.
	KeepResults, KeepMessages int
	// KeepRounds, when above 0, runs MicrocompactRounds keeping that many
	// recent tool-call rounds intact as the first microcompact stage instead
	// of Microcompact(KeepResults).
	KeepRounds int
	// Head is how much of a cleared tool result stays; 0 means 200.
	Head int
	// StripEncodedMin, when above 0, runs StripEncoded on runs of at least
	// StripEncodedMin characters as a free deterministic stage ("strip")
	// before any model-assisted compaction.
	StripEncodedMin int
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
	// DigestUser, when true alongside SummarizeChunk, also digests oversized
	// User messages (preserving their leading DigestHead characters verbatim,
	// default 4000) so large inputs handed to a step fit the budget.
	// MaxDigestRounds bounds re-digests of a single user message (default 1).
	DigestUser      bool
	DigestHead      int
	MaxDigestRounds int
	// OnCompact, when set, is told each stage that ran, as "microcompact",
	// "strip", "digest", or "fold", with the estimate before and after.
	OnCompact func(stage string, before, after int)
}

func (c *Compactor) estimate(msgs []afagent.Message) int {
	if c.Estimator != nil {
		return c.Estimator(msgs)
	}
	return Estimate(msgs)
}

// BeforeTurn compacts msgs if they are past the threshold, in stages that
// each run only if the ones before were not enough: Microcompact (or
// MicrocompactRounds) keeping recent results whole; StripEncoded (when
// StripEncodedMin > 0); Digest of results (and oversized user messages when
// DigestUser is set); Microcompact keeping only the latest; Fold keeping
// KeepMessages messages, then only the latest round. It returns
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
	size := c.estimate(msgs)
	if size <= limit {
		return msgs, nil
	}
	keepResults, keepMessages, head := or(c.KeepResults, 4), or(c.KeepMessages, 6), or(c.Head, 200)
	compacted := msgs
	try := func(stage string, next []afagent.Message) {
		if after := c.estimate(next); after < size {
			c.report(stage, size, after)
			compacted, size = next, after
		}
	}
	if size > limit {
		if c.KeepRounds > 0 {
			try("microcompact", MicrocompactRounds(compacted, c.KeepRounds, head))
		} else {
			try("microcompact", Microcompact(compacted, keepResults, head))
		}
	}
	if size > limit && c.StripEncodedMin > 0 {
		if stripped, ok := StripEncoded(compacted, c.StripEncodedMin); ok {
			try("strip", stripped)
		}
	}
	// Large results the agent still needs, the latest included: condense
	// them in parallel chunks rather than clearing them.
	if size > limit && c.SummarizeChunk != nil {
		over, chunk, conc := or(c.DigestOver, 8000), or(c.Chunk, 6000), or(c.Concurrency, 4)
		digested, err := Digest(ctx, compacted, over, chunk, conc, c.SummarizeChunk)
		if err != nil {
			return nil, err
		}
		try("digest", digested)
		if size > limit && c.DigestUser {
			digestedUser, err := c.digestUserMessages(ctx, compacted, over, chunk, conc, limit)
			if err != nil {
				return nil, err
			}
			try("digest", digestedUser)
		}
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

// digestUserMessages condenses oversized User messages while preserving their
// leading DigestHead runes (the task instructions) verbatim.
func (c *Compactor) digestUserMessages(ctx context.Context, msgs []afagent.Message, over, chunk, concurrency, limit int) ([]afagent.Message, error) {
	headChars := or(c.DigestHead, 4000)
	rounds := or(c.MaxDigestRounds, 1)
	out := append([]afagent.Message(nil), msgs...)
	changed := false
	for i := range out {
		if c.estimate(out) <= limit {
			break
		}
		if out[i].Role != afagent.User || len(out[i].Parts) > 0 {
			continue
		}
		r := []rune(out[i].Content)
		if len(r) <= over || len(r) <= headChars {
			continue
		}
		head := string(r[:headChars])
		rest := string(r[headChars:])
		origRestLen := len(r) - headChars
		notes := rest
		for range rounds {
			if c.estimate(out) <= limit {
				break
			}
			chunks := split(notes, chunk)
			condensed, err := digestChunks(ctx, afagent.ToolCall{Name: "user_input"}, chunks, concurrency, c.SummarizeChunk)
			if err != nil {
				return nil, err
			}
			next := strings.Join(condensed, "\n")
			if utf8.RuneCountInString(next) >= utf8.RuneCountInString(notes) {
				break
			}
			notes = next
			out[i].Content = head + "\n\n" + fmt.Sprintf(DigestPrefix, origRestLen, len(chunks)) + notes
			changed = true
		}
	}
	if !changed {
		return msgs, nil
	}
	return out, nil
}

// digestChunks condenses chunks for a single message using concurrency workers.
func digestChunks(ctx context.Context, call afagent.ToolCall, chunks []string, concurrency int, summarize ChunkSummarizer) ([]string, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		failed error
		once   sync.Once
		mu     sync.Mutex
		wg     sync.WaitGroup
	)
	fail := func(err error) {
		once.Do(func() {
			failed = err
			cancel()
		})
	}
	type job struct {
		part int
		text string
	}
	results := make([]string, len(chunks))
	queue := make(chan job)
	for range min(max(1, concurrency), len(chunks)) {
		wg.Go(func() {
			for j := range queue {
				if err := ctx.Err(); err != nil {
					fail(err)
					continue
				}
				out, err := summarize(ctx, call, j.part+1, len(chunks), j.text)
				if err != nil {
					fail(err)
					continue
				}
				mu.Lock()
				results[j.part] = out
				mu.Unlock()
			}
		})
	}
feed:
	for p, text := range chunks {
		if ctx.Err() != nil {
			fail(ctx.Err())
			break
		}
		select {
		case queue <- job{part: p, text: text}:
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
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
