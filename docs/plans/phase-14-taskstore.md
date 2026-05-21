# Phase 14 — taskstore

## Summary

Phase 14 completes the server-side MCP Tasks extension: it supplies the durable
`TaskStore` driver on the `Store` seam, the `TaskHandle` handler API (progress,
status messages, cooperative cancellation, `input_required` elicitation), the
manifest-tunable lifecycle controls (max TTL, per-requestor concurrency cap,
background TTL purge sweep), and the task security model (crypto-strong IDs,
auth-context binding, `tasks/list` withholding). It also folds in the transport
mount Phase 13 deferred: `tasks/*` JSON-RPC frames are routed into
`Engine.Dispatch` ahead of the SDK server on stdio and streamable-HTTP, and the
`tasks` capability is injected into the `initialize` handshake, so a real MCP
client drives `tasks/*` end to end over the wire.

## RFC anchor

- RFC §8.4 — the handler API: handlers stay sync-shaped; `TaskHandle` for
  progress / status / cooperative cancellation / `input_required` elicitation;
  raw experimental protocol structs never reach the handler-facing API.
- RFC §8.5 — the TaskStore on the storage seam: max TTL, per-requestor
  concurrent-task cap, background TTL purge sweep, all manifest-tunable;
  crypto-strong (≥128-bit) task IDs; auth-context binding rejects cross-context
  `tasks/get|result|cancel`; `tasks/list` scopes to the caller and is not
  advertised in unauthenticated single-user stdio mode.
- RFC §8.2 — the V1 implementation is a shim, by necessity (the go-sdk has no
  Tasks API). Phase 14 mounts the shim on the live transport.
- RFC §13 — the `Store` seam: durable task state is a `Store` persistence
  concern, forward-only migrations, proven by a shared conformance suite.
- RFC §15 — security: crypto-strong task IDs, auth-context binding, `tasks/list`
  withholding, no hardcoded secrets.

## Briefs informing this phase

- brief 02 — MCP Tasks extension
- brief 06 — Go stack and toolchain

## Brief findings incorporated

- **brief 02 §4.5 / §5 ("Avoid")** — without an auth context, task IDs are the
  only access control, so the default generator MUST be crypto-strong (128-bit
  `crypto/rand`); the runtime MUST NOT advertise `tasks/list` when it cannot
  identify requestors; with an auth context `tasks/get|result|cancel` reject
  cross-context access and `tasks/list` scopes to the caller. Implemented as the
  engine's `AuthContext`-bound dispatch and the `RequestorIdentifiable` gate on
  the `tasks` capability.
- **brief 02 §4.6 / §3** — tasks are durable state; a leak is unbounded DB
  growth, so the runtime needs an enforced max TTL, a per-requestor concurrent-
  task cap, and a background purge sweep — all manifest-level config. Implemented
  as the `tasks` manifest block (`max_ttl`, `default_ttl`, `purge_interval`,
  `max_concurrent_per_requestor`) feeding the engine's `Lifecycle` options.
- **brief 02 §3 (the TaskStore sketch) / §5 "Build"** — the brief's `TaskStore`
  carries a `Purge(ctx, now)` sweep and auth-scoped `List`. Phase 14 adds
  `Delete`, `PurgeExpired` and `ListByAuthContext` to the Phase 13 seam and
  implements them in every driver.
- **brief 02 §4.7 / §5** — `cancelled` is cooperative: a handler observes `ctx`
  cancellation and unwinds; a late terminal transition on an already-cancelled
  task is ignored, never an error. The `TaskHandle` surfaces cancellation through
  `ctx` and `Cancelled()`, never a forced kill.
- **brief 06 §2.8 / §4 R6 (D-026)** — `modernc.org/sqlite` is pure-Go and
  CGo-free and cross-compiles cleanly; the durable `TaskStore` is a typed facade
  over the existing `Store` seam, so it inherits the sqlite driver with no new
  CGo dependency.

## Findings I'm departing from (if any)

- **brief 02 §3 — the `TaskStore` is sketched as a *new* `Store`-level driver.**
  The settled `Store` seam (D-025) is deliberately a generic namespaced KV
  primitive, with sub-stores (TaskStore, ObsStore) built as **thin typed facades
  over a `Store`** rather than as separate drivers. Phase 14 follows D-025: the
  durable `TaskStore` is a facade constructed over any `store.Store` (inmem or
  sqlite), owning its own forward-only migration and namespace. CLAUDE.md §9's
  "proven by the shared conformance suite" is honoured by a dedicated
  `TaskStore` conformance suite (`runtime/tasks/taskstoretest`) run against
  every backing — the Phase 13 in-memory stub, the durable facade over the inmem
  `Store`, and the durable facade over the sqlite `Store`. Filed as **D-070**.

## Goals

