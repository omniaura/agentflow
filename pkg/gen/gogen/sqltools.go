package gogen

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
	"github.com/peyton-spencer/caseconv/bytcase"
)

// sqlTool is a tool written in SQL, bound to its sqlc query.
type sqlTool struct {
	t   *decl.Tool
	pkg *sqlcgo.Package
	q   *sqlcgo.Query
	// params[i] is the tool parameter query argument i reads, or nil when
	// it comes from the caller.
	params []*decl.Param
}

// bindSQLTools binds each tool written in SQL to its query. A query
// argument that is neither a .param nor .auth becomes a parameter, typed
// after the argument, so a tool needs no .param lines unless they add
// descriptions or narrow types. It returns the file's tools with those
// parameters added, leaving f's alone.
func bindSQLTools(f ast.File, opts Options) ([]*decl.Tool, []*sqlTool, error) {
	tools := slices.Clone(f.Tools)
	var bound []*sqlTool
	for i, t := range tools {
		if t.SQL == "" {
			continue
		}
		if opts.Queries == nil {
			return nil, nil, &DeclError{Offset: t.SQLOffset, Err: fmt.Errorf("tools written in SQL need a sqlc config (sqlc.yaml) in the module")}
		}
		pkg, q, err := opts.Queries(ToolQuery(t))
		if err != nil {
			return nil, nil, &DeclError{Offset: t.SQLOffset, Err: err}
		}
		clone := *t
		clone.Params = slices.Clone(t.Params)
		st := &sqlTool{t: &clone, pkg: pkg, q: q, params: make([]*decl.Param, len(q.Args))}
		matches := func(name string, a sqlcgo.Field) bool {
			return normalize(name) == normalize(a.Name) || normalize(name) == normalize(a.JSON)
		}
		for _, name := range t.Auth {
			if !slices.ContainsFunc(q.Args, func(a sqlcgo.Field) bool { return matches(name, a) }) {
				return nil, nil, &DeclError{Offset: t.Start, Err: fmt.Errorf("tool %s: .auth %s matches no argument of query %s (%s)", t.Name, name, q.Name, argNames(q))}
			}
		}
		used := map[*decl.Param]bool{}
		for j, a := range q.Args {
			if slices.ContainsFunc(t.Auth, func(name string) bool { return matches(name, a) }) {
				continue
			}
			k := slices.IndexFunc(clone.Params, func(p *decl.Param) bool { return matches(p.Name, a) })
			if k < 0 {
				p, err := inferParam(a)
				if err != nil {
					return nil, nil, &DeclError{Offset: t.SQLOffset, Err: fmt.Errorf("tool %s: %w", t.Name, err)}
				}
				clone.Params = append(clone.Params, p)
				k = len(clone.Params) - 1
			}
			st.params[j] = clone.Params[k]
			used[clone.Params[k]] = true
		}
		for _, p := range clone.Params {
			if !used[p] {
				return nil, nil, &DeclError{Offset: p.Offset, Err: fmt.Errorf("tool %s: parameter %s matches no argument of query %s (%s)", t.Name, p.Name, q.Name, argNames(q))}
			}
		}
		tools[i] = &clone
		bound = append(bound, st)
	}
	return tools, bound, nil
}

func argNames(q *sqlcgo.Query) string {
	if len(q.Args) == 0 {
		return "it takes none"
	}
	var names []string
	for _, a := range q.Args {
		names = append(names, a.JSON)
	}
	return strings.Join(names, ", ")
}

var (
	intTypes   = []string{"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64"}
	floatTypes = []string{"float32", "float64"}
)

// inferParam returns the tool parameter for query argument a, named in
// snake case after its Go name: NoteID is note_id.
func inferParam(a sqlcgo.Field) (*decl.Param, error) {
	p := &decl.Param{Name: string(bytcase.ToSnake([]byte(a.Name)))}
	typ := a.Type.Expr
	switch {
	case a.Type.Bytes:
		p.Type.Kind = decl.Any
		return p, nil
	case strings.HasPrefix(typ, "[]"):
		p.Type.Array = true
		typ = typ[2:]
	}
	switch {
	case slices.Contains(intTypes, typ):
		p.Type.Kind = decl.Int
	case slices.Contains(floatTypes, typ):
		p.Type.Kind = decl.Float
	case typ == "bool":
		p.Type.Kind = decl.Bool
	case p.Type.Array && typ != "string":
		// A list of database types: say what the model sends.
		return nil, fmt.Errorf("query argument %s has type %s; declare it with .param to say what the model sends", a.JSON, a.Type.Expr)
	default:
		// Strings, and database types that parse one, such as pgtype.UUID,
		// time.Time and enums.
		p.Type.Kind = decl.String
	}
	return p, nil
}

// goType is the Go type of a tool parameter's field in its Args struct.
func goType(p *decl.Param) string {
	switch p.Type.Kind {
	case decl.Int:
		return "int"
	case decl.Float:
		return "float64"
	case decl.Bool:
		return "bool"
	case decl.Any:
		return "json.RawMessage"
	case decl.Object:
		return ""
	}
	return "string"
}

