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
package coarse

import (
	"bytes"
	"fmt"

	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
)

// Kind represents coarse-grained token types for AST compatibility
type Kind int

const (
	Unset Kind = iota
	Title
	Var
	OptionalBlock
	ElseBlock
	EndTag
	Text
	LoopBlock
)

// Token represents a coarse-grained token for AST and code generation
type Token struct {
	Kind  Kind
	Start int
	End   int
}

func (t Token) Get(in []byte) []byte {
	return in[t.Start:t.End]
}

func (t Token) Stringify(in []byte) string {
	return fmt.Sprintf("%v: [%d:%d] %q", t.Kind, t.Start, t.End, t.Get(in))
}

func (t Token) GetVar(in []byte, typeCache map[string]string) token.VarInfo {
	b := in[t.Start:t.End]

	// Handle conditional expressions for OptionalBlock tokens
	if t.Kind == OptionalBlock {
		return token.ParseConditionalExpression(b, typeCache)
	}

	// Handle regular variable tokens
	parts := bytes.Fields(b)
	var path [][]byte
	var typ string
	if len(parts) > 0 {
		path = bytes.Split(parts[0], []byte{'.'})
	}

	// Create cache key from the variable path
	pathKey := string(bytes.Join(path, []byte(".")))

	if len(parts) > 1 {
		// Type is explicitly specified, cache it
		typ = string(parts[1])
		typeCache[pathKey] = typ
	} else {
		// No type specified, check cache
		if cachedType, exists := typeCache[pathKey]; exists {
			typ = cachedType
		} else {
			typ = "string" // default
		}
	}
	return token.VarInfo{Path: path, Type: typ}
}

// Convert converts fine-grained tokens to coarse-grained tokens for AST compatibility
func Convert(tokens token.Slice, input []byte) []Token {
	var coarse []Token
	i := 0
	// bodyStart is where the current prompt body's text begins, past any
	// newlines that follow its title.
	bodyStart := 0

	for i < len(tokens) {
		switch tokens[i].Kind {
		case kind.TitleDirective:
			// Find the extent of the title text (not including the directive)
			var titleStart, titleEnd int
			i++ // Skip the directive token

			// Skip optional whitespace
			if i < len(tokens) && tokens[i].Kind == kind.Whitespace {
				i++
			}

			// Include title text if present
			if i < len(tokens) && tokens[i].Kind == kind.TitleText {
				titleStart = tokens[i].Start
				titleEnd = tokens[i].End
				i++
			}

			// The body starts after the newlines that follow the title line,
			// including blank lines. Indentation on its first line is kept.
			bodyStart = tokens[i-1].End
			for bodyStart < len(input) && input[bodyStart] == '\n' {
				bodyStart++
			}
			for i < len(tokens) && tokens[i].Kind == kind.Whitespace && tokens[i].End <= bodyStart {
				i++
			}

			// Only add title token if we found title text
			if titleEnd > titleStart {
				coarse = append(coarse, Token{
					Kind:  Title,
					Start: titleStart,
					End:   titleEnd,
				})
			}

		case kind.OpenBracket:
			// Group bracket sequences into logical units
			if i+1 < len(tokens) {
				switch tokens[i+1].Kind {
				case kind.DirectiveLoop:
					if tok, ok := groupVariableTokens(tokens, i); ok {
						tok.Kind = LoopBlock
						end := skipToClosingBracket(tokens, i)
						tok.End = tokens[end].Start
						coarse = append(coarse, tok)
						i = end + 1
					} else {
						end := skipToClosingBracket(tokens, i)
						coarse = append(coarse, Token{Kind: Text, Start: tokens[i].Start, End: tokens[end].End})
						i = end + 1
					}
				case kind.DirectiveVar:
					// Variable: <! ... >
					if tok, ok := groupVariableTokens(tokens, i); ok {
						coarse = append(coarse, tok)
						i = skipToClosingBracket(tokens, i) + 1
					} else {
						coarse = append(coarse, Token{
							Kind:  Text,
							Start: tokens[i].Start,
							End:   tokens[i].End,
						})
						i++
					}

				case kind.DirectiveCond:
					// Conditional: <? ... >
					if tok, ok := groupConditionalTokens(tokens, i); ok {
						coarse = append(coarse, tok)
						i = skipToClosingBracket(tokens, i) + 1
					} else {
						coarse = append(coarse, Token{
							Kind:  Text,
							Start: tokens[i].Start,
							End:   tokens[i].End,
						})
						i++
					}

				case kind.DirectiveEnd:
					// End tag: </ ... >
					if tok, ok := groupEndTagTokens(tokens, i); ok {
						coarse = append(coarse, tok)
						i = skipToClosingBracket(tokens, i) + 1
					} else {
						coarse = append(coarse, Token{
							Kind:  Text,
							Start: tokens[i].Start,
							End:   tokens[i].End,
						})
						i++
					}

				case kind.DirectiveElse:
					// Else: <else>
					end := skipToClosingBracket(tokens, i)
					coarse = append(coarse, Token{
						Kind:  ElseBlock,
						Start: tokens[i].Start,
						End:   tokens[end].End,
					})
					i = end + 1

				default:
					// Not a recognized directive, treat as text
					coarse = append(coarse, Token{
						Kind:  Text,
						Start: tokens[i].Start,
						End:   tokens[i].End,
					})
					i++
				}
			} else {
				// Standalone <, treat as text
				coarse = append(coarse, Token{
					Kind:  Text,
					Start: tokens[i].Start,
					End:   tokens[i].End,
				})
				i++
			}

		case kind.Text, kind.Whitespace:
			// Skip whitespace that precedes a title directive (inter-prompt whitespace)
			if tokens[i].Kind == kind.Whitespace && i+1 < len(tokens) && tokens[i+1].Kind == kind.TitleDirective {
				i++ // Skip this whitespace token
				continue
			}

			// Group consecutive text/whitespace tokens
			start := max(tokens[i].Start, bodyStart)
			end := tokens[i].End
			i++

			for i < len(tokens) && (tokens[i].Kind == kind.Text || tokens[i].Kind == kind.Whitespace) {
				// Stop grouping if the next token after whitespace is a title directive
				// (this whitespace is inter-prompt separation and should be skipped)
				if tokens[i].Kind == kind.Whitespace && i+1 < len(tokens) && endsBody(tokens[i+1].Kind) {
					break
				}
				end = tokens[i].End
				i++
			}

			// Trim trailing whitespace at the end of a prompt body: before the
			// next title directive or section, or at end of file.
			if i == len(tokens) || endsBody(tokens[i].Kind) {
				end = start + len(bytes.TrimRight(input[start:end], " \t\r\n"))
			}

			if end > start {
				coarse = append(coarse, Token{
					Kind:  Text,
					Start: start,
					End:   end,
				})
			}

		default:
			// Skip unrecognized tokens
			i++
		}
	}

	return coarse
}

