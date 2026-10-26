package decl

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseRoute(t *testing.T) {
	src := ".route PATCH /agents/{agent_id}/draft/{rest...} UpsertDraft\n.auth user_id, kg_id\n.auth user_id\n.status 201\n.maxbytes 4096\nSaves a draft.\n\n.route GET /health db.Health\n.public\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Routes) != 2 || len(f.Tools) != 0 {
		t.Fatalf("got %d routes, %d tools", len(f.Routes), len(f.Tools))
	}
	r := f.Routes[0]
	want := Route{
		Method: "PATCH", Path: "/agents/{agent_id}/draft/{rest...}", PathParams: []string{"agent_id", "rest"},
		Query: "UpsertDraft", Auth: []string{"user_id", "kg_id"}, Status: 201, MaxBytes: 4096,
		Description: "Saves a draft.", Start: 0, End: r.End, QueryOffset: strings.Index(src, "UpsertDraft"),
	}
	if !reflect.DeepEqual(*r, want) {
		t.Fatalf("got  %+v\nwant %+v", *r, want)
	}
	if r.BodyFromQueryString() || !f.Routes[1].BodyFromQueryString() {
		t.Fatal("BodyFromQueryString")
	}
	if h := f.Routes[1]; h.Query != "db.Health" || !h.Public || len(h.PathParams) != 0 {
		t.Fatalf("health = %+v", h)
	}
}

func TestParseRouteErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"missing fields", ".route GET /x\n", "expected .route <METHOD> <path> <Query>"},
		{"bad method", ".route FETCH /x Q\n", `method "FETCH"`},
		{"relative path", ".route GET x Q\n", `must start with /`},
		{"bad path param", ".route GET /{a-b} Q\n", `{a-b} must be an identifier`},
		{"repeated path param", ".route GET /{a}/{a} Q\n", `{a} appears twice`},
		{"lowercase query", ".route GET /x getX\n", `query "getX"`},
		{"auth path param", ".route GET /{id} Q\n.auth id\n", `"id" is a path parameter`},
		{"public auth", ".route GET /x Q\n.public\n.auth user_id\n", "cannot use .auth"},
		{"bad status", ".route GET /x Q\n.status 404\n", ".status needs a 2xx"},
		{"duplicate route", ".route GET /x Q\n.route GET /x R\n", "route GET /x is declared twice"},
		{"sql without kind", ".route GET /x Q\n.sql\nSELECT 1\n", ".sql needs the query's sqlc command"},
		{"sql bad kind", ".route GET /x Q\n.sql :batchone\nSELECT 1\n", `got ":batchone"`},
		{"sql empty", ".route GET /x Q\n.sql :one\n\n", "write the query on the lines after .sql"},
		{"sql qualified", ".route GET /x db.Q\n.sql :one\nSELECT 1\n", "cannot name a package"},
		{"sql twice", ".route GET /x Q\n.sql :one\nSELECT 1\n.route GET /y Q\n.sql :one\nSELECT 2\n", "query Q is written twice"},
		{"empty without message", ".route GET /x Q\n.empty 409\n", "usage: .empty [4xx status] message"},
		{"empty blank", ".route GET /x Q\n.empty\n", "usage: .empty"},
		{"empty 5xx", ".route GET /x Q\n.empty 503 down\n", `.empty needs a 4xx status, got "503"`},
		{"empty twice", ".route GET /x Q\n.empty a\n.empty b\n", "only one .empty line"},
		{"wrap blank", ".route GET /x Q\n.wrap\n", "usage: .wrap key"},
		{"wrap two words", ".route GET /x Q\n.wrap the todos\n", `got "the todos"`},
		{"wrap dash", ".route GET /x Q\n.wrap my-todos\n", "usage: .wrap key"},
		{"wrap twice", ".route GET /x Q\n.wrap a\n.wrap b\n", "only one .wrap line"},
		{"wrap after sql", ".route GET /x Q\n.sql :many\nSELECT 1\n.wrap rows\n", ".wrap must come before .sql"},
		{"bind no equals", ".route GET /x Q\n.bind id memory_id\n", "usage: .bind arg = name"},
		{"bind blank name", ".route GET /x Q\n.bind id =\n", "usage: .bind arg = name"},
		{"bind dash", ".route GET /x Q\n.bind id = memory-id\n", "usage: .bind arg = name"},
		{"bind arg twice", ".route GET /x Q\n.bind id = a\n.bind id = b\n", "id is bound twice"},
		{"bind name twice", ".route GET /x Q\n.bind a = x\n.bind b = x\n", "x already binds a"},
		{"bind auth", ".route GET /x Q\n.bind owner_id = user\n.auth owner_id\n", "owner_id comes from .auth"},
		{"bind auth name", ".route GET /x Q\n.bind owner = user_id\n.auth user_id\n", "user_id comes from .auth"},
		{"bind auth other case", ".route GET /x Q\n.bind ownerId = user\n.auth owner_id\n", "owner_id comes from .auth"},
		{"bind arg twice other case", ".route GET /x Q\n.bind id = a\n.bind ID = b\n", "ID is bound twice"},
		{"bind name twice other case", ".route GET /x Q\n.bind a = user_id\n.bind b = userId\n", "userId already binds a"},
		{"empty after sql", ".route GET /x Q\n.sql :one\nSELECT 1\n.empty gone\n", ".empty must come before .sql"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src))
			var de *Error
			if !errors.As(err, &de) || !strings.Contains(de.Msg, tt.msg) {
				t.Fatalf("got %v, want error containing %q", err, tt.msg)
			}
		})
	}
}

