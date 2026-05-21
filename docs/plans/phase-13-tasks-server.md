# Phase 13 — tasks-server

## Summary

Phase 13 ships the server-side MCP Tasks extension shim (RFC §8.2): the
`tasks/*` method router, the `tasks` capability advertisement, the
`CreateTaskResult` substitution for a task-augmented `tools/call`, and the
five-status lifecycle with enforced transitions. The Tasks JSON-RPC wire layer
(method envelopes, results, capability block) is extended inside
`internal/protocolcodec`; the routing, lifecycle and engine live in a new
`runtime/tasks` package. The durable `TaskStore`, the `TaskHandle` handler API,
TTL/concurrency/purge, crypto-strong IDs and auth binding are Phase 14 — this
phase leaves the `TaskStore` seam they plug into.

## RFC anchor

- RFC §8.1 — authoritative source (the `experimental-ext-tasks` schema, not the
  overview page).
- RFC §8.2 — the V1 implementation is a shim, by necessity (the go-sdk has no
  Tasks API; Dockyard routes `tasks/*` itself behind the `protocolcodec` seam).
- RFC §8.3 — lifecycle and methods (five statuses; `tasks/get`/`result`/`cancel`/
  `list`).

## Briefs informing this phase

- brief 02 — MCP Tasks extension
- brief 03 — go-mcp-sdk audit

## Brief findings incorporated

- **brief 02 §2.2** — five statuses, `working` mandatory initial; legal
  transitions `working → {input_required, completed, failed, cancelled}` and
  `input_required → {working, completed, failed, cancelled}`; the three terminal
  statuses are immutable. Implemented as `tasks.Engine` lifecycle enforcement,
  building on `protocolcodec.TaskStatus.CanTransitionTo`.
- **brief 02 §2.3** — the authoritative method table: `tasks/get` non-blocking,
  `tasks/result` blocks until terminal, `tasks/cancel` MUST transition to
  `cancelled` before responding, `tasks/list` cursor-paginated. The engine
  serves exactly these four; no `tasks/update` (overview-page artifact, §2.4).
- **brief 02 §2.6 / §2.8** — the server advertises `tasks` (`list`, `cancel`,
  `requests.tools.call`) and MUST check the negotiated capability before
  returning a `CreateTaskResult`; per-tool `execution.taskSupport`
  (`forbidden` default) is the finer second layer.
- **brief 03 R3 / brief 02 §4.3** — the go-sdk has no Tasks surface and its
  receiving-method dispatch (`serverMethodInfos`) is a fixed package map an
  unknown method never reaches; Dockyard routes `tasks/*` itself. The engine
  exposes a transport-agnostic `Dispatch` the Phase 14 transport wiring and the
  inspector drive.
- **brief 02 §4.5 / §4.7** — task IDs are the only access control absent an auth
  context, so the default generator is crypto-strong (128-bit `crypto/rand`);
  `cancelled` is cooperative — a late terminal transition on an already-cancelled
  task is ignored, never an error.

## Findings I'm departing from (if any)

