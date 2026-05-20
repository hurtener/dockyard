# Dockyard — Architectural decisions log

Append-only record of decisions that have been settled. **One entry per decision.**
Reading this file is the fastest way to answer "wait, why did we pick X?" without
re-litigating.

If a decision is later reversed or superseded, do **not** delete the original entry —
append a new entry with `Supersedes: D-NN` and set the superseded entry's `Status`
to `Superseded by D-MM`.

These decisions are mirrored in the RFC, which is the design source of truth. When
they conflict, the RFC wins; file an entry here noting the discrepancy and resolve
it in the same PR.

D-001…D-016 are the foundational decisions established while authoring RFC-001
(research briefs `01..06`, RFC review 2026-05-20). Later phases append `D-017+`.

---

## D-001 — Dockyard is the MCP Apps framework, server-side

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §1, §2
**Why:** Dockyard is the third product in the Portico (gateway) / Harbor (agent
framework) / Dockyard (MCP Apps framework) ecosystem. It builds MCP **servers** and
**apps**. Harbor owns the MCP client; Dockyard ships no production client. This
keeps each product's surface coherent and avoids re-implementing the client.

---

## D-002 — Build on the official Go MCP SDK; never fork

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.1
**Why:** `github.com/modelcontextprotocol/go-sdk` is stable (v1.x, no-breaking-changes
guarantee, Google-co-maintained), CGo-free, and ships the entire server primitive
set and all needed transports (brief 03). It exposes `AddExtension` + `_meta` hooks
so Apps and Tasks layer on without forking. Forking would forfeit the compatibility
guarantee and security updates.

---

## D-003 — Server-side only; the inspector is the lone client-shaped component

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §1 (P4), §12
**Why:** Harbor owns the MCP client. Dockyard's only client-shaped code is the local
inspector, which is test-only, dev-mode-gated, and localhost-bound. A production
client is out of scope and a forbidden practice (AGENTS.md §13).

---

## D-004 — Contract-first: the Go struct is the single source of truth

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §1 (P1), §6
**Why:** A tool's input/output Go structs are authored once; JSON Schema, TypeScript
types, and fixtures are generated. mcp-use has types but no contracts, so server↔UI
drift goes silent (brief 04 §2.6) — contract-first is Dockyard's headline
differentiator.

---

## D-005 — Codegen Design A: independent generation, pure-Go, no Node

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §6.2
**Why:** JSON Schema (`google/jsonschema-go` — the engine the MCP SDK already uses)
and TypeScript (`gzuidhof/tygo`, AST-based) are generated **independently from Go**,
each by a pure-Go tool, so the codegen path has no Node dependency (brief 06 §3.1).
`dockyard validate` cross-checks the two for drift; stale generated output is a
build blocker.

---

## D-006 — Plain Svelte + Vite for app UIs

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.2, §17
**Why:** The three Apps display modes (inline / fullscreen / pip) are a runtime
protocol negotiation handled by Dockyard's bridge shell library, not a build-
framework feature. Plain Svelte + Vite covers the full spec, embeds as a static
`dist/` with no adapter ceremony, and is the efficient choice; SvelteKit's
routing/SSR buys nothing for a single-view embedded App.

---

## D-007 — Persistence: `modernc.org/sqlite` behind a `Store` driver seam

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §13
**Why:** V1 bundles the pure-Go, CGo-free `modernc.org/sqlite`, but only behind a
`Store` interface seam (driver pattern). The seam is mandatory so a future Postgres
(or other) driver for distributed / at-scale HTTP deployments is addable without a
rewrite.

---

## D-008 — Observability is a protocol: `obs/v1`

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §1 (P2), §11
**Why:** The runtime is headless and emits a canonical, versioned, public,
third-party-consumable event stream — `obs/v1` — modeled on the Harbor Console
pattern. The inspector and the post-V1 console are pure clients of it. The OTel
export adapter ships in V1 ("OTel from day one") as a config knob, never a
prerequisite. This rejects the mcp-mesh anti-pattern of observability that only
works once an external Grafana/Tempo stack is operated (brief 05 §2.1).

---

## D-009 — All MCP extension wire formats are isolated in `internal/protocolcodec`

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §1 (P3), §5.4, §16
**Why:** The MCP protocol and its extensions move independently and fast. Confining
every extension wire format (`_meta` keys, capability blocks, Tasks envelopes) to
one internal package with versioned codecs makes a spec bump a localized,
regenerate-and-diff change rather than a refactor.

