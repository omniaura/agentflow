package eject

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var versions = map[string]map[string]string{
	"1.0.0": {
		"a.go":   "// Package aftool is a test runtime.\npackage aftool\n\nfunc A() int { return 1 }\n\nfunc B() int { return 2 }\n",
		"old.go": "package aftool\n\nfunc Old() {}\n",
	},
	"1.1.0": {
		"a.go":   "// Package aftool is a test runtime.\npackage aftool\n\nfunc A() int { return 10 }\n\nfunc B() int { return 2 }\n",
		"new.go": "package aftool\n\nfunc New() {}\n",
	},
	"1.2.0": {
		"a.go":   "// Package aftool is a test runtime.\npackage aftool\n\nfunc A() int { return 100 }\n\nfunc B() int { return 2 }\n",
		"new.go": "package aftool\n\nfunc New() {}\n",
	},
}

func opts(version string) Options {
	return Options{
		Version: version,
		Files: func(pkg string) (map[string][]byte, error) {
			out := map[string][]byte{}
			for name, src := range versions[version] {
				out[name] = []byte(src)
			}
			return out, nil
		},
		Base: func(v, pkg, file string) ([]byte, error) {
			src, ok := versions[v][file]
			if !ok {
				return nil, errors.New("no such version")
			}
			return []byte(src), nil
		},
	}
}

