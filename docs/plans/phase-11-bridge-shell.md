# Phase 11 — Svelte bridge shell library

## Summary

Phase 11 delivers `web/bridge/` — the Svelte/TypeScript library that implements the
**host side of the `ui/` postMessage JSON-RPC dialect** so MCP App authors never
hand-write protocol code. It performs the `ui/initialize` handshake, exposes
`hostContext` as Svelte stores, fans out host→view notifications, offers typed
view→host helpers (including inline/fullscreen/pip display-mode negotiation), and
framework-manages `_meta.viewUUID`-keyed view-state across re-renders. It is the
first frontend code in the repo and therefore also establishes the `web/`
toolchain and a frontend CI gate.

## RFC anchor

- RFC §7.2 — the three display modes (inline / fullscreen / pip), settled as a
  runtime protocol negotiation.
- RFC §7.3 — the bridge shell library: the one piece of client-shaped code,
  shipped to app authors as a Svelte library.

## Briefs informing this phase

- brief 01

## Brief findings incorporated

- **The View↔host channel is a JSON-RPC dialect of MCP over `postMessage`**
  (brief 01 §2.4). The bridge implements the *View* side: it sends `ui/initialize`
  with `{protocolVersion, capabilities.appCapabilities, clientInfo}`, receives
  `McpUiInitializeResult` with `hostContext`/`hostCapabilities`/`hostInfo`, and
  **waits for `ui/notifications/initialized` before assuming readiness**.
- **`hostContext` carries `theme`, `styles.variables`, `displayMode`,
  `availableDisplayModes`, `locale`, `containerDimensions`, …** (brief 01 §2.4).
  The bridge exposes these as reactive Svelte stores; `styles.variables` are
  applied as CSS custom properties on the document root.
- **Host→View notifications** are `ui/notifications/tool-input`,
  `tool-input-partial`, `tool-result`, `tool-cancelled`, `size-changed`,
  `host-context-changed` (brief 01 §2.4). The bridge fans these out to typed
  subscribers; `host-context-changed` is a `Partial<HostContext>` patch.
- **View→Host requests** are `ui/open-link`, `ui/message`,
  `ui/request-display-mode`, `ui/update-model-context`, and proxied `tools/call`
  (brief 01 §2.4). The bridge offers typed helpers; the host **proxies** all
  View-initiated `tools/call` to the MCP server over the normal transport.
- **`_meta.viewUUID` is the hook for view-state persistence across re-renders**
  (brief 01 §2.6, open question Q-9). The bridge resolves Q-9 in favour of
  *framework-managed* view-state (RFC §7.3): a `viewUUID`-keyed snapshot store
  that survives a re-render of the same view.
- **`structuredContent` is the typed, UI-only payload** excluded from model
  context (brief 01 §2.6). The bridge consumes a generated `contracts.ts` so the
  `structuredContent` of a `tool-result` is typed and cannot drift from the Go
  output struct — `tool-result` notifications carry a generic typed payload.
- **Forward-compatibility** (brief 01 §4.4): the bridge keeps the negotiated
  `protocolVersion` and tolerates reading the reserved `sandbox-proxy` /
  `sandbox-resource` notifications without crashing (it ignores them).

## Findings I'm departing from (if any)

- Brief 01 §2.8 / §5 describe a per-host **capability matrix** as a build-time
  gate. The bridge does **not** implement one — per RFC §7.5 and AGENTS.md §6,
  host support is read from the negotiated `hostCapabilities` /
  `availableDisplayModes` at run time, never from a hardcoded host list. The
  bridge only offers display modes the host actually advertised; degradation is
  capability-driven. (Filed as D-059.)

## Goals

- Ship `web/bridge/` as a plain-Svelte + TypeScript library (D-006 — no
  SvelteKit) with a real build, type-check, and unit-test toolchain.
- Implement the View half of the `ui/` postMessage dialect: handshake,
  notification fan-out, typed view→host helpers, display-mode negotiation.
- Expose `hostContext` as reactive Svelte stores; apply `styles.variables`.
- Framework-manage `_meta.viewUUID`-keyed view-state across re-renders.
- Consume a generated `contracts.ts` shape so `structuredContent` is typed.
- Establish the **frontend CI gate**: a `web/` job in `.github/workflows/ci.yml`
  and a matching `Makefile` target so the frontend is gated like the Go code.

