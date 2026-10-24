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
  - [Structured Outputs](#structured-outputs)
  - [Agents](#agents)
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
- [Upgrading](#upgrading)
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

#### Tools written in SQL

A tool that is one database query needs no Go at all:

```af
.tool find_notes
.param text The words to look for.
.auth owner_id

Finds the caller's notes that contain the text.

.sql :many
SELECT id, body, pinned FROM notes
WHERE owner_id = @owner_id AND strpos(lower(body), lower(@text)) > 0
```

`af gen` hands the query to sqlc as `FindNotesTool`, so a sqlc config must read the file's `_af.sql`. It then generates the tool from sqlc's output:
- **Parameters:** inferred from the query's arguments. Integers become `integer`, text stays `string`, and database types such as `pgtype.UUID` or `time.Time` are parsed from strings. Nullable arguments such as `sqlc.narg('done')` are optional and stay NULL when left out. `.param` is only needed for a description or a narrower type.
- **Caller arguments:** `.auth owner_id` takes the argument from the caller, so the model can't see or set it.
- **Implementation:** a `<File>SQLTools` struct implements the tools:

```go
tools := &AssistantSQLTools{
	Queries: db.New(pool),
	// The caller your auth middleware put in the context.
	Caller: func(ctx context.Context) (Caller, error) { return callerFrom(ctx) },
}
agent := &NotesAgent{Model: model, Tools: tools} // or embed it next to Go tools
```

A query you already have in a sqlc query file needs no copy. Link it with `.sqlc`:

```af
.tool finish_todo
.sqlc db/queries.sql SetTodoDone
.auth owner_id

Marks one of the caller's todos done or not done.

.tool search_todos
.sqlc db/search.sql
.auth owner_id

Finds the caller's todos whose title contains the text.

.tool list_todos
.sqlc ListTodos
.auth owner_id

Lists the caller's todos.
```

- `.sqlc file.sql Query` runs that query of the file. The path is relative to the `.af` file.
- `.sqlc file.sql` runs the file's only query, or the one named after the tool (`search_todos` runs `SearchTodos`).
- `.sqlc Query` runs a query sqlc generated from any of its files (`db.Query` names its package).

The file stays yours: `af gen` checks that a sqlc config reads it and that `sqlc generate` has generated the query, but doesn't write it or run sqlc for it. Parameters, `.auth` and `<File>SQLTools` work as for `.sql`, and go to definition on a `.sqlc` line opens the query.

Results are returned as follows:

| Query | The model receives |
| --- | --- |
| `:one` | the row, as JSON |
| `:many` | the rows, as JSON (`[]` when there are none) |
| `:exec` | `{"ok":true}` |
| `:execrows` | `{"rows_affected":n}` |
| no row, or zero rows changed | a `not found` error (another owner's row looks the same as a missing one) |
| a database or caller failure | `internal error` or `unauthorized` (`*aftool.Error`; the cause stays available to `OnEvent` and logs) |

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

**Writing the query in the route.** End a route with `.sql` and its sqlc command, and write the query below it. The endpoint and its SQL then live in one place:

```af
.route PATCH /notes/{id} UpdateNote
.auth owner_id
Replaces a note's body. The server sets updated_at.
.sql :execrows
UPDATE notes SET body = @body, updated_at = now()
WHERE id = @id AND owner_id = @owner_id
```

`af gen` writes the queries of `notes.af` to `notes_af.sql`. Add that file, or its directory, to the `queries` of your `sqlc.yaml`. When the queries change, `af gen` runs `sqlc generate` (if `sqlc` is on PATH; `--sqlc=never` turns that off), then generates the handlers from sqlc's output. One command takes you from SQL to a running endpoint. `af check` reports a `_af.sql` that is missing or out of date, a `sqlc.yaml` that doesn't read it, and queries sqlc hasn't generated yet. Values the server sets, such as `updated_at`, belong in the SQL (`now()`), not the request.

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

Errors are [go-kit errs](https://github.com/omniaura/go-kit/tree/main/errs) errors, written as its JSON:

```json
{"fields":{"id":"invalid value \"abc\""},"message":"invalid request","code":"invalid_request","ref":"d3f1...","action":"fix_input","status":400}
```

The runtime declares a factory for each failure: `ErrInvalidRequest` (400, with a field for each bad input), `ErrTooLarge` (413), `ErrUnauthorized` (401, when `Auth` fails), `ErrNotFound` (404), `ErrConflict` (409, for a unique violation) and `ErrModelFailed` (502). Any other query failure is `errs.Unknown`, a 500 whose message says nothing of the cause. `Auth` and `Prepare` can return your own errs factories, and they are written as they are. The cause goes to errs's log and sinks with the `ref`, never to the client. The runtime is `github.com/omniaura/agentflow/runtime/afhttp`, which depends only on `go-kit/errs`. See [`examples/routes`](examples/routes), which is tested against Postgres in CI.

### Structured Outputs

An `.output` section declares the JSON a model must answer with. Declare the fields once. AgentFlow generates the Go struct, a strict JSON Schema for OpenAI and Anthropic structured outputs, a strict decoder, and a description of the fields you can drop into the prompt, so the prompt, the schema and the struct can't drift apart.

```af
.title Judge

Decide whether this task is worth saving as a workflow. Answer with:
<!fields>

.output task_value_check
.field repeatable bool Would the person want this exact task done again?
.field confidence float 0 to 1, how sure you are.
.field why One short sentence citing the evidence.
.field cadence? enum(daily, weekly, monthly) How often it would run.
.field sources[].name The app or site the task reads.
```

`.field` lines follow the `.param` rules: types default to `string`, `?` makes a field optional, and dotted names imply objects. Strict structured outputs require every field, so an optional field is required but may be `null`. For the same reason, `.output` rejects `any` and objects with no declared fields.

```go
judge := Judge{Fields: TaskValueCheckSchema.Fields}
// OpenAI: response_format = TaskValueCheckSchema.OpenAI()
// Anthropic: output_config.format = TaskValueCheckSchema.Anthropic()
verdict, err := TaskValueCheckSchema.Decode(answer) // TaskValueCheckOutput
```

`Fields` reads like this:

    - "repeatable" (true or false): Would the person want this exact task done again?
    - "cadence" (one of "daily", "weekly", "monthly", or null): How often it would run.

### Agents

An `.agent` section turns a prompt and the file's tools into a tool-calling agent. AgentFlow generates the loop: send the conversation, decode each tool call with its `.tool` decoder, run your method, send the results back, and stop when the model answers.

```af
.title Support

You are a support agent for <!shop>. Look orders up before answering
questions about them. Offer a refund only for damaged or late orders.

.tool lookup_order
.param order_id The order ID, as printed on the receipt.

Load one order by ID. Returns its status and total.

.tool offer_refund
.param order_id The order to refund.
.param reason enum(damaged, late) Why the order qualifies.

Show the customer a refund offer they must accept.

.agent support
.maxturns 6

Answers customer questions about their orders.
```

| Directive | Meaning |
| --- | --- |
| `.system Title` | The prompt to use as the system prompt. Optional when the file has one prompt. |
| `.tools a, b` | The tools the agent may call. All the file's tools when omitted. |
| `.maxturns n` | The most model calls a run may make (default 10). |
| `.returns name` | The `.output` its final answer must be. `Run` then returns it decoded. |
| `.serve POST /path` | Also serve the agent over HTTP (`POST` or `PUT`). |

You implement the tools as methods and bring a model:

```go
type shop struct{ db *db.Queries }

func (s *shop) LookupOrder(ctx context.Context, args LookupOrderArgs) (string, error) { ... }
func (s *shop) OfferRefund(ctx context.Context, args OfferRefundArgs) (string, error) { ... }

agent := &SupportAgent{Model: model, Tools: &shop{q}, Concurrency: 4}
res, err := agent.Run(ctx, &Support{Shop: "Acme"},
	afagent.Message{Role: afagent.User, Content: "My order A-1 was late."})
// res.Final is the answer; res.Messages the conversation to store.
```

Arguments that fail validation, unknown tools, tool errors and panics go back to the model as failed tool results, so it can correct itself. `OnEvent` sees every model response and tool call for logging and streaming. The model is the `afagent.Model` interface, one method that takes the system prompt, messages and tools and returns the next assistant message, so any provider or gateway plugs in. `afagent` has no dependencies and can be ejected like the other runtime packages.

With `.returns`, the agent ends with structured data instead of text. The model can still call tools first:

```af
.output triage
.field category enum(order, refund, account, other) What the customer needs.
.field order_id? The order the message is about, if any.
.field urgent bool Whether a person should answer within the hour.

.agent triager
.tools lookup_order
.returns triage
```

```go
res, err := (&TriagerAgent{Model: model, Tools: impl}).Run(ctx, &Support{Shop: "Acme"}, msg)
if err != nil {
	return err // e.g. afagent.ErrMaxTurns: no valid triage within .maxturns
}
if res.Output.Urgent { ... } // res.Output is a TriageOutput
```

The adapters send the output's strict schema as the provider's structured output format. `Run` checks each final answer with the output's decoder. An answer that doesn't decode goes back to the model with the error, so it can try again within `.maxturns`.

With `.serve POST /triage`, the agent is also an HTTP endpoint:

```go
srv := &TriagerAgentServer{
	Agent: &TriagerAgent{Model: model, Tools: impl},
	// Authenticate, then load the prompt's values and the conversation so far.
	Prepare: func(r *http.Request) (*Support, []afagent.Message, error) { ... },
	// Store the conversation for the next request.
	Done: func(r *http.Request, res TriagerAgentResult) error { ... },
}
srv.Register(mux)
```

A client that sends `Accept: text/event-stream` gets the run as server-sent events:

```
event: tool_called
data: {"name":"lookup_order"}

event: tool_returned
data: {"name":"lookup_order","failed":false}

event: text
data: {"text":"Your order"}

event: done
data: {"answer":"Your order A-1 ...","usage":{...}}
```

`text` events stream the answer as the model writes it. `afanthropic` streams always; `afopenai` switches to streaming when someone listens. With `.returns`, the text is the JSON answer, sent once it passes the schema check, so a rejected answer never reaches the client. `tool_called`/`tool_returned` carry only the tool's name and whether it failed, never its arguments or results, and `answer_rejected` means the model is asked again. The stream ends with `done`, which has the same body as the JSON response, or with `error` (the same errs JSON as an HTTP error) once the 200 has been sent. Errors before the run starts, such as a bad body or failed authentication, are still plain HTTP errors. `afagent.ClientEvent` gives the same projection for your own servers.

The client sends only its new message, `{"message": "..."}`. It gets back `{"output": {...}, "usage": {...}}`, or `{"answer": "..."}` without `.returns`. The history comes from `Prepare` on the server, so a client can't forge the model's or a tool's earlier messages. Errors follow the `.route` conventions:
- the body is strict and capped (`MaxBytes`, 1 MiB by default)
- `Prepare` errors are written as they are when they are `*errs.Error`; wrap authentication failures with `afhttp.Unauthorized(ctx, err)`
- a model that refuses, runs out of turns or is cut off returns 502
- causes are never sent to the client

Two adapters implement `afagent.Model` on the official SDKs:

```go
// Claude, through github.com/anthropics/anthropic-sdk-go. Reads ANTHROPIC_API_KEY.
model := &afanthropic.Model{} // claude-opus-5-5, 16000 max tokens by default

// OpenAI Chat Completions, or any gateway that speaks it, through
// github.com/openai/openai-go/v3. Reads OPENAI_API_KEY.
model := &afopenai.Model{Model: modelID, Options: []option.RequestOption{option.WithBaseURL(gatewayURL)}}
```

`afanthropic` streams, so long answers don't time out, and turns on prompt caching, so each turn reuses the conversation before it. It keeps Claude's own content blocks on each assistant message (`Message.Native`), so thinking blocks go back unchanged on the next turn. Both adapters report cached input tokens in `Usage`, and return errors wrapping `afagent.ErrTruncated` for an answer cut off by the token limit and `afagent.ErrRefused` for a refusal. A `Params` hook adjusts each request, for example to set thinking, effort or `tool_choice`.

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

A second skill, `agentflow-migrate`, is installed alongside it. It walks a coding agent through moving hand-written code into `.af` one site at a time, using `af survey` to find candidates. For each site the agent writes the declaration, switches the callers, deletes the old code, and pins the old behaviour with a test. The skill also covers resolving upgrade conflicts. `af skills print agentflow-migrate` shows it.

## Ejecting the Runtime

Code generated for `.tool`, `.route` and `.agent` imports small runtime packages, `aftool`, `afhttp` and `afagent`, from `github.com/omniaura/agentflow/runtime`. Like shadcn/ui, you can pop them out into your own repository and own them:

    go tool af eject          # or: af eject aftool
    go tool af gen prompts ./...

The model adapters `afanthropic` and `afopenai` are ejected only when named: `af eject afanthropic`. An ejected adapter is your LLM gateway, and you can add retries, routing or logging to it. Ejected packages import each other's copies. If you eject a package after something that imports it, `af eject` updates the earlier copy and keeps your edits.

`af eject` copies the packages to `internal/agentflow/<package>` (choose another place with `--dir`) and records them in `agentflow.json` at the module root. From then on, `af gen` makes generated code import your copies. Their only dependencies are the model SDKs (for the adapters) and `go-kit/errs` (for `afhttp`), so a module that ejects everything doesn't need agentflow as a library at all.

Edit the copies freely. To upgrade them after upgrading af, run `af eject` again:

| Your copy | What happens |
| --- | --- |
| unedited | replaced with the new version |
| edited | merged three ways with the version it came from (fetched from the Go module cache), keeping your edits; overlapping changes are left as conflict markers and `af eject` exits non-zero |
| written by hand, or the original isn't available | kept, with the new version written next to it as `<file>.af-new` |

Files a new version drops are removed unless you've edited them. `--force` replaces edited files instead of merging. Each copied file records its af version and a content hash in an `agentflow-vendored:` comment. Leave that comment in place so upgrades can tell your edits apart.

## Upgrading

    go tool af upgrade          # or: go tool af upgrade v0.14.0

`af upgrade` moves the module's `af` tool pin to the new version, then uses that af to:
1. upgrade the runtime packages listed in `agentflow.json`, merging your edits (`af eject`)
2. upgrade the installed skills (`af skills install`)
3. regenerate every `_af.go` (`af gen prompts .`)
4. check the result (`af check ./...`)

It stops at the first failing step. If ejected files have merge conflicts, resolve the markers and run it again. The module must pin af with `go get -tool github.com/omniaura/agentflow/cmd/af`.

## Surveying a Codebase

`af survey [dir]` estimates how much of a Go module agentflow could define. It finds hand-written LLM tool schemas (`.tool`), structured-output schemas (`.output`), HTTP handlers that only serve one sqlc query (`.route`), long prompt strings (`.title`) and hand-written tool-calling loops (`.agent`): loops that call a model, read its tool calls and send results back. It reports them next to the code agentflow and sqlc already generate:

    candidates for .af                   sites    lines of hand-written
      tool schemas (.tool)                 122     1966 0.4%
      single-query handlers (.route)       205     6587 1.3%
      tool-calling loops (.agent)            9     2666 0.5%
      ...

`--top N` lists the largest sites of each kind, so you know where to start, and `--json` prints the full report. The survey reads source syntactically with heuristics, so treat its numbers as an estimate to steer migration, not a measurement.

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
