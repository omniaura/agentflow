package gogen

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

func genRoutesFile(t *testing.T, src string) (string, error) {
	t.Helper()
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("api.af", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup})
	return buf.String(), err
}

func TestGenRoutes(t *testing.T) {
	code, err := genRoutesFile(t, ".title Hello\nHi <!name>.\n\n.route GET /a/{ID} GetTodo\n.auth ownerId\n\n.route DELETE /a/{id} GetTodo\n.auth owner_id\n.status 200\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type ApiQueries interface {\n\tGetTodo(ctx context.Context, arg db.GetTodoParams) (db.Todo, error)\n}",
		// One query served twice gets a handler per method.
		`mux.HandleFunc("GET /a/{ID}", rt.GetTodoGet)`,
		`mux.HandleFunc("DELETE /a/{id}", rt.GetTodoDelete)`,
		// Names match whatever their case: {ID} is the ID field.
		`afhttp.Parse("ID", r.PathValue("ID"), &arg.ID)`,
		"arg.OwnerID = caller.OwnerID",
		"afhttp.WriteJSON(w, r, http.StatusOK, row)",
		"type Hello struct",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
}

func TestGenRoutesErrors(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"unknown query", ".route GET /x Nope\n", "no sqlc query Nope"},
		{"unmatched path param", ".route GET /x/{todo} GetTodo\n", "path parameter todo matches no argument of query GetTodo (id, ownerId)"},
		{"unmatched auth", ".route GET /x/{id} GetTodo\n.auth user_id\n", "auth parameter user_id matches no argument"},
		{"no args", ".route GET /x/{id} CountTodos\n", "query CountTodos takes no arguments"},
		{"bound twice", ".route GET /x/{id} GetTodo\n.auth ID\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genRoutesFile(t, tt.src)
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(re.Error(), tt.msg) {
				t.Fatalf("got %v, want RouteError containing %q", err, tt.msg)
			}
			if re.Offset != strings.LastIndex(tt.src[:strings.IndexByte(tt.src, '\n')], " ")+1 {
				t.Fatalf("offset %d does not point at the query name", re.Offset)
			}
		})
	}
}

