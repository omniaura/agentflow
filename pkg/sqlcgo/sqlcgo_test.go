package sqlcgo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = "../../examples/routes"

func TestFindExample(t *testing.T) {
	repo, err := Find(example)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.Packages) != 1 {
		t.Fatalf("got %d packages", len(repo.Packages))
	}
	p := repo.Packages[0]
	if p.Name != "db" || p.ImportPath != "github.com/omniaura/agentflow/examples/routes/db" || p.SQLPackage != "pgx/v5" {
		t.Fatalf("package = %+v", p)
	}
	if path, expr := p.NoRowsImport(); path != "github.com/jackc/pgx/v5" || expr != "pgx.ErrNoRows" {
		t.Fatalf("NoRowsImport = %s %s", path, expr)
	}
	kinds := map[string]Kind{
		"CreateTodo": One, "GetTodo": One, "ListTodos": Many, "SetTodoDone": ExecRows,
		"DeleteTodo": ExecRows, "CountTodos": One, "GetTodoTitle": One,
	}
	for name, kind := range kinds {
		_, q, err := repo.Lookup(name)
		if err != nil || q.Kind != kind {
			t.Fatalf("%s: %+v, %v; want kind %s", name, q, err, kind)
		}
	}
	_, create, _ := repo.Lookup("db.CreateTodo")
	if create.Params != "db.CreateTodoParams" || create.Result.Expr != "db.Todo" || len(create.Args) != 3 {
		t.Fatalf("CreateTodo = %+v", create)
	}
	if meta := create.Args[2]; meta.Name != "Meta" || meta.JSON != "meta" || !meta.Type.Bytes {
		t.Fatalf("Meta = %+v", meta)
	}
	_, list, _ := repo.Lookup("ListTodos")
	done := list.Args[1]
	if done.Type.Expr != "pgtype.Bool" || len(done.Type.Imports) != 1 || done.Type.Imports[0] != "github.com/jackc/pgx/v5/pgtype" {
		t.Fatalf("Done = %+v", done)
	}
	_, title, _ := repo.Lookup("GetTodoTitle")
	if title.Params != "" || len(title.Args) != 1 || title.Args[0].Name != "id" || title.Args[0].Type.Expr != "int64" || title.Result.Expr != "string" {
		t.Fatalf("GetTodoTitle = %+v", title)
	}
	_, count, _ := repo.Lookup("CountTodos")
	if len(count.Args) != 0 || count.Result.Expr != "int64" {
		t.Fatalf("CountTodos = %+v", count)
	}
	if _, _, err := repo.Lookup("Nope"); err == nil || !strings.Contains(err.Error(), "no sqlc query Nope in package db") {
		t.Fatalf("Lookup(Nope) = %v", err)
	}
	if _, _, err := repo.Lookup("other.GetTodo"); err == nil {
		t.Fatal("Lookup found a query in the wrong package")
	}
}

// writeModule writes a module with two sqlc packages that both define Get,
// with its sqlc config below the module root.
func writeModule(t *testing.T) string {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/m\n",
		"internal/store/sqlc.json": `{"version":"2","sql":[
			{"gen":{"go":{"package":"a","out":"a","sql_package":"pgx/v4"}}},
			{"gen":{"go":{"package":"b","out":"b"}}},
			{"gen":{"kotlin":{}}}]}`,
		"internal/store/a/q.sql.go": "package a\n\nimport \"context\"\n\nconst get = `-- name: Get :one\nSELECT 1`\n\ntype Queries struct{}\n\ntype Thing struct{ N int }\n\nfunc (q *Queries) Get(ctx context.Context, ids []int64) ([]Thing, error) { return nil, nil }\n",
		"internal/store/b/q.sql.go": "package b\n\nimport (\n\t\"context\"\n\tpg \"github.com/jackc/pgx/v5/pgtype\"\n)\n\nconst get = `-- name: Get :exec\nSELECT 1`\nconst other = `-- name: Other :many\nSELECT 1`\n\ntype Queries struct{}\n\ntype GetParams struct {\n\tA *pg.Text `json:\"a_a\"`\n\tB map[string]Kind\n}\n\ntype Kind string\n\nfunc (q *Queries) Get(ctx context.Context, arg GetParams) error { return nil }\nfunc (q *Queries) Other(ctx context.Context) ([]Kind, error) { return nil, nil }\nfunc (q *Queries) Helper(ctx context.Context) error { return nil }\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFindModule(t *testing.T) {
	root := writeModule(t)
	// The config is below the module root, so Find searches the module.
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := Find(filepath.Join(root, "api"))
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.Packages) != 2 {
		t.Fatalf("got %d packages", len(repo.Packages))
	}
	if _, _, err := repo.Lookup("Get"); err == nil || !strings.Contains(err.Error(), "qualify it as a.Get") {
		t.Fatalf("ambiguous Get = %v", err)
	}
	pa, qa, err := repo.Lookup("a.Get")
	if err != nil || pa.SQLPackage != "pgx/v4" || qa.Args[0].Type.Expr != "[]int64" || qa.Result.Expr != "[]a.Thing" || qa.Result.Imports[0] != "example.com/m/internal/store/a" {
		t.Fatalf("a.Get = %+v %+v %v", pa, qa, err)
	}
	pb, qb, err := repo.Lookup("b.Get")
	if err != nil || pb.SQLPackage != "database/sql" || qb.Kind != Exec {
		t.Fatalf("b.Get = %+v %v", qb, err)
	}
	if a := qb.Args[0]; a.JSON != "a_a" || a.Type.Expr != "*pg.Text" || a.Type.Imports[0] != "github.com/jackc/pgx/v5/pgtype" {
		t.Fatalf("A = %+v", a)
	}
	if b := qb.Args[1]; b.JSON != "B" || b.Type.Expr != "map[string]b.Kind" {
		t.Fatalf("B = %+v", b)
	}
	if _, q, _ := repo.Lookup("Other"); q.Result.Expr != "b.Kind" {
		t.Fatalf("Other = %+v", q)
	}
	// Helper has no sqlc query comment, so it is not a query.
	if _, _, err := repo.Lookup("Helper"); err == nil {
		t.Fatal("Helper is not a sqlc query")
	}
}

func TestFindErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "no sqlc.yaml or sqlc.yml or sqlc.json") {
		t.Fatalf("got %v", err)
	}
	os.WriteFile(filepath.Join(dir, "sqlc.yaml"), []byte("version: \"2\"\nsql:\n  - queries: [q.sql, more]\n    gen:\n      go:\n        package: db\n        out: db\n"), 0o644)
	// Before sqlc first runs, the config is known but its package is not.
	repo, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := repo.Packages[0]
	if p.Config != filepath.Join(dir, "sqlc.yaml") || p.ImportPath != "x/db" || !p.ReadsQueryFile(filepath.Join(dir, "q.sql")) || p.ReadsQueryFile(filepath.Join(dir, "other.sql")) {
		t.Fatalf("package = %+v", p)
	}
	os.MkdirAll(filepath.Join(dir, "more"), 0o755)
	if !p.ReadsQueryFile(filepath.Join(dir, "more", "x_af.sql")) {
		t.Fatal("a file in a query directory is read")
	}
	if _, _, err := repo.Lookup("GetX"); err == nil || !strings.Contains(err.Error(), "run sqlc generate") {
		t.Fatalf("Lookup = %v", err)
	}
}

func TestQueryWrites(t *testing.T) {
	for _, tt := range []struct {
		kind Kind
		sql  string
		want bool
	}{
		{One, "-- name: GetUser :one\nSELECT * FROM users WHERE id = $1", false},
		{Many, "-- name: ListUsers :many\n/* all */ select id, updated_at, deleted FROM users", false},
		{One, "-- name: BanUser :one\nUPDATE users SET banned = true RETURNING id", true},
		{One, "-- name: CreateUser :one\n-- a comment\n  insert INTO users DEFAULT VALUES RETURNING id", true},
		{Many, "-- name: Purge :many\nDELETE FROM users RETURNING id", true},
		{One, "-- name: Move :one\nWITH moved AS (DELETE FROM a RETURNING *) INSERT INTO b SELECT * FROM moved RETURNING id", true},
		{One, "-- name: Recent :one\nWITH r AS (SELECT * FROM users ORDER BY id DESC LIMIT 1) SELECT id FROM r", false},
		// Locking rows is not writing them.
		{One, "-- name: Lock :one\nWITH r AS (SELECT id FROM users WHERE id = $1 FOR UPDATE) SELECT id FROM r", false},
		{One, "-- name: LockNoKey :one\nWITH r AS (SELECT id FROM users FOR NO KEY UPDATE) SELECT id FROM r", false},
		{Exec, "-- name: Touch :exec\nSELECT touch($1)", true},
		{ExecRows, "-- name: Count :execrows\nSELECT 1", true},
	} {
		if got := (&Query{Kind: tt.kind, SQL: tt.sql}).Writes(); got != tt.want {
			t.Errorf("%q: got %v", tt.sql, got)
		}
	}
}
