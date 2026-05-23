<!--
  ChartFrame.svelte — the template-local Apache ECharts wrapper.

  Lives in the template (not web/ui) because wrappers around third-party fat
  libraries do not belong in the shared inventory (CLAUDE.md §20, decision
  D-127). Owns: ECharts setup, responsive resizing via ResizeObserver, theme
  propagation, and cleanup-on-unmount.

  The chart options are derived from the friendly shorthand
  (`type` + `data` + optional `options`): Dockyard builds sensible ECharts
  defaults from the type + data, then merges any caller-provided `options`
  over the top so a developer can override anything ECharts supports without
  having to re-state the basics.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import * as echarts from 'echarts';

  type Series = { name: string; values: number[] };
  type ChartData = { series: Series[]; categories?: string[] };

  interface Props {
    type: string;
    data: ChartData;
    options?: Record<string, unknown>;
    theme: 'light' | 'dark';
  }
  let { type, data, options, theme }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let chart: echarts.ECharts | undefined;
  let observer: ResizeObserver | undefined;

  function buildOptions(): Record<string, unknown> {
    const base: Record<string, unknown> = {
      tooltip: { trigger: 'item' },
      legend: { show: data.series.length > 1 },
      grid: { left: 40, right: 16, top: 24, bottom: 32 },
    };
    if (type !== 'pie' && type !== 'radar' && data.categories) {
      base.xAxis = { type: 'category', data: data.categories };
      base.yAxis = { type: 'value' };
    }
    const seriesType = type === 'area' ? 'line' : type;
    base.series = data.series.map((s) => {
      const entry: Record<string, unknown> = {
        name: s.name,
        type: seriesType,
        data:
          type === 'pie'
            ? s.values.map((v, i) => ({ value: v, name: data.categories?.[i] ?? `#${i + 1}` }))
            : s.values,
      };
      if (type === 'area') entry.areaStyle = {};
      return entry;
    });
    return { ...base, ...(options ?? {}) };
  }

  onMount(() => {
    if (!container) return;
    chart = echarts.init(container, theme === 'dark' ? 'dark' : undefined);
    chart.setOption(buildOptions());
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(() => chart?.resize());
      observer.observe(container);
    }
  });

  // Re-render when props change.
  $effect(() => {
    if (!chart || !container) return;
    chart.setOption(buildOptions(), true);
  });

  onDestroy(() => {
    observer?.disconnect();
    chart?.dispose();
  });
</script>

<div bind:this={container} class="chart-frame" data-testid="chart-frame"></div>

<style>
  .chart-frame {
    width: 100%;
    height: 240px;
    min-height: 200px;
  }
</style>
