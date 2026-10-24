package prompts

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	gotoken "go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	afast "github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
)

// securityRE matches a swag security definition in general API info:
// // @securityDefinitions.apikey BearerAuth
var securityRE = regexp.MustCompile(`(?m)^//\s*@securityDefinitions\.\w+\s+(\S+)`)

// swagOptions returns the swag annotations to write on the routes of the
// .af file name, or nil when its module doesn't use swag. Routes are
// tagged with the file's name and secured with the security definition
// nearest above the file, or the module's only one.
func swagOptions(name string, f afast.File) *gogen.Swag {
	root := moduleRoot(filepath.Dir(name))
	if root == "" || !usesSwag(filepath.Join(root, "go.mod")) {
		return nil
	}
	sw := &gogen.Swag{Tags: strings.TrimSuffix(filepath.Base(name), ".af"), MapTypes: mapTypes(name, f)}
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		if names := securityNames(dir, false); len(names) > 0 {
			sw.Security = names[0]
			break
		}
		if dir == root || dir == filepath.Dir(dir) {
			if names := securityNames(root, true); len(names) == 1 {
				sw.Security = names[0]
			}
			break
		}
	}
	return sw
}

// moduleRoot returns the directory of the go.mod above dir, or "".
func moduleRoot(dir string) string {
	dir, _ = filepath.Abs(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// usesSwag reports whether a go.mod requires swaggo/swag.
func usesSwag(gomod string) bool {
	f, err := os.Open(gomod)
	if err != nil {
		return false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.Contains(s.Text(), "github.com/swaggo/swag") {
			return true
		}
	}
	return false
}

// securityNames returns the swag security definitions declared in the Go
// files of dir, or of every directory under it when deep.
func securityNames(dir string, deep bool) []string {
	var names []string
	seen := map[string]bool{}
	scan := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(b, []byte("@securityDefinitions")) {
			return
		}
		for _, m := range securityRE.FindAllSubmatch(b, -1) {
			if n := string(m[1]); !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	if !deep {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, p := range paths {
			scan(p)
		}
		return names
	}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != dir {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			scan(path)
		}
		return nil
	})
	return names
}

// mapTypes returns the result types of the file's .map funcs, as written
// in the Go package next to it.
func mapTypes(name string, f afast.File) map[string]string {
	want := map[string]bool{}
	for _, r := range f.Routes {
		if r.Map != "" {
			want[r.Map] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	out := map[string]string{}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(name), "*.go"))
	fset := gotoken.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") || p == OutputPath(name) {
			continue
		}
		gf, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range gf.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !want[fn.Name.Name] || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			var b bytes.Buffer
			printer.Fprint(&b, fset, fn.Type.Results.List[0].Type)
			out[fn.Name.Name] = b.String()
		}
	}
	return out
}
