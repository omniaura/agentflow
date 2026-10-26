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

// sqlWords returns the keywords and identifiers of sql in upper case, in
// order, without its comments, string literals and quoted identifiers, so
// a column named "select" or a string 'update' isn't taken for a clause.
func sqlWords(sql string) []string {
	var words []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			words = append(words, strings.ToUpper(word.String()))
			word.Reset()
		}
	}
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			flush()
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			flush()
			if end := strings.Index(sql[i+2:], "*/"); end >= 0 {
				i += end + 3
			} else {
				i = len(sql)
			}
		case c == '$' && dollarTag(sql[i:]) != "":
			// A dollar-quoted string, $$...$$ or $tag$...$tag$; $1 is a
			// parameter.
			flush()
			delim := dollarTag(sql[i:])
			if end := strings.Index(sql[i+len(delim):], delim); end >= 0 {
				i += len(delim) + end + len(delim) - 1
			} else {
				i = len(sql)
			}
		case c == '\'' || c == '"':
			flush()
			// Skip to the closing quote; a doubled quote escapes one.
			for i++; i < len(sql); i++ {
				if sql[i] == c {
					if i+1 < len(sql) && sql[i+1] == c {
						i++
						continue
					}
					break
				}
			}
		case c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			word.WriteByte(c)
		default:
			flush()
		}
	}
	flush()
	return words
}

// dollarTag returns the opening delimiter of a dollar-quoted string at the
// start of s, such as $$ or $body$, or "".
func dollarTag(s string) string {
	for j := 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return s[:j+1]
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 || j > 1 && c >= '0' && c <= '9':
		default:
			return ""
		}
	}
	return ""
}

// mayFindNone reports whether a query may find or change no row: anything
// but a plain INSERT ... VALUES, which always returns its row. One with a
// WHERE, a SELECT or ON CONFLICT may insert nothing.
func mayFindNone(sql string) bool {
	words := sqlWords(sql)
	if len(words) == 0 || words[0] != "INSERT" {
		return true
	}
	for i := 1; i < len(words); i++ {
		if words[i-1] == "ON" && words[i] == "CONFLICT" {
			return true
		}
	}
	return slices.Contains(words, "WHERE") || slices.Contains(words, "SELECT")
}

// mayConflict reports whether a query may violate a unique constraint,
// which the handler reports as 409: an INSERT or UPDATE, or a WITH query
// holding one.
func mayConflict(sql string) bool {
	words := sqlWords(sql)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "INSERT", "UPDATE":
		return true
	case "WITH":
		return slices.Contains(words, "INSERT") || slices.Contains(words, "UPDATE")
	}
	return false
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
	if withBody || r.Empty != nil && r.Empty.HasBody() {
		line("Produce json")
	}
	inputs := bodyType != ""
	for i, a := range q.Args {
		switch b.sources[i] {
		case "path":
			inputs = true
			name := b.names[i]
			line("Param %s path %s true %q", name, swagType(a.Type.Expr), name)
		case "query":
			inputs = true
			line("Param %s query %s %t %q", b.names[i], swagType(a.Type.Expr), !nullable(a.Type.Expr), b.names[i])
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
			if typ = b.mapFn.Result; typ == "" {
				typ = "object"
			}
			if slices.Contains(b.mapFn.Params, "rows") {
				// The func makes the whole response from all the rows.
				kind = "object"
				if elem, ok := strings.CutPrefix(typ, "[]"); ok {
					kind, typ = "array", elem
				}
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
	success := status
	if !withBody && r.Status == 0 {
		success = 204
	}
	if e := r.Empty; e != nil && e.OK() && e.Status != success {
		// What a query that finds no row responds with instead.
		switch {
		case e.Status == 204:
			line("Success 204")
		case e.Status == 205:
			line("Success 205")
		default:
			line("Success %d {object} %s", e.Status, swagObject(b.emptyFn.Result))
		}
	}
	failures := map[int]bool{500: true}
	failures[400] = inputs
	failures[401] = !r.Public
	// An :execrows query may always change no row.
	if (q.Kind == sqlcgo.ExecRows || q.Kind == sqlcgo.One && (r.Empty != nil || mayFindNone(q.SQL))) && (r.Empty == nil || !r.Empty.OK()) {
		code := 404
		if r.Empty != nil {
			code = r.Empty.Status
		}
		failures[code] = true
	}
	failures[409] = failures[409] || mayConflict(q.SQL)
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
