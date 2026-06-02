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

### Changed

- **`dockyard-bridge`'s `ui/` wire layer is now pinned to the vendored official
  `@modelcontextprotocol/ext-apps` schema. (D-182)** The bridge previously
  hand-transcribed the MCP Apps wire dialect, which drifted silently (the cause
  of the 1.6.1 handshake bugs). The schema is vendored into the repo by upstream
  SHA, and a **wire-conformance test** now `.parse()`s the bridge's outbound wire
  against it — a drift is a failing build, not a blank App in a host. The shipped
  App bundle stays Zod-free and consumers need no schema dependency (the schema
  is referenced only by the bridge's test layer).
- **`HostContext` gains schema-accurate fields (additive).** `containerDimensions`
  now models flexible sizing (`maxWidth`/`maxHeight`) alongside fixed
  `width`/`height`; `styles` gains `css.fonts`; `toolInfo`, `platform`, and
  `deviceCapabilities` match the schema.

### Fixed

- **`dockyard-bridge` advertises `appCapabilities.availableDisplayModes`, not
  `displayModes`. (D-182, item A)** The host's parse silently stripped the
  non-schema `displayModes` key, so it never learned which display modes an App
  supported and fullscreen/pip degradation never worked. The public `displayModes`
  bridge option is unchanged.
- **`dockyard-bridge` handles `ui/resource-teardown` as a request, not a
  notification. (D-182, item B)** A spec host sends teardown as a request and
  waits for the View's response before tearing the iframe down; the bridge now
  responds, then closes. Adds the app-initiated `ui/notifications/request-teardown`
  (`BridgeShell.requestTeardown()`).
- **`dockyard-bridge` applies host fonts. (D-182, item D)** Host-provided
  `styles.css.fonts` CSS is injected into the View document so the host's fonts
  load.
- **`ui/message` and `ui/update-model-context` now send schema-conformant
  content. (D-182 — checkpoint audit)** Both sent a bare-string `content` (and
  `ui/message` allowed a non-`user` role); the schema requires `content:
  ContentBlock[]` (and `role: "user"`), so a spec host would have rejected them.
  `sendMessage` now wraps a string into a text block; `UpdateModelContextParams.content`
  is `ContentBlock[]`. The conformance test now `.parse()`s **every** View→host
  request (open-link, message, request-display-mode, update-model-context), not
  just the handshake.
- **The local inspector consumes the View's `size-changed` and `request-teardown`.
  (D-182, item 4 — checkpoint audit)** It sizes the preview iframe to the App's
  reported content height (mirroring a real host) and remounts on a teardown
  request, instead of silently dropping both.
- **Conformance coverage extended to the full wire (D-182 — second audit pass).**
  The conformance layer now also guards the bridge's **inbound** reads (a
  schema-valid `tool-input`/`tool-result`/`tool-cancelled`/`host-context-changed`
  reaches the App subscriber with every field intact), the **inspector's
  outbound** host→View wire (the reference host's `ui/initialize` result,
  `request-display-mode` result, and notifications `.parse()` clean), and the
  **server-emitted** `_meta.ui` + capability shapes. Inbound notification types
  (`arguments`, `width`/`height`) are now optional to match the schema.

### Packaging

- **`dockyard-bridge/spec`** exposes the vendored ext-apps schema (used by the
  inspector and the bridge's own conformance tests). Its `zod` +
  `@modelcontextprotocol/sdk` imports are provided by the consumer (`devDependencies`
  of both `dockyard-bridge` and `web/inspector`); they are intentionally **not**
  declared as peer dependencies — an optional peer makes a bundler stub it and
  breaks the production build. The package's `.` entry imports no zod, so App
  authors importing only `.` install nothing extra.
- **The local inspector is a faithful, validating spec host. (D-182, item 4)** It
  no longer sends a host→View `ui/notifications/initialized`; it marks itself ready
  when the View sends `initialized`, reads `availableDisplayModes`, and now
  **validates the View's `ui/initialize` against the vendored schema**, rejecting a
  non-spec shape with a JSON-RPC error. This removes the leniency that let the
  1.6.1 View bugs pass locally — the inspector now catches them. The schema is
  shared via a new opt-in `dockyard-bridge/spec` subpath (the package's `.` entry
  stays Zod-free for App consumers).

### Notes

- Dockyard's Tasks×Apps `ui/` notifications (`task-progress`,
  `elicitation-response`) are now explicitly fenced as **Dockyard extensions**
  outside the MCP Apps schema; they function only against a Dockyard-aware host
  (the inspector, or Harbor). (D-183)

