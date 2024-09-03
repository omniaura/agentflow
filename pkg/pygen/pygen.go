package pygen

import (
	"fmt"
	"io"

	"github.com/ditto-assistant/agentflow/pkg/varmatcher"
	"github.com/iancoleman/strcase"
)

func FunctionHeader(w io.Writer, name string, stringVars []string) {
	name = strcase.ToSnake(name)
	fmt.Fprintln(w, "def "+name+"(")
	for i := range stringVars {
		fmt.Fprint(w, "  "+stringVars[i]+": str")
		if i < len(stringVars)-1 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, ") -> str:")
}

func ReturnStringTemplate(w io.Writer, lines []string) {
	fmt.Fprint(w, `    return f"""`)
	for _, line := range lines {
		line = varmatcher.Regex.ReplaceAllString(line, "{${1}}")
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, `"""`)
}

func ReturnStringLiteral(w io.Writer, lines []string) {
	fmt.Fprint(w, `    return """`)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, `"""`)
}
