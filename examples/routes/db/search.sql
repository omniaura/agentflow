-- name: SearchTodos :many
SELECT id, title, done FROM todos
WHERE owner_id = @owner_id AND strpos(lower(title), lower(@text)) > 0
ORDER BY id;
