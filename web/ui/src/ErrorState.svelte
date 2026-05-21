<!--
  ErrorState — the error panel of the four-state PageState family.
  CONVENTIONS.md §4: the error state is MANDATORY and carries a WORKING retry,
  never a dead end. The retry callback is the load-bearing prop.
-->
<script lang="ts">
  interface Props {
    /** The error headline — real copy. */
    title?: string;
    /** A human-readable explanation of what failed. */
    description?: string;
    /** Retry button label. */
    retryLabel?: string;
    /** Invoked when retry is pressed. When omitted, no retry button renders. */
    onretry?: () => void;
  }

  let {
    title = 'Something went wrong',
    description,
    retryLabel = 'Retry',
    onretry,
  }: Props = $props();
</script>

<div class="dy-state" role="alert" data-testid="error-state">
  <p class="dy-state__title">{title}</p>
  {#if description}
    <p class="dy-state__description">{description}</p>
  {/if}
  {#if onretry}
    <button type="button" class="dy-state__retry" onclick={onretry}>
      {retryLabel}
    </button>
  {/if}
</div>

<style>
  .dy-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--dy-space-2);
    padding: var(--dy-space-7) var(--dy-space-5);
    font-family: var(--dy-font-sans);
    text-align: center;
  }

  .dy-state__title {
    margin: 0;
    color: var(--dy-state-error-fg);
    font-size: var(--dy-text-md);
    font-weight: var(--dy-weight-semibold);
  }

  .dy-state__description {
    margin: 0;
    max-width: 42ch;
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-base);
    line-height: var(--dy-line-normal);
  }

  .dy-state__retry {
    margin-top: var(--dy-space-2);
    padding: var(--dy-space-2) var(--dy-space-4);
    border: 1px solid var(--dy-color-primary);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-primary);
    color: var(--dy-color-surface);
    font-family: inherit;
    font-size: var(--dy-text-base);
    font-weight: var(--dy-weight-medium);
    cursor: pointer;
  }

  .dy-state__retry:hover {
    background: var(--dy-color-primary-strong);
    border-color: var(--dy-color-primary-strong);
  }

  .dy-state__retry:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }
</style>
