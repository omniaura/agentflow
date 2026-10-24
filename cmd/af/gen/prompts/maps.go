package prompts

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"strings"

	afast "github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
)

// checkMapFuncs reports a .map naming no top-level func or var of the Go
// package next to name, where the generated handler calls it. Without the
// check, the mistake surfaces as a compile error in the generated file.
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
	fset := gotoken.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") || p == OutputPath(name) {
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
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				if d.Tok != gotoken.VAR {
					continue
				}
				for _, s := range d.Specs {
					for _, n := range s.(*ast.ValueSpec).Names {
						declared[n.Name] = true
					}
				}
			}
		}
	}
	for _, i := range want {
		r := f.Routes[i]
		if !declared[r.Map] {
			return &gogen.RouteError{Offset: r.MapOffset, Err: fmt.Errorf(".map %s: package %s declares no func %s; declare it in a .go file next to %s, taking the query's row and returning the response", r.Map, filepath.Base(dir), r.Map, filepath.Base(name))}
		}
	}
	return nil
}
