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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/coarse"
	"github.com/omniaura/agentflow/pkg/token/kind"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const maxOpenDocuments = 128

var ErrTooManyOpenDocuments = errors.New("too many open documents")

// Document represents a text document being managed by the LSP server
type Document struct {
	URI     string
	Version protocol.Integer
	Content []byte
	Lines   [][]byte

	// Parsed AST and tokens from AgentFlow parser
	AST    ast.File
	Tokens token.Slice

	// Analysis results
	Variables []Variable
	Titles    []Title
	Errors    []ParseError
}

// Variable represents a variable found in the document
type Variable struct {
	Name    string
	Type    string
	Range   Range
	DotPath []string // For nested variables like user.name
	IsTyped bool     // Whether type was explicitly declared
}

// Title represents a .title directive
type Title struct {
	Name  string
	Range Range
}

// ParseError represents a syntax or semantic error
type ParseError struct {
	Range   Range
	Message string
	Code    string
}

// DocumentManager manages all open documents
type DocumentManager struct {
	mu        sync.RWMutex
	documents map[string]*Document
}

// NewDocumentManager creates a new document manager
func NewDocumentManager() *DocumentManager {
	slog.Info("Creating new document manager")
	return &DocumentManager{
		documents: make(map[string]*Document),
	}
}

// OpenDocument opens and parses a new document
func (dm *DocumentManager) OpenDocument(document protocol.TextDocumentItem) (*Document, error) {
	start := time.Now()
	uri := document.URI

	slog.Info("Opening document",
		"uri", uri,
		"languageId", document.LanguageID,
		"version", document.Version,
		"contentLength", len(document.Text),
		"lineCount", strings.Count(document.Text, "\n")+1)

	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Check if document is already open
	if existing, exists := dm.documents[uri]; exists {
		slog.Warn("Document already open, replacing",
			"uri", uri,
			"existingVersion", existing.Version,
			"newVersion", document.Version)
	} else if len(dm.documents) >= maxOpenDocuments {
		slog.Error("Document limit reached",
			"uri", uri,
			"maxOpenDocuments", maxOpenDocuments)
		return nil, ErrTooManyOpenDocuments
	}

	docBytes := []byte(document.Text)
	doc := &Document{
		URI:     uri,
		Version: document.Version,
		Content: docBytes,
		Lines:   bytes.Split(docBytes, []byte("\n")),
	}

	slog.Debug("Document content split into lines",
		"uri", uri,
		"lineCount", len(doc.Lines))

	// Parse the document using AgentFlow parser
	parseStart := time.Now()
	if err := doc.parse(); err != nil {
		slog.Error("Parse failed for document",
			"uri", uri,
			"error", err,
			"parseDuration", time.Since(parseStart))
		// Even if parsing fails, we still want to track the document
		doc.Errors = append(doc.Errors, ParseError{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
			Message: fmt.Sprintf("Parse error: %v", err),
			Code:    "parse_error",
		})
	} else {
		slog.Info("Document parsed successfully",
			"uri", uri,
			"parseDuration", time.Since(parseStart),
			"variableCount", len(doc.Variables),
			"titleCount", len(doc.Titles),
			"errorCount", len(doc.Errors),
			"tokenCount", len(doc.Tokens))
	}

	dm.documents[doc.URI] = doc

	totalDuration := time.Since(start)
	slog.Info("Document opened successfully",
		"uri", uri,
		"totalDuration", totalDuration,
		"totalDocuments", len(dm.documents))

	return doc, nil
}

