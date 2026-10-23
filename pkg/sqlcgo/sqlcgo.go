// Package sqlcgo finds the Go packages sqlc generates and reads the
// signatures of their queries, so af gen can serve them as HTTP routes.
//
// It reads sqlc's output syntactically, without type checking, so it works
// before the rest of the package compiles and needs no build of the
// repository.
package sqlcgo

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// ConfigNames are the file names sqlc reads its configuration from.
var ConfigNames = []string{"sqlc.yaml", "sqlc.yml", "sqlc.json"}

// Kind is a sqlc query command: :one, :many, :exec, :execrows, ...
type Kind string

const (
	One        Kind = ":one"
	Many       Kind = ":many"
	Exec       Kind = ":exec"
	ExecRows   Kind = ":execrows"
	ExecResult Kind = ":execresult"
)

// Type is a Go type expression from sqlc's output, rendered for use from
// another package: names sqlc's package declares are qualified with its name.
type Type struct {
	Expr string
	// Imports are the import paths Expr refers to.
	Imports []string
	// Bytes reports a []byte, which sqlc uses for json and jsonb columns.
	Bytes bool
}

// Field is one argument of a query.
type Field struct {
	// Name is the Go name: a Params field, or the parameter of a query that
	// takes a single argument.
	Name string
	// JSON is the field's json tag name, or Name when it has none.
	JSON string
	Type Type
}

// Query is one generated query method.
type Query struct {
	Name string
	Kind Kind
	// Params is the type of the query's Params struct, empty when the
	// query takes one argument or none.
	Params string
	// Args are the query's arguments: the Params fields, or the single
	// argument.
	Args []Field
	// Result is the row type of :one and :many, or "" otherwise.
	Result Type
	// File and Line locate the query's method in sqlc's output.
	File string
	Line int
}

// Package is one sqlc-generated Go package.
type Package struct {
	Name, ImportPath, Dir string
	// SQLPackage is sqlc's sql_package: "pgx/v5", "pgx/v4" or
	// "database/sql".
	SQLPackage string
	Queries    map[string]*Query
	// Config is the sqlc config that generates the package, and QueryFiles
	// the absolute paths of its queries: files or directories.
	Config     string
	QueryFiles []string
	// LoadErr is why the package could not be read, such as sqlc not
	// having generated it yet.
	LoadErr error
}

// ReadsQueryFile reports whether sqlc reads the queries in path for p: path
// is one of its query files or inside one of its query directories.
func (p *Package) ReadsQueryFile(path string) bool {
	path = mustAbs(path)
	for _, q := range p.QueryFiles {
		if q == path {
			return true
		}
		if rel, err := filepath.Rel(q, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			if fi, err := os.Stat(q); err == nil && fi.IsDir() {
				return true
			}
		}
	}
	return false
}

func mustAbs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// ResetCache forgets the packages read so far, so the next Find rereads
// them, such as after sqlc generate.
func ResetCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = map[string]*Repo{}
}

// NoRowsImport and NoRowsExpr return the import and the expression of the
// error a :one query returns when it finds no row.
func (p *Package) NoRowsImport() (path, expr string) {
	switch p.SQLPackage {
	case "pgx/v5":
		return "github.com/jackc/pgx/v5", "pgx.ErrNoRows"
	case "pgx/v4":
		return "github.com/jackc/pgx/v4", "pgx.ErrNoRows"
	}
	return "database/sql", "sql.ErrNoRows"
}

// Repo holds the sqlc packages configured for a directory.
type Repo struct {
	Packages []*Package
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Repo{}
)

