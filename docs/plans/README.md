# Dockyard — Master Phase Plan

## How to read this file

This is the canonical execution index for Dockyard's V1 build. Every individual
phase plan (`docs/plans/phase-NN-<slug>.md`) lives under it and inherits its
done-definition, dependency declarations, and coverage discipline.

- **Source of truth:** `RFC-001-Dockyard.md` (referenced as RFC §X.X). Every phase
  below traces to one or more RFC sections; if a phase plan and the RFC drift, the
  RFC wins (`AGENTS.md` §2).
- **Research substrate:** the six briefs in `docs/research/01..06` (index:
  `docs/research/INDEX.md`). Every phase cites the briefs informing it.
- **Numbering:** `phase-NN-<slug>.md`, two-digit zero-padded. Lettered suffixes
  (`09a`, …) insert work into a band without renumbering.
- **Done-definition (binding, `AGENTS.md` §4.2):** (a) all acceptance criteria
  pass; (b) coverage targets met; (c) `scripts/smoke/phase-NN.sh` shows
  `OK ≥ count(criteria)` and `FAIL = 0`; (d) prior phases' smoke scripts still pass.
- **Coverage defaults (override per phase):** 80% new packages; 85% the `Store`
  drivers and conformance-tested subsystems; 70% CLI / tooling.
- **Delivery:** wave-based (`AGENTS.md` §17). A wave is a coherent subsystem slice;
  its final phase bundles a `test/integration/waveN_test.go` end-to-end test, and a
  read-only checkpoint audit lands as a `chore(checkpoint)` PR before the next
  wave's planning. Phase implementation runs in worktrees / feature branches.

## Phase index

