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
package token

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/token/kind"
	"github.com/peyton-spencer/caseconv/bytcase"
)

type Slice []T

func (t Slice) Equal(o Slice) bool {
	if len(t) != len(o) {
		return false
	}
	for i, tok := range t {
		if tok != o[i] {
			return false
		}
	}
	return true
}

type T struct {
	Kind  kind.Kind
	Start int
	End   int
}

func (t T) Get(in []byte) []byte {
	return in[t.Start:t.End]
}


func (t T) GetWrap(in []byte, left, right byte) []byte {
	out := make([]byte, 0, len(in)+2)
	out = append(out, left)
	out = append(out, in[t.Start:t.End]...)
	out = append(out, right)
	return out
}

func (t T) GetWrapLL(in []byte, left []byte, right byte) []byte {
	out := make([]byte, 0, len(in)+len(left)+1)
	out = append(out, left...)
	out = append(out, in[t.Start:t.End]...)
	out = append(out, right)
	return out
}

func (t T) GetJSFmtVar(in []byte) []byte {
	out := make([]byte, 0, len(in)+3)
	out = append(out, '$', '{')
	out = append(out, bytcase.ToLowerCamel(in[t.Start:t.End])...)
	out = append(out, '}')
	return out
}

var (
	cmdTitle = []byte(".title")
)


func Tokenize(input []byte) (Slice, error) {
	var tokens []T
	i := 0
	
	for i < len(input) {
		// Check for .title directive at start of line
		if i == 0 || (i > 0 && input[i-1] == '\n') {
			if i < len(input) && input[i] == '.' {
				if title := tryParseTitle(input, i); title != nil {
					tokens = append(tokens, title...)
					i += titleLength(input, i)
					continue
				}
			}
		}
		
		// Check for < with lookahead for valid directives
		if input[i] == '<' {
			if tag := tryParseTag(input, i); tag != nil {
				tokens = append(tokens, tag...)
				i += tagLength(input, i)
				continue
			}
		}
		
		// Check for whitespace
		if isWhitespace(input[i]) {
			ws := parseWhitespace(input, i)
			tokens = append(tokens, ws)
			i += ws.End - ws.Start
			continue
		}
		
		// Regular text content
		text := parseText(input, i)
		tokens = append(tokens, text)
		i += text.End - text.Start
	}
	
	return tokens, nil
}

// tryParseTitle attempts to parse .title directive
func tryParseTitle(input []byte, start int) []T {
	if start+6 >= len(input) || !bytes.HasPrefix(input[start:], []byte(".title")) {
		return nil
	}
	
	// Check that ".title" is followed by whitespace or end of input
	if start+6 < len(input) && !isWhitespace(input[start+6]) {
		return nil
	}
	
	var tokens []T
	
	// .title directive
	tokens = append(tokens, T{
		Kind:  kind.TitleDirective,
		Start: start,
		End:   start + 6,
	})
	
	pos := start + 6
	
	// Optional whitespace after .title
	if pos < len(input) && isWhitespace(input[pos]) {
		wsStart := pos
		for pos < len(input) && isWhitespace(input[pos]) && input[pos] != '\n' {
			pos++
		}
		tokens = append(tokens, T{
			Kind:  kind.Whitespace,
			Start: wsStart,
			End:   pos,
		})
	}
	
	// Title text (rest of line)
	if pos < len(input) && input[pos] != '\n' {
		textStart := pos
		for pos < len(input) && input[pos] != '\n' {
			pos++
		}
		// Trim trailing whitespace from title text
		textEnd := pos
		for textEnd > textStart && isWhitespace(input[textEnd-1]) {
			textEnd--
		}
		if textEnd > textStart {
			tokens = append(tokens, T{
				Kind:  kind.TitleText,
				Start: textStart,
				End:   textEnd,
			})
		}
	}
	
	return tokens
}

