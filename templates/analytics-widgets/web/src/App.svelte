<!--
  App.svelte — the single Svelte App the analytics-widgets template ships.

  Reads `structuredContent.kind` (chart | table | metric_card) from the
  `tool-result` notification and dispatches to the right widget renderer.
  Composes web/ui primitives for state and chrome; the only template-local
  component is ChartFrame (ECharts wrapper, decision D-127).

  Theming. The bridge propagates the host's theme via
  `hostContext.styles.variables` (RFC §7.3). The App applies them to its root
  by default; a per-call `theme` override on the tool input pins a specific
  palette. Decisions D-125 / D-127.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { createBridge, type StyleVariables } from 'dockyard-bridge';
  import { PageState } from 'dockyard-ui';
  import type { PageStateValue } from 'dockyard-ui';

  import Chart from './widgets/Chart.svelte';
  import Table from './widgets/Table.svelte';
  import MetricCardWidget from './widgets/MetricCardWidget.svelte';
  import { applyHostVariables, hostThemeHint, resolveTheme, type ThemeMode } from './theme.js';

  type WidgetState =
    | 'ready'
    | 'empty'
    | 'error'
    | 'permission'
    | 'loading';

  type ChartPayload = {
    kind: 'chart';
    type: string;
    data: { series: Array<{ name: string; values: number[] }>; categories?: string[] };
    title?: string;
    options?: Record<string, unknown>;
    theme: ThemeMode;
    state: WidgetState;
    message?: string;
  };
  type TablePayload = {
    kind: 'table';
    columns: Array<{ key: string; label: string; type: string; sortable?: boolean }>;
    rows: Array<Record<string, unknown>>;
    sort?: { column: string; dir: string } | null;
    theme: ThemeMode;
    state: WidgetState;
    message?: string;
  };
  type MetricPayload = {
    kind: 'metric_card';
    label: string;
    value: unknown;
    unit?: string;
    delta?: { value: string; tone: 'ok' | 'warn' | 'error' } | null;
    series?: number[];
    breakdowns?: Array<{ label: string; value: unknown; share?: number }>;
    theme: ThemeMode;
    state: WidgetState;
    message?: string;
  };
  type Payload = ChartPayload | TablePayload | MetricPayload;

  let rootEl: HTMLDivElement | undefined = $state();
  let pageState: PageStateValue = $state('loading');
  let payload: Payload | null = $state(null);
  let resolvedTheme: 'light' | 'dark' = $state('light');
  let message = $state('Waiting for tool result…');

  const bridge = createBridge({ displayModes: ['inline'] });

  // Subscribe to tool-result; the dispatcher reads `kind` and selects the
  // widget. A non-ready state on the payload is forwarded directly to
  // PageState — the host's chat surface always sees a real state, never a
  // blank widget.
  const offResult = bridge.onToolResult<Payload>((r) => {
    if (!r.structuredContent) {
      pageState = 'error';
      message = 'The tool returned no structured payload.';
      payload = null;
      return;
    }
    payload = r.structuredContent;
    pageState = mapState(payload.state);
    message = payload.message ?? '';
    resolvedTheme = resolveTheme(
      payload.theme as ThemeMode,
      hostThemeHint(currentVariables),
    );
  });

  // Apply the host's styles.variables to the App root, and re-apply on every
  // host-context change.
  let currentVariables: StyleVariables | undefined;
  const offHost = bridge.onHostContextChanged((p) => {
    if (p.styles?.variables) {
      currentVariables = p.styles.variables;
      if (rootEl) applyHostVariables(rootEl, currentVariables);
      if (payload) {
        resolvedTheme = resolveTheme(
          payload.theme as ThemeMode,
          hostThemeHint(currentVariables),
        );
      }
    }
  });

  onMount(() => {
    // Kick off the `ui/initialize` handshake against the host. Subscriptions
    // above (onToolResult, onHostContextChanged) were registered against the
    // bridge at module construction so they are live the moment the host
    // dispatches a notification; connect() awaits the initialize round trip.
    // A failed handshake routes to PageState's error slot via the four-state
    // gate, never a silent hang.
    bridge.connect().catch((err: unknown) => {
      pageState = 'error';
      message = `Bridge handshake failed: ${(err as Error)?.message ?? err}`;
    });
    if (rootEl && currentVariables) applyHostVariables(rootEl, currentVariables);
  });

  onDestroy(() => {
    offResult();
    offHost();
    bridge.close();
  });

  function mapState(s: WidgetState): PageStateValue {
    switch (s) {
      case 'ready':
        return 'ready';
      case 'empty':
        return 'empty';
      case 'error':
        return 'error';
      case 'permission':
        return 'error'; // PageState renders the permission slot when provided.
      case 'loading':
      default:
        return 'loading';
    }
  }
</script>

<div bind:this={rootEl} class="analytics-widgets" data-dy-theme={resolvedTheme} data-testid="analytics-widgets">
  <PageState
    state={pageState}
    loadingMessage="Loading…"
    emptyTitle="No data for this period"
    emptyDescription={message}
    errorTitle="Something went wrong"
    errorDescription={message}
    onRetry={() => { pageState = 'loading'; message = 'Retrying…'; }}
  >
    {#if payload?.kind === 'chart'}
      <Chart payload={payload as ChartPayload} theme={resolvedTheme} />
    {:else if payload?.kind === 'table'}
      <Table payload={payload as TablePayload} />
    {:else if payload?.kind === 'metric_card'}
      <MetricCardWidget payload={payload as MetricPayload} />
    {:else}
      <p class="status error">Unknown widget kind.</p>
    {/if}
  </PageState>
</div>

<style>
  .analytics-widgets {
    display: block;
    padding: var(--dy-space-2, 8px);
    font-family: var(--dy-font-sans, system-ui, sans-serif);
    color: var(--dy-color-ink, #1a1d22);
    background: var(--dy-color-canvas, transparent);
  }
  .analytics-widgets[data-dy-theme='dark'] {
    color-scheme: dark;
  }
  .status {
    font-size: var(--dy-text-sm, 0.875rem);
    color: var(--dy-color-ink-soft, #555);
    margin: 0;
  }
  .status.error {
    color: var(--dy-state-error-fg, #b1352c);
  }
</style>
