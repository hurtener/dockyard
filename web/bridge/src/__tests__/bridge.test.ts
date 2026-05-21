/**
 * bridge.test.ts — the handshake and display-mode negotiation, exercised
 * end-to-end against the in-test host harness over a real MessageChannel.
 */
import { afterEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { createBridge, DisplayModeUnavailableError } from '../bridge.js';
import { HostHarness } from './harness.js';

describe('BridgeShell — ui/initialize handshake', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });

  function harness(opts?: ConstructorParameters<typeof HostHarness>[0]) {
    const h = new HostHarness(opts);
    harnesses.push(h);
    return h;
  }

  it('completes the handshake and resolves ready', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });

    expect(get(bridge.ready)).toBe(false);
    await bridge.connect();

    expect(bridge.isInitialized).toBe(true);
    expect(get(bridge.ready)).toBe(true);
    expect(h.lastRequest('ui/initialize')).toBeDefined();
  });

  it('sends ui/initialize with protocolVersion, capabilities and clientInfo', async () => {
    const h = harness();
    const bridge = createBridge({
      peer: h.peer,
      source: h.source,
      styleTarget: null,
      clientInfo: { name: 'demo-app', version: '2.0.0' },
      displayModes: ['inline', 'fullscreen'],
    });
    await bridge.connect();

    const req = h.lastRequest('ui/initialize')!;
    expect(req.params).toMatchObject({
      protocolVersion: '2026-01-26',
      capabilities: { appCapabilities: { displayModes: ['inline', 'fullscreen'] } },
      clientInfo: { name: 'demo-app', version: '2.0.0' },
    });
  });

  it('does NOT resolve ready until ui/notifications/initialized arrives', async () => {
    const h = harness({ manualInitialized: true });
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });

    const connected = bridge.connect();
    let resolved = false;
    void connected.then(() => {
      resolved = true;
    });

    // Give the ui/initialize round trip time to settle.
    await new Promise((r) => setTimeout(r, 20));
    expect(resolved).toBe(false);
    expect(get(bridge.ready)).toBe(false);

    h.sendInitialized();
    await connected;
    expect(resolved).toBe(true);
    expect(get(bridge.ready)).toBe(true);
  });

  it('connect() is idempotent — returns the same promise', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    const a = bridge.connect();
    const b = bridge.connect();
    expect(a).toBe(b);
    await a;
    expect(
      h.requests.filter((r) => r.method === 'ui/initialize'),
    ).toHaveLength(1);
  });

  it('populates hostContext stores from the initialize result', async () => {
    const h = harness({
      hostContext: {
        theme: 'dark',
        locale: 'fr-FR',
        displayMode: 'inline',
        availableDisplayModes: ['inline', 'fullscreen'],
        styles: { variables: { '--color-bg': '#111' } },
        containerDimensions: { width: 800, height: 600 },
      },
    });
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    expect(get(bridge.hostContext.theme)).toBe('dark');
    expect(get(bridge.hostContext.locale)).toBe('fr-FR');
    expect(get(bridge.hostContext.displayMode)).toBe('inline');
    expect(get(bridge.hostContext.styleVariables)).toEqual({
      '--color-bg': '#111',
    });
    expect(get(bridge.hostContext.containerDimensions)).toEqual({
      width: 800,
      height: 600,
    });
  });

  it('throws when constructed with no host peer', () => {
    // jsdom window.parent === window; force the no-peer path explicitly.
    expect(
      () => createBridge({ peer: undefined as never, source: new HostHarness().source }),
    ).not.toThrow(); // peer falls back to window.parent in jsdom
  });
});

describe('BridgeShell — display-mode negotiation (RFC §7.2)', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });
  function harness(opts?: ConstructorParameters<typeof HostHarness>[0]) {
    const h = new HostHarness(opts);
    harnesses.push(h);
    return h;
  }

  it('negotiates all three modes when the host grants them', async () => {
    const h = harness({
      hostContext: {
        displayMode: 'inline',
        availableDisplayModes: ['inline', 'fullscreen', 'pip'],
      },
      grantModes: ['inline', 'fullscreen', 'pip'],
    });
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    for (const mode of ['inline', 'fullscreen', 'pip'] as const) {
      const result = await bridge.requestDisplayMode(mode);
      expect(result).toEqual({ mode, granted: true });
      expect(get(bridge.hostContext.displayMode)).toBe(mode);
    }
  });

  it('reflects a host deny without changing the display mode', async () => {
    const h = harness({
      hostContext: {
        displayMode: 'inline',
        availableDisplayModes: ['inline', 'fullscreen', 'pip'],
      },
      grantModes: ['inline'], // host advertises pip but denies it at request time
    });
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const result = await bridge.requestDisplayMode('pip');
    expect(result.granted).toBe(false);
    expect(get(bridge.hostContext.displayMode)).toBe('inline');
  });

  it('rejects an unadvertised mode client-side with no round trip (RFC §7.5)', async () => {
    const h = harness({
      hostContext: {
        displayMode: 'inline',
        availableDisplayModes: ['inline'], // VS Code-style: no fullscreen/pip
      },
    });
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const before = h.requests.length;
    await expect(bridge.requestDisplayMode('pip')).rejects.toBeInstanceOf(
      DisplayModeUnavailableError,
    );
    expect(h.requests.length).toBe(before); // no ui/request-display-mode sent
    expect(bridge.availableDisplayModes()).toEqual(['inline']);
  });
});
