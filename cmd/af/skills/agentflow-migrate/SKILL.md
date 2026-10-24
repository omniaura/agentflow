---
name: agentflow-migrate
description: "Migrate a Go backend onto AgentFlow and keep it current: move hand-written LLM tool schemas, structured-output schemas, tool-calling loops, prompt strings and single-query HTTP handlers into .af files, and upgrade af with its ejected runtime. Use when asked to adopt agentflow in existing code, shrink hand-written LLM or route code, act on af survey results, upgrade af, or resolve af eject merge conflicts."
metadata:
  agentflow-vendored: STAMP
---

# Migrating to AgentFlow

Two jobs: moving hand-written code into `.af` files (adoption), and moving a module to a
new af version (upgrade). Read the `agentflow` skill for the `.af` syntax.

## Upgrade

```sh
go tool af upgrade            # or: go tool af upgrade v0.14.0
```

`af upgrade` pins the new af, then runs it to upgrade the ejected runtime
(`agentflow.json`), the installed skills and every `_af.go`, and ends with `af check`.

- **A merge conflict in an ejected file** (`internal/agentflow/...`) stops the upgrade.
  Keep the module's edit and the new version's change both, unless they contradict.
  Then remove the markers, keep the `agentflow-vendored:` comment, and run
  `go tool af upgrade` again.
- **`af check` reporting drift** after an upgrade is normal. The upgrade already
  regenerated, so commit the regenerated files.
- **New lint errors** come from a stricter af. Fix the `.af` file, not the generated
  code.
- Run `go build ./... && go test ./...` before committing the upgrade.

## Adoption

### 1. Find candidates

```sh
go tool af survey --top 20     # add --json to read every site
```

Each category has a `.af` replacement:

| Survey category | Hand-written code | Replace with |
| --- | --- | --- |
| tool schemas | JSON Schema maps or SDK tool params, args structs, argument checks | `.tool` |
| structured outputs | response-format schemas and the structs they decode into; the call that sends one, validates the answer and asks again (`retry until valid` sites) | `.output`; for the call, an `.agent` with `.returns` and no tools, with the validation in `Check` |
| tool-calling loops | a loop that calls a model, runs its tool calls and sends results back, often ending in a tools-off final call when turns run out | `.agent` (+ `.returns`); the last turn already answers without tools |
| prompt strings | long string constants with prompt text | `.title` prompts |
| single-query handlers | decode request, call one sqlc query, write JSON (or build it from the rows in Go) | `.route` (+ `.sql`; `.map` for the rows-to-JSON part) |
| gateway code: retry | a loop that calls a model again after an error: waiting, on a 429/529, or under an attempt counter | `afmodel.Retry(m, policy)` (see "Adapters and the gateway") |
| gateway code: fallback | trying a backup model, or each provider in turn, when a call fails | `afmodel.Fallback(primary, backup)` |
| gateway code: usage | reading a response's token counts to record or bill them | `afmodel.Meter(m, record)` |
| read-check-write handlers | call a sqlc read, check its result in Go, call a sqlc write | one `.route` + `.sql` holding the check, `.empty` for its failure (see below) |

### 2. Migrate one site at a time

1. **Read the site and everything it touches.** That means its callers, its tests and
   any validation it does. The `.af` version must accept and reject the same inputs,
   so write those rules down first.
2. **Write the `.af` declaration** next to the Go code, in the same package. Reuse the
   original names: `.tool lookup_order` keeps the tool name the model already knows.
   Copy descriptions word for word, because the model reads them.
3. **Generate the code:** run `go tool af gen prompts <dir>` and `go tool af lint <file>`.
4. **Switch the callers** to the generated code. Examples:
   - `XTool.OpenAI()` / `.Anthropic()` replace a hand-built schema.
   - `XTool.Decode(args)` replaces `json.Unmarshal` plus checks.
   - `XSchema.Decode(answer)` replaces output parsing.
   - `(&XAgent{Model: m, Tools: impl}).Run(...)` replaces the loop. Turn the loop's
     `switch` cases into `XTools` methods. Tools spread over several Go files can stay
     in several `.af` files of the package: the agent's `.tools` names them all.
     A loop that filters its tool catalog per user or per run, and checks each call against
     that allowlist, becomes the agent's `Allow` func.
     A budget on tool calls (stop offering tools after N calls) becomes `.maxcalls N`.
     A tool that spawns another agent loop (a "sub-agent") becomes that agent's `.agent`, named
     in the parent's `.tools`; wire it with `AsTool`.
   - `NewXRoutes(queries, auth).Register(mux)` replaces the handlers.
5. **Delete the hand-written code** the generated code replaced. Don't keep both.
6. **Verify with tests:** `go build ./... && go test ./...`. Add a test that pins the
   behaviour you wrote down in step 1, such as rejected enum values, required fields
   or status codes. Then run `go tool af check ./...`.
