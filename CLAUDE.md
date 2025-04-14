# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands
- Build and run: `just go [ARGS]`
- Install: `just install` or `go install github.com/omniaura/agentflow/cmd/af@latest`
- Format code: `just fmt`
- Run tests: `just test [ARGS]`
- Run specific test: `just test -run=TestName`

## Code Style Guidelines
- **Imports**: Standard library first, blank line, then third-party imports, alphabetically ordered
- **Formatting**: Use tabs for indentation, align parameters and fields
- **Types**: Use descriptive type names, with slice types defined explicitly
- **Naming**: CamelCase for exported identifiers, lowercase for unexported, short names for limited scope
- **Error Handling**: Use custom errs package with E(), ES(), EF() methods
- **Testing**: Use table-driven tests with descriptive case names, require package for assertions
- **Documentation**: Provide package-level docs, include TODOs for improvements
- **Structure**: Maintain separation of packages (ast, token, gen), with tests alongside implementation