# Phase 22 — Inspector core: bridge host-half + obs view

## Summary

Phase 22 builds the **inspector core** — Dockyard's local test/debug surface
(RFC §12). It delivers the Go `internal/inspector` localhost HTTP backend, the
**host half** of the `ui/` postMessage bridge, the `web/inspector` Svelte
frontend (composing the `web/ui` design system), and the live `obs/v1` event
stream + JSON-RPC log views. The inspector is dev-mode-gated, localhost-only, and
read-only — it refuses any non-loopback bind. Phase 23 extends this core with the
fixture switcher, analytics, verdicts, capability emulation, task rendering, and
the standalone `dockyard inspect` command.

## RFC anchor

- RFC §12 — the inspector (the local test/debug surface; dev-mode-gated,
  localhost-only, read-only; the host half of the `ui/` bridge; the
  CVE-2025-49596 cautionary tale; what it surfaces).
- RFC §7.2 — the three display modes (display mode is a runtime negotiation).
- RFC §7.3 — the bridge shell library: the inspector is the other consumer of the
  host half of the `ui/` postMessage dialect.
- RFC §11 — the `obs/v1` stream the inspector consumes as a pure SSE client.

## Briefs informing this phase

- brief 05 — observability & landscape (the inspector design, the CVE tale).
- brief 04 — MCP-use DX teardown (the inspector DX bar to clear).
- brief 01 — the MCP Apps bridge (the `ui/` postMessage dialect).

## Brief findings incorporated

- **brief 05 §4.2** — "the inspector is dev-mode-gated, localhost-only, and
  read-only; the CVE-2025-49596 RCE in the official Inspector's proxy is the
  cautionary tale." The Go backend makes the loopback bind an *explicit* typed
  check (`errNonLoopbackBind`) that runs before the listener opens — a
  non-loopback bind is never served. The backend is read-only: it relays the
  obs stream and an RPC log; it is never an arbitrary-execution proxy.
- **brief 05 §2.3** — the inspector "renders the App, emulates the bridge,
  switches devices" and adds drift detection / fixture testing only a framework
  that owns the contracts can. Phase 22 delivers render + bridge emulation; the
  drift/fixture surface is the Phase 23 seam.
- **brief 01 §2.4** — the `ui/` dialect (`ui/initialize`, `ui/notifications/*`,
  `hostContext`, host→view notifications). The host half reuses `web/bridge`'s
  `protocol.ts` verbatim — the protocol constants are never forked.
- **brief 04** — the DX bar: an App must render and complete its handshake
  locally without a real host. The inspector's host-half bridge completes a real
  `ui/initialize` round-trip with the `web/bridge` View half over a
  `MessageChannel`-equivalent `postMessage` channel.

## Findings I'm departing from (if any)

None. Phase 22 is a strict subset of RFC §12 — the advanced surface (fixtures,
analytics, verdicts, capability emulation, task rendering, `dockyard inspect`) is
deferred to Phase 23 by the master plan, not a departure.

## Goals

- A Go `internal/inspector` package: a localhost-only, dev-mode-gated, read-only
  HTTP server that serves the inspector UI and relays the obs/v1 SSE stream and a
  JSON-RPC log.
- The **host half** of the `ui/` bridge: renders an MCP App in a sandboxed iframe
  under a deny-by-default CSP, completes the `ui/initialize` handshake, supplies
  `hostContext`, fans host→view notifications, and reflects display-mode
  negotiation.
- The `web/inspector` Svelte frontend composing the `web/ui` inventory to the
  `design-spec.md §4` layout and the approved `mockups/inspector.png`.
- The live `obs/v1` event stream rendered as a `Timeline`, and the JSON-RPC log
  with method filtering — both routing through the four-state `PageState`.

## Non-goals

- The fixture switcher, per-tool analytics, contract-drift / spec-compliance
  verdicts, capability-set emulation, and task-lifecycle rendering — **Phase 23**.
