package gogen

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/omniaura/agentflow/pkg/ast"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

// AfhttpImport is the runtime package generated route code imports.
const AfhttpImport = "github.com/omniaura/agentflow/runtime/afhttp"

// ErrsImport is go-kit's errs, which generated code declares errors with.
const ErrsImport = "github.com/omniaura/go-kit/errs"

// Options configure generation.
type Options struct {
	// Queries resolves the sqlc query a .route names. Files with routes
	// need it.
	Queries func(name string) (*sqlcgo.Package, *sqlcgo.Query, error)
	// Swag, when set, writes swag annotations on route handlers.
	Swag *Swag
	// MapFuncs are the signatures of the package's .map funcs, by name. A
	// func missing from it takes the row and returns the response.
	MapFuncs map[string]MapFunc
	// PrepareFuncs are the signatures of the package's .prepare funcs, by
	// name. A func missing from it takes a pointer to the arguments and
	// returns an error.
	PrepareFuncs map[string]PrepareFunc
	// EmptyFuncs are the signatures of the package's funcs that make a
	// successful .empty response, by name. A func missing from it takes
	// nothing and returns the response.
	EmptyFuncs map[string]MapFunc
	// SiblingTools are the tools of the other .af files of the package,
	// which an agent's .tools may name.
	SiblingTools []ast.SiblingTool
	// Imports replaces import paths in generated code, such as a runtime
	// package with the copy af eject wrote into the repository.
	Imports map[string]string
}

// MapFunc is the signature of a .map func, as af reads it in the package.
type MapFunc struct {
	// Params are what the func takes, in order: the "row" (each row of a
	// :many result), the "rows" (the whole :many result, a slice), the
	// authenticated "caller", the request's "ctx" or the "request".
	Params []string
	// Err reports a second result, an error, which fails the request.
	Err bool
	// Row is the type of the row or rows parameter, as the package writes
	// it. A slice is all the rows, unless it is the query's row type.
	Row string
	// Result is the response's type, the first result, as the package
	// writes it.
	Result string
}

// For returns f as a route whose query's row type is row calls it: a slice
// parameter is all the rows, unless its type is row itself, such as []byte.
// Types compare without their package names, which imports may alias.
func (f MapFunc) For(row string) MapFunc {
	i := slices.Index(f.Params, "rows")
	if i < 0 || unqualified(f.Row) != unqualified(row) {
		return f
	}
	f.Params = slices.Clone(f.Params)
	f.Params[i] = "row"
	return f
}

// qualifierRE matches the package name of a qualified type, db. in db.Todo.
var qualifierRE = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)

// unqualified is the type expression t without its package names.
func unqualified(t string) string {
	return qualifierRE.ReplaceAllString(strings.ReplaceAll(t, " ", ""), "")
}

// PrepareFunc is the signature of a .prepare func, as af reads it in the
// package.
type PrepareFunc struct {
	// Params are what the func takes, in order: a pointer to the query's
	// "args", the authenticated "caller", the request's "ctx" or the
	// "request".
	Params []string
	// Err reports a result, an error, which fails the request before the
	// query runs.
	Err bool
}

// simple reports whether f takes only the row and can't fail.
func (f MapFunc) simple() bool {
	return !f.Err && len(f.Params) == 1 && f.Params[0] == "row"
}

// DeclError reports a declaration section that cannot be generated, such
// as a .route whose query does not exist, at a byte offset in the source.
type DeclError struct {
	Offset int
	Err    error
}

func (e *DeclError) Error() string { return e.Err.Error() }
func (e *DeclError) Unwrap() error { return e.Err }

// RouteError is the DeclError of a .route.
type RouteError = DeclError

type boundRoute struct {
	r       *decl.Route
	pkg     *sqlcgo.Package
	q       *sqlcgo.Query
	handler string
	// empty is the errs factory of the route's .empty, or "".
	empty string
	// source of each argument, by index: "path", "auth", "query" or "body".
	sources []string
	// names of the arguments in the request, by index: the {path}
	// parameter, query string or body field each is read from.
	names []string
	// mapFn is the signature of the route's .map func.
	mapFn MapFunc
	// prepareFn is the signature of the route's .prepare func.
	prepareFn PrepareFunc
	// emptyFn is the signature of the func of a successful .empty.
	emptyFn MapFunc
}

