# AgentFlow Documentation

This directory contains documentation for integrating AgentFlow language features with editors and IDEs.

## Files

### [vscode-semantic-tokens.md](./vscode-semantic-tokens.md)
**Comprehensive implementation guide** for VSCode extension developers to add semantic tokens support for syntax highlighting. Includes:
- Complete technical specifications
- Token type and modifier mappings  
- Step-by-step implementation instructions
- Troubleshooting and debugging guide
- Performance considerations and future enhancements

### [semantic-tokens-quick-reference.md](./semantic-tokens-quick-reference.md)
**Quick reference guide** with essential information for implementing semantic tokens:
- Token type constants and mappings
- Ready-to-use code snippets
- Configuration examples
- Test procedures

### [test-semantic-tokens.af](./test-semantic-tokens.af)
**Comprehensive test file** containing all AgentFlow syntax elements for validating semantic tokens implementation:
- All token types (keywords, variables, conditionals, etc.)
- Complex nested structures
- Real-world usage patterns
- Edge cases and variations

## Usage

1. **For VSCode extension developers**: Start with `vscode-semantic-tokens.md` for complete implementation details
2. **For quick implementation**: Use `semantic-tokens-quick-reference.md` for essential code snippets  
3. **For testing**: Open `test-semantic-tokens.af` in your editor to verify syntax highlighting works correctly

## AgentFlow LSP Server

The semantic tokens are provided by the AgentFlow LSP server (`./af lsp`). The server:
- Automatically advertises semantic tokens capability
- Supports real-time token updates as documents change
- Provides detailed logging for debugging (`--debug` flag)
- Works with any LSP-compatible editor

## Supported LSP Version

- **Minimum**: LSP 3.16+ (for semantic tokens support)
- **Recommended**: Latest LSP version for best compatibility

## Testing Your Implementation

1. Start the AgentFlow LSP server: `./af lsp --debug`
2. Open `test-semantic-tokens.af` in your editor
3. Verify syntax highlighting appears correctly
4. Use VSCode's "Developer: Inspect Editor Tokens and Scopes" to debug token types

## Support

For questions about the LSP implementation:
- Check the server logs for debugging information
- Review the source code in `pkg/lsp/`
- Test with the examples in `examples/` 