// tryParseTag attempts to parse < with lookahead for valid directives
func tryParseTag(input []byte, start int) []T {
	if start >= len(input) || input[start] != '<' {
		return nil
	}
	
	// Check what follows <
	if start+1 >= len(input) {
		return nil
	}
	
	switch input[start+1] {
	case '!':
		return parseVarTag(input, start)
	case '?':
		return parseCondTag(input, start)
	case '/':
		return parseEndTag(input, start)
	default:
		// Check for <else>
		if start+4 < len(input) && bytes.HasPrefix(input[start:], []byte("<else")) {
			if start+5 >= len(input) || input[start+5] == '>' {
				return parseElseTag(input, start)
			}
		}
		return nil
	}
}

// parseVarTag parses <!variable> or <!variable type>
func parseVarTag(input []byte, start int) []T {
	var tokens []T
	pos := start
	
	// Find closing >
	closePos := -1
	for i := start + 2; i < len(input); i++ {
		if input[i] == '>' {
			closePos = i
			break
		}
	}
	if closePos == -1 {
		return nil
	}
	
	// OpenBracket + DirectiveVar
	tokens = append(tokens, T{Kind: kind.OpenBracket, Start: pos, End: pos + 1})
	pos++
	tokens = append(tokens, T{Kind: kind.DirectiveVar, Start: pos, End: pos + 1})
	pos++
	
	// Parse content between <! and >
	content := input[pos:closePos]
	contentTokens := parseVarContent(content, pos)
	tokens = append(tokens, contentTokens...)
	
	// CloseBracket
	tokens = append(tokens, T{Kind: kind.CloseBracket, Start: closePos, End: closePos + 1})
	
	return tokens
}

// parseCondTag parses <?variable> or <?variable operator value>
func parseCondTag(input []byte, start int) []T {
	var tokens []T
	pos := start
	
	// Find closing >
	closePos := -1
	for i := start + 2; i < len(input); i++ {
		if input[i] == '>' {
			closePos = i
			break
		}
	}
	if closePos == -1 {
		return nil
	}
	
	// OpenBracket + DirectiveCond
	tokens = append(tokens, T{Kind: kind.OpenBracket, Start: pos, End: pos + 1})
	pos++
	tokens = append(tokens, T{Kind: kind.DirectiveCond, Start: pos, End: pos + 1})
	pos++
	
	// Parse content between <? and >
	content := input[pos:closePos]
	contentTokens := parseCondContent(content, pos)
	tokens = append(tokens, contentTokens...)
	
	// CloseBracket
	tokens = append(tokens, T{Kind: kind.CloseBracket, Start: closePos, End: closePos + 1})
	
	return tokens
}

// parseEndTag parses </variable>
func parseEndTag(input []byte, start int) []T {
	var tokens []T
	pos := start
	
	// Find closing >
	closePos := -1
	for i := start + 2; i < len(input); i++ {
		if input[i] == '>' {
			closePos = i
			break
		}
	}
	if closePos == -1 {
		return nil
	}
	
	// OpenBracket + DirectiveEnd
	tokens = append(tokens, T{Kind: kind.OpenBracket, Start: pos, End: pos + 1})
	pos++
	tokens = append(tokens, T{Kind: kind.DirectiveEnd, Start: pos, End: pos + 1})
	pos++
	
	// Variable name
	if closePos > pos {
		tokens = append(tokens, T{Kind: kind.VarName, Start: pos, End: closePos})
	}
	
	// CloseBracket
	tokens = append(tokens, T{Kind: kind.CloseBracket, Start: closePos, End: closePos + 1})
	
	return tokens
}

// parseElseTag parses <else>
func parseElseTag(input []byte, start int) []T {
	var tokens []T
	
	// OpenBracket
	tokens = append(tokens, T{Kind: kind.OpenBracket, Start: start, End: start + 1})
	
	// DirectiveElse
	tokens = append(tokens, T{Kind: kind.DirectiveElse, Start: start + 1, End: start + 5})
	
	// CloseBracket
	tokens = append(tokens, T{Kind: kind.CloseBracket, Start: start + 5, End: start + 6})
	
	return tokens
}

