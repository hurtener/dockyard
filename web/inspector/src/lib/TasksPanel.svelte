<script lang="ts">
  /**
   * TasksPanel — the inspector DetailRail's Tasks tab.
   *
   * Renders the MCP Tasks five-status lifecycle and `input_required`
   * round-trips (RFC §12, §8.6) as a web/ui `Timeline`. The lifecycle is
   * folded from the `obs/v1` `task.progress` event stream the inspector
   * already consumes (P2 — `tasks.ts`). Routes through the four-state
   * `PageState`; composes only `@dockyard/ui`.
   */
  import {
    PageState,
    Timeline,
    StatusChip,
    type PageStateValue,
  } from '@dockyard/ui';
  import type { ObsEvent } from './obs.js';
  import {
    foldTasks,
    lifecycleToTimeline,
    toneForStatus,
    isTerminal,
  } from './tasks.js';

  interface Props {
    /** The obs/v1 events received so far, oldest first. */
    events: ObsEvent[];
    /** The stream connection state — drives the four-state PageState. */
    streamState: PageStateValue;
    /** Called when the user retries a failed stream connection. */
    onRetry?: () => void;
  }

  let { events, streamState, onRetry }: Props = $props();

  const lifecycles = $derived(foldTasks(events));
  // The Tasks panel is empty until a task.progress event arrives, even when
  // the obs stream itself is ready — route the four-state on task presence.
  const panelState = $derived<PageStateValue>(
    streamState === 'error'
      ? 'error'
      : streamState === 'loading'
        ? 'loading'
        : lifecycles.length === 0
          ? 'empty'
          : 'ready',
  );
</script>

<div class="tasks-panel" data-testid="tasks-panel">
  <PageState
    state={panelState}
    emptyTitle="No tasks yet"
    emptyDescription="No task lifecycle observed — call a task-returning tool to see its working / input_required / completed transitions here."
    errorTitle="Stream disconnected"
    errorDescription="The obs/v1 task stream is unavailable. Retry the connection."
    onRetry={onRetry}
  >
    <div class="task-list">
      {#each lifecycles as life (life.taskId)}
        <div class="task" data-testid="task-lifecycle">
          <div class="task-head">
            <span class="task-id">{life.taskId}</span>
            <StatusChip
              label={life.current}
              tone={toneForStatus(life.current)}
              dot
            />
            {#if life.awaitingInput}
              <StatusChip label="awaiting input" tone="info" />
            {:else if isTerminal(life.current)}
              <StatusChip label="terminal" tone="neutral" />
            {/if}
          </div>
          <Timeline events={lifecycleToTimeline(life)} />
        </div>
      {/each}
    </div>
  </PageState>
</div>

<style>
  .tasks-panel {
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .task-list {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-4);
  }
  .task-head {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
    margin-bottom: var(--dy-space-2);
  }
  .task-id {
    font-family: var(--dy-font-mono);
    font-size: var(--dy-text-sm);
    font-weight: 600;
  }
</style>
