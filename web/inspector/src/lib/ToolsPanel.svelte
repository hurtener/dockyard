<script lang="ts">
  /**
   * ToolsPanel — the inspector DetailRail's Tools/Resources tab.
   *
   * Lists the attached server's tools (from its generated contracts) in a
   * `DataTable`, and lets a developer pick one to invoke. Invocation in the
   * inspector is a dev test answered from the active fixture (RFC §12 — the
   * inspector is read-only, never an arbitrary-execution proxy): selecting a
   * tool here selects it in the fixture switcher. The panel routes through the
   * four-state `PageState`; composes only `@dockyard/ui`.
   */
  import {
    DataTable,
    type Column,
    type PageStateValue,
    type Row,
  } from '@dockyard/ui';
  import type { ToolContract } from './contracts.js';

  interface Props {
    /** The attached server's generated tool contracts. */
    contracts: ToolContract[];
    /** The fetch state — drives the four-state PageState. */
    panelState: PageStateValue;
    /** Called when the user retries a failed contracts fetch. */
    onRetry?: () => void;
    /** Called when a tool row is picked — selects it for fixture-driven invoke. */
    onSelectTool?: (contract: ToolContract) => void;
  }

  let { contracts, panelState, onRetry, onSelectTool }: Props = $props();

  const columns: Column[] = [
    { key: 'name', label: 'Tool', sortable: true },
    { key: 'description', label: 'Description' },
    { key: 'output', label: 'Output' },
  ];

  const rows = $derived<Row[]>(
    contracts.map((c) => ({
      name: c.name,
      description: c.description ?? '—',
      output: c.outputSchema?.type ?? 'unknown',
    })),
  );

  function onRowClick(row: Row): void {
    const c = contracts.find((t) => t.name === row.name);
    if (c) onSelectTool?.(c);
  }
</script>

<div class="tools-panel" data-testid="tools-panel">
  <DataTable
    {columns}
    {rows}
    pageState={panelState}
    {onRowClick}
    {onRetry}
    emptyTitle="No tools"
    emptyDescription="The attached server registered no tools. Add a typed Go tool handler and regenerate."
  />
  <p class="tools-note">
    Invocation in the inspector is a localhost dev test answered from the
    active fixture — the inspector is read-only.
  </p>
</div>

<style>
  .tools-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2, 0.5rem);
    min-height: 0;
  }
  .tools-note {
    margin: 0;
    font-size: var(--dy-font-size-xs, 0.75rem);
    color: var(--dy-color-text-muted, #71717a);
  }
</style>
