# Phase 04 — contract-first codegen + typed tool builder

## Summary

Phase 04 builds the spine of Dockyard's contract-first property (P1): a pure-Go
`internal/codegen` package that turns a Go contract struct into a deterministic JSON
Schema via `github.com/google/jsonschema-go` (the same engine the MCP SDK uses), and
a `runtime/tool` package that gives app authors a typed, fluent tool builder which
composes `runtime/server`'s `AddTool`, registers the generated schema on the tool,
and routes typed handler output into `structuredContent`. It leaves clean seams for
Go to TypeScript (Phase 05) and the manifest (Phase 06) to build on.

## RFC anchor

- RFC §6 — the contract-first model & codegen pipeline (§6.1 single source of truth,
  §6.2 Design A pipeline, §6.3 `content` vs `structuredContent`)
- RFC §5.1 — the SDK is the foundation; the builder wraps `runtime/server`'s typed
  `AddTool`
- RFC §3 — repository layout: `internal/codegen` is the codegen home; the app-facing
  tool builder belongs in `runtime/`

## Briefs informing this phase

- brief 06 — Go-2026 no-CGo stack & toolchain
- brief 04 — mcp-use DX teardown

## Brief findings incorporated

- **Brief 06 §2.3 / §3.1 / §3.3:** "Use `google/jsonschema-go` as the single schema
  engine ... the official MCP `go-sdk` already depends on it ... using anything else
  would create a second, divergent schema dialect." `internal/codegen` calls
  `jsonschema.For[T]` and adopts no parallel schema library.
- **Brief 06 §3.1 (Design A):** "Go structs are the SoT. Schema and TS are generated
  *independently from Go*, each by a pure-Go tool ... No Node dependency in the
  codegen path." Phase 04 ships the schema half of Design A; the package boundary
  (`SchemaFor` plus a deterministic `Marshal`) is shaped so Phase 05's `tygo` step
  and Phase 06's manifest sit beside it without rework.
- **Brief 06 R1 / R3:** schema/TS drift is a real risk and `google/jsonschema-go` is
  young — so the generated schema is covered by **golden tests** (fixed input to
  fixed output) and the marshaller is deterministic (sorted keys), making a drift or
  an upstream-inference regression a visible diff.
- **Brief 04 §2.6:** mcp-use "has *types* but not *contracts*" — widget generics are
  hand-declared and drift silently. Dockyard's builder makes the Go struct the only
  shape: the registered tool's input/output schema *is* the generated schema, never
  hand-written (P1, AGENTS.md §6).
- **Brief 04 §3 (builder sketch):** `app.Tool("show_customer_health").Input[...]().
  Output[...]().UI(...).Handler(...)` — Phase 04 ships this fluent builder, adapted
  to a Go-legal generic shape (see departures plus D-029).

## Findings I'm departing from (if any)

