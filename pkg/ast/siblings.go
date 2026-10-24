package ast

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
)

// SiblingTool is a tool, or an agent another agent may call as one,
// declared in another .af file of the same directory, whose generated code
// shares the Go package. Exactly one of Tool and Agent is set.
type SiblingTool struct {
	// Path is the .af file that declares it, and Src its source.
	Path  string
	Src   []byte
	Tool  *decl.Tool
	Agent *decl.Agent
}

// Name is the tool's or agent's name.
func (s SiblingTool) Name() string {
	if s.Agent != nil {
		return s.Agent.Name
	}
	return s.Tool.Name
}

// Start is the offset of its section in Src.
func (s SiblingTool) Start() int {
	if s.Agent != nil {
		return s.Agent.Start
	}
	return s.Tool.Start
}

// ReadFunc reads a file, such as os.ReadFile, or an editor's buffer of it
// before the disk.
type ReadFunc func(path string) ([]byte, error)

// SiblingTools returns the tools and agents declared in the .af files next
// to path, other than path itself, in file order. An agent's .tools may
// name them.
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
		for _, a := range f.Agents {
			out = append(out, SiblingTool{Path: p, Src: src, Agent: a})
		}
	}
	return out
}

// FindTool returns what an agent's .tools names: a tool, or another agent
// to call as a subagent, declared in the file or in the other .af files of
// its package. The generated code of both shares the Go package, so the
// agent calls the other file's tool or agent by name.
func FindTool(tools []*decl.Tool, agents []*decl.Agent, siblings []SiblingTool, name string) (*decl.Tool, *decl.Agent, error) {
	var tool *decl.Tool
	var agent *decl.Agent
	var where []string
	if i := slices.IndexFunc(tools, func(t *decl.Tool) bool { return t.Name == name }); i >= 0 {
		tool = tools[i]
		where = append(where, "this file")
	}
	if i := slices.IndexFunc(agents, func(a *decl.Agent) bool { return a.Name == name }); i >= 0 {
		agent = agents[i]
		where = append(where, "this file")
	}
	for _, s := range siblings {
		if s.Name() == name {
			tool, agent = s.Tool, s.Agent
			where = append(where, filepath.Base(s.Path))
		}
	}
	switch {
	case len(where) > 1:
		return nil, nil, fmt.Errorf("%s is declared in %s; a package declares each tool and agent name once", name, strings.Join(where, " and "))
	case tool != nil || agent != nil:
		return tool, agent, nil
	}
	have := make([]string, 0, len(tools)+len(agents)+len(siblings))
	for _, t := range tools {
		have = append(have, t.Name)
	}
	for _, a := range agents {
		have = append(have, a.Name)
	}
	for _, s := range siblings {
		have = append(have, s.Name())
	}
	list := "they declare none"
	if len(have) > 0 {
		list = "have " + strings.Join(have, ", ")
	}
	return nil, nil, fmt.Errorf("no tool or agent %s in this file or the other .af files of its directory (%s)", name, list)
}

// SubagentCycle returns the chain by which agent name calls itself through
// subagents named in .tools, such as [a b a], or nil. agents are the
// agents of the package by name: a cycle could delegate without end.
func SubagentCycle(name string, agents map[string]*decl.Agent) []string {
	var path []string
	seen := map[string]bool{}
	var visit func(n string) bool
	visit = func(n string) bool {
		path = append(path, n)
		if len(path) > 1 && n == name {
			return true
		}
		if seen[n] {
			path = path[:len(path)-1]
			return false
		}
		seen[n] = true
		if a := agents[n]; a != nil {
			for _, t := range a.Tools {
				if agents[t] != nil && visit(t) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if visit(name) {
		return path
	}
	return nil
}

// PackageAgents returns the agents of a file and its siblings by name.
func PackageAgents(agents []*decl.Agent, siblings []SiblingTool) map[string]*decl.Agent {
	out := map[string]*decl.Agent{}
	for _, s := range siblings {
		if s.Agent != nil {
			out[s.Agent.Name] = s.Agent
		}
	}
	for _, a := range agents {
		out[a.Name] = a
	}
	return out
}