## [1.6.1] - 2026-06-01

### Fixed

- **`dockyard-bridge` `ui/initialize` now uses the MCP Apps `ui/` dialect — a
  spec-compliant host no longer rejects the View handshake. (D-179)** The View's
  `ui/initialize` request sent base-MCP `{capabilities:{appCapabilities},
  clientInfo}`; the MCP Apps host (`@modelcontextprotocol/ext-apps`) validates the
  request against a strict schema requiring top-level `{appInfo, appCapabilities,
  protocolVersion}` (`appInfo` REQUIRED). The mismatch made the host return a
  JSON-RPC error for the first handshake message, so `connect()` rejected, `ready`
  never became true, and an otherwise-correct App rendered blank with no visible
  error. The bridge now sends the `ui/` dialect shape. (Hosts only spoke this
  dialect; the local inspector accepted the base-MCP shape, which is why this
  passed locally.) The public `clientInfo` bridge option is unchanged.

- **`dockyard-bridge` SENDS `ui/notifications/initialized` rather than awaiting it.
  (D-180)** The handshake waited to *receive* `ui/notifications/initialized`
  before resolving `ready`. Per the JSON-RPC/MCP lifecycle (and the ext-apps
  reference View) the View is the initiator and *sends* `initialized` after the
  `ui/initialize` result, then is ready. A spec-compliant host never sends a
  View→host message, so the old code deadlocked. An inbound `initialized` from a
  non-spec host is now ignored.

- **`dockyard-bridge` reports View content size to the host via
  `ui/notifications/size-changed`. (D-181)** The bridge only ever *received*
  `size-changed` (host→View); it never measured or reported its own content
  height. A spec-compliant host sizes the App iframe from the View's report;
  without it the iframe collapses to ~0px and the App looks blank even after it
  paints. The bridge now runs a `ResizeObserver` and emits a de-duplicated
  `size-changed` on ready and on every change, torn down in `close()`.

## [1.6.0] - 2026-05-30

### Changed

- **`_meta.ui.domain` is now a host-supplied verbatim value; server-side
  auto-derivation is retired. (Behaviour change — minor.)** The MCP Apps spec
  makes `domain` host-dependent — the host *mints* the dedicated iframe origin
  and documents its format; a server copies it verbatim or leaves it empty.
  `App.Domain` is emitted on `resources/read` **byte-for-byte**; Dockyard no
  longer synthesises Claude's `{hash}.claudemcpcontent.com` subdomain (which a
  local connector rejects). **What changes for you:** a project that set
  `HostProfile: "claude"` + `Domain` previously got a derived
  `claudemcpcontent.com` origin and now gets its `Domain` verbatim — set `Domain`
  to the exact origin your host documents for a verified remote deployment, or
  leave it empty for the host's default per-conversation origin. `App.HostProfile`
  and `App.ServerURL` are **deprecated** (retained, ignored for derivation); the
  pluggable host-profile seam stays for a future host-blessed transform. The
  default scaffold and templates set no `Domain`, so they are unaffected. (D-176)

- **A stdio-only server that sets a `Domain` now warns at startup.** A dedicated
  origin is honoured only on a remote connector; a local (stdio) connector
  ignores it. `ServeStdio` logs a loud `slog.Warn` naming the App;
  `HTTPHandler` does not. (D-176)

- **The product templates scaffold the html-style
  `ui://<server>/<app>/index.html` resource URI.** This matches the reference MCP
  Apps SDK convention. The framework treats the `ui://` URI as an opaque string,
  so this is a convention + docs change only — an existing project's
  `ui://<server>/<app>` URI keeps working. (D-178)

### Added

- **A server-level opt-in to additionally emit the deprecated flat tool-UI
  `_meta` key.** `server.Options{EmitLegacyToolUIMeta: true}` makes every
  UI-bearing tool registered through the `runtime/tool` builder carry the
  deprecated flat key alongside the canonical nested `_meta.ui.resourceUri`, for
  a host that still reads the flat form. The default (off) is unchanged
  RFC-compliant nested-only output; the 2026-01-26 spec marks the flat form
  deprecated, so Dockyard never emits it by default. (D-177)

