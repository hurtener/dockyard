<!--
  MetricCardWidget.svelte — the create_metric_card widget renderer.

  Composes web/ui's MetricCard primitive (label / value / delta) plus the new
  shared Sparkline (when a series is present) plus a simple breakdown table
  underneath. The split keeps a single source of truth for the KPI primitive
  (web/ui MetricCard) while letting this template render the richer surface a
  metric card needs (sparkline + breakdowns).
-->
<script lang="ts">
  import { MetricCard, Sparkline, StatusChip } from 'dockyard-ui';
  import type { CreateMetricCardOutput } from '../../../internal/contracts/contracts.js';

  interface Props {
    payload: CreateMetricCardOutput;
  }
  let { payload }: Props = $props();

  let valueText = $derived(
    String(payload.value ?? '') + (payload.unit ? payload.unit : ''),
  );
  let trend = $derived(
    payload.delta?.tone === 'ok'
      ? ('up' as const)
      : payload.delta?.tone === 'error'
        ? ('down' as const)
        : ('flat' as const),
  );
</script>

<section class="metric" data-testid="widget-metric-card">
  <MetricCard
    label={payload.label}
    value={valueText}
    delta={payload.delta?.value}
    {trend}
  />
  {#if payload.series && payload.series.length > 0}
    <div class="metric__sparkline">
      <Sparkline
        values={payload.series}
        ariaLabel={`${payload.label} trend`}
        tone={trend === 'down' ? 'error' : trend === 'up' ? 'ok' : 'neutral'}
      />
    </div>
  {/if}
  {#if payload.breakdowns && payload.breakdowns.length > 0}
    <ul class="metric__breakdown">
      {#each payload.breakdowns as row (row.label)}
        <li class="metric__row">
          <span class="metric__row-label">{row.label}</span>
          <span class="metric__row-value">{row.value}</span>
          {#if row.share != null}
            <StatusChip
              label={`${Math.round(row.share * 100)}%`}
              tone="neutral"
            />
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .metric {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3, 12px);
  }
  .metric__sparkline {
    padding: 0 var(--dy-space-1, 4px);
  }
  .metric__breakdown {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1, 4px);
  }
  .metric__row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    align-items: center;
    gap: var(--dy-space-2, 8px);
    padding: var(--dy-space-1, 4px) 0;
    border-top: 1px solid var(--dy-color-border, #e3e6ea);
    font-size: var(--dy-text-sm, 0.875rem);
  }
  .metric__row:first-child {
    border-top: 0;
  }
  .metric__row-label {
    color: var(--dy-color-ink-soft, #555);
  }
  .metric__row-value {
    color: var(--dy-color-ink, #1a1d22);
    font-variant-numeric: tabular-nums;
  }
</style>