func TestGenRoutesNeedsSqlc(t *testing.T) {
	f, err := ast.NewFile("api.af", []byte(".route GET /x Q\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = GenFile(&bytes.Buffer{}, f, "api")
	if err == nil || !strings.Contains(err.Error(), "sqlc config") {
		t.Fatalf("got %v", err)
	}
}

func TestGenRoutesEmpty(t *testing.T) {
	code, err := genRoutesFile(t, ".route GET /a/{id} GetTodo\n.auth owner_id\n.empty 403 that todo is not yours\n\n.route PUT /a/{id}/done SetTodoDone\n.auth owner_id\n.empty 409 that todo is gone or not yours\n\n.route DELETE /a/{id} DeleteTodo\n.auth owner_id\n.empty 418 teapot\n\n.route GET /b/{id} GetTodoTitle\n.public\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"errApiGetTodo     = errs.NewFactory(http.StatusForbidden, \"that todo is not yours\")",
		"errApiSetTodoDone = errs.NewFactory(http.StatusConflict, \"that todo is gone or not yours\")",
		// A status without a net/http name is written as a number.
		"errApiDeleteTodo  = errs.NewFactory(418, \"teapot\")",
		"rt.fail(w, r, errApiGetTodo.New(r.Context()).AddError(err))",
		"if n == 0 {\n\t\trt.fail(w, r, errApiSetTodoDone.New(r.Context()))",
		// Routes without .empty keep 404.
		"rt.fail(w, r, afhttp.ErrNotFound.New(r.Context()).AddError(err))",
		`"github.com/omniaura/go-kit/errs"`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	// No .empty, no errs import.
	code, err = genRoutesFile(t, ".route GET /b/{id} GetTodoTitle\n.public\n")
	if err != nil || strings.Contains(code, "go-kit/errs") {
		t.Errorf("errs imported without .empty: %v", err)
	}
	// :many has no empty result to name, nor has :exec.
	for _, src := range []string{".route GET /a ListTodos\n.auth owner_id\n.empty none\n", ".route POST /a/{id} SetTodoDone\n.auth owner_id\n\n.route GET /n ListNotes\n.auth owner_id\n.empty none\n"} {
		_, err := genRoutesFile(t, src)
		var re *RouteError
		if !errors.As(err, &re) || !strings.Contains(err.Error(), ".empty applies to a :one query") || src[re.Offset:re.Offset+6] != ".empty" {
			t.Errorf("%q: got %v", src, err)
		}
	}
}

func TestGenRoutesWrap(t *testing.T) {
	code, err := genRoutesFile(t, ".route GET /a ListTodos\n.auth owner_id\n.wrap todos\n\n.route GET /a/{id} GetTodo\n.auth owner_id\n.wrap todo\n\n.route GET /c CountTodos\n.public\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`afhttp.WriteJSON(w, r, http.StatusOK, map[string]any{"todos": rows})`,
		`afhttp.WriteJSON(w, r, http.StatusOK, map[string]any{"todo": row})`,
		// No .wrap: the bare result.
		`afhttp.WriteJSON(w, r, http.StatusOK, row)`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q", want)
		}
	}
	// An empty list is still [], not null, inside the object.
	if !strings.Contains(code, "if rows == nil {\n\t\trows = []db.Todo{}") {
		t.Error("the empty list is not []")
	}
	src := ".route PUT /a/{id}/done SetTodoDone\n.auth owner_id\n.wrap result\n"
	_, err = genRoutesFile(t, src)
	var re *RouteError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), ".wrap puts a :one or :many result in an object") || src[re.Offset:re.Offset+5] != ".wrap" {
		t.Errorf("wrap on :execrows: %v", err)
	}
}

func TestGenRoutesMap(t *testing.T) {
	code, err := genRoutesFile(t, ".route GET /a ListTodos\n.auth owner_id\n.map todoCard\n.wrap cards\n\n.route GET /a/{id} GetTodo\n.auth owner_id\n.map todoCard\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// Each row of a :many result, inside the .wrap object.
		`afhttp.WriteJSON(w, r, http.StatusOK, map[string]any{"cards": afhttp.Map(rows, todoCard)})`,
		`afhttp.WriteJSON(w, r, http.StatusOK, todoCard(row))`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	src := ".route PUT /a/{id}/done SetTodoDone\n.auth owner_id\n.map todoCard\n"
	_, err = genRoutesFile(t, src)
	var re *RouteError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), ".map turns a :one or :many result into the response") || src[re.Offset:re.Offset+4] != ".map" {
		t.Errorf("map on :execrows: %v", err)
	}
}

func TestGenRoutesCache(t *testing.T) {
	code, err := genRoutesFile(t, ".route GET /a ListTodos\n.auth owner_id\n\n.route GET /a/{id} GetTodo\n.auth owner_id\n.cache 5m\n\n.route GET /c CountTodos\n.public\n.cache 1m\n\n.route GET /t/{id} GetTodoTitle\n.public\n\n.route PUT /a/{id}/done SetTodoDone\n.auth owner_id\n")
	if err != nil {
		t.Fatal(err)
	}
	handler := func(name string) string {
		_, rest, _ := strings.Cut(code, ") "+name+"(w http.ResponseWriter, r *http.Request) {\n")
		body, _, _ := strings.Cut(rest, "\n}\n")
		return body
	}
	set := func(v string) string { return `w.Header().Set("Cache-Control", "` + v + `")` }
	// no-store goes first, so errors aren't kept either.
	if body := handler("ListTodos"); !strings.HasPrefix(body, "\t"+set("no-store")+"\n") || strings.Count(body, "Cache-Control") != 1 {
		t.Errorf("ListTodos:\n%s", body)
	}
	for _, c := range []struct{ handler, want string }{
		// A cached caller's result varies by the credentials that chose it.
		{"GetTodo", set("private, max-age=300") + "\n\t" + `w.Header().Add("Vary", "Authorization, Cookie")`},
		{"CountTodos", set("public, max-age=60")},
	} {
		// Right before the success response, so errors aren't cached.
		if body := handler(c.handler); !strings.Contains(body, c.want+"\n\tafhttp.WriteJSON") || strings.Count(body, "Cache-Control") != 1 {
			t.Errorf("%s: missing %s before WriteJSON in\n%s", c.handler, c.want, body)
		}
	}
	if body := handler("CountTodos"); strings.Contains(body, "Vary") {
		t.Errorf("public result varies:\n%s", body)
	}
	// Public with no .cache, and a 204 with no body to cache.
	for _, h := range []string{"GetTodoTitle", "SetTodoDone"} {
		if body := handler(h); body == "" || strings.Contains(body, "Cache-Control") {
			t.Errorf("%s sets Cache-Control:\n%s", h, body)
		}
	}
	// However it's spelled, no-store covers errors too.
	code, err = genRoutesFile(t, ".route GET /t/{id} GetTodoTitle\n.public\n.cache private, No-Store\n")
	if err != nil {
		t.Fatal(err)
	}
	if body := handler("GetTodoTitle"); !strings.HasPrefix(body, "\t"+set("private, No-Store")+"\n") || strings.Count(body, "Cache-Control") != 1 {
		t.Errorf("No-Store:\n%s", body)
	}
	src := ".route PUT /a/{id}/done SetTodoDone\n.auth owner_id\n.cache no-cache\n"
	_, err = genRoutesFile(t, src)
	var re *RouteError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), ".cache applies to a :one or :many result") || src[re.Offset:re.Offset+6] != ".cache" {
		t.Errorf("cache on :execrows: %v", err)
	}
}

