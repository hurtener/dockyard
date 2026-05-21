# Phase 10a — design-system

## Summary

Establishes Dockyard's shared frontend foundation before any page is built: the
design-token module (colour, spacing, typography, radius, elevation) and the
`web/ui/` shared Svelte component inventory. Every later Dockyard surface — the
inspector, the template App UIs, the docs site — composes this package rather than
re-implementing components, which is the lesson from Harbor's late, costly
design-system remediation (`AGENTS.md` §20).

## RFC anchor

<!-- Phase 10a is a tooling/UI-foundation phase; it builds the primitives the
     RFC-cited surfaces compose, rather than a runtime subsystem. -->

- RFC §7 — MCP Apps: the App UIs and the bridge shell consume the tokens and the
  shared inventory; tokens feed the host-themeable CSS variables an App receives via
  `hostContext.styles.variables`.
- RFC §10 — Templates: the V1 template App UIs (phases 24–26) compose `web/ui/`.
- RFC §12 — Inspector: the inspector (phases 22–23) is built entirely from this
  inventory; `docs/design/design-spec.md` §4 is its page spec, drawn against the
  approved mockup `docs/design/mockups/inspector.png`.

## Briefs informing this phase

- brief 04 — mcp-use DX teardown.
- brief 01 — MCP Apps.

## Brief findings incorporated

- Brief 04 (DX teardown) — a low cost-of-a-new-page is a developer-experience
  property: a shared, composable component inventory keeps that cost low and is the
  reason the design system is a day-one artifact, not a retrofit. Phase 10a delivers
  the inventory up front so no surface ever pays the duplication tax.
- Brief 01 (MCP Apps) §2.3 — an MCP App receives host-themeable CSS variables via
  `hostContext.styles.variables`. The token module is therefore shipped as CSS
  custom properties (`--dy-*`) so the same variable surface a component reads is the
  surface a host theme overrides — one source of visual truth, host-overridable.
- Brief 01 §2.4 — the View runs in a sandboxed iframe; components must be
  plain-Svelte (D-006), framework-agnostic, with no SvelteKit runtime dependency, so
  they drop into a bare iframe bundle unchanged.

## Findings I'm departing from (if any)

None.

## Goals

- Ship a design-token module: colour, spacing, typography, radius, and elevation as
  CSS custom properties plus a typed TypeScript export — the single source of visual
  truth, structured so a dark theme is a token-set swap.
- Ship the `web/ui/` shared Svelte component inventory specified in
  `docs/design/design-spec.md` §3 — shell/layout, data display, and the four-state
  `PageState` family — all typed, token-driven, accessible, plain-Svelte.
- Document the delivered inventory in `docs/design/CONVENTIONS.md` §3 and wire the
  `web/ui` project into the `make web` / `make web-install` frontend gate.

## Non-goals

- Building the inspector itself — that is Wave 8, phases 22–23.
- Building the template App UIs or their pattern blocks (e.g. `ApprovalPanel`) —
  those land with their template phases 24–26.
- The docs site (Phase 29) and the multi-server console (post-V1).
- A dark theme: V1 ships the light theme; the token structure must merely not
  preclude a dark swap.

## Acceptance criteria

- [ ] Every component in the `docs/design/design-spec.md` §3 inventory exists in
      `web/ui/src/` and is exported from the `src/index.ts` barrel.
- [ ] The design tokens are delivered as a module (CSS custom properties + typed TS
      export) and are the single source of visual truth — no component carries an
      ad-hoc hex or magic spacing number.
- [ ] `PageState` routes to exactly one of loading / empty / error / ready; the
      empty and error panels take real copy and a working retry/action affordance.
- [ ] The `web/ui` package type-checks and its component tests pass (`npm run gate`).
- [ ] `docs/design/CONVENTIONS.md` §3 documents the delivered inventory (one line
      per component), and the App-pattern bullet is reconciled to state that
      template-pattern blocks land with their phases.
- [ ] The logo (`docs/design/logo.png`) and the approved inspector mockup
      (`docs/design/mockups/inspector.png`) exist.
- [ ] `make web` / `make web-install` gate `web/ui` alongside `web/bridge`.

## Files added or changed

- `web/ui/` — new frontend project (adds no new top-level directory; `web/` exists):
  - `package.json`, `tsconfig.json`, `svelte.config.js`, `vitest.config.ts`,
    `.gitignore`, `README.md`
  - `src/tokens.css` — the `--dy-*` CSS custom properties (light theme)
  - `src/tokens.ts` — typed token export + token-name constants
  - `src/types.ts` — shared component prop types
  - `src/*.svelte` — the component inventory (shell/layout, data display, state)
  - `src/index.ts` — the public barrel
  - `src/__tests__/*.test.ts` — Vitest + `@testing-library/svelte` component tests
