<!--
  StatusChip — a small state pill. The tone maps to a semantic state token trio
  (ok / warn / error / info) or the neutral palette. Used by the inspector's
  Verdicts panel and anywhere a compact status is shown.
-->
<script lang="ts">
  import type { StatusTone } from './types.js';

  interface Props {
    /** The chip text. */
    label: string;
    /** The semantic tone. Default `neutral`. */
    tone?: StatusTone;
    /** When true, render a leading status dot. */
    dot?: boolean;
  }

  let { label, tone = 'neutral', dot = false }: Props = $props();
</script>

<span class="dy-chip" data-tone={tone} data-testid="status-chip">
  {#if dot}<span class="dy-chip__dot" aria-hidden="true"></span>{/if}
  {label}
</span>

<style>
  .dy-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--dy-space-1);
    padding: 2px var(--dy-space-2);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-full);
    background: var(--dy-color-canvas);
    color: var(--dy-color-ink-soft);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-xs);
    font-weight: var(--dy-weight-medium);
    white-space: nowrap;
  }

  .dy-chip__dot {
    width: 6px;
    height: 6px;
    border-radius: var(--dy-radius-full);
    background: currentColor;
  }

  .dy-chip[data-tone='ok'] {
    background: var(--dy-state-ok-bg);
    border-color: var(--dy-state-ok-border);
    color: var(--dy-state-ok-fg);
  }

  .dy-chip[data-tone='warn'] {
    background: var(--dy-state-warn-bg);
    border-color: var(--dy-state-warn-border);
    color: var(--dy-state-warn-fg);
  }

  .dy-chip[data-tone='error'] {
    background: var(--dy-state-error-bg);
    border-color: var(--dy-state-error-border);
    color: var(--dy-state-error-fg);
  }

  .dy-chip[data-tone='info'] {
    background: var(--dy-state-info-bg);
    border-color: var(--dy-state-info-border);
    color: var(--dy-state-info-fg);
  }
</style>