func TestHasNoStore(t *testing.T) {
	for cc, want := range map[string]bool{
		"no-store":                         true,
		"private, No-Store":                true,
		"private,no-store ":                true,
		"no-cache":                         false,
		"private, max-age=300, x-no-store": false,
		`private, x="no-store"`:            false,
		"":                                 false,
	} {
		if got := hasNoStore(cc); got != want {
			t.Errorf("hasNoStore(%q) = %v, want %v", cc, got, want)
		}
	}
}

func TestGenRoutesSwag(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("todos.af", []byte(".route GET /todos/{id} GetTodo\n.auth owner_id\nGets a todo.\nThe caller's only.\n\n.route POST /todos CreateTodo\n.auth owner_id\n.status 201\n\n.route GET /todos ListTodos\n.auth owner_id\n.wrap todos\n\n.route GET /stats/count CountTodos\n.public\n\n.route PUT /todos/{id}/done SetTodoDone\n.auth owner_id\n.empty 409 already done\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, Swag: &Swag{Security: "BearerAuth", Tags: "todos"}}); err != nil {
		t.Fatal(err)
	}
	code := buf.String()
	for _, want := range []string{
		"// @Summary Gets a todo.\n// @Description Gets a todo. The caller's only.\n// @Tags todos\n// @Security BearerAuth\n// @Produce json\n// @Param id path integer true \"id\"\n// @Success 200 {object} db.Todo\n// @Failure 400 {object} errs.Error\n// @Failure 401 {object} errs.Error\n// @Failure 404 {object} errs.Error\n// @Failure 500 {object} errs.Error\n// @Router /todos/{id} [get]\nfunc (rt *TodosRoutes) GetTodo(",
		// A body is a named type swag can read; an insert can conflict.
		"// @Accept json\n// @Produce json\n// @Param body body todosCreateTodoBody true \"request body\"\n// @Success 201 {object} db.Todo\n",
		// An INSERT always returns its row: no 404.
		"// @Failure 400 {object} errs.Error\n// @Failure 401 {object} errs.Error\n// @Failure 409 {object} errs.Error\n// @Failure 500 {object} errs.Error\n// @Router /todos [post]\nfunc (rt *TodosRoutes) CreateTodo(",
		"type todosCreateTodoBody struct {",
		// Query parameters, nullable ones optional; a wrapped list.
		"// @Param done query boolean false \"done\"\n// @Success 200 {object} map[string][]db.Todo\n",
		// Public: no security, no 401.
		"// @Produce json\n// @Success 200 {object} int64\n// @Failure 404 {object} errs.Error\n// @Failure 500 {object} errs.Error\n// @Router /stats/count [get]\n",
		// No body: 204, and the .empty status.
		"// @Success 204\n// @Failure 400 {object} errs.Error\n// @Failure 401 {object} errs.Error\n// @Failure 409 {object} errs.Error\n// @Failure 500 {object} errs.Error\n// @Router /todos/{id}/done [put]\n",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing\n%s\nin\n%s", want, code)
		}
	}
	if strings.Contains(code, "@Tags todos\n// @Security BearerAuth\n// @Produce json\n// @Success 200 {object} int64") {
		t.Error("public route secured")
	}
	// Without Swag, no annotations.
	buf.Reset()
	if err := GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup}); err != nil || strings.Contains(buf.String(), "@Router") {
		t.Errorf("annotated without Swag: %v", err)
	}
}