// parseVarContent parses the content inside <!...>
func parseVarContent(content []byte, offset int) []T {
	var tokens []T
	pos := 0
	
	// Skip leading whitespace
	for pos < len(content) && isWhitespace(content[pos]) {
		pos++
	}
	if pos >= len(content) {
		return tokens
	}
	
	// Variable name (everything until whitespace or end)
	varStart := pos
	for pos < len(content) && !isWhitespace(content[pos]) {
		pos++
	}
	if pos > varStart {
		tokens = append(tokens, T{
			Kind:  kind.VarName,
			Start: offset + varStart,
			End:   offset + pos,
		})
	}
	
	// Optional whitespace + type
	if pos < len(content) {
		// Whitespace
		wsStart := pos
		for pos < len(content) && isWhitespace(content[pos]) {
			pos++
		}
		if pos > wsStart {
			tokens = append(tokens, T{
				Kind:  kind.Whitespace,
				Start: offset + wsStart,
				End:   offset + pos,
			})
		}
		
		// Type name
		if pos < len(content) {
			typeStart := pos
			for pos < len(content) && !isWhitespace(content[pos]) {
				pos++
			}
			if pos > typeStart {
				tokens = append(tokens, T{
					Kind:  kind.TypeName,
					Start: offset + typeStart,
					End:   offset + pos,
				})
			}
		}
	}
	
	return tokens
}

