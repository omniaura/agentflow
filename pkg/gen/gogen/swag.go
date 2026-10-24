package gogen

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/omniaura/agentflow/pkg/sqlcgo"
)

// Swag configures swag (github.com/swaggo/swag) annotations on generated
// route handlers, so `swag init` documents them with the hand-written ones.
type Swag struct {
	// Security names the security definition authenticated routes use,
	// such as BearerAuth, or "" for none.
	Security string
	// Tags groups the file's routes, such as its file name.
	Tags string
	// MapTypes are the result types of .map funcs by name, such as
	// TodoCard, when the parser could tell.
	MapTypes map[string]string
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

var wildcardRE = regexp.MustCompile(`\{([^{}.]+)\.\.\.\}`)

// swagType is the swag parameter type of a Go type.
func swagType(goType string) string {
	switch goType {
	case "pgtype.Int2", "pgtype.Int4", "pgtype.Int8", "sql.NullInt16", "sql.NullInt32", "sql.NullInt64":
		return "integer"
	case "pgtype.Float4", "pgtype.Float8", "sql.NullFloat64":
		return "number"
	case "pgtype.Bool", "sql.NullBool":
		return "boolean"
	}
	switch {
	case slices.Contains(intTypes, goType):
		return "integer"
	case slices.Contains(floatTypes, goType):
		return "number"
	case goType == "bool":
		return "boolean"
	}
	return "string"
}

// mayFindNone reports whether a query may find or change no row: anything
// but an INSERT with no WHERE, which always returns its row.
func mayFindNone(sql string) bool {
	var code []string
	for _, l := range strings.Split(strings.ToUpper(sql), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "--") {
			code = append(code, l)
		}
	}
	stmt := strings.Join(code, " ")
	return !strings.HasPrefix(stmt, "INSERT") || strings.Contains(stmt, "WHERE")
}

// writeSwag writes the swag annotations of b's handler, as lines of its
// doc comment.
func writeSwag(buf *bytes.Buffer, b *boundRoute, bodyType string, sw *Swag, imports map[string]bool) {
	r, q := b.r, b.q
	line := func(format string, args ...any) {
		fmt.Fprintf(buf, "// @"+format+"\n", args...)
	}
	buf.WriteString("//\n")
	summary := firstLine(r.Description)
	if summary == "" {
		summary = r.Method + " " + r.Path
	}
	line("Summary %s", summary)
	if desc := strings.Join(strings.Fields(r.Description), " "); desc != "" && desc != summary {
		line("Description %s", desc)
	}
	if sw.Tags != "" {
		line("Tags %s", sw.Tags)
	}
	if !r.Public && sw.Security != "" {
		line("Security %s", sw.Security)
	}
	if bodyType != "" {
		line("Accept json")
	}
	withBody := q.Kind == sqlcgo.One || q.Kind == sqlcgo.Many
	if withBody {
		line("Produce json")
	}
	inputs := bodyType != ""
	for i, a := range q.Args {
		switch b.sources[i] {
		case "path":
			inputs = true
			name := pathName(r, a)
			line("Param %s path %s true %q", name, swagType(a.Type.Expr), name)
		case "query":
			inputs = true
			line("Param %s query %s %t %q", a.JSON, swagType(a.Type.Expr), !nullable(a.Type.Expr), a.JSON)
		}
	}
	if bodyType != "" {
		line("Param body body %s true \"request body\"", bodyType)
	}
	status := 200
	if r.Status != 0 {
		status = r.Status
	}
	if withBody {
		kind, typ := "object", q.Result.Expr
		if q.Kind == sqlcgo.Many {
			kind = "array"
		}
		if r.Map != "" {
			if typ = sw.MapTypes[r.Map]; typ == "" {
				typ = "object"
			}
		}
		if r.Wrap != "" {
			// swag reads Go map types.
			elem := typ
			if kind == "array" {
				elem = "[]" + typ
			}
			kind, typ = "object", "map[string]"+elem
		}
		line("Success %d {%s} %s", status, kind, typ)
	} else {
		line("Success 204")
	}
	failures := map[int]bool{500: true}
	failures[400] = inputs
	failures[401] = !r.Public
	if (q.Kind == sqlcgo.One || q.Kind == sqlcgo.ExecRows) && (r.Empty != nil || mayFindNone(q.SQL)) {
		code := 404
		if r.Empty != nil {
			code = r.Empty.Status
		}
		failures[code] = true
	}
	failures[409] = failures[409] || q.Writes()
	imports[ErrsImport] = true
	for _, code := range []int{400, 401, 403, 404, 409, 410, 422, 500} {
		if failures[code] {
			line("Failure %d {object} errs.Error", code)
		}
	}
	for code := range failures {
		if failures[code] && !slices.Contains([]int{400, 401, 403, 404, 409, 410, 422, 500}, code) {
			line("Failure %d {object} errs.Error", code)
		}
	}
	line("Router %s [%s]", wildcardRE.ReplaceAllString(r.Path, "{$1}"), strings.ToLower(r.Method))
}
