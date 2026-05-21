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

## D

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

## K

**KV namespace** — a logical partition of a `Store`'s key space. Every key is
addressed by `(namespace, key)`; future sub-stores (`TaskStore`, `ObsStore`) each own
one or more namespaces, and the migration runner reserves `__store_migrations__`.
RFC §13. D-025.

## M

**Manifest** — `dockyard.app.yaml`, an app's control plane: it declares tools,
`ui://` apps, transports, and quality requirements, and drives `validate`,
`generate`, `dev`, `test`, `build`, and `install`. Loaded and structurally
validated by `internal/manifest` into a typed `Manifest` Go struct; invalid
manifests fail with source-located (`file:line`) errors. RFC §4.2. D-035, D-036.

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

## O

**`obs/v1`** — Dockyard's canonical, versioned, public observability event protocol.
The headless runtime emits it; the inspector and the post-V1 console are pure
clients. RFC §11. D-008.

## P

**`protocolcodec`** — the internal package (`internal/protocolcodec`) that is the
*only* place raw MCP extension wire formats are imported. Codecs are versioned and
keyed on the negotiated `protocolVersion`. The mechanism behind forward-
compatibility (P3). RFC §5.4. D-009.

**P1 / P2 / P3 / P4** — Dockyard's four binding properties: contract-first;
observability is a protocol; forward-compatibility by isolation; server-side only.
RFC §1.

## Q

**Quality gate** — a `quality.*` knob in the manifest (`require_loading_state`,
`require_fixtures`, `require_contract_tests`, …) declaring a quality requirement
the app opts into. `internal/manifest` parses and shape-checks the `quality`
block; the gates are *enforced* by `dockyard validate`. RFC §4.2, §9.4. D-035.

## R

**Recursive contract** — a Go contract type that, directly or transitively,
contains itself. An explicit, documented V1 limitation of the schema generator:
the pinned inference engine cannot emit `$ref`/`$defs` for cycles, so
`SchemaForType` rejects a recursive contract with `ErrRecursiveContract` rather
than fail vaguely. RFC §6.1. D-052.

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

**Single-file bundle** — the default build output for a Dockyard App UI: one HTML
file with no external origins, so the deny-by-default CSP works without declaring
domains. RFC §7.4.

**`Store` seam** — the `Store` interface all durable state goes through (tasks,
`obs/v1` history, inspector state). V1 driver: `modernc.org/sqlite`. The seam keeps
a future Postgres driver addable without a rewrite. RFC §13. D-007, D-025.

**Store driver** — a concrete implementation of the `Store` seam registered via an
`init()` blank-import. V1 ships two: `inmem` (in-memory, for single-user stdio apps)
and `sqlite` (durable, `modernc.org/sqlite`). Every driver must pass the conformance
suite. RFC §13. D-026.

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
