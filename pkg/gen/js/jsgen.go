package js

import (
	"bytes"
	"io"

	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen"
	"github.com/ditto-assistant/agentflow/pkg/token"
	"github.com/peyton-spencer/caseconv/bytcase"
)

func GenFile(w io.Writer, f ast.File) error {
	var buf bytes.Buffer
	if len(f.Prompts) == 0 {
		return gen.ErrNoPrompts
	}
	if len(f.Prompts) == 1 {
		p := f.Prompts[0]
		vars := p.Vars(f.Content)
		var title []byte
		if p.Title.Kind == token.KindTitle {
			title = bytcase.ToCamel(p.Title.Get(f.Content))
		} else {
			title = bytcase.ToCamel([]byte(f.Name))
		}
		functionHeader(&buf, title, vars)
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
		vars := p.Vars(f.Content)
		title := p.Title.Get(f.Content)
		functionHeader(&buf, title, vars)
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

func jsDoc(buf *bytes.Buffer, title []byte, stringVars [][]byte) {
	buf.WriteString("/*")
}

func functionHeader(buf *bytes.Buffer, title []byte, stringVars [][]byte) {
	title = bytcase.ToSnake(title)
	buf.WriteString("def ")
	buf.Write(title)
	buf.WriteString("(")
	for i := range stringVars {
		buf.Write(stringVars[i])
		if i < len(stringVars)-1 {
			buf.WriteString(",")
		}
		buf.WriteString(": str")
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
}
