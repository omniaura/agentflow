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

//go:embed aftool/*.go afhttp/*.go afagent/*.go afanthropic/*.go afopenai/*.go afmodel/*.go afthread/*.go
var sources embed.FS

// Packages are the runtime packages generated code imports, by name, as
// ImportPrefix + name. Their only dependency outside the standard library
// is go-kit's errs, for afhttp.
var Packages = []string{"afagent", "afhttp", "aftool"}

// Adapters are the packages generated code doesn't import, ejected only
// when named: afanthropic and afopenai run agents on a provider's SDK,
// afmodel adds retries, fallback and metering to any model, and afthread
// keeps threads in a database. They import afagent, and the SDK or
// go-kit's errs.
var Adapters = []string{"afanthropic", "afopenai", "afmodel", "afthread"}

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
