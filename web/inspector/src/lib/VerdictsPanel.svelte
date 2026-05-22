<script lang="ts">
  /**
   * VerdictsPanel — the inspector DetailRail's Verdicts tab.
   *
   * Surfaces contract-drift, schema-validation, and spec-compliance results
   * (RFC §12) as `StatusChip` rows. The verdicts are produced by the backend's
   * `/api/verdicts` endpoint, which re-runs `internal/validate.Run` — the same
   * `dockyard validate` engine — so the inspector never reimplements the
   * checks. The panel routes through the four-state `PageState` (CLAUDE.md
   * §20): a real empty state and an error state with a working retry. Every
   * component is composed from `@dockyard/ui`.
   */
  import {
    PageState,
    StatusChip,
    type PageStateValue,
    type StatusTone,
  } from '@dockyard/ui';
  import type { VerdictRow } from './api.js';

  interface Props {
    /** The verdict rows from the backend. */
    verdicts: VerdictRow[];
    /** The fetch state — drives the four-state PageState. */
    panelState: PageStateValue;
    /** Called when the user retries a failed verdicts fetch. */
    onRetry?: () => void;
  }

  let { verdicts, panelState, onRetry }: Props = $props();

  /** Maps a backend verdict severity onto a StatusChip tone. */
  function toneFor(severity: string): StatusTone {
    switch (severity) {
      case 'ok':
        return 'ok';
      case 'error':
        return 'error';
      case 'warn':
      default:
        return 'warn';
    }
  }
</script>

<div class="verdicts-panel" data-testid="verdicts-panel">
  <PageState
    state={panelState}
    emptyTitle="No verdicts yet"
    emptyDescription="Attach a project so the inspector can run contract-drift, schema, and spec-compliance checks."
    errorTitle="Verdicts unavailable"
    errorDescription="The inspector could not run the quality checks. Retry."
    onRetry={onRetry}
  >
    <ul class="verdict-list">
      {#each verdicts as verdict, i (`${verdict.check}-${i}`)}
        <li class="verdict-row" data-testid="verdict-row">
          <StatusChip label={verdict.check} tone={toneFor(verdict.severity)} dot />
          <span class="verdict-message">{verdict.message}</span>
        </li>
      {/each}
    </ul>
  </PageState>
</div>

<style>
  .verdicts-panel {
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .verdict-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
  }
  .verdict-row {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
  }
  .verdict-message {
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink);
  }
</style>
