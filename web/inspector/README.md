# `@dockyard/inspector` — the inspector frontend

The Svelte frontend of Dockyard's local inspector (RFC §12) — the dev-mode-gated,
localhost-only, read-only test/debug surface. It is built to the page spec in
`docs/design/design-spec.md §4` and the approved mockup
`docs/design/mockups/inspector.png`, and composes the shared `@dockyard/ui`
design system (CONVENTIONS.md §3) — it never re-implements a shared component.

Unlike `web/ui` and `web/bridge` (libraries), `web/inspector` is a Vite
**application**: `vite build` emits a static `dist/` tree that the Go inspector
backend (`internal/inspector`) embeds and serves from its localhost listener.

## What this builds (Phase 22 — the inspector core)

- **`src/host/`** — the **host half** of the `ui/` postMessage bridge: the
  counterpart to `@dockyard/bridge`'s View half. It renders an MCP App in a
  sandboxed iframe, completes the `ui/initialize` handshake, supplies
  `hostContext`, and negotiates display mode. It imports the `ui/` protocol
  contract verbatim from `@dockyard/bridge` — the dialect is never forked.
- **`src/lib/`** — the inspector views: the `AppShell` layout, the App preview
  frame, the **Events** panel (the live `obs/v1` stream as a `Timeline`), and
  the **RPC** panel (the JSON-RPC log). Every async region routes through the
  four-state `PageState`.

The Fixtures / Tools / Verdicts / Tasks rail tabs and the Host/Display-mode
control deep behaviour are Phase 23.

## Scripts

- `npm run dev` — Vite dev server.
- `npm run build` — production build to `dist/`.
- `npm run check` — `svelte-check` + `tsc`.
- `npm run test` / `npm run coverage` — Vitest.
- `npm run gate` — `check` + `coverage` (the `make web` gate).
