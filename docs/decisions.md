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
**Status:** Settled — enforcement claim partially superseded by D-111. The
`ErrMigrationMutated` / fingerprint runtime-enforcement claim below is false (a
Go func cannot be content-hashed) and was removed in remediation R3; the
forward-only / append-only / idempotent / runner-tracked semantics and the
`ErrMigrationOutOfOrder` reorder/removal check stand. See D-111.
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
**Status:** Superseded by D-176 (2026-05-30) — `_meta.ui.domain` is now
a host-supplied verbatim value; server-side auto-derivation is retired. The
host-profile **seam** survives; only the **synthesising derivation** this entry
introduced is gone. This entry stands as the record of what was decided then.
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
**Status:** Superseded by D-176 (2026-05-30) — the synthesising Claude
host profile is **retired**. The MCP Apps spec makes `domain` a host-minted,
developer-copied verbatim value, not a framework-computed one, and the derived
origin was rejected by Claude Desktop on a local connector. `runtime/apps/hostprofile_claude.go`
is removed; `runtime/apps` ships only the generic verbatim profile behind the
retained seam. This entry stands as the record of what was decided then.
**Where it lives (historical):** RFC §7.5, `runtime/apps/hostprofile_claude.go` (removed),
phase plan `phase-12-host-profiles`
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
**Status:** Settled — the seam **contract** (`HostProfile.RequiresServerURL`)
survives D-176 for any future host-blessed signing profile, but **no
signing profile ships built-in** since D-176 retired the Claude derivation;
`runtime/apps` ships only the generic verbatim profile. The `App.ServerURL`
field is deprecated (D-176).
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

---

## D-075 — the obs/v1 SSE sink is an out-of-band, localhost-bound emitter driver

**Status:** Settled (Phase 16).

Phase 16 implements RFC §11.3's out-of-band `SSESink`. The implementation
settles its shape:

1. **Out-of-band by construction.** `obs.SSESink` owns its OWN loopback
   `net/http` listener; it holds no reference to the MCP transport and cannot
   write to `os.Stdout`/`os.Stdin`. When the MCP transport is stdio, obs/v1
   events go out the SSE channel and never onto the JSON-RPC pipe — a stdio
   server stays debuggable without protocol corruption (brief 05 §2.2, §3.3).
   The integration test proves it: every byte the server writes to the stdio
   pipe parses as a `"jsonrpc":"2.0"` message, no obs event ever leaks.

2. **Localhost-only.** `NewSSESink` rejects any non-loopback bind address
   (`errSSENonLoopback`): a wildcard host (`":0"`, `0.0.0.0`) or a routable
   address is refused; only `127.0.0.1`, `[::1]`, and `localhost` are accepted.
   The SSE sink is a dev-mode surface and is never reachable off-localhost
   (CLAUDE.md §7, P4). It is NOT an MCP client — it speaks SSE to dev tooling.

3. **Non-blocking by construction.** Each subscriber has a bounded send queue
   (`sseSubscriberBuffer`). A slow or stalled subscriber has events DROPPED for
   that subscriber — `Emit` does a non-blocking channel send per subscriber and
   never stalls the runtime's emit path (CLAUDE.md §8). Drops are counted via
   `SSESink.Dropped()`. `SSESink` registers behind the Phase 15
   `obs.RegisterDriver` seam under the driver name `sse`; its config string is
   the loopback listen address.

4. **Event framing.** Each obs/v1 event is one SSE message: the `event:` field
   carries the event kind (so a consumer can filter without parsing the body)
   and the `data:` field carries the canonical obs/v1 JSON. The endpoint is
   `/obs/v1/stream`. The framing is documented so the Wave 8 inspector consumes
   a stable surface.

---

## D-076 — the OTelEmitter is an off-by-default span adapter behind the obs seam; log events export as span events

**Status:** Settled (Phase 16).

Phase 16 implements RFC §11.3's optional `OTelEmitter` (brief 05 §3.4, Q-5
answered "V1 scope"). The implementation settles three points:

1. **Off by default.** The OTel adapter lives in its own package
   (`runtime/obs/otel`) so the OTel dependency and the still-"Development" MCP
   semantic conventions are contained — an attribute-name shift is a localized
   edit, `obs/v1` stays the stable contract (brief 05 §4 risk 1). `otelobs.New`
   with no span processor returns an emitter that discards every event; opening
   the `otel` driver by name through the `obs` seam yields an `obs.NopEmitter`,
   because the seam's string config cannot carry a live export pipeline. Local
   observation (ring buffer + SSE) therefore needs ZERO OTel configuration
   (CLAUDE.md §8); only an explicitly-supplied `sdktrace.SpanProcessor`
   activates export.

2. **W3C-derived span identity.** The adapter owns an internal `TracerProvider`
   built with a context-keyed `IDGenerator`: the obs/v1 event's own W3C
   trace-id and span-id (`obs.SpanContext`, set in Phase 15) become the exported
   OTel span's trace-id and span-id. A Dockyard span therefore nests natively
   under a calling Harbor agent's `execute_tool` span (RFC §11.2). A
   `tool.call` event maps to a `span.mcp.server`-shaped span "tools/call
   {tool}" with `mcp.method.name`, `gen_ai.tool.name`,
   `gen_ai.operation.name=execute_tool`, `mcp.session.id`, `network.transport`,
   and — on failure — `error.type`; a `resource.read` carries
   `mcp.resource.uri`.

3. **Log events as span events.** An obs/v1 `log` event has no lifecycle of its
   own. The adapter exports it as an OTel **span event** on a one-shot
   correlated span, NOT via the separate OpenTelemetry logs SDK — that SDK is
   still `v0.x`, and keeping the new dependency surface to the stable OTel
   trace SDK is deliberate. This is revisited if the OTel logs SDK reaches
   `v1`. Only the start half of a start/end pair is skipped; the end/emit event
   produces exactly one span per obs unit of work, with the correct duration.

---

## D-077 — the MCP logging capability is bridged into obs/v1, not bypassed

**Status:** Settled (Phase 16).

Phase 16 implements RFC §11.3's MCP `logging` → obs/v1 `log`-event bridge as
`server.LogBridge`. The implementation settles the bridge's shape:

1. **The bridge is an event SOURCE, not a back channel.** A Dockyard server
   still speaks STANDARD MCP `logging`: `LogBridge.Log` delivers a record as an
   MCP `notifications/message` through `ServerSession.Log` exactly as the spec
   and the go-sdk define it — a client that negotiated `logging` and called
   `SetLevel` receives `notifications/message` unchanged. The SAME record is
   ALSO emitted as an obs/v1 `log` event through the shared `obs.Recorder`.
   obs/v1 remains a one-way emitted stream; nothing reads runtime internals to
   observe (P2, CLAUDE.md §6).

2. **The session is threaded through the handler context.** A Dockyard typed
   tool handler (`func(ctx, In) (Result[Out], error)`) does not receive a raw
   SDK request, so it cannot reach the in-flight `*mcp.ServerSession`.
   `runtime/server` therefore threads the request's `ServerSession` onto the
   handler context (`withRequestSession`, applied in both `AddTool` and
   `AddToolWithSchemas`); `LogBridge.Log` resolves it from the context. The
   typed handler API never exposes a raw SDK session (P3, RFC §5.4).
   `LogBridge.LogTo` is the lower-level entry point for a caller that already
   holds a session. A record logged outside a request still emits the obs/v1
   `log` event; only the MCP `notifications/message` delivery is skipped when
   there is no client session.

3. **obs/v1 log events are independent of the client's MCP log level.** The
   MCP `notifications/message` honours the client's negotiated minimum level;
   the obs/v1 `log` event is emitted regardless, so the inspector observes
   every server log record even when the client did not raise its MCP log
   level.

---

## D-078 — Wave 6 checkpoint: the obs/v1 wave-end E2E design and two folded web-gate hygiene fixes

**Status:** Settled (Wave 6 checkpoint).

The Wave 6 checkpoint (RFC §11 — phases 15 and 16) lands its wave-end
end-to-end integration test and two folded-in `web`-gate hygiene fixes. This
entry records the design choices, the way D-072 records the Wave 5 checkpoint's.

1. **The Wave 6 E2E drives the obs/v1 protocol as one wired whole, with real
   drivers at every seam.** `test/integration/wave6_test.go` builds a real
   `runtime/server` carrying contract-first tools, a real resource and a real
   MCP App, plus a real `tasks.Engine`, all emitting through ONE real
   `obs.FanOut` composed over a real ring-buffer driver, a real out-of-band
   `SSESink` on a real loopback listener, and a real `OTelEmitter` wired to a
   REAL in-memory OTel span recorder (`tracetest.SpanRecorder`) — never a mock
   at the OTel boundary (CLAUDE.md §17). MCP calls run over a REAL stdio-shaped
   transport (newline-delimited JSON-RPC over OS pipes) so the no-corruption
   proof is genuine. It asserts every obs/v1 event kind lands with a well-formed
   W3C trace identity, the SSE channel streams while the stdio pipe stays clean,
   the OTel pipeline receives `mcp.*`/`gen_ai.*` spans, and a log record fans to
   BOTH MCP `notifications/message` and an obs/v1 `log` event; it covers a
   failure mode per seam (a stalled SSE subscriber, a slow ring consumer, and
   OTel-not-configured) and runs an N=14 concurrency stress under `-race`
   against the shared `FanOut` + SSE sink (with subscriber churn) + ring buffer,
   with a post-teardown goroutine-leak assertion.

2. **The Phase 11 smoke surfaces `make web` output on failure (C1).**
   `scripts/smoke/phase-11.sh` ran the frontend gate as `make web >/dev/null
   2>&1`, so a CI frontend-gate failure was undiagnosable. The smoke now tees
   `make web` to a temp log and prints the tail on failure, staying quiet on
   success — the `ok`/`fail` semantics are unchanged.

3. **The flaky `web/bridge` transport notification test is made deterministic
   (C2).** `transport.test.ts`'s "dispatches inbound notifications to handlers"
   waited for `MessageChannel` delivery with a single `setTimeout(…, 0)`.
   `MessageChannel` delivery is its own event-loop task, so on a loaded runner
   one `setTimeout(0)` can run ahead of the `message` event — the test then
   asserts against an empty `seen` array and times out. The fix awaits the real
   signal: the test resolves its promise from inside the `onNotification`
   handler rather than after an arbitrary timeout. No `web/bridge` runtime
   behaviour changed — this is a test-determinism fix only. The sibling negative
   assertions ("ignores reserved…", "drops non-JSON-RPC…") keep their
   `setTimeout(0)` form: a negative `not.toHaveBeenCalled()` assertion is
   already correct under either delivery order.

---

## D-079 — the obs/v1 handler-span context seam; a handler-emitted log event correlates to its tool.call

**Status:** Settled (Phase 17, folds in the Wave 6 checkpoint item S1).

The Wave 6 checkpoint filed S1: an `obs/v1` `log` event emitted from inside a
tool handler — through the MCP-logging → obs/v1 bridge (`LogBridge`, D-077) —
was not trace-correlated to its enclosing `tool.call`. `LogBridge.LogTo` minted
a fresh `obs.NewTrace()` per record, so a handler-emitted log event carried an
unrelated trace id and no parent span: an inspector could not tie a log line to
the tool call that produced it.

Phase 17 owns the fix. `runtime/obs` gains a context seam mirroring the Phase 15
`obs.WithSession` pattern (D-074):

- `obs.WithSpan(ctx, SpanContext)` stamps an in-flight span onto a context.
- `obs.SpanFromContext(ctx)` reads it back.
- `obs.ChildOrNewTrace(ctx)` is the one-call "child of the enclosing span if
  there is one, else a fresh root trace" form an emit site nested inside a
  possibly-instrumented unit of work uses.

`runtime/server`'s tool-handler edge (`AddTool` and `AddToolWithSchemas`) opens
the `tool.call` span once and threads it onto the handler context via
`obs.WithSpan` before invoking the handler. `LogBridge.LogTo` then emits its
`obs/v1` log event with `obs.ChildOrNewTrace(ctx)` instead of `obs.NewTrace()`:
inside a handler the log event is a **child** of the `tool.call` span — same
trace id, `ParentSpanID` set to the `tool.call` span id — and outside a request
(a record logged with no enclosing span) it still gets a well-formed fresh root
trace. The MCP `notifications/message` side of the bridge is unchanged.

This is purely additive to the obs/v1 contract — no event shape changed, the
correlation simply became correct. A `-race`-clean test
(`runtime/server/logbridge_trace_test.go`) drives a real `tools/call` whose
handler emits a log record and asserts the log event shares the `tool.call`'s
trace id and nests under its span.

---

## D-080 — a scaffolded project's go.mod uses a replace directive until Dockyard is published

**Status:** Settled (Phase 17).

`dockyard new` scaffolds a project that imports the Dockyard runtime library
(`github.com/hurtener/dockyard/runtime/...`). Until Dockyard is published to a
module registry with a tagged version, a scaffolded project cannot resolve that
import with `go get` — there is no published module to fetch.

The scaffold therefore supports an optional `go.mod` `replace` directive
pointing the Dockyard import at a local checkout. It is surfaced as the hidden
`dockyard new --dockyard-path <path>` flag (and the `scaffold.Options.Dockyard-
Replace` field). When set, the scaffolded `go.mod` carries
`replace github.com/hurtener/dockyard => <abs path>`; when unset, the scaffold
depends on the module version directly (the released-Dockyard workflow).

The flag is hidden because a released `dockyard` CLI will not need it — once
Dockyard ships a tagged release, a scaffolded project depends on the published
version and the replace directive disappears. The pre-release integration test
and the Phase 17 smoke script pass `--dockyard-path` pointed at the repo root so
the scaffolded project genuinely compiles against the real runtime. Revisiting
this when Dockyard first publishes a release is a Phase 30 (release) follow-up.

---

## D-081 — `dockyard generate` produces JSON Schema via an ephemeral in-project generator

**Status:** Settled (Phase 18).

The Design A schema engine — `github.com/google/jsonschema-go` — infers a
contract's JSON Schema from a `reflect.Type`. `dockyard generate` runs against a
*developer's* project, whose contract structs are compiled into a *different* Go
module than the `dockyard` binary, so the binary cannot reflect on them
directly. And `internal/codegen` is an `internal/` package, so a scaffolded
project cannot import it either.

Phase 18 resolves this with a split pipeline:

- **TypeScript** is generated in-process. `internal/codegen.TypeScriptForDir`
  works purely from Go *source text* (it parses the AST), so the `dockyard`
  binary runs it directly on the project's `internal/contracts/*.go`.
- **JSON Schema** is generated by an **ephemeral generator**. `dockyard
  generate` templates a small Go `main` into a temp directory inside the
  project, `go run`s it, then deletes it. The generated program imports the
  project's *own* contracts package and the *public* `runtime/tool` API —
  never an `internal/` package — so it compiles inside the project's module. It
  reflects on the real contract types via `tool.New[In,Out](...).Schemas()` and
  writes each schema with `runtime/tool.MarshalSchema`, the public re-export of
  the deterministic `codegen.Marshal`.

The generator is run as a package directory (`go run ./.dockyard-gen-XXX`), not
a bare file: `go run <file>` compiles the file as the pseudo-package
`command-line-arguments`, which is not rooted in the module and cannot import
the project's `internal/contracts`. Running the directory makes it a real
package inside the module, and the directory is a child of the project root —
the parent of `internal/` — so the internal-import rule is satisfied.

The cost is one `go run` per `generate`. That is acceptable for a quality verb;
`dockyard dev` (Phase 19) decides its own caching posture. The alternative —
synthesising schemas from `go/types` inside the `dockyard` binary — would
reimplement a large part of the codegen pipeline and risk a divergent schema
dialect, which RFC §6.2 explicitly forbids.

---

## D-082 — `dockyard validate` is a reusable engine with a structured `Report`

**Status:** Settled (Phase 18).

The RFC §9.4 quality gates are enforced by `dockyard validate` and, later, by
`dockyard build` (Phase 20) and `dockyard test` (Phase 21). To avoid three
copies of the gate logic, the validation engine lives in `internal/validate` as
`Run(Options) (*Report, error)` — a pure function the `dockyard validate` cobra
verb is a thin wrapper over.

`Report` is a flat list of `Diagnostic`s, each carrying a `Check` class
(manifest, schema, tool-ui-mapping, mime, spec-compliance, ui-states,
stale-codegen) and a `Severity`. `Severity` is the RFC §9.4 taxonomy made a
type: `Blocker` (a build blocker) or `Warning`. `Report.HasBlockers()` is the
single exit-code seam — `dockyard validate` exits non-zero exactly when it is
true. A quality fault is *always* a `Diagnostic`, never a returned `error`; the
returned error is reserved for "validation could not run at all" (a missing
project, an I/O fault). This lets phases 20 and 21 consume the same `Report` and
make their own gating decision from it.

The manifest check runs first and is load-bearing: a manifest that does not load
at all is a `Blocker`, and the remaining checks (which all need a coherent
loaded manifest) are skipped — there is nothing meaningful to check against.

---

## D-083 — V1 spec compliance is the mechanically-checkable subset against the vendored specs

**Status:** Settled (Phase 18).

RFC §9.4 lists "an Apps/Tasks construct that violates the vendored spec" as a
build blocker. `dockyard validate` enforces this against the vendored spec
snapshots in `docs/specifications/` only — never a live host (CLAUDE.md §11).

In V1 the mechanically-checkable subset is: the `ui://` URI grammar, the
display-mode set, single-file-bundle CSP coherence, the single MVP Apps MIME
type, Tasks TTL/concurrency-limit coherence, and the presence of the vendored
spec files themselves. Most of this is already enforced structurally by
`internal/manifest` (which validates against the same vendored specs); `validate`
surfaces those faults under its `manifest` and `spec-compliance` check classes
and adds the meta-check that the vendored specs are present at all.

Deep wire-level conformance — exercising a real Apps/Tasks handshake and
asserting the on-the-wire envelopes match the spec — is **not** in `validate`'s
scope; it is `dockyard test` (Phase 21), which runs the spec-compliance test
suite. `validate` is the fast, static gate; `test` is the behavioural one. This
split keeps `validate` cheap enough to run on every save under `dockyard dev`.

---

## D-084 — `dockyard dev` is a reusable orchestrator package, not a cobra RunE

**Status:** Settled (Phase 19).

RFC §9.2 settles that `dockyard dev` is an embedded `fsnotify` orchestrator. The
orchestration logic — the file watcher, the child-process supervisors, the
codegen-on-change step, the lifecycle teardown — lives in `internal/devloop` as
a reusable, concurrency-safe package with one public entrypoint,
`Run(ctx, Config) error`. The `dockyard dev` cobra verb (`internal/cli/dev.go`)
is a thin wrapper that resolves the project directory, builds a `Config`, and
calls `Run`.

This mirrors D-082 (`dockyard validate` is `internal/validate.Run`, the cobra
verb a wrapper): orchestration is testable and `-race`-provable as a package,
and the integration test drives the exact same `Run` the CLI does — no logic
buried in a `RunE` closure that only an end-to-end CLI invocation can reach. A
later phase that wants to embed the dev loop (a future console, a richer
`dockyard` subcommand) consumes the package, not the verb.

The child-process layer is an `interface`-free but factored seam: a generic
`supervisor` owns one child's lifecycle (start / restart / clean stop), and the
orchestrator holds a slice of supervisors. Adding the inspector to the
supervised tree later (RFC §9.2 names it; Phase 22+ builds it) is one more
supervisor, not a restructure.

---

## D-085 — Phase 19's dev tree supervises the Go server, codegen, and Vite — the inspector is deferred

**Status:** Settled (Phase 19).

RFC §9.2's prose describes the `dockyard dev` process tree as "MCP server +
Svelte dev server + inspector + codegen watcher". The inspector surface does
not exist until Phase 22+. Phase 19 therefore ships the dev orchestrator
supervising three concerns — the Go MCP server (restart on `.go` change),
in-process codegen (re-run on a contract change), and the Vite dev server
(Svelte HMR) — and **defers** inspector attachment to the phase that builds the
inspector.

This is a deliberate, non-silent scoping, not a departure from RFC §9.2: the
RFC's tree is the eventual shape, and the supervisor seam (D-084) is built so
the inspector lands as one additional supervised entry. Recording it here so a
future reader does not mistake the inspector's absence from Phase 19 for drift.

---

## D-086 — `dockyard dev` degrades gracefully for a project with no `web/` UI

**Status:** Settled (Phase 19).

`dockyard new`'s no-template path scaffolds a blank MCP server with no `web/`
UI project (RFC §4.1: a UI resource is additive, not a requirement). When
`dockyard dev` runs against such a project, it supervises only the Go server
and the codegen watcher, logs that no UI project was found, and does **not**
error.

The detection signal is a `web/package.json` — the Vite UI is a real npm
project and its `package.json` is the unambiguous root marker. A project that
gains a `web/` UI later picks up Vite supervision on the next `dockyard dev`
invocation; the dev loop does not need to be taught about the UI ahead of time.
This keeps the blank-server DX a first-class path: `dockyard new && dockyard
dev` works with zero UI ceremony.

---

## D-087 — `dockyard build` runs the cross-compile matrix sequentially and collects per-target failures

**Status:** Settled (Phase 20).

`dockyard build` cross-compiles the RFC §14 matrix — darwin/linux/windows ×
amd64/arm64. Phase 20 builds the matrix **sequentially**, one `go build`
invocation per target, and **collects** a per-target failure rather than
aborting the whole run on the first one: every target is attempted, and the
aggregate error (`errors.Join` of the failures) is returned after the matrix
has run.

Sequential, not parallel: the matrix is bounded at six triples and a build is
not the latency-critical path (`dockyard dev` is); correctness and a readable,
deterministic `dist/` tree win over a few seconds of wall-clock. Collect, not
abort: one unbuildable triple — for example a project that has added a CGo
dependency, which cannot cross-compile under `CGO_ENABLED=0` — must not hide
that the rest of the matrix is green. The release engineer (Phase 30) inherits
a build that reports the full picture in one run.

A parallel matrix is a later refinement if build latency ever becomes a
complaint; the `Build` API (a `Result` of `Artifact`s) does not change when it
does.

---

## D-088 — `dockyard install`'s boot check is a throwaway localhost spawn, not a production MCP client

**Status:** Settled (Phase 20).

`dockyard install claude|cursor` writes the host's MCP config and then verifies
the server boots. The verification — the **boot check** — spawns the freshly
built server binary exactly as the host config launches it (a local stdio
subprocess), drives one real MCP `initialize` handshake against it with a
bounded timeout, and tears the process down.

This does **not** violate P4 (server-side only; Harbor owns the MCP client).
The boot check is the same test-only, dev-mode, localhost client carve-out the
inspector occupies (CLAUDE.md §1 P4, §4.2): it is throwaway, it is bounded, it
is never a long-lived or production client, and it exists only to give the
developer a clear "the config you just wrote launches a working server" signal.
`dockyard install` itself writes a *host* config — a filesystem write — and is
not a client at all.

