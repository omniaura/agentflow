// Package eject copies agentflow's runtime packages into a repository, the
// way shadcn/ui copies components: the copy is yours to edit, generated code
// imports it, and running af eject again upgrades it, merging your edits.
//
// The copies are recorded in agentflow.json at the module root:
//
//	{"runtime": {"aftool": "internal/agentflow/aftool"}}
//
// Each copied file carries a vendored stamp (see pkg/vendored) with the af
// version it came from. On upgrade, an unedited file is replaced. An edited
// file is merged three ways, with the original from that af version
// (fetched through the Go module cache) as the base.
package eject

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/omniaura/agentflow/pkg/sqlcgo"
	"github.com/omniaura/agentflow/pkg/vendored"
	"github.com/omniaura/agentflow/runtime"
)

// ConfigName is the file, at the module root, that records ejected packages.
const ConfigName = "agentflow.json"

// DefaultDir is where packages are ejected, relative to the module root.
const DefaultDir = "internal/agentflow"

// Module is agentflow's module path, which base versions are fetched from.
const Module = "github.com/omniaura/agentflow"

// Config is the contents of agentflow.json.
type Config struct {
	// Runtime maps an ejected runtime package to its directory, relative
	// to the module root.
	Runtime map[string]string `json:"runtime,omitempty"`
}

// Module locates a Go module.
type moduleInfo struct {
	root, path string
}

func findModule(dir string) (moduleInfo, error) {
	root, path, err := sqlcgo.ModuleRoot(dir)
	if err != nil {
		return moduleInfo{}, err
	}
	rel, err := filepath.Rel(root, mustAbs(dir))
	if err != nil {
		return moduleInfo{}, err
	}
	if rel != "." {
		path = strings.TrimSuffix(path, "/"+filepath.ToSlash(rel))
	}
	return moduleInfo{root: root, path: path}, nil
}

func mustAbs(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// ReadConfig reads agentflow.json in root; a missing file is an empty
// config.
func ReadConfig(root string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(filepath.Join(root, ConfigName))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", filepath.Join(root, ConfigName), err)
	}
	return cfg, nil
}

func writeConfig(root string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ConfigName), append(b, '\n'), 0o644)
}

var (
	importsMu    sync.Mutex
	importsCache = map[string]map[string]string{}
)

// Imports returns the import path replacements for the module containing
// dir: each ejected runtime package's import path, mapped to its copy's.
func Imports(dir string) (map[string]string, error) {
	mod, err := findModule(dir)
	if err != nil {
		// Outside a module there is nothing ejected.
		return nil, nil
	}
	importsMu.Lock()
	defer importsMu.Unlock()
	if m, ok := importsCache[mod.root]; ok {
		return m, nil
	}
	cfg, err := ReadConfig(mod.root)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for pkg, d := range cfg.Runtime {
		m[runtime.ImportPrefix+pkg] = mod.path + "/" + path.Clean(filepath.ToSlash(d))
	}
	importsCache[mod.root] = m
	return m, nil
}

// Result is what happened to one file.
type Result struct {
	Path   string
	Action string // created, unchanged, upgraded, merged, conflict, kept, overwritten, removed
	Note   string
}

// Options configure Eject.
type Options struct {
	// Dir is where to put newly ejected packages, relative to the module
	// root; DefaultDir when empty. Already ejected packages stay put.
	Dir string
	// Version is the af version being ejected.
	Version string
	// Force overwrites edited files instead of merging them.
	Force bool
	// Base returns the original source of file in runtime package pkg at
	// af version; nil fetches it through the Go module cache.
	Base func(version, pkg, file string) ([]byte, error)
	// Files returns the sources of runtime package pkg at Version; nil
	// means the sources built into af.
	Files func(pkg string) (map[string][]byte, error)
}

