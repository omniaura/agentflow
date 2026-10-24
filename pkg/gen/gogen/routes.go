package gogen

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

// AfhttpImport is the runtime package generated route code imports.
const AfhttpImport = "github.com/omniaura/agentflow/runtime/afhttp"

// Options configure generation.
type Options struct {
	// Queries resolves the sqlc query a .route names. Files with routes
	// need it.
	Queries func(name string) (*sqlcgo.Package, *sqlcgo.Query, error)
	// Imports replaces import paths in generated code, such as a runtime
	// package with the copy af eject wrote into the repository.
	Imports map[string]string
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
	// source of each argument, by index: "path", "auth", "query" or "body".
	sources []string
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
		if err != nil {
			return &RouteError{Offset: r.QueryOffset, Err: err}
		}
		uses[q.Name]++
		bound = append(bound, b)
	}
	for _, b := range bound {
		b.handler = b.q.Name
		if uses[b.q.Name] > 1 {
			b.handler += camel(strings.ToLower(b.r.Method))
		}
	}

	name := camel(file)
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
		writeHandler(buf, name, b, imports)
	}
	return nil
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
}

// Bind reports where route r reads each argument of query q from.
func Bind(r *decl.Route, pkg *sqlcgo.Package, q *sqlcgo.Query) ([]Binding, error) {
	b, err := bind(r, pkg, q)
	if err != nil {
		return nil, err
	}
	out := make([]Binding, len(q.Args))
	for i, a := range q.Args {
		out[i] = Binding{Arg: a, Source: b.sources[i]}
	}
	return out, nil
}

// normalize folds a name for matching: user_id, userId and UserID are
// one name.
func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}

func bind(r *decl.Route, pkg *sqlcgo.Package, q *sqlcgo.Query) (*boundRoute, error) {
	b := &boundRoute{r: r, pkg: pkg, q: q, sources: make([]string, len(q.Args))}
	var names []string
	for _, a := range q.Args {
		names = append(names, a.JSON)
	}
	claim := func(name, source string) error {
		for i, a := range q.Args {
			if normalize(a.Name) == normalize(name) || normalize(a.JSON) == normalize(name) {
				if b.sources[i] != "" {
					return fmt.Errorf("%s is both %s and %s for query %s", name, b.sources[i], source, q.Name)
				}
				b.sources[i] = source
				return nil
			}
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

func writeHandler(buf *bytes.Buffer, routes string, b *boundRoute, imports map[string]bool) {
	r, q := b.r, b.q
	fmt.Fprintf(buf, "// %s serves %s %s with the %s query.\n", b.handler, r.Method, r.Path, q.Name)
	if r.Description != "" {
		buf.WriteString("//\n")
		for _, l := range strings.Split(r.Description, "\n") {
			fmt.Fprintf(buf, "// %s\n", strings.TrimRight(l, " \t"))
		}
	}
	fmt.Fprintf(buf, "func (rt *%sRoutes) %s(w http.ResponseWriter, r *http.Request) {\n", routes, b.handler)
	usesCaller := slices.Contains(b.sources, "auth")
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
	var body []int
	for i, a := range q.Args {
		switch b.sources[i] {
		case "path":
			name := pathName(r, a)
			if a.Type.Expr == "string" {
				fmt.Fprintf(buf, "%s = r.PathValue(%q)\n", field(i), name)
				continue
			}
			fmt.Fprintf(buf, "if err := afhttp.Parse(%q, r.PathValue(%q), &%s); err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", name, name, field(i))
		case "auth":
			fmt.Fprintf(buf, "%s = caller.%s\n", field(i), callerField(a))
		case "query":
			fmt.Fprintf(buf, "if err := afhttp.ParseQuery(r, %q, &%s); err != nil {\nrt.fail(w, r, err)\nreturn\n}\n", a.JSON, field(i))
		case "body":
			body = append(body, i)
		}
	}
	if len(body) > 0 {
		buf.WriteString("var body struct {\n")
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
			fmt.Fprintf(buf, "%s %s `json:%q`\n", camel(a.Name), typ, a.JSON)
		}
		buf.WriteString("}\n")
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
	switch q.Kind {
	case sqlcgo.One:
		path, expr := b.pkg.NoRowsImport()
		imports[path] = true
		imports["errors"] = true
		fmt.Fprintf(buf, "row, err := %s\n", call)
		fmt.Fprintf(buf, "if errors.Is(err, %s) {\nrt.fail(w, r, afhttp.ErrNotFound.New(r.Context()).AddError(err))\nreturn\n}\n", expr)
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, row)\n", status("http.StatusOK"))
	case sqlcgo.Many:
		fmt.Fprintf(buf, "rows, err := %s\n", call)
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		fmt.Fprintf(buf, "if rows == nil {\nrows = []%s{}\n}\n", q.Result.Expr)
		fmt.Fprintf(buf, "afhttp.WriteJSON(w, r, %s, rows)\n", status("http.StatusOK"))
	case sqlcgo.ExecRows:
		fmt.Fprintf(buf, "n, err := %s\n", call)
		buf.WriteString("if err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n")
		buf.WriteString("if n == 0 {\nrt.fail(w, r, afhttp.ErrNotFound.New(r.Context()))\nreturn\n}\n")
		fmt.Fprintf(buf, "w.WriteHeader(%s)\n", status("http.StatusNoContent"))
	default:
		fmt.Fprintf(buf, "if err := %s; err != nil {\nrt.fail(w, r, afhttp.QueryFailed(r.Context(), err))\nreturn\n}\n", call)
		fmt.Fprintf(buf, "w.WriteHeader(%s)\n", status("http.StatusNoContent"))
	}
	buf.WriteString("}\n\n")
}

var statusNames = map[int]string{
	200: "StatusOK",
	201: "StatusCreated",
	202: "StatusAccepted",
	203: "StatusNonAuthoritativeInfo",
	204: "StatusNoContent",
	205: "StatusResetContent",
	206: "StatusPartialContent",
}

// pathName returns the {name} in r's path that a binds to.
func pathName(r *decl.Route, a sqlcgo.Field) string {
	for _, p := range r.PathParams {
		if normalize(p) == normalize(a.Name) || normalize(p) == normalize(a.JSON) {
			return p
		}
	}
	return a.JSON
}

// callerField returns the Caller field an .auth name reads: the Params
// field's own name, so the caller field for user_id is UserID, initialisms
// included.
func callerField(a sqlcgo.Field) string {
	return strings.ToUpper(a.Name[:1]) + a.Name[1:]
}
