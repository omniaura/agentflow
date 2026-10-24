package ast

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
)

// SiblingTool is a tool declared in another .af file of the same
// directory, whose generated code shares the Go package.
type SiblingTool struct {
	// Path is the .af file that declares the tool, and Src its source.
	Path string
	Src  []byte
	Tool *decl.Tool
}

// ReadFunc reads a file, such as os.ReadFile, or an editor's buffer of it
// before the disk.
type ReadFunc func(path string) ([]byte, error)

// SiblingTools returns the tools declared in the .af files next to path,
// other than path itself, in file order. An agent's .tools may name them.
// A file that can't be read or parsed is skipped: it reports its own
// errors when it is generated or linted.
func SiblingTools(path string) []SiblingTool {
	return SiblingToolsRead(path, os.ReadFile)
}

// SiblingToolsRead is SiblingTools reading each file with read.
func SiblingToolsRead(path string, read ReadFunc) []SiblingTool {
	dir := filepath.Dir(path)
	paths, err := filepath.Glob(filepath.Join(dir, "*.af"))
	if err != nil {
		return nil
	}
	slices.Sort(paths)
	self := filepath.Base(path)
	var out []SiblingTool
	for _, p := range paths {
		if filepath.Base(p) == self {
			continue
		}
		src, err := read(p)
		if err != nil {
			continue
		}
		f, err := NewFile(filepath.Base(p), src)
		if err != nil {
			continue
		}
		for _, t := range f.Tools {
			out = append(out, SiblingTool{Path: p, Src: src, Tool: t})
		}
	}
	return out
}

// FindTool returns the tool an agent's .tools names: one of the file's,
// or of the other .af files of its package. The generated code of both
// shares the Go package, so the agent calls the other file's tool by name.
func FindTool(tools []*decl.Tool, siblings []SiblingTool, name string) (*decl.Tool, error) {
	var found *decl.Tool
	var where []string
	if i := slices.IndexFunc(tools, func(t *decl.Tool) bool { return t.Name == name }); i >= 0 {
		found = tools[i]
		where = append(where, "this file")
	}
	for _, s := range siblings {
		if s.Tool.Name == name {
			found = s.Tool
			where = append(where, filepath.Base(s.Path))
		}
	}
	switch {
	case len(where) > 1:
		return nil, fmt.Errorf("tool %s is declared in %s; a package declares each tool once", name, strings.Join(where, " and "))
	case found != nil:
		return found, nil
	}
	have := make([]string, 0, len(tools)+len(siblings))
	for _, t := range tools {
		have = append(have, t.Name)
	}
	for _, s := range siblings {
		have = append(have, s.Tool.Name)
	}
	list := "they declare none"
	if len(have) > 0 {
		list = "have " + strings.Join(have, ", ")
	}
	return nil, fmt.Errorf("no tool %s in this file or the other .af files of its directory (%s)", name, list)
}
