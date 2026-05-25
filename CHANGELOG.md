# Changelog

All notable changes to Dockyard are recorded in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The post-v1.0.0 semver policy is documented in
[`docs/RELEASING.md`](docs/RELEASING.md).

A version's section is the canonical release-body source for the
GitHub Release the `release` workflow creates; the
`internal/changelogx` extractor reads from this file directly, so the
heading shape (`## [<version>] - <YYYY-MM-DD>`) is load-bearing.

Pre-v1.0.0 history is not reconstructed here. The repository was a
doc-driven build with thirty named phases (00..30); the phase-by-phase
record lives in `docs/plans/` and the architectural decisions log in
`docs/decisions.md`. The v1.0.0 entry below tells the developer-meets-V1
story instead — what shipped, how it hangs together, what is
deliberately deferred to V2.

## [Unreleased]

Nothing yet.

## [1.0.0] - 2026-05-25

The first stable release of Dockyard — a Go-native framework for
production-grade MCP Servers and MCP Apps. One CGo-free static binary
(the `dockyard` CLI), one app runtime library every generated server
imports, a contract-first codegen pipeline, the MCP Apps + Tasks
extensions implemented server-side, an intrinsic observability
protocol the runtime emits natively, a local inspector, two product-
pattern templates, eight agent skills, and a published technical-
documentation site.

This entry is the developer-meets-V1 story, framed by the four
binding properties the design is built on. The phase-by-phase log
lives in [`docs/plans/`](docs/plans/); the settled architectural
decisions in [`docs/decisions.md`](docs/decisions.md).

### Highlights — the four binding properties

Dockyard's V1 design is held together by four non-negotiable
properties. Every shipped subsystem traces back to one of them; a
change that would weaken any of them is rejected.

- **P1 — Contract-first.** A tool's input and output are typed Go
  structs. JSON Schema, TypeScript types, and fixtures are
  *generated*, never hand-written. `dockyard validate` fails on
  stale or drifted generated output, so server↔UI drift is caught
  by the toolchain before it reaches a user. No hand-written
  contract schema lives anywhere in the repo and no PR that
  introduces one will merge.
- **P2 — Observability is a protocol.** The runtime emits the
  canonical `obs/v1` event stream. The inspector and any future
  multi-server console are pure clients of that contract; no
  component reads runtime internals to observe. OpenTelemetry
  export is an *optional* adapter, off by default — never a
  prerequisite to see what your server is doing locally.
- **P3 — Forward-compatibility by isolation.** Every MCP extension
  wire format lives in exactly one package
  (`internal/protocolcodec`). A spec bump is a vendored-snapshot
  update + a regenerate-and-diff in that one package; handler-
  facing and manifest-facing APIs never see a raw protocol struct.
  A boundary test walks the whole module to enforce it.
- **P4 — Server-side only.** Dockyard builds MCP *servers* and
  Apps. Harbor owns the MCP client. The one client-shaped
  component — the inspector — is a local, dev-mode-gated,
  localhost-bound test surface; it refuses any non-loopback bind
  before its listener opens. There is no production MCP client in
  the shipped artifact.

### The runtime

The Dockyard app runtime is a Go library every generated server
imports. The generated server's `main.go` stays thin; the runtime
carries the weight.

- **`runtime/server`** — the MCP server core, built on the official
  `modelcontextprotocol/go-sdk`. Stdio and streamable-HTTP
  transports; the explicit `HTTPSecurity` posture
  (DNS-rebinding, Origin/Content-Type, cross-origin protections
  set deliberately, never inherited from SDK defaults); the typed
  handler runtime (`Result[Out]`, the `content` / `structuredContent`
  split, edge validation of incoming arguments); the
  `guardHandler` panic recovery that turns a handler panic into a
  typed error result, never a server crash. W3C TraceContext
  extraction on inbound HTTP so a Dockyard handler's span nests
  natively under a calling Harbor agent's `execute_tool` span.
  Prompts support via `AddPrompt` with obs/v1 carrier events.
- **`runtime/apps`** — the MCP Apps extension server-side
  (`io.modelcontextprotocol/ui`, spec revision 2026-01-26).
  `ui://` resource registration with the
  `text/html;profile=mcp-app` MIME type, `_meta.ui` on tools
  (nested form) and on resource-read responses (CSP, domain,
  permissions); the `extensions` capability negotiation; plain-MCP
  graceful degradation when a host does not advertise the
  extension. UI auto-discovery convention lifts a `.svelte` file
  under `web/src/apps/` into a `ui://` resource. Pluggable host
  profiles auto-derive `_meta.ui.domain` — including Claude's
  SHA-256 signed `claudemcpcontent.com` origin — without a
  hardcoded host matrix.
