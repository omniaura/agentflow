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

import protocol "github.com/tliron/glsp/protocol_3_16"

// LSP Protocol structures
// Based on the Language Server Protocol specification: https://microsoft.github.io/language-server-protocol/

// Position represents a position in a text document
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a text range in a document
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location inside a resource
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a text document
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier extends TextDocumentIdentifier with version
type VersionedTextDocumentIdentifier struct {
	TextDocumentIdentifier
	Version int `json:"version"`
}

// TextDocumentItem represents a text document
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentContentChangeEvent describes a change to a text document
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength *int   `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// DiagnosticSeverity represents the severity of a diagnostic
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// Diagnostic represents a diagnostic message
type Diagnostic struct {
	Range              Range              `json:"range"`
	Severity           DiagnosticSeverity `json:"severity,omitempty"`
	Code               string             `json:"code,omitempty"`
	CodeDescription    string             `json:"codeDescription,omitempty"`
	Source             string             `json:"source,omitempty"`
	Message            string             `json:"message"`
	Tags               []int              `json:"tags,omitempty"`
	RelatedInformation []interface{}      `json:"relatedInformation,omitempty"`
	Data               interface{}        `json:"data,omitempty"`
}

// CompletionItem represents a completion suggestion
type CompletionItem struct {
	Label               string                      `json:"label"`
	Kind                protocol.CompletionItemKind `json:"kind,omitempty"`
	Tags                []int                       `json:"tags,omitempty"`
	Detail              string                      `json:"detail,omitempty"`
	Documentation       interface{}                 `json:"documentation,omitempty"`
	Deprecated          bool                        `json:"deprecated,omitempty"`
	Preselect           bool                        `json:"preselect,omitempty"`
	SortText            string                      `json:"sortText,omitempty"`
	FilterText          string                      `json:"filterText,omitempty"`
	InsertText          string                      `json:"insertText,omitempty"`
	InsertTextFormat    int                         `json:"insertTextFormat,omitempty"`
	InsertTextMode      int                         `json:"insertTextMode,omitempty"`
	TextEdit            interface{}                 `json:"textEdit,omitempty"`
	AdditionalTextEdits []interface{}               `json:"additionalTextEdits,omitempty"`
	CommitCharacters    []string                    `json:"commitCharacters,omitempty"`
	Command             interface{}                 `json:"command,omitempty"`
	Data                interface{}                 `json:"data,omitempty"`
}

// SymbolKind represents the kind of a document symbol
type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

// DocumentSymbol represents a symbol in a document
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Tags           []int            `json:"tags,omitempty"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// Hover represents hover information
type Hover struct {
	Contents interface{} `json:"contents"`
	Range    *Range      `json:"range,omitempty"`
}

// MarkupContent represents marked up content
type MarkupContent struct {
	Kind  string `json:"kind"` // "plaintext" or "markdown"
	Value string `json:"value"`
}

// LSP Request/Response parameters

// InitializeParams represents initialization parameters
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	ClientInfo            interface{}        `json:"clientInfo,omitempty"`
	Locale                string             `json:"locale,omitempty"`
	RootPath              string             `json:"rootPath,omitempty"`
	RootURI               string             `json:"rootUri"`
	InitializationOptions interface{}        `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	Trace                 string             `json:"trace,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// ClientCapabilities represents client capabilities
type ClientCapabilities struct {
	Workspace    interface{} `json:"workspace,omitempty"`
	TextDocument interface{} `json:"textDocument,omitempty"`
	Window       interface{} `json:"window,omitempty"`
	General      interface{} `json:"general,omitempty"`
	Experimental interface{} `json:"experimental,omitempty"`
}

