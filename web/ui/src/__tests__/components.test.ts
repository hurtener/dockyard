/**
 * components.test.ts — rendering + prop wiring for the rest of the inventory:
 * shell/layout, status display, and the recursive JsonInspector.
 */
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import StatusChip from '../StatusChip.svelte';
import MetricCard from '../MetricCard.svelte';
import Pagination from '../Pagination.svelte';
import FilterBar from '../FilterBar.svelte';
import JsonInspector from '../JsonInspector.svelte';
import LoadingState from '../LoadingState.svelte';
import PermissionState from '../PermissionState.svelte';

describe('StatusChip', () => {
  it('renders the label and reflects the tone', () => {
    const { getByTestId } = render(StatusChip, { label: 'passing', tone: 'ok' });
    const chip = getByTestId('status-chip');
    expect(chip.textContent?.trim()).toContain('passing');
    expect(chip.getAttribute('data-tone')).toBe('ok');
  });

  it('defaults to the neutral tone', () => {
    const { getByTestId } = render(StatusChip, { label: 'idle' });
    expect(getByTestId('status-chip').getAttribute('data-tone')).toBe('neutral');
  });
});

describe('MetricCard', () => {
  it('renders label, value and an optional delta', () => {
    render(MetricCard, { label: 'Events', value: 42, delta: '+8', trend: 'up' });
    expect(screen.getByText('Events')).toBeDefined();
    expect(screen.getByText('42')).toBeDefined();
    expect(screen.getByText('+8')).toBeDefined();
  });
});

describe('Pagination', () => {
  it('disables previous on the first page and emits the next page', () => {
    const onpage = vi.fn();
    const { container } = render(Pagination, { page: 0, pageCount: 3, onpage });
    const [prev, next] = [...container.querySelectorAll('button')];
    expect((prev as HTMLButtonElement).disabled).toBe(true);
    (next as HTMLButtonElement).click();
    expect(onpage).toHaveBeenCalledWith(1);
  });

  it('disables next on the last page', () => {
    const { container } = render(Pagination, { page: 2, pageCount: 3 });
    const next = [...container.querySelectorAll('button')][1] as HTMLButtonElement;
    expect(next.disabled).toBe(true);
  });
});

describe('FilterBar', () => {
  it('emits the query on input', async () => {
    const onquery = vi.fn();
    const { container } = render(FilterBar, { onquery });
    const input = container.querySelector('input') as HTMLInputElement;
    input.value = 'tools/call';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    expect(onquery).toHaveBeenCalledWith('tools/call');
  });

  it('toggles a filter chip and emits the new active set', () => {
    const onfilter = vi.fn();
    const { container } = render(FilterBar, {
      filters: [{ id: 'errors', label: 'Errors' }],
      active: [],
      onfilter,
    });
    const chip = container.querySelector(
      '.dy-filterbar__chip',
    ) as HTMLButtonElement;
    chip.click();
    expect(onfilter).toHaveBeenCalledWith(['errors']);
  });
});

describe('JsonInspector', () => {
  it('renders a leaf value', () => {
    const { getByTestId } = render(JsonInspector, { value: 'hello' });
    expect(getByTestId('json-inspector').textContent).toContain('"hello"');
  });

  it('renders a container with a child count preview', () => {
    const { getAllByTestId } = render(JsonInspector, {
      value: { a: 1, b: 2 },
      collapseDepth: 5,
    });
    // The root plus two recursively-rendered leaf nodes.
    expect(getAllByTestId('json-inspector').length).toBeGreaterThanOrEqual(3);
  });
});

describe('LoadingState', () => {
  it('renders a default message and an aria-live region', () => {
    const { getByTestId } = render(LoadingState, {});
    const el = getByTestId('loading-state');
    expect(el.getAttribute('role')).toBe('status');
    expect(el.textContent).toContain('Loading');
  });
});

describe('PermissionState', () => {
  it('renders copy and an optional action', () => {
    const onaction = vi.fn();
    const { container } = render(PermissionState, {
      title: 'Tasks unavailable',
      description: 'The requestor is not identifiable.',
      actionLabel: 'Learn more',
      onaction,
    });
    expect(screen.getByText('Tasks unavailable')).toBeDefined();
    (container.querySelector('button') as HTMLButtonElement).click();
    expect(onaction).toHaveBeenCalledOnce();
  });
});
