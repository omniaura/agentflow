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
package format

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
)

var elsePattern = regexp.MustCompile(`<\s*else\s*>`)

func Format(name string, src []byte) ([]byte, error) {
	normalized := normalizeLineEndings(src)
	tokens, err := token.Tokenize(normalized)
	if err != nil {
		return nil, err
	}

	directives := elsePattern.ReplaceAll(normalizeDirectives(tokens, normalized), []byte("<else>"))
	lines := normalizeStructuralLines(string(directives))
	return []byte(lines), nil
}

func normalizeLineEndings(src []byte) []byte {
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
}

func normalizeDirectives(tokens token.Slice, src []byte) []byte {
	var out strings.Builder
	out.Grow(len(src) + 1)

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch tok.Kind {
		case kind.TitleDirective:
			i = writeTitle(&out, tokens, src, i)
		case kind.OpenBracket:
			if next, ok := peek(tokens, i+1); ok {
				switch next.Kind {
				case kind.DirectiveVar:
					i = writeTag(&out, tokens, src, i, "<!", ">", true)
				case kind.DirectiveCond:
					i = writeTag(&out, tokens, src, i, "<?", ">", true)
				case kind.DirectiveEnd:
					i = writeTag(&out, tokens, src, i, "</", ">", false)
				case kind.DirectiveElse:
					out.WriteString("<else>")
					i = skipToClose(tokens, i)
				default:
					out.Write(tok.Get(src))
				}
			} else {
				out.Write(tok.Get(src))
			}
		default:
			out.Write(tok.Get(src))
		}
	}

	return []byte(out.String())
}

func writeTitle(out *strings.Builder, tokens token.Slice, src []byte, start int) int {
	out.WriteString(".title")

	i := start + 1
	if i < len(tokens) && tokens[i].Kind == kind.Whitespace && !strings.Contains(string(tokens[i].Get(src)), "\n") {
		i++
	}
	if i < len(tokens) && tokens[i].Kind == kind.TitleText {
		title := strings.TrimSpace(string(tokens[i].Get(src)))
		if title != "" {
			out.WriteByte(' ')
			out.WriteString(title)
		}
		i++
	}
	if i < len(tokens) && tokens[i].Kind == kind.Whitespace && string(tokens[i].Get(src)) == "\n" {
		out.WriteByte('\n')
		return i
	}

	return i - 1
}

func writeTag(out *strings.Builder, tokens token.Slice, src []byte, start int, prefix, suffix string, keepSpaces bool) int {
	end := skipToClose(tokens, start)
	if end <= start+1 || end >= len(tokens) {
		out.Write(tokens[start].Get(src))
		return start
	}

	contentStart := tokens[start+2].Start
	contentEnd := tokens[end].Start
	if contentEnd < contentStart {
		contentEnd = contentStart
	}
	parts := strings.Fields(string(src[contentStart:contentEnd]))

	out.WriteString(prefix)
	if keepSpaces {
		out.WriteString(strings.Join(parts, " "))
	} else if len(parts) > 0 {
		out.WriteString(parts[0])
	}
	out.WriteString(suffix)
	return end
}

func skipToClose(tokens token.Slice, start int) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Kind == kind.CloseBracket {
			return i
		}
	}
	return start
}

func peek(tokens token.Slice, index int) (token.T, bool) {
	if index < 0 || index >= len(tokens) {
		return token.T{}, false
	}
	return tokens[index], true
}

func normalizeStructuralLines(src string) string {
	lines := strings.Split(src, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	out := make([]string, 0, len(lines)+4)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if isTitleLine(line) {
			trimTrailingBlankLines(&out)
			if hasNonEmptyLine(out) {
				out = append(out, "")
			}

			out = append(out, line)
			for i+1 < len(lines) && lines[i+1] == "" {
				i++
			}
			if i+1 < len(lines) && !isTitleLine(lines[i+1]) {
				out = append(out, "")
			}
			continue
		}

		out = append(out, line)
	}

	trimTrailingBlankLines(&out)
	return strings.Join(out, "\n") + "\n"
}

func isTitleLine(line string) bool {
	return line == ".title" || strings.HasPrefix(line, ".title ")
}

func trimTrailingBlankLines(lines *[]string) {
	for len(*lines) > 0 && (*lines)[len(*lines)-1] == "" {
		*lines = (*lines)[:len(*lines)-1]
	}
}

func hasNonEmptyLine(lines []string) bool {
	for _, line := range lines {
		if line != "" {
			return true
		}
	}
	return false
}
