package gogen

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

func genRoutesFile(t *testing.T, src string) (string, error) {
	t.Helper()
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("api.af", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup})
	return buf.String(), err
}

func TestGenRoutes(t *testing.T) {
	code, err := genRoutesFile(t, ".title Hello\nHi <!name>.\n\n.route GET /a/{ID} GetTodo\n.auth ownerId\n\n.route DELETE /a/{id} GetTodo\n.auth owner_id\n.status 200\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type ApiQueries interface {\n\tGetTodo(ctx context.Context, arg db.GetTodoParams) (db.Todo, error)\n}",
		// One query served twice gets a handler per method.
		`mux.HandleFunc("GET /a/{ID}", rt.GetTodoGet)`,
		`mux.HandleFunc("DELETE /a/{id}", rt.GetTodoDelete)`,
		// Names match whatever their case: {ID} is the ID field.
		`afhttp.Parse("ID", r.PathValue("ID"), &arg.ID)`,
		"arg.OwnerID = caller.OwnerID",
		"afhttp.WriteJSON(w, http.StatusOK, row)",
		"type Hello struct",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
}

func TestGenRoutesErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"unknown query", ".route GET /x Nope\n", "no sqlc query Nope"},
		{"unmatched path param", ".route GET /x/{todo} GetTodo\n", "path parameter todo matches no argument of query GetTodo (id, ownerId)"},
		{"unmatched auth", ".route GET /x/{id} GetTodo\n.auth user_id\n", "auth parameter user_id matches no argument"},
		{"no args", ".route GET /x/{id} CountTodos\n", "query CountTodos takes no arguments"},
		{"bound twice", ".route GET /x/{id} GetTodo\n.auth ID\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genRoutesFile(t, tt.src)
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(re.Error(), tt.msg) {
				t.Fatalf("got %v, want RouteError containing %q", err, tt.msg)
			}
			if re.Offset != strings.LastIndex(tt.src[:strings.IndexByte(tt.src, '\n')], " ")+1 {
				t.Fatalf("offset %d does not point at the query name", re.Offset)
			}
		})
	}
}

func TestGenRoutesNeedsSqlc(t *testing.T) {
	f, err := ast.NewFile("api.af", []byte(".route GET /x Q\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = GenFile(&bytes.Buffer{}, f, "api")
	if err == nil || !strings.Contains(err.Error(), "sqlc config") {
		t.Fatalf("got %v", err)
	}
}
