# Phase 01 — Runtime library skeleton + go-sdk baseline

## Summary

Establishes `runtime/` as Dockyard's importable app-runtime library and stands
up a minimal MCP server on the official Go MCP SDK that registers one typed
tool and serves it over stdio. Lays down the module layout from AGENTS.md §3
(`runtime/server/`, `cmd/dockyard/`) so every later phase extends a shape that
already matches the RFC.

## RFC anchor

- RFC §3 — Repository / architecture overview; the runtime is a library
  vendored into every generated app and a thin `cmd/dockyard` entrypoint.
- RFC §5 — MCP server core; the SDK is the settled foundation (§5.1),
  transports (§5.2), the extension hooks Apps/Tasks will attach to (§5.3).

## Briefs informing this phase

- brief 03 — Official Go MCP SDK audit.

## Brief findings incorporated

- **Pin a recent v1.x SDK.** Brief 03 §2.1 records the SDK at v1.6.0
  (2026-05-08), stable with a no-breaking-changes guarantee. This phase pins
  `github.com/modelcontextprotocol/go-sdk v1.6.0` in `go.mod`.
- **The server primitive set is free; never re-implement.** Brief 03 §2.2 / §5
  ("Adopt"): `mcp.AddTool[In,Out]` infers JSON Schema from the Go types and
  validates incoming arguments. `runtime/server.AddTool` wraps it directly
  rather than re-implementing tool registration.
- **`InMemoryTransport` is the contract-test backbone.** Brief 03 §2.3 / §5:
  `NewInMemoryTransports()` is "the backbone of the Dockyard inspector and
  contract tests." The end-to-end and concurrency tests use it.
- **Layer, do not fork.** Brief 03 §5 ("Avoid"): the runtime wraps the SDK and
  never patches it; the wrapper keeps the seam thin so Phases 02/07 extend it.
- **Set security options explicitly, never trust SDK defaults.** Brief 03 §2.3
  / R4: defaults flipped between releases. Phase 01 is stdio-only and adds no
  HTTP transport, so no security default applies yet — Phase 07 owns that and
  this phase deliberately leaves the HTTP seam unimplemented (see Non-goals).

## Findings I'm departing from (if any)

None. Phase 01 is a strict subset of brief 03's "Adopt" list; the "Build" items
(Apps/Tasks layers, typed `_meta` accessors, the full contract-first builder)
are later phases by the master plan and are explicit non-goals here.

## Goals

- A `runtime/server` package: a Dockyard-owned `Server` that wraps an SDK
  `*mcp.Server`, with construction, typed tool registration, and a stdio serve
  loop.
- A `cmd/dockyard` placeholder so `make build` produces a CGo-free binary and
  the module layout matches AGENTS.md §3 from this phase on.
- The SDK pinned to v1.6.0; `CGO_ENABLED=0` build verified.

## Non-goals

- The streamable-HTTP transport and its explicit security knobs — Phase 07.
- The `protocolcodec` seam and `_meta` accessors — Phase 02.
- The contract-first codegen and the full `app.Tool(...).Input[T]()` builder —
  Phase 04. Phase 01 ships only the minimal typed `AddTool` it will sit on.
- The `Result[Out]` shape and the `content`/`structuredContent` split — Phase
  08. Phase 01 relies on the SDK's automatic structured-output population.
- The `obs/v1` emitter, the `Store` seam, the Apps and Tasks layers.
- A real cobra CLI — Wave 7 (phases 17+).

## Acceptance criteria

- [ ] A trivial server registers one tool and serves it: a client over the
      in-memory transport discovers the tool via `tools/list` and a `tools/call`
      returns the typed output in `structuredContent`.
- [ ] The stdio serve path (`Server.ServeStdio`) honours context cancellation.
- [ ] The SDK version is pinned to a recent v1.x — `go-sdk v1.6.0` in `go.mod`.
- [ ] `CGO_ENABLED=0 go build ./...` succeeds; `cmd/dockyard` builds.
- [ ] The package layout matches AGENTS.md §3 — `runtime/server/`,
      `cmd/dockyard/`.
- [ ] A single `Server` serves multiple concurrent sessions cleanly under
      `-race`.

## Files added or changed