// genRoutes writes the routes type of file name: an interface of the sqlc
// queries the routes use, a <Name>Routes struct with Register, and one
// handler per route. It adds the imports the code needs.
func genRoutes(buf *bytes.Buffer, file string, routes []*decl.Route, opts Options, imports map[string]bool) error {
	if opts.Queries == nil {
		return &RouteError{Offset: routes[0].QueryOffset, Err: fmt.Errorf("routes need a sqlc config (sqlc.yaml) in the module")}
	}
	var bound []*boundRoute
	uses := map[string]int{}
	for _, r := range routes {
		pkg, q, err := opts.Queries(r.Query)
		if err != nil {
			return &RouteError{Offset: r.QueryOffset, Err: err}
		}
		b, err := bind(r, pkg, q)
		var re *RouteError
		if errors.As(err, &re) {
			return re
		}
		if err != nil {
			return &RouteError{Offset: r.QueryOffset, Err: err}
		}
		uses[q.Name]++
		bound = append(bound, b)
	}
	name := camel(file)
	var empties []*boundRoute
	for _, b := range bound {
		b.handler = b.q.Name
		if uses[b.q.Name] > 1 {
			b.handler += camel(strings.ToLower(b.r.Method))
		}
		if b.r.CacheOffset != 0 && b.q.Kind != sqlcgo.One && b.q.Kind != sqlcgo.Many {
			return &RouteError{Offset: b.r.CacheOffset, Err: fmt.Errorf(".cache applies to a :one or :many result; query %s is %s, which responds 204 with no body", b.q.Name, b.q.Kind)}
		}
		if b.r.Map != "" && b.q.Kind != sqlcgo.One && b.q.Kind != sqlcgo.Many {
			return &RouteError{Offset: b.r.MapOffset, Err: fmt.Errorf(".map turns a :one or :many result into the response; query %s is %s, which responds 204 with no body", b.q.Name, b.q.Kind)}
		}
		if b.r.Map != "" {
			b.mapFn = MapFunc{Params: []string{"row"}}
			if f, ok := opts.MapFuncs[b.r.Map]; ok {
				b.mapFn = f.For(b.q.Result.Expr)
			}
			if slices.Contains(b.mapFn.Params, "rows") && b.q.Kind != sqlcgo.Many {
				return &RouteError{Offset: b.r.MapOffset, Err: fmt.Errorf(".map %s takes the whole result, a slice; query %s is %s, so it gets one row", b.r.Map, b.q.Name, b.q.Kind)}
			}
			if slices.Contains(b.mapFn.Params, "caller") && b.r.Public {
				return &RouteError{Offset: b.r.MapOffset, Err: fmt.Errorf(".map %s takes the Caller, which a .public route has none of", b.r.Map)}
			}
		}
		if b.r.Prepare != "" {
			b.prepareFn = PrepareFunc{Params: []string{"args"}, Err: true}
			if f, ok := opts.PrepareFuncs[b.r.Prepare]; ok {
				b.prepareFn = f
			}
			if len(b.q.Args) == 0 {
				return &RouteError{Offset: b.r.PrepareOffset, Err: fmt.Errorf(".prepare %s: query %s takes no arguments to prepare", b.r.Prepare, b.q.Name)}
			}
			if slices.Contains(b.prepareFn.Params, "caller") && b.r.Public {
				return &RouteError{Offset: b.r.PrepareOffset, Err: fmt.Errorf(".prepare %s takes the Caller, which a .public route has none of", b.r.Prepare)}
			}
		}
		if b.r.Wrap != "" && b.q.Kind != sqlcgo.One && b.q.Kind != sqlcgo.Many {
			return &RouteError{Offset: b.r.WrapOffset, Err: fmt.Errorf(".wrap puts a :one or :many result in an object; query %s is %s, which responds 204 with no body", b.q.Name, b.q.Kind)}
		}
		if e := b.r.Empty; e != nil {
			if b.q.Kind != sqlcgo.One && b.q.Kind != sqlcgo.ExecRows {
				return &RouteError{Offset: e.Offset, Err: fmt.Errorf(".empty applies to a :one query that finds no row or an :execrows query that changes none; query %s is %s", b.q.Name, b.q.Kind)}
			}
			if e.OK() {
				if e.Func != "" {
					b.emptyFn = opts.EmptyFuncs[e.Func]
				}
				switch {
				case slices.Contains(b.emptyFn.Params, "caller") && b.r.Public:
					return &RouteError{Offset: e.Offset, Err: fmt.Errorf(".empty %d %s takes the Caller, which a .public route has none of", e.Status, e.Func)}
				case slices.Contains(b.emptyFn.Params, "args") && len(b.q.Args) == 0:
					return &RouteError{Offset: e.Offset, Err: fmt.Errorf(".empty %d %s takes the query's arguments; query %s takes none", e.Status, e.Func, b.q.Name)}
				}
			} else {
				b.empty = "err" + name + b.handler
				empties = append(empties, b)
			}
		}
	}
	if opts.Swag != nil && len(empties) == 0 {
		// The error responses' type for swag, which resolves it through
		// the file's imports.
		imports[ErrsImport] = true
		buf.WriteString("// errs.Error is the body of the routes' error responses.\nvar _ *errs.Error\n\n")
	}
	if len(empties) > 0 {
		imports[ErrsImport] = true
		buf.WriteString("// The errors of the routes with .empty: what a query that finds no row,\n// or changes none, means.\nvar (\n")
		for _, b := range empties {
			status := strconv.Itoa(b.r.Empty.Status)
			if n, ok := statusNames[b.r.Empty.Status]; ok {
				status = "http." + n
			}
			fmt.Fprintf(buf, "%s = errs.NewFactory(%s, %s)\n", b.empty, status, strconv.Quote(b.r.Empty.Msg))
		}
		buf.WriteString(")\n\n")
	}

	authed := slices.ContainsFunc(bound, func(b *boundRoute) bool { return !b.r.Public })
	imports["context"] = true
	imports["net/http"] = true
	imports[AfhttpImport] = true

	fmt.Fprintf(buf, "// %sQueries are the sqlc queries the routes in %s.af use. The\n// generated *Queries satisfies it.\n", name, file)
	fmt.Fprintf(buf, "type %sQueries interface {\n", name)
	seen := map[string]bool{}
	for _, b := range bound {
		if seen[b.q.Name] {
			continue
		}
		seen[b.q.Name] = true
		buf.WriteString(b.q.Name + "(ctx context.Context")
		if len(b.q.Args) > 0 {
			buf.WriteString(", arg " + argType(b))
			addImports(imports, b.pkg, argImports(b))
		}
		buf.WriteString(") ")
		switch b.q.Kind {
		case sqlcgo.One:
			fmt.Fprintf(buf, "(%s, error)", b.q.Result.Expr)
			addImports(imports, b.pkg, b.q.Result.Imports)
		case sqlcgo.Many:
			fmt.Fprintf(buf, "([]%s, error)", b.q.Result.Expr)
			addImports(imports, b.pkg, b.q.Result.Imports)
		case sqlcgo.ExecRows:
			buf.WriteString("(int64, error)")
		case sqlcgo.ExecResult:
			// The result type depends on the sql package; the routes
			// only need the error.
			return &RouteError{Offset: b.r.QueryOffset, Err: fmt.Errorf("query %s is :execresult; use :exec or :execrows for a route", b.q.Name)}
		default:
			buf.WriteString("error")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "// %sRoutes serves the routes declared in %s.af. Set its fields, then\n// call Register.\n", name, file)
	fmt.Fprintf(buf, "type %sRoutes struct {\n", name)
	fmt.Fprintf(buf, "Queries %sQueries\n", name)
	if authed {
		buf.WriteString("// Auth authenticates the caller of every route that is not .public.\n")
		buf.WriteString("// An error fails the request with afhttp.ErrUnauthorized, or as it\n// is when it is an *errs.Error. Declare Caller in this package; .auth\n// names read its fields.\n")
		buf.WriteString("Auth func(r *http.Request) (Caller, error)\n")
	}
	buf.WriteString("// OnError writes a failed request's error; nil means afhttp.WriteError.\n")
	buf.WriteString("OnError func(w http.ResponseWriter, r *http.Request, err error)\n")
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "// Register adds the routes to mux.\nfunc (rt *%sRoutes) Register(mux *http.ServeMux) {\n", name)
	for _, b := range bound {
		fmt.Fprintf(buf, "mux.HandleFunc(%s, rt.%s)\n", strconv.Quote(b.r.Method+" "+b.r.Path), b.handler)
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "func (rt *%sRoutes) fail(w http.ResponseWriter, r *http.Request, err error) {\n", name)
	buf.WriteString("if rt.OnError != nil {\nrt.OnError(w, r, err)\nreturn\n}\nafhttp.WriteError(w, r, err)\n}\n\n")

	for _, b := range bound {
		writeHandler(buf, name, b, opts, imports)
	}
	return nil
}