func TestGenRoutesSwagMap(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("cards.af", []byte(".route GET /cards ListTodos\n.auth owner_id\n.map todoCard\n\n.route GET /cards/{id} GetTodo\n.auth owner_id\n.map other\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, MapFuncs: map[string]MapFunc{"todoCard": {Params: []string{"row"}, Result: "TodoCard"}}, Swag: &Swag{}}); err != nil {
		t.Fatal(err)
	}
	// The .map func's result type, when known; otherwise a plain object.
	for _, want := range []string{"// @Success 200 {array} TodoCard\n", "// @Success 200 {object} object\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSwagFailureRules(t *testing.T) {
	for sql, want := range map[string][2]bool{ // mayFindNone, mayConflict
		"-- name: CreateTodo :one\nINSERT INTO todos (title) VALUES ($1) RETURNING *": {false, true},
		"INSERT INTO t (a) VALUES ($1) ON CONFLICT DO NOTHING RETURNING *":            {true, true},
		"INSERT INTO t (a) SELECT a FROM s RETURNING *":                               {true, true},
		"UPDATE t SET a = $1 WHERE id = $2":                                           {true, true},
		"DELETE FROM t WHERE id = $1":                                                 {true, false},
		"SELECT * FROM t WHERE id = $1":                                               {true, false},
		"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x":                          {true, true},
		// Keywords in quoted identifiers, strings and comments don't count.
		`INSERT INTO t ("select", note) VALUES ($1, 'where on conflict') RETURNING *`: {false, true},
		"INSERT INTO t (a) VALUES ($1) /* WHERE */ RETURNING * -- SELECT":             {false, true},
		"WITH x AS (SELECT 'update' AS \"insert\") SELECT * FROM x":                   {true, false},
		`SELECT 'it''s' AS "a""b" FROM t`:                                             {true, false},
	} {
		if got := [2]bool{mayFindNone(sql), mayConflict(sql)}; got != want {
			t.Errorf("%q: got %v, want %v", sql, got, want)
		}
	}
}

func TestGenRoutesBind(t *testing.T) {
	code, err := genRoutesFile(t, ".route GET /notes/{note_id} GetTodo\n.bind id = note_id\n.auth owner_id\n\n.route POST /notes CreateNote\n.bind body = text\n.auth owner_id\n\n.route GET /find FindNotesTool\n.bind text = q\n.auth owner_id\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// A path parameter of another name.
		`mux.HandleFunc("GET /notes/{note_id}", rt.GetTodo)`,
		`afhttp.Parse("note_id", r.PathValue("note_id"), &arg.ID)`,
		// A body field.
		"Body string `json:\"text\"`",
		// A query string parameter.
		`afhttp.ParseQuery(r, "q", &arg.Text)`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	for _, tt := range []struct{ name, src, msg, at string }{
		{"no such argument", ".route GET /x/{id} GetTodo\n.auth owner_id\n.bind title = name\n", "query GetTodo has no argument title (id, ownerId)", ".bind"},
		{"another argument's name", ".route POST /x CreateNote\n.bind body = owner_id\n", "owner_id is the name of another argument", ".bind"},
		// The bound argument is read only by its new name.
		{"old name in the path", ".route GET /x/{id} GetTodo\n.auth owner_id\n.bind id = note_id\n", "argument id of query GetTodo is read from note_id", "GetTodo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genRoutesFile(t, tt.src)
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("got %v, want %q", err, tt.msg)
			}
			if !strings.HasPrefix(tt.src[re.Offset:], tt.at) {
				t.Errorf("offset points at %q, want %q", tt.src[re.Offset:], tt.at)
			}
		})
	}
}

// Swag names a bound argument as the request does.
func TestGenRoutesSwagBind(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("todos.af", []byte(".route GET /notes/{note_id} GetTodo\n.bind id = note_id\n.auth owner_id\n\n.route GET /find FindNotesTool\n.bind text = q\n.auth owner_id\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, Swag: &Swag{Tags: "todos"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// @Param note_id path integer true \"note_id\"", "// @Param q query string true \"q\""} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in\n%s", want, buf.String())
		}
	}
}

