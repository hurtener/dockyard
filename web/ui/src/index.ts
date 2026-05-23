/**
 * @dockyard/ui — the public barrel.
 *
 * The shared design system: design tokens and the web/ui component inventory
 * every Dockyard frontend surface composes (docs/design/CONVENTIONS.md §3,
 * AGENTS.md §20). Importing from this module also side-effect-loads `tokens.css`
 * via `./tokens.js`, so a consumer gets the `--dy-*` custom properties.
 */

// -- Design tokens --
export { tokens, tokenVar, applyTheme } from './tokens.js';
export type { TokenName, ThemeName } from './tokens.js';

// -- Shared types --
export type {
  PageStateValue,
  StatusTone,
  Column,
  SortState,
  TimelineEvent,
  Row,
} from './types.js';

// -- Shell & layout --
export { default as AppShell } from './AppShell.svelte';
export { default as PageHeader } from './PageHeader.svelte';
export { default as DetailRail } from './DetailRail.svelte';
export { default as RailCard } from './RailCard.svelte';
export { default as ActionBar } from './ActionBar.svelte';
export { default as ConnectionFooter } from './ConnectionFooter.svelte';

// -- Data display --
export { default as DataTable } from './DataTable.svelte';
export { default as Pagination } from './Pagination.svelte';
export { default as FilterBar } from './FilterBar.svelte';
export { default as MetricCard } from './MetricCard.svelte';
export { default as Sparkline } from './Sparkline.svelte';
export { default as StatusChip } from './StatusChip.svelte';
export { default as Timeline } from './Timeline.svelte';
export { default as JsonInspector } from './JsonInspector.svelte';
export { default as CodeBlock } from './CodeBlock.svelte';

// -- State — the four-state PageState family --
export { default as PageState } from './PageState.svelte';
export { default as LoadingState } from './LoadingState.svelte';
export { default as EmptyState } from './EmptyState.svelte';
export { default as ErrorState } from './ErrorState.svelte';
export { default as PermissionState } from './PermissionState.svelte';