// Find loads the sqlc packages of the nearest sqlc config at or above dir.
// Results are cached per config file.
func Find(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for d := abs; ; d = filepath.Dir(d) {
		for _, name := range ConfigNames {
			path := filepath.Join(d, name)
			if _, err := os.Stat(path); err == nil {
				return load(path)
			}
		}
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil || filepath.Dir(d) == d {
			break
		}
	}
	// A monorepo may keep its sqlc config next to the package, below the
	// module root: look for configs anywhere in the module.
	root, _, err := moduleRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("no %s found at or above %s", strings.Join(ConfigNames, ", "), dir)
	}
	repo := &Repo{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && slices.Contains(ConfigNames, d.Name()) {
			r, err := load(path)
			if err != nil {
				return err
			}
			repo.Packages = append(repo.Packages, r.Packages...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(repo.Packages) == 0 {
		return nil, fmt.Errorf("no %s found in the module at %s", strings.Join(ConfigNames, " or "), root)
	}
	return repo, nil
}

// Lookup finds the query named name, or pkg.Name.
func (r *Repo) Lookup(name string) (*Package, *Query, error) {
	pkgName, qname, qualified := strings.Cut(name, ".")
	if !qualified {
		qname, pkgName = pkgName, ""
	}
	var found []*Package
	for _, p := range r.Packages {
		if pkgName != "" && p.Name != pkgName {
			continue
		}
		if _, ok := p.Queries[qname]; ok {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		for _, p := range r.Packages {
			if p.LoadErr != nil && (pkgName == "" || p.Name == pkgName) {
				return nil, nil, p.LoadErr
			}
		}
		var names []string
		for _, p := range r.Packages {
			names = append(names, p.Name)
		}
		return nil, nil, fmt.Errorf("no sqlc query %s in package %s", name, strings.Join(names, ", "))
	case 1:
		return found[0], found[0].Queries[qname], nil
	}
	return nil, nil, fmt.Errorf("sqlc query %s is in several packages (%s and %s); qualify it as %s.%s", qname, found[0].Name, found[1].Name, found[0].Name, qname)
}

type config struct {
	SQL []struct {
		// Queries is a path or a list of paths.
		Queries any `yaml:"queries"`
		Gen     struct {
			Go *struct {
				Package    string `yaml:"package"`
				Out        string `yaml:"out"`
				SQLPackage string `yaml:"sql_package"`
			} `yaml:"go"`
		} `yaml:"gen"`
	} `yaml:"sql"`
}

func load(path string) (*Repo, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if r, ok := cache[path]; ok {
		return r, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config
	// YAML is a superset of JSON, so this reads sqlc.json too.
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r := &Repo{}
	for _, s := range cfg.SQL {
		g := s.Gen.Go
		if g == nil {
			continue
		}
		dir := filepath.Join(filepath.Dir(path), g.Out)
		p, err := LoadPackage(dir)
		if err != nil {
			// Keep the package so its config and query files are known:
			// af gen writes queries before sqlc first generates it.
			_, importPath, _ := moduleRoot(dir)
			p = &Package{Name: g.Package, Dir: dir, ImportPath: importPath, Queries: map[string]*Query{}, LoadErr: fmt.Errorf("%s: %w", path, err)}
		}
		p.Config = path
		var queries []any
		switch q := s.Queries.(type) {
		case string:
			queries = []any{q}
		case []any:
			queries = q
		}
		for _, q := range queries {
			if q, ok := q.(string); ok {
				p.QueryFiles = append(p.QueryFiles, filepath.Join(filepath.Dir(path), q))
			}
		}
		if g.Package != "" && p.Name != g.Package {
			return nil, fmt.Errorf("%s: package %s in %s is not %s", path, p.Name, dir, g.Package)
		}
		p.SQLPackage = g.SQLPackage
		if p.SQLPackage == "" {
			p.SQLPackage = "database/sql"
		}
		r.Packages = append(r.Packages, p)
	}
	if len(r.Packages) == 0 {
		return nil, fmt.Errorf("%s has no gen.go output", path)
	}
	cache[path] = r
	return r, nil
}

var (
	nameRE  = regexp.MustCompile(`-- name: (\w+) (:\w+)`)
	majorRE = regexp.MustCompile(`^v[0-9]+$`)
)

// LoadPackage reads the sqlc-generated package in dir.
func LoadPackage(dir string) (*Package, error) {
	_, importPath, err := moduleRoot(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist yet; run sqlc generate", dir)
	}
	if err != nil {
		return nil, err
	}
	p := &Package{Dir: dir, ImportPath: importPath, Queries: map[string]*Query{}}
	kinds := map[string]Kind{}
	structs := map[string]*ast.StructType{}
	type method struct {
		decl *ast.FuncDecl
		file *ast.File
	}
	var methods []method
	files := map[*ast.File]bool{}
	local := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if p.Name == "" {
			p.Name = f.Name.Name
		}
		files[f] = true
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						local[spec.Name.Name] = true
						if st, ok := spec.Type.(*ast.StructType); ok {
							structs[spec.Name.Name] = st
						}
					case *ast.ValueSpec:
						for _, v := range spec.Values {
							if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if m := nameRE.FindStringSubmatch(lit.Value); m != nil {
									kinds[m[1]] = Kind(m[2])
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if isQueriesMethod(d) {
					methods = append(methods, method{d, f})
				}
			}
		}
	}
	if p.Name == "" {
		return nil, fmt.Errorf("no Go files in %s; run sqlc generate", dir)
	}
	q := qualifier{pkg: p.Name, importPath: p.ImportPath, local: local}
	for _, m := range methods {
		name := m.decl.Name.Name
		kind, ok := kinds[name]
		if !ok {
			continue
		}
		pos := fset.Position(m.decl.Name.Pos())
		query := &Query{Name: name, Kind: kind, File: pos.Filename, Line: pos.Line}
		params := m.decl.Type.Params.List[1:]
		switch {
		case len(params) == 0:
		case len(params) == 1 && len(params[0].Names) == 1:
			typ := q.render(m.file, params[0].Type)
			if id, ok := params[0].Type.(*ast.Ident); ok && structs[id.Name] != nil && strings.HasSuffix(id.Name, "Params") {
				query.Params = typ.Expr
				for _, f := range structs[id.Name].Fields.List {
					for _, n := range f.Names {
						query.Args = append(query.Args, Field{Name: n.Name, JSON: jsonName(f.Tag, n.Name), Type: q.render(m.file, f.Type)})
					}
				}
			} else {
				query.Args = []Field{{Name: params[0].Names[0].Name, JSON: params[0].Names[0].Name, Type: typ}}
			}
		default:
			// sqlc emits one argument or a Params struct; anything else
			// was not written by sqlc.
			continue
		}
		if kind == One || kind == Many {
			res := m.decl.Type.Results.List[0].Type
			if arr, ok := res.(*ast.ArrayType); ok && kind == Many {
				res = arr.Elt
			}
			query.Result = q.render(m.file, res)
		}
		p.Queries[name] = query
	}
	return p, nil
}

func isQueriesMethod(d *ast.FuncDecl) bool {
	if d.Recv == nil || len(d.Recv.List) != 1 || !d.Name.IsExported() {
		return false
	}
	star, ok := d.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Queries" && d.Type.Params.NumFields() >= 1 && d.Type.Results != nil
}

func jsonName(tag *ast.BasicLit, name string) string {
	if tag == nil {
		return name
	}
	s, err := strconv.Unquote(tag.Value)
	if err != nil {
		return name
	}
	j, _, _ := strings.Cut(reflect.StructTag(s).Get("json"), ",")
	if j == "" || j == "-" {
		return name
	}
	return j
}

type qualifier struct {
	pkg, importPath string
	local           map[string]bool
}

// render prints the type expression e from file f for use in another
// package.
func (q qualifier) render(f *ast.File, e ast.Expr) Type {
	var t Type
	if arr, ok := e.(*ast.ArrayType); ok && arr.Len == nil {
		if id, ok := arr.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") {
			t.Bytes = true
		}
	}
	e = q.qualify(f, e, &t)
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), e)
	t.Expr = b.String()
	return t
}

