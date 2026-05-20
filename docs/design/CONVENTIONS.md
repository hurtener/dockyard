# Dockyard — Frontend Design System & UI Conventions

**Status:** Charter (established at scaffold time; the component inventory in §3 is
filled by master-plan **Phase 10a**).

> This document exists from the **start** of the project on purpose. The sibling
> project Harbor did not establish a design system up front: pages were built
> ad hoc, components and patterns were duplicated across the Console, and a costly
> remediation was needed later to retrofit a shared foundation. Dockyard does not
> repeat that mistake — the design system is a day-one artifact, and composing it
> is mandatory repo hygiene (`AGENTS.md` §20).

---

## 1. Why this exists

Dockyard ships several frontend surfaces. Without one shared foundation they would
drift into duplicated tables, divergent loading states, inconsistent spacing, and
parallel data-fetching patterns. One design system keeps every Dockyard surface
coherent and keeps the cost of a new page low.

## 2. Scope — the surfaces this governs

| Surface | What | Phase |
|---------|------|-------|
| Template App UIs | The Svelte UIs of the `analytical-card`, `approval-flow`, `inspector` templates | 24–26 |
| The inspector | Dockyard's local test/debug surface | 22–23 |
| The bridge shell library | `web/bridge/` — the `ui/` postMessage layer (non-visual, but consumes tokens) | 11 |
| The docs site | The published GitHub Pages technical docs | 29 |
| The multi-server console | **Post-V1** — must build on this same system | RFC §19 |

## 3. The shared component inventory (`web/ui/`)

Shared Svelte components live in `web/ui/` and are the **only** source of these
building blocks — pages compose them, never re-implement them. Phase 10a delivers
and documents the inventory here; the planned set:

- **Shell & layout** — `AppShell`, `PageHeader`, `DetailRail`, `RailCard`,
  `ActionBar`.
- **Data display** — `DataTable`, `Pagination`, `FilterBar`, `MetricCard`,
  `StatusChip` / `StatusBadge`, `Timeline`, `JsonInspector`, `ArtifactPreview`.
- **State** — `PageState` (see §4) and its `LoadingState`, `EmptyState`,
  `ErrorState`, `PermissionState` panels.
- **App-pattern** — `ApprovalPanel` and other pattern blocks the templates need.

A genuinely new shared component lands in `web/ui/` **and** in this section in the
same PR. A component that is truly page-specific stays page-local but is **composed
inside** `web/ui/` primitives — it never reinvents one.

## 4. The four-state page rule

Every page (and every async region) routes its state through the shared
`PageState`, which has exactly four states:

```text
loading → ready
        → empty    (no data — real copy + a refresh/retry affordance)
        → error    (a working Retry, not a dead end)
```

**Empty and error states are mandatory**, not optional polish. An empty table with
no copy, or an error with no retry, is a defect — `dockyard validate`'s quality
gate (RFC §9.4) and `AGENTS.md` §14 enforce this.

## 5. Design tokens

Colour, spacing, typography, radius, and elevation come from a single token set
(delivered by Phase 10a). Components and pages reference tokens — never ad-hoc hex
values or magic spacing numbers. The tokens also feed the host-themeable CSS
variables an MCP App receives via `hostContext.styles.variables` (RFC §7.3).

## 6. The spec → mockup → build process

UI is designed before it is coded, so look & feel is locked up front:

1. **Spec.** The UI-bearing phase plan carries a page spec — the page's purpose,
   regions, data, and states.
2. **Mockup.** A visual mockup is produced and **approved** before implementation.
   Mockups live under `docs/design/` (e.g. `docs/design/mockups/`).
3. **Build.** Only then is the page implemented, composing `web/ui/`.

The Dockyard logo and the brand are also Dockyard design artifacts and live here.

## 7. The rule (binding)

- Compose `web/ui/`; never duplicate a component or fork a pattern page-locally.
- A new shared component ⇒ `web/ui/` + this document, same PR.
- Every page: the four-state `PageState`, empty + error included.
- Tokens are the single source of visual truth.
- UI-bearing phases follow spec → mockup → build (§6).

See `AGENTS.md` §20.
