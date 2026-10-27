// Package routes serves a todo API from todos.af and the sqlc queries in db.
package routes

//go:generate go tool af gen prompts .

// Caller is the authenticated user. Its fields are the .auth names.
type Caller struct {
	OwnerID string
}
