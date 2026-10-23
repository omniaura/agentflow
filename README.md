# agentflow
[![Godoc Reference](https://godoc.org/github.com/omniaura/agentflow?status.svg)](http://godoc.org/github.com/omniaura/agentflow)
[![Go Coverage](https://github.com/omniaura/agentflow/wiki/coverage.svg)](https://raw.githack.com/wiki/omniaura/agentflow/coverage.html)
[![Go Report Card](https://goreportcard.com/badge/github.com/omniaura/agentflow)](https://goreportcard.com/report/github.com/omniaura/agentflow)

A powerful template engine for AI prompt engineering with type-safe variable interpolation and conditional logic.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [AgentFlow (.af) Syntax](#agentflow-af-syntax)
  - [Titles](#titles)
  - [Variables](#variables)
  - [Type System](#type-system)
  - [Conditional Blocks](#conditional-blocks)
  - [Nested Variables](#nested-variables)
  - [Comments and Text](#comments-and-text)
  - [Tools](#tools)
  - [Routes](#routes)
- [Examples](#examples)
  - [Basic Variables](#basic-variables)
  - [Typed Variables](#typed-variables)
  - [Conditional Prompts](#conditional-prompts)
  - [Complex Nested Structures](#complex-nested-structures)
- [Generated Code](#generated-code)
- [go:generate Workflow](#gogenerate-workflow)
- [Checking Generated Code in CI](#checking-generated-code-in-ci)
- [LSP Server](#lsp-server)
- [Agent Skills](#agent-skills)
- [Ejecting the Runtime](#ejecting-the-runtime)
- [CLI Usage](#cli-usage)
- [Releases](#releases)

## Installation

    go install github.com/omniaura/agentflow/cmd/af@latest

As of go 1.24 you can now use `af` as a go tool:

    go get -tool github.com/omniaura/agentflow/cmd/af@latest

This installs `af` scoped to your go project and you call it using `go tool af`

## Quick Start

Create a simple prompt template:

```af
.title Greeting
Hello <!name>! Welcome to <!platform>.

.title Personalized Message  
<?premium bool>
Thanks for being a premium user! You have access to all features.
<else>
Consider upgrading to premium for additional features.
</premium>
```

Generate Go code:

    af gen prompts examples/

Or, if you pin AgentFlow as a project-scoped Go tool:

    go tool af gen prompts examples/

## AgentFlow (.af) Syntax

AgentFlow uses a simple but powerful syntax for creating dynamic prompt templates with type-safe variable interpolation.

### Titles

Titles define distinct prompt sections within a file. Each title becomes a separate Go function.

```af
.title System Prompt
You are a helpful assistant.

.title User Greeting
Hello <!user_name>, how can I help you today?
```

**Generated Go:**
```go
type SystemPrompt struct{}
func (input *SystemPrompt) String() string { /* ... */ }

type UserGreeting struct {
    UserName string
}
func (input *UserGreeting) String() string { /* ... */ }
```

### Variables

Variables are interpolated using the `<!variable_name>` syntax:

```af
Hello <!name>! You have <!count> messages.
```

Variables support dot notation for nested structures:

```af
Welcome <!user.name>! Your email is <!user.email>.
```

### Type System

AgentFlow supports a rich type system with explicit type annotations and intelligent type caching.

#### Supported Types
- `string` (default)
- `int` 
- `bool`
- `float32`
- `float64`

#### Type Declaration and Caching

Variables can be declared with explicit types on first use:

```af
User <!username> has <!count int> messages and is <!active bool> status.
Message Count: <!count>
Active Status: <!active>
```

**Key Features:**
- **Type Declaration**: `<!count int>` - declares variable with type
- **Type Caching**: `<!count>` - subsequent references reuse the cached type
- **Automatic Conversion**: Generates appropriate `strconv.Itoa()` and `strconv.FormatBool()` calls

**Generated Go:**
```go
type Example struct {
    Username string
    Count    int      // ✅ Correctly typed
    Active   bool     // ✅ Correctly typed  
}

func (input *Example) String() string {
    var b strings.Builder
    var0 := strconv.Itoa(input.Count)  // ✅ Efficient reuse
    // ... both references use var0
}
```

### Conditional Blocks

Conditional blocks allow content to be included based on variable values:

```af
<?user.premium bool>
Welcome premium user! You have access to all features.
Your tier: <!user.subscription.tier int>.
<else>
Welcome! Upgrade to premium for more features.
</user.premium>
```

**Syntax:**
- `<?variable_name>` - Start conditional block
- `<else>` - Alternative content (optional)
- `</variable_name>` - End conditional block

**Conditional Logic:**
- `string` variables: included if not empty (`!= ""`)
- `int` variables: included if not zero (`!= 0`) 
- `bool` variables: included if true

### Nested Variables

AgentFlow supports complex nested data structures:

```af
<?memory.long>
## Long Term Memory
<!memory.long>
</memory.long>

<?script.name>
Working on: <!script.name> (<!script.type> project)
</script.name>
```

**Generated Go struct:**
```go
type Template struct {
    Memory struct {
        Long  string
        Short string
    }
    Script struct {
        Name string
        Type string
    }
}
```

### Comments and Text

All text outside of special syntax is treated as literal content. AgentFlow preserves whitespace and formatting exactly as written.

```af
This is literal text that will be included as-is.

Variables like <!name> are interpolated.
But this <text> is just literal text.
```

### Tools

A `.tool` section declares a tool an LLM can call. AgentFlow generates its JSON Schema, a typed `Args` struct and a strict decoder from one declaration, so the schema the model sees and the struct your handler reads can't drift apart.

```af
.tool offer_refund
.param order_id The order to refund.
.param reason enum(damaged, late, wrong_item, other) Why the customer wants a refund.
.param lines[].sku The line's SKU.
.param lines[].quantity int How many units to refund.
.param note? A note for the support team.

Show the customer a refund offer they must accept.
```

Lines starting with a directive declare the tool. Everything else in the section is the description the model reads.

Parameters work like `<!variables>`: nothing is declared up front, and AgentFlow infers the structure from what you write.

- `.param <name> [type] [description]` declares a parameter. The type defaults to `string`.
- A `?` on the name or the type makes a parameter optional: `.param note? ...` or `.param limit int? ...`.
- Types: `string`, `int`, `float`, `bool`, `enum(a, b, c)`, `object`, `any`, or `[]` of any of them.
- Dotted names imply objects: `.param filter.since` makes `filter` an object, and `.param lines[].sku` makes `lines` an array of objects. An implied object is required when any of its properties is. To give it a description or make it optional, declare it too, anywhere in the section: `.param lines? []object Refund only these lines.`
- `.maxbytes <n>` caps the size of the arguments (default 64 KiB).

`af lint` warns (AF012) when a description starts with what looks like a misspelled type, such as `strng`.

A section ends at the next `.title` or `.tool` line.

**Generated Go:**

```go
type OfferRefundArgs struct {
	OrderId string                 `json:"order_id"`
	Reason  string                 `json:"reason"`
	Lines   []OfferRefundArgsLines `json:"lines,omitempty"`
}

const (
	OfferRefundReasonDamaged   = "damaged"
	OfferRefundReasonLate      = "late"
	// ...
)

var OfferRefundTool = &aftool.Def[OfferRefundArgs]{Name: "offer_refund", Description: "...", Schema: ..., Shape: ...}
```

Use the definition to advertise the tool and to decode each call:

```go
tools := []json.RawMessage{OfferRefundTool.OpenAI()} // or OfferRefundTool.Anthropic()

args, err := OfferRefundTool.Decode(call.Arguments)
if err != nil {
	return err.Error() // e.g. offer_refund: invalid arguments: reason: "meh" is not one of damaged, late, wrong_item, other
}
```

`Decode` rejects arguments that are too large, unknown or missing properties, null required values, wrong types and values outside an enum. Every error is an `*aftool.ArgsError` worded for the model, so you can return it as the tool result and let the model fix its call. The runtime package `github.com/omniaura/agentflow/runtime/aftool` has no dependencies.

### Routes

A `.route` section serves a [sqlc](https://sqlc.dev) query over HTTP. You write the SQL. sqlc generates the typed query. One line of `.af` generates the handler: authentication, argument binding, strict decoding, the query call, and the JSON response or error.

```af
.route POST /todos CreateTodo
.auth owner_id
.status 201
Creates a todo for the caller.

.route GET /todos/{id} GetTodo
.auth owner_id

.route GET /todos ListTodos
.auth owner_id

.route DELETE /todos/{id} DeleteTodo
.auth owner_id

.route GET /stats/count CountTodos
.public
```

`.route <METHOD> <path> <Query>` names a query sqlc generated. AgentFlow finds `sqlc.yaml` (or `sqlc.yml` or `sqlc.json`) at or above the `.af` file, or anywhere in the module, and reads the queries from sqlc's Go output. If several sqlc packages define the same name, qualify it as `pkg.Query`.

Nothing else is declared. Each query argument is bound by name, with `user_id`, `userId` and `UserID` all treated as one name, in this order:

1. a `{path}` parameter
2. an `.auth` name, read from the authenticated caller
3. the JSON body, or the query string for `GET`, `HEAD` and `DELETE`

Arguments named by the path or by `.auth` never come from the request body, so a client can't set `owner_id`.

| Directive | Meaning |
| --- | --- |
| `.auth a, b` | Arguments read from the caller's fields. |
| `.public` | Skip authentication. Routes authenticate by default. |
| `.status 201` | The success status. |
| `.maxbytes n` | Cap the request body (default 1 MiB). |

The text that isn't a directive becomes the handler's doc comment.

| Query kind | Response |
| --- | --- |
| `:one` | the row as JSON, or 404 when there is none |
| `:many` | a JSON array, `[]` when empty |
| `:exec` | 204 |
| `:execrows` | 204, or 404 when no row changed |

Bodies reject unknown fields and trailing data. sqlc's `[]byte` json and jsonb arguments take raw JSON.

**Generated Go:** a `TodosQueries` interface that sqlc's `*Queries` satisfies (and a test fake can), and a `TodosRoutes` type:

```go
type Caller struct{ OwnerID string } // the fields .auth names read

routes := &TodosRoutes{
	Queries: db.New(pool),
	Auth: func(r *http.Request) (Caller, error) { return authenticate(r) },
	// OnError is optional: plug in your own error format and logging.
}
routes.Register(mux)
```

Errors are written as `{"error": "..."}` with their status: 400 for bad input, 401 when `Auth` fails (or the status of an `*afhttp.Error` it returns), 404, 413, and 500 for query failures. The client never sees the cause of a 500. The runtime is `github.com/omniaura/agentflow/runtime/afhttp`, which has no dependencies. See [`examples/routes`](examples/routes), which is tested against Postgres in CI.

## Examples

### Basic Variables

```af
.title Simple Greeting
Hello <!name>! Today is <!date>.
```

### Typed Variables

```af
.title User Stats
User <!username> has <!message_count int> messages.
Premium status: <!is_premium bool>
Account balance: $<!balance float64>
```

### Conditional Prompts

```af
.title Welcome Message
<?user.premium bool>
🌟 Welcome back, premium user <!user.name>!
You have access to all premium features.
<else>
Hello <!user.name>! Consider upgrading to premium.
</user.premium>

<?notifications int>
You have <!notifications> new notifications.
</notifications>
```

### Complex Nested Structures

```af
.title AI Assistant
You are <!ai.name>, an AI assistant.

<?user.preferences.language>
Respond in <!user.preferences.language>.
</user.preferences.language>

<?context.previous_messages>
Previous conversation:
<!context.previous_messages>
</context.previous_messages>

Current user: <!user.name>
Timestamp: <!timestamp>
```

## Generated Code

AgentFlow generates efficient, type-safe Go code with:

- **Two-pass string building** for optimal performance
- **Proper type handling** with automatic conversions
- **Conditional logic** that mirrors your template structure
- **Variable reuse** to minimize allocations

Example generated code:
```go
func (input *Template) String() string {
    var b strings.Builder
    var0 := strconv.Itoa(input.Count)
    length := 0
    length += 5  // "User "
    length += len(input.Username)
    length += 5  // " has "
    length += len(var0)
    length += 10 // " messages."
    b.Grow(length)
    b.WriteString(`User `)
    b.WriteString(input.Username)
    b.WriteString(` has `)
    b.WriteString(var0)
    b.WriteString(` messages.`)
    return b.String()
}
```

## go:generate Workflow

AgentFlow works well as a checked-in Go tool dependency so prompt generation is repeatable for every contributor and CI job.

1. Add the tool to your module:

       go get -tool github.com/omniaura/agentflow/cmd/af@latest

2. Add a generate directive next to your `.af` files:

```go
//go:generate go tool af gen prompts .
```

3. Regenerate code whenever templates change:

       go generate ./...

When `go generate` runs, AgentFlow respects `$GOPACKAGE`, so generating files from a package root works without extra flags.

## Checking Generated Code in CI

`af check` fails when committed `_af.go` files are not what your `.af` sources and your pinned af version generate. It regenerates every file in memory and reports:

- **drift**: an edited `.af` file or a hand-edited `_af.go`, shown as a unified diff
- **missing**: a `.af` file without its `_af.go`
- **orphan**: an agentflow-generated `_af.go` whose `.af` file was deleted
- **unformatted**: a `.af` file that `af fmt --write` would change
- **lint**: errors from `af lint`
- **generation errors**, as `file:line:col: message`

Add one step to your CI job:

```yaml
- run: go tool af check ./...
```

It ends with a count line, such as `ok checked=17 drift=0 unformatted=0 lint_errors=0 gen_errors=0`, and exits non-zero on any finding. It also fails if it finds no `.af` files, so a wrong path can't pass silently. Patterns work like the go tool: `dir` checks one directory, and `dir/...` recurses, skipping `vendor`, `testdata` and directories starting with `.` or `_`. The default is `./...`.

Make sure your CI path filters include `*.af`, so a change to a prompt alone still runs the check.

Flags:

- `--json` - print the report as JSON for tooling
- `--ignore-version` - ignore the af version in generated-code headers

## LSP Server

AgentFlow ships with a language server for `.af` files.

Start it over stdio for editor integrations:

    af lsp

Or, if you use the Go tool workflow:

    go tool af lsp

Available flags:

- `--mode stdio|tcp` - transport to use; `stdio` is the default for editors
- `--port 4389` - TCP port when `--mode tcp` is selected
- `--debug` - verbose logging to stderr

Editor features:

- diagnostics from the parser and `af lint`, with their codes (AF001…), errors and warnings
- formatting with `af fmt` (`textDocument/formatting`)
- go to definition:
  - a variable to its first typed use
  - a loop alias to its loop
  - a `.title` or `.tool` to the Go code generated for it
  - a `.route` query to the sqlc method that runs it
- rename for variable path segments and loop aliases
- hover for variables, operators, `.tool` parameters, and `.route` bindings. A route's hover shows where each query argument comes from: path, caller, body or query string.
- completion for directives (aware of the `.tool` or `.route` section you're in), variables, types, `.param` types and operators
- document symbols for prompts, variables, tools with their parameters, and routes
- semantic tokens

Positions are UTF-16, as LSP requires, so emoji and non-ASCII text in prompts don't shift columns.

**Neovim** (0.11+):

```lua
vim.filetype.add({ extension = { af = "agentflow" } })
vim.lsp.config("agentflow", {
  cmd = { "go", "tool", "af", "lsp" }, -- or { "af", "lsp" }
  filetypes = { "agentflow" },
  root_markers = { "go.mod" },
})
vim.lsp.enable("agentflow")
```

**Helix** (`languages.toml`):

```toml
[language-server.agentflow]
command = "go"
args = ["tool", "af", "lsp"]

[[language]]
name = "agentflow"
scope = "source.agentflow"
file-types = ["af"]
roots = ["go.mod"]
language-servers = ["agentflow"]
```

The companion VS Code extension lives at `https://github.com/omniaura/agentflow-vscode` and can launch the LSP automatically.

## Agent Skills

`af` ships the agent skill that teaches coding agents such as Claude Code and Codex to write `.af` files: the syntax, the generated Go API, and the fmt, gen and check workflow. Install it into your repository:

    go tool af skills install

This writes `.agents/skills/agentflow/SKILL.md`. It also writes `.claude/skills/agentflow/SKILL.md` when the repository has a `.claude` directory that doesn't already share `.agents/skills` through a symlink. Use `--dir` to choose other directories.

Run it again after upgrading af to upgrade the skill. Each installed file carries a version stamp and a hash. A file you haven't edited is replaced. A file you have edited is kept, unless you pass `--force`.

`af skills print` writes the skill to stdout.

## Ejecting the Runtime

Code generated for `.tool` and `.route` imports small runtime packages, `aftool` and `afhttp`, from `github.com/omniaura/agentflow/runtime`. Like shadcn/ui, you can pop them out into your own repository and own them:

    go tool af eject          # or: af eject aftool
    go tool af gen prompts ./...

`af eject` copies the packages to `internal/agentflow/<package>` (choose another place with `--dir`) and records them in `agentflow.json` at the module root. From then on, `af gen` makes generated code import your copies. They have no dependencies, so a module that ejects everything doesn't need agentflow as a library at all.

Edit the copies freely. To upgrade them after upgrading af, run `af eject` again:

| Your copy | What happens |
| --- | --- |
| unedited | replaced with the new version |
| edited | merged three ways with the version it came from (fetched from the Go module cache), keeping your edits; overlapping changes are left as conflict markers and `af eject` exits non-zero |
| written by hand, or the original isn't available | kept, with the new version written next to it as `<file>.af-new` |

Files a new version drops are removed unless you've edited them. `--force` replaces edited files instead of merging. Each copied file records its af version and a content hash in an `agentflow-vendored:` comment. Leave that comment in place so upgrades can tell your edits apart.

## CLI Usage

Generate Go code from .af templates:

    af gen prompts [directory]

**Options:**
- `-d, --dir`: Directory containing .af files (default: current directory)

**Example:**

    af gen prompts examples/simple

Check generated code, formatting and lint (see [Checking Generated Code in CI](#checking-generated-code-in-ci)):

    af check ./...

Format and lint .af files:

    af fmt --write --dir .
    af lint --dir .

## Releases

### Typed slice loops

Use a Go slice type and a lexical alias:

    .title system
    <*extensions []Extension as extension>
    <!extension.name>: <!extension.text>
    </extensions>

    .title user
    <*names []string as name>Hello <!name>!</names>
    <*scores []int as score><!score>,</scores>

Declare Extension alongside the generated Go file. Exported fields use the
existing dot notation. Annotate non-string fields, for example
<!extension.priority int>. Nested loops retain access to outer aliases;
shadowing active aliases is rejected. Close each block with its source path.
Nil and empty slices render nothing; whitespace repeats exactly in slice order.
No separators are inserted. Conditions and their else branches work inside loops.

Local named Go types, pointers and nested slices are accepted as element types.
Use concrete struct slices for nested field access; Go compilation checks fields.
Direct non-string values use fmt.Sprint. Custom String methods retain their own
semantics. Nil pointers may be printed, but dereferencing one through nested
field access has ordinary Go panic behavior. Input strings are data and are
never parsed as templates. Non-loop templates retain existing generation.


Release history and changelogs are tracked in GitHub Releases:

- `https://github.com/omniaura/agentflow/releases`
