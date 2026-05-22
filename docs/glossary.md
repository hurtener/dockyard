# Dockyard — Glossary

Authoritative definitions for Dockyard-specific vocabulary. Add a term here in the
same PR that introduces it. When in doubt, the RFC wins (AGENTS.md §15).

---

## A

**App runtime** — the Dockyard runtime library (`runtime/`): the importable Go
package tree — `runtime/server` (the MCP server core), and later
`runtime/apps`, `runtime/tasks`, `runtime/obs`, `runtime/store` — vendored into
every generated Dockyard app. A generated app's `main.go` stays thin and
delegates the protocol weight to the app runtime. RFC §3. D-020.

## B

**Build blocker** — a `dockyard validate` diagnostic class severe enough to fail
the build: an invalid manifest or schema, a missing or mismatched `ui://`
resource, an invalid MIME, an Apps/Tasks construct that violates the vendored
spec, stale generated contracts, or a missing required UI state. A `validate`
run carrying any build blocker exits non-zero. The complement is a *warning* —
reported but non-fatal. RFC §9.4. D-082.

**Boot check** — `dockyard install`'s post-write verification: a throwaway,
localhost, dev-only spawn of the freshly built server binary that drives one
real MCP `initialize` handshake to confirm the host config it just wrote
launches a working server. It is the test-only client carve-out (P4), never a
production or long-lived MCP client. Implemented in `internal/installpkg`.
RFC §14. D-088.

**Build pipeline** — the ordered sequence `dockyard build` runs to produce a
project's shippable artifact: regenerate the contract artifacts → run the
`dockyard validate` quality gate (a build blocker fails the build — P1 at build
time) → `vite build` the project's `web/` UI when one exists → `go build` one
CGo-free, statically-linked binary per cross-compile target with the UI
embedded → emit a SHA-256 checksum per artifact. Implemented in
`internal/buildpkg`. RFC §14. D-087.

**Bridge shell library** — the Svelte/TypeScript library (`web/bridge/`) vendored
into every Dockyard app. It implements the *View half* of the `ui/` `postMessage`
JSON-RPC dialect — the side that runs inside the App's sandboxed iframe — so app
authors never hand-write protocol code: it runs the `ui/initialize` handshake,
exposes `hostContext` as Svelte stores, fans out host→view notifications, offers
typed view→host helpers, negotiates display modes, and framework-manages
`viewUUID` view-state. Its peer, the *host half* of the same dialect, is the
inspector. RFC §7.2, §7.3. D-016, D-059, D-060, D-061.

## C

**Capability negotiation** — the MCP `initialize` handshake in which a host
advertises the extensions and capabilities it supports. Dockyard reads this at run
time and adapts; it never hardcodes a per-host capability matrix. RFC §7.5. D-011.

**Component inventory** — the shared set of Svelte components in `web/ui/` that
every Dockyard frontend surface composes rather than re-implementing. Phase 10a
delivers the V1 inventory; a genuinely new shared component lands in `web/ui/`
and `docs/design/CONVENTIONS.md` §3 in the same PR. AGENTS.md §20. D-066.

**Conformance suite** — the shared `runtime/store/storetest` test battery every
`Store` driver must pass. A new persistence guarantee is added to the suite once and
proven against every driver, never bolted onto one driver. RFC §13. D-025, D-026.
**Codec** — a `protocolcodec` encoder/decoder pair for one negotiated MCP
`protocolVersion`. A codec encodes Dockyard domain types into MCP extension wire
formats and decodes wire formats back; it is obtained with `CodecFor` /
`CodecForStrict`. Encoders emit only current spec shapes; decoders tolerate
unknown keys and deprecated forms. RFC §5.4, §16. D-022.

**Contract struct** — a Go input or output struct that is the single source of
truth for a tool's schema. `internal/codegen` generates the tool's JSON Schema
from it (Phase 04) and `tygo` generates the TypeScript types from it (Phase 05);
a contract struct's top-level type must be an object (a struct or string-keyed
map). RFC §6.1. D-029, D-030.

**Contract-first** — the property (P1) that a tool's input and output are typed Go
structs (the single source of truth) from which JSON Schema, TypeScript types, and
fixtures are generated. RFC §6. D-004.

**Content split** — the routing rule for a tool result (RFC §6.3): the handler's
model-facing `Text` goes to `CallToolResult.content[]` (it enters the LLM
context) and its typed `Structured` output goes to `structuredContent` (UI-facing,
excluded from model context). The Dockyard handler runtime hardens the split so no
empty `TextContent` block is emitted when `Text` is empty. RFC §6.3. D-043.

