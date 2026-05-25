# backend-tools-only

A worked example showing **Dockyard as a pure-tools MCP server** — three
typed Go tool handlers, no MCP App, no UI, no Tasks. The common case for a
developer who just wants to expose tools to an agent host.

This example sits alongside the two interactive templates
([D-150](../../docs/decisions.md)) so a developer sees both shapes:
`dockyard new --template analytics-widgets` (UI-bearing widgets) vs this
example (pure backend).

## What it does

The server publishes an in-memory bookmarks catalog with three tools:

- **`list_bookmarks`** — return every entry, optionally filtered by tag.
- **`add_bookmark`** — add a new bookmark; returns the stored record.
- **`search_bookmarks`** — case-insensitive substring search across
  title, URL, and notes.

The catalog is seeded with three real entries (Effective Go, the MCP
spec, Dockyard) so the inspector's first call shows something
meaningful.

## Layout

```text
examples/backend-tools-only/
├── dockyard.app.yaml                   # the manifest — no apps[], no UI
├── cmd/server/main.go                  # registers the 3 tools, serves stdio/http
├── internal/contracts/contracts.go     # the typed Go contracts (P1 — source of truth)
└── internal/handlers/                  # the typed handlers + their tests
    ├── handlers.go
    └── handlers_test.go
```

Generated JSON Schema + TypeScript live under
`internal/contracts/_generated/` after you run `dockyard generate` — the
example does not check generated artifacts in (consistent with the
templates).

## Try it

This example lives **inside the Dockyard repo** as a reference; it
shares the root `go.mod`. To run it:

```bash
# From the repo root.
cd examples/backend-tools-only

# 1) Generate the schemas + TypeScript from the Go contracts
#    (Dockyard P1 — the Go struct is the source of truth).
dockyard generate

# 2) Validate the manifest + the contract resolution.
dockyard validate

# 3) Run it over stdio (the default transport).
go run ./cmd/server

# 4) Or, run it over streamable-HTTP on 127.0.0.1:8080:
DOCKYARD_TRANSPORT=http go run ./cmd/server

# 5) Drive it under the inspector — works the same as a UI-bearing server,
#    just with no App preview pane:
DOCKYARD_TRANSPORT=http go run ./cmd/server &
dockyard inspect --url http://127.0.0.1:8080
```

Run the handler tests:

```bash
go test ./internal/handlers
```

## Use this example when

You want a **backend MCP server reached from an agent host** with no
in-chat UI — the common case for tooling like "summarise a JIRA
ticket", "query a database", "wrap a third-party API". The
`analytics-widgets` template is for when you want a widget rendered
inline in the chat; this example is for when the agent consumes the
result directly.

## Swap to a real backend

Replace the body of `Catalog` (in `internal/handlers/handlers.go`) with
a call into your real backing store — a SQL database via the Dockyard
[`runtime/store`](../../runtime/store) seam, a Postgres client, an HTTP
API. The typed contracts in `internal/contracts/contracts.go` are the
integration surface; keep them stable, or regenerate the schema with
`dockyard generate` after a change.

## Pre-publish notes (D-139)

A scaffold built from this example via `dockyard new` would need
`go mod tidy` once before `go test ./...`, because a pre-publish
scaffold's generated `go.mod` carries a `replace` directive but no
`go.sum`. The example itself lives inside the Dockyard repo and uses
the root `go.mod`, so no extra step is needed.

## Related

- [`examples/combined-patterns`](../combined-patterns) — composition of
  a widget tool and an approval-flow tool on one App.
- [`examples/prompts-demo`](../prompts-demo) — registering an MCP
  Prompt via Dockyard's prompts API ([D-151](../../docs/decisions.md)).
- [Templates](../../templates) — the scaffolded entry points (`dockyard
  new --template …`).