// respond writes what turns the query's result v into the response, and
// returns the value to respond with: v, passed through the route's .map
// func, or an object holding that under the route's .wrap key. A .map
// func that fails fails the request.
func (b *boundRoute) respond(buf *bytes.Buffer, v string) string {
	if b.r.Map != "" {
		v = b.mapped(buf, v)
	}
	if b.r.Wrap == "" {
		return v
	}
	return fmt.Sprintf("map[string]any{%s: %s}", strconv.Quote(b.r.Wrap), v)
}

// mapped returns v passed through the route's .map func, writing the call
// first when the func can fail.
func (b *boundRoute) mapped(buf *bytes.Buffer, v string) string {
	f, fn := b.mapFn, b.r.Map
	if f.simple() {
		if b.q.Kind == sqlcgo.Many {
			return fmt.Sprintf("afhttp.Map(%s, %s)", v, fn)
		}
		return fmt.Sprintf("%s(%s)", fn, v)
	}
	call := func(row string) string {
		var args []string
		for _, p := range f.Params {
			switch p {
			case "row", "rows":
				args = append(args, row)
			case "caller":
				args = append(args, "caller")
			case "ctx":
				args = append(args, "r.Context()")
			case "request":
				args = append(args, "r")
			}
		}
		return fn + "(" + strings.Join(args, ", ") + ")"
	}
	expr := call(v)
	if b.q.Kind == sqlcgo.Many && slices.Contains(f.Params, "row") {
		// Each row, with what else the func takes. The response is only
		// encoded, so its elements can be any: the closure needn't name
		// the func's result type, nor import its package.
		if f.Err {
			expr = fmt.Sprintf("afhttp.MapErr(%s, func(row %s) (any, error) {\nreturn %s\n})", v, b.q.Result.Expr, call("row"))
		} else {
			expr = fmt.Sprintf("afhttp.Map(%s, func(row %s) any {\nreturn %s\n})", v, b.q.Result.Expr, call("row"))
		}
	}
	if !f.Err {
		return expr
	}
	fmt.Fprintf(buf, "resp, err := %s\n", expr)
	buf.WriteString("if err != nil {\nrt.fail(w, r, err)\nreturn\n}\n")
	return "resp"
}

