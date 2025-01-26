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

func (i1 InputStruct) Equal(i2 InputStruct) bool {
	if len(i1.TopLevel) != len(i2.TopLevel) {
		return false
	}
	for i := range i1.TopLevel {
		if !i1.TopLevel[i].Equal(i2.TopLevel[i]) {
			return false
		}
	}
	return true
}

func (i1 InputNode) Equal(i2 InputNode) bool {
	if !bytes.Equal(i1.Name, i2.Name) {
		return false
	}
	if len(i1.Subnodes) != len(i2.Subnodes) {
		return false
	}
	for i := range i1.Subnodes {
		if !i1.Subnodes[i].Equal(i2.Subnodes[i]) {
			return false
		}
	}
	return true
}

func (ii InputStruct) String() string {
	var buf strings.Builder
	buf.WriteString("type Input struct {\n")
	for _, n := range ii.TopLevel {
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
	Subnodes []InputNode
}

func (n InputNode) String() string {
	var buf strings.Builder
	if len(n.Subnodes) == 0 {
		buf.WriteString("string")
		return buf.String()
	}

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

func (ii *InputStruct) insertVar(node token.T, content []byte, c caseconv.Case) {
	name := bytes.Split(node.Get(content), []byte{'.'})
	if len(name) == 1 {
		nn := c.BytCase(name[0])
		ii.TopLevel = append(ii.TopLevel, InputNode{
			Name: nn,
		})
		return
	}
	nn := c.BytCase(name[0])
	idx := slices.IndexFunc(ii.TopLevel, func(n InputNode) bool {
		return bytes.Equal(n.Name, nn)
	})
	if idx == -1 {
		ii.TopLevel = append(ii.TopLevel, InputNode{
			Name: nn,
		})
		idx = len(ii.TopLevel) - 1
	}
	ii.TopLevel[idx].insertMultiLevelVar(name[1:], c)
}

func (n *InputNode) insertMultiLevelVar(name [][]byte, c caseconv.Case) {
	if len(name) == 0 {
		slog.Debug("0 len name reached")
		return
	}
	nn := c.BytCase(name[0])
	idx := slices.IndexFunc(n.Subnodes, func(n InputNode) bool {
		return bytes.Equal(n.Name, nn)
	})
	if idx == -1 {
		n.Subnodes = append(n.Subnodes, InputNode{
			Name: nn,
		})
		idx = len(n.Subnodes) - 1
	}
	if len(name) == 1 {
		return
	}
	n.Subnodes[idx].insertMultiLevelVar(name[1:], c)
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