- The standalone `dockyard inspect --url/--port/--no-open` command — **Phase 23**.
  Phase 22 exposes the inspector as a Go API (`inspector.New` / `Serve`) only.
- Device/viewport emulation deep behaviour — Phase 22 scaffolds the App frame;
  the device matrix is Phase 23.

## Acceptance criteria

- [ ] An MCP App renders in the inspector and completes its `ui/initialize`
      handshake (the host half ↔ the `web/bridge` View half).
- [ ] The live `obs/v1` event stream displays in the Events panel.
- [ ] The inspector refuses any non-localhost bind — a non-loopback bind address
      is a typed error and is never served.
- [ ] `internal/inspector` exists, builds CGo-free, and is concurrent-safe under
      `-race` when fanning events to multiple UI clients.
- [ ] The `web/inspector` frontend exists, is in the `make web` set, has a
      coverage threshold, and composes `@dockyard/ui` (never re-implements a
      component).

## Files added or changed

- `internal/inspector/` (new) — `inspector.go` (the server + loopback gate),
  `relay.go` (the obs SSE + RPC-log relay), `assets.go` (embeds `web/inspector`
  `dist/`), `doc.go`, tests.
- `web/inspector/` (new) — the Svelte Vite app: `src/host/` (the host-half
  bridge), `src/lib/` (the inspector views composing `web/ui`), `src/App.svelte`,
  config (`package.json`, `vite.config.ts`, `vitest.config.ts`, `tsconfig.json`,
  `svelte.config.js`), tests.
- `Makefile` — `WEB_PROJECTS` adds `web/inspector`.
- `internal/coveragecheck/coverage.json` — a `new-package` entry for
  `internal/inspector`.
- `scripts/smoke/phase-22.sh` (new).
- `test/integration/inspector_test.go` (new) — the end-to-end test.
- `docs/decisions.md` — D-096..D-098.
- `docs/glossary.md` — inspector, host-half bridge, inspector relay.
- `docs/plans/README.md` — Phase 22 marked landed.
- No agent-skill / docs-site update: Phase 29 has not landed, so §19 is inert.

## Public API surface

- `inspector.New(opts inspector.Options) (*inspector.Inspector, error)` — builds
  the inspector backend; returns `ErrNonLoopbackBind` for a non-loopback `Addr`.
- `(*inspector.Inspector).Serve(ctx) error` / `Addr() string` / `Close() error`.
- `inspector.Options{ Addr string; ObsSink *obs.SSESink; App *AppPreview;
  ServerInfo ServerInfo }`.

## Test plan

- **Unit:** Go — table-driven loopback-gate tests (loopback accepted, wildcard /
  non-loopback / malformed rejected); relay handler tests; asset-serving tests.
  Frontend — Vitest + `@testing-library/svelte` for the host-half bridge
  handshake, the Events panel, the RPC panel, and the App-frame wrapper.
- **Integration:** `test/integration/inspector_test.go` — a real `runtime/server`
  serving a real `runtime/apps` App with a real `obs.SSESink`; drives the
  inspector backend; asserts the relay streams real obs events and a non-loopback
  bind is refused. ≥1 failure mode (the non-loopback rejection).
- **Concurrency / golden:** the inspector relay fans the obs stream to multiple
  concurrent HTTP subscribers — a `-race` concurrency test. No golden output.

## Smoke script additions

- `internal/inspector` exists and builds.
- The inspector refuses a non-localhost bind (the typed rejection).
- The host-half bridge source exists and reuses the `web/bridge` `ui/` dialect.
- The `obs/v1` stream relay exists.
- The `web/inspector` frontend project exists and is in the `make web` set.
- `web/inspector` composes `@dockyard/ui` and does not re-implement a component.

## Coverage target

- `internal/inspector` — 80% (new-package band).
- `web/inspector` — 70% (matches the `web/ui` / `web/bridge` frontend band).

