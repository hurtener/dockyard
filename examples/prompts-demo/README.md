# prompts-demo

A worked example showing **MCP Prompts** via Dockyard's Phase 28 prompts
API — `runtime/server.AddPrompt` (D-151).

## What MCP Prompts are

MCP separates two model-facing primitives:

- **Tools** — things the model PUSHES: a typed input becomes a typed
  output, the host validates, the runtime emits an `obs/v1` event.
- **Prompts** — templates the host PULLS: a named template a chat host
  surfaces to the user (a `/` slash-command, a quick-action button) so
  the user can seed a chat with a curated message set.

Dockyard supports both. The two earlier shipped templates exercise
tools + Apps; this example exercises prompts.

## What it ships

Three prompts via `runtime/server.AddPrompt`:

- **`summarize_for_review`** — a 2-message system+user template that
  primes a careful, review-oriented summarisation chat. Arguments:
  `passage` (required), `audience` (optional, defaults to "an
  engineering peer").
- **`code_review`** — a 4-message review-against-rubric template.
  Arguments: `diff` (required), `language` (default "Go"), `rubric`
  (default 4-point list).
- **`explain_error`** — a single-message "explain this error" template.
  Arguments: `error` (required), `language` (default "Go").

Plus one tool — `summarize_text` — so the manifest is valid and the
example also shows the tool / prompt distinction side-by-side.

## Why no contract-first prompts

MCP prompts carry a flat string-keyed argument map, not a structured
object — there is no JSON Schema layer on the argument shape. Dockyard's
contract-first pattern (typed Go struct → JSON Schema) **does not extend
naturally to prompts** (D-152). `AddPrompt` is a thin pass-through:

- A typed `PromptDef` so a developer reads the same `Name` /
  `Title` / `Description` / `Arguments` shape MCP documents.
- A typed `PromptRequest` / `PromptResult` so the handler signature
  never exposes the raw SDK `*GetPromptRequest` / `*GetPromptResult`
  (RFC §5.4, P3 — runtime/server keeps the protocol struct out).
- The same `obs/v1` `prompt.get` lifecycle event every other Dockyard
  handler gets (Phase 28 added `obs.KindPromptGet` + `Recorder.PromptGet`).
- The same panic recovery — a panicking handler becomes a typed
  Dockyard error, not a process crash (AGENTS.md §5, §13).

For an argument shape that needs structured validation, register a
**tool** instead — the contract-first pipeline applies there.

## Layout

```text
examples/prompts-demo/
├── dockyard.app.yaml                   # one tool; no apps[]
├── cmd/server/main.go                  # registers tool + 3 prompts
├── internal/contracts/contracts.go     # the tool's typed contracts
└── internal/handlers/                  # tool handler + 3 prompt handlers
    ├── handlers.go
    └── handlers_test.go
```

## Try it

```bash
# From the repo root.
cd examples/prompts-demo

# 1) Generate the schemas for the one tool.
dockyard generate

# 2) Validate the manifest + contracts.
dockyard validate

# 3) Run it (stdio is the default).
go run ./cmd/server

# 4) Or, run it over streamable-HTTP on 127.0.0.1:8080:
DOCKYARD_TRANSPORT=http go run ./cmd/server

# 5) The inspector renders Tools but not Prompts (Phase 23 scope was
#    tools / resources / Tasks); to drive prompts/list + prompts/get
#    use a host that surfaces prompts (Claude, an MCP CLI), or call the
#    JSON-RPC frames directly. See the screenshot under
#    docs/screenshots/phase-28/ for the inspect-side view.
```

Run the handler tests:

```bash
go test ./internal/handlers
```

## Use this example when

You want to surface **template messages** that a host can hand the user
as quick actions — a `/summarise` chip, a `/explain-error` slash command,
a "review this diff" sub-agent seed. Prompts are server-side curated
content; tools are model-driven actions.

## Pre-publish notes (D-139)

A scaffold built from this example via `dockyard new` would need
`go mod tidy` once after the scaffold; the example itself lives inside
the Dockyard repo and uses the root `go.mod`, so no extra step is
needed.

## Related

- [`examples/backend-tools-only`](../backend-tools-only) — pure-tools
  pattern (no UI, no prompts).
- [`examples/combined-patterns`](../combined-patterns) — analytics
  widget + approval-flow composition.
- [Decisions log: D-151, D-152](../../docs/decisions.md) — the prompts
  API rationale.
