package js

import (
	"bytes"
	"io"

	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen"
	"github.com/ditto-assistant/agentflow/pkg/token"
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
		vars := p.Vars(f.Content, caseconv.CaseCamel)
		var title []byte
		if p.Title.Kind == token.KindTitle {
			title = bytcase.ToLowerCamel(p.Title.Get(f.Content))
		} else {
			title = bytcase.ToLowerCamel([]byte(f.Name))
		}
		jsDoc(&buf, vars)
		functionHeader(&buf, title, vars)
		stringTemplate(&buf, p.Nodes, f.Content)
		_, err := buf.WriteTo(w)
		return err
	}
	for i, p := range f.Prompts {
		if p.Title.Kind == token.KindUnset {
			return gen.ErrMissingTitle.F("index: %d", i)
		}
		vars := p.Vars(f.Content, caseconv.CaseCamel)
		title := p.Title.Get(f.Content)
		title = bytcase.ToLowerCamel(title)
		jsDoc(&buf, vars)
		functionHeader(&buf, title, vars)
		stringTemplate(&buf, p.Nodes, f.Content)
		if i < len(f.Prompts)-1 {
			buf.WriteRune('\n')
		}
	}
	_, err := buf.WriteTo(w)
	return err
}

func jsDoc(buf *bytes.Buffer, stringVars [][]byte) {
	buf.WriteString("/**\n")
	for _, varName := range stringVars {
		buf.WriteString(" * @param {string} ")
		buf.Write(varName)
		buf.WriteRune('\n')
	}
	buf.WriteString(" * @returns {string}\n")
	buf.WriteString(" */\n")
}

func functionHeader(buf *bytes.Buffer, title []byte, stringVars [][]byte) {
	buf.WriteString("export function ")
	buf.Write(title)
	buf.WriteString("(")
	for i := range stringVars {
		buf.Write(stringVars[i])
		if i < len(stringVars)-1 {
			buf.WriteString(", ")
		}
	}
	buf.WriteString(") {\n")
}

func stringTemplate(buf *bytes.Buffer, toks token.Slice, content []byte) {
	buf.WriteString("	return `")
	for _, t := range toks {
		if t.Kind == token.KindVar {
			buf.Write(t.GetJSFmtVar(content))
		} else {
			buf.Write(t.Get(content))
		}
	}
	buf.WriteString("`;\n}\n")
}

var leftW = []byte("${")
