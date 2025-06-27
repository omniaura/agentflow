# AgentFlow Semantic Tokens - Quick Reference

## Token Types (in order, 0-9)

```typescript
const AGENTFLOW_TOKEN_TYPES = [
  'keyword',     // 0: .title, <else>
  'variable',    // 1: <!variable>
  'string',      // 2: text content  
  'comment',     // 3: comments
  'operator',    // 4: <, >, </tag>
  'type',        // 5: type annotations
  'parameter',   // 6: parameters
  'decorator',   // 7: decorative elements
  'function',    // 8: conditional blocks <?...>
  'property',    // 9: variable path segments
];
```

## Token Modifiers (bit flags)

```typescript
const AGENTFLOW_TOKEN_MODIFIERS = [
  'declaration',    // 0: .title directives
  'definition',     // 1: definitions  
  'readonly',       // 2: readonly elements
  'documentation',  // 3: documentation
];
```

## LSP Client Setup

```typescript
import { LanguageClient, SemanticTokensFeature } from 'vscode-languageclient/node';

// 1. Create language client with semantic tokens support
const client = new LanguageClient(
  'agentflow-lsp',
  'AgentFlow Language Server', 
  serverOptions,
  {
    documentSelector: [{ scheme: 'file', language: 'agentflow' }],
    initializationOptions: {},
  }
);

// 2. Register semantic tokens feature
client.registerFeature(new SemanticTokensFeature(client));

// 3. Start client
client.start();
```

## AgentFlow Syntax → Token Mapping

| AgentFlow Syntax | Token Type | Example |
|------------------|------------|---------|
| `.title Text` | `keyword` + `declaration` | `.title System Prompt` |
| `<!var>` | `variable` | `<!username>` |
| `<?condition>` | `function` | `<?user.premium bool>` |
| `<else>` | `keyword` | `<else>` |
| `</tag>` | `operator` | `</user.premium>` |
| `Regular text` | `string` | `Hello world` |

## Expected Colors (Default Dark+ Theme)

- **keyword** → Blue (`#569cd6`)
- **variable** → Light Blue (`#9cdcfe`)  
- **function** → Yellow (`#dcdcaa`)
- **string** → Orange (`#ce9178`)
- **operator** → White (`#d4d4d4`)

## Test File

Create `test.af`:
```agentflow
.title System Prompt
You are an AI assistant named <!assistant_name>.

.title Greeting  
<?user.is_premium bool>
Welcome premium user <!user.name>!
<else>
Hello <!user.name>!
</user.is_premium>
```

## Debugging

1. **Command Palette** → "Developer: Inspect Editor Tokens and Scopes"
2. Click tokens to verify semantic types
3. Check LSP server logs: `./af lsp --debug`

## Package.json Changes

```json
{
  "contributes": {
    "languages": [{
      "id": "agentflow",
      "extensions": [".af"]
    }]
  },
  "activationEvents": ["onLanguage:agentflow"]
}
```

## User Settings (Optional)

Users can customize colors:
```json
{
  "editor.semanticTokenColorCustomizations": {
    "rules": {
      "variable": "#4fc1ff",
      "function": "#ffd700"  
    }
  }
}
``` 