// emptyErr is the errs factory for a query that finds no row or changes
// none.
func (b *boundRoute) emptyErr() string {
	if b.empty != "" {
		return b.empty
	}
	return "afhttp.ErrNotFound"
}

func argType(b *boundRoute) string {
	if b.q.Params != "" {
		return b.q.Params
	}
	return b.q.Args[0].Type.Expr
}

func argImports(b *boundRoute) []string {
	if b.q.Params != "" {
		return []string{b.pkg.ImportPath}
	}
	return b.q.Args[0].Type.Imports
}

func addImports(imports map[string]bool, pkg *sqlcgo.Package, paths []string) {
	for _, p := range paths {
		imports[p] = true
	}
}

// Binding is where a route reads one query argument from.
type Binding struct {
	Arg sqlcgo.Field
	// Source is "path", "auth", "query" or "body".
	Source string
	// Name is the request's name for it: the {path} parameter, query
	// string or body field it is read from.
	Name string
}

// Bind reports where route r reads each argument of query q from.
func Bind(r *decl.Route, pkg *sqlcgo.Package, q *sqlcgo.Query) ([]Binding, error) {
	b, err := bind(r, pkg, q)
	if err != nil {
		return nil, err
	}
	out := make([]Binding, len(q.Args))
	for i, a := range q.Args {
		out[i] = Binding{Arg: a, Source: b.sources[i], Name: b.names[i]}
	}
	return out, nil
}

// normalize folds a name for matching: user_id, userId and UserID are
// one name.
func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}

