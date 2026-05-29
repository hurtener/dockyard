<!--
  DataTable — columns, rows, optional sort, optional row-select. It COMPOSES the
  shared primitives rather than re-implementing them (AGENTS.md §20):
   - PageState wraps the body, so loading / empty / error are handled uniformly
     and the mandatory empty + error states come for free;
   - Pagination renders the page controls.
  Sorting is client-side over the supplied rows when a column is `sortable`.
-->
<script lang="ts">
  import type { Column, Row, SortState, PageStateValue } from './types.js';
  import PageState from './PageState.svelte';
  import Pagination from './Pagination.svelte';

  interface Props {
    /** The column descriptors. */
    columns: Column[];
    /** The row objects. */
    rows: Row[];
    /** Async state — routed through PageState. Default `ready`. */
    pageState?: PageStateValue;
    /** Page size. When set, the table paginates client-side. */
    pageSize?: number;
    /** The initial sort. */
    sort?: SortState;
    /** Fired when a row is clicked, with the row object. */
    onRowClick?: (row: Row) => void;
    /** Retry callback wired into the PageState error panel. */
    onRetry?: () => void;
    /** Headline for the empty state — real copy. */
    emptyTitle?: string;
    /** Supporting copy for the empty state. */
    emptyDescription?: string;
  }

  let {
    columns,
    rows,
    pageState = 'ready',
    pageSize,
    sort,
    onRowClick,
    onRetry,
    emptyTitle = 'No rows to show',
    emptyDescription,
  }: Props = $props();

  // `sort` is an initial-value prop: the table owns the live sort after mount.
  // svelte-ignore state_referenced_locally
  let activeSort = $state<SortState | undefined>(sort);
  let page = $state(0);

  function compare(a: unknown, b: unknown): number {
    if (typeof a === 'number' && typeof b === 'number') return a - b;
    return String(a ?? '').localeCompare(String(b ?? ''));
  }

  const sortedRows = $derived.by(() => {
    if (!activeSort) return rows;
    const { key, direction } = activeSort;
    const copy = [...rows];
    copy.sort((r1, r2) => {
      const cmp = compare(r1[key], r2[key]);
      return direction === 'asc' ? cmp : -cmp;
    });
    return copy;
  });

  const pageCount = $derived(
    pageSize ? Math.max(1, Math.ceil(sortedRows.length / pageSize)) : 1,
  );

  const visibleRows = $derived.by(() => {
    if (!pageSize) return sortedRows;
    const start = page * pageSize;
    return sortedRows.slice(start, start + pageSize);
  });

  // The PageState routing: rows are empty only when genuinely ready with none.
  const effectiveState = $derived<PageStateValue>(
    pageState === 'ready' && rows.length === 0 ? 'empty' : pageState,
  );

  function toggleSort(column: Column): void {
    if (!column.sortable) return;
    if (activeSort?.key === column.key) {
      activeSort = {
        key: column.key,
        direction: activeSort.direction === 'asc' ? 'desc' : 'asc',
      };
    } else {
      activeSort = { key: column.key, direction: 'asc' };
    }
    page = 0;
  }

  function onPage(next: number): void {
    page = next;
  }
</script>

<div class="dy-table" data-testid="data-table">
  <PageState
    state={effectiveState}
    emptyTitle={emptyTitle}
    emptyDescription={emptyDescription}
    onRetry={onRetry}
  >
    <table class="dy-table__table">
      <thead>
        <tr>
          {#each columns as column (column.key)}
            <th
              scope="col"
              style:width={column.width}
              style:text-align={column.align ?? 'left'}
              aria-sort={activeSort?.key === column.key
                ? activeSort.direction === 'asc'
                  ? 'ascending'
                  : 'descending'
                : 'none'}
            >
              {#if column.sortable}
                <button
                  type="button"
                  class="dy-table__sort"
                  onclick={() => toggleSort(column)}
                >
                  {column.label}
                  <span class="dy-table__sortglyph" aria-hidden="true">
                    {activeSort?.key === column.key
                      ? activeSort.direction === 'asc'
                        ? '▲'
                        : '▼'
                      : '↕'}
                  </span>
                </button>
              {:else}
                {column.label}
              {/if}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each visibleRows as row, i (i)}
          <tr
            class="dy-table__row"
            class:dy-table__row--clickable={!!onRowClick}
            onclick={() => onRowClick?.(row)}
          >
            {#each columns as column (column.key)}
              <td style:text-align={column.align ?? 'left'}>
                {row[column.key] ?? ''}
              </td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
    {#if pageSize && pageCount > 1}
      <div class="dy-table__footer">
        <Pagination {page} {pageCount} onpage={onPage} />
      </div>
    {/if}
  </PageState>
</div>

<style>
  .dy-table {
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
    overflow: hidden;
    font-family: var(--dy-font-sans);
  }

  .dy-table__table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--dy-text-base);
  }

  .dy-table__table th {
    padding: var(--dy-space-2) var(--dy-space-3);
    border-bottom: 1px solid var(--dy-color-border);
    background: var(--dy-color-canvas);
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-xs);
    font-weight: var(--dy-weight-semibold);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .dy-table__sort {
    display: inline-flex;
    align-items: center;
    gap: var(--dy-space-1);
    border: 0;
    background: transparent;
    padding: 0;
    font: inherit;
    color: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
    cursor: pointer;
  }

  .dy-table__sort:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
    border-radius: var(--dy-radius-sm);
  }

  .dy-table__sortglyph {
    color: var(--dy-color-primary);
  }

  .dy-table__table td {
    padding: var(--dy-space-2) var(--dy-space-3);
    border-bottom: 1px solid var(--dy-color-border);
    color: var(--dy-color-ink);
    /* Break long unbreakable tokens (e.g. snake_case tool names like
       `create_cinematic_image_video`) instead of forcing the column wide
       and overflowing the container with a horizontal scrollbar. */
    overflow-wrap: anywhere;
  }

  .dy-table__table tbody tr:last-child td {
    border-bottom: 0;
  }

  .dy-table__row--clickable {
    cursor: pointer;
  }

  .dy-table__row--clickable:hover {
    background: var(--dy-color-mint);
  }

  .dy-table__footer {
    display: flex;
    justify-content: flex-end;
    padding: var(--dy-space-2) var(--dy-space-3);
    border-top: 1px solid var(--dy-color-border);
  }
</style>
