# Dockyard — V2 Backlog

> The consolidated post-V1 backlog. Every item below is a deferral or
> follow-up recorded during the V1 build that did not earn a place in the
> V1 critical path but is worth picking up post-v1.0.0. Each entry names
> its originating decision number(s), the deferral rationale, and the
> "definition of done" criteria a future phase or PR would need to meet
> to claim it.
>
> This document is the single, navigable home for what comes next. A
> future phase plan that wants to ship one of these items cites the
> backlog line in its `Files added or changed` section.

The phase index (`docs/plans/README.md`) closes at Phase 30. Items in
this backlog do **not** carry new phase numbers; if one ships post-V1,
its phase number is assigned at planning time.

## How items are organised

Items are grouped by theme. Inside a theme they are listed roughly by
recorded order in the decisions log. Each item carries:

- **Title** — a short, descriptive headline.
- **Origin** — the originating decision (and any related decisions).
- **What was deferred + why** — the deferral rationale, in plain prose.
- **Definition of done** — the criteria a future PR or phase would need
  to meet to claim the item.

## Themes

- [Framework surface](#framework-surface)
- [CLI / DX](#cli--dx)
- [Templates](#templates)
- [Documentation & release engineering](#documentation--release-engineering)
- [Forward-compatibility & spec evolution](#forward-compatibility--spec-evolution)
- [Persistence](#persistence)
- [Ecosystem](#ecosystem)

---

## Framework surface

### Enterprise auth / OAuth-shaped flows

- **Origin.** RFC §19 Non-goal N4; the master-plan post-V1 follow-ups
  paragraph; this is also the work D-088 (the `dockyard install` boot
  check) explicitly does **not** cover — D-088 carves out a
  test-only-client boot check that is **not** enterprise auth.
- **What was deferred + why.** The MCP enterprise-auth extensions
  (enterprise-managed authorization, OAuth client-credentials) are V2.
  V1 ships the manifest hooks an HTTP-transport server needs to declare
  a requestor-identity source (`Options.TasksAuthContext`), but the
  authorization-flow plumbing — token negotiation, identity-provider
  trust, key rotation — is a substantial cross-cutting workstream that
  needs its own design (per-host profiles, registration shapes, refresh
  semantics) and is not on the V1 critical path.
- **Definition of done.** A new RFC section (or RFC bump) covers the
  enterprise-auth model end to end; `runtime/server` exposes a typed
  auth-context shape every transport honours; the conformance suite
  asserts the auth-context binding around `tasks/list` /
  `tasks/{get,result,cancel}` (RFC §8.5) continues to hold under the
  new shape; a worked example shows a real OAuth-credentials flow end
  to end against a public test IdP.

### `dockyard dev`'s inspector auto-attach seam

- **Status: Closed in v1.1 Wave A** — D-161 (the auto-attach seam +
  `--no-inspector` opt-out), D-162 (the in-process-vs-subprocess
  choice). The plan is
  `docs/plans/v1.1-wave-A-inspector-polish.md`.
- **Origin.** D-085 (Phase 19 deferred inspector attachment to the
  phase that builds the inspector); D-101 (Phase 23 left the
  auto-attach as a clean follow-up seam). RFC §12 names both
  `dockyard inspect` (standalone) and automatic inspector operation
  inside `dockyard dev` as entry points.
- **What was deferred + why.** A developer using `dockyard dev` today
  reaches the full inspector by running `dockyard inspect --url` in a
  second terminal against the dev-loop's HTTP server. Embedding the
  inspector HTTP backend into the `internal/devloop` supervisor needs
  its own lifecycle and teardown story (the inspector is a long-lived
  HTTP listener; the dev-loop supervisor's restart-on-change semantics
  must not racily kill the inspector with the Go server). D-101 made
  the supervisor seam in a shape that admits this addition cleanly;
  the work was off the V1 critical path.
- **Definition of done.** `dockyard dev` spawns the inspector backend
  as a fourth supervised child alongside the Go server, codegen
  watcher, and Vite; the inspector survives Go-server restarts (it
  re-attaches to the new HTTP listener on the same port); a teardown
  signal (Ctrl-C, context cancel) tears the inspector down cleanly
  alongside the rest of the tree. The auto-attach is opt-in (flag or
  manifest setting) so a developer who prefers the standalone path
  keeps it.

### Scaffold + `dockyard run` auto-wire of `tasks.Engine`

> **Status:** **Claimed by v1.1 wave B (D-164).** The scaffold detects
> task-supporting tools at generation time and emits a
> `tasks.NewInMemoryStore()` + `tasks.NewEngine(...)` block plus
> `server.Options{Tasks: engine}` attachment in `main.go`; `dockyard
> run` warns when the manifest demands an engine but `main.go` does
> not appear to wire one. See
> `docs/plans/v1.1-wave-B-runtime-cleanups.md` for the implementation
> details. The entry stays here as the audit trail of what was
> deferred and when it shipped.

- **Origin.** D-108 (R2 follow-up explicitly named); D-135 (the
  `approval-flows` template's `main.go` does this directly).
- **What was deferred + why.** R2 closed the wiring gap by giving
  `runtime/server` an `Options.Tasks` engine attachment seam — the
  app author wires their own `tasks.Engine` in `main.go` and the
  server hosts `tasks/*` over its real transports. The
  `approval-flows` template's scaffold does this in a hand-written
  `main.go` block. Generalising the auto-wire — so any template (or
  no-template scaffold) whose tools declare `task_support: optional`
  or `required` gets a working `tasks.Engine` + `Store` constructed
  in the generated `main.go` without hand edits — needs per-tool
  task-support detection from the manifest, engine + Store
  construction in the entrypoint, and a transport-specific
  identifiability decision. R2's seam fix did not include that work;
  it is the next layer.
- **Definition of done.** The scaffold detects task-supporting tools
  in the manifest at scaffold time; emits the `tasks.NewEngine` + the
  `Store` driver of the project's choice + the `server.Options.Tasks`
  attachment in the generated `main.go`; `dockyard run` similarly
  attaches the engine when running a project whose manifest declares
  task support; the `analytics-widgets` template (which currently
  forbids task support) is unaffected; a `dockyard new` of a fresh
  no-template scaffold whose example tool is changed to declare
  `task_support: optional` gets a working `tasks/*` surface without a
  hand edit.

### Inspector Prompts panel

- **Status: Closed in v1.1 Wave A** — D-163 (the panel + the
  operator-initiated `GET /api/prompts` + `POST /api/prompts/get`
  surfaces). The plan is
  `docs/plans/v1.1-wave-A-inspector-polish.md`.
- **Origin.** D-151 (the Phase 28 `runtime/server.AddPrompt` surface);
  the `prompts-demo` example's README documents the missing
  visible-demo path.
- **What was deferred + why.** Phase 28 added Prompts support to the
  runtime (`AddPrompt`, the `obs/v1` `prompt.get` carrier event, the
  `prompts-demo` example). The inspector's panels in Phase 23 covered
  tools / resources / Tasks; an inspector Prompts panel was outside
  scope and would have widened Phase 28's surface. A developer running
  the `prompts-demo` example today verifies it against a
  Prompts-aware host (Claude Code, an MCP CLI); the inspector does
  not yet render the primitive.
- **Definition of done.** The inspector grows a Prompts DetailRail
  panel that lists every prompt the attached server registers,
  exercises a `prompts/get` call against it (operator-initiated only
  — same P4 framing as D-131), and renders the resulting messages.
  Six fixture states wired through the prompt's flat-string argument
  shape (consistent with D-152) drive the panel.

### Analytics-widgets / Claude signed-origin follow-up

> **Status:** **Claimed by v1.1 wave B (D-165), Path B.** The
> `HostProfile` interface gained a `RequiresServerURL() bool` method;
> the capability category in `internal/testgate/categories.go` now
> consults it (the `syntheticServerURL` constant retired). See
> `docs/plans/v1.1-wave-B-runtime-cleanups.md` for the implementation
> details. The entry stays here as the audit trail of what was
> deferred and when it shipped.

- **Origin.** Phase 29 live-skill validation surfaced the gap;
  `internal/testgate/categories.go`'s `runCapability` carries the
  in-tree workaround (a synthetic placeholder server URL satisfies the
  signing host profile's derivation invariant — see
  `syntheticServerURL` around line 269).
- **What was deferred + why.** A signing host profile (e.g. Claude —
  D-063, D-064) refuses to derive a stable signed origin when an App
  declares a non-empty domain label but the runtime is given an empty
  `serverURL`. The capability category proves the *seam* resolves for
  every host, so a synthetic placeholder URL is correct for the
  capability test (the seam is what is under test). The underlying
  improvement — letting an `analytics-widgets`-shaped App declare its
  `_meta.ui.domain` in the manifest so the signed-origin derivation
  has a stable input even when no server URL is yet known, or a
  different host-profile API that does not need a `serverURL` for the
  capability case — was not on the V1 critical path.
- **Definition of done.** A manifest field (or a host-profile API
  change) lets a UI-bearing App declare its `_meta.ui.domain` in a way
  the signing host profiles consume without a synthetic placeholder;
  the `internal/testgate` `syntheticServerURL` workaround is removed;
  the `analytics-widgets` template's manifest exercises the new shape;
  the conformance suite verifies signed-origin derivation continues to
  hold for every shipped host profile.

### Apps `media-src` / `data:` / `blob:` declaration

- **Origin.** Downstream feedback (first external MCP-Apps builder, an
  image/video App). Surfaced that the `_meta.ui.csp` model
  (`internal/protocolcodec/apps.go`) declares **domain allowlists**
  (`connect`/`resource`/`frame`/`base-uri`) but offers no way to declare
  intent for inline media — `data:` thumbnails, `blob:` video.
- **What was deferred + why.** Dockyard's manifest models the CSP as
  domain allowlists; the **literal CSP string** (including whether `data:`
  / `blob:` are permitted for `img-src` / `media-src`) is **host-built**,
  per the MCP Apps spec, and Dockyard does not model it. For a single-file
  bundle that inlines assets as `data:` URIs — or a media App that streams
  a `blob:` — there is no manifest knob to declare that the App needs
  `data:`/`blob:` media, so a builder must "design to degrade" and cannot
  state the requirement. A first-class declaration is a genuine model
  addition that needs its own design (a new `csp` sub-shape vs a
  host-profile derivation; how a host that refuses `data:`/`blob:` is
  surfaced) and was not on any V1 critical path.
- **Definition of done.** The manifest's `csp` block (or an explicit
  `media` declaration) lets a UI-bearing App declare `data:` / `blob:` /
  per-origin `media-src` / `img-src` intent; `internal/protocolcodec`
  carries the shape behind the seam (P3); the `attach-a-ui-resource` skill
  documents it (dropping the "design to degrade" caveat); a worked
  media/image example exercises it end to end through `dockyard inspect`;
  the conformance suite asserts the host-built policy honours the
  declaration.

### Bridge View-side task-progress channel

- **Origin.** Upstream-team feedback (2026-05-29). Related: D-119 / the
  Tasks engine (`runtime/tasks.TaskHandle.Progress`).
- **What was deferred + why.** A task handler can report progress
  server-side via `TaskHandle.Progress`, but the Svelte bridge
  (`web/bridge`) exposes **no View-side progress channel** — so an App's
  card cannot render a live "62%". Progress only surfaces in the host's own
  task UI, not inside the App's iframe. Closing it is a new bridge feature:
  a `ui/` progress notification on the View↔host protocol plus a View-side
  subscribe helper (mirroring `onToolResult` / `onHostContextChanged`), with
  its own design (the notification shape, how it rides the `obs/v1` /
  task-progress stream into the inspector host-bridge, and the
  capability-negotiation story for hosts that do not forward it). It was not
  on the v1.3 wave-A critical path.
- **Definition of done.** `web/bridge` ships a typed View-side progress
  subscription (e.g. `onTaskProgress`) fed by a `ui/` progress
  notification; the inspector host-bridge forwards the runtime's task
  progress to the View; an `approval-flows`-shaped App renders a live
  progress value end to end through `dockyard inspect`; the bridge unit
  suite covers the new channel; degradation is clean when a host does not
  forward progress.

### Surface a task's `input_required` schema to the requestor

- **Origin.** v1.5 wave A wiring audit (2026-05-29). Related: D-134 (the
  elicitation-response channel), D-175.
- **What was deferred + why.** `runtime/tasks.InputPrompt.Schema` lets a
  handler attach a JSON Schema to an `input_required` elicitation so the
  requestor can render a typed form. The engine records it and exposes it via
  `Engine.PendingInput`, but **no V1 wire/transport surface pushes it to the
  requestor** — only the prompt `Message` reaches a poller (as the task
  `StatusMessage`). Building the surface (a `tasks/get`-borne elicitation
  requirement, or an inspector `GET pending-input` endpoint the App-frame
  renders) is a genuine protocol/host-surface addition, not a one-line wire,
  so v1.5 corrected the field's doc to stop over-promising rather than build
  it. The `Declined` reply path is fully wired; only the prompt **schema**
  delivery is missing.
- **Definition of done.** A host surface carries `InputPrompt.Schema` to the
  requestor (`internal/protocolcodec` behind the seam, P3); the inspector
  renders a schema-driven elicitation form through `dockyard inspect`; an
  `approval-flows`-shaped App round-trips a typed `input_required` reply; the
  `InputPrompt.Schema` doc drops the "no V1 surface" caveat.

### Populate `obs/v1` `ToolCallPayload.ContractOK`

- **Origin.** v1.5 wave A wiring audit (2026-05-29). Related: P1 (contract-
  first), the `obs/v1` contract.
- **What was deferred + why.** `ToolCallPayload.ContractOK *bool` is documented
  to report whether a tool's input/output validated against the generated
  contract schema (P1); `nil` means "not checked". The contract-first handler
  runtime *does* validate input at the catalog edge, but the validation
  outcome is never threaded into the `obs/v1` `tool.call` event — so
  `ContractOK` is always `nil` ("not checked") and the OTel
  `dockyard.obs.contract_ok` attribute is never emitted. This is within the
  documented contract (`nil` is valid) — an unfulfilled opportunity, not a
  broken promise — and wiring it cleanly needs a `Recorder.ToolCall` signature
  change rippling through `runtime/server` and `runtime/tool`, larger than a
  friction fix. Deferred rather than churned into the v1.5 wiring PR.
- **Definition of done.** The handler runtime surfaces its input/output
  contract-validation result to the `tool.call` end event; `Recorder.ToolCall`
  carries it; `ContractOK` is non-nil on a real `tool.call` event and the OTel
  adapter emits the attribute; a test asserts a contract-valid and a
  contract-invalid call set it true/false.

---

## CLI / DX

### Pre-publish `--dockyard-path` workflow

> **Status:** **Claimed by v1.2 wave A (D-166).** `dockyard new` now runs
> `go mod tidy` + `dockyard generate` for the developer at scaffold time
> (best-effort, with a `--no-postgen` opt-out), so a fresh scaffold —
> blank or `--template` — reaches a green `dockyard validate` on the
> first try with no manual command. The two manual steps were dropped
> from the `scaffold-a-server` skill + getting-started docs in the same
> PR (§19). See `docs/plans/v1.2-wave-A-scaffold-and-changelog.md`. The
> entry stays here as the audit trail of what was deferred and when it
> shipped.

- **Origin.** D-139 (the documented one-time-`go-mod-tidy` +
  `dockyard generate` workflow before v1.0.0 was on a registry).
- **What was deferred + why.** Before v1.0.0 the scaffold's generated
  `go.mod` carries a `replace` directive pointing at the local
  Dockyard checkout (D-080), with no `go.sum`. A user needed to run
  `go mod tidy` once after scaffolding and `dockyard generate` once
  for a template scaffold (the blank scaffold ships generated
  artifacts pre-built). v1.0.0 makes `go install
  github.com/hurtener/dockyard/cmd/dockyard@v1.0.0` the recommended
  install; the `--dockyard-path` workflow stays as the "build from
  source" alternative.
- **Definition of done.** A future scaffold-pipeline improvement
  auto-runs `go mod tidy` and `dockyard generate` at scaffold time,
  so a developer following either the recommended or build-from-source
  path lands at a green `dockyard validate` on the first try without
  extra commands. The `skills/scaffold-a-server/SKILL.md` and the
  docs-site getting-started pages drop the two extra steps in the
  same PR (§19 hygiene).

### `dockyard publish`

- **Origin.** RFC §19 (V2 fast-follow); RFC §2 N5 (no hosted /
  cloud); master plan post-V1 follow-ups paragraph.
- **What was deferred + why.** V1 deliberately stops at portable
  artifacts. A `dockyard publish` verb would either need a hosted
  service (out of scope — RFC G9, RFC §2 N5) or a minimal open
  registry that lets servers express "built with Dockyard". The
  trade-offs (registry shape, hosting, governance, naming) were not
  worth resolving for V1.
- **Definition of done.** An RFC bump defines the open-registry
  shape and the publish protocol; a new CLI verb `dockyard publish`
  pushes a release artifact to the registry; a registry-side reader
  is documented; the §19 hygiene rule covers the new verb (skill +
  docs page in the same PR).

### Inspector BYOK chat tab

- **Origin.** RFC §19.
- **What was deferred + why.** A model-driven tool-selection view in
  the inspector ("ask an LLM to pick a tool") needs an LLM-key path
  that V1 deliberately does not embed (P4 — Dockyard ships no
  production MCP / LLM client; the inspector is the lone
  client-shaped component and it does not call models). Adding a
  BYOK chat surface is a meaningful design choice (where keys live,
  how they are scoped, what the security story is) that earns its
  own RFC section.
- **Definition of done.** An RFC bump (or new RFC section) defines
  the BYOK shape; the inspector grows a chat tab that drives
  operator-initiated tool selection via a configured LLM; the P4
  framing remains intact (the inspector is still test-only,
  dev-mode-gated, localhost-bound).

### Publish `@dockyard/bridge` + `@dockyard/ui` to npm

> **Status:** **Claimed by v1.3 wave B (D-172).** Both packages set
> `publishConfig.access: "public"`, track the repo version (off `0.1.0`),
> and publish from a gated tag-push `npm-publish` job in
> `.github/workflows/release.yml` (`NPM_TOKEN`, `--access public`,
> idempotent — verified by `npm pack` + a scaffold-install build before the
> publish). The scaffold's `__DOCKYARD_*_SPEC__` tokens now resolve to the
> published versions (a caret `^X.Y.Z`) when `--dockyard-path` is omitted, so
> a `--template` scaffold's `web/` `npm install` succeeds with no checkout;
> the `scaffold-a-server` / `attach-a-ui-resource` skills dropped the
> `--dockyard-path`-for-UI caveat in the same PR. See
> `docs/plans/v1.3-wave-B-npm-and-bridge-progress.md`. The entry stays here
> as the audit trail of what was deferred and when it shipped.

- **Origin.** Downstream feedback (first external MCP-Apps builder).
  Called out as the gap "most likely to bite a new MCP-Apps builder."
  Related: D-080 (the pre-publish `replace` workflow).
- **What was deferred + why.** The Dockyard **Go module is published**
  (`go install …@vX.Y.Z` works with no checkout), but the **frontend
  packages `@dockyard/bridge` and `@dockyard/ui` are workspace packages**
  (`main: ./src/index.ts`), not on npm. So a UI project's `web/` can only
  resolve them from a **local Dockyard checkout** via `--dockyard-path`; a
  template scaffold without it (and without the checkout) fails at
  `npm install` with no obvious cause. The Go/npm asymmetry is invisible —
  the Go half "just works" from the proxy while the frontend half silently
  requires the checkout. Publishing the two packages (build/versioning/
  release-pipeline work for the npm side, mirroring the Go module's
  release) was not on a V1 critical path. **Interim mitigation shipped:**
  the `scaffold-a-server` and `attach-a-ui-resource` skills now call the
  requirement out loudly (the `--dockyard-path`-for-UI note).
- **Near-term candidate (maintainer has an npm token).** As of 2026-05-29
  the maintainer has an npm publish token and floated wiring an npm-publish
  job into the release CI on the next tagged version. Grounding from the
  current package shapes: the two packages have **no interdependency**
  (simplifies); the templates' `web/package.json` already reference them
  through a substitution token (`__DOCKYARD_BRIDGE_SPEC__` /
  `__DOCKYARD_UI_SPEC__`), so flipping the consuming side to a published
  version is contained. The **publish mechanism** is a genuine quick win
  (a tag-triggered `npm publish --access public` job + `publishConfig`),
  but two things must be right first: (1) the packages currently publish
  **raw `./src/index.ts` / `.svelte` source** with no build and no `svelte`
  export condition — a downstream Svelte/Vite consumer needs either a
  proper build (`@sveltejs/package` for ui, `tsup` for bridge) or the
  correct `exports` conditions; (2) the **version policy** (both are
  `0.1.0`; the Go module is `1.2.0`) — pin them to the repo version and
  bump in the release-prep step. First publish is semi-irreversible, so
  verify with `npm pack` + an install-into-a-fresh-scaffold smoke before
  enabling auto-publish.
- **Definition of done.** `@dockyard/bridge` and `@dockyard/ui` are
  published to npm under a versioning + release flow that ages with the Go
  module's tags; their `exports` (or a build) make them consumable by a
  downstream Svelte/Vite build; the release workflow publishes them on a
  tag push using the `NPM_TOKEN` secret, gated behind an `npm pack` /
  install verification; the scaffold's `web/package.json` token resolves to
  the published versions (no `file:` workspace path) when `--dockyard-path`
  is omitted; a `--template` scaffold's `web/` `npm install` succeeds with
  **no** `--dockyard-path` and no local checkout; the skills drop the
  "`--dockyard-path` required for UI builds" caveat in the same PR (§19);
  `--dockyard-path` reverts to a pure build-from-source convenience.

---

## Templates

### The `inspector` (or successor) template slot

- **Origin.** D-136 (the V1 deferral of Phase 26 — the original
  three-template plan was set when the product was hand-wavy; after
  Phases 24 + 25 shipped, the third template slot was judged not
  worth its maintenance cost for V1).
- **What was deferred + why.** The two V1 templates
  (`analytics-widgets`, `approval-flows`) cover the dominant MCP App
  patterns end-to-end (read-side widgets, write-side
  human-in-the-loop with Tasks). A third "drill-down / detail-view"
  template would mostly re-use Phase 24's capabilities without
  exercising a new framework surface, and the name "inspector
  template" was structurally confusing against the framework's
  debugging `dockyard inspect` tool.
- **Definition of done.** D-136's three criteria: (a) the template
  exercises a framework capability the two shipped templates do not
  already prove (e.g. MCP prompts, dynamic resource templates beyond
  `ui://`, a no-UI backend-only minimal server pattern,
  auth-context binding from the enterprise-auth path); (b) it ships
  a real Playwright-proven demo end to end through `dockyard inspect`
  against the scaffolded project; (c) it comes with the same six
  fixture states wired to its generated contracts.

### The remaining post-V1 templates

- **Origin.** RFC §10 (the master template list); RFC §19; master
  plan post-V1 follow-ups paragraph.
- **What was deferred + why.** RFC §10 names a fuller post-V1
  template set: `document-review`, `task-runner`, `artifact-viewer`,
  `form-tool`, `agent-console`. V1 ships two — the smallest set
  that proves the framework's read- and write-side patterns end to
  end. Adding more for V1 would defer V1.
- **Definition of done.** Each post-V1 template ships under the
  same bar Phases 24 + 25 established: a contract-first tool set,
  a Svelte App composing the shared `web/ui/` inventory, a real
  Playwright-proven demo end to end through `dockyard inspect`,
  six fixture states, and a docs-site walkthrough that the §19
  hook would mechanically require.

---

## Documentation & release engineering

### Signed releases + SLSA provenance

- **Origin.** Phase 30 plan (`docs/plans/phase-30-v1-cut.md` —
  Non-goals); recorded here for V2 follow-up.
- **What was deferred + why.** V1 ships SHA-256 checksums (the same
  `internal/buildpkg` convention `dockyard build` emits) and the
  source URL pin via the tag — enough for `go install` to verify
  against `go.sum` / `sumdb`. Cosign signing of the release
  artifacts and a SLSA-provenance attestation pipeline are real
  hardening surfaces but each is a meaningful workstream of its own
  (key storage, key rotation, the SLSA generator action, the
  release-verification path a downloader runs). Folding them into
  the V1 release cut would have widened Phase 30 beyond a release
  engineering pass into a release-hardening pass.
- **Definition of done.** The `release` workflow signs each
  artifact with a Cosign keyless flow (GitHub OIDC); the workflow
  emits a SLSA v1.0 attestation for every artifact; a documented
  verification command (`cosign verify-blob …`) is in
  `docs/RELEASING.md`'s post-release checklist; the
  `docs/RELEASING.md` rollback procedure covers the
  "signed-release-needs-revocation" case.

### Versioned docs (multi-version side-by-side)

- **Origin.** Phase 29 plan (deferred to Phase 30 if needed);
  Phase 30 plan (V1's versioning story is "client-side search + a
  v1.0.0 release callout on the home page").
- **What was deferred + why.** VitePress's built-in client-side
  search and the home-page release callout are V1's versioning
  story. A multi-version doc tree (1.0 / 1.1 / 2.0 side by side, a
  version switcher in the nav) is genuinely useful only once there
  are multiple stable releases to switch between; building it before
  there is anything to switch between is busywork.
- **Definition of done.** The docs site grows a version switcher
  (driven by branch / tag naming convention or VitePress
  multi-version layout); CI deploys a `latest` alias to the
  current release tag; a `next` alias points at `main`; release
  artifacts (`docs/RELEASING.md`'s checklist) include "promote the
  docs alias" as a step.

### Conventional-Commits-generated changelog supplement

> **Status:** **Claimed by v1.2 wave A (D-167).** The release pipeline
> now appends a Conventional-Commits-derived list (rendered by a pure,
> golden-tested `internal/changelogx.Supplement`) below the hand-authored
> CHANGELOG section in the GitHub Release body, on a tag push only. The
> hand-authored prose stays the canonical narrative (D-154). See
> `docs/plans/v1.2-wave-A-scaffold-and-changelog.md`. The entry stays
> here as the audit trail of what was deferred and when it shipped.

- **Origin.** Phase 30 plan (the v1.0.0 changelog is deliberately
  hand-authored; from v1.1.0 onward the format is open to
  augmentation).
- **What was deferred + why.** The v1.0.0 entry tells the V1 story
  in human-authored prose framed by P1–P4. An auto-generated
  Conventional-Commits-derived list of PRs alongside the
  hand-authored body is plausibly useful for a v1.1.x release
  ("what landed in detail"); it is not worth designing before
  there is anything to summarise.
- **Definition of done.** A future release-engineering pass adds a
  step to `.github/workflows/release.yml` that runs a
  Conventional-Commits parser between the previous tag and the
  current one, appends the resulting list under a `### Changed`
  / `### Fixed` block in the release body, and leaves the
  hand-authored prose as the canonical narrative.

---

## Forward-compatibility & spec evolution

### A schema → Go generator for the Tasks wire layer

- **Origin.** D-024.
- **What was deferred + why.** RFC §16 specifies a regenerate-and-
  diff discipline for the MCP Tasks wire layer against the
  vendored experimental schema. For V1 the Go wire types are
  hand-written against the snapshot and guarded by golden tests:
  a spec revision is a visible diff, just produced manually. A
  real schema → Go generator was deferred because the small
  `_meta`-borne / capability subset V1 needs does not warrant the
  generator's design + maintenance cost. The forward-compatibility
  property holds either way (one isolation seam, a pinned snapshot,
  a diffable update).
- **Definition of done.** A generator lands under `internal/` (or a
  separate sub-module) that derives the Tasks wire Go types from
  the vendored experimental schema; the generator runs in CI when
  the vendored schema changes; the generated types replace the
  hand-written ones behind the unchanged `protocolcodec` interface;
  golden tests pin the generator's output.

### `encoding/json/v2` adoption

- **Origin.** RFC §17 (stack decisions table); RFC §19.
- **What was deferred + why.** `encoding/json/v2` is still
  experimental in Go 1.26 (the toolchain V1 pins); stricter
  contract validation it would enable is a real DX win but not
  worth risking on an experimental package. V1 stays on
  `encoding/json` v1.
- **Definition of done.** Go 1.27 or 1.28 stabilises
  `encoding/json/v2`; Dockyard's `internal/codegen` and the
  generated server-side `tools/call` argument decode path switch
  to v2; the validate gate gains the stricter assertions v2
  enables (unknown-field rejection, strict number parsing); the
  conformance suite continues to pass.

### A native SDK Tasks API

- **Origin.** RFC §19.
- **What was deferred + why.** Dockyard's Tasks layer is built on
  the experimental `tasks/*` shim
  (`runtime/tasks.Engine` + `Mount` — the work D-071, D-108,
  D-109, D-110 settled). The Go MCP SDK does not yet ship a
  Tasks API. When it does, Dockyard's Tasks shim is swapped for
  it behind the unchanged `protocolcodec` interface.
- **Definition of done.** The Go MCP SDK ships a Tasks API;
  Dockyard's `runtime/tasks` is reshaped to call the SDK's API
  for the wire layer while keeping its own `TaskStore`, `TaskHandle`,
  and security policies (the value Dockyard adds above the SDK
  shape); the conformance suite continues to pass; the swap is
  invisible to a Dockyard app.

---

## Persistence

### A Postgres `Store` driver

- **Origin.** D-007 ("a future Postgres driver"); D-025 (the
  `Store` seam shape); RFC §19.
- **What was deferred + why.** V1 ships `modernc.org/sqlite` (the
  pure-Go SQLite that keeps the CGo-free guarantee) and an
  in-memory driver. A Postgres driver is the natural next step for
  distributed / at-scale HTTP deployments; it was not on the V1
  critical path because no V1 user requires it yet, and the
  driver's design needs decisions (connection pool shape,
  migration ordering across instances, the JSON column shape for
  task payloads) that earn their own design pass.
- **Definition of done.** A `runtime/store/postgres` driver lands
  under the same interface + factory + driver pattern (CLAUDE.md
  §4.4); it passes the shared conformance suite end to end; the
  driver is CGo-free (uses `pgx`'s pure-Go layer); its
  forward-only migrations layer (D-027) integrates with the
  existing `MigrationSet` shape (D-073).

---

## Ecosystem

### ChatGPT Apps SDK as a second host protocol

- **Origin.** RFC §19; RFC §2 N2; master plan post-V1 follow-ups
  paragraph.
- **What was deferred + why.** The V1 Svelte bridge shell library
  (RFC §7.2 / §7.3) was designed so a second host-protocol
  adapter is a clean fast-follow, not a rewrite. The actual
  adapter was deferred: the ChatGPT Apps SDK shape is not yet
  stable enough for a Dockyard binding worth maintaining, and
  building one for V1 would have delayed the MCP-side ship.
- **Definition of done.** A new `web/bridge-chatgpt/` (or the
  equivalent) implements the ChatGPT Apps SDK's host-side
  dialect against the same View half the MCP `ui/` bridge uses;
  a templates-side proof shows one App rendering correctly
  under both host protocols; the conformance suite asserts
  parity on the App's behaviour across the two protocols.

### The multi-server fleet console

- **Origin.** RFC §11 / §19 / §2 N3; D-002 (the post-V1 console
  is a pure `obs/v1` client of every Dockyard server it watches).
- **What was deferred + why.** V1 ships the `obs/v1` protocol and
  a single-server inspector. The multi-server fleet console is a
  later pure `obs/v1` client that fans in many servers' streams
  into one view. Ownership (Dockyard satellite repo vs folded
  into Portico) is undecided; brief 05 Q-7 records the question.
- **Definition of done.** The ownership question is resolved (in
  an RFC bump or a Portico-side RFC); the console ships as a
  pure `obs/v1` client; a multi-server demo wires three
  Dockyard servers into one console view; the §19 hygiene rule
  covers the new surface.