func bind(r *decl.Route, pkg *sqlcgo.Package, q *sqlcgo.Query) (*boundRoute, error) {
	b := &boundRoute{r: r, pkg: pkg, q: q, sources: make([]string, len(q.Args)), names: make([]string, len(q.Args))}
	var names []string
	for i, a := range q.Args {
		names = append(names, a.JSON)
		b.names[i] = a.JSON
	}
	matches := func(a sqlcgo.Field, name string) bool {
		return normalize(a.Name) == normalize(name) || normalize(a.JSON) == normalize(name)
	}
	// bound[i] is the .bind of argument i, if any: it is read only by
	// that name.
	bound := make([]*decl.Bind, len(q.Args))
	for _, bd := range r.Binds {
		found := false
		for i, a := range q.Args {
			if matches(a, bd.Arg) {
				bound[i], found = &bd, true
				b.names[i] = bd.Name
			} else if matches(a, bd.Name) {
				return nil, &RouteError{Offset: bd.Offset, Err: fmt.Errorf(".bind %s = %s: %s is the name of another argument of query %s", bd.Arg, bd.Name, bd.Name, q.Name)}
			}
		}
		if !found {
			return nil, &RouteError{Offset: bd.Offset, Err: fmt.Errorf(".bind %s = %s: query %s has no argument %s (%s)", bd.Arg, bd.Name, q.Name, bd.Arg, strings.Join(names, ", "))}
		}
	}
	claim := func(name, source string) error {
		for i, a := range q.Args {
			if bd := bound[i]; bd != nil {
				if normalize(bd.Name) != normalize(name) {
					if matches(a, name) {
						return fmt.Errorf("%s parameter %s: argument %s of query %s is read from %s, as .bind says", source, name, a.JSON, q.Name, bd.Name)
					}
					continue
				}
			} else if !matches(a, name) {
				continue
			}
			if b.sources[i] != "" {
				return fmt.Errorf("%s is both %s and %s for query %s", name, b.sources[i], source, q.Name)
			}
			b.sources[i] = source
			if source == "path" {
				b.names[i] = name
			}
			return nil
		}
		if len(names) == 0 {
			return fmt.Errorf("%s parameter %s: query %s takes no arguments", source, name, q.Name)
		}
		return fmt.Errorf("%s parameter %s matches no argument of query %s (%s)", source, name, q.Name, strings.Join(names, ", "))
	}
	for _, p := range r.PathParams {
		if err := claim(p, "path"); err != nil {
			return nil, err
		}
	}
	for _, a := range r.Auth {
		if err := claim(a, "auth"); err != nil {
			return nil, err
		}
	}
	rest := "body"
	if r.BodyFromQueryString() {
		rest = "query"
	}
	for i := range b.sources {
		if b.sources[i] == "" {
			b.sources[i] = rest
		}
	}
	return b, nil
}

