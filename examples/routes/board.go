package routes

import (
	"context"
	"net/http"

	"github.com/omniaura/agentflow/examples/routes/db"
	"github.com/omniaura/go-kit/errs"
)

// TodoBoard is the caller's todos in columns.
type TodoBoard struct {
	Owner string     `json:"owner"`
	Open  []TodoCard `json:"open"`
	Done  []TodoCard `json:"done"`
}

// maxBoard is the most todos a board shows.
const maxBoard = 100

var errBoardTooBig = errs.NewFactory(http.StatusUnprocessableEntity, "the board shows at most 100 todos; filter with ?done", errs.WithCode("board_too_big"))

// todoBoard is the .map func of GET /board in board.af. It takes all the
// rows, so it can group them, the request's context and the caller; its
// error fails the request.
func todoBoard(ctx context.Context, c Caller, todos []db.Todo) (TodoBoard, error) {
	if len(todos) > maxBoard {
		return TodoBoard{}, errBoardTooBig.New(ctx)
	}
	b := TodoBoard{Owner: c.OwnerID, Open: []TodoCard{}, Done: []TodoCard{}}
	for _, t := range todos {
		if t.Done {
			b.Done = append(b.Done, todoCard(t))
		} else {
			b.Open = append(b.Open, todoCard(t))
		}
	}
	return b, nil
}
