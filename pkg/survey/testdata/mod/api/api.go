package api

import (
	"encoding/json"
	"net/http"

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
