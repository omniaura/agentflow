package routes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// The tools in todo_tools.af link queries sqlc already reads with .sqlc:
// two from the hand-written db/queries.sql, by file and name, one by name
// alone, and db/search.sql's only query by its file. This test runs them
// against Postgres.
func TestLinkedSQLTools(t *testing.T) {
	pool := testPool(t)
	if pool == nil {
		t.Skip("set AF_TEST_DATABASE_URL to run the SQL tools against Postgres")
	}
	ctx := context.Background()
	q := db.New(pool)
	todo := func(owner, title string) db.Todo {
		td, err := q.CreateTodo(ctx, db.CreateTodoParams{OwnerID: owner, Title: title, Meta: []byte("{}")})
		if err != nil {
			t.Fatal(err)
		}
		return td
	}
	milk, _, secret := todo("alice", "Buy milk"), todo("alice", "Walk the dog"), todo("mallory", "Buy secrets")
	tools := &TodoToolsSQLTools{Queries: q, Caller: func(context.Context) (Caller, error) { return Caller{OwnerID: "alice"}, nil }}

	got, err := tools.GetTodo(ctx, GetTodoArgs{Id: int(milk.ID)})
	if err != nil || !strings.Contains(got, `"title":"Buy milk"`) {
		t.Errorf("get_todo = %s, %v", got, err)
	}
	if _, err := tools.GetTodo(ctx, GetTodoArgs{Id: int(secret.ID)}); err == nil || err.Error() != "not found" {
		t.Errorf("get_todo of another owner's todo: %v", err)
	}
	if got, err := tools.FinishTodo(ctx, FinishTodoArgs{Id: int(milk.ID), Done: true}); err != nil || got != `{"rows_affected":1}` {
		t.Errorf("finish_todo = %s, %v", got, err)
	}
	if _, err := tools.FinishTodo(ctx, FinishTodoArgs{Id: int(secret.ID), Done: true}); err == nil || err.Error() != "not found" {
		t.Errorf("finish_todo of another owner's todo: %v", err)
	}

	// done is sqlc.narg('done'): left out, it is NULL and lists every todo.
	titles := func(out string) []string {
		var rows []struct{ Title string }
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		var ts []string
		for _, r := range rows {
			ts = append(ts, r.Title)
		}
		return ts
	}
	for _, tt := range []struct {
		args string
		want string
	}{
		{`{}`, "Buy milk,Walk the dog"},
		{`{"done":"true"}`, "Buy milk"},
		{`{"done":"false"}`, "Walk the dog"},
	} {
		args, err := ListTodosTool.Decode(tt.args)
		if err != nil {
			t.Fatalf("%s: %v", tt.args, err)
		}
		out, err := tools.ListTodos(ctx, args)
		if err != nil || strings.Join(titles(out), ",") != tt.want {
			t.Errorf("list_todos %s = %s, %v; want %s", tt.args, out, err, tt.want)
		}
	}
	if _, err := tools.ListTodos(ctx, ListTodosArgs{Done: "maybe"}); err == nil || !strings.Contains(err.Error(), `invalid done "maybe"`) {
		t.Errorf("list_todos done=maybe: %v", err)
	}

	out, err := tools.SearchTodos(ctx, SearchTodosArgs{Text: "BUY"})
	if err != nil || strings.Join(titles(out), ",") != "Buy milk" {
		t.Errorf("search_todos = %s, %v", out, err)
	}
	if !strings.Contains(string(SearchTodosTool.Schema), `"required":["text"]`) || strings.Contains(string(SearchTodosTool.Schema), "owner") {
		t.Errorf("search_todos schema = %s", SearchTodosTool.Schema)
	}
}