---

## D-010 — Tasks V1 is a `_meta` shim with a code-generated wire layer

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §8.1, §8.2
**Why:** The Go SDK has no released Tasks API (brief 03 R1). V1 implements `tasks/*`
routing, capability advertisement, and `CreateTaskResult` substitution itself, on
the SDK's `_meta`/extension primitives, with the wire layer generated from a
vendored snapshot of the **`experimental-ext-tasks` schema** (not the out-of-date
overview page, which documents a non-existent `tasks/update` method — brief 02
§2.3). When the SDK ships a native Tasks API, the shim is swapped behind the
unchanged internal interface.

---

## D-011 — Capability-driven graceful degradation; no static host matrix

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.5, §16.7
**Why:** Host support for the Apps extension varies and keeps changing. A hardcoded
per-host capability matrix would always drift and would mean encoding internet-
researched guesses. Instead Dockyard reads the MCP capability-negotiation handshake
at run time and degrades gracefully, so a new host works without a Dockyard
release. Harbor's MCP client is kept fully spec-compliant as the reference client.

---

## D-012 — `_meta.ui.domain` is auto-derived behind pluggable host profiles

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.5
**Why:** Developers build for all hosts; Dockyard derives the dedicated iframe
origin automatically, including host-specific signed forms (e.g. Claude's SHA-256
`claudemcpcontent.com` subdomain). These are small derivation **functions** behind
pluggable host profiles — algorithms, not a capability matrix (see D-011) — and are
never hardcoded in the core.

---

## D-013 — `dockyard validate` enforces spec compliance, not per-host compatibility

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §9.4
**Why:** Dockyard can verify that an app conforms to the vendored MCP Apps / Tasks
specs; it cannot meaningfully validate against every host in existence. Quality
gating tests spec compliance and graceful-degradation behaviour, not a host matrix.

---

## D-014 — `notifications/tasks/status` on by default; no custom notifications in V1

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §8.3, §18 Q-1/Q-7
**Why:** Dockyard emits `notifications/tasks/status` by default (a manifest knob
disables it); `model-immediate-response` is a per-tool opt-in. V1 needs no custom
server→client notifications — the Apps bridge runs over `postMessage` and Tasks
progress uses the standard `progress` utility — so SDK issue #745 does not block;
it is monitored, with middleware as the interim workaround if a future need arises.

---

## D-015 — License: Apache-2.0

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §17, `LICENSE`
**Why:** Apache-2.0 matches the ecosystem and keeps Dockyard genuinely open source —
no capability is gated behind a hosted service.

---

## D-016 — `_meta.viewUUID` view-state is framework-managed

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.3, §18 Q-10
**Why:** The Svelte bridge shell library persists an App's view-state keyed on
`_meta.viewUUID` across re-renders, so app authors never hand-roll view-state
plumbing.

---

## D-017 — Agent skills + published docs are V1 deliverables and ongoing hygiene

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §2 (G10), master plan Phase 29, AGENTS.md §19
**Why:** A developer building MCP apps with Dockyard via an AI coding agent should
be productive from day one. Dockyard authors a set of agent skills (Agent Skills /
`SKILL.md` format, agentskills.io conventions) and publishes a GitHub Pages
technical-documentation site. Phase 29 establishes both; from that phase on,
keeping them in lockstep with the user-facing surface is mandatory — skill/doc
drift is a defect, the same as RFC drift.

---

## D-018 — A shared frontend design system is established before any page is built

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §10, master plan Phase 10a, `docs/design/CONVENTIONS.md`,
AGENTS.md §20
**Why:** The sibling project Harbor did not establish a design system up front;
pages were built ad hoc, components and patterns were duplicated across the Console,
and a costly remediation was needed later to retrofit a shared foundation. Dockyard
has more frontend surface (the inspector, three template App UIs, the bridge shell,
the docs site, and post-V1 the multi-server console), so it establishes the design
system as a day-one charter (`docs/design/CONVENTIONS.md`) and builds the shared
`web/ui/` inventory + tokens at Phase 10a — before any page-bearing phase. From
Phase 10a on, composing the shared inventory (no duplicated components, the
four-state `PageState` on every page, tokens as the single source of visual truth,
spec→mockup→build) is mandatory hygiene.