// convert writes code that sets dst, of query type a, from the tool
// argument src of parameter p, or reports why it can't.
func convert(buf *bytes.Buffer, imports map[string]bool, p *decl.Param, a sqlcgo.Field, dst, src string) error {
	from, to := goType(p), a.Type.Expr
	if p.Type.Array {
		elem := strings.TrimPrefix(to, "[]")
		switch {
		case !strings.HasPrefix(to, "[]") || a.Type.Bytes:
			return fmt.Errorf("parameter %s is a list, but query argument %s is %s", p.Name, a.JSON, to)
		case elem == from:
			fmt.Fprintf(buf, "%s = %s\n", dst, src)
			return nil
		}
		// Convert element by element, checking each like a single value.
		// Optional applies to the list, not to its elements.
		single := *p
		single.Type.Array, single.Optional = false, false
		fmt.Fprintf(buf, "%s = make(%s, len(%s))\nfor i, v := range %s {\n", dst, to, src, src)
		if err := convert(buf, imports, &single, sqlcgo.Field{Name: a.Name, JSON: a.JSON, Type: sqlcgo.Type{Expr: elem}}, dst+"[i]", "v"); err != nil {
			return fmt.Errorf("parameter %s is a list of %s, but query argument %s is %s", p.Name, from, a.JSON, to)
		}
		buf.WriteString("}\n")
		return nil
	}
	parse := func(s string) {
		imports[AfhttpImport] = true
		fmt.Fprintf(buf, "if err := afhttp.Parse(%q, %s, &%s); err != nil {\nreturn \"\", err\n}\n", p.Name, s, dst)
	}
	switch {
	case from == "":
		return fmt.Errorf("parameter %s is an object; query argument %s needs a single value", p.Name, a.JSON)
	case from == to:
		fmt.Fprintf(buf, "%s = %s\n", dst, src)
	case from == "json.RawMessage" && a.Type.Bytes:
		fmt.Fprintf(buf, "%s = []byte(%s)\n", dst, src)
	case from == "int" && to == "int64":
		fmt.Fprintf(buf, "%s = %s(%s)\n", dst, to, src)
	case from == "float64" && to == "float32":
		// Parse, which rejects values float32 can't hold instead of
		// turning them into infinity.
		imports["strconv"] = true
		parse(fmt.Sprintf("strconv.FormatFloat(%s, 'g', -1, 64)", src))
	case from == "int" && slices.Contains(intTypes, to):
		// Narrower integers: parse, which checks the range.
		imports["strconv"] = true
		parse(fmt.Sprintf("strconv.Itoa(%s)", src))
	case from == "string" && !slices.Contains(intTypes, to) && !slices.Contains(floatTypes, to) && to != "bool" && !a.Type.Bytes && !strings.HasPrefix(to, "[]"):
		// Database types that parse a string. An optional parameter left
		// out stays the zero value: NULL for nullable types.
		if p.Optional {
			fmt.Fprintf(buf, "if %s != \"\" {\n", src)
			parse(src)
			buf.WriteString("}\n")
			return nil
		}
		parse(src)
	case from == "string" || from == "int" || from == "float64" || from == "bool":
		// A number or bool the model sends as text: parse it.
		if from != "string" {
			return fmt.Errorf("parameter %s is %s, but query argument %s is %s", p.Name, from, a.JSON, to)
		}
		if p.Optional {
			fmt.Fprintf(buf, "if %s != \"\" {\n", src)
			parse(src)
			buf.WriteString("}\n")
			return nil
		}
		parse(src)
	default:
		return fmt.Errorf("parameter %s is %s, but query argument %s is %s", p.Name, from, a.JSON, to)
	}
	return nil
}

