package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// CHECK constraints in db/schema.sql validate what clients and models
// write: a rejected value is a 400 naming the column, and a tool error the
// model can act on, with no Go validation code.
func TestConstraintsValidate(t *testing.T) {
	pool := testPool(t)
	if pool == nil {
		t.Skip("set AF_TEST_DATABASE_URL to check constraints against Postgres")
	}
	mux := http.NewServeMux()
	(&NotesRoutes{
		Queries: db.New(pool),
		Auth:    func(r *http.Request) (Caller, error) { return Caller{OwnerID: "ann"}, nil },
	}).Register(mux)
	post := func(body string) (int, string) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/notes", strings.NewReader(body)))
		b, _ := io.ReadAll(w.Body)
		return w.Code, string(b)
	}
	for _, tt := range []struct{ name, body string }{
		{"empty", `{"body":""}`},
		// The second CHECK on the column: Postgres names it notes_body_check1.
		{"too long", `{"body":"` + strings.Repeat("x", 2001) + `"}`},
	} {
		code, body := post(tt.body)
		if code != 400 || !strings.Contains(body, `"fields":{"body":"is invalid"}`) || strings.Contains(body, "SQLSTATE") {
			t.Errorf("%s: %d %s", tt.name, code, body)
		}
	}
	if code, body := post(`{"body":"fine"}`); code != 201 {
		t.Fatalf("valid note: %d %s", code, body)
	}

	tools := &AssistantSQLTools{Queries: db.New(pool), Caller: func(context.Context) (Caller, error) { return Caller{OwnerID: "ann"}, nil }}
	if _, err := tools.AddNote(context.Background(), AddNoteArgs{Body: ""}); err == nil || err.Error() != "invalid request; body is invalid" {
		t.Errorf("tool error = %v", err)
	}
	if out, err := tools.AddNote(context.Background(), AddNoteArgs{Body: "from the model"}); err != nil || !strings.Contains(out, "from the model") {
		t.Errorf("AddNote = %s, %v", out, err)
	}
}
