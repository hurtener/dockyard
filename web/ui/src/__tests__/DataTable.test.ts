/**
 * DataTable.test.ts — DataTable composes PageState + Pagination: it routes the
 * empty state, sorts client-side, paginates, and emits row clicks.
 */
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import DataTable from '../DataTable.svelte';
import type { Column, Row } from '../types.js';

const columns: Column[] = [
  { key: 'name', label: 'Name', sortable: true },
  { key: 'count', label: 'Count', sortable: true, align: 'right' },
];

const rows: Row[] = [
  { name: 'beta', count: 2 },
  { name: 'alpha', count: 5 },
  { name: 'gamma', count: 1 },
];

describe('DataTable', () => {
  it('renders a row per data row and a header per column', () => {
    const { container } = render(DataTable, { columns, rows });
    expect(container.querySelectorAll('thead th')).toHaveLength(2);
    expect(container.querySelectorAll('tbody tr')).toHaveLength(3);
  });

  it('routes to the empty state when ready with zero rows', () => {
    render(DataTable, {
      columns,
      rows: [],
      emptyTitle: 'No tools registered',
    });
    expect(screen.getByTestId('empty-state')).toBeDefined();
    expect(screen.getByText('No tools registered')).toBeDefined();
  });

  it('routes through PageState when an explicit state is given', () => {
    render(DataTable, { columns, rows, pageState: 'loading' });
    expect(screen.getByTestId('loading-state')).toBeDefined();
  });

  it('emits onRowClick with the clicked row', async () => {
    const onRowClick = vi.fn();
    const { container } = render(DataTable, { columns, rows, onRowClick });
    const firstRow = container.querySelector('tbody tr');
    (firstRow as HTMLElement).click();
    expect(onRowClick).toHaveBeenCalledOnce();
    expect(onRowClick.mock.calls[0][0]).toMatchObject({ name: 'beta' });
  });

  it('sorts ascending when a sortable header is clicked', async () => {
    const { container } = render(DataTable, { columns, rows });
    const nameSort = container.querySelector('thead th button') as HTMLElement;
    nameSort.click();
    await Promise.resolve();
    const firstCell = container.querySelector('tbody tr td');
    expect(firstCell?.textContent?.trim()).toBe('alpha');
  });

  it('paginates client-side when pageSize is set', () => {
    const { container } = render(DataTable, { columns, rows, pageSize: 2 });
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    // Pagination control is rendered because pageCount > 1.
    expect(screen.getByTestId('pagination')).toBeDefined();
  });
});
