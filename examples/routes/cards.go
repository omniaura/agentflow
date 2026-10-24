package routes

import (
	"fmt"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// TodoCard is a todo as the board shows it. Its fields are computed in Go,
// and the owner and metadata stay out of the response.
type TodoCard struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

// todoCard is the .map func of the card routes in cards.af: the generated
// handlers pass it each row.
func todoCard(t db.Todo) TodoCard {
	status := "open"
	if t.Done {
		status = "done"
	}
	return TodoCard{ID: t.ID, Title: t.Title, Status: status, URL: fmt.Sprintf("/todos/%d", t.ID)}
}
