package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// fakeNotes is an in-memory NotesQueries.
type fakeNotes struct {
	notes []db.Note
}

func (f *fakeNotes) CreateNote(ctx context.Context, arg db.CreateNoteParams) (db.Note, error) {
	n := db.Note{ID: int64(len(f.notes) + 1), OwnerID: arg.OwnerID, Body: arg.Body}
	f.notes = append(f.notes, n)
	return n, nil
}

func (f *fakeNotes) ListNotes(ctx context.Context, ownerID string) ([]db.Note, error) {
	var out []db.Note
	for _, n := range f.notes {
		if n.OwnerID == ownerID {
			out = append(out, n)
		}
	}
	slices.SortStableFunc(out, func(a, b db.Note) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		return int(a.ID - b.ID)
	})
	return out, nil
}

func (f *fakeNotes) update(id int64, owner string, fn func(*db.Note)) int64 {
	for i := range f.notes {
		if f.notes[i].ID == id && f.notes[i].OwnerID == owner {
			fn(&f.notes[i])
			return 1
		}
	}
	return 0
}

func (f *fakeNotes) UpdateNote(ctx context.Context, arg db.UpdateNoteParams) (int64, error) {
	return f.update(arg.ID, arg.OwnerID, func(n *db.Note) { n.Body = arg.Body }), nil
}

func (f *fakeNotes) PinNote(ctx context.Context, arg db.PinNoteParams) (int64, error) {
	return f.update(arg.ID, arg.OwnerID, func(n *db.Note) { n.Pinned = true }), nil
}

func notesQueries(t *testing.T) NotesQueries {
	pool := testPool(t)
	if pool == nil {
		return &fakeNotes{}
	}
	return db.New(pool)
}

// The notes routes write their SQL in notes.af: af gen writes notes_af.sql,
// runs sqlc, and generates the handlers from its output.
func TestNoteRoutes(t *testing.T) {
	mux := http.NewServeMux()
	(&NotesRoutes{
		Queries: notesQueries(t),
		Auth: func(r *http.Request) (Caller, error) {
			return Caller{OwnerID: r.Header.Get("X-User")}, nil
		},
	}).Register(mux)

	do := func(method, path, user, body string, wantStatus int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-User", user)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		b, _ := io.ReadAll(w.Body)
		if w.Code != wantStatus {
			t.Fatalf("%s %s: got %d %s, want %d", method, path, w.Code, b, wantStatus)
		}
		return strings.TrimSpace(string(b))
	}
	do("POST", "/notes", "ann", `{"body":"first"}`, 201)
	do("POST", "/notes", "ann", `{"body":"second"}`, 201)
	do("POST", "/notes", "bob", `{"body":"bob's"}`, 201)
	// The owner comes from the caller even when the query takes it alone.
	do("POST", "/notes", "ann", `{"body":"x","ownerId":"bob"}`, 400)
	do("PUT", "/notes/2/pin", "ann", "", 204)
	do("PUT", "/notes/3/pin", "ann", "", 404)
	do("PATCH", "/notes/1", "ann", `{"body":"edited"}`, 204)
	list := do("GET", "/notes", "ann", "", 200)
	if i, j := strings.Index(list, `"body":"second"`), strings.Index(list, `"body":"edited"`); i < 0 || j < 0 || i > j || strings.Contains(list, "bob") {
		t.Fatalf("list = %s", list)
	}
}