// UpdateDocument updates an existing document
func (dm *DocumentManager) UpdateDocument(uri string, version protocol.Integer, changes []TextDocumentContentChangeEvent) (*Document, error) {
	start := time.Now()

	slog.Info("Updating document",
		"uri", uri,
		"version", version,
		"changeCount", len(changes))

	dm.mu.Lock()
	defer dm.mu.Unlock()

	doc, exists := dm.documents[uri]
	if !exists {
		slog.Error("Document not found for update", "uri", uri)
		return nil, fmt.Errorf("document not found: %s", uri)
	}

	oldVersion := doc.Version
	oldContentLength := len(doc.Content)

	slog.Debug("Current document state",
		"uri", uri,
		"oldVersion", oldVersion,
		"newVersion", version,
		"oldContentLength", oldContentLength,
		"oldLineCount", len(doc.Lines))

	// Apply changes to document content
	for i, change := range changes {
		changeStart := time.Now()

		if change.Range == nil {
			// Full document replacement
			slog.Debug("Applying full document replacement",
				"uri", uri,
				"changeIndex", i,
				"newContentLength", len(change.Text))
			doc.Content = []byte(change.Text)
		} else {
			// Incremental change - for now, we'll implement full replacement
			// TODO: Implement proper incremental updates
			slog.Debug("Applying incremental change (as full replacement)",
				"uri", uri,
				"changeIndex", i,
				"rangeStart", change.Range.Start,
				"rangeEnd", change.Range.End,
				"newContentLength", len(change.Text))
			doc.Content = []byte(change.Text)
		}

		slog.Debug("Change applied",
			"uri", uri,
			"changeIndex", i,
			"changeDuration", time.Since(changeStart))
	}

	doc.Version = version
	doc.Lines = bytes.Split([]byte(doc.Content), []byte("\n"))

	slog.Debug("Document content updated",
		"uri", uri,
		"newContentLength", len(doc.Content),
		"newLineCount", len(doc.Lines),
		"contentLengthDelta", len(doc.Content)-oldContentLength)

	// Re-parse the document
	parseStart := time.Now()
	doc.Variables = nil
	doc.Titles = nil
	doc.Errors = nil

	slog.Debug("Re-parsing document after update", "uri", uri)

	if err := doc.parse(); err != nil {
		slog.Error("Re-parse failed for updated document",
			"uri", uri,
			"error", err,
			"parseDuration", time.Since(parseStart))
		doc.Errors = append(doc.Errors, ParseError{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
			Message: fmt.Sprintf("Parse error: %v", err),
			Code:    "parse_error",
		})
	} else {
		slog.Info("Document re-parsed successfully",
			"uri", uri,
			"parseDuration", time.Since(parseStart),
			"variableCount", len(doc.Variables),
			"titleCount", len(doc.Titles),
			"errorCount", len(doc.Errors),
			"tokenCount", len(doc.Tokens))
	}

	totalDuration := time.Since(start)
	slog.Info("Document updated successfully",
		"uri", uri,
		"versionChange", fmt.Sprintf("%d -> %d", oldVersion, version),
		"totalDuration", totalDuration)

	return doc, nil
}

// CloseDocument removes a document from management
func (dm *DocumentManager) CloseDocument(uri string) {
	start := time.Now()

	slog.Info("Closing document", "uri", uri)

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, exists := dm.documents[uri]; exists {
		delete(dm.documents, uri)
		slog.Info("Document closed successfully",
			"uri", uri,
			"remainingDocuments", len(dm.documents),
			"duration", time.Since(start))
	} else {
		slog.Warn("Attempted to close non-existent document", "uri", uri)
	}
}

// GetDocument retrieves a document
func (dm *DocumentManager) GetDocument(uri string) (*Document, bool) {
	start := time.Now()

	dm.mu.RLock()
	defer dm.mu.RUnlock()

	doc, exists := dm.documents[uri]

	duration := time.Since(start)
	if exists {
		slog.Debug("Document retrieved",
			"uri", uri,
			"version", doc.Version,
			"contentLength", len(doc.Content),
			"duration", duration)
	} else {
		slog.Debug("Document not found",
			"uri", uri,
			"availableDocuments", len(dm.documents),
			"duration", duration)
	}

	return doc, exists
}

// parse parses the document content using AgentFlow parser
func (d *Document) parse() error {
	start := time.Now()

	slog.Debug("Starting document parse",
		"uri", d.URI,
		"contentLength", len(d.Content))

	// Extract filename from URI for the parser
	filename := extractFilename(d.URI)
	slog.Debug("Extracted filename for parsing",
		"uri", d.URI,
		"filename", filename)

	// Parse using AgentFlow's existing parser
	astStart := time.Now()
	astFile, err := ast.NewFile(filename, []byte(d.Content))
	if err != nil {
		slog.Error("AST parsing failed",
			"uri", d.URI,
			"filename", filename,
			"error", err,
			"astDuration", time.Since(astStart))
		return err
	}

	slog.Debug("AST parsing completed",
		"uri", d.URI,
		"astDuration", time.Since(astStart),
		"promptCount", len(astFile.Prompts))

	d.AST = astFile

	// Tokenize for detailed analysis
	tokenStart := time.Now()
	tokens, err := token.Tokenize([]byte(d.Content))
	if err != nil {
		slog.Error("Tokenization failed",
			"uri", d.URI,
			"error", err,
			"tokenDuration", time.Since(tokenStart))
		return err
	}

	slog.Debug("Tokenization completed",
		"uri", d.URI,
		"tokenDuration", time.Since(tokenStart),
		"tokenCount", len(tokens))

	d.Tokens = tokens

	// Extract variables and titles from the AST/tokens
	extractStart := time.Now()
	d.extractVariables()
	variablesDuration := time.Since(extractStart)

	titleStart := time.Now()
	d.extractTitles()
	titlesDuration := time.Since(titleStart)

	totalDuration := time.Since(start)
	slog.Info("Document parsing completed",
		"uri", d.URI,
		"totalDuration", totalDuration,
		"astDuration", time.Since(astStart),
		"tokenDuration", time.Since(tokenStart),
		"variablesDuration", variablesDuration,
		"titlesDuration", titlesDuration,
		"variableCount", len(d.Variables),
		"titleCount", len(d.Titles))

	return nil
}

