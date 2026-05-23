# RFC-001 — Dockyard: Architecture & V1 Scope

**Status:** Draft
**Date:** 2026-05-20
**Supersedes:** none
**Research substrate:** `docs/research/01..06` (canonical index: `docs/research/INDEX.md`)

> This RFC is the design source of truth for Dockyard. When a phase plan or any
> other document drifts from it, the RFC wins. Settled decisions in this document
> are referenced as **RFC §X.Y**; the append-only decision log lives in
> `docs/decisions.md`. Open questions are collected in §18.

---

## 1. Executive summary

Dockyard is a Go-native, web-aware framework for building **production-grade MCP
Servers and MCP Apps**. It is the third product in a three-part ecosystem:

```text
Portico  — the MCP gateway      (connects and governs tools)
Harbor   — the agent framework  (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework (builds the MCP servers and apps users touch)
```

An **MCP App**, in protocol terms, is not a new primitive: it is an ordinary MCP
server whose tool carries `_meta.ui` metadata pointing at a `ui://` resource that
the host renders as a sandboxed iframe. Dockyard's thesis is that the protocol is
the easy part; the hard part is that, without a framework, every team re-invents
contract wiring, schema generation, UI quality states, local preview, host
compatibility, testing, observability, and packaging — and ships inconsistent,
demo-grade results.

Dockyard is the **paved road**: a developer scaffolds or starts blank, writes typed
Go tool handlers, optionally attaches Svelte UI resources, and gets generated
contracts, a local inspector, quality gates, an intrinsic observability stream, and
one-command packaging into a single CGo-free static binary that runs over stdio,
HTTP, or behind Portico.

**V1 scope.** A `dockyard` CLI; a server runtime building on the official Go MCP
SDK; full server-side implementation of the MCP **Apps** and **Tasks** extensions;
a contract-first codegen pipeline; a local single-server **inspector**; an
**`obs/v1`** observability protocol; three optional showcase templates; and
packaging for the three deployment modes. The multi-server fleet console, the
ChatGPT Apps SDK, and the enterprise-auth extensions are explicitly **post-V1** (§19).

**Mission statement.** Dockyard is the best open-source framework for building
**secure, observable, scalable** MCP Servers and MCP Apps, with a high minimum
quality bar enforced by the toolchain rather than by documentation.

---

## 2. Goals and non-goals

### 2.1 Goals

- **G1 — Paved road.** The fast path produces a structured, typed, testable,
  observable, secure, polished MCP server. The default is good.
- **G2 — Full protocol compliance.** Dockyard implements the MCP **Apps**
  extension (`io.modelcontextprotocol/ui`, spec revision 2026-01-26) and the MCP
  **Tasks** extension (`io.modelcontextprotocol/tasks`, experimental) completely,
  server-side, including all three Apps display modes (inline / fullscreen / pip).
- **G3 — Contract-first.** A Go contract struct is the single source of truth;
  JSON Schema and TypeScript types are generated, never hand-written; server↔UI
  drift is detected by the toolchain.
- **G4 — Server-side only.** Dockyard builds MCP servers and apps. Harbor owns the
  MCP client. The one client-shaped component Dockyard ships — the inspector — is
  a local, test-only surface (§12).
- **G5 — Quality through tooling.** `dockyard validate` and `dockyard test`
  enforce the minimum bar; sloppy apps fail the toolchain, not a review checklist.
- **G6 — Intrinsic observability.** Every Dockyard server emits a canonical
  `obs/v1` event stream with zero external infrastructure. OpenTelemetry export is
  an optional adapter, never a prerequisite.
- **G7 — One artifact, three modes.** A Dockyard app compiles to one CGo-free
  static binary that runs as a local stdio subprocess, an HTTP service, or a
  Portico-managed app — selected at run time, not baked in.
- **G8 — Forward-compatible.** The MCP protocol and its extensions are moving
  targets; Dockyard stays compliant as they evolve by isolating all extension wire
  code behind one internal interface with versioned codecs.
- **G9 — Genuinely open source.** No capability is gated behind a hosted service.
  `build` emits portable artifacts; Portico is the optional, open-source control
  plane.
- **G10 — Agent-native DX.** Dockyard ships and maintains a set of agent skills
  (Agent Skills / `SKILL.md` format) and a published technical-documentation site,
  so a developer building with Dockyard via an AI coding agent is productive from
  day one. Keeping skills and docs in lockstep with the surface is mandatory repo
  hygiene (AGENTS.md §19; master plan Phase 29).

### 2.2 Non-goals (V1)

- **N1.** The MCP **client** — owned by Harbor. Dockyard never ships a production
  client; the inspector is test-only.
- **N2.** The **ChatGPT Apps SDK** protocol — post-V1 fast-follow; the V1 widget
  shell is designed so it can be added without a rewrite (§19).
- **N3.** The **multi-server fleet console** — post-V1. V1 ships the `obs/v1`
  protocol and a single-server inspector; the console is a later pure `obs/v1`
  client (§11, §19).
- **N4.** The MCP **enterprise-auth extensions** (enterprise-managed authorization,
  OAuth client-credentials) — V2 (§19).
- **N5.** A **hosted/cloud** deployment service — Dockyard stops at portable
  artifacts. An OSS-friendly `dockyard publish` to an open registry is a parked
  V2 idea (§19), not a V1 commitment.
- **N6.** Replacing TypeScript for UI. Svelte/TypeScript own the UI; Go owns the
  server, contracts, and packaging.
- **N7.** Multimodal-output tooling, an agent runtime, or planner logic — those
  are Harbor's concerns.

---

## 3. Architecture overview

Dockyard is, structurally, **two Go programs and a contract between them**:

1. **The `dockyard` CLI / generator** — scaffolds projects, runs the dev loop,
   regenerates contracts, validates, tests, builds, and installs. One CGo-free
   static binary. (§9)
2. **The Dockyard app runtime** — a Go library, vendored into every generated app,
   that wraps the official MCP SDK and adds the Apps layer, the Tasks layer, the
   observability runtime, and the storage seam. The generated app's `main.go` is
   thin; the runtime carries the weight. (§5–§8, §11, §13)