func TestParseRouteSQL(t *testing.T) {
	src := ".route PATCH /notes/{id} UpdateNote\n.auth owner_id\nReplaces a note.\n.sql :execrows\nUPDATE notes SET body = @body\nWHERE id = @id;\n\n.route GET /notes/{id} UpdateNote\n.auth owner_id\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routes[0]
	if r.SQLKind != ":execrows" || r.SQL != "UPDATE notes SET body = @body\nWHERE id = @id;" || r.Description != "Replaces a note." || r.SQLOffset != strings.Index(src, ".sql") {
		t.Fatalf("route = %+v", r)
	}
	// A second route may serve a query written elsewhere in the file.
	if f.Routes[1].SQL != "" || f.Routes[1].Query != "UpdateNote" {
		t.Fatalf("second route = %+v", f.Routes[1])
	}
}

func TestParseRouteEmpty(t *testing.T) {
	for _, tt := range []struct {
		line   string
		status int
		msg    string
	}{
		{".empty 409 you already have 5 notes", 409, "you already have 5 notes"},
		{".empty that note is gone", 404, "that note is gone"},
		{".empty 409\tafter a tab", 409, "after a tab"},
		// A leading number that is not a status is part of the message.
		{".empty 5 notes at most", 404, "5 notes at most"},
		{".empty 2025 was a year", 404, "2025 was a year"},
	} {
		f, err := Parse([]byte(".route GET /x Q\n" + tt.line + "\n"))
		if err != nil {
			t.Fatalf("%s: %v", tt.line, err)
		}
		e := f.Routes[0].Empty
		if e == nil || e.Status != tt.status || e.Msg != tt.msg || e.Offset != len(".route GET /x Q\n") {
			t.Errorf("%s: got %+v", tt.line, e)
		}
	}
}

