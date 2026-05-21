# Phase 08 — handler-runtime

## Summary

Phase 08 turns the minimal `Result[Out]` and the basic `content` /
`structuredContent` routing that Phase 04 shipped into the production tool
handler runtime: incoming tool-call arguments are validated against the tool's
generated input JSON Schema **at the catalog edge** and produce a typed error;
the `content` / `structuredContent` split is hardened so no empty `TextContent`
block is emitted when a handler returns no model-facing text; and oversized or
misrouted (UI-shaped data leaked into `content`) payloads are detected and
flagged. The work lands in `runtime/tool` on top of `runtime/server`'s
`AddToolWithSchemas` seam.

## RFC anchor

- RFC §5 — MCP server core (tool registration, the SDK foundation, the
  `protocolcodec` boundary the handler runtime stays behind).
- RFC §6.3 — `content` vs `structuredContent`: typed output → `structuredContent`,
  model-facing text → `content`; routing UI payloads into `content` pollutes and
  inflates model context.

## Briefs informing this phase

- brief 01 — MCP Apps extension.
- brief 03 — Go MCP SDK audit.

## Brief findings incorporated

- **brief 01 §2.6** — "`content` is the model-facing representation;
  `structuredContent` is the UI-optimized payload … deliberately excluded from
  the model's reasoning context." Phase 08's content split routes the handler's
  typed `Structured` output exclusively to `structuredContent` and the handler's
  `Text` exclusively to `content`.
- **brief 01 §3 (finding 7), "`structuredContent` vs `content` discipline"** —
  "Putting large UI payloads in `content` pollutes (and inflates) model context.
  Dockyard's typed-output path must route UI data exclusively to
  `structuredContent`; the braindump's 'oversized output payloads' warning maps
  directly here." Phase 08 adds the runtime detector for that misroute and for
  oversized outputs.
- **brief 03 §2 (tools)** — "the SDK … validates incoming arguments against the
  input schema, and unmarshals into the typed `In`." Phase 08 keeps the SDK's
  decode but adds a Dockyard-owned edge validation pass against the *generated*
  schema so an invalid argument is a typed Dockyard error, not a generic SDK
  string (D-044).
- **brief 03 §4 (R7)** — "`_meta` is untyped … Dockyard should wrap `_meta`
  access in typed helpers so … bugs surface in Dockyard's own validation, not at
  runtime in a host." Phase 08's flagging surface follows the same principle:
  payload-routing defects surface as typed Dockyard `Flag` values, observable
  before a host ever sees the result.

## Findings I'm departing from (if any)

None.

## Goals

- Validate incoming tool-call arguments against the tool's generated input JSON
  Schema before the handler runs; an invalid argument is a typed Dockyard error.
- Harden the `content` / `structuredContent` split (RFC §6.3): typed output to
  `structuredContent`, model text to `content`, and **no empty `TextContent`
  block** when the handler returns no text.
- Detect and flag oversized tool outputs and UI-shaped data misrouted into
  `content`, as a runtime signal complementing the static `dockyard validate`
  warning.

## Non-goals

- The Apps `ui://` resource layer and `_meta.ui` wiring — Phase 09.
- The Tasks layer (`TaskHandle`, progress, cancellation) — Phase 13.
- The static `dockyard validate` content-misroute warning — that is the
  validate command's job; Phase 08 ships the *runtime* detector, not the CLI
  surface.
- Emitting the flags as `obs/v1` events — the `obs` runtime is a later phase;
  Phase 08 exposes flags through a typed accessor a future obs bridge consumes.

## Acceptance criteria

- [ ] A handler's typed output lands in `CallToolResult.structuredContent`, and
      the handler's model-facing text lands in `content[]`.
- [ ] No empty `TextContent` block is emitted when the handler returns no model
      text (`Result.Text == ""`) — the Wave 2 audit quirk is fixed (D-043).
- [ ] Invalid tool-call arguments (schema-violating) produce a typed Dockyard
      error (`*tool.ArgumentError`, `errors.Is(err, tool.ErrInvalidArguments)`)
      before the handler runs — never a panic, never a vague failure.
- [ ] An oversized tool output is detected and surfaced as a typed `tool.Flag`.
- [ ] UI-shaped data misrouted into `content` is detected and surfaced as a
      typed `tool.Flag`.
- [ ] The handler runtime is safe under concurrent tool calls, proven under
      `-race`.

## Files added or changed

```text
docs/plans/phase-08-handler-runtime.md   (new) this plan
docs/decisions.md                        (changed) D-043, D-044, D-045
docs/glossary.md                         (changed) edge validation, routing flag, content split
scripts/smoke/phase-08.sh                (new) smoke assertions
runtime/server/tool.go                   (changed) empty-TextContent fix (D-043)
runtime/server/toolschema_test.go        (changed) empty-block assertion
runtime/tool/runtime.go                  (new) edge validation + content split + flagging
runtime/tool/runtime_test.go             (new) unit tests
runtime/tool/flag.go                     (new) Flag type + detection thresholds
runtime/tool/flag_test.go                (new) flag detection tests
runtime/tool/builder.go                  (changed) Register wires the handler runtime
runtime/tool/builder_test.go             (changed) edge-validation + flag coverage
runtime/tool/concurrency_test.go         (changed) concurrent-call test for the runtime
test/integration/handler_runtime_test.go (new) end-to-end over InMemoryTransport
```