- A durable `TaskStore` driver, a typed facade over the `Store` seam, with a
  forward-only migration, exercised by a shared `TaskStore` conformance suite
  against every backing.
- The `TaskHandle` handler API: progress, status messages, cooperative
  cancellation, `input_required` elicitation — handlers stay sync-shaped.
- Manifest-tunable lifecycle controls: enforced max TTL, per-requestor
  concurrent-task cap, a background TTL purge sweep that shuts down cleanly.
- The task security model: crypto-strong IDs (kept from Phase 13), auth-context
  binding on `tasks/get|result|cancel`, `tasks/list` scoped to the caller and
  withheld when requestors are not identifiable.
- The transport mount: `tasks/*` frames routed into `Engine.Dispatch` over stdio
  and streamable-HTTP; the `tasks` capability injected into `initialize`.

## Non-goals

- A Postgres `Store` driver — the seam admits one later (RFC §13); V1 ships
  inmem + sqlite.
- The inspector's task-lifecycle rendering (RFC §8.6 — a later inspector phase).
- `notifications/tasks/status` push delivery — polling is the contract of record
  (RFC §8.3); a later phase may add the best-effort notification.
- The `dockyard.app.yaml` Tasks block surfaced through the CLI `validate`/`new`
  flow — Phase 14 adds the manifest schema + loader support and the example
  manifest; CLI surfacing is a later CLI phase.

## Acceptance criteria

- [ ] A long-running handler reports progress through a `TaskHandle` and is
      cooperatively cancellable — `tasks/cancel` cancels its context, the handler
      observes it and unwinds, the task ends `cancelled`.
- [ ] The background TTL purge sweep reaps expired tasks; an expired task is
      gone from the store after a sweep.
- [ ] Cross-context task access is rejected — a task created under one auth
      context is not reachable via `tasks/get|result|cancel` from another, with a
      typed rejection that does not leak the task's existence.
- [ ] `tasks/list` is withheld (not advertised, not served) when requestors are
      not identifiable (unauthenticated single-user stdio mode); when advertised
      it scopes to the calling auth context.
- [ ] The durable `TaskStore` driver passes the shared `TaskStore` conformance
      suite against the inmem and sqlite `Store` backings; forward-only migration.
- [ ] **(folded in)** A real MCP client drives `tasks/get`/`result`/`cancel`/
      `list` end to end over a real transport — `tasks/*` frames reach
      `Engine.Dispatch` and the `tasks` capability is in the `initialize` result.
- [ ] Each new `tasks` manifest key is documented in `internal/manifest`, the
      example manifest, and the smoke script.

## Files added or changed

- `runtime/tasks/store.go` — extend the `TaskStore` seam: `Delete`,
  `PurgeExpired`, `ListByAuthContext`; implement them on the in-memory stub.
- `runtime/tasks/storedriver.go` — the durable `TaskStore` facade over the
  `Store` seam, with its forward-only migration and codec-free JSON row format.
- `runtime/tasks/handle.go` — the `TaskHandle` handler API.
- `runtime/tasks/lifecycle.go` — TTL enforcement + the background purge sweep.
- `runtime/tasks/security.go` — auth-context binding helpers.
- `runtime/tasks/transport.go` — the `tasks/*` transport mount + capability
  injection seam.
- `runtime/tasks/engine.go` — auth-bound dispatch; `TaskHandle`-shaped run;
  TTL/concurrency-cap enforcement on create; identifiability gate.
- `runtime/tasks/taskstoretest/conformance.go` — the shared `TaskStore`
  conformance suite (new package; AGENTS.md §3 — under `runtime/tasks/`).
- `runtime/tasks/*_test.go` — unit, concurrency, golden tests.
- `runtime/server/http.go`, `runtime/server/stdio` mount — wire `tasks/*` and
  the `tasks` capability into the transports.
- `internal/manifest/manifest.go`, `internal/manifest/validate.go` — the `tasks`
  manifest block + its validation.
- `internal/manifest/testdata/valid-full.yaml`, `bad-*.yaml` — fixtures.
- `examples/customer-health/dockyard.app.yaml` — example `tasks` block.
- `test/integration/phase14_taskstore_test.go` — integration test.
- `scripts/smoke/phase-14.sh` — one assertion per acceptance criterion.
- `docs/plans/phase-14-taskstore.md`, `docs/plans/README.md` (Phase 14 block),
  `docs/decisions.md`, `docs/glossary.md`.

## Public API surface

