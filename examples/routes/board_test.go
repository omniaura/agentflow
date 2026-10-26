package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// GET /board responds with what todoBoard makes of all the caller's rows,
// and fails as it does.
func TestBoardRoute(t *testing.T) {
	q := &fakeQueries{todos: []db.Todo{
		{ID: 1, OwnerID: "ann", Title: "milk", Done: true},
		{ID: 2, OwnerID: "ann", Title: "bread"},
		{ID: 3, OwnerID: "bob", Title: "bob's"},
	}}
	for i := range maxBoard + 1 {
		q.todos = append(q.todos, db.Todo{ID: int64(10 + i), OwnerID: "cat", Title: fmt.Sprint("task ", i)})
	}
	mux := http.NewServeMux()
	(&BoardRoutes{Queries: q, Auth: func(r *http.Request) (Caller, error) {
		return Caller{OwnerID: r.Header.Get("X-User")}, nil
	}}).Register(mux)
	get := func(user, path string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-User", user)
		mux.ServeHTTP(w, r)
		return w.Code, strings.TrimSpace(w.Body.String())
	}
	if code, body := get("ann", "/board"); code != 200 || body != `{"owner":"ann","open":[{"id":2,"title":"bread","status":"open","url":"/todos/2"}],"done":[{"id":1,"title":"milk","status":"done","url":"/todos/1"}]}` {
		t.Errorf("ann's board: %d %s", code, body)
	}
	if code, body := get("dan", "/board"); code != 200 || body != `{"owner":"dan","open":[],"done":[]}` {
		t.Errorf("an empty board: %d %s", code, body)
	}
	if code, body := get("cat", "/board"); code != 422 || !strings.Contains(body, `"code":"board_too_big"`) {
		t.Errorf("too big a board: %d %s", code, body)
	}
}