func writeHandler(buf *bytes.Buffer, routes string, b *boundRoute, opts Options, imports map[string]bool) {
	r, q := b.r, b.q
	var body []int
	for i := range q.Args {
		if b.sources[i] == "body" {
			body = append(body, i)
		}
	}
	bodyType := ""
	if len(body) > 0 {
		// Named, so API docs such as swag's can refer to it.
		bodyType = lowerFirst(routes) + b.handler + "Body"
		fmt.Fprintf(buf, "// %s is the JSON body of %s %s.\ntype %s struct {\n", bodyType, r.Method, r.Path, bodyType)
		for _, i := range body {
			a := q.Args[i]
			typ := a.Type.Expr
			if a.Type.Bytes {
				// sqlc maps json and jsonb to []byte, which encoding/json
				// reads as base64: take the JSON value as is instead.
				typ = "json.RawMessage"
				imports["encoding/json"] = true
			} else {
				addImports(imports, b.pkg, a.Type.Imports)
			}
			fmt.Fprintf(buf, "%s %s `json:%q`\n", camel(a.Name), typ, b.names[i])
		}
		buf.WriteString("}\n\n")
	}
	fmt.Fprintf(buf, "// %s serves %s %s with the %s query.\n", b.handler, r.Method, r.Path, q.Name)
	if r.Description != "" {
		buf.WriteString("//\n")
		for _, l := range strings.Split(r.Description, "\n") {
			fmt.Fprintf(buf, "// %s\n", strings.TrimRight(l, " \t"))
		}
	}
	if opts.Swag != nil {
		writeSwag(buf, b, bodyType, opts.Swag, imports)
	}
	fmt.Fprintf(buf, "func (rt *%sRoutes) %s(w http.ResponseWriter, r *http.Request) {\n", routes, b.handler)
	cc := r.CacheControl()
	withBody := q.Kind == sqlcgo.One || q.Kind == sqlcgo.Many
	noStore := withBody && hasNoStore(cc)
	if noStore {
		// On every response, errors too: none of them may be kept.
		fmt.Fprintf(buf, "w.Header().Set(\"Cache-Control\", %q)\n", cc)
	}
	usesCaller := slices.Contains(b.sources, "auth") || slices.Contains(b.mapFn.Params, "caller") || slices.Contains(b.prepareFn.Params, "caller") || slices.Contains(b.emptyFn.Params, "caller")
	if !r.Public {
		// An unset Auth fails closed rather than panicking.
		buf.WriteString("if rt.Auth == nil {\nrt.fail(w, r, errors.New(\"afhttp: Auth is not set\"))\nreturn\n}\n")
		imports["errors"] = true
		if usesCaller {
			buf.WriteString("caller, err := rt.Auth(r)\n")
		} else {
			buf.WriteString("_, err := rt.Auth(r)\n")
		}
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.Unauthorized(r.Context(), err))\nreturn\n}\n")
	}

	// field returns the Go expression of argument i.
	field := func(i int) string {
		if q.Params != "" {
			return "arg." + q.Args[i].Name
		}
		return "arg"
	}
	if len(q.Args) > 0 {
		fmt.Fprintf(buf, "var arg %s\n", argType(b))
	}
	for i, a := range q.Args {
		switch b.sources[i] {
		case "path":
			name := b.names[i]
			if a.Type.Expr == "string" {
				fmt.Fprintf(buf, "%s = r.PathValue(%q)\n", field(i), name)
				continue
			}
			fmt.Fprintf(buf, "if err := afhttp.Parse(%q, r.PathValue(%q), &%s); err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", name, name, field(i))
		case "auth":
			fmt.Fprintf(buf, "%s = caller.%s\n", field(i), callerField(a))
		case "query":
			fmt.Fprintf(buf, "if err := afhttp.ParseQuery(r, %q, &%s); err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", b.names[i], field(i))
		}
	}
	if len(body) > 0 {
		fmt.Fprintf(buf, "var body %s\n", bodyType)
		fmt.Fprintf(buf, "if err := afhttp.DecodeBody(w, r, %d, &body); err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", r.MaxBytes)
		for _, i := range body {
			a := q.Args[i]
			if a.Type.Bytes {
				fmt.Fprintf(buf, "%s = []byte(body.%s)\n", field(i), camel(a.Name))
				continue
			}
			fmt.Fprintf(buf, "%s = body.%s\n", field(i), camel(a.Name))
		}
	}

	if r.Prepare != "" {
		var args []string
		for _, p := range b.prepareFn.Params {
			args = append(args, map[string]string{"args": "&arg", "caller": "caller", "ctx": "r.Context()", "request": "r"}[p])
		}
		prep := r.Prepare + "(" + strings.Join(args, ", ") + ")"
		if b.prepareFn.Err {
			fmt.Fprintf(buf, "if err := %s; err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", prep)
		} else {
			buf.WriteString(prep + "\n")
		}
	}
	call := fmt.Sprintf("rt.Queries.%s(r.Context()", q.Name)
	if len(q.Args) > 0 {
		call += ", arg"
	}
	call += ")"
	status := func(def string) string {
		if name, ok := statusNames[r.Status]; ok {
			return "http." + name
		}
		if r.Status != 0 {
			return strconv.Itoa(r.Status)
		}
		return def
	}
	cache := ""
	if cc != "" && !noStore {
		// Set on success only, so no cache keeps an error.
		cache = fmt.Sprintf("w.Header().Set(\"Cache-Control\", %q)\n", cc)
		if v := r.Vary(); v != "" {
			// Add, keeping what middleware varies by, such as Origin.
			cache += fmt.Sprintf("w.Header().Add(\"Vary\", %q)\n", v)
		}
	}
	switch q.Kind {
	case sqlcgo.One:
		path, expr := b.pkg.NoRowsImport()
		imports[path] = true
		imports["errors"] = true
		fmt.Fprintf(buf, "row, err := %s\n", call)
		if e := r.Empty; e != nil && e.OK() {
			fmt.Fprintf(buf, "if errors.Is(err, %s) {\n", expr)
			b.writeEmptyOK(buf, cache, imports)
			buf.WriteString("return\n}\n")
		} else {
			fmt.Fprintf(buf, "if errors.Is(err, %s) {\nrt.fail(w, r, %s.New(r.Context()).AddError(err))\nreturn\n}\n", expr, b.emptyErr())
		}
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		resp := b.respond(buf, "row")
		buf.WriteString(cache)
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, %s)\n", status("http.StatusOK"), resp)
	case sqlcgo.Many:
		fmt.Fprintf(buf, "rows, err := %s\n", call)
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		fmt.Fprintf(buf, "if rows == nil {\nrows = []%s{}\n}\n", q.Result.Expr)
		resp := b.respond(buf, "rows")
		buf.WriteString(cache)
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, %s)\n", status("http.StatusOK"), resp)
	case sqlcgo.ExecRows:
		fmt.Fprintf(buf, "n, err := %s\n", call)
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		if e := r.Empty; e != nil && e.OK() {
			buf.WriteString("if n == 0 {\n")
			b.writeEmptyOK(buf, "", imports)
			buf.WriteString("return\n}\n")
		} else {
			fmt.Fprintf(buf, "if n == 0 {\nrt.fail(w, r, %s.New(r.Context()))\nreturn\n}\n", b.emptyErr())
		}
		fmt.Fprintf(buf, "w.WriteHeader(%s)\n", status("http.StatusNoContent"))
	default:
		fmt.Fprintf(buf, "if err := %s; err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n", call)
		fmt.Fprintf(buf, "w.WriteHeader(%s)\n", status("http.StatusNoContent"))
	}
	buf.WriteString("}\n\n")
}