- **`runtime/tasks`** — the MCP Tasks extension server-side
  (`io.modelcontextprotocol/tasks`, experimental). The
  five-status lifecycle (`working`, `input_required`, `completed`,
  `failed`, `cancelled`); `tasks/*` JSON-RPC routing; the
  `CreateTaskResult` substitution for task-augmented `tools/call`;
  the durable `TaskStore` on the `Store` seam; the `TaskHandle`
  handler API (progress, status messages, cooperative cancellation,
  `input_required`-driven elicitation); crypto-strong (≥128-bit)
  task IDs; auth-context binding that rejects cross-context access;
  per-requestor concurrency caps; a TTL purge sweep. The
  `tasks/*` transport mount joins onto `runtime/server` via the
  `server.Options.Tasks` engine attachment.
- **`runtime/obs`** — `obs/v1`, Dockyard's canonical, versioned
  observability event protocol. A non-blocking headless emitter
  (a slow consumer never stalls the runtime); the in-memory ring
  buffer; the out-of-band localhost SSE sink the inspector
  consumes; the optional OTel adapter (MCP semconv:
  `mcp.*` / `gen_ai.*` attributes) for export to an external
  observability stack; the MCP `logging` → `obs/v1` `log`-event
  bridge so a Dockyard server still speaks standard MCP logging
  to any client. Shape + size capture by default — secrets and
  PII never leak into the event stream; full-content capture is
  opt-in and redaction-aware.
- **`runtime/store`** — the persistence seam. V1 driver:
  `modernc.org/sqlite` (pure-Go, CGo-free); an in-memory driver
  for stdio single-user apps. Forward-only migrations applied
  through an explicit `MigrationSet` (no process-global mutable
  registry); two stores migrate concurrently from independent
  sets with no shared state. The shared conformance suite every
  driver must pass.
- **`internal/protocolcodec`** — the one and only importer of MCP
  extension wire formats. Versioned codecs keyed on the negotiated
  `protocolVersion`; encoders emit only current spec shapes;
  decoders tolerate the deprecated flat `_meta["ui/resourceUri"]`
  shape on read but never emit it. The Apps spec
  (2026-01-26) and the Tasks experimental schema are vendored
  into `docs/specifications/`, pinned by upstream commit SHA + date.

### The CLI — nine verbs

The `dockyard` binary ships nine subcommands. Each one closes a
DX gap that hand-rolled MCP server development leaves open.

- **`dockyard new`** — scaffold a new project. The first-class path
  is `dockyard new <name>` with no flag: a blank but working MCP
  server (one manifest, one example contract-first tool, the
  generated artifacts, a runnable `main.go`, a contract test).
  Templates are optional product-pattern showcases:
  `--template analytics-widgets` and `--template approval-flows`.
- **`dockyard generate`** — run the Design A codegen pipeline
  (Go contract structs → JSON Schema + TypeScript types).
  Idempotent: a rerun with no contract change is a byte-identical
  no-op.
- **`dockyard validate`** — the quality gate. Manifest, schemas,
  tool↔UI mappings, MIME, spec compliance, UI states,
  stale-codegen drift. Exits non-zero on any build blocker.
- **`dockyard dev`** — the embedded edit-feedback loop. One
  `fsnotify` watcher choreographing a Go-server restart on a
  `.go` change, an in-process codegen re-run on a contract
  change, and a supervised Vite dev server. One process tree,
  one Ctrl-C teardown. No external `air` / `wgo` dependency.
- **`dockyard build`** — the build pipeline. Regenerate contracts
  → run validate gate (a build blocker fails the build —
  P1 at build time) → `vite build` the project's `web/` UI →
  `go build` one CGo-free static binary per cross-compile target
  with the UI embedded via `//go:embed all:dist` → SHA-256
  checksum sidecar per artifact. The matrix is darwin/linux/windows
  × amd64/arm64.
- **`dockyard run`** — run the built server at a chosen transport
  (`stdio` or `http`). The selection is at run time, not baked into
  the binary; one artifact serves all three deployment modes.
- **`dockyard install`** — write the host's MCP config (Claude
  Desktop, Cursor) and verify a real MCP `initialize` handshake.
  The boot check is a throwaway localhost-bound dev-only spawn —
  the test-only client carve-out, never a long-lived production
  MCP client.