## Public API surface

```go
// runtime/tool

// ErrInvalidArguments is the sentinel for an edge argument-validation failure.
var ErrInvalidArguments = errors.New(...)

// ArgumentError is the typed error produced when incoming tool-call arguments
// fail validation against the tool's generated input schema. It wraps
// ErrInvalidArguments.
type ArgumentError struct { Tool string; Detail string }
func (e *ArgumentError) Error() string
func (e *ArgumentError) Unwrap() error

// FlagKind classifies a handler-runtime payload-routing defect.
type FlagKind int
const (
    FlagOversizeOutput   FlagKind = iota + 1 // structuredContent exceeds the size budget
    FlagMisroutedContent                     // UI-shaped data leaked into content[]
)

// Flag is a typed, non-fatal handler-runtime signal: an oversized or misrouted
// payload. It does not fail the tool call; it is recorded for inspection.
type Flag struct { Kind FlagKind; Tool string; Detail string; SizeBytes int }

// Flags reports the flags raised by a Builder's handler so far, newest last.
// The returned slice is a copy and safe to retain.
func (b *Builder[In, Out]) Flags() []Flag
```

`runtime/server` keeps its existing `AddToolWithSchemas` surface; only the
internal handler body changes (the empty-`TextContent` fix).

## Test plan

- **Unit:** edge validation accepts a valid argument and rejects each schema
  violation class (missing required field, wrong type, unknown field) with a
  typed `*ArgumentError`; the content split routes `Text`→`content`,
  `Structured`→`structuredContent`; the empty-`TextContent` fix emits zero
  content blocks when `Text == ""` and exactly one when `Text != ""`; flag
  detection raises `FlagOversizeOutput` past the size budget and
  `FlagMisroutedContent` when UI-shaped JSON is placed in `Text`.
- **Integration:** `test/integration/handler_runtime_test.go` registers a tool
  through the contract-first builder, serves it over `InMemoryTransport`, and a
  real MCP client asserts (a) a valid call returns typed output in
  `structuredContent` with no empty content block, (b) an invalid-argument call
  surfaces a typed error, (c) an oversized handler output raises a flag — real
  drivers on the `runtime/server` ↔ `runtime/tool` seam (AGENTS.md §17).
- **Concurrency / golden:** a concurrent-reuse test issues N parallel tool calls
  against one registered tool under `-race`, asserting flags accumulate without
  data races and each call's result is independent. No golden output — this
  phase produces no codegen artifact.

## Smoke script additions

- `runtime/tool/runtime.go` exists.
- `runtime/tool/flag.go` exists and defines `FlagOversizeOutput` /
  `FlagMisroutedContent`.
- `runtime/server/tool.go` no longer emits an unconditional `TextContent{Text:
  out.Text}` block (the empty-block fix is present).
- `runtime/tool` builds CGo-free.
- `runtime/tool` and `runtime/server` tests pass.
- `tool.ErrInvalidArguments` / `tool.ArgumentError` are present.
- `Builder.Flags()` accessor is present.

## Coverage target

- `runtime/tool` — 85% (conformance-tested handler-runtime subsystem).
- `runtime/server` — the empty-`TextContent` fix is covered by the package's
  existing 85% target; Phase 08 only adds an assertion, no new uncovered code.

## Dependencies

- Phase 07 — `runtime/server` (`AddToolWithSchemas`, `ToolOutput`, transports).
- Phase 04 — `runtime/tool` (the contract-first builder, `Result[Out]`),
  `internal/codegen` (`SchemaFor`).

## Risks / open questions

- **SDK double validation.** The go-sdk already validates arguments against the
  input schema with a generic message. Dockyard's edge validation runs *first*,
  against the same generated schema, so an invalid argument is caught and typed
  before the SDK sees it; the SDK pass becomes a redundant safety net, not a
  conflicting one. Documented in D-044.
- **Size budget value.** The oversized-output threshold is a heuristic, not a
  protocol limit. Phase 08 picks a conservative default (256 KiB) and makes the
  flag non-fatal so a large-but-legitimate payload is observable, never blocked.
  Documented in D-045.
- **Misroute detection is heuristic.** Detecting UI-shaped JSON in `content`
  cannot be exact without a host. Phase 08 flags the high-confidence case —
  `Text` that parses as a JSON object or array — and keeps it a non-fatal
  warning.

## Glossary additions

- **Edge validation** — argument validation at the catalog edge.
- **Handler runtime** — the `runtime/tool` layer that validates, routes, and
  flags a tool call.
- **Content split** — the `content` / `structuredContent` routing rule.
- **Routing flag** — a typed non-fatal handler-runtime signal.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
