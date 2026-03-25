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
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/omniaura/agentflow/pkg/ptrconv"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"
)

const maxJSONPositionValue = 1_000_000

var (
	version   = "0.1.0"
	handler   protocol.Handler
	documents *DocumentManager
	debugMode bool
)

// NewServer creates a new LSP server using the GLSP framework
func NewServer(debug bool) *server.Server {
	debugMode = debug
	slog.Info("Creating new LSP server", "debug", debug, "version", version)

	documents = NewDocumentManager()
	handler = protocol.Handler{
		Initialize:                     initialize,
		Initialized:                    initialized,
		Shutdown:                       shutdown,
		SetTrace:                       setTrace,
		TextDocumentDidOpen:            textDocumentDidOpen,
		TextDocumentDidChange:          textDocumentDidChange,
		TextDocumentDidClose:           textDocumentDidClose,
		TextDocumentCompletion:         textDocumentCompletion,
		TextDocumentHover:              textDocumentHover,
		TextDocumentDocumentSymbol:     textDocumentDocumentSymbol,
		TextDocumentSemanticTokensFull: textDocumentSemanticTokensFull,
	}

	s := server.NewServer(&handler, "AgentFlow LSP", debug)
	slog.Info("LSP server created successfully")
	return s
}

// LSP Handler Functions

func initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	start := time.Now()
	slog.Info("LSP initialize request received",
		"processId", params.ProcessID,
		"rootUri", params.RootURI,
		"clientName", getClientName(params.ClientInfo))

	if debugMode {
		slog.Debug("Initialize params", "params", params)
	}

	capabilities := handler.CreateServerCapabilities()

	// Configure our specific capabilities
	capabilities.TextDocumentSync = protocol.TextDocumentSyncOptions{
		OpenClose: ptrconv.Bool(true),
		Change:    ptrconv.Int32(protocol.TextDocumentSyncKindFull),
	}
	capabilities.HoverProvider = ptrconv.Bool(true)
	capabilities.CompletionProvider = &protocol.CompletionOptions{
		ResolveProvider:   ptrconv.Bool(false),
		TriggerCharacters: []string{"<", "!", "?", ".", " "},
	}
	capabilities.DocumentSymbolProvider = ptrconv.Bool(true)

	// Configure semantic tokens capability
	capabilities.SemanticTokensProvider = protocol.SemanticTokensOptions{
		Legend: protocol.SemanticTokensLegend{
			TokenTypes: []string{
				TokenTypeKeyword,   // .title
				TokenTypeVariable,  // <!variable>
				TokenTypeString,    // text content
				TokenTypeComment,   // comments
				TokenTypeOperator,  // eq, gte, lte operators
				TokenTypeType,      // type annotations
				TokenTypeParameter, // variable parameters
				TokenTypeDecorator, // .title prefix
				TokenTypeFunction,  // conditional blocks
				TokenTypeProperty,  // variable path segments
				TokenTypeTagOpen,   // <!, <?, </
				TokenTypeTagClose,  // >
			},
			TokenModifiers: []string{
				TokenModifierDeclaration,
				TokenModifierDefinition,
				TokenModifierReadonly,
				TokenModifierDocumentation,
			},
		},
		Range: ptrconv.Bool(false), // We'll implement full document only for now
		Full:  ptrconv.Bool(true),
	}

	change := protocol.FileOperationRegistrationOptions{
		Filters: []protocol.FileOperationFilter{
			{
				Scheme: ptrconv.Str("file"),
				Pattern: protocol.FileOperationPattern{
					Glob: "**/*.af",
				},
			},
		},
	}
	capabilities.Workspace = &protocol.ServerCapabilitiesWorkspace{
		FileOperations: &protocol.ServerCapabilitiesWorkspaceFileOperations{
			DidCreate: &change,
			DidRename: &change,
			DidDelete: &change,
		},
	}

	result := protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "AgentFlow LSP",
			Version: ptrconv.Str(version),
		},
	}

	duration := time.Since(start)
	slog.Info("LSP initialize completed", "duration", duration)

	if debugMode {
		slog.Debug("Initialize result", "capabilities", capabilities)
	}

	return result, nil
}

func initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	slog.Info("LSP initialized notification received")
	if debugMode {
		slog.Debug("Initialized params", "params", params)
	}
	slog.Info("AgentFlow LSP server is ready to accept requests")
	return nil
}

