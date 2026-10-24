package api

import (
	"encoding/json"
	"net/http"

	"example.com/mod/audit"
	"example.com/mod/db"
	"github.com/openai/openai-go"
)

type Server struct{ q *db.Queries }

// GetUser only serves one query: a .route candidate.
func (s *Server) GetUser(w http.ResponseWriter, r *http.Request) {
	n, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(n)
}

// Twice calls two queries: not a single-query route.
func (s *Server) Twice(w http.ResponseWriter, r *http.Request) {
	s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	s.q.GetUser(r.Context(), db.GetUserParams{ID: 2})
}

// Ban reads, checks, then writes: one .route with .empty.
func (s *Server) Ban(w http.ResponseWriter, r *http.Request) {
	n, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	if err != nil || n == 0 {
		http.Error(w, "not yours", 403)
		return
	}
	s.q.BanUser(r.Context(), n)
}

// CreateThenRead writes first: not a check.
func (s *Server) CreateThenRead(w http.ResponseWriter, r *http.Request) {
	s.q.CreateUser(r.Context())
	s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
}

// Either reads or writes, never both: not a check.
func (s *Server) Either(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	} else {
		s.q.BanUser(r.Context(), 1)
	}
}

// EitherCase reads or writes by case: not a check either.
func (s *Server) EitherCase(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/a":
		s.q.GetUser(r.Context(), db.GetUserParams{ID: 2})
	default:
		s.q.BanUser(r.Context(), 2)
	}
}

// Audited reads db.GetUser, then writes audit.GetUser: the Params type
// says which package each call is.
func (s *Server) Audited(w http.ResponseWriter, r *http.Request) {
	var a *audit.Queries
	if _, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1}); err != nil {
		return
	}
	a.GetUser(r.Context(), audit.GetUserParams{ID: 1})
}

// Unchecked writes whatever the read returned: no check to move into SQL.
func (s *Server) Unchecked(w http.ResponseWriter, r *http.Request) {
	n, _ := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	s.q.BanUser(r.Context(), n)
}

// Logged tests the read, but writes either way: not a check.
func (s *Server) Logged(w http.ResponseWriter, r *http.Request) {
	n, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	if err != nil {
		println(err.Error())
	}
	s.q.BanUser(r.Context(), n)
}

// Guarded writes inside the check: a check.
func (s *Server) Guarded(w http.ResponseWriter, r *http.Request) {
	if n, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1}); err == nil && n > 0 {
		s.q.BanUser(r.Context(), n)
	}
}

// Fallthrough runs the read's case into the write's: a check.
func (s *Server) Fallthrough(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		n, err := s.q.GetUser(r.Context(), db.GetUserParams{ID: 1})
		if err != nil || n == 0 {
			return
		}
		fallthrough
	case "PUT":
		s.q.BanUser(r.Context(), 1)
	}
}

// Transfer needs its transaction: not one statement.
func (s *Server) Transfer(w http.ResponseWriter, r *http.Request) {
	q := s.q.WithTx(nil)
	q.GetUser(r.Context(), db.GetUserParams{ID: 1})
	q.CreateUser(r.Context())
}

var lookup = openai.FunctionDefinitionParam{
	Name: "lookup",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
	},
}

func dynamicTool() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

var format = openai.ResponseFormatJSONSchemaParam{}

const system = `You are a careful assistant who reviews pull requests for a busy team. Read the diff, the description and the discussion, then decide whether the change is ready to merge. Never approve a change that has failing checks, and always explain your decision in one short paragraph that the author will understand without opening the code.`

const sql = `SELECT id, name, email, created_at FROM users WHERE id = $1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 100; SELECT id, name, email, created_at FROM users WHERE id = $1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 100; -- you are not a prompt, even though this text says "do not" and is long enough`
