package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func openAt(t *testing.T, path, text string) *Document {
	t.Helper()
	doc, err := NewDocumentManager().OpenDocument(protocol.TextDocumentItem{URI: fileURI(path), LanguageID: "agentflow", Version: 1, Text: text})
	require.NoError(t, err)
	return doc
}

// at returns the position of the n-th (0-based) occurrence of needle.
func at(t *testing.T, doc *Document, needle string, n int) Position {
	t.Helper()
	off := -1
	for i := 0; i <= n; i++ {
		j := strings.Index(string(doc.Content[off+1:]), needle)
		if j < 0 {
			t.Fatalf("no occurrence %d of %q", n, needle)
		}
		off += j + 1
	}
	return doc.byteOffsetToPosition(off)
}

func TestUTF16Positions(t *testing.T) {
	src := []byte("héllo 👋 <!name>\nx")
	off := strings.Index(string(src), "name")
	pos := offsetToPosition(src, off)
	// h é l l o space (6 units) + 👋 (2 units) + space + "<!" = 11
	if pos != (Position{Line: 0, Character: 11}) {
		t.Fatalf("pos = %+v", pos)
	}
	if got := positionToOffset(src, pos); got != off {
		t.Fatalf("offset = %d, want %d", got, off)
	}
	// Positions past the end of a line clamp to it.
	if got := positionToOffset(src, Position{Line: 0, Character: 99}); got != strings.IndexByte(string(src), '\n') {
		t.Fatalf("clamped offset = %d", got)
	}
	if got := positionToOffset(src, Position{Line: 5}); got != len(src) {
		t.Fatalf("past end = %d", got)
	}
}

func TestIncrementalUpdate(t *testing.T) {
	dm := NewDocumentManager()
	_, err := dm.OpenDocument(protocol.TextDocumentItem{URI: "file:///tmp/u.af", Text: ".title A\n👋 <!name>\n"})
	require.NoError(t, err)
	r := &Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 9}}
	doc, err := dm.UpdateDocument("file:///tmp/u.af", 2, []TextDocumentContentChangeEvent{{Range: r, Text: "user"}})
	require.NoError(t, err)
	if string(doc.Content) != ".title A\n👋 <!user>\n" {
		t.Fatalf("content = %q", doc.Content)
	}
}

