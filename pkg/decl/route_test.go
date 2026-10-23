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
