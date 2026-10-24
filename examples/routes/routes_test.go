package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/omniaura/agentflow/examples/routes/db"
)

// fakeQueries is an in-memory TodosQueries, so the routes can be tested
// without Postgres. Set AF_TEST_DATABASE_URL to run them against a real
// database with the sqlc-generated queries instead.
type fakeQueries struct {
	todos []db.Todo
}

func (f *fakeQueries) CreateTodo(ctx context.Context, arg db.CreateTodoParams) (db.Todo, error) {
	t := db.Todo{ID: int64(len(f.todos) + 1), OwnerID: arg.OwnerID, Title: arg.Title, Meta: arg.Meta}
	f.todos = append(f.todos, t)
	return t, nil
}

func (f *fakeQueries) find(id int64, owner string) int {
	return slices.IndexFunc(f.todos, func(t db.Todo) bool { return t.ID == id && t.OwnerID == owner })
}

func (f *fakeQueries) GetTodo(ctx context.Context, arg db.GetTodoParams) (db.Todo, error) {
	if i := f.find(arg.ID, arg.OwnerID); i >= 0 {
		return f.todos[i], nil
	}
	return db.Todo{}, pgx.ErrNoRows
}

func (f *fakeQueries) ListTodos(ctx context.Context, arg db.ListTodosParams) ([]db.Todo, error) {
	var out []db.Todo
	for _, t := range f.todos {
		if t.OwnerID == arg.OwnerID && (!arg.Done.Valid || t.Done == arg.Done.Bool) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeQueries) SetTodoDone(ctx context.Context, arg db.SetTodoDoneParams) (int64, error) {
	i := f.find(arg.ID, arg.OwnerID)
	if i < 0 {
		return 0, nil
	}
	f.todos[i].Done = arg.Done
	return 1, nil
}

func (f *fakeQueries) DeleteTodo(ctx context.Context, arg db.DeleteTodoParams) (int64, error) {
	i := f.find(arg.ID, arg.OwnerID)
	if i < 0 {
		return 0, nil
	}
	f.todos = slices.Delete(f.todos, i, i+1)
	return 1, nil
}

func (f *fakeQueries) CountTodos(ctx context.Context) (int64, error) {
	return int64(len(f.todos)), nil
}

func (f *fakeQueries) GetTodoTitle(ctx context.Context, id int64) (string, error) {
	for _, t := range f.todos {
		if t.ID == id {
			return t.Title, nil
		}
	}
	return "", pgx.ErrNoRows
}

func queries(t *testing.T) TodosQueries {
	pool := testPool(t)
	if pool == nil {
		return &fakeQueries{}
	}
	return db.New(pool)
}

func TestUnsetAuthFailsClosed(t *testing.T) {
	mux := http.NewServeMux()
	(&TodosRoutes{Queries: &fakeQueries{}}).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/todos", nil))
	if w.Code != 500 {
		t.Fatalf("got %d, want 500", w.Code)
	}
	// Public routes don't need Auth.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/stats/count", nil))
	if w.Code != 200 {
		t.Fatalf("public route: got %d", w.Code)
	}
}

func TestTodoRoutes(t *testing.T) {
	mux := http.NewServeMux()
	(&TodosRoutes{
		Queries: queries(t),
		Auth: func(r *http.Request) (Caller, error) {
			user, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				return Caller{}, errors.New("no token")
			}
			return Caller{OwnerID: user}, nil
		},
	}).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	do := func(method, path, user, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if user != "" {
			req.Header.Set("Authorization", "Bearer "+user)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	expect := func(method, path, user, body string, wantStatus int, wantBody string) string {
		t.Helper()
		status, got := do(method, path, user, body)
		if status != wantStatus || (wantBody != "" && !strings.Contains(got, wantBody)) {
			t.Fatalf("%s %s: got %d %s, want %d containing %s", method, path, status, got, wantStatus, wantBody)
		}
		return got
	}

	expect("POST", "/todos", "", `{"title":"x"}`, 401, `"code":"unauthorized"`)
	created := expect("POST", "/todos", "ann", `{"title":"Write docs","meta":{"tags":["a"]}}`, 201, `"title":"Write docs"`)
	var todo db.Todo
	if err := json.Unmarshal([]byte(created), &todo); err != nil || todo.OwnerID != "ann" || string(todo.Meta) != `{"tags":["a"]}` && string(todo.Meta) != `{"tags": ["a"]}` {
		t.Fatalf("created %s: %+v %v", created, todo, err)
	}
	// The owner comes from the caller, never the body.
	expect("POST", "/todos", "ann", `{"title":"x","ownerId":"bob"}`, 400, `unknown field \"ownerId\"`)
	expect("POST", "/todos", "ann", `{"title":"x"} {}`, 400, "unexpected data")
	expect("POST", "/todos", "ann", ``, 400, `"fields":{"body":"is empty; send a JSON object"}`)
	expect("POST", "/todos", "bob", `{"title":"Bob's"}`, 201, `"ownerId":"bob"`)
	// A unique violation is a 409; the client never sees the SQL error.
	if got := expect("POST", "/todos", "bob", `{"title":"Bob's"}`, 409, `"code":"already_exists"`); strings.Contains(got, "duplicate") {
		t.Fatalf("the cause reached the client: %s", got)
	}

	expect("GET", "/todos/1", "ann", "", 200, `"title":"Write docs"`)
	expect("GET", "/todos/1", "bob", "", 404, `"message":"not found","code":"not_found"`)
	expect("GET", "/todos/abc", "ann", "", 400, `"fields":{"id":"invalid value \"abc\""}`)

	expect("PUT", "/todos/1/done", "ann", `{"done":true}`, 204, "")
	expect("PUT", "/todos/1/done", "bob", `{"done":true}`, 404, "")
	expect("GET", "/todos?done=true", "ann", "", 200, `"done":true`)
	if _, got := do("GET", "/todos?done=false", "ann", ""); got != "[]" {
		t.Fatalf("done=false lists %s, want []", got)
	}
	expect("GET", "/todos?done=maybe", "ann", "", 400, `"fields":{"done":"invalid value \"maybe\""}`)

	expect("GET", "/stats/count", "", "", 200, "2")
	// Responses to a caller are never cached; .cache 1m lets caches keep
	// the public count, but not its errors.
	for _, c := range []struct{ method, path, user, want string }{
		{"GET", "/todos/2", "bob", "no-store"},
		{"GET", "/todos", "ann", "no-store"},
		{"GET", "/todos/1", "bob", ""},
		{"GET", "/stats/count", "", "public, max-age=60"},
		{"GET", "/todos/2/title", "", ""},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, nil)
		if c.user != "" {
			req.Header.Set("Authorization", "Bearer "+c.user)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Cache-Control"); got != c.want {
			t.Errorf("%s %s: Cache-Control %q, want %q", c.method, c.path, got, c.want)
		}
	}
	expect("GET", "/todos/2/title", "", "", 200, `"Bob's"`)
	expect("DELETE", "/todos/1", "bob", "", 404, "")
	expect("DELETE", "/todos/1", "ann", "", 204, "")
	expect("GET", "/todos/1/title", "", "", 404, "")
}
