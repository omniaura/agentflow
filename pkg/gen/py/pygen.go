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
package py

import (
	"bytes"
	"io"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/peyton-spencer/caseconv"
	"github.com/peyton-spencer/caseconv/bytcase"
)

func GenFile(w io.Writer, f ast.File) error {
	var buf bytes.Buffer
	if len(f.Prompts) == 0 {
		return gen.ErrNoPrompts
	}
	if len(f.Prompts) == 1 {
		p := f.Prompts[0]
		vars, length := p.Vars(f.Content, caseconv.CaseSnake)
		var title []byte
		if p.Title.Kind == token.KindTitle {
			title = bytcase.ToSnake(p.Title.Get(f.Content))
		} else {
			title = bytcase.ToSnake([]byte(f.Name))
		}
		functionHeader(&buf, title, vars, length)
		if len(vars) == 0 {
			stringLiteral(&buf, p.Nodes, f.Content)
		} else {
			stringTemplate(&buf, p.Nodes, f.Content)
		}

		_, err := buf.WriteTo(w)
		return err
	}
	for i, p := range f.Prompts {
		if p.Title.Kind == token.KindUnset {
			return gen.ErrMissingTitle.F("index: %d", i)
		}
		vars, length := p.Vars(f.Content, caseconv.CaseSnake)
		title := p.Title.Get(f.Content)
		functionHeader(&buf, title, vars, length)
		if len(vars) == 0 {
			stringLiteral(&buf, p.Nodes, f.Content)
		} else {
			stringTemplate(&buf, p.Nodes, f.Content)
		}
		if i < len(f.Prompts)-1 {
			buf.WriteString("\n\n")
		}
	}
	_, err := buf.WriteTo(w)
	return err
}

func functionHeader(buf *bytes.Buffer, title []byte, stringVars [][]byte, length int) {
	title = bytcase.ToSnake(title)
	buf.WriteString("def ")
	buf.Write(title)
	buf.WriteRune('(')
	if len(title)+length+10 > cfg.MaxLineLen {
		for i := range stringVars {
			if i == 0 {
				buf.WriteRune('\n')
			}
			buf.WriteRune('\t')
			buf.Write(stringVars[i])
			buf.WriteString(": str,\n")
		}
	} else {
		for i := range stringVars {
			buf.Write(stringVars[i])
			buf.WriteString(": str")
			if i < len(stringVars)-1 {
				buf.WriteString(", ")
			}
		}
	}
	buf.WriteString(") -> str:")
}

func stringTemplate(buf *bytes.Buffer, toks token.Slice, content []byte) {
	buf.WriteRune('\n')
	buf.WriteString(`	return f"""`)
	for _, t := range toks {
		if t.Kind == token.KindVar {
			buf.Write(t.GetWrap(content, '{', '}'))
		} else {
			buf.Write(t.Get(content))
		}
	}
	buf.WriteString(`"""`)
	buf.WriteRune('\n')
}

func stringLiteral(buf *bytes.Buffer, toks token.Slice, content []byte) {
	buf.WriteRune('\n')
	buf.WriteString(`	return """`)
	for _, t := range toks {
		if t.Kind == token.KindVar {
			buf.Write(t.GetWrap(content, '{', '}'))
		} else {
			buf.Write(t.Get(content))
		}
	}
	buf.WriteString(`"""`)
	buf.WriteRune('\n')
}
