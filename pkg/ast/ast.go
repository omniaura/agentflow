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
	"slices"
	"strings"

	"github.com/omniaura/agentflow/pkg/token"
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

func (p Prompt) Vars(content []byte, c caseconv.Case) [][]byte {
	vars := make([][]byte, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		if node.Kind == token.KindVar {
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
	}
	return vars
}

func (p1 Prompt) Equal(p2 Prompt) bool {
	return p1.Title == p2.Title && p1.Nodes.Equal(p2.Nodes)
}

func MustFile(name string, content []byte) File {
	f, err := NewFile(name, content)
	if err != nil {
		panic(err)
	}
	return f
}

func NewFile(name string, content []byte) (f File, err error) {
	tokens, err := token.Tokenize(content)
	if err != nil {
		return
	}
	if !strings.HasSuffix(name, ".af") {
		return File{}, fmt.Errorf("file does not have .af extension: %s", name)
	}
	f.Name = strings.TrimSuffix(name, ".af")
	f.Content = content
	f.Prompts, err = newPrompts(tokens)
	return
}

func newPrompts(tokens token.Slice) (prompts []Prompt, err error) {
	for _, t := range tokens {
		if t.Kind == token.KindTitle {
			prompts = append(prompts, Prompt{Title: t})
		} else if len(prompts) == 0 {
			prompts = append(prompts, Prompt{Nodes: token.Slice{t}})
		} else {
			prompts[len(prompts)-1].Nodes = append(prompts[len(prompts)-1].Nodes, t)
		}
	}
	return
}
