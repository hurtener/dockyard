<!--
  FilterBar — a search input plus filter chips. design-spec.md §3.2: this is the
  one place a search lives — it is never embedded inside PageHeader.
-->
<script lang="ts">
  interface Filter {
    /** Stable identity. */
    id: string;
    /** The chip label. */
    label: string;
  }

  interface Props {
    /** The search query — bindable. */
    query?: string;
    /** Placeholder for the search input. */
    placeholder?: string;
    /** The available filter chips. */
    filters?: Filter[];
    /** The ids of the currently active filters. */
    active?: string[];
    /** Fired when the query changes. */
    onquery?: (query: string) => void;
    /** Fired when a filter chip is toggled, with the new active id set. */
    onfilter?: (active: string[]) => void;
  }

  let {
    query = '',
    placeholder = 'Search…',
    filters = [],
    active = [],
    onquery,
    onfilter,
  }: Props = $props();

  function onInput(event: Event): void {
    const value = (event.currentTarget as HTMLInputElement).value;
    onquery?.(value);
  }

  function toggle(id: string): void {
    const next = active.includes(id)
      ? active.filter((a) => a !== id)
      : [...active, id];
    onfilter?.(next);
  }
</script>

<div class="dy-filterbar" data-testid="filter-bar">
  <input
    type="search"
    class="dy-filterbar__search"
    {placeholder}
    value={query}
    aria-label={placeholder}
    oninput={onInput}
  />
  {#if filters.length > 0}
    <div class="dy-filterbar__chips" role="group" aria-label="Filters">
      {#each filters as filter (filter.id)}
        <button
          type="button"
          class="dy-filterbar__chip"
          class:dy-filterbar__chip--active={active.includes(filter.id)}
          aria-pressed={active.includes(filter.id)}
          onclick={() => toggle(filter.id)}
        >
          {filter.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .dy-filterbar {
    display: flex;
    align-items: center;
    gap: var(--dy-space-3);
    flex-wrap: wrap;
    font-family: var(--dy-font-sans);
  }

  .dy-filterbar__search {
    flex: 1;
    min-width: 180px;
    padding: var(--dy-space-2) var(--dy-space-3);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
    font: inherit;
    font-size: var(--dy-text-base);
  }

  .dy-filterbar__search:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }

  .dy-filterbar__chips {
    display: flex;
    gap: var(--dy-space-1);
    flex-wrap: wrap;
  }

  .dy-filterbar__chip {
    padding: var(--dy-space-1) var(--dy-space-3);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-full);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink-soft);
    font: inherit;
    font-size: var(--dy-text-sm);
    cursor: pointer;
  }

  .dy-filterbar__chip--active {
    background: var(--dy-color-mint);
    border-color: var(--dy-color-primary);
    color: var(--dy-color-primary-strong);
    font-weight: var(--dy-weight-medium);
  }

  .dy-filterbar__chip:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }
</style>