func TestDiagnosticsIncludeLint(t *testing.T) {
	doc := mustOpenTestDocument(t, ".title A\n<?x>\nhi\n\n.tool t\n.param a strng The a.\n.param b-c\n")
	var got []string
	for _, d := range doc.Diagnostics() {
		got = append(got, d.Code.Value.(string)+":"+string(rune('0'+*d.Severity))+":"+string(rune('0'+d.Range.Start.Line)))
	}
	slices.Sort(got)
	// AF003 unclosed conditional, AF011 once (not also from the parser).
	want := []string{"AF003:1:1", "AF011:1:6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	doc = mustOpenTestDocument(t, ".tool t\n.param a strng The a.\nDoes t.\n")
	ds := doc.Diagnostics()
	if len(ds) != 1 || ds[0].Code.Value != "AF012" || *ds[0].Severity != protocol.DiagnosticSeverityWarning || ds[0].Range.Start.Line != 1 {
		t.Fatalf("got %+v", ds)
	}
}

func TestFormatEdits(t *testing.T) {
	doc := mustOpenTestDocument(t, ".title A\nHi <! name >.\n\n\n")
	edits, err := doc.FormatEdits()
	require.NoError(t, err)
	if len(edits) != 1 || edits[0].NewText != ".title A\n\nHi <!name>.\n" || edits[0].Range.End.Line != 4 {
		t.Fatalf("edits = %+v", edits)
	}
	doc = mustOpenTestDocument(t, ".title A\n\nHi.\n")
	edits, err = doc.FormatEdits()
	require.NoError(t, err)
	if len(edits) != 0 {
		t.Fatalf("formatted document got edits %+v", edits)
	}
}

func TestDefinitionVariables(t *testing.T) {
	doc := mustOpenTestDocument(t, ".title A\n<!user.name> <!user.age int>\n<!user.age>\n<*items []Item as item><!item.name></items>\n")
	loc := doc.Definition(at(t, doc, "age", 1))
	if len(loc) != 1 || loc[0].Range.Start != (protocol.Position{Line: 1, Character: 20}) {
		t.Fatalf("user.age -> %+v", loc)
	}
	// The first segment's definition is the path's first use.
	loc = doc.Definition(at(t, doc, "user", 1))
	if len(loc) != 1 || loc[0].Range.Start != (protocol.Position{Line: 1, Character: 2}) || loc[0].Range.End.Character != 6 {
		t.Fatalf("user -> %+v", loc)
	}
	// A loop alias goes to the loop that binds it.
	loc = doc.Definition(at(t, doc, "item.name", 0))
	aliasPos := at(t, doc, "item>", 0)
	if len(loc) != 1 || loc[0].Range.Start != (protocol.Position{Line: protocol.UInteger(aliasPos.Line), Character: protocol.UInteger(aliasPos.Character)}) {
		t.Fatalf("item -> %+v", loc)
	}
}

func TestDefinitionGenerated(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "p_af.go"), []byte("package p\n\ntype SystemPrompt struct {\n}\n\nvar LookupOrderTool = &aftool.Def[LookupOrderArgs]{}\n"), 0o644)
	doc := openAt(t, filepath.Join(dir, "p.af"), ".title System Prompt\nHi.\n\n.tool lookup_order\nLook up.\n")
	loc := doc.Definition(at(t, doc, "Prompt", 0))
	if len(loc) != 1 || !strings.HasSuffix(loc[0].URI, "/p_af.go") || loc[0].Range.Start != (protocol.Position{Line: 2, Character: 5}) {
		t.Fatalf("title -> %+v", loc)
	}
	loc = doc.Definition(at(t, doc, "lookup_order", 0))
	if len(loc) != 1 || loc[0].Range.Start != (protocol.Position{Line: 5, Character: 4}) {
		t.Fatalf("tool -> %+v", loc)
	}
	// The description is not the tool name.
	if loc := doc.Definition(at(t, doc, "Look up", 0)); loc != nil {
		t.Fatalf("description -> %+v", loc)
	}
}

func TestDefinitionSQLC(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/todo_tools.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	for _, tt := range []struct {
		at, file, line string
	}{
		// A file and a query: the query's line in the file.
		{"SetTodoDone", "/examples/routes/db/queries.sql", "-- name: SetTodoDone :execrows"},
		// A file alone: its only query.
		{"db/search.sql", "/examples/routes/db/search.sql", "-- name: SearchTodos :many"},
		// A query alone: the method sqlc generated, like a route's query.
		{"ListTodos", "/examples/routes/db/queries.sql.go", "func (q *Queries) ListTodos("},
	} {
		loc := doc.Definition(at(t, doc, tt.at, 0))
		if len(loc) != 1 || !strings.HasSuffix(loc[0].URI, tt.file) {
			t.Fatalf("%s -> %+v", tt.at, loc)
		}
		b, _ := os.ReadFile(strings.TrimPrefix(loc[0].URI, "file://"))
		if line := strings.Split(string(b), "\n")[loc[0].Range.Start.Line]; !strings.HasPrefix(line, tt.line) {
			t.Errorf("%s: line %d is %q", tt.at, loc[0].Range.Start.Line, line)
		}
	}
}

func TestDefinitionSQLCNoMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "q.sql"), []byte("-- name: A :one\nSELECT 1;\n\n-- name: B :one\nSELECT 2;\n"), 0o644))
	src := ".tool pick\nPicks.\n.sqlc q.sql\n\n.tool missing\nMisses.\n.sqlc q.sql C\n"
	doc := openAt(t, filepath.Join(dir, "x.af"), src)
	// Several queries and none named after the tool, or a query the file
	// lacks: the file, at the top.
	for _, line := range []int{2, 6} {
		loc := doc.Definition(Position{Line: line, Character: 7})
		if len(loc) != 1 || !strings.HasSuffix(loc[0].URI, "/q.sql") || loc[0].Range.Start.Line != 0 {
			t.Errorf("line %d -> %+v", line, loc)
		}
	}
}

func TestDefinitionRouteQuery(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/todos.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	loc := doc.Definition(at(t, doc, "GetTodo", 0))
	if len(loc) != 1 || !strings.HasSuffix(loc[0].URI, "/examples/routes/db/queries.sql.go") {
		t.Fatalf("query -> %+v", loc)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "db", "queries.sql.go"))
	line := strings.Split(string(b), "\n")[loc[0].Range.Start.Line]
	if !strings.HasPrefix(line, "func (q *Queries) GetTodo(") {
		t.Fatalf("line %d is %q", loc[0].Range.Start.Line, line)
	}

	hover := doc.SectionHover(at(t, doc, "GetTodo", 0))
	for _, want := range []string{"**GET /todos/{id}** → `GetTodo`", "`db.GetTodo` is `:one`", "- `id` int64 ← path", "- `ownerId` string ← caller", "Requires authentication."} {
		if !strings.Contains(hover, want) {
			t.Errorf("hover is missing %q:\n%s", want, hover)
		}
	}
	hover = doc.SectionHover(at(t, doc, "CreateTodo", 0))
	if !strings.Contains(hover, "- `title` string ← JSON body") {
		t.Errorf("create hover:\n%s", hover)
	}
}

func applyEdits(t *testing.T, doc *Document, edit *protocol.WorkspaceEdit) string {
	t.Helper()
	edits := edit.Changes[doc.URI]
	slices.SortFunc(edits, func(a, b protocol.TextEdit) int {
		return positionToOffset(doc.Content, protocolPositionToPosition(b.Range.Start)) - positionToOffset(doc.Content, protocolPositionToPosition(a.Range.Start))
	})
	out := string(doc.Content)
	for _, e := range edits {
		s := positionToOffset(doc.Content, protocolPositionToPosition(e.Range.Start))
		en := positionToOffset(doc.Content, protocolPositionToPosition(e.Range.End))
		out = out[:s] + e.NewText + out[en:]
	}
	return out
}

func TestRename(t *testing.T) {
	src := ".title A\n<?user.premium bool>👋 <!user.name></user.premium>\n<!username> <!user>\n<*users []User as user><!user.name></users>\n"
	doc := mustOpenTestDocument(t, src)

	// Renaming the head segment renames every path under it, but not the
	// loop alias that shadows it, nor the different variable username.
	edit, err := doc.Rename(at(t, doc, "user", 0), "customer")
	require.NoError(t, err)
	want := ".title A\n<?customer.premium bool>👋 <!customer.name></customer.premium>\n<!username> <!customer>\n<*users []User as user><!user.name></users>\n"
	if got := applyEdits(t, doc, edit); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	// A later segment renames only paths with the same prefix.
	edit, err = doc.Rename(at(t, doc, "premium", 1), "vip")
	require.NoError(t, err)
	if got := applyEdits(t, doc, edit); got != strings.ReplaceAll(src, "premium", "vip") {
		t.Fatalf("got\n%s", got)
	}

	// The alias, from its use or its declaration, renames within its loop.
	for _, pos := range []Position{at(t, doc, "user.name", 1), at(t, doc, "user>", 1)} {
		edit, err = doc.Rename(pos, "u")
		require.NoError(t, err)
		if got := applyEdits(t, doc, edit); !strings.HasSuffix(got, "<*users []User as u><!u.name></users>\n") || !strings.Contains(got, "<!user.name></user.premium>") {
			t.Fatalf("alias rename:\n%s", got)
		}
	}

	if _, err := doc.Rename(at(t, doc, "user", 0), "not valid"); err == nil {
		t.Fatal("renamed to an invalid name")
	}
	if _, err := doc.Rename(Position{Line: 0, Character: 1}, "x"); err != ErrNoSymbol {
		t.Fatalf("rename on .title = %v", err)
	}
}

