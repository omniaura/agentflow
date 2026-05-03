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
	"log/slog"
	"time"

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
		case kind.DirectiveVar, kind.DirectiveCond, kind.DirectiveEnd, kind.DirectiveElse:
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
		case kind.Comment:
			tokenType = TokenTypeComment
		case kind.Text:
			tokenType = TokenTypeString // Regular text content
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
