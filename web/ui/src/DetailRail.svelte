<!--
  DetailRail — the right-hand rail container. Holds stacked RailCards, or, when
  `tabs` is given, a tab strip that switches between named panels. The inspector
  (design-spec.md §4.2) uses the tabbed form: Events | RPC | Fixtures | …
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** Tab labels. When omitted, the rail simply stacks `children`. */
    tabs?: string[];
    /** The index of the initially active tab. Default 0. */
    active?: number;
    /** Fired when the active tab changes. */
    onTabChange?: (index: number) => void;
    /**
     * The rail body. When `tabs` is set this is a snippet taking the active
     * index; otherwise a plain snippet of stacked RailCards.
     */
    children: Snippet<[number]> | Snippet;
  }

  let { tabs, active = 0, onTabChange, children }: Props = $props();

  // `active` is an initial-value prop: the rail owns the active tab after mount.
  // svelte-ignore state_referenced_locally
  let current = $state(active);

  function select(index: number): void {
    current = index;
    onTabChange?.(index);
  }

  // Narrow the children snippet to whichever arity the consumer passed.
  const tabbedChildren = $derived(children as Snippet<[number]>);
  const plainChildren = $derived(children as Snippet);
</script>

<div class="dy-rail" data-testid="detail-rail">
  {#if tabs && tabs.length > 0}
    <div class="dy-rail__tabs" role="tablist">
      {#each tabs as label, i (label)}
        <button
          type="button"
          role="tab"
          aria-selected={current === i}
          class="dy-rail__tab"
          class:dy-rail__tab--active={current === i}
          onclick={() => select(i)}
        >
          {label}
        </button>
      {/each}
    </div>
    <div class="dy-rail__panel" role="tabpanel">
      {@render tabbedChildren(current)}
    </div>
  {:else}
    <div class="dy-rail__stack">{@render plainChildren()}</div>
  {/if}
</div>

<style>
  .dy-rail {
    display: flex;
    flex-direction: column;
    height: 100%;
    font-family: var(--dy-font-sans);
  }

  .dy-rail__tabs {
    display: flex;
    flex-wrap: wrap;
    gap: var(--dy-space-1);
    padding: var(--dy-space-2);
    border-bottom: 1px solid var(--dy-color-border);
  }

  .dy-rail__tab {
    padding: var(--dy-space-1) var(--dy-space-3);
    border: 0;
    border-radius: var(--dy-radius-sm);
    background: transparent;
    color: var(--dy-color-ink-soft);
    font: inherit;
    font-size: var(--dy-text-sm);
    font-weight: var(--dy-weight-medium);
    cursor: pointer;
  }

  .dy-rail__tab:hover {
    color: var(--dy-color-ink);
  }

  .dy-rail__tab--active {
    background: var(--dy-color-mint);
    color: var(--dy-color-primary-strong);
  }

  .dy-rail__tab:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }

  .dy-rail__panel {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
</style>
