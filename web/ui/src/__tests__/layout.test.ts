/**
 * layout.test.ts — the shell & layout primitives: slot rendering, the tabbed
 * DetailRail, RailCard collapse, ConnectionFooter state, and the Timeline.
 */
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import AppShell from '../AppShell.svelte';
import PageHeader from '../PageHeader.svelte';
import DetailRail from '../DetailRail.svelte';
import RailCard from '../RailCard.svelte';
import ConnectionFooter from '../ConnectionFooter.svelte';
import ActionBar from '../ActionBar.svelte';
import Timeline from '../Timeline.svelte';
import CodeBlock from '../CodeBlock.svelte';
import type { TimelineEvent } from '../types.js';

const body = createRawSnippet(() => ({
  render: () => '<p data-testid="card-body">body</p>',
}));

const named = (id: string) =>
  createRawSnippet(() => ({
    render: () => `<p data-testid="${id}">${id}</p>`,
  }));

describe('AppShell', () => {
  it('renders header, main, rail and footer slots', () => {
    const { getByTestId } = render(AppShell, {
      props: {
        header: named('shell-header'),
        rail: named('shell-rail'),
        footer: named('shell-footer'),
        children: named('shell-main'),
      },
    });
    expect(getByTestId('shell-header')).toBeDefined();
    expect(getByTestId('shell-rail')).toBeDefined();
    expect(getByTestId('shell-footer')).toBeDefined();
    expect(getByTestId('shell-main')).toBeDefined();
    expect(getByTestId('app-shell').getAttribute('data-density')).toBe(
      'comfortable',
    );
  });

  it('omits the rail aside when no rail slot is given', () => {
    const { container } = render(AppShell, {
      props: { children: named('only-main'), density: 'compact' },
    });
    expect(container.querySelector('.dy-shell__rail')).toBeNull();
    expect(container.querySelector('.dy-shell')?.getAttribute('data-density')).toBe(
      'compact',
    );
  });
});

describe('PageHeader', () => {
  it('renders title, subtitle and the action/status/lead slots', () => {
    const { getByTestId } = render(PageHeader, {
      props: {
        title: 'Dockyard Inspector',
        subtitle: 'demo-server v1',
        lead: named('hdr-lead'),
        status: named('hdr-status'),
        actions: named('hdr-actions'),
      },
    });
    expect(screen.getByText('Dockyard Inspector')).toBeDefined();
    expect(screen.getByText('demo-server v1')).toBeDefined();
    expect(getByTestId('hdr-lead')).toBeDefined();
    expect(getByTestId('hdr-status')).toBeDefined();
    expect(getByTestId('hdr-actions')).toBeDefined();
  });
});

describe('DetailRail', () => {
  it('stacks children when no tabs are given', () => {
    const { getByTestId } = render(DetailRail, {
      props: { children: named('rail-stack') },
    });
    expect(getByTestId('rail-stack')).toBeDefined();
  });

  it('renders a tab strip and switches the active tab', async () => {
    const { container } = render(DetailRail, {
      props: { tabs: ['Events', 'RPC'], children: named('rail-tabbed') },
    });
    const tabs = [...container.querySelectorAll('[role="tab"]')];
    expect(tabs).toHaveLength(2);
    expect(tabs[0].getAttribute('aria-selected')).toBe('true');
    expect(tabs[1].getAttribute('aria-selected')).toBe('false');
    (tabs[1] as HTMLButtonElement).click();
    await Promise.resolve();
    expect(tabs[1].getAttribute('aria-selected')).toBe('true');
    expect(tabs[0].getAttribute('aria-selected')).toBe('false');
  });

  it('fires onTabChange with the selected index', async () => {
    const onTabChange = vi.fn();
    const { container } = render(DetailRail, {
      props: { tabs: ['A', 'B', 'C'], children: named('rail-cb'), onTabChange },
    });
    const tabs = [...container.querySelectorAll('[role="tab"]')];
    (tabs[2] as HTMLButtonElement).click();
    expect(onTabChange).toHaveBeenCalledWith(2);
  });
});

describe('RailCard', () => {
  it('renders title and body when not collapsible', () => {
    render(RailCard, { title: 'Events', children: body });
    expect(screen.getByText('Events')).toBeDefined();
    expect(screen.getByTestId('card-body')).toBeDefined();
  });

  it('a collapsible card hides its body when toggled closed', async () => {
    const { container } = render(RailCard, {
      title: 'RPC',
      collapsible: true,
      children: body,
    });
    expect(screen.queryByTestId('card-body')).not.toBeNull();
    const header = container.querySelector('button') as HTMLButtonElement;
    header.click();
    await Promise.resolve();
    expect(screen.queryByTestId('card-body')).toBeNull();
    expect(header.getAttribute('aria-expanded')).toBe('false');
  });
});

describe('ConnectionFooter', () => {
  it('reflects the connection state and label', () => {
    const { getByTestId } = render(ConnectionFooter, {
      connection: 'connected',
      label: 'session-7',
      transport: 'stdio',
    });
    const footer = getByTestId('connection-footer');
    expect(footer.textContent).toContain('connected');
    expect(footer.textContent).toContain('session-7');
    expect(footer.textContent).toContain('stdio');
    expect(footer.querySelector('.dy-footer__dot')?.getAttribute('data-connection')).toBe(
      'connected',
    );
  });

  it('marks the dot live when streaming', () => {
    const { getByTestId } = render(ConnectionFooter, {
      connection: 'connected',
      live: true,
    });
    expect(
      getByTestId('connection-footer').querySelector('.dy-footer__dot--live'),
    ).not.toBeNull();
  });
});

describe('ActionBar', () => {
  it('reflects the alignment', () => {
    const { getByTestId } = render(ActionBar, {
      align: 'between',
      children: body,
    });
    expect(getByTestId('action-bar').getAttribute('data-align')).toBe('between');
  });
});

describe('Timeline', () => {
  const events: TimelineEvent[] = [
    { id: '1', title: 'tools/call', timestamp: '00:01', tone: 'info' },
    { id: '2', title: 'result', timestamp: '00:02', tone: 'ok', detail: 'ok' },
  ];

  // Timeline's `events` prop shares a name with a Testing Library render
  // option, so its props are passed under the explicit `props` key.
  it('renders an item per event', () => {
    const { container } = render(Timeline, { props: { events } });
    expect(container.querySelectorAll('.dy-timeline__item')).toHaveLength(2);
  });

  it('emits onselect with the activated event', () => {
    const onselect = vi.fn();
    const { container } = render(Timeline, { props: { events, onselect } });
    (container.querySelector('.dy-timeline__row') as HTMLButtonElement).click();
    expect(onselect).toHaveBeenCalledOnce();
    expect(onselect.mock.calls[0][0]).toMatchObject({ id: '1' });
  });

  it('renders nothing for an empty event list', () => {
    const { container } = render(Timeline, { props: { events: [] } });
    expect(container.querySelectorAll('.dy-timeline__item')).toHaveLength(0);
  });
});

describe('CodeBlock', () => {
  it('renders the code and an optional language label', () => {
    const { getByTestId } = render(CodeBlock, {
      code: 'dockyard dev',
      language: 'sh',
    });
    const block = getByTestId('code-block');
    expect(block.textContent).toContain('dockyard dev');
    expect(block.textContent).toContain('sh');
  });
});
