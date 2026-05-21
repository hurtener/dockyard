<!--
  MetricCard — one KPI: a label, a value, and an optional delta/trend. Used by
  analytical surfaces and the inspector's count summaries.
-->
<script lang="ts">
  interface Props {
    /** The metric name. */
    label: string;
    /** The metric value — rendered as text. */
    value: string | number;
    /** Optional delta string, e.g. "+12%" or "-3". */
    delta?: string;
    /** The delta's direction — colours the delta. Default `flat`. */
    trend?: 'up' | 'down' | 'flat';
  }

  let { label, value, delta, trend = 'flat' }: Props = $props();
</script>

<div class="dy-metric" data-testid="metric-card">
  <span class="dy-metric__label">{label}</span>
  <span class="dy-metric__value">{value}</span>
  {#if delta}
    <span class="dy-metric__delta" data-trend={trend}>{delta}</span>
  {/if}
</div>

<style>
  .dy-metric {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1);
    padding: var(--dy-space-4);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
    box-shadow: var(--dy-elevation-raised);
    font-family: var(--dy-font-sans);
  }

  .dy-metric__label {
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-xs);
    font-weight: var(--dy-weight-medium);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .dy-metric__value {
    color: var(--dy-color-ink);
    font-size: var(--dy-text-xl);
    font-weight: var(--dy-weight-semibold);
    line-height: var(--dy-line-tight);
  }

  .dy-metric__delta {
    font-size: var(--dy-text-sm);
    font-weight: var(--dy-weight-medium);
    color: var(--dy-color-ink-soft);
  }

  .dy-metric__delta[data-trend='up'] {
    color: var(--dy-state-ok-fg);
  }

  .dy-metric__delta[data-trend='down'] {
    color: var(--dy-state-error-fg);
  }
</style>
