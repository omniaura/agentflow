package ts

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/iancoleman/strcase"
)

func FunctionHeader(w io.Writer, name string, stringVars []string) {
	name = strcase.ToCamel(name)
	fmt.Fprintln(w, "export function "+name+"(")
	for i := range stringVars {
		fmt.Fprint(w, "  "+stringVars[i]+": string")
		fmt.Fprintln(w, ",")
	}
	fmt.Fprintln(w, "): string {")
}

func ReturnStringTemplate(w io.Writer, lines []string) {
	fmt.Fprint(w, "  return `")
	for _, line := range lines {
		// line = varmatcher.Regex.ReplaceAllString(line, "${$1}")
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, "`;")
}

func ReturnStringLiteral(w io.Writer, lines []string) {
	fmt.Fprint(w, "  return `")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, "`;")
}

func IndexFile(categories []string) error {
	indexFile, err := os.Create("ts/index.ts")
	if err != nil {
		return err
	}
	defer indexFile.Close()
	for _, cat := range categories {
		if len(cat) < 2 {
			return fmt.Errorf("category name too short: %s", cat)
		}
		catName := strings.ToUpper(cat[:1]) + cat[1:]
		fmt.Fprintln(indexFile, "export * as "+catName+" from './"+cat+".ts';")
	}
	return nil
}