```go
// runtime/tasks — the durable TaskStore facade
func NewStore(s store.Store) (TaskStore, error)        // durable, over the Store seam
func RegisterMigrations()                              // forward-only TaskStore migration

// the extended TaskStore seam
type TaskStore interface {
	Create(ctx context.Context, rec TaskRecord) error
	Get(ctx context.Context, id string) (TaskRecord, error)
	Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (TaskRecord, error)
	SetResult(ctx context.Context, id string, result TaskResult) error
	List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error)
	ListByAuthContext(ctx context.Context, authContext, cursor string, limit int) ([]TaskRecord, string, error)
	Delete(ctx context.Context, id string) error
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}

// the TaskHandle handler API
type TaskHandle interface {
	Progress(ctx context.Context, fraction float64, message string) error
	Status(ctx context.Context, message string) error
	Cancelled() bool
	RequireInput(ctx context.Context, prompt InputPrompt) (InputResponse, error)
}
type HandleFunc func(ctx context.Context, h TaskHandle) (json.RawMessage, error)

// lifecycle controls
type Lifecycle struct {
	MaxTTL                    time.Duration
	DefaultTTL                time.Duration
	PurgeInterval             time.Duration
	MaxConcurrentPerRequestor int
}

// the transport mount
type Mount struct { /* ... */ }
func NewMount(e *Engine) *Mount
func (m *Mount) ServeStdio(ctx context.Context, ...) error
func (m *Mount) HTTPMiddleware(next http.Handler) http.Handler
```

## Test plan

- **Unit:** the durable `TaskStore` JSON row round-trip; TTL enforcement
  (requested > max clamps to max; default applied when absent); the purge sweep
  reaps only expired tasks; the per-requestor concurrency cap rejects an
  over-cap create; `TaskHandle` progress/status transitions; auth-context
  rejection is a typed error; the identifiability gate.
- **Integration:** `test/integration/phase14_taskstore_test.go` — the durable
  `TaskStore` over a real `modernc.org/sqlite` `Store` (no mocks at the seam):
  auth-context propagation and cross-context rejection; ≥1 failure mode per seam
  (a cross-context `tasks/get`; a purge racing a live task); an N≥10 concurrency
  stress (concurrent creates against the cap; the purge sweep racing live tasks)
  under `-race` with a post-teardown goroutine-leak assertion; a real MCP client
  driving `tasks/*` over a transport.
- **Conformance:** the `TaskStore` conformance suite runs against the in-memory
  stub, the durable facade over `inmem`, and the durable facade over `sqlite`.
- **Concurrency / golden:** the purge sweep and the durable `TaskStore` are
  reusable concurrent artifacts — concurrent-reuse tests under `-race`. Golden
  tests pin the durable row JSON and the injected `tasks` capability block.

## Smoke script additions

- `runtime/tasks` durable `TaskStore` (`storedriver.go`) builds CGo-free.
- The `TaskHandle` API is present (progress, cooperative cancellation).
- The TTL purge sweep is present.
- Auth-context binding rejects cross-context access.
- `tasks/list` is withheld when requestors are not identifiable.
- The transport mount routes `tasks/*` into `Engine.Dispatch`.
- Each `tasks` manifest key (`max_ttl`, `default_ttl`, `purge_interval`,
  `max_concurrent_per_requestor`) is defined and loads.
- The `TaskStore` conformance suite exists and runs against every backing.

## Coverage target

- `runtime/tasks` — 85% (the durable `TaskStore` is a conformance-tested
  persistence subsystem; CLAUDE.md §11).
- `runtime/tasks/taskstoretest` — the conformance suite itself; exercised by the
  driver tests.
- `internal/manifest` — additions keep the package ≥ its existing target.

## Dependencies

- Phase 13 — the Tasks engine, the `TaskStore` seam, the `tasks/*` router.
- Phase 03 — the `Store` seam, the inmem + sqlite drivers, the migration runner
  and the shared `Store` conformance suite.

## Risks / open questions

- **RFC §18 Q-1 / Q-7** — the go-sdk rejects unknown JSON-RPC methods before
  middleware; Phase 14 mounts `tasks/*` ahead of the SDK server, the documented
  shim seam. Cancellation propagation is cooperative — a handler that ignores
  `ctx` keeps running underneath while the task stays `cancelled` (brief 02
  §4.7); the `TaskHandle` makes the cooperative contract explicit.
- The durable `TaskStore` stores task rows as JSON KV values rather than a typed
  SQL schema — the `Store` seam is intentionally a KV primitive (D-025). A
  future at-scale need for indexed task queries would motivate a typed schema;
  for V1 the KV facade with a per-auth-context index key is sufficient.

## Glossary additions

- **`TaskHandle`** — the handler-facing API for a long-running task: progress,
  status, cooperative cancellation, `input_required` elicitation.
- **TaskStore (durable)** — the `Store`-seam-backed `TaskStore` driver.
- **TTL purge sweep** — the background goroutine that reaps expired tasks.
- **Auth-context binding** — scoping `tasks/*` access to the requestor's
  authorization context.
- **Tasks transport mount** — the seam routing `tasks/*` JSON-RPC frames into
  `Engine.Dispatch` ahead of the SDK server.

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
