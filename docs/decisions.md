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
