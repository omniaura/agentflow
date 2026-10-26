package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	afast "github.com/omniaura/agentflow/pkg/ast"
)

// RouteFuncs are the signatures of the Go funcs a file's routes call, by
// name.
type RouteFuncs struct {
	Map     map[string]MapFunc
	Prepare map[string]PrepareFunc
	// Empty are the funcs that make a successful .empty response. Their
	// Params may hold the query's "args" in place of a row.
	Empty map[string]MapFunc
	// Unread reports that a Go file of the package doesn't parse, so no
	// signature was read.
	Unread bool
}

// ReadRouteFuncs reads the signatures of the .map and .prepare funcs of f's
// routes in the Go package next to name, the .af file, leaving out the file
// generated from it at out: top-level funcs, or vars holding one, which the
// generated handlers call. It reports a func the package doesn't declare,
// or one whose signature doesn't fit (see MapFunc and PrepareFunc), at the
// directive's line. Without the check, the mistake surfaces as a compile
// error in the generated file. Whether the row's or arguments' types are
// right is left to the compiler. When a Go file of the package doesn't
// parse, it reads none and reports no error, which the compiler reports
// better, but sets Unread.
func ReadRouteFuncs(name, out string, f afast.File) (RouteFuncs, error) {
	var funcs RouteFuncs
	want := map[string]bool{}
	for _, r := range f.Routes {
		want[r.Map], want[r.Prepare] = true, true
		if r.Empty != nil {
			want[r.Empty.Func] = true
		}
	}
	delete(want, "")
	if len(want) == 0 {
		return funcs, nil
	}
	dir := filepath.Dir(name)
	declared, fset, err := readFuncs(dir, out)
	if err != nil {
		return funcs, err
	}
	if declared == nil {
		funcs.Unread = true
		return funcs, nil
	}
	missing := func(directive, fn, what string, off int) error {
		return &RouteError{Offset: off, Err: fmt.Errorf("%s %s: package %s declares no func %s; declare it in a .go file next to %s, %s", directive, fn, filepath.Base(dir), fn, filepath.Base(name), what)}
	}
	for _, r := range f.Routes {
		if r.Map != "" {
			d, found := declared[r.Map]
			if !found {
				return funcs, missing(".map", r.Map, "taking the query's row and returning the response", r.MapOffset)
			}
			shape := d.mapShape(fset)
			if shape == nil {
				continue
			}
			if shape.err != "" {
				return funcs, &RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: %s must be a func taking the query's row, or all its rows as a slice, optionally with the Caller, a context.Context or the *http.Request, and returning the response, optionally with an error; %s", r.Map, r.Map, shape.err)}
			}
			if funcs.Map == nil {
				funcs.Map = map[string]MapFunc{}
			}
			funcs.Map[r.Map] = shape.fn
		}
		if r.Prepare != "" {
			d, found := declared[r.Prepare]
			if !found {
				return funcs, missing(".prepare", r.Prepare, "taking a pointer to the query's arguments", r.PrepareOffset)
			}
			switch fn, why := d.prepareShape(); {
			case why != "":
				return funcs, &RouteError{Offset: r.PrepareOffset, Err: fmt.Errorf(".prepare %s: %s must be a func taking a pointer to the query's arguments, optionally with the Caller, a context.Context or the *http.Request, and returning nothing or an error; %s", r.Prepare, r.Prepare, why)}
			case fn != nil:
				if funcs.Prepare == nil {
					funcs.Prepare = map[string]PrepareFunc{}
				}
				funcs.Prepare[r.Prepare] = *fn
			}
		}
		if e := r.Empty; e != nil && e.Func != "" {
			d, found := declared[e.Func]
			if !found {
				return funcs, missing(".empty", e.Func, "returning the response", e.Offset)
			}
			switch fn, why := d.emptyShape(fset); {
			case why != "":
				return funcs, &RouteError{Offset: e.Offset, Err: fmt.Errorf(".empty %d %s: %s must be a func returning the response, optionally with an error, and taking any of the query's arguments, the Caller, a context.Context or the *http.Request; %s", e.Status, e.Func, e.Func, why)}
			case fn != nil:
				if funcs.Empty == nil {
					funcs.Empty = map[string]MapFunc{}
				}
				funcs.Empty[e.Func] = *fn
			}
		}
	}
	return funcs, nil
}

