<script lang="ts">
  /**
   * EventsPanel — the inspector DetailRail's Events tab.
   *
   * Renders the live obs/v1 event stream as a web/ui `Timeline`, with the
   * selected event's full payload in a web/ui `JsonInspector`. The panel routes
   * through the four-state `PageState` (CLAUDE.md §20): a real empty state
   * ("No events yet — call a tool to see traffic") and an error state with a
   * working retry. Every component here is composed from `dockyard-ui` — none
   * is re-implemented.
   */
  import {
    PageState,
    Timeline,
    JsonInspector,
    FilterBar,
    type PageStateValue,
    type TimelineEvent,
  } from 'dockyard-ui';
  import type { ObsEvent } from './obs.js';
  import { toTimelineEvent, kindsIn, filterByKind } from './timeline.js';

  interface Props {
    /** The obs/v1 events received so far, oldest first. */
    events: ObsEvent[];
    /** The stream connection state — drives the four-state PageState. */
    streamState: PageStateValue;
    /** Called when the user retries a failed stream connection. */
    onRetry?: () => void;
  }

  let { events, streamState, onRetry }: Props = $props();

  let activeKinds = $state<string[]>([]);
  let selectedId = $state<string | undefined>(undefined);

  const kinds = $derived(kindsIn(events));
  const filtered = $derived(filterByKind(events, activeKinds));
  const timelineEvents = $derived<TimelineEvent[]>(
    filtered.map(toTimelineEvent),
  );
  const selected = $derived(
    events.find((e) => e.id === selectedId) ?? filtered[filtered.length - 1],
  );

  const filterChips = $derived(
    kinds.map((k) => ({ id: k, label: k })),
  );

  function onSelect(ev: TimelineEvent): void {
    selectedId = ev.id;
  }
</script>

<div class="events-panel" data-testid="events-panel">
  <PageState
    state={streamState}
    emptyTitle="No events yet"
    emptyDescription="No events yet — call a tool to see traffic."
    errorTitle="Stream disconnected"
    errorDescription="The obs/v1 event stream is unavailable. Retry the connection."
    onRetry={onRetry}
  >
    {#if filterChips.length > 0}
      <FilterBar
        filters={filterChips}
        active={activeKinds}
        placeholder="Filter events by kind"
        onfilter={(active) => (activeKinds = active)}
      />
    {/if}
    <div class="events-body">
      <Timeline events={timelineEvents} onselect={onSelect} />
      {#if selected}
        <div class="event-detail" data-testid="event-detail">
          <h3>Event detail</h3>
          <JsonInspector value={selected} name={selected.kind} />
        </div>
      {/if}
    </div>
  </PageState>
</div>

<style>
  .events-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    min-height: 0;
  }
  .events-body {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
  }
  .event-detail h3 {
    margin: 0 0 var(--dy-space-2);
    font-size: var(--dy-text-sm);
  }
</style>
