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

// ReadMapFuncs reads the signatures of the .map funcs of f's routes in the
// Go package next to name, the .af file, leaving out the file generated
// from it at out: top-level funcs, or vars holding one, which the generated
// handlers call. It reports a .map naming none, or one that doesn't take the
// query's row (or all its rows, a slice), optionally with the Caller, the
// request's context.Context or the *http.Request, and return the response,
// optionally with an error. Without the check, the mistake surfaces as a
// compile error in the generated file. Whether the row's type is right is
// left to the compiler.
func ReadMapFuncs(name, out string, f afast.File) (map[string]MapFunc, error) {
	var want []int
	for i, r := range f.Routes {
		if r.Map != "" {
			want = append(want, i)
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	dir := filepath.Dir(name)
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	// declared holds what each name may be as a .map func: its shape, or
	// nil when it may be one but its signature isn't known.
	declared := map[string]*mapShape{}
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
			return nil, err
		}
		gf, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			// The compiler reports it better; don't block generation.
			return nil, nil
		}
		for _, d := range gf.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = shapeOf(fset, d.Type)
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
		declared[n] = shapeOfVar(fset, d.typ, d.v, types)
	}
	funcs := map[string]MapFunc{}
	for _, i := range want {
		r := f.Routes[i]
		shape, found := declared[r.Map]
		if !found {
			return nil, &RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: package %s declares no func %s; declare it in a .go file next to %s, taking the query's row and returning the response", r.Map, filepath.Base(dir), r.Map, filepath.Base(name))}
		}
		if shape == nil {
			continue
		}
		if shape.err != "" {
			return nil, &RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: %s must be a func taking the query's row, or all its rows as a slice, optionally with the Caller, a context.Context or the *http.Request, and returning the response, optionally with an error; %s", r.Map, r.Map, shape.err)}
		}
		funcs[r.Map] = shape.fn
	}
	return funcs, nil
}

// mapShape is what a func can be as a .map func: its signature, or why it
// can't be one.
type mapShape struct {
	fn  MapFunc
	err string
}

// shapeOf reads a func type as a .map func's.
func shapeOf(fset *gotoken.FileSet, ft *ast.FuncType) *mapShape {
	var fn MapFunc
	rows := 0
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			kind := paramKind(field.Type)
			for range max(1, len(field.Names)) {
				if kind == "row" || kind == "rows" {
					rows++
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
	case n != 1:
		return &mapShape{err: fmt.Sprintf("it returns %d values", n)}
	}
	var b bytes.Buffer
	printer.Fprint(&b, fset, ft.Results.List[0].Type)
	fn.Result = b.String()
	return &mapShape{fn: fn}
}

var kindNames = map[string]string{"caller": "Caller", "ctx": "context", "request": "request"}

// paramKind is what a .map func parameter of type t is given: the
// "caller", the request's "ctx", the "request", all the "rows" (a slice),
// or else the "row".
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

// shapeOfVar reads a var of type typ, or holding value v when typ is nil,
// as a .map func: its shape, nil when it may be one of unknown signature,
// or a shape with an error when it plainly isn't one. Named types resolve
// through types, the package's own. A var of an imported type, or one
// holding another func, may compile, and the compiler checks the rest.
func shapeOfVar(fset *gotoken.FileSet, typ, v ast.Expr, types map[string]ast.Expr) *mapShape {
	notFunc := &mapShape{err: "it isn't a func"}
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
		return shapeOf(fset, t)
	case *ast.Ident:
		if slices.Contains(basicTypes, t.Name) {
			return notFunc
		}
		return nil
	case *ast.ArrayType, *ast.MapType, *ast.StructType, *ast.StarExpr, *ast.ChanType, *ast.InterfaceType:
		return notFunc
	case nil:
	default:
		return nil
	}
	switch v := v.(type) {
	case *ast.FuncLit:
		return shapeOf(fset, v.Type)
	case *ast.BasicLit, *ast.CompositeLit:
		return notFunc
	}
	return nil
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
