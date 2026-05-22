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
**Status:** Superseded by D-042 — Phase 07 retired `Server.MCP()`.
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

---

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

---

## D-028 — Wave 1 ships a wave-end E2E regression gate over its three foundation phases

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** AGENTS.md §17 / §17.7, `test/integration/wave1_test.go`
**Why:** Wave 1 shipped three independent foundation phases — `runtime/server`
(phase 01), `internal/protocolcodec` (phase 02), and `runtime/store` (phase 03) —
that each depend only on phase 00 and do not yet consume one another, so no single
phase owned a cross-subsystem integration test. AGENTS.md §17 still requires a
wave-boundary gate. `test/integration/wave1_test.go` is that gate: it exercises all
three packages' real public surfaces in one binary — a real `runtime/server` over
the SDK in-memory transport, the real `protocolcodec` codecs, and BOTH real `Store`
drivers (`inmem` and `sqlitestore`) — proves the wave's surface composes cleanly
side by side, covers a failure mode on each subsystem, and runs an N≥10 concurrency
stress under `-race` with a goroutine-leak assertion after teardown. It is a
regression gate over the shipped surface, not a fabricated seam between phases that
do not yet wire together. The §17.5 checkpoint audit of Wave 1 lands alongside it as
the same `chore(checkpoint)` PR.

---

## D-029 — The contract-first tool builder binds its contract types at construction, not via fluent type-parameter methods

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `runtime/tool` (`builder.go`), `docs/plans/phase-04-codegen.md`
**Why:** Brief 04 §3 sketches the contract-first builder as
`app.Tool("show_customer_health").Input[ShowCustomerHealthInput]().Output[...]().UI(...).Handler(...)`
— a fluent chain in which `Input` and `Output` are generic *methods*. That chain
is not legal Go: the language does not permit type parameters on methods, only on
functions and types. (Brief 06 §2.1 confirms generics are mature in Go 1.26 but
names no change that lifts this restriction; Phase 01's `runtime/server.AddTool`
is already a package function for exactly this reason.) Phase 04 therefore binds
the input and output contract types once, at construction, through the
package-level generic constructor `tool.New[In, Out](name)`; `Describe`, `UI`,
`Handler`, and `Register` are plain methods on the resulting `Builder[In, Out]`.
This is a Go-language adaptation, not a design change — the contract-first
property (P1) and the fluent, single-source-of-truth ergonomics of the brief
sketch are fully preserved. The builder still generates the tool's JSON Schema
from the bound Go structs and registers the generated schema, so the registered
schema is provably the contract.

---

## D-030 — `google/jsonschema-go` is the single JSON Schema engine and a direct dependency

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`schema.go`), `go.mod`, RFC §6.2
**Why:** RFC §6.2 and brief 06 §2.3 settle that Dockyard generates JSON Schema
with `github.com/google/jsonschema-go` — the same engine the official MCP Go SDK
uses internally. Phase 04 makes this concrete: `internal/codegen` calls
`jsonschema.For`/`ForType`, and the dependency is promoted from indirect (pulled
transitively by the SDK) to a **direct** `go.mod` requirement, because Dockyard
now imports it in first-party code. Adopting any second schema library
(`invopop/jsonschema`, `swaggest/jsonschema-go`) is rejected: it would create a
divergent schema dialect between what Dockyard generates and what the SDK
validates against. Phase 04 keeps the version the SDK already pins (`v0.4.3`) so
there is exactly one dialect; RFC §18 Q-6 (formalising the lockstep as the SDK
updates) remains open and is revisited when the SDK's pin moves.

---

## D-031 — Generated JSON Schema is marshalled deterministically and pinned by golden tests

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`schema.go` `Marshal`, `golden_test.go`,
`testdata/*.golden`), AGENTS.md §11
**Why:** Design A generates JSON Schema and TypeScript independently from Go
(RFC §6.2), so a bug in either generator — or a regression in the young upstream
`google/jsonschema-go` inference engine (brief 06 R1/R3) — could silently desync
the artifacts. Phase 04 closes this on the schema side two ways. First,
`codegen.Marshal` is deterministic: identical input always yields byte-identical
output (object properties render in struct-field order via the engine's
`PropertyOrder`; two-space indent; trailing newline), so regeneration is
byte-stable and a real change is the only thing that produces a diff. Second,
the generated schema for a representative contract set is pinned by **golden
tests** (`testdata/*.golden`, fixed contract to fixed JSON, regenerated with
`-update`), per AGENTS.md §11. Any drift in codegen output, or an upstream
inference change, surfaces as a reviewable diff rather than as a silent contract
break. This is the schema half of the RFC §6.2 drift defence; the schema-to-TS
cross-check is Phase 05's.

---

## D-032 — `gzuidhof/tygo` is the Go→TypeScript generator; the codegen path stays pure-Go

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`typescript.go`), `go.mod`, RFC §6.2,
brief 06 §2.4 / §3.1
**Why:** RFC §6.2 settles Design A — JSON Schema and TypeScript are generated
*independently from Go*, each by a pure-Go tool, so the codegen path has no Node
dependency. Phase 05 makes the TypeScript half concrete with
`github.com/gzuidhof/tygo`: an AST-based generator that reads Go source (not
reflection), so doc comments, enums and constants survive into the generated
`contracts.ts` (brief 06 §2.4). Phase 05 drives tygo through
`tygo.ConvertGoToTypescript`, which converts a Go type-declaration fragment
straight to TypeScript in-process — no on-disk importable package and no
shell-out are required, which keeps the generator unit-testable and the whole
codegen path pure-Go and CGo-free. `tygo` is promoted to a **direct** `go.mod`
dependency because `internal/codegen` now imports it in first-party code. The
Node-dependent alternative (Design B — `Go → JSON Schema → TS` via
`json-schema-to-typescript`) is rejected per RFC §6.2 and brief 06: it would pull
a Node toolchain into `dockyard generate`. Reflection-based Go→TS generators
(`typescriptify-golang-structs`) are also rejected — they lose comments and
constants (brief 06 §2.4).

---

## D-033 — Generated TypeScript carries a Code-generated header and is pinned by golden tests

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`typescript.go`, `golden_ts_test.go`,
`testdata/*.ts.golden`), AGENTS.md §5 / §11
**Why:** Design A generates schema and TypeScript independently (RFC §6.2), so —
exactly as for the schema half (D-031) — a bug in the TypeScript generator, or a
regression in tygo, could silently desync the artifacts. Phase 05 closes this on
the TypeScript side two ways. First, `TypeScriptForSource` is deterministic:
contract declarations are re-rendered through `go/printer` in source order and
tygo preserves that order, so identical input always yields byte-identical
output. Second, the generated TypeScript for a representative contract set is
pinned by **golden tests** (`testdata/*.ts.golden`, regenerated with `-update`),
per AGENTS.md §11. Every generated TypeScript file opens with the canonical
`// Code generated by dockyard; DO NOT EDIT.` marker (AGENTS.md §5) so tools and
reviewers recognise it and an app author knows the file is regenerated, never
hand-edited.

---

## D-034 — The schema↔TypeScript drift cross-check is a hard-failing library seam

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`drift.go` — `CrossCheck`, `CheckStale`,
`ErrSchemaTSDrift`, `ErrStaleGenerated`), RFC §6.2, brief 06 R1
**Why:** RFC §6.2 requires that, because schema and TypeScript are generated
independently, `dockyard validate` cross-verifies them and hard-fails on drift
or stale generated output — stale generated files are a build blocker, not a
warning (brief 06 R1). Phase 05 ships that cross-verifier as a **library** in
`internal/codegen`, leaving the `dockyard validate` CLI command to Phase 18 (a
thin caller). `CrossCheck` confirms the JSON Schema and the TypeScript interface
for one contract describe the same property set with consistent optionality;
`CheckStale` confirms an on-disk generated artifact still matches a fresh
regeneration of its Go source. Both return errors wrapping exported sentinels
(`ErrSchemaTSDrift`, `ErrStaleGenerated`) so callers branch with `errors.Is`.
`CrossCheck` deliberately compares the property *name set and optionality*, not
the full type graph: both artifacts derive from the same Go field, so a
value-type divergence would be a generator bug already caught by the
independent golden tests (D-031, D-033). Full type-graph equality is left as a
possible later hardening if a generator-divergence class ever escapes the golden
tests.

---

## D-035 — The manifest is parsed and validated structurally; quality gates are parsed but enforced later

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/manifest` (`load.go`, `validate.go`), RFC §4.2, §9.4
**Why:** RFC §4.2 makes `dockyard.app.yaml` the control plane every Wave 7
command reads. Phase 06's scope is the schema + loader + structural validation
only — not the commands that consume it. The boundary is drawn deliberately:
`internal/manifest` checks everything that is *structural* — required identity
fields, a well-formed semantic version, known enum values, unique tool and app
names, well-formed `ui://` URIs, and the `tools[].ui` → `apps[].id`
cross-reference — and rejects an invalid manifest with a source-located error.
It does **not** touch the filesystem or Go source: the `quality.*` block
(RFC §9.4) is parsed and shape-checked into the typed `Quality` struct but its
gates (loading/empty/error states present, fixtures present, contract tests
present) are *enforced* by `dockyard validate` (Phase 18), which is the
component with the project tree in hand. This keeps the manifest package a pure,
fast, side-effect-free library that any later command can call, and concentrates
all filesystem-aware checks in the CLI where they belong. Folding gate
enforcement into the loader was rejected: it would make every manifest load a
filesystem walk and couple `internal/manifest` to the generated-project layout.