// extractVariables extracts all variables from the parsed tokens
func (d *Document) extractVariables() {
	start := time.Now()

	slog.Debug("Extracting variables from tokens",
		"uri", d.URI,
		"tokenCount", len(d.Tokens))

	variableCount := 0

	for i, tok := range d.Tokens {
		// Process granular tokens for variable analysis
		if tok.Kind == kind.VarName {
			varStart := time.Now()

			slog.Debug("Processing variable token",
				"uri", d.URI,
				"tokenIndex", i,
				"tokenKind", tok.Kind,
				"tokenStart", tok.Start,
				"tokenEnd", tok.End)

			// Extract variable name from granular token
			varName := string(tok.Get([]byte(d.Content)))

			// Convert byte position to line/character position
			startPos := d.byteOffsetToPosition(tok.Start)
			endPos := d.byteOffsetToPosition(tok.End)

			// Split variable name by dots for path
			pathParts := strings.Split(varName, ".")

			// Determine type - look for TypeName token after variable name (skip whitespace)
			varType := "string" // default
			for j := i + 1; j < len(d.Tokens); j++ {
				if d.Tokens[j].Kind == kind.TypeName {
					varType = string(d.Tokens[j].Get([]byte(d.Content)))
					break
				} else if d.Tokens[j].Kind != kind.Whitespace {
					// Stop if we hit a non-whitespace, non-type token
					break
				}
			}

			variable := Variable{
				Name:    varName,
				Type:    varType,
				DotPath: pathParts,
				IsTyped: varType != "" && varType != "string",
				Range: Range{
					Start: startPos,
					End:   endPos,
				},
			}

			d.Variables = append(d.Variables, variable)
			variableCount++

			slog.Debug("Variable extracted",
				"uri", d.URI,
				"variableName", variable.Name,
				"variableType", variable.Type,
				"isTyped", variable.IsTyped,
				"dotPathLength", len(variable.DotPath),
				"extractDuration", time.Since(varStart))
		}
	}

	duration := time.Since(start)
	slog.Info("Variable extraction completed",
		"uri", d.URI,
		"variableCount", variableCount,
		"duration", duration)
}

// extractTitles extracts all .title directives from the parsed tokens
func (d *Document) extractTitles() {
	start := time.Now()

	slog.Debug("Extracting titles from AST",
		"uri", d.URI,
		"promptCount", len(d.AST.Prompts))

	titleCount := 0

	for i, prompt := range d.AST.Prompts {
		// Process coarse tokens for title analysis
		if prompt.Title.Kind == coarse.Title {
			titleStart := time.Now()

			slog.Debug("Processing title token",
				"uri", d.URI,
				"promptIndex", i,
				"titleStart", prompt.Title.Start,
				"titleEnd", prompt.Title.End)

			startPos := d.byteOffsetToPosition(prompt.Title.Start)
			endPos := d.byteOffsetToPosition(prompt.Title.End)

			titleText := string(d.AST.Content[prompt.Title.Start:prompt.Title.End])

			title := Title{
				Name: titleText,
				Range: Range{
					Start: startPos,
					End:   endPos,
				},
			}

			d.Titles = append(d.Titles, title)
			titleCount++

			slog.Debug("Title extracted",
				"uri", d.URI,
				"titleName", titleText,
				"extractDuration", time.Since(titleStart))
		}
	}

	duration := time.Since(start)
	slog.Info("Title extraction completed",
		"uri", d.URI,
		"titleCount", titleCount,
		"duration", duration)
}

// byteOffsetToPosition converts a byte offset to a Position
func (d *Document) byteOffsetToPosition(offset int) Position {
	start := time.Now()

	if offset < 0 {
		slog.Debug("Invalid byte offset (negative)",
			"uri", d.URI,
			"offset", offset)
		return Position{Line: 0, Character: 0}
	}

	line := 0
	character := 0

	for i := 0; i < len(d.Content) && i < offset; i++ {
		if d.Content[i] == '\n' {
			line++
			character = 0
		} else {
			character++
		}
	}

	result := Position{Line: line, Character: character}

	duration := time.Since(start)
	if debugMode {
		slog.Debug("Byte offset converted to position",
			"uri", d.URI,
			"offset", offset,
			"line", line,
			"character", character,
			"duration", duration)
	}

	return result
}

