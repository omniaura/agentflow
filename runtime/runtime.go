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

//go:embed aftool/*.go afhttp/*.go afagent/*.go afanthropic/*.go afopenai/*.go
var sources embed.FS

// Packages are the runtime packages generated code imports, by name, as
// ImportPrefix + name. They have no dependencies.
var Packages = []string{"afagent", "afhttp", "aftool"}

// Adapters are the packages that run agents on a provider's SDK. They
// import afagent and the SDK, and are ejected only when named.
var Adapters = []string{"afanthropic", "afopenai"}

// ImportPrefix is the import path of this directory, plus a slash.
const ImportPrefix = "github.com/omniaura/agentflow/runtime/"

// Files returns the non-test source files of package pkg, by file name.
func Files(pkg string) (map[string][]byte, error) {
	if !slices.Contains(Packages, pkg) && !slices.Contains(Adapters, pkg) {
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
