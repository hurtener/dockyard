<!--
  Sparkline — a small, token-driven, pure-SVG inline chart.

  The reusable primitive added to the shared web/ui inventory in Phase 24
  (decision D-127). Composable inside MetricCard, the inspector, the docs
  site, and any template that wants a thin trend visual. Pure SVG, no
  third-party dependency — wrappers around fat libraries are
  template-local (CLAUDE.md §20).

  The component is responsive: width defaults to 100% of the container, and
  the rendered viewBox preserves the aspect ratio at any size. An all-zero
  or single-point series renders a centred horizontal baseline rather than
  dividing by zero.
-->
<script lang="ts">
  import type { StatusTone } from './types.js';

  interface Props {
    /** The series of numeric values to plot. */
    values: number[];
    /** Rendered viewBox width in SVG units. Default 120. */
    width?: number;
    /** Rendered viewBox height in SVG units. Default 32. */
    height?: number;
    /**
     * Semantic tone — colours the line via the matching state token. The
     * `neutral` default uses the primary token so the sparkline reads as
     * informational, not stately.
     */
    tone?: StatusTone;
    /**
     * Accessible label for the SVG root. Required content-wise — every
     * sparkline carries one — but defaulted so a missing label does not
     * crash the renderer.
     */
    ariaLabel?: string;
    /**
     * Stroke width in SVG units. The line is drawn over a baseline at the
     * value range's lower bound; thicker strokes read better at large
     * heights.
     */
    strokeWidth?: number;
  }

  let {
    values,
    width = 120,
    height = 32,
    tone = 'neutral',
    ariaLabel = 'Trend over time',
    strokeWidth = 1.5,
  }: Props = $props();

  // The viewBox padding leaves room for the stroke so the line never clips
  // at the edges. Half a stroke is conservative; one full stroke is safe
  // for line caps too.
  const padX = $derived(strokeWidth);
  const padY = $derived(strokeWidth);

  const innerWidth = $derived(Math.max(1, width - 2 * padX));
  const innerHeight = $derived(Math.max(1, height - 2 * padY));

  // Resolve the y-range; a constant series (max == min) renders a baseline
  // with no division-by-zero.
  const range = $derived.by(() => {
    if (!values || values.length === 0) return { min: 0, max: 0, span: 0 };
    let min = values[0]!;
    let max = values[0]!;
    for (const v of values) {
      if (v < min) min = v;
      if (v > max) max = v;
    }
    return { min, max, span: max - min };
  });

  // Build the path from the value series. `M x,y L x,y L …`. With one point
  // the path is `M x,y` (a single moveto); with no points the path is the
  // empty string and the renderer falls back to a baseline.
  const path = $derived.by(() => {
    if (!values || values.length === 0) return '';
    if (values.length === 1) {
      const x = padX + innerWidth / 2;
      const y = padY + innerHeight / 2;
      return `M ${x},${y}`;
    }
    const { min, span } = range;
    const step = innerWidth / Math.max(1, values.length - 1);
    const parts: string[] = [];
    for (let i = 0; i < values.length; i++) {
      const x = padX + step * i;
      // y in SVG grows downward, so a higher value should render higher;
      // when span is 0 we centre the baseline.
      const normalised = span === 0 ? 0.5 : (values[i]! - min) / span;
      const y = padY + innerHeight * (1 - normalised);
      parts.push(`${i === 0 ? 'M' : 'L'} ${x.toFixed(2)},${y.toFixed(2)}`);
    }
    return parts.join(' ');
  });

  // The flat-baseline fallback when there is no path (no data) or only one
  // point — rendered as a thin horizontal line so the sparkline never
  // collapses to invisible content.
  const baseline = $derived(`M ${padX},${padY + innerHeight / 2} L ${
    padX + innerWidth
  },${padY + innerHeight / 2}`);

  const showBaseline = $derived(!values || values.length <= 1);
</script>

<svg
  class="dy-sparkline"
  data-testid="sparkline"
  data-tone={tone}
  viewBox={`0 0 ${width} ${height}`}
  width="100%"
  height={height}
  preserveAspectRatio="none"
  role="img"
  aria-label={ariaLabel}
>
  {#if showBaseline}
    <path class="dy-sparkline__baseline" d={baseline} fill="none" stroke-width={strokeWidth} />
  {:else}
    <path
      class="dy-sparkline__line"
      d={path}
      fill="none"
      stroke-width={strokeWidth}
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  {/if}
</svg>

<style>
  .dy-sparkline {
    display: block;
    color: var(--dy-color-primary);
  }
  .dy-sparkline[data-tone='ok'] {
    color: var(--dy-state-ok-fg);
  }
  .dy-sparkline[data-tone='warn'] {
    color: var(--dy-state-warn-fg);
  }
  .dy-sparkline[data-tone='error'] {
    color: var(--dy-state-error-fg);
  }
  .dy-sparkline[data-tone='info'] {
    color: var(--dy-state-info-fg);
  }
  .dy-sparkline[data-tone='neutral'] {
    color: var(--dy-color-primary);
  }
  .dy-sparkline__line {
    stroke: currentColor;
  }
  .dy-sparkline__baseline {
    stroke: var(--dy-color-border);
    stroke-dasharray: 2 3;
  }
</style>
