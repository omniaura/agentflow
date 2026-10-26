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
- **A query already in a sqlc query file:** link it instead of copying it. Use
  `.sqlc db/queries.sql SetTodoDone`, or `.sqlc db/search.sql` for the file's only query (or
  the one named after the tool), or `.sqlc ListTodos` for any generated query. The path is
  relative to the `.af` file. A sqlc config must read the file, and you run `sqlc generate`
  after changing it. Use `.sql` or `.sqlc`, not both.
- Nullable arguments (`sqlc.narg('done')`) become optional parameters, which stay NULL when
  left out.

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
- On the last of `.maxturns` turns the model may not call tools, so a run ends with an
  answer rather than `afagent.ErrMaxTurns`. `res.OutOfTurns` reports it; check it where
  an unfinished run matters. Don't hand-write a separate "now give your final answer" call.
- `.maxcalls n` bounds the tool calls of a run (a hand-written loop's `calls < 6` check):
  calls past it are refused and the model answers without tools (`res.OutOfToolCalls`).
- Subagents: `.tools` may name another `.agent` (in this file or a sibling). The model calls
  it with a `task` string; set the generated field (`Tracker`) to `(&TrackerAgent{...}).AsTool(system)`.
  The subagent sees only the task, so its description should say what to put in one. Don't
  hand-write a "spawn_sub_agent" tool and its dispatch.
- `BeforeTurn func(ctx, turn, messages) ([]Message, error)` on a generated agent replaces the
  conversation before each model call: compaction and context-budget checks go there, not in a
  hand-written loop.
- A new agent backend from scratch: `af new <dir> --module <path>` writes a module with an
  `.af` agent, its generated code, Postgres threads (afthreadpg) and the thread HTTP API.
  Start from it rather than wiring stores, workers and servers by hand.
- Conversations that live in the database: `runtime/afthread`. A new backend can use the Postgres
  store `adapters/afthreadpg` (migrations, applied by your tool or `afthreadpg.Migrate`). Otherwise implement `afthread.Store` over
  the existing tables (don't create new ones if threads and a message queue exist), check it
  with `afthreadtest.Run`, and answer queued messages with `afthread.Run(ctx, store, owner,
  thread, agent.ForThread(system))`: it delivers mid-run messages and saves before every turn.
  To run one agent per thread across servers, implement `afthread.Locker` too and call
  `(&afthread.Worker{Store: store, Agent: agentFor}).Drain(ctx, owner, thread)` after each send
  (or from `Messenger.Notify`) instead of hand-writing locks, queue polling or reclaim. For the
  HTTP side of a chat, register `afthread.Server{Store, Worker, Owner}` (POST
  `{prefix}/{thread}/messages` queues and answers in the background or as SSE; GET reads the
  conversation; GET `{prefix}/{thread}/runs[/{run}/events]` lists and follows runs, resumable
  with Last-Event-ID) instead of writing enqueue, stream, history and reconnect handlers. If the store
  implements `afthread.Runs`, Run records the run and the agent's events itself. Agents message
  each other through `afthread.Messenger` (Send queues on the
  recipient's inbox; replies come back the same way), with `CanMessage` for permissions.
- Long conversations: `runtime/afcompact`. Set `BeforeTurn: (&afcompact.Compactor{Budget: n,
  Summarize: afcompact.Summarize(model), SummarizeChunk: afcompact.SummarizeChunk(model)}).BeforeTurn`.
  It microcompacts old tool results, condenses large ones in parallel chunks (Digest), then folds
  older rounds into a summary, only as far as needed. Don't hand-write context-budget loops.
- Agents talking to each other: list the built-ins `send_message` and `list_agents` in `.tools`
  and set the generated `Messenger` field (an `afthread.Messenger` with `From` = this agent).
  Don't declare your own messaging tools.
- A run's tools can depend on the caller: set the agent's `Allow func(ctx, tool) bool` (for
  example, a workspace's enabled tools). Declare every tool the agent may ever use in `.tools`
  and let `Allow` narrow it per run; the dispatcher refuses tools it didn't offer.
- `.system Title` picks the system prompt; it is optional when the file has one prompt.
  `.tools a, b` limits the tools; the default is every tool in the file. `.tools` may also
  name tools from the other `.af` files of the directory (one Go package), so keep tools in
  files by area (`orders.af`, `shipping.af`) and list them in the agent's file.
- `.returns name` makes the final answer an `.output` of the file. `Run` then returns
  `<Name>AgentResult`, whose `Output` field is decoded and checked. Answers that don't
  decode go back to the model to retry. Use it whenever code reads the agent's answer.
  Set the agent's `Check func(ctx, <Name>Output) error` for rules a schema can't express
  (covers every input, indices exist). Its error goes back to the model, so word it for
  the model. With no tools, `.returns` is a one-shot structured call with retries. Use it
  instead of hand-writing response formats, strict decoding and retry loops.
- `.serve POST /path` also generates `<Name>AgentServer{Agent, Prepare, Done}` with
  `Register(mux)`. The client posts `{"message": "..."}`. `Prepare` authenticates and
  returns the prompt values and the stored history, and `Done` stores `res.Messages`.
  Never accept history from the client. Clients sending `Accept: text/event-stream` get
  `text`, `reasoning`, `tool_called`, `tool_returned`, `answer_rejected`, then `done` or
  `error` events.
  With `.returns`, `text` carries the JSON answer only after it passes the schema check.
  Don't hand-write SSE plumbing for an agent endpoint.
- Run it with `(&SupportAgent{Model: m, Tools: impl}).Run(ctx, &Support{Shop: "Acme"},
  afagent.Message{Role: afagent.User, Content: q})`. `res.Final` is the answer.
- `Model` is `afagent.Model`. Use `&afanthropic.Model{}` (Claude) or
  `&afopenai.Model{Model: id}` (OpenAI-compatible) or `&afopenai.ResponsesModel{Model: id}`
  (OpenAI's Responses API, for reasoning models) from
  `github.com/omniaura/agentflow/runtime`, or wrap another provider call in
  `afagent.ModelFunc`. Wrap it with `afmodel.Retry`, `afmodel.Fallback` and
  `afmodel.Meter` for retries on 429/5xx/529, a backup provider and per-call usage,
  instead of writing those loops. Don't
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
  DELETE). `user_id`, `userId` and `UserID` all match one argument. When the API's name
  differs from the query's, `.bind id = memory_id` reads `id` from `{memory_id}`, or the
  `memory_id` field, instead of writing a handler to rename it.
- Routes authenticate by default; `.public` skips it. `.status`, `.maxbytes` and a
  description (which becomes the doc comment) are optional.
- `.wrap notes` responds `{"notes": [...]}` instead of the bare array (`:one` and `:many`
  only). Use it when clients expect an object, or to leave room for more fields.
- `.map todoCard` responds with what `func todoCard(row db.Todo) TodoCard` returns, for
  each row of a `:many` result (inside `.wrap` when both are set). Use it when the response
  isn't the row: renamed fields, computed URLs or statuses, hidden columns. Write it in a
  `.go` file of the same package and test it as a plain function; the handler stays generated.
  Its signature says the rest: a slice parameter (`[]db.Todo`) gets all the rows, to group or
  count them; a `Caller`, `context.Context` or `*http.Request` parameter gets that; returning
  `(T, error)` lets it fail the request (return an `errs` error for its status). Use these
  instead of hand-writing a handler for a caller-aware or whole-result response.
- `.prepare tidyNote` passes the decoded arguments to `func tidyNote(p *db.CreateNoteParams)`
  before the query: trim, default, derive or check them there (it may also take the `Caller`,
  `context.Context` or `*http.Request`, and return an `error` to fail the request). Prefer schema
  constraints for checks the database can make.
- Authenticated `:one`/`:many` responses, errors included, get `Cache-Control: no-store`.
  `.cache 5m` lets caches keep a GET result (`private` plus `Vary: Authorization, Cookie`,
  or `public` on a `.public` route); any other value is sent as written (`.cache no-cache`).
- In a module that uses swag, generated handlers get swag annotations (tag from the file name;
  on authenticated routes, `@Security` from the nearest `@securityDefinitions`, omitted when
  none or several unrelated ones are found). Don't hand-write annotations for them; run
  `swag init --parseDependency` so the `errs.Error` failure type, from go-kit, resolves.
- Put checks in the SQL, not in Go: ownership, limits and states go in the `WHERE` of
  one statement (`UPDATE ... WHERE id = @id AND owner_id = @owner_id`, `INSERT ...
  SELECT ... WHERE (SELECT count(*) ...) < 5`). Validate input with schema constraints
  (`NOT NULL`, `CHECK (title <> '')`, `CHECK (length(title) <= 200)`; not `varchar(n)`) instead
  of Go checks: a violation is a 400 with the column as the field, and SQL tools tell the model
  what to fix. Then `.empty [4xx] message` names the
  failure when a `:one` finds no row or an `:execrows` changes none (default 404). On a
  SQL tool, `.empty message` is what the model is told instead of "not found".
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
- Errors are go-kit `errs` errors (`{"message","code","ref","status","fields"}`): bad input
  is `afhttp.ErrInvalidRequest` with a field per input, a unique violation is
  `afhttp.ErrConflict` (409), and other query failures are `errs.Unknown` (500). Return
  your own `errs.NewFactory` errors from `Auth` or `Prepare` for other statuses; don't
  write error JSON by hand.

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