---

## D-036 — Manifest errors are source-located; YAML positions come from a node-walk index

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/manifest` (`errors.go`, `position.go`,
`load.go`), RFC §4.2
**Why:** The RFC §4.2 acceptance criterion is that an invalid manifest "fails
with source-located errors (`file:line` where possible)". A typed Go struct
decoded from YAML has, by itself, no line information, so structural validation
running on the struct cannot point at a line. Phase 06 solves this by decoding
the document **twice**: once into a `yaml.Node` tree, from which a
`positionIndex` records the line of every node keyed by its dotted path
(`tools[0].input`); and once into the typed `Manifest`. Validation then looks up
each fault's field path in the index, so a struct-level rejection still renders
as `dockyard.app.yaml:7: tools[0].input: required`. Where a position is genuinely
unavailable — a missing field has no node, and `Manifest.Validate()` on a
hand-built struct has no YAML at all — the error degrades cleanly to naming the
source without a line, never failing to report. `yaml.v3`'s `KnownFields(true)`
is enabled so an unknown manifest key is a hard error, not a silent drop, and
faults are accumulated into an `ErrorList` so one load reports every problem.
A regex-only single-pass validator was rejected: it cannot give typed access to
the manifest or precise per-node positions.

---

## D-037 — Tool contract references resolve through a one-method ContractResolver seam

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/manifest` (`resolve.go`), `internal/codegen`
(`SchemaForType`), RFC §4.2, §6.1
**Why:** RFC §4.2 settles that `tools[].input` / `tools[].output` are **Go type
references**, not inline schema — "the codegen pipeline resolves them; the
manifest never duplicates schema". The reference's wire form is
`"<package/path>.TypeName"`. Phase 06 must prove these references *resolve*
(an acceptance criterion) without owning the mechanism that locates a Go type
from a string — that mechanism is `dockyard generate`'s (Phase 18), and it will
scan Go source. So Phase 06 defines a minimal seam: `ContractResolver`, a single
`Resolve(ref string) (*jsonschema.Schema, error)` method, and ships
`RegistryResolver` — an explicit `reference → reflect.Type` registry whose
`Resolve` runs the type through `internal/codegen.SchemaForType`, the
reflect-based entry point Phase 04 shaped for exactly this caller. Phase 18's
source-scanning resolver satisfies the same one-method interface, so the
manifest package never depends on *how* a type is found. The reference parser
(`ParseContractReference`) is also exported, since `dockyard generate` needs the
split package path and type name. Embedding type resolution inside the manifest
loader was rejected: it would couple `internal/manifest` to Go source scanning
and to the generated-project layout, the same coupling D-035 avoids.

---

## D-038 — Wave 2 ships a wave-end E2E test of the integrated contract-first pipeline

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** AGENTS.md §17 / §17.7, `test/integration/wave2_test.go`
**Why:** Wave 2 shipped the contract-first pipeline (RFC §4.2, §6) across three
phases — `internal/codegen` (Phases 04/05: Go struct → JSON Schema, Go → TypeScript,
the schema↔TS drift cross-check), `runtime/tool` (Phase 04: the contract-first
typed builder), and `internal/manifest` (Phase 06: the manifest loader and the
`ContractResolver` seam). Unlike Wave 1's independent foundations (D-028), these
phases are genuinely integrated: `internal/manifest.ResolveContracts` resolves a
manifest's `tools[].input`/`output` Go type references *through* `internal/codegen`,
and `runtime/tool.Builder.Register` generates a tool's schema with `internal/codegen`
and installs it on a `runtime/server`. AGENTS.md §17 requires a wave-boundary gate;
because Wave 2 is a real integrated flow, that gate is a genuine end-to-end test.
`test/integration/wave2_test.go` drives the pipeline with real components and no
mocks at the seams: it loads the shipped example manifest, resolves its tool
contract references via a `RegistryResolver` → `internal/codegen`, runs both halves
of Design A on the contract and cross-checks them, builds the tool from the
contract and invokes it over the SDK in-memory transport (asserting the registered
schema is the generated/resolved schema and typed output lands in
`structuredContent`), covers ≥1 failure mode per seam (a located manifest error,
an unresolved contract reference, `ErrSchemaTSDrift`, `ErrStaleGenerated`, a
rejected non-object contract), and runs an N≥10 concurrency stress under `-race`
against shared components with a goroutine-leak assertion after teardown. It reuses
the `integration`-package helpers established by `wave1_test.go` and
`phase04_codegen_test.go` (`quietLogger`, `stableGoroutineCount`,
`assertNoGoroutineLeak`, `canonical`) rather than duplicating them, and does not
re-cover what the existing per-phase integration tests (`phase04_codegen_test.go`,
`phase05_drift_test.go`) already pin — it is the cross-phase pipeline test. The
§17 checkpoint audit of Wave 2 folds in alongside it as one `chore(checkpoint)` PR.

---

## D-039 — The drift-check golden tests double-define each contract, by deliberate scope choice

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** `internal/codegen` (`drift_test.go`, `golden_ts_test.go`),
`test/integration/phase05_drift_test.go`, AGENTS.md §17.5 (Wave 2 checkpoint audit)
**Why:** The Wave 2 checkpoint audit observed that the schema↔TypeScript
drift-check tests carry each contract *twice*: once as real Go contract structs
(the input to `codegen.SchemaFor[...]`) and once as a hand-kept Go-source string
constant (the input to `codegen.TypeScriptForSource`) — e.g. `driftRevenueSource`
beside `showRevenueOutput`, and `driftContractTS` beside `driftContractInput` /
`driftContractOutput`. The TypeScript generator (`tygo`, D-032) reads Go *source
text*, not reflection or a live type; the schema generator (`google/jsonschema-go`,
D-030) reads a reflected Go *type*. The two halves of Design A therefore consume
two different representations of the same contract, and a test exercising both
must supply both. The duplicated string is hand-kept in lockstep with the struct,
so a careless edit to one and not the other could mask a real drift — the very
desync class `CrossCheck` exists to catch (D-034).

This is recorded as an **accepted, deliberate scope boundary** of the drift-check
golden tests, not a defect to fix in this checkpoint. The duplication is small,
local to the test files, and visible; the contracts are trivial fixtures; and the
golden tests pin each generator's output independently, so a divergence between
the struct and the string surfaces as a golden diff rather than silently. A future
hardening — single-sourcing the fixture, e.g. by deriving the Go-source string
from the struct via `go/ast` printing, or generating the struct from a shared
fixture file — is possible and would remove the hand-kept copy; it is left as
optional later work, deliberately out of scope for the Wave 2 checkpoint per its
read-only-audit-plus-punch-list charter (AGENTS.md §17.5). References D-034 (the
drift cross-check is a hard-failing library seam) and D-032 (tygo reads Go
source).

---

## D-040 — HTTP DNS-rebinding protection is a positive-sense flag mapped explicitly onto the SDK's negative knob

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.2, AGENTS.md §7, `runtime/server` (`http.go`), phase
plan `phase-07-server-core`
**Why:** AGENTS.md §7 and RFC §5.2 require the HTTP transport's security
protections to be set **explicitly**, never inherited from an SDK default —
"defaults have flipped between SDK releases" (brief 03 §2.3). The go-sdk
expresses DNS-rebinding (localhost) protection as a *negative* option,
`StreamableHTTPOptions.DisableLocalhostProtection` (on by default; set to true
to disable). A Dockyard app reasoning about its security posture should not have
to think in double negatives, and a positive-sense option cannot be left
silently at an SDK-chosen default. Phase 07 therefore exposes
`HTTPSecurity.DNSRebindingProtection` as a positive-sense boolean and maps it
explicitly: `DisableLocalhostProtection: !sec.DNSRebindingProtection`. The
Dockyard runtime always passes this field a concrete value computed from
`HTTPSecurity`, so a future SDK flip of the `DisableLocalhostProtection` default
cannot change Dockyard behaviour. `DefaultHTTPSecurity()` sets it ON.

---

## D-041 — Cross-origin protection is applied as net/http middleware, not via the deprecated SDK field

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5.2, AGENTS.md §7, `runtime/server` (`http.go`), phase
plan `phase-07-server-core`
**Why:** The go-sdk v1.6.0 `StreamableHTTPOptions.CrossOriginProtection` field
is **deprecated**: its godoc directs callers to wrap the handler with
`net/http.CrossOriginProtection` middleware instead, and notes the field's
behaviour is gated behind an `MCPGODEBUG` compatibility parameter until v1.8.0.
Building Phase 07 on a deprecated field would mean rework when it is removed and
would couple Dockyard's security posture to an SDK-version-specific debug knob —
the opposite of the explicit-security requirement (RFC §5.2, AGENTS.md §7,
brief 03 §2.3). Phase 07 therefore applies cross-origin (CSRF) protection the
SDK-recommended way: `HTTPHandler` wraps the SDK handler with
`http.NewCrossOriginProtection().Handler(...)` when
`HTTPSecurity.CrossOriginProtection` is set. `HTTPSecurity.TrustedOrigins` maps
onto `CrossOriginProtection.AddTrustedOrigin`; a malformed origin is a
constructor error, never a silent misconfiguration. The standard-library
middleware also covers Origin verification — it rejects non-safe cross-origin
requests by comparing the `Origin`/`Sec-Fetch-Site` headers against `Host`.
`DefaultHTTPSecurity()` sets it ON.