The per-host config-file locations (Claude Desktop's
`claude_desktop_config.json` under the per-OS application-support directory;
Cursor's `~/.cursor/mcp.json`) are kept behind a small per-host `hostProfile`
struct in `internal/installpkg`. This is a filesystem-path derivation, not a
capability matrix — CLAUDE.md §6 forbids a hardcoded *capability* matrix; a
two-entry path-derivation struct is the correct, non-sprawling shape here, and
isolating it means a host's config-location change is one localized edit.

---

## D-089 — `dockyard test` is a reusable gate engine composing the existing seams

**Status:** Settled (Phase 21).

`dockyard test` runs five test categories — `go test`, the contract-first
assertions, the fixture/golden snapshots, MCP spec compliance, and
capability-degradation tests — as one command (RFC §9.1, §9.4). The
orchestration lives in `internal/testgate` as a reusable `Run(Options)
(*Report, error)` engine; the cobra `test` command is a thin wrapper over it.

This mirrors D-082 (`dockyard validate`) and D-084 (`dockyard dev`): the gate is
testable and `-race`-provable as a package, the integration test drives the
exact same `Run` the CLI does, and a later phase that wants to embed the gate
(a CI surface, a richer subcommand) consumes the package, not the verb.

`testgate.Run` does not reimplement any check. It **composes the existing
seams**: the contract category regenerates via `internal/generate.Plan` and
diffs with `internal/codegen.CheckStale`/`CrossCheck`; the spec-compliance
category calls `internal/validate.Run` and reports its `CheckSpec` diagnostics;
the capability category resolves every App through the `runtime/apps`
host-profile registry. Each category yields a `Result` with an explicit
`Gating` flag — every V1 category is gating (RFC §9.4 build blockers), and the
flag keeps the exit-code logic open to a future informational category.

The capability-degradation category never consults a per-host capability matrix
(CLAUDE.md §6): it exercises the project across the *registered host profiles*
(the interface+factory+driver seam) and the Apps-negotiated/not-negotiated
axis, asserting a UI-bearing tool always has a model-facing fallback.

---

## D-090 — The scaffolded server selects its transport from `DOCKYARD_TRANSPORT` — the Phase 20↔17 wiring-gap fix

**Status:** Settled (Phase 21).

Phase 17's scaffold generated a `main.go` that called `srv.ServeStdio`
unconditionally. Phase 20's `dockyard run --transport http` launches the
scaffolded server child and needs a way to tell it which transport to bring up
— but a stdio-only `main.go` would ignore that instruction, so
`dockyard run --transport http` would silently serve stdio. That is a real
wiring gap between the two phases.

**Decision.** The scaffolded server's `main.go` reads the `DOCKYARD_TRANSPORT`
environment variable and serves the selected transport: `stdio` (the default
when the variable is unset — the local single-user mode) or `http` (the
streamable-HTTP service mode, served via `runtime/server.HTTPHandler` with the
secure-by-default HTTP posture). An unrecognised value is a clean, explained
failure, never a silent fallback. The HTTP listen address defaults to
`127.0.0.1:8080` and is overridable with `DOCKYARD_HTTP_ADDR`.

`DOCKYARD_TRANSPORT` is the **contract** Phase 20's `dockyard run` honours: its
`run --transport <t>` sets `DOCKYARD_TRANSPORT=<t>` and `DOCKYARD_HTTP_ADDR` on
the server child, and the scaffold reads both. Phase 21 owns this fix — folded
in deliberately because it is self-contained in the Phase 17 scaffold and the
environment-variable contract. This branch was authored before Phase 20 landed
and was later rebased onto it; the contract was then verified directly against
`internal/runpkg`. One mismatch was found and fixed in the same change:
`runpkg`'s `defaultHTTPAddr` was `:8080` (all interfaces), which would have
silently widened the scaffold's secure `127.0.0.1:8080` localhost default for a
no-`--addr` HTTP run — `runpkg` now defaults to `127.0.0.1:8080`, matching the
scaffold (CLAUDE.md §17 cross-phase fix). A scaffold integration test builds the
generated server and proves it completes a real MCP initialize over HTTP under
`DOCKYARD_TRANSPORT=http`, closing the seam end to end.

---

## D-091 — Wave 7 checkpoint: the CLI wave-end E2E design and the folded hygiene fixes

**Status:** Settled (Wave 7 checkpoint).

The Wave 7 checkpoint (RFC §9, §10, §14 — phases 17-21) lands its wave-end
end-to-end integration test and the hygiene fixes the §17.5 audit surfaced.
This entry records the design choices, the way D-078 records the Wave 6
checkpoint's.

1. **The Wave 7 E2E drives the `dockyard` CLI as one wired tool, against the
   real built binary.** `test/integration/wave7_test.go` compiles the actual
   `dockyard` binary from `cmd/dockyard` (CGo-free) and exercises the whole
   developer workflow — `new` → `generate` → `validate` → `build` → `run` →
   `test` — by invoking that binary as a subprocess, exactly as a developer
   runs it. No verb is reached through an in-process package shortcut: every
   stage goes through the real cobra root, so the command-tree composition and
   each verb's wiring are genuinely tested. It asserts the scaffold builds, that
   `generate` is idempotent (a clean rerun reports "no changes"), that
   `validate` exits 0 clean / non-zero on an injected stale-codegen drift, that
   `build` emits a CGo-free statically-linked binary, that `run --transport
   http` serves a real MCP `initialize` over streamable-HTTP on a localhost
   port, and that `test` runs every category and a contract regression fails
   the gate. It covers ≥1 failure mode per seam — a `validate` stale-codegen
   blocker, a `build` blocked by that validation, a `test` gate failed by a
   contract regression, and an `install` against an unwritable host-config path
   — and runs under `-race` with a post-teardown goroutine-leak assertion after
   the spawned `dockyard run` child is torn down.

2. **The dev-loop concurrency proof drives the real `devloop.Run`
   orchestrator.** Wave 7's one reusable concurrent artifact is the
   `internal/devloop` orchestrator. The wave7 test drives the REAL
   `devloop.Run` against a real scaffolded project and stresses its supervised
   restart path with N≥12 concurrent `.go` edits from N goroutines under
   `-race`, asserting the loop tears down with no goroutine leak. The
   supervisor's own lock-level race safety stays covered in-package by
   `internal/devloop`'s `TestSupervisorConcurrentRestart`; the wave7 test does
   not add an exported test-only surface to `devloop` to reach the unexported
   supervisor directly — it exercises the same artifact through the genuine
   orchestrator + watcher seam instead.

3. **Two flaky devloop tests were de-flaked — folded fixes (§17).** The audit
   reproduced `TestSupervisorReportsCrash` and `TestRunSupervisesViteWhenWebPresent`
   failing only under the saturated `go test -race ./...` matrix, never in
   isolation: both bounded a child process's spawn/exit/observe sequence with a
   timeout (3s and 5s) too tight for a CPU-saturated `-race` run with many
   packages compiling in parallel. Both waits already return the instant their
   observable signal fires, so the ceilings were raised (to 10s and 15s) with
   no cost on a healthy run; a genuine never-fires bug still fails, just later.
   A flaky test is a defect (§17), so this was fixed in the checkpoint PR.

4. **Shipped-phase status hygiene was corrected.** The phase index in
   `docs/plans/README.md` still listed phases 18, 19 and 20 as `Pending` though
   their code, smoke scripts and integration tests had all landed; they are now
   `Shipped`. The phase plans for 18, 19 and 21 had left their acceptance-
   criteria and pre-merge-checklist boxes unchecked though the work was done;
   they are now checked, matching the convention phases 17 and 20 already
   followed. These are documentation-drift defects of the same class as RFC
   drift and were fixed in the checkpoint PR.

**Verdict.** The Wave 7 `dockyard` CLI foundation is sound: all eight verbs
compose onto one cobra root, the `new` → `generate` → `validate` → `build` →
`run` → `test` workflow holds end to end against the real binary, the
`DOCKYARD_TRANSPORT` scaffold↔`run` contract (D-090) is verified localhost-
bound, and P1 (contract-first) and P4 (server-side only — the install boot
check is the throwaway localhost carve-out of D-088) are upheld. No blocker was
found; the audit punch-list is should-fix and nit hygiene, fixed in the
checkpoint PR.

---

## D-092 — Coverage bands become a mechanical CI gate (`internal/coveragecheck`)

**Status:** Settled (Phase 21.5).

The AGENTS.md §11 coverage bands — 80% new packages, 85% the Store drivers and
conformance-tested subsystems, 70% CLI / tooling — were, until Phase 21.5,
enforced only by reviewer diligence: `make test` ran `go test -race ./...` with
no `-coverprofile` and no threshold. Phase 21.5 makes the bands mechanical, the
way RFC §9.4 makes the quality bar a toolchain gate rather than documentation.

1. **`internal/coveragecheck` is the gate.** It parses a Go coverage profile,
   aggregates per-package statement coverage, and compares each package to a
   required threshold. It exits non-zero on a shortfall, and also on a *measured
   package with no config entry* — a new package must be gated deliberately,
   never silently ungated. It is wired into `make coverage` and the CI `go` job.

2. **The threshold config is `internal/coveragecheck/coverage.json`** — a
   per-package map keyed to the §11 bands. Thresholds are set **at the band**;
   every package currently measures above its band with margin.

3. **Two override classes, each carrying a documented reason in the config:**
   - `subprocess-override` (70%) — `internal/buildpkg` / `runpkg` / `installpkg`
     orchestrate subprocesses with toolchain-failure / signal-race branches a
     hermetic suite cannot drive (the Phase 20 finding). Held to the CLI-tooling
     band, not the new-package band.
   - `harness-override` (65%) — `runtime/store/storetest` and
     `runtime/tasks/taskstoretest` are conformance *harness* packages; their
     statements are exercised when a driver runs the suite, so self-coverage
     sits below every product band by construction.
   An override is never a silent lowering of a band: the class and its reason
   travel in the config and are reviewable.

4. **Informing source.** Phase 21.5 is hygiene hardening, not an RFC-specified
   subsystem. Its informing source is the §11 bands plus an independent test-
   quality audit; RFC §9.4 is the closest anchor (the "toolchain, not
   documentation" principle). Recorded honestly here rather than retro-fitting
   an RFC section.

---

## D-093 — The "no web UI tests" audit finding was wrong; only frontend coverage thresholds are added

**Status:** Settled (Phase 21.5).

The test-quality audit claimed Dockyard had "no web UI tests." It was
investigated and **rejected**: `web/ui/src/__tests__/` (5 files) and
`web/bridge/src/__tests__/` (6 files) hold ~94 Vitest tests, run by `make web`
(`npm run gate`). The audit had grepped for Go `_test.go` files and missed the
Vitest suites.

Phase 21.5 therefore does **not** add web tests. It adds frontend coverage
*enforcement*: `web/ui` and `web/bridge` already carried `coverage.thresholds`
in their `vitest.config.ts` and a `coverage` script, but the `gate` script ran
plain `vitest run`. Phase 21.5 changes each `gate` to `check && coverage`, so
`make web` now runs `vitest run --coverage` and a frontend coverage regression
fails the gate — the frontend half of D-092's mechanical coverage gate. A wrong
audit finding is recorded as a decision so it is not re-litigated.

---

## D-094 — `codegen.TypeScriptForSource` recovers a tygo panic on malformed input

**Status:** Settled (Phase 21.5).

The Phase 21.5 `FuzzTypeScriptForSource` fuzz target found a genuine bug: a Go
contract source fragment with a syntactically invalid struct tag (e.g. a bare
backtick-quoted tag value, not a `key:"value"` pair) drove the `tygo`
dependency to **panic** ("bad syntax for struct tag pair") rather than return
an error — `tygo` parses tags via `reflect.StructTag`, which panics on a
malformed pair.

A panic across the `dockyard generate` process boundary violates CLAUDE.md §13.
The fix (CLAUDE.md §17 — fix the bug the test surfaced, in the same PR) wraps
the `tygo.ConvertGoToTypescript` call in a `recover` guard that converts the
panic into an ordinary `ErrTypeScriptGen`-wrapped error, so a malformed
contract file fails the codegen step cleanly. The recover is at one well-
defined seam guarding a third-party dependency — it is not panic-for-control-
flow. The fuzzer's minimised crasher is committed as a regression seed under
`internal/codegen/testdata/fuzz/`, and `TestTypeScriptForSource_MalformedStructTagNoPanic`
locks the fix.

---

## D-095 — The shared Store benchmark suite lives in `storetest`, exercised by a self-guard

**Status:** Settled (Phase 21.5).

Phase 21.5 adds a shared Store benchmark suite, `storetest.RunBenchmarks`,
mirroring `RunConformance`: one suite, every driver (`inmem`, `sqlite`) runs it.
Like `RunConformance`, it must be in a regular `.go` file (`benchmark.go`), not
a `_test.go` file, so the driver packages can import the exported entry point —
a `_test.go` file is visible only to its own package's tests.

A consequence: `benchmark.go`'s statements count toward the `storetest`
package's coverage total, but `go test` does not run benchmarks by default, so
they would read as uncovered and drag the harness package below even its
override threshold. Rather than lower the threshold to hide un-run code,
`storetest_test.go` gains `TestRunBenchmarksSmoke`, which drives the benchmark
suite via `testing.Benchmark` inside an ordinary test. The benchmark code is
then genuinely covered, and a silently-broken benchmark — a panicking seed, a
misnamed namespace — is caught by the normal `go test` run, not only by
`make bench`. It mirrors the conformance harness self-guard.

---

## D-096 — The inspector is a localhost HTTP backend serving an embedded frontend

**Status:** Settled (Phase 22).

The inspector (RFC §12) is split into a Go backend (`internal/inspector`) and a
Svelte frontend (`web/inspector`). The backend is a localhost HTTP server: it
serves the built frontend bundle and relays the obs/v1 stream and a JSON-RPC log
to it, read-only. The frontend talks only to this backend over its read-only
HTTP API — never directly to the MCP server. This keeps the one client-shaped
surface (P4) narrow and auditable: there is exactly one localhost listener, it
is loopback-bound by an explicit typed check (`ErrNonLoopbackBind`) that runs
before the listener opens, and it exposes no mutating route. A non-loopback bind
is never served — the mechanical enforcement of RFC §12 and the
CVE-2025-49596 lesson. `internal/inspector` mirrors `runtime/obs`'s SSE-sink
loopback gate verbatim rather than inventing a second policy.

---

## D-097 — The `ui/` host half reuses `web/bridge`'s protocol contract verbatim

**Status:** Settled (Phase 22).

`web/bridge` ships the *View* half of the `ui/` postMessage dialect; Phase 22's
`web/inspector/src/host/host-bridge.ts` ships the *host* half. The host half
imports every protocol constant and wire type — `ViewMethod`, `HostNotification`,
`ViewNotification`, `InitializeParams`, `InitializeResult`, the JSON-RPC type
guards — from `@dockyard/bridge` rather than redefining them. The `ui/` dialect
therefore lives in exactly one place (`web/bridge/src/protocol.ts`); a spec
revision is a single reviewable diff, and the host and View halves cannot drift
(P3). To make this possible, `@dockyard/bridge`'s barrel additionally re-exports
the JSON-RPC envelope types and the `isJsonRpc*` guards — a widening of the
barrel, not a new contract.

A consequence settled here: the `ui/initialize` handshake's
`ui/notifications/initialized` is a **host→View** notification only. The host's
side of the handshake is complete the moment it has *answered* `ui/initialize`
and *sent* `ui/notifications/initialized`; there is no inbound View
`initialized` for the host to wait on. `HostBridge.ready()` resolves at that
point.

---

## D-098 — The inspector frontend is a Vite application; Events + RPC are the Phase 22 core

**Status:** Settled (Phase 22).

`web/inspector` is a plain-Svelte Vite *application* (it builds to `dist/` and
is embedded into the Go backend), unlike `web/ui` / `web/bridge` which are
libraries. It joins the `make web` `WEB_PROJECTS` set and the Phase 21.5
coverage gate with a 70% frontend threshold — the band `web/ui` / `web/bridge`
use. Phase 22 builds the inspector *core*: the `AppShell` layout, the App
preview frame with the host-half bridge, and the DetailRail's **Events** and
**RPC** panels as the working tabs. The Fixtures / Tools / Verdicts / Tasks
tabs are scaffolded as explicit "coming in Phase 23" placeholders, and the
Host/Display-mode controls are shown read-only — the rail's tab structure and
the App-frame's bridge wiring are the clean seams Phase 23 extends. A committed
placeholder bundle (`internal/inspector/dist/`) keeps the `//go:embed`
directive resolvable before any frontend build; wiring the production
`web/inspector` build into the binary is the Phase 23 `dockyard inspect`
packaging step.

---

## D-099 — `dockyard inspect` attaches a read-only obs relay, not an MCP client

**Status:** Settled (Phase 23).

`dockyard inspect --url <server>` attaches the inspector to a running MCP
server by pointing the inspector's read-only obs/v1 relay at the server's
`/obs/v1/stream` endpoint — it deliberately does **not** open an MCP client
session. This keeps P4 intact: Dockyard ships no production MCP client, and the
inspector — the lone client-shaped surface — stays a localhost-bound, read-only
relay, never an arbitrary-execution proxy. `--url` derives the obs stream URL
from the server base URL (the canonical `/obs/v1/stream` path is appended); a
non-http(s) or hostless URL is a typed error. `--port` selects the inspector's
own loopback port (host is always `127.0.0.1`; a non-loopback bind is refused by
Phase 22's `ErrNonLoopbackBind` before the listener opens). `--no-open`
suppresses the browser-open for CI. The Tools/Resources panel's invoke path is
fixture-backed in the standalone case — a `tools/call` from a previewed App is
answered from the active fixture, never proxied to live execution.

---

## D-100 — Capability-set emulation is a capability toggle set, never a host matrix

**Status:** Settled (Phase 23).

The inspector's Host control emulates a host's capability set as a set of
capability **toggles** — Apps on/off, Tasks on/off, which display modes the
host grants — held in a plain `CapabilitySet` of boolean/value fields. Flipping
a toggle re-derives the `hostContext` + `hostCapabilities` the host-half bridge
advertises in the `ui/initialize` handshake, and the App re-runs the handshake
and degrades for real. There is **no hardcoded per-host capability matrix**
(CLAUDE.md §6 / §13 — a host matrix always drifts). The named presets
(`Fully capable`, `Apps only`, `Inline only`, `No Apps extension`) are
convenience starting toggle-sets only: selecting one seeds the toggles and is
never consulted at handshake time — adding or removing a preset changes no
negotiation logic. The fixture switcher closes Phase 22's `tools/call`
not-wired seam: `HostBridge.setCallToolResponder` lets the active fixture
answer a `tools/call`, and a successful fixture resolves the call with
synthetic `structuredContent` derived from the tool's generated output
contract (P1), an error fixture rejects it — so an App's six UI states are
exercised without a backend.

---

## D-101 — The inspector auto-attach inside `dockyard dev` is a deferred seam

**Status:** Settled (Phase 23).

RFC §12 names two inspector entry points: `dockyard inspect` (standalone) and
automatic operation inside `dockyard dev`. Phase 23 ships `dockyard inspect`
and **defers** embedding the inspector backend into the `dockyard dev`
supervisor. The `internal/devloop` supervisor is a self-contained process-tree
orchestrator (the Go server child, the Vite child, the fsnotify watcher);
embedding the inspector HTTP backend into that process is a devloop change with
its own lifecycle and teardown concerns, not an inspector change, and folding
it in alongside Phase 23's substantial inspector-advanced surface would widen
the phase's risk. The auto-attach is a clean follow-up seam: a developer
already gets the full inspector against a `dockyard dev` server by running
`dockyard inspect --url` against that server's HTTP transport. This is a
deliberate, documented deviation from building the `dockyard dev` integration
in this phase; the master plan's Phase 23 acceptance criteria
(`dockyard inspect` attaches to any running server) are met.

---

## D-102 — The Wave 8 E2E goroutine-leak baseline is read after fixture setup

**Status:** Settled (Wave 8 checkpoint).

`test/integration/wave8_test.go` drives the integrated inspector with a
long-lived in-process fixture — a real `runtime/server`, a real `runtime/obs`
SSE sink, a live MCP client session, and a `runtime/tasks` engine — that stays
up until the test's `t.Cleanup` runs, *after* the test body's
goroutine-leak assertion. Reading the leak baseline at the top of the test (as
the Wave 7 CLI E2E does, where the artifact under test is a *subprocess* and the
in-process fixture is minimal) would therefore count every fixture goroutine —
the sink's HTTP server, the server's session pump — as a leak.

Wave 8's leak baseline is instead read **after** `wave8Server` has brought the
fixture up and `stableGoroutineCount` reports it quiescent. The assertion then
targets exactly the artifact under test — the `internal/inspector` backend, its
relay's SSE-client loop, and the per-UI-client HTTP goroutines — which is the
Wave 8 reusable concurrent artifact the checkpoint must prove tears down clean.
It continues to use `wave1_test.go`'s robust poll-until-settled
`assertNoGoroutineLeak` (the Wave 7 checkpoint's fix), never a one-shot
snapshot. This is an E2E-test-design choice, not a runtime change; the relay's
own teardown contract is unchanged.

---

## D-103 — The inspector renders Apps via a read-only `resources/read`; supersedes D-099's "no MCP client"

**Status:** Settled (remediation R1).

D-099 settled that `dockyard inspect --url` attaches "a read-only obs relay,
not an MCP client" — and scoped the inspector too narrowly. RFC §12 line 711 is
binding: the inspector "implements the host half of the `ui/` postMessage
bridge to **render Apps locally**." Rendering an App requires obtaining the
App's HTML, and the App's HTML is a `ui://` resource of the attached MCP server.
The pre-Wave-9 depth audit found the consequence: the `AppFrame` → `HostBridge`
→ fixture-switcher chain was unreachable from the shipping `dockyard inspect`,
because nothing ever fetched an App.

This decision extends D-099. The inspector additionally performs two
**read-only** MCP client operations against the attached server, only to render
its Apps: a `resources/list` to find the server's `ui://` resources, and a
`resources/read` of each. `internal/inspector.AppsFromServer` opens a fresh,
short-lived MCP client session per `GET /api/apps` request, reads every `ui://`
resource, and closes the session — it holds no long-lived client. It never
issues a mutating MCP call (`tools/call` against the previewed App stays
fixture-backed — D-100), the inspector's listener stays loopback-bound by the
`ErrNonLoopbackBind` gate, and it is still dev-mode-gated. P4 is intact: a
read-only resource read by the lone client-shaped dev surface is not "a
production MCP client" and not "an arbitrary-execution proxy." `internal/
inspector` may use the SDK client internally exactly as `runtime/server` uses
the SDK server — no raw SDK type leaks through `AppSource` / `AppPreview` (P3).

The `obs/v1` relay path of D-099 is unchanged: a server's obs stream is still a
pure SSE consume, never an MCP session. The two paths are distinct — the relay
URL appends `/obs/v1/stream`; the App source connects the bare MCP base URL.

---

## D-104 — `dockyard inspect` sources verdicts and contracts from a project directory

**Status:** Settled (remediation R1).

The inspector's `Options.Verdicts` and `Options.Contracts` were built and
test-covered in Phase 22/23, but the shipping `dockyard inspect` command's
`runInspect` never set them — so `/api/verdicts` and `/api/contracts` always
returned `[]` in the product, leaving the Verdicts panel and the Fixtures
switcher permanently empty (the pre-Wave-9 depth audit's Blocker 1).

`dockyard inspect` now resolves a project directory — a new `--dir` flag
defaulting to the current working directory, the same `resolveProjectDir` seam
`dockyard generate` / `validate` / `test` already use — and wires
`Options.Verdicts` from `inspector.VerdictsFromValidate(dir)` and
`Options.Contracts` from `inspector.ContractsFromProject(dir)`.
`ContractsFromProject` loads the project's `dockyard.app.yaml` for the tool
list and reads each tool's generated `<tool>_<side>.schema.json` from
`internal/contracts/` — the files `dockyard generate` writes (P1: the fixtures
derive from the generated schema, never a hand-written one). Both sources
degrade gracefully: a `--dir` that names no Dockyard project, or a project
never `dockyard generate`d, yields the panels' honest four-state empty state,
never a crash — so `dockyard inspect --url <remote>` with no local project
still works.

---

## D-105, D-106, D-107 — unused reserved range (parallel pre-Wave-9 remediation allocation)

**Status:** Reserved, never used; superseded by D-108 and onward.

The pre-Wave-9 depth audit produced multiple remediations (R1–R3) authored in
parallel against the same `docs/decisions.md` log. R1 was allocated D-103/D-104
and merged first; R2 was pre-allocated the next ascending block but landed only
its actual architectural decision under D-108 (the `Options.Tasks` engine
attachment) — its other R2 surface (the stdio mount's pipe-pair plumbing, the
HTTP middleware ordering correction) was settled in the same wave under D-109
and D-110. The D-105/D-106/D-107 ordinals were the worktree-coordination
buffer between the two parallel branches and were never claimed by a real
decision before R3 advanced the counter; rather than rewrite or backfill them
they are recorded here so the gap is not mistaken for lost history. A future
decision picks up at the next free ordinal (post-D-118 at the time of this
note); the unused range is permanently retired (remediation R4 S3).

---

## D-108 — The Tasks transport mount is joined to `runtime/server` via an `Options.Tasks` engine attachment

**Status:** Settled (remediation R2 — pre-Wave-9 depth audit).

A depth audit found a wiring gap: the MCP Tasks transport mount
(`runtime/tasks.Mount` — `NewMount`, `HTTPMiddleware`, `ServeStdioFrames`)
shipped in Phase 14 and was tested in isolation, but nothing joined it to the
real server transport. `runtime/server` carried zero `tasks` references;
`cmd/`, `internal/cli`, and `internal/runpkg` never constructed a
`tasks.Mount`. So a real MCP client could not drive `tasks/*` over a real
Dockyard server — the Phase 14 acceptance criterion "a real MCP client drives
`tasks/*` over a real transport" was met only by an integration test using a
hand-written `sdkStandIn` HTTP handler, not by the product.

R2 closes the seam with an engine-attachment option on `runtime/server`,
matching the existing server-options idiom (the `obs` emitter attaches via
`Options.Obs`): `Options.Tasks *tasks.Engine` (plus an imperative
`WithTasks(engine, authContext)` for a caller that builds the engine after the
server) attaches a Tasks engine; `New`/`WithTasks` build the `tasks.Mount`
once and store it unexported on the `Server`. When a Tasks engine is attached
the streamable-HTTP handler is wrapped with `Mount.HTTPMiddleware` and the
stdio transport path runs the mount's frame pump (D-109); when no engine is
attached the server is byte-for-byte a plain MCP server with no `tasks/*`
interception and no added overhead. `Options.TasksAuthContext` carries the
HTTP requestor-identity seam (RFC §8.5); `TasksEnabled()` exposes the wiring
for a transport entrypoint and a smoke check without reaching into server
internals. The mount itself is unchanged — R2 *connects* the existing shim
(RFC §8.2), it does not reinvent it. P3 is preserved: `runtime/server` depends
only on the `tasks` package's exported `Engine`/`Mount` surface; no raw MCP
extension wire type crosses out of `internal/protocolcodec`.

**Scope of R2 and an owned follow-up.** R2's non-negotiable deliverable — a
real MCP client genuinely drives `tasks/*` over a real `runtime/server`
transport — is met: the `runtime/server` seam is the core fix, proven by
`test/integration/r2_tasks_mount_test.go`. R2 deliberately does **not** make
`dockyard run` / the scaffolded `main.go` *construct and attach* a
`tasks.Engine` automatically: the scaffold currently declares its example tool
`task_support: forbidden`, builds no engine, and constructs no `Store` in
`main.go`; auto-attaching one needs per-tool task-support detection from the
manifest, engine + `Store` construction in the generated entrypoint, and an
identifiability decision per transport — a substantial scaffold/CLI change with
its own surface and tests, larger than the seam fix. That work is a recorded,
owned follow-up: **a later CLI/scaffold phase wires `dockyard run` and the
scaffolded `main.go` to attach a `tasks.Engine` when the project declares
task-supporting tools.** Until then, an app author reaches the wiring directly
through `server.Options.Tasks` / `Server.WithTasks` in their own `main.go`.

---

## D-109 — The stdio Tasks mount runs the SDK server on a forwarded in-process pipe pair

**Status:** Settled (remediation R2).

The streamable-HTTP transport has a natural middleware seam — `HTTPHandler`
wraps the SDK handler with `Mount.HTTPMiddleware`. The stdio transport has
none: the go-sdk's `StdioTransport` reads `os.Stdin` and writes `os.Stdout`
itself, and the go-sdk rejects an unknown JSON-RPC method (every `tasks/*`
method) before any interception point. So `Server.ServeStdio`, when a Tasks
engine is attached, runs `serveStdioWithTasks`: the SDK server is run on an
`mcp.IOTransport` over an in-process pipe pair, and the mount's
`ServeStdioFrames` pump owns the real `os.Stdin`/`os.Stdout`. A `tasks/*`
request frame on real stdin is answered by the engine and written to real
stdout; every other frame is forwarded into the SDK's input pipe, and a
dedicated copy goroutine relays the SDK's output pipe verbatim to real stdout —
so SDK-initiated frames (notifications, server→client requests) are never
dropped. The mount pump's `forward` callback therefore writes the frame and
returns `nil` (no synchronous response); the response arrives asynchronously on
the SDK output pipe. Teardown closes the pipes and joins both goroutines;
pipe-closed / EOF / context-cancelled errors from the SDK serve and the pump
are recognised as benign shutdown, not faults. With no Tasks engine attached,
`ServeStdio` is unchanged — the SDK serves real stdio directly.

---

## D-110 — The HTTP Tasks mount sits inside the explicit HTTPSecurity boundary

**Status:** Settled (remediation R2).

When `HTTPHandler` wraps the SDK handler with `Mount.HTTPMiddleware`, the
ordering is deliberate: the mount is wrapped *inside* the explicit HTTPSecurity
middleware chain. After R3's Content-Type check landed in the same chain
(D-112), the final handler stack is
`CrossOriginProtection( ContentType( Mount( SDKHandler ) ) )`. A `tasks/*` POST
is therefore subject to the same explicit `HTTPSecurity` posture (DNS-rebinding
protection, cross-origin/CSRF protection — D-040, D-041; Content-Type
verification — D-112) as every other request before the mount ever inspects it; the mount cannot bypass or weaken the security posture
(CLAUDE.md §7 — the HTTP posture is set explicitly, never inherited or
sidestepped). This corrected a latent ordering bug found while wiring R2: the
cross-origin middleware previously wrapped the raw SDK handler
(`cop.Handler(handler)`); once the mount was inserted, that would have left the
mount *outside* the CSRF boundary. `HTTPHandler` now wraps the composed handler
(`cop.Handler(h)`), keeping security outermost. A `tasks/*` cross-site POST is
rejected with 403 exactly as a `tools/call` cross-site POST is — proven by
`runtime/server` `TestTasksMount_HTTP_SecurityStillEnforced`.

---

## D-111 — "no migration edits after merge" is a review-enforced rule, not a runtime-enforced one (supersedes D-027's enforcement claim)

**Status:** Settled (depth-audit remediation R3).

**Supersedes:** the enforcement claim of D-027.

D-027 stated the migration runner detects "a recorded migration whose
fingerprint diverges (`ErrMigrationMutated` — a migration edited after merge)".
The pre-Wave-9 depth audit found that claim false. A `store.Migration`'s effect
is a Go func (`Up`); `Migration.fingerprint` hashed only
`fmt.Sprintf("%d\x00%s", ordinal, m.ID)` — the ordinal and ID — because a Go
func value cannot be content-hashed (the language offers no stable hash of a
closure body, and closures over different source share an entry point). Editing
a migration's `Up` body in place — same `ID`, same ordinal — therefore produced
an identical fingerprint, so the `ErrMigrationMutated` branch could never fire
for that case. It was unreachable dead code, and the runner's `rec.Ordinal !=
ordinal` check already covered the only thing the fingerprint could detect
(reorder).

R3 takes the honest option: `ErrMigrationMutated`, the `Migration.fingerprint`
method, and the `appliedRecord.Fingerprint` field are removed; the
`appliedRecord` now stores only the ordinal. The runner still enforces, at
runtime, that the registered sequence extends the applied sequence as a prefix
and that no applied migration was reordered or removed
(`ErrMigrationOutOfOrder`). The rule "never edit a migration's `Up` body after
it merges" (CLAUDE.md §9) stands — but it is enforced by **code review and the
CI diff**, not by the runtime, and the code (`Migration`/`RunMigrations`/
`errors.go` docstrings) now says so plainly. CLAUDE.md §9's wording ("never edit
a migration after it merges") was already a statement of the rule, not a claim
of runtime enforcement, so it is left unchanged.

The preferred option — a genuine content hash — was considered and rejected: it
would require every `Migration` author to supply a hashable representation of
the migration's effect (declared SQL text or an author-set checksum), which the
V1 pure-Go-func `Migration` shape does not carry. Adding that is a larger design
change than a remediation should make unilaterally; if a future phase wants
runtime enforcement it must first give `Migration` a hashable effect
representation (an RFC change), at which point this decision is revisited.

---

## D-112 — HTTP Content-Type verification is an explicit field of the Dockyard HTTPSecurity posture

**Status:** Settled (depth-audit remediation R3).

CLAUDE.md §7 requires the HTTP transport's DNS-rebinding, Origin/Content-Type,
and cross-origin protections to be set **explicitly** by Dockyard, "never
inherited from an SDK default" — SDK security defaults have flipped between
releases. The depth audit found `HTTPSecurity` set DNS-rebinding and
cross-origin/Origin protection explicitly but addressed **Content-Type
verification nowhere** — it was left to whatever the linked go-sdk does.

R3 adds `HTTPSecurity.ContentTypeVerification`, on in `DefaultHTTPSecurity`.
When set, `HTTPHandler` wraps the handler in Dockyard's own
`contentTypeMiddleware`, which rejects a POST whose request-body `Content-Type`
is not the JSON media type the MCP streamable-HTTP transport mandates
(`application/json`, charset parameter tolerated) with `415 Unsupported Media
Type`. GET (the SSE stream) and DELETE (session teardown) carry no body and are
passed through. The check is Dockyard's own posture — verified by a behavioural
test that asserts on the middleware's distinct rejection body — so it holds
regardless of what the linked SDK defaults to. It is opt-out via an explicit
non-zero `HTTPSecurity` with the flag off.

---

## D-113 — `dockyard validate` runs codegen.CrossCheck so the standalone and `dockyard test` paths catch schema↔TS desync; `dockyard build` defends via regeneration

**Status:** Settled (depth-audit remediation R3; wording clarified by R4 N3).

P1 (contract-first) makes a desync between the two independently-generated
Design-A artifacts — the JSON Schema and the TypeScript — a hard failure;
`codegen.CrossCheck` (RFC §6.2) is the check built for it. The depth audit found
`CrossCheck` was wired only into `dockyard test` (`internal/testgate`), not into
`internal/validate`: `validate`'s `checkStaleCodegen` only byte-compared each
artifact against a fresh regeneration. Yet `internal/validate/doc.go` claimed
artifacts are "cross-checked for schema↔TS drift", and `dockyard build` runs
`validate` — so a committed, internally-inconsistent schema/TS pair passed
`validate` (and read past the doc comment as passing `build`).

R3 adds `checkCrossCodegen` to `internal/validate`: it reads the committed
schema files and `contracts.ts` from disk (deliberately NOT a regeneration —
the point is to gate what is checked in) and runs `codegen.CrossCheck` per tool
side. A desync is reported under the existing `CheckStaleCodegen` class as a
Blocker — the same build-blocker class as stale codegen, consistent with P1.

**Which entry point catches which class of drift (R4 N3 clarification).**
The defense varies by entry point, and the original D-113 wording overpromised
the `build` path:

- `dockyard validate` (standalone) and `dockyard test` invoke the validate
  gate directly on the committed sources, so `checkCrossCodegen` runs and a
  hand-edited drifted `contracts.ts` is flagged as a Blocker. This is the
  honest cross-check defense.
- `dockyard build` runs `regenerateContracts` (stage 1 of `internal/buildpkg`)
  BEFORE invoking the validate gate (stage 2). At build time, a hand-edited
  `contracts.ts` is therefore *rewritten* by the regeneration step before
  `checkCrossCodegen` ever reads it. So `build`'s defense is "the regeneration
  step erases the drift", not "the validate gate flags it" — and the
  build artifact still upholds P1 (the binary embeds consistent contracts),
  just via a different mechanism than `validate`-standalone.

Both mechanisms uphold P1; `validate/doc.go`'s wording now distinguishes them
explicitly so the gate's behaviour matches the documentation.

---

## D-114 — the OTel adapter parents an exported span under the obs/v1 event's ParentSpanID

**Status:** Settled (depth-audit remediation R3).

D-076 settled that the `OTelEmitter` makes a Dockyard span carry the obs/v1
event's own W3C trace-id and span-id so it "nests natively under a calling
Harbor agent's `execute_tool` span", and D-079 made a handler `log` event a true
child of its enclosing `tool.call` in obs/v1. The depth audit found the OTel
adapter built the span-start context from only `{trace, span}` IDs and never
established a parent span context — so every exported OTel span was a root in
its trace, and the D-079 intra-trace parent linkage was lost on OTel export.

R3 carries the event's `ParentSpanID` through into the export path: `obsIDs`
parses a well-formed `ParentSpanID`, and `OTelEmitter.startContext` seats a
remote parent span context (`oteltrace.ContextWithRemoteSpanContext` over a
`SpanContext` with the event's trace-id and the parent span-id) on the context
passed to `tracer.Start`, so the exported span nests under its parent. An absent
or malformed `ParentSpanID` is tolerated as "no parent" — the span is exported
as a trace root rather than the event being dropped. The IDGenerator continues
to assign the span's own IDs unchanged; only the parent linkage is added.

---

## D-115 — `make build` embeds the production `web/inspector` SPA via an `inspector-bundle` prerequisite; `internal/inspector/dist/` is a `.gitkeep`-anchored, gitignored staging tree

**Status:** Settled (depth-audit-2 remediation R4 B1).
**Supersedes:** the committed-placeholder-bundle scheme of D-098 (the
`internal/inspector/dist/index.html` placeholder file).

D-098 settled — at Phase 22, when the inspector's `//go:embed all:dist`
directive landed and the production frontend build was still nascent — that
"a committed placeholder bundle (`internal/inspector/dist/`) keeps the
`//go:embed` directive resolvable before any frontend build; wiring the
production `web/inspector` build into the binary is the Phase 23 `dockyard
inspect` packaging step." The second pre-Wave-9 depth audit (Blocker B1)
found that the Phase 23 packaging step was never built: `Makefile` and the
CI `go` job ran `go build` directly, the `web/inspector` Vite output was
never copied into `internal/inspector/dist/`, and so the shipped `bin/dockyard`
embedded only the Phase 22 placeholder. RFC §12 line 711 ("renders Apps
locally") was met by the in-package tests and the per-package frontend gate,
but not by the product a developer installed and ran.

R4 closes the packaging step. A new `make inspector-bundle` target runs
`vite build` for `web/inspector` (after `npm ci` if needed) and stages the
resulting `dist/` tree into `internal/inspector/dist/`; `make build` declares
`inspector-bundle` as a prerequisite so the canonical build always produces a
binary whose inspector is the production Svelte SPA. `make build` still pins
`CGO_ENABLED=0` — the staged frontend is a build artifact, not a CGo
dependency. The CI `go` job gained a `setup-node` step so the build pipeline
has `npm` available; the `preflight` job's Node cache list now includes
`web/inspector/package-lock.json` for parity. `make web` is unchanged —
type-check + tests + coverage for every `web/` project — keeping the
developer's per-project frontend loop separate from the bundling concern.

The staging tree is hygienic by construction. The Phase 22 placeholder
`internal/inspector/dist/index.html` is replaced with a tracked, empty
`internal/inspector/dist/.gitkeep` anchor (so `//go:embed all:dist` always
resolves); the rest of the dist tree (`internal/inspector/dist/*`) is added
to `.gitignore` so a `make build` never dirties the working tree. When the
bundle has not been staged (a fresh clone, or `go build ./cmd/dockyard`
directly without the Makefile), the inspector backend falls back to its
in-Go `placeholderHTML` page (`internal/inspector/assets.go` — the existing
behaviour) so the backend is always usable; the placeholder is now Go source,
not a tracked HTML file. `make clean` restores the dist tree to its
`.gitkeep` anchor.

The regression guard (R4 S6) lives in `scripts/smoke/phase-23.sh`: the smoke
script asserts the staged `internal/inspector/dist/index.html` carries a
Vite-emitted `<script type="module" crossorigin src=...>` reference to the
hashed asset bundle, and fails when the legacy placeholder string returns or
no script tag is present. Together B1 + S6 mean a regression of the
`make inspector-bundle` prerequisite — a typo in the Makefile, a CI step
that skips `make build` — fails preflight, not the user.

---

## D-119 — the stdio Tasks mount serialises all writes to real stdout through one shared lock

**Status:** Settled (depth-audit remediation R5).

D-109 wired the stdio Tasks mount as a forwarded-pipe-pair design: the SDK
server runs on an `mcp.IOTransport` over an in-process pipe pair, the mount
pump owns real `os.Stdin`/`os.Stdout`, and a copy goroutine relays the SDK's
output pipe straight to real `os.Stdout`. The mount pump serialises its own
writes through an internal `sync.Mutex` (`writeMu` in
`runtime/tasks.Mount.ServeStdioFrames`) — but the SDK-output copy goroutine
ran `io.Copy(os.Stdout, sdkOutR)` on the SAME `os.Stdout` with no shared
mutex against the pump. The depth audit's read of the previous code's header
comment ("the two writers never interleave a single frame because each writes
whole lines") overstated the guarantee: a single `os.File.Write` to a pipe is
atomic only up to `PIPE_BUF` (4096 on macOS/Linux), and `io.Copy` uses a 32
KB buffer, so a large SDK frame can be split by the kernel and a mount-
written frame can intersperse mid-emission. JSON-RPC frames are typically
small so the practical-today risk was low — but the property the stdio
JSON-RPC pipe contract requires (every emitted frame is whole) was not
actually guaranteed.

R5 introduces a `sync.Mutex`-backed `io.Writer` adapter (`lockedWriter` in
`runtime/server/stdio.go`) that wraps real `os.Stdout` once and is passed
to BOTH writers: the SDK-output copy goroutine's `io.Copy` destination AND
the Tasks mount pump's `out` argument. Every write to real stdout now
serialises through one shared lock, regardless of frame size or the kernel's
per-Write atomicity bound. The mount's internal `writeMu` is preserved (it
keeps `ServeStdioFrames` standalone-safe when a caller does not provide a
serialising writer — its public contract is still "writes to out are
serialised"). The stdio entrypoint is refactored to `serveStdioWithTasksOn`
parameterised over stdin/stdout so the property is testable on in-memory
sinks; `serveStdioWithTasks` is the thin caller that binds the parameters
to the real OS streams. The new internal-package test
`TestLockedWriter_SerialisesConcurrentWrites` proves `lockedWriter` admits no
interleave under -race even with 64 KB frames, and
`TestServeStdioWithTasks_SharedStdoutSerialised` exercises the full wiring
end to end — concurrent tasks/list + initialize frames produce only well-
formed JSON-RPC frames on the shared stdout.

---

## D-120 — `obs/v1` `Event.SessionID` is populated from the in-flight MCP session

**Status:** Settled (depth-audit remediation R5).

Phase 15 introduced `obs.WithSession(ctx, sessionID)` and a private
`sessionFromContext` extractor, and made `obs.Event.SessionID` part of the
versioned public `obs/v1` wire contract — but `Recorder.emit` never read
`sessionFromContext(ctx)` to populate the field, and no transport ever
called `obs.WithSession`. The depth audit flagged the orphan: the wire field
was always empty, the doc comment claimed "Phase 16's transports populate it"
(they did not), and the only `_ = sessionFromContext(ctx)` reference in
`Recorder.ToolCall` was dead code.

R5 wires the orphan end to end:

1. `Recorder.emit` reads `sessionFromContext(ctx)` and stamps it onto
   `e.SessionID`. The wire field is `omitempty`, so a ctx without a session
   continues to emit an event with no `session_id` on the wire — the contract
   is unchanged for the no-session case.

2. The tool-handler edge (`runtime/server.withRequestSession`) and the new
   resource-handler edge (`withResourceRequestSession`) call
   `obs.WithSession(ctx, req.Session.ID())`. `req.Session.ID()` is the SDK's
   own session-id seam — it returns the streamable-HTTP transport's
   `Mcp-Session-Id`, and `""` on transports that do not mint one (in-memory,
   IO, SSE). The handler-edge wiring is the right place for the V1 because
   it is the single choke point every tool and resource handler passes
   through; a future per-transport propagation pass can layer in front of
   it without changing the emit sites.

The doc comments on `WithSession`/`sessionFromContext` are rewritten so the
seam's description matches reality. `TestRecorder_EmitStampsSessionID` (an
internal recorder unit test) along with `TestR5_S2_ToolCallEventCarriesSessionID`
and `TestR5_N1_ResourceReadEventCarriesSessionID` (real tools/call and
resources/read over the streamable-HTTP transport) prove the field lands on
the emitted events.

---

## D-121 — the resource handler span is threaded onto ctx so a nested obs/v1 event correlates as a child

**Status:** Settled (depth-audit remediation R5).

D-079 closed the obs/v1 handler-span correlation seam for the tool-handler
edge: `runtime/server/tool.go` opens the `tool.call` span and threads it onto
ctx via `obs.WithSpan` so a handler-emitted `log` event correlates as a
child of the enclosing `tool.call` rather than minting an unrelated trace.
The depth audit flagged the same gap class on the resource edge:
`runtime/server/resource.go` opened the `resource.read` lifecycle with
`obs.NewTrace()` and never threaded the span onto ctx; `runtime/apps.go`
emitted `app.load` with `obs.NewTrace()` unconditionally. A handler-emitted
log during a resources/read — or an `app.load` event minted inside a
resources/read — would NOT trace-correlate. Today's handlers do not emit
either, so the gap was latent; but it was the same consistency defect
D-079 closed, and the cost to close it was small.

R5 threads the resource handler's `obs.SpanContext` onto its handler context
via `obs.WithSpan` (both the non-template `AddResource` and the
`AddResourceTemplate` paths, mirroring the `tool.go` pattern), and
`runtime/apps.Register`'s read handler emits `app.load` via
`obs.ChildOrNewTrace(ctx)` so an `app.load` minted inside a resources/read
is a child of the read's span — same trace id, parent span id set to the
read's span id. The R5/N1 fix combines with R5/N2 (D-122): the resource
handler's span itself comes from `obs.NewTraceFromContext`, so the chain is
`inbound caller → resource.read → app.load` when an HTTP traceparent is
present. `TestR5_N1_ResourceReadSpanCorrelatesToChildEmits` proves the
correlation end to end via a real resources/read over the streamable-HTTP
transport.

---

## D-122 — the streamable-HTTP transport extracts inbound W3C TraceContext so handler spans inherit the caller's trace

**Status:** Settled (depth-audit remediation R5).

The OTel adapter's package doc (RFC §11.3, D-076) claimed a Dockyard span
"nests natively under a calling Harbor agent's `execute_tool` span". D-114
fixed the *intra-trace* parent linkage on OTel export (a handler `log`
child-of-its-`tool.call` survives the lowering). But the cross-process
inheritance — a Harbor agent's outbound `tool.call` span becoming the
parent of a Dockyard server's `tool.call` span — was never wired: every
handler-edge call site minted a fresh `obs.NewTrace()`, with no path for an
inbound W3C `traceparent` to seed the trace identity. The claim was
aspirational; R5 makes it real for the streamable-HTTP transport (the only
production-grade transport where a remote agent calls in).

R5 adds three pieces, all server-side, with no OTel dependency in
`runtime/server`:

1. `obs.WithInboundTrace(ctx, parent)` / `InboundTraceFromContext` — the
   context seam a transport-layer propagator uses to thread the inbound
   parent SpanContext onto a request context.
2. `obs.NewTraceFromContext(ctx)` — the handler-edge counterpart of
   `NewTrace`; when ctx carries an inbound parent, it returns
   `parent.Child()` (preserving the caller's TraceID, ParentID = inbound
   span id, fresh own SpanID); otherwise it falls back to `NewTrace()`.
3. `runtime/server.traceparentMiddleware` — a small W3C TraceContext
   extractor on the streamable-HTTP transport boundary. It parses the
   `traceparent` request header (version `00` only, the only well-defined
   format today; `tracestate` is not yet carried by `obs.SpanContext` and
   is a versioned future addition) and stamps the parsed SpanContext via
   `obs.WithInboundTrace`. The middleware sits OUTERMOST in
   `HTTPHandler`'s chain so the parent context reaches every downstream
   handler (the Tasks mount, the Content-Type check, the SDK handler).
   Extraction is purely read-only — it never authorises, never fails a
   request, and observability never fails a request (P2). Parsing is by
   hand: the W3C format is small, and a `go.opentelemetry.io/otel`
   dependency at the always-on transport boundary would leak the optional
   adapter into the server core (P3 / CLAUDE.md §10 — only
   `internal/protocolcodec` and the optional `runtime/obs/otel` carry MCP
   extension or OTel dependencies).

The tool and resource handler edges (`runtime/server/tool.go`,
`runtime/server/resource.go`) call `obs.NewTraceFromContext(ctx)` in place
of `obs.NewTrace()` so the propagator's parent is honoured when present;
with no inbound traceparent the behaviour is unchanged — a fresh root span.
On OTel export the result automatically nests: the exported span carries the
event's TraceID and ParentSpanID, and D-114's `startContext` seats a remote
parent context for OTel, so the Dockyard span lands as a child of the
caller's `execute_tool` span without any extra OTel wiring. The OTel
adapter doc claims (otel.go package doc + the `OTelEmitter` comment) are
rewritten so they describe what is actually wired now, not the prior
aspiration. `TestParseTraceparent_Valid` / `_Invalid`,
`TestTraceparentMiddleware_*`, and the end-to-end
`TestR5_N2_ToolCallInheritsInboundTraceparent` (a real tools/call over
streamable-HTTP with a `Traceparent` header) prove the inheritance lands.

The stdio transport is deliberately out of scope: stdio is the single-user
local-deployment mode (RFC §5.2), there is no cross-process agent to
inherit from, and the JSON-RPC frame layer carries no header analogue.

## D-123 — Templates use the spec + the inspector's live preview as the §20 verification (no static mockup)

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** docs/plans/phase-24-analytics-widgets.md "Findings I'm
departing from"; CLAUDE.md §20 governs the general rule.

**Why.** CLAUDE.md §20's spec → mockup → build rule mandates an approved
static visual mockup before any UI work. For a *template phase* the
verification surface is materially different from the inspector or the docs
site: a template is a generated showcase whose visual quality is verified by
(a) the page spec carried in the phase plan and (b) the *live* preview a
developer sees when they run the materialised project under the inspector
(`dockyard inspect`) or against any MCP host. A static `.png` adds overhead
without buying review confidence — the inspector's fixture switcher already
walks every UI state.

**Scope (explicit).** This carve-out applies **only to `templates/<name>/`
phases** — Phase 24 (`analytics-widgets`), Phase 25 (`approval-flow`), Phase
26 (`inspector`), and any post-V1 templates from RFC §19. The inspector
(Phases 22, 23), the docs site (Phase 29), the bridge shell, and the post-V1
multi-server console all keep the full §20 spec → mockup → build process.

**Substitute verification (what a template phase must do instead of the
mockup):**

1. The phase plan carries a page spec (purpose, regions, data, the four
   states). This is unchanged.
2. The phase plan enumerates the fixture set every rendered widget walks
   under the inspector's switcher (`happy`, `empty`, `error`, `permission`,
   `slow`, `large`).
3. The Phase 24 integration test asserts each fixture drives a distinct UI
   state in the dispatcher, and the `web/inspector` vitest harness asserts
   the rendered DOM picks the right renderer per `kind` and that a dark
   host-theme propagates through `hostContext.styles.variables`.

**Approval.** The user approved the carve-out explicitly when scoping
Phase 24 (the prompt's §1 / §4 record the approval). The §14 checklist's
"every page has loading/empty/error/ready states" gate is unchanged — the
mandatory four-state `PageState` discipline still applies to every widget the
template renders.

**Implication for §20.** §20 is unchanged in wording; D-123 is the
documented per-phase deviation. A future PR may roll D-123 forward into the
§20 text if the template-phase pattern proves out across Phases 25 and 26;
until then, every template phase that wants the carve-out cites D-123
explicitly in its "Findings I'm departing from" section.

---

## D-124 — V1 template name: `analytics-widgets` (replaces `analytical-card`)

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** RFC §10, master plan `docs/plans/README.md` Phase 24 row
and detail block, this phase plan.
**Supersedes:** the master-plan stub `analytical-card` (un-numbered — never
shipped as code).

**Why.** The master plan's original name `analytical-card` framed the
template as one *card* (a single rendered surface). The shipped template is
*three widget tools* in one App — a chart, a table, and a metric card — each
inline-rendered through a dispatcher. `analytics-widgets` describes what the
template actually showcases (a widget set), reads naturally in the
`dockyard new --template <name>` invocation, and remains a *workflow*-named
template per brief 04 §2.3 (not a transport-named one). The rename is part
of Phase 24; the only historical mention preserved is in
`docs/research/04-mcp-use-dx-teardown.md`, with an editor's note pointing at
this entry.

---

## D-125 — Apache ECharts is the V1 chart renderer; the tool contract stays renderer-agnostic

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** templates/analytics-widgets/web/src/widgets/ChartFrame.svelte;
templates/analytics-widgets/internal/contracts/contracts.go.

**Why.** The `create_chart` contract takes a friendly shorthand (`type` +
`data` + optional `title`/`options`/`theme`) and emits a `structuredContent`
that an App-side renderer turns into a chart. V1 uses Apache ECharts because
it covers the V1 type set (`bar | line | area | pie | scatter | radar`) out
of the box, ships an Apache-2.0 licence aligned with Dockyard's, has a
stable API, and handles theme propagation cleanly. The wrapper
(`ChartFrame.svelte`) lives in the template — not `web/ui/` — because
CLAUDE.md §20 reserves the shared inventory for primitives, and a wrapper
around a third-party fat library is by nature template-local (a future
template that does not chart should not pull ECharts into its bundle).

**Scope.** The contract surface (`CreateChartInput.options` is an
`EChartsOptions` passthrough today) is the only place ECharts leaks into the
generated artifact. The dispatcher consumes the structured payload through
the typed contract; a future template (or a developer fork) could swap the
renderer without changing the tool contract — provided they keep the same
`structuredContent.kind = "chart"` shape. That keeps the *contract*
renderer-agnostic while making the *V1 default* concrete.

---

## D-126 — `analytics-widgets` declares `display_modes: [inline]` only

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** templates/analytics-widgets/dockyard.app.yaml.

**Why.** RFC §7.2 specifies three display modes (`inline`, `fullscreen`,
`pip`) and the App declares the subset it supports. The widget set the
template renders is small, dense, and meaningful in the host's chat surface
— a metric card, an inline chart, a paged table — so `inline` is the right
and only mode for V1. Declaring `[inline]` means the bridge only ever grants
inline, which is the lightest CSP and the most-supported host surface, and
keeps the template's purpose unambiguous (it is a *widget* showcase, not a
fullscreen-dashboard showcase). Future templates exercise fullscreen and pip
(`approval-flow`, `inspector` likely candidates).

---

## D-127 — `Sparkline` lands in `web/ui/`; `ChartFrame` stays template-local

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** web/ui/src/Sparkline.svelte; docs/design/CONVENTIONS.md §3;
templates/analytics-widgets/web/src/widgets/ChartFrame.svelte.

**Why.** Sparkline is a small, generally-useful primitive — pure SVG,
token-driven, no third-party dependency, reusable by the inspector, the
docs site, and any future template. By CLAUDE.md §20 it belongs in the
shared inventory; landing it in `web/ui/` (and listing it in
`CONVENTIONS.md` §3) makes it composable for everyone. ChartFrame, in
contrast, is a wrapper around Apache ECharts — a heavy third-party library
whose dependency footprint should not leak into surfaces that do not chart.
The split keeps the shared inventory cheap to depend on while letting the
template render anything ECharts can.

---

## D-128 — The template-discovery seam is interface + Registry + builtin init

**Date:** 2026-05-23
**Status:** Settled (Phase 24)
**Where it lives:** internal/scaffold/template.go.

**Why.** Adding a future template (Phases 25, 26, the post-V1 set) must be
one new `templates/<name>/` directory plus one registration; nothing about a
specific template's name belongs in the CLI or in the materialiser. The
implementation follows CLAUDE.md §4.4's extensibility-seam pattern: a
`Template` interface, a process-wide `Registry` keyed on template name, and
`init()` registration in a builtin file that lives next to each template's
source. The seam exposes `LookupTemplate(name) (Template, bool)` and
`GenerateFromTemplate(opts, name)`; an unknown name returns the typed
sentinel `ErrUnknownTemplate`. The integration test for Phase 24 exercises
the seam through the registered `analytics-widgets` template; a separate
unit test registers a stub template to prove the seam itself is not coupled
to `analytics-widgets`.

---

## D-129 — Bridge default peer posts with a wildcard targetOrigin; host bridge clones every outbound message

**Date:** 2026-05-23
**Status:** Settled (Phase 24 — post-mortem from the inspector demo)
**Where it lives:** `web/bridge/src/bridge.ts` (`defaultParentSink`),
`web/inspector/src/host/host-bridge.ts` (`postSafe`).

**Why.** Phase 24's end-to-end demo surfaced two postMessage-shaped
defects nothing else exercised:

1. The bridge's default peer was `window.parent` — and the bridge's
   `Transport.request(...)` called `peer.postMessage(message)` with one
   argument. `Window.postMessage`'s one-arg form defaults `targetOrigin`
   to `'/'` (same-origin only); an MCP App runs inside a sandboxed
   `allow-scripts`-only iframe whose origin is opaque (`null`) while the
   parent has its own origin, so every outbound `ui/initialize` was
   silently dropped at the boundary and the handshake hung forever.
2. The inspector's host bridge passes payloads that originate from
   Svelte 5 `$state` (capabilities, hostContext, fixture content). The
   structured-clone algorithm `Window.postMessage` runs refuses Proxies
   with a `DataCloneError`, so the host's `ui/initialize` response (and
   every subsequent `tool-result` notification) failed to serialise.

**Decision.**

- The bridge's default peer is now a sink that posts to `window.parent`
  with `targetOrigin: '*'` explicitly. `'*'` is the correct value here:
  the View half can't usefully narrow the origin because the host frame
  is cross-origin (or opaque) from its perspective — the trust boundary
  is the host's inbound handler, which is what already validates JSON-RPC
  envelopes and the fixture-backed responder. A regression test in
  `web/bridge/src/__tests__/bridge.test.ts` asserts the default path
  posts with `'*'`.
- The inspector's host bridge applies a `JSON.parse(JSON.stringify(...))`
  round-trip to every outbound message in `postSafe()`. The protocol's
  payloads are plain JSON-shaped objects (no functions, Maps, or Dates),
  so the round-trip is safe; it unwraps any `$state` Proxy into a plain
  object before `postMessage`. A `structuredClone` walk would also unwrap
  but would re-enter Svelte's reactivity graph and trigger
  `effect_update_depth_exceeded`; JSON-stringify is non-reactive and
  deterministic.

The two fixes together let `dockyard inspect` complete the
`ui/initialize` handshake and render the analytics-widgets chart, table,
and metric-card widgets end-to-end (the Phase 24 acceptance bar). The
`docs/screenshots/analytics-widgets/{chart,metric-card,table}.png`
artifacts are the captured proof.

---

## D-130 — The inspector loads on-disk project fixtures from `<dir>/fixtures/<tool>/<kind>.json` and prefers them over the schema-derived synthetic fixtures

**Date:** 2026-05-23
**Status:** Settled (Phase 24 — closing the demo bar)
**Where it lives:** `internal/inspector/fixtures.go`,
`internal/inspector/assets.go` (`/api/fixtures`),
`web/inspector/src/lib/fixtures.ts` (`buildFixtures` overrides),
`web/inspector/src/lib/api.ts` (`fetchProjectFixtures`).

**Why.** The Phase 23 fixture switcher synthesises `structuredContent`
from the tool's generated output schema. That is structurally correct
(P1 — the synthesised value is a valid instance of the schema) but the
field values are placeholders — `"sample-value"` strings, `42` numbers
— so an App's dispatcher does not see a `kind` it knows
(`"chart"`/`"table"`/`"metric_card"`) and the rendered widget can never
be the realistic data the template ships. The Phase 24 demo bar is
"the inspector visibly renders all three widgets"; placeholders cannot
satisfy it.

**Decision.** The inspector loads project on-disk fixtures from the
attached project's `fixtures/<tool>/<kind>.json` tree and serves them
at `GET /api/fixtures`. The frontend's `FixturesPanel` prefers a
project fixture over the schema-derived synthetic when one exists for
the `(tool, kind)` tuple, and falls back to the synthetic otherwise.
The loader is read-only (no `tools/call`); it only reads files the
developer's own project carries, so P4 (the inspector is a dev surface,
never a production MCP client) is preserved.

A fixture's `output_override` is used verbatim when present; otherwise
the loader derives `structuredContent` from `input` by adding the
well-known dispatcher discriminators (`kind`, `state`, default
`theme: "auto"`). This mirrors the analytics-widgets handlers, which
construct their output by copying input and adding `kind` + `state`.

Templates that need richer derivation (e.g. a future approval-flow
template that produces a different shape than its input) ship explicit
`output_override` blocks.

---

## D-131 — Operator-initiated `tools/call` from the inspector is within P4

**Date:** 2026-05-23
**Status:** Settled (Phase 24 — finishing pass)
**Where it lives:** `internal/inspector/invoke.go`,
`internal/inspector/assets.go` (`POST /api/tools/invoke`),
`internal/cli/inspect.go` (`Invoker: inspector.ToolsFromServer(cfg.serverURL)`),
`web/inspector/src/lib/ToolsPanel.svelte` (the operator form + Invoke),
`web/inspector/src/lib/schema-form.ts` (form fields from input JSON Schema),
`web/inspector/src/lib/api.ts` (`invokeTool` client).

**Why.** Phase 24 shipped the analytics-widgets template and proved its
three widgets render through the Fixtures switcher (D-129/D-130). But
the inspector still answered every Apps `tools/call` from a fixture —
a developer who wanted to drive a real `tools/call` against the
attached server with arbitrary parameters had to leave the inspector
(e.g. `curl` the streamable-HTTP transport directly), which is a
documentation gap, not a tool. The Phase-24-finish bar made by the
user is "the framework is exercisable end-to-end through this initial
example through its inspector"; the absence of operator-driven
invocation was the load-bearing missing piece.

**Decision.** The inspector additionally issues real `tools/call` to
the attached server **when an operator initiates it through the UI**.
The frontend's Tools panel generates a parameter form from the tool's
generated input JSON Schema (P1 — the schema is the source of truth),
POSTs `{tool, arguments}` to the backend's `POST /api/tools/invoke`,
and the backend opens a short-lived MCP client session, calls
`tools/call`, and returns the result. The structured result flows
through the same `pushToolResult` path the Fixtures switcher uses
(D-129) so the App preview re-renders with the operator's parameters.

**Why this stays within P4.** P4 (CLAUDE.md §1, §13) says the
inspector is "the lone client-shaped component" and is "test-only,
dev-mode-gated, localhost-bound". D-131 keeps every clause intact:

- The inspector remains the lone client-shaped surface — no new
  package, no new long-lived client, no production client.
- The endpoint is localhost-only via the existing `requireLoopback`
  gate (CVE-2025-49596 lesson; brief 05 §4.2). An off-localhost actor
  cannot reach it.
- The operator is the one driving the write through the UI — not an
  agent and not an off-localhost actor; this is symmetric to D-103's
  read-only `resources/read` (the operator's UI action drives a
  read-only RPC; D-131 drives a mutating RPC, but only on the
  operator's deliberate Invoke click).
- Each invocation opens a fresh client session, calls one tool, and
  closes — no long-lived client state and no client SDK leaks into
  Dockyard's surface (no raw MCP types in handler-facing APIs, P3 —
  the `InvokeRequest`/`InvokeResponse` types are the inspector's
  own).

**Supersedes / extends.** D-131 extends D-099 (the inspector
"attaches a read-only obs relay, not an MCP client") and D-103 (the
inspector additionally performs read-only `resources/list` and
`resources/read` to render Apps). Both prior decisions stand for
their original surfaces; D-131 adds the operator-initiated
`tools/call` surface to the same lone client-shaped component.

A `tool-level` error (the tool's `CallToolResult.isError` is true) is
a successful RPC: HTTP 200 with `isError: true` in the response, the
inspector renders the error surface without conflating it with a
transport failure. A transport-level failure (server unreachable,
tool not found, validation error) answers 502 with a typed JSON
message and the frontend renders an `ErrorState` with a working
retry (CLAUDE.md §20).

---

## D-132 — The analytics-widgets template mounts the obs/v1 SSE stream on the same HTTP listener as the MCP transport

**Date:** 2026-05-23
**Status:** Settled (Phase 24 — finishing pass)
**Where it lives:** `templates/analytics-widgets/main.go.tmpl`
(constructs `obs.NewSSESink("")`, mounts `obsSink.Handler()` at
`/obs/v1/stream` of the MCP HTTP mux, passes the sink as
`server.Options.Obs`).

**Why.** Phase 24 templated an analytics-widgets server that built and
served the streamable-HTTP MCP transport, but did NOT expose the
obs/v1 SSE endpoint. The inspector's relay (Phase 22, D-099) subscribes
to `<server>/obs/v1/stream`; with no SSE listener on the server, the
Events / Analytics / RPC / Tasks panels stayed empty even after real
`tools/call` invocations. The Phase-24-finish brief required every
rail tab to be exercised end-to-end through the demo; the empty
Events tab was the most visible regression.

**Decision.** The analytics-widgets template instantiates an
`obs.SSESink` at startup, passes it as `server.Options.Obs` so the
runtime emits obs/v1 events through it, and mounts its
`/obs/v1/stream` handler on the MCP HTTP server's mux (a small
`http.NewServeMux` that delegates `/` to the MCP handler and serves
`/obs/v1/stream` separately). This keeps the operator UX simple — one
URL the inspector connects to — without breaking the out-of-band
property the SSESink exists for: the sink ALSO holds its own
loopback-bound listener (the one stdio servers use), so a stdio
deployment stays correct.

**Trade-off acknowledged.** Mounting the SSE handler on the MCP
listener means the same listener serves both the MCP transport and
the obs/v1 stream. This is acceptable because:

- The default HTTP bind is loopback (`127.0.0.1:8080`); a developer
  who binds wider is making an explicit choice.
- The obs/v1 capture defaults to shape + size only (CLAUDE.md §7); a
  redaction-aware policy gates full content.
- Future templates that need a stricter posture can opt out by
  binding the SSESink's own listener and not mounting the handler.

---

## D-133 — `AppFrame.sendToolResult` is guarded against re-firing for the same payload

**Date:** 2026-05-23
**Status:** Settled (Phase 24 — finishing pass)
**Where it lives:** `web/inspector/src/lib/AppFrame.svelte` (the
`lastSentPayload` closure variable in the `pushToolResult` `$effect`).

**Why.** Phase 24's `pushToolResult` `$effect` (D-127) tracks both
the pushed payload AND `frameStatus`, so the post-handshake transition
fires the effect for the initial mount. The Phase-24-finish work
discovered a real defect: after an operator-initiated `tools/call`
(D-131) the App preview iframe's response loops back through the
host-bridge, triggers a re-render in the inspector, and the
`frameStatus` transitions handshaking → ready → handshaking → ready
indefinitely. Each "ready" edge re-fired the effect, re-sent the
same `tool-result` notification, and Svelte eventually hit
`effect_update_depth_exceeded` — at which point every interactive
control froze (the bug the Phase-24-finish Playwright walkthrough
surfaced).

**Decision.** The `pushToolResult` effect compares the serialised
payload to the last sent one. An unchanged payload is a no-op; an
actual fixture-or-invoke change goes through once. The comparison
key is `JSON.stringify(pushToolResult)` — non-reactive,
deterministic, and identity-safe across Svelte $state Proxy unwrap
(D-129's lesson).

**Why a closure variable not a `$state`.** The guard is an
implementation detail of the effect; making it reactive would
re-introduce the loop the guard is preventing. A plain `let`
captured by the effect's closure is the lightest fix and matches
the pattern FixturesPanel already uses for its `lastApplied`
auto-apply gate.

---

## D-134 — The bridge ships a typed View→host `elicitation-response` notification; the inspector forwards it to `tasks/result`

**Date:** 2026-05-23
**Status:** Settled (Phase 25)
**Where it lives:** `web/bridge/src/protocol.ts`
(`ViewNotification.elicitationResponse` + `ElicitationResponseParams`),
`web/bridge/src/bridge.ts` (`BridgeShell.sendElicitationResponse`),
`web/inspector/src/host/host-bridge.ts` (the host-half dispatcher +
`setElicitationResponder`), `internal/inspector/elicitation.go` (the
`Elicitor` seam + `ElicitationFromServer` adapter),
`internal/inspector/assets.go`
(`POST /api/tasks/elicitation`), `internal/cli/inspect.go`
(`Elicitor: inspector.ElicitationFromServer(cfg.serverURL)`),
`web/inspector/src/lib/api.ts` (`postElicitationResponse`).

**Why.** Phase 25's `approval-flows` template is the first product
driver of MCP Tasks × Apps (RFC §8.6). A handler calls
`TaskHandle.RequireInput`, the task pauses at `input_required`, the App
renders the prompt — and then needed a way to *answer* it from inside
the iframe. The View → host postMessage dialect did not carry the
elicitation reply; the bridge had no helper for it; the inspector's
host-half had no path to deliver it to the attached server's
`tasks/result` endpoint. Without this seam the App's "Approve" /
"Reject" click had nowhere to go and the suspended task could not
resume — the Tasks × Apps composition was a dead loop.

**Decision.** Three pieces, one named decision:

1. **The protocol.** A new View → host notification —
   `ViewNotification.elicitationResponse` (`ui/notifications/
   elicitation-response`) — with a typed `ElicitationResponseParams =
   { taskId: string, data?: unknown, declined?: boolean }`. Fire-and-
   forget: the App observes the task's terminal status through a
   subsequent `tool-result` push or through the inspector's Tasks panel,
   not a synchronous reply on this channel. Mirrors the existing
   notification shape (`ui/notifications/initialized`,
   `ui/notifications/tool-result`) and avoids a second round-trip on
   the happy path.

2. **The View helper.** The bridge exposes
   `BridgeShell.sendElicitationResponse(taskId, data?, options?)`. An
   App author never hand-builds the wire — same posture as
   `bridge.callTool`, `bridge.openLink`, etc.

3. **The inspector's host-half + backend.** The inspector's host-bridge
   gains an `elicitationResponder` setter; the inspector backend gains a
   new `Elicitor` seam, an `ElicitationFromServer(baseURL)` adapter
   that posts a raw `tasks/result` JSON-RPC frame to the attached
   server, and `POST /api/tasks/elicitation` as the operator-facing
   surface. The inspector frontend posts to it from
   `postElicitationResponse`. Localhost-only via the existing
   `requireLoopback` gate (CVE-2025-49596 lesson; brief 05 §4.2).

**Why this stays within P4.** P4 says the inspector is "the lone
client-shaped component, dev-mode-gated, localhost-bound" (CLAUDE.md
§1, §13). D-134 keeps every clause intact, by direct analogy with
D-131's operator-initiated `tools/call`:

- The inspector remains the lone client-shaped surface — no new
  package, no new long-lived client, no production client.
- The endpoint is localhost-only via the same `requireLoopback` gate
  that gates D-131. An off-localhost actor cannot reach it.
- The operator is the one driving the write — the App's "Approve" /
  "Reject" click in the inspector's preview frame. An agent or an
  off-localhost actor cannot trigger an elicitation delivery
  unilaterally; the App's iframe is the only producer.
- Each delivery posts one JSON-RPC frame and reads one response — no
  long-lived client state and no client SDK leaks into Dockyard's
  surface. The wire shape is plain JSON-RPC (Tasks methods sit
  outside the go-sdk's typed dispatch table — RFC §8.2; D-108), so
  the inspector does not import the SDK for this path.

**Supersedes / extends.** D-134 extends D-131 (operator-initiated
`tools/call`). Both decisions add a single operator-driven mutating
RPC surface to the inspector — different RPC, same posture.

**Failure modes.** A server-side refusal (the JSON-RPC envelope
carries an `error` block) is a successful RPC: HTTP 200 with
`delivered=false` + the server's error message, mirroring D-131's
`IsError`-as-200 pattern. A transport-level failure (unreachable,
malformed body) answers 502 with a typed JSON message and the
frontend logs the failure for the developer (a fire-and-forget
notification cannot surface in the App's UI without another protocol
round-trip).

**An alternate shape considered.** A *request* / *response* dialect
(the App calls a `ui/tasks/result` request and awaits a result block)
would let the App show "delivering…" / "delivered" / "rejected"
chrome synchronously. Rejected for V1: it doubles the round-trips on
the happy path, complicates the host-half's idempotency story, and
delivers no information the inspector's Tasks panel + a subsequent
`tool-result` push do not already surface. The cost of moving to a
request shape later is a one-line method add + a host-half handler
swap; the cost of pulling it back if it proves unnecessary is the
same — symmetric and reversible.

---

## D-135 — A template that declares task-supporting tools attaches a `tasks.Engine` in the scaffolded `main.go`

**Date:** 2026-05-23
**Status:** Settled (Phase 25)
**Where it lives:** `templates/approval-flows/main.go.tmpl`
(the scaffolded entrypoint constructs `tasks.NewInMemoryStore` +
`tasks.NewEngine` and passes the engine via
`server.Options{Tasks: engine, TasksAuthContext: …}`; starts the
purge sweep on context, stops it on shutdown).

**Why.** R2 (D-108) shipped the `runtime/server.Options.Tasks` seam
that connects a Tasks engine to the server transports, but explicitly
deferred wiring `dockyard run` and the scaffolded `main.go` to
construct one. The deferred follow-up read:

> A later CLI/scaffold phase wires `dockyard run` and the scaffolded
> `main.go` to attach a `tasks.Engine` when the project declares
> task-supporting tools.

Phase 25 is that phase for templates that need it. The
`approval-flows` template's two tools declare `task_support:
required` — neither tool works without a real engine — so a template
that omitted the wiring would scaffold a project that builds but
returns a clean error on every tool call ("Tasks engine not
attached"). Asking the developer to copy a multi-line construction
into their own `main.go` is exactly the friction the framework
exists to remove. The scaffolded `main.go` *is* the integration
surface.

**Decision.** A template that declares any tool with `task_support`
∈ {`optional`, `required`} ships a `main.go.tmpl` that:

1. Constructs a real `TaskStore` (the in-memory driver for the
   single-user stdio default; the README documents the swap to
   `sqlitestore.Open` for HTTP).
2. Constructs a `tasks.NewEngine` over that store, with a sensible
   default `Options` (poll interval, obs emitter wired). For stdio
   single-user, `RequestorIdentifiable=false` and `AdvertiseList=false`
   — `tasks/list` is withheld per brief 02 §4.5. For HTTP with real
   auth, the comment block points at the `WithTasks(engine,
   AuthContextFunc)` form.
3. Attaches the engine via `server.Options.Tasks` (matching the
   `Options.Obs` idiom).
4. Starts the engine's `StartSweep(ctx)` and defers `StopSweep` for a
   clean shutdown.

**Scope.** D-135 is binding for *templates* that declare
task-supporting tools — Phase 25's `approval-flows`, and any future
template that wants Tasks. The no-template `dockyard new` scaffold
(Phase 17) declares its example tool `task_support: forbidden`, so
the wiring stays inert there: a server with no task-supporting tool
gets no Tasks engine, no overhead, byte-for-byte the same shape as
before. A future no-template scaffold that opts in to Tasks would
land its own wiring under the same idiom.

**Why a template author writes the wiring, not a code generator.**
A scaffold helper (e.g. `runtime/tasks.Standard()`) would shorten
the boilerplate, but the boilerplate IS the integration surface the
developer needs to *see* — the engine, the store, the auth context,
the sweep. Hiding it behind a one-liner would scaffold a project
that "just works" until the moment the developer needs to add an
auth context (HTTP) or swap the store (durable) or tune the cap
(`MaxConcurrentPerRequestor`), and then they would have nowhere to
edit. The boilerplate is teaching surface. The template's README
explains every line in plain English; the developer who reads the
file owns the engine's behaviour without grepping the framework.

**Failure modes / risks.** A template author can forget to
`StopSweep` — a goroutine leak under repeated restarts. The
template's `defer engine.StopSweep()` next to the construction is
the visible reminder; the integration test covers a clean shutdown
implicitly through `-race`. A template that scaffolds an HTTP-only
posture with `RequestorIdentifiable=true` but no real
`AuthContextFunc` would advertise `tasks/list` but bind every task
to the empty auth context — the brief 02 §4.5 problem. The README
documents the HTTP shape and the integration test asserts the stdio
default; an HTTP conformance test is Phase 27's concern, not D-135's
(filed as a risk on phase-25-approval-flows.md).

---

## D-136 — Phase 26 (`inspector` template) deferred post-V1; Wave 9 closes at two templates

**Date:** 2026-05-23
**Status:** Settled (Wave 9 closure decision)
**Where it lives:** `docs/plans/README.md` (Phase 26 detail block, Wave 9 row,
phase index, post-V1 follow-ups paragraph); `docs/design/design-spec.md` §5;
this file.

**Why:** The original three-template plan (`analytical-card`, `approval-flow`,
`inspector`) was set early when the product was hand-wavy. After Phases 24 and
25 shipped end-to-end with Playwright-proven demos, the `inspector` template
slot was reviewed and judged not to earn its place in V1:

1. **It would not exercise a new framework capability.** Phase 24
   (`analytics-widgets`, D-124) proved read-side rendering: App + bridge +
   fixtures + `obs/v1`. Phase 25 (`approval-flows`, D-134/D-135) proved
   write-side: MCP Tasks + `input_required` + view→host writes + the full
   lifecycle through `runtime/server`'s tasks-mount seam (the R2 follow-up
   D-108 named, closed here). A "drill-down / detail-view" template would
   mostly re-use Phase 24's capabilities — richer data display composable
   from `MetricCard` / `DataTable` / `JsonInspector` / `web/ui` primitives a
   developer already has — without exercising a framework surface neither of
   the two shipped templates already proves.
2. **The name was structurally confusing.** "`inspector` template" against the
   framework's debugging `dockyard inspect` tool forced every reader to
   disambiguate; the concept was never sharp enough to deserve the name.
3. **Templates are showcases, not products** (RFC §10). They earn their place
   by demonstrating something the framework does; three is not better than
   two if the third does not earn it.
4. **Wave 10 work is higher leverage for V1.** Agent skills + the published
   docs site (Phase 29) directly drive adoption; hardening + spec-compliance
   conformance (Phase 27) drives ship readiness; the V1 cut (Phase 30) is
   the goal. Reallocating Phase 26's capacity to those gets V1 done sooner
   and with more polish.

**Criteria a future template would need to claim Phase 26's slot post-V1.** Any
later template that earns it must (a) exercise a framework capability the two
shipped templates do not already prove (e.g. MCP prompts, dynamic resource
templates beyond `ui://`, a no-UI backend-only minimal server pattern,
auth-context binding from D-088's V2 path), (b) ship a real Playwright-proven
demo end-to-end through `dockyard inspect` against the scaffolded project
(the bar Phases 24 + 25 established), and (c) come with the same six fixture
states wired to its generated contracts. Without those it is not a template
worth its maintenance cost.

This decision **closes Wave 9 at two templates** and clears the way for Wave
10. Phase 29's dependency line is updated from `21, 26` to `21, 25`
accordingly.

---

## D-137 — The published tech-docs site is built with VitePress under `docs/site/`

**Date:** 2026-05-23
**Status:** Settled
**Where it lives:** RFC §2 (the authoritative-sources priority chain the docs
site teaches), AGENTS.md §19 (the published docs are part of the §19 hygiene
surface), `docs/plans/phase-29-skills-docs.md`.

**The decision.** Dockyard's published technical-documentation site
(`docs/site/`, deployed to GitHub Pages by `.github/workflows/docs.yml`) is
built with **VitePress** (1.6.x at the time of writing). The site stitches
together: the home page; a getting-started section with one walkthrough per
shipped V1 template (`analytics-widgets`, `approval-flows`); per-surface
guides; a CLI reference auto-derived from the cobra command tree by
`internal/clidocs`; an agent-skills index; and reference pages that
**transclude** the in-repo canonical sources (RFC-001, the master plan,
the decisions log, the glossary, the design conventions) via VitePress's
`<!--@include: …-->` directive so the site is the same source of truth, not
a copy that drifts.

**Why VitePress, not mdBook / Astro Starlight / MkDocs Material.**

1. **Same toolchain we already ship.** The repo already uses Vite for
   `web/bridge`, `web/ui`, `web/inspector`, and every template's
   `web/`. VitePress is Vite-powered; the docs site adds no new runtime
   technology — `make docs` is the same `npm ci && npm run build` shape
   the existing `make web` target uses. No second tool family to teach.
2. **CGo-free / Node-only.** The published artifact is a static site,
   consistent with the framework's pure-Go runtime guarantee. mdBook
   would add a Rust toolchain to CI for one job; Astro Starlight is
   heavier and uses an additional component model; MkDocs would add
   Python. VitePress reuses node + npm.
3. **Markdown-native + transclusion.** VitePress's `<!--@include: …-->`
   directive lets the canonical RFC, plans, decisions, glossary, and
   design conventions render as the docs site's reference pages without
   duplication. A canonical-source edit is reflected on the next site
   build — drift is mechanically impossible for those pages.
4. **Built-in search + dead-link checking.** VitePress's `themeConfig.search`
   (local provider) ships search without a third-party service; setting
   `ignoreDeadLinks: false` (the config default we keep on) fails the
   build on a dead internal link — exactly the §19 hygiene rule the
   docs surface is supposed to enforce.
5. **Low operational complexity.** One config file, one build command,
   one CI workflow. The published site is one tag away from a release
   cut (Phase 30).

**What the decision rules out for V1.**

- A docs CMS (Sphinx, Hugo, Docusaurus, MkDocs) — heavier, and none
  reduce the toolchain count below "VitePress reuses Vite".
- Versioned docs trees and a heavy search backend (Algolia, MeiliSearch).
  V1 ships from `main`; if a future post-V1 need for versioned docs
  arises, VitePress supports it via the standard multi-config pattern.

**Knock-on rules (enforced).** The docs site is part of the AGENTS.md §19
hygiene surface: a PR that changes user-facing surface (a CLI verb, a
manifest field, a template, the generated-project shape, a public runtime
API) updates the affected docs page(s) in the same PR. The `make docs`
build step fails on a dead internal link; CI runs it on every PR (build-
only) and on every push to `main` (build + deploy).

---

## D-138 — `scripts/drift-audit.sh` enforces AGENTS.md §19 mechanically

**Date:** 2026-05-23
**Status:** Settled
**Where it lives:** AGENTS.md §19 ("keeping skills and docs in sync with the
surface is mandatory repo hygiene"), `scripts/drift-audit.sh` §6,
`docs/plans/phase-29-skills-docs.md`.

**The decision.** Before Phase 29, AGENTS.md §19 existed in the text but
had no mechanical enforcement: a contributor changing a CLI verb without
updating the affected skill or docs page passed CI silently. Phase 29
closes that gap. `scripts/drift-audit.sh` now carries three §19 checks:

1. **Every `SKILL.md` parses against the agentskills.io spec.** The
   `internal/skillcheck` Go validator (with golden-tested fixtures for
   each violation class) is invoked over the `skills/` tree; a
   malformed `SKILL.md` fails drift-audit, which fails CI.
2. **Every `dockyard` CLI verb has a referencing skill or docs page.**
   The hook reads the verbs from `internal/cli/root.go` (the
   `root.AddCommand(newXxxCmd)` registration block, the canonical
   composition point) and asserts each verb is mentioned in either
   `skills/` or `docs/site/`. A new verb without docs trips the hook.
3. **Every shipped template has a docs walkthrough.** The hook walks
   `templates/*/` and asserts each one whose `builtin.go` exists (the
   canonical "this template is shipped" marker, D-128)
   has a `docs/site/getting-started/<name>.md` page.

The hook short-circuits when `skills/` or `docs/site/` is absent so it
remains correct across the lifetime of the repo — pre-Phase-29 commits
do not retroactively fail. The malformed-fixture path is exercised by
`scripts/smoke/phase-29.sh` so a future regression in the validator
fails the smoke check, not just the silent §19 hygiene.

**Why not a lint plugin / `go generate` hook.** The `drift-audit` script
is the project's existing convention for mechanical design-coherence
checks (mirror, phase-plan↔smoke pairing, RFC references, brief
references). Adding the §19 checks there keeps one entry point, one
failure shape, and one CI step. A separate lint plugin would fragment
the surface contributors look at when they hit a drift failure.

**Knock-on rule.** A PR that changes user-facing surface and skips the
skill / docs update fails the local pre-commit hook (which runs
`make drift-audit`) and the CI `drift-audit` job. The §14 pre-merge
checklist explicitly references this rule.

---

## D-139 — Pre-publish workflow: a fresh scaffold needs `go mod tidy` + `dockyard generate` once

**Date:** 2026-05-23
**Status:** Settled (documented behaviour, not new design)
**Where it lives:** Documented in `skills/scaffold-a-server/SKILL.md`,
`docs/site/getting-started/index.md`,
`docs/site/getting-started/analytics-widgets.md`,
`docs/site/getting-started/approval-flows.md`.

**Observation.** During Phase 29's live-skill validation, a freshly
scaffolded project (blank or template) needs two one-time post-scaffold
steps before `dockyard validate` / `go test` will pass:

1. `go mod tidy` — the generated `go.mod` carries a `replace` directive
   to the local Dockyard checkout (D-080), but no `go.sum`. The
   first `go test` / `go run` would otherwise fail to resolve transitive
   deps.
2. `dockyard generate` — a **template** scaffold (not the blank one)
   ships the Go contracts but not the generated JSON Schema + TypeScript;
   `dockyard validate` flags them as stale until `dockyard generate` has
   run once. The blank scaffold ships the generated artifacts pre-built
   so the first `dockyard validate` is already clean.

**Decision.** Surface both steps in the published docs and the
`scaffold-a-server` skill so a developer following either path lands at
a green `dockyard validate` on the first try. The scaffold pipeline
itself is **not** changed in Phase 29 (changing it would touch the
template engine and the codegen step ordering — outside scope; the right
place is a future scaffold-quality pass). Phase 29's bar is "the skills
work end-to-end as written"; documenting the two extra commands gets
the bar.

**Knock-on.** A future scaffold-pipeline improvement that auto-runs
`go mod tidy` and `dockyard generate` at scaffold time will supersede
this decision — at that point, remove the two extra steps from the
skill + docs in the same PR (§19 hygiene).

---

## D-140 — `internal/clidocs` renders the CLI reference page from the cobra command tree

**Date:** 2026-05-23
**Status:** Settled
**Where it lives:** `internal/clidocs/`, `docs/site/cli/index.md` (the
generated output), `Makefile` `docs` target, AGENTS.md §19.

**The decision.** The published docs site's CLI reference page
(`docs/site/cli/index.md`) is **generated** from the cobra command tree
by a small in-repo helper (`internal/clidocs.Render`, with a tiny
`cmd/clidocs/main.go` driver). `make docs` invokes the helper before the
VitePress build so the page is regenerated on every site build. The
output is deterministic — given the same command tree, the generated
bytes are identical — so a rerun with no CLI surface change produces no
diff.

**Why generate, not hand-write.** A hand-maintained CLI reference would
drift the first time a flag was added without a docs update. Cobra
already owns every verb's `Short`, `Long`, `Use` and flag list as the
single source of truth; the helper just renders it. The §19 hygiene
rule for CLI verbs is partially enforced by the
D-138 drift-audit hook (verb has a skill or docs page); for
flag-level drift, the auto-render closes the gap so a developer literally
cannot ship a CLI change that leaves the page stale (the new flag will
appear in the rebuilt page; a removed flag will disappear).

**Hidden flags excluded.** The renderer filters `cobra.Flag.Hidden` so
internal pre-publish seams (notably `dockyard new --dockyard-path`,
D-080) do not appear in the public docs.

**Knock-on.** A future contributor adding a verb only needs to land
the verb's `internal/cli/<verb>.go` file and the `root.AddCommand` line;
`make docs` regenerates the page automatically. The §14 pre-merge
checklist already requires running `make preflight` / `make drift-audit`
before merge — the docs site's CI workflow runs `make docs`, so a
divergent commit-hand-edit of `docs/site/cli/index.md` is also caught.

---

## D-141 — Skills + docs site live alongside repo source under `skills/` and `docs/site/`

**Date:** 2026-05-23
**Status:** Settled
**Where it lives:** AGENTS.md §3 (the repository-layout invariant — the
two new top-level / under-`docs/` paths Phase 29 introduces), AGENTS.md
§19, `docs/plans/phase-29-skills-docs.md`.

**The decision.** The Agent Skills tree lives at the repo root under
`skills/`; the published documentation site lives under `docs/site/`.
Both are tracked in-tree and version-controlled with the source code
they document.

**Why in-tree, not a separate docs repo.** Three reasons.

1. **§19 hygiene is a single-PR rule.** "Updating the affected skill(s)
   and docs in the same PR" is only mechanically enforceable when the
   skills and docs live with the source — a separate repo would split
   the change across PR boundaries, defeating the rule.
2. **The transclusion model.** The reference pages (`docs/site/reference/`)
   transclude the canonical RFC / plans / decisions / glossary / design
   conventions via VitePress's `<!--@include: …-->`. Cross-repo transclusion
   would require a build step copying files between repos, which is the
   exact drift surface §19 forbids.
3. **Discoverability.** A developer reading `skills/` in the repo gets
   the same instructions an agent gets when it loads them. There is one
   source.

**Why `docs/site/` instead of a new top-level `site/`.** The repo's
documentation already lives under `docs/`; nesting `site/` under it
keeps the layout coherent and discoverable. The `docs/site/.vitepress/`
prefix marks it as a built artefact directory clearly.

**Why `skills/` at the root, not under `docs/`.** The agentskills.io
specification expects discovery of skills at a directory root (an agent
points at `skills/` as its scan root). Putting them under `docs/` would
work but obscure the convention. Keeping the layout flat at the root
matches every other skills-bearing repo in the ecosystem.

**Knock-on.** AGENTS.md §3's layout table is updated in this PR to list
both new paths.

---

## D-142 — VitePress dead-link checking is the §19 fail-fast for docs page references

**Date:** 2026-05-23
**Status:** Settled
**Where it lives:** `docs/site/.vitepress/config.ts` (`ignoreDeadLinks: false`),
`.github/workflows/docs.yml` (the build job that runs on every PR).

**The decision.** VitePress's `ignoreDeadLinks` option is left at its
strict default (`false`) so an internal link to a page that no longer
exists fails the docs build. The CI `docs` workflow runs the build on
every PR — not only on `main` — so a PR that breaks a docs page surface
is caught before merge, not after deploy.

**Why this is part of §19.** §19's promise is "a PR that changes
user-facing surface updates the affected docs in the same PR." A removed
docs page is a user-facing surface change too — and the strictest catch
is the link-check: any other page that linked to the removed page now
fails the build. Combined with the D-138 drift-audit hook
(every CLI verb has a referencing surface, every template has a
walkthrough), this gives the §19 hygiene rule two complementary teeth:
drift-audit catches missing surfaces; the docs build catches broken
references between surfaces.

**Knock-on.** A contributor who wants to remove a docs page for a real
reason (e.g. a removed surface) must, in the same PR, remove every
inbound link. The build will tell them which ones.

---

## D-143 — The MCP spec-compliance conformance suite lives under `test/conformance/` with fixture-side citation headers

**Date:** 2026-05-24
**Status:** Settled (Phase 27)
**Where it lives:** `test/conformance/` (the package), `test/conformance/fixtures/`
(the JSON fixtures), `docs/specifications/` (the vendored spec snapshots the
fixtures cite).

**The decision.** Dockyard's MCP spec-compliance conformance suite (RFC §16)
lives in a top-level `test/conformance/` package, parallel to
`test/integration/`. Each conformance test rounds-trips a Dockyard wire
shape through `internal/protocolcodec` against a fixture that lives in
`test/conformance/fixtures/` and carries a `_cite` field at its top level.
The `_cite` field names the vendored spec snapshot section the fixture is
derived from, including the snapshot's pinned commit SHA + date — so the
source of truth for every conformance assertion is one grep away from the
fixture, and a spec bump that changes a wire shape surfaces as a diff in
both the snapshot AND the fixture (never silent).

**Why a separate package, not folded into `internal/protocolcodec/golden_test.go`.**
The codec's existing golden tests assert one expected wire string for one
constructed input — the encoder's canonicalization made concrete. The
conformance suite drives the codec from the OUTSIDE: given a spec-canonical
JSON input, decode it, re-encode it, and assert the round-trip is byte-stable
against the same fixture. The two suites complement each other — the golden
tests pin the encoder's emission; the conformance suite pins the codec's
spec-compliance — and live in different files so a contributor running
`go test ./test/conformance/...` gets the spec-side bar without the golden-
side coupling, and vice versa.

**Why `_cite` instead of a docstring.** A JSON fixture is data — it cannot
hold Go-side comments. A `_cite` field with a fixed key keeps the citation
co-located with the data and machine-strippable. The fixture loader strips
`_cite` recursively before the byte comparison, so the citation does not
participate in the assertion — only the wire shape does.

**Knock-on.** A future spec bump (e.g. Apps 2027-xx-xx) lands as: (1) the
vendored snapshot is updated in `docs/specifications/` with new SHA + date;
(2) a new codec version is registered in `internal/protocolcodec/version.go`;
(3) new fixtures land in `test/conformance/fixtures/` with `_cite` headers
naming the new snapshot section; (4) any deprecated shape gains a "tolerate
on read, never emit" conformance test on the new codec. The cycle stays
diffable, just like RFC §16 promises.

---

## D-144 — The inspector's "read-only" claim is re-cast as "operator-initiated only" (clarifies D-099, D-103, D-131, D-134)

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — security re-audit)
**Where it lives:** `internal/inspector/doc.go` (the package documentation),
`test/integration/phase27_inspector_security_test.go` (the bounded
`mcp.NewClient` audit), this entry.

**Why.** The inspector's pre-Phase-24 doc claim was "read-only" — true while
D-099's `obs/v1` relay and D-103's `resources/list + resources/read` were
the only client-shaped operations. After Phase 24's D-131 (operator-
initiated `tools/call`) and Phase 25's D-134 (operator-initiated
elicitation `tasks/result`) landed, the claim overpromised: a `tools/call`
that runs an arbitrary handler is not read-only at the protocol level, even
when the inspector is dev-gated and localhost-only. Phase 27's security
re-audit replaces the overpromise with a more precise framing.

**The decision.** The inspector is **operator-initiated only**, not
"read-only". Every client-shaped operation the inspector performs has:
(a) a named operator UI trigger (a button click in the localhost-bound web
frontend); (b) a short-lived per-request MCP client session, never a
long-lived client; (c) a documented decision entry (D-099 / D-103 / D-131
/ D-134) explaining why the operation stays within P4. The package doc
makes this framing explicit, the "no production MCP client" rule is
preserved (P4 still holds — the inspector is the lone client-shaped
component, dev-mode-gated, localhost-bound), and the new framing is
mechanically guarded by the Phase 27 inspector security re-audit:
`test/integration/phase27_inspector_security_test.go` walks the production
source tree, finds every `mcp.NewClient` call site, and asserts each is in
a bounded allow-list (the inspector's three client surfaces + the
`installpkg` boot check, D-088). A new production `mcp.NewClient` outside
that allow-list fails the audit before it can merge.

**Why this stays within P4.** The framing change is editorial honesty —
the inspector's surface is unchanged. It is still:

- The lone client-shaped component (no new package, no production client).
- Dev-mode-gated, localhost-bound by `requireLoopback` (further hardened
  in D-145).
- Driven only by an operator's explicit UI action, never by an off-localhost
  actor or the server.
- Short-lived per request — no long-lived client state ever.

The inspector is still not a production MCP client — every clause of P4
holds. The new framing makes the read/write boundary precise.

**Knock-on.** A future inspector PR that adds a new client-shaped surface
(another `mcp.NewClient`) must add an allow-list entry in
`test/integration/phase27_inspector_security_test.go` AND file a
superseding-or-extending decision in the same PR — same shape as D-131
and D-134.

---

## D-145 — `requireLoopback` rejects whitespace-padded ports as well as non-loopback hosts (Phase 27 hardening)

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — security audit defect fix)
**Where it lives:** `internal/inspector/inspector.go` (the `requireLoopback`
function), `internal/inspector/inspector_security_test.go` (the bind-shape
adversarial sweep).

**Why.** The Phase 27 inspector security re-audit surfaced a latent defect
in the localhost-only gate: `net.SplitHostPort` tolerates whitespace inside
the port component, so an address like `"127.0.0.1:0 "` (trailing space)
passed `requireLoopback`'s host check, then failed at `net.Listen` with an
opaque `listen tcp: lookup tcp/0 : unknown port` error. The bind was still
rejected — there was no off-localhost reachability — but the operator's
error class was inconsistent across malformed-address shapes: a
non-loopback IP got the typed `ErrNonLoopbackBind`, while a whitespace-
padded port got a transport-layer error.

**The decision.** `requireLoopback` now validates the port string is a
clean numeric value (digits only, no whitespace, no non-digit characters)
via the new internal helper `isCleanPort`. A malformed port surfaces as
`ErrNonLoopbackBind` — the same typed class every other malformed-address
shape uses. Empty port (the "OS-assigned port" shortcut) is unchanged
and still accepted.

**Why not parse the port as an integer.** A `strconv.Atoi` would catch
the same set of malformed shapes but would also accept negative numbers
or leading-zero forms; `isCleanPort`'s "digit characters only" rule is
the safest minimal check. The empty-port shortcut is preserved
because Go's `net.Listen` treats it as "ask the OS for a port" and the
inspector's `defaultAddr` already depends on that semantics.

**Knock-on.** The change is a defect fix, not a behaviour change for
legitimate callers — every previously-accepted address remains accepted.
Operator-visible error classes are now consistent across malformed-
address shapes, which improves the diagnostic quality of the
`ErrNonLoopbackBind` typed error.

---

## D-146 — `make coverage` captures `go test` output to a temp log and surfaces it on failure (CI diagnostic hygiene)

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — folded-in CI hygiene fix)
**Where it lives:** `Makefile` (the `coverage` target), `.gitignore`
(the `coverage-test.log` artifact ignore), `scripts/smoke/phase-27.sh`
(asserts the `>/dev/null` foot-gun is gone).

**Why.** The pre-Phase-27 `make coverage` recipe redirected `go test`
output to `/dev/null` unconditionally:

    CGO_ENABLED=1 go test -race -covermode=atomic \
        -coverprofile=coverage.out ./... >/dev/null && \
    go run ./internal/coveragecheck/cmd/coveragecheck ...

A clean run stayed quiet, which was the point. But a failure — including
a CI flake — surfaced as a bare `make: *** [coverage] Error 1` with no
indication of which test failed or why. The PR #49 episode the user
flagged was exactly this: an intermittent coverage-step failure on CI
that was undiagnosable from the run log alone.

**The decision.** `make coverage` now tees `go test`'s output to a temp
log. On success the log is removed (the run stays as quiet as before).
On failure the recipe (a) copies the log to a stable filename
`coverage-test.log` (so a CI artifact-upload step picks it up), (b)
prints the full log inline (so the failing test name + assertion appears
in the CI step's standard log without requiring artifact navigation),
and (c) exits with the original go-test status code.

The `coverage-test.log` artifact is gitignored. The Phase 27 smoke
script asserts the recipe no longer carries the `go test … >/dev/null`
foot-gun — a regression that re-introduced it would fail Phase 27's
own smoke check.

**Why not unconditional inline output.** A clean run prints zero test
output, which is the contract every Phase-21.5-onwards contributor
expects (`make coverage` is the band-check, not a re-run of every test).
Tee'ing to a temp + printing-on-failure preserves the quiet-success
property AND fixes the diagnostic gap.

**Knock-on.** This is the same R4 / Phase 24-finish pattern the `make
web` recipe already followed (`tee` + print-on-failure). Other
`make` targets that suppress output the same way are candidates for
the same treatment; none have surfaced an incident yet, so they are
left for a future hygiene pass.

---

## D-147 — The inspector mux fuzz target exercises every endpoint with arbitrary bodies (Phase 27 hostile-input sweep)

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — hostile-input fuzz sweep)
**Where it lives:** `internal/inspector/fuzz_test.go`
(`FuzzInspectorMux`), `scripts/smoke/phase-27.sh` (asserts the target
exists).

**Why.** Phase 27's hostile-input sweep (sub-goal A) requires every
wire-decoding surface in Dockyard to have a fuzz target with a
meaningful seed corpus and a "never panic" invariant. The inspector's
HTTP mux is a wire-decoding surface — it accepts operator-supplied
request bodies on two POST endpoints (`/api/tools/invoke`,
`/api/tasks/elicitation`) and a `requireLoopback`-gated path enumeration
otherwise. Before Phase 27 only the codec / manifest / codegen / tool-
argument decoders had fuzz coverage; the inspector mux did not.

**The decision.** `FuzzInspectorMux` constructs a real `newMux` over
stubbed Invoker and Elicitor sources, then drives every endpoint in
a fixed enumeration with the fuzz-supplied body. The invariant is
panic-freedom: any status code (200 / 400 / 415 / 503 / 502 / 404) is
acceptable; only a panic is a fuzz failure. The fuzz harness
specifically also cancels the request context immediately so the
streaming endpoints (`/api/obs/stream`, `/api/rpc/log`) return
promptly rather than blocking the fuzzer.

**Why a stub Invoker / Elicitor.** The fuzz target is a parse-surface
audit, not an end-to-end test. Stubbing the mutating sources to
"always succeed" exercises the full handler path (body decode →
field validation → dispatch) without depending on a live MCP server
the fuzzer cannot spin up. The end-to-end behaviour is covered by
integration tests; the fuzz target's bar is the parse surface.

**Knock-on.** Any new inspector HTTP endpoint added in the future must
land its endpoint in the `endpoints` slice of `FuzzInspectorMux` in
the same PR. The Phase 27 smoke check asserts the fuzz file exists
with at least one `Fuzz*` target; the line-level check is a future
hygiene improvement.

---

## D-148 — The Tasks JSON-RPC frame parser has a dedicated fuzz target with a live engine (Phase 27 hostile-input sweep)

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — hostile-input fuzz sweep)
**Where it lives:** `runtime/tasks/fuzz_test.go` (`FuzzMountHandleFrame`),
`scripts/smoke/phase-27.sh` (asserts the file exists).

**Why.** The Tasks transport mount is the wire surface where Dockyard
adds frame-routing on top of the SDK's JSON-RPC dispatch table (D-108 /
D-110). Before Phase 27 the codec's wire types had fuzz coverage
(Phase 21.5) but the mount's `HandleFrame` parser — which receives
attacker-influenced bytes off the streamable-HTTP transport when a
Tasks engine is attached — did not. Phase 27's audit closes that gap.

**The decision.** `FuzzMountHandleFrame` builds a real `tasks.Engine`
over `NewInMemoryStore` with `AdvertiseList=true` +
`RequestorIdentifiable=true` so the full method-routing surface
(tasks/get, tasks/result, tasks/cancel, tasks/list, the Dockyard-
internal `dockyard/tasks/supplyInput`) is reachable from the fuzzer.
The invariant under fuzz is uniform: `HandleFrame` NEVER panics on any
input. A frame-decoding error, an unknown-method error, a typed
dispatch error, or a "not handled" pass-through are all acceptable;
only a panic is a fuzz failure. The fuzz harness pre-cancels its
context so a tasks/result frame that would otherwise block does not
hang the fuzz session.

**Why a live engine, not a stub.** The parser dispatches to the engine
in the SAME goroutine that decoded the frame, so a stub would miss any
panic the engine introduces under adversarial input (a malformed
SupplyInputParams, a hostile-large taskId). A live in-memory engine is
cheap and exercises the full path.

**Knock-on.** A future Tasks method (vendor-prefixed or otherwise) is
automatically covered as long as it routes through `Engine.Dispatch` /
`Engine.DispatchAs`; the harness re-uses the same mount and engine. A
spec bump that changes the Tasks wire envelope adds a new fuzz seed
to the corpus in the same PR.

---

## D-149 — The HTTPSecurity stress test uses Stateless=true so the goroutine-leak sentinel is meaningful

**Date:** 2026-05-24
**Status:** Settled (Phase 27 — HTTPSecurity stress)
**Where it lives:** `test/integration/phase27_httpsecurity_stress_test.go`
(`TestPhase27_HTTPSecurity_StressUnderAdversarialLoad`).

**Why.** The Phase 27 HTTPSecurity stress test (sub-goal B) drives ≥20
concurrent clients × 30 requests against a real
`runtime/server.HTTPHandler` with the explicit `DefaultHTTPSecurity`
posture and a real `tasks.Engine` attached, then asserts no panic, no
incorrect rejection, and no goroutine leak past a settle window. In the
default stateful mode, the SDK's streamable-HTTP transport retains a
per-session goroutine for a session-cleanup window AFTER the client has
disconnected — so 600 sequential initialize POSTs leave ~150-200
goroutines alive when the test's settle window expires. That is a
legitimate SDK behaviour, not a Dockyard leak, but it would make the
goroutine-leak sentinel impossible to tune sensibly.

**The decision.** The stress test constructs the HTTP handler with
`Stateless: true`. Each POST is then served as an ephemeral session
that initializes, dispatches, and tears down within the request
lifetime — the cleanup window is zero, and the goroutine count
settles to baseline (within a generous tolerance) inside seconds. The
tolerance (50 extra goroutines past baseline) is wide enough to absorb
test-harness goroutines (the `httptest` server, the `http.Client`
pool) and narrow enough to detect a real leak in `runtime/server` or
the Tasks mount.

**Why not a stateful run.** The stateful path is exercised by the
existing Wave 3 tests and the Phase 24 + 25 end-to-end runs. The
Phase 27 stress test's bar is the SECURITY posture under adversarial
concurrency, not the SDK's session-lifecycle behaviour. Using
`Stateless: true` keeps the test focused on the layers Dockyard owns
(the explicit security middleware + the Tasks mount) without entangling
the goroutine assertion with SDK session-cleanup timing.

**Knock-on.** A future Phase that needs to stress-test the stateful
session path adds a sibling test with `Stateless: false` and a
session-aware goroutine-leak sentinel (e.g. polling until the SDK's
session-cleanup interval elapses). That is out of scope for Phase 27.

---

## D-150 — Phase 28 worked examples ship under `examples/`, NOT as `dockyard new --template` entry points

**Date:** 2026-05-24
**Status:** Settled (Phase 28 — worked examples)
**Where it lives:** `examples/backend-tools-only`,
`examples/combined-patterns`, `examples/prompts-demo`,
`scripts/drift-audit.sh` (the §19 hook extension that enforces the
README + docs-link requirement), `docs/site/getting-started/examples.md`.

**Why.** Phase 28 ships three worked examples that complement the two
shipped templates (`analytics-widgets`, `approval-flows`). The natural
question: should they also be `dockyard new --template <slug>` entry
points, embedded into the binary the way `internal/scaffold/builtin.go`
imports the template `builtin.go` files? Two reasons say no.

First, **templates and examples answer different developer questions.**
A template answers "I want to start a new server with this shape — give
me the scaffold." An example answers "I want to see this pattern in
context — show me the code, the wiring, the tests." A developer reads
an example; they scaffold a template. Embedding the examples into the
binary would conflate the two affordances, and the `dockyard new
--template` flag already has a clear contract — one entry per shipped
template.

Second, **examples grow more freely than templates.** A template is a
contract: a developer who scaffolded with v1.0 expects v1.1 to keep
the same shape; a substantive divergence is a breaking template change.
Examples have no such contract — a developer reads them at one point
in time. Keeping them out of the binary lets new examples land without
the scaffold-substitution discipline templates require.

**The decision.** Examples live under `examples/<slug>/` as standalone
reference projects, share the root `go.mod`, build with `go build
./examples/<slug>/...`, and are NEVER embedded into the binary. The
`dockyard new` verb is unchanged — its `--template` flag continues to
take only the two shipped template names.

**Why under the root module, not nested modules.** A nested
`examples/<slug>/go.mod` with a `replace` directive would mirror what
`dockyard new` materialises today (a generated project with a
`replace github.com/hurtener/dockyard => …` line). The downside: nested
modules are skipped by `go test ./...`, so the examples' contract
tests would not be part of the in-tree gate. Living under the root
module makes the examples first-class members of the test + coverage
matrix — a runtime API change that broke an example would fail CI in
the same PR.

**Knock-on.** The drift-audit §19 hook is extended to enforce every
shipped example has a README and is referenced from
`docs/site/getting-started/examples.md` (the canonical link target).
A "shipped example" is identified by the presence of a `cmd/server/`
subdirectory — mirrors the templates' `builtin.go` marker.
`examples/customer-health/` is the RFC §4.2 manifest reference fixture
(consumed by `test/integration/wave2_test.go`); it has no `cmd/server/`
and is intentionally exempt from the hook.

---

## D-151 — `runtime/server.AddPrompt` is a focused, registration-only API; obs/v1 carries `prompt.get` with a resource.read-shaped payload

**Date:** 2026-05-24
**Status:** Settled (Phase 28 — prompts surface)
**Where it lives:** `runtime/server/prompt.go` (the API),
`runtime/server/prompt_test.go` (the tests), `runtime/obs/payload.go`
(PromptGetPayload), `runtime/obs/recorder.go` (Recorder.PromptGet),
`runtime/obs/event.go` (KindPromptGet existed since Phase 15 but had
no carrier), `examples/prompts-demo/` (the worked example).

**Why.** Dockyard supports the MCP Tools primitive end to end —
contract-first input/output, the runtime/tool builder, the
inspector's Tools panel, every fixture state. The MCP Prompts primitive
the SDK supports via `mcp.Server.AddPrompt` was reachable via the SDK
but unused in Dockyard: no Dockyard-flavored API, no obs/v1 carrier,
no example. Phase 28 closes the gap with the **minimum** scope that
makes prompts a first-class Dockyard surface without straying into
prompts-API-design.

**The decision.** `runtime/server.AddPrompt(s *Server, def PromptDef,
fn PromptHandler) error` registers a prompt. PromptDef mirrors the
SDK's mcp.Prompt fields (Name, Title, Description, Arguments) but uses
a typed Dockyard `PromptArgument` so the runtime API never exposes the
raw SDK type (RFC §5.4, P3). PromptHandler signature is
`func(ctx, PromptRequest) (PromptResult, error)`. The handler is
panic-recovered with the same `guardHandler` mechanism every tool +
resource handler routes through (AGENTS.md §5, §13, D-053). Each
prompts/get invocation emits an obs/v1 `prompt.get` start+end pair via
`Recorder.PromptGet` — payload shape mirrors `resource.read`
(name + message count + serialized byte size) rather than `tool.call`
(full input/output capture), because a prompt's "input" is a flat
argument map and its "output" is a rendered message list — neither
benefits from the tool.call capture-policy machinery.

A new `runtime/server` field — `prompts []string` — tracks
registration order; the `Server.Prompts()` accessor mirrors
`Server.Tools()`. A new logbridge helper —
`withPromptRequestSession` — stamps obs.WithSession from the prompt
request's session id so emitted events carry SessionID.

**Why not a contract-first prompts builder.** See D-152.

**Why a defensive recover() around `mcp.Server.AddPrompt`.** The
current SDK's AddPrompt does not return an error and does not panic.
Mirrors `addToolSafe`'s pattern: a future SDK that panics on a bad
registration becomes a swallowed registration rather than a crash
during server assembly. The current SDK never panics here; the
recover() is for forward-compatibility, not present-day need.

**Knock-on.** The inspector's panels do not (yet) render Prompts —
Phase 23 scope was tools / resources / Tasks. The `prompts-demo`
example's README documents this gap and points at a Prompts-aware
host (Claude Code, an MCP CLI) for the visible demo. Adding a
Prompts panel to the inspector is a post-V1 candidate, governed by
the same operator-initiated-only (D-144) framing the existing
panels are.

---

## D-152 — Contract-first does NOT extend to prompts; AddPrompt is registration-only

**Date:** 2026-05-24
**Status:** Settled (Phase 28 — prompts surface)
**Where it lives:** `runtime/server/prompt.go` (the API + the comment
documenting the constraint), `runtime/server/doc.go` (the Phase 28
prompts paragraph), `docs/decisions.md` (this entry).

**Why.** Dockyard's contract-first guarantee — typed Go struct → JSON
Schema, the schema the host sees IS the Go type — is the framework's
spine (P1, RFC §6). The natural question for any new primitive: does
contract-first apply? For tools (Phase 04) yes. For resources (Phase
07) the URI is the contract and the content is opaque bytes; the JSON
Schema layer is not the right model. For prompts, the picture is
similar but distinct.

The MCP spec types `Prompt.Arguments` as a flat list of named string
fields — each argument is just `{Name, Title?, Description?, Required?}`.
A host calls `prompts/get` with a `map[string]string` argument map; the
handler returns a list of rendered messages. There is no structured
argument object the way `tools/call` has a typed `Arguments` payload —
no JSON Schema fits naturally over a flat string map (every field has
type "string"; the structure has no nested shape).

A "contract-first prompts builder" would either:

- Force the developer to declare a Go struct whose fields are all
  strings, generate a trivial schema, and accept that the schema
  carries no real validation power (a contract for the sake of having
  one); or
- Invent a Dockyard-specific richer prompt shape and lower it onto the
  SDK's flat-string shape on the wire, introducing a Dockyard-vs-MCP
  asymmetry (a P3 violation: the runtime API should not expose a
  shape the wire does not).

Neither is worth the API surface. **The decision** is the second
path's negation: AddPrompt is a registration-only pass-through that
exposes the SDK's flat-string argument shape verbatim (under
Dockyard-flavored types so the SDK struct does not leak through). A
developer who needs a structured argument shape registers a **tool**
instead — the contract-first pipeline applies there. A developer who
just needs a curated template uses AddPrompt with string arguments —
the MCP-native shape.

**Why this is consistent with P1.** P1 says "Contract-first: a tool's
input and output are typed Go structs; JSON Schema, TypeScript types,
and fixtures are generated, never hand-written." It scopes to tools.
Prompts are a sibling primitive with a different shape; P1's
"generated, never hand-written" guarantee does not generalise to a
primitive that has nothing to generate.

**Knock-on.** A future Dockyard phase that wants richer prompts (e.g.
a Dockyard-side template engine that fills slots with structured
data before rendering) would build it ABOVE AddPrompt — a Dockyard
helper that takes a typed struct, fills a template, and calls
AddPrompt with the rendered messages. The runtime API stays at the
spec-shaped layer; helpers live above it.

---

## D-153 — `scripts/drift-audit.sh` §19 hook extends to `examples/`: every shipped example needs a README + a docs-site reference

**Date:** 2026-05-24
**Status:** Settled (Phase 28 — drift-audit extension)
**Where it lives:** `scripts/drift-audit.sh` (the `6d.` block),
`docs/site/getting-started/examples.md` (the canonical link target),
`scripts/smoke/phase-28.sh` (the smoke check that asserts the hook
exists).

**Why.** The §19 hook (D-138) mechanically enforces docs hygiene
around the surfaces Phase 29 made publishable: CLI verbs, shipped
templates, the SKILL.md format. With Phase 28's three worked examples,
the same drift risk applies to `examples/`: an example without a
README is in-flight work or cruft, and an example whose README is not
linked from the docs site is unreachable from a developer's first
read. The §19 rule already mandates the hygiene in prose; the hook
mechanises it.

**The decision.** A new `6d.` block in `scripts/drift-audit.sh` walks
every `examples/<slug>/` directory whose name marks it as a buildable
example (a `cmd/server/` subdirectory — the analog of the templates'
`builtin.go` marker). For each such directory, it requires:

1. `examples/<slug>/README.md` exists.
2. The slug appears in `docs/site/getting-started/examples.md`
   (the canonical examples index page).

A new example without either fails drift-audit and the PR cannot
merge. The check intentionally does NOT require an individual
docs-site walkthrough for each example (templates have that
requirement; examples are explicitly the "read the code + the README"
surface — the index page is the single authoritative reference). This
keeps the cost of adding an example low.

**Why a `cmd/server/` marker, not a `builtin.go` marker.** Examples
are explicitly NOT embedded in the binary (D-150), so they have no
`builtin.go`. The `cmd/server/` directory is the canonical entrypoint
shape Phase 28's three examples standardised; it is the smallest
mechanical signal that distinguishes a buildable example from a
fixture-only directory.

**Why the exemption matters: `examples/customer-health/`.** That
directory has been in-tree since Phase 06 as the RFC §4.2 manifest
reference fixture, consumed by `test/integration/wave2_test.go` to
verify the manifest loader against the published example shape. It
has no `cmd/server/` and is intentionally not a buildable example;
the hook's `cmd/server/` marker correctly skips it without an
allow-list entry.

**Knock-on.** A future post-V1 example that wants its own
docs-walkthrough page (a more involved demo, a multi-part tutorial)
adds the page under `docs/site/getting-started/<slug>.md` and the
sidebar entry in the VitePress config — same shape the templates'
walkthroughs use. The hook continues to require only the index-page
reference; the walkthrough is additive.

---

## D-154 — `CHANGELOG.md` follows Keep a Changelog and frames every release by the four binding properties

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `CHANGELOG.md` (the file itself + its preamble),
`docs/RELEASING.md` (the "pre-flight" + "post-release verification"
sections that depend on the format), `internal/changelogx`
(the parser + extractor — the workflow contract that depends on the
file's heading shape).

**Why a hand-authored Keep-a-Changelog file, not a generated one.**
The natural alternatives were:

- A Conventional-Commits-generated changelog (parse PR titles since
  the previous tag, emit a `### Changed` / `### Fixed` block).
- A phase-by-phase log (one section per Phase NN).

Both are wrong for the v1.0.0 cut. A generated CC log is a phase-by-
phase diary in another shape — it lists what landed, not what the
release means. A phase log is the institutional memory the project
already has in `docs/plans/` and `docs/decisions.md` — duplicating it
adds noise without adding signal. The v1.0.0 entry is the
developer-meets-V1 story; the right frame is the four binding
properties (P1 contract-first, P2 obs as a protocol, P3
forward-compatibility by isolation, P4 server-side only — RFC §1).

**The decision.** `CHANGELOG.md` follows
[Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/): a
top-level title + preamble, an `## [Unreleased]` section, then one
`## [<version>] - <YYYY-MM-DD>` section per release in
reverse-chronological order, with a reference-link footer block at
the bottom. The v1.0.0 entry is framed by P1–P4 as the headline; the
rest of the entry (runtime, CLI, inspector, templates, DX, quality,
deferred, acknowledgements) is the supporting structure. Pre-v1.0.0
history is **not** reconstructed — the phase log is the canonical
record of how the framework got here.

From v1.1.0 onward the format is open to augmentation: a maintainer
may prepend a Conventional-Commits-generated PR list under the
hand-authored prose, but the canonical narrative is hand-authored.
A future workflow extension that auto-appends the CC list is a
recorded V2 follow-up (`docs/V2-BACKLOG.md` —
"Conventional-Commits-generated changelog supplement").

**The heading shape is load-bearing.** The release workflow's
"extract release notes" step calls `internal/changelogx` against
`CHANGELOG.md` and the tag's version; the extractor finds the
matching `## [<version>]` heading and returns the section body. A
malformed heading is a release-time failure (the workflow blocks
the GitHub Release creation), so the parser is golden-tested
against the in-repo `CHANGELOG.md` directly to make a future
authoring change a unit-test failure instead of a release-time
failure.

---

## D-155 — The release pipeline is a tag-triggered GitHub Actions workflow with a `workflow_dispatch` dry-run trigger

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `.github/workflows/release.yml`,
`internal/releasebuild` (the workflow's cross-compile driver),
`internal/changelogx` (the workflow's release-notes source),
`docs/RELEASING.md` (the operational manual the workflow implements).

**The release pipeline shape.** A push of a tag matching `v*` to
the repository triggers the `release` workflow:

1. **preflight** — `make preflight`. The same gate the pre-commit
   hook and CI enforce. A failed preflight stops the release before
   any artifact is built; a failed release is no release, not a
   partial one. (The acceptance bar — CLAUDE.md §4.1.)
2. **build** — drives `internal/releasebuild` against the
   `cmd/dockyard` main package over the RFC §14 cross-compile matrix
   (darwin / linux / windows × amd64 / arm64). Each artifact is a
   CGo-free, statically-linked binary with a per-artifact `.sha256`
   sidecar; an aggregate `checksums.txt` lets a downloader verify
   every artifact in one `sha256sum -c` invocation.
3. **release** — extracts the matching CHANGELOG section via
   `internal/changelogx` and creates / updates the GitHub Release
   via `softprops/action-gh-release`. The action is idempotent on
   re-run; the upstream-recommended path for a release that needs
   re-uploading or a body fix.

**Why also `workflow_dispatch`.** A maintainer needs to verify the
pipeline end-to-end without publishing — a real test of "does the
workflow actually run, does it produce the right shape, does the
gate fire" before pressing the tag-push button. The dispatch path
narrows the matrix to the runner's host target (via the
`releasebuild -host-only` flag) so a dry-run completes in 2–3
minutes; it stops at artifact upload (the artifacts land as a
workflow-run artifact, no GitHub Release is created); the body
extractor looks up the `Unreleased` section instead of the
nominal version. This is the dry-run sub-goal F of the Phase 30
spec.

**Why split the build into preflight / build / release jobs.** Three
isolating reasons:

1. **Caching.** The preflight job's Node + Go cache hits do not
   need to redo themselves in the release job (which only needs
   the artifacts). Splitting keeps each job tight.
2. **Idempotence on re-run.** A failure in the release job can be
   re-run alone (Re-run failed jobs); the build artifacts are
   already uploaded. A monolithic job would re-cross-compile.
3. **Permission scoping.** Only the release job needs
   `contents: write`; preflight + build run as read-only. A
   compromised dependency in the (large) build step cannot escalate
   to write the repo.

**Why `softprops/action-gh-release`.** It is the upstream-
recommended action for this. The release-update-in-place behaviour
is what makes the workflow idempotent against re-runs without
custom logic; the `make_latest` flag does the "tag this as the
'Latest' release in the UI" step; `fail_on_unmatched_files: true`
catches a missed-artifact bug at workflow time.

**Knock-on.** Cosign signing + SLSA-provenance attestation are
deliberate V2 hardening surfaces (`docs/V2-BACKLOG.md` —
"Signed releases + SLSA provenance"). When they land, they slot
into the existing release job rather than reshaping the pipeline.

---

## D-156 — `internal/releasebuild` is a thin driver wrapping `go build` for the cross-compile matrix, separate from `internal/buildpkg`

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `internal/releasebuild/` (the package + its
small CLI under `cmd/releasebuild/`).

**Why a new package, not a new `buildpkg` knob.** `internal/buildpkg`
is the per-project `dockyard build` pipeline: it expects a
`dockyard.app.yaml` at the project root, runs the codegen +
validate + Vite stages, then drives `go build` against a Dockyard
project. The Dockyard repository itself is NOT a Dockyard project
(there is no `dockyard.app.yaml` at the root); it is the CLI's
source tree. Folding the release driver into `buildpkg` would
either pollute the per-project pipeline with a tag-shaped
artifact-naming branch, or make every per-project Build call
optionally drive the release flow. Both shapes are worse than two
small packages with one job each.

**The decision.** `internal/releasebuild` is a small, internal Go
package + CLI that:

- runs its own per-target `go build` of `./cmd/dockyard` (NOT
  `buildpkg.Build`), with `CGO_ENABLED=0` and `GOOS`/`GOARCH` set,
  using the same `-ldflags='-s -w'` flags `make build` uses for
  the dockyard CLI itself;
- names each artifact under the release-publish shape
  (`dockyard-<version>-<os>-<arch>[.exe]`) so the published
  filename carries the version a user is auditing;
- writes a per-artifact `.sha256` sidecar in the
  `sha256sum -c`-compatible line shape (the same convention
  `buildpkg.writeChecksum` uses, so a release artifact and a
  developer-built `dockyard build` artifact verify the same way);
- writes an aggregate `checksums.txt` next to the artifacts, sorted
  by basename for byte-determinism on a re-run.

**What it re-exports.** `releasebuild.Target` and
`releasebuild.DefaultMatrix` are aliases for `buildpkg.Target` and
`buildpkg.DefaultMatrix` — the RFC §14 matrix is one piece of truth;
the release driver consumes it rather than re-inventing it. This is
the same principle behind the existing `runtime/server` /
`runtime/apps` / `runtime/tasks` split: package boundaries by job,
not by data type.

**Why a separate `cmd/releasebuild/` CLI.** The release workflow
needs a callable binary, not a Go API; same shape as `cmd/clidocs`
and `cmd/skillcheck`. The CLI is a thin flag-parser over
`Release(ctx, opts)` — the testable seam lives in the package,
not the CLI.

---

## D-157 — `internal/changelogx` parses Keep-a-Changelog with a stdlib-only parser pinned to the in-repo CHANGELOG

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `internal/changelogx/` (the package, its tests,
its CLI under `cmd/changelogx/`).

**Why a stdlib-only parser, not a third-party CommonMark library.**
The release pipeline's release-body source is the matching
CHANGELOG section. The parsing surface is tiny — find the `## [v]`
heading, return the body between it and the next H2, exclude the
reference-link footer block. A CommonMark library would solve this
plus a hundred problems we do not have; it would also become a
transitive-update silent-behaviour risk against a load-bearing
release-pipeline gate. A small in-tree parser pinned by golden
tests against the in-repo CHANGELOG turns a future authoring change
into a unit-test failure (loud, in PR) rather than a release-time
failure (quiet, at tag push).

**The decision.** `internal/changelogx.ExtractSection(content, version)`
is a pure-functional, read-only stdlib-only parser. It accepts both
`v1.0.0` and `1.0.0` on input (canonicalises the trial set
internally); it returns the body without the H2 heading and without
the reference-link footer; it returns `ErrSectionNotFound` for a
missing version and `ErrMalformed` for a structurally-broken file.
A unit test runs the parser against the actual in-repo
`CHANGELOG.md` to catch a future authoring change before the
release workflow does.

**The CLI is exit-code-aware.** The release workflow branches on
the CLI's exit status: `ErrSectionNotFound` is exit 2 (the most
likely cause is a forgotten CHANGELOG entry — fail the release
cleanly), other errors are exit 1 (a broken CHANGELOG or an IO
fault — investigate before tagging again). This is the same
exit-code-discipline `dockyard validate` follows for its three
diagnostic classes.

---

## D-158 — `docs/V2-BACKLOG.md` is the consolidated post-V1 deferral list; new deferrals land here in the same PR

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `docs/V2-BACKLOG.md`, `docs/RELEASING.md`
(the "What the release workflow does NOT do" section that points
at V2-BACKLOG), `CHANGELOG.md` (the v1.0.0 "Deferred to V2" section
that cross-references it).

**Why one document, not a label / issue tracker / decision-log
walk.** Before Phase 30 the post-V1 backlog was scattered across:

- the master plan's "post-V1 follow-ups" paragraph (RFC §19 + the
  prose at the bottom of `docs/plans/README.md`);
- individual D-NNN entries documenting deferrals (D-088, D-101,
  D-108, D-136, D-139, the analytics-widgets / Claude
  signed-origin synthetic-URL workaround);
- in-code TODO-shaped notes (`internal/testgate/categories.go`'s
  syntheticServerURL block).

A developer asking "what comes next" had no single page to read.
GitHub issues / labels are a credible option but they leave the
repository's documentation incomplete — and the project's
methodology is doc-driven (RFC > plans > AGENTS.md > briefs >
comments). The backlog belongs in the repository's documentation,
keyed off the decisions log.

**The decision.** `docs/V2-BACKLOG.md` is the canonical post-V1
backlog. Every recorded post-V1 deferral lives there with: a
short title; the originating D-NNN (and any related ones); the
deferral rationale; the criteria a future phase / PR would need to
meet to claim it (the "definition of done"). New deferrals land
in V2-BACKLOG in the same PR that records them in
`docs/decisions.md`. A future phase that wants to ship one of the
items cites the V2-BACKLOG line in its plan's
`Files added or changed` section.

**The backlog is themed, not phase-numbered.** Phase numbers close
at 30 (the V1 critical path). A V2 item that ships post-V1 gets
its phase number at planning time; the backlog entry then carries
the assigned phase number so the line stays navigable.

**Knock-on.** The §19 hook is not extended to V2-BACKLOG: the
backlog is not a user-facing surface (no CLI verb, no manifest
field, no template, no public runtime API). It is documentation
hygiene rather than mechanical hygiene; a stale backlog entry is a
reviewer's catch in the next deferral PR.

---

## D-159 — Semver policy post-v1.0.0: major = breaking, minor = additive, patch = fix; obs/v1 shape bump rides with a major

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `docs/RELEASING.md` (the "Semver policy"
section), `CHANGELOG.md` (the preamble that anchors to
`docs/RELEASING.md`).

**The decision.** Dockyard follows
[Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Concretely from v1.0.0 onward:

- **Major** — a breaking change to: a public runtime API
  (`runtime/server`, `runtime/tool`, `runtime/apps`,
  `runtime/tasks`, `runtime/obs`, `runtime/store`); the
  `dockyard.app.yaml` manifest schema; a CLI verb (removed or
  fundamentally reshaped); the `obs/v1` event shape (a structural
  change requires bumping the `obs/v1` schema version per
  AGENTS.md §8, and that bump rides with a Dockyard major); or
  one of the four binding properties (P1–P4) — a P-level change
  is governed by an RFC PR, not just a major release.
- **Minor** — an additive feature: a new CLI verb, a new runtime
  API, a new manifest field with a sensible default, a new
  template, a new agent skill. Existing users see no behaviour
  change.
- **Patch** — a bug fix, a documentation fix, a security fix, a
  dependency bump that does not change Dockyard's behaviour.
  Existing users see no API or wire change.

Pre-releases follow semver's pre-release form (`v1.1.0-rc.1`,
`v2.0.0-beta.1`); the release pipeline accepts them and marks the
GitHub Release as pre-release (not "Latest").

**Why "lean breaking" in doubt.** A surprise breakage in a minor
release is worse than a major version bump a user accepts cleanly.
The "lean breaking" rule sidesteps the temptation to slip a
mild-looking incompatibility into a minor because "few users will
notice"; few-users-notice breakages are still breakages.

**Why `obs/v1` shape bump rides with a major.** AGENTS.md §8
already requires that an `obs/v1` event-shape change is a
versioned, documented `schema_version` bump (D-074). The wire
shape is part of Dockyard's public contract; bumping it without a
Dockyard major would surprise an `obs/v1` consumer (the inspector,
the post-V1 multi-server console, an OTel exporter that reads the
event shape). The rule pairs the two bumps so a consumer can rely
on the Dockyard version to communicate the `obs/v1` shape.

---

## D-160 — The release-pipeline dry-run is captured under `docs/release/v1.0.0/` as the v1.0.0 release artifact

**Date:** 2026-05-25
**Status:** Settled (Phase 30 — V1 release engineering + cut)
**Where it lives:** `docs/release/v1.0.0/` (the captured
transcripts), `docs/plans/phase-30-v1-cut.md` (the acceptance
criterion that requires them), `scripts/smoke/phase-30.sh` (the
smoke check that asserts they exist + are non-empty).

**Why capture the dry-run in-tree.** The release pipeline cannot
be exercised end-to-end against the actual `v1.0.0` tag before
that tag is pushed (a chicken-and-egg condition); the workflow's
correctness has to be proven another way. The Phase 30 spec's
sub-goal F requires a dry-run: building the cross-compile matrix
locally, confirming the binaries boot, running `make preflight`
clean from a fresh worktree. The natural way to make that proof
durable + auditable is to capture the transcripts as part of the
release artifact set.

**The decision.** Three transcripts + an index land under
`docs/release/v1.0.0/`:

- `cross-compile-matrix.txt` — the output of the local
  cross-compile dry-run (the same matrix the workflow drives,
  produced by the same `internal/releasebuild` driver).
- `binary-help.txt` — the `--help` output of one of the
  cross-compiled artifacts, confirming the binary boots.
- `preflight.txt` — the `make preflight` output from a clean
  checkout (the release-gate the workflow runs).
- `README.md` — a one-paragraph index explaining what the captures
  are + when they were produced.

The smoke script asserts each file is present and non-empty; a
future release-engineering pass that supersedes Phase 30's
dry-run posture (a real release-pipeline run against a tag the
first time) replaces these artifacts with the actual run's
transcripts — same shape, real source.

**Why not a workflow-side step that writes the transcripts.** A
workflow-side `gh release upload …` of the workflow's own log is
plausible, but the workflow's log is already publicly visible on
the run page; duplicating it is noise. The in-tree captures are
the V1-cut artifact — proof produced by the maintainer that the
pipeline really works, durable + audit-friendly in the same
repository the framework ships in.

---

## D-161 — `dockyard dev` auto-attaches the inspector as a third supervised child, with `--no-inspector` opt-out (closes the V2-backlog auto-attach seam)

**Date:** 2026-05-25
**Status:** Settled (v1.1 Wave A — inspector polish)
**Where it lives:** `internal/devloop/devloop.go` (the orchestrator
that brings up the third child), `internal/devloop/inspector.go`
(the inspector child wiring), `internal/cli/dev.go` (the
`--no-inspector` flag), `docs/V2-BACKLOG.md` ("dockyard dev's
inspector auto-attach seam" — the closure of this item),
`scripts/smoke/v1.1-wave-A.sh` (the mechanical assertion that the
seam is wired).

**Why now.** The v1.0.0 cut left the auto-attach as a deferred seam
(D-085 in phase 19; D-101 in phase 23 — both noting the supervisor
was shaped to accept it). The deferral was correct in V1: the
inspector was still solidifying (Phases 22 / 23 / 24-finish / 25 /
27 each added or sharpened a client-shaped surface), and embedding
it into `internal/devloop` would have widened a phase already
carrying the framework's largest surface. v1.1 picks up the
follow-up exactly as the V2-BACKLOG entry's "definition of done"
described it: the inspector is a third supervised child alongside
the Go server and Vite, auto-attached by default, with a clean
opt-out flag for CI / headless dev.

**The decision.** When `dockyard dev` is run without
`--no-inspector`, the orchestrator brings up three supervised
children rather than two:

1. The Go MCP server (as before — `go run .` with `CGO_ENABLED=0`).
2. The Vite dev server, when a `web/` UI is present (as before).
3. The inspector, hosted in-process via the importable
   `internal/inspector` package (D-162 covers the in-process
   choice). Its bind is loopback-only (`127.0.0.1:0` by default,
   OS-assigned port); its server-URL is `http://127.0.0.1:8080`
   (the v1.1-default address the dev loop pins for the Go server
   so the inspector has a known MCP base URL to attach to); its
   project-source wiring (Verdicts / Contracts / Fixtures /
   Prompts) is identical to the standalone `dockyard inspect`
   path so the two entry points surface the same panels.

The inspector child uses the same loopback-only gate
(`requireLoopback`) the standalone path does — a developer who
passes a non-loopback `--inspector-addr` gets the typed
`ErrNonLoopbackBind` and the dev tree continues without it (the
P4 invariant is mechanical, not by policy).

**Why default-on.** The V2-BACKLOG entry's "definition of done"
makes the choice: a developer running `dockyard dev` should get
the inspector reachable without remembering a flag, and the few
contexts where the inspector is unwanted (CI, headless servers,
screen-shares) are exactly the contexts where the operator types
a flag anyway. Default-on plus a clean opt-out is the smaller
cognitive load over default-off plus a remember-this flag.

**Why pin the Go server to HTTP on a known port.** The inspector
needs a deterministic MCP base URL to attach to; without a pin
the supervised server would default to stdio (the scaffold's
default — `DOCKYARD_TRANSPORT` unset). The dev loop pins
`DOCKYARD_TRANSPORT=http` and `DOCKYARD_HTTP_ADDR=127.0.0.1:8080`
as **defaults** on the child environment — a developer who
already exported either env-var in their shell wins via the
later-entry-wins rule `os/exec` follows. With `--no-inspector`
the pins are not applied at all, preserving the pre-v1.1
behaviour exactly. A future `--server-addr` flag could expose
the pin choice; left out of Wave A as the default works for the
scaffolded templates and `examples/prompts-demo`.

**Teardown ordering.** Children are stopped before the watcher
(as in V1), and the inspector is stopped FIRST among the
children — its in-process HTTP server drains while the Go server
is still up, so the operator's last UI action sees a clean
shutdown rather than a transient 502 from a backend whose
upstream just disappeared. The Vite child and the Go server stop
in the previous order.

**Knock-on.** The `--no-inspector` flag becomes a new
user-facing CLI surface — the §19 hygiene rule mandates a same-PR
update to `skills/run-the-dev-loop/SKILL.md` and to
`docs/site/guides/dev-loop.md`. The published docs pages
describe the new default + the opt-out; the skill teaches it.

---

## D-162 — The supervised inspector is hosted in-process, not as a `bin/dockyard inspect` subprocess

**Date:** 2026-05-25
**Status:** Settled (v1.1 Wave A — inspector polish)
**Where it lives:** `internal/devloop/inspector.go` (the
`inspectorChild` type that hosts the inspector in-process),
`internal/devloop/devloop.go` (the orchestrator that drives the
in-process lifecycle directly rather than spawning a subprocess).

**Why this is settled rather than implementation-internal.** The
auto-attach (D-161) had two credible shapes:

- **In-process.** `internal/inspector` is an importable Go
  package; the dev loop calls `inspector.New(opts)` and runs the
  HTTP server in a supervised goroutine. The inspector's
  lifecycle rides on the dev session's context.
- **Subprocess.** The dev loop spawns `bin/dockyard inspect
  --url … --dir …` as a third supervised child, the same way it
  spawns the Go server and Vite — uniform supervisor pattern.

Either shape works; the rationale for the choice is worth
recording so a future contributor does not re-litigate it.

**The decision.** The supervised inspector is hosted **in-process**.
The reasons, in order of weight:

1. **No `bin/dockyard` resolution at runtime.** A subprocess
   approach needs to locate the dockyard binary at dev time
   (`os.Executable`, then a $PATH fallback). The most common
   contributor configuration runs `dockyard dev` via
   `go run ./cmd/dockyard` — there is no installed binary; the
   subprocess shape would have to re-spawn the same `go run` with
   different args, which is brittle and slow (a second `go run`
   startup on every dev session start).
2. **One process, one shutdown.** A subprocess inspector survives
   only as long as the supervisor signals it correctly through
   the process group; the in-process path rides on the same
   `context.Context` the rest of the dev loop already uses. One
   fewer concurrent-shutdown ordering to get right (the v1
   `wave9_test.go` audit's lesson).
3. **No transitive PATH ordering.** A user with a `dockyard` from
   a homebrew install on their PATH and a freshly-built
   `./bin/dockyard` in the worktree would land in unpredictable
   territory; the in-process path uses exactly the code that is
   compiled into the running `dockyard dev` binary, no version
   drift.
4. **The inspector lifecycle is already an importable Go API.**
   `internal/inspector.New(opts)` is the same API
   `internal/cli/inspect.go` already uses. The dev loop building
   options is a thin reshape, not a new public surface.

**Why this does not violate the supervisor uniformity.** The
supervisor pattern's load-bearing property is "every supervised
child is started, restarted, and stopped through one explicit
seam" — not "every supervised child is a subprocess." The
`inspectorChild` type honours that seam (`Start` / `Stop`,
context-driven cancellation, the same teardown ordering as the
process supervisors); it just runs inside the dev-loop process
rather than outside it. The two supervisor flavours
(`*supervisor` for subprocesses, `*inspectorChild` for the
in-process inspector) are intentionally not unified into a single
interface — the abstractions are different (an `*exec.Cmd` is
not an `*inspector.Inspector`), and the surface area is small
enough that the dev-loop orchestrator can drive both directly.

**Knock-on.** A future second in-process child (say, a watched
HTTP probe) would land alongside `inspectorChild`. A unified
"child" interface is the right move only when there are three+
in-process children with comparable lifecycles — premature
abstraction with one in-process child is a worse trade than the
two small concrete types.

---

## D-163 — The inspector's Prompts panel + `POST /api/prompts/get` is an operator-initiated client-shaped surface (extends D-131 to a third read)

**Date:** 2026-05-25
**Status:** Settled (v1.1 Wave A — inspector polish; closes
[`docs/V2-BACKLOG.md`]'s "Inspector Prompts panel" entry, D-151)
**Where it lives:** `internal/inspector/prompts.go` (the typed
`PromptSource` + `PromptInvoker` + the SDK client wiring),
`internal/inspector/assets.go` (the `GET /api/prompts` +
`POST /api/prompts/get` mux routes),
`internal/inspector/inspector.go` (`Options.Prompts` +
`Options.PromptInvoker`),
`web/inspector/src/lib/PromptsPanel.svelte` (the rail tab),
`web/inspector/src/lib/prompts.ts` (the typed prompt model),
`web/inspector/src/lib/api.ts` (`fetchPrompts` / `invokePrompt`),
`test/integration/phase27_inspector_security_test.go` (the
`prompts.go` allow-list entry — the mechanical guard that an
inspector PR adding a new `mcp.NewClient` filed a decision).

**Why now.** Phase 28 (D-151 / D-152) shipped the prompts surface
on the runtime side — `runtime/server.AddPrompt`, the obs/v1
`prompt.get` carrier, the `examples/prompts-demo` worked example
— but the inspector did not render Prompts (D-151 explicitly
called this a post-V1 candidate). The result: a developer
running `examples/prompts-demo` could see their prompts work
against a host that surfaces prompts (Claude, an MCP CLI), but
not through the inspector — a gap the V2-BACKLOG named as a
clean v1.1 closure.

**The decision.** The inspector grows a Prompts rail tab and
two new backend endpoints:

- `GET /api/prompts` — a read-only `prompts/list` of the
  attached server's registered prompts. Returns the inspector's
  typed `PromptInfo` slice — no raw SDK type leaks (P3,
  mirroring D-103's `AppPreview` and D-131's `InvokeResponse`).
- `POST /api/prompts/get` — an operator-initiated `prompts/get`
  against the attached server. The panel POSTs `{name,
  arguments}`; the backend opens a short-lived MCP client
  session, calls `prompts/get`, and returns the rendered
  messages. A detached inspector answers 503; a malformed body
  answers 400; a transport-level failure answers 502 with a
  typed JSON message; a server-side `prompts/get` error is a
  successful RPC (200 with `error` filled in the response —
  the same isError-as-200 pattern D-131 set for `tools/invoke`).

**Why this stays within P4 (CLAUDE.md §1, §13).** D-163 extends
D-131's "operator-initiated `tools/call`" framing to a third
operator-initiated client-shaped surface. Every clause of P4
remains intact:

- The inspector is still the lone client-shaped component — no
  new package, no long-lived client, no production client.
- The endpoint is localhost-only via the existing
  `requireLoopback` gate (CVE-2025-49596 lesson; D-145
  hardening).
- The operator is the one driving the read through the UI —
  not an off-localhost actor; same shape as D-099 (operator
  views the relay), D-103 (operator views Apps), D-131
  (operator invokes tools), D-134 (operator approves a task).
- Each invocation opens a fresh client session, calls one
  prompt, and closes — no long-lived client state, no SDK
  types leak into Dockyard's handler-facing surface (P3 — the
  `PromptGetRequest` / `PromptGetResponse` types are the
  inspector's own).

**Why this is "operator-initiated", not just "read-only".** A
`prompts/get` is a read in the MCP semantic sense (the host
pulls a rendered template — no side-effect on the server), so
"read-only" would be defensible. But D-144 already re-cast the
inspector's framing as "operator-initiated only" rather than
"read-only" — every client-shaped surface is driven by an
explicit UI action with a documented decision. D-163 follows
the same framing for consistency: the panel renders, the
operator clicks Invoke, the inspector makes one prompts/get
call. The audit gate (the Phase 27 `mcp.NewClient` allow-list)
treats every new client surface uniformly; the consistency
keeps the audit's mental model simple.

**Why not contract-first for the argument form.** MCP prompt
arguments are a flat string-keyed map (D-152) — no JSON Schema
on the argument shape. The panel renders a simple typed form
keyed by `PromptArgument.Name` rather than reusing the
contract-first `schema-form.ts` pipeline the Tools panel uses.
This is the same constraint D-152 settled on the runtime side;
the inspector mirrors it.

**Knock-on.** The §19 hygiene rule mandates a same-PR update to
`skills/test-with-the-inspector/SKILL.md` and to
`docs/site/guides/inspector.md`. The published docs page
describes the new Prompts panel + its operator-initiated
framing; the skill teaches a developer to drive it. Pagination
of `prompts/list` is left for V2 (the SDK supports cursor
pagination; the V1 panel walks the first page only — a follow-
up V2 item, small and additive).

---

## D-164 — Scaffold + `dockyard run` auto-wire the Tasks engine when the manifest declares task-supporting tools

**Date:** 2026-05-25
**Status:** Settled (v1.1 wave B — runtime cleanups)
**Where it lives:** `internal/scaffold/manifest_detect.go` (the
`RequiresTasksEngine` detection seam), `internal/scaffold/templates.go`
(the `renderMainGoWithTasks` engine-wired branch + the original
`renderMainGoPlain` engine-free branch), `internal/scaffold/scaffold.go`
(the `Options.ExampleToolTaskSupport` knob the no-template scaffold
takes), `internal/runpkg/run.go` (the `auditAutoWire` warning emitter
the `dockyard run` verb consults at start time),
`templates/approval-flows/main.go.tmpl` (the template whose hand-written
wiring matches the generator's output so a future maintainer reads one
shape, not two), `docs/V2-BACKLOG.md` (the original deferral entry, now
cross-linked to this decision).

**The decision.** Whenever the project's manifest declares any tool
with `task_support: optional` or `task_support: required`, the
scaffolded `main.go` constructs a real `tasks.NewInMemoryStore()` +
`tasks.NewEngine(...)` and attaches it via `server.Options{Tasks:
engine}` — no hand edit required. The detection is the new
`scaffold.RequiresTasksEngine(*manifest.Manifest) bool` helper, called
both at scaffold time (the renderer branches on
`Options.wireTasksEngine()`) and at `dockyard run` start time (the
audit warns if the manifest demands an engine but `main.go`'s source
does not appear to attach one — a heads-up the engine the manifest
implies is not wired). The two paths agree by construction: the same
predicate over the same value.

**Why scaffold-time emission, not run-time engine attachment.** The
brief offered two paths — detect at scaffold time and write the
engine into `main.go`, or detect at run time and inject the engine
into the running binary. Run-time injection is wrong: `dockyard run`
is a build-and-exec verb, not a runtime; the project's binary is
self-contained and runs without `dockyard run` (Claude Code execs the
binary directly via the manifest's stdio transport entry). Scaffold-
time emission keeps the project authoritative — the generated code is
what runs, the user can edit it, the engine is visible in their
source tree. The run-time audit is a soft check that surfaces a
post-scaffold manifest edit ("user added `task_support: required` to
a tool after scaffolding but didn't re-scaffold") with one warning
line on stderr — never a failure.

**Why in-memory store, not the SQLite driver.** The auto-wire is
deployment-shape agnostic; an in-memory store is correct for the
single-user stdio default the blank scaffold targets. A developer
running a durable HTTP/Portico deployment edits `main.go` to swap in
`sqlitestore.Open` + the engine's migration step — the same edit the
`approval-flows` template's docs already describe. Auto-wiring SQLite
would force a `modernc.org/sqlite` import on every task-supporting
project regardless of deployment shape, and would commit the
auto-wire to a manifest field (`tasks.store: sqlite`) we have not
designed yet.

**Why `Options.ExampleToolTaskSupport` rather than a manifest read.**
The blank `Generate()` builds the manifest YAML from a hardcoded
template; the renderer can either re-parse its own output or take the
knob directly. Taking the knob is simpler, cheaper, and lets a
caller (the integration test, a future custom scaffold path) opt the
example tool into tasks without round-tripping through YAML. The
`scaffold.RequiresTasksEngine` helper remains the read side a
post-scaffold loader (`dockyard run`, `dockyard validate`) calls
against a loaded manifest — the predicate is the same in both
directions.

**Conservative Lifecycle defaults.** The auto-wired engine ships
with: `MaxTTL: 1h`, `DefaultTTL: 5m`, `PurgeInterval: 30s`,
`MaxConcurrentPerRequestor: 16`, `RequestorIdentifiable: false`,
`AdvertiseList: false`, `PollInterval: 250`. The non-zero
`MaxConcurrentPerRequestor` keeps the brief 02 §4.6 resource-
exhaustion guard active by default; the 16-task ceiling is large
enough that no realistic single-user stdio workload trips it. A
developer with a different production posture edits the rendered
`main.go`. The `approval-flows` template's hand-written wiring is
brought into the same shape so a future maintainer reading both
finds the engine-construction block identical.

**Supersedes.** D-108 (the original R2 follow-up deferral —
"scaffold + run auto-wire is a future CLI/scaffold phase"). The
V2-backlog entry that recorded the deferral stays in
`docs/V2-BACKLOG.md` as a cross-link audit trail; it is now marked
claimed by v1.1 wave B.

---

## D-165 — `HostProfile.RequiresServerURL` retires the synthetic-URL workaround in the capability testgate

**Date:** 2026-05-25
**Status:** Settled (v1.1 wave B — runtime cleanups)
**Where it lives:** `runtime/apps/hostprofile.go` (the new
`RequiresServerURL() bool` method on the `HostProfile` interface),
`runtime/apps/hostprofile_generic.go` (returns `false`),
`runtime/apps/hostprofile_claude.go` (returns `true`),
`internal/testgate/categories.go` (`runCapability` consults the new
method; the `syntheticServerURL` constant + comment block are
removed), `docs/V2-BACKLOG.md` (the original deferral entry, now
cross-linked).

**The decision.** The host-profile interface gains a
`RequiresServerURL() bool` method. The capability-degradation
testgate category (`internal/testgate/categories.go`) consults it
when driving each App through each registered host profile: a
profile that returns `true` (a signing host whose origin derivation
binds to the server URL — D-063, D-064) is exempt from the
empty-URL derivation; a profile that returns `false` (a pass-through
profile like `generic`) derives cleanly against an empty URL. The
`syntheticServerURL` constant (`https://capability-test.example/mcp`)
that the gate fabricated to dodge the signing-host invariant is
removed; the gate fabricates no URL.

**Why path B, not path A.** The V2-backlog entry identified two
fixes: (A) declare `_meta.ui.domain` in the `analytics-widgets`
manifest so the signed-origin derivation has the metadata it needs,
or (B) extend the host-profile API. Path B generalises: any future
host profile with a similar invariant declares it honestly via the
new method, and the capability gate threads through that declaration
rather than fabricating an input per host. Path A would force every
UI-bearing template manifest to declare a `_meta.ui.domain` purely
to satisfy the capability test — template-specific noise that
solves the immediate symptom but does not generalise. The host-
profile API churn is minimal (one method, two implementations); the
semver implications are addressed below.

**Semver framing.** Adding a method to the `HostProfile` interface
is breaking for an out-of-tree implementer (they must add the
method) but additive for a caller (it only reads the new method
through the same interface). The change rides with the v1.1 minor
per the semver policy (D-159 — minor for additive changes,
"breaking on a public interface" is the borderline case here). Two
mitigations: (1) no out-of-tree host profile has been observed yet
(the registry is V1 but the pattern is documented per RFC §7.5, not
declared and used by any third party we know of); (2) the new
method is trivial to add — one line returning the right bool.

**Why exempt rather than test-with-a-URL.** A signing profile's
derivation is bound to the server URL by design; a synthetic URL
satisfies the derivation invariant nominally but exercises a *fake*
binding, not the seam the profile is meant to prove. The honest
posture is "the capability gate proves the seam resolves; the
signed-origin binding is proven by the profile's own tests under
`runtime/apps/`". Exempting a `RequiresServerURL` profile from the
empty-URL derivation says exactly that on the wire.

**Supersedes.** D-145 (the original synthetic-URL workaround that
recorded "Phase 27 hardening" as the deferral). The V2-backlog
entry that recorded the deferral stays as a cross-link; it is now
marked claimed by v1.1 wave B.

---

## D-166 — `dockyard new` runs `go mod tidy` + `dockyard generate` at scaffold time (supersedes D-139)

**Date:** 2026-05-28
**Status:** Settled (v1.2 wave A — scaffold autogen + changelog supplement)
**Where it lives:** `internal/cli/new.go` (the post-scaffold step + the
`--no-postgen` flag + the `goModTidyFn` / `generateFn` seams),
`internal/cli/new_test.go`, `test/integration/v1_2_wave_a_test.go`,
`docs/site/cli/index.md` (the regenerated CLI reference),
`skills/scaffold-a-server/SKILL.md`,
`docs/site/getting-started/{index,analytics-widgets,approval-flows}.md`,
`templates/{analytics-widgets,approval-flows}/README.md.tmpl`,
`docs/V2-BACKLOG.md` (the D-139 deferral, now cross-linked).

**The decision.** `dockyard new` (blank or `--template`) runs, after the
pure scaffold writes its tree, two one-time steps so a fresh project
reaches a green `dockyard validate` with no manual command: (1) `go mod
tidy` (resolve the deps the generated `go.mod`'s `replace` directive
declares — RFC §4.3) and (2) `dockyard generate` in-process (materialise
a template's JSON Schema + TypeScript — RFC §6.2). This supersedes
D-139's "document the two manual steps" stopgap.

**Where the steps live, and why.** The steps run at the CLI boundary
(`internal/cli/new.go`), **not** inside `scaffold.Generate()`. The pure
`scaffold` package stays a network-free, deterministic, golden-tested
file generator — its `TestGolden` is byte-identical and untouched. The
two side-effecting steps (one shell-out, one filesystem-mutating codegen
run) are the CLI's job, and they are package-var seams (`goModTidyFn`,
`generateFn`) so the success and failure paths are unit-tested without a
real toolchain or network.

**Best-effort, with `--no-postgen`.** `go mod tidy` needs the module
proxy; an offline / air-gapped developer would otherwise see a hard
failure. So both steps are best-effort: a failure prints one warning and
`printNextSteps` shows the manual recovery (`go mod tidy` + `dockyard
generate`) — the project tree is already written, so the scaffold still
exits 0. `--no-postgen` skips both steps outright (hermetic / CI runs, or
a developer who runs them separately). This is a deliberate refinement of
the V2-backlog "without extra commands" definition of done: the common
path needs no commands; the offline path degrades to a clear manual
fallback rather than a broken scaffold.

**Why run generate even for the blank scaffold.** The blank scaffold
ships its generated artifacts pre-built, so `generate` is idempotent
there (no diff). Running it unconditionally keeps one code path and makes
the template case — where it is load-bearing — the same as the blank
case. The integration test's negative control proves the step is
load-bearing: a `--template` scaffold without `generate` validates *red*
(missing/stale codegen), and green only after the step runs.

**Knock-on (D-139 §19 hygiene).** The two manual steps are removed from
the `scaffold-a-server` skill and the getting-started docs in the same
PR; the template `README.md.tmpl`s are reframed to "the project is
ready" (retaining a `go mod tidy` mention as a day-to-day note). The CLI
reference page is regenerated (D-140) so the new `--no-postgen` flag
appears.

**Supersedes.** D-139 (the documented manual `go mod tidy` + `dockyard
generate` workflow). The V2-backlog entry that recorded the deferral
stays as a cross-link; it is now marked claimed by v1.2 wave A.

---

## D-167 — the release pipeline appends a Conventional-Commits supplement below the hand-authored CHANGELOG section

**Date:** 2026-05-28
**Status:** Settled (v1.2 wave A — scaffold autogen + changelog supplement)
**Where it lives:** `internal/changelogx/supplement.go` (the pure
`Supplement([]Commit) string` + `ParseCommit` + the `Commit` type),
`internal/changelogx/supplement_test.go` +
`internal/changelogx/testdata/supplement.golden`,
`internal/changelogx/cmd/changelogx/main.go` (the `-supplement` mode +
the `git log` driver), `.github/workflows/release.yml` (the
tag-push-only append step + `fetch-depth: 0`), `docs/RELEASING.md`.

**The decision.** The GitHub Release body stays the hand-authored
CHANGELOG section (the canonical P1–P4 narrative — D-154), and the
release pipeline appends below it an auto-generated, Conventional-Commits
-derived list of what landed. The build job runs `changelogx -supplement`
over the `previous-tag..this-tag` range; the pure classifier groups
commits into Keep-a-Changelog categories (feat → Added, fix → Fixed,
everything else → Changed) and drops the noise types
(`docs`/`chore`/`test`/`ci`/`build`/`style`). This is the V2 follow-up
D-154 explicitly named.

**Why a pure, stdlib-only, golden-tested classifier.** Same reasoning as
D-157 for the extractor: the release-body path is a load-bearing gate, so
a third-party changelog-generator or CommonMark dependency would be a
transitive-behaviour risk on it. The classifier is a small pure function
(`Supplement`) pinned by a golden test against a fixed commit fixture, so
a future format change is a loud unit-test failure in PR, not a quiet
release-time surprise. The `git log` shell-out lives only in the
`cmd/changelogx` driver, not in the package — the package stays pure and
testable.

**Tag-push only; narrative stays canonical.** The supplement is appended
only on a real tag push, and only when a previous tag exists to diff
against (the first release, and every `workflow_dispatch` dry-run, keep
the bare extracted section). The supplement is rendered under a distinct
`### Commits` heading with bold category labels (not `### Added`
headings) so it never collides visually with the narrative's own
headings. The supplement is computed at release time and lives only in
the GitHub Release body — it is never committed back into `CHANGELOG.md`.

**Scope: signal-only.** `docs`/`chore`/`test`/`ci`/`build`/`style`
commits are dropped — they are noise in a release body. `feat`/`fix` map
to Added/Fixed; `perf`/`refactor`/an unknown prefix/a non-conventional
subject fold into Changed as a safe catch-all. The `git log --no-merges`
invocation keeps merge commits out of the catch-all. This is tunable in
one place (the pure `classify` function) if a future release wants a
fuller list.

---

## D-168 — the `require_fixtures` / `require_contract_tests` manifest gates must be enforced (they are currently declared-but-dead)

**Date:** 2026-05-29
**Status:** Settled; **implemented in v1.3 wave A** (`internal/validate`
now enforces both gates — see D-169 for the scoping the implementation
resolved).
**Where it lives (today):** `internal/manifest/manifest.go` (the
`RequireFixtures` / `RequireContractTests` fields),
`internal/scaffold/templates.go` (the blank scaffold sets both `true`),
`skills/attach-a-ui-resource/SKILL.md` (documents validate failing on a
missing fixture). **No consumer in `internal/validate` or
`internal/testgate`.**

**The finding.** Downstream feedback (the first external Dockyard user)
reported that a no-UI server with `require_fixtures: true` and
`require_contract_tests: true` passed `dockyard validate` / `dockyard
test` green **with no fixtures present and after the scaffold's
`greet_test.go` was deleted**. Verified: both fields exist on the
manifest and the blank scaffold sets them `true`, but **nothing in the
validate or test gates reads either field** — they are advertised quality
gates that never bite. The `attach-a-ui-resource` skill even documents
`dockyard validate` failing on a missing fixture, so the **docs already
promise the behaviour the code does not deliver**. This is a correctness
gap, not a design open question: a gate a user turns on must enforce, or
not exist.

**The decision.** Enforce, do not remove. The two flags are wired into
the quality gates:

- `require_fixtures: true` — `dockyard validate` reports a **Blocker** for
  any tool (or, per the manifest's existing semantics, any UI-bearing
  tool) that lacks its fixture set; `dockyard test`'s contract category
  fails likewise.
- `require_contract_tests: true` — the gate asserts the project carries a
  contract test for the declared tools; a project that has deleted /
  omitted them is a Blocker.

**Why enforce, not remove.** The flags were a deliberate, documented part
of the contract-first quality story (the scaffold opts in by default and
the UI skill documents the failure mode). Removing them would walk back an
advertised guarantee and quietly weaken the "gates that bite" ethos that
the same downstream review praised. The honest fix is to make the code
match the long-standing documented intent.

**Behaviour-change note (semver).** Enforcing a previously-dead gate can
turn a project that was green red (a project that legitimately had no
fixtures while the gate was a no-op). Per D-159 this is treated as a
**bug fix** (the gate was always advertised as enforcing), but the
implementing PR calls it out prominently in `CHANGELOG.md` and the release
notes so an existing user is not surprised. The default-on scaffold has
always shipped its fixtures + contract test, so a freshly scaffolded
project stays green.

**Considered and rejected.** (a) *Remove the flags* — walks back a
documented guarantee; rejected. (b) *Leave as documentation-only* — a
gate that never bites is worse than no gate (it manufactures false
confidence); rejected.

---

## D-169 — `require_fixtures` is UI-scoped; `require_contract_tests` is project-wide (the enforcement semantics for D-168)

**Date:** 2026-05-29
**Status:** Settled (v1.3 wave A).
**Where it lives:** `internal/validate/checks.go` (`checkFixtures`,
`checkContractTests`), `internal/validate/validate.go` (the `CheckFixtures`
/ `CheckContractTests` constants + wiring), `internal/scaffold/templates.go`
(the blank scaffold's clarified quality comment),
`examples/combined-patterns/dockyard.app.yaml` (reconciled).

**The open question.** D-168 settled that the two dead gates must be
enforced; it left "does `require_fixtures` apply to every tool or only
UI-bearing tools?" to implementation. The deciding constraints, found by
auditing every in-repo manifest:

- Fixtures (`fixtures/<tool>/<state>.json`) are the **inspector's
  App-preview inputs** (D-130) — they exist to drive a UI-bearing tool's
  rendered states. A non-UI tool has no App to preview.
- The blank scaffold's `greet` tool is non-UI and ships no fixtures; the
  no-UI examples (`backend-tools-only`, `prompts-demo`) ship none either.
  Enforcing fixtures for *every* tool would fail a fresh blank scaffold
  (regressing D-166's green-on-first-validate) and would demand
  conceptually-meaningless fixtures for backend-only tools.

**The decision.**

- **`require_fixtures` is UI-scoped.** Each tool that declares a `ui:` app
  must ship ≥1 `fixtures/<tool>/*.json`; a non-UI tool requires none. So a
  blank no-UI server stays green with the gate on (the gate is primed for
  when a UI is attached), and the two V1 templates' UI tools (which ship
  the six-state fixtures) satisfy it. A UI-bearing tool missing fixtures is
  a Blocker.
- **`require_contract_tests` is project-wide.** The project must carry ≥1
  `*_test.go` (the `web/`, `vendor`, `node_modules` and dot-directories are
  skipped). A static validator cannot prove a test *exercises* a given
  tool, but it can prove the project is not test-free — the exact
  regression the gate guards (the downstream report: deleting the
  scaffold's `greet_test.go` and `validate` still passing). Absence is a
  Blocker.

**Reconciliation.** Auditing the gate-setting manifests surfaced two that
declared `require_fixtures: true` with UI tools but shipped no fixtures:
`examples/combined-patterns` (a shipped composition demo) and
`examples/customer-health` (a manifest-only loader test fixture, never
validated). `combined-patterns` is set to `require_fixtures: false` (a
worked-code demo is not a fixture showcase — the two templates are);
`customer-health` is left untouched (it is loader test data, not a
validated project). No test in the suite runs `validate` against the
examples, so the enforcement breaks no CI; the scaffold and both templates
were already coherent.

**Behaviour change (semver, per D-168).** A project that set
`require_contract_tests: true` and carries no test — or
`require_fixtures: true` with a UI tool and no fixtures — now fails
`dockyard validate` where it previously passed. Called out in
`CHANGELOG.md`. A freshly scaffolded project (blank or template) stays
green.

---

## D-170 — `dockyard new` pins the CLI's resolved version into the scaffolded go.mod (closes the `v0.0.0` + replace sharp edge)

**Date:** 2026-05-29
**Status:** Settled (v1.3 wave A). Extends D-080.
**Where it lives:** `internal/cli/root.go` (`ResolvedVersion`),
`internal/releasebuild/release.go` (the `-X …/internal/cli.Version` ldflags
stamp), `internal/scaffold/templates.go` (`renderGoMod` /
`requireVersion`), `internal/scaffold/scaffold.go`
(`Options.DockyardVersion`), `internal/cli/new.go`.

**The sharp edge.** The scaffold wrote `require github.com/hurtener/dockyard
v0.0.0` — a placeholder that only resolves behind the local `replace`
directive (D-080). A developer dropping the replace to use the published
module hit `go get …@v1.2.0: v0.0.0: unknown revision` and had to hand-edit
the require line. Worse, a released `dockyard new` *without*
`--dockyard-path` wrote `v0.0.0` + no replace → `go mod tidy` failed
outright (the D-166 post-step could not be green on the published path).

**Root cause found.** `cli.Version`'s comment claimed an `-ldflags -X`
stamp, but **nothing actually stamped it** (`internal/releasebuild` passed
only `-s -w`) and there was no `ReadBuildInfo` fallback — so every binary,
released or `go install`-ed, reported `0.0.0-dev`.

**The decision.**

- `cli.ResolvedVersion()` resolves the version in order: the `-ldflags -X`
  stamp (release-pipeline binaries) → `debug.ReadBuildInfo().Main.Version`
  (a `go install …@vX.Y.Z` binary, which carries no stamp but records the
  module version) → the `0.0.0-dev` placeholder (a `go build` /
  `make build` from a checkout).
- `internal/releasebuild` now stamps
  `-X github.com/hurtener/dockyard/internal/cli.Version=<canonical version>`,
  so released binaries report the real version too.
- `dockyard new` passes `ResolvedVersion()` into
  `scaffold.Options.DockyardVersion`; `renderGoMod` pins it in the require
  directive when it is a real release version (vX.Y.Z), else falls back to
  `v0.0.0` (only ever resolved through the replace — the build-from-source
  path). Both real install paths (release binary, `go install @vX.Y.Z`)
  therefore pin a clean tag, so a project that drops the replace resolves
  the published module without a hand edit, and the published-path
  `dockyard new` is green on the first `validate`.

**Determinism.** The scaffold golden test passes no `DockyardVersion`, so
the golden go.mod keeps `v0.0.0` (byte-stable); the pin is exercised by a
dedicated unit test. A `make build` checkout binary may pin a pseudo-version
(its build-info version) on a no-replace scaffold — a dev corner case
(dev builds use `--dockyard-path`, where the replace wins regardless), not
a regression over the prior `v0.0.0`.

---

## D-171 — the bridge View-side task-progress channel rides an additive `obs/v1` `PhaseProgress` event

**Date:** 2026-05-29
**Status:** Settled (v1.3 wave B). Closes the V2-BACKLOG "Bridge View-side
task-progress channel" item.
**Where it lives:** `web/bridge/src/protocol.ts`
(`HostNotification.taskProgress`, `TaskProgressParams`),
`web/bridge/src/notifications.ts` + `bridge.ts` (`onTaskProgress`),
`web/inspector/src/host/host-bridge.ts` (`sendTaskProgress`),
`web/inspector/src/lib/tasks.ts` (`latestTaskProgress`) + `App.svelte` /
`AppFrame.svelte` (the forwarding wiring), `runtime/obs/payload.go`
(`TaskProgressPayload.Fraction`), `runtime/tasks/handle.go`
(`Progress`/`Status` emit `PhaseProgress`).

**The gap.** `runtime/tasks.TaskHandle.Progress` reports a task's progress
server-side, but it only updated the polled `StatusMessage` (a working→
working metadata transition) — the bridge (`web/bridge`) exposed no
View-side progress channel, so an App's card could not render a live "62%".
Progress surfaced only in the host's own task UI, never inside the App's
iframe (the first external MCP-Apps builder's feedback, 2026-05-29).

**The decision.**

- **A host→View notification, not a request.** `ui/notifications/task-
  progress` carries `{ taskId, fraction?, message?, status? }`. The View
  subscribes with `BridgeShell.onTaskProgress`, mirroring `onToolResult` /
  `onHostContextChanged` (a typed `Topic` + a router dispatch case). Every
  field but `taskId` is optional so an App renders whatever the host
  forwards. Host→View only — an App is a View (P4), so progress flows down
  to it, never up.
- **The runtime emits the reserved `PhaseProgress` event.** `obs/event.go`
  already declared `PhaseProgress` "reserved for future mid-task updates";
  `TaskHandle.Progress` now emits a `task.progress` `PhaseProgress` event
  carrying the clamped fraction + the raw message, and `TaskHandle.Status`
  emits one with the message and no fraction. Emission is gated on the
  status update succeeding, so a `Progress` call on a task that has left
  working still errors and emits nothing. The emit path is non-blocking
  (P2) — a chatty handler never blocks; the inspector folds by task id.
- **The inspector forwards it.** The inspector is a pure `obs/v1` client
  (P2): it derives the latest `PhaseProgress` point from the stream and
  pushes it to the App preview's host-half bridge via `sendTaskProgress`,
  so the channel is demoable through `dockyard inspect` end to end.
- **Degradation by absence (RFC §7.5).** A host that never forwards
  `task-progress` simply never triggers `onTaskProgress` — no capability
  flag, no host matrix. This mirrors the elicitation channel (D-134).

**The obs/v1 shape change.** `TaskProgressPayload` gains an **additive,
optional** `Fraction *float64` (`omitempty`). An existing consumer that
does not read it is unaffected, the schema version stays `dockyard.obs/v1`
(an additive field is not a shape break, per CLAUDE.md §8), it is documented
in `CHANGELOG.md`, and the obs golden/recorder tests are re-pinned to the
new shape. The fraction folds into the existing "62% — message"
`StatusMessage` for `tasks/get` pollers unchanged, so the polled surface is
untouched.

**Considered and rejected.** (a) *Parse the fraction back out of the
`StatusMessage` string in the inspector* — fragile and lossy; a typed
field is the honest contract. (b) *A negotiated `appCapabilities`/
`hostCapabilities` flag for task-progress* — unnecessary ceremony; absence
already degrades cleanly, and a flag would invite a per-host matrix the
project forbids (§6). (c) *Emit a brand-new `obs/v1` event kind rather than
reusing `task.progress`/`PhaseProgress`* — `PhaseProgress` was reserved for
exactly this; a new kind would be a larger, unneeded contract change.

---

## D-172 — `@dockyard/bridge` + `@dockyard/ui` publish to npm as source with a `svelte` export condition (not a `dist` build), versioned to the repo

**Date:** 2026-05-29
**Status:** Settled (v1.3 wave B). Closes the V2-BACKLOG "Publish
`@dockyard/bridge` + `@dockyard/ui` to npm" item; extends D-080 / D-170.
**Where it lives:** `web/bridge/package.json`, `web/ui/package.json`
(`publishConfig`, version, `svelte`/`exports` conditions, `peerDependencies`),
`.github/workflows/release.yml` (the gated `npm-publish` job),
`internal/scaffold/templates.go` (`WebDepSpecs`), the two
`templates/*/builtin.go` (consume it), `docs/RELEASING.md` (the version-bump
step), `skills/scaffold-a-server` + `skills/attach-a-ui-resource` (caveat
dropped).

**The gap.** The Dockyard Go module is published, but `@dockyard/bridge` and
`@dockyard/ui` were workspace-only (`main: ./src/index.ts`, version `0.1.0`,
no `publishConfig`), so every UI scaffold needed the hidden `--dockyard-path`
flag + a local checkout — the top downstream-reported friction. The Go/npm
asymmetry was invisible: the Go half "just works" from the proxy while the
frontend half silently required a checkout.

**The packaging decision — publish source, not a `dist` build.** The task
left the choice open between a build (`@sveltejs/package` for `ui`,
`tsup`/`tsc` for `bridge`) and the correct `exports` conditions. We ship
**source** with a `svelte` export condition, because:

- The `--dockyard-path` `file:` workflow **already proves the source shape
  builds** — it points the template's `web/` at the source dirs and the
  template's Vite + `@sveltejs/vite-plugin-svelte` build (the only Apps
  toolchain Dockyard supports, D-006) compiles it. Publishing the same
  source is therefore sufficient by construction; the only thing missing was
  the npm presence.
- A `dist` build would **break the internal consumer**: `web/inspector`
  resolves `@dockyard/bridge` / `@dockyard/ui` via `file:` symlinks to
  `./src/index.ts`, and the `make web` gate type-checks + builds it. Pointing
  `exports` at a `dist/` that only exists after a build step would force a
  build-before-anything ordering and a parallel resolution path for dev vs
  publish — cost and risk for no consumer benefit.
- Each package adds `"svelte": "./src/index.ts"` + an `exports` map with
  `types` / `svelte` / `default` conditions (all pointing at the source
  entry, so internal resolution is unchanged), `publishConfig.access:
  "public"` (scoped packages default to restricted), `svelte` as a
  `peerDependency`, and `files: ["src"]` (the published tarball is the
  source tree). `bridge` is pure TS (no `.svelte`), so its `default`/`svelte`
  entry transpiles via the consumer's esbuild; `ui`'s `.svelte` files are
  compiled by the consumer's svelte plugin via the `svelte` condition.

**The version policy.** Both bump off `0.1.0` to **track the repo / Go-module
version** (set to `1.4.0`, the release that first publishes them). The `npm-publish` job derives the
version **from the git tag** at publish time (`npm version <tag>
--no-git-tag-version --allow-same-version`), so the tag is the single source
of truth (as for the Go build, D-170) and a missed release-prep bump cannot
publish a mismatched version. `docs/RELEASING.md` records the release-prep
package.json bump for honesty in-tree.

**The publish job (gated + idempotent).** A tag-push-only `npm-publish` job
in `release.yml`: `npm pack` both packages, scaffold a real `--template`
project, install the **packed tarballs** into its `web/` and run the
template's build (the acceptance gate — proves the published shape builds a
downstream UI project), then `npm publish --access public` each, **skipping
a version already on npm** (`npm view <pkg>@<v>`), so a re-run or tag re-push
is safe. The token is referenced only as `secrets.NPM_TOKEN` →
`NODE_AUTH_TOKEN`; the value is never in the workflow (§7). `workflow_dispatch`
dry-runs never publish — a scoped public publish is semi-irreversible, so the
first real publish is a maintainer-driven tag.

**The consume side.** `scaffold.WebDepSpecs` resolves the
`__DOCKYARD_BRIDGE_SPEC__` / `__DOCKYARD_UI_SPEC__` tokens to a caret
`^X.Y.Z` (derived from the CLI's resolved version) when `--dockyard-path` is
omitted, and to `file:` specs when it is set. So a `--template` scaffold's
`web/` `npm install` resolves the packages from npm with no checkout;
`--dockyard-path` reverts to a pure build-from-source convenience. The two
UI templates share the one resolver, and the skills drop the
"`--dockyard-path` required for UI builds" caveat (§19).

**Considered and rejected.** (a) *`@sveltejs/package` + `tsup` dist builds* —
the spec-blessed path for a standalone library, but it breaks the internal
`file:` consumer and adds a build-ordering burden the source-publish path
avoids; the `file:` workflow already proves source consumption, so a build
is gold-plating here. (b) *Leave the scaffold emitting `*`* — `*` resolves to
latest-published, which silently floats a UI project across majors; a caret
pinned to the CLI version is the honest spec. (c) *Publish on merge to
`main`* — a publish must be a deliberate "release this" act (the tag), never
a merge side-effect (mirrors D-159 / the GitHub-Release gate).

---

## D-173 — the runtime/tool builder wires `_meta.ui` via a server `AppLink` seam (fail-loud), with per-tool visibility

**Date:** 2026-05-29
**Status:** Settled (v1.5 wave A).
**Where it lives:** `runtime/server/server.go` (`AppLink`, `RegisterAppLink`,
`AppLinkByName`, `appLinks`), `runtime/apps/apps.go` (`Register` records the
link), `runtime/tool/builder.go` (`UI(name, visibility...)`, `Register` sets
`def.Meta`, `VisibilityModel`/`VisibilityApp`).

**The bug.** `tool.New[...].UI(appName).Register(srv)` **silently dropped the
UI link**: `Builder.Register` built `server.ToolDef{Name, Description}` with no
`Meta`, and `b.uiResource` was read only by the `UIResource()` getter. So the
registered tool carried no `_meta.ui.resourceUri` (RFC §7.1). The `ui://`
resource registered fine; only the tool→resource link was missing — so a host
that renders MCP Apps (Claude Desktop) had nothing linking the tool result to
its App and rendered the text fallback. The plumbing existed
(`apps.ToolMetaFor` → `internal/protocolcodec`, emitting the nested
`{resourceUri, visibility}` form) but was never called from the builder, even
though `ToolDef.Meta`'s and `UI()`'s own doc comments said the Apps layer wires
it here. The canonical `analytics-widgets` template used the same
`.UI(appName).Register(srv)` path, so it carried the bug too. Upstream feedback
(go-video-mcp, 2026-05-29).

**The decision — Option B (a server seam), not a URI convention.**

- `runtime/server` records a name→link map: `RegisterAppLink(name, AppLink{URI})`
  / `AppLinkByName(name)`. `runtime/apps.Register` records one per App after the
  resource is installed. `tool.Builder.Register`, when `.UI()` was called,
  resolves the name to the App's URI and sets `def.Meta =
  apps.ToolMetaFor(ToolLink{ResourceURI, Visibility})`.
- **Fail-loud.** An unresolved `.UI(name)` (no App registered under that name)
  is a typed error at `Register` that names the tool, the missing App, and the
  fix — never a silent no-op (the trap that cost the upstream debugging
  session). This imposes an **apps-before-tools ordering** rule, which the
  templates and real projects already satisfy (`registerApp` before
  `registerTools`).
- **Per-tool visibility.** `UI(name, visibility ...string)` gains an optional
  visibility variadic; `tool.VisibilityModel` / `tool.VisibilityApp` are
  re-exported so an author need not import `runtime/apps`. Omitted → the
  `visibility` key is absent (a host treats it as both — the spec default);
  `VisibilityApp` alone marks a UI-only action tool.

**Why Option B over the convention (Option A).** Option A reconstructs the URI
as `ui://<server.Info().Name>/<uiResource>`. But `apps.App.URI` is developer-set
and only validated for the `ui://` scheme — a custom URI would silently
mismatch, re-introducing exactly the silent-failure class being fixed. The seam
handles any URI and matches the "Apps layer consumes it" doc intent. The import
direction is safe: `apps`→`server`, `tool`→`server`, and `tool`→`apps` adds no
cycle (`apps` does not import `tool`).

**The guard is a framework regression test, not a new user gate.** The bug was
a *framework* defect; a user's `dockyard validate` passed because their manifest
was correct, so no user-facing gate "would have caught it." The regression
guard is the new `runtime/tool` builder test (`.UI().Register()` emits
`_meta.ui.resourceUri`, exercising the builder path that `TestRegisterAndDiscover`
skipped by calling `server.AddTool` with hand-built meta) + the fail-loud
`Register` + the existing static `checkToolUIMappings`. `validate`/`testgate`
stay static by design (D-082); a runtime `_meta.ui`-present assertion would need
an ephemeral-server run (D-081) and is a V2-BACKLOG follow-up.

**Behaviour change (semver — minor, per D-159).** A previously silent
`.UI("typo")` now errors at `Register`. A correctly-ordered project (App before
tool) is unaffected and newly gets the `_meta.ui` it always should have had.
Called out in `CHANGELOG.md`.

---

## D-174 — `@dockyard/bridge` / `@dockyard/ui` are renamed to unscoped `dockyard-bridge` / `dockyard-ui` (supersedes D-172's naming)

**Date:** 2026-05-29
**Status:** Settled (v1.5 wave A). Supersedes the **package-naming** part of
D-172 (the packaging-as-source, version-policy, and gated-publish-job
decisions of D-172 stand unchanged).
**Where it lives:** `web/bridge/package.json`, `web/ui/package.json` (the
`name` fields), every live consumer (`web/inspector`, both templates' `web/`,
`web/{bridge,ui}/src`), `.github/workflows/release.yml` (the publish job),
`docs/RELEASING.md`, the skills, and the docs-site pages.

**The problem.** D-172 published the packages under the **`@dockyard` scope**.
The v1.4.0 release built binaries + a GitHub Release successfully, but the
`npm-publish` job **404'd**: `@dockyard` is an npm **org** the maintainer
cannot create, and npm masks an unauthorized scoped publish as a 404. (The
`NPM_TOKEN` secret had also expired — the other half of the failure.) So the
packages never published.

**The decision.** Rename both to **unscoped** names — `@dockyard/bridge` →
`dockyard-bridge`, `@dockyard/ui` → `dockyard-ui` — which publish under the
maintainer's personal npm account (`hurtener`) with **no scope/org to own**.
`@dockyard/inspector` is **not** renamed: the inspector frontend is never
published to npm (it is bundled into the `dockyard` binary), so it keeps its
internal `@dockyard/inspector` workspace name.

**Verified before committing to the rename (the friction that prompted the
check):** the names `dockyard-bridge` / `dockyard-ui` are free on npm, and the
personal token authenticates as `hurtener` — any authenticated account can
create a new unscoped public package with a free name, so the publish will
succeed. (A scoped `@hurtener/...` personal-scope name would also have worked
since `hurtener` is the username; unscoped was chosen for simplicity and to be
robust against the username/scope-ownership class of failure entirely.)

**Why not keep `@dockyard` and create the org.** The maintainer cannot create
the `@dockyard` org, and an org publish needs the token's account to own the
scope. Unscoped sidesteps scope ownership completely — the exact failure mode
that blocked v1.4.0.

**History is not rewritten.** D-172 keeps its `@dockyard/*` names as the record
of what was decided then; the past phase/wave plans, the v1.0.0 release
transcript, and the 1.3.0/1.4.0 `CHANGELOG` sections keep the old names too.
This entry is the supersession of record. The v1.4.0 npm publish that 404'd is
simply abandoned (nothing was published under `@dockyard`); v1.5 is the first
successful npm release, under the unscoped names.

**A downstream App's imports change** from `@dockyard/bridge` / `@dockyard/ui`
to `dockyard-bridge` / `dockyard-ui`; the templates and the
`attach-a-ui-resource` skill are updated in the same PR (§19). Because nothing
was ever published under `@dockyard`, there is no npm deprecation/redirect to
manage.

---

## D-175 — `require_spec_compliance` is enforced (it gates the spec-compliance check), closing a declared-but-dead quality gate

**Date:** 2026-05-29
**Status:** Settled (v1.5 wave A). Same enforcement class as D-168.
**Where it lives:** `internal/validate/checks.go` (`checkSpecCompliance` early-returns
when the flag is off); `internal/testgate` inherits it (the spec-compliance
category delegates to `validate.Run`).

**The finding (a wiring-audit sweep, the item-1 / D-168 class).** A framework-wide
audit for "declared-but-never-wired" friction found that the
`quality.require_spec_compliance` manifest flag was **inert**: the scaffold and both
templates set it `true` and the `quality:` block is documented "enforced by
`dockyard validate`" (RFC §9.4), but **no code read `Quality.RequireSpecCompliance`** —
`checkSpecCompliance` ran *unconditionally*. So toggling the flag changed nothing in
either direction: `false` did not opt out, `true` did nothing extra. This is exactly
the gate-that-lies class D-168 fixed for `require_fixtures` / `require_contract_tests`.

**The decision.** Enforce, do not remove (D-168's reasoning). `checkSpecCompliance`
now early-returns when `Quality.RequireSpecCompliance` is false — making the flag an
opt-out gate consistent with the other six `quality.*` gates (`checkUIStates` /
`checkFixtures` / `checkContractTests` each respect their flag). `dockyard test`'s
spec-compliance category delegates to `validate.Run`, so the gate propagates there
for free.

**Behaviour.** All eight in-repo manifests (scaffold, both templates, all five
examples, the loader testdata) already set `require_spec_compliance: true`, so no real
project changes behaviour. A manifest that omits the flag (zero value) now opts out of
the spec-compliance check — matching the other gates, which also default off and are
opted into by the scaffold. A project that sets it `false` genuinely opts out where
before the check fired regardless. Proven by a gate-bites test
(`TestRun_SpecComplianceGateRespectsFlag`): a withheld vendored spec is a `CheckSpec`
Blocker with the flag on, and skipped with it off.

**The audit's other findings (recorded, not all fixed here).** The same sweep
confirmed the framework's core wiring is sound (every `runtime/apps.App` field reaches
the wire; every CLI flag and `scaffold.Options` field is consumed). It also fixed two
bridge wiring gaps in this wave (see CHANGELOG): `ui/resource-teardown` was documented
as tearing the View down via `BridgeShell.close()` but was never dispatched; and the
negotiated `protocolVersion` / `hostInfo` from `ui/initialize` were discarded despite
`protocol.ts` promising retention. Deferred to `docs/V2-BACKLOG.md` (enhancements, not
broken wires): populating `obs ToolCallPayload.ContractOK` (the doc allows nil =
"not checked"; wiring it needs a `Recorder.ToolCall` API change across three packages),
and surfacing `InputPrompt.Schema` to the requestor (no V1 wire surface carries it).
The reserved `obs/v1` kinds `app.user_action` / `host.compat` / `app.bridge` have no
V1 server-side producer by design ("Dockyard sees only its half of the iframe bridge")
— reserved contract surfaces, left as-is.

---

## D-176 — `_meta.ui.domain` is a host-supplied verbatim value; server-side auto-derivation is retired

**Date:** 2026-05-30
**Status:** Settled (v1.6 wave A — MCP Apps spec-alignment). **Supersedes
D-062 and D-063**; amends RFC §7.5.
**Where it lives:** RFC §7.5, `runtime/apps` (`apps.go`, `domain.go`,
`hostprofile.go`), `runtime/server` (`server.go` — the stdio guard), plan
`docs/plans/v1.6-wave-A-apps-spec-alignment.md`, glossary (**Dedicated origin**,
**Domain label**).

**Why.** The vendored MCP Apps spec
(`docs/specifications/mcp-apps-2026-01-26.mdx:205-300`) defines `domain` as
**host-dependent**: *"Servers MUST consult host-specific documentation for the
expected domain format … If omitted, Host uses default sandbox origin."* The host
**mints** the value; a server copies it verbatim or leaves it empty. Dockyard
instead **auto-derived** Claude's `{hash}.claudemcpcontent.com` subdomain itself
(D-062/D-063), which (a) re-implements a host's internal algorithm — the "host
matrix" drift CLAUDE.md §6 forbids, in derivation-function clothing — and (b) is
**rejected by Claude Desktop on a local connector** (the exact error an upstream
team hit building an inline App on v1.5.0).

**The decision.** `App.Domain` is the host-supplied dedicated origin, emitted on
`resources/read` `_meta.ui.domain` **byte-for-byte**. `apps.resourceMeta` no
longer routes `Domain` through `DerivedDomain`; it passes `App.Domain` straight
to the codec. An empty `Domain` omits `_meta.ui.domain` (the host's default
per-conversation origin). The synthesising Claude host profile
(`hostprofile_claude.go`) is **deleted**; `runtime/apps` ships only the generic
verbatim profile.

**The seam is kept, the synthesis is retired (P3 / §4.4).** The `HostProfile`
interface, `RegisterHostProfile`, the registry, and `RequiresServerURL` survive
so a future *legitimate*, host-documented-and-blessed transform has a home; only
the synthesising Claude derivation is gone. `DerivedDomain` remains a verbatim
passthrough via the generic profile (signature unchanged). The testgate
capability category still resolves every registered profile through the seam.

**Deprecate, not remove (locked open question).** `App.HostProfile` and
`App.ServerURL` are **deprecated** (godoc + no longer drive any derivation),
**not removed** — removing public fields is a breaking change (D-159 → major),
and this wave is a **minor** bump. They are ignored: setting `HostProfile:
"claude"` now yields `Domain` verbatim, not a derived origin. Removal is a future
major.

**Stdio guard (the static-feasible half of the feedback's §5).** A server whose
only transport is **stdio** (`ServeStdio`) with any registered App carrying a
non-empty `Domain` logs a loud `slog.Warn` at startup naming the App — a
dedicated origin is honoured only on a remote connector; a local (stdio)
connector ignores it. `HTTPHandler` does not warn. `validate`/`testgate` stay
static by design (D-082), so a *static* `domain`-on-stdio gate would need
`domain` surfaced as a manifest field (Go-only today) — recorded as a follow-up,
not built here.

**Behaviour change (semver — minor, D-159).** A project that set `HostProfile:
"claude"` + `Domain` previously got a derived `claudemcpcontent.com` origin and
now gets its `Domain` verbatim. Called out in `CHANGELOG.md`. The default
scaffold/templates set no `Domain`, so no in-repo project changes wire output.

**Non-goal (tracked).** Why Claude Desktop renders the reference app
(`pengui-slides`) locally but not a wire-matched Dockyard App is an
**investigation**, not a known Dockyard defect — tracked as a `docs/V2-BACKLOG.md`
spike, not gated into this wave.

---

## D-177 — An opt-in additionally emits the deprecated flat `_meta` tool-UI key; the default stays nested-only

**Date:** 2026-05-30
**Status:** Settled (v1.6 wave A — MCP Apps spec-alignment).
**Where it lives:** `runtime/server` (`Options.EmitLegacyToolUIMeta`,
`Server.EmitLegacyToolUIMeta`), `runtime/tool/builder.go`, `runtime/apps`
(`ToolLink.EmitLegacyResourceURI`, `ToolMetaFor`), `internal/protocolcodec`
(`AppsToolMeta.EmitLegacyResourceURI`, the tool-meta encoder), plan
`docs/plans/v1.6-wave-A-apps-spec-alignment.md`.

**Why.** The reference `@modelcontextprotocol/ext-apps` SDK emits **both** the
nested `_meta.ui.resourceUri` and the deprecated flat tool-UI key "for backward
compatibility." Dockyard emits nested-only by RFC §7.1 + brief 01 §2.3, and the
2026-01-26 spec marks the flat form deprecated — so the **default does not
change**. But because some hosts may still read the flat key, a server-level
opt-in lets a developer emit both without betting the RFC-compliant default on an
**unproven** change (the upstream feedback notes Claude Desktop did not render
even with both keys present).

**The decision (knob surface locked).** A server-level boolean —
`server.Options.EmitLegacyToolUIMeta` (default **false**) — threads through
`Server.EmitLegacyToolUIMeta()` → the `runtime/tool` builder's `.UI()` wiring →
`apps.ToolLink.EmitLegacyResourceURI` → the `internal/protocolcodec` tool-meta
encoder, which then writes the flat key equal to the nested `resourceUri`
alongside the nested form. A server-wide boolean (not a manifest `compat:` block,
not a per-`.UI()` option) was chosen because the concern is **server-wide wire
compat**, not per-tool. No exported function signature changes — the opt-in
threads through configuration and an additive struct field. The
`protocolcodec`/`apps` "never emit the flat key" assertions stay green as the
**default-mode** tests; a new golden pins the both-keys output. The flat key
remains **tolerated on read** in both modes; the P3 "tolerate on read, never
emit" rule becomes "…never emit *unless explicitly opted in*."

---

## D-178 — The scaffold adopts the html-style `ui://<server>/<app>/index.html` URI convention

**Date:** 2026-05-30
**Status:** Settled (v1.6 wave A — MCP Apps spec-alignment).
**Where it lives:** `templates/{analytics-widgets,approval-flows}`
(`dockyard.app.yaml` `uri:`, `main.go.tmpl` `appURI` const + docstrings),
`skills/attach-a-ui-resource/SKILL.md`, `docs/site` Apps guide, plan
`docs/plans/v1.6-wave-A-apps-spec-alignment.md`.

**Why.** The reference app uses an html-style resource URI
(`ui://deck-editor/index.html`); Dockyard scaffolded `ui://<server>/<app>` (no
extension). Some hosts may key off the html-style path, and matching the
reference removes one more diff an upstream team has to reconcile.

**The decision.** `dockyard new --template` scaffolds the App `ui://` URI as
`ui://<server>/<app>/index.html`. The framework treats `ui://authority/path` as
an **opaque string**, so this is a **convention + docs change only** — no
behaviour change: `validate` / `build` / App registration accept the new URI
exactly as before, and an existing project's `ui://<server>/<app>` URI keeps
working (the docs say so explicitly). The blank `dockyard new` scaffold has no UI
resource, so only the two product templates carry the convention (a documented
deviation from the plan's "blank + --template" wording — blank has no `ui://` to
change).

---

## D-179 — `dockyard-bridge` `ui/initialize` uses the MCP Apps `ui/` dialect, not the base-MCP request shape

**Date:** 2026-06-01
**Status:** Settled (bugfix — App rendered blank against a spec-compliant host).
**Where it lives:** `web/bridge/src/bridge.ts` (`runHandshake`), `web/bridge/src/protocol.ts` (`InitializeParams`), `web/bridge/src/__tests__/{bridge.test.ts,harness.ts}`.

**Why.** A Dockyard App that is correct on paper — nested+flat `_meta.ui`,
`text/html;profile=mcp-app`, `ui/initialize` @ 2026-01-26 — rendered blank/white
in Claude Desktop while never erroring visibly. The View-side bridge sent its
`ui/initialize` request in the **base-MCP** field shape
(`{capabilities:{appCapabilities}, clientInfo}`). A spec-compliant host
(`@modelcontextprotocol/ext-apps`) validates the request against a strict schema
requiring the **`ui/` dialect**: top-level `{appInfo, appCapabilities,
protocolVersion}` with **`appInfo` REQUIRED**. The SDK's `parseWithCompat` runs
*before* the request handler; with `appInfo`/`appCapabilities` absent, Zod throws,
the SDK returns a JSON-RPC error carrying the request id, the bridge's
`transport.request('ui/initialize')` rejects, `runHandshake` throws before
`ready` flips, `connect()` rejects, and the App never paints. The Dockyard
inspector accepted the base-MCP shape, so this passed locally — the inspector's
host and the bridge's View shared the same non-spec assumption (see D-181 note).

**The decision.** The View sends the `ui/` dialect shape: `{protocolVersion,
appCapabilities, appInfo}`. The public `BridgeOptions.clientInfo` source option is
retained (it maps to the wire `appInfo` value), so no consumer API breaks. The
result side already matched the host's result schema and is unchanged. Regression
guard: a test asserts the emitted params equal the dialect shape **and** that the
legacy `capabilities`/`clientInfo` keys are absent.

---

## D-180 — `dockyard-bridge` SENDS `ui/notifications/initialized`; it never awaits one

**Date:** 2026-06-01
**Status:** Settled (bugfix — handshake deadlock against a spec host).
**Where it lives:** `web/bridge/src/bridge.ts` (`runHandshake`), `web/bridge/src/__tests__/{bridge.test.ts,harness.ts}`.

**Why.** `runHandshake` subscribed and **awaited receipt** of
`ui/notifications/initialized` before resolving `ready`. Per the JSON-RPC/MCP
lifecycle and the `@modelcontextprotocol/ext-apps` reference View, the View is the
initiator: after the `ui/initialize` result it **sends** `initialized` and is
immediately ready. A spec-compliant host never emits a View→host notification, so
the old code deadlocked — both sides waited, `ready` never resolved, blank App.
`protocol.ts` already classified `initialized` as a View→host notification; only
`bridge.ts` used it backwards. (This sat behind D-179 — the handshake failed at
`ui/initialize` first, so the deadlock was unreachable until D-179 was fixed.)

**The decision.** After the `ui/initialize` result the bridge calls
`transport.notify(ViewNotification.initialized, {})`, sets `initialized = true`,
and flips `ready`. An inbound `initialized` from a non-spec host is ignored
(no deadlock, no double-ready). The in-test host harness no longer auto-sends
`initialized` host→View, matching a real spec host; a regression test asserts
`ready` flips when the host sends nothing and that an inbound `initialized` is
harmless.

---

## D-181 — `dockyard-bridge` reports View content size via `ui/notifications/size-changed`

**Date:** 2026-06-01
**Status:** Settled (bugfix — collapsed iframe rendered blank).
**Where it lives:** `web/bridge/src/bridge.ts` (`startSizeReporting`, `close`), `web/bridge/src/__tests__/bridge.test.ts`.

**Why.** The bridge only ever **received** `ui/notifications/size-changed`
(host→View); it never measured or reported its own content height. A
spec-compliant host sizes the App iframe from the View's report (the ext-apps
reference View does this with a `ResizeObserver` under `autoResize`); without it
the iframe collapses to ~0px and the App looks blank even after it paints. (Like
D-180, this was masked by D-179 — content never rendered to be measured.)

**The decision.** On `ready` the bridge starts a `ResizeObserver` that measures
`document.documentElement` under `fit-content`, adds the scrollbar gutter, and
emits a de-duplicated, `requestAnimationFrame`-throttled `size-changed`
(View→host) on ready and on every change, torn down in `close()`. It no-ops
without a DOM / `ResizeObserver` (unit env). The wire method string
`ui/notifications/size-changed` is shared with the host→View direction; the
View→host emit reuses it. A regression test stubs `ResizeObserver` + a synchronous
`requestAnimationFrame` and asserts a `size-changed` is sent after `ready`.

**Inspector follow-up (noted, not in this change).** All three bugs were invisible
to the Dockyard inspector because its host accepted the non-spec View behaviour.
The inspector's host-side handshake should be validated against the official
`@modelcontextprotocol/ext-apps` schemas so the inspector and a real host agree;
filed as a V2-backlog hardening item. **Superseded as a follow-up by D-182**,
which makes the vendored ext-apps schema the conformance source for both the
bridge and the inspector host (v1.7 wave A).

---

## D-182 — The View bridge wire layer is derived from + conformance-tested against the vendored ext-apps schema (option B)

**Date:** 2026-06-01
**Status:** Settled (v1.7 wave A — bridge spec-conformance). Supersedes the
implicit "hand-derive the View wire layer" stance (Phase 11). Amends RFC §7.3;
extends RFC §10/§16. Subsumes the D-181 inspector-hardening follow-up.
**Where it lives:** `web/bridge/src/spec/ext-apps-schema.ts` (vendored),
`web/bridge/src/protocol.ts` (derived types), `web/bridge/src/__tests__/conformance.test.ts`,
`web/inspector/src/host/host-bridge.ts`, RFC §7.3, plan
`docs/plans/v1.7-wave-A-bridge-spec-conformance.md`.

**The problem.** `web/bridge` hand-transcribed the MCP Apps `ui/` wire dialect —
method names, notification shapes, capability and `hostContext` types, the
handshake lifecycle. A hand-transcription drifts silently: the v1.6.1 bugs
(D-179/D-180/D-181) were three transcription errors that no test caught because
the only thing that validated the wire was the equally-lenient inspector host. A
post-mortem diff against the official `@modelcontextprotocol/ext-apps` schema
(`src/generated/schema.ts`) found four more latent divergences
(`appCapabilities.availableDisplayModes` vs our `displayModes`; `ui/resource-teardown`
being a request we treat as a notification; `containerDimensions` flexible-sizing;
host fonts). RFC §8.2 had **already** chosen "vendor the schema, generate the wire
layer" for the Tasks shim and §5.4 for the Go `protocolcodec` seam — the View
bridge simply never got the same discipline because Phase 11 predated a stable
published ext-apps schema.

**The decision.** Apply the §8.2 pattern to the View half:

1. **Vendor** the machine-readable ext-apps schema into the repo, pinned by
   upstream commit SHA + date (RFC §10, alongside the existing
   `docs/specifications/mcp-apps-2026-01-26.mdx`). It is not hand-edited.
2. **Derive** the bridge's wire types from the vendored schema by type-inference
   (`z.input`/`z.infer`) rather than hand-declaring them — a mis-named or
   wrong-shaped field becomes a `tsc` error.
3. **Conformance-test** the bridge's outbound wire (the `ui/initialize` params and
   every View→host request/notification) by `.parse()`ing it against the vendored
   schema. The `ui/initialize` case is the regression that would have caught
   D-179; the same layer hardens the **inspector host**, which validates inbound
   View messages against the schema and becomes a faithful spec host (resolves
   ready on receiving the View's `initialized`, no host-sent `initialized`, sends
   `ui/resource-teardown` as a request).
4. **Keep the runtime Zod-free** (RFC §7.4): the vendored schema is reachable only
   via `import type` on runtime paths and `.parse()` in tests; a bundle guard
   asserts no schema-validation runtime leaks into a single-file App bundle.

**What this is not.** Not "import the official SDK at runtime" — that would ship
Zod into every App bundle and breach §7.4. The lean, Svelte-idiomatic runtime
binding (stores, `callContract`, view-state) stays Dockyard's — only the *wire
layer* is sourced from upstream. **Infer over generate:** type-inference from the
vendored Zod schema is chosen over a bespoke `.ts` generator (nothing to
maintain; the types are the spec by construction); a generator remains the
fallback if a build tool fails to tree-shake the type-only import. **Vendor over
npm devDep:** the schema is vendored as a file (RFC §10 hermetic-pin culture),
with the npm `@modelcontextprotocol/ext-apps` devDep as the fallback if the
schema's import closure is impractical to vendor. Both fallbacks are recorded as
open questions in the plan and locked at implementation. The four conformance
fixes (A–D) ride this decision as acceptance criteria, not separate decisions —
they are what the schema forces into the open.

**Implementation notes (deviations, recorded per CLAUDE.md §4.3 — v1.7 wave A).**

- **The schema is referenced only by the test layer; `protocol.ts` imports
  neither it nor Zod.** `dockyard-bridge` is published as *source* (D-172), so a
  schema import in the runtime/public type graph would force every consuming
  project to install `zod` + `@modelcontextprotocol/sdk` just to type-check the
  bridge. Keeping the schema test-only preserves the consumer zero-dep property
  and the Zod-free App bundle. The plan's "derive the types in `protocol.ts` by
  inference" is therefore realised as: hand-written clean public types in
  `protocol.ts`, **pinned by the runtime `conformance.test.ts`** that `.parse()`s
  the bridge's emitted wire against the schema. This is in fact the *stronger*
  guard — a structural `extends` assertion cannot catch a renamed field (excess
  properties pass), whereas the round-trip `.parse()` does (it caught item A).
- **The "bundle is Zod-free" guard is the static "schema referenced only by
  tests" check** (`scripts/smoke/v1.7-wave-A.sh`), not a build-and-grep of a
  template's output. Zod can only enter the App bundle via a runtime import,
  which the static check forbids — a stronger and cheaper guarantee than
  inspecting a built artifact.
- **Pure file-vendoring was impractical → `zod` + `@modelcontextprotocol/sdk`
  are `web/bridge` devDeps** (the documented fallback). The upstream
  `src/generated/schema.ts` imports seven base schemas from
  `@modelcontextprotocol/sdk/types.js`, so vendoring the ext-apps schema
  self-contained would mean vendoring the entire SDK types closure. The ext-apps
  schema file itself is vendored by SHA (commit `7d4434e`, 2026-06-01); its
  `zod` + SDK imports resolve to devDeps.
- **The inspector host validates inbound against the schema** (item 4, completed).
  It `.safeParse()`s the View's `ui/initialize` params against
  `McpUiInitializeRequestSchema` and rejects a non-spec shape with a JSON-RPC
  `-32602` error — so the inspector now *catches* what only a real host used to
  (a test sends the base-MCP `{capabilities, clientInfo}` shape that caused D-179
  and asserts the rejection). The vendored schema is shared via a new
  **`dockyard-bridge/spec` subpath export** — the opt-in, **Zod-bearing** surface;
  `web/inspector` adds its own `zod` + `@modelcontextprotocol/sdk` devDeps. The
  package's **`.` entry stays Zod-free** (the consumer zero-dep guarantee holds
  for App authors; only `./spec`, imported by tooling/tests, pulls Zod).

---

## D-183 — Dockyard's Tasks×Apps `ui/` notifications are explicit extensions outside the MCP Apps schema

**Date:** 2026-06-01
**Status:** Settled (v1.7 wave A — bridge spec-conformance).
**Where it lives:** `web/bridge/src/dockyard-ext.ts`, the `web/bridge`
conformance test, `skills/attach-a-ui-resource/SKILL.md`, the docs-site Apps
guide, plan `docs/plans/v1.7-wave-A-bridge-spec-conformance.md`.

**Why.** `ui/notifications/task-progress` and
`ui/notifications/elicitation-response` are emitted by the bridge for the
Tasks×Apps surface (RFC §8, D-134) but are **not** in the vendored MCP Apps
schema — they are Dockyard extensions. Under D-182's conformance layer they would
read as "drift" unless explicitly fenced. More importantly they are **not
portable**: a stock host (e.g. Claude Desktop) does not implement them, so a
Tasks-augmented App's progress/elicitation behaviour works only against a
Dockyard-aware host (the inspector, or Harbor as the MCP client).

**The decision.** These two notifications live in a clearly-named `dockyard-ext`
module, separate from the conformed `protocol.ts` surface, documented as Dockyard
extensions that require a Dockyard-aware host. The D-182 conformance layer asserts
they are the **only** View→host messages the bridge emits that are absent from the
vendored schema — so the extension boundary is explicit and can't silently grow.
The `attach-a-ui-resource` skill and the docs-site Apps guide state the host
requirement, so an App author does not expect task progress / elicitation to work
on a stock host. This neither promotes the extensions into the spec nor removes
them — it fences them honestly. A future upstream MCP Apps Tasks integration would
let these migrate from `dockyard-ext` into the conformed surface.