## Non-goals

- The `web/ui` design-system inventory and visual components (Phase 10a). The
  bridge is non-visual protocol plumbing and must not depend on Phase 10a tokens.
- The host-side implementation of the dialect (the inspector, Phase 22) — the
  bridge is the *View* side; a minimal in-test host harness stands in for a host.
- Real `contracts.ts` generation (Phase 06 codegen owns that) — the bridge ships
  the *type contract* `contracts.ts` must satisfy and a hand-written test fixture.
- Host-profile / `_meta.ui.domain` derivation (Phase 12).
- Bundling apps into single-file HTML (Phase 24+ templates / RFC §7.4).

## Acceptance criteria

- [ ] The `ui/initialize` handshake completes against a test host harness: the
      bridge sends `ui/initialize`, receives the result, waits for
      `ui/notifications/initialized`, and only then resolves `ready`.
- [ ] All three display modes (inline / fullscreen / pip) negotiate:
      `requestDisplayMode` sends `ui/request-display-mode`, the host grant/deny is
      reflected, and a mode absent from `availableDisplayModes` is rejected
      client-side without a round trip.
- [ ] The bridge consumes typed contract data: a `tool-result` notification
      surfaces `structuredContent` typed against the generated `contracts.ts`
      shape.
- [ ] `_meta.viewUUID` view-state round-trips: a value saved under a `viewUUID`
      is recovered after a simulated re-render of the same view, and is isolated
      from a different `viewUUID`.
- [ ] Host→view notifications (`tool-input`, `tool-input-partial`, `tool-result`,
      `tool-cancelled`, `size-changed`, `host-context-changed`) fan out to
      subscribers; `host-context-changed` patches the `hostContext` stores.
- [ ] `web/bridge` type-checks (`svelte-check` + `tsc`) and its `vitest` unit
      suite passes; the frontend gate runs in CI and via a `Makefile` target.

## Files added or changed

```text
web/bridge/
  package.json                 # plain-Svelte library, deps + scripts
  tsconfig.json                # strict TS config
  svelte.config.js             # plain-Svelte (vitePreprocess), no SvelteKit
  vitest.config.ts             # jsdom test environment
  .gitignore                   # node_modules, build output
  README.md                    # library overview + usage
  src/
    index.ts                   # public entry point — re-exports
    protocol.ts                # the ui/ JSON-RPC dialect types + method names
    contracts.ts               # the typed-contract shape codegen must satisfy
    transport.ts               # postMessage JSON-RPC transport (window-agnostic)
    host-context.ts            # hostContext Svelte stores + styles.variables
    notifications.ts           # host→view notification fan-out
    view-state.ts              # viewUUID-keyed framework-managed view-state
    bridge.ts                  # the BridgeShell — handshake + typed helpers
  src/__tests__/
    transport.test.ts
    bridge.test.ts             # handshake + display-mode negotiation
    notifications.test.ts
    view-state.test.ts
    contracts.test.ts
    harness.ts                 # in-test host harness (host half of the dialect)
.github/workflows/ci.yml       # + a `web` job (frontend type-check + tests)
Makefile                       # + `web` and `web-install` targets; help text
docs/plans/phase-11-bridge-shell.md
docs/decisions.md              # + D-059, D-060, D-061
docs/glossary.md               # + bridge shell, View, hostContext, viewUUID …
scripts/smoke/phase-11.sh
```

A new top-level `web/` directory: it is already named in AGENTS.md §3, so no
AGENTS.md edit is required for the directory itself. AGENTS.md §4 does not yet
mention the frontend gate — flagged in the PR for the coordinator to record
(AGENTS.md is not editable by this phase).

## Public API surface

The library is TypeScript, not Go. The public surface (`web/bridge/src/index.ts`):

