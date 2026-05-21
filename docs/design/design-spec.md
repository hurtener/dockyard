# Dockyard — Design Spec

**Status:** Draft — for mockup review, then Phase 10a build.
**Companion:** `docs/design/CONVENTIONS.md` (the binding charter). This file is the
*concrete* spec — design tokens, the `web/ui` component inventory, and the inspector
page layout — that Phase 10a builds, and that the inspector mockup is drawn against.

> Template App layouts (`analytical-card`, `approval-flow`, `inspector`) are
> **deliberately not specced here** — the template set may be reworked before Wave 9.
> Each template is specced + mocked up with its own phase (24–26). Phase 10a's
> mockup scope is therefore the **inspector only**, plus the logo.

---

## 1. Brand

The Dockyard logo (`docs/design/logo.png`) — a gantry crane lifting a ship in a
dockyard — is the brand mark. The design tokens below derive their palette from it.

---

## 2. Design tokens

The single source of visual truth (`CONVENTIONS.md` §5). Phase 10a ships these as a
token module the `web/ui` components and every Dockyard surface consume; no
component carries an ad-hoc colour or spacing value. Tokens also feed the
host-themeable CSS variables an MCP App receives via `hostContext.styles.variables`.

### 2.1 Colour — palette (from the logo)

| Token | Value (approx) | Role |
|-------|----------------|------|
| `ink` | `#0E2B33` | near-black navy — primary text, dark surfaces, the crane/hull |
| `ink-soft` | `#3A5159` | secondary text, muted labels |
| `primary` | `#2FC4C9` | teal — primary actions, links, active state, focus ring |
| `primary-strong` | `#1B9AA0` | teal hover/pressed |
| `mint` | `#CDEDE8` | light fill — subtle backgrounds, selected rows |
| `accent` | `#F4A93C` | orange — the one attention colour: warnings-in-progress, the "live" dot, highlights. Used sparingly. |
| `surface` | `#FFFFFF` | cards, panels |
| `canvas` | `#F4F7F8` | app background |
| `border` | `#D9E2E4` | hairlines, dividers, table rules |

### 2.2 Semantic state colours

| Token | Role |
|-------|------|
| `state-ok` | success / passing verdict (a calm green) |
| `state-warn` | warning / flag raised (`accent` family) |
| `state-error` | error / failing verdict / drift |
| `state-info` | neutral informational |

Each state colour ships as a trio: `-fg` (text/icon), `-bg` (tint background),
`-border`.

### 2.3 Spacing, type, radius, elevation

- **Spacing scale** (4 px base): `0, 4, 8, 12, 16, 24, 32, 48, 64`.
- **Type:** one sans family (`--font-sans`) + one mono (`--font-mono`, for the RPC
  log / JSON). Size scale: `xs 12 / sm 13 / base 14 / md 16 / lg 20 / xl 28`.
  Weights: `regular 400 / medium 500 / semibold 600`.
- **Radius:** `sm 4 / md 8 / lg 12 / full`.
- **Elevation:** `flat` (border only) · `raised` (card) · `overlay` (modal/popover).
  Soft, low-spread shadows — no heavy drops.

### 2.4 Dark mode

Tokens are defined as CSS custom properties so a dark theme is a token-set swap, not
a component change. V1 ships the light theme; the token structure must not preclude
dark. (An MCP host may also push its own theme via `hostContext` — the bridge shell
maps that onto the same variables.)

---

## 3. The `web/ui` component inventory

Shared Svelte components Phase 10a builds into `web/ui/` and documents in
`CONVENTIONS.md §3`. Every Dockyard surface composes these — no page re-implements
one (`AGENTS.md` §20). Each entry: **purpose** · key props · states.

### 3.1 Shell & layout

- **`AppShell`** — the outer frame: header slot, optional rail slot, main content
  slot, footer slot. Props: `rail?`, `density?`.
- **`PageHeader`** — page title + subtitle, an actions slot, an optional status
  area. Props: `title`, `subtitle?`.
- **`DetailRail`** — the right-hand rail container; holds stacked or tabbed
  `RailCard`s. Props: `tabs?`.
- **`RailCard`** — one titled card within the rail. Props: `title`, `collapsible?`.
- **`ActionBar`** — a row of buttons/controls, right- or left-aligned.
- **`ConnectionFooter`** — the app-shell status bar: connection state, server id,
  transport.

### 3.2 Data display

- **`DataTable`** — columns, rows, sort, optional row-select; composes `Pagination`
  and `PageState`. Props: `columns`, `rows`, `sort?`, `onRowClick?`.
- **`Pagination`** — page controls for a `DataTable`.
- **`FilterBar`** — search input + filter chips; the place a search lives (never
  embedded in `PageHeader`).
