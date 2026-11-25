package afcompact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// DefaultResultToolName is the default tool name for reading files spilled into
// a ResultWorkspace.
const DefaultResultToolName = "read_result_file"

// Default workspace bounds.
const (
	DefaultInlineMax        = 16 << 10
	DefaultInlineBudget     = 100_000
	DefaultAlwaysInlineMax  = 4 << 10
	DefaultPreviewBytes     = 2 << 10
	DefaultPrettyJSONMax    = 4 << 20
	DefaultWorkspaceBytes   = 64 << 20
	DefaultWorkspaceFiles   = 256
	DefaultReadMaxBytes     = 32 << 10
	DefaultReadLines        = 400
	DefaultReadLineMaxBytes = 2000
)

// WorkspaceOptions configures a bounded per-run ResultWorkspace. Zero fields
// use the Default* constants.
type WorkspaceOptions struct {
	// ToolName is the name of the read tool offered by ToolSpec (default
	// DefaultResultToolName).
	ToolName string
	// InlineMax is the largest single result passed inline while budget lasts.
	InlineMax int
	// InlineBudget is the total inline bytes allowed across the run before
	// results larger than AlwaysInlineMax are spilled.
	InlineBudget int
	// AlwaysInlineMax is the size below which a descriptor would cost about as
	// much as the result itself.
	AlwaysInlineMax int
	// PreviewBytes is the head preview carried in a spilled result's descriptor.
	PreviewBytes int
	// MaxBytes and MaxFiles bound the total bytes and files stored in the
	// workspace.
	MaxBytes int
	MaxFiles int
	// ReadMaxBytes, ReadLines, and ReadLineMaxBytes bound one Read response.
	ReadMaxBytes     int
	ReadLines        int
	ReadLineMaxBytes int
}