```text
                         ┌──────────────────────────────────────────┐
   developer ── dockyard ─┤ new · dev · generate · validate · test ·  │
   (CLI/generator)        │ build · install · run · inspect           │
                         └───────────────────┬──────────────────────┘
                                             │ scaffolds / builds
                                             ▼
   ┌─────────────────────────── a Dockyard app (one static binary) ───────────────┐
   │                                                                              │
   │   internal/contracts  ──► JSON Schema + TS  (codegen, §6, single source)      │
   │                                                                              │
   │   Dockyard app runtime                                                       │
   │     ├─ MCP server core      (official go-sdk: tools, resources, transports)   │
   │     ├─ Apps layer           (ui:// resources, _meta.ui, bridge contract, §7)  │
   │     ├─ Tasks layer          (tasks/* shim over _meta/extension, §8)           │
   │     ├─ obs/v1 runtime       (headless canonical event emitter, §11)           │
   │     └─ Store seam           (modernc.org/sqlite V1 driver, §13)               │
   │                                                                              │
   │   web/  (embedded)          Svelte App UIs + Dockyard bridge shell library    │
   │                                                                              │
   └───────────────┬──────────────────────────────────────┬───────────────────────┘
                   │ stdio / streamable-HTTP               │ obs/v1 (out-of-band)
                   ▼                                       ▼
            MCP host (Claude, VS Code, …)          dockyard inspector (test-only, §12)
```

**The four-property model.** Three are product properties; one is a scope boundary:

- **P1 — Contract-first.** Every tool's input and output are typed Go structs; all
  derived artifacts (JSON Schema, TS types, fixtures) are generated.
- **P2 — Observability is a protocol.** The runtime is headless and emits `obs/v1`;
  the inspector (and the future console) are pure clients of that contract and
  never read runtime internals.
- **P3 — Forward-compatibility by isolation.** All MCP extension wire formats live
  behind one internal `protocolcodec` interface; a spec bump is a localized change.
- **P4 — Server-side only.** Dockyard produces servers. The inspector is the lone
  client-shaped component and is test-only, dev-mode-gated, and localhost-bound.

If a change would weaken P1–P3 or breach P4, stop and revise this RFC instead.

---

## 4. The Dockyard app model

### 4.1 An App is a server

There is no separate "app" object. A Dockyard **app** is an MCP **server** that
exposes tools and resources; a **UI resource** is an additive feature, not a
requirement (Brief 01 §2.1). The mission covers MCP Servers *and* MCP Apps because,
in this model, they are the same artifact at different levels of completeness:

```text
plain MCP server   =  tools + resources
MCP App            =  tools + resources + one or more ui:// UI resources
```

Consequently:

- **`dockyard new <name>` with no `--template` scaffolds a blank server** — a
  working MCP server with one example tool and no UI. This is a first-class path,
  not a degenerate case (RFC §10).
- **Adding a UI resource is a seamless, incremental step.** A developer drops a
  `.svelte` file under `web/src/apps/` and the convention-based discovery (§7.6)
  registers it as a `ui://` resource and links it to a tool — no manual protocol
  wiring, in the spirit of mcp-use's auto-discovery (Brief 04 §2.4) but with the
  wiring made explicit in the manifest so the architecture stays inspectable.
- **Templates are optional showcases** of how little ceremony a polished App
  takes — not a mandatory starting point (RFC §10).

### 4.2 The manifest — `dockyard.app.yaml`

Every Dockyard app has a manifest at its root. The manifest is the **control
plane**: `validate`, `generate`, `dev`, `test`, `build`, and `install` all read it.
It makes conventions explicit and is the single place the tool↔UI wiring is visible.

```yaml
name: customer-health
title: Customer Health
version: 0.1.0

runtime:
  transports: [stdio, http]      # which deployment modes this app supports
  ui:
    framework: svelte            # V1: svelte only
    bundle: single-file          # default; deny-by-default CSP (§7.4)

tools:
  - name: show_customer_health
    description: Show an interactive customer health dashboard for an account.
    input:  internal/contracts.ShowCustomerHealthInput
    output: internal/contracts.ShowCustomerHealthOutput
    ui: customer_health          # links to the apps[] entry below
    task_support: optional       # forbidden | optional | required (§8.4)

apps:
  - id: customer_health
    uri: ui://customer-health/main
    entry: web/src/apps/customer-health.svelte
    display_modes: [inline, fullscreen]   # subset of inline|fullscreen|pip
    csp:
      connect: [https://api.company.com]
    visibility: [model, app]

quality:                          # see §9.4 — enforced by `dockyard validate`
  require_loading_state: true
  require_empty_state: true
  require_error_state: true
  require_permission_state: true
  require_fixtures: true
  require_contract_tests: true
  require_spec_compliance: true
```

The manifest's tool input/output values are **Go type references**; the codegen
pipeline (§6) resolves them. The manifest never duplicates schema — schema is
generated.

### 4.3 The generated project layout

```text
my-app/
  dockyard.app.yaml
  go.mod
  cmd/my-app/main.go            # thin entrypoint; calls the Dockyard runtime
  internal/
    contracts/types.go          # SOURCE OF TRUTH — Go contract structs
    contracts/generated.schema.json   # generated (§6) — do not edit
    tools/show_customer_health.go     # tool handler(s)
    app/server.go               # runtime wiring (mostly generated)
  web/
    src/apps/customer-health.svelte   # a ui:// resource (convention §7.6)
    src/lib/                    # Dockyard bridge shell library (vendored)
    src/generated/contracts.ts  # generated (§6) — do not edit
    src/states/{Loading,Empty,Error,Permission}.svelte
    vite.config.ts
    dist/                       # built UI; //go:embed target (§14)
  fixtures/{happy-path,empty-state,error-state,permission-denied}.json
  tests/
    contract_test.go
    host_compat_test.go
    snapshots/
```

