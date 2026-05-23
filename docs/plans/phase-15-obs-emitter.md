# Phase 15 — obs/v1 event model + headless emitter

## Summary

Phase 15 implements the foundation of Dockyard's observability protocol
(RFC §11.1/§11.2): the canonical, versioned `obs.Event` model and event kinds, a
non-blocking headless emitter behind an interface + factory + driver seam, the
in-memory ring-buffer driver, W3C Trace Context correlation IDs, and shape+size
default capture. `runtime/server`, `runtime/apps`, and `runtime/tasks` are
instrumented to *emit* the obs/v1 stream — nothing reads another subsystem's
internals to observe (P2). It also folds in the Wave 5 checkpoint **S1** fix:
the Store migration registry is made `t.Parallel()`-safe by replacing the
mutable process-global with a caller-owned `MigrationSet`.

> **Remediation note (R5, depth audit 2).** Phase 15 introduced
> `obs.WithSession`/`sessionFromContext` and made `obs.Event.SessionID` part
> of the versioned `obs/v1` wire contract, but `Recorder.emit` never read the
> ctx-stamped session id and no transport ever called `obs.WithSession` — the
> wire field was always empty, and the doc comment's claim that "Phase 16's
> transports populate it" was untrue. Remediation **R5** wires it end to end:
> `Recorder.emit` stamps `e.SessionID = sessionFromContext(ctx)`, and the
> tool-handler edge (`runtime/server.withRequestSession`) and the resource-
> handler edge (`withResourceRequestSession`) call
> `obs.WithSession(ctx, req.Session.ID())`. The handler-edge wiring is the
> single choke point every tool and resource handler passes through; a
> future per-transport propagation pass can layer in front of it without
> changing the emit sites. See **D-120**, `runtime/obs/recorder_test.go`
> (`TestRecorder_EmitStampsSessionID`), and
> `runtime/server/r5_obs_wiring_test.go` (the streamable-HTTP transport
> exercises `req.Session.ID()` — in-memory / IO / SSE transports return ""
> per the SDK contract, so the test uses HTTP).

## RFC anchor

- RFC §11.1 — observability is a protocol (settled).
- RFC §11.2 — the event model.
- RFC §11.3 — read (not implemented here) to leave the right seam for Phase 16's
  SSE sink, OTel adapter, and MCP `logging` bridge.

## Briefs informing this phase

- brief 05 — Observability & competitive landscape.

## Brief findings incorporated

- **"Observability as a protocol — the Harbor Console model"** (brief 05 §3,
  §1): the runtime is headless and emits a canonical, versioned event stream;
  the inspector and the post-V1 console are pure clients. Implemented as the
  `obs.Emitter` seam — the runtime depends only on the interface.
- **The canonical event shape** (brief 05 §3.1): `obs.Event` carries
  `schema_version`, identity, correlation IDs, `kind`, `phase`, a typed
  `payload`, optional `duration_ms`, and an `ErrorInfo` with a `silent` flag for
  protocol-masked failures (the Sentry insight, brief 05 §2.2). Implemented
  verbatim; the wire shape is golden-pinned.
- **"Capture shape and size, not content"** (brief 05 §4.3, §3.3, §2.3 risk 3):
  tool input/output capture defaults to shape + size; full-content capture is
  opt-in and redaction-aware. Implemented as `CapturePolicyShape` (the default)
  and `obs.Shape`; `CapturePolicyFull` is honoured only with an `obs.Redactor`.
- **W3C Trace Context** (brief 05 Q-4, RFC §11.2): adopt W3C trace/span IDs so a
  Dockyard server's spans nest under a Harbor agent's `execute_tool` span.
  Implemented as `obs.SpanContext` (`NewTrace`, `Child`).
- **"obs/v1 is a public, versioned, third-party-consumable contract"** (brief 05
  Q-2 answered, RFC §11.3): the `obs.Event` JSON shape is pinned by golden tests
  — an accidental change fails CI.
- **App half-visibility** (brief 05 §2.5): Dockyard sees only its half of the
  iframe bridge — "served the resource, handshake received or not". The
  `app.load` / `app.bridge` event kinds and payloads are framed exactly so.
