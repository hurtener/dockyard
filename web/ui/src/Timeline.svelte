<!--
  Timeline — an ordered sequence of timestamped events. Used by the inspector's
  Events panel (the obs/v1 stream) and the Tasks panel (task lifecycle).
  An empty list renders nothing; the caller wraps Timeline in PageState to show
  the mandatory empty state.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { TimelineEvent } from './types.js';

  interface Props {
    /** The events, in display order. */
    events: TimelineEvent[];
    /** Optional per-event detail snippet — receives the event. */
    detail?: Snippet<[TimelineEvent]>;
    /** Fired when an event row is activated. */
    onselect?: (event: TimelineEvent) => void;
  }

  let { events, detail, onselect }: Props = $props();
</script>

<ol class="dy-timeline" data-testid="timeline">
  {#each events as event (event.id)}
    <li class="dy-timeline__item">
      <span
        class="dy-timeline__marker"
        data-tone={event.tone ?? 'neutral'}
        aria-hidden="true"
      ></span>
      <div class="dy-timeline__body">
        <button
          type="button"
          class="dy-timeline__row"
          onclick={() => onselect?.(event)}
        >
          <span class="dy-timeline__title">{event.title}</span>
          <span class="dy-timeline__time">{event.timestamp}</span>
        </button>
        {#if event.detail}
          <p class="dy-timeline__detail">{event.detail}</p>
        {/if}
        {#if detail}
          <div class="dy-timeline__slot">{@render detail(event)}</div>
        {/if}
      </div>
    </li>
  {/each}
</ol>

<style>
  .dy-timeline {
    margin: 0;
    padding: 0;
    list-style: none;
    font-family: var(--dy-font-sans);
  }

  .dy-timeline__item {
    display: flex;
    gap: var(--dy-space-3);
    padding: var(--dy-space-2) 0;
  }

  .dy-timeline__marker {
    flex-shrink: 0;
    width: 10px;
    height: 10px;
    margin-top: 4px;
    border-radius: var(--dy-radius-full);
    background: var(--dy-color-ink-soft);
  }

  .dy-timeline__marker[data-tone='ok'] {
    background: var(--dy-state-ok-fg);
  }
  .dy-timeline__marker[data-tone='warn'] {
    background: var(--dy-state-warn-fg);
  }
  .dy-timeline__marker[data-tone='error'] {
    background: var(--dy-state-error-fg);
  }
  .dy-timeline__marker[data-tone='info'] {
    background: var(--dy-state-info-fg);
  }

  .dy-timeline__body {
    flex: 1;
    min-width: 0;
  }

  .dy-timeline__row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--dy-space-3);
    width: 100%;
    border: 0;
    background: transparent;
    padding: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .dy-timeline__row:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
    border-radius: var(--dy-radius-sm);
  }

  .dy-timeline__title {
    color: var(--dy-color-ink);
    font-size: var(--dy-text-base);
    font-weight: var(--dy-weight-medium);
  }

  .dy-timeline__time {
    flex-shrink: 0;
    color: var(--dy-color-ink-soft);
    font-family: var(--dy-font-mono);
    font-size: var(--dy-text-xs);
  }

  .dy-timeline__detail {
    margin: var(--dy-space-1) 0 0;
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-sm);
  }

  .dy-timeline__slot {
    margin-top: var(--dy-space-2);
  }
</style>
