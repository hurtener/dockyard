# Phase 16 — obs/v1 transports — SSE + OTel + log bridge

## Summary

Phase 16 ships the three `obs/v1` transports/adapters RFC §11.3 specifies, all
behind the Phase 15 emitter seam: the out-of-band localhost **SSE sink** that
streams the live event stream to dev tooling without corrupting a stdio
JSON-RPC pipe; the optional, off-by-default **`OTelEmitter`** adapter that lowers
`obs.Event`s onto OpenTelemetry spans carrying MCP semantic-convention
attributes (`mcp.*` / `gen_ai.*`); and the **MCP `logging` → `obs/v1` `log`-event
bridge**, a new event source that surfaces server log records as `log` events
while a Dockyard server still speaks standard MCP `notifications/message` to any
client. None of this is an MCP client (P4); the SSE sink is dev-mode-oriented
and localhost-bound (CLAUDE.md §7).

## RFC anchor

- RFC §11.3 — transport and OTel: the out-of-band `SSESink`, the `OTelEmitter`
  MCP-semconv mapping, and the MCP `logging` bridge into `obs/v1` `log` events.

## Briefs informing this phase

- brief 05 — Observability & competitive landscape.
- brief 01 — MCP Apps extension (the `notifications/message` logging surface an
  MCP App / host sees; the half-visibility model).

## Brief findings incorporated

- **"Out-of-band transport so a stdio server stays debuggable"** (brief 05 §3.3,
  §2.2, Q-1): the local obs stream is carried on a separate localhost HTTP+SSE
  channel — never the stdio JSON-RPC pipe. Implemented as `SSESink`, which owns
  its own `net/http` listener bound to a loopback address and never touches
  `os.Stdout` / `os.Stdin`. The no-corruption property is the headline
  acceptance criterion and is proven by the integration test.
- **"OTel MCP semconv is the export vocabulary"** (brief 05 §3.4, §2 — semconv
  1.40.0): `tool.call` → a `span.mcp.server` span named `tools/call {tool}` with
  `mcp.method.name`, `gen_ai.tool.name`, `gen_ai.operation.name=execute_tool`,
  `mcp.session.id`, `network.transport`, `error.type`; `resource.read` carries
  `mcp.resource.uri`. Implemented in `runtime/obs/otel` as the `obs.Event` →
  OTel span mapping.
- **"OTel is an export adapter behind obs/v1, never the internal model"**
  (brief 05 §4 risk 1, §3.4, Q-5): `obs/v1` stays the stable contract; the OTel
  adapter is off by default and absorbs OTel semconv churn. Implemented as a
  driver behind the same `obs.RegisterDriver` seam, gated off unless explicitly
  configured — local observation works with zero OTel config via the ring
  buffer + SSE.
- **"Sensitive payloads — capture shape and size, not content"** (brief 05 §4.3,
  §2.3 risk 3): the SSE sink and the OTel adapter both consume the already
  shape+size-captured `obs.Event` — neither re-derives content, so the Phase 15
  capture policy is honoured transitively. The OTel adapter never emits the
  opt-in `gen_ai.tool.call.arguments` attribute in V1.
- **"The MCP `logging` capability is bridged, not bypassed"** (brief 05 §3.3,
  RFC §11.3; brief 01 — the `notifications/message` host surface): a Dockyard
  server still emits standard MCP `notifications/message`; the same record ALSO
  becomes an `obs/v1` `log` event. Implemented as `server.LogBridge`, which fans
  a log record to both the SDK `ServerSession.Log` path and the `obs.Recorder`.

## Findings I'm departing from (if any)

None. Phase 16 implements brief 05 §3.3/§3.4 and RFC §11.3 directly. One scoping
choice is recorded in D-076 (not a departure): the `OTelEmitter` maps `obs/v1`
events onto OTel **spans** via the OTel trace SDK; `obs/v1` `log` events are
exported as **span events** on the correlated span rather than via the separate,
still-`v0.x` OTel logs SDK — this keeps the new dependency surface to the stable
OTel trace SDK only and is revisited if the logs SDK reaches `v1`.

## Goals

- An out-of-band localhost SSE sink (`obs.SSESink`) that streams `obs/v1` events
  over Server-Sent Events on a loopback-bound HTTP endpoint, registered behind
  the Phase 15 emitter seam under driver name `sse`. Non-blocking: a slow or
  stalled SSE subscriber never blocks the runtime's emit path.