| #  | Name                                          | Subsystem              | RFC §            | Briefs    | Deps          | Cov. | Status  |
|---:|-----------------------------------------------|------------------------|------------------|-----------|---------------|-----:|---------|
| 00 | Repo skeleton & hygiene                       | repo / hygiene         | n/a              | —         | —             | n/a  | Shipped |
| 01 | Runtime library skeleton + go-sdk baseline    | runtime/server         | §3, §5           | 03        | 00            | 80%  | Shipped |
| 02 | `protocolcodec` seam + vendored specs         | internal/protocolcodec | §5.4, §16        | 01,02,03  | 00            | 85%  | Shipped |
| 03 | `Store` seam + sqlite + inmem + conformance   | runtime/store          | §13              | 06, 02    | 00            | 85%  | Shipped |
| 04 | Contract-first codegen + typed tool builder   | internal/codegen       | §6               | 06, 04    | 01            | 80%  | Shipped |
| 05 | Go → TypeScript codegen + drift cross-check   | internal/codegen       | §6.2             | 06, 04    | 04            | 80%  | Shipped |
| 06 | Manifest — `dockyard.app.yaml`                | internal/manifest      | §4.2             | 04, 01    | 04            | 80%  | Shipped |
| 07 | MCP server core — transports + security       | runtime/server         | §5               | 03, 01    | 01, 02        | 85%  | Shipped |
| 08 | Tool handler runtime — `Result`, content split| runtime/server         | §5, §6.3         | 01, 03    | 07, 04        | 85%  | Shipped |
| 09 | MCP Apps extension — server-side              | runtime/apps           | §7.1, §7.4       | 01, 03    | 07, 02, 06    | 85%  | Shipped |
| 10 | UI auto-discovery + embed pipeline            | runtime/apps           | §7.6, §14        | 06, 04    | 09            | 80%  | Shipped |
| 10a| UI design system, tokens & conventions        | web/ui, docs/design    | §7, §10, §12     | 04, 01    | 10            | n/a  | Shipped |
| 11 | Svelte bridge shell library                   | web/bridge             | §7.2, §7.3       | 01        | 09            | n/a  | Shipped |
| 12 | Host profiles + `_meta.ui.domain` derivation  | runtime/apps           | §7.5             | 01, 05    | 09            | 80%  | Shipped |
| 13 | MCP Tasks extension — server-side             | runtime/tasks          | §8.1, §8.2, §8.3 | 02, 03    | 07, 02        | 85%  | Pending |
| 14 | TaskStore + `TaskHandle` + task security      | runtime/tasks          | §8.4, §8.5, §15  | 02, 06    | 13, 03        | 85%  | Pending |
| 15 | `obs/v1` event model + headless emitter       | runtime/obs            | §11.1, §11.2     | 05        | 07            | 85%  | Pending |
| 16 | `obs/v1` transports — SSE + OTel + log bridge | runtime/obs            | §11.3            | 05, 01    | 15            | 80%  | Pending |
| 17 | `dockyard` CLI skeleton + `new`               | internal/cli, scaffold | §9, §10          | 04, 06    | 06            | 70%  | Pending |
| 18 | `dockyard generate` + `dockyard validate`     | internal/cli, codegen  | §6, §9.4         | 06, 01, 02| 17, 05, 09, 13| 75%  | Pending |
| 19 | `dockyard dev` — fsnotify orchestrator        | internal/devloop       | §9.2             | 06, 04    | 17, 18        | 75%  | Pending |
| 20 | `dockyard build` + `run` + `install`          | internal/cli           | §14              | 06, 01    | 17, 10        | 75%  | Pending |
| 21 | `dockyard test` — contract + compliance gate  | internal/cli           | §9.1, §9.4       | 04, 01    | 18            | 75%  | Pending |
| 22 | Inspector core — bridge host-half + obs view  | internal/inspector     | §12              | 05, 04, 01| 09, 10a, 11, 16| 80% | Pending |
| 23 | Inspector advanced + `dockyard inspect`       | internal/inspector     | §12              | 05, 04    | 22, 14, 21    | 80%  | Pending |
| 24 | Template system + `analytical-card`           | templates              | §10              | 04, 01    | 19, 20, 10a   | 75%  | Pending |
| 25 | `approval-flow` template                      | templates              | §10, §8.6        | 02, 01    | 24, 14        | 75%  | Pending |
| 26 | `inspector` template                          | templates              | §10              | 05, 01    | 24            | 75%  | Pending |
| 27 | Security pass + spec-compliance conformance   | runtime/*, test        | §15, §16         | 01,02,03  | 09, 13, 14    | 90%  | Pending |
| 28 | Examples, godoc, docs hygiene                 | docs / examples        | §2               | —         | 01–27         | n/a  | Pending |
| 29 | Agent skills & published tech-docs site       | skills / docs          | §1, §2           | 04        | 21, 26        | n/a  | Pending |
| 30 | V1 release engineering + cut                  | release                | §1, §14          | —         | 27, 28, 29    | n/a  | Pending |

**V1 critical path:** phases 01–30 plus 10a (31 phases beyond the skeleton), grouped
into ten waves. Post-V1 follow-ups (the ChatGPT Apps SDK, the multi-server console, the
remaining five templates, enterprise auth, `dockyard publish`) are tracked in
RFC §19, not numbered here.

## Wave structure

| Wave | Phases | Theme |
|-----:|--------|-------|
| 0 | 00 | Skeleton & hygiene (shipped) |
| 1 | 01, 02, 03 | Foundations — runtime skeleton, the `protocolcodec` and `Store` seams |
| 2 | 04, 05, 06 | Contracts — codegen pipeline + the manifest |
| 3 | 07, 08 | MCP server core |
| 4 | 09, 10, 10a, 11, 12 | The MCP Apps extension + the shared UI design system |
| 5 | 13, 14 | The MCP Tasks extension |
| 6 | 15, 16 | The `obs/v1` observability protocol |
| 7 | 17, 18, 19, 20, 21 | The `dockyard` CLI & developer experience |
| 8 | 22, 23 | The local inspector |
| 9 | 24, 25, 26 | Templates |
| 10 | 27, 28, 29, 30 | Hardening, conformance, docs & skills, and the V1 cut |

Each wave's final phase bundles a `test/integration/waveN_test.go` exercising the
wave's surface end-to-end with real drivers; a checkpoint audit (`AGENTS.md` §17)
gates the next wave.

---

## Per-phase detail

Format: **Phase NN — Name** (RFC §X.X). Each entry is the stub the per-PR phase
plan (`docs/plans/phase-NN-*.md`, from `_template.md`) expands. Acceptance criteria
become binding once the phase ships.

### Wave 1 — Foundations

#### 01 — Runtime library skeleton + go-sdk baseline (RFC §3, §5)

**Goal.** Establish `runtime/` as the importable app-runtime library and stand up a
minimal MCP server on `github.com/modelcontextprotocol/go-sdk` that boots over
stdio. Module layout per AGENTS.md §3; `cmd/dockyard` placeholder.
**Acceptance.** A trivial server registers one tool and serves it over stdio; SDK
version pinned; `CGO_ENABLED=0` build verified; package layout matches §3.
**Briefs.** 03. **Deps.** 00.

#### 02 — `protocolcodec` seam + vendored specs (RFC §5.4, §16)

**Goal.** The `internal/protocolcodec` package: the sole importer of MCP extension
wire formats, versioned codecs keyed on `protocolVersion`, typed `_meta` accessors.
Vendor the Apps spec (2026-01-26) and the Tasks experimental schema into
`docs/specifications/`, pinned by SHA.
**Acceptance.** Codecs round-trip Apps + Tasks `_meta` shapes; deprecated flat
`_meta["ui/resourceUri"]` tolerated on read, never emitted; no other package
imports raw extension types (enforced by a lint/test).
**Briefs.** 01, 02, 03. **Deps.** 00.

#### 03 — `Store` seam + sqlite + inmem + conformance (RFC §13)

**Goal.** The `Store` interface (driver pattern), an in-memory driver, a
`modernc.org/sqlite` driver, and a shared conformance suite every driver passes.
Forward-only migrations.
**Acceptance.** Both drivers pass the conformance suite; CGo-free; concurrent-reuse
test under `-race`; migration idempotency verified.
**Briefs.** 06, 02. **Deps.** 00.

### Wave 2 — Contracts

#### 04 — Contract-first codegen + typed tool builder (RFC §6)

**Goal.** Go contract structs → JSON Schema via `google/jsonschema-go`; the
contract-first tool builder API (`app.Tool(...).Input[T]().Output[T]()`).
**Note.** That fluent sketch is not legal Go — type parameters cannot sit on
methods. D-029 settled the shipped, Go-legal shape: the package-level generic
constructor `tool.New[In, Out](name)` binds the contract types at construction.
**Acceptance.** A Go struct generates a correct JSON Schema; the builder produces a
registered tool; golden tests cover generated output.
**Briefs.** 06, 04. **Deps.** 01.

#### 05 — Go → TypeScript codegen + drift cross-check (RFC §6.2)

**Goal.** Go contract structs → `web/src/generated/contracts.ts` via `tygo`
(Design A); the `validate` drift cross-check that fails on desync or stale output.
**Acceptance.** Generated TS compiles; schema↔TS drift is detected and fails;
golden tests on TS output.
**Briefs.** 06, 04. **Deps.** 04.

#### 06 — Manifest — `dockyard.app.yaml` (RFC §4.2)

**Goal.** The manifest schema, loader, and structural validation — tools, `apps`,
transports, `quality` knobs, `task_support`.
**Acceptance.** A valid manifest loads to a typed struct; invalid manifests fail
with source-located errors; Go type references resolve.
**Briefs.** 04, 01. **Deps.** 04.

### Wave 3 — MCP server core

#### 07 — MCP server core — transports + security (RFC §5)

**Goal.** Tool/resource registration over go-sdk; stdio + streamable-HTTP
transports; security options (DNS-rebinding, Origin/Content-Type, cross-origin) set
explicitly; `InMemoryTransport` wired for tests.
**Acceptance.** A server serves over both transports; security options asserted
set; `getServer` per-request seam exercised.
**Briefs.** 03, 01. **Deps.** 01, 02.

#### 08 — Tool handler runtime — `Result`, content split (RFC §5, §6.3)

**Goal.** The `Result[Out]` handler return shape; mapping to `CallToolResult` with
model-facing text in `content` and typed UI data in `structuredContent`; argument
validation at the catalog edge.
**Acceptance.** A handler's typed output lands in `structuredContent`; oversized/
misrouted payloads flagged; invalid args produce typed errors.
**Briefs.** 01, 03. **Deps.** 07, 04.

### Wave 4 — The MCP Apps extension

#### 09 — MCP Apps extension — server-side (RFC §7.1, §7.4)

**Goal.** `ui://` resource registration with `text/html;profile=mcp-app`; `_meta.ui`
on tools (nested form) and on resource-read responses (CSP/domain/permissions);
`extensions` capability negotiation; plain-MCP graceful degradation.
**Acceptance.** A tool↔`ui://` resource pair is discoverable; CSP defaults
deny-by-default; a non-Apps host still gets working tools.
**Briefs.** 01, 03. **Deps.** 07, 02, 06.

#### 10 — UI auto-discovery + embed pipeline (RFC §7.6, §14)

**Goal.** Convention discovery of `web/src/apps/*.svelte` → `ui://` resources with
the wiring written to the manifest; the Vite build → `//go:embed all:dist`
pipeline with correct build ordering.
**Acceptance.** A dropped `.svelte` file registers as a resource; the embedded
bundle serves over both the MCP resource handler and HTTP; build fails cleanly if
`dist/` is absent.
**Briefs.** 06, 04. **Deps.** 09.

#### 10a — UI design system, tokens & conventions (RFC §7, §10, §12)

**Goal.** Establish Dockyard's shared frontend foundation **before any page is
built**, so the inspector, the template App UIs, and the docs site never duplicate
components or fork patterns — the lesson from Harbor's late, costly design-system
remediation (`docs/design/CONVENTIONS.md`, `AGENTS.md` §20). Deliver: the design
tokens (colour, spacing, typography, radius); the shared Svelte `web/ui/` component
inventory (`AppShell`, `PageHeader`, `FilterBar`, `DataTable`, `Pagination`,
`RailCard`, `StatusChip`, the four-state `PageState` + its `Loading`/`Empty`/
`Error`/`Permission` panels, `MetricCard`, `JsonInspector`, …); and the filled
`docs/design/CONVENTIONS.md` §3 inventory + the spec→mockup→build process. The
concrete spec is `docs/design/design-spec.md`. The Dockyard logo and brand are
produced here. Template-specific blocks (e.g. an `ApprovalPanel`) are NOT in the V1
`web/ui` inventory — they land with their template phase.
**Acceptance.** Every component in the `web/ui/` inventory exists and is documented
in CONVENTIONS.md §3; design tokens are the single source of visual truth; the
logo + an approved visual mockup of the **inspector** exist (template mockups are
deferred to their own phases 24–26, since the template set may be reworked before
Wave 9); the `AGENTS.md` §20 hygiene rule is in force and reflected in the §14
checklist.
**Briefs.** 04 (DX). **Deps.** 10.

#### 11 — Svelte bridge shell library (RFC §7.2, §7.3)

**Goal.** `web/bridge/` — the `ui/` `postMessage` dialect: `ui/initialize`
handshake, `hostContext` stores, host→view notification fan-out, typed view→host
helpers, display-mode negotiation (inline/fullscreen/pip), `viewUUID` view-state.
**Acceptance.** The handshake completes against a test host; all three display
modes negotiate; `contracts.ts` consumed for typed `structuredContent`.
**Briefs.** 01. **Deps.** 09.

#### 12 — Host profiles + `_meta.ui.domain` derivation (RFC §7.5)

**Goal.** Pluggable host profiles carrying derivation functions; auto-derive
`_meta.ui.domain`, including Claude's SHA-256 signed origin.
**Acceptance.** The domain is auto-derived; the Claude profile produces the correct
signed `claudemcpcontent.com` form; profiles register via the seam.
**Briefs.** 01, 05. **Deps.** 09.

### Wave 5 — The MCP Tasks extension

#### 13 — MCP Tasks extension — server-side (RFC §8.1, §8.2, §8.3)

**Goal.** `tasks/*` method routing, `tasks` capability advertisement,
`CreateTaskResult` substitution for task-augmented `tools/call`; the wire layer
hand-derived from the vendored experimental schema and pinned by golden tests
(D-069 — a `ts → Go` generator is disproportionate for one schema file); the
five-status lifecycle.
**Acceptance.** A task-augmented call returns `CreateTaskResult`; `tasks/get`/
`result`/`cancel`/`list` behave per spec; lifecycle transitions enforced.
**Briefs.** 02, 03. **Deps.** 07, 02.

#### 14 — TaskStore + `TaskHandle` + task security (RFC §8.4, §8.5, §15)

**Goal.** The durable `TaskStore` on the `Store` seam; the `TaskHandle` handler API
(progress, status, cooperative cancellation, `input_required` elicitation); TTL,
per-requestor concurrency caps, purge sweep; crypto-strong IDs + auth binding.
**Acceptance.** A long handler reports progress and is cancellable; TTL purge
works; cross-context task access rejected; `tasks/list` withheld when unauthed.
**Briefs.** 02, 06. **Deps.** 13, 03.

### Wave 6 — Observability

#### 15 — `obs/v1` event model + headless emitter (RFC §11.1, §11.2)

**Goal.** The canonical `obs.Event` model + event kinds; a non-blocking headless
emitter; the in-memory ring buffer; W3C Trace Context IDs; shape+size default
capture.
**Acceptance.** Tool/resource/app/task events emit; the emitter never blocks on a
slow consumer; the ring buffer serves recent events.
**Briefs.** 05. **Deps.** 07.

#### 16 — `obs/v1` transports — SSE + OTel + log bridge (RFC §11.3)

**Goal.** The out-of-band localhost SSE sink; the optional `OTelEmitter` adapter
(MCP semconv); bridge the MCP `logging` capability into `obs/v1` `log` events.
**Acceptance.** The SSE sink streams without corrupting a stdio pipe; OTel spans
carry `mcp.*`/`gen_ai.*` attributes; `notifications/message` surface as `log` events.
**Briefs.** 05, 01. **Deps.** 15.

### Wave 7 — The CLI & developer experience

#### 17 — `dockyard` CLI skeleton + `new` (RFC §9, §10)

**Goal.** The cobra command tree; `dockyard new` scaffolding a blank MCP server
(no template) — manifest, one example tool, contracts, tests.
**Acceptance.** `dockyard new` produces a project that builds and serves; the
no-template path is first-class.
**Briefs.** 04, 06. **Deps.** 06.

#### 18 — `dockyard generate` + `dockyard validate` (RFC §6, §9.4)

**Goal.** `generate` (schema + TS, Design A); `validate` (manifest, schemas,
tool↔UI mappings, MIME, spec compliance, UI states, stale-codegen drift).
**Acceptance.** `generate` is idempotent; `validate` exits non-zero on each
build-blocker class; stale generated output fails.
**Briefs.** 06, 01, 02. **Deps.** 17, 05, 09, 13.

#### 19 — `dockyard dev` — fsnotify orchestrator (RFC §9.2)

**Goal.** The embedded `fsnotify` dev orchestrator: restart the Go server on `.go`
changes, re-run codegen on contract changes, supervise the Vite dev server — one
process tree.
**Acceptance.** Editing a contract regenerates types live; the Go server restarts;
Svelte HMR works; one `dockyard dev` process, no external dev tool.
**Briefs.** 06, 04. **Deps.** 17, 18.

#### 20 — `dockyard build` + `run` + `install` (RFC §14)

**Goal.** `build` (Vite → `go build`, embed ordering, cross-compile matrix +
checksums); `run --transport`; `install claude|cursor` (write host config, verify
boot).
**Acceptance.** One CGo-free static binary embeds the UI; cross-compile matrix
green; `install` writes valid host config and confirms the server boots.
**Briefs.** 06, 01. **Deps.** 17, 10.

#### 21 — `dockyard test` — contract + compliance gate (RFC §9.1, §9.4)

**Goal.** `dockyard test`: `go test` + contract tests + fixture/golden snapshots +
spec-compliance + capability-degradation tests.
**Acceptance.** The command runs all categories; a contract regression fails it; a
spec-compliance violation fails it.
**Briefs.** 04, 01. **Deps.** 18.

### Wave 8 — The local inspector

#### 22 — Inspector core — bridge host-half + obs view (RFC §12)

**Goal.** The inspector's host half of the `ui/` bridge; sandboxed App rendering;
the live `obs/v1` stream view + JSON-RPC log. Dev-mode-gated, localhost-only.
**Acceptance.** An App renders and completes its handshake in the inspector; the
`obs/v1` stream displays; the inspector refuses non-localhost binds.
**Briefs.** 05, 04, 01. **Deps.** 09, 10a, 11, 16.

#### 23 — Inspector advanced + `dockyard inspect` (RFC §12)

**Goal.** Fixture switcher (happy/empty/error/permission/slow/large) wired to
generated contracts; per-tool latency analytics; contract-drift + spec-compliance
verdicts; capability-set emulation; task-lifecycle rendering; standalone
`dockyard inspect --url`.
**Acceptance.** Fixtures drive UI states; capability emulation degrades an App
correctly; `dockyard inspect` attaches to any running server.
**Briefs.** 05, 04. **Deps.** 22, 14, 21.

### Wave 9 — Templates

#### 24 — Template system + `analytical-card` (RFC §10)

**Goal.** The `--template` mechanism; the `analytical-card` template (KPI / chart /
table / explanation) with fixtures, tests, manifest, and all UI states.
**Acceptance.** `dockyard new --template analytical-card` produces a project that
builds, validates, tests, and renders in the inspector.
**Briefs.** 04, 01. **Deps.** 19, 20, 10a.

#### 25 — `approval-flow` template (RFC §10, §8.6)

**Goal.** The `approval-flow` template — a human-in-the-loop App bound to a
task-returning tool (Tasks × Apps).
**Acceptance.** The generated app drives a task to `input_required`, takes an
approve/reject, and resumes; renders correctly in the inspector.
**Briefs.** 02, 01. **Deps.** 24, 14.

#### 26 — `inspector` template (RFC §10)

**Goal.** The `inspector` template — object / log / trace / metadata inspection
panels.
**Acceptance.** `dockyard new --template inspector` produces a building, validating
project that renders in the inspector.
**Briefs.** 05, 01. **Deps.** 24.

### Wave 10 — Hardening & release

#### 27 — Security pass + spec-compliance conformance (RFC §15, §16)

**Goal.** A cross-cutting security review against `AGENTS.md` §7; the Apps + Tasks
spec-compliance conformance suite run against the vendored specs.
**Acceptance.** The conformance suite passes; CSP/sandbox, task-ID entropy, auth
binding, HTTP security options, and inspector localhost-binding all verified.
**Briefs.** 01, 02, 03. **Deps.** 09, 13, 14.

#### 28 — Examples, godoc, docs hygiene (RFC §2)

**Goal.** Worked examples in `examples/`; godoc on every public package; a docs
pass for drift.
**Acceptance.** Examples build and run; godoc complete on exported surface;
`drift-audit` clean.
**Deps.** 01–27.

#### 29 — Agent skills & published tech-docs site (RFC §1, §2)

**Goal.** Author Dockyard's agent-skill set in the Agent Skills `SKILL.md` format
(agentskills.io conventions), covering the core developer workflows — scaffold a
server, add a tool, attach a UI resource, define contracts, run the dev loop,
validate, package — so a developer building with Dockyard via an AI coding agent is
productive from day one. Stand up the GitHub Pages technical-documentation site,
built and deployed by CI. From this phase on, keeping skills and docs in sync with
the surface is mandatory repo hygiene (AGENTS.md §19).
**Acceptance.** `skills/` ships installable Agent Skills that validate against the
`SKILL.md` format; the GitHub Pages site builds and publishes the tech docs from
CI; the AGENTS.md §19 hygiene rule is in force and reflected in the §14 checklist.
**Briefs.** 04 (DX). **Deps.** 21, 26.

#### 30 — V1 release engineering + cut (RFC §1, §14)

**Goal.** Versioning, changelog, the cross-compile release matrix + checksums, and
the V1 tag.
**Acceptance.** A reproducible release build for every target triple; checksums;
the V1 cut is tagged.
**Deps.** 27, 28, 29.