**Contract reference** — the `"<package/path>.TypeName"` string a manifest's
`tools[].input` / `tools[].output` field holds: a reference to a Go contract
struct, not inline schema. Resolved to a JSON Schema through the manifest's
`ContractResolver` seam (the codegen pipeline). RFC §4.2, §6.1. D-037.

**Cross-compile matrix** — the set of GOOS/GOARCH target triples `dockyard
build` produces an artifact and a SHA-256 checksum for: darwin, linux and
windows × amd64 and arm64. Built sequentially, one `go build` per target with
`CGO_ENABLED=0` pinned; a per-target failure is collected and reported rather
than aborting the run. `buildpkg.DefaultMatrix()` returns it. RFC §14. D-087.

## D

**dev loop** — the `dockyard dev` orchestrated edit-feedback cycle (Phase 19):
an embedded `fsnotify` watcher choreographing a Go-server restart on a `.go`
change, an in-process codegen re-run on a contract change, and a supervised Vite
dev server (which owns Svelte HMR) — all as one process tree from one command.
Dockyard does not shell out to `air` or `wgo`. RFC §9.2. D-084, D-085, D-086.

**`dockyard dev`** — the CLI verb (Phase 19) that runs the dev loop. A thin
wrapper over the `internal/devloop` orchestrator: it watches the project,
restarts the Go MCP server, re-runs codegen, and supervises Vite, tearing the
whole tree down cleanly on Ctrl-C. RFC §9.2, §9.1. D-084.

**`dockyard generate`** — the contract-first codegen CLI verb (Phase 18). It
runs the Design A pipeline (RFC §6.2) over a project: it regenerates the
per-contract JSON Schema files and the TypeScript contract types from the typed
Go contract structs, which are the single source of truth (P1). `generate` is
idempotent — a rerun with no contract change is a byte-identical no-op. RFC §6,
§9.1. D-081.

**`dockyard validate`** — the quality-gate CLI verb (Phase 18). It runs the
RFC §9.4 checks — manifest, schemas, tool↔UI mappings, MIME, spec compliance, UI
states, stale-codegen drift — and exits non-zero on any *build blocker*. It
wraps the reusable `internal/validate` engine, the same gate `dockyard build`
and `dockyard test` consume. RFC §9.4, §9.1. D-082, D-083.

**Design token** — a named, single-source visual constant — colour, spacing,
typography, radius, or elevation — shipped by Phase 10a as a `--dy-*` CSS custom
property (`web/ui/src/tokens.css`) with a typed companion (`tokens.ts`). Tokens
are the single source of visual truth: no `web/ui` component or Dockyard page
carries an ad-hoc hex or magic spacing number. `docs/design/CONVENTIONS.md` §5.
D-065.

**Dedicated origin** — the stable, per-App sandboxed-iframe origin a host serves
an App's HTML from (`_meta.ui.domain`), needed by APIs that allowlist origins
(CORS). Dockyard auto-derives it from a host-agnostic domain label through the
host-profile seam — including a host's signed form (e.g. Claude's SHA-256
`claudemcpcontent.com` subdomain). RFC §7.5. D-062, D-063.

**Deny-by-default CSP** — the Content-Security-Policy a UI resource gets when it
declares no `_meta.ui.csp` domains: zero external origins, so a single-file HTML
bundle just works. `runtime/apps` encodes it by **omitting** the `_meta.ui` CSP
rather than emitting an empty allowlist — a host applies its deny-by-default
policy when no CSP is present. A host may further restrict it but never loosen
it. RFC §7.4. D-048.

**Deployment mode** — one of the three run-time modes a single Dockyard app binary
supports: local **stdio** subprocess, **HTTP** service, or **Portico-managed**.
Selected at run time, not baked in. RFC §14.

**Display mode** — one of the three MCP Apps viewing styles: **inline** (widget),
**fullscreen**, **pip**. Negotiated at run time via `ui/request-display-mode` and
`hostContext.displayMode`, handled by the bridge shell library. RFC §7.2.

**Display-mode negotiation** — the runtime protocol exchange by which a View
moves between inline, fullscreen, and pip: the View calls
`ui/request-display-mode`, the host grants or denies, and the result is reflected
in `hostContext.displayMode`. The bridge shell only offers a mode the host
advertised in `availableDisplayModes` — capability-driven, never a host matrix.
RFC §7.2, §7.5. D-059.

**Dockyard app** — an MCP server (tools + resources) optionally extended with one or
more `ui://` UI resources. A plain MCP server and an MCP App are the same artifact
at different levels of completeness. RFC §4.1.

