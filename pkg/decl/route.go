package decl

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RouteDirectives are the directives allowed inside a route section.
var RouteDirectives = []string{".auth", ".public", ".status", ".maxbytes", ".empty", ".wrap", ".cache", ".sql"}

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
	Status   int
	MaxBytes int
	// Empty, from .empty, is the error for a :one query that finds no row
	// or an :execrows query that changes none, instead of 404 not found.
	Empty *Empty
	// Wrap, from .wrap, names the one field of a JSON object the response
	// puts the query's result in: {"todos": [...]}. WrapOffset is the
	// .wrap line's.
	Wrap       string
	WrapOffset int
	// Cache, from .cache, is the Cache-Control value written as is, and
	// MaxAge the seconds of a .cache duration; CacheOffset is the .cache
	// line's. CacheControl resolves them.
	Cache       string
	MaxAge      int
	CacheOffset int
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
		case hasDirective(b, ".wrap"):
			if r.Wrap != "" {
				return nil, &Error{Offset: off, Msg: "only one .wrap line"}
			}
			key := strings.TrimSpace(strings.TrimPrefix(l, ".wrap"))
			if !identRE.MatchString(key) {
				return nil, &Error{Offset: off, Msg: fmt.Sprintf("usage: .wrap key, as in .wrap todos to respond {\"todos\": [...]}; got %q", key)}
			}
			r.Wrap, r.WrapOffset = key, off
		case hasDirective(b, ".cache"):
			if r.CacheOffset != 0 {
				return nil, &Error{Offset: off, Msg: "only one .cache line"}
			}
			v := strings.TrimSpace(strings.TrimPrefix(l, ".cache"))
			if err := r.parseCache(v); err != nil {
				return nil, &Error{Offset: off, Msg: err.Error()}
			}
			r.CacheOffset = off
		case hasDirective(b, ".empty"):
			if r.Empty != nil {
				return nil, &Error{Offset: off, Msg: "only one .empty line"}
			}
			e, err := parseEmpty(strings.TrimSpace(strings.TrimPrefix(l, ".empty")), off)
			if err != nil {
				return nil, err
			}
			r.Empty = e
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
	if r.MaxAge > 0 && r.Method != "GET" && r.Method != "HEAD" {
		return nil, &Error{Offset: r.CacheOffset, Msg: fmt.Sprintf("only GET and HEAD responses are cached; a %s route can use .cache no-store", r.Method)}
	}
	r.Description = strings.TrimRight(trimLeadingBlankLines(strings.Join(desc, "\n")), " \t\n")
	return r, nil
}

// parseCache reads a .cache value: a duration such as 5m, or a
// Cache-Control value to write as is.
func (r *Route) parseCache(v string) error {
	if v == "" {
		return fmt.Errorf("usage: .cache 5m, or a Cache-Control value such as .cache no-cache")
	}
	if d, err := time.ParseDuration(v); err == nil {
		if d < time.Second || d%time.Second != 0 {
			return fmt.Errorf(".cache needs a duration of whole seconds, at least 1s; got %s", v)
		}
		r.MaxAge = int(d / time.Second)
		return nil
	}
	if v[0] >= '0' && v[0] <= '9' {
		return fmt.Errorf(".cache needs a duration such as 30s, 5m or 1h; got %q", v)
	}
	for _, c := range v {
		if c < ' ' || c > '~' {
			return fmt.Errorf(".cache value %q must be printable ASCII", v)
		}
	}
	r.Cache = v
	return nil
}

// CacheControl is the Cache-Control header of the route's responses with
// a body, or "" for none. A .cache duration is private to the caller, or
// public on a .public route. Without .cache, responses to an
// authenticated caller are no-store, so no cache keeps one caller's data.
func (r *Route) CacheControl() string {
	switch {
	case r.MaxAge > 0 && r.Public:
		return fmt.Sprintf("public, max-age=%d", r.MaxAge)
	case r.MaxAge > 0:
		return fmt.Sprintf("private, max-age=%d", r.MaxAge)
	case r.Cache != "":
		return r.Cache
	case !r.Public:
		return "no-store"
	}
	return ""
}

// BodyFromQueryString reports whether the route reads its remaining
// arguments from the query string rather than a JSON body.
func (r *Route) BodyFromQueryString() bool {
	return r.Method == "GET" || r.Method == "HEAD" || r.Method == "DELETE"
}

// Empty is the failure a query that finds no row or changes none reports,
// from a .empty line:
//
//	.empty 409 you already have 20 thought trains
//
// A query can check what a handler would check in Go (who owns a row, a
// limit, a state) in its WHERE clause; .empty then names the failure.
type Empty struct {
	// Status is the HTTP status of a route's error, 400-499; a tool's
	// Empty has none.
	Status int
	// Msg is the message the client or model sees.
	Msg    string
	Offset int
}

// parseEmpty parses the arguments of a route's .empty line: an optional
// 4xx status, then the message.
func parseEmpty(args string, off int) (*Empty, error) {
	e := &Empty{Status: 404, Msg: args, Offset: off}
	if fields := strings.Fields(args); len(fields) > 0 && len(fields[0]) == 3 && fields[0][0] >= '0' && fields[0][0] <= '9' {
		n, err := strconv.Atoi(fields[0])
		if err != nil || n < 400 || n > 499 {
			return nil, &Error{Offset: off, Msg: fmt.Sprintf(".empty needs a 4xx status, got %q", fields[0])}
		}
		e.Status, e.Msg = n, strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	}
	if e.Msg == "" {
		return nil, &Error{Offset: off, Msg: "usage: .empty [4xx status] message, as in .empty 409 you already have 20 thought trains"}
	}
	return e, nil
}
