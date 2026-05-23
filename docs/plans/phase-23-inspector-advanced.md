# Phase 23 — Inspector advanced + `dockyard inspect`

## Summary

Phase 23 closes the inspector (Wave 8). It fills the four DetailRail tabs Phase 22
scaffolded — **Fixtures**, **Tools/Resources**, **Verdicts**, **Tasks** — and the
**Host** + **Display-mode** `PageHeader` controls; it adds per-tool latency / error
/ volume analytics derived from the `obs/v1` stream; and it ships the standalone
`dockyard inspect` CLI command. The inspector stays dev-mode-gated, localhost-only,
and read-only — Phase 23 adds no mutating surface.

## RFC anchor

- RFC §12 — the inspector (the fixture switcher wired to generated contracts;
  per-tool analytics; contract-drift / schema-validation / spec-compliance
  verdicts; capability-set emulation; task-lifecycle rendering; standalone
  `dockyard inspect` with `--url`, `--port`, `--no-open`).
- RFC §6 — contract-first: the fixture switcher derives its six fixtures from the
  generated contracts, never hand-written (P1).
- RFC §8.6 — Tasks × Apps: the Tasks panel renders the task five-status lifecycle
  and `input_required` round-trips.
- RFC §9 — the CLI: `dockyard inspect` joins the command surface.
- RFC §7.5 — host profiles: the capability-set emulation axis — a capability
  toggle set, never a hardcoded per-host matrix.

## Briefs informing this phase

- brief 05 — observability & landscape.
- brief 04 — MCP-use DX teardown.

## Brief findings incorporated

- **brief 05 §2.3** — the inspector "renders the App, emulates the bridge,
  switches devices" and adds "what only the framework that owns the contracts
  can: drift detection, fixture-driven state testing, host-compat verdicts."
  Phase 23 delivers exactly that third tier: the Fixtures panel derives its six
  states from the generated contracts, and the Verdicts panel runs
  `internal/validate.Run` + the `internal/codegen` drift cross-check.
- **brief 05 §4.2** — "the inspector is dev-mode-gated, localhost-only, and
  read-only; the CVE-2025-49596 RCE in the official Inspector's proxy is the
  cautionary tale." `dockyard inspect` reuses Phase 22's `ErrNonLoopbackBind`
  gate verbatim; the new Tools/Resources invoke path is a dev test answered from
  a fixture or a localhost server, never an arbitrary-execution proxy.
- **brief 04** — the DX bar: an App must render and exercise its UI states
  locally without a real host. The fixture switcher feeds the App synthetic
  `structuredContent` so every UI state (empty / error / permission / slow /
  large) is reachable before a backend exists.
- **brief 05 §2.2** — capability negotiation is read from the handshake, never a
  per-host matrix. The Host control is a set of capability toggles (Apps / Tasks
  / display modes) driven through the injectable `hostContext` seam; flipping a
  toggle re-runs the `ui/initialize` negotiation so the App degrades for real.

## Findings I'm departing from (if any)

None. Phase 23 is the remaining RFC §12 surface the master plan assigns to it;
the BYOK chat tab and the multi-server console are RFC §12 post-V1 scope and are
not built.

## Goals

- A **Fixtures** DetailRail panel: a `happy` / `empty` / `error` / `permission` /
  `slow` / `large` switcher wired to the generated contracts; selecting a fixture
  feeds the App synthetic `structuredContent` and closes Phase 22's `tools/call`
  not-wired seam.
- Per-tool **latency / error / volume analytics** derived from the `obs/v1`
  stream the inspector already consumes (P2).
- A **Verdicts** DetailRail panel surfacing contract-drift, schema-validation, and
  spec-compliance results as `StatusChip` rows, reusing `internal/validate.Run`.
- **Capability-set emulation**: the `PageHeader` Host control — a capability
  toggle set (Apps / Tasks / a display mode) driven through the injectable
  `hostContext`, never a hardcoded host matrix.
- A **Tasks** DetailRail panel rendering the task five-status lifecycle and
  `input_required` round-trips as a `Timeline`.
