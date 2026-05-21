/**
 * PageState.test.ts — the four-state routing contract: PageState renders
 * EXACTLY ONE of loading / empty / error / ready, and the empty + error panels
 * carry their mandatory copy and working actions (CONVENTIONS.md §4).
 */
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import PageState from '../PageState.svelte';
import ErrorState from '../ErrorState.svelte';
import EmptyState from '../EmptyState.svelte';

// A minimal ready-content snippet for the slot.
const readySnippet = createRawSnippet(() => ({
  render: () => '<p data-testid="ready-content">ready</p>',
}));

describe('PageState — four-state routing', () => {
  it('renders the loading panel and nothing else when state=loading', () => {
    render(PageState, {
      state: 'loading',
      children: readySnippet,
      loadingMessage: 'Fetching events…',
    });
    expect(screen.getByTestId('loading-state')).toBeDefined();
    expect(screen.queryByTestId('empty-state')).toBeNull();
    expect(screen.queryByTestId('error-state')).toBeNull();
    expect(screen.queryByTestId('ready-content')).toBeNull();
    expect(screen.getByText('Fetching events…')).toBeDefined();
  });

  it('renders the empty panel with real copy when state=empty', () => {
    render(PageState, {
      state: 'empty',
      children: readySnippet,
      emptyTitle: 'No events yet',
      emptyDescription: 'Call a tool to see traffic.',
    });
    expect(screen.getByTestId('empty-state')).toBeDefined();
    expect(screen.getByText('No events yet')).toBeDefined();
    expect(screen.getByText('Call a tool to see traffic.')).toBeDefined();
    expect(screen.queryByTestId('ready-content')).toBeNull();
  });

  it('renders the error panel when state=error', () => {
    render(PageState, {
      state: 'error',
      children: readySnippet,
      errorTitle: 'Failed to load',
    });
    expect(screen.getByTestId('error-state')).toBeDefined();
    expect(screen.getByText('Failed to load')).toBeDefined();
    expect(screen.queryByTestId('ready-content')).toBeNull();
  });

  it('renders the ready slot when state=ready', () => {
    render(PageState, { state: 'ready', children: readySnippet });
    expect(screen.getByTestId('ready-content')).toBeDefined();
    expect(screen.queryByTestId('loading-state')).toBeNull();
  });
});

describe('ErrorState — the working retry affordance', () => {
  it('invokes onretry when the retry button is pressed', async () => {
    const onretry = vi.fn();
    const { container } = render(ErrorState, {
      title: 'Network error',
      onretry,
    });
    const button = container.querySelector('button');
    expect(button).not.toBeNull();
    button!.click();
    expect(onretry).toHaveBeenCalledOnce();
  });

  it('omits the retry button when no onretry is given', () => {
    const { container } = render(ErrorState, { title: 'Frozen error' });
    expect(container.querySelector('button')).toBeNull();
  });
});

describe('EmptyState — the action affordance', () => {
  it('invokes onaction when the action button is pressed', () => {
    const onaction = vi.fn();
    const { container } = render(EmptyState, {
      title: 'Nothing here',
      actionLabel: 'Create one',
      onaction,
    });
    const button = container.querySelector('button');
    expect(button).not.toBeNull();
    button!.click();
    expect(onaction).toHaveBeenCalledOnce();
  });

  it('renders no action button without both label and callback', () => {
    const { container } = render(EmptyState, { title: 'Just copy' });
    expect(container.querySelector('button')).toBeNull();
  });
});