## Dependencies

- Phase 09 — `runtime/apps` (the MCP Apps extension the inspector renders).
- Phase 10a — `web/ui` (the shared design system the frontend composes).
- Phase 11 — `web/bridge` (the View half of the `ui/` bridge).
- Phase 16 — `runtime/obs` SSE sink (the stream the inspector consumes).

## Risks / open questions

- The `web/inspector` project is a Vite *application* (it is built to `dist/` and
  embedded), unlike `web/ui` / `web/bridge` which are libraries. Its build output
  is embedded by the Go backend; the embed target is allowed to be empty before a
  `vite build` (graceful skip), mirroring `runtime/apps`'s `Bundle.Validate`.
- RFC §18 — none directly; the Apps spec churn risk (brief 01 §4.4) is contained
  by reusing `web/bridge`'s `protocol.ts`.

## Glossary additions

- **inspector** — Dockyard's local test/debug surface (RFC §12).
- **host-half bridge** — the host side of the `ui/` postMessage dialect; the
  inspector implements it to render an MCP App locally.
- **inspector relay** — the read-only `internal/inspector` HTTP relay of the
  obs/v1 SSE stream and the JSON-RPC log to the inspector UI.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`

## Remediation history

### R1 — inspector production wiring (pre-Wave-9 depth audit)

A depth audit found that Phase 22 built the App-preview frame (`AppFrame` →
`HostBridge` → fixture switcher) but the shipping inspector had **no production
entry point for it**: `web/inspector/src/main.ts` mounted `App` with no props,
so `App.svelte`'s `appHtml` was always `''` and the frame always rendered "No
App attached"; there was no backend endpoint serving App HTML. RFC §12 line 711
("renders Apps locally") was therefore unmet in the product.

R1 closed this. The inspector backend gained a read-only `/api/apps` endpoint
(`internal/inspector/appsource.go`): `AppsFromServer` performs a read-only
`resources/list` + `resources/read` of the attached server's `ui://` resources
and returns the App HTML — a P4-honest path recorded in D-103, which extends
D-099. `App.svelte` now loads a real App from `/api/apps` and routes the
preview region through the four-state `PageState` (loading / empty / error /
ready). The `appHtml` prop is retained as a test-only override. A token audit
also repointed ~22 inspector `--dy-*` references to the real token names in
`web/ui/src/tokens.css` and removed their ad-hoc hex fallbacks (§20). No
behaviour from the original Phase 22 acceptance criteria changed; the
remediation only wired surface that was built but unreachable.

### R4 — `internal/inspector/dist/` is an embed anchor, not a placeholder bundle (depth-audit-2)

A second pre-Wave-9 depth audit found that the Phase 22 committed
`internal/inspector/dist/index.html` placeholder bundle — D-098's
"committed-placeholder bundle (`internal/inspector/dist/`) keeps the
`//go:embed` directive resolvable before any frontend build" — was still the
only thing the shipped binary embedded, because the Phase 23 packaging step
(D-098's "wiring the production `web/inspector` build into the binary is the
Phase 23 `dockyard inspect` packaging step") was never built. The real fix
belongs to Phase 23 and is recorded there; this Phase 22 entry only notes
that the placeholder approach D-098 settled is superseded.

R4 replaces the tracked placeholder `internal/inspector/dist/index.html` with
a tracked `.gitkeep` anchor and adds the dist tree to `.gitignore`, so the
`//go:embed all:dist` directive still resolves and the build is never dirtied
by a rebuild. When no real bundle has been staged the inspector backend
falls back to its in-Go `placeholderHTML` page (see
`internal/inspector/assets.go`) — the behaviour the Phase 22 placeholder
provided is preserved, just sourced from Go rather than from a tracked HTML
file. See Phase 23's R4 entry for the corresponding `make build` /
`inspector-bundle` packaging step.
