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
package gogen

import (
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/peyton-spencer/caseconv"
	"github.com/peyton-spencer/caseconv/bytcase"
)

var replacerPackageName = strings.NewReplacer(" ", "", "-", "", "_", "")

func GenFile(w io.Writer, f ast.File) error {
	var buf bytes.Buffer
	buf.WriteString("package ")
	buf.WriteString(strings.ToLower(replacerPackageName.Replace(f.Name)))
	buf.WriteString("\n\n")
	hasVars := false
	for _, p := range f.Prompts {
		_, ll := p.Vars(f.Content, caseconv.CaseCamel)
		if ll > 0 {
			hasVars = true
			break
		}
	}
	if hasVars {
		buf.WriteString("import \"strings\"\n\n")
	}

	if len(f.Prompts) == 0 {
		return gen.ErrNoPrompts
	}
	if len(f.Prompts) == 1 {
		p := f.Prompts[0]
		vars, length := p.Vars(f.Content, caseconv.CaseCamel)
		var title []byte
		if p.Title.Kind == token.KindTitle {
			title = bytcase.ToCamel(p.Title.Get(f.Content))
		} else {
			title = bytcase.ToCamel([]byte(f.Name))
		}
		functionHeader(&buf, title, vars, length)
		stringTemplate(&buf, p.Nodes, f.Content)
		_, err := buf.WriteTo(w)
		return err
	}
	for i, p := range f.Prompts {
		if p.Title.Kind == token.KindUnset {
			return gen.ErrMissingTitle.F("index: %d", i)
		}
		vars, length := p.Vars(f.Content, caseconv.CaseCamel)
		title := p.Title.Get(f.Content)
		title = bytcase.ToCamel(title)
		functionHeader(&buf, title, vars, length)
		stringTemplate(&buf, p.Nodes, f.Content)
		if i < len(f.Prompts)-1 {
			buf.WriteRune('\n')
		}
	}
	_, err := buf.WriteTo(w)
	return err
}

func functionHeader(buf *bytes.Buffer, title []byte, stringVars [][]byte, length int) {
	buf.WriteString("func ")
	buf.Write(title)
	buf.WriteRune('(')
	if len(title)+length+19 > cfg.MaxLineLen {
		for i := range stringVars {
			if i == 0 {
				buf.WriteRune('\n')
			}
			buf.WriteRune('\t')
			buf.Write(stringVars[i])
			buf.WriteString(" string,\n")
		}
	} else {
		for i := range stringVars {
			buf.Write(stringVars[i])
			buf.WriteString(" string")
			if i < len(stringVars)-1 {
				buf.WriteString(", ")
			}
		}
	}
	buf.WriteString(") string {\n")
}

func stringTemplate(buf *bytes.Buffer, toks token.Slice, content []byte) {
	// Check if there are any variables
	hasVars := false
	for _, t := range toks {
		if t.Kind == token.KindVar {
			hasVars = true
			break
		}
	}

	if !hasVars {
		// For no variables, return the string literal directly
		buf.WriteString("\treturn `")
		for _, t := range toks {
			buf.Write(t.Get(content))
		}
		buf.WriteString("`\n}\n")
		return
	}

	// For strings with variables, use strings.Builder
	buf.WriteString("\tvar b strings.Builder\n")

	// Calculate total length for Grow
	buf.WriteString("\tb.Grow(")
	textLen := 0
	hasWrittenLen := false
	for _, t := range toks {
		if t.Kind == token.KindVar {
			if hasWrittenLen && textLen > 0 {
				buf.WriteString(" + ")
			}
			if textLen > 0 {
				buf.WriteString(strconv.Itoa(textLen))
				buf.WriteString(" + ")
				textLen = 0
			}
			buf.WriteString("len(")
			varName := bytcase.ToLowerCamel(t.Get(content))
			buf.Write(varName)
			buf.WriteString(")")
			hasWrittenLen = true
		} else {
			textLen += len(t.Get(content))
		}
	}
	if textLen > 0 {
		if hasWrittenLen {
			buf.WriteString(" + ")
		}
		buf.WriteString(strconv.Itoa(textLen))
	}
	buf.WriteString(")\n")

	// Write the string parts
	for _, t := range toks {
		if t.Kind == token.KindVar {
			buf.WriteString("\tb.WriteString(")
			varName := bytcase.ToLowerCamel(t.Get(content))
			buf.Write(varName)
			buf.WriteString(")\n")
		} else {
			content := t.Get(content)
			if len(content) > 0 { // Only write non-empty strings
				buf.WriteString("\tb.WriteString(`")
				buf.Write(content)
				buf.WriteString("`)\n")
			}
		}
	}

	buf.WriteString("\treturn b.String()\n}\n")
}
