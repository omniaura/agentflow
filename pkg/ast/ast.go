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

// Helper to parse variable path and type from a tag like "user.subscription.tier int"
func parseVarAndType(tag []byte) (path [][]byte, typ string) {
	parts := bytes.Fields(tag)
	if len(parts) == 0 {
		return nil, ""
	}
	path = bytes.Split(parts[0], []byte{'.'})
	if len(parts) > 1 {
		typ = string(parts[1])
	}
	return
}

func (ii *InputStruct) insertVar(node token.T, content []byte, c caseconv.Case) {
	path, typ := parseVarAndType(node.Get(content))
	if len(path) == 0 {
		return
	}
	nn := c.BytCase(path[0])
	idx := slices.IndexFunc(ii.TopLevel, func(n InputNode) bool {
		return bytes.Equal(n.Name, nn)
	})
	if idx == -1 {
		newNode := InputNode{Name: nn}
		if len(path) == 1 {
			newNode.Type = typ
		}
		ii.TopLevel = append(ii.TopLevel, newNode)
		idx = len(ii.TopLevel) - 1
	}
	if len(path) == 1 {
		// Set type if not struct
		ii.TopLevel[idx].Type = typ
		// If not struct, clear subnodes
		if typ != "struct" {
			ii.TopLevel[idx].Subnodes = nil
		}
		return
	}
	ii.TopLevel[idx].insertMultiLevelVar(path[1:], typ, c)
}

func (n *InputNode) insertMultiLevelVar(path [][]byte, typ string, c caseconv.Case) {
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
		if len(path) == 1 {
			newNode.Type = typ
		}
		n.Subnodes = append(n.Subnodes, newNode)
		idx = len(n.Subnodes) - 1
	}
	if len(path) == 1 {
		n.Subnodes[idx].Type = typ
		if typ != "struct" {
			n.Subnodes[idx].Subnodes = nil
		}
		return
	}
	n.Subnodes[idx].insertMultiLevelVar(path[1:], typ, c)
}

func (p Prompt) GetInputs(content []byte, c caseconv.Case) (ii InputStruct, err error) {
	for _, node := range p.Nodes {
		switch node.Kind {
		case kind.Var, kind.OptionalBlock:
			ii.insertVar(node, content, c)
		}
	}
	return
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
