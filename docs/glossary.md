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

**Bridge shell library** — the Svelte library (`web/bridge/`) vendored into every
Dockyard app. It implements the *host half* of the `ui/` `postMessage` JSON-RPC
dialect so app authors never hand-write protocol code: it runs the `ui/initialize`
handshake, exposes `hostContext` as stores, fans out host→view notifications,
offers typed view→host helpers, negotiates display modes, and framework-manages
`viewUUID` view-state. RFC §7.3. D-016.

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

**Contract reference** — the `"<package/path>.TypeName"` string a manifest's
`tools[].input` / `tools[].output` field holds: a reference to a Go contract
struct, not inline schema. Resolved to a JSON Schema through the manifest's
`ContractResolver` seam (the codegen pipeline). RFC §4.2, §6.1. D-037.

## D

**Deployment mode** — one of the three run-time modes a single Dockyard app binary
supports: local **stdio** subprocess, **HTTP** service, or **Portico-managed**.
Selected at run time, not baked in. RFC §14.

**Display mode** — one of the three MCP Apps viewing styles: **inline** (widget),
**fullscreen**, **pip**. Negotiated at run time via `ui/request-display-mode` and
`hostContext.displayMode`, handled by the bridge shell library. RFC §7.2.

**Dockyard app** — an MCP server (tools + resources) optionally extended with one or
more `ui://` UI resources. A plain MCP server and an MCP App are the same artifact
at different levels of completeness. RFC §4.1.

**Drift cross-check** — the `internal/codegen` library check that hard-fails when
the generated JSON Schema and the generated TypeScript for a contract desync (a
property present in one artifact and absent or differently-optional in the
other), or when generated output is stale versus its Go source. Because Design A
generates schema and TypeScript independently (RFC §6.2), the cross-check is what
makes a generator bug a loud build failure rather than silent server↔UI drift.
Phase 18's `dockyard validate` command calls it. RFC §6.2. D-034.

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

## H

**Host profile** — a pluggable set of host-specific *derivation functions* (e.g.
deriving Claude's signed `claudemcpcontent.com` iframe origin). A host profile is
algorithms, not a capability matrix. RFC §7.5. D-012.

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