- An optional `OTelEmitter` adapter (`runtime/obs/otel`) that exports `obs.Event`
  as OpenTelemetry spans carrying `mcp.*` / `gen_ai.*` attributes and the
  W3C-derived trace/span IDs, registered behind the same seam under driver name
  `otel`, **off by default**.
- The MCP `logging` → `obs/v1` `log`-event bridge (`server.LogBridge`): a server
  log record surfaces BOTH as a standard MCP `notifications/message` AND as an
  `obs/v1` `log` event.
- `make build` stays CGo-free with the OTel dependency added.

## Non-goals

- The inspector UI that consumes the SSE stream — Wave 8 (Phase 22). Phase 16
  ships the SSE sink the inspector will consume; the event framing is clean and
  documented, but no inspector is built.
- A production MCP client (P4) — the SSE sink is a server-side event *producer*,
  not an MCP client; it speaks SSE to dev tooling, not MCP.
- An OTel logs-SDK integration — `obs/v1` `log` events are exported as OTel span
  events (D-076); the separate OTel logs SDK is out of scope while it is `v0.x`.
- An OTLP exporter wired by default — the `OTelEmitter` accepts a caller-supplied
  `trace.SpanProcessor`/exporter; the CLI knob that selects an OTLP endpoint is
  Wave 7 CLI scope.
- The concrete redaction pipeline behind `CapturePolicyFull` — still Phase 16+
  per the Phase 15 plan; Phase 16 consumes the shape+size-captured event as-is.

## Acceptance criteria

- [x] The SSE sink streams `obs/v1` events to a connected subscriber and does
      NOT corrupt a stdio MCP pipe — `os.Stdout`/`os.Stdin` carry only clean
      JSON-RPC framing while obs events flow out the separate SSE channel
      (master plan; the headline criterion).
- [x] OTel spans carry `mcp.*` / `gen_ai.*` attributes — `mcp.method.name`,
      `gen_ai.tool.name`, `gen_ai.operation.name`, `mcp.session.id`,
      `network.transport`, and `mcp.resource.uri` where applicable (master plan).
- [x] `notifications/message` log records surface as `obs/v1` `log` events; a
      client that negotiated `logging` still receives `notifications/message`
      exactly as before (master plan).
- [x] The `OTelEmitter` is off by default — local observation (ring buffer +
      SSE) works with zero OTel configuration (CLAUDE.md §8).
- [x] The SSE sink binds localhost-only and refuses a non-loopback bind address
      (CLAUDE.md §7).
- [x] `make build` stays CGo-free with the OTel dependency present.

## Files added or changed

```text
runtime/obs/
  sse.go                       # the out-of-band localhost SSE sink + driver
  sse_test.go                  # SSE unit + concurrency tests (-race)
  otel/
    otel.go                    # the OTelEmitter adapter + driver + semconv mapping
    otel_test.go               # OTel unit tests (mcp.*/gen_ai.* attribute golden)
runtime/server/
  logbridge.go                 # the MCP logging -> obs/v1 log-event bridge
  logbridge_test.go            # log-bridge unit tests
test/integration/
  phase16_obs_transports_test.go  # real stdio server: SSE no-corruption + OTel + log bridge
scripts/smoke/phase-16.sh      # smoke assertions (one per acceptance criterion)
docs/plans/phase-16-obs-transports.md   # this plan
docs/decisions.md              # D-075, D-076, D-077
docs/glossary.md               # SSE sink, OTelEmitter, MCP semconv, logging bridge
go.mod / go.sum                # OTel trace SDK dependency
```

## Public API surface

- `runtime/obs`:
  - `func NewSSESink(addr string) (*SSESink, error)` — construct a localhost SSE
    sink; a non-loopback `addr` is rejected.
  - `func (*SSESink) Emit(context.Context, Event)` — the non-blocking emit path.
  - `func (*SSESink) Handler() http.Handler` — the SSE HTTP handler (for the
    inspector to mount, Wave 8).
  - `func (*SSESink) Addr() string` — the resolved listen address.
  - `func (*SSESink) Subscribers() int` — current live subscriber count.
  - `func (*SSESink) Close() error` — shut down, draining subscribers.
  - Driver `"sse"` registered via `RegisterDriver`; config = loopback addr.