// endsBody reports whether a token of kind k ends the prompt body before it.
// Declaration sections are not part of any prompt, so they end it too.
func endsBody(k kind.Kind) bool {
	return k == kind.TitleDirective || k == kind.Section
}

func groupVariableTokens(tokens token.Slice, start int) (Token, bool) {
	end := skipToClosingBracket(tokens, start)
	if !validGroupedRange(tokens, start, end) {
		return Token{}, false
	}
	return Token{
		Kind:  Var,
		Start: tokens[start+2].Start, // Skip < and ! to get to content
		End:   tokens[end-1].End,     // End before >
	}, true
}

func groupConditionalTokens(tokens token.Slice, start int) (Token, bool) {
	end := skipToClosingBracket(tokens, start)
	if !validGroupedRange(tokens, start, end) {
		return Token{}, false
	}
	return Token{
		Kind:  OptionalBlock,
		Start: tokens[start+2].Start, // Skip < and ? to get to content
		End:   tokens[end-1].End,     // End before >
	}, true
}

func groupEndTagTokens(tokens token.Slice, start int) (Token, bool) {
	end := skipToClosingBracket(tokens, start)
	if !validGroupedRange(tokens, start, end) {
		return Token{}, false
	}
	return Token{
		Kind:  EndTag,
		Start: tokens[start+2].Start, // Skip < and / to get to content
		End:   tokens[end-1].End,     // End before >
	}, true
}

func skipToClosingBracket(tokens token.Slice, start int) int {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Kind == kind.CloseBracket {
			return i
		}
	}
	return len(tokens) - 1
}

func validGroupedRange(tokens token.Slice, start, end int) bool {
	if start < 0 || end < 0 || start >= len(tokens) || end >= len(tokens) {
		return false
	}
	if end-start < 3 {
		return false
	}
	if tokens[end].Kind != kind.CloseBracket {
		return false
	}
	return start+2 <= end-1
}