- `createBridge(options?: BridgeOptions): BridgeShell` — constructs the shell.
- `BridgeShell` — `connect(): Promise<void>` (runs the handshake); `ready` store;
  `hostContext` stores (`theme`, `displayMode`, `availableDisplayModes`,
  `styleVariables`, `locale`, `containerDimensions`, `raw`);
  `onToolInput`/`onToolInputPartial`/`onToolResult<T>`/`onToolCancelled`/
  `onSizeChanged`/`onHostContextChanged` subscriptions;
  `requestDisplayMode(mode)`, `openLink(url)`, `sendMessage(role, content)`,
  `updateModelContext(patch)`, `callTool<I, O>(name, args)` helpers;
  `viewState<T>(uuid): ViewStateHandle<T>` — framework-managed view-state.
- `protocol.ts` exports the `ui/` method names and the wire types.
- `contracts.ts` exports `ToolContract<I, O>` — the shape `dockyard generate`'s
  `contracts.ts` must satisfy (Phase 06 / RFC §6).

## Test plan

- **Unit:** `transport.test.ts` (JSON-RPC framing, request/response correlation,
  notification dispatch, ignoring reserved sandbox-proxy notifications);
  `bridge.test.ts` (handshake order: `ui/initialize` → result → wait for
  `ui/notifications/initialized` → `ready`; all three display modes negotiate;
  a mode absent from `availableDisplayModes` is rejected without a round trip);
  `notifications.test.ts` (each host→view notification fans out;
  `host-context-changed` patches stores); `view-state.test.ts` (`viewUUID`
  round-trip across a simulated re-render; isolation between UUIDs);
  `contracts.test.ts` (a typed `tool-result` payload conforms to the
  `contracts.ts` shape).
- **Integration:** the in-test **host harness** (`harness.ts`) plays the host
  half of the dialect over a `MessageChannel`, so `bridge.test.ts` exercises the
  real handshake end-to-end rather than mocking the transport — this is the
  cross-half wiring proof for a TS library (AGENTS.md §17 in spirit; the Go
  integration-test rule is N/A for a TS package).
- **Concurrency / golden:** N/A — a browser `postMessage` channel is
  single-threaded (event loop); no Go race surface. No golden codegen output in
  this phase.

## Smoke script additions

`scripts/smoke/phase-11.sh` asserts: `web/bridge/package.json` exists; the public
entry `src/index.ts` and `src/bridge.ts` exist; the bridge declares no SvelteKit
dependency (D-006); the `vitest` config is present; `npm` is available and the
`web/bridge` type-check + unit suite passes (`make web`); the CI workflow has a
`web` job; the `Makefile` has a `web` target. A check against an unbuilt surface
or a missing `npm` skips rather than fails.

## Coverage target

- `web/bridge` — 80% (new package; the TS equivalent of the new-package default,
  AGENTS.md §11). Measured by `vitest --coverage`. Go coverage is N/A: this phase
  ships no Go code.

## Dependencies

- Phase 09 (MCP Apps extension, server-side) — establishes the `_meta.ui` /
  `structuredContent` contract the bridge consumes from the View side.

## Risks / open questions

- **Spec churn (brief 01 §4.4, RFC §18 Q-4).** The Apps spec is under active
  development; the `ui/` dialect may gain methods. Mitigation: the transport
  keeps the negotiated `protocolVersion`, every method name is centralised in
  `protocol.ts`, and unknown / reserved notifications are ignored, not fatal.
- **`contracts.ts` is owned by Phase 06 codegen, which has not landed.** The
  bridge ships the *type contract* `contracts.ts` must satisfy plus a fixture;
  when Phase 06 lands, generated output must conform — recorded in D-061.
- **The bridge is the View side only.** The host half (the inspector, Phase 22)
  must agree on the dialect. The in-test harness pins the wire shape so a future
  host implementation has an executable reference.

## Glossary additions

- **Bridge shell** — the `web/bridge/` Svelte/TypeScript library implementing the
  View side of the `ui/` postMessage dialect.
- **View** — the App's UI running inside the host's sandboxed iframe; the
  client-shaped peer of an MCP host in the `ui/` dialect.
- **hostContext** — the host-supplied context (theme, `styles.variables`,
  display mode, locale, dimensions) delivered in the `ui/initialize` result.
- **viewUUID** — the `_meta.viewUUID` key under which the bridge persists an
  App's view-state across re-renders.
- **Display-mode negotiation** — the runtime protocol exchange
  (`ui/request-display-mode` / `hostContext.displayMode`) by which a View moves
  between inline, fullscreen, and pip.

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