// funcDecl is a func of the package, or what may be one: its type, nil
// when its signature isn't known, or notFunc when it plainly isn't a func.
type funcDecl struct {
	ft      *ast.FuncType
	notFunc bool
}

// readFuncs reads the funcs of the Go package in dir, leaving out out, the
// generated file: top-level funcs, and vars that may hold one. It returns
// nil when a file doesn't parse, which the compiler reports better.
func readFuncs(dir, out string) (map[string]funcDecl, *gotoken.FileSet, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, nil, err
	}
	declared := map[string]funcDecl{}
	// Vars are judged after the scan, once every local type is known.
	type varDecl struct{ typ, v ast.Expr }
	vars := map[string]varDecl{}
	types := map[string]ast.Expr{}
	fset := gotoken.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") || p == out {
			continue
		}
		// Files the build leaves out, by build tag or GOOS/GOARCH suffix,
		// declare nothing the generated file sees.
		if ok, err := build.Default.MatchFile(dir, filepath.Base(p)); err != nil || !ok {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		gf, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, nil
		}
		for _, d := range gf.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = funcDecl{ft: d.Type}
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.TypeParams == nil {
							types[s.Name.Name] = s.Type
						}
					case *ast.ValueSpec:
						if d.Tok != gotoken.VAR {
							continue
						}
						for i, n := range s.Names {
							var v ast.Expr
							if i < len(s.Values) {
								v = s.Values[i]
							}
							vars[n.Name] = varDecl{s.Type, v}
						}
					}
				}
			}
		}
	}
	for n, d := range vars {
		declared[n] = varFunc(d.typ, d.v, types)
	}
	return declared, fset, nil
}

// mapShape is what a func can be as a .map func: its signature, or why it
// can't be one.
type mapShape struct {
	fn  MapFunc
	err string
}

// mapShape reads d as a .map func: nil when its signature isn't known.
func (d funcDecl) mapShape(fset *gotoken.FileSet) *mapShape {
	switch {
	case d.notFunc:
		return &mapShape{err: "it isn't a func"}
	case d.ft == nil:
		return nil
	}
	ft := d.ft
	var fn MapFunc
	rows := 0
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			kind := paramKind(field.Type)
			for range max(1, len(field.Names)) {
				if kind == "row" || kind == "rows" {
					rows++
					var b bytes.Buffer
					printer.Fprint(&b, fset, field.Type)
					fn.Row = b.String()
				} else if slices.Contains(fn.Params, kind) {
					return &mapShape{err: "it takes the " + kindNames[kind] + " twice"}
				}
				fn.Params = append(fn.Params, kind)
			}
		}
	}
	switch {
	case rows == 0:
		return &mapShape{err: "it takes no row"}
	case rows > 1:
		return &mapShape{err: "it takes more than one row"}
	}
	switch n := count(ft.Results); {
	case n == 2 && isIdent(ft.Results.List[len(ft.Results.List)-1].Type, "error"):
		fn.Err = true
	case n == 1 && isIdent(ft.Results.List[0].Type, "error"):
		return &mapShape{err: "it returns only an error, not the response"}
	case n != 1:
		return &mapShape{err: fmt.Sprintf("it returns %d values", n)}
	}
	var b bytes.Buffer
	printer.Fprint(&b, fset, ft.Results.List[0].Type)
	fn.Result = b.String()
	return &mapShape{fn: fn}
}

// prepareShape reads d as a .prepare func: its signature, or why it can't
// be one; neither when its signature isn't known.
func (d funcDecl) prepareShape() (*PrepareFunc, string) {
	switch {
	case d.notFunc:
		return nil, "it isn't a func"
	case d.ft == nil:
		return nil, ""
	}
	var fn PrepareFunc
	args := 0
	if d.ft.Params != nil {
		for _, field := range d.ft.Params.List {
			kind := paramKind(field.Type)
			switch kind {
			case "row", "rows":
				if _, ptr := field.Type.(*ast.StarExpr); !ptr {
					return nil, "it takes the arguments by value, so its changes would be lost; take a pointer"
				}
				kind = "args"
			}
			for range max(1, len(field.Names)) {
				if kind == "args" {
					args++
				} else if slices.Contains(fn.Params, kind) {
					return nil, "it takes the " + kindNames[kind] + " twice"
				}
				fn.Params = append(fn.Params, kind)
			}
		}
	}
	switch {
	case args == 0:
		return nil, "it takes no pointer to the arguments"
	case args > 1:
		return nil, "it takes the arguments more than once"
	}
	switch n := count(d.ft.Results); {
	case n == 1 && isIdent(d.ft.Results.List[0].Type, "error"):
		fn.Err = true
	case n != 0:
		return nil, "it returns something other than an error"
	}
	return &fn, ""
}

