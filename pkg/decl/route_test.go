package decl

import (
	"errors"
	"reflect"
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
