/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package ast

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
	"github.com/peyton-spencer/caseconv"
	"github.com/peyton-spencer/caseconv/bytcase"
)

type File struct {
	Name    string
	Content []byte
	Prompts []Prompt
}

func (f File) String() string {
	var sb strings.Builder
	sb.WriteString("File{\n")
	sb.WriteString(fmt.Sprintf("  Name: %s,\n", f.Name))
	sb.WriteString("  Prompts: [\n")
	for i, prompt := range f.Prompts {
		sb.WriteString("    ")
		sb.WriteString(prompt.Stringify(f.Content))
		if i < len(f.Prompts)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("  ]\n")
	sb.WriteRune('}')
	return sb.String()
}

func (f1 File) Equal(f2 File) bool {
	e := f1.Name == f2.Name && bytes.Equal(f1.Content, f2.Content)
	if !e {
		return false
	}
	if len(f1.Prompts) != len(f2.Prompts) {
		return false
	}
	for i, p1 := range f1.Prompts {
		if !p1.Equal(f2.Prompts[i]) {
			return false
		}
	}
	return true
}

type Prompt struct {
	// Title is the name of the prompt.
	Title token.T
	// Nodes are the nodes of the prompt.
	Nodes token.Slice
}

func (p Prompt) Stringify(content []byte) string {
	var buf strings.Builder
	buf.WriteString("Prompt{Title: ")
	buf.Write(content[p.Title.Start:p.Title.End])
	buf.WriteString(", Nodes: ")
	for i, node := range p.Nodes {
		buf.WriteString(node.Stringify(content))
		if i < len(p.Nodes)-1 {
			buf.WriteString(", ")
		}
	}
	buf.WriteRune('}')
	return buf.String()
}

func (p Prompt) Vars(content []byte, c caseconv.Case) (vars [][]byte, length int) {
	vars = make([][]byte, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		if node.Kind == kind.Var {
			name := node.Get(content)
			if slices.ContainsFunc(vars, func(b []byte) bool { return bytes.Equal(b, name) }) {
				continue
			}
			vars = append(vars, name)
		}
	}
	for i := range vars {
		switch c {
		case caseconv.CaseCamel:
			vars[i] = bytcase.ToLowerCamel(vars[i])
		case caseconv.CaseSnake:
			vars[i] = bytcase.ToSnake(vars[i])
		}
		length += len(vars[i])
	}
	return
}

type InputStruct struct {
	TopLevel []InputNode
}

func (i InputStruct) Equal(other InputStruct) bool {
	return slices.EqualFunc(i.TopLevel, other.TopLevel, InputNode.Equal)
}

func (i InputStruct) String() string {
	var buf strings.Builder
	buf.WriteString("type Input struct {\n")
	for _, n := range i.TopLevel {
		buf.WriteString("    ")
		buf.Write(n.Name)
		buf.WriteString(" ")
		buf.WriteString(n.String())
		buf.WriteString("\n")
	}
	buf.WriteString("}")
	return buf.String()
}

type InputNode struct {
	Name     []byte
	Type     string // New: type of the variable ("string", "int", "bool", etc.)
	Subnodes []InputNode
}

func (n InputNode) Equal(other InputNode) bool {
	return bytes.Equal(n.Name, other.Name) && n.Type == other.Type && slices.EqualFunc(n.Subnodes, other.Subnodes, InputNode.Equal)
}

func (n InputNode) String() string {
	if len(n.Subnodes) == 0 {
		if n.Type == "" {
			return "string"
		}
		return n.Type
	}

	var buf strings.Builder
	buf.WriteString("struct {\n")
	for _, sub := range n.Subnodes {
		buf.WriteString("        ")
		buf.Write(sub.Name)
		buf.WriteString(" ")
		buf.WriteString(sub.String())
		buf.WriteString("\n")
	}
	buf.WriteString("    }")
	return buf.String()
}

func (ii *InputStruct) insertVar(node token.T, content []byte, c caseconv.Case, typeCache map[string]string) {
	varInfo := node.GetVar(content, typeCache)
	if len(varInfo.Path) == 0 {
		return
	}

	// Insert all path parts and set their types based on the cache
	ii.insertVarWithCache(varInfo.Path, varInfo.Type, c, typeCache)
}

func (ii *InputStruct) insertVarWithCache(path [][]byte, typ string, c caseconv.Case, typeCache map[string]string) {
	if len(path) == 0 {
		return
	}

	nn := c.BytCase(path[0])
	idx := slices.IndexFunc(ii.TopLevel, func(n InputNode) bool {
		return bytes.Equal(n.Name, nn)
	})
	if idx == -1 {
		newNode := InputNode{Name: nn}
		ii.TopLevel = append(ii.TopLevel, newNode)
		idx = len(ii.TopLevel) - 1
	}

	if len(path) == 1 {
		// This is a leaf node, set its type from the cache
		pathKey := string(bytes.Join(path, []byte(".")))
		if cachedType, exists := typeCache[pathKey]; exists && cachedType != "" {
			ii.TopLevel[idx].Type = cachedType
		} else if ii.TopLevel[idx].Type == "" {
			ii.TopLevel[idx].Type = typ
		}
		return
	}

	// Check if this intermediate node should be a struct
	pathKey := string(bytes.Join(path[:1], []byte(".")))
	if cachedType, exists := typeCache[pathKey]; exists && cachedType == "struct" {
		ii.TopLevel[idx].Type = "struct"
	}

	// We're adding nested fields
	ii.TopLevel[idx].insertMultiLevelVarWithCache(path[1:], typ, c, typeCache, path[:1])
}

func (n *InputNode) insertMultiLevelVarWithCache(path [][]byte, typ string, c caseconv.Case, typeCache map[string]string, parentPath [][]byte) {
	if len(path) == 0 {
		slog.Debug("0 len name reached")
		return
	}
	nn := c.BytCase(path[0])
	idx := slices.IndexFunc(n.Subnodes, func(n InputNode) bool {
		return bytes.Equal(n.Name, nn)
	})
	if idx == -1 {
		newNode := InputNode{Name: nn}
		n.Subnodes = append(n.Subnodes, newNode)
		idx = len(n.Subnodes) - 1
	}

	// Build full path for cache lookup
	fullPath := make([][]byte, len(parentPath)+1)
	copy(fullPath, parentPath)
	fullPath[len(parentPath)] = path[0]

	if len(path) == 1 {
		// This is a leaf node, set its type from the cache or the provided type
		pathKey := string(bytes.Join(fullPath, []byte(".")))
		if cachedType, exists := typeCache[pathKey]; exists && cachedType != "" {
			n.Subnodes[idx].Type = cachedType
		} else if n.Subnodes[idx].Type == "" {
			n.Subnodes[idx].Type = typ
		}
		return
	}

	// Check if this intermediate node should be a struct
	pathKey := string(bytes.Join(fullPath, []byte(".")))
	if cachedType, exists := typeCache[pathKey]; exists && cachedType == "struct" {
		n.Subnodes[idx].Type = "struct"
	}

	// We're adding nested fields
	n.Subnodes[idx].insertMultiLevelVarWithCache(path[1:], typ, c, typeCache, fullPath)
}

func (p Prompt) GetInputs(content []byte, c caseconv.Case) (ii InputStruct, err error) {
	typeCache := make(map[string]string)
	allPaths := make(map[string]bool)

	// First pass: collect all variable paths to understand the complete structure
	for _, node := range p.Nodes {
		switch node.Kind {
		case kind.Var, kind.OptionalBlock:
			varInfo := node.GetVar(content, typeCache)
			if len(varInfo.Path) > 0 {
				pathKey := string(bytes.Join(varInfo.Path, []byte(".")))
				allPaths[pathKey] = true

				// Store explicit type information
				if varInfo.Type != "" && varInfo.Type != "string" {
					typeCache[pathKey] = varInfo.Type
				}
			}
		}
	}

	// Analyze paths to determine which should be structs
	inferStructTypes(allPaths, typeCache)

	// Second pass: build the struct with complete type information
	for _, node := range p.Nodes {
		switch node.Kind {
		case kind.Var, kind.OptionalBlock:
			ii.insertVar(node, content, c, typeCache)
		}
	}

	// Post-process: set type to "struct" for nodes that have subnodes
	for i := range ii.TopLevel {
		ii.TopLevel[i].setStructTypes()
	}

	return
}

// inferStructTypes analyzes all variable paths to determine which should be struct types
// A path should be a struct if there are other paths that are extensions of it
func inferStructTypes(allPaths map[string]bool, typeCache map[string]string) {
	for path := range allPaths {
		// Check if this path has any children (i.e., other paths that start with this path + ".")
		hasChildren := false
		pathPrefix := path + "."

		for otherPath := range allPaths {
			if otherPath != path && strings.HasPrefix(otherPath, pathPrefix) {
				hasChildren = true
				break
			}
		}

		// If this path has children and doesn't already have a non-struct type, mark it as struct
		if hasChildren {
			if existingType, exists := typeCache[path]; !exists || existingType == "" || existingType == "string" {
				typeCache[path] = "struct"
			}
		}
	}
}

// setStructTypes recursively sets the type to "struct" for nodes that have subnodes
func (n *InputNode) setStructTypes() {
	if len(n.Subnodes) > 0 {
		n.Type = "struct"
	}
	for i := range n.Subnodes {
		n.Subnodes[i].setStructTypes()
	}
}

func (p1 Prompt) Equal(p2 Prompt) bool {
	return p1.Title == p2.Title && p1.Nodes.Equal(p2.Nodes)
}

func NewFile(name string, content []byte) (f File, err error) {
	if !strings.HasSuffix(name, ".af") {
		err = fmt.Errorf("file does not have .af extension: %s", name)
		return
	}
	tokens, err := token.Tokenize(content)
	if err != nil {
		return
	}
	f.Name = strings.TrimSuffix(name, ".af")
	f.Content = content
	f.Prompts, err = newPrompts(tokens)
	return
}

func newPrompts(tokens token.Slice) (prompts []Prompt, err error) {
	for _, t := range tokens {
		if t.Kind == kind.Title {
			prompts = append(prompts, Prompt{Title: t})
		} else if len(prompts) == 0 {
			prompts = append(prompts, Prompt{Nodes: token.Slice{t}})
		} else {
			prompts[len(prompts)-1].Nodes = append(prompts[len(prompts)-1].Nodes, t)
		}
	}
	return
}