**Domain label** — the host-agnostic domain identifier an App author declares
(`App.Domain`). It is not carried verbatim onto `_meta.ui.domain`: it is the
input to host-profile derivation, which turns it into the host's concrete
dedicated origin. RFC §7.5. D-062.

**Drift cross-check** — the `internal/codegen` library check that hard-fails when
the generated JSON Schema and the generated TypeScript for a contract desync (a
property present in one artifact and absent or differently-optional in the
other), or when generated output is stale versus its Go source. Because Design A
generates schema and TypeScript independently (RFC §6.2), the cross-check is what
makes a generator bug a loud build failure rather than silent server↔UI drift.
Phase 18's `dockyard validate` command calls it. RFC §6.2. D-034.

## E

**Edge validation** — argument validation at the catalog edge: the Dockyard
handler runtime validates a tool call's incoming arguments against the tool's
generated input JSON Schema *before* the typed handler runs, so a schema-violating
argument becomes a typed `tool.ArgumentError` (wrapping `ErrInvalidArguments`)
rather than a panic or a vague failure. RFC §5, §6.3. D-044.

**Embedded UI bundle** — the built Svelte `dist/` tree compiled into the Go
binary via `//go:embed all:dist`. One `embed.FS` backs both the `ui://` MCP
resource handler and (Phase 22) the inspector's HTTP preview — there is never a
second copy of the UI assets. Surfaced at runtime as an `apps.Bundle`. RFC §14.
D-057.

**Ephemeral generator** — the small Go `main` `dockyard generate` templates into
a temp directory inside a project, `go run`s, and deletes, to produce the
contracts' JSON Schema files. It exists because the schema engine reflects on
real Go types the `dockyard` binary cannot see — the project's contract structs
live in a different module — and `internal/codegen` is not importable from a
scaffolded project. The ephemeral generator imports only the project's own
contracts package and the public `runtime/tool` API, so it compiles inside the
project's module. RFC §6.2. D-081.

**Embedded-struct flattening** — the `internal/codegen` step that inlines an
embedded (anonymous) struct's fields into the embedding interface's generated
TypeScript, matching how the JSON Schema and Go's own `encoding/json` promote
those fields. Without it the TypeScript and the schema disagree. RFC §6.2.
D-051.

**Enum registration** — supplying a named contract type's constant set to the
schema generator (`WithEnum`, or `EnumsFromSource` parsed from contract source)
so the generated JSON Schema carries an `enum` array. Reflection cannot see a
Go `const` block, so the values must be registered. RFC §6.1. D-051.

## F

**Forward-only migration** — an append-only, ordered, idempotent schema or data step
applied through the `Store` seam's migration runner. Once a migration has merged it
is never edited; the runner rejects reordering, removal, or post-merge mutation.
RFC §13, AGENTS.md §9. D-027.

## G

**Generated schema** — the JSON Schema produced from a contract struct by
`internal/codegen` via `google/jsonschema-go`. It is never hand-written (P1); it
is marshalled deterministically and pinned by golden tests so any drift is a
visible diff. The typed tool builder registers the generated schema on the tool,
so the schema a host sees is provably the contract. RFC §6.1, §6.2. D-030, D-031.

**Generated TypeScript** — `web/src/generated/contracts.ts`: the TypeScript
contract types produced from the Go contract structs by `internal/codegen` via
`gzuidhof/tygo`. The UI-facing half of the Design A codegen pipeline; never
hand-authored (P1), deterministic, headered with the `Code generated … DO NOT
EDIT.` marker, and pinned by golden tests. RFC §6.2. D-032, D-033.

## G (continued)