func TestParseRouteWrap(t *testing.T) {
	f, err := Parse([]byte(".route GET /todos ListTodos\n.wrap todos\nLists todos.\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Routes[0]
	if r.Wrap != "todos" || r.WrapOffset != len(".route GET /todos ListTodos\n") || r.Description != "Lists todos." {
		t.Fatalf("got %q at %d, description %q", r.Wrap, r.WrapOffset, r.Description)
	}
}

func TestParseRouteCache(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Authenticated responses are never cached by default.
		{".route GET /todos ListTodos\n", "no-store"},
		{".route GET /todos ListTodos\n.public\n", ""},
		// A duration is private to the caller, or public on a .public route,
		// wherever .public appears.
		{".route GET /todos ListTodos\n.cache 5m\n", "private, max-age=300"},
		{".route GET /todos ListTodos\n.cache 1h\n.public\n", "public, max-age=3600"},
		{".route HEAD /todos ListTodos\n.cache 30s\n", "private, max-age=30"},
		{".route GET /todos ListTodos\n.public\n.cache 8760h\n", "public, max-age=31536000"},
		// Anything else is written as is, on any method.
		{".route GET /todos ListTodos\n.public\n.cache no-cache\n", "no-cache"},
		{".route POST /todos CreateTodo\n.cache private, no-store\n", "private, no-store"},
	} {
		f, err := Parse([]byte(c.src))
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		r := f.Routes[0]
		if got := r.CacheControl(); got != c.want {
			t.Errorf("%q: Cache-Control %q, want %q", c.src, got, c.want)
		}
		// Only a private max-age needs Vary.
		if got, want := r.Vary() != "", strings.HasPrefix(c.want, "private, max-age"); got != want {
			t.Errorf("%q: Vary %q", c.src, r.Vary())
		}
	}
	for _, c := range []struct{ src, want string }{
		{".route GET /a ListTodos\n.cache\n", "usage: .cache 5m"},
		{".route GET /a ListTodos\n.cache 500ms\n", "whole seconds, at least 1s"},
		{".route GET /a ListTodos\n.cache 1.5s\n", "whole seconds"},
		{".route GET /a ListTodos\n.cache 5 minutes\n", "duration such as 30s, 5m or 1h"},
		{".route GET /a ListTodos\n.cache 1d\n", "duration such as 30s, 5m or 1h"},
		// Past a year, and past what int holds on 32-bit platforms.
		{".route GET /a ListTodos\n.cache 8761h\n", "at most a year"},
		{".route GET /a ListTodos\n.cache 3000000000s\n", "at most a year"},
		{".route GET /a ListTodos\n.cache 1m\n.cache 2m\n", "only one .cache line"},
		{".route POST /a CreateTodo\n.cache 5m\n", "only GET and HEAD responses are cached"},
		{".route GET /a ListTodos\n.cache no-\x7fstore\n", "printable ASCII"},
	} {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.src, err, c.want)
		}
	}
}

func TestParseRouteMap(t *testing.T) {
	f, err := Parse([]byte(".route GET /cards ListTodos\n.map todoCard\n.wrap cards\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Routes[0]; r.Map != "todoCard" || r.MapOffset != len(".route GET /cards ListTodos\n") {
		t.Fatalf("got %q at %d", r.Map, r.MapOffset)
	}
	for src, want := range map[string]string{
		".route GET /a ListTodos\n.map\n":           "usage: .map fn",
		".route GET /a ListTodos\n.map db.toCard\n": "usage: .map fn",
		".route GET /a ListTodos\n.map to card\n":   "usage: .map fn",
		".route GET /a ListTodos\n.map a\n.map b\n": "only one .map line",
	} {
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}

func TestParseRouteBind(t *testing.T) {
	src := ".route GET /memories/{memory_id} GetMemory\n.bind id = memory_id\n.bind owner = user_id\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []Bind{{Arg: "id", Name: "memory_id", Offset: strings.Index(src, ".bind id")}, {Arg: "owner", Name: "user_id", Offset: strings.Index(src, ".bind owner")}}
	if got := f.Routes[0].Binds; !slices.Equal(got, want) {
		t.Fatalf("Binds = %+v, want %+v", got, want)
	}
}

func TestParseRoutePrepare(t *testing.T) {
	src := ".route POST /notes CreateNote\n.prepare tidyNote\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Routes[0]; r.Prepare != "tidyNote" || r.PrepareOffset != strings.Index(src, ".prepare") {
		t.Fatalf("got %q at %d", r.Prepare, r.PrepareOffset)
	}
	for src, want := range map[string]string{
		".route POST /a Q\n.prepare\n":                        "usage: .prepare fn",
		".route POST /a Q\n.prepare db.tidy\n":                "usage: .prepare fn",
		".route POST /a Q\n.prepare a\n.prepare b\n":          "only one .prepare line",
		".route POST /a Q\n.sql :one\nSELECT 1\n.prepare a\n": ".prepare must come before .sql",
	} {
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}