- **OTel is an export adapter, never the internal model** (brief 05 §4 risk 1,
  §5): obs/v1 is the stable contract; `ErrorInfo.Type` lowers onto OTel
  `error.type` and the event shape lowers onto MCP semconv, but no OTel
  dependency is introduced in Phase 15 — the adapter is Phase 16.

## Findings I'm departing from (if any)

None. Phase 15 implements brief 05's recommendations directly. Two naming
choices are recorded in D-074 (not departures): the brief's `progress` event
kind is named `task.progress` for clarity, and brief 05 Q-8 ("are task progress
events in obs/v1 V1?") is answered "yes" since Tasks is V1 scope.

## Goals

- The canonical, versioned `obs.Event` model and the closed set of event kinds
  (tool, resource, prompt, app, app-bridge, user-action, host-compat, log,
  server-lifecycle, task), with a golden-pinned wire shape.
- A non-blocking headless emitter behind an interface + factory + driver seam
  (`obs.Emitter`, `obs.RegisterDriver`, `obs.Open`).
- The in-memory ring-buffer driver — bounded, non-blocking, concurrent-safe,
  serving recent event history.
- W3C Trace Context correlation IDs on every event.
- Shape+size default capture; the redaction-gated full-capture hook designed.
- `runtime/server`, `runtime/apps`, `runtime/tasks` instrumented to emit.
- **S1 fold-in:** the Store migration registry made `t.Parallel()`-safe.

## Non-goals

- The out-of-band localhost SSE sink — **Phase 16**.
- The OTel export adapter — **Phase 16**.
- The MCP `logging` → obs/v1 `log`-event bridge — **Phase 16** (the `log` event
  kind and `LogPayload` shape are defined now so Phase 16 is a new event source).
- The concrete redaction pipeline behind `CapturePolicyFull` — Phase 16+ (the
  `obs.Redactor` interface is defined; full capture degrades to shape+size
  without one).
- The inspector UI that consumes obs/v1 — RFC §12, a later wave.

## Acceptance criteria

- [x] Tool, resource, app, and task events emit (master plan).
- [x] The emitter never blocks on a slow consumer (master plan).
- [x] The ring buffer serves recent events (master plan).
- [x] **S1 fold-in:** the Store migration registry is `t.Parallel()`-safe,
      proven by a `-race` concurrent-migration test.

## Files added or changed

```text
runtime/obs/                       # NEW — the obs/v1 runtime (AGENTS.md §3 already lists it)
├── doc.go                         # package overview
├── event.go                       # obs.Event, EventKind, Phase, ErrorInfo
├── payload.go                     # per-kind typed payloads
├── trace.go                       # W3C Trace Context — SpanContext, NewTrace, Child
├── capture.go                     # CapturePolicy, Redactor, Shape, ValueShape
├── emitter.go                     # Emitter seam — interface + factory + driver, FanOut, Nop
├── ringbuffer.go                  # the bounded ring-buffer driver (self-registers)
├── recorder.go                    # the headless emit helper subsystems use
├── event_test.go                  # golden wire-shape tests + validity tests
├── trace_test.go                  # W3C ID tests
├── capture_test.go                # shape/size + redaction-gating tests
├── ringbuffer_test.go             # ring-buffer + concurrency tests
├── emitter_test.go                # seam + non-blocking tests
└── recorder_test.go               # recorder + concurrent-emit tests
runtime/server/server.go           # Options.Obs/CapturePolicy/Redactor; rec field; lifecycle emit
runtime/server/obs.go              # NEW — server-side obs instrumentation helpers
runtime/server/tool.go             # tool.call emit in AddTool / AddToolWithSchemas
runtime/server/resource.go         # resource.read emit in AddResource / AddResourceTemplate
runtime/apps/apps.go               # app.load emit from the App resource-read handler
runtime/tasks/engine.go            # Options.Obs/ServerID; task.progress emit; obs span map
runtime/tasks/dispatch.go          # task.progress terminal emit on tasks/cancel
runtime/store/migrate.go           # S1 — MigrationSet replaces the global registry
runtime/store/store.go             # S1 — Store.Migrate(ctx, *MigrationSet)
runtime/store/errors.go            # S1 — ErrDuplicateMigration is an Add return value
runtime/store/inmem/inmem.go       # S1 — Migrate signature
runtime/store/sqlitestore/sqlitestore.go  # S1 — Migrate signature
runtime/store/store_test.go        # S1 — MigrationSet tests + concurrent-migrate -race test
runtime/store/storetest/conformance.go    # S1 — conformance uses MigrationSet
runtime/store/sqlitestore/sqlitestore_test.go  # S1 — Migrate signature
runtime/tasks/storedriver.go       # S1 — Migrations() replaces RegisterMigrations()
runtime/tasks/storedriver_test.go  # S1 — fixtures use MigrationSet, now t.Parallel()
test/integration/phase15_obs_test.go       # NEW — the Phase 15 integration test
test/integration/phase14_taskstore_test.go # S1 — fixture uses tasks.Migrations()
test/integration/wave5_test.go             # S1 — migrationSetupMu workaround removed
test/integration/wave1_test.go             # S1 — Migrate(ctx, nil)
test/integration/store_seam_test.go        # S1 — Migrate(ctx, nil)
scripts/smoke/phase-15.sh          # NEW — Phase 15 smoke
scripts/smoke/phase-14.sh          # S1 — assertion 2 updated for the new migration API
docs/plans/phase-15-obs-emitter.md # NEW — this plan
docs/decisions.md                  # D-073 (S1 fix), D-074 (obs/v1)
docs/glossary.md                   # obs.Event, event kind, emitter seam, OTel adapter,
                                    #   Recorder, ring-buffer emitter, shape+size capture,
                                    #   MigrationSet, W3C Trace Context
```

## Public API surface

```go
// runtime/obs — the obs/v1 protocol package.
const SchemaVersion = "dockyard.obs/v1"

type Event struct { /* schema_version, id, timestamp, server/session id,
                       trace/span ids, kind, phase, payload, duration_ms, error */ }
type EventKind string   // tool.call | resource.read | prompt.get | app.load |
                         // app.bridge | app.user_action | host.compat | log |
                         // server.lifecycle | task.progress
type Phase string        // start | end | progress | emit
type ErrorInfo struct { Type, Message string; Retryable, Silent bool }

type Emitter interface { Emit(ctx context.Context, e Event) }
type Factory func(cfg string) (Emitter, error)
func RegisterDriver(name string, f Factory)
func Open(driver, cfg string) (Emitter, error)
func Drivers() []string
type NopEmitter struct{}
type FanOut struct{ /* ... */ }   // bounded multi-driver emitter

type RingBuffer struct{ /* ... */ }   // the "ringbuffer" driver
func NewRingBuffer(capacity int) *RingBuffer
func (r *RingBuffer) Recent(n int) []Event
func (r *RingBuffer) Len() int
func (r *RingBuffer) Dropped() int64

type SpanContext struct{ TraceID, SpanID, ParentID string }   // W3C Trace Context
func NewTrace() SpanContext
func (sc SpanContext) Child() SpanContext

type CapturePolicy int   // CapturePolicyShape (default) | CapturePolicyFull
type Redactor interface { Redact(json.RawMessage) json.RawMessage }
type ValueShape struct{ /* kind, bytes, fields, len — content-free */ }
func Shape(raw json.RawMessage) ValueShape

type Recorder struct{ /* ... */ }   // the headless emit helper
func NewRecorder(emitter Emitter, serverID string, opts ...RecorderOption) *Recorder

// runtime/server — Options gains obs wiring.
type Options struct { /* ...; Obs obs.Emitter; CapturePolicy obs.CapturePolicy;
                         Redactor obs.Redactor */ }
func (s *Server) Recorder() *obs.Recorder

// runtime/tasks — Options gains obs wiring.
type Options struct { /* ...; Obs obs.Emitter; ServerID string */ }

// runtime/store — S1: the migration registry is a caller-owned value.
type MigrationSet struct{ /* ... */ }
func NewMigrationSet() *MigrationSet
func (s *MigrationSet) Add(m Migration) (*MigrationSet, error)
func (s *MigrationSet) MustAdd(m Migration) *MigrationSet
func (s *MigrationSet) Extend(other *MigrationSet) (*MigrationSet, error)
type Store interface { Migrate(ctx context.Context, set *MigrationSet) error; /* ... */ }
func RunMigrations(ctx context.Context, s Store, set *MigrationSet) error

// runtime/tasks — S1.
func Migrations() *store.MigrationSet   // replaces RegisterMigrations()
```

## Test plan

- **Unit (`runtime/obs`):** table-driven kind/phase/validity tests; capture
  policy and redaction-gating tests; trace-ID well-formedness/uniqueness;
  emitter seam (Open/Drivers/duplicate-panic); ring-buffer bounded/recent/drop.
  Coverage 96.9%.
- **Golden:** `TestEvent_GoldenShape` / `_Minimal` pin the obs/v1 `Event` JSON
  wire shape — a versioned public contract (CLAUDE.md §8).
- **Concurrency (`-race`):** `TestRingBuffer_ConcurrentEmitAndRead` (many
  emitters + readers); `TestRingBuffer_SlowConsumerNeverBlocksEmit` and
  `TestPhase15_EmitterNeverBlocksRuntime` (the non-blocking acceptance
  criterion); `TestRecorder_ConcurrentEmit`; `TestMigrationSet_ConcurrentMigrate`
  (the S1 fix proof).
- **Integration (`test/integration/phase15_obs_test.go`, `-race`):** a real
  `runtime/server` over the real in-memory transport with real contract-first
  tools, a real resource, a real MCP App, and a real `tasks.Engine` — no mocks
  at any seam — asserts `tool.call`, `resource.read`, `app.load`,
  `task.progress`, and `server.lifecycle` events land in a real ring buffer
  with correct kinds, start/end pairing, and W3C trace IDs. Covers a failure
  mode (a handler error surfacing as an obs `ErrorInfo`).
- **S1 conformance:** the shared Store conformance suite's migration-runner test
  exercises the `MigrationSet` API against every driver (inmem + sqlite).

## Smoke script additions

`scripts/smoke/phase-15.sh` — 13 assertions: obs builds CGo-free; the event
model carries the four required kinds; the emitter is an interface+factory+driver
seam with a self-registering ring-buffer driver; the ring buffer serves recent
events; a non-blocking slow-consumer test exists; W3C trace IDs; shape+size
default capture; server/apps/tasks emit obs/v1; the Event shape is golden-pinned;
the S1 migration registry is a caller-owned `MigrationSet` (the global removed);
an S1 concurrent-migrate `-race` test exists; obs+store tests pass; the Phase 15
integration test is present.

## Coverage target

- `runtime/obs` — 80% (new package). **Achieved: 96.9%.**
- `runtime/store` — 85% (conformance-tested subsystem). **Achieved: 93.5%.**
- `runtime/server`, `runtime/apps`, `runtime/tasks` — instrumentation only; the
  existing per-package targets are preserved (their suites stay green).

## Dependencies

- Phase 07 — the transport entrypoints and the server core the obs
  instrumentation hooks into.

## Risks / open questions

- **OTel semconv churn** (brief 05 §4 risk 1): mitigated by keeping obs/v1 the
  internal model and deferring the OTel adapter to Phase 16 behind the seam.
- **App half-visibility** (brief 05 §4 risk 4): obs/v1 honestly reports only the
  server side of the iframe bridge; `app.bridge` events are framed accordingly.
- **Phase 16 seam:** the SSE sink and OTel adapter register as `obs` drivers via
  `obs.RegisterDriver`; the MCP `logging` bridge is a new event source emitting
  `KindLog` events through the same `obs.Recorder`/`obs.Emitter`. The
  `obs.Event` `session_id` field and `obs.WithSession` context seam are defined
  now so Phase 16 transports stamp session identity without a contract change.
  No Phase 15 API needs to change for Phase 16.

## Glossary additions

`obs.Event`, event kind, emitter seam, OTel export adapter, Recorder,
ring-buffer emitter, shape + size capture, `MigrationSet`, W3C Trace Context —
all added to `docs/glossary.md` in this PR. The `obs/v1` entry is expanded.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New public API (`runtime/obs`, `Options.Obs`, `MigrationSet`) has a smoke
      check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race` (ring
      buffer, emitter, recorder, migration runner)
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New architectural decisions filed in `docs/decisions.md` (D-073, D-074)
