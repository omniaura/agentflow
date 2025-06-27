# AgentFlow LSP Semantic Tokens Integration

This document provides implementation details for integrating AgentFlow's semantic tokens with the VSCode extension for enhanced syntax highlighting.

## Overview

The AgentFlow LSP server now supports **Semantic Tokens** (LSP 3.16+), which enables rich, context-aware syntax highlighting in VSCode. This goes beyond basic TextMate grammars by providing token classification based on the actual parsed syntax tree.

### What's Implemented

- ✅ **Full Document Semantic Tokens** (`textDocument/semanticTokens/full`)
- ✅ **10 Token Types** for different AgentFlow syntax elements  
- ✅ **4 Token Modifiers** for additional styling context
- ✅ **Real-time Updates** when documents change
- ⚠️ **Range-based tokens** not yet implemented (full document only)

## Token Types

The LSP server defines these token types in order (indices 0-9):

| Index | Token Type    | AgentFlow Usage                    | Example                           |
|-------|---------------|-----------------------------------|-----------------------------------|
| 0     | `keyword`     | `.title` directives, `<else>` tags | `.title System Prompt`           |
| 1     | `variable`    | Variable references               | `<!username>`, `<!user.age>`     |
| 2     | `string`      | Regular text content              | `Hello, world!`                   |
| 3     | `comment`     | Comments (future use)             | `<!-- comment -->`                |
| 4     | `operator`    | Tag operators, end tags           | `<`, `>`, `</tag>`                |
| 5     | `type`        | Type annotations (future use)     | `<!age int>`                      |
| 6     | `parameter`   | Variable parameters (future use)  | Variable parts in complex vars   |
| 7     | `decorator`   | Decorative elements               | `.` prefix in `.title`            |
| 8     | `function`    | Conditional blocks                | `<?user.premium bool>`            |
| 9     | `property`    | Variable path segments            | `user` in `<!user.name>`          |

## Token Modifiers

The LSP server defines these token modifiers as bit flags:

| Bit | Modifier        | Usage                               |
|-----|-----------------|-------------------------------------|
| 0   | `declaration`   | Applied to `.title` directives     |
| 1   | `definition`    | Variable definitions (future use)  |
| 2   | `readonly`      | Read-only elements (future use)    |
| 3   | `documentation` | Documentation elements (future use)|

## AgentFlow Syntax Mapping

### Title Directives
```agentflow
.title System Prompt
```
- **`.title`** → Token type `keyword` with `declaration` modifier
- **`System Prompt`** → Token type `string`

### Variable References
```agentflow
Hello <!username>! You have <!message_count int> messages.
```
- **`Hello `** → Token type `string`
- **`<!username>`** → Token type `variable`
- **`! You have `** → Token type `string`
- **`<!message_count int>`** → Token type `variable`
- **` messages.`** → Token type `string`

### Conditional Blocks
```agentflow
<?user.premium bool>
Premium content here
<else>
Standard content
</user.premium>
```
- **`<?user.premium bool>`** → Token type `function`
- **`Premium content here`** → Token type `string`
- **`<else>`** → Token type `keyword`
- **`Standard content`** → Token type `string`
- **`</user.premium>`** → Token type `operator`

## VSCode Extension Integration

### 1. Enable Semantic Tokens

In your VSCode extension's `package.json`, ensure semantic tokens are enabled:

```json
{
  "contributes": {
    "languages": [
      {
        "id": "agentflow",
        "extensions": [".af"],
        "configuration": "./language-configuration.json"
      }
    ],
    "grammars": [
      {
        "language": "agentflow",
        "scopeName": "source.agentflow",
        "path": "./syntaxes/agentflow.tmLanguage.json"
      }
    ]
  },
  "activationEvents": [
    "onLanguage:agentflow"
  ]
}
```

### 2. Language Client Configuration

Configure your language client to request semantic tokens:

```typescript
import { LanguageClient, SemanticTokensFeature } from 'vscode-languageclient/node';

const client = new LanguageClient(
  'agentflow-lsp',
  'AgentFlow Language Server',
  serverOptions,
  {
    documentSelector: [{ scheme: 'file', language: 'agentflow' }],
    // Enable semantic tokens
    initializationOptions: {},
  }
);

// Register semantic tokens feature
client.registerFeature(new SemanticTokensFeature(client));
```

### 3. Token Type Mapping

Map the LSP token types to VSCode semantic token types in your client:

```typescript
const tokenTypes = [
  'keyword',     // 0 - .title, <else>
  'variable',    // 1 - <!variable>
  'string',      // 2 - text content
  'comment',     // 3 - comments
  'operator',    // 4 - <, >, </tag>
  'type',        // 5 - type annotations
  'parameter',   // 6 - parameters
  'decorator',   // 7 - decorative elements
  'function',    // 8 - conditional blocks
  'property',    // 9 - variable path segments
];

const tokenModifiers = [
  'declaration',    // 0 - .title directives
  'definition',     // 1 - definitions
  'readonly',       // 2 - readonly elements
  'documentation',  // 3 - documentation
];
```

### 4. Theme Integration

The semantic tokens will automatically work with VSCode themes that support semantic highlighting. Users can customize colors in their `settings.json`:

```json
{
  "editor.semanticTokenColorCustomizations": {
    "rules": {
      "keyword": "#569cd6",
      "variable": "#9cdcfe",
      "string": "#ce9178",
      "function": "#dcdcaa",
      "operator": "#d4d4d4",
      "property": "#4fc1ff"
    }
  }
}
```

## Testing Semantic Tokens

### Test File Example

Create a test file `test.af`:

```agentflow
.title System Prompt
You are a helpful AI assistant named <!assistant_name>.

.title User Greeting
<?user.is_premium bool>
Welcome back, premium user <!user.name>!
<else>
Hello <!user.name>, thanks for using our service.
</user.is_premium>

Your account has <!account.message_count int> messages.
```

### Expected Token Highlighting

- **`.title`** should appear as keywords (blue)
- **`<!assistant_name>`**, **`<!user.name>`**, **`<!account.message_count int>`** should appear as variables (light blue)
- **`<?user.is_premium bool>`** should appear as a function (yellow)
- **`<else>`** should appear as a keyword (blue)
- Regular text should appear as strings (orange/brown)

### Debugging

To debug semantic tokens in VSCode:

1. Open Command Palette (`Cmd+Shift+P`)
2. Run "Developer: Inspect Editor Tokens and Scopes"
3. Click on different parts of your `.af` file
4. Verify the semantic token types are correctly assigned

## LSP Server Configuration

The AgentFlow LSP server automatically advertises semantic tokens capability:

```json
{
  "capabilities": {
    "semanticTokensProvider": {
      "legend": {
        "tokenTypes": ["keyword", "variable", "string", ...],
        "tokenModifiers": ["declaration", "definition", ...]
      },
      "range": false,
      "full": true
    }
  }
}
```

## Fallback Behavior

If semantic tokens are not available or supported:
- The extension should fall back to TextMate grammar highlighting
- Basic syntax highlighting will still work
- No errors should be thrown

## Performance Considerations

- Semantic tokens are generated in real-time as documents change
- The LSP server uses delta encoding for efficient transmission
- Large documents may have a slight delay in highlighting updates
- Consider implementing range-based tokens for very large files (future enhancement)

## Future Enhancements

Planned improvements to the semantic tokens implementation:

1. **Range-based tokens** for better performance on large files
2. **Incremental updates** with delta encoding
3. **More granular token types** for:
   - Type annotations (`int`, `bool`, `string`)
   - Operators (`eq`, `ne`, `gt`, `lt`, etc.)
   - Variable path segments
4. **Hover integration** with semantic token context
5. **Error highlighting** for invalid syntax

## Troubleshooting

### Common Issues

**Semantic tokens not appearing:**
1. Verify LSP server is running and connected
2. Check that `semanticTokensProvider` capability is advertised
3. Ensure VSCode semantic highlighting is enabled in settings

**Wrong colors:**
1. Check theme supports semantic tokens
2. Verify token type mappings in client
3. Check user's semantic token color customizations

**Performance issues:**
1. Monitor LSP server logs for timing information
2. Consider implementing range-based requests
3. Check for memory leaks in token generation

### Debug Commands

Test the LSP server directly:

```bash
# Start LSP server in debug mode
./af lsp --debug

# Test semantic tokens request (using your preferred LSP client)
# Request: textDocument/semanticTokens/full
# Params: { "textDocument": { "uri": "file:///path/to/test.af" } }
```

## Contact

For questions about this implementation:
- Check the LSP server logs for debugging information
- Review the source code in `pkg/lsp/document.go` (`generateSemanticTokens`)
- Test with the example files in `examples/`

---

**Last Updated:** $(date +%Y-%m-%d)  
**LSP Server Version:** 0.1.0  
**Supported LSP Version:** 3.16+ 