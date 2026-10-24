package gogen

import (
	"bytes"
	"errors"
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
	if err := GenFileOptions(&buf, f, "api", Options{Queries: repo.Lookup, Swag: &Swag{MapTypes: map[string]string{"todoCard": "TodoCard"}}}); err != nil {
		t.Fatal(err)
	}
	// The .map func's result type, when known; otherwise a plain object.
	for _, want := range []string{"// @Success 200 {array} TodoCard\n", "// @Success 200 {object} object\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}