func TestSectionSymbols(t *testing.T) {
	doc := mustOpenTestDocument(t, ".tool search\n.param filter.since\n.param q\nSearch.\n\n.route GET /x/{id} GetX\n.public\n")
	syms := doc.SectionSymbols()
	if len(syms) != 2 || syms[0].Name != "search" || syms[1].Name != "GET /x/{id}" || *syms[1].Detail != "route → GetX" {
		t.Fatalf("symbols = %+v", syms)
	}
	params := syms[0].Children
	if len(params) != 2 || params[0].Name != "filter" || len(params[0].Children) != 1 || params[0].Children[0].Name != "since" || *params[1].Detail != "string" {
		t.Fatalf("params = %+v", params)
	}
	if hover := doc.SectionHover(at(t, doc, "search", 0)); !strings.Contains(hover, "- `filter` object\n  - `since` string") {
		t.Fatalf("tool hover:\n%s", hover)
	}
}

func TestDirectiveCompletions(t *testing.T) {
	labels := func(items []CompletionItem) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Label)
		}
		return out
	}
	doc := mustOpenTestDocument(t, ".title A\nHi.\n\n.tool t\n.param q \n\n.route GET /x Q\n\n")
	if got := labels(doc.GetCompletionItems(Position{Line: 1, Character: 0})); !slices.Contains(got, ".tool") || slices.Contains(got, ".param") {
		t.Fatalf("in a prompt: %v", got)
	}
	if got := labels(doc.GetCompletionItems(Position{Line: 5, Character: 0})); !slices.Contains(got, ".param") || !slices.Contains(got, ".sql") || !slices.Contains(got, ".route") {
		t.Fatalf("in a tool: %v", got)
	}
	if got := labels(doc.GetCompletionItems(Position{Line: 4, Character: 9})); !slices.Contains(got, "enum()") || slices.Contains(got, ".param") {
		t.Fatalf("after .param q: %v", got)
	}
	if got := labels(doc.GetCompletionItems(Position{Line: 7, Character: 0})); !slices.Contains(got, ".auth") || slices.Contains(got, ".param") {
		t.Fatalf("in a route: %v", got)
	}
}

func TestOutputSections(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "o_af.go"), []byte("package o\n\nvar TaskValueCheckSchema = &aftool.Output[TaskValueCheckOutput]{}\n"), 0o644)
	doc := openAt(t, filepath.Join(dir, "o.af"), ".output task_value_check\n.field repeatable bool Again?\n.field why \nWhether to save it.\n")
	syms := doc.SectionSymbols()
	if len(syms) != 1 || syms[0].Kind != protocol.SymbolKindStruct || *syms[0].Detail != "output" || len(syms[0].Children) != 2 {
		t.Fatalf("symbols = %+v", syms)
	}
	if loc := doc.Definition(at(t, doc, "task_value_check", 0)); len(loc) != 1 || loc[0].Range.Start.Line != 2 {
		t.Fatalf("definition = %+v", loc)
	}
	if hover := doc.SectionHover(at(t, doc, "task_value_check", 0)); !strings.Contains(hover, "**output task_value_check**") || !strings.Contains(hover, "- `repeatable` bool: Again?") {
		t.Fatalf("hover:\n%s", hover)
	}
	var labels []string
	for _, it := range doc.GetCompletionItems(Position{Line: 2, Character: 11}) {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, "bool") || slices.Contains(labels, "any") || slices.Contains(labels, "object") {
		t.Fatalf("field types = %v", labels)
	}
	labels = nil
	for _, it := range doc.GetCompletionItems(Position{Line: 3, Character: 0}) {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, ".field") || slices.Contains(labels, ".param") {
		t.Fatalf("directives = %v", labels)
	}
}