```text
runtime/server/doc.go            # package doc — the runtime library charter
runtime/server/server.go         # Server, Info, Options, Run, ServeStdio
runtime/server/tool.go           # ToolFunc, ToolDef, AddTool[In,Out]
runtime/server/server_test.go    # validation, registration, e2e, concurrency
runtime/server/stdio_test.go     # stdio serve-loop cancellation
cmd/dockyard/main.go             # CLI placeholder (RFC §9 lands phase 17+)
go.mod / go.sum                  # pin go-sdk v1.6.0 + transitive deps
docs/plans/phase-01-runtime-skeleton.md
scripts/smoke/phase-01.sh
docs/decisions.md                # D-019, D-020, D-021
docs/glossary.md                 # "App runtime", "MCP server core"
```

No new top-level directory — `runtime/` and `cmd/` are already in AGENTS.md §3.

## Public API surface

```go
package server // github.com/hurtener/dockyard/runtime/server

type Info struct{ Name, Title, Version string }
type Options struct{ Logger *slog.Logger }

func New(info Info, opts *Options) (*Server, error)
func (s *Server) Info() Info
func (s *Server) Tools() []string
func (s *Server) MCP() *mcp.Server          // temporary SDK seam for phases 02/07
func (s *Server) Run(ctx context.Context, t mcp.Transport) error
func (s *Server) ServeStdio(ctx context.Context) error

type ToolDef struct{ Name, Description string }
type ToolFunc[In, Out any] func(ctx context.Context, in In) (Out, error)
func AddTool[In, Out any](s *Server, def ToolDef, fn ToolFunc[In, Out]) error
```

## Test plan

- **Unit:** `New` validation table; `AddTool` error table (nil server, empty
  name, nil handler, duplicate name, non-object input type → error not panic);
  `Tools()` returns a defensive copy; `Run` rejects a nil transport.
- **Integration:** `TestServeAndCallTool` — Dockyard server + SDK client over
  `NewInMemoryTransports()`, real `tools/list` + `tools/call`, asserts the
  typed output lands in `structuredContent`. Real driver on the boundary, no
  mocks (AGENTS.md §17). Phase 01 opens the `runtime/server` public interface
  later phases build on, so an integration test is binding.
- **Concurrency / golden:** `TestConcurrentReuse` — one `Server` serves eight
  concurrent sessions under `-race` (reusable-artifact requirement, AGENTS.md
  §5). `TestServeStdio` — the stdio loop returns on context cancellation. No
  golden tests (no codegen output this phase).

## Smoke script additions

`scripts/smoke/phase-01.sh` asserts:

- `runtime/server` package directory exists.
- `cmd/dockyard/main.go` exists.
- `go-sdk` is pinned to a `v1.x` in `go.mod`.
- `CGO_ENABLED=0 go build ./...` succeeds.
- `CGO_ENABLED=0 go test ./runtime/server/...` passes.
- The `dockyard version` placeholder binary runs.

## Coverage target

- `runtime/server` — 80% (new-package default, AGENTS.md §11). Achieved: 92%.
- `cmd/dockyard` — placeholder, not covered; the real CLI carries the 70%
  tooling target from Phase 17.

## Dependencies

- Phase 00 — repo skeleton & hygiene (shipped).

## Risks / open questions

- **Rolling Go floor / fast SDK cadence** (brief 03 R3/R4, RFC §18). v1.6.0 is
  pinned; bumps are deliberate, reviewed updates. Mitigated by isolating SDK
  use behind `runtime/server`.
- **`Server.MCP()` SDK leak.** Exposing `*mcp.Server` violates the "never
  expose raw SDK structs" intent (RFC §5.4) if it outlives its purpose. It is
  documented as a temporary seam for sibling phases 02/07 and is expected to be
  unexported once the Dockyard-owned registration surface is complete. Filed as
  D-021.
- **Missing custom-notification API** (brief 03 R2, #745) — not reached this
  phase; monitored by D-014.

## Glossary additions

- **App runtime** — the Dockyard runtime library (`runtime/`).
- **MCP server core** — the `runtime/server` package.

Both added to `docs/glossary.md` in this PR.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target (92% ≥ 80%)
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
