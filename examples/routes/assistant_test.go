package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/examples/routes/db"
	"github.com/omniaura/agentflow/runtime/afagent"
)

type callerKey struct{}

// The tools of the notes agent are written in SQL in assistant.af: af gen
// writes assistant_af.sql, runs sqlc, and generates AssistantSQLTools. This
// test runs them against Postgres, through the agent's HTTP endpoint, with
// a scripted model.
func TestNotesAssistant(t *testing.T) {
	pool := testPool(t)
	if pool == nil {
		t.Skip("set AF_TEST_DATABASE_URL to run the SQL tools against Postgres")
	}
	ctx := context.Background()
	q := db.New(pool)
	var milk, secret db.Note
	for _, n := range []struct {
		owner, body string
		into        *db.Note
	}{{"alice", "buy milk", &milk}, {"alice", "call bob", nil}, {"alice", "50% off", nil}, {"mallory", "secret plans", &secret}} {
		note, err := q.CreateNote(ctx, db.CreateNoteParams{OwnerID: n.owner, Body: n.body})
		if err != nil {
			t.Fatal(err)
		}
		if n.into != nil {
			*n.into = note
		}
	}

	// The model finds the milk note, pins it, and tries to pin mallory's.
	var reqs []afagent.Request
	turns := []afagent.Message{
		{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "find_notes", Arguments: `{"text":"MILK"}`}}},
		{ToolCalls: []afagent.ToolCall{
			{ID: "2", Name: "pin_note", Arguments: fmt.Sprintf(`{"note_id":%d}`, milk.ID)},
			{ID: "3", Name: "pin_note", Arguments: fmt.Sprintf(`{"note_id":%d}`, secret.ID)},
		}},
		{ToolCalls: []afagent.ToolCall{
			// % is text, not a wildcard: only "50% off" matches.
			{ID: "4", Name: "find_notes", Arguments: `{"text":"0%"}`},
			{ID: "5", Name: "unpin_notes", Arguments: fmt.Sprintf(`{"note_ids":[%d,%d]}`, milk.ID, secret.ID)},
		}},
		{Content: "Pinned \"buy milk\"."},
	}
	model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		reqs = append(reqs, req)
		if len(reqs) > len(turns) {
			return afagent.Response{}, errors.New("unexpected model call")
		}
		return afagent.Response{Message: turns[len(reqs)-1]}, nil
	})

	tools := &AssistantSQLTools{
		Queries: q,
		Caller: func(ctx context.Context) (Caller, error) {
			c, ok := ctx.Value(callerKey{}).(Caller)
			if !ok {
				return Caller{}, errors.New("no caller")
			}
			return c, nil
		},
	}
	mux := http.NewServeMux()
	(&NotesAgentServer{
		Agent: &NotesAgent{Model: model, Tools: tools},
		Prepare: func(r *http.Request) (*NotesAssistant, []afagent.Message, error) {
			return &NotesAssistant{Name: "Alice"}, nil, nil
		},
	}).Register(mux)
	// Authentication middleware puts the caller in the request's context.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, Caller{OwnerID: "alice"})))
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/assistant", strings.NewReader(`{"message":"pin my milk note"}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `Pinned \"buy milk\".`) {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	// The tool parameters were inferred from the queries: note_id from
	// @note_id, typed as an integer; owner_id is the caller's, not the
	// model's.
	if !strings.Contains(string(PinNoteTool.Schema), `"note_id":{"type":"integer"}`) || strings.Contains(string(PinNoteTool.Schema), "owner") {
		t.Errorf("pin_note schema = %s", PinNoteTool.Schema)
	}

	// The last turn saw the wildcard search and the unpin: only alice's
	// note changed, and the id list was converted to bigint[].
	last := reqs[3].Messages
	if wild, unpinned := last[7], last[8]; !strings.Contains(wild.Content, "50% off") || strings.Contains(wild.Content, "milk") || unpinned.Content != `{"rows_affected":1}` {
		t.Errorf("find 0%% = %q, unpin = %+v", wild.Content, unpinned)
	}
	msgs := reqs[2].Messages
	found, pinned, denied := msgs[2], msgs[4], msgs[5]
	var rows []db.FindNotesToolRow
	if err := json.Unmarshal([]byte(found.Content), &rows); err != nil || len(rows) != 1 || rows[0].ID != milk.ID {
		t.Errorf("find_notes returned %q (%v)", found.Content, err)
	}
	if pinned.IsError || pinned.Content != `{"rows_affected":1}` {
		t.Errorf("pin_note = %+v", pinned)
	}
	// Mallory's note is out of reach: the same answer as a missing note.
	if !denied.IsError || denied.Content != "not found" {
		t.Errorf("pinning another owner's note = %+v", denied)
	}
	for _, n := range []struct {
		id   int64
		want bool
	}{{milk.ID, false}, {secret.ID, false}} {
		var got bool
		if err := pool.QueryRow(ctx, "SELECT pinned FROM notes WHERE id = $1", n.id).Scan(&got); err != nil || got != n.want {
			t.Errorf("note %d pinned = %v (%v), want %v", n.id, got, err, n.want)
		}
	}
}

func TestSQLToolErrorsStayInternal(t *testing.T) {
	tools := &AssistantSQLTools{Queries: failingQueries{}, Caller: func(context.Context) (Caller, error) { return Caller{OwnerID: "a"}, nil }}
	_, err := tools.ListNotes(context.Background(), ListNotesArgs{})
	if err == nil || err.Error() != "internal error" || !strings.Contains(errors.Unwrap(err).Error(), "connection refused") {
		t.Fatalf("got %v", err)
	}
	_, err = (&AssistantSQLTools{Queries: failingQueries{}}).ListNotes(context.Background(), ListNotesArgs{})
	if err == nil || err.Error() != "internal error" {
		t.Fatalf("no Caller: got %v", err)
	}
	tools.Caller = func(context.Context) (Caller, error) { return Caller{}, errors.New("token expired") }
	if _, err := tools.PinNote(context.Background(), PinNoteArgs{NoteId: 1}); err == nil || err.Error() != "unauthorized" {
		t.Fatalf("bad caller: got %v", err)
	}
}

type failingQueries struct{}

func (failingQueries) ListNotesTool(context.Context, string) ([]db.ListNotesToolRow, error) {
	return nil, errors.New("dial tcp: connection refused")
}
func (failingQueries) FindNotesTool(context.Context, db.FindNotesToolParams) ([]db.FindNotesToolRow, error) {
	return nil, errors.New("dial tcp: connection refused")
}
func (failingQueries) PinNoteTool(context.Context, db.PinNoteToolParams) (int64, error) {
	return 0, errors.New("dial tcp: connection refused")
}
func (failingQueries) UnpinNotesTool(context.Context, db.UnpinNotesToolParams) (int64, error) {
	return 0, errors.New("dial tcp: connection refused")
}
