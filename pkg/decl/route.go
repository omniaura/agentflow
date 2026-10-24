package decl

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// RouteDirectives are the directives allowed inside a route section.
var RouteDirectives = []string{".auth", ".public", ".status", ".maxbytes", ".sql"}

// SQLKinds are the sqlc query commands a .sql line may name.
var SQLKinds = []string{":one", ":many", ":exec", ":execrows"}

// Route is a parsed .route section: an HTTP route served by a sqlc query.
//
//	.route PATCH /agents/{agent_id}/draft UpsertAgentDraft
//	.auth user_id
//	Saves the caller's draft for one agent.
//
// The query's arguments are bound by name, in order: path parameters, then
// the .auth names (fields of the caller), then the JSON body, or the query
// string for GET, HEAD and DELETE. Routes authenticate the caller unless
// they are .public.
type Route struct {
	Method string
	// Path is the route pattern, as http.ServeMux reads it.
	Path string
	// PathParams are the {names} in Path, in order.
	PathParams []string
	// Query names the sqlc query: Name, or pkg.Name when the repository
	// has several sqlc packages.
	Query string
	// Auth names the query arguments that come from the caller.
	Auth   []string
	Public bool
	// Status is the success status; 0 means the default for the query.
	Status      int
	MaxBytes    int
	Description string
	// SQL is the query written in the section after a .sql line, which af
	// gen hands to sqlc, and SQLKind its sqlc command (":one", ...). Both
	// are empty when the query lives in sqlc's own query files.
	SQL, SQLKind string
	// SQLOffset is the byte offset of the .sql line.
	SQLOffset int
	// Start and End delimit the section in the source; QueryOffset is the
	// byte offset of the query name.
	Start, End, QueryOffset int
}

var (
	methods     = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}
	pathParamRE = regexp.MustCompile(`\{([^{}]*)\}`)
	queryRE     = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*\.)?[A-Z][a-zA-Z0-9_]*$`)
)

// ParseRoute parses the route section src[start:end], whose first line is
// its .route line.
func ParseRoute(src []byte, start, end int) (*Route, error) {
	r := &Route{Start: start, End: end}
	header, i := line(src, start, end)
	fields := strings.Fields(strings.TrimPrefix(header, ".route"))
	if len(fields) != 3 {
		return nil, &Error{Offset: start, Msg: "expected .route <METHOD> <path> <Query>"}
	}
	r.Method, r.Path, r.Query = fields[0], fields[1], fields[2]
	r.QueryOffset = start + strings.LastIndex(header, r.Query)
	if !slices.Contains(methods, r.Method) {
		return nil, &Error{Offset: start, Msg: fmt.Sprintf("method %q must be one of %s", r.Method, strings.Join(methods, ", "))}
	}
	if !strings.HasPrefix(r.Path, "/") {
		return nil, &Error{Offset: start, Msg: fmt.Sprintf("path %q must start with /", r.Path)}
	}
	for _, m := range pathParamRE.FindAllStringSubmatch(r.Path, -1) {
		name := strings.TrimSuffix(m[1], "...")
		if !identRE.MatchString(name) {
			return nil, &Error{Offset: start, Msg: fmt.Sprintf("path parameter {%s} must be an identifier", m[1])}
		}
		if slices.Contains(r.PathParams, name) {
			return nil, &Error{Offset: start, Msg: fmt.Sprintf("path parameter {%s} appears twice", name)}
		}
		r.PathParams = append(r.PathParams, name)
	}
	if !queryRE.MatchString(r.Query) {
		return nil, &Error{Offset: r.QueryOffset, Msg: fmt.Sprintf("query %q must be an exported sqlc query name, optionally qualified by its package", r.Query)}
	}
	var desc []string
	for i < end {
		off := i
		var l string
		l, i = line(src, i, end)
		b := []byte(l)
		switch {
		case hasDirective(b, ".sql"):
			// The rest of the section is the query.
			kind := strings.TrimSpace(strings.TrimPrefix(l, ".sql"))
			if !slices.Contains(SQLKinds, kind) {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".sql needs the query's sqlc command, one of %s; got %q", strings.Join(SQLKinds, ", "), kind)}
			}
			if strings.Contains(r.Query, ".") {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf("query %s is written here, so it cannot name a package", r.Query)}
			}
			r.SQLKind, r.SQLOffset = kind, off
			sql, err := sqlBody(src, i, end, RouteDirectives)
			if err != nil {
				return nil, err
			}
			r.SQL = sql
			i = end
		case hasDirective(b, ".auth"):
			for _, name := range strings.FieldsFunc(strings.TrimPrefix(l, ".auth"), func(r rune) bool { return r == ' ' || r == '\t' || r == ',' }) {
				if !identRE.MatchString(name) {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf(".auth name %q must be an identifier", name)}
				}
				if slices.Contains(r.PathParams, name) {
					return nil, &Error{Offset: off, Msg: fmt.Sprintf("%q is a path parameter, so it cannot come from .auth", name)}
				}
				if !slices.Contains(r.Auth, name) {
					r.Auth = append(r.Auth, name)
				}
			}
		case hasDirective(b, ".public"):
			r.Public = true
		case hasDirective(b, ".status"):
			v := strings.TrimSpace(strings.TrimPrefix(l, ".status"))
			n, err := strconv.Atoi(v)
			if err != nil || n < 200 || n > 299 {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".status needs a 2xx status code, got %q", v)}
			}
			r.Status = n
		case hasDirective(b, ".maxbytes"):
			v := strings.TrimSpace(strings.TrimPrefix(l, ".maxbytes"))
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf(".maxbytes needs a positive integer, got %q", v)}
			}
			r.MaxBytes = n
		default:
			desc = append(desc, l)
		}
	}
	if r.Public && len(r.Auth) > 0 {
		return nil, &Error{Offset: start, Msg: "a .public route has no caller, so it cannot use .auth"}
	}
	r.Description = strings.TrimRight(trimLeadingBlankLines(strings.Join(desc, "\n")), " \t\n")
	return r, nil
}

// BodyFromQueryString reports whether the route reads its remaining
// arguments from the query string rather than a JSON body.
func (r *Route) BodyFromQueryString() bool {
	return r.Method == "GET" || r.Method == "HEAD" || r.Method == "DELETE"
}