- `runtime/obs/otel`:
  - `func New(tp trace.TracerProvider) *OTelEmitter` — construct the adapter
    over a caller-supplied OTel `TracerProvider`.
  - `func (*OTelEmitter) Emit(context.Context, obs.Event)` — span export.
  - Driver `"otel"` registered via `obs.RegisterDriver`; off unless opened.
- `runtime/server`:
  - `func (s *Server) LogBridge() *LogBridge` — the server's log bridge.
  - `func (b *LogBridge) Log(ctx, sess *mcp.ServerSession, rec LogRecord)` —
    fan a log record to MCP `notifications/message` AND obs/v1.
  - `type LogRecord struct { Level, Logger, Message string }`.

## Test plan

- **Unit:** SSE — event framing (`data:` lines, `event:` type, JSON body);
  localhost-bind enforcement; driver registration + `Open`. OTel — `obs.Event` →
  span attribute mapping table (`tool.call`, `resource.read`, `log` kinds);
  W3C-ID → OTel trace/span-ID derivation; error → `error.type`; off-by-default.
  Log bridge — a record fans to both sinks; level mapping.
- **Integration** (binding — Deps name Phase 15, consumes the emitter seam,
  instruments `runtime/server`): `test/integration/phase16_obs_transports_test.go`
  drives a REAL `runtime/server` over a REAL stdio transport and asserts (1) obs
  events flow out the SSE channel while stdout/stdin carry only clean MCP
  JSON-RPC framing (the no-corruption proof), (2) a real OTel in-memory span
  recorder (not a mock at the boundary) receives spans with `mcp.*`/`gen_ai.*`
  attributes and W3C-derived trace IDs, (3) a server log record arrives BOTH as
  a standard MCP `notifications/message` AND as an `obs/v1` `log` event. Covers
  a failure mode (a stalled SSE subscriber must not block emit); runs `-race`.
- **Concurrency / golden:** SSE is a reusable concurrent artifact — a `-race`
  test with many concurrent subscribers, a deliberately stalled subscriber that
  must NOT block the emit path, subscriber connect/disconnect churn, and clean
  shutdown with no goroutine leak. The OTel attribute set is pinned by a golden
  assertion.

## Smoke script additions

- `runtime/obs/sse.go` exists and `runtime/obs` builds CGo-free.
- The SSE sink registers behind the Phase 15 emitter seam (`RegisterDriver`).
- The SSE sink enforces a localhost-only bind.
- The `OTelEmitter` adapter exists, registers behind the seam, and is off by
  default.
- OTel spans carry `mcp.*` / `gen_ai.*` attributes (mapping present).
- The MCP `logging` → `obs/v1` `log`-event bridge exists in `runtime/server`.
- `make build` stays CGo-free with the OTel dependency.
- The Phase 16 unit + integration tests pass.

## Coverage target

- `runtime/obs` (SSE additions) — 80% (new-package default; the package is
  conformance-adjacent but the §11 default applies).
- `runtime/obs/otel` — 80% (new package).
- `runtime/server` (logbridge additions) — 80% on the new file.

## Dependencies

- Phase 15 — the `obs.Event` model, the `Emitter` interface + `RegisterDriver`
  seam, the ring-buffer driver pattern, W3C Trace Context, the `log` event kind
  and `LogPayload`, the `obs.Recorder`, the `WithSession` context seam.

## Risks / open questions

- **OTel semconv is still "Development"** (brief 05 §4 risk 1, Q-5). Mitigation:
  the adapter is an isolated package behind the emitter seam; an attribute-name
  shift is a localized edit, never a contract change — `obs/v1` is the stable
  contract.
- **SSE on Windows / loopback resolution** (brief 05 Q-1). Mitigation: the sink
  binds an explicit loopback address (`127.0.0.1:0` default) and never `0.0.0.0`;
  the bind check rejects any non-loopback host.
- **OTel dependency surface / CGo.** Mitigation: only the pure-Go OTel trace SDK
  is added; `make build` (CGO_ENABLED=0) is verified green; the smoke script
  asserts it.

## Glossary additions

- **SSE sink** — the out-of-band localhost Server-Sent-Events `obs/v1` emitter
  driver.
- **`OTelEmitter`** — the optional, off-by-default OpenTelemetry export adapter.
- **MCP semconv** — the OpenTelemetry MCP semantic conventions (`mcp.*`,
  `gen_ai.*`) the `OTelEmitter` emits.
- **logging bridge** — `server.LogBridge`, the MCP `logging` → `obs/v1` `log`
  event source.

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