- **`dockyard test`** — the contract + compliance gate. Runs as
  one command: `go test`, the contract-first assertions, the
  fixture/golden snapshots, MCP spec compliance against the
  vendored specs, capability-degradation tests. Exits non-zero on
  a regression in any gating category.
- **`dockyard inspect`** — attach the inspector to a running MCP
  server. Standalone form: `dockyard inspect --url
  http://127.0.0.1:8080`. The inspector is dev-mode-gated,
  localhost-only, and operator-initiated only.

### The inspector

Dockyard's local debug surface — the lone client-shaped component.
It is the test-only client carve-out the P4 boundary leaves room
for: dev-mode-gated, localhost-bound (refuses any non-loopback
bind before the listener opens), and operator-initiated only.

- **Sandboxed App rendering.** Renders an App in a sandboxed iframe
  through the same `ui/` postMessage host bridge a production host
  would use — the dialect is imported verbatim from
  `@dockyard/bridge`, never forked.
- **Live `obs/v1` stream.** A read-only fan-out subscriber to the
  out-of-band SSE sink. The full event stream — `tool.call`,
  `resource.read`, `prompt.get`, `app.load`, `app.bridge`,
  `host.compat`, `log`, `server.lifecycle`, `task.progress` — in
  one place.
- **The JSON-RPC log.** Every framed call + response, bounded; the
  inspector's wire-protocol view.
- **Fixtures, contract-driven.** The six UI states
  (`happy` / `empty` / `error` / `permission` / `slow` / `large`)
  drive the App's six visual conditions. Built from the tool's
  generated output contract (P1: never hand-written); on-disk
  project fixtures override the synthetic ones when present.
- **Capability-set emulation.** Toggle Apps on/off, Tasks on/off,
  display modes per host — render an App as a host that does or
  does not negotiate a capability. The framework's
  capability-driven degradation is exercisable from the UI.
- **Operator-initiated `tools/call`.** Drive a tool by hand from the
  inspector's Tools panel — fill the schema-derived form, press
  Invoke. One client-shaped operation per UI action; gated by the
  same loopback bind as the rest of the inspector.
- **Tasks Timeline.** Walk a task's five-status lifecycle as a
  timeline; the `input_required` round-trip is visible end to end.
- **Verdicts.** Contract-drift, schema-validation, spec-compliance
  results surfaced as `ok` / `warn` / `error` chips — sourced from
  the same `internal/validate.Run` engine `dockyard validate`
  consumes, never reimplemented.

### The shared design system

`web/ui/` is the single Svelte component inventory every Dockyard
frontend surface composes — the inspector, the template App UIs,
the Svelte bridge shell, the docs site, and (post-V1) the
multi-server console. This rule was set up front to avoid the
late, costly remediation Harbor's design-system divergence caused.

- A typed component inventory: `AppShell`, `PageHeader`,
  `FilterBar`, `DataTable`, `Pagination`, `RailCard`, `StatusChip`,
  the four-state `PageState` (`Loading` / `Empty` / `Error` /
  `Permission`), `MetricCard`, `JsonInspector`, `Sparkline`,
  `FieldDiff`, …
- Design tokens (`--dy-*` CSS custom properties) as the single
  source of visual truth — no ad-hoc hex or magic-spacing values
  in a component.
- The mandatory four-state page rule: every async region routes
  through `PageState`; the empty and error states carry real copy
  and a working retry action. An empty table with no copy is a
  defect.
- Spec → mockup → build for UI-bearing phases: a page spec, an
  approved visual mockup, then the build.

### The two V1 templates

Templates are optional product-pattern showcases; the blank
no-template scaffold is the first-class path.

- **`analytics-widgets`** — the read-side example. Three
  contract-first widget tools (`create_chart`, `create_table`,
  `create_metric_card`) rendered inline by one Svelte App that
  composes the shared `web/ui/` design system plus the new
  `Sparkline` and the template-local `ChartFrame` (the Apache
  ECharts wrapper). Manifest declares `display_modes: [inline]`;
  host-theme propagation through `hostContext.styles.variables` is
  automatic with an explicit per-call `theme` override; six
  fixtures per tool drive the inspector's switcher.
- **`approval-flows`** — the write-side example, the Tasks × Apps
  showcase. Two contract-first task-augmented tools
  (`request_approval` — generic approve/reject;
  `propose_with_edits` — structured change with user-editable
  fields) and one inline App that renders the human-in-the-loop
  card / form, drives the `input_required` round-trip from inside
  the iframe, and completes the task with the user's decision.
  Bundles three pieces of supporting framework wiring the
  template is the first real product driver of: the bridge's
  typed `elicitation-response` notification, the scaffold's
  `tasks.Engine` attachment when a template declares
  task-supporting tools, and a new shared `FieldDiff` `web/ui`
  component.