- `Makefile` — `web` / `web-install` extended to loop over `web/bridge` + `web/ui`.
- `docs/plans/phase-10a-design-system.md` — this plan.
- `docs/design/CONVENTIONS.md` — §3 inventory filled with the delivered set.
- `docs/decisions.md` — D-065, D-066, D-067.
- `docs/glossary.md` — design token, PageState, component inventory.
- `scripts/smoke/phase-10a.sh` — the smoke script.

## Public API surface

`web/ui` is a frontend (TypeScript/Svelte) package, not a Go surface. Its public
surface is the `@dockyard/ui` `src/index.ts` barrel:

- Token surface: `tokens` (typed token tree), `tokenVar(name)` (CSS-var accessor),
  and `tokens.css` as a side-effect import.
- Components: `AppShell`, `PageHeader`, `DetailRail`, `RailCard`, `ActionBar`,
  `ConnectionFooter`, `DataTable`, `Pagination`, `FilterBar`, `MetricCard`,
  `StatusChip`, `Timeline`, `JsonInspector`, `CodeBlock`, `PageState`,
  `LoadingState`, `EmptyState`, `ErrorState`, `PermissionState`.
- Types: `PageStateValue`, `Column`, `StatusTone`, `TimelineEvent`, and the
  per-component prop interfaces.

## Test plan

- **Unit:** Vitest + `@testing-library/svelte` component tests — rendering, prop
  wiring, the `PageState` four-way routing, and `ErrorState`/`EmptyState` action
  callbacks. Token module: the typed export matches the CSS custom-property names.
- **Integration:** N/A — Phase 10a depends on Phase 10 (a doc/convention dep, not a
  code seam) and opens no cross-subsystem code seam. The composition relationships
  (`DataTable` composes `Pagination` + `PageState`) are exercised within the unit
  tests of this single package.
- **Concurrency / golden:** N/A — no Go runtime artifact, no codegen output.

## Smoke script additions

`scripts/smoke/phase-10a.sh` asserts: the `web/ui` package and its config files
exist; the token module (`tokens.css` + `tokens.ts`) exists; every component file
in the `design-spec.md` §3 inventory exists; the `src/index.ts` barrel exists;
`CONVENTIONS.md` §3 is filled (no longer says "the planned set"); the logo and the
inspector mockup exist; the `Makefile` `web` target references `web/ui`. Where npm
is absent the build/type-check check skips rather than fails.

## Coverage target

- `web/ui` — 70% (tooling tier, `AGENTS.md` §11); component tests aim higher where
  cheap.

## Dependencies

- Phase 10 — the MCP Apps convention layer (a doc/convention dependency; Phase 10a
  builds the UI foundation those App UIs will compose).

## Risks / open questions

- The token palette is derived by eye from `docs/design/logo.png`; exact brand
  values may be tuned later. Mitigated: every value lives in one token module, so a
  brand correction is a single-file change (D-065).
- The inspector mockup is the only approved mockup in scope; template mockups are
  deferred to phases 24–26 (the template set may be reworked before Wave 9) — this
  is per `docs/design/design-spec.md` §5 and the master-plan Phase 10a block, not a
  deviation.

## Glossary additions

- **design token** — a named, single-source visual constant (colour, spacing,
  typography, radius, elevation) shipped as a CSS custom property + typed export.
- **PageState** — the shared four-state async wrapper (loading / empty / error /
  ready); empty and error are mandatory.
- **component inventory** — the shared `web/ui/` Svelte component set every Dockyard
  surface composes rather than re-implementing.

## Pre-merge checklist

- [x] `make drift-audit` passes
- [x] `make check-mirror` passes
- [x] `make preflight` passes
- [x] `go test -race ./...` and `golangci-lint run` clean
- [x] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [x] Coverage on touched packages ≥ stated target
- [x] New CLI command / manifest field / public API has a smoke check in this PR
- [x] Reusable-artifact change ⇒ concurrent-reuse test under `-race` (N/A — no Go
      reusable artifact; `web/ui` components are stateless render units)
- [x] Cross-subsystem seam opened/consumed ⇒ integration test (N/A — see Test plan)
- [x] New vocabulary added to `docs/glossary.md`
- [x] New / changed architectural decision filed in `docs/decisions.md`