func TestAgentSections(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a_af.go"), []byte("package a\n\ntype SupportAgent struct {\n}\n"), 0o644)
	doc := openAt(t, filepath.Join(dir, "a.af"), ".title S\nHi.\n\n.tool t\nT.\n\n.agent support\n.tools t\n\n")
	syms := doc.SectionSymbols()
	if len(syms) != 2 || syms[1].Name != "support" || *syms[1].Detail != "agent → t" {
		t.Fatalf("symbols = %+v", syms)
	}
	if loc := doc.Definition(at(t, doc, "support", 0)); len(loc) != 1 || loc[0].Range.Start.Line != 2 {
		t.Fatalf("definition = %+v", loc)
	}
	var labels []string
	for _, it := range doc.GetCompletionItems(Position{Line: 8, Character: 0}) {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, ".maxturns") || !slices.Contains(labels, ".returns") || !slices.Contains(labels, ".serve") || slices.Contains(labels, ".param") {
		t.Fatalf("directives = %v", labels)
	}
}

func TestRouteEmptyHoverAndCompletion(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/note_copies.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	hover := doc.SectionHover(at(t, doc, "CopyNote", 0))
	if !strings.Contains(hover, `No row: 409 "that note is not yours, or you already have 5 notes".`) {
		t.Errorf("hover:\n%s", hover)
	}
	doc = openAt(t, path, ".route GET /x GetTodo\n.\n")
	var labels []string
	for _, c := range doc.directiveCompletions(Position{Line: 1, Character: 1}, []byte(".")) {
		labels = append(labels, c.Label)
	}
	if !slices.Contains(labels, ".empty") {
		t.Errorf("route completions: %v", labels)
	}
}

func TestRouteMapHover(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/cards.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	for query, want := range map[string]string{
		"ListTodos": "Responds `{\"cards\": [todoCard(row) for each row]}`.",
		"GetTodo":   "Responds `todoCard(row)`.",
	} {
		if hover := doc.SectionHover(at(t, doc, query, 0)); !strings.Contains(hover, want) {
			t.Errorf("%s hover, want %q:\n%s", query, want, hover)
		}
	}
}

func TestRouteWrapHover(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/notes.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	if hover := doc.SectionHover(at(t, doc, "ListNotes", 0)); !strings.Contains(hover, "Responds `{\"notes\": rows}`.") {
		t.Errorf("hover:\n%s", hover)
	}
	if hover := doc.SectionHover(at(t, doc, "CreateNote", 0)); strings.Contains(hover, "Responds `{") {
		t.Errorf("hover of a route without .wrap:\n%s", hover)
	}
}