## [1.5.0] - 2026-05-29

### Changed

- **The frontend packages are renamed to unscoped names: `@dockyard/bridge` →
  `dockyard-bridge`, `@dockyard/ui` → `dockyard-ui`.** They publish under an
  unscoped personal-account name (the `@dockyard` org scope was unownable and
  blocked the v1.4.0 npm publish). An App's imports change accordingly
  (`import { createBridge } from 'dockyard-bridge'`); the templates and the
  `attach-a-ui-resource` skill are updated to match. Nothing was ever published
  under `@dockyard`, so there is no deprecation to manage. The internal
  inspector frontend keeps its `@dockyard/inspector` workspace name (it is never
  published). (D-174)

- **`Builder.UI` gained an optional visibility variadic** —
  `.UI(appName, tool.VisibilityApp)` sets `_meta.ui.visibility` for a UI-only
  action tool; omitting it keeps the spec default (model + app). New
  `tool.VisibilityModel` / `tool.VisibilityApp` consts. **Behaviour change:** a
  `.UI("name")` that references no registered App now returns a typed error at
  `Register` (previously a silent no-op) — register the App (`apps.Register`)
  before the tool. A correctly-ordered project is unaffected. (D-173)

### Fixed

- **A framework-wide wiring audit (the same class as the `_meta.ui` bug) fixed
  three declared-but-unwired seams:**
  - **`require_spec_compliance` is now enforced.** The quality flag was
    declared, scaffolded `true`, and documented "enforced by `dockyard
    validate`", but no consumer read it — the spec-compliance check ran
    unconditionally, so toggling the flag did nothing. It now gates the check
    (opt-out), consistent with the other `quality.*` gates. All shipped
    manifests set it `true`, so no real project changes behaviour. (D-175)
  - **`@dockyard/bridge`'s `ui/resource-teardown` now tears the View down.**
    The notification was documented as triggering `BridgeShell.close()` but was
    never dispatched — a production host sending it would leak the bridge's
    listeners/transport and leave `ready` stuck `true`. It now calls `close()`.
  - **The bridge retains the negotiated `protocolVersion` + `hostInfo`** from
    the `ui/initialize` result (exposed as `bridge.protocolVersion` /
    `bridge.hostInfo`); `protocol.ts` promised retention but both were
    discarded.
- **`tool.New[...].UI(appName).Register(srv)` now emits the tool→App link.**
  The builder previously dropped it silently — the registered tool carried no
  `_meta.ui.resourceUri` (RFC §7.1), so a host that renders MCP Apps showed the
  text fallback instead of the App. The builder now resolves the App's name to
  its `ui://` URI (via a new `server.AppLink` seam recorded by `apps.Register`)
  and emits `_meta.ui` at `Register`. The `analytics-widgets` template is fixed
  automatically (no template change). (D-173)

## [1.4.0] - 2026-05-29

### Added

- **`@dockyard/bridge` and `@dockyard/ui` are published to npm.** A
  scaffolded UI project's `web/` now resolves them from npm with **no
  `--dockyard-path` and no local Dockyard checkout** — `dockyard new
  --template analytics-widgets` then `cd web && npm install` just works. The
  packages set `publishConfig.access: "public"`, track the repo version, and
  publish from a gated, idempotent tag-push job (verified by `npm pack` + a
  scaffold-install build first). `--dockyard-path` reverts to a pure
  build-from-source convenience (D-172).
- **Bridge View-side task-progress channel.** `@dockyard/bridge` exposes a
  typed `bridge.onTaskProgress((p) => …)` subscription so an MCP App's card
  can render a live progress value (e.g. "62%") for a long-running task,
  fed by a new `ui/notifications/task-progress` host→View notification
  (RFC §8.4). The Dockyard runtime emits each `TaskHandle.Progress` /
  `TaskHandle.Status` call as an `obs/v1` `task.progress` `progress`-phase
  event; the inspector forwards those to the App preview, so the channel is
  demoable through `dockyard inspect`. A host that does not forward progress
  degrades cleanly — the subscriber simply never fires (D-171).

### Changed