// GetVariableAt returns the variable at a specific position
func (d *Document) GetVariableAt(pos Position) *Variable {
	start := time.Now()

	slog.Debug("Looking for variable at position",
		"uri", d.URI,
		"line", pos.Line,
		"character", pos.Character,
		"totalVariables", len(d.Variables))

	for i, variable := range d.Variables {
		if positionInRange(pos, variable.Range) {
			duration := time.Since(start)
			slog.Debug("Variable found at position",
				"uri", d.URI,
				"variableIndex", i,
				"variableName", variable.Name,
				"variableType", variable.Type,
				"searchDuration", duration)
			return &variable
		}
	}

	duration := time.Since(start)
	slog.Debug("No variable found at position",
		"uri", d.URI,
		"line", pos.Line,
		"character", pos.Character,
		"searchDuration", duration)

	return nil
}

// GetCompletionItems returns completion suggestions for a given position
func (d *Document) GetCompletionItems(pos Position) []CompletionItem {
	start := time.Now()

	slog.Debug("Generating completion items",
		"uri", d.URI,
		"line", pos.Line,
		"character", pos.Character)

	var items []CompletionItem

	// Get current line and character context
	if pos.Line >= len(d.Lines) {
		slog.Debug("Position beyond document lines",
			"uri", d.URI,
			"requestedLine", pos.Line,
			"maxLine", len(d.Lines)-1)
		return items
	}

	line := d.Lines[pos.Line]
	if pos.Character > len(line) {
		slog.Debug("Character position beyond line length",
			"uri", d.URI,
			"line", pos.Line,
			"requestedChar", pos.Character,
			"lineLength", len(line))
		return items
	}

	// Check if we're inside a variable declaration context
	beforeCursor := line[:pos.Character]

	slog.Debug("Analyzing context for completion",
		"uri", d.URI,
		"lineContent", line,
		"beforeCursor", beforeCursor)

	itemCount := 0

	// If we're after "<!" suggest variables
	if bytes.Contains(beforeCursor, []byte("<!")) && !bytes.Contains(beforeCursor, []byte(">")) {
		slog.Debug("In variable context, suggesting variables", "uri", d.URI)

		// Suggest existing variables
		seenVars := make(map[string]bool)
		for _, variable := range d.Variables {
			if !seenVars[variable.Name] {
				items = append(items, CompletionItem{
					Label:  variable.Name,
					Kind:   protocol.CompletionItemKindVariable,
					Detail: variable.Type,
				})
				seenVars[variable.Name] = true
				itemCount++
			}
		}

		slog.Debug("Added variable suggestions",
			"uri", d.URI,
			"uniqueVariables", len(seenVars))

		// Suggest types if we're after a space
		if bytes.Contains(beforeCursor, []byte(" ")) {
			slog.Debug("In type context, suggesting types", "uri", d.URI)

			types := []string{"string", "int", "bool", "float32", "float64"}
			for _, typ := range types {
				items = append(items, CompletionItem{
					Label: typ,
					Kind:  protocol.CompletionItemKindKeyword,
				})
				itemCount++
			}
		}
	}

	// If we're after "<?" suggest conditional operators
	if bytes.Contains(beforeCursor, []byte("<?")) && !bytes.Contains(beforeCursor, []byte(">")) {
		slog.Debug("In conditional context, suggesting operators", "uri", d.URI)

		operators := []string{"eq", "ne", "gt", "lt", "gte", "lte"}
		for _, op := range operators {
			items = append(items, CompletionItem{
				Label: op,
				Kind:  protocol.CompletionItemKindOperator,
			})
			itemCount++
		}
	}

	// Suggest .title at beginning of line
	if len(bytes.TrimSpace(beforeCursor)) == 0 || bytes.HasPrefix(bytes.TrimSpace(beforeCursor), []byte(".")) {
		slog.Debug("At line start, suggesting directives", "uri", d.URI)

		items = append(items, CompletionItem{
			Label:      ".title",
			Kind:       protocol.CompletionItemKindKeyword,
			InsertText: ".title ",
		})
		itemCount++
	}

	duration := time.Since(start)
	slog.Info("Completion items generated",
		"uri", d.URI,
		"itemCount", itemCount,
		"duration", duration)

	return items
}

// positionInRange checks if a position is within a range
func positionInRange(pos Position, r Range) bool {
	if pos.Line < r.Start.Line || pos.Line > r.End.Line {
		return false
	}
	if pos.Line == r.Start.Line && pos.Character < r.Start.Character {
		return false
	}
	if pos.Line == r.End.Line && pos.Character > r.End.Character {
		return false
	}
	return true
}

// extractFilename extracts filename from URI
func extractFilename(uri string) string {
	// Simple implementation - in reality, we'd properly parse the URI
	parts := strings.Split(uri, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		slog.Debug("Filename extracted from URI",
			"uri", uri,
			"filename", filename)
		return filename
	}
	slog.Debug("Could not extract filename from URI, using default",
		"uri", uri,
		"defaultFilename", "unknown.af")
	return "unknown.af"
}
