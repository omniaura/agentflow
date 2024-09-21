package py

import (
	"bytes"
	"errors"
	"io"

	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/token"
	"github.com/peyton-spencer/caseconv/bytcase"
)

func GenFile(w io.Writer, f ast.File) error {
	var buf bytes.Buffer
	if len(f.Prompts) == 0 {
		return errors.New("file has 0 prompts")
	}
	if len(f.Prompts) == 1 {
		p := f.Prompts[0]
		vars := p.Vars(f.Content)
		var name []byte
		if p.Title.Kind == token.KindTitle {
			name = bytcase.ToSnake(p.Title.Get(f.Content))
		} else {
			name = bytcase.ToSnake([]byte(f.Name))
		}
		FunctionHeader(&buf, name, vars)
		if len(vars) == 0 {
			ReturnStringLiteral(&buf, p.Nodes, f.Content)
		} else {
			ReturnStringTemplate(&buf, p.Nodes, f.Content)
		}

		_, err := buf.WriteTo(w)
		return err
	}
	// for _, prompt := range f.Prompts {
	// w.Write([]byte(prompt.Stringify(f.Content)))
	// }
	_, err := buf.WriteTo(w)
	return err
}

func FunctionHeader(buf *bytes.Buffer, name []byte, stringVars [][]byte) {
	name = bytcase.ToSnake(name)
	buf.WriteString("def ")
	buf.Write(name)
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

func ReturnStringTemplate(buf *bytes.Buffer, toks token.Slice, content []byte) {
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

func ReturnStringLiteral(buf *bytes.Buffer, toks token.Slice, content []byte) {
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