- **`obs/v1` `task.progress` payload gained an optional `fraction` field**
  (the task's completion fraction in [0, 1] at a mid-flight progress point).
  This is an **additive** change to the `obs/v1` contract — existing
  consumers that do not read it are unaffected, the schema version stays
  `dockyard.obs/v1`, and the golden tests pin the new shape (D-171).

## [1.3.0] - 2026-05-29

### Added

- `dockyard new --here` — scaffold into an existing **non-empty** directory
  (e.g. one you already `git init`-ed). Existing files are left untouched;
  a scaffold output that would overwrite a file is refused, never silently
  overwritten.

### Changed

- **The `require_fixtures` and `require_contract_tests` quality gates are
  now enforced** by `dockyard validate` (previously declared but inert).
  `require_fixtures` is UI-scoped — each tool with a `ui:` app must ship
  inspector fixtures (`fixtures/<tool>/*.json`); a non-UI tool needs none.
  `require_contract_tests` requires the project to carry at least one
  `*_test.go`. **Behaviour change:** a project that turned a gate on but
  does not satisfy it (a UI tool with no fixtures, or a project with no
  test) now fails `dockyard validate` where it previously passed. A freshly
  scaffolded project — blank or template — stays green.
- **`dockyard new` pins the CLI's version into the scaffolded `go.mod`**
  (instead of the `v0.0.0` placeholder) when the CLI knows its release
  version, so a project that drops the local `replace` directive resolves
  the published module without a hand edit. Released binaries and
  `go install …@vX.Y.Z` now also report their real version.

### Fixed

- `dockyard new`'s "directory not empty" error now **names the entries** it
  found (so a hidden `.git/` or `.gitignore` is visible as the cause) and
  points at `--here`.

## [1.2.0] - 2026-05-29

### Added

- `dockyard new --no-postgen` — opt out of the new post-scaffold steps
  (for hermetic / air-gapped / CI runs, or to run them yourself).

### Changed

- **`dockyard new` now runs `go mod tidy` + `dockyard generate` for you
  at scaffold time**, so a fresh project — blank or `--template` —
  reaches a green `dockyard validate` on the first try with no manual
  command. The steps are best-effort: a failure (e.g. no module-proxy
  reach) prints a warning and the manual fallback rather than failing the
  scaffold. Opt out with `--no-postgen`.
- **Release notes now carry an auto-generated commit supplement.** A
  GitHub Release body is the hand-authored `CHANGELOG.md` section
  followed by a Conventional-Commits-derived list of what landed
  (`feat` → Added, `fix` → Fixed, the rest → Changed; `docs`/`chore`/
  `test`/`ci`/`build`/`style` dropped). The hand-authored prose stays the
  canonical narrative; the supplement is appended on a tag push only.

## [1.1.0] - 2026-05-26

### Added

- `dockyard dev` now **auto-attaches the inspector** as a third
  supervised child alongside the Go server and Vite. The inspector
  URL is printed to stdout once it is reachable. Opt out with
  `--no-inspector` (for CI / headless dev runs). The dev loop pins
  the supervised Go server to HTTP on `127.0.0.1:8080` by default
  so the inspector has a known MCP base URL to attach to; a
  developer who already exported `DOCKYARD_TRANSPORT` /
  `DOCKYARD_HTTP_ADDR` wins. Closes the v1.0.0 deferral; decisions
  D-161 (the auto-attach seam), D-162 (the in-process choice).
- The inspector grows a **Prompts rail tab** that lists the
  attached server's registered MCP prompts and lets an operator
  invoke `prompts/get` against one. Two new operator-initiated
  client-shaped backend endpoints — `GET /api/prompts` (read-only
  `prompts/list`) and `POST /api/prompts/get` (one short-lived
  MCP client session per click) — back it. Closes the v1.0.0
  deferral; decision D-163.
- New CLI flags on `dockyard dev`: `--no-inspector` (skip the
  supervised inspector child) and `--inspector-addr` (override the
  inspector's loopback bind; default `127.0.0.1:0`).
- **Scaffold + `dockyard run` auto-wire the Tasks engine when the
  manifest declares task-supporting tools** (D-164). Whenever the
  project's manifest declares any tool with `task_support: optional`
  or `task_support: required`, the scaffolded `main.go` now
  constructs `tasks.NewInMemoryStore()` + `tasks.NewEngine(...)` and
  attaches it via `server.Options{Tasks: engine}` — no hand edit
  required. A new `scaffold.Options.ExampleToolTaskSupport` field
  lets a caller choose the example tool's declaration; the renderer
  branches on it. The blank scaffold (whose example tool declares
  `task_support: forbidden`) and the `analytics-widgets` template
  (whose tools all declare `forbidden`) keep their engine-free
  `main.go` — zero overhead. `dockyard run` reads the project's
  manifest at start time and warns when the manifest declares task
  support but `main.go` does not appear to wire the engine.
- **`HostProfile.RequiresServerURL() bool`** (D-165). A new method
  on the `runtime/apps.HostProfile` interface declares whether the
  profile's domain derivation requires a non-empty server URL — the
  Claude profile returns `true` (it binds the signed origin to the
  server URL per D-063/D-064); the generic pass-through profile
  returns `false`. The capability-degradation testgate category
  consults the method to exercise each profile honestly, replacing
  the synthetic-URL workaround D-145 recorded.

### Changed

- **`HostProfile` interface gains `RequiresServerURL`.** This is
  additive for callers, breaking for an out-of-tree
  implementer — they must add the one-line method. The semver minor
  framing follows D-159; the change is documented in D-165.
- **The scaffold's example-tool manifest now writes the explicit
  `task_support:` declaration** rather than always writing the
  literal string "forbidden". The default (the no-template
  scaffold's `task_support: forbidden`) is unchanged; a custom
  `ExampleToolTaskSupport` flows through to the rendered YAML.
- **`scripts/preflight.sh` discovers `vN.N-wave-*.sh` smoke scripts**
  alongside the per-phase smoke scripts. Post-V1 release waves are
  not phase-paired by drift-audit (a wave is not a phase) but their
  smoke checks still run on every preflight (D-164).

### Removed

- **The `syntheticServerURL` constant in
  `internal/testgate/categories.go`.** Retired by D-165 — the
  capability category now consults
  `HostProfile.RequiresServerURL()` to exempt signing profiles from
  the empty-URL derivation rather than fabricating a placeholder
  URL to dodge the invariant.

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
- **P2 — Observability is a protocol.** The runtime emits **Logbook**,
  Dockyard's canonical event stream (wire format identifier: `obs/v1`).
  The inspector and any future multi-server console are pure clients of
  that contract; no component reads runtime internals to observe.
  OpenTelemetry export is an *optional* adapter, off by default — never
  a prerequisite to see what your server is doing locally.
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
  Prompts support via `AddPrompt` with Logbook carrier events.
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
- **`runtime/obs`** — the Logbook implementation (wire format
  identifier: `obs/v1`), Dockyard's canonical, versioned observability
  event protocol. A non-blocking headless emitter (a slow consumer
  never stalls the runtime); the in-memory ring buffer; the
  out-of-band localhost SSE sink the inspector consumes; the optional
  OTel adapter (MCP semconv: `mcp.*` / `gen_ai.*` attributes) for
  export to an external observability stack; the MCP `logging` →
  Logbook `log`-event bridge so a Dockyard server still speaks
  standard MCP logging to any client. Shape + size capture by default
  — secrets and PII never leak into the event stream; full-content
  capture is opt-in and redaction-aware.
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
- **Live Logbook stream.** A read-only fan-out subscriber to the
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
- Logbook tool input/output capture defaults to shape+size;
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
- The **analytics-widgets / Claude signed-origin** follow-up. The
  capability test uses a synthetic `serverURL` workaround in
  `internal/testgate` (D-145); the underlying improvement — manifest
  `_meta.ui.domain`, or a different host-profile API — is open.
- The **ChatGPT Apps SDK** as a second host protocol.
- The **multi-server fleet console** (a post-V1 pure Logbook
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

[Unreleased]: https://github.com/hurtener/dockyard/compare/v1.6.1...HEAD
[1.6.1]: https://github.com/hurtener/dockyard/releases/tag/v1.6.1
[1.6.0]: https://github.com/hurtener/dockyard/releases/tag/v1.6.0
[1.5.0]: https://github.com/hurtener/dockyard/releases/tag/v1.5.0
[1.4.0]: https://github.com/hurtener/dockyard/releases/tag/v1.4.0
[1.3.0]: https://github.com/hurtener/dockyard/releases/tag/v1.3.0
[1.2.0]: https://github.com/hurtener/dockyard/releases/tag/v1.2.0
[1.1.0]: https://github.com/hurtener/dockyard/releases/tag/v1.1.0
[1.0.0]: https://github.com/hurtener/dockyard/releases/tag/v1.0.0