func TestRouteCacheHover(t *testing.T) {
	path, err := filepath.Abs("../../examples/routes/todos.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))
	for query, want := range map[string]string{
		"CountTodos":   "`Cache-Control: public, max-age=60`.",
		"ListTodos":    "`Cache-Control: no-store`.",
		"GetTodoTitle": "",
		// 204: no body to cache.
		"SetTodoDone": "",
	} {
		hover := doc.SectionHover(at(t, doc, query, 0))
		if want == "" && strings.Contains(hover, "Cache-Control") || want != "" && !strings.Contains(hover, want) {
			t.Errorf("%s hover, want %q:\n%s", query, want, hover)
		}
	}
}

func TestRouteCompletionsFollowQueryKind(t *testing.T) {
	dir, err := filepath.Abs("../../examples/routes")
	require.NoError(t, err)
	labels := func(src string, line int) []string {
		t.Helper()
		doc := openAt(t, filepath.Join(dir, "x.af"), src)
		var out []string
		for _, c := range doc.directiveCompletions(Position{Line: line, Character: 1}, []byte(".")) {
			out = append(out, c.Label)
		}
		return out
	}
	for _, tt := range []struct {
		name, src string
		line      int
		want, not []string
	}{
		// The kind from .sql, even below the cursor.
		{"inline :execrows", ".route PUT /a/{id} SetX\n.\n.sql :execrows\nUPDATE t SET x = 1 WHERE id = @id\n", 1, []string{".empty", ".auth"}, []string{".wrap"}},
		{"inline :many", ".route GET /a ListX\n.\n.sql :many\nSELECT 1\n", 1, []string{".wrap", ".map", ".cache"}, []string{".empty"}},
		{"inline :exec", ".route POST /a TouchX\n.\n.sql :exec\nSELECT 1\n", 1, []string{".status"}, []string{".wrap", ".map", ".empty", ".cache"}},
		// The kind from sqlc's output: ListTodos is :many, GetTodo :one.
		{"sqlc :many", ".route GET /todos ListTodos\n.\n", 1, []string{".wrap"}, []string{".empty"}},
		{"sqlc :one", ".route GET /todos/{id} GetTodo\n.\n", 1, []string{".wrap", ".empty"}, nil},
		// A kind .sql doesn't take is unknown, not a filter.
		{"typo kind", ".route GET /a ListX\n.\n.sql :mnay\nSELECT 1\n", 1, []string{".wrap", ".empty"}, nil},
		// An unknown query offers both.
		{"unknown", ".route GET /x Nope\n.\n", 1, []string{".wrap", ".empty"}, nil},
		// Below .sql is SQL: no route directives.
		{"after .sql", ".route GET /a ListX\n.sql :many\nSELECT 1\n.\n", 3, nil, []string{".auth", ".wrap", ".empty", ".sql"}},
	} {
		got := labels(tt.src, tt.line)
		for _, w := range tt.want {
			if !slices.Contains(got, w) {
				t.Errorf("%s: missing %s in %v", tt.name, w, got)
			}
		}
		for _, n := range tt.not {
			if slices.Contains(got, n) {
				t.Errorf("%s: offers %s: %v", tt.name, n, got)
			}
		}
	}
}

func TestAgentToolsDefinitionAndCompletion(t *testing.T) {
	path, err := filepath.Abs("../../examples/agent/support.af")
	require.NoError(t, err)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := openAt(t, path, string(src))

	// A tool of another file of the package: its .tool line there.
	loc := doc.Definition(at(t, doc, "track_shipment", 0))
	if len(loc) != 1 || !strings.HasSuffix(string(loc[0].URI), "/examples/agent/shipping.af") || loc[0].Range.Start.Line != 0 {
		t.Fatalf("track_shipment definition = %+v", loc)
	}
	// A tool of this file, from the middle of its name.
	pos := at(t, doc, "lookup_order, track", 0)
	pos.Character += 3
	loc = doc.Definition(pos)
	want := doc.byteOffsetToPosition(strings.Index(string(src), ".tool lookup_order"))
	if len(loc) != 1 || loc[0].URI != protocol.DocumentUri(doc.URI) || loc[0].Range.Start.Line != uint32(want.Line) {
		t.Fatalf("lookup_order definition = %+v, want line %d", loc, want.Line)
	}
	// The .tools directive itself isn't a name.
	if loc := doc.Definition(at(t, doc, ".tools lookup_order, track", 0)); loc != nil {
		t.Fatalf(".tools definition = %+v", loc)
	}

	// Completion after .tools offers every tool of the package not yet listed.
	line := at(t, doc, ".tools lookup_order, track", 0).Line
	labels := map[string]string{}
	for _, it := range doc.directiveCompletions(Position{Line: line, Character: len(".tools lookup_order, ")}, []byte(".tools lookup_order, ")) {
		labels[it.Label] = it.Detail
	}
	if labels["track_shipment"] != "tool in shipping.af" || labels["offer_refund"] != "tool in this file" || labels["lookup_order"] != "" || len(labels) != 2 {
		t.Fatalf("completions = %v", labels)
	}
}