---

## D-042 — `Server.MCP()` is retired in Phase 07; the runtime exposes no raw SDK server

**Date:** 2026-05-20
**Status:** Settled — supersedes D-021
**Where it lives:** RFC §5.4, P3, `runtime/server` (`server.go`), phase plan
`phase-07-server-core`
**Why:** D-021 introduced `Server.MCP() *mcp.Server` in Phase 01 as a
deliberate, godoc-flagged *temporary* seam, recording that "once Phase 07 lands
the full Dockyard registration API, `MCP()` is expected to be unexported." Phase
07 lands exactly that surface. The Dockyard-owned registration API is now
complete — `AddTool`, `AddToolWithSchemas` (Phase 04), and `AddResource`
(Phase 07) — as are the transport entrypoints `Run`, `ServeStdio`,
`ServeInMemory`, and `HTTPHandler`. A repo-wide search confirms no remaining
consumer of `MCP()`: `internal/protocolcodec` (Phase 02) never touches the
server, and `runtime/tool` (Phase 04) registers tools through the typed
`AddToolWithSchemas` surface. The only use was a Phase 01 unit test asserting
the server was constructed, now re-expressed as a behavioural check
(registering a tool succeeds). Phase 07 therefore **removes the exported
`MCP()` method entirely** rather than merely unexporting it: there is no
in-package caller either, so an unexported `mcp()` accessor would be dead code.
The SDK `*mcp.Server` is now reached only through the unexported `s.mcp` field,
restoring RFC §5.4 / P3 — the runtime surface exposes no raw SDK or protocol
structs. This decision supersedes D-021; the bounded leak D-021 tracked is
closed.

---

