---
name: agentflow
description: "Write and edit AgentFlow .af files: typed prompt templates, LLM tool declarations, structured outputs and HTTP routes over sqlc queries that af gen compiles to Go. Use when creating or changing a .af file or its generated _af.go, adding a prompt, a tool an LLM can call, a JSON output schema or an API route backed by a SQL query, or when af gen, af lint or af check fails."
metadata:
  agentflow-vendored: STAMP
---

# AgentFlow (.af)

A `.af` file is the source of truth for prompts and LLM tools. `af gen` compiles each
`name.af` to `name_af.go` in the same Go package. Never edit `_af.go` files: change the
`.af` file and regenerate.

## Workflow

1. Edit the `.af` file.
2. `go tool af fmt --write <file.af>` (or `af fmt` if installed globally).
3. `go tool af gen prompts <dir>`. A `//go:generate go tool af gen prompts .` line
   next to the files lets `go generate ./...` do this.
4. `go tool af check ./...` must print `ok`. It fails on stale `_af.go` files,
   unformatted `.af` files, lint errors and generation errors, and CI runs it.

Use `af lint --format json <file.af>` for machine-readable diagnostics (`file:line:col`).

## The syntax is light: declare nothing up front

AgentFlow infers the Go input struct from what you write. Don't write struct
definitions, imports or type declarations for prompt inputs: use a variable and it
becomes a field.

### Prompts

```af
.title Support Agent

You are <!agent.name>, helping <!user.name>.
The user has <!open_tickets int> open tickets.

<?user.premium bool>
Offer priority support.
<else>
Mention the premium plan once.
</user.premium>

<?open_tickets gt 3>
Ask which ticket matters most.
</open_tickets>
```

- `.title Name` starts a prompt. It becomes a Go struct `SupportAgent` with a
  `String() string` method. A file with several prompts needs a title on each.
- `<!path>` interpolates a value. `<!a.b.c>` makes nested struct fields. Field names are
  CamelCase of the path parts (`user_name` becomes `UserName`).
- Add a type on first use: `<!count int>`. Later uses reuse it (`<!count>`). Types are
  `string` (the default), `int`, `bool`, `float32` and `float64`.
- `<?path>` ... `<else>` ... `</path>` renders when the value is non-zero. Comparisons
  use word operators only: `eq ne gt gte lt lte`, as in `<?tier eq "gold">` or
  `<?age int gte 18>`. Never use `>=`, `==` or `!=` (lint error AF009).
- Close every block with its own path: `</user.premium>`.

### Loops

```af
.title Tool List

<*tools []Tool as tool>
- <!tool.name>: <!tool.summary>
</tools>
```

`<*path []Type as alias>` loops over a Go slice. `Type` is a Go type that you declare
in a normal `.go` file in the same package. Fields that aren't strings need a type:
`<!tool.priority int>`.

### Tools an LLM can call

```af
.tool offer_refund
.param order_id The order to refund.
.param reason enum(damaged, late, wrong_item, other) Why the customer wants a refund.
.param lines[].sku The line's SKU.
.param lines[].quantity int How many units to refund.
.param note? A note for the support team.

Show the customer a refund offer they must accept.
```

- `.tool name` starts a section that lasts until the next `.title`, `.tool`, `.output`, `.route` or `.agent`. Its text
  that isn't a directive is the description the model reads. Write that description
  for the model.
- `.param name [type] [description]`: the type defaults to `string`. A `?` on the name
  or the type makes the parameter optional. Types are `string`, `int`, `float`, `bool`,
  `enum(a, b)`, `object`, `any`, and `[]` of any of them.
- Objects are implied. `filter.since` makes `filter` an object, and `lines[].sku` makes
  `lines` an array of objects. Declare the object too (`.param lines? []object
  Description.`) only to describe it or make it optional.
- `.maxbytes 4096` caps the size of the arguments (the default is 64 KiB).

Generated Go for `offer_refund`: an `OfferRefundArgs` struct, `OfferRefundReason*`
constants, and `OfferRefundTool`, an `*aftool.Def[OfferRefundArgs]`:

```go
tools := []json.RawMessage{OfferRefundTool.OpenAI()} // or .Anthropic()

args, err := OfferRefundTool.Decode(call.Arguments)
if err != nil {
	return err.Error() // worded for the model: send it back as the tool result
}
switch args.Reason {
case OfferRefundReasonDamaged:
	// ...
}
```

`Decode` rejects oversized arguments, unknown or missing properties, wrong types and
values outside an enum. Don't hand-write JSON Schemas, args structs or argument
validation for a tool that a `.tool` section can declare.

A tool that is one database query is written in SQL, and af generates its Go too:

```af
.title Notes

You help with notes.

.tool pin_note
.auth owner_id

Pins one of the caller's notes.

.sql :execrows
UPDATE notes SET pinned = true WHERE id = @note_id AND owner_id = @owner_id
```

- `.sql :one|:many|:exec|:execrows` ends the section. The query goes to sqlc as
  `PinNoteTool` (name + `Tool`), so the module needs a sqlc config that reads `<file>_af.sql`.
- **No `.param` needed:** the parameters are inferred from the query's arguments
  (`note_id` is an integer). Add `.param note_id The note's ID.` to give it a description.
- `.auth owner_id` reads `owner_id` from the caller, never from the model. Use it for every
  ownership check.
- af generates `<File>SQLTools{Queries, Caller}`, whose methods implement the tools.
  `Caller func(ctx) (Caller, error)` reads the caller from the run's context. Embed it in
  an agent's tools type, or pass it as the tools when every tool is SQL.
