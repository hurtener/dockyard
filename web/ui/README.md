# @dockyard/ui

Dockyard's shared UI design system: the design tokens and the `web/ui` Svelte
component inventory that every Dockyard frontend surface — the inspector, the
template App UIs, the docs site — composes rather than re-implementing.

This package is the **only** source of these building blocks (`AGENTS.md` §20,
`docs/design/CONVENTIONS.md`). A page never forks a component or hard-codes a
visual value; it composes the inventory and reads tokens.

## What's here

- `src/tokens.css` — the `--dy-*` design tokens as CSS custom properties (light
  theme; structured so a dark theme is a token-set swap).
- `src/tokens.ts` — the typed token tree + the `tokenVar()` accessor.
- `src/*.svelte` — the component inventory: shell & layout, data display, and the
  four-state `PageState` family.
- `src/index.ts` — the public barrel.

## Conventions

- Plain Svelte 5, no SvelteKit (D-006). Components are framework-agnostic render
  units that drop into a bare iframe bundle unchanged.
- Typed props, token-driven (no ad-hoc hex / magic spacing), keyboard-accessible
  with the `primary` focus ring.
- Every async region routes through `PageState`; its empty and error panels carry
  real copy and a working retry — the four-state rule is mandatory.

## Develop

```bash
npm install      # or: npm ci
npm run gate     # svelte-check + tsc + vitest — the frontend gate
```

`make web` runs this gate for every `web/` project in CI.