// A .map func's signature says what the handler passes it and whether it
// can fail.
func TestGenRoutesMapShapes(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	gen := func(src string, funcs map[string]MapFunc) (string, error) {
		f, err := ast.NewFile("cards.af", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		err = GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, MapFuncs: funcs})
		return buf.String(), err
	}
	code, err := gen(".route GET /a ListTodos\n.auth owner_id\n.map board\n.wrap board\n\n.route GET /b ListNotes\n.auth owner_id\n.map noteView\n\n.route GET /c/{id} GetTodo\n.auth owner_id\n.map todoView\n\n.route GET /d/{id} GetTodoTitle\n.public\n.map titleView\n\n.route POST /e/{id} GetTodoTitle\n.map titleFor\n", map[string]MapFunc{
		"titleFor":  {Params: []string{"row", "caller"}},
		"board":     {Params: []string{"caller", "rows"}, Err: true},
		"noteView":  {Params: []string{"ctx", "row", "caller"}, Err: true},
		"todoView":  {Params: []string{"row", "caller"}},
		"titleView": {Params: []string{"request", "row"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// The whole result, with the caller; its error fails the request.
		"resp, err := board(caller, rows)\n\tif err != nil {\n\t\trt.fail(w, r, err)\n\t\treturn\n\t}\n\tafhttp.WriteJSON(w, r, http.StatusOK, map[string]any{\"board\": resp})",
		// Each row, with the context and caller.
		"resp, err := afhttp.MapErr(rows, func(row db.Note) (any, error) {\n\t\treturn noteView(r.Context(), row, caller)\n\t})",
		"afhttp.WriteJSON(w, r, http.StatusOK, todoView(row, caller))",
		"afhttp.WriteJSON(w, r, http.StatusOK, titleView(r, row))",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	// A route whose only use of the caller is its .map still reads it.
	handler := code[strings.Index(code, "func (rt *CardsRoutes) GetTodoTitlePost("):]
	if !strings.Contains(handler, "caller, err := rt.Auth(r)") || !strings.Contains(handler, "titleFor(row, caller)") {
		t.Errorf("the caller isn't read for the .map:\n%s", handler)
	}
	// A slice that is the query's row type is one row, not all of them.
	code, err = gen(".route GET /c/{id} GetTodo\n.auth owner_id\n.map raw\n", map[string]MapFunc{"raw": {Params: []string{"rows"}, Row: "db.Todo"}})
	if err != nil || !strings.Contains(code, "afhttp.WriteJSON(w, r, http.StatusOK, raw(row))") {
		t.Errorf("a slice row: %v\n%s", err, code)
	}
	for _, tt := range []struct{ name, src, msg string }{
		{"rows of a :one", ".route GET /c/{id} GetTodo\n.auth owner_id\n.map board\n", ".map board takes the whole result, a slice; query GetTodo is :one"},
		{"caller on a public route", ".route GET /d/{id} GetTodoTitle\n.public\n.map todoView\n", ".map todoView takes the Caller, which a .public route has none of"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gen(tt.src, map[string]MapFunc{"board": {Params: []string{"rows"}}, "todoView": {Params: []string{"row", "caller"}}})
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(err.Error(), tt.msg) || !strings.HasPrefix(tt.src[re.Offset:], ".map") {
				t.Fatalf("got %v, want %q at .map", err, tt.msg)
			}
		})
	}
}

// A .map func of all the rows makes the whole response: swag documents
// its type as it is, not as an array of it.
func TestGenRoutesSwagMapShapes(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.NewFile("cards.af", []byte(".route GET /board ListTodos\n.auth owner_id\n.map board\n\n.route GET /cards ListNotes\n.auth owner_id\n.map cards\n"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = GenFileOptions(&buf, f, "api", Options{
		Queries:  repo.Lookup,
		MapFuncs: map[string]MapFunc{"board": {Params: []string{"rows"}, Err: true, Result: "Board"}, "cards": {Params: []string{"rows"}, Result: "[]Card"}},
		Swag:     &Swag{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// @Success 200 {object} Board\n", "// @Success 200 {array} Card\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestMapFuncFor(t *testing.T) {
	for _, tt := range []struct {
		row, typ string
		want     []string
	}{
		{"db.Todo", "[]db.Todo", []string{"caller", "rows"}},
		// The query's row is a slice: the parameter is one row.
		{"[]byte", "[]byte", []string{"caller", "row"}},
		// Through another import name of the same package.
		{"[]db.Blob", "[]store.Blob", []string{"caller", "row"}},
		{"db.Blob", "[]store.Blob", []string{"caller", "rows"}},
	} {
		f := MapFunc{Params: []string{"caller", "rows"}, Row: tt.typ}
		if got := f.For(tt.row).Params; !slices.Equal(got, tt.want) {
			t.Errorf("%s for row %s: %v, want %v", tt.typ, tt.row, got, tt.want)
		}
		if f.Params[1] != "rows" {
			t.Fatal("For changed the func it was called on")
		}
	}
}

// A .prepare func gets the bound, decoded arguments before the query, as
// its signature says.
func TestGenRoutesPrepare(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	gen := func(src string, funcs map[string]PrepareFunc) (string, error) {
		f, err := ast.NewFile("notes.af", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		err = GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, PrepareFuncs: funcs})
		return buf.String(), err
	}
	code, err := gen(".route POST /a CreateNote\n.auth owner_id\n.prepare tidyNote\n\n.route GET /b/{id} GetTodoTitle\n.prepare checkID\n\n.route PUT /c/{id} UpdateNote\n.auth owner_id\n.prepare trim\n", map[string]PrepareFunc{
		"checkID": {Params: []string{"ctx", "args", "caller"}, Err: true},
		"trim":    {Params: []string{"args"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// After the body is decoded, before the query; a func af doesn't
		// know takes the arguments and can fail.
		"arg.Body = body.Body\n\tif err := tidyNote(&arg); err != nil {\n\t\trt.fail(w, r, err)\n\t\treturn\n\t}\n\trow, err := rt.Queries.CreateNote(r.Context(), arg)",
		// A route that reads the caller only for its .prepare reads it.
		"caller, err := rt.Auth(r)",
		"if err := checkID(r.Context(), &arg, caller); err != nil {",
		// One that can't fail.
		"trim(&arg)\n\tn, err := rt.Queries.UpdateNote(r.Context(), arg)",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	start := strings.Index(code, "func (rt *NotesRoutes) GetTodoTitle(")
	end := strings.Index(code[start:], "func (rt *NotesRoutes) UpdateNote(")
	if handler := code[start : start+end]; !strings.Contains(handler, "caller, err := rt.Auth(r)") {
		t.Errorf("the caller isn't read for .prepare:\n%s", handler)
	}
	for _, tt := range []struct{ name, src, msg string }{
		{"no arguments", ".route GET /n CountTodos\n.public\n.prepare tidyNote\n", ".prepare tidyNote: query CountTodos takes no arguments"},
		{"caller on a public route", ".route GET /b/{id} GetTodoTitle\n.public\n.prepare checkID\n", ".prepare checkID takes the Caller, which a .public route has none of"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gen(tt.src, map[string]PrepareFunc{"checkID": {Params: []string{"args", "caller"}}})
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(err.Error(), tt.msg) || !strings.HasPrefix(tt.src[re.Offset:], ".prepare") {
				t.Fatalf("got %v, want %q at .prepare", err, tt.msg)
			}
		})
	}
}

// A successful .empty answers a query that finds no row, or changes none,
// with its response instead of an error.
func TestGenRoutesEmptyOK(t *testing.T) {
	repo, err := sqlcgo.Find("../../../examples/routes")
	if err != nil {
		t.Fatal(err)
	}
	gen := func(src string, funcs map[string]MapFunc) (string, error) {
		f, err := ast.NewFile("todos.af", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		err = GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, EmptyFuncs: funcs, Swag: &Swag{}})
		return buf.String(), err
	}
	code, err := gen(".route GET /a/{id} GetTodo\n.auth owner_id\n.empty 200 draft\n.cache 1m\n\n.route GET /b/{id} GetTodoTitle\n.public\n.empty 200 \"untitled\"\n\n.route PUT /c/{id}/done SetTodoDone\n.auth owner_id\n.empty 204\n\n.route DELETE /d/{id} DeleteTodo\n.auth owner_id\n.empty 200 {\"deleted\": false}\n", map[string]MapFunc{
		"draft": {Params: []string{"ctx", "args", "caller"}, Err: true, Result: "Draft"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// A func, with the arguments, context and caller; it can fail. A
		// response with a body gets the route's caching.
		"if errors.Is(err, pgx.ErrNoRows) {\n\t\tresp, err := draft(r.Context(), arg, caller)\n\t\tif err != nil {\n\t\t\trt.fail(w, r, err)\n\t\t\treturn\n\t\t}\n\t\tw.Header().Set(\"Cache-Control\", \"private, max-age=60\")",
		"afhttp.WriteJSON(w, r, http.StatusOK, resp)\n\t\treturn\n\t}",
		"afhttp.WriteJSON(w, r, http.StatusOK, json.RawMessage(\"\\\"untitled\\\"\"))",
		// An :execrows query that changes none.
		"if n == 0 {\n\t\tw.WriteHeader(http.StatusNoContent)\n\t\treturn\n\t}",
		// A body after an :execrows query is kept from caches too, and
		// documented as JSON.
		"if n == 0 {\n\t\tw.Header().Set(\"Cache-Control\", \"no-store\")\n\t\tafhttp.WriteJSON(w, r, http.StatusOK, json.RawMessage(\"{\\\"deleted\\\": false}\"))",
		"// @Produce json\n// @Param id path integer true \"id\"\n// @Success 204\n// @Success 200 {object} object\n",
		// Swag: a success of another status, and no 404. (One of the same
		// status keeps the route's type: swag has one per status.)
		"// @Success 204\n// @Success 200 {object} object\n",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in\n%s", want, code)
		}
	}
	if strings.Contains(code, "errGetTodo") || strings.Contains(code, "@Failure 404") {
		t.Errorf("a successful .empty declared an error:\n%s", code)
	}
	for _, tt := range []struct{ name, src, msg string }{
		{"caller on a public route", ".route GET /b/{id} GetTodoTitle\n.public\n.empty 200 draft\n", ".empty 200 draft takes the Caller, which a .public route has none of"},
		{"arguments of a query with none", ".route GET /n CountTodos\n.public\n.empty 200 fromArgs\n", ".empty 200 fromArgs takes the query's arguments; query CountTodos takes none"},
		{"a :many", ".route GET /l ListTodos\n.auth owner_id\n.empty 204\n", ".empty applies to a :one query"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gen(tt.src, map[string]MapFunc{"draft": {Params: []string{"caller"}}, "fromArgs": {Params: []string{"args"}}})
			var re *RouteError
			if !errors.As(err, &re) || !strings.Contains(err.Error(), tt.msg) || !strings.HasPrefix(tt.src[re.Offset:], ".empty") {
				t.Fatalf("got %v, want %q at .empty", err, tt.msg)
			}
		})
	}
}

// Swag names a func's result type only when the generated file can.
func TestSwagObject(t *testing.T) {
	for in, want := range map[string]string{"Draft": "Draft", "views.Draft": "object", "": "object", "[]Card": "[]Card"} {
		if got := swagObject(in); got != want {
			t.Errorf("swagObject(%q) = %q, want %q", in, got, want)
		}
	}
}
