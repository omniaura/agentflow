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
| structured outputs | response-format schemas and the structs they decode into | `.output` |
| tool-calling loops | a loop that calls a model, runs its tool calls and sends results back | `.agent` (+ `.returns`) |
| prompt strings | long string constants with prompt text | `.title` prompts |
| single-query handlers | decode request, call one sqlc query, write JSON | `.route` (+ `.sql`) |

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
     `switch` cases into `XTools` methods.
   - `NewXRoutes(queries, auth).Register(mux)` replaces the handlers.
5. **Delete the hand-written code** the generated code replaced. Don't keep both.
6. **Verify with tests:** `go build ./... && go test ./...`. Add a test that pins the
   behaviour you wrote down in step 1, such as rejected enum values, required fields
   or status codes. Then run `go tool af check ./...`.
7. **Commit each site separately**, so each change is easy to review and revert.

### What not to migrate

These stay hand-written:
- **Handlers with real logic:** several queries, transactions, side effects or
  streaming. `.route` serves exactly one sqlc query.
- **Loops that mix providers mid-run, or rewrite the conversation between turns**
  (compaction, re-ranking). `.agent` runs a plain loop over `afagent.Model`, so wrap
  that logic in a `Model` instead, or leave the loop as it is.
- **Schemas with features `.param` can't express:** `oneOf`, recursive types,
  patterns. Leave them, or add a comment saying why.

### Adapters and the gateway

If the backend has its own LLM gateway, implement `afagent.Model` over it with
`afagent.ModelFunc`, and agents reuse its routing, retries and billing. Otherwise use
`afanthropic.Model` or `afopenai.Model`. Run `go tool af eject afanthropic` to own an
adapter and extend it.

### Measure

Run `go tool af survey` again after a batch of migrations and report the before and
after line counts for each category.