// Eject copies or upgrades the runtime packages pkgs into the module that
// contains dir and records them in agentflow.json.
func Eject(dir string, pkgs []string, opts Options) ([]Result, error) {
	mod, err := findModule(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := ReadConfig(mod.root)
	if err != nil {
		return nil, err
	}
	if cfg.Runtime == nil {
		cfg.Runtime = map[string]string{}
	}
	if opts.Base == nil {
		opts.Base = moduleCacheBase
	}
	if opts.Dir == "" {
		opts.Dir = DefaultDir
	}
	if opts.Files == nil {
		opts.Files = runtime.Files
	}
	var results []Result
	for _, pkg := range pkgs {
		files, err := opts.Files(pkg)
		if err != nil {
			return results, fmt.Errorf("unknown runtime package %q (have %s)", pkg, strings.Join(runtime.Packages, ", "))
		}
		rel, ok := cfg.Runtime[pkg]
		if !ok {
			rel = path.Join(filepath.ToSlash(opts.Dir), pkg)
		}
		if path.Base(rel) != pkg {
			return results, fmt.Errorf("%s: the directory of package %s must be named %s", rel, pkg, pkg)
		}
		pkgDir := filepath.Join(mod.root, filepath.FromSlash(rel))
		rs, err := ejectPackage(pkgDir, pkg, files, opts)
		results = append(results, rs...)
		if err != nil {
			return results, err
		}
		cfg.Runtime[pkg] = rel
	}
	if err := writeConfig(mod.root, cfg); err != nil {
		return results, err
	}
	importsMu.Lock()
	delete(importsCache, mod.root)
	importsMu.Unlock()
	return results, nil
}

// header goes above each ejected file. The package's doc comment follows
// it after a blank line, so it stays the package doc.
func header(pkg string) string {
	return fmt.Sprintf("// Code ejected from %s/runtime/%s by af eject.\n"+
		"// This copy is yours to edit: af eject upgrades it and merges your edits.\n"+
		"// %s%s\n\n", Module, pkg, vendored.Marker, vendored.Placeholder)
}

func render(pkg string, src []byte, version string) ([]byte, error) {
	return vendored.Stamp(append([]byte(header(pkg)), src...), version)
}

func ejectPackage(dir, pkg string, files map[string][]byte, opts Options) ([]Result, error) {
	var results []Result
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r, err := ejectFile(filepath.Join(dir, name), pkg, name, files[name], opts)
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	// Remove files a newer version dropped, unless they were edited.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if _, ok := files[e.Name()]; ok || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return results, err
		}
		info, ok := vendored.Read(b)
		if !ok {
			continue // Not ours.
		}
		if info.Edited && !opts.Force {
			results = append(results, Result{Path: p, Action: "kept", Note: "dropped by af " + opts.Version + ", but edited"})
			continue
		}
		if err := os.Remove(p); err != nil {
			return results, err
		}
		results = append(results, Result{Path: p, Action: "removed"})
	}
	return results, nil
}

func ejectFile(p, pkg, name string, src []byte, opts Options) (Result, error) {
	next, err := render(pkg, src, opts.Version)
	if err != nil {
		return Result{}, err
	}
	cur, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Result{}, err
		}
		return Result{Path: p, Action: "created"}, os.WriteFile(p, next, 0o644)
	}
	if err != nil {
		return Result{}, err
	}
	if bytes.Equal(cur, next) {
		return Result{Path: p, Action: "unchanged"}, nil
	}
	info, stamped := vendored.Read(cur)
	switch {
	case stamped && !info.Edited:
		return Result{Path: p, Action: "upgraded", Note: "v" + info.Version + " -> v" + opts.Version}, os.WriteFile(p, next, 0o644)
	case opts.Force:
		return Result{Path: p, Action: "overwritten"}, os.WriteFile(p, next, 0o644)
	case !stamped:
		return Result{Path: p, Action: "kept", Note: "not written by af eject; rerun with --force to replace it"}, nil
	}
	if info.Version == opts.Version {
		// Edited, and already this version: nothing to upgrade.
		return Result{Path: p, Action: "unchanged", Note: "edited"}, nil
	}
	baseSrc, err := opts.Base(info.Version, pkg, name)
	var base []byte
	if err == nil {
		base, err = render(pkg, baseSrc, info.Version)
	}
	if err != nil {
		side := p + ".af-new"
		if werr := os.WriteFile(side, next, 0o644); werr != nil {
			return Result{}, werr
		}
		return Result{Path: p, Action: "kept", Note: fmt.Sprintf("edited, and the v%s original is unavailable (%v); the new version is in %s", info.Version, err, filepath.Base(side))}, nil
	}
	merged, conflicts, err := merge3(cur, base, next, info.Version, opts.Version)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(p, merged, 0o644); err != nil {
		return Result{}, err
	}
	if conflicts > 0 {
		return Result{Path: p, Action: "conflict", Note: fmt.Sprintf("%d conflict(s) to resolve between your edits and v%s", conflicts, opts.Version)}, nil
	}
	return Result{Path: p, Action: "merged", Note: "your edits kept, v" + info.Version + " -> v" + opts.Version}, nil
}

// merge3 merges the changes from base to theirs into ours with git
// merge-file, and returns the result and the number of conflicts.
func merge3(ours, base, theirs []byte, baseVersion, version string) ([]byte, int, error) {
	dir, err := os.MkdirTemp("", "af-merge")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs")}
	for i, b := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(paths[i], b, 0o644); err != nil {
			return nil, 0, err
		}
	}
	cmd := exec.Command("git", "merge-file", "-p",
		"-L", "yours", "-L", "v"+baseVersion, "-L", "v"+version,
		paths[0], paths[1], paths[2])
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out, 0, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0 && exitErr.ExitCode() < 128:
		// git merge-file exits with the number of conflicts.
		return out, exitErr.ExitCode(), nil
	}
	return nil, 0, fmt.Errorf("git merge-file: %v: %s", err, stderr.String())
}

// moduleCacheBase reads a runtime file of agentflow at version from the Go
// module cache, downloading the module if needed.
func moduleCacheBase(version, pkg, file string) ([]byte, error) {
	out, err := exec.Command("go", "mod", "download", "-json", Module+"@v"+strings.TrimPrefix(version, "v")).Output()
	if err != nil {
		return nil, fmt.Errorf("go mod download %s@v%s: %w", Module, version, err)
	}
	var info struct {
		Dir   string
		Error string
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	if info.Error != "" {
		return nil, errors.New(info.Error)
	}
	return os.ReadFile(filepath.Join(info.Dir, "runtime", pkg, file))
}
