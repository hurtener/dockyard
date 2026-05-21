<!--
  RailCard — one titled card within a DetailRail. Optionally collapsible.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** The card title. */
    title: string;
    /** When true, the card header toggles the body open/closed. */
    collapsible?: boolean;
    /** Initial open state (collapsible cards only). Default open. */
    open?: boolean;
    /** Optional trailing header slot — e.g. a count chip. */
    accessory?: Snippet;
    /** The card body. */
    children: Snippet;
  }

  let {
    title,
    collapsible = false,
    open = true,
    accessory,
    children,
  }: Props = $props();

  // `open` is an initial-value prop: the card owns its open state after mount.
  // svelte-ignore state_referenced_locally
  let isOpen = $state(open);

  function toggle(): void {
    if (collapsible) isOpen = !isOpen;
  }
</script>

<section class="dy-railcard" data-testid="rail-card">
  {#if collapsible}
    <button
      type="button"
      class="dy-railcard__header dy-railcard__header--button"
      aria-expanded={isOpen}
      onclick={toggle}
    >
      <span class="dy-railcard__title">{title}</span>
      {#if accessory}<span class="dy-railcard__accessory">{@render accessory()}</span>{/if}
      <span class="dy-railcard__chevron" class:dy-railcard__chevron--open={isOpen} aria-hidden="true">›</span>
    </button>
  {:else}
    <div class="dy-railcard__header">
      <span class="dy-railcard__title">{title}</span>
      {#if accessory}<span class="dy-railcard__accessory">{@render accessory()}</span>{/if}
    </div>
  {/if}
  {#if isOpen}
    <div class="dy-railcard__body">{@render children()}</div>
  {/if}
</section>

<style>
  .dy-railcard {
    border-bottom: 1px solid var(--dy-color-border);
    font-family: var(--dy-font-sans);
  }

  .dy-railcard__header {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
    width: 100%;
    padding: var(--dy-space-3) var(--dy-space-4);
  }

  .dy-railcard__header--button {
    border: 0;
    background: transparent;
    cursor: pointer;
    text-align: left;
    font: inherit;
  }

  .dy-railcard__header--button:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }

  .dy-railcard__title {
    flex: 1;
    color: var(--dy-color-ink);
    font-size: var(--dy-text-sm);
    font-weight: var(--dy-weight-semibold);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .dy-railcard__accessory {
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-xs);
  }

  .dy-railcard__chevron {
    color: var(--dy-color-ink-soft);
    transform: rotate(90deg);
    transition: transform 0.15s ease;
  }

  .dy-railcard__chevron--open {
    transform: rotate(-90deg);
  }

  .dy-railcard__body {
    padding: 0 var(--dy-space-4) var(--dy-space-4);
  }
</style>