// WorkspaceFile describes one file stored in a ResultWorkspace.
type WorkspaceFile struct {
	Path        string `json:"path"`
	Tool        string `json:"tool"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
}

type savedResultDescriptor struct {
	Saved   savedResultMeta `json:"saved_file"`
	Preview string          `json:"preview"`
	Note    string          `json:"note"`
}

type savedResultMeta struct {
	Path         string `json:"path"`
	Tool         string `json:"tool"`
	ContentType  string `json:"content_type"`
	Bytes        int    `json:"bytes"`
	ApproxTokens int    `json:"approx_tokens"`
}

// ResultWorkspace is an in-memory, per-run bounded store for oversized tool
// results. Small results stay inline; oversized results are stored under
// /results/... and replaced with a compact descriptor and preview so the model
// can inspect only the lines it needs via the read tool without overflowing
// context or leaking files across runs or users.
type ResultWorkspace struct {
	opts      WorkspaceOptions
	mu        sync.Mutex
	files     map[string][]byte
	meta      map[string]WorkspaceFile
	usedBytes int
	remaining int
	seq       int
}

// NewResultWorkspace builds a fresh, isolated ResultWorkspace for one agent run.
func NewResultWorkspace(opts WorkspaceOptions) *ResultWorkspace {
	if strings.TrimSpace(opts.ToolName) == "" {
		opts.ToolName = DefaultResultToolName
	}
	opts.InlineMax = or(opts.InlineMax, DefaultInlineMax)
	opts.InlineBudget = or(opts.InlineBudget, DefaultInlineBudget)
	opts.AlwaysInlineMax = or(opts.AlwaysInlineMax, DefaultAlwaysInlineMax)
	opts.PreviewBytes = or(opts.PreviewBytes, DefaultPreviewBytes)
	opts.MaxBytes = or(opts.MaxBytes, DefaultWorkspaceBytes)
	opts.MaxFiles = or(opts.MaxFiles, DefaultWorkspaceFiles)
	opts.ReadMaxBytes = or(opts.ReadMaxBytes, DefaultReadMaxBytes)
	opts.ReadLines = or(opts.ReadLines, DefaultReadLines)
	opts.ReadLineMaxBytes = or(opts.ReadLineMaxBytes, DefaultReadLineMaxBytes)
	return &ResultWorkspace{
		opts:      opts,
		files:     map[string][]byte{},
		meta:      map[string]WorkspaceFile{},
		remaining: opts.InlineBudget,
	}
}

// ToolName returns the workspace's read tool name.
func (w *ResultWorkspace) ToolName() string { return w.opts.ToolName }

// ToolSpec describes the workspace's read tool to a model.
func (w *ResultWorkspace) ToolSpec() afagent.ToolSpec {
	schema := fmt.Sprintf(`{"type":"object","properties":{"path":{"type":"string","description":"The file path, such as /results/001-search.json. Omit to list files."},"offset":{"type":"integer","description":"The first line to read, from 1 (default 1)."},"limit":{"type":"integer","description":"Maximum lines to read (default %d)."}},"additionalProperties":false}`, w.opts.ReadLines)
	return afagent.ToolSpec{
		Name: w.opts.ToolName,
		Description: fmt.Sprintf(
			"Read a file saved in this run's result workspace. Large tool results are saved here and returned as {\"saved_file\": {\"path\": ...}} with a preview. "+
				"Returns numbered lines (%d by default); pass offset (1-based line) and limit to paginate. Omit path to list saved files.",
			w.opts.ReadLines,
		),
		Schema: json.RawMessage(schema),
	}
}

// Admit returns content inline while it fits the per-call and per-run inline
// budgets, or saves it to the workspace and returns a JSON descriptor with a
// UTF-8-safe preview. If the workspace is full, it returns content truncated
// with an explicit truncation notice rather than silently dropping data.
func (w *ResultWorkspace) Admit(toolName, content string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	inlineCap := max(min(w.opts.InlineMax, w.remaining), w.opts.AlwaysInlineMax)
	if len(content) <= inlineCap {
		w.remaining -= len(content)
		return content
	}
	stored := []byte(content)
	contentType := "text/plain"
	ext := ".txt"
	if len(stored) <= DefaultPrettyJSONMax && json.Valid(stored) {
		var indented bytes.Buffer
		if json.Indent(&indented, stored, "", "  ") == nil {
			stored = indented.Bytes()
		}
		contentType = "application/json"
		ext = ".json"
	}
	if len(w.files) >= w.opts.MaxFiles || w.usedBytes+len(stored) > w.opts.MaxBytes {
		preview := utf8Prefix(content, w.opts.PreviewBytes)
		b, err := json.Marshal(map[string]any{
			"truncated":    true,
			"tool":         toolName,
			"bytes":        len(content),
			"preview":      preview,
			"workspace":    "full",
			"trunc_notice": "Result exceeded inline budget and the run workspace is full; narrow the tool call to retrieve specific data.",
		})
		if err != nil {
			return preview + "\n[truncated: workspace full]"
		}
		w.remaining -= len(b)
		return string(b)
	}
	w.seq++
	filePath := fmt.Sprintf("/results/%03d-%s%s", w.seq, sanitizeToolSlug(toolName), ext)
	w.files[filePath] = stored
	w.usedBytes += len(stored)
	w.meta[filePath] = WorkspaceFile{
		Path:        filePath,
		Tool:        toolName,
		ContentType: contentType,
		Bytes:       len(stored),
	}
	desc := savedResultDescriptor{
		Saved: savedResultMeta{
			Path:         filePath,
			Tool:         toolName,
			ContentType:  contentType,
			Bytes:        len(stored),
			ApproxTokens: (len(stored) + CharsPerToken - 1) / CharsPerToken,
		},
		Preview: utf8Prefix(content, w.opts.PreviewBytes),
		Note:    fmt.Sprintf("Result saved to %s. Read slices with %s(path, offset, limit) instead of re-running the tool.", filePath, w.opts.ToolName),
	}
	b, err := json.Marshal(desc)
	if err != nil {
		w.remaining -= len(content)
		return content
	}
	w.remaining -= len(b)
	return string(b)
}

// List returns metadata for all files stored in the workspace in path order.
func (w *ResultWorkspace) List() []WorkspaceFile {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]WorkspaceFile, 0, len(w.meta))
	for _, f := range w.meta {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Read returns numbered lines from filePath (1-based offset), or a JSON listing
// when filePath is empty.
func (w *ResultWorkspace) Read(filePath string, offset, limit int) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		files := w.List()
		if len(files) == 0 {
			return "This run has no saved result files yet.", nil
		}
		b, err := json.Marshal(map[string]any{"files": files})
		return string(b), err
	}
	clean := path.Clean("/" + trimmed)
	w.mu.Lock()
	data, ok := w.files[clean]
	w.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no file %q in this run's workspace; call %s with no path to list files", filePath, w.opts.ToolName)
	}
	if len(data) == 0 {
		return "(" + clean + " is empty)", nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s is a binary file and cannot be read as text", clean)
	}
	first := max(offset, 1)
	if limit <= 0 {
		limit = w.opts.ReadLines
	}
	var b strings.Builder
	line, next := 1, 0
	for rest := string(data); rest != ""; line++ {
		text := rest
		if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
			text, rest = rest[:idx], rest[idx+1:]
		} else {
			rest = ""
		}
		if line < first {
			continue
		}
		if line-first >= limit {
			next = line
			break
		}
		if len(text) > w.opts.ReadLineMaxBytes {
			cut := utf8Cut(text, w.opts.ReadLineMaxBytes)
			text = text[:cut] + "… [" + strconv.Itoa(len(text)-cut) + " more bytes on this line]"
		}
		entry := fmt.Sprintf("%6d\t%s\n", line, text)
		if b.Len() > 0 && b.Len()+len(entry) > w.opts.ReadMaxBytes {
			next = line
			break
		}
		if b.Len() == 0 && len(entry) > w.opts.ReadMaxBytes {
			// Always make progress on the first requested line even when
			// ReadMaxBytes is tighter than a single formatted line, marking
			// the truncation explicitly.
			prefix := fmt.Sprintf("%6d\t", line)
			const suffix = "… [line truncated to fit response]\n"
			avail := max(w.opts.ReadMaxBytes-len(prefix)-len(suffix), 0)
			cut := utf8Cut(text, avail)
			entry = prefix + text[:cut] + suffix
			b.WriteString(entry)
			if rest != "" {
				next = line + 1
			}
			break
		}
		b.WriteString(entry)
	}
	switch {
	case b.Len() == 0 && line <= first:
		return "", fmt.Errorf("%s has %d lines; offset %d is past the end", clean, line-1, first)
	case next > 0:
		fmt.Fprintf(&b, "[more: continue with offset %d]\n", next)
	}
	return b.String(), nil
}

// Dispatch handles a tool call targeting the workspace's read tool.
func (w *ResultWorkspace) Dispatch(_ context.Context, call afagent.ToolCall) (string, error) {
	if call.Name != w.opts.ToolName {
		return "", fmt.Errorf("unknown workspace tool %q", call.Name)
	}
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	raw := strings.TrimSpace(call.Arguments)
	if raw == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", errors.New("arguments must be a JSON object")
	}
	return w.Read(args.Path, args.Offset, args.Limit)
}

func sanitizeToolSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '.' || r == '/':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "result"
	}
	return b.String()
}

func utf8Cut(s string, maxBytes int) int {
	if len(s) <= maxBytes {
		return len(s)
	}
	n := max(maxBytes, 0)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

func utf8Prefix(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:utf8Cut(s, maxBytes)] + "…"
}
