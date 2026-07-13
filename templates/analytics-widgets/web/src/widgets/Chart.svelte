<!--
  Chart.svelte — the create_chart widget renderer.

  Delegates the heavy lifting to ChartFrame (the template-local ECharts
  wrapper, decision D-127). This file is intentionally thin so a developer
  who wants to swap the renderer (or strip ECharts entirely) only touches
  ChartFrame.
-->
<script lang="ts">
  import type { CreateChartOutput } from '../../../internal/contracts/contracts.js';

  import ChartFrame from './ChartFrame.svelte';

  interface Props {
    payload: CreateChartOutput;
    theme: 'light' | 'dark';
  }
  let { payload, theme }: Props = $props();
</script>

<section class="chart" data-testid="widget-chart">
  {#if payload.title}
    <h3 class="chart__title">{payload.title}</h3>
  {/if}
  <ChartFrame
    type={payload.type}
    data={payload.data}
    options={payload.options}
    theme={theme}
  />
</section>

<style>
  .chart {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2, 8px);
  }
  .chart__title {
    margin: 0;
    font-size: var(--dy-text-md, 1rem);
    font-weight: var(--dy-weight-semibold, 600);
    color: var(--dy-color-ink, #1a1d22);
  }
</style>
