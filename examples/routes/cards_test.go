package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// The card routes respond with what todoCard computes from each row,
// through the generated handlers' auth, errors and caching.
func TestCardRoutes(t *testing.T) {
	q := &fakeQueries{todos: []db.Todo{
		{ID: 1, OwnerID: "ann", Title: "milk", Done: true, Meta: []byte(`{"secret":1}`)},
		{ID: 2, OwnerID: "ann", Title: "bread"},
		{ID: 3, OwnerID: "bob", Title: "bob's"},
	}}
	mux := http.NewServeMux()
	(&CardsRoutes{Queries: q, Auth: func(r *http.Request) (Caller, error) {
		return Caller{OwnerID: r.Header.Get("X-User")}, nil
	}}).Register(mux)
	get := func(user, path string) (int, string, string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-User", user)
		mux.ServeHTTP(w, r)
		return w.Code, strings.TrimSpace(w.Body.String()), w.Header().Get("Cache-Control")
	}
	for _, c := range []struct {
		user, path string
		code       int
		body       string
	}{
		{"ann", "/cards", 200, `{"cards":[{"id":1,"title":"milk","status":"done","url":"/todos/1"},{"id":2,"title":"bread","status":"open","url":"/todos/2"}]}`},
		{"ann", "/cards?done=false", 200, `{"cards":[{"id":2,"title":"bread","status":"open","url":"/todos/2"}]}`},
		// No rows: an empty list, not null.
		{"cat", "/cards", 200, `{"cards":[]}`},
		{"ann", "/cards/1", 200, `{"id":1,"title":"milk","status":"done","url":"/todos/1"}`},
		// Bob's todo is out of ann's reach; the mapper never sees it.
		{"ann", "/cards/3", 404, ""},
	} {
		code, body, cache := get(c.user, c.path)
		if code != c.code || c.body != "" && body != c.body || cache != "no-store" {
			t.Errorf("%s %s: %d %s (Cache-Control %q)", c.user, c.path, code, body, cache)
		}
	}
}
