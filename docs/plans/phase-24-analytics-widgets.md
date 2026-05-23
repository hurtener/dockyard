# Phase 24 — Template system + the `analytics-widgets` template

<!--
The first phase of Wave 9 (Templates). Ships two things in one PR:
  1. The `dockyard new --template <name>` mechanism + the template-discovery seam.
  2. The `analytics-widgets` template — one MCP App that exposes three contract-
     first widget tools (chart / table / metric card), rendered inline by a
     single Svelte App that composes `web/ui/` (and the new shared `Sparkline`).

The master plan named this template `analytical-card`. The user has approved
renaming it to `analytics-widgets`; the rename is part of this PR (see Files
added or changed).
-->

## Summary

Phase 24 turns on `dockyard new --template <name>` and ships the V1 template
set's first entry. The template system is a small extension of the
`internal/scaffold` package — a discovery seam (`templates/<name>/` →
materialiser → renamed file tree) plus a new `--template` flag on the CLI;
`dockyard new` with no `--template` keeps Phase 17's first-class blank-scaffold
behaviour unchanged. The `analytics-widgets` template is one MCP App that
registers three contract-first widget tools (`create_chart`, `create_table`,
`create_metric_card`) and renders each inline in the host's chat surface via a
single Svelte App. The App composes the shared `web/ui/` inventory (plus a new
`Sparkline` that lands in `web/ui/` this PR), wraps Apache ECharts behind a
template-local `ChartFrame`, and honours the host theme automatically through
`hostContext.styles.variables` with an explicit per-tool `theme?` override.

## RFC anchor

