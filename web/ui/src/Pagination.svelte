<!--
  Pagination — page controls. Composed by DataTable; usable standalone.
  Zero-based `page`; emits the requested page through `onpage`.
-->
<script lang="ts">
  interface Props {
    /** The current zero-based page index. */
    page: number;
    /** The total number of pages (>= 1). */
    pageCount: number;
    /** Fired with the requested zero-based page index. */
    onpage?: (page: number) => void;
  }

  let { page, pageCount, onpage }: Props = $props();

  const atStart = $derived(page <= 0);
  const atEnd = $derived(page >= pageCount - 1);

  function go(to: number): void {
    const clamped = Math.max(0, Math.min(pageCount - 1, to));
    if (clamped !== page) onpage?.(clamped);
  }
</script>

<nav class="dy-pagination" aria-label="Pagination" data-testid="pagination">
  <button
    type="button"
    class="dy-pagination__btn"
    disabled={atStart}
    aria-label="Previous page"
    onclick={() => go(page - 1)}
  >
    ‹
  </button>
  <span class="dy-pagination__label">
    Page {pageCount === 0 ? 0 : page + 1} of {pageCount}
  </span>
  <button
    type="button"
    class="dy-pagination__btn"
    disabled={atEnd}
    aria-label="Next page"
    onclick={() => go(page + 1)}
  >
    ›
  </button>
</nav>

<style>
  .dy-pagination {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }

  .dy-pagination__btn {
    width: 28px;
    height: 28px;
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
    font: inherit;
    cursor: pointer;
  }

  .dy-pagination__btn:hover:not(:disabled) {
    border-color: var(--dy-color-primary);
    color: var(--dy-color-primary-strong);
  }

  .dy-pagination__btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .dy-pagination__btn:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }
</style>
