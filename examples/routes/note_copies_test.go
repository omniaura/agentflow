package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
	"github.com/omniaura/go-kit/errs"
)

// CopyNote checks ownership and a limit in its SQL; .empty names the
// failure when the INSERT ... SELECT inserts nothing.
func TestCopyNote(t *testing.T) {
	pool := testPool(t)
	if pool == nil {
		t.Skip("set AF_TEST_DATABASE_URL to run the route against Postgres")
	}
	q := db.New(pool)
	ctx := context.Background()
	mine, err := q.CreateNote(ctx, db.CreateNoteParams{OwnerID: "ann", Body: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := q.CreateNote(ctx, db.CreateNoteParams{OwnerID: "bob", Body: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&NoteCopiesRoutes{
		Queries: q,
		Auth:    func(r *http.Request) (Caller, error) { return Caller{OwnerID: r.Header.Get("X-User")}, nil },
	}).Register(mux)
	post := func(user string, id int64) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", fmt.Sprintf("/notes/%d/copies", id), nil)
		r.Header.Set("X-User", user)
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	conflict := func(what string, code int, body string) {
		t.Helper()
		var pub errs.Public
		if code != 409 || json.Unmarshal([]byte(body), &pub) != nil || pub.Message != "that note is not yours, or you already have 5 notes" || pub.Code != "conflict" {
			t.Fatalf("%s: got %d %s", what, code, body)
		}
	}

	// Ann has 1 note: four copies bring her to the limit of 5.
	for i := range 4 {
		code, body := post("ann", mine.ID)
		if code != 201 || !strings.Contains(body, `"body":"plan"`) || !strings.Contains(body, `"ownerId":"ann"`) {
			t.Fatalf("copy %d: got %d %s", i, code, body)
		}
	}
	code, body := post("ann", mine.ID)
	conflict("a sixth note", code, body)
	// Bob's note is out of reach, though bob is under his limit.
	code, body = post("ann", theirs.ID)
	conflict("ann copying bob's note", code, body)
	code, body = post("bob", theirs.ID)
	if code != 201 {
		t.Fatalf("bob copying his note: got %d %s", code, body)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notes WHERE owner_id = 'ann'").Scan(&n); err != nil || n != 5 {
		t.Fatalf("ann has %d notes (%v), want 5", n, err)
	}
}
