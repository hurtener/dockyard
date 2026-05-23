# Phase 25 — `approval-flows` template

<!--
The second phase of Wave 9 (Templates). Ships:
  1. The `approval-flows` builtin template — the first real product-path
     driver of MCP Tasks × Apps (RFC §8.6). Two contract-first
     task-augmented tools (`request_approval` + `propose_with_edits`)
     and one App that renders the human-in-the-loop card / form, drives
     the `input_required` round-trip from inside the iframe, and
     completes the task with the user's decision.
  2. The framework pieces this template is the first to need:
     - the bridge **elicitation-response** message (View → host → server
       via `tasks/result`), so the App can answer an `input_required`
       prompt without leaving the iframe (D-134);
     - the **scaffold wiring** that attaches a `tasks.Engine` in the
       materialised `main.go` when a template declares task-supporting
       tools — the R2 follow-up D-108 named (D-135);
     - the shared `FieldDiff` web/ui component the form renderer
       composes (the editable current → proposed pair);
     - the inspector full-viewport layout fix (the cosmetic follow-up
       Phase-24-finish surfaced — the inspector body grew past 100vh
       because each region grew with its content).

The master plan named this template `approval-flow` (singular). It is
renamed to `approval-flows` (plural) in this PR — same precedent as
Phase 24's `analytical-card → analytics-widgets` rename.
-->

## Summary

Phase 25 turns the Tasks × Apps composition into a working, scaffolded
product path. The `approval-flows` template ships two contract-first
task-augmented tools — `request_approval` (a generic approve/reject
card) and `propose_with_edits` (a structured form with user-editable
proposed values) — and one Svelte App that renders the human-in-the-
loop card / form, drives the `input_required` round-trip from inside
the iframe, and completes the task with the user's decision. To make
this product path real Phase 25 closes three concrete seams: the
bridge's View→host elicitation-response message so an App can answer
an `input_required` prompt (D-134); the scaffold wiring that attaches
a `tasks.Engine` in the materialised `main.go` when the template
declares task-supporting tools (D-135 — the R2 follow-up D-108
named); and a new shared `FieldDiff` web/ui component the form
renderer composes. The inspector's full-viewport layout fix lands
in the same PR.

## RFC anchor

- RFC §10 — Templates (the V1 template set; `approval-flows` is the
  Tasks×Apps showcase).
- RFC §8 — MCP Tasks: §8.3 lifecycle/methods; §8.4 the `TaskHandle`
  handler API + `input_required` elicitation; §8.5 TaskStore +
  security + auth-context binding; §8.6 Tasks × Apps — the canonical
  pattern this template makes real.
- RFC §7 — MCP Apps (`hostContext`, bridge shell, CSP defaults,
  display-mode negotiation).
- RFC §12 — Inspector (the App preview + the Tasks panel that
  renders the lifecycle as a Timeline).
- RFC §14 — Packaging (the App's UI is embedded into the binary via
  `//go:embed all:dist`).
- RFC §6 — Contract-first (both tool contracts are typed Go structs;
  schemas + TypeScript are generated — P1).
- RFC §9 — CLI / DX (the `dockyard new --template approval-flows` verb;
  the scaffolded `main.go` is real, runnable code).

## Briefs informing this phase

- brief 02 — the MCP Tasks extension teardown (the lifecycle, the
  `input_required` elicitation contract, the auth-context binding, the
  resource-exhaustion controls; §4.5 + §4.7).
- brief 01 — the MCP Apps audit (the View→host postMessage dialect, the
  single-file bundle + deny-by-default CSP, the display-mode
  negotiation).
