<script lang="ts">
  /**
   * AnalyticsPanel — per-tool latency / error / volume analytics.
   *
   * Derived purely from the `obs/v1` event stream the inspector consumes (P2 —
   * `analytics.ts`): a `MetricCard` row of aggregate totals and a `DataTable`
   * of per-tool latency / error rate / call volume. Routes through the
   * four-state `PageState`; composes only `dockyard-ui`.
   */
  import {
    PageState,
    MetricCard,
    DataTable,
    type Column,
    type PageStateValue,
    type Row,
  } from 'dockyard-ui';
  import type { ObsEvent } from './obs.js';
  import { foldAnalytics, totalsOf, formatRate } from './analytics.js';

  interface Props {
    /** The obs/v1 events received so far, oldest first. */
    events: ObsEvent[];
    /** The stream connection state — drives the four-state PageState. */
    streamState: PageStateValue;
    /** Called when the user retries a failed stream connection. */
    onRetry?: () => void;
  }

  let { events, streamState, onRetry }: Props = $props();

  const rows = $derived(foldAnalytics(events));
  const totals = $derived(totalsOf(rows));
  const panelState = $derived<PageStateValue>(
    streamState === 'error'
      ? 'error'
      : streamState === 'loading'
        ? 'loading'
        : rows.length === 0
          ? 'empty'
          : 'ready',
  );

  const columns: Column[] = [
    { key: 'tool', label: 'Tool', sortable: true },
    { key: 'calls', label: 'Calls', sortable: true, align: 'right' },
    { key: 'errorRate', label: 'Error rate', sortable: true, align: 'right' },
    { key: 'avgLatencyMs', label: 'Avg ms', sortable: true, align: 'right' },
    { key: 'maxLatencyMs', label: 'Max ms', sortable: true, align: 'right' },
  ];

  const tableRows = $derived<Row[]>(
    rows.map((r) => ({
      tool: r.tool,
      calls: r.calls,
      errorRate: formatRate(r.errorRate),
      avgLatencyMs: r.avgLatencyMs,
      maxLatencyMs: r.maxLatencyMs,
    })),
  );
</script>

<div class="analytics-panel" data-testid="analytics-panel">
  <PageState
    state={panelState}
    emptyTitle="No tool calls yet"
    emptyDescription="No tool.call events observed — call a tool to see per-tool latency, error rate, and volume."
    errorTitle="Stream disconnected"
    errorDescription="The obs/v1 event stream is unavailable. Retry the connection."
    onRetry={onRetry}
  >
    <div class="metrics">
      <MetricCard label="Total calls" value={totals.calls} />
      <MetricCard label="Errored" value={totals.errors} />
      <MetricCard label="Error rate" value={formatRate(totals.errorRate)} />
      <MetricCard label="Avg latency" value={`${totals.avgLatencyMs} ms`} />
    </div>
    <DataTable {columns} rows={tableRows} pageState="ready" />
  </PageState>
</div>

<style>
  .analytics-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    min-height: 0;
  }
  .metrics {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--dy-space-2);
  }
</style>