- **`MetricCard`** — one KPI: label, value, optional delta/trend.
- **`StatusChip`** — a small state pill (`ok` / `warn` / `error` / `info` / neutral).
- **`Timeline`** — an ordered sequence of timestamped events (used by the obs
  stream and the task-lifecycle view).
- **`JsonInspector`** — collapsible, syntax-highlighted JSON tree (mono font); used
  for RPC payloads, `structuredContent`, schemas.
- **`CodeBlock`** — read-only mono block with copy.

### 3.3 State — the four-state `PageState`

- **`PageState`** — wraps an async region; routes to exactly one of:
  `LoadingState` · `EmptyState` · `ErrorState` · ready (slot). **Empty and error
  are mandatory** with real copy + a working retry (`CONVENTIONS.md` §4).
- **`LoadingState`** · **`EmptyState`** · **`ErrorState`** · **`PermissionState`**
  — the individual panels; each takes a message + an optional action.

### 3.4 Notes

- Genuinely template-specific blocks (e.g. an `ApprovalPanel`) are **not** in the
  V1 `web/ui` inventory — they land with their template phase, composed *from* these
  primitives. Phase 10a builds the universal set above.
- All components are typed (TypeScript), token-driven, accessible (keyboard +
  focus-ring from `primary`), and framework-plain-Svelte (D-006).

---

## 4. The inspector — page spec

The inspector is Dockyard's local **test/debug surface** (RFC §12) — dev-mode-gated,
localhost-only, read-only. It runs inside `dockyard dev` and standalone via
`dockyard inspect`. It implements the **host half** of the `ui/` bridge to render an
MCP App, and consumes the `obs/v1` stream. This is the layout the inspector mockup
should be drawn against; the inspector is built later (Wave 8, phases 22–23) to the
approved mockup.

### 4.1 Layout

One `AppShell`:

```text
┌─ PageHeader ──────────────────────────────────────────────────────────┐
│ [logo] Dockyard Inspector   ·  <server name> v<x>  ·  <transport>      │
│                                      [ Host: ▾ ] [ Display: inline ▾ ] │
├──────────────────────────────────────────┬────────────────────────────┤
│                                          │  DetailRail (tabbed)        │
│   App preview frame                      │  ┌────────────────────────┐ │
│   — the rendered MCP App in its          │  │ Events | RPC | Fixtures│ │
│     sandboxed iframe                     │  │ Tools  | Verdicts |Tasks│ │
│   — viewport / device selector           │  │                        │ │
│   — display-mode reflects inline/        │  │  (active panel body)   │ │
│     fullscreen/pip negotiation           │  │                        │ │
│                                          │  └────────────────────────┘ │
├──────────────────────────────────────────┴────────────────────────────┤
│ ConnectionFooter: ● connected · session <id> · 42 events · 0 errors    │
└─────────────────────────────────────────────────────────────────────────┘
```

### 4.2 Regions

- **PageHeader** — Dockyard logo + "Inspector"; the connected server's name /
  version / transport; a `StatusChip` for connection state. Two controls:
  **Host** (capability-set emulation — emulate a host that does/doesn't negotiate
  Apps, Tasks, a given display mode — RFC §7.5/§12) and **Display mode**
  (inline / fullscreen / pip).
- **App preview frame** (main) — the MCP App rendered in its sandboxed iframe via
  the host half of the `ui/` bridge; a viewport/device selector; the frame reflects
  the negotiated display mode.
- **DetailRail** — tabbed `RailCard`s:
  - **Events** — the live `obs/v1` stream as a `Timeline`; per-event detail in a
    `JsonInspector`. Filterable.
  - **RPC** — the JSON-RPC message log (`tools/call`, `resources/read`, `ui/*`),
    method-filterable, payloads in `JsonInspector`.
  - **Fixtures** — the fixture switcher: `happy` / `empty` / `error` / `permission`
    / `slow` / `large`, wired to the generated contracts so UI states are exercised
    without a backend.
  - **Tools / Resources** — list (`DataTable`) + invoke a tool / read a resource.
  - **Verdicts** — contract-drift, schema-validation, and spec-compliance results
    as `StatusChip` rows.
  - **Tasks** — task-lifecycle + `input_required` round-trips as a `Timeline`.
- **ConnectionFooter** — connection state, session id, event/error counts; the
  `accent` "live" dot when events are streaming.

### 4.3 State & quality rules

- Every async region routes through `PageState` — the Events/RPC panels have a real
  **empty** state ("No events yet — call a tool to see traffic") and the App frame
  an **error** state with retry.
- The inspector composes only `web/ui`; it never re-implements a component (§20).
- Read-only, localhost-only, dev-mode-gated — the spec must not imply any
  mutating/production capability.

---

## 5. Templates — deferred

The three V1 template App layouts are intentionally **out of this spec**. The
template set may be reworked before Wave 9; each template is specced and mocked up
with its own phase (24–26), composing the `web/ui` inventory and tokens above.