// WorkspaceFolder represents a workspace folder
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ServerCapabilities represents server capabilities
type ServerCapabilities struct {
	TextDocumentSync                 interface{} `json:"textDocumentSync,omitempty"`
	CompletionProvider               interface{} `json:"completionProvider,omitempty"`
	HoverProvider                    bool        `json:"hoverProvider,omitempty"`
	SignatureHelpProvider            interface{} `json:"signatureHelpProvider,omitempty"`
	DeclarationProvider              interface{} `json:"declarationProvider,omitempty"`
	DefinitionProvider               bool        `json:"definitionProvider,omitempty"`
	TypeDefinitionProvider           interface{} `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider           interface{} `json:"implementationProvider,omitempty"`
	ReferencesProvider               bool        `json:"referencesProvider,omitempty"`
	DocumentHighlightProvider        bool        `json:"documentHighlightProvider,omitempty"`
	DocumentSymbolProvider           bool        `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider               interface{} `json:"codeActionProvider,omitempty"`
	CodeLensProvider                 interface{} `json:"codeLensProvider,omitempty"`
	DocumentLinkProvider             interface{} `json:"documentLinkProvider,omitempty"`
	ColorProvider                    interface{} `json:"colorProvider,omitempty"`
	DocumentFormattingProvider       bool        `json:"documentFormattingProvider,omitempty"`
	DocumentRangeFormattingProvider  bool        `json:"documentRangeFormattingProvider,omitempty"`
	DocumentOnTypeFormattingProvider interface{} `json:"documentOnTypeFormattingProvider,omitempty"`
	RenameProvider                   interface{} `json:"renameProvider,omitempty"`
	FoldingRangeProvider             interface{} `json:"foldingRangeProvider,omitempty"`
	ExecuteCommandProvider           interface{} `json:"executeCommandProvider,omitempty"`
	SelectionRangeProvider           interface{} `json:"selectionRangeProvider,omitempty"`
	LinkedEditingRangeProvider       interface{} `json:"linkedEditingRangeProvider,omitempty"`
	CallHierarchyProvider            interface{} `json:"callHierarchyProvider,omitempty"`
	SemanticTokensProvider           interface{} `json:"semanticTokensProvider,omitempty"`
	MonikerProvider                  interface{} `json:"monikerProvider,omitempty"`
	TypeHierarchyProvider            interface{} `json:"typeHierarchyProvider,omitempty"`
	InlineValueProvider              interface{} `json:"inlineValueProvider,omitempty"`
	InlayHintProvider                interface{} `json:"inlayHintProvider,omitempty"`
	DiagnosticProvider               interface{} `json:"diagnosticProvider,omitempty"`
	WorkspaceSymbolProvider          bool        `json:"workspaceSymbolProvider,omitempty"`
	Workspace                        interface{} `json:"workspace,omitempty"`
	Experimental                     interface{} `json:"experimental,omitempty"`
}

// InitializeResult represents the result of initialization
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   interface{}        `json:"serverInfo,omitempty"`
}

// DidOpenTextDocumentParams represents parameters for textDocument/didOpen
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams represents parameters for textDocument/didChange
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams represents parameters for textDocument/didClose
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// CompletionParams represents parameters for textDocument/completion
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      interface{}            `json:"context,omitempty"`
}

// HoverParams represents parameters for textDocument/hover
type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DocumentSymbolParams represents parameters for textDocument/documentSymbol
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// PublishDiagnosticsParams represents parameters for textDocument/publishDiagnostics
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Token types for AgentFlow
const (
	TokenTypeKeyword   = "keyword"
	TokenTypeVariable  = "variable"
	TokenTypeString    = "string"
	TokenTypeComment   = "comment"
	TokenTypeOperator  = "operator"
	TokenTypeType      = "type"
	TokenTypeParameter = "parameter"
	TokenTypeDecorator = "decorator"
	TokenTypeFunction  = "function"
	TokenTypeProperty  = "property"
	TokenTypeTagOpen   = "tagOpen"
	TokenTypeTagClose  = "tagClose"
)

// Token modifiers for AgentFlow
const (
	TokenModifierDeclaration    = "declaration"
	TokenModifierDefinition     = "definition"
	TokenModifierReadonly       = "readonly"
	TokenModifierStatic         = "static"
	TokenModifierDeprecated     = "deprecated"
	TokenModifierAsync          = "async"
	TokenModifierModification   = "modification"
	TokenModifierDocumentation  = "documentation"
	TokenModifierDefaultLibrary = "defaultLibrary"
)