func shutdown(context *glsp.Context) error {
	slog.Info("LSP shutdown request received")
	protocol.SetTraceValue(protocol.TraceValueOff)
	slog.Info("LSP server shutting down")
	return nil
}

func setTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	slog.Info("LSP setTrace request received", "value", params.Value)
	protocol.SetTraceValue(params.Value)
	slog.Info("Trace value set", "value", params.Value)
	return nil
}

func textDocumentDidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	start := time.Now()
	uri := params.TextDocument.URI
	slog.Info("LSP textDocument/didOpen received",
		"uri", uri,
		"languageId", params.TextDocument.LanguageID,
		"version", params.TextDocument.Version,
		"contentLength", len(params.TextDocument.Text))

	if debugMode {
		slog.Debug("Document open params", "params", params)
	}

	doc, err := documents.OpenDocument(params.TextDocument)
	if err != nil {
		slog.Error("Failed to open document",
			"uri", uri,
			"error", err,
			"duration", time.Since(start))
		return fmt.Errorf("failed to open document: %w", err)
	}

	slog.Info("Document opened successfully",
		"uri", uri,
		"variableCount", len(doc.Variables),
		"titleCount", len(doc.Titles),
		"errorCount", len(doc.Errors),
		"duration", time.Since(start))

	// Send diagnostics
	diagStart := time.Now()
	publishDiagnostics(context, doc)
	slog.Debug("Published diagnostics",
		"uri", uri,
		"diagnosticCount", len(doc.Errors),
		"diagnosticDuration", time.Since(diagStart))

	return nil
}

func textDocumentDidChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	start := time.Now()
	uri := params.TextDocument.URI
	version := params.TextDocument.Version
	changeCount := len(params.ContentChanges)

	slog.Info("LSP textDocument/didChange received",
		"uri", uri,
		"version", version,
		"changeCount", changeCount)

	if debugMode {
		slog.Debug("Document change params", "params", params)
	}

	// Convert GLSP change events to our format
	var changes []TextDocumentContentChangeEvent
	for i, change := range params.ContentChanges {
		// Handle both full and incremental changes by type assertion
		changeEvent := TextDocumentContentChangeEvent{}

		switch v := change.(type) {
		case protocol.TextDocumentContentChangeEvent:
			// Handle incremental changes
			slog.Debug("Processing incremental change",
				"uri", uri,
				"changeIndex", i,
				"textLength", len(v.Text))
			changeEvent.Text = v.Text
			if v.Range != nil {
				changeEvent.Range = &Range{
					Start: protocolPositionToPosition(v.Range.Start),
					End:   protocolPositionToPosition(v.Range.End),
				}
				slog.Debug("Change has range",
					"uri", uri,
					"changeIndex", i,
					"startLine", changeEvent.Range.Start.Line,
					"startChar", changeEvent.Range.Start.Character,
					"endLine", changeEvent.Range.End.Line,
					"endChar", changeEvent.Range.End.Character)
			}
			if v.RangeLength != nil {
				rangeLength := protocolUIntegerToInt(*v.RangeLength)
				changeEvent.RangeLength = &rangeLength
				slog.Debug("Change has range length",
					"uri", uri,
					"changeIndex", i,
					"rangeLength", rangeLength)
			}

		case protocol.TextDocumentContentChangeEventWhole:
			// Handle full document changes
			slog.Debug("Processing full document change",
				"uri", uri,
				"changeIndex", i,
				"textLength", len(v.Text))
			changeEvent.Text = v.Text

		case map[string]any:
			slog.Warn("Unexpected change event type map[string]any",
				"uri", uri,
				"changeIndex", i,
				"change", v)
			parsedChange, ok := parseChangeEventFromMap(v, uri, i)
			if !ok {
				continue
			}
			changeEvent = parsedChange
		}
		changes = append(changes, changeEvent)
	}

	updateStart := time.Now()
	doc, err := documents.UpdateDocument(uri, version, changes)
	if err != nil {
		slog.Error("Failed to update document",
			"uri", uri,
			"version", version,
			"error", err,
			"duration", time.Since(start))
		return fmt.Errorf("failed to update document: %w", err)
	}

	slog.Info("Document updated successfully",
		"uri", uri,
		"version", version,
		"variableCount", len(doc.Variables),
		"titleCount", len(doc.Titles),
		"errorCount", len(doc.Errors),
		"updateDuration", time.Since(updateStart),
		"totalDuration", time.Since(start))

	// Send updated diagnostics
	diagStart := time.Now()
	publishDiagnostics(context, doc)
	slog.Debug("Published updated diagnostics",
		"uri", uri,
		"diagnosticCount", len(doc.Errors),
		"diagnosticDuration", time.Since(diagStart))

	return nil
}

