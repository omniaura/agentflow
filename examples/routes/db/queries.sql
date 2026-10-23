-- name: CreateTodo :one
INSERT INTO todos (owner_id, title, meta) VALUES ($1, $2, $3) RETURNING *;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = $1 AND owner_id = $2;

-- name: ListTodos :many
SELECT * FROM todos WHERE owner_id = $1 AND (sqlc.narg('done')::boolean IS NULL OR done = sqlc.narg('done')) ORDER BY id;

-- name: SetTodoDone :execrows
UPDATE todos SET done = $3 WHERE id = $1 AND owner_id = $2;

-- name: DeleteTodo :execrows
DELETE FROM todos WHERE id = $1 AND owner_id = $2;

-- name: CountTodos :one
SELECT count(*) FROM todos;

-- name: GetTodoTitle :one
SELECT title FROM todos WHERE id = $1;