// hasNoStore reports whether Cache-Control value cc has the no-store
// directive. Directive names are case-insensitive.
func hasNoStore(cc string) bool {
	for _, d := range strings.Split(cc, ",") {
		name, _, _ := strings.Cut(d, "=")
		if strings.EqualFold(strings.TrimSpace(name), "no-store") {
			return true
		}
	}
	return false
}

// writeEmptyOK writes the successful response of the route's .empty, for a
// query that found no row or changed none: no body, the JSON .empty gives,
// or what its func makes, which may fail the request. cache is the
// Cache-Control header a response with a body gets.
func (b *boundRoute) writeEmptyOK(buf *bytes.Buffer, cache string, imports map[string]bool) {
	e := b.r.Empty
	status := strconv.Itoa(e.Status)
	if name, ok := statusNames[e.Status]; ok {
		status = "http." + name
	}
	switch {
	case e.Func != "":
		var args []string
		for _, p := range b.emptyFn.Params {
			args = append(args, map[string]string{"args": "arg", "caller": "caller", "ctx": "r.Context()", "request": "r"}[p])
		}
		call := e.Func + "(" + strings.Join(args, ", ") + ")"
		if b.emptyFn.Err {
			fmt.Fprintf(buf, "resp, err := %s\nif err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", call)
			call = "resp"
		}
		buf.WriteString(cache)
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, %s)\n", status, call)
	case e.Body != "":
		imports["encoding/json"] = true
		buf.WriteString(cache)
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, json.RawMessage(%s))\n", status, strconv.Quote(e.Body))
	default:
		fmt.Fprintf(buf, "w.WriteHeader(%s)\n", status)
	}
}

var statusNames = map[int]string{
	200: "StatusOK",
	201: "StatusCreated",
	202: "StatusAccepted",
	203: "StatusNonAuthoritativeInfo",
	204: "StatusNoContent",
	205: "StatusResetContent",
	206: "StatusPartialContent",
	400: "StatusBadRequest",
	401: "StatusUnauthorized",
	402: "StatusPaymentRequired",
	403: "StatusForbidden",
	404: "StatusNotFound",
	409: "StatusConflict",
	410: "StatusGone",
	412: "StatusPreconditionFailed",
	422: "StatusUnprocessableEntity",
	423: "StatusLocked",
	429: "StatusTooManyRequests",
}

// callerField returns the Caller field an .auth name reads: the Params
// field's own name, so the caller field for user_id is UserID, initialisms
// included.
func callerField(a sqlcgo.Field) string {
	return strings.ToUpper(a.Name[:1]) + a.Name[1:]
}
