package prompts

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	afast "github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
)

// checkMapFuncs reports a .map naming no func of the Go package next to
// name that takes one argument and returns one value: a top-level func, or
// a var holding a func. The generated handler calls it with the row.
// Without the check, the mistake surfaces as a compile error in the
// generated file. Whether the argument is the row's type is left to the
// compiler.
func checkMapFuncs(name string, f afast.File) error {
	var want []int
	for i, r := range f.Routes {
		if r.Map != "" {
			want = append(want, i)
		}
	}
	if len(want) == 0 {
		return nil
	}
	dir := filepath.Dir(name)
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	declared := map[string]bool{}
	// Vars are judged after the scan, once every local type is known.
	type varDecl struct{ typ, v ast.Expr }
	vars := map[string]varDecl{}
	types := map[string]ast.Expr{}
	fset := gotoken.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") || p == OutputPath(name) {
			continue
		}
		// Files the build leaves out, by build tag or GOOS/GOARCH suffix,
		// declare nothing the generated file sees.
		if ok, err := build.Default.MatchFile(dir, filepath.Base(p)); err != nil || !ok {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		gf, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			// The compiler reports it better; don't block generation.
			return nil
		}
		for _, d := range gf.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = mapper(d.Type)
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
		declared[n] = mapperVar(d.typ, d.v, types)
	}
	for _, i := range want {
		r := f.Routes[i]
		if ok, found := declared[r.Map]; found && !ok {
			return &gogen.RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: %s must be a func taking the query's row and returning the response", r.Map, r.Map)}
		}
		if !declared[r.Map] {
			return &gogen.RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: package %s declares no func %s; declare it in a .go file next to %s, taking the query's row and returning the response", r.Map, filepath.Base(dir), r.Map, filepath.Base(name))}
		}
	}
	return nil
}

// mapperVar reports whether a var of type typ, or holding value v when
// typ is nil, may be a .map func. Named types resolve through types, the
// package's own. Only what is plainly not one is rejected: a var of an
// imported type, or one holding another func, may compile, and the
// compiler checks the rest.
func mapperVar(typ, v ast.Expr, types map[string]ast.Expr) bool {
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
		return mapper(t)
	case *ast.Ident:
		return !slices.Contains(basicTypes, t.Name)
	case *ast.ArrayType, *ast.MapType, *ast.StructType, *ast.StarExpr, *ast.ChanType, *ast.InterfaceType:
		return false
	case nil:
	default:
		return true
	}
	switch v := v.(type) {
	case *ast.FuncLit:
		return mapper(v.Type)
	case *ast.BasicLit, *ast.CompositeLit:
		return false
	}
	return true
}

var basicTypes = []string{"bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "byte", "rune", "error", "any"}

// mapper reports whether a func of type ft can be a .map func: one
// parameter, one result.
func mapper(ft *ast.FuncType) bool {
	return count(ft.Params) == 1 && count(ft.Results) == 1
}

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