**`getServer` seam** — the per-request server-selection callback the streamable-HTTP
transport invokes once per incoming HTTP request to choose the server that handles
it (the go-sdk's `getServer func(*http.Request) *Server`). Dockyard exposes it as
`HTTPOptions.ServerForRequest`; it is the seam for per-session and multi-tenant HTTP
wiring. RFC §5.2. brief 03 §2.3.

## H

**hostContext** — the host-supplied context delivered to a View in the
`ui/initialize` result and patched by `ui/notifications/host-context-changed`:
theme, `styles.variables` (standardized host CSS custom properties), `displayMode`,
`availableDisplayModes`, `locale`, container dimensions, and more. The bridge
shell exposes it as reactive Svelte stores. RFC §7.3. brief 01 §2.4.

**Handler runtime** — the `runtime/tool` layer that wraps a contract-first tool
handler in production: it validates incoming arguments at the catalog edge (edge
validation), runs the typed handler, hardens the content split (RFC §6.3), and
raises routing flags for oversized or misrouted payloads. Built once per tool by
`Builder.Register`; safe for concurrent tool calls. RFC §5, §6.3. D-043, D-044,
D-045.

**Handler panic recovery** — the toolchain-enforced guarantee that an app
author's tool or resource handler that panics on a live MCP request becomes a
typed error result, never a server-process crash. Every handler-invocation path
in `runtime/server` routes through `guardHandler`, which `recover()`s a panic
into a `*panicError` (wrapping the `ErrHandlerPanic` sentinel) and logs the
stack. The "never panic across the MCP boundary" rule made a guarantee, not a
docstring instruction. AGENTS.md §5, §13. D-053.

**Handler span** — the `obs/v1` `tool.call` `SpanContext` that `runtime/server`
threads onto a tool handler's `context.Context` (via `obs.WithSpan`) so an
`obs/v1` event emitted from *inside* the handler — most notably a
handler-emitted `log` event through the MCP-logging bridge — derives a child
span of the enclosing `tool.call` rather than minting an unrelated trace.
RFC §11.2. D-079.

**Host profile** — a pluggable set of host-specific *derivation functions* (e.g.
deriving Claude's signed `claudemcpcontent.com` iframe origin). A host profile is
algorithms, not a capability matrix. Implemented as the `apps.HostProfile`
interface; drivers self-register with the host-profile registry. RFC §7.5.
D-012, D-062, D-063.

**Host-profile registry** — the process-wide interface + factory + driver
registry of `HostProfile` derivation drivers in `runtime/apps`. Drivers
self-register via `init()`; `HostProfileFor` looks one up by host id and an
empty id resolves to the `generic` verbatim default. It is the seam through
which a new host is a new driver file, never a core edit. RFC §7.5. D-062.

**HTTP security options** — the explicit security posture of Dockyard's
streamable-HTTP transport: DNS-rebinding (localhost) protection,
Origin/Content-Type verification, and cross-origin (CSRF) protection. Represented
by `runtime/server.HTTPSecurity` and set deliberately by Dockyard — never inherited
from an SDK default, because the go-sdk has flipped these defaults between releases.
`DefaultHTTPSecurity()` returns the recommended all-on posture. RFC §5.2.
AGENTS.md §7. D-040, D-041.

## I

**Inspector** — Dockyard's local, test-only debug surface; the lone client-shaped
component. It implements the host half of the `ui/` bridge to render Apps locally,
surfaces the `obs/v1` stream, fixtures, latency analytics, drift verdicts, and
capability-set emulation. Dev-mode-gated, localhost-only, read-only. RFC §12.

**Install host profile** — the small per-MCP-host (`claude`, `cursor`)
structure in `internal/installpkg` that derives that host's MCP config-file
location — Claude Desktop's per-OS `claude_desktop_config.json`, Cursor's
`~/.cursor/mcp.json`. A filesystem-path derivation, not a capability matrix
(distinct from the `apps.HostProfile` iframe-origin derivation): `dockyard
install` writes a host config, and isolating the path here keeps a host's
location change one localized edit. RFC §14. D-088.

## K

**KV namespace** — a logical partition of a `Store`'s key space. Every key is
addressed by `(namespace, key)`; future sub-stores (`TaskStore`, `ObsStore`) each own
one or more namespaces, and the migration runner reserves `__store_migrations__`.
RFC §13. D-025.

## L

**logging bridge** — `server.LogBridge`, the Phase 16 MCP `logging` → `obs/v1`
`log`-event source. `LogBridge.Log` delivers a server log record as a standard
MCP `notifications/message` (a Dockyard server still speaks standard MCP
`logging` to any client) AND emits the same record as an `obs/v1` `log` event.
The bridge is an event source, never a back channel (P2); it resolves the
in-flight MCP `ServerSession` from the handler context so a typed tool handler
never touches a raw SDK session (P3). RFC §11.3. D-077.

## M

**Manifest** — `dockyard.app.yaml`, an app's control plane: it declares tools,
`ui://` apps, transports, and quality requirements, and drives `validate`,
`generate`, `dev`, `test`, `build`, and `install`. Loaded and structurally
validated by `internal/manifest` into a typed `Manifest` Go struct; invalid
manifests fail with source-located (`file:line`) errors. RFC §4.2. D-035, D-036.

**MCP semconv** — the OpenTelemetry semantic conventions for the Model Context
Protocol (the `mcp.*` and `gen_ai.*` attribute families, the `span.mcp.server`
span shape). Dockyard's Phase 16 `OTelEmitter` emits them as the OTel *export
vocabulary* — `mcp.method.name`, `gen_ai.tool.name`,
`gen_ai.operation.name=execute_tool`, `mcp.session.id`, `mcp.resource.uri`,
`network.transport`, `error.type`. The conventions are still "Development"
upstream, so they are contained in `runtime/obs/otel`; `obs/v1` stays the
stable contract. RFC §11.3. brief 05 §3.4. D-076.

**`MigrationSet`** — an explicit, caller-owned, ordered collection of Store
migrations (`store.MigrationSet`). It replaced the former mutable process-global
migration registry: a caller builds a set (`NewMigrationSet`, `Add`, `MustAdd`,
`Extend`), passes it to `Store.Migrate(ctx, set)`, and two stores migrate
concurrently from independent sets with no shared state and no locking. A
sub-store exposes its migrations as a fresh set per call (e.g.
`tasks.Migrations()`). RFC §13. CLAUDE.md §9. D-073.

**MCP server core** — the `runtime/server` package: the part of the app runtime
that wraps the official Go MCP SDK and exposes Dockyard's server construction,
typed tool registration, and transport serve loop. The settled foundation
(RFC §5.1, D-002); Dockyard layers Apps, Tasks, and `obs/v1` on top and never
forks the SDK. RFC §5. D-019, D-020.

**MCP App** — at the protocol level, an MCP tool carrying `_meta.ui` metadata that
links it to a `ui://` resource the host renders as a sandboxed iframe. Not a new
wire primitive — a convention over tools + resources. RFC §7.1.

**MCP Apps extension** — the MCP extension Dockyard implements server-side,
identified `io.modelcontextprotocol/ui` (SEP-1865, spec revision 2026-01-26).
It defines the `ui://` resource convention, the `_meta.ui` tool/resource
metadata, and the `postMessage` bridge dialect. Negotiated through the core
`extensions` capability; a host that does not advertise it still gets a fully
working plain MCP server. RFC §7. D-047.

**`_meta.ui`** — the nested MCP Apps metadata object. On a **tool** definition it
carries `{resourceUri, visibility}` linking the tool to its `ui://` resource; on
a **`resources/read` response** it carries `{csp, permissions, domain,
prefersBorder}`. Dockyard emits only this nested form, never the deprecated flat
`_meta["ui/resourceUri"]` form; all encoding goes through `internal/protocolcodec`.
RFC §7.1. D-047, D-048.

## N

**No-template path** — `dockyard new <name>` invoked with no `--template`
flag: the first-class scaffold that produces a blank but working MCP server —
a manifest, one example contract-first tool, generated contract artifacts, a
runnable main, and a contract test. RFC §10 names it the first-class path;
templates are optional product-pattern showcases layered on later. RFC §9.1,
§10.

## O

**`obs/v1`** — Dockyard's canonical, versioned, public observability event protocol.
The headless runtime emits it; the inspector and the post-V1 console are pure
clients. The wire shape of `obs.Event` is pinned by golden tests — a change is a
versioned, documented `schema_version` bump. RFC §11. D-008, D-074.

**`obs.Event`** — the one canonical obs/v1 event type (`runtime/obs`): a
`schema_version`, an event id, a timestamp, server/session identity, W3C
trace/span IDs, a `kind`, a `phase`, a typed per-kind `payload`, an optional
`duration_ms`, and an optional `ErrorInfo`. The only type the inspector and the
post-V1 console consume; no raw runtime or SDK type leaks through it. RFC §11.2.
D-074.

**Event kind** — the classification of an `obs.Event`: `tool.call`,
`resource.read`, `prompt.get`, `app.load`, `app.bridge`, `app.user_action`,
`host.compat`, `log`, `server.lifecycle`, `task.progress`. The set is closed for
obs/v1; a new kind is a versioned addition. RFC §11.2. D-074.

**Emitter seam** — the obs/v1 interface + factory + driver seam (`obs.Emitter`,
`obs.RegisterDriver`, `obs.Open`). The runtime depends only on `obs.Emitter`;
drivers register a factory in an `init()` block. Phase 15 ships the ring-buffer
driver; Phase 16's SSE sink and OTel adapter plug in behind the same seam.
CLAUDE.md §4.4. RFC §11.3. D-074.

**`FanOut`** — the bounded fan-out `obs.Emitter` (`runtime/obs`) that forwards
every `obs.Event` to several drivers at once — the ring buffer, the SSE sink,
and the `OTelEmitter` together. It is the CLAUDE.md §8 bounded fan-out: a slow
driver cannot stall a fast one because every driver's `Emit` is itself
non-blocking. A `FanOut` is a reusable concurrent artifact and `Close` joins its
drivers' close errors. CLAUDE.md §8. RFC §11.3.

**`OTelEmitter`** — Dockyard's optional, off-by-default OpenTelemetry export
adapter for `obs/v1` (`runtime/obs/otel`). It lowers an `obs.Event` onto an
OpenTelemetry span carrying MCP-semconv attributes (`mcp.*` / `gen_ai.*`); the
W3C Trace Context IDs `obs/v1` already assigns become the OTel span's
trace-id / span-id, so a Dockyard span nests under a calling Harbor agent's
`execute_tool` span. OTel is an interoperability *option*, never a prerequisite
to observe locally — the ring buffer and the SSE sink work with zero OTel
config. RFC §11.3. brief 05 §3.4. D-076.

**OTel export adapter** — the optional `obs/v1` driver (Phase 16) that maps an
`obs.Event` onto OpenTelemetry MCP semantic conventions for export to an
external observability stack. It is off by default and never a prerequisite to
observe locally — obs/v1 is the stable contract; the adapter absorbs OTel
semconv churn. RFC §11.3. D-074.

**Orchestrator** — the reusable, concurrency-safe `internal/devloop` package
(Phase 19) that implements the dev loop: it embeds the `fsnotify` watcher,
holds one supervisor per supervised child, runs codegen in-process on a contract
change, and tears the whole process tree down cleanly on context cancellation.
`devloop.Run` is its single public entrypoint; the `dockyard dev` verb is a thin
wrapper over it. RFC §9.2. D-084.

## P

**`protocolcodec`** — the internal package (`internal/protocolcodec`) that is the
*only* place raw MCP extension wire formats are imported. Codecs are versioned and
keyed on the negotiated `protocolVersion`. The mechanism behind forward-
compatibility (P3). RFC §5.4. D-009.

**P1 / P2 / P3 / P4** — Dockyard's four binding properties: contract-first;
observability is a protocol; forward-compatibility by isolation; server-side only.
RFC §1.

**PageState** — the shared four-state async wrapper in the `web/ui` inventory.
It routes an async region to exactly one of loading / empty / error / ready; the
empty and error panels are mandatory and carry real copy plus a working
retry/action affordance. Every Dockyard page and async region routes through it.
`docs/design/CONVENTIONS.md` §4. AGENTS.md §20. D-066.

## Q

**Quality gate** — a `quality.*` knob in the manifest (`require_loading_state`,
`require_fixtures`, `require_contract_tests`, …) declaring a quality requirement
the app opts into. `internal/manifest` parses and shape-checks the `quality`
block; the gates are *enforced* by `dockyard validate`. RFC §4.2, §9.4. D-035.

## R

**Recorder** — the headless obs/v1 emit helper (`obs.Recorder`) a subsystem
uses to record events without hand-assembling an `obs.Event`. It binds a server
identity and an `obs.Emitter` once; each event it builds carries the schema
version, a fresh id, a timestamp, and the identity automatically. `runtime/server`,
`runtime/apps`, and `runtime/tasks` all emit through one shared Recorder. RFC §11.2.
D-074.

**Recursive contract** — a Go contract type that, directly or transitively,
contains itself. An explicit, documented V1 limitation of the schema generator:
the pinned inference engine cannot emit `$ref`/`$defs` for cycles, so
`SchemaForType` rejects a recursive contract with `ErrRecursiveContract` rather
than fail vaguely. RFC §6.1. D-052.

**Ring-buffer emitter** — the in-memory, bounded obs/v1 emitter driver
(`obs.RingBuffer`, registered as `"ringbuffer"`) Phase 15 ships — the source the
inspector pulls recent event history from. It is non-blocking by construction: a
full buffer overwrites its oldest event (counted via `Dropped()`), so a slow or
absent consumer can never stall the runtime. RFC §11.3. D-074.

**Resource template** — a server registration that serves a *family* of
resources addressed by an RFC 6570 URI template (e.g. `ui://app/{view}`) rather
than one fixed URI. Exposed as `runtime/server.Server.AddResourceTemplate` with
a typed `ResourceTemplateDef`; the handler receives the concrete URI a host
requested. The typed surface Phase 10's `ui://` auto-discovery composes. RFC
§5.1. D-054.

**Routing flag** — a typed, non-fatal signal the handler runtime raises when a
tool's output is oversized (`FlagOversizeOutput` — serialized `structuredContent`
over the size budget) or misrouted (`FlagMisroutedContent` — UI-shaped JSON placed
in the model-facing `Text`). A flag never fails the tool call; it is recorded on
the tool's `Builder` and read through `Builder.Flags()`. RFC §6.3. D-045.

## S

**Scaffold** — the project tree `dockyard new` generates: the manifest, the
example contract-first tool, the generated contract artifacts, a runnable
`main.go`, a contract test, and project metadata. Owned by `internal/scaffold`;
deterministic for a fixed invocation and golden-pinned. RFC §9.1, §10.

**Shape + size capture** — the default obs/v1 tool input/output capture policy
(`obs.CapturePolicyShape`): an event carries only the structural fingerprint of
a value (`obs.ValueShape` — kind, byte size, object field *names*, array length)
and never the values themselves, so secrets and PII never leak into the event
stream. Full-content capture (`CapturePolicyFull`) is an opt-in honoured only
when a redaction-aware `obs.Redactor` is supplied. CLAUDE.md §7. RFC §11.2. D-074.

**Single-file bundle** — the default build output for a Dockyard App UI: one HTML
file with no external origins, so the deny-by-default CSP works without declaring
domains. RFC §7.4.

**SSE sink** — `obs.SSESink`, Dockyard's out-of-band, localhost-bound
Server-Sent-Events `obs/v1` emitter driver (`runtime/obs`, driver name `sse`).
It streams the live `obs/v1` event stream to dev tooling — the Wave 8 inspector
consumes it — over its OWN loopback HTTP listener, so a stdio MCP server's
JSON-RPC pipe is never corrupted. It is non-blocking (a slow subscriber has
events dropped, never the runtime stalled) and refuses any non-loopback bind
address. RFC §11.3. brief 05 §3.3. D-075.

**`Store` seam** — the `Store` interface all durable state goes through (tasks,
`obs/v1` history, inspector state). V1 driver: `modernc.org/sqlite`. The seam keeps
a future Postgres driver addable without a rewrite. RFC §13. D-007, D-025.

**Store driver** — a concrete implementation of the `Store` seam registered via an
`init()` blank-import. V1 ships two: `inmem` (in-memory, for single-user stdio apps)
and `sqlite` (durable, `modernc.org/sqlite`). Every driver must pass the conformance
suite. RFC §13. D-026.

**Supervisor** — within the dev-loop orchestrator (Phase 19), a
`context`-scoped unit owning one child process's lifecycle: start, restart
(terminate-then-start, no orphan), and a clean stop with a SIGTERM-then-SIGKILL
grace window. The orchestrator holds one supervisor per supervised child — the
Go MCP server, the Vite dev server — and the inspector lands later as one more.
RFC §9.2. D-084, D-085.

**Streamable-HTTP transport** — the current MCP HTTP transport (the 2025-03-26
spec's streamable HTTP), one of Dockyard's two production transports alongside
stdio. `runtime/server.Server.HTTPHandler` returns an `http.Handler` serving it,
with explicit HTTP security options and the `getServer` per-request seam. RFC §5.2.
brief 03 §2.3.

**Stale generated output** — a generated artifact (a JSON Schema or
`contracts.ts`) whose on-disk content no longer matches a fresh regeneration
from its Go contract source — i.e. the source changed without `dockyard
generate` being rerun. Detected by the drift cross-check (`codegen.CheckStale`);
a build blocker, never a warning. RFC §6.2. D-034.

## T

**Task** — a durable MCP Tasks state machine that wraps a task-augmented
request: instead of blocking for the final result, the receiver returns a task
the requestor polls and resumes. A task carries an ID, a lifecycle status, and
the underlying request's eventual result (RFC §8.1, brief 02 §2.1).

**Task lifecycle** — the five-status state machine a task moves through:
`working` (mandatory initial), `input_required`, and the terminal `completed` /
`failed` / `cancelled`. Legal transitions are `working →
{input_required, completed, failed, cancelled}` and `input_required →
{working, completed, failed, cancelled}`; terminal statuses are immutable.
Enforced by `runtime/tasks.Engine` (RFC §8.3, brief 02 §2.2).

**`CreateTaskResult`** — the result a receiver returns for an accepted
task-augmented request, in place of the underlying request's own result. It
wraps the `Task` object; the actual result is fetched later via `tasks/result`
(RFC §8.3, brief 02 §2.3).

**`input_required`** — the non-terminal task status meaning the receiver needs
input from the requestor (e.g. an elicitation). The requestor responds by
calling `tasks/result`, the channel over which the elicitation is delivered
(RFC §8.3, brief 02 §2.5).

**Tasks engine** — `runtime/tasks.Engine`, the server-side `tasks/*` JSON-RPC
method router and task-lifecycle owner. It routes `tasks/get`/`result`/`cancel`/
`list` and substitutes a `CreateTaskResult` for a task-augmented `tools/call`.
The go-sdk cannot route a method outside its fixed dispatch table, so Dockyard
routes `tasks/*` itself behind the engine (RFC §8.2, brief 03).

**`TaskStore`** — the persistence seam for durable task state
(`runtime/tasks.TaskStore`). Phase 13 ships an in-memory driver; Phase 14
supplies the durable `Store`-backed driver — a typed facade over the `Store`
seam (`tasks.NewStore`, D-070) with its own forward-only migration, TTL
enforcement, per-requestor concurrency caps, and a purge sweep (RFC §8.5).
Proven against every backing by the shared `TaskStore` conformance suite
(`runtime/tasks/taskstoretest`).

**`TaskHandle`** — the handler-facing API for a long-running task
(`runtime/tasks.TaskHandle`, RFC §8.4): progress reporting, status messages,
cooperative cancellation, and `input_required`-driven elicitation. Handlers stay
sync-shaped — the `TaskHandle` is how a sync-shaped handler does long
async-feeling work. It exposes only clean Dockyard types; no raw experimental
protocol struct reaches it (P3).

**TTL purge sweep** — the background goroutine that reaps expired tasks from the
`TaskStore` on a manifest-tunable interval (RFC §8.5). It honours context
cancellation and shuts down cleanly — a reusable concurrent artifact.

**Auth-context binding** — the task-security rule (RFC §8.5, brief 02 §4.5) that
scopes `tasks/*` access to the requestor's authorization context: `tasks/get`/
`result`/`cancel` reject a task created under a different context (a typed
rejection indistinguishable from "not found", so the task's existence does not
leak), and `tasks/list` scopes to the caller and is withheld entirely when the
deployment cannot identify requestors.

**Tasks transport mount** — `runtime/tasks.Mount`, the seam that routes `tasks/*`
JSON-RPC frames into `Engine.Dispatch` ahead of the SDK server (the go-sdk
rejects unknown methods before middleware) and injects the `capabilities.tasks`
block into the `initialize` handshake. RFC §8.2's "shim, by necessity"; D-071.

**Tool builder** — the `runtime/tool` fluent, typed API an app author uses to
declare an MCP tool: `tool.New[In, Out](name)` binds the input and output
contract structs, then `Describe`/`UI`/`Handler` set the rest and `Register`
installs the tool on a server with its generated schema. The contract-first
app-facing surface (RFC §6, brief 04 §3). D-029.

**Task support** — a tool's declared relationship to the MCP Tasks extension:
`forbidden`, `optional`, or `required` (manifest `task_support`, → `execution.taskSupport`
in `tools/list`). RFC §8.4.

## U

**UI auto-discovery** — the RFC §7.6 convention by which a `.svelte` file under
`web/src/apps/` becomes a `ui://` resource without a manual registration call:
`apps.Discover` walks the convention directory and lifts each file into a
`DiscoveredApp`, and `manifest.WriteDiscoveredApps` writes the resulting
`apps[]` entry back into `dockyard.app.yaml` so the tool↔UI wiring stays
visible and inspectable — convenience without hiding the architecture. The
`tools[].ui` link itself stays an explicit developer-authored field. RFC §7.6.
D-056.

**UI resource** — a resource served under the `ui://` scheme with MIME type
`text/html;profile=mcp-app`, containing the App's HTML bundle. RFC §7.1.

## V

**Vendored spec** — an external MCP specification mirrored into
`docs/specifications/`, pinned by upstream commit SHA + date, so Dockyard's
build is reproducible and the source of truth is searchable in-repo. A spec bump
is a deliberate, reviewed update of the vendored file. RFC §16. AGENTS.md §10.

**Versioned codec** — the forward-compatibility mechanism of `protocolcodec`:
codecs are keyed on the negotiated MCP `protocolVersion`, so a spec revision
registers a *new* codec for the *new* version while older peers keep theirs — a
spec bump is localized, never a refactor. RFC §16. D-009, D-022.

**View** — an MCP App's UI running inside the host's sandboxed iframe; the
client-shaped peer of an MCP host in the `ui/` `postMessage` dialect. The bridge
shell library implements the View side of that dialect. RFC §7.3. brief 01 §2.4.

**viewUUID** — the `_meta.viewUUID` key under which the bridge shell persists an
App's view-state across host-driven re-renders. The bridge framework-manages it:
asking for the same `viewUUID` again recovers the same state snapshot. RFC §7.3.
D-060.

## W

**W3C Trace Context** — the W3C distributed-tracing standard
(<https://www.w3.org/TR/trace-context/>) obs/v1 adopts for its correlation IDs:
a 16-byte trace-id and an 8-byte span-id, lowercase hex. Modelled in
`runtime/obs` as `obs.SpanContext` (`NewTrace`, `Child`). Adopting it means a
Dockyard server's spans nest natively under a calling Harbor agent's
`execute_tool` span, and Phase 16's OTel adapter has spec-shaped IDs to export.
RFC §11.2. D-074.