func textDocumentDidClose(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	slog.Info("LSP textDocument/didClose received", "uri", uri)

	if debugMode {
		slog.Debug("Document close params", "params", params)
	}

	documents.CloseDocument(uri)
	slog.Info("Document closed", "uri", uri)
	return nil
}

func textDocumentCompletion(context *glsp.Context, params *protocol.CompletionParams) (any, error) {
	start := time.Now()
	uri := params.TextDocument.URI
	line := params.Position.Line
	character := params.Position.Character

	slog.Info("LSP textDocument/completion received",
		"uri", uri,
		"line", line,
		"character", character)

	if debugMode {
		slog.Debug("Completion params", "params", params)
	}

	doc, exists := documents.GetDocument(uri)
	if !exists {
		slog.Warn("Document not found for completion", "uri", uri)
		return []protocol.CompletionItem{}, nil
	}

	pos := protocolPositionToPosition(params.Position)

	items := doc.GetCompletionItems(pos)
	slog.Info("Completion items generated",
		"uri", uri,
		"itemCount", len(items),
		"duration", time.Since(start))

	// Convert our completion items to GLSP format
	var glspItems []protocol.CompletionItem
	for _, item := range items {
		glspItem := protocol.CompletionItem{
			Label:  item.Label,
			Kind:   &item.Kind,
			Detail: &item.Detail,
		}
		if item.InsertText != "" {
			glspItem.InsertText = &item.InsertText
		}
		glspItems = append(glspItems, glspItem)
	}

	if debugMode {
		slog.Debug("Completion result", "uri", uri, "items", glspItems)
	}

	return glspItems, nil
}

func textDocumentHover(context *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	start := time.Now()
	uri := params.TextDocument.URI
	line := params.Position.Line
	character := params.Position.Character

	slog.Info("LSP textDocument/hover received",
		"uri", uri,
		"line", line,
		"character", character)

	if debugMode {
		slog.Debug("Hover params", "params", params)
	}

	doc, exists := documents.GetDocument(uri)
	if !exists {
		slog.Warn("Document not found for hover", "uri", uri)
		return nil, nil
	}

	pos := protocolPositionToPosition(params.Position)

	variable := doc.GetVariableAt(pos)
	if variable != nil {
		hoverContent := fmt.Sprintf("**%s** (%s)\n\nAgentFlow variable", variable.Name, variable.Type)
		if len(variable.DotPath) > 1 {
			hoverContent += fmt.Sprintf("\n\nNested path: %s", joinString(variable.DotPath, " → "))
		}

		result := &protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: hoverContent,
			},
			Range: &protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(variable.Range.Start.Line),
					Character: protocol.UInteger(variable.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(variable.Range.End.Line),
					Character: protocol.UInteger(variable.Range.End.Character),
				},
			},
		}

		slog.Info("Hover information provided",
			"uri", uri,
			"variableName", variable.Name,
			"variableType", variable.Type,
			"duration", time.Since(start))

		if debugMode {
			slog.Debug("Hover result", "uri", uri, "result", result)
		}

		return result, nil
	}

	operatorInfo := doc.GetOperatorAt(pos)
	if operatorInfo != nil {
		hoverContent := formatOperatorHover(operatorInfo)

		result := &protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: hoverContent,
			},
		}

		slog.Info("Operator hover information provided",
			"uri", uri,
			"operator", operatorInfo.Name,
			"duration", time.Since(start))

		if debugMode {
			slog.Debug("Operator hover result", "uri", uri, "result", result)
		}

		return result, nil
	}

	slog.Debug("No hover information found at position",
		"uri", uri,
		"line", line,
		"character", character,
		"duration", time.Since(start))
	return nil, nil
}

