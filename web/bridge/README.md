# `@dockyard/bridge` — the Svelte bridge shell

The View side of the MCP Apps `ui/` postMessage JSON-RPC dialect (RFC §7.2,
§7.3). It is the one piece of client-shaped code Dockyard ships — a Svelte +
TypeScript **library** vendored into every generated app, *not* a
Dockyard-operated client (P4). Its peer, the host half of the same dialect, is
the inspector (Phase 22).

## What it does

- Performs the `ui/initialize` handshake and waits for
  `ui/notifications/initialized` before reporting ready.
- Exposes `hostContext` — theme, `styles.variables`, display mode, locale,
  container dimensions — as reactive Svelte stores.
- Fans out host -> View notifications (`tool-input`, `tool-input-partial`,
  `tool-result`, `tool-cancelled`, `size-changed`, `host-context-changed`).
- Offers typed view -> host helpers — display-mode negotiation across
  inline / fullscreen / pip, `ui/open-link`, `ui/message`,
  `ui/update-model-context`, and proxied `tools/call`.
- Framework-manages `_meta.viewUUID`-keyed view-state across re-renders.
- Consumes the generated `contracts.ts` shape so `structuredContent` is typed.

It is **non-visual**: it applies the host's CSS variables but defines no design
tokens of its own (it does not depend on the Phase 10a design system).

## Usage

```ts
import { createBridge } from '@dockyard/bridge';

const bridge = createBridge({
  clientInfo: { name: 'customer-health', version: '1.0.0' },
  displayModes: ['inline', 'fullscreen'],
});

await bridge.connect();

bridge.onToolResult<MyToolOutput>((result) => {
  render(result.structuredContent);
});

await bridge.requestDisplayMode('fullscreen');
```

## Toolchain

Plain Svelte + TypeScript — no SvelteKit (decision D-006).

```sh
npm install            # from web/bridge/
npm run check          # svelte-check + tsc --noEmit
npm run test           # vitest run
npm run coverage       # vitest run --coverage
npm run gate           # check + test — the frontend gate (make web)
```

The repo-level gate is `make web` (type-check + unit tests) and `make
web-install` (install dependencies); both run in CI.