---

## D-025 — The `Store` seam is a generic namespaced KV primitive, not Tasks/Obs accessors

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §13, `runtime/store`, Phase 03 plan
**Why:** RFC §13's illustrative `Store` interface sketches `Tasks() TaskStore` and
`Obs() ObsStore` accessors. Phase 03's scope is the seam and its two drivers only —
not the `TaskStore` (Phase 14, RFC §8.5) or `ObsStore` (Phase 15, RFC §11). Shipping
those accessors now would force Phase 03 to define out-of-scope sub-store types. The
V1 `Store` interface instead exposes a generic, namespaced, transactional key-value
primitive (`View`/`Update` → `Tx` with `Get`/`Put`/`Delete`/`Scan`) plus `Migrate`,
`Ping`, and `Close`. The future `TaskStore` and `ObsStore` are thin typed facades
constructed over a `Store`, each owning its own forward-only migrations registered
through `store.AddMigration`. This preserves RFC §13's intent — "a future driver
implements the same interface; a new persistence concern is proven by the
conformance suite" — while keeping the seam shippable in one phase. It is an
interface-shape decision, not a design change; the RFC's `Tasks()`/`Obs()` sketch is
illustrative, and a Postgres driver still implements one unchanged interface.

---

## D-026 — V1 `Store` drivers: `inmem` and `modernc.org/sqlite`; the SQLite driver is `database/sql`-based

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §13, RFC §17, `runtime/store/inmem`, `runtime/store/sqlitestore`
**Why:** RFC §13 and §17 settle `modernc.org/sqlite` — a pure-Go, CGo-free port of
SQLite3 (brief 06 §2.8) — as the V1 durable driver, with an in-memory driver for
single-user stdio apps. The SQLite driver is built on the `database/sql` driver
`modernc.org/sqlite` registers, using a single `kv(ns, key, value)` table
(`WITHOUT ROWID`, composite primary key); a future sub-store layers typed structure
via its own migrations. The connection pool is pinned to one connection
(`SetMaxOpenConns(1)`): an in-memory SQLite database is per-connection, and SQLite
serializes writers regardless. WAL journaling and a busy timeout are set via DSN
pragmas. `modernc.org/sqlite` supports all four Dockyard target triples
(darwin/arm64, linux/amd64, linux/arm64, windows/amd64 — brief 06 §4 R6); the
cross-compile matrix is verified by a later release-engineering phase. The build is
CGo-free and CI enforces `CGO_ENABLED=0` (brief 06 §4 R7).

---

