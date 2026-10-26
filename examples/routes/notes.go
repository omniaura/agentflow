package routes

import (
	"strings"

	"github.com/omniaura/agentflow/examples/routes/db"
)

// tidyNote is the .prepare func of POST /notes in notes.af: the generated
// handler passes it the decoded arguments before the query. A body of
// only spaces trims to "", which the table's CHECK rejects as a 400.
func tidyNote(p *db.CreateNoteParams) {
	p.Body = strings.TrimSpace(p.Body)
}