// qualify returns a copy of e with the package's own type names qualified,
// and records the imports e uses.
func (q qualifier) qualify(f *ast.File, e ast.Expr, t *Type) ast.Expr {
	switch e := e.(type) {
	case *ast.Ident:
		if q.local[e.Name] {
			t.Imports = appendUnique(t.Imports, q.importPath)
			return &ast.SelectorExpr{X: ast.NewIdent(q.pkg), Sel: ast.NewIdent(e.Name)}
		}
		return e
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if path := importPath(f, x.Name); path != "" {
				t.Imports = appendUnique(t.Imports, path)
			}
		}
		return e
	case *ast.StarExpr:
		return &ast.StarExpr{X: q.qualify(f, e.X, t)}
	case *ast.ArrayType:
		return &ast.ArrayType{Len: e.Len, Elt: q.qualify(f, e.Elt, t)}
	case *ast.MapType:
		return &ast.MapType{Key: q.qualify(f, e.Key, t), Value: q.qualify(f, e.Value, t)}
	}
	return e
}

func appendUnique(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

func importPath(f *ast.File, name string) string {
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if imp.Name != nil {
			if imp.Name.Name == name {
				return path
			}
			continue
		}
		base := path[strings.LastIndexByte(path, '/')+1:]
		// Major version suffixes (pgx/v5) are not the package name.
		if majorRE.MatchString(base) {
			trimmed := strings.TrimSuffix(path, "/"+base)
			base = trimmed[strings.LastIndexByte(trimmed, '/')+1:]
		}
		if base == name {
			return path
		}
	}
	return ""
}

// ModuleRoot returns the directory of the go.mod at or above dir and the
// import path of dir.
func ModuleRoot(dir string) (root, importPath string, err error) {
	return moduleRoot(dir)
}

// moduleRoot returns the directory of the go.mod above dir and the import
// path of dir.
func moduleRoot(dir string) (root, importPath string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for d := abs; ; d = filepath.Dir(d) {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			mod := modulePath(b)
			if mod == "" {
				return "", "", fmt.Errorf("%s/go.mod has no module line", d)
			}
			rel, err := filepath.Rel(d, abs)
			if err != nil {
				return "", "", err
			}
			if rel == "." {
				return d, mod, nil
			}
			return d, mod + "/" + filepath.ToSlash(rel), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		if filepath.Dir(d) == d {
			return "", "", fmt.Errorf("no go.mod at or above %s", dir)
		}
	}
}

func modulePath(gomod []byte) string {
	for _, l := range strings.Split(string(gomod), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`)
		}
	}
	return ""
}