- **Brief 04's literal `app.Tool("x").Input[T]().Output[T]()` chain is not legal
  Go** — Go does not permit type parameters on methods (confirmed by brief 06 §2.1,
  which notes generics are "mature" but says nothing that lifts this restriction; the
  existing `runtime/server.AddTool` is already a package function "because Go does
  not allow type parameters on methods"). The builder's type parameters are therefore
  bound once, at construction, by a package-level generic constructor
  `tool.New[In, Out](name)`; the rest of the chain (`Describe`, `UI`, `Handler`,
  `Register`) is plain methods. The fluent, contract-first *spirit* of the brief
  sketch is preserved. Filed as **D-029**.

## Goals

- A Go contract struct generates a correct, deterministic JSON Schema with stable
  key ordering, suitable for golden-testing and byte-stable regeneration.
- An app-facing typed tool builder produces a tool registered on a `runtime/server`
  whose input/output JSON Schema is exactly the generated schema.
- Typed handler output is routed to `structuredContent` and model-facing text to
  `content`, per RFC §6.3.
- The `internal/codegen` package boundary is shaped so Phase 05 (Go to TS) and
  Phase 06 (manifest) extend it without refactoring.

## Non-goals

- Go to TypeScript generation (`tygo`) — Phase 05.
- The `dockyard validate` schema-to-TS drift cross-check — Phase 05.
- The `dockyard.app.yaml` manifest schema and loader — Phase 06.
- The `dockyard generate` CLI command and file-writing — Phase 18.
- The full `Result[Out]` handler return shape and oversized-payload validation —
  Phase 08 (RFC §6.3); Phase 04 ships the minimal `Text` plus typed `Structured`
  split.
- HTTP transport, security options, capability negotiation — Phase 07.

## Acceptance criteria

- [x] A Go contract struct generates a correct JSON Schema: object type, required vs.
      optional from `omitempty`/`omitzero`, `json` tag property names, `jsonschema`
      tag descriptions, nested structs and slices.
- [x] The generated schema marshals deterministically (stable, sorted property keys)
      so regeneration is byte-stable.
- [x] The typed tool builder produces a tool registered on a `runtime/server`; the
      registered tool's input and output schema is the generated schema.
- [x] The builder routes typed handler output to `structuredContent` and handler text
      to `content` (RFC §6.3).
- [x] Golden tests cover the generated schema output for a representative contract
      set (fixed input to a fixed `.golden` JSON file).
- [x] The builder rejects misuse (empty name, nil handler, non-object contract type,
      double registration) with typed errors — never a panic across the boundary.
- [x] `internal/codegen` and `runtime/tool` reach at least 80% coverage.

## Files added or changed

```text
internal/codegen/
  doc.go                 # package doc — Design A, the codegen home
  schema.go              # SchemaFor[T] + Marshal — Go type to deterministic JSON Schema
  schema_test.go         # unit tests
  golden_test.go         # golden tests over the contract fixtures
  testdata/
    *.golden             # golden JSON Schema outputs
runtime/tool/
  doc.go                 # package doc — the contract-first tool builder
  builder.go             # Builder[In,Out], New[In,Out], Describe/UI/Handler/Register
  result.go              # Result[Out] — Text + Structured + Meta (minimal, RFC §6.3)
  builder_test.go        # unit tests
  concurrency_test.go    # concurrent-reuse test under -race
runtime/server/
  tool.go                # add AddToolWithSchemas — register with explicit schemas
docs/plans/phase-04-codegen.md
docs/decisions.md         # D-029, D-030, D-031
docs/glossary.md          # new terms
scripts/smoke/phase-04.sh
test/integration/phase04_codegen_test.go  # codegen to builder to server wiring
```

## Public API surface

```go
// internal/codegen
func SchemaFor[T any]() (*jsonschema.Schema, error)
func SchemaForType(t reflect.Type) (*jsonschema.Schema, error)
func Marshal(s *jsonschema.Schema) ([]byte, error)   // deterministic, sorted keys

// runtime/tool
type Result[Out any] struct {
    Text       string
    Structured Out
    Meta       map[string]any
}
type Handler[In, Out any] func(ctx context.Context, in In) (Result[Out], error)
type Builder[In, Out any] struct { /* ... */ }
func New[In, Out any](name string) *Builder[In, Out]
func (b *Builder[In, Out]) Describe(desc string) *Builder[In, Out]
func (b *Builder[In, Out]) UI(resourceName string) *Builder[In, Out]
func (b *Builder[In, Out]) Handler(h Handler[In, Out]) *Builder[In, Out]
func (b *Builder[In, Out]) Register(s *server.Server) error

// runtime/server (added)
type ToolOutput[Out any] struct { Text string; Structured Out; Meta map[string]any }
type ToolOutputFunc[In, Out any] func(ctx context.Context, in In) (ToolOutput[Out], error)
func AddToolWithSchemas[In, Out any](s *Server, def ToolDef,
    in, out *jsonschema.Schema, fn ToolOutputFunc[In, Out]) error
```

## Test plan

- **Unit:** `SchemaFor` over scalars, structs, nested structs, slices, optional vs.
  required fields, description tags, error cases (channel/func/complex types,
  non-object top-level type). Builder misuse: empty name, nil handler, double
  register, non-object contract type. `Marshal` determinism (two calls byte-equal;
  key order independent of map iteration).
- **Integration:** `test/integration/phase04_codegen_test.go` — drive a contract
  through `codegen.SchemaFor` to `tool.New` to `Register` on a real `runtime/server`,
  serve over the SDK `InMemoryTransport`, call the tool, and assert the registered
  schema matches the generated one and that typed output lands in
  `structuredContent` and text in `content`. Consumes Phase 01's `runtime/server`
  (AGENTS.md §17 — Deps names a shipped phase).
- **Concurrency / golden:** golden tests in `internal/codegen` (fixed contract to a
  `.golden` JSON). `runtime/tool` concurrent-reuse test: N goroutines build and
  register tools on independent servers under `-race`.

## Smoke script additions

- `internal/codegen` package exists.
- `runtime/tool` builder package exists.
- `go test ./internal/codegen/... ./runtime/tool/...` passes.
- The golden testdata directory exists and is non-empty.
- `github.com/google/jsonschema-go` is a direct dependency in `go.mod`.
- The `internal/codegen` package builds CGo-free (`CGO_ENABLED=0`).

## Coverage target

- `internal/codegen` — 80% (new package).
- `runtime/tool` — 80% (new package).
- `runtime/server` — the added `AddToolWithSchemas` is covered by the new tests;
  the package's existing target is held.

## Dependencies

- Phase 01 — `runtime/server` (the typed `AddTool` the builder wraps).

## Risks / open questions

- **`google/jsonschema-go` is young (brief 06 R3).** Inference edge cases may shift
  between releases. Mitigated by golden tests — an upstream change surfaces as a
  diff, not a silent drift — and by pinning the version. RFC §18 Q-6 (lockstep with
  the SDK's pinned version) is noted; Phase 04 keeps `v0.4.3`, the version the SDK
  already pulls, so there is one schema dialect.
- **Inference is type-only — three Go shapes need Dockyard-side correction
  (depth-remediation).** The engine infers a property schema from its Go type
  alone, which a depth audit found lossy or wrong for real contracts: `time.Time`
  dropped its `format: date-time`, `json.RawMessage` rendered as a byte array, and
  a named-constant enum lost its `enum` array. All three are corrected inside the
  single schema dialect — `time.Time`/`json.RawMessage` via the engine's
  `TypeSchemas` hook, enums via the `WithEnum` option fed by `EnumsFromSource`
  (D-050, D-051). Golden fixtures now exercise every one of these shapes so a
  regression is a visible diff.
- **Recursive contracts are a documented V1 limitation (D-052).** A
  self-referential contract type cannot be expressed as a schema:
  `google/jsonschema-go` does not emit `$ref`/`$defs` for recursive Go types and
  exposes no hook to break the cycle, and forking the engine would create the
  divergent schema dialect RFC §6.2 settled against. `SchemaForType` detects the
  cycle up front and returns `ErrRecursiveContract` — a specific, actionable error
  citing D-052 — rather than leaking the engine's vague `cycle detected` string.
  The TypeScript half (tygo) handles recursion natively; only the schema half is
  constrained. Revisiting `$defs` support is deferred to a post-V1 phase; an
  author who needs a tree shape uses a non-recursive encoding (a flat node list
  with id references) in the meantime.
- **The builder's generic shape departs from the brief sketch (D-029).** Risk is
  bounded: the departure is a Go-language constraint, not a design change, and the
  fluent contract-first ergonomics survive.
- **`structuredContent` routing is minimal here.** Phase 08 owns the full
  `Result[Out]` semantics and oversized-payload validation; Phase 04 ships only the
  split so the builder is usable end-to-end.

## Glossary additions

- **Contract struct** — a Go input or output struct that is the single source of
  truth for a tool's schema; JSON Schema and TypeScript are generated from it.
- **Generated schema** — the JSON Schema produced from a contract struct by
  `internal/codegen`; never hand-written (P1).
- **Tool builder** — the `runtime/tool` fluent, typed API an app author uses to
  declare a tool: bind input/output contract types, attach a handler, register.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages at least the stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change to concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed to integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
