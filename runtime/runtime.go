// Package runtime embeds the sources of the runtime packages that generated
// code imports, so af eject can copy them into a repository.
package runtime

import (
	"embed"
	"io/fs"
	"path"
	"slices"
	"strings"
)

//go:embed aftool/*.go afhttp/*.go
var sources embed.FS

// Packages are the runtime packages, by name. Generated code imports
// ImportPrefix + name.
var Packages = []string{"afhttp", "aftool"}

// ImportPrefix is the import path of this directory, plus a slash.
const ImportPrefix = "github.com/omniaura/agentflow/runtime/"

// Files returns the non-test source files of package pkg, by file name.
func Files(pkg string) (map[string][]byte, error) {
	if !slices.Contains(Packages, pkg) {
		return nil, fs.ErrNotExist
	}
	entries, err := sources.ReadDir(pkg)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := sources.ReadFile(path.Join(pkg, e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = b
	}
	return files, nil
}