- **brief 02 §3 / §5 ("code generation of wire types from the upstream
  schema").** The brief and the Phase 13 master-plan goal say the wire layer is
  "code-generated" from `mcp-tasks-experimental.schema.ts`. The settled Phase 02
  pattern (D-010) instead hand-derives the `protocolcodec` wire structs from the
  vendored schema and pins them with **golden tests** that are themselves the
  spec-compliance assertion — there is no `ts → Go` generator in the tree and
  the Apps wire layer follows the same hand-derived + golden pattern. Phase 13
  aligns with the established `protocolcodec` pattern rather than introducing a
  one-off generator: a spec bump is still regenerate-and-diff in spirit
  (re-derive against the new vendored snapshot; the golden tests surface every
  shape change as a diff). Filed as **D-069**.

## Goals

- Route the four `tasks/*` JSON-RPC methods on a server-side Tasks engine.
- Advertise the `tasks` capability in the initialize handshake, capability-driven.
- Substitute a `CreateTaskResult` for a task-augmented `tools/call` instead of
  the immediate tool result.
- Enforce the five-status lifecycle; an illegal transition is a typed error.
- Extend the `protocolcodec` Tasks wire layer with the method envelopes.
- Leave a clean `TaskStore` seam for Phase 14's durable driver.

## Non-goals

- The durable `TaskStore` driver on the `Store` seam (Phase 14).
- The `TaskHandle` handler API — progress, cooperative cancellation,
  `input_required` elicitation (Phase 14).
- TTL enforcement, per-requestor concurrency caps, the purge sweep (Phase 14).
- Auth-context binding and `tasks/list` withholding when unauthenticated
  (Phase 14 — the seam carries an `AuthContext` so Phase 14 enforces it).
- Mounting the engine onto the live SDK transport dispatch (Phase 14 wires the
  transport seam; Phase 13 ships the engine and its `Dispatch`).
- `dockyard.app.yaml` `task_support` manifest field (Phase 06 owns the manifest;
  surfaced end-to-end in a later CLI phase).

## Acceptance criteria

- [ ] A task-augmented `tools/call` returns a `CreateTaskResult` (the `task`
      object, status `working`) instead of the immediate tool result.
- [ ] `tasks/get`, `tasks/result`, `tasks/cancel`, `tasks/list` behave per the
      vendored spec (`tasks/get` non-blocking; `tasks/result` blocks to terminal;
      `tasks/cancel` transitions to `cancelled` before responding; `tasks/list`
      cursor-paginated).
- [ ] Lifecycle transitions are enforced — an illegal transition is a typed
      error, never a panic across the MCP boundary.
- [ ] The `tasks` capability is advertised, capability-driven (no host matrix).
- [ ] Every `tasks/*` wire shape is encoded/decoded through
      `internal/protocolcodec`; no raw envelope key is hand-built outside it.

## Files added or changed

- `internal/protocolcodec/tasks.go` — add the method-envelope domain types.
- `internal/protocolcodec/codec.go` — add the `tasks/*` envelope codec methods.
- `internal/protocolcodec/codec_test.go`, `golden_test.go` — envelope coverage.
- `runtime/tasks/doc.go` — package doc (new package; AGENTS.md §3 names it).
- `runtime/tasks/engine.go` — the `Engine`: `tasks/*` routing + lifecycle.
- `runtime/tasks/store.go` — the `TaskStore` seam + the in-memory stub store.
- `runtime/tasks/capability.go` — `tasks` capability advertisement helper.
- `runtime/tasks/errors.go` — typed Tasks errors (JSON-RPC code mapping).
- `runtime/tasks/*_test.go` — unit, golden, concurrency tests.
- `test/integration/phase13_tasks_test.go` — integration test (real engine +
  real `protocolcodec`).
- `scripts/smoke/phase-13.sh` — one assertion per acceptance criterion.
- `docs/plans/phase-13-tasks-server.md`, `docs/decisions.md`, `docs/glossary.md`.

## Public API surface

```go
// runtime/tasks — the Tasks engine
type Engine struct { /* ... */ }
func NewEngine(store TaskStore, opts *Options) (*Engine, error)
func (e *Engine) Dispatch(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
func (e *Engine) CreateForToolCall(ctx context.Context, p CreateToolCallParams) (json.RawMessage, error)
func (e *Engine) Capability() protocolcodec.TasksServerCapability
func (e *Engine) CapabilityJSON() (json.RawMessage, error)
type RunFunc func(ctx context.Context) (json.RawMessage, error)

// the Phase 14 seam — durable TaskStore plugs in here
type TaskStore interface {
	Create(ctx context.Context, rec TaskRecord) error
	Get(ctx context.Context, id string) (TaskRecord, error)
	Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (TaskRecord, error)
	SetResult(ctx context.Context, id string, result TaskResult) error
	List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error)
}
func NewInMemoryStore() TaskStore   // Phase 13 stub driver
```

The Tasks capability is the top-level `capabilities.tasks` object, not a
`capabilities.extensions` entry — so it is surfaced as `CapabilityJSON()`, not
a `server.ExtensionCapability`. The go-sdk has no native `capabilities.tasks`
field; the Phase 14 transport mount injects this block into the handshake.

## Test plan

- **Unit:** lifecycle transition table (every legal/illegal pair); each `tasks/*`
  method against the in-memory store; `CreateTaskResult` substitution; capability
  encoding; typed-error → JSON-RPC code mapping; the codec envelope round-trips.
- **Integration:** `test/integration/phase13_tasks_test.go` — a task-augmented
  `tools/call` driven through a real `Engine` over the real `protocolcodec`
  codec, polled via `tasks/get` to terminal, result fetched via `tasks/result`;
  ≥1 failure mode — an illegal lifecycle transition and a `tasks/cancel` of an
  already-terminal task both surface the spec's `-32602`.
- **Concurrency / golden:** `Engine` is a reusable concurrent artifact —
  concurrent `Dispatch` + `CreateForToolCall` under `-race`. Golden tests pin
  the exact wire JSON of `CreateTaskResult`, `GetTaskResult`, `ListTasksResult`
  and the `tasks` capability block.

## Smoke script additions

- `runtime/tasks` package exists and builds CGo-free.
- `runtime/tasks` routes wire shapes through `protocolcodec` — no raw envelope
  keys.
- The five lifecycle statuses and the `tasks/*` method names are defined.
- `runtime/tasks` + `protocolcodec` tests pass.
- The Phase 13 integration test is present.

## Coverage target

- `runtime/tasks` — 85% (the master plan sets Phase 13 at 85%).
- `internal/protocolcodec` — additions keep the package ≥ 85%.

## Dependencies

- Phase 07 — MCP server core (the `server.ExtensionCapability` seam, transports).
- Phase 02 — the `protocolcodec` seam (the Tasks domain types live there).

## Risks / open questions

- **RFC §18 Q-1 / Q-7** — the engine routes `tasks/*` itself because the go-sdk
  cannot (its `serverMethodInfos` is a fixed package map). The transport mount —
  feeding `tasks/*` JSON-RPC frames into `Engine.Dispatch` ahead of the SDK
  server — is left as the documented Phase 14 seam. Cancellation propagation to
  a running handler is cooperative and also Phase 14 (the `TaskHandle` API).
- The in-memory stub store is process-local and unbounded; Phase 14 replaces it
  with the durable `Store`-backed driver carrying TTL and caps.

## Glossary additions

- **Task** — a durable MCP Tasks state machine wrapping a task-augmented request.
- **Task lifecycle** — the five-status state machine and its legal transitions.
- **`CreateTaskResult`** — the result returned for an accepted task-augmented
  request, wrapping the `Task` object.
- **`input_required`** — the non-terminal task status meaning the receiver needs
  input from the requestor.
- **Tasks engine** — `runtime/tasks.Engine`, the server-side `tasks/*` router.
- **`TaskStore`** — the Phase 14 persistence seam for durable task state.

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
