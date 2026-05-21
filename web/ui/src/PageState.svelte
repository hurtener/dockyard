<!--
  PageState — the four-state async wrapper (CONVENTIONS.md §4, AGENTS.md §20).

  Every async region in every Dockyard surface routes through this component. It
  renders EXACTLY ONE of: loading / empty / error / ready. The empty and error
  panels are mandatory and carry real copy + a working action — that rule is
  enforced here by construction: an `error` state without `onretry` still renders
  ErrorState, and the four-state branching cannot be bypassed.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { PageStateValue } from './types.js';
  import LoadingState from './LoadingState.svelte';
  import EmptyState from './EmptyState.svelte';
  import ErrorState from './ErrorState.svelte';

  interface Props {
    /** Which of the four states to render. */
    state: PageStateValue;
    /** The ready content — rendered only when `state === 'ready'`. */
    children: Snippet;
    /** Message for the loading panel. */
    loadingMessage?: string;
    /** Headline for the empty panel — real copy. */
    emptyTitle?: string;
    /** Supporting copy for the empty panel. */
    emptyDescription?: string;
    /** Action label for the empty panel. */
    emptyActionLabel?: string;
    /** Empty-panel action callback. */
    onEmptyAction?: () => void;
    /** Headline for the error panel. */
    errorTitle?: string;
    /** Supporting copy for the error panel. */
    errorDescription?: string;
    /** Retry callback for the error panel — the load-bearing affordance. */
    onRetry?: () => void;
  }

  let {
    state,
    children,
    loadingMessage = 'Loading…',
    emptyTitle = 'Nothing here yet',
    emptyDescription,
    emptyActionLabel,
    onEmptyAction,
    errorTitle = 'Something went wrong',
    errorDescription,
    onRetry,
  }: Props = $props();
</script>

<div class="dy-page-state" data-state={state} data-testid="page-state">
  {#if state === 'loading'}
    <LoadingState message={loadingMessage} />
  {:else if state === 'empty'}
    <EmptyState
      title={emptyTitle}
      description={emptyDescription}
      actionLabel={emptyActionLabel}
      onaction={onEmptyAction}
    />
  {:else if state === 'error'}
    <ErrorState
      title={errorTitle}
      description={errorDescription}
      onretry={onRetry}
    />
  {:else}
    {@render children()}
  {/if}
</div>

<style>
  .dy-page-state {
    display: contents;
  }
</style>
