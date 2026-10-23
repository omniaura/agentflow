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
package lsp

import (
	"bytes"
	"log/slog"
	"slices"
	"time"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
	"github.com/tliron/glsp/protocol_3_16"
)

// generateGranularSemanticTokens creates semantic tokens using the new granular token system
func generateGranularSemanticTokens(doc *Document) []protocol.UInteger {
	start := time.Now()

	slog.Debug("Generating semantic tokens with granular tokens",
		"uri", doc.URI,
		"tokenCount", len(doc.Tokens))

	// Token type indices (must match the order in server.go capabilities)
	tokenTypeMap := map[string]protocol.UInteger{
		TokenTypeKeyword:   0,  // .title, !, ?, /, else
		TokenTypeVariable:  1,  // variable names
		TokenTypeString:    2,  // string values, title text
		TokenTypeComment:   3,  // comments (unused)
		TokenTypeOperator:  4,  // eq, gte, lte operators
		TokenTypeType:      5,  // type annotations
		TokenTypeParameter: 6,  // int/bool values
		TokenTypeDecorator: 7,  // .title directive
		TokenTypeFunction:  8,  // conditional blocks (unused)
		TokenTypeProperty:  9,  // variable path segments (unused)
		TokenTypeTagOpen:   10, // < and >
		TokenTypeTagClose:  11, // (same as TagOpen)
	}

	var semanticTokens []protocol.UInteger
	var lastLine protocol.UInteger = 0
	var lastChar protocol.UInteger = 0

	// Helper function to add a semantic token
	addToken := func(line, char, length protocol.UInteger, tokenType string) {
		// Calculate deltas
		deltaLine := line - lastLine
		var deltaChar protocol.UInteger
		if deltaLine == 0 {
			deltaChar = char - lastChar
		} else {
			deltaChar = char
		}

		// Get token type index
		tokenTypeIndex, exists := tokenTypeMap[tokenType]
		if !exists {
			tokenTypeIndex = tokenTypeMap[TokenTypeString] // fallback
		}

		semanticTokens = append(semanticTokens,
			deltaLine,      // deltaLine
			deltaChar,      // deltaStart
			length,         // length
			tokenTypeIndex, // tokenType
			0,              // tokenModifiers (no modifiers needed)
		)

		lastLine = line
		lastChar = char

		slog.Debug("Added semantic token",
			"uri", doc.URI,
			"line", line,
			"char", char,
			"length", length,
			"tokenType", tokenType,
			"tokenTypeIndex", tokenTypeIndex)
	}

	// Process granular tokens directly
	for _, tok := range doc.Tokens {
		position := doc.byteOffsetToPosition(tok.Start)
		line := protocol.UInteger(position.Line)
		char := protocol.UInteger(position.Character)
		length := protocol.UInteger(tok.End - tok.Start)

		var tokenType string
		switch tok.Kind {
		case kind.TitleDirective:
			tokenType = TokenTypeKeyword // Same color as 'else' and other directives
		case kind.TitleText:
			tokenType = TokenTypeFunction // Title text should be colored like function names
		case kind.OpenBracket:
			tokenType = TokenTypeTagOpen
		case kind.CloseBracket:
			tokenType = TokenTypeTagClose // Properly distinguish open from close brackets
		case kind.DirectiveVar, kind.DirectiveCond, kind.DirectiveEnd, kind.DirectiveElse, kind.DirectiveLoop:
			tokenType = TokenTypeKeyword
		case kind.VarName:
			tokenType = TokenTypeVariable
		case kind.TypeName:
			tokenType = TokenTypeType
		case kind.Operator:
			tokenType = TokenTypeOperator
		case kind.StringValue:
			tokenType = TokenTypeParameter // String values in conditionals should be colored like parameters
		case kind.IntValue, kind.BoolValue:
			tokenType = TokenTypeParameter
		case kind.Text:
			tokenType = TokenTypeString // Regular text content
		case kind.Section:
			for _, st := range sectionTokens(doc.AST.Content, tok) {
				pos := doc.byteOffsetToPosition(st.start)
				addToken(protocol.UInteger(pos.Line), protocol.UInteger(pos.Character), protocol.UInteger(st.end-st.start), st.typ)
			}
			continue
		case kind.Whitespace:
			// Skip whitespace tokens - they don't need semantic highlighting
			continue
		default:
			// Skip unrecognized tokens
			continue
		}

		addToken(line, char, length, tokenType)
	}

	duration := time.Since(start)
	slog.Debug("Generated semantic tokens",
		"uri", doc.URI,
		"tokenCount", len(semanticTokens)/5,
		"rawDataLength", len(semanticTokens),
		"duration", duration)

	return semanticTokens
}

type sectionToken struct {
	start, end int
	typ        string
}

// sectionTokens splits a declaration section such as .tool into one-line
// semantic tokens: directives as keywords, the tool name as a function,
// parameter names as properties, their types as types, and descriptions as
// strings.
func sectionTokens(src []byte, tok token.T) []sectionToken {
	var out []sectionToken
	if tok.End > len(src) {
		return nil
	}
	for i := tok.Start; i < tok.End; {
		end := bytes.IndexByte(src[i:tok.End], '\n')
		if end < 0 {
			end = tok.End
		} else {
			end += i
		}
		line := src[i:end]
		fields := lineFields(line, i, 3)
		switch {
		case len(fields) > 0 && slices.Contains(decl.SectionDirectives, string(fields[0].text(src))):
			out = append(out, sectionToken{fields[0].start, fields[0].end, TokenTypeKeyword})
			if len(fields) > 1 {
				out = append(out, sectionToken{fields[1].start, end, TokenTypeFunction})
			}
		case len(fields) > 0 && string(fields[0].text(src)) == ".param":
			out = append(out, sectionToken{fields[0].start, fields[0].end, TokenTypeKeyword})
			if len(fields) > 1 {
				out = append(out, sectionToken{fields[1].start, fields[1].end, TokenTypeProperty})
			}
			if len(fields) > 2 {
				typEnd := fields[2].end
				if close := bytes.IndexByte(src[fields[2].start:end], ')'); bytes.Contains(fields[2].text(src), []byte("(")) && close >= 0 {
					typEnd = fields[2].start + close + 1
					if typEnd < end && src[typEnd] == '?' {
						typEnd++
					}
				}
				out = append(out, sectionToken{fields[2].start, typEnd, TokenTypeType})
				if desc := bytes.TrimLeft(src[typEnd:end], " \t"); len(desc) > 0 {
					out = append(out, sectionToken{end - len(desc), end, TokenTypeString})
				}
			}
		case len(fields) > 0 && slices.Contains(decl.ToolDirectives, string(fields[0].text(src))):
			out = append(out, sectionToken{fields[0].start, fields[0].end, TokenTypeKeyword})
			if len(fields) > 1 {
				out = append(out, sectionToken{fields[1].start, end, TokenTypeParameter})
			}
		case len(bytes.TrimSpace(line)) > 0:
			out = append(out, sectionToken{i, end, TokenTypeString})
		}
		i = end + 1
	}
	return out
}

type field struct{ start, end int }

func (f field) text(src []byte) []byte { return src[f.start:f.end] }

// lineFields returns the first n space-separated fields of line, which starts
// at offset off in the source.
func lineFields(line []byte, off, n int) []field {
	var out []field
	for i := 0; i < len(line) && len(out) < n; {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' && line[j] != '\r' {
			j++
		}
		if j > i {
			out = append(out, field{off + i, off + j})
		}
		i = j
	}
	return out
}