- brief 04 — the mcp-use DX teardown (the V1 template set; "templates
  are workflows, never transports"; the six-fixture default).

## Brief findings incorporated

- **brief 02 §3.2 — "the App is the surface, Tasks is the protocol".**
  The Tasks×Apps pattern is the App rendering a turn-by-turn
  interaction backed by a task that pauses at `input_required` and
  resumes with the requestor's reply. Phase 25 makes this concrete: the
  scaffolded App calls a task-augmented tool, observes the
  `input_required` round-trip, and answers the prompt by sending the
  user's decision through the bridge.
- **brief 02 §4.7 — "cancellation is cooperative".** The template's
  handlers observe `ctx.Done()` and `TaskHandle.Cancelled()`; a
  cancellation surfaces to the App as the task moving to `cancelled`
  in the Tasks panel and the App rendering a "decision withdrawn"
  empty state.
- **brief 02 §4.5 — "withhold tasks/list when requestors are
  unidentifiable".** The template's scaffolded `main.go` opts
  `RequestorIdentifiable=false` on stdio (the single-user default),
  so `tasks/list` is not advertised; on HTTP the template documents
  the bearer-token shape the developer plugs into `WithTasks`.
- **brief 01 §2.4 — "the View → host dialect is JSON-RPC, single
  source of truth in `protocol.ts`".** Phase 25 adds the
  `ui/elicitation-response` notification (D-134) into `protocol.ts`
  rather than inventing a new transport. The host-half delivers it
  into the attached server's task via the inspector's loopback
  surface; the bridge ships a typed View helper.
- **brief 01 §2.5 — "single-file bundle + empty CSP".** The
  `approval-flows` App ships a single-file bundle with an empty
  `csp:` block — the deny-by-default CSP just works, exactly as the
  analytics-widgets template demonstrated in Phase 24.
- **brief 04 §2.2 — "scaffolded fixtures + states by default".** The
  template ships six fixtures (`happy`, `empty`, `error`,
  `permission`, `slow`, `large`) per tool — twelve total — each
  driving a distinct UI state in the inspector's Fixtures switcher.

## Findings I'm departing from (if any)

- **None — but two non-departures worth naming for the reviewer.**
  (a) The §20 spec → mockup → build rule for templates is already
  carved out at D-123: the template's live preview in the inspector +
  the rendered App in any MCP host is the visual mockup for a
  template. Phase 25 inherits D-123; no new departure. (b) The
  template ships an empty CSP and a single-file bundle — the same
  posture brief 01 §2.5 calls for; the Phase 24 precedent
  established it for analytics-widgets and Phase 25 follows it
  verbatim.

## Goals

- The `approval-flows` template materialises a working project with
  two task-augmented tools (`request_approval`, `propose_with_edits`)
  and one App that drives the human-in-the-loop round-trip end to end.
- The scaffolded `main.go` attaches a real `tasks.Engine` — the
  scaffold detects that the template declares a tool with
  `task_support` ∈ {optional, required} and emits the engine
  construction + the `server.Options.Tasks` attachment (D-135). This
  generalises beyond approval-flows: any future template (or
  no-template scaffold) that declares task-supporting tools gets the
  same wiring.
- The bridge ships an elicitation-response notification
  (`ui/notifications/elicitation-response`) the App posts when the
  user decides (D-134). The inspector's host-half delivers the
  response to the attached server's task via a new loopback endpoint
  on the inspector backend.
- A new shared `FieldDiff` web/ui component lands in `web/ui/` and
  is documented in `docs/design/CONVENTIONS.md` §3. The
  `propose_with_edits` App composes it.
- The inspector's layout fills 100 vh with each region a fixed-
  height container and `overflow: auto` internally — the cosmetic
  follow-up surfaced in Phase-24-finish.
- `approval-flow` is renamed to `approval-flows` across the repo
  (the research briefs keep the historical reference with an editor
  note — research is *context*, not *design*).

## Non-goals

- The `inspector` template (Phase 26).
- A theme / skin system for the approval card or the form — the
  `propose_with_edits` form follows the same token-driven posture
  every web/ui component does; no per-template skinning surface.
- An identity / auth UI for HTTP — the scaffolded `main.go`
  documents the bearer-token shape but does not ship an identity
  provider integration.
- A Tasks-cancellation control in the App itself — the inspector's
  Tasks panel surfaces `cancelled`; cancellation in V1 is operator-
  driven through the inspector, not user-driven from the App's UI
  (a future enhancement, not Phase 25).
- A custom field type system beyond the five V1 types
  (`string`, `number`, `boolean`, `enum`, `text`) — the contract is
  fixed at V1; adding a `date` or a `multiselect` is a contract
  extension, not a Phase 25 feature.

## Acceptance criteria

- [ ] `dockyard new --template approval-flows <name>` materialises a
      working project under the named directory; the project builds
      CGo-free under `go build ./...` and ships passing contract tests
      under `go test ./...`.
- [ ] The materialised project's `dockyard.app.yaml` declares two
      tools (`request_approval`, `propose_with_edits`), one app with
      `display_modes: [inline]`, a single-file bundle, an empty CSP,
      and the four UI-state gates on. Both tools declare
      `task_support: required` (the tools always run as tasks — the
      `input_required` round-trip is the product, not an optional
      capability).
- [ ] The scaffolded `cmd/server/main.go` constructs a real
      `tasks.Engine` over an in-memory `TaskStore`, attaches it via
      `server.Options{Tasks: engine}`, and starts the engine's purge
      sweep (D-135). This wiring is generic — a future template (or
      the no-template scaffold) that declares a task-supporting tool
      gets the same generated code.
- [ ] Both tools register via `tool.New[...].Handler(handlers.Xxx).Register(srv)`
      against typed Go contracts in `templates/approval-flows/pkg/contracts/`;
      handlers are `HandleFunc`s that call `TaskHandle.RequireInput` to
      drive the `input_required` elicitation; the contracts'
      generated JSON Schema + TypeScript carry the
      `Code generated … DO NOT EDIT.` header (P1).
- [ ] The bridge exposes a typed View helper
      `bridge.sendElicitationResponse(taskId, payload)` that posts a
      `ui/notifications/elicitation-response` notification. The
      protocol message lives in `web/bridge/src/protocol.ts` and is
      the single source of truth (D-134).
- [ ] The inspector's host-half delivers the elicitation-response
      to the attached server's task: a new loopback endpoint
      `POST /api/tasks/elicitation` on the inspector backend opens
      a short-lived MCP client session, calls `tasks/result` with
      the elicited payload, and answers the View's notification.
      Localhost-only via the existing `requireLoopback` gate
      (P4 — symmetric to D-131's operator-initiated `tools/call`).
- [ ] A new shared component `FieldDiff` exists in `web/ui/src/`
      (Svelte 5, token-driven, accessible — `aria-labelledby` links the
      label to the editable input, the diff hint is screen-reader-
      announced), is exported from `web/ui/src/index.ts`, is covered
      by a vitest test that meets the per-package coverage threshold,
      and is documented in `docs/design/CONVENTIONS.md` §3.
- [ ] The `propose_with_edits` App composes `FieldDiff` for every
      field; the `request_approval` App composes web/ui primitives
      (`PageState`, `StatusChip`, `EmptyState`, …) — neither App
      re-implements a web/ui component (CLAUDE.md §20).
- [ ] Twelve fixtures (six per tool — `happy`, `empty`, `error`,
      `permission`, `slow`, `large`) drive distinct UI states in the
      inspector's Fixtures switcher.
- [ ] The inspector's Tasks panel renders the live
      `input_required → completed` lifecycle for a real approval (the
      panel was empty in Phase 24-finish — Phase 25 makes it real).
- [ ] The inspector's layout fills 100 vh — each region (App
      preview, rail panels) is a fixed-height container with
      `overflow: auto` internally; the inspector body does not grow
      past the viewport. Captured as a before/after screenshot.
- [ ] `approval-flow` (singular) does not appear in
      `RFC-001-Dockyard.md`, `internal/`, `cmd/`, `docs/plans/`,
      `docs/design/`, `docs/glossary.md`, or `scripts/`. The only
      retained references are in `docs/research/02-mcp-tasks-extension.md`
      and `docs/research/04-mcp-use-dx-teardown.md` (historical brief
      content) with editor's notes.
- [ ] `scripts/smoke/phase-25.sh` reports `OK ≥ count(criteria)` and
      `FAIL = 0`; every prior smoke script still passes.
- [ ] An integration test under `test/integration/` materialises the
      `approval-flows` template, exercises both tools' `input_required`
      lifecycle end to end against a real `runtime/server` + real
      `tasks.Engine` (no mocks at the seam), and asserts the terminal
      task carries the approve / reject / edits payload. Bridge
      protocol round-trip covered by a Vitest test
      (`web/bridge/src/__tests__/elicitation-response.test.ts`) that
      drives the View helper through a `MessageChannel` and asserts
      the host-half receives the typed notification.
- [ ] Screenshots in `docs/screenshots/phase-25/` prove the end-to-
      end demo: `request-approval.png`, `propose-with-edits.png`,
      `tasks-panel-live.png`, `layout-fullvh.png`.

## Files added or changed

```text
docs/
  plans/
    phase-25-approval-flows.md            # this file
    README.md                             # rename row 25; rewrite Phase 25 detail
  decisions.md                            # D-134, D-135 appended; further D-NNN
                                          # only on genuinely architectural calls
  glossary.md                             # 'approval-flows', 'FieldDiff',
                                          # 'elicitation-response', 'scaffold-tasks-engine'
  design/
    CONVENTIONS.md                        # rename + FieldDiff in §3
    design-spec.md                        # rename references
  research/
    02-mcp-tasks-extension.md             # editor's note: V1 ships approval-flows
    04-mcp-use-dx-teardown.md             # editor's note: V1 ships approval-flows
  screenshots/phase-25/                   # request-approval / propose-with-edits /
                                          # tasks-panel-live / layout-fullvh
internal/
  cli/new.go                              # help text rename
  scaffold/
    doc.go                                # doc rename + the tasks-engine wiring
    template.go                           # (no change unless wiring needs a hook)
  inspector/
    elicitation.go                        # POST /api/tasks/elicitation; talks
                                          # to the attached server's tasks/result
    elicitation_test.go                   # unit + integration
    assets.go                             # wire the new route
    inspector.go                          # Options.Elicitor (the new seam)
runtime/
  tasks/
    (unchanged — Phase 25 only consumes)
  server/
    (unchanged — the Options.Tasks seam already exists, D-108)
templates/
  approval-flows/
    builtin.go                            # //go:embed snapshot + init()
                                          # RegisterTemplate
    dockyard.app.yaml                     # 2 tools, 1 inline app, single-file
                                          # bundle, 4 UI-state gates
    go.mod.tmpl
    cmd/server/main.go.tmpl               # constructs tasks.Engine + Options.Tasks
                                          # (D-135 — the template author writes
                                          # the wiring; future no-template
                                          # scaffold for task-supporting tools
                                          # generalises this)
    pkg/contracts/contracts.go            # 2 typed contract pairs (P1)
    pkg/handlers/handlers.go              # HandleFunc handlers that call
                                          # TaskHandle.RequireInput
    pkg/handlers/handlers_test.go         # unit tests over the contract surface
    web/                                  # the App
      package.json
      vite.config.ts
      tsconfig.json
      svelte.config.js
      src/
        App.svelte                        # dispatcher: kind → approval card /
                                          # edits form
        ApprovalCard.svelte               # the request_approval renderer
        EditsForm.svelte                  # the propose_with_edits renderer
                                          # (composes web/ui FieldDiff)
        main.ts
    fixtures/
      request_approval/{happy,empty,error,permission,slow,large}.json
      propose_with_edits/{happy,empty,error,permission,slow,large}.json
    README.md.tmpl
    .gitignore.tmpl
cmd/dockyard/main.go                      # one new blank import:
                                          # _ "...templates/approval-flows"
web/
  bridge/
    src/protocol.ts                       # add ViewNotification.elicitationResponse
                                          # + typed ElicitationResponseParams
    src/bridge.ts                         # add sendElicitationResponse helper
    src/__tests__/elicitation-response.test.ts
                                          # MessageChannel round-trip test
  ui/
    src/FieldDiff.svelte                  # the new shared component
    src/index.ts                          # export FieldDiff
    src/__tests__/FieldDiff.test.ts       # vitest coverage
  inspector/
    src/App.svelte                        # layout fix: 100vh, scrollable regions
    src/lib/api.ts                        # postElicitationResponse client
    src/__tests__/elicitation.test.ts     # frontend client round-trip
scripts/smoke/
  phase-25.sh                             # one assertion per acceptance criterion
test/integration/
  phase25_approval_flows_test.go          # end-to-end: materialise + build +
                                          # tools/call (both tools) +
                                          # tasks/result elicitation +
                                          # asserts terminal payload
RFC-001-Dockyard.md                       # §10 rename only
```

## Public API surface

- `internal/inspector.Elicitor` — a new function-typed seam, called
  by `POST /api/tasks/elicitation`. Signature:
  `func(ctx context.Context, req ElicitationRequest) (*ElicitationResponse, error)`.
  Localhost-only (the listener's `requireLoopback` gate already
  enforces it). `ElicitationFromServer(baseURL)` adapts a running
  MCP server's `tasks/result` endpoint into this seam.
- `web/bridge` — `ViewNotification.elicitationResponse` (the wire
  method name), `ElicitationResponseParams` (the typed payload), and
  `BridgeShell.sendElicitationResponse(taskId, payload)` (the typed
  View helper).
- `web/ui` — `FieldDiff` component:
  `FieldDiff({ id, label, type, current, proposed, options?,
  onChange?, helperText?, ariaDescribedBy? })`.

## Test plan

- **Unit (`internal/inspector`):** the new endpoint validates the
  body, dispatches to the seam, returns a typed response;
  `ElicitationFromServer` opens a short-lived client, calls
  `tasks/result`, closes; a detached inspector answers 503;
  a transport-level failure surfaces as 502.
- **Unit (`templates/approval-flows/pkg/handlers`):** each handler
  hits its happy path; a `RequireInput` reply with `Declined=true`
  resolves the task with `approved=false`; a cancellation via
  `Cancelled()` returns a typed error and a `cancelled` lifecycle.
- **Unit / golden (`internal/scaffold`):** the
  `approval-flows` materialisation produces the expected tree
  (a goldens-style test mirroring Phase 24's
  `analytics-widgets.golden`).
- **Unit (`web/bridge`):** the elicitation-response View helper
  posts the right notification shape; the protocol module exports
  the new symbols.
- **Unit (`web/ui`):** `FieldDiff` renders the original / proposed
  pair, the editable input reflects the proposed value, an
  `onChange` callback fires with the final value, the
  `aria-describedby` chain is wired; meets the per-package
  coverage threshold.
- **Integration (`test/integration/phase25_approval_flows_test.go`):**
  materialise the template against the real Dockyard checkout (the
  `DockyardReplace` workflow), `go mod tidy`, `go build`, spin up
  the server with a real `tasks.Engine`, drive each tool with a
  real SDK client over the `runtime/server` transport: call
  `tools/call` → observe `CreateTaskResult` → poll `tasks/get`
  until `input_required` → call `tasks/result` with the elicitation
  payload → observe the task transition to `completed` → assert
  the final payload (`approved`, `reason`, `edits`, `decided_at`).
  Cover approve / reject / edits-then-approve for both tools.
  Run under `-race`; real drivers, no mocks at the seam (AGENTS.md
  §17).
- **Integration (`web/bridge`):** the elicitation-response message
  round-trips View → host through a `MessageChannel`; the host-half
  receives the typed notification verbatim.
- **Integration (`web/inspector`):** the elicitation-response client
  POSTs the typed request; the inspector backend's elicitor seam
  receives the typed payload; a mocked seam asserts the request shape.
- **Concurrency:** Phase 25 does not change a reusable artifact's
  contract (the runtime `Engine` is unchanged; the scaffolded
  `main.go` constructs one Engine per server). The bridge / web/ui
  additions are single-threaded by browser event loop. No new
  `-race` regression surface — Phase 13/14 tests still cover the
  engine.

## Smoke script additions

`scripts/smoke/phase-25.sh` — one assertion per acceptance criterion:

- `dockyard new --template approval-flows ...` produces a project.
- The resulting project builds CGo-free (`go build ./...`).
- The resulting project's contract tests pass (`go test ./...`).
- The manifest declares exactly two tools, both `task_support: required`,
  and exactly one app with `display_modes: [inline]` only.
- The materialised `cmd/server/main.go` mentions `tasks.NewEngine`,
  `Options{...Tasks:` and `engine.StartSweep` — the D-135 wiring is
  emitted, not hand-written by the developer.
- `templates/approval-flows/web/src/App.svelte`,
  `ApprovalCard.svelte` and `EditsForm.svelte` exist; the App
  imports `FieldDiff` from `@dockyard/ui` (not template-local).
- `web/ui/src/FieldDiff.svelte` exists and is exported from
  `web/ui/src/index.ts`.
- `docs/design/CONVENTIONS.md` §3 lists `FieldDiff`.
- The bridge declares `ViewNotification.elicitationResponse` and
  exposes `sendElicitationResponse` on `BridgeShell`.
- The inspector backend declares `POST /api/tasks/elicitation`.
- A `grep` confirms `approval-flow` (singular, word-boundary) is
  absent from `internal/`, `cmd/`, `docs/plans/`, `docs/design/`,
  `docs/glossary.md`, `scripts/`, and the RFC. The two brief
  historical mentions are the only allowed matches.
- All four phase-25 screenshots exist under
  `docs/screenshots/phase-25/`.

A check against an unbuilt surface skips, never fails (per
`scripts/smoke/common.sh`).

## Coverage target

- `internal/inspector` (the new elicitation surface) — 80%.
- `templates/approval-flows/internal/handlers` — 75%
  (template handlers are demonstration code; the contract surface
  is exercised by the Phase 25 integration test).
- `web/ui` `FieldDiff` — meets the `web/ui` per-package threshold
  Phase 21.5 enforces.
- `web/bridge` — meets the `web/bridge` per-package threshold (the
  new helper + protocol message land with their tests).

## Dependencies

- Phase 14 (the `tasks.Engine` + `TaskHandle` + the transport mount —
  the runtime surface this template is the first product driver of).
- Phase 19 (`dockyard dev` — the developer's local loop the template
  runs under).
- Phase 20 (`dockyard build` / `run` — the materialised project's
  build pipeline + the `//go:embed` step).
- Phase 22 / 23 (the inspector — the host-half of the bridge, the
  Tasks panel, the Fixtures switcher).
- Phase 24 (the template system — the `internal/scaffold` seam this
  template plugs into; the `analytics-widgets` precedent for the
  template shape).

## Risks / open questions

- **Tasks engine attached on a no-template scaffold?** D-135 makes
  the wiring generic, but Phase 17's no-template scaffold currently
  declares its example tool `task_support: forbidden`; auto-attaching
  an engine there would be unused code. D-135 scopes the wiring to
  *templates that declare task-supporting tools*; the no-template
  scaffold is unchanged until a later phase opts it in. Scoping is
  filed in the decision.
- **Elicitation response over HTTP — auth context.** On HTTP a real
  deployment binds the `tasks/result` call to the requestor's
  authorization context (RFC §8.5). The template ships the stdio
  posture (single-user, unauthenticated); the README documents the
  HTTP shape, but the integration test exercises only stdio. An HTTP
  conformance test is Phase 27's concern, not Phase 25's. Filed
  in §16 / risks of the decision so a reader does not miss it.
- **`FieldDiff` for a non-template consumer?** D-127's
  `Sparkline` precedent says reusable primitives land in `web/ui`
  even when one template is the first consumer. `FieldDiff` is
  genuinely reusable — the inspector's future fixture-editor UX and
  any future "review before commit" template will need an editable
  current → proposed pair. It lands in `web/ui` on the same
  reasoning.

## Glossary additions

- `approval-flows` — the V1 template name (replaces `approval-flow`
  in the master plan and RFC §10).
- `FieldDiff` — the shared web/ui component that shows an original
  value paired with an editable proposed value. Used by the
  `propose_with_edits` App, reusable beyond.
- `elicitation-response` — the bridge's View→host notification that
  carries the user's reply to an `input_required` task prompt; the
  inspector's host-half delivers it to the attached server's
  `tasks/result` endpoint (D-134).
- `scaffold-tasks-engine` — the scaffold wiring that emits a
  `tasks.Engine` construction + `server.Options.Tasks` attachment in
  the materialised `main.go` when the template declares a
  task-supporting tool (D-135).

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
- [ ] D-134 / D-135 filed in `docs/decisions.md`
- [ ] UI touched ⇒ composes `web/ui`; new shared component (`FieldDiff`)
      landed in `web/ui/` + `CONVENTIONS.md`; every page routes through
      `PageState` (loading / empty / error / ready)
