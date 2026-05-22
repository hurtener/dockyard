/**
 * host-bridge.test.ts — the host half of the ui/ bridge.
 *
 * The binding RFC §12 acceptance criterion is exercised end-to-end here: the
 * real @dockyard/bridge View half (BridgeShell) completes its ui/initialize
 * handshake against this phase's HostBridge over a MessageChannel — no mock at
 * the protocol seam.
 */
import { describe, expect, it, vi } from 'vitest';
import { createBridge, type MessageSink } from '@dockyard/bridge';
import { HostBridge, defaultHostContext } from '../host/host-bridge.js';

/** A MessageChannel-backed source the HostBridge / BridgeShell can both drive. */
function portSource(port: MessagePort) {
  return {
    addEventListener(_t: 'message', l: (ev: { data: unknown }) => void) {
      port.addEventListener('message', (ev) => l({ data: ev.data }));
    },
    removeEventListener() {
      /* the test channel lives for the test */
    },
    start() {
      port.start();
    },
  };
}

describe('HostBridge handshake', () => {
  it('completes a real ui/initialize handshake with the bridge View half', async () => {
    const channel = new MessageChannel();
    // Host side on port1, View (BridgeShell) side on port2.
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();

    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
      displayModes: ['inline', 'fullscreen'],
    });

    await Promise.all([view.connect(), host.ready()]);

    expect(host.isReady).toBe(true);
    expect(host.handshakeStarted).toBe(true);
    // The host narrowed availableDisplayModes to the App's advertised subset.
    expect(host.availableDisplayModes.sort()).toEqual(['fullscreen', 'inline']);
    host.close();
    view.close();
  });

  it('grants a display mode the App advertised and denies one it did not', async () => {
    const channel = new MessageChannel();
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
      displayModes: ['inline', 'pip'],
    });
    await Promise.all([view.connect(), host.ready()]);

    const granted = await view.requestDisplayMode('pip');
    expect(granted.granted).toBe(true);
    expect(host.displayMode).toBe('pip');

    // 'fullscreen' was not advertised by the App — the View rejects it
    // client-side before a round trip (capability-driven, never a host matrix).
    await expect(view.requestDisplayMode('fullscreen')).rejects.toThrow();
    host.close();
    view.close();
  });

  it('feeds the RPC log callback for both directions', async () => {
    const channel = new MessageChannel();
    const entries: string[] = [];
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
      onRpc: (e) => entries.push(`${e.direction}:${e.method ?? 'response'}`),
    });
    host.start();
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
    });
    await Promise.all([view.connect(), host.ready()]);

    expect(entries).toContain('inbound:ui/initialize');
    expect(entries).toContain('outbound:response');
    expect(entries).toContain('outbound:ui/notifications/initialized');
    host.close();
    view.close();
  });

  it('answers a tools/call with an error when no fixture is wired', async () => {
    const channel = new MessageChannel();
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
    });
    await Promise.all([view.connect(), host.ready()]);

    await expect(view.callTool('demo', {})).rejects.toThrow(/fixture/);
    host.close();
    view.close();
  });

  it('answers a tools/call from the wired fixture responder (the fixture switcher)', async () => {
    const channel = new MessageChannel();
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
    });
    await Promise.all([view.connect(), host.ready()]);

    // A successful fixture: the App's tools/call resolves with synthetic
    // structuredContent — the fixture drives the App's UI state.
    host.setCallToolResponder(() => ({
      structuredContent: { total: 1234 },
      text: 'happy fixture',
    }));
    const ok = await view.callTool('demo', {});
    expect(ok.structuredContent).toEqual({ total: 1234 });

    // An error fixture: the tools/call rejects so the App renders its error
    // state.
    host.setCallToolResponder(() => ({
      error: { code: -32003, message: 'permission denied' },
    }));
    await expect(view.callTool('demo', {})).rejects.toThrow(/permission denied/);
    host.close();
    view.close();
  });

  it('patches host context and notifies the View', async () => {
    const channel = new MessageChannel();
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: portSource(channel.port2),
    });
    await Promise.all([view.connect(), host.ready()]);

    const seen = vi.fn();
    view.onHostContextChanged(seen);
    host.patchHostContext({ theme: 'dark' });
    await new Promise((r) => setTimeout(r, 20));
    expect(seen).toHaveBeenCalledWith(expect.objectContaining({ theme: 'dark' }));
    expect(host.currentHostContext().theme).toBe('dark');
    host.close();
    view.close();
  });
});

describe('defaultHostContext', () => {
  it('emulates a capable host offering all three display modes', () => {
    const ctx = defaultHostContext();
    expect(ctx.availableDisplayModes).toEqual(['inline', 'fullscreen', 'pip']);
    expect(ctx.displayMode).toBe('inline');
  });
});