## D-027 — Migrations are forward-only, append-only, idempotent, and runner-tracked

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §13, AGENTS.md §9, `runtime/store/migrate.go`
**Why:** AGENTS.md §9 mandates forward-only migrations that are never edited after
merge. Phase 03 implements one shared migration runner (`store.RunMigrations`) that
every driver's `Migrate` delegates to, so migration semantics are identical across
drivers. Migrations register globally via `store.AddMigration` (typically from a
sub-store package's `init`); the runner records each applied migration in a reserved
KV namespace (`__store_migrations__`) through the same `Tx` primitive, so idempotency
and tracking work uniformly on both drivers with no driver-specific schema. The
runner enforces append-only ordering: it rejects a registered sequence that does not
extend the applied sequence as a prefix (`ErrMigrationOutOfOrder` — covers reordering
and removal) and a recorded migration whose fingerprint diverges
(`ErrMigrationMutated` — a migration edited after merge). Each migration runs in its
own transaction so a failure leaves a clean applied prefix.
## D-022 — `protocolcodec` exposes versioned codecs behind a `Codec` interface

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.4, §16, `internal/protocolcodec`, phase plan 02
**Why:** RFC §16 mandates codecs keyed on the negotiated `protocolVersion`.
Phase 02 makes that concrete: `protocolcodec` exposes a `Codec` interface and a
registry, with `CodecFor(version)` selecting an implementation and falling back
to the default codec for an empty or unrecognised version (graceful
degradation, RFC §16 item 7), plus `CodecForStrict` for tooling that must flag
an unknown version. V1 registers one `v1Codec` for every supported version
because the Apps (2026-01-26) and Tasks (experimental) wire shapes are stable
across them; the registry is the seam at which a future spec bump registers a
*new* codec for a *new* version, leaving old peers on their old codec. Encoders
emit only current spec shapes; decoders are tolerant (ignore unknown keys,
accept deprecated forms).

---

## D-023 — The deprecated flat `_meta["ui/resourceUri"]` form is read-tolerated, never emitted

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §16 item 3, RFC §7.1, brief 01 §2.3,
`internal/protocolcodec`
**Why:** The MCP Apps spec (revision 2026-01-26) marks the flat
`_meta["ui/resourceUri"]` form deprecated and slated for removal before GA,
replaced by the nested `_meta.ui` object. Per RFC §16 item 3 deprecated shapes
are tolerated on read and never emitted. `protocolcodec` implements exactly
that: `DecodeAppsToolMeta` reads the nested form and, if absent, falls back to
the flat form (so a tool authored against an older host still links its UI);
`EncodeAppsToolMeta` emits only the nested form and actively strips the flat
key from any base `_meta` it is given. A round-trip test (decode-flat →
re-encode → assert flat key absent) makes the guarantee binding.

---

## D-024 — Phase 02 hand-writes the Tasks wire types; the schema→Go generator is deferred

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §8.2, §16 item 4, brief 02 §3/§5, phase plan 02
("Findings I'm departing from")
**Why:** RFC §16 item 4 and brief 02 §5 describe the Tasks wire layer as
"code-generated from the vendored schema." Phase 02's scope is the isolation
seam and the `_meta`-borne / capability surface — a small, stable subset of the
Tasks schema. Standing up a TypeScript-schema → Go code generator now would be
premature: the generator is a contracts-pipeline concern (Wave 2, Phase 04,
RFC §6.2) and the full `tasks/*` method-envelope wire layer is needed only when
the V1 Tasks runtime lands (Wave 5). Phase 02 therefore vendors the
authoritative schema (`mcp-tasks-experimental.schema.ts`, pinned by SHA) and
hand-writes the Go wire types this phase needs, guarded by golden tests so a
spec change is a visible diff. The forward-compatibility property is preserved
(one seam, pinned snapshot, regenerate-and-diff discipline); only the *means*
of regeneration is deferred. When the generated layer lands it replaces the
hand-written types behind the unchanged `Codec` interface.
## D-019 — Pin the Go MCP SDK to v1.6.0

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.1, `go.mod`, phase plan `phase-01-runtime-skeleton`
**Why:** D-002 settled that Dockyard builds on `github.com/modelcontextprotocol/go-sdk`
and never forks it; this records the concrete pinned version. Brief 03 §2.1
identifies v1.6.0 (2026-05-08) as the current stable release, under the v1.x
no-breaking-changes guarantee and CGo-free. Phase 01 pins exactly v1.6.0.
Version bumps are deliberate, reviewed `go.mod` changes — never a floating
dependency — and the SDK surface is isolated behind `runtime/server` so a bump
is a localized change (brief 03 R3/R4).

---

## D-020 — The runtime is an importable library; `cmd/dockyard` is a separate binary

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §3, AGENTS.md §3, phase plan `phase-01-runtime-skeleton`
**Why:** RFC §3 describes Dockyard as "two Go programs and a contract between
them" — the `dockyard` CLI/generator and the app runtime. Phase 01 establishes
the runtime as a normal importable Go package tree under `runtime/` (starting
with `runtime/server`), vendored into a generated app whose `main.go` stays
thin, and `cmd/dockyard` as a distinct binary entrypoint. They are not merged
into one package: the CLI is a developer tool, the runtime is a dependency of
every shipped app, and conflating them would pull CLI/generator code into every
app binary.

---

## D-021 — `Server.MCP()` is a temporary, documented SDK seam, not long-term API

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.4, phase plan `phase-01-runtime-skeleton`
**Why:** RFC §5.4 / P3 require that handler-facing and manifest-facing Dockyard
APIs never expose raw SDK or protocol structs. Phase 01 ships `Server.MCP()
*mcp.Server` anyway, as a deliberate, godoc-flagged seam: sibling Phases 02
(`protocolcodec`) and 07 (server core — transports, security, resource
registration) need SDK-level access before the Dockyard-owned registration
surface is complete. The leak is bounded and tracked: once Phase 07 lands the
full Dockyard registration API, `MCP()` is expected to be unexported. Recording
it here so the departure from the §5.4 intent is visible and reversible, not
silent (AGENTS.md §15).
