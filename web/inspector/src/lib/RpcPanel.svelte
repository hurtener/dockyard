<script lang="ts">
  /**
   * RpcPanel — the inspector DetailRail's RPC tab.
   *
   * Renders the JSON-RPC message log (`tools/call`, `resources/read`, `ui/*`)
   * as a web/ui `DataTable`, method-filterable, with the selected message's
   * payload in a web/ui `JsonInspector`. Routes through the four-state
   * `PageState` — a real empty state, an error state with retry. Composes
   * `@dockyard/ui` only.
   */
  import {
    PageState,
    DataTable,
    JsonInspector,
    FilterBar,
    StatusChip,
    type PageStateValue,
    type Column,
    type Row,
  } from '@dockyard/ui';
  import type { RpcEntry } from './rpc.js';
  import { methodsIn, filterByMethod } from './rpc.js';

  interface Props {
    /** The JSON-RPC log entries, oldest first. */
    entries: RpcEntry[];
    /** The log-load state — drives the four-state PageState. */
    logState: PageStateValue;
    /** Called when the user retries a failed log load. */
    onRetry?: () => void;
  }

  let { entries, logState, onRetry }: Props = $props();

  let activeMethods = $state<string[]>([]);
  let selectedId = $state<string | undefined>(undefined);

  const methods = $derived(methodsIn(entries));
  const filtered = $derived(filterByMethod(entries, activeMethods));
  const selected = $derived(entries.find((e) => e.id === selectedId));

  const columns: Column[] = [
    { key: 'direction', label: 'Dir' },
    { key: 'method', label: 'Method', sortable: true },
    { key: 'time', label: 'Time' },
  ];

  const rows = $derived<Row[]>(
    filtered.map((e) => ({
      id: e.id,
      direction: e.direction === 'inbound' ? '→ server' : '← server',
      method: e.method ?? '(response)',
      time: new Date(e.at).toLocaleTimeString(),
    })),
  );

  const filterChips = $derived(methods.map((m) => ({ id: m, label: m })));

  function onRowSelect(row: Row): void {
    selectedId = typeof row.id === 'string' ? row.id : undefined;
  }
</script>

<div class="rpc-panel" data-testid="rpc-panel">
  <PageState
    state={logState}
    emptyTitle="No RPC traffic yet"
    emptyDescription="No JSON-RPC messages yet — call a tool or render an App to see traffic."
    errorTitle="RPC log unavailable"
    errorDescription="The JSON-RPC log could not be loaded. Retry."
    onRetry={onRetry}
  >
    <div class="rpc-meta">
      <StatusChip label={`${entries.length} messages`} tone="info" />
    </div>
    {#if filterChips.length > 0}
      <FilterBar
        filters={filterChips}
        active={activeMethods}
        placeholder="Filter by method"
        onfilter={(active) => (activeMethods = active)}
      />
    {/if}
    <DataTable {columns} {rows} onRowClick={onRowSelect} />
    {#if selected}
      <div class="rpc-detail" data-testid="rpc-detail">
        <h3>{selected.method ?? 'response'}</h3>
        <JsonInspector value={selected.payload} name="payload" />
      </div>
    {/if}
  </PageState>
</div>

<style>
  .rpc-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3, 0.75rem);
    min-height: 0;
  }
  .rpc-meta {
    display: flex;
    gap: var(--dy-space-2, 0.5rem);
  }
  .rpc-detail h3 {
    margin: 0 0 var(--dy-space-2, 0.5rem);
    font-size: var(--dy-font-size-sm, 0.875rem);
    font-family: var(--dy-font-mono, monospace);
  }
</style>