func newModule(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func actions(rs []Result) map[string]string {
	m := map[string]string{}
	for _, r := range rs {
		m[filepath.Base(r.Path)] = r.Action
	}
	return m
}

func read(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func edit(t *testing.T, path, old, new string) {
	s := read(t, path)
	if !strings.Contains(s, old) {
		t.Fatalf("%s has no %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(s, old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEjectUpgradeMerge(t *testing.T) {
	root := newModule(t)
	sub := filepath.Join(root, "cmd", "x")
	os.MkdirAll(sub, 0o755)
	rs, err := Eject(sub, []string{"aftool"}, opts("1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(rs); got["a.go"] != "created" || got["old.go"] != "created" {
		t.Fatalf("eject: %v", got)
	}
	a := filepath.Join(root, "internal", "agentflow", "aftool", "a.go")
	if s := read(t, a); !strings.HasPrefix(s, "// Code ejected from github.com/omniaura/agentflow/runtime/aftool by af eject.\n") || !strings.Contains(s, "agentflow-vendored: v1.0.0 sha256=") || !strings.Contains(s, "\n\n// Package aftool is a test runtime.\npackage aftool") {
		t.Fatalf("a.go =\n%s", s)
	}
	imports, err := Imports(sub)
	if err != nil || imports["github.com/omniaura/agentflow/runtime/aftool"] != "example.com/m/internal/agentflow/aftool" {
		t.Fatalf("Imports = %v, %v", imports, err)
	}

	// Edit B, then upgrade: A's upstream change merges with the edit, the
	// dropped old.go goes, and new.go arrives.
	edit(t, a, "return 2", "return 20")
	rs, err = Eject(root, []string{"aftool"}, opts("1.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(rs); got["a.go"] != "merged" || got["old.go"] != "removed" || got["new.go"] != "created" {
		t.Fatalf("upgrade: %v", got)
	}
	s := read(t, a)
	if !strings.Contains(s, "return 10") || !strings.Contains(s, "return 20") || !strings.Contains(s, "agentflow-vendored: v1.1.0") {
		t.Fatalf("merged a.go =\n%s", s)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(a), "old.go")); !os.IsNotExist(err) {
		t.Fatal("old.go was not removed")
	}
	// Running the same version again changes nothing.
	rs, _ = Eject(root, []string{"aftool"}, opts("1.1.0"))
	if got := actions(rs); got["a.go"] != "unchanged" || got["new.go"] != "unchanged" {
		t.Fatalf("rerun: %v", got)
	}

	// An edit to the line upstream also changes conflicts.
	edit(t, a, "return 10", "return 11")
	rs, err = Eject(root, []string{"aftool"}, opts("1.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(rs); got["a.go"] != "conflict" || got["new.go"] != "upgraded" {
		t.Fatalf("conflict: %v", got)
	}
	if s := read(t, a); !strings.Contains(s, "<<<<<<< yours") || !strings.Contains(s, ">>>>>>> v1.2.0") || !strings.Contains(s, "return 20") {
		t.Fatalf("conflicted a.go =\n%s", s)
	}

	// --force replaces the edited file.
	o := opts("1.2.0")
	o.Force = true
	rs, _ = Eject(root, []string{"aftool"}, o)
	if got := actions(rs); got["a.go"] != "overwritten" {
		t.Fatalf("force: %v", got)
	}
	if s := read(t, a); strings.Contains(s, "<<<<<<<") || strings.Contains(s, "return 20") {
		t.Fatalf("forced a.go =\n%s", s)
	}
}

func TestEjectBaseUnavailable(t *testing.T) {
	root := newModule(t)
	if _, err := Eject(root, []string{"aftool"}, opts("1.0.0")); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(root, "internal", "agentflow", "aftool", "a.go")
	edit(t, a, "return 2", "return 20")
	o := opts("1.1.0")
	o.Base = func(string, string, string) ([]byte, error) { return nil, errors.New("offline") }
	rs, err := Eject(root, []string{"aftool"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(rs); got["a.go"] != "kept" {
		t.Fatalf("got %v", got)
	}
	if s := read(t, a); !strings.Contains(s, "return 20") || !strings.Contains(s, "v1.0.0") {
		t.Fatal("the edited file changed")
	}
	if s := read(t, a+".af-new"); !strings.Contains(s, "return 10") {
		t.Fatalf("af-new =\n%s", s)
	}
}

func TestEjectKeepsForeignFiles(t *testing.T) {
	root := newModule(t)
	dir := filepath.Join(root, "internal", "agentflow", "aftool")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package aftool // mine\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "extra.go"), []byte("package aftool\n"), 0o644)
	rs, err := Eject(root, []string{"aftool"}, opts("1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(rs); got["a.go"] != "kept" || got["extra.go"] != "" {
		t.Fatalf("got %v", got)
	}
}

func TestEjectErrors(t *testing.T) {
	root := newModule(t)
	if _, err := Eject(root, []string{"nope"}, Options{Version: "1"}); err == nil || !strings.Contains(err.Error(), `unknown runtime package "nope"`) {
		t.Fatalf("got %v", err)
	}
	os.WriteFile(filepath.Join(root, ConfigName), []byte(`{"runtime":{"aftool":"internal/tools"}}`), 0o644)
	if _, err := Eject(root, []string{"aftool"}, opts("1.0.0")); err == nil || !strings.Contains(err.Error(), "must be named aftool") {
		t.Fatalf("got %v", err)
	}
	os.WriteFile(filepath.Join(root, ConfigName), []byte(`{`), 0o644)
	if _, err := Eject(root, []string{"aftool"}, opts("1.0.0")); err == nil {
		t.Fatal("want an error for a bad agentflow.json")
	}
	if _, err := Eject(t.TempDir(), []string{"aftool"}, opts("1.0.0")); err == nil {
		t.Fatal("want an error outside a module")
	}
}

func TestRealRuntimeFiles(t *testing.T) {
	root := newModule(t)
	rs, err := Eject(root, nil, Options{Version: "9.9.9"})
	if err != nil || len(rs) != 0 {
		t.Fatalf("no packages: %v %v", rs, err)
	}
	rs, err = Eject(root, []string{"aftool", "afhttp"}, Options{Version: "9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	got := actions(rs)
	if got["aftool.go"] != "created" || got["afhttp.go"] != "created" {
		t.Fatalf("got %v", got)
	}
}
