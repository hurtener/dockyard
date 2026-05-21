<!--
  EmptyState — the empty panel of the four-state PageState family.
  CONVENTIONS.md §4: the empty state is MANDATORY and carries real copy plus an
  optional action affordance. An empty region with no copy is a defect.
-->
<script lang="ts">
  interface Props {
    /** The headline — what is empty. Required, real copy (not a placeholder). */
    title: string;
    /** Optional supporting line — what to do about it. */
    description?: string;
    /** Optional action button label. When set, `onaction` is wired. */
    actionLabel?: string;
    /** Invoked when the action button is pressed. */
    onaction?: () => void;
  }

  let { title, description, actionLabel, onaction }: Props = $props();
</script>

<div class="dy-state" data-testid="empty-state">
  <p class="dy-state__title">{title}</p>
  {#if description}
    <p class="dy-state__description">{description}</p>
  {/if}
  {#if actionLabel && onaction}
    <button type="button" class="dy-state__action" onclick={onaction}>
      {actionLabel}
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
    color: var(--dy-color-ink);
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

  .dy-state__action {
    margin-top: var(--dy-space-2);
    padding: var(--dy-space-2) var(--dy-space-4);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
    font-family: inherit;
    font-size: var(--dy-text-base);
    font-weight: var(--dy-weight-medium);
    cursor: pointer;
  }

  .dy-state__action:hover {
    border-color: var(--dy-color-primary);
    color: var(--dy-color-primary-strong);
  }

  .dy-state__action:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }
</style>