A developer opening any Dockyard project finds the same layout. The framework
reduces boilerplate but does not hide the architecture: generated files are boring,
readable, and clearly marked; the manifest exposes every wiring decision.

---

## 5. MCP server core

### 5.1 The SDK is the foundation — settled

Dockyard's MCP server core **builds on `github.com/modelcontextprotocol/go-sdk`**
and does not re-implement the protocol (Brief 03). The audit verdict: the SDK is
genuinely stable (v1.x, a formal no-breaking-changes guarantee, current v1.6.0,
co-maintained with Google), CGo-free, and ships the entire server primitive set —
typed tools with JSON Schema inference, resources and resource templates, prompts,
completion, elicitation, sampling, logging, progress, pagination — plus every
transport Dockyard needs.

**Settled (RFC §5.1):** Dockyard depends on the official Go MCP SDK, pinned to a
recent version, and **never forks it**. Apps and Tasks are layered on top using the
SDK's own extension hooks (§5.3).

### 5.2 Transports

The SDK provides `StdioTransport`, `StreamableServerTransport` (streamable-HTTP),
`SSEServerTransport` (legacy), `CommandTransport`, and `InMemoryTransport`. Dockyard
V1 uses **stdio** and **streamable-HTTP** as its two production transports, and
**`InMemoryTransport`** as the backbone of the inspector and contract tests.

The SDK's HTTP handlers take a `getServer func(*http.Request) *Server` callback —
a per-request server seam that Dockyard uses for HTTP-mode session wiring.

**Security defaults are set explicitly.** The SDK has flipped security-relevant
defaults between releases (cross-origin protection on in v1.4.1, off in v1.6.0 —
Brief 03 §2.3, R4). Dockyard's runtime sets DNS-rebinding protection, Origin and
Content-Type verification, and cross-origin protection **explicitly** for HTTP
deployments rather than trusting any SDK default.

### 5.3 Extension hooks — how Apps and Tasks attach

The SDK exposes exactly two hooks Dockyard needs (Brief 03 §2.4):

- **`ServerCapabilities.AddExtension(name, settings)`** — generic extension
  capability negotiation (SEP-2133). Apps negotiates as
  `extensions["io.modelcontextprotocol/ui"]`; Tasks as `["…/tasks"]`.
- **`Meta` (`map[string]any`, JSON `_meta`)** — first-class on `Tool`, `Resource`,
  `CallToolParams`, `CallToolResult`, and request/result types. Apps links a tool
  to its `ui://` resource purely through `_meta`; the Tasks shim rides the same
  plumbing.