7. **Commit each site separately**, so each change is easy to review and revert.

### Keep the response shape

A `.route` responds with the sqlc row, or a list of rows, as JSON. Make that match what the
old handler wrote, so clients don't break:
- **An object around the result** (`map[string]any{"companies": rows}`): add `.wrap companies`.
- **Field names:** sqlc names JSON fields after columns (and `json_tags_case_style`). Rename
  with `AS` in the SELECT, and select only the fields the old response had.
- **API docs:** with swag in the module, the generated handler is annotated, so the route stays
  in `swag init`'s output. Compare the regenerated swagger file before and after; the tag is
  the `.af` file's name, so name the file after the old `@Tags`.
- **Headers:** results of authenticated routes are sent with `Cache-Control: no-store`.
  If the old handler set a max-age, add `.cache` with it (`.cache 5m` sends `private,
  max-age=300`, or `public` on a `.public` route); for any other value, `.cache <value>`
  sends it as written.
- **Computed fields** (URLs built in Go, renamed keys, redacted values): move the loop
  body that builds each item into a func of the package, `func toCard(row db.X) Card`, and
  add `.map toCard`. The route then responds with its result, for each row of a `:many`
  query. Simple renames can be `AS` in the SQL instead. Keep the handler when building
  the response needs the context, a service or another query.

Pin the old shape with a test that decodes the response before switching.

### Handlers that read, check, then write

Many handlers load a row, check something in Go and then write: who owns it, a limit
(`len(existing) >= max`), or a state (`if share.Status != "pending"`). Fold the check into
the write's SQL, and the handler becomes one `.route`:

- **Ownership:** `UPDATE ... WHERE id = @id AND owner_id = @owner_id`, as `:execrows`.
- **A limit:** `INSERT ... SELECT ... WHERE (SELECT count(*) FROM t WHERE owner_id = @owner_id) < 20 RETURNING *`, as `:one`.
- **A state:** `UPDATE ... WHERE id = @id AND status = 'pending'`.

Then add `.empty <status> <message>` with the status and message the old handler used.
Ownership and state checks in an `UPDATE`/`DELETE` hold under concurrency; a `count(*)`
limit can be overshot by concurrent inserts, as the old read-then-write could. Keep any
lock or trigger the old code relied on. If
it told several failures apart (404 vs 403 vs 409), say so in the one message, or keep the
handler. Write a test for each failure.

### What not to migrate

These stay hand-written:
- **Handlers with real logic:** several queries, transactions, side effects or
  streaming. `.route` serves exactly one sqlc query.
- **Loops that mix providers mid-run.** `.agent` runs one `afagent.Model`, so put the switch
  inside a `Model` (or `afmodel.Fallback`), or leave the loop as it is. Loops that rewrite the
  conversation between turns (compaction, context budgets) can migrate: that logic becomes
  the agent's `BeforeTurn`.
- **Schemas with features `.param` can't express:** `oneOf`, recursive types,
  patterns. Leave them, or add a comment saying why.

### Threads, queues and workers

A backend that keeps chat threads and a queue of incoming messages in its database, served by
a worker that claims queued messages, runs the agent and saves the transcript, maps onto
`runtime/afthread`. Write an adapter: implement `afthread.Store` over the existing tables, with
no data migration. The thread table's transcript backs `Transcript`, `Append` and `Replace`. The
queue table's client message ID, status (queued, started, done, failed) and sequence back
`Send`, `Claim`, `Finish` and `Reclaim` (a claimed-at column lets a restarted worker requeue
what a crashed one left started). Scope every query by the owner column. Run
`afthreadtest.Run` against a test database until it passes, then replace the worker's
claim-run-save code with `afthread.Run`. Keep the queue's existing consumers working: the
statuses map one to one.

### Adapters and the gateway

If the backend has its own LLM gateway, implement `afagent.Model` over it with
`afagent.ModelFunc`, and agents reuse its routing, retries and billing. Otherwise use
`afanthropic.Model` or `afopenai.Model`. Run `go tool af eject afanthropic` to own an
adapter and extend it.

Gateway code that retries a rate-limited or overloaded provider, falls back to another
one, or records usage per call maps onto `runtime/afmodel`: `Retry(m, policy)` (a go-kit
`errs.RetryPolicy`), `Fallback(primary, backup)` and `Meter(m, record)`. Move such code one
behaviour at a time. Keep product rules (which plan may use which model, billing rates)
in the `record` func or in a `ModelFunc`, and test the old retry and fallback cases
against the wrappers with a fake `Model`.

### Measure

Run `go tool af survey` again after a batch of migrations and report the before and
after line counts for each category.