func textDocumentDocumentSymbol(context *glsp.Context, params *protocol.DocumentSymbolParams) (any, error) {
	start := time.Now()
	uri := params.TextDocument.URI

	slog.Info("LSP textDocument/documentSymbol received", "uri", uri)

	if debugMode {
		slog.Debug("Document symbol params", "params", params)
	}

	doc, exists := documents.GetDocument(uri)
	if !exists {
		slog.Warn("Document not found for symbols", "uri", uri)
		return []protocol.DocumentSymbol{}, nil
	}

	var symbols []protocol.DocumentSymbol

	// Add titles as symbols
	for _, title := range doc.Titles {
		symbol := protocol.DocumentSymbol{
			Name: title.Name,
			Kind: protocol.SymbolKindFunction,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(title.Range.Start.Line),
					Character: protocol.UInteger(title.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(title.Range.End.Line),
					Character: protocol.UInteger(title.Range.End.Character),
				},
			},
			SelectionRange: protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(title.Range.Start.Line),
					Character: protocol.UInteger(title.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(title.Range.End.Line),
					Character: protocol.UInteger(title.Range.End.Character),
				},
			},
		}
		symbols = append(symbols, symbol)
	}

	// Add variables as symbols
	for _, variable := range doc.Variables {
		symbol := protocol.DocumentSymbol{
			Name:   variable.Name,
			Detail: &variable.Type,
			Kind:   protocol.SymbolKindVariable,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(variable.Range.Start.Line),
					Character: protocol.UInteger(variable.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(variable.Range.End.Line),
					Character: protocol.UInteger(variable.Range.End.Character),
				},
			},
			SelectionRange: protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(variable.Range.Start.Line),
					Character: protocol.UInteger(variable.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(variable.Range.End.Line),
					Character: protocol.UInteger(variable.Range.End.Character),
				},
			},
		}
		symbols = append(symbols, symbol)
	}

	slog.Info("Document symbols generated",
		"uri", uri,
		"titleCount", len(doc.Titles),
		"variableCount", len(doc.Variables),
		"totalSymbols", len(symbols),
		"duration", time.Since(start))

	if debugMode {
		slog.Debug("Document symbols result", "uri", uri, "symbols", symbols)
	}

	return symbols, nil
}

func textDocumentSemanticTokensFull(context *glsp.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	start := time.Now()
	uri := params.TextDocument.URI

	slog.Info("LSP textDocument/semanticTokens/full received", "uri", uri)

	if debugMode {
		slog.Debug("Semantic tokens params", "params", params)
	}

	doc, exists := documents.GetDocument(uri)
	if !exists {
		slog.Warn("Document not found for semantic tokens", "uri", uri)
		return &protocol.SemanticTokens{Data: []protocol.UInteger{}}, nil
	}

	tokens := generateGranularSemanticTokens(doc)

	result := &protocol.SemanticTokens{
		Data: tokens,
	}

	slog.Info("Semantic tokens generated",
		"uri", uri,
		"tokenCount", len(tokens)/5, // Each semantic token is 5 values
		"duration", time.Since(start))

	if debugMode {
		slog.Debug("Semantic tokens result", "uri", uri, "tokenData", tokens)
	}

	return result, nil
}

// publishDiagnostics sends diagnostic information to the client
func publishDiagnostics(context *glsp.Context, doc *Document) {
	start := time.Now()

	// Initialize with an empty array (never nil) to avoid client errors
	diagnostics := []protocol.Diagnostic{}

	// Convert parse errors to diagnostics
	for _, parseError := range doc.Errors {
		severity := protocol.DiagnosticSeverityError
		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      protocol.UInteger(parseError.Range.Start.Line),
					Character: protocol.UInteger(parseError.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      protocol.UInteger(parseError.Range.End.Line),
					Character: protocol.UInteger(parseError.Range.End.Character),
				},
			},
			Severity: &severity, // Use error severity
			Source:   ptrconv.Str("agentflow"),
			Message:  parseError.Message,
			Code:     &protocol.IntegerOrString{Value: parseError.Code},
		}
		diagnostics = append(diagnostics, diagnostic)
	}

	publishParams := protocol.PublishDiagnosticsParams{
		URI: doc.URI,
		// Version:     &doc.Version, // optional
		Diagnostics: diagnostics,
	}

	context.Notify(protocol.ServerTextDocumentPublishDiagnostics, publishParams)

	slog.Info("Diagnostics published",
		"uri", doc.URI,
		"diagnosticCount", len(diagnostics),
		"duration", time.Since(start))

	if debugMode {
		slog.Debug("Published diagnostics", "uri", doc.URI, "diagnostics", diagnostics)
	}
}