// emptyShape reads d as a successful .empty's func: its signature, or why
// it can't be one; neither when its signature isn't known.
func (d funcDecl) emptyShape(fset *gotoken.FileSet) (*MapFunc, string) {
	switch {
	case d.notFunc:
		return nil, "it isn't a func"
	case d.ft == nil:
		return nil, ""
	}
	var fn MapFunc
	if d.ft.Params != nil {
		for _, field := range d.ft.Params.List {
			kind := paramKind(field.Type)
			if kind == "row" || kind == "rows" {
				kind = "args"
			}
			for range max(1, len(field.Names)) {
				if slices.Contains(fn.Params, kind) {
					return nil, "it takes the " + map[string]string{"args": "arguments", "caller": "Caller", "ctx": "context", "request": "request"}[kind] + " twice"
				}
				fn.Params = append(fn.Params, kind)
			}
		}
	}
	switch n := count(d.ft.Results); {
	case n == 2 && isIdent(d.ft.Results.List[len(d.ft.Results.List)-1].Type, "error"):
		fn.Err = true
	case n == 1 && isIdent(d.ft.Results.List[0].Type, "error"):
		return nil, "it returns only an error, not the response"
	case n != 1:
		return nil, fmt.Sprintf("it returns %d values", n)
	}
	var b bytes.Buffer
	printer.Fprint(&b, fset, d.ft.Results.List[0].Type)
	fn.Result = b.String()
	return &fn, ""
}

var kindNames = map[string]string{"caller": "Caller", "ctx": "context", "request": "request"}

// paramKind is what a route func's parameter of type t is given: the
// "caller", the request's "ctx", the "request", all the "rows" (a slice),
// or else the "row" (or, for a .prepare func, the arguments).
func paramKind(t ast.Expr) string {
	switch t := t.(type) {
	case *ast.Ident:
		if t.Name == "Caller" {
			return "caller"
		}
	case *ast.SelectorExpr:
		if isIdent(t.X, "context") && t.Sel.Name == "Context" {
			return "ctx"
		}
	case *ast.StarExpr:
		if s, ok := t.X.(*ast.SelectorExpr); ok && isIdent(s.X, "http") && s.Sel.Name == "Request" {
			return "request"
		}
	case *ast.ArrayType:
		if t.Len == nil {
			return "rows"
		}
	}
	return "row"
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// varFunc reads a var of type typ, or holding value v when typ is nil, as
// a func. Named types resolve through types, the package's own. A var of
// an imported type, or one holding another func, may compile: its
// signature is unknown, and the compiler checks the rest.
func varFunc(typ, v ast.Expr, types map[string]ast.Expr) funcDecl {
	notFunc := funcDecl{notFunc: true}
	for range 10 {
		// Follow type A B chains, but not around a cycle.
		id, ok := typ.(*ast.Ident)
		if !ok || types[id.Name] == nil {
			break
		}
		typ = types[id.Name]
	}
	switch t := typ.(type) {
	case *ast.FuncType:
		return funcDecl{ft: t}
	case *ast.Ident:
		if slices.Contains(basicTypes, t.Name) {
			return notFunc
		}
		return funcDecl{}
	case *ast.ArrayType, *ast.MapType, *ast.StructType, *ast.StarExpr, *ast.ChanType, *ast.InterfaceType:
		return notFunc
	case nil:
	default:
		return funcDecl{}
	}
	switch v := v.(type) {
	case *ast.FuncLit:
		return funcDecl{ft: v.Type}
	case *ast.BasicLit, *ast.CompositeLit:
		return notFunc
	}
	return funcDecl{}
}

var basicTypes = []string{"bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "byte", "rune", "error", "any"}

// count is the number of values in a parameter or result list: func(a, b
// int) has two.
func count(l *ast.FieldList) int {
	if l == nil {
		return 0
	}
	n := 0
	for _, f := range l.List {
		n += max(1, len(f.Names))
	}
	return n
}