The SDK maintainers **deliberately scoped first-class Apps support out**
(issue #933, "all primitives in place"). That is not a gap to lament — it is
precisely Dockyard's value-add. The Apps ergonomics, manifest wiring, MIME
correctness, host compatibility, and bridge contract are 100% Dockyard's to build.

### 5.4 The `protocolcodec` isolation seam — settled

All MCP extension **wire formats** (the exact `_meta` key shapes, capability
blocks, Tasks method envelopes) live behind a single internal package,
`internal/protocolcodec`. Handler-facing and manifest-facing Dockyard APIs never
expose raw protocol structs. A spec revision is then a localized,
regenerate-and-diff change — this is the mechanism behind P3 (forward-compatibility).

Because `_meta` is untyped (`map[string]any`), `protocolcodec` provides **typed
accessors** so extension-metadata bugs surface in Dockyard's own validation rather
than at runtime inside a host (Brief 03 R7).

---

## 6. The contract-first model & codegen pipeline

### 6.1 Single source of truth

A tool's **Go input and output structs are the single source of truth** (P1).
JSON Schema and TypeScript types are downstream artifacts; neither is hand-authored.
This closes the defect that makes mcp-use unsafe at scale: there, widget types are
hand-declared generics with nothing tying them back to the tool's output schema, so
server↔UI drift is silent (Brief 04 §2.6).

### 6.2 The pipeline — Design A (settled)

**Settled (RFC §6.2):** Dockyard adopts **Design A** from Brief 06 §3.1 — schema
and TypeScript are generated **independently from Go**, each by a pure-Go tool, so
the codegen path has **no Node dependency**:

```text
                       ┌─ google/jsonschema-go .For() ─► internal/contracts/generated.schema.json
internal/contracts/*.go │
   (SOURCE OF TRUTH) ────┤
                       └─ gzuidhof/tygo ───────────────► web/src/generated/contracts.ts
```

- **JSON Schema:** `github.com/google/jsonschema-go` — the same engine the official
  MCP SDK already uses internally (Brief 06 §2.3). Using anything else would create
  a divergent schema dialect; Dockyard standardizes on it.
- **TypeScript:** `github.com/gzuidhof/tygo` — AST-based, so it preserves doc
  comments, enums, and constants (Brief 06 §2.4).
- **Drift check:** because schema and TS are generated independently, a bug in one
  generator could silently desync them. `dockyard validate` **cross-verifies**
  schema vs. TS and **hard-fails on drift or stale generated output** (Brief 06 R1).
  Stale generated files are a build blocker, not a warning.

### 6.3 `content` vs `structuredContent`

A tool result is a standard MCP `CallToolResult` carrying both `content`
(model-facing, enters the LLM context) and `structuredContent` (UI-facing, excluded
from model context) (Brief 01 §2.6). Dockyard's typed-output struct maps to
**`structuredContent`**; the model-facing text the handler returns maps to
`content`. Routing UI payloads into `content` — which pollutes and inflates model
context — is a `dockyard validate` warning. This is the contract behind the
braindump's "oversized output payloads" caution.

A Dockyard tool handler therefore returns a small typed result:

```go
type Result[Out any] struct {
    Text       string         // -> content[]  (model-facing)
    Structured Out            // -> structuredContent (UI-facing, typed)
    Meta       map[string]any // -> _meta (e.g. viewUUID)
}
```

---

## 7. The MCP Apps extension

Dockyard implements the Apps extension server-side, completely, against spec
revision **2026-01-26 / SEP-1865**, extension id **`io.modelcontextprotocol/ui`**
(Brief 01).

### 7.1 What Dockyard registers

- **`ui://` resources** with `mimeType = text/html;profile=mcp-app` (the only MVP
  type). The HTML is the built Svelte bundle, served via `resources/read` as `text`
  or `blob`.
- **`_meta.ui` on tools** — the nested form `_meta.ui = {resourceUri, visibility}`.
  Dockyard **emits only the nested form** and tolerates reading the deprecated flat
  `_meta["ui/resourceUri"]` form (Brief 01 §2.3).
- **`_meta.ui` on the resource-read response** — `csp`, `permissions`, `domain`,
  `prefersBorder`. Critically, CSP and domain are read from the *`resources/read`
  response*, not only the static resource declaration (Brief 01 §2.2); Dockyard
  threads them through a single choke point so every read reply carries correct
  metadata.
- **The `extensions` capability** advertising
  `io.modelcontextprotocol/ui: {mimeTypes: ["text/html;profile=mcp-app"]}`. When a
  host does not advertise the extension, Dockyard behaves as a plain MCP server —
  the same tools work, the UI is simply not assumed (Brief 01 §2.7). Graceful
  degradation is mandatory.

### 7.2 The three display modes — settled

The Apps spec defines three viewing styles: **inline** (widget), **fullscreen**,
and **pip**. Full-spec compliance (G2) requires all three.

**Settled (RFC §7.2):** display mode is a **runtime protocol negotiation**, not a
build-framework concern. The App, inside its iframe, calls `ui/request-display-mode`
over the postMessage bridge and reacts to `hostContext.displayMode` /
`availableDisplayModes`; the host grants or denies. Therefore **plain Svelte + Vite
covers all three modes** — the negotiation lives in Dockyard's bridge shell library
(§7.3), independent of any frontend framework. An app declares the subset it
supports in `dockyard.app.yaml` (`apps[].display_modes`), and at run time the
bridge shell only offers modes the host actually negotiated (§7.5).

### 7.3 The bridge shell library — the one piece of client-shaped code

The Apps `postMessage` JSON-RPC dialect (`ui/initialize`, `ui/notifications/*`,
`ui/open-link`, `ui/request-display-mode`, `ui/message`, proxied `tools/call`) runs
**inside the iframe** — it is client-shaped. Dockyard ships it as a **Svelte bridge
shell library** (`web/src/lib/`, vendored into every app) so app authors never
hand-write protocol code. The shell:

- performs the `ui/initialize` handshake and waits for
  `ui/notifications/initialized`;
- exposes `hostContext` (theme, `styles.variables`, `displayMode`, `locale`,
  dimensions, …) as Svelte stores;
- fans out host→view notifications (`tool-input`, `tool-input-partial`,
  `tool-result`, `tool-cancelled`, `size-changed`, `host-context-changed`);
- offers typed helpers for view→host requests (display-mode, open-link, message,
  `tools/call`);
- framework-manages `_meta.viewUUID`-keyed view-state, persisting an App's view
  state across re-renders so app authors never hand-roll it;
- consumes the generated `contracts.ts` so the `structuredContent` payload an app
  renders is typed and cannot drift from the tool's output struct.

This does not breach P4 (server-side only): the shell is a *library shipped to app
authors*, not a Dockyard-operated client. The other consumer of the host half of
this dialect is the inspector (§12).

### 7.4 CSP, sandboxing, single-file bundles — settled

Apps always run in a sandboxed iframe under a deny-by-default CSP (Brief 01 §2.5).

**Settled (RFC §7.4):** generated apps default to **single-file HTML bundles**
(`vite-plugin-singlefile` or equivalent) — zero external origins, so the
deny-by-default CSP just works. Declaring `connect`/`resource` domains in the
manifest is an explicit opt-out. `_meta.ui.permissions` (camera, microphone,
geolocation, clipboard-write) are likewise opt-in via the manifest.

### 7.5 Capability-driven graceful degradation

Host support for the Apps extension varies, and it will keep changing. Dockyard
deliberately does **not** maintain a static per-host capability matrix — such a
matrix would always drift, and keeping it current would mean researching every host
on the internet and encoding compatibility guesses.

Instead, Dockyard relies on the mechanism the protocol already provides: the **MCP
capability-negotiation handshake**. A host advertises the extensions and
capabilities it supports during `initialize`; the Dockyard runtime and the bridge
shell read that and **adapt at run time** — if a host does not negotiate the Apps
extension the UI is simply not assumed (the server still works as a plain MCP
server, §7.1); if a host does not grant `pip`, the App falls back to a granted
display mode. Degradation is driven by negotiated capabilities, never by a
hardcoded host list, so a brand-new host works without a Dockyard release.

`dockyard validate` correspondingly checks **spec compliance** — that the app
conforms to the vendored MCP Apps / Tasks specs — not per-host compatibility (§9.4).
Dockyard cannot meaningfully validate against every host in existence.

Two host-specific concerns remain, and both are handled without a capability matrix:

- **`_meta.ui.domain` is auto-derived.** Developers build for all hosts; Dockyard
  derives the dedicated iframe origin automatically, including host-specific signed
  forms (e.g. Claude's SHA-256-derived `claudemcpcontent.com` subdomain). These are
  small **derivation functions behind pluggable host profiles** — algorithms, not
  capability matrices — and are never hardcoded in the core.
- **Harbor is the reference client.** Dockyard keeps Harbor's MCP client fully
  MCP-spec-compliant, so the two ecosystem halves are validated against each other
  and stay aligned as the spec evolves.

### 7.6 UI-resource auto-discovery

A `.svelte` file under `web/src/apps/` is discovered by convention and registered
as a `ui://` resource (Brief 04 §2.4) — but, unlike mcp-use, the resulting tool↔UI
wiring is **written into `dockyard.app.yaml`** so it stays visible and inspectable.
Convenience without hiding the architecture.

---

## 8. The MCP Tasks extension

Dockyard V1 implements the Tasks extension server-side
(`io.modelcontextprotocol/tasks`, experimental, SEP-1686/2663) (Brief 02).

### 8.1 Authoritative source — settled

**Settled (RFC §8.1):** Dockyard builds against the **`experimental-ext-tasks`
schema** (`schema/draft/schema.ts` + `tasks.mdx`), **not** the
`/extensions/tasks/overview` page — the overview page is out of sync and documents
a `tasks/update` method and `inputRequests`/`inputResponses` map that **do not
exist** in the real spec (Brief 02 §2.3). Dockyard does not implement them.

### 8.2 The V1 implementation — a shim, by necessity

The official Go SDK has **no released Tasks API** as of 2026-05 (Brief 03 R1).

**Settled (RFC §8.2):** Dockyard V1 implements Tasks itself — `tasks/*` method
routing, capability advertisement, and `CreateTaskResult` substitution — layered on
the SDK's `_meta`/extension primitives, with the wire layer **code-generated from a
vendored schema snapshot** and isolated in `internal/protocolcodec` (§5.4). When the
SDK ships a native Tasks API, Dockyard swaps the shim for it behind the unchanged
internal interface.

### 8.3 Lifecycle and methods

Five statuses — `working` (mandatory initial), `input_required`, and the terminal
`completed` / `failed` / `cancelled` (Brief 02 §2.2). The receiver serves
`tasks/get` (non-blocking poll), `tasks/result` (blocks until terminal; also the
channel over which `input_required` elicitations are delivered), `tasks/cancel`
(must transition to `cancelled` before responding), and `tasks/list` (paginated,
gated on requestor identifiability). Polling is the contract of record;
`notifications/tasks/status` and core `progress` notifications are best-effort.
Dockyard **emits `notifications/tasks/status` by default** — a manifest config knob
disables it; `io.modelcontextprotocol/model-immediate-response` is a per-tool
opt-in given its provisional status.

### 8.4 The handler API — handlers stay sync-shaped

Opting a tool into Tasks is a **registration-time declaration**
(`task_support: forbidden|optional|required` in the manifest, → `execution.taskSupport`
in `tools/list`). The handler signature stays simple; for genuinely long work a
handler receives a `TaskHandle` for progress, status messages, cooperative
cancellation, and `input_required`-driven elicitation. Raw experimental protocol
structs never reach the handler-facing API (Brief 02 §5).

### 8.5 The TaskStore — on the storage seam

Durable task state lives behind the `Store` seam (§13): the V1 driver is
`modernc.org/sqlite`; in-memory is used for single-user stdio apps. The runtime
enforces a max TTL, a per-requestor concurrent-task cap, and a background TTL purge
sweep — all manifest-tunable. Task IDs are crypto-strong (≥128-bit random); with an
auth context, `tasks/get|result|cancel` reject cross-context access and `tasks/list`
scopes to the caller; **`tasks/list` is not advertised** in unauthenticated
single-user stdio mode (Brief 02 §4.5).

### 8.6 Tasks × Apps

Tasks and Apps compose: the braindump's `task-runner` and `approval-flow` patterns
are an App UI bound to a task-returning tool, polling `tasks/get` (or consuming
`notifications/tasks/status`) to render progress and cancel/retry actions. The
inspector renders the task lifecycle and `input_required` round-trips so Tasks is
debuggable locally.

---

## 9. The CLI & developer experience

### 9.1 One binary, command surface

Dockyard ships **one statically-linked CGo-free binary**, `dockyard` — no `npx`, no
package fan-out, no Node on any install target (Brief 04 §3). Commands:

| Command | Purpose |
|---|---|
| `dockyard new <name> [--template t]` | Scaffold. No `--template` ⇒ blank server (§4.1, §10). |
| `dockyard dev` | MCP server + Svelte dev server + inspector + codegen watcher, one process. |
| `dockyard generate` | Regenerate JSON Schema + `contracts.ts` from Go contracts (§6). |
| `dockyard validate` | Manifest, schemas, tool↔UI mappings, MIME, spec compliance, UI states, stale codegen — non-zero exit on failure. |
| `dockyard test` | `go test` + contract tests + fixture/golden snapshots + spec-compliance + capability-degradation tests. |
| `dockyard build [--transport stdio\|http]` | One embedded-asset binary; cross-compile matrix. |
| `dockyard install claude\|cursor\|…` | Write host config, point at the binary, verify it boots. |
| `dockyard run --transport http [--port]` | Run HTTP service mode. |
| `dockyard inspect [--url …]` | Standalone inspector against any running MCP server (§12). |

This is a deliberate superset of mcp-use's `create → dev → build → deploy`, closing
every gap from Brief 04 §2.9: a real `test`/`validate` toolchain, first-class
codegen, host-config `install`, and **no proprietary cloud deploy**.

### 9.2 The `dev` loop — settled

**Settled (RFC §9.2):** `dockyard dev` is an **embedded `fsnotify`-based
orchestrator** — Dockyard does not shell out to `air`/`wgo` (Brief 06 §2.6). One
`dockyard dev` process supervises a process tree: it restarts the Go server on
`.go` changes, re-runs codegen on `internal/contracts` changes, and supervises the
Vite dev server (which handles Svelte HMR itself). The developer experiences one
command and one process even though two runtimes are choreographed underneath.

### 9.3 CLI stack — settled

**Settled (RFC §9.3):** the CLI uses **`spf13/cobra`** (Brief 06 §2.5) — a
multi-verb tool with subcommands, shell completions, and `gh`/`kubectl`-familiar
ergonomics.

### 9.4 Quality gates

`dockyard validate` and `dockyard test` are how the high minimum bar (G5) is
enforced — by the toolchain, not documentation. Categories (Brief 01 §5, braindump
Dump 1 "quality bar"):

- **Build blockers** — invalid manifest or schema; missing/mismatched `ui://`
  resource; invalid MIME; an Apps/Tasks construct that violates the vendored spec;
  broken frontend build; **stale generated contracts**; failing contract tests.
- **Required defaults** — every app ships typed input/output, fixtures, and
  loading / empty / error / permission states (`quality.*` in the manifest).
- **Warnings** — vague tool descriptions; UI payload routed into `content`;
  oversized output; action tool left at model visibility; a UI feature used with
  no graceful-degradation path for hosts that do not negotiate it.

---

## 10. Templates

Templates are **optional showcases** (§4.1), not a mandatory starting point. Two
scaffold paths:

- **`dockyard new <name>`** (no `--template`) — a blank, working MCP server: one
  example tool, typed contracts, tests, manifest, no UI. The first-class path for
  building plain MCP servers.
- **`dockyard new <name> --template <t>`** — a product-pattern showcase. **V1 ships
  three:**
  - **`analytics-widgets`** — chart / table / metric-card widgets, rendered
    inline (one App, three contract-first widget tools; see decision D-124).
  - **`approval-flow`** — human-in-the-loop review (pairs with Tasks, §8.6).
  - **`inspector`** — object / log / trace / metadata inspection panels.

Every template generates fixtures, tests, the manifest, and loading/empty/error/
permission states by default. The remaining ~5 patterns from the braindump
(`document-review`, `task-runner`, `artifact-viewer`, `form-tool`, `agent-console`)
are a **post-V1** expansion (§19). Templates are protocol-agnostic in framing —
named for workflows, never for transports (the mcp-use anti-pattern, Brief 04 §2.3).

**Shared design system.** Every template App UI, and the inspector (§12), compose
one shared frontend design system — the `web/ui/` Svelte component inventory and
the `docs/design/CONVENTIONS.md` conventions (design tokens, the four-state
`PageState`, the spec→mockup→build process). It is established **before any page is
built** (master plan Phase 10a) so Dockyard's own surfaces never drift into
duplicated components — and it is the foundation the post-V1 multi-server console
(§19) builds on too. Composing it is mandatory hygiene (`AGENTS.md` §20).

---

## 11. Observability — the `obs/v1` protocol

### 11.1 Observability is a protocol — settled

**Settled (RFC §11.1):** Dockyard observability is a **headless, canonical,
versioned event protocol — `obs/v1`** — modeled on the Harbor Console pattern
(Brief 05 §3). The app runtime emits `obs/v1` events; the inspector (§12) and the
post-V1 multi-server console are **pure clients** of that contract and never read
runtime internals (P2).

This rejects the MCP Mesh anti-pattern, where "observability" means generated Helm
charts wiring your data into a Grafana/Tempo/Redis stack you must operate yourself
(Brief 05 §2.1). Dockyard observability is **intrinsic and zero-dependency**: a
developer with one server and ten minutes sees everything, with nothing else
installed.

### 11.2 The event model

A canonical `obs.Event` carries `schema_version`, identity (`server_id`,
`session_id`), correlation IDs, `kind`, `phase`, a typed `payload`, optional
`duration_ms`, and `error` (Brief 05 §3.1). Event kinds cover `tool.call`,
`resource.read`, `prompt.get`, `app.load`, `app.bridge`, `app.user_action`,
`host.compat`, `log`, `server.lifecycle`, and task `progress`. `ErrorInfo` carries a
`silent` flag for protocol-masked failures — the class of bug the stdio transport
normally hides (Brief 05 §2.2, Sentry's insight).

Settled details: trace/span IDs adopt **W3C Trace Context** so a Dockyard server's
spans nest natively under a calling Harbor agent's `execute_tool` span. Tool
input/output capture defaults to **shape + size only**; full-content capture is
opt-in and redaction-aware (Brief 05 §4.3).

### 11.3 Transport and OTel

The runtime emits to a `RingBufferEmitter` (the inspector's source) and an
out-of-band `SSESink` on a localhost dev port — **out-of-band** so a stdio server
stays debuggable without corrupting its JSON-RPC pipe (Brief 05 §2.2). An
`OTelEmitter` maps `obs.Event` onto the OpenTelemetry MCP semantic conventions
(`span.mcp.server`, `mcp.method.name`, `gen_ai.tool.name`, …); **OTel export ships
in V1** ("OTel from day one") as a config knob — it interoperates with existing
observability stacks but is never a prerequisite to observe locally. The MCP
`logging` capability is *bridged* into `obs/v1` `log` events, not bypassed — a
Dockyard server still speaks standard MCP logging to any client.

`obs/v1` is **versioned and stable from V1, and a public, documented,
third-party-consumable contract** — the inspector, the post-V1 console, and any
external tool consume the same protocol.

---

## 12. The inspector

The inspector is Dockyard's local **test-and-debug surface** — the single place to
exercise an MCP server and its Apps without a real host. It is the lone
client-shaped component Dockyard ships and is **dev-mode-gated, localhost-only, and
read-only** (Brief 05 §4.2; the CVE-2025-49596 RCE in the official Inspector's
proxy is the cautionary tale).

It runs automatically inside `dockyard dev` and standalone via `dockyard inspect`
(`--url`, `--port`, `--no-open` for CI). It implements the **host half** of the
`ui/` postMessage bridge to render Apps locally, and surfaces:

- the live `obs/v1` event stream + a JSON-RPC log with method filtering;
- per-tool latency / error / volume analytics;
- App rendering with host-bridge emulation, device/locale/CSP testing, and all
  three display modes;
- a **fixture switcher** (happy / empty / error / permission / slow / large) wired
  to the generated contracts, so UI work proceeds before the backend is done;
- contract-drift, schema-validation, and spec-compliance verdicts;
- capability-set emulation — render an App as a host that does or does not
  negotiate Apps, Tasks, or a given display mode, to exercise graceful degradation;
- task-lifecycle and `input_required` round-trip rendering (§8.6).

This clears the bar set by mcp-use and MCPJam (render the App, emulate the bridge,
switch devices) and adds what only the framework that owns the server *and* its
generated contracts can: drift detection, fixture-driven state testing, and
host-compat verdicts (Brief 05 §2.3).

**V1 scope boundary.** The inspector is single-server. A BYOK chat tab (model-driven
tool selection) is **post-V1** — it needs an LLM-key path that V1's fixture- and
bridge-focused inspector does not. The multi-server console is post-V1 (§19).

---

## 13. Persistence & the storage seam

**Settled (RFC §13):** Dockyard V1 bundles **`modernc.org/sqlite`** — a pure-Go,
CGo-free SQLite — as its persistence driver, **behind a `Store` interface seam**
(driver pattern). The seam is mandatory: a future Postgres (or other) driver for
distributed / at-scale HTTP deployments must be addable without a rewrite.

```go
// runtime/store — the seam. V1 driver: modernc.org/sqlite.
type Store interface {
    Tasks() TaskStore   // §8.5 — durable task state
    Obs()   ObsStore    // §11  — observability history
    // future drivers (postgres, …) implement the same interface.
}
```

What is persisted: the durable `TaskStore` (HTTP / Portico modes), `obs/v1` history,
and inspector state. Single-user stdio apps may run the in-memory driver. CI
enforces `CGO_ENABLED=0`; the cross-compile target matrix is verified against
`modernc.org/sqlite`'s supported OS/arch set (Brief 06 R6).

---

## 14. Packaging & deployment modes

A Dockyard app builds to **one CGo-free static binary** with the Svelte UI embedded
via `//go:embed all:dist` (Brief 06 §2.2). The same `embed.FS` backs both the
`ui://` MCP resource handler and the inspector's HTTP preview. `dockyard build`
sequences `vite build` → `go build` so the embed target exists before compilation.

Three deployment modes, selected at run time from one artifact (G7):

- **stdio** — a local subprocess; the host config is just
  `{"command": "/path/to/app"}`. `dockyard install` writes that config and verifies
  boot.
- **HTTP** — `dockyard run --transport http`; streamable-HTTP, explicit security
  options (§5.2), durable `Store`.
- **Portico-managed** — Portico launches or routes to the app; the app keeps its
  own tool/UI contracts. Portico is the optional, open-source control plane — there
  is no proprietary cloud (G9).

`dockyard build` cross-compiles the darwin/linux/windows × arm64/amd64 matrix and
emits checksums.

---

## 15. Security

- **Sandbox + CSP.** Apps render in a sandboxed iframe under a deny-by-default CSP;
  single-file bundles are the default (§7.4). Domains and iframe permissions are
  opt-in via the manifest. Hosts may further restrict but never loosen.
- **Tasks.** Crypto-strong (≥128-bit) task IDs; auth-context binding rejects
  cross-context access; `tasks/list` is withheld when requestors are not
  identifiable; enforced max TTL and per-requestor concurrency caps (§8.5).
- **HTTP transport.** DNS-rebinding, Origin/Content-Type, and cross-origin
  protections set explicitly — never inherited from SDK defaults (§5.2).
- **Inspector.** Dev-mode-gated, localhost-only, read-only; never a production
  client and never an arbitrary-execution proxy (§12).
- **Observability.** Tool input/output capture defaults to shape+size; full-content
  capture is opt-in and redaction-aware (§11.2).
- **Secrets.** No hardcoded secrets, including in generated code and tests;
  `dockyard validate` flags hardcoded-environment assumptions.

---

## 16. Forward-compatibility strategy

Dockyard's compliance must survive a moving protocol (G8, P3). The mechanisms:

1. **One isolation seam.** All extension wire formats live in
   `internal/protocolcodec` (§5.4); nothing else imports raw protocol structs.
2. **Vendored spec snapshots.** The Apps spec (revision 2026-01-26) and the Tasks
   experimental schema are vendored into `docs/specifications/`, pinned by commit
   SHA and date. A spec bump is a deliberate, reviewed update.
3. **Versioned codecs.** `protocolcodec` keys its encoders/decoders on the
   negotiated `protocolVersion`; deprecated shapes (the flat `_meta["ui/resourceUri"]`)
   are tolerated on read, never emitted.
4. **Schema-pinned Tasks wire layer.** The Tasks wire layer is built against the
   vendored, SHA-pinned experimental schema (§8.1, §8.2). For V1 the Go wire types
   are **hand-written** against that snapshot and guarded by golden tests, so a
   spec revision still surfaces as a visible diff — the regenerate-and-diff
   discipline holds even though the regeneration is manual. A schema → Go
   *generator* is a deliberate deferral (decision D-024): standing it up now would
   be premature, and the small `_meta`-borne / capability subset V1 needs does not
   warrant it. When the generator lands it replaces the hand-written types behind
   the unchanged `protocolcodec` interface; the forward-compatibility property —
   one isolation seam, a pinned snapshot, a diffable update — is preserved either
   way.
5. **SDK currency.** Dockyard pins a recent SDK version and runs an SDK-version
   compatibility check in CI, because the SDK's security defaults shift between
   releases (§5.2, Brief 03 R4/R7).
6. **MCP spec version.** V1 targets the spec version the pinned SDK supports
   (2025-11-25, the current stable); Dockyard tracks the Go SDK and updates as it
   advances.
7. **Capability-driven adaptation.** Dockyard never hardcodes a per-host capability
   matrix — host support is read from the MCP capability-negotiation handshake at
   run time and features degrade gracefully (§7.5). New hosts work without a
   Dockyard release; Harbor is kept fully spec-compliant as the reference client so
   the two halves of the ecosystem stay aligned.

---

## 17. Stack decisions

Settled, from Brief 06 unless noted. CGo-free throughout; CI enforces `CGO_ENABLED=0`.

| Concern | Choice | Rationale |
|---|---|---|
| Language / toolchain | **Go 1.26**, pinned in every `go.mod` | "Go 2026 standards"; mature generics + `slog` |
| MCP protocol core | **`modelcontextprotocol/go-sdk`** v1.x, pinned | stable, CGo-free, full server surface (§5) |
| Logging | **`log/slog`** (+ `NewMultiHandler`) | stdlib; fans out to dev console + `obs/v1` |
| JSON | stdlib **`encoding/json`** (v1) | `json/v2` still experimental in 1.26 — deferred |
| JSON Schema | **`google/jsonschema-go`** | same engine as the MCP SDK; one dialect (§6) |
| Go → TypeScript | **`gzuidhof/tygo`** | AST-based; keeps comments/enums; pure Go |
| CLI | **`spf13/cobra`** | multi-verb, completions, familiar ergonomics |
| File watch | **`fsnotify`** in an embedded orchestrator | no external dev-tool dependency (§9.2) |
| UI framework | **plain Svelte + Vite** | covers all 3 display modes; trivial to embed (§7.2) |
| Asset embedding | **`//go:embed all:dist`** | stdlib; one static binary |
| Persistence | **`modernc.org/sqlite`** behind a `Store` seam | pure-Go SQLite; swappable for Postgres (§13) |
| Lint | **golangci-lint v2**, shipped inside generated projects | uniform quality bar through tooling |
| License | **Apache-2.0** | matches the ecosystem; OSS-pure (G9) |

---

## 18. Resolved questions

The questions the briefs surfaced have all been resolved; recorded here for
traceability — they become `D-NNN` entries when `docs/decisions.md` is seeded.

- **Q-1 — Custom server→client notifications.** Resolved: V1 needs none. The Apps
  bridge runs over `postMessage` (not SDK notifications); Tasks progress uses the
  standard `progress` utility and `notifications/tasks/status`. SDK issue #745 is
  monitored but does not block; middleware is the interim workaround if a future
  need appears. (Briefs 03 Q-2, 02.)
- **Q-2 — `obs/v1` exposure + OTel.** Resolved: `obs/v1` is a **public, documented,
  third-party-consumable** contract from V1, and the **OTel export adapter ships in
  V1** ("OTel from day one"), as a config knob (§11.3). (Brief 05 Q-2/Q-5.)
- **Q-3 — MCP spec version.** Resolved: V1 targets **2025-11-25** (current stable);
  Dockyard tracks the Go SDK and updates as it advances (§16.6). (Brief 03 Q-4.)
- **Q-4 — Host capability matrix.** Resolved: **no static matrix.** Host support is
  read from the MCP capability-negotiation handshake at run time; Harbor is kept
  fully compliant as the reference client (§7.5, §16.7). (Briefs 01 Q-3, 05 Q-6.)
- **Q-5 — `_meta.ui.domain` derivation.** Resolved: **auto-derived**, including
  host-specific signed forms, behind pluggable host-profile derivation functions
  (§7.5). (Brief 01 Q-5.)
- **Q-6 — `validate` scope.** Resolved: `dockyard validate` enforces **spec
  compliance**, not per-host compatibility — Dockyard cannot validate against every
  host in existence (§9.4). (Brief 01 Q-6.)
- **Q-7 — `notifications/tasks/status`.** Resolved: a **manifest config knob,
  default on** (§8.3); `model-immediate-response` is a per-tool opt-in. (Brief 02
  Q-5/Q-6.)
- **Q-8 — `dockyard publish`.** Resolved: **not V1.** Parked for V2 — a minimal
  open registry expressing "MCP servers built with Dockyard" (§19). (Brief 04 Q-8.)
- **Q-9 — License.** Resolved: **Apache-2.0** (§17). (Brief 06 / §17.)
- **Q-10 — `viewUUID` view-state.** Resolved: **framework-managed** — the bridge
  shell persists App view-state keyed on `_meta.viewUUID` (§7.3). (Brief 01 Q-9.)

Implementation-level questions that surface during phase work (e.g. confirming the
SDK's per-session concurrency guarantees, Brief 03 Q-5) are handled in the phase
plans, not here.

---

## 19. Out of scope for V1 / future work

- **ChatGPT Apps SDK** — a second host protocol alongside MCP Apps. The V1 Svelte
  bridge shell (§7.3) is designed so this is a clean fast-follow, not a rewrite.
- **The multi-server fleet console** — a pure `obs/v1` fan-in client aggregating
  many Dockyard servers. Post-V1; ownership (Dockyard satellite repo vs. folded into
  Portico) is undecided (Brief 05 Q-7).
- **The remaining templates** — `document-review`, `task-runner`, `artifact-viewer`,
  `form-tool`, `agent-console` (§10).
- **Enterprise-auth extensions** — enterprise-managed authorization and OAuth
  client-credentials. V2.
- **Inspector BYOK chat tab** — model-driven tool selection in the inspector;
  post-V1 (needs an LLM-key path).
- **`dockyard publish`** — V2. There is no hosted/cloud deploy; the parked idea is
  a minimal open registry that lets servers express "built with Dockyard" (§2 N5).
- **`encoding/json/v2`** — adopt once it stabilizes (Go 1.27/1.28?) for stricter
  contract validation.
- **A native SDK Tasks API** — when the SDK ships one, Dockyard's Tasks shim (§8.2)
  is swapped for it behind the unchanged internal interface.
- **Additional persistence drivers** — a Postgres `Store` driver for distributed /
  at-scale HTTP deployments (§13).

---

## Appendix A — subsystem ↔ brief cross-reference

| RFC section | Subsystem | Informing briefs |
|---|---|---|
| §5 | MCP server core / SDK integration | 03 |
| §6 | Contract-first codegen pipeline | 06, 04, 01 |
| §7 | MCP Apps extension | 01, 03 |
| §8 | MCP Tasks extension | 02, 03 |
| §9, §10 | CLI, dev loop, templates | 04, 06 |
| §11 | `obs/v1` observability protocol | 05 |
| §12 | Inspector | 05, 04, 01 |
| §13 | Persistence & storage seam | 06, 02 |
| §14 | Packaging & deployment | 06, 04 |
| §15 | Security | 01, 02, 03, 05 |
| §16 | Forward-compatibility | 01, 02, 03 |
| §17 | Stack decisions | 06, 03 |

## Appendix B — glossary seed

Terms to formalize in `docs/glossary.md` when the scaffold lands: *MCP App*,
*UI resource* (`ui://`), *bridge shell library*, *display mode*, *contract-first*,
*`obs/v1`*, *inspector*, *host capability matrix*, *host profile*, *`Store` seam*,
*`protocolcodec`*, *task support*, *single-file bundle*, *deployment mode*.