### Developer experience

- **Agent skills.** Eight `SKILL.md` files under
  [`skills/`](skills/), authored in the
  [agentskills.io](https://agentskills.io) format, so an AI coding
  agent picks Dockyard up cold and ships:
  `scaffold-a-server`, `add-a-tool`, `attach-a-ui-resource`,
  `define-contracts`, `run-the-dev-loop`, `validate`, `package`,
  `test-with-the-inspector`. Validated by `internal/skillcheck`
  in CI; a malformed `SKILL.md` fails drift-audit.
- **Published docs site.** A VitePress site under
  [`docs/site/`](docs/site/), deployed to GitHub Pages by
  `.github/workflows/docs.yml`. Home, per-template
  getting-started walkthrough, per-surface guides
  (contracts, UI resources, dev loop, validate, packaging,
  inspector), an auto-derived CLI reference (regenerated from
  the cobra command tree by `internal/clidocs`), the agent-skills
  index, and reference pages that transclude the RFC, master
  plan, decisions log, glossary, and design conventions via
  VitePress's `<!--@include: …-->` directive. Dead internal
  links fail the build — the §19 fail-fast.
- **Three worked examples.** Real, in-tree, runnable projects under
  [`examples/`](examples/) — `backend-tools-only`,
  `combined-patterns`, `prompts-demo` — first-class members of the
  in-repo test and coverage matrix.
- **godoc examples.** `runtime/server` and `runtime/tool` carry
  `Example*` functions visible on pkg.go.dev.
- **§19 hygiene rule, mechanically enforced.** A PR that changes
  user-facing surface (a CLI verb, a manifest field, a template,
  the generated-project shape, a public runtime API) updates the
  affected skill(s) and docs page(s) **in the same PR**. The
  `scripts/drift-audit.sh` §19 hook walks the CLI's command tree,
  the templates' `builtin.go` markers, and the examples' `cmd/
  server` markers, and fails the build on a missing skill,
  walkthrough, or examples-index reference.

### Quality bar

The minimum quality bar is enforced by the toolchain, not by a
review checklist.

- **The MCP spec-compliance conformance suite.** Round-trips every
  Apps + Tasks wire shape through `internal/protocolcodec` against
  fixtures derived from the vendored spec snapshots — the
  framework-side proof that Dockyard's wire shapes conform to the
  spec. Cited from the fixture-side headers so a future spec bump
  produces a visible diff.
- **The mechanical coverage gate.** `internal/coveragecheck` parses
  the per-package coverage profile, compares each package to its
  AGENTS.md §11 coverage band (80% new packages; 85% for Store
  drivers and conformance-tested subsystems; 70% CLI / tooling),
  and fails the build on any shortfall. `make coverage` runs in
  CI; a coverage regression is a build failure, not a reviewer's
  catch.
- **Fuzz targets.** `protocolcodec`, `manifest`, `codegen`, the
  JSON-RPC tool-argument frame path, the inspector mux, and the
  Tasks JSON-RPC frame parser all carry Go native `FuzzXxx`
  targets. The seed corpus runs as an ordinary CI test; a longer
  `-fuzz` session runs on demand.
- **Benchmarks.** The obs ring buffer, the `protocolcodec` codecs,
  and the `Store` drivers carry Go `BenchmarkXxx` functions. Run
  on demand via `make bench`; not a CI gate.
- **The pre-commit hook + the preflight gate.** `make preflight` is
  the same gate the pre-commit hook and CI enforce: build, run
  every per-phase smoke script, run drift-audit. Bypassing the
  hook with `--no-verify` is forbidden outside a documented
  emergency.

### Packaging

- **One CGo-free static binary per target.** `dockyard build`
  produces a CGo-free, statically-linked binary with the Svelte
  UI embedded; the same `embed.FS` backs both the `ui://` MCP
  resource handler and the inspector's HTTP preview — there is
  never a second copy of the UI assets.
- **The cross-compile matrix.** `internal/buildpkg` drives the RFC
  §14 cross-compile matrix: darwin / linux / windows × amd64 /
  arm64. Each artifact gets a `.sha256` sidecar (`sha256sum -c`
  compatible).
- **Three deployment modes per artifact.** stdio (local subprocess),
  streamable-HTTP (a service), and Portico-managed — selected at
  run time, not baked in.
- **The release pipeline.** `.github/workflows/release.yml`
  triggers on a `v*` tag push (and `workflow_dispatch` for
  dry-runs): runs `make preflight`, drives the cross-compile
  matrix via `internal/releasebuild`, writes a `checksums.txt`
  aggregate, creates or updates a GitHub Release with the
  artifacts attached and the CHANGELOG section as the body. The
  procedure for cutting a release is documented in
  [`docs/RELEASING.md`](docs/RELEASING.md).

### Security

- Sandboxed App rendering under a deny-by-default CSP; single-file
  bundles are the default. Domains and iframe permissions are
  opt-in via the manifest; a host may further restrict but never
  loosen.
- Crypto-strong (≥128-bit) task IDs; auth-context binding rejects
  cross-context access; `tasks/list` is withheld when requestors
  are not identifiable; enforced max TTL and per-requestor
  concurrency caps.
- HTTP transport: DNS-rebinding, Origin/Content-Type, and
  cross-origin protections set deliberately by Dockyard — never
  inherited from SDK defaults (defaults have flipped between SDK
  releases).
- The inspector is dev-mode-gated, localhost-only,
  operator-initiated only; never a production client and never an
  arbitrary-execution proxy.
- `obs/v1` tool input/output capture defaults to shape+size;
  full-content capture is opt-in and redaction-aware.
- No hardcoded secrets, including in generated code and tests.

### Deferred to V2

The post-V1 backlog is consolidated in
[`docs/V2-BACKLOG.md`](docs/V2-BACKLOG.md). Each item names its
originating decision, the deferral rationale, and the criteria a
future phase or PR would need to meet to claim it.

- **D-088** — enterprise auth (enterprise-managed authorization,
  OAuth client-credentials).
- **D-101** — `dockyard dev`'s inspector auto-attach seam.
- **D-108** — the scaffold's `tasks.Engine` auto-wire for any
  template (or no-template scaffold) that needs the Tasks
  extension. The V1 `approval-flows` template did this directly;
  generalising it lives in V2.
- **D-136** — the third (`inspector`) template slot.
- **D-139** — pre-publish `--dockyard-path` workflow. v1.0.0 makes
  `go install …@v1.0.0` the recommended path; `--dockyard-path`
  stays as the "build from source" alternative.
- The **analytics-widgets / Claude signed-origin** follow-up
  (Phase 29 worked around it via the synthetic-URL fix in
  `internal/testgate`; the underlying improvement — manifest
  `_meta.ui.domain`, or a different host-profile API — is open).
- The **ChatGPT Apps SDK** as a second host protocol.
- The **multi-server fleet console** (a post-V1 pure `obs/v1`
  client fan-in).
- The remaining templates: `document-review`, `task-runner`,
  `artifact-viewer`, `form-tool`, `agent-console`.
- The **Postgres `Store` driver** for distributed / at-scale HTTP
  deployments.
- The **`dockyard publish`** verb (and the open registry behind
  it, if one is built).
- **Signed releases + SLSA provenance** — cosign signing of the
  release artifacts and a SLSA attestation pipeline.
- **A versioned docs tree** — multi-version side-by-side docs
  switching when there are multiple stable releases.

### Acknowledgements

Dockyard is the third product in a three-part ecosystem — Portico
(the MCP gateway), Harbor (the agent framework), and Dockyard
itself (the MCP servers and apps users touch).

The build methodology was doc-driven from day zero: six research
briefs (`docs/research/`) informed `RFC-001-Dockyard.md` (the
design source of truth); the RFC informed a master phase plan
(`docs/plans/README.md`) that decomposed V1 into thirty named
phases (00..30); each phase plan inherited the master plan's
done-definition and shipped its surface only when its smoke script
reported `OK ≥ count(criteria)` and `FAIL = 0` and the preflight
gate stayed green. Every architectural decision is append-only in
[`docs/decisions.md`](docs/decisions.md) (153 settled D-NNN
entries before this PR; D-154..D-160 land with this release).

Built on the official [Go MCP
SDK](https://github.com/modelcontextprotocol/go-sdk), Svelte,
Vite, [tygo](https://github.com/gzuidhof/tygo),
[google/jsonschema-go](https://github.com/google/jsonschema-go),
[cobra](https://github.com/spf13/cobra),
[fsnotify](https://github.com/fsnotify/fsnotify),
[modernc.org/sqlite](https://gitlab.com/cznic/sqlite), and
[VitePress](https://vitepress.dev). Apache-2.0 licensed.

[Unreleased]: https://github.com/hurtener/dockyard/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/hurtener/dockyard/releases/tag/v1.0.0