// Helper function since strings.Join isn't available here
func joinString(slice []string, sep string) string {
	if len(slice) == 0 {
		return ""
	}
	if len(slice) == 1 {
		return slice[0]
	}
	result := slice[0]
	for i := 1; i < len(slice); i++ {
		result += sep + slice[i]
	}
	return result
}

// getClientName extracts client name from client info
func getClientName(clientInfo interface{}) string {
	if clientInfo == nil {
		return "unknown"
	}
	if info, ok := clientInfo.(map[string]interface{}); ok {
		if name, exists := info["name"]; exists {
			if nameStr, ok := name.(string); ok {
				return nameStr
			}
		}
	}
	return "unknown"
}

func protocolPositionToPosition(pos protocol.Position) Position {
	return Position{
		Line:      protocolUIntegerToInt(pos.Line),
		Character: protocolUIntegerToInt(pos.Character),
	}
}

func protocolUIntegerToInt(value protocol.UInteger) int {
	if uint64(value) > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}

func protocolIntegerToInt(value protocol.Integer) int {
	if value < 0 {
		return 0
	}
	if uint64(value) > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}

func parseChangeEventFromMap(change map[string]any, uri string, changeIndex int) (TextDocumentContentChangeEvent, bool) {
	text, textOk := change["text"].(string)
	if !textOk {
		slog.Warn("Ignoring map change without string text",
			"uri", uri,
			"changeIndex", changeIndex)
		return TextDocumentContentChangeEvent{}, false
	}

	changeEvent := TextDocumentContentChangeEvent{Text: text}
	slog.Debug("Extracted text from map change",
		"uri", uri,
		"changeIndex", changeIndex,
		"textLength", len(text))

	if parsedRange, ok := parseRangeFromMap(change); ok {
		changeEvent.Range = parsedRange
		slog.Debug("Extracted range from map change",
			"uri", uri,
			"changeIndex", changeIndex,
			"startLine", parsedRange.Start.Line,
			"startChar", parsedRange.Start.Character,
			"endLine", parsedRange.End.Line,
			"endChar", parsedRange.End.Character)
	}

	if rangeLengthData, rangeLengthOk := change["rangeLength"]; rangeLengthOk && rangeLengthData != nil {
		if rangeLengthFloat, ok := rangeLengthData.(float64); ok {
			rangeLength, rangeLengthOK := jsonFloatToInt(rangeLengthFloat)
			if !rangeLengthOK {
				slog.Warn("Ignoring invalid range length from map change",
					"uri", uri,
					"changeIndex", changeIndex,
					"rangeLength", rangeLengthFloat)
			} else {
				changeEvent.RangeLength = &rangeLength
				slog.Debug("Extracted range length from map change",
					"uri", uri,
					"changeIndex", changeIndex,
					"rangeLength", rangeLength)
			}
		}
	}

	return changeEvent, true
}

func parseRangeFromMap(change map[string]any) (*Range, bool) {
	rangeData, ok := change["range"]
	if !ok || rangeData == nil {
		return nil, false
	}

	rangeMap, ok := rangeData.(map[string]any)
	if !ok {
		return nil, false
	}

	start, ok := parsePositionMap(rangeMap["start"])
	if !ok {
		return nil, false
	}

	end, ok := parsePositionMap(rangeMap["end"])
	if !ok {
		return nil, false
	}

	return &Range{Start: start, End: end}, true
}

func parsePositionMap(raw any) (Position, bool) {
	positionMap, ok := raw.(map[string]any)
	if !ok {
		return Position{}, false
	}

	line, ok := jsonNumberField(positionMap, "line")
	if !ok {
		return Position{}, false
	}

	character, ok := jsonNumberField(positionMap, "character")
	if !ok {
		return Position{}, false
	}

	return Position{Line: line, Character: character}, true
}

func jsonNumberField(values map[string]any, key string) (int, bool) {
	raw, ok := values[key]
	if !ok {
		return 0, false
	}

	value, ok := raw.(float64)
	if !ok {
		return 0, false
	}

	return jsonFloatToInt(value)
}

func jsonFloatToInt(value float64) (int, bool) {
	if value < 0 || value > maxJSONPositionValue {
		return 0, false
	}
	if math.Trunc(value) != value {
		return 0, false
	}
	return int(value), true
}