// parseCondContent parses the content inside <?...>
func parseCondContent(content []byte, offset int) []T {
	var tokens []T
	
	// Split on whitespace and parse each part
	parts := bytes.Fields(content)
	pos := 0
	
	for i, part := range parts {
		// Skip to the start of this part
		for pos < len(content) && isWhitespace(content[pos]) {
			if i > 0 {
				// Add whitespace token
				wsStart := pos
				for pos < len(content) && isWhitespace(content[pos]) {
					pos++
				}
				tokens = append(tokens, T{
					Kind:  kind.Whitespace,
					Start: offset + wsStart,
					End:   offset + pos,
				})
				break
			}
			pos++
		}
		
		partStart := pos
		partEnd := pos + len(part)
		
		if i == 0 {
			// First part is variable name
			tokens = append(tokens, T{
				Kind:  kind.VarName,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		} else if isOperator(string(part)) {
			// Operator
			tokens = append(tokens, T{
				Kind:  kind.Operator,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		} else if isType(string(part)) {
			// Type
			tokens = append(tokens, T{
				Kind:  kind.TypeName,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		} else if isBoolValue(string(part)) {
			// Boolean value
			tokens = append(tokens, T{
				Kind:  kind.BoolValue,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		} else if isIntValue(string(part)) {
			// Integer value
			tokens = append(tokens, T{
				Kind:  kind.IntValue,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		} else {
			// String value (including quoted strings)
			tokens = append(tokens, T{
				Kind:  kind.StringValue,
				Start: offset + partStart,
				End:   offset + partEnd,
			})
		}
		
		pos = partEnd
	}
	
	return tokens
}

// parseWhitespace parses whitespace characters
func parseWhitespace(input []byte, start int) T {
	pos := start
	for pos < len(input) && isWhitespace(input[pos]) {
		pos++
	}
	return T{
		Kind:  kind.Whitespace,
		Start: start,
		End:   pos,
	}
}

// parseText parses regular text content
func parseText(input []byte, start int) T {
	pos := start
	for pos < len(input) {
		if input[pos] == '<' {
			// Check if this might be a tag
			if tryParseTag(input, pos) != nil {
				break
			}
		}
		if input[pos] == '.' && (pos == 0 || input[pos-1] == '\n') {
			// Check if this might be a title directive
			if tryParseTitle(input, pos) != nil {
				break
			}
		}
		if isWhitespace(input[pos]) {
			break
		}
		pos++
	}
	return T{
		Kind:  kind.Text,
		Start: start,
		End:   pos,
	}
}

// Helper functions
func titleLength(input []byte, start int) int {
	pos := start + 6 // ".title"
	for pos < len(input) && input[pos] != '\n' {
		pos++
	}
	return pos - start
}

func tagLength(input []byte, start int) int {
	for i := start + 1; i < len(input); i++ {
		if input[i] == '>' {
			return i - start + 1
		}
	}
	return len(input) - start
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isOperator(s string) bool {
	operators := []string{"eq", "ne", "gt", "lt", "gte", "lte"}
	for _, op := range operators {
		if s == op {
			return true
		}
	}
	return false
}

func isType(s string) bool {
	types := []string{"int", "bool", "string", "float", "float32", "float64"}
	for _, t := range types {
		if s == t {
			return true
		}
	}
	return false
}

func isBoolValue(s string) bool {
	return s == "true" || s == "false"
}

func isIntValue(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func (t T) Stringify(in []byte) string {
	var buf strings.Builder
	buf.Grow(len(in) + 100)
	buf.WriteString(t.Kind.String())
	buf.WriteString(":\t[")
	buf.WriteString(strconv.Itoa(t.Start))
	buf.WriteString(":")
	buf.WriteString(strconv.Itoa(t.End))
	buf.WriteString("]\t")
	if t.Start < 0 || t.End > len(in) || t.Start > t.End {
		buf.WriteString("INVALID BOUNDS")
	} else {
		buf.WriteString("\"")
		buf.Write(in[t.Start:t.End])
		buf.WriteString("\"")
	}
	return buf.String()
}

func (s Slice) Stringify(in []byte) string {
	if len(in) == 0 {
		return "no content"
	}
	if len(s) == 0 {
		return "no tokens"
	}
	var buf strings.Builder
	buf.Grow(len(in) + 100)
	for i, tok := range s {
		buf.WriteString(tok.Stringify(in))
		if i != len(s)-1 {
			buf.WriteRune('\n')
		}
	}
	return buf.String()
}

// VarInfo holds the parsed variable path and type from a var token
// Path is the dot-separated path as a slice of []byte (e.g. ["user", "subscription", "tier"])
// Type is the type string (e.g. "string", "int", "bool")
// Operator is the conditional operator (e.g. ">=", "==", "!=") for OptionalBlock tokens
// Operand is the value to compare against (e.g. "30", "true", "\"gold\"")
type VarInfo struct {
	Path     [][]byte
	Type     string
	Operator string // For conditionals: ">=", "<=", "==", "!=", ">", "<"
	Operand  string // For conditionals: the value to compare against
}

// GetVar parses the variable name and type from a var token
// The typeCache map stores previously seen variable types for reuse
// For OptionalBlock tokens, it also parses conditional operators and operands
func (t T) GetVar(in []byte, typeCache map[string]string) VarInfo {
	b := in[t.Start:t.End]

	// Handle conditional expressions for conditional directive tokens
	if t.Kind == kind.DirectiveCond {
		return ParseConditionalExpression(b, typeCache)
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
	return VarInfo{Path: path, Type: typ}
}

// ParseConditionalExpression parses conditional expressions like "writer.current_streak gte 30"
func ParseConditionalExpression(expr []byte, typeCache map[string]string) VarInfo {
	exprStr := string(expr)

	// List of word operators to check for
	operators := []string{"gte", "lte", "gt", "lt", "eq", "ne"}

	for _, op := range operators {
		if idx := strings.Index(exprStr, " "+op+" "); idx != -1 {
			// Found an operator
			varPart := strings.TrimSpace(exprStr[:idx])
			operandPart := strings.TrimSpace(exprStr[idx+len(op)+2:]) // +2 for the spaces around operator

			// Parse variable path and type
			varInfo := parseVariablePart(varPart, typeCache)

			// Check if operand is a variable reference (contains dot notation but is not a numeric literal)
			if strings.Contains(operandPart, ".") && !strings.Contains(operandPart, "\"") && !strings.Contains(operandPart, "'") && !isNumericLiteral(operandPart) {
				// Operand is another variable - convert to Go field access
				operandPath := strings.Split(operandPart, ".")
				var operandFieldAccess strings.Builder
				operandFieldAccess.WriteString("input.")
				for i, part := range operandPath {
					// Convert to CamelCase using the proper bytcase function
					if len(part) > 0 {
						operandFieldAccess.Write(bytcase.ToCamel([]byte(part)))
						if i < len(operandPath)-1 {
							operandFieldAccess.WriteString(".")
						}
					}
				}
				varInfo.Operand = operandFieldAccess.String()
			} else {
				// Infer type from operand constant
				inferredType := inferTypeFromOperand(operandPart)
				if inferredType != "string" || varInfo.Type == "string" {
					varInfo.Type = inferredType
					// Cache the inferred type
					pathKey := string(bytes.Join(varInfo.Path, []byte(".")))
					typeCache[pathKey] = inferredType
				}
				varInfo.Operand = formatOperandForGeneration(operandPart, varInfo.Type)
			}

			varInfo.Operator = op
			return varInfo
		}
	}

	// No operator found - treat as simple truthiness check
	varInfo := parseVariablePart(exprStr, typeCache)
	return varInfo
}

// formatOperandForGeneration formats operands for code generation
func formatOperandForGeneration(operand, varType string) string {
	operand = strings.TrimSpace(operand)

	switch varType {
	case "string":
		// Ensure string operands are properly quoted
		if !strings.HasPrefix(operand, "\"") && !strings.HasPrefix(operand, "'") {
			return "\"" + operand + "\""
		}
		return operand
	case "int", "float32", "float64":
		// Numeric types - use as-is
		return operand
	case "bool":
		// Boolean types - use as-is
		return operand
	default:
		// Default to quoted string
		if !strings.HasPrefix(operand, "\"") && !strings.HasPrefix(operand, "'") {
			return "\"" + operand + "\""
		}
		return operand
	}
}

// parseVariablePart parses the variable name and optional explicit type
func parseVariablePart(varPart string, typeCache map[string]string) VarInfo {
	parts := strings.Fields(varPart)
	var path [][]byte
	var typ string

	if len(parts) > 0 {
		path = bytes.Split([]byte(parts[0]), []byte{'.'})
	}

	// Create cache key from the variable path
	pathKey := string(bytes.Join(path, []byte(".")))

	if len(parts) > 1 {
		// Type is explicitly specified, cache it
		typ = parts[1]
		typeCache[pathKey] = typ
	} else {
		// No type specified, check cache
		if cachedType, exists := typeCache[pathKey]; exists {
			typ = cachedType
		} else {
			typ = "string" // default
		}
	}

	return VarInfo{Path: path, Type: typ}
}

// inferTypeFromOperand infers the Go type from the operand value
func inferTypeFromOperand(operand string) string {
	operand = strings.TrimSpace(operand)

	// Check for boolean values
	if operand == "true" || operand == "false" {
		return "bool"
	}

	// Check for quoted strings
	if (strings.HasPrefix(operand, "\"") && strings.HasSuffix(operand, "\"")) ||
		(strings.HasPrefix(operand, "'") && strings.HasSuffix(operand, "'")) {
		return "string"
	}

	// Check for floating point numbers
	if strings.Contains(operand, ".") {
		if _, err := strconv.ParseFloat(operand, 64); err == nil {
			return "float64"
		}
	}

	// Check for integers
	if _, err := strconv.Atoi(operand); err == nil {
		return "int"
	}

	// Default to string for unquoted values
	return "string"
}

// isNumericLiteral checks if a string represents a numeric literal (int or float)
func isNumericLiteral(operand string) bool {
	operand = strings.TrimSpace(operand)
	
	// Check for floating point numbers
	if strings.Contains(operand, ".") {
		if _, err := strconv.ParseFloat(operand, 64); err == nil {
			return true
		}
	}
	
	// Check for integers
	if _, err := strconv.Atoi(operand); err == nil {
		return true
	}
	
	return false
}

// hasComparisonOperator checks if the token content contains any comparison operators
func hasComparisonOperator(content string) bool {
	operators := []string{">=", "<=", "==", "!=", ">", "<"}
	for _, op := range operators {
		if strings.Contains(content, op) {
			return true
		}
	}
	return false
}