- **Results:** rows come back to the model as JSON. No row is `not found`, and a database
  failure is `internal error`, with the cause kept for logs.

### Structured outputs

```af
.title Judge

Decide whether this task is worth saving as a workflow. Answer with:
<!fields>

.output task_value_check
.field repeatable bool Would the person want this exact task done again?
.field confidence float 0 to 1, how sure you are.
.field why One short sentence citing the evidence.
.field cadence? enum(daily, weekly, monthly) How often it would run.
```

- `.output name` declares the JSON a model must answer with. `.field` follows the `.param`
  rules.
- The generated code is a `TaskValueCheckOutput` struct and `TaskValueCheckSchema`.
  Pass `.OpenAI()` as `response_format`, or `.Anthropic()` as `output_config.format`.
  `.Decode(answer)` checks the answer and decodes it, and `.Fields` describes the fields
  for the prompt.
- Don't repeat field descriptions in the prompt by hand: render `.Fields` into it.
- The schema is strict: optional fields become nullable. `any` and free-form objects
  are rejected.

### Agents: the tool-calling loop

```af
.title Support

You are a support agent for <!shop>. Look orders up before answering.

.tool lookup_order
.param order_id The order ID.

Load one order by ID.

.agent support
.maxturns 6

Answers customer questions about their orders.
```

- `.agent name` generates `SupportAgent`, a `SupportTools` interface with one method per
  tool (`LookupOrder(ctx, LookupOrderArgs) (string, error)`) and `Run`, which loops
  until the model answers without calling a tool.
- `.system Title` picks the system prompt; it is optional when the file has one prompt.
  `.tools a, b` limits the tools; the default is every tool in the file.
- `.returns name` makes the final answer an `.output` of the file. `Run` then returns
  `<Name>AgentResult`, whose `Output` field is decoded and checked. Answers that don't
  decode go back to the model to retry. Use it whenever code reads the agent's answer.
- `.serve POST /path` also generates `<Name>AgentServer{Agent, Prepare, Done}` with
  `Register(mux)`. The client posts `{"message": "..."}`. `Prepare` authenticates and
  returns the prompt values and the stored history, and `Done` stores `res.Messages`.
  Never accept history from the client. Clients sending `Accept: text/event-stream` get
  `text`, `tool_called`, `tool_returned`, `answer_rejected`, then `done` or `error` events.
  Don't hand-write SSE plumbing for an agent endpoint.
- Run it with `(&SupportAgent{Model: m, Tools: impl}).Run(ctx, &Support{Shop: "Acme"},
  afagent.Message{Role: afagent.User, Content: q})`. `res.Final` is the answer.
- `Model` is `afagent.Model`. Use `&afanthropic.Model{}` (Claude) or
  `&afopenai.Model{Model: id}` (OpenAI-compatible) from
  `github.com/omniaura/agentflow/runtime`, or wrap another provider call in
  `afagent.ModelFunc`. Don't
  hand-write tool-calling loops, dispatch switches or argument decoding for an agent
  that an `.agent` section can declare. Bad arguments and tool errors already go back
  to the model as failed tool results.

### HTTP routes over sqlc queries

```af
.route POST /todos CreateTodo
.auth owner_id
.status 201
Creates a todo for the caller.

.route GET /todos/{id} GetTodo
.auth owner_id

.route GET /stats/count CountTodos
.public
```

- Write the SQL in the sqlc queries file and run `sqlc generate`. `.route METHOD /path
  Query` then serves the query, and af finds it through `sqlc.yaml`. Don't hand-write
  handlers that only decode a request, call one query and write JSON.
- Arguments bind by name, in order: `{path}` parameters, then `.auth` names (fields of
  your `Caller` type), then the JSON body (or the query string for GET, HEAD and
  DELETE). `user_id`, `userId` and `UserID` all match one argument.
- Routes authenticate by default; `.public` skips it. `.status`, `.maxbytes` and a
  description (which becomes the doc comment) are optional.
- The generated code is a `<File>Queries` interface and a `<File>Routes` struct. Declare
  `type Caller struct{ OwnerID string }` in the package, set `Queries`, `Auth` and
  optionally `OnError`, then call `Register(mux)`.
- Prefer writing the query in the route: end the section with `.sql :one` (or
  `:many`, `:exec`, `:execrows`) and the SQL below it, using sqlc's `@name` parameters.
  `af gen` writes `<file>_af.sql`, runs `sqlc generate`, and generates the handler. The
  `_af.sql` must be listed in the `queries` of `sqlc.yaml`. Server-set values such as
  timestamps go in the SQL (`now()`).
- Responses: `:one` returns 200 with JSON, or 404 when there is no row. `:many` returns a
  JSON array. `:exec` and `:execrows` return 204, and `:execrows` returns 404 when no row
  changed.

## Ejected runtime

If the repository has `agentflow.json`, the runtime packages (`aftool`, `afhttp`, `afagent`,
and maybe the `afanthropic`/`afopenai` adapters) have been copied into it with `af eject`.
Generated code imports those copies. Editing the copies is fine; keep their
`agentflow-vendored:` comment. To move to a new af, run `go tool af upgrade`, which
merges the new runtime into the copies. Then fix any conflict markers it leaves (see the
`agentflow-migrate` skill).

## Pitfalls

- Text in `<...>` that isn't a directive (`<!`, `<?`, `</`, `<*`, `<else>`) stays
  literal.
- Whitespace in prompt bodies is kept exactly. Blank lines after a `.title` line, and
  trailing whitespace at the end of a body, are ignored.
- One `.af` file generates one `_af.go`. Tool and prompt names must be unique in the Go
  package.
- After upgrading af, rerun `af gen` and commit the regenerated files.