- RFC §10 — Templates (the `--template` mechanism; templates are optional
  showcases; the V1 template set exercises the framework's capabilities).
- RFC §7 — MCP Apps (`hostContext.styles.variables` for host theming; the
  bridge shell is the postMessage View half; CSP defaults; display-mode
  negotiation).
- RFC §14 — Packaging (the App's UI is embedded into the binary via
  `//go:embed all:dist`).
- RFC §6 — Contract-first model (the three tool contracts are typed Go
  structs; their JSON Schema and TypeScript are generated, never hand-written
  — P1).
- RFC §9 — CLI & developer experience (the `dockyard new --template` verb).

## Briefs informing this phase

- brief 04 — the mcp-use DX teardown (the source of the V1 template set; the
  "templates are workflows, never transports" framing; the "fixtures + states by
  default" contract).
- brief 01 — the MCP Apps extension audit (display modes, the `_meta.ui`
  shape, single-file bundles, deny-by-default CSP).

## Brief findings incorporated

- **brief 04 §2.4 — "templates exercise the framework, not the product".** The
  V1 template set is a *showcase* that demonstrates the manifest, codegen,
  bridge, inspector, and obs surfaces in one runnable project. The
  `analytics-widgets` template is deliberately one App with three small,
  realistic tools (a chart, a table, a metric card) — enough to exercise every
  surface, not a real analytics product.
- **brief 04 §2.3 — "templates are named for workflows, not transports".** The
  template's name is `analytics-widgets` (the *what*), never `chart-app` or
  `inline-widgets` (the *how*). The same template materialises whether the
  developer later serves stdio or http.
- **brief 04 §2.2 — "scaffolded fixtures + states by default".** The template
  ships six fixtures (`happy`, `empty`, `error`, `permission`, `slow`, `large`)
  for each of its three tools — eighteen total. Each maps to a distinct UI
  state the inspector's fixture switcher (Phase 23) drives. The four-state
  `PageState` (`web/ui/` §3) is mandatory on every rendered widget.
- **brief 01 §2.5 — "single-file bundles are the default; opt-out is explicit
  in the manifest".** The template's `dockyard.app.yaml` declares
  `runtime.ui.bundle = single-file` and an empty `csp:` block — the
  deny-by-default CSP just works because the App ships zero external origins.
  Apache ECharts is bundled into the App's single-file output.
- **brief 01 §2.7 — "display-mode negotiation, never a static matrix".** The
  template declares `display_modes: [inline]` only; the bridge shell only ever
  grants inline. A host that does not advertise the Apps extension still sees
  the three tools work as plain MCP tools — the App is additive (RFC §7.1).

## Findings I'm departing from (if any)

- **Departure from CLAUDE.md §20's spec → mockup → build rule for templates.**
  §20 mandates an approved static mockup before any UI implementation. For a
  *template phase* the verification surface is different: a template is a
  generated showcase, and its visual quality is verifiable through the
  inspector's live preview + the rendered widget in any MCP host, not through
  a static `.png`. The page spec lives in this plan; the inspector + a host
  preview is the live "mockup". The carve-out is filed and scoped to templates
  only as **D-123**. The inspector and the docs site still require a mockup;
  the per-template scope of D-123 is explicit.

## Goals

- The `dockyard new --template <name>` verb materialises a named template from
  `templates/<name>/` into a working project; the no-template path is
  unchanged (Phase 17 first-class).
- A template-discovery seam exists: adding a future template (Phases 25, 26
  and the post-V1 set in RFC §19) is one new `templates/<name>/` directory
  plus one registration call; nothing about `analytics-widgets` is hardcoded
  into the seam.
- The `analytics-widgets` template ships: one App, three contract-first
  widget tools (`create_chart`, `create_table`, `create_metric_card`), a
  single dispatching Svelte App, eighteen fixtures (six per tool), inline-only
  display, automatic host-theme propagation with an explicit per-call
  override, and a README explaining how a developer swaps the synthetic data
  for a real source.
- A new shared `Sparkline` component lands in `web/ui/` and is documented in
  `docs/design/CONVENTIONS.md` §3 alongside the other primitives.
- `analytical-card` is renamed to `analytics-widgets` across the repo (the
  research brief 04 historical mention is preserved with an editor's note).

## Non-goals

- The `approval-flow` and `inspector` templates (Phases 25 and 26).
- Fullscreen or pip display modes for the analytics-widgets App (manifest
  declares `inline` only; future templates may declare others).
- A theme registry, a skin system, or a plugin pattern for widget renderers —
  the entire theming story is host-variable propagation + a per-call override.
- A real analytics product, real customer data, or real charts beyond the
  ECharts default rendering set (V1 covers `bar | line | area | pie | scatter
  | radar`; ECharts itself supports more — adding a renderer is a future
  enhancement).
- Wrapping ECharts in a shared `web/ui/` component (ChartFrame stays
  template-local — CLAUDE.md §20: wrappers around third-party fat libs are
  not shared inventory).

## Acceptance criteria

- [ ] `dockyard new --template analytics-widgets <name>` materialises a
      working project under the named directory; the project builds CGo-free
      under `go build ./...` and ships passing contract tests under
      `go test ./...`.
- [ ] `dockyard new <name>` with no `--template` still produces Phase 17's
      blank scaffold (unchanged); the help text lists `--template`.
- [ ] The template-discovery seam is keyed on template name and refuses an
      unknown name with a typed error; the seam is exercised by a unit test
      that registers a stub template, not by referencing
      `analytics-widgets` by name.
- [ ] The `analytics-widgets` project's `dockyard.app.yaml` declares three
      tools (`create_chart`, `create_table`, `create_metric_card`), one app
      with `display_modes: [inline]`, single-file bundle, empty CSP, the
      quality gates on (incl. all four UI-state gates).
- [ ] Each of the three tools registers via the runtime `tool.New[...]`
      builder over typed Go contract structs in
      `templates/analytics-widgets/internal/contracts/`; their generated
      JSON Schema + TypeScript carry the `Code generated … DO NOT EDIT.`
      header (P1).
- [ ] The materialised project's Svelte App reads `structuredContent.kind`
      and dispatches to a `chart` / `table` / `metric_card` renderer; the
      renderers compose `web/ui/` primitives (`MetricCard`, `DataTable`,
      `StatusChip`, `PageState`, and the new `Sparkline`); no `web/ui`
      component is re-implemented.
- [ ] A new shared component `Sparkline` exists in `web/ui/src/` (pure SVG,
      token-driven), is exported from `web/ui/src/index.ts`, is covered by a
      vitest test that meets the per-package coverage threshold, and is
      documented in `docs/design/CONVENTIONS.md` §3.
- [ ] The materialised project's handlers return realistic synthetic data
      (a customer-health metric, a revenue-by-month chart, a top-accounts
      table) so a developer sees something real on first run; the README
      documents how to swap to a real source.
- [ ] Theming: a `hostContext.styles.variables` carrying a dark theme
      propagates into the rendered widget without any developer change; each
      tool's input accepts `theme?: "light" | "dark" | "auto"` (default
      `auto` = honour host) as a per-call override.
- [ ] Six fixtures per tool — `happy`, `empty`, `error`, `permission`,
      `slow`, `large` — are wired to the generated contracts (consumed by
      the inspector's Phase 23 fixture switcher) and each drives a distinct
      UI state.
- [ ] `analytical-card` no longer appears in source under `internal/`,
      `cmd/`, `RFC-001-Dockyard.md`, `docs/plans/`, `docs/design/`,
      `docs/glossary.md`, or `scripts/`; the only retained reference is in
      `docs/research/04-mcp-use-dx-teardown.md` (historical brief content)
      with an editor's note.
- [ ] `scripts/smoke/phase-24.sh` reports `OK ≥ count(criteria)` and
      `FAIL = 0`; every prior smoke script still passes.
- [ ] An integration test under `test/integration/` materialises the
      `analytics-widgets` template, builds the resulting project, drives
      each of the three tools end to end against a real `runtime/server`,
      and asserts the structuredContent shape every fixture produces. The
      `web/inspector` vitest harness is extended with a `MessageChannel`
      handshake test that proves the App's dispatcher selects the right
      renderer for each `kind` and that a dark host-theme propagates into
      the widget root.

## Files added or changed

```text
docs/
  plans/
    phase-24-analytics-widgets.md                   # this file
    README.md                                       # rename row, rewrite Phase 24 detail block
  decisions.md                                      # D-123..D-125 appended
  glossary.md                                       # 'analytics-widgets', 'Sparkline', 'ChartFrame', 'template-discovery seam'
  design/
    CONVENTIONS.md                                  # rename + Sparkline in §3
    design-spec.md                                  # rename references
internal/
  cli/new.go                                        # add --template flag; route to scaffold.GenerateFromTemplate
  scaffold/
    doc.go                                          # extend doc — the template system, not just the blank scaffold
    template.go                                     # template-discovery seam: Registry, builtin registration, materialiser
    template_test.go                                # unit tests for the seam (stub template, errors, substitutions)
    template_golden_test.go                         # golden test for analytics-widgets materialisation
    testdata/
      analytics-widgets.golden/                     # expected materialised tree
templates/
  analytics-widgets/
    dockyard.app.yaml                               # manifest — 3 tools, 1 inline app, single-file bundle, all 4 UI-state gates
    go.mod.tmpl                                     # go.mod template — module path + dockyard replace substituted in
    cmd/server/main.go.tmpl                         # entrypoint template: server + 3 tool registrations
    pkg/contracts/contracts.go                      # 3 typed input/output contract pairs (P1). Real .go for in-tree
                                                    #   compile/test; PathRemap rewrites pkg/ → internal/ on materialise
                                                    #   so the user gets RFC §4.3's canonical internal/contracts/ layout.
    pkg/handlers/handlers.go                        # the 3 handlers (synthetic but realistic data); ditto PathRemap.
    pkg/handlers/handlers_test.go                   # contract tests for the handlers.
    builtin.go                                      # //go:embed snapshot + init() RegisterTemplate (the only top-level .go)
    web/
      package.json
      vite.config.ts
      tsconfig.json
      svelte.config.js
      src/
        App.svelte                                  # dispatcher: kind → widget renderer
        widgets/Chart.svelte
        widgets/Table.svelte
        widgets/MetricCardWidget.svelte
        widgets/ChartFrame.svelte                   # template-local ECharts wrapper
        theme.ts                                    # host-theme propagation + per-call override
        main.ts
    fixtures/
      create_chart/{happy,empty,error,permission,slow,large}.json
      create_table/{happy,empty,error,permission,slow,large}.json
      create_metric_card/{happy,empty,error,permission,slow,large}.json
    README.md
web/
  ui/
    src/
      Sparkline.svelte                              # new shared component
      index.ts                                      # export Sparkline
    src/__tests__/Sparkline.test.ts                 # vitest coverage of the Sparkline
scripts/smoke/
  phase-24.sh                                       # one assertion per acceptance criterion
test/integration/
  phase24_analytics_widgets_test.go                 # end-to-end materialise + build + tools/call + fixture-shape integration
RFC-001-Dockyard.md                                 # §10 rename only (decision is the template's name, the design is unchanged)
```

## Public API surface

- `internal/scaffold.GenerateFromTemplate(opts Options, templateName string)` —
  materialises a registered template. Errors: `ErrInvalidName`,
  `ErrTargetExists`, `ErrUnknownTemplate`.
- `internal/scaffold.RegisterTemplate(name string, t Template)` /
  `LookupTemplate(name) (Template, bool)` — the template-discovery seam.
  Templates register at `init()` time; consumers look up by name.
- `Template` is an interface with one method:
  `Materialise(opts Options) (map[string][]byte, error)`. The built-in
  `analytics-widgets` template registers from a `//go:embed`ed snapshot of
  `templates/analytics-widgets/`.
- `web/ui` exports a new `Sparkline` component:
  `Sparkline({ values: number[], width?: number, height?: number,
  tone?: 'neutral' | 'ok' | 'warn' | 'error', ariaLabel?: string })`.

## Test plan

- **Unit (`internal/scaffold`):** the template-discovery seam — register a
  stub `Template`, look it up, materialise it; reject `ErrUnknownTemplate`
  on an unregistered name; substitutions (project name, module path) flow
  through; deterministic output (the same options → identical bytes).
- **Unit (`internal/cli`):** the `--template` flag is wired (cobra),
  conflict-free with `--module` and `--dir`, and a typed error from the
  scaffold layer maps to a clean CLI message.
- **Golden (`internal/scaffold`):** the `analytics-widgets` materialisation
  produces the expected tree byte-for-byte (the file list and per-file
  contents under `testdata/analytics-widgets.golden/`).
- **Web unit (`web/ui`):** `Sparkline` renders an `<svg>` with the right
  point count, normalises a flat series without dividing by zero, applies
  the `tone` token, and exposes its `ariaLabel` on the SVG root. Coverage
  meets the per-package threshold.
- **Web unit (`templates/analytics-widgets/web`):** the App's dispatcher
  selects the right widget for each `kind`; the `theme.ts` helper resolves
  `auto` against `hostContext.styles.variables`; ChartFrame initialises and
  disposes ECharts on mount/unmount. (Run as part of the project's local
  `npm test` — wired into `make web` when the template directory carries
  its own `package.json`.)
- **Integration (`test/integration/phase24_analytics_widgets_test.go`):**
  materialise the template against the real Dockyard checkout (the
  `DockyardReplace` workflow), `go mod tidy`, `go build`, run the
  contract tests, then spin up an in-process server registering the three
  real handlers and drive each tool with a real SDK client. Assert the
  `structuredContent` shape matches each fixture's expected shape (the
  Phase 23 fixture switcher consumes the same schemas, so a passing
  fixture proves the inspector wiring will hold).
- **Web integration (`web/inspector`):** extend the existing host-bridge
  vitest harness with a `MessageChannel` end-to-end run that loads the
  analytics-widgets App's dispatcher, posts a `tool-result` for each
  `kind` plus a dark `hostContext.styles.variables`, and asserts the
  rendered DOM picks the right renderer and applies the dark theme.
- **Concurrency:** `-race` is on for every Go test in this phase; the
  template-discovery seam's `Registry` is exercised concurrently in a
  `t.Parallel()` test that registers and looks up from N goroutines.

## Smoke script additions

`scripts/smoke/phase-24.sh` (one assertion per acceptance criterion):

- `dockyard new --template analytics-widgets ...` produces a project.
- The resulting project builds CGo-free (`go build ./...`).
- The resulting project's contract tests pass (`go test ./...`).
- The manifest declares exactly three tools and exactly one app with
  `display_modes: [inline]` only.
- `templates/analytics-widgets/web/src/App.svelte` exists; the
  ChartFrame is template-local; no `Sparkline.svelte` lives under the
  template (it must be sourced from `web/ui`).
- `web/ui/src/Sparkline.svelte` exists and is exported from
  `web/ui/src/index.ts`.
- `docs/design/CONVENTIONS.md` §3 lists `Sparkline`.
- A `grep` confirms `analytical-card` is absent from `internal/`, `cmd/`,
  `docs/plans/`, `docs/design/`, `docs/glossary.md`, `scripts/`, and the
  RFC. The brief 04 historical mention is the only allowed match.
- `dockyard new` (no `--template`) still works (calls back through Phase 17).

A check against an unbuilt surface skips, never fails (per
`scripts/smoke/common.sh`).

## Coverage target

- `internal/scaffold` — 80% on the new template seam (the CLI/tooling band
  is 70%, but the seam is the closed surface other phases will register
  against and earns the standard 80%).
- `templates/analytics-widgets/internal/handlers` — 75% (template handlers
  are demonstration code with synthetic data; the contract surface is
  exercised by the Phase 24 integration test).
- `web/ui` Sparkline — meets the `web/ui` per-package threshold the
  Phase 21.5 gate enforces.

## Dependencies

- Phase 19 (`dockyard dev` — the developer's local loop the template
  runs under).
- Phase 20 (`dockyard build` / `run` / `install` — the materialised
  project's build pipeline + the `//go:embed` step).
- Phase 10a (the shared `web/ui/` inventory + `docs/design/CONVENTIONS.md`
  — the design system the App composes; the new `Sparkline` extends it).

## Risks / open questions

- **Apache ECharts bundle size.** A single-file App with ECharts bundled is
  larger than a hand-rolled SVG. Acceptable for V1 — ECharts is the
  industry-standard, broadest chart-type coverage, and the
  `web/inspector`/`web/bridge` projects already accept larger bundle sizes
  for capability. Future templates may opt out by not using ECharts; the
  contract is renderer-agnostic.
- **Template files at `templates/analytics-widgets/web/` are not built by
  the framework's `make web` gate** — they are a *template source* for a
  scaffolded project, and their own `make web` runs inside the generated
  project. The smoke script asserts the source tree is present, the
  integration test asserts the materialised project's tree resolves; the
  built artifact is built by the developer's `dockyard build` once the
  project is materialised.
- **The rename is wide.** Brief 04 is research, not a settled-design source
  — we keep its historical mention and add an editor's note rather than
  rewriting research findings (CLAUDE.md §16 — research is *context*, not
  *design*).

## Glossary additions

- `analytics-widgets` — the V1 template name (replaces `analytical-card`
  in the master plan and RFC §10).
- `Sparkline` — the small, token-driven, pure-SVG chart in `web/ui`.
- `ChartFrame` — the template-local Svelte wrapper around Apache ECharts;
  lives in `templates/analytics-widgets/web/src/widgets/` because
  wrappers around third-party fat libraries do not belong in the shared
  `web/ui` inventory (CLAUDE.md §20).
- **Template-discovery seam** — the `internal/scaffold.Registry` +
  `Template` interface that lets a future template directory register
  itself with the CLI without modifying the CLI.

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
- [ ] D-123 (the §20 deviation for templates) filed in `docs/decisions.md`
- [ ] UI touched ⇒ composes `web/ui`; new shared component (`Sparkline`)
      landed in `web/ui/` + `CONVENTIONS.md`; every page has loading /
      empty / error / ready states (the Svelte App routes every widget
      through `PageState`)

## Phase 24 finish — follow-up (operator invoke + every rail tab verified end-to-end)

PR #44 merged the analytics-widgets template and proved three widgets render
through the inspector's Fixtures switcher. PR #45 closed three real gaps the user
spotted before V1:

1. **Inspector logo (Gap 1).** `web/inspector/src/App.svelte` now passes the
   canonical Dockyard wordmark (`docs/design/logo.png` → imported through Vite's
   asset pipeline as `web/inspector/src/assets/dockyard-logo.png`) to
   `PageHeader.lead`. Sized 32 px tall through a token-scoped class; no
   regression in the `data-testid="page-header"` selector. The `@dockyard/ui`
   `PageHeader.test` already covered the `lead` slot, so no new shared-component
   test was needed.
2. **Operator-initiated `tools/call` (Gap 2; D-131).** The inspector backend
   gained `POST /api/tools/invoke` (`internal/inspector/invoke.go`,
   `assets.go`). `internal/cli/inspect.go` wires
   `inspector.ToolsFromServer(cfg.serverURL)` as the `Invoker`. The frontend's
   `ToolsPanel.svelte` generates a parameter form from each tool's input JSON
   Schema (`web/inspector/src/lib/schema-form.ts`), POSTs the typed JSON, and
   threads the structured result through the same `pushToolResult` path the
   Fixtures switcher uses (D-129) so the App preview re-renders with the
   operator's parameters. Unit + integration tests live in
   `internal/inspector/invoke_test.go` (a real `runtime/server` tool exercised
   end-to-end) and `web/inspector/src/__tests__/{schema-form,invoke}.test.ts`.
3. **Every rail tab verified end-to-end (Gap 3).** The eight tabs (Events, RPC,
   Fixtures, Tools, Verdicts, Tasks, Analytics, plus the new-with-logo header)
   were driven through the real analytics-widgets demo via Playwright; the
   screenshots live in `docs/screenshots/phase-24-finish/{events, rpc, tools-invoke,
   verdicts, tasks, analytics, fixtures-with-logo}.png`. Two real defects
   surfaced and were fixed in the same pass:
   - **D-132 — the template did not expose obs/v1.** The analytics-widgets
     template's `serveHTTP` mounted only the MCP handler — the inspector's
     relay received no events. Fixed by instantiating `obs.NewSSESink("")`,
     passing it as `server.Options.Obs`, and mounting `/obs/v1/stream` on the
     same listener as the MCP transport.
   - **D-133 — `AppFrame.sendToolResult` looped.** After an operator-initiated
     invoke, the iframe's response cycled `frameStatus` between handshaking and
     ready, the `pushToolResult` effect re-fired indefinitely, and Svelte hit
     `effect_update_depth_exceeded` — every interactive control froze. Fixed by
     guarding the effect with a `lastSentPayload` closure variable that
     short-circuits a re-send of an identical payload.

The Phase 24 smoke (`scripts/smoke/phase-24.sh`) gains two new assertions: the
`POST /api/tools/invoke` endpoint exists, and the inspector frontend bundle
embeds the Dockyard logo asset. `make preflight` is green: OK=243 / FAIL=0.