## D-043 — The empty `TextContent` block is suppressed with a non-nil empty `Content` slice

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §6.3, `runtime/server` (`tool.go`), phase plan
`phase-08-handler-runtime`
**Why:** The Wave 2 checkpoint audit flagged that `runtime/server`'s
`AddToolWithSchemas` emitted an empty `TextContent` block whenever a handler
returned no model-facing text — its handler unconditionally set
`res.Content = []Content{&TextContent{Text: out.Text}}`, so an empty `Text`
produced a `TextContent{Text: ""}` block. An empty text block is noise in the
model context and a wire-shape defect. The naive fix — leaving `res.Content`
nil when `Text` is empty — is wrong: the go-sdk auto-fills a nil `Content` with
the JSON of the typed output (`server.go`, "If the Content field isn't being
used …"), which would route the entire UI-facing `structuredContent` payload
into `content[]` and pollute the model context — exactly the RFC §6.3 misroute.
Phase 08 therefore sets `res.Content` to a **non-nil but empty** slice
(`[]mcpsdk.Content{}`) when `Text == ""`: a non-nil slice still suppresses the
SDK's auto-fill (the SDK only auto-fills when `Content == nil`), so no payload
leaks, and no empty `TextContent` block is emitted either. A non-empty `Text`
yields exactly one `TextContent` block, unchanged.

---

## D-044 — Edge argument validation is a typed Dockyard pass layered over the SDK's wire validation

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §5, §6.3, `runtime/tool` (`runtime.go`), `runtime/server`
(`tool.go`), phase plan `phase-08-handler-runtime`
**Why:** Phase 08's acceptance criterion requires invalid tool-call arguments to
produce a *typed* error, not a panic and not a vague failure. The go-sdk
already validates incoming arguments against the input schema at the wire and
rejects a violation as a `CallToolResult` error before a typed handler runs —
so the no-panic guarantee already holds. What the SDK does not give is a *typed
Dockyard* error a contract-first in-process caller (the inspector, a contract
test, a future obs bridge) can branch on. Phase 08 adds that layer: the
`runtime/tool` handler runtime resolves the tool's generated input JSON Schema
once at registration and validates incoming arguments against it at the catalog
edge, before the handler runs, producing a typed `*ArgumentError` that wraps the
`ErrInvalidArguments` sentinel. The raw, undecoded wire arguments are reached
through a new `runtime/server` seam — `WithRawArguments` / `RawArguments` on the
handler context — because validating the raw JSON catches violations that do
not survive Go's decode (a missing required field decodes to a zero value; a
type mismatch is silently coerced or dropped). When no raw arguments are present
(an in-process invocation), the runtime re-serializes the decoded value to JSON
and validates that — the `jsonschema-go` validator validates JSON-shaped data,
not Go structs directly (upstream issue #23). The SDK pass remains as
defense-in-depth at the wire; the Dockyard pass is the authoritative typed
edge-validation surface for the contract-first handler runtime.

---

## D-045 — Oversized and misrouted payloads are non-fatal typed runtime flags, not errors

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §6.3, `runtime/tool` (`flag.go`, `runtime.go`), phase
plan `phase-08-handler-runtime`
**Why:** RFC §6.3 and brief 01 §3 (finding 7) warn that routing UI-shaped data
into `content[]` pollutes and inflates the model context, and the braindump
cautions against oversized output payloads. Phase 08 makes both observable as a
**runtime** signal — complementing the static `dockyard validate` warning. The
signal is a typed `tool.Flag` (`FlagOversizeOutput`, `FlagMisroutedContent`),
not an error: a flag never fails the tool call. A large output may be entirely
legitimate, and a host, not Dockyard, owns the decision to truncate or reject;
failing the call would break a working tool over a heuristic. The oversize
threshold is `DefaultOutputSizeBudget` = 256 KiB — a conservative default, not
an MCP protocol limit. Misroute detection is high-confidence only: it fires when
the model-facing `Text` parses whole as a JSON object or array; a bare JSON
string, number, or boolean is legitimate model-facing text and is not flagged.
Flags accumulate on the tool's `Builder` and are read through `Builder.Flags()`;
a future `obs/v1` bridge consumes the same typed values. This mirrors brief 03
R7's principle for `_meta`: a payload-routing defect surfaces in Dockyard's own
typed surfaces before a host ever sees the result.

---

## D-046 — Wave 3 ships a wave-end E2E test of the server core + handler runtime

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** AGENTS.md §17 / §17.7, `test/integration/wave3_test.go`
**Why:** Wave 3 shipped the MCP server core and the contract-first tool handler
runtime (RFC §5, §6.3): `runtime/server` registers typed tools and resources
over the go-sdk and serves them over the stdio, streamable-HTTP and in-memory
transports with an explicit HTTP security posture (`HTTPSecurity` /
`DefaultHTTPSecurity`), the `getServer` per-request seam, `ServeInMemory`, and
the `WithRawArguments` / `RawArguments` handler-context seam; `runtime/tool`
ships the contract-first builder and the production handler runtime — edge
argument validation against the generated input JSON Schema (typed
`*ArgumentError`), the `content` / `structuredContent` split, and the routing
flags (`FlagOversizeOutput`, `FlagMisroutedContent`). AGENTS.md §17 requires a
wave-boundary gate; because Wave 3 is a genuinely integrated server-core +
handler-runtime flow, that gate is a real end-to-end test.
`test/integration/wave3_test.go` drives the integrated surface with real
components and no mocks at the seams: contract-first tools built with the
`runtime/tool` builder and a resource are registered on a real `runtime/server`,
the server is served over the real streamable-HTTP transport (behind an
`httptest.Server` with `DefaultHTTPSecurity`) and the real in-memory transport,
and a real SDK client drives `tools/list`, `tools/call` and `resources/read`
against both — asserting typed output lands in `structuredContent` and the
model-facing text in `content[]`. It covers ≥1 failure mode per seam: a typed
`*ArgumentError` (wrapping `ErrInvalidArguments`) for invalid tool-call
arguments caught at the catalog edge with no panic, a `FlagMisroutedContent`
and a `FlagOversizeOutput` for misrouted / oversized payloads, and a cross-site
HTTP POST rejected with `403 Forbidden` by the explicit cross-origin
protection. It runs an N≥10 (`workers = 16`) concurrency stress under `-race`
against shared components — one server build, one HTTP listener, one in-memory
server — exercising every transport and RPC concurrently and asserting both
race-free flag accumulation (one flag per worker, none lost or duplicated) and
no goroutine leak after teardown. It reuses the `integration`-package helpers
(`quietLogger`, `stableGoroutineCount`, `assertNoGoroutineLeak`) rather than
duplicating them, and does not re-cover the Wave 2 contract-first codegen
pipeline or what the per-phase integration tests (`phase07_transports_test.go`,
`phase08_handler_runtime_test.go`) already pin — it is the cross-phase
server-core + handler-runtime wave-end test.

---

## D-047 — The server-side MCP Apps layer is a thin `runtime/apps` package over additive `runtime/server` seams

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.1, §7.4, §5.3, `runtime/apps`, `runtime/server`
(`server.go`, `tool.go`, `resource.go`), phase plan `phase-09-apps-extension`
**Why:** An MCP App is not a new wire primitive — it is a convention layered on
a tool and a resource, made discoverable through `_meta` and optional through
the `extensions` capability (brief 01 §2.1). Phase 09 therefore ships the Apps
extension as a small `runtime/apps` package that composes the Wave-3 server
core rather than reshaping it. Composing it cleanly needs three pieces of
metadata the Phase-07 server surface did not yet carry, so Phase 09 adds them
as **additive, non-breaking** fields: `server.ToolDef.Meta` (the tool
definition's `_meta`, where `_meta.ui` links a tool to its `ui://` resource),
`server.ResourceContent.Meta` (the resource-read response `_meta` — the choke
point the Apps spec mandates, brief 01 §2.2) and `server.ResourceDef.Meta` (the
static resource-declaration `_meta`), plus `server.Options.Extensions` /
`server.ExtensionCapability` to advertise the SEP-2133 `extensions` capability
block during `initialize` (RFC §5.3). Every existing call site uses named-field
struct literals, so the fields are purely additive. `runtime/apps` itself
constructs no raw extension wire shape: every `_meta.ui` object and the
capability JSON is produced by `internal/protocolcodec` (P3, RFC §5.4) and
normalized to a plain `map[string]any` so a caller sees the same JSON shape
in-process and over the wire. The server runtime copies these `_meta` maps
verbatim and never inspects them — it never reasons about a protocolcodec
type — keeping the isolation seam intact.

---

## D-048 — The resource-read response is the single Apps `_meta.ui` choke point; an undeclared CSP is deny-by-default by omission

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.1, §7.4, `runtime/apps` (`apps.go`), phase plan
`phase-09-apps-extension`
**Why:** Brief 01 §2.2 is explicit: a host reads `_meta.ui.csp` and
`_meta.ui.domain` from the `resources/read` *response*, not only the static
resource declaration. `runtime/apps` threads `_meta.ui` through one choke point
— `App.resourceMeta`, computed once at `Register` and returned by the read
handler on every `resources/read` of the App — so every read reply carries
correct metadata. The same `_meta.ui` is also attached to the static resource
declaration so a host inspecting `resources/list` sees it, but the read
response is the authoritative surface. The read `_meta.ui` is
**host-independent**: the resource handler does not branch on negotiated client
capabilities (the Phase-07 `ResourceFunc` seam does not receive them, and it
does not need to — a non-Apps host simply ignores `_meta.ui`, so graceful
degradation needs no per-host branching, RFC §7.5). Critically, when an App
declares no CSP, no permissions, no domain and no border preference,
`protocolcodec` omits the `_meta.ui` object entirely. That omission **is** the
deny-by-default policy required by RFC §7.4 / brief 01 §2.5: with no
`_meta.ui.csp` a host applies its deny-by-default CSP — zero external origins —
which is exactly why generated apps default to single-file bundles. Emitting an
explicit empty CSP object would be redundant and risks a host misreading an
empty allowlist; omission is the correct, spec-faithful encoding.

---

## D-049 — Phase 09 plumbs `_meta.ui.domain` verbatim; host-profile derivation is deferred to Phase 12

**Date:** 2026-05-20
**Status:** Settled
**Where it lives:** RFC §7.5, RFC §18 Q-5, `runtime/apps` (`apps.go`), phase
plan `phase-09-apps-extension`
**Why:** Brief 01 §2.8 / §5 call for a per-host capability matrix and note that
`_meta.ui.domain` is host-specific — Claude derives a SHA-256-signed
`<hash>.claudemcpcontent.com` subdomain from the server URL. RFC §7.5 and
AGENTS.md §6 settled that Dockyard does **not** maintain a per-host capability
matrix: host support is read from the MCP capability-negotiation handshake at
run time, and host-specific *derivations* live behind pluggable host profiles —
algorithms, not matrices. Phase 09 deliberately departs from brief 01's
"build a host matrix" framing: it builds none. It only plumbs the
`_meta.ui.domain` field through `_meta.ui` — an `App.Domain` set by the
developer is carried verbatim onto the resource-read response and never
derived, hashed, or host-specialized. Auto-derivation of the dedicated iframe
origin, including Claude's signed form, is Phase 12's pluggable host profiles
(RFC §7.5, RFC §18 Q-5). This keeps Phase 09 scoped to the server-side
registration + capability + CSP surface and keeps host-specific code out of the
Apps core, exactly as RFC §7.5 mandates.

---

## D-050 — `time.Time` and `json.RawMessage` get corrected schema mappings, not the inference engine's defaults

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §6.1, `internal/codegen` (`schema.go`), phase plan
`phase-04-codegen`
**Why:** A depth audit of the codegen pipeline found `internal/codegen`
silently mishandling two standard-library types because
`github.com/google/jsonschema-go` infers a property's schema from its Go type
alone. A `time.Time` field rendered as a bare `{"type":"string"}` — the engine
drops the `format: date-time` qualifier, even though `time.Time` marshals to an
RFC 3339 string. A `json.RawMessage` field rendered as `[]byte` does:
`{"type":["null","array"],"items":{integer 0-255}}` — an outright *wrong*
schema, since `json.RawMessage` is arbitrary embedded JSON, not a byte array.
Both are corrected via the engine's own `ForOptions.TypeSchemas` hook (the
documented extension point): `time.Time` maps to `{type:"string",
format:"date-time"}` and `json.RawMessage` maps to the empty schema, which
marshals to the unconstrained `true` — accepting any JSON value. The correction
lives in one place, `contractTypeSchemas`, applied on every `SchemaForType`
call, so a misdeclared `time.Time` or `json.RawMessage` contract field can
never reach a host with a lossy or wrong schema. This stays inside the single
schema dialect Dockyard standardizes on (RFC §6.2) — it overrides two
translations, it does not add a parallel schema library.

---

## D-051 — Named-constant enums, embedded structs, and value-type drift: the generated artifacts must faithfully mirror every Go contract shape

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §6.1, §6.2, `internal/codegen` (`schema.go`,
`enums.go`, `typescript.go`, `drift.go`), phase plans `phase-04-codegen`,
`phase-05-typescript-codegen`
**Why:** The same depth audit found three further ways the two generated
artifacts diverged from Go's actual JSON shape — each invisible because the
test fixtures never exercised the shape.
**Named-constant enums.** A `type Severity string` plus a `const` set rendered
as a plain `{"type":"string"}`: the engine infers from the field *type*, and a
named type's constants are invisible to reflection, so the `enum` array was
lost — while the TypeScript generator (tygo, AST-based) *did* emit the union,
so schema and TS diverged. The fix is the `WithEnum` schema option: it
registers a named type's constant values, and `SchemaFor` post-processes the
schema to stamp the `enum` array onto every property of that type — top-level,
nested, slice items, map values. `EnumsFromSource` discovers those constant
sets by parsing contract source, since reflection alone cannot; it is the seam
the `generate` pipeline uses. `enum` is additive — the inferred `type` stays —
so the schema now matches the tygo union.
**Embedded structs.** The schema *inlines* an embedded struct's fields
(correct — it matches Go's `encoding/json` field promotion), but tygo emitted
the embedded type as a *named property* (`Base: Base;`), so the two artifacts
disagreed and neither matched the wire format the UI deserializes.
`typeDeclSource` now flattens embedded struct fields at the AST level before
handing source to tygo: an anonymous field whose type is a locally declared
struct is replaced by that struct's fields, transitively, applying Go's "outer
wins" shadowing rule. An embedded type from another package is left for tygo,
since its fields are not visible. The TypeScript now inlines exactly as the
schema does.
**Value-type drift.** `CrossCheck` compared only the property *name set* and
optionality, so it was structurally blind to exactly the divergences above — a
property could be a string in the schema and a number in TypeScript and pass.
`CrossCheck` now also compares a coarse value-type *kind*
(string/number/boolean/array/object); a same-named property whose kind diverges
between the two artifacts is reported as drift. The comparison is deliberately
coarse — robust across two independent generators and their cosmetic noise
(tygo's `/* int */`, a nilable `["null",T]` type set) — and a named or
unconstrained type is treated as kind-compatible, so a legitimately opaque
field never reports a false drift. The documented `WithNullOptional`
limitation (an optional field renders `T | null` with no `?`, read as required
by the line-oriented parser) is unchanged: callers still feed `CrossCheck`
default-style TypeScript.

---

## D-052 — Recursive contracts are an explicit, documented V1 limitation, not a silent gap

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §6.1, `internal/codegen` (`schema.go`, package docs),
phase plan `phase-04-codegen`
**Why:** A recursive (self-referential) contract — a Go type that, directly or
transitively, contains itself — made `internal/codegen` hard-fail.
`github.com/google/jsonschema-go` returned a vague internal `cycle detected`
string and `SchemaForType` propagated it as a generic `ErrInvalidContract`.
JSON Schema's `$ref`/`$defs` exist precisely to express cycles, so the first
remediation attempt was a real fix: emit `$defs`/`$ref` for recursive types, or
post-process the engine's output to break the cycle. That attempt **failed for
a concrete reason**: the pinned engine — the single schema dialect Dockyard
standardizes on (RFC §6.2) — does not emit `$defs` for recursive Go types at
all. Its cycle detection fires deep inside the reflection walk *before* any
schema node for the recursive type exists, and `ForOptions` exposes no hook to
supply a `$ref`, break the cycle, or post-process it. A real fix would mean
forking the engine and maintaining a divergent inference path — exactly the
divergent-dialect cost RFC §6.2 settled against. Recursion is therefore an
**explicit V1 limitation**. `SchemaForType` detects the cycle up front with its
own depth-first walk and returns `ErrRecursiveContract` — a specific,
actionable error that names the cycle path and cites this decision — instead of
leaking the engine's vague string. `ErrRecursiveContract` wraps
`ErrInvalidContract`, so existing `errors.Is` callers keep working. The
limitation is asymmetric: the TypeScript generator (tygo) handles recursion
natively, so only the schema half is constrained; a contract author who needs a
tree shape uses a non-recursive encoding (e.g. a flat node list with id
references) until a post-V1 phase revisits `$defs` support.

---

## D-053 — Panic safety is a toolchain-enforced guarantee: every handler-invocation path is recover-wrapped

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §5, AGENTS.md §5/§13, `runtime/server` (`recover.go`,
`tool.go`, `resource.go`), phase plans 07/08
**Why:** The "never panic across the MCP boundary" rule (AGENTS.md §5, §13) was
enforced only on the *registration* path — `addToolSafe`, `addResourceSafe`
recover a schema-inference or bad-URI panic. The *handler-invocation* path was
unguarded: `AddTool`, `AddToolWithSchemas`, and `AddResource` called the app
author's handler directly, and the pinned go-sdk v1.6.0 does not recover handler
panics either. A single panicking tool or resource handler on a live
`tools/call` / `resources/read` therefore crashed the whole server process,
breaking every connected host — the rule was a docstring instruction, not a
guarantee. This decision makes it a guarantee: a single chokepoint, `guardHandler`,
wraps every handler-invocation path; a recovered panic becomes a typed error
(`*panicError`, wrapping the exported `ErrHandlerPanic` sentinel) which the SDK
turns into a clean `IsError` tool result / resource-read error. The panic is
logged with its stack via `slog` so the bug stays diagnosable, but it never
reaches the host and never crashes the process. Because the contract-first
handler runtime (`runtime/tool`) installs its handler through
`AddToolWithSchemas`, wrapping the three `runtime/server` entry points covers the
whole runtime — `runtime/tool` needs no separate guard. The guarantee is proven
by tests that register a panicking handler, call it over a real transport, and
assert the server survives and returns an error result.

---

## D-054 — `AddResourceTemplate` is exposed as a typed, panic-recovered runtime surface

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §5.1, brief 03 §2.2, `runtime/server` (`resource.go`),
phase plan 07
**Why:** The go-sdk offers `(*Server).AddResourceTemplate`, RFC §5.1 names
resource templates among the SDK primitives Dockyard builds on, and brief 03
§2.2 ties them to the `ui://` scheme — a template serves a `ui://` family
without enumerating every member. Phase 07 exposed `AddResource` but not
`AddResourceTemplate`, leaving Phase 10's planned `ui://` auto-discovery without
the typed surface it needs. This decision adds `AddResourceTemplate` consistent
with `AddResource`: a typed `ResourceTemplateDef` (no raw SDK struct on the
surface, P3 / RFC §5.4), the same `ResourceFunc` handler shape (the handler
receives the concrete URI a host requested, since a template addresses a
family), absolute-URI-template validation that rejects a scheme-less template as
a Dockyard error rather than an SDK panic, duplicate rejection, and the
D-053 panic-recovered handler invocation. The runtime surface gains one method;
the Apps layer composes it rather than reaching past the runtime to the SDK.

---

## D-055 — Manifest validation gains origin, CSP/bundle-coherence, and orphan-app checks

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §4.2, §7.4, §8.6, `internal/manifest` (`validate.go`),
phase plan 06
**Why:** The manifest loader validated structure well but skipped three checks a
depth audit found. (1) `csp.connect` / `csp.resource` values were never checked
as well-formed origins, and a `bundle: single-file` app declaring external CSP
origins was accepted despite being internally contradictory — a single-file
bundle inlines every asset and loads no external origin (RFC §7.4), so the
opt-out is dead config. (2) The reverse of the `tools[].ui → apps[].id` check
was missing: an orphan `apps[]` entry referenced by no tool shipped silently.
(3) `task_support` had no cross-field coherence check. This decision adds all
three as structural, source-located validations: `validateOrigin` requires a
`scheme://host[:port]` form with an allowed scheme and no path/query/fragment; a
single-file bundle with any CSP origin is rejected with a fix hint; an
unreferenced app is flagged; and tools that wire the *same* `apps[]` entry must
agree on `task_support`, because an App's UI is built against one task model and
cannot serve both a synchronous and a task-returning tool on the same surface
(RFC §8.6). The reference example manifest and the `valid-full` fixture, which
both carried the single-file-plus-CSP contradiction, are corrected to
`bundle: multi-file` — the coherent shape for an app that opts into an external
origin.

---

## D-056 — Discovery surfaces the App; the tool↔UI link stays an explicit manifest field

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.6, `runtime/apps` (`discovery.go`),
`internal/manifest` (`wiring.go`), phase plan 10
**Why:** mcp-use's widget-by-convention binds a widget to a tool *implicitly*
by matching the component file stem to the tool name (brief 04 §2.4). Adopting
that wholesale would re-introduce exactly the hidden-architecture problem
RFC §7.6 rejects — "convenience without hiding the architecture." Phase 10
therefore splits the convention in two: `apps.Discover` finds a `.svelte` file
under `web/src/apps/` and lifts it into a `DiscoveredApp`, and
`manifest.WriteDiscoveredApps` writes the corresponding `apps[]` entry into
`dockyard.app.yaml` — but the `tools[].ui` reference that actually wires a tool
to that App stays a developer-authored manifest field. Discovery removes the
boilerplate of hand-registering the resource; it never silently invents the
tool↔UI mapping. The wiring is always visible and inspectable in the manifest,
and `dockyard validate` (Phase 18) guides the developer to add the
`tools[].ui` line — the manifest's orphan-app check (D-055) names any App no
tool has wired yet.

---

## D-057 — One embed.FS backs the ui:// resource handler; an empty bundle is a typed error

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §14, `runtime/apps` (`embed.go`, `bundlefs.go`),
phase plan 10
**Why:** RFC §14 and brief 06 §2.2 settle that a Dockyard app embeds its built
Svelte UI via `//go:embed all:dist` and that the *same* `embed.FS` backs both
the `ui://` MCP resource handler and the inspector's HTTP preview — never a
second copy. Phase 10 ships the `apps.Bundle` type: a read-only,
`embed.FS`-backed view of the built `dist/` tree, immutable after construction
and safe for concurrent use, from which `RegisterDiscovered` reads each App's
HTML. A missing `dist/` directory makes the Go *build* fail at the `//go:embed`
directive itself — the clean build-time failure the acceptance criterion
requires. Its runtime-side analogue — an embed target that resolved but holds
no built files — is `Bundle.Validate` returning a typed error wrapping
`ErrEmptyBundle`; the runtime never panics on an empty bundle. The `all:`
prefix on the directive is load-bearing: without it `//go:embed` skips
`_`/`.`-prefixed files, which a multi-file Vite build can emit as hashed chunk
names (brief 06 §2.2).

---

## D-058 — WriteDiscoveredApps re-marshals the manifest; inline comments are not preserved

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.6, `internal/manifest` (`wiring.go`), phase plan 10
**Why:** Writing the discovered wiring back into `dockyard.app.yaml` (RFC §7.6)
means rewriting the file. `WriteDiscoveredApps` parses the manifest into the
typed `Manifest` struct, merges the discovered `apps[]` entries, structurally
validates the merged result, and re-marshals through `gopkg.in/yaml.v3`.
`yaml.v3` re-marshalling normalises formatting and does **not** preserve inline
comments. This is accepted for V1: the manifest is machine-authored by
`dockyard new` and machine-maintained by discovery, the merge is conservative
(a developer-authored `apps[]` entry is never overwritten) and idempotent (a
re-run with the same discovery set is a no-op), and the merged result is
validated before any byte is written — `WriteDiscoveredApps` never writes an
invalid manifest. To read a manifest that legitimately carries a `tools[].ui`
pointing at an App not yet discovered — the natural pre-discovery state, which
`Load`'s cross-reference check would reject — `WriteDiscoveredApps` parses
without the cross-reference checks and applies full validation only to the
merged result. A comment-preserving manifest editor is a deliberate deferral.

--

## D-059 — The bridge shell negotiates display modes by capability, never a host matrix

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.2, §7.5, `web/bridge`, phase plan 11
**Why:** Brief 01 §2.8 / §5 describe a per-host capability matrix (e.g. "VS Code
has no fullscreen/pip") as a build-time gate. RFC §7.5 and AGENTS.md §6 forbid a
hardcoded host matrix — it always drifts. Phase 11's bridge shell therefore
negotiates display modes (inline / fullscreen / pip, RFC §7.2) **purely from the
negotiated `hostContext.availableDisplayModes`** delivered in the `ui/initialize`
result and patched by `host-context-changed`. `requestDisplayMode(mode)` rejects
a mode absent from `availableDisplayModes` *client-side* with a
`DisplayModeUnavailableError` and no round trip, and otherwise forwards
`ui/request-display-mode` and reflects the host's grant/deny. A brand-new host
works without a Dockyard release. The phase ships no host matrix; that concern is
out of scope (host-specific *derivations* live behind Phase 12's host profiles).

---

## D-060 — `_meta.viewUUID` view-state is framework-managed by the bridge shell

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.3, `web/bridge` (`view-state.ts`), phase plan 11
**Why:** Brief 01 open question Q-9 asks whether `_meta.viewUUID`-based view-state
persistence is framework-managed or left to the app author. RFC §7.3 settles that
the bridge **framework-manages** it. Phase 11 implements a `ViewStateStore`: one
in-memory snapshot per `viewUUID`, exposed as a Svelte store via
`bridge.viewState<T>(uuid)`. Asking for the same `viewUUID` again recovers the
same snapshot — that is how an App's view-state round-trips across a host-driven
re-render (a result re-push, a display-mode change, a re-mount). The store is
scoped to one bridge session, the lifetime `viewUUID` is defined over; it is not
a cross-session durable layer (that is the host's Store, RFC §13). `callTool`
attaches `_meta.viewUUID` when a view handle is supplied so a proxied `tools/call`
correlates with its view. App authors never hand-roll view-state.

---

## D-061 — The bridge consumes a `ToolContract` shape; codegen must satisfy it

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §6, §7.3, `web/bridge` (`contracts.ts`), phase plan 11
**Why:** RFC §7.3 says the bridge consumes the generated `contracts.ts` so an
App's `structuredContent` payload is typed and cannot drift from the tool's Go
output struct (P1). The contract-first codegen that emits `contracts.ts` is
Phase 06, which has not landed when Phase 11 ships. Rather than depend on
non-existent generated output, Phase 11 defines the **shape** generated
`contracts.ts` must satisfy: a `ToolContract<I, O>` interface (tool name + phantom
input/output type carriers) plus `ContractInput` / `ContractOutput` extractors and
a `defineContract` helper. `bridge.callContract(contract, args)` and
`onToolResult<ContractOutput<C>>` are typed end-to-end against it. When Phase 06's
codegen lands, its generated `contracts.ts` must structurally satisfy
`ToolContract` (a `contracts` object of `ToolContract` values keyed by tool name)
for the typed `structuredContent` path to hold — recorded here so the obligation
is not lost.

--

## D-062 — `_meta.ui.domain` is auto-derived through a pluggable host-profile seam

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.5, RFC §18 Q-5, `runtime/apps`
(`hostprofile.go`, `domain.go`, `apps.go`), phase plan `phase-12-host-profiles`
**Why:** D-049 deferred `_meta.ui.domain` derivation: Phase 09 carried an
`App.Domain` onto the resource-read response verbatim. Phase 12 resolves it.
`App.Domain` is now a host-agnostic *domain label*; the concrete
`_meta.ui.domain` origin is **auto-derived** (RFC §7.5, D-012, RFC §18 Q-5
resolution) through a `HostProfile` — an interface + factory + driver seam
(AGENTS.md §4.4). A `HostProfile` carries host-specific *derivation functions
only* — algorithms, never a capability matrix — reaffirming the brief 01 §2.8 /
§5 / §6 Q-3 departure D-049 first recorded: Dockyard builds no per-host feature
table (D-011). Drivers self-register via `init()`; `HostProfileFor` looks up by
host id; an empty id resolves to the always-registered `generic` verbatim
profile, so the Phase 09 behaviour is the default and a non-signing host is
unaffected. The single choke point `DerivedDomain` runs the chosen profile, and
`apps.go` calls it without naming any host — host-specific code lives only in
driver files behind the seam, exactly as RFC §7.5 mandates. An empty label
still derives an empty origin, preserving Phase 09's deny-by-default `_meta.ui`
omission (RFC §7.4).

---

## D-063 — The Claude host profile derives `<hex128>.claudemcpcontent.com` from SHA-256

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.5, `runtime/apps/hostprofile_claude.go`, phase plan
`phase-12-host-profiles`
**Why:** Brief 01 §2.5 documents Claude's dedicated-origin form as
`<hash32>.claudemcpcontent.com`, "a SHA-256 hash of the MCP server URL", and
§4 sharp edge 3 stresses this is a Claude implementation detail, not a spec
mandate, that must not be hardcoded in the core. The `claude` host profile
implements it concretely as: `hash = lowercase-hex(SHA-256(serverURL + "\x00" +
domainLabel)[:16])`, origin = `hash + ".claudemcpcontent.com"`. The chosen
concrete form fixes three under-specified points. (1) **Length:** the first
16 bytes of the digest, hex-encoded to 32 characters — matching brief 01's
`hash32`, 128 bits of collision resistance, well inside the 63-character
DNS-label limit. (2) **Hash input:** both the server URL *and* the App's domain
label, NUL-separated, so each server gets an origin it cannot forge for another
server *and* two Apps on one server can request two distinct dedicated origins;
the NUL separator cannot appear in either half, so distinct pairs cannot
collide by concatenation. (3) **Missing server URL:** a non-empty label with no
server URL is a typed error, never a guessable/forgeable origin. The form is
isolated in one driver file, so a correction when Claude's exact algorithm is
confirmed is a one-file change behind the seam.

---

## D-064 — A signing host profile requires the MCP server URL on the App

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** RFC §7.5, `runtime/apps` (`apps.go`,
`hostprofile_claude.go`), phase plan `phase-12-host-profiles`
**Why:** A signed dedicated origin (D-063) is, by construction, derived from the
MCP server URL — that binding is the property that stops one server claiming
another's origin. The runtime therefore needs the server URL at App
registration. Rather than reach into transport state — which is not known at
`apps.Register` time and varies per deployment — Phase 12 adds an explicit
`App.ServerURL` field the developer (or a future scaffold/manifest layer)
supplies. The default `generic` verbatim profile ignores it, so the field is
optional for the common single-file-bundle case; a signing profile that is
handed a non-empty domain label with an empty `ServerURL` fails `Register` with
a wrapped `ErrInvalidApp`, never a panic and never a forgeable origin. This
keeps the host-profile seam pure (a derivation function over explicit inputs)
and defers negotiated-host plumbing — wiring the profile id and server URL out
of the live `initialize` handshake — to a later phase, as the Phase 12 plan's
non-goals state.

---

## D-065 — Design tokens ship as `--dy-*` CSS custom properties with a typed companion

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `web/ui/src/tokens.css`, `web/ui/src/tokens.ts`,
`docs/design/CONVENTIONS.md` §5, `docs/design/design-spec.md` §2, phase plan
`phase-10a-design-system`
**Why:** The design system needs one source of visual truth that is *both* what
a browser actually renders and what an MCP host theme can override. A CSS
custom property is the only form that satisfies both: brief 01 §2.3 documents
that an MCP App receives host-themeable CSS variables via
`hostContext.styles.variables`, so the `--dy-*` surface a `web/ui` component
reads is the *same* surface a host theme overrides — no translation layer.
Tokens are therefore the runtime source of truth as a `tokens.css` `:root`
block (`--dy-` prefix, namespaced so they cannot collide with host or app
variables), and `tokens.ts` is a *typed companion* — it names every token so
component code refers to a token by a TypeScript-checked identifier rather than
a raw string, and `tokenVar()` builds the `var()` reference. The TS layer holds
token *names*, never values: values live only in CSS, because they are
theme-dependent. Theming (design-spec.md §2.4) is a token-set swap: the blocks
are scoped to `[data-dy-theme]`, V1 ships `light`, and a dark theme is a new
`[data-dy-theme='dark']` block with no component change. The palette is derived
by eye from `docs/design/logo.png`; a brand correction is a one-file edit to
`tokens.css`.

---

## D-066 — `web/ui` components use plain-Svelte props + callback props, no SvelteKit

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `web/ui/` (every `*.svelte` component), `web/ui/svelte.config.js`,
`docs/design/CONVENTIONS.md` §3, phase plan `phase-10a-design-system`
**Why:** `web/ui` is consumed by surfaces with different mount stories — the
inspector, the template App UIs inside a sandboxed iframe, and the docs site.
D-006 already settles plain Svelte over SvelteKit framework-wide; Phase 10a
fixes the *component-API* convention that follows from it. Every component is a
plain Svelte 5 render unit: typed props via the `$props()` rune, content via
typed `Snippet` slots, and events surfaced as **callback props** (`onretry`,
`onRowClick`, `onTabChange`) rather than `createEventDispatcher`. Callback props
are the Svelte 5 idiom, they type-check end to end, and they keep a component a
pure function of its inputs with no framework-runtime dependency — so a
component drops into a bare iframe bundle unchanged. Components own only
transient view state (a tab index, a collapse flag); all data and all async
state flow in as props, which is what lets `PageState` enforce the four-state
rule by construction. One consequence recorded for future phases: a prop must
not be named `state` (it collides with the `$state` rune) — `DataTable` takes
`pageState`.

---

## D-067 — `web/ui` is a separate npm package gated as its own `web/` project

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `web/ui/package.json`, `Makefile` (`web` / `web-install`
targets, `WEB_PROJECTS`), `.github/workflows/ci.yml`, phase plan
`phase-10a-design-system`
**Why:** `web/ui` could have been folded into `web/bridge`, but the two are
distinct artifacts with distinct consumers: `web/bridge` is the `ui/`
postMessage dialect (View-side protocol), `web/ui` is the visual component
inventory. Keeping them as separate `@dockyard/*` packages keeps each
dependency set and gate honest and lets a consumer import only what it needs —
the docs site wants `web/ui` and not the bridge. To stop the `make web` gate
from silently covering only the first project, the Makefile now carries an
explicit `WEB_PROJECTS` list and `web` / `web-install` loop over it, failing
fast if any project's gate fails; CI's npm cache keys on both lockfiles. The
rule for a future `web/` project (the multi-server console, etc.): add its
directory to `WEB_PROJECTS` in the same PR that lands it, so the frontend gate
never drifts behind the surface.

---

## D-068 — The Wave 4 wave-end E2E drives the 09→10→12 Go chain as one wired path

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `test/integration/wave4_test.go`, AGENTS.md §17 (wave-end
E2E), checkpoint PR `chore(checkpoint): wave-4`
**Why:** Wave 4 spans two kinds of artifact — a Go surface (`runtime/apps` Apps
extension, `ui://` discovery + embed pipeline, host-profile domain derivation;
phases 09, 10, 12) and a frontend surface (`web/bridge` the View-half `ui/`
dialect, `web/ui` the design system; phases 11, 10a). The wave-end E2E mandated
by §17 must drive *integrated* surface with real components, but a Go test
cannot drive a Svelte/TS library. This decision settles the split: `wave4_test.go`
drives the **Go** chain end to end — a contract-first tool linked to a `ui://`
App registered through the real Apps extension on a real `runtime/server`,
served over the SDK in-memory transport, with `.svelte` auto-discovery over the
committed convention tree, the real `//go:embed` bundle, host-profile
`_meta.ui.domain` derivation (generic verbatim + the Claude signed
`claudemcpcontent.com` origin), and the discovered wiring round-tripped through
a real `dockyard.app.yaml`. The frontend halves (`web/bridge`, `web/ui`) stay
gated by `make web` (svelte-check + vitest), exactly as D-067 keeps each `web/`
project's gate honest. The bridge's View-half contract is instead reconciled by
inspection in the checkpoint audit: its `EXTENSION_ID`, `RESOURCE_MIME_TYPE`,
and `PROTOCOL_VERSION` constants are checked against the Go `protocolcodec`
constants (`ExtensionApps`, `MIMETypeApp`, `VersionApps20260126`) — a
doc/contract check, not a cross-language test. This keeps the wave-end E2E a
real integration of the Go seams (09↔10↔12) without fabricating a brittle
Node-in-Go harness for the frontend.

---

## D-069 — The Tasks wire layer is hand-derived from the vendored schema and pinned by golden tests, not code-generated

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `internal/protocolcodec/tasks.go`,
`internal/protocolcodec/codec.go`, `internal/protocolcodec/golden_test.go`,
phase plan `phase-13-tasks-server`, master plan `docs/plans/README.md` (Phase 13
goal), `AGENTS.md` / `CLAUDE.md` §10
**Why:** The Phase 13 master-plan goal and brief 02 (§3, §5) describe the Tasks
wire layer as "code-generated from the vendored experimental schema"
(`mcp-tasks-experimental.schema.ts`). The settled Phase 02 pattern (D-010)
instead **hand-derives** the `protocolcodec` wire structs from the vendored
schema and pins them with **golden tests** that are themselves the
spec-compliance assertion — there is no `ts → Go` generator in the tree, and
the MCP Apps wire layer (also vendored, also experimental-adjacent) follows the
same hand-derived + golden pattern. Phase 13 aligns the Tasks wire layer with
that established pattern rather than introducing a one-off TypeScript-to-Go
generator for a single schema file: the cost of a bespoke generator (a JS/TS
toolchain dependency, its own drift surface) is not repaid by one ~350-line
vendored schema. The forward-compatibility guarantee is unchanged in substance —
a spec bump is still regenerate-and-diff *in spirit*: the vendored snapshot is
re-pinned by upstream SHA, the wire structs are re-derived against it, and the
golden tests surface every changed shape as a visible diff. The
`internal/protocolcodec` doc and the codec comments already frame a spec bump as
"a deliberate, reviewed update of those files followed by a regenerate-and-diff
of this package"; D-069 records that "regenerate" here means re-derive-and-
golden-diff, not run a generator. A future spec revision that materially grows
the Tasks surface may revisit this; for V1 the hand-derived layer is the
decision. This decision **supersedes** the prior wording: the master-plan
Phase 13 goal and `AGENTS.md` / `CLAUDE.md` §10 are corrected in the same PR to
say "hand-derived … pinned by golden tests" — the §15 way to override a
specified decision (a visible reconciliation, not a contradiction left to
drift).

---

## D-070 — The durable TaskStore is a typed facade over the Store seam, proven by its own conformance suite

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `runtime/tasks/storedriver.go`,
`runtime/tasks/taskstoretest/conformance.go`, `runtime/tasks/store.go`,
phase plan `phase-14-taskstore`
**Why:** Brief 02 §3 sketches the `TaskStore` as a *new* `Store`-level driver
alongside `inmem` and `sqlite`. The settled `Store` seam (D-025) is instead a
generic namespaced key-value primitive, with sub-stores (the `TaskStore`, the
future `ObsStore`) built as **thin typed facades over a `Store`** — each owning
its own forward-only migrations. Phase 14 follows D-025: the durable `TaskStore`
(`tasks.NewStore`) is a facade constructed over any `store.Store`, persisting
task rows as versioned JSON KV values in the `dockyard_tasks` namespace and
registering one forward-only migration through `store.AddMigration`. It is not a
`store.Register`'d driver. This means the durable `TaskStore` automatically
inherits every `Store` driver — the `modernc.org/sqlite` driver for durable
HTTP/Portico apps, the in-memory driver for tests — with no new CGo dependency
(`modernc.org/sqlite` is pure-Go; D-026). CLAUDE.md §9's rule that a new
persistence concern is "proven by the shared conformance suite, never bolted
onto one driver" is honoured at the sub-store layer: a dedicated **`TaskStore`
conformance suite** (`runtime/tasks/taskstoretest`) is run against every backing
— the Phase 13 in-memory stub, the durable facade over the in-memory `Store`,
and the durable facade over the `sqlite` `Store` — so the seam's guarantees
(lifecycle enforcement, auth-context-scoped listing, idempotent delete, the TTL
purge sweep) are proven uniformly. The alternative — a second `store.Register`'d
driver — would duplicate the KV machinery the `Store` seam already provides and
fork the migration runner, which is exactly the bolt-on D-025 exists to prevent.

## D-071 — Phase 14 folds in the tasks/* transport mount Phase 13 deferred

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `runtime/tasks/transport.go`, phase plan
`phase-14-taskstore`, master plan `docs/plans/README.md` (Phase 14 block)
**Why:** Phase 13 found (and recorded in its plan's Risks section) that the
go-sdk routes receiving methods through a fixed package-level dispatch table; an
unknown method — `tasks/get`, `tasks/result`, … — is rejected by the SDK before
any middleware runs, so a `tasks/*` frame never reaches `Engine.Dispatch` over a
real transport. Phase 13 shipped the engine and its transport-agnostic
`Dispatch` but **deferred the actual transport mount**, leaving it as a
documented Phase 14 seam. The Wave 5 master-plan Phase 14 block, written before
this was discovered, does not list the transport mount. Phase 14 folds it in by
an explicit, documented project decision: `runtime/tasks.Mount` routes `tasks/*`
JSON-RPC frames into `Engine.DispatchAs` ahead of the SDK server (an
`http.Handler` middleware for streamable-HTTP, a frame pump for stdio) and
injects the `capabilities.tasks` block into the `initialize` handshake response,
so a real MCP client drives `tasks/get`/`result`/`cancel`/`list` end to end over
the wire — RFC §8.2's "shim, by necessity". The mount operates at the raw
JSON-RPC frame layer (the SDK's `jsonrpc.Message` types are unexported behind
`internal/`, so interception at the SDK layer is impossible); the JSON-RPC v2
envelope types it uses are protocol-neutral, not MCP extension wire types — the
MCP Tasks wire shapes stay inside `internal/protocolcodec` (P3). The master-plan
Phase 14 block is updated in the same PR to name the transport mount in its
Goal and Acceptance — the CLAUDE.md §4.3 way to record a reasonable plan
deviation, not a silent scope change.

---

## D-072 — Wave 5 checkpoint: the Tasks transport mount injects `capabilities.tasks` into an SSE-framed initialize response, and a finished task's outcome is recorded atomically ahead of the terminal transition

**Date:** 2026-05-21
**Status:** Settled
**Where it lives:** `runtime/tasks/transport.go` (`serveInitialize`,
`mergeTasksCapabilitySSE`), `runtime/tasks/engine.go` (`finish`),
`runtime/tasks/dispatch.go` (`handleCancel`), `runtime/tasks/transport_test.go`,
`test/integration/wave5_test.go`
**Why:** The Wave 5 wave-end E2E test (`test/integration/wave5_test.go`, the
§17.5 checkpoint) drove the integrated Tasks surface against the *real*
components — a real `runtime/server` over the real go-sdk streamable-HTTP
transport, a real durable `TaskStore` over real `modernc.org/sqlite` — rather
than the stand-ins the per-phase tests used. Doing so surfaced three real
defects, fixed in the checkpoint PR per CLAUDE.md §17 ("fix anything an
integration test surfaces in the same PR, even when the root cause is an
earlier phase"):

  1. **`capabilities.tasks` was silently dropped over a real HTTP transport.**
     `Mount.serveInitialize` (Phase 14, D-071) merged the `capabilities.tasks`
     block only into a plain-`application/json` initialize response, relaying a
     `text/event-stream` (SSE) response untouched. The real go-sdk
     streamable-HTTP transport frames the initialize response as SSE — so in a
     real durable HTTP/Portico deployment the Tasks capability never reached the
     client, defeating Phase 14's "the `tasks` capability is in the
     `initialize` result" acceptance criterion. The Phase 14 unit test
     `TestMount_HTTPMiddleware_InjectsTasksCapability` passed only because it
     used a plain-JSON SDK stand-in — a mock at a seam that should have used the
     real framing. The fix: `serveInitialize` now also handles the SSE case
     (`mergeTasksCapabilitySSE` rewrites the `data:` line carrying the
     JSON-RPC initialize envelope); a new unit test covers the SSE path with
     the real go-sdk SSE shape.

  2. **`tasks/result` could observe a terminal status before the result
     payload.** `Engine.finish` wrote the terminal-status transition *before*
     the result payload, in two separate `Store` operations. A `tasks/result`
     waiter unblocks the instant it observes a terminal status, so it could
     read `completed` and an empty payload. The fix: `finish` records the
     result *before* the transition, so the payload is always present once the
     status is terminal; `handleCancel` is reordered the same way.

  3. **`tasks/cancel` raced the handler's cooperative unwind.** `handleCancel`
     cancelled the handler's run context *before* its own `→ cancelled`
     transition; a handler observing `ctx.Done()` promptly could return and
     drive `finish` to `failed`/`completed` first, leaving `tasks/cancel` with
     an illegal `failed → cancelled` transition. The fix: `handleCancel`
     transitions the task to `cancelled` (and records the cancelled outcome)
     *before* signalling the handler's context; `finish` is now a true
     cooperative no-op on an already-terminal task — it neither transitions nor
     overwrites the recorded outcome — so the handler's later unwind preserves
     the authoritative `cancelled` result (brief 02 §4.7).

This decision also records the Wave 5 E2E design choice (the §17 requirement
that a wave-end test names its informing decision, as `wave4_test.go` cites
D-068): the Wave 5 E2E drives the Tasks surface over a real streamable-HTTP
transport with the real `tasks.Mount` middleware in front of a real
`runtime/server`, a real durable `TaskStore` over real sqlite, and a real SDK
client for the handshake — no mocks at any seam — exactly so that
real-transport framing and cross-subsystem timing are exercised, which is what
surfaced defects 1–3. The audit verdict: with these three fixes the Wave 5
foundation is sound.

---

## D-073 — The Store migration registry is a caller-owned `MigrationSet`, not a process global (Wave 5 checkpoint S1 fix)

**Status:** Settled (Phase 15).

The Wave 5 checkpoint filed **S1**: the Store migration registry in
`runtime/store` was a mutable process-global (`migrations []Migration`,
`migrationIDs map[string]struct{}`), mutated by `AddMigration` and cleared by
`ResetMigrationsForTest`. A `sync.Mutex` guarded a *single* `AddMigration`
call, but the registry's *use* was a non-atomic three-step sequence —
`ResetMigrationsForTest()` → register → `Store.Migrate()` — that two
`t.Parallel()` test fixtures would interleave: one fixture's reset wiped
another's just-registered migrations, and `AddMigration`'s duplicate-ID panic
fired on timing luck alone. The Wave 5 E2E fixture (`wave5_test.go`) only
avoided the panic with an external `migrationSetupMu` mutex held across
`Migrate`; the Phase 14 fixtures reset-and-cleanup the global per test. This is
shared mutable state masquerading as a registry — the wrong shape.

**Decision.** Remove the mutable global entirely. Migrations are now carried by
an explicit, caller-owned **`store.MigrationSet`** value:

- `store.NewMigrationSet()` returns an empty set; `set.Add(Migration)` returns
  an error (no panic) on a duplicate/empty/nil-Up migration; `set.MustAdd` is
  the panic-on-error variant for a constructor assembling a fixed set;
  `set.Extend(other)` composes the sets of several sub-stores.
- `Store.Migrate` takes the set explicitly: `Migrate(ctx, *MigrationSet)`. A
  nil set is a valid no-op. `RunMigrations(ctx, Store, *MigrationSet)` is the
  shared runner every driver delegates to.
- The package globals (`migrations`, `migrationIDs`, `migrationsMu`,
  `AddMigration`, `registeredMigrations`, `ResetMigrationsForTest`) are
  **deleted**. `ErrDuplicateMigration` is now an `Add`/`Extend` return value
  (and the `MustAdd` panic value), not a global-registry panic.
- `tasks.RegisterMigrations()` (which mutated the global) is replaced by
  `tasks.Migrations()`, which returns a fresh, caller-owned
  `*store.MigrationSet` on every call. An application composes it with any
  future sub-store's set and passes the result to `Store.Migrate`.

With no shared state, two stores migrate concurrently from independent sets
with no coordination and no locking. Every migration-runner test is now
`t.Parallel()`, and `TestMigrationSet_ConcurrentMigrate` runs 32 goroutines
each building their own set and migrating their own store under `-race` — the
durable proof of the S1 fix. The `migrationSetupMu` workaround in
`wave5_test.go` and the per-test global reset/cleanup in the Phase 14 and
TaskStore-conformance fixtures are removed in the same PR; they are
unnecessary once the global is gone.

This supersedes the registration mechanism described in D-025's "registered
through `AddMigration`" prose and in the Phase 03 / Phase 14 plans — the
forward-only, append-only, fingerprinted migration *semantics* are unchanged;
only the registration surface moved from a global to an explicit value. The
Phase 03 and Phase 14 plan files' historical references to `AddMigration` /
`RegisterMigrations` are left as written (they record what those phases
shipped); this entry is the authoritative current state.

---

## D-074 — obs/v1 is an explicit, public, versioned event contract with a headless interface+factory+driver emitter seam

**Status:** Settled (Phase 15).

Phase 15 implements RFC §11.1/§11.2 — observability is a protocol. The
implementation settles several shapes the RFC and brief 05 left as sketches:

1. **The event contract is golden-pinned.** `obs.Event` carries
   `schema_version` = `"dockyard.obs/v1"`; its JSON wire shape is a public,
   third-party-consumable contract (RFC §11.3, CLAUDE.md §8) and is pinned by
   golden tests in `runtime/obs/event_test.go`. An accidental field/order
   change fails CI; a deliberate change bumps `SchemaVersion` and updates the
   golden. Event kinds: `tool.call`, `resource.read`, `prompt.get`,
   `app.load`, `app.bridge`, `app.user_action`, `host.compat`, `log`,
   `server.lifecycle`, and `task.progress` (the brief's `progress` kind is
   named `task.progress` for clarity; Tasks is V1 scope, so task events are in
   obs/v1 V1 — brief 05 Q-8 answered "yes").

2. **The emitter is an interface + factory + driver seam** (CLAUDE.md §4.4),
   matching the Store seam: `obs.Emitter` is the only interface the runtime
   depends on; drivers register a factory via `obs.RegisterDriver` in an
   `init()` block; `obs.Open(driver, cfg)` constructs by name. Phase 15 ships
   the `ringbuffer` driver; Phase 16's SSE sink and OTel adapter register
   behind the *same* seam, and the MCP `logging`→obs/v1 bridge is just another
   event source. `obs.FanOut` is the bounded multi-driver emitter.

3. **Emit is non-blocking by construction.** The ring-buffer driver is a
   bounded ring: a full buffer overwrites its oldest event (counted via
   `Dropped()`), it never stalls an emitter. `FanOut` is non-blocking provided
   each driver is — which every V1 driver is. The runtime never blocks on a
   slow consumer (CLAUDE.md §8).

4. **W3C Trace Context.** `obs.SpanContext` carries a 16-byte trace-id and
   8-byte span-id as lowercase hex (`NewTrace`, `Child`), so a Dockyard
   server's spans nest natively under a calling Harbor agent's `execute_tool`
   span and Phase 16's OTel adapter has spec-shaped IDs to export (RFC §11.2,
   brief 05 Q-4).

5. **Capture defaults to shape + size.** `obs.Shape` computes a content-free
   structural fingerprint (kind, byte size, object field *names*, array
   length) — never values. `CapturePolicyShape` (the zero value, the default)
   captures only the shape; `CapturePolicyFull` is the opt-in hook, honoured
   *only* when a redaction-aware `obs.Redactor` is supplied, otherwise it
   degrades to shape+size — full content is never the silent default
   (CLAUDE.md §7). The `Redactor` interface is defined; the concrete redaction
   pipeline is deliberately out of Phase 15 scope (Phase 16+).

6. **Headless instrumentation, no back channel (P2).** `runtime/server`
   carries an `obs.Recorder` (built from `Options.Obs`) and emits `tool.call`,
   `resource.read`, and `server.lifecycle`; `runtime/apps` emits `app.load`
   from the App resource-read handler; `runtime/tasks.Engine` emits
   `task.progress` start/end events. Every subsystem EMITS through the shared
   `obs.Emitter`; nothing reads another subsystem's internals to observe — if
   a signal is needed, an event is added (CLAUDE.md §6). The `obs/v1`
   `session_id` field and a `WithSession` context seam are defined now so
   Phase 16's transports can stamp session identity without a contract change.