- A **Tools/Resources** DetailRail panel: list the server's tools/resources and
  invoke a tool / read a resource, read-only.
- The standalone **`dockyard inspect`** command: `--url`, `--port`, `--no-open`;
  dev-mode-gated, localhost-only.

## Non-goals

- The BYOK chat tab and the multi-server console — RFC §12 post-V1.
- A production MCP client — `dockyard inspect --url` attaches the read-only
  inspector relay to a server's obs endpoint; it is not an MCP client (P4).
- New `web/ui` shared components — Phase 23 composes the existing inventory.
- Embedding the inspector inside the `dockyard dev` supervisor process tree —
  the `internal/devloop` supervisor is a self-contained process orchestrator;
  embedding the inspector backend into it is a devloop change, not an inspector
  change. Phase 23 ships `dockyard inspect` as the deliberate inspector entry
  point and notes the `dockyard dev` auto-attach as a clean follow-up seam (a
  caller already runs `dockyard inspect --url` against the `dockyard dev`
  server's HTTP transport). Filed as D-101.

## Acceptance criteria

- [ ] The fixture switcher exists with the six fixtures (`happy`/`empty`/`error`/
      `permission`/`slow`/`large`) wired to the generated contracts; selecting a
      fixture drives the App's UI state.
- [ ] Capability-set emulation exists as a capability toggle set (Apps / Tasks /
      display modes) — no hardcoded per-host matrix — and degrades an App
      correctly (an Apps-off or Tasks-off toggle is reflected in the handshake).
- [ ] The Verdicts panel renders contract-drift / schema-validation /
      spec-compliance results as `StatusChip` rows.
- [ ] The Tasks panel renders the task lifecycle and `input_required` round-trips.
- [ ] `dockyard inspect` is wired with `--url` / `--port` / `--no-open` and
      attaches the inspector to any running MCP server.

## Files added or changed

- `internal/cli/inspect.go` (new) — the `dockyard inspect` command constructor.
- `internal/cli/inspect_test.go` (new) — command unit tests.
- `internal/cli/root.go` — one `root.AddCommand(newInspectCmd())` line.
- `internal/inspector/assets.go` — `/api/verdicts` + `/api/contracts`
  read-only handlers.
- `internal/inspector/verdicts.go` (new) — the verdicts seam over
  `internal/validate.Run`.
- `internal/inspector/verdicts_test.go` (new).
- `internal/inspector/inspector.go` — `Options.Verdicts` + `Options.Contracts`
  wiring.
- `web/inspector/src/lib/fixtures.ts` (new) — fixture generation from contracts.
- `web/inspector/src/lib/FixturesPanel.svelte` (new).
- `web/inspector/src/lib/ToolsPanel.svelte` (new).
- `web/inspector/src/lib/VerdictsPanel.svelte` (new).
- `web/inspector/src/lib/TasksPanel.svelte` (new).
- `web/inspector/src/lib/AnalyticsPanel.svelte` (new).
- `web/inspector/src/lib/analytics.ts` (new) — per-tool analytics from obs/v1.
- `web/inspector/src/lib/capability.ts` (new) — the capability toggle model.
- `web/inspector/src/lib/HostControl.svelte` (new) — the Host capability toggles.
- `web/inspector/src/lib/contracts.ts` (new) — the generated-contract model.
- `web/inspector/src/lib/api.ts` — `fetchVerdicts` / `fetchContracts`.
- `web/inspector/src/App.svelte` — wires the new panels + Host control.
- `web/inspector/src/__tests__/` — Vitest specs for every new module.
- `scripts/smoke/phase-23.sh` (new).
- `test/integration/phase23_inspector_test.go` (new).
- `docs/decisions.md` — D-099..D-101.
- `docs/glossary.md` — fixture switcher, capability-set emulation, inspector
  verdict.
- `docs/plans/README.md` — Phase 23 marked landed.
- No agent-skill / docs-site update: Phase 29 has not landed, so §19 is inert.

## Public API surface

- `inspector.Options.Verdicts inspector.VerdictSource` — an optional verdict
  source; when set, `GET /api/verdicts` returns its result.
- `inspector.Options.Contracts inspector.ContractsSource` — an optional
  generated-contract source; when set, `GET /api/contracts` returns its JSON.
- `inspector.Verdict{ Check, Severity, Message string }` — one verdict row.
- `inspector.VerdictsFromValidate(projectDir string) inspector.VerdictSource`
  — adapts `internal/validate.Run` into the verdict source.
- `cli.newInspectCmd()` — internal; the `dockyard inspect` constructor.

## Test plan

- **Unit:** Go — `verdicts.go` table-driven tests (validate report → verdict
  rows; severity mapping; missing-project tolerated); `inspect.go` flag-parsing
  and loopback-gate tests. Frontend — Vitest for `fixtures.ts` (six fixtures from
  a contract; shape per fixture), `analytics.ts` (per-tool latency/error/volume
  from obs/v1 events), `capability.ts` (toggle set → `hostContext`, no matrix),
  `contracts.ts`; component tests for the five new panels + `HostControl`.
- **Integration:** `test/integration/phase23_inspector_test.go` — a real
  `runtime/server` + App + `runtime/tasks` + `obs.SSESink`; `dockyard inspect`'s
  inspector backend attached; asserts the relay attaches to the running server,
  the verdicts endpoint surfaces a real validate result, and a real task
  lifecycle emits obs/v1 `task.progress` events the inspector relays. ≥1 failure
  mode (the non-loopback `--port` rejection). Runs under `-race`.
- **Concurrency / golden:** the verdict source is invoked from the HTTP handler
  concurrently — covered by the existing relay concurrency test pattern. No
  golden output (the panels are not codegen).

## Smoke script additions

- The fixture switcher exists and the six fixtures are wired to generated
  contracts.
- The verdicts panel + backend endpoint exist.
- Capability-set emulation exists and is a toggle set — assert NO hardcoded host
  matrix in the capability module.
- The Tasks panel exists.
- The Tools/Resources panel exists.
- `dockyard inspect` is registered with `--url` / `--port` / `--no-open`.
- The `web/inspector` frontend gate passes.

## Coverage target

- `internal/inspector` — 80% (new-package band; existing entry).
- `internal/cli` — 70% (cli-tooling band; existing entry).
- `web/inspector` — 70% (frontend band; existing `coverage.thresholds`).

## Dependencies

- Phase 22 — the inspector core (the rail-tab + Host-control + `tools/call`
  seams Phase 23 fills).
- Phase 14 — `runtime/tasks` taskstore (the task-lifecycle surface the Tasks
  panel renders).
- Phase 21 — the `dockyard test` quality gate / the `internal/validate` seam the
  Verdicts panel reuses.

## Risks / open questions

- `dockyard inspect --url` attaches the read-only obs relay to a server's
  `/obs/v1/stream` endpoint; it deliberately does not open an MCP session, so it
  is not a production MCP client (P4). The Tools/Resources invoke path in the
  standalone case is fixture-backed; live invocation against a `dockyard dev`
  server is the in-`dev` path. Documented in D-099.
- The fixture switcher derives fixtures from generated contracts shipped to the
  inspector as a read-only JSON contract model — when no contracts are present
  (a blank server) the panel shows the four-state empty state.

## Glossary additions

- **fixture switcher** — the inspector's Fixtures panel: a `happy`/`empty`/
  `error`/`permission`/`slow`/`large` selector wired to the generated contracts,
  feeding the App synthetic `structuredContent` so UI states are exercised
  without a backend (RFC §12, §6).
- **capability-set emulation** — the inspector's Host control: a capability
  toggle set (Apps / Tasks / display modes) driven through the injectable
  `hostContext`, so an App can be rendered as a host that does or does not
  negotiate a capability — never a hardcoded per-host matrix (RFC §7.5, §12).
- **inspector verdict** — one row of the inspector's Verdicts panel: a
  contract-drift, schema-validation, or spec-compliance result surfaced as an
  ok / warn / error `StatusChip`, sourced from `internal/validate.Run`.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`

## Remediation history

### R1 — inspector production wiring (pre-Wave-9 depth audit)

A depth audit found that Phase 23 built `Options.Verdicts` and
`Options.Contracts` and covered them in tests, but the shipping `dockyard
inspect` command's `runInspect` (`internal/cli/inspect.go`) never set them — it
built `inspector.Options` with only `Addr`/`Relay`/`Assets`/`ServerInfo`/
`Logger`. So in the only shipping entry point, `/api/verdicts` and
`/api/contracts` always returned `[]`, leaving the Verdicts panel and the
Fixtures switcher permanently empty in the product.

R1 fixed this. `dockyard inspect` gained a `--dir` flag (defaulting to the
working directory, via the same `resolveProjectDir` seam `generate` / `validate`
/ `test` use) and `runInspect` now wires `Options.Verdicts` from
`VerdictsFromValidate(dir)` and `Options.Contracts` from the new
`ContractsFromProject(dir)` — see D-104. `ContractsFromProject` reads the
project manifest and the generated `internal/contracts/*.schema.json` files;
both sources degrade to their honest empty state when `--dir` names no project.

R1 also corrected the `inspect.go` doc/help text, which falsely claimed the
inspector "runs automatically inside `dockyard dev`" — D-101 deliberately
deferred that auto-attach; the text now describes reality. The auto-attach
remains deferred (not implemented).

The audit further noted that every prior inspector test constructed
`inspector.Options` directly, bypassing `runInspect` — which is why the wiring
gap shipped undetected. R1 added `test/integration/r1_inspector_test.go`, which
drives the real `dockyard inspect` binary as a subprocess against a real HTTP
MCP server and a real project directory and asserts `/api/verdicts`,
`/api/contracts`, and `/api/apps` all return real content — so a future
regression of the CLI wiring fails a test.

### R4 B1 + S6 — `make build` embeds the real web/inspector SPA (depth-audit-2)

A second pre-Wave-9 depth audit found that D-098's "wiring the production
`web/inspector` build into the binary is the Phase 23 `dockyard inspect`
packaging step" was settled but **never built**: `internal/inspector/dist/`
shipped only a placeholder `index.html` ("run `make web` to build it"), and
neither `Makefile` nor `.github/workflows/ci.yml` had a step that ran
`vite build` for `web/inspector` and copied the result into
`internal/inspector/dist/` before `go build`. So the shipped `dockyard
inspect` command served a placeholder page — the inspector UI was unusable
to a developer who installed the binary.

R4 closes the packaging step. A new `make inspector-bundle` target runs the
`web/inspector` Vite build (after `npm ci` if `node_modules/` is absent) and
stages the output (`web/inspector/dist/`) into `internal/inspector/dist/` so
the `//go:embed all:dist` directive in `internal/inspector/assets_embed.go`
picks up the real SPA. `make build` declares this target as a prerequisite,
so the canonical `make build` produces a `bin/dockyard` whose inspector is
the real Svelte SPA. The CI `go` job (`build / vet / test / lint`) gained a
`setup-node` step so the build pipeline has `npm` available; the `preflight`
job's Node cache list now includes `web/inspector/package-lock.json` for
parity. `make build` still pins `CGO_ENABLED=0` — the staged frontend is a
build artifact, not a CGo dependency. The placeholder `index.html` was
replaced with a tracked `.gitkeep` anchor (so `//go:embed all:dist` always
resolves) and the dist tree was added to `.gitignore` (so a rebuild never
dirties the working tree); when the bundle has not been staged the inspector
backend falls back to its in-Go `placeholderHTML` page, exactly as before.

S6 lands the regression guard: `scripts/smoke/phase-23.sh` now asserts the
committed/staged `internal/inspector/dist/index.html` is the real Vite SPA
(a `<script type="module" crossorigin src=...>` reference to the hashed
asset bundle), failing CI loud when the legacy placeholder string returns or
when no script tag is present. Together B1 + S6 mean a future regression of
the `make inspector-bundle` prerequisite — or a hand-edit that re-injects a
non-SPA index.html — fails preflight, not the user.
