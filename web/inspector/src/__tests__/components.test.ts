/**
 * components.test.ts — the inspector's own composed views.
 *
 * These confirm the Events panel, the RPC panel, and the App-frame wrapper
 * render through the four-state PageState and compose the dockyard-ui
 * inventory (CLAUDE.md §20 — empty + error states are mandatory).
 */
import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/svelte';
import EventsPanel from '../lib/EventsPanel.svelte';
import RpcPanel from '../lib/RpcPanel.svelte';
import AppFrame from '../lib/AppFrame.svelte';
import type { ObsEvent } from '../lib/obs.js';
import type { RpcEntry } from '../lib/rpc.js';

const obsEvent: ObsEvent = {
  schema_version: 'dockyard.obs/v1',
  id: 'ev1',
  timestamp: '2026-05-22T10:00:00Z',
  server_id: 'srv',
  trace_id: 't',
  span_id: 's',
  kind: 'tool.call',
  phase: 'end',
};

describe('EventsPanel', () => {
  it('shows the real empty-state copy when no events have arrived', () => {
    const { getByText } = render(EventsPanel, {
      props: { events: [], streamState: 'empty' },
    });
    expect(getByText(/No events yet — call a tool/)).toBeDefined();
  });

  it('shows the error state when the stream disconnects', () => {
    const { getByText } = render(EventsPanel, {
      props: { events: [], streamState: 'error' },
    });
    expect(getByText(/Stream disconnected/)).toBeDefined();
  });

  it('renders the obs/v1 events when ready', () => {
    const { getByTestId } = render(EventsPanel, {
      props: { events: [obsEvent], streamState: 'ready' },
    });
    expect(getByTestId('events-panel')).toBeDefined();
    expect(getByTestId('event-detail')).toBeDefined();
  });
});

describe('RpcPanel', () => {
  it('shows the real empty-state copy with no traffic', () => {
    const { getByText } = render(RpcPanel, {
      props: { entries: [], logState: 'empty' },
    });
    expect(getByText(/No JSON-RPC messages yet/)).toBeDefined();
  });

  it('shows the error state when the log is unavailable', () => {
    const { getByText } = render(RpcPanel, {
      props: { entries: [], logState: 'error' },
    });
    expect(getByText(/RPC log unavailable/)).toBeDefined();
  });

  it('renders the JSON-RPC entries when ready', () => {
    const entries: RpcEntry[] = [
      { id: 'a', direction: 'inbound', method: 'tools/call', payload: {}, at: 1 },
    ];
    const { getByTestId } = render(RpcPanel, {
      props: { entries, logState: 'ready' },
    });
    expect(getByTestId('rpc-panel')).toBeDefined();
  });
});

describe('AppFrame', () => {
  it('renders the App in a sandboxed iframe', async () => {
    const { container } = render(AppFrame, {
      props: {
        html: '<!doctype html><title>app</title>',
        appName: 'Demo App',
      },
    });
    const iframe = container.querySelector('iframe');
    expect(iframe).not.toBeNull();
    // The iframe is sandboxed deny-by-default (CLAUDE.md §7).
    expect(iframe?.getAttribute('sandbox')).toBe('allow-scripts');
    // `srcdoc` is set inside a queueMicrotask in the mount effect so the
    // attribute is not present in the same tick as render(). Awaiting a
    // microtask is the documented Chromium-iframe-race workaround
    // (see AppFrame.svelte).
    await new Promise<void>((resolve) => queueMicrotask(resolve));
    expect(iframe?.getAttribute('srcdoc')).toContain('app');
  });
});