// genSQLTools writes <File>SQLTools, which implements the tools written
// in SQL by running their queries.
func genSQLTools(buf *bytes.Buffer, file string, tools []*sqlTool, imports map[string]bool) error {
	name := camel(file)
	authed := slices.ContainsFunc(tools, func(st *sqlTool) bool { return len(st.t.Auth) > 0 })
	imports["context"] = true
	imports["errors"] = true
	imports["encoding/json"] = true
	imports[AftoolImport] = true

	fmt.Fprintf(buf, "// %sToolQueries are the sqlc queries the tools in %s.af that are\n// written in SQL run. The generated *Queries satisfies it.\n", name, file)
	fmt.Fprintf(buf, "type %sToolQueries interface {\n", name)
	for _, st := range tools {
		q := st.q
		buf.WriteString(q.Name + "(ctx context.Context")
		if len(q.Args) > 0 {
			if q.Params != "" {
				fmt.Fprintf(buf, ", arg %s", q.Params)
				imports[st.pkg.ImportPath] = true
			} else {
				fmt.Fprintf(buf, ", arg %s", q.Args[0].Type.Expr)
				addImports(imports, st.pkg, q.Args[0].Type.Imports)
			}
		}
		buf.WriteString(") ")
		switch q.Kind {
		case sqlcgo.One:
			fmt.Fprintf(buf, "(%s, error)", q.Result.Expr)
			addImports(imports, st.pkg, q.Result.Imports)
		case sqlcgo.Many:
			fmt.Fprintf(buf, "([]%s, error)", q.Result.Expr)
			addImports(imports, st.pkg, q.Result.Imports)
		case sqlcgo.ExecRows:
			buf.WriteString("(int64, error)")
		case sqlcgo.ExecResult:
			return &DeclError{Offset: st.t.SQLOffset, Err: fmt.Errorf("query %s is :execresult; use :exec or :execrows for a tool", q.Name)}
		default:
			buf.WriteString("error")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "// %sSQLTools runs the tools in %s.af that are written in SQL. Embed\n// it in the type that implements an agent's other tools.\n", name, file)
	fmt.Fprintf(buf, "type %sSQLTools struct {\nQueries %sToolQueries\n", name, name)
	if authed {
		buf.WriteString("// Caller returns the caller that .auth arguments come from, such as\n// the user an HTTP request authenticated, from the run's context.\n// Declare Caller in this package; .auth names read its fields.\n")
		buf.WriteString("Caller func(ctx context.Context) (Caller, error)\n")
	}
	buf.WriteString("}\n\n")

	for _, st := range tools {
		if err := writeSQLTool(buf, name, st, imports); err != nil {
			return &DeclError{Offset: st.t.SQLOffset, Err: fmt.Errorf("tool %s: %w", st.t.Name, err)}
		}
	}
	return nil
}

func writeSQLTool(buf *bytes.Buffer, name string, st *sqlTool, imports map[string]bool) error {
	t, q := st.t, st.q
	tn := camel(t.Name)
	fmt.Fprintf(buf, "// %s runs the %s tool: query %s.\n", tn, t.Name, q.Name)
	fmt.Fprintf(buf, "func (t *%sSQLTools) %s(ctx context.Context, args %sArgs) (string, error) {\n", name, tn, tn)
	if len(t.Auth) > 0 {
		// An unset Caller fails closed.
		fmt.Fprintf(buf, "if t.Caller == nil {\nreturn \"\", &aftool.Error{Msg: \"internal error\", Err: errors.New(\"%sSQLTools.Caller is not set\")}\n}\n", name)
		buf.WriteString("caller, err := t.Caller(ctx)\nif err != nil {\nreturn \"\", &aftool.Error{Msg: \"unauthorized\", Err: err}\n}\n")
	}
	field := func(i int) string {
		if q.Params != "" {
			return "arg." + q.Args[i].Name
		}
		return "arg"
	}
	if len(q.Args) > 0 {
		if q.Params != "" {
			fmt.Fprintf(buf, "var arg %s\n", q.Params)
		} else {
			fmt.Fprintf(buf, "var arg %s\n", q.Args[0].Type.Expr)
			addImports(imports, st.pkg, q.Args[0].Type.Imports)
		}
	}
	for i, a := range q.Args {
		p := st.params[i]
		if p == nil {
			fmt.Fprintf(buf, "%s = caller.%s\n", field(i), callerField(a))
			continue
		}
		if err := convert(buf, imports, p, a, field(i), "args."+camel(p.Name)); err != nil {
			return err
		}
	}
	call := fmt.Sprintf("t.Queries.%s(ctx", q.Name)
	if len(q.Args) > 0 {
		call += ", arg"
	}
	call += ")"
	failed := "if err != nil {\nreturn \"\", &aftool.Error{Msg: \"internal error\", Err: err}\n}\n"
	switch q.Kind {
	case sqlcgo.One:
		path, expr := st.pkg.NoRowsImport()
		imports[path] = true
		fmt.Fprintf(buf, "row, err := %s\n", call)
		fmt.Fprintf(buf, "if errors.Is(err, %s) {\nreturn \"\", &aftool.Error{Msg: \"not found\", Err: err}\n}\n", expr)
		buf.WriteString(failed)
		buf.WriteString("b, err := json.Marshal(row)\nreturn string(b), err\n}\n\n")
	case sqlcgo.Many:
		fmt.Fprintf(buf, "rows, err := %s\n", call)
		buf.WriteString(failed)
		fmt.Fprintf(buf, "if rows == nil {\nrows = []%s{}\n}\n", q.Result.Expr)
		buf.WriteString("b, err := json.Marshal(rows)\nreturn string(b), err\n}\n\n")
	case sqlcgo.ExecRows:
		fmt.Fprintf(buf, "n, err := %s\n", call)
		buf.WriteString(failed)
		// No row changed: missing, or not the caller's.
		buf.WriteString("if n == 0 {\nreturn \"\", &aftool.Error{Msg: \"not found\"}\n}\n")
		buf.WriteString("return fmt.Sprintf(`{\"rows_affected\":%d}`, n), nil\n}\n\n")
		imports["fmt"] = true
	default:
		fmt.Fprintf(buf, "if err := %s; err != nil {\nreturn \"\", &aftool.Error{Msg: \"internal error\", Err: err}\n}\n", call)
		buf.WriteString("return `{\"ok\":true}`, nil\n}\n\n")
	}
	return nil
}
