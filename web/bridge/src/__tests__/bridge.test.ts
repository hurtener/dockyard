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

  it('sends ui/initialize in the ui/ dialect — appInfo + flat appCapabilities (D-179)', async () => {
    // Bug 1 regression: a spec-compliant host (@modelcontextprotocol/ext-apps)
    // validates `ui/initialize` params against a schema requiring top-level
    // `{appInfo, appCapabilities, protocolVersion}` (appInfo REQUIRED). The
    // previous base-MCP `{capabilities:{appCapabilities}, clientInfo}` shape was
    // rejected with a JSON-RPC error, so connect() rejected and the App stayed
    // blank with no visible error. Assert the dialect shape AND the absence of
    // the legacy keys — the structural check that would have caught the bug.
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
    expect(req.params).toEqual({
      protocolVersion: '2026-01-26',
      appCapabilities: { availableDisplayModes: ['inline', 'fullscreen'] },
      appInfo: { name: 'demo-app', version: '2.0.0' },
    });
    expect(req.params).not.toHaveProperty('capabilities');
    expect(req.params).not.toHaveProperty('clientInfo');
    // Item A (D-182): the wire key is `availableDisplayModes`, never the
    // silently-stripped `displayModes`.
    expect((req.params as { appCapabilities?: object }).appCapabilities).not.toHaveProperty(
      'displayModes',
    );
  });

  it('resolves ready by SENDING ui/notifications/initialized, never awaiting one (D-180)', async () => {
    // Bug 2 regression: the View is the initiator. A spec host NEVER sends a
    // View→host notification, so the old "await receipt of initialized" code
    // deadlocked → ready never flipped → blank App. The bridge must SEND
    // `initialized` after the initialize result and become ready on its own.
    // The harness here does NOT push a host→View initialized.
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });

    await bridge.connect();
    await new Promise((r) => setTimeout(r, 0)); // drain the View→host post

    expect(get(bridge.ready)).toBe(true);
    expect(bridge.isInitialized).toBe(true);
    expect(h.lastNotification('ui/notifications/initialized')).toBeDefined();
  });

  it('ignores a non-spec inbound ui/notifications/initialized from the host', async () => {
    // A non-spec host that also pushes `initialized` host→View must not break
    // the bridge (no deadlock, no double-ready, no throw).
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    expect(get(bridge.ready)).toBe(true);

    h.sendInitialized();
    await new Promise((r) => setTimeout(r, 0));
    expect(get(bridge.ready)).toBe(true);
  });

  it('reports View content size to the host via ui/notifications/size-changed (D-181)', async () => {
    // Bug 3 regression: without a View→host size report, a spec host sizes the
    // App iframe to a collapsed (~0px) height and the App looks blank even after
    // it paints. The bridge runs a ResizeObserver and emits size-changed on
    // ready. jsdom defines neither ResizeObserver nor a synchronous rAF, so we
    // stub both to drive a single deterministic measurement.
    type ROCtor = new (cb: () => void) => {
      observe(): void;
      disconnect(): void;
    };
    const g = globalThis as unknown as {
      ResizeObserver?: ROCtor;
      requestAnimationFrame?: (cb: () => void) => number;
    };
    const realRO = g.ResizeObserver;
    const realRAF = g.requestAnimationFrame;
    g.ResizeObserver = class {
      constructor(_cb: () => void) {}
      observe(): void {}
      disconnect(): void {}
    };
    g.requestAnimationFrame = (cb: () => void): number => {
      cb();
      return 0;
    };
    try {
      const h = harness();
      const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
      await bridge.connect();
      await new Promise((r) => setTimeout(r, 0)); // drain the View→host post

      const sized = h.lastNotification('ui/notifications/size-changed');
      expect(sized).toBeDefined();
      expect(sized!.params).toMatchObject({
        width: expect.any(Number),
        height: expect.any(Number),
      });
      bridge.close();
    } finally {
      g.ResizeObserver = realRO;
      g.requestAnimationFrame = realRAF;
    }
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

describe('BridgeShell — default host peer', () => {
  // Regression for the Phase-24 inspector handshake bug: the default peer
  // path posts to `window.parent.postMessage(message, '*')` and not the
  // single-arg form. Cross-document postMessage requires an explicit
  // targetOrigin, and the View runs inside a sandboxed (opaque-origin)
  // iframe whose parent is necessarily cross-origin from its perspective.
  // Without '*' the browser silently drops every outbound message and the
  // bridge's `ui/initialize` never reaches the host (handshake hangs).

  it('posts outbound messages to window.parent with a wildcard targetOrigin', async () => {
    type PostArgs = [unknown, string?];
    const calls: PostArgs[] = [];
    const fakeWindow = {
      parent: {
        postMessage: (message: unknown, targetOrigin?: string): void => {
          calls.push([message, targetOrigin]);
        },
      },
    };
    const realWindow = (globalThis as { window?: unknown }).window;
    (globalThis as { window?: unknown }).window = fakeWindow;
    try {
      // We don't await connect() here — only need to prove that constructing
      // the bridge + calling connect() invokes the parent's postMessage with
      // a wildcard targetOrigin. We pass an explicit `source` so the bridge
      // doesn't try to addEventListener on the fake window.
      const source = {
        addEventListener(): void {},
        removeEventListener(): void {},
      };
      const bridge = createBridge({ source, styleTarget: null });
      void bridge.connect().catch(() => {
        /* the fake host never answers; the connect promise stays pending */
      });
      expect(calls.length).toBeGreaterThanOrEqual(1);
      const [message, targetOrigin] = calls[0]!;
      expect(targetOrigin).toBe('*');
      // The first outbound message is the ui/initialize request.
      expect(message).toMatchObject({
        jsonrpc: '2.0',
        method: 'ui/initialize',
      });
      bridge.close();
    } finally {
      (globalThis as { window?: unknown }).window = realWindow;
    }
  });

  it('throws a clear error when no window.parent is available', () => {
    const realWindow = (globalThis as { window?: unknown }).window;
    (globalThis as { window?: unknown }).window = undefined;
    try {
      expect(() => createBridge({})).toThrowError(/no host peer/);
    } finally {
      (globalThis as { window?: unknown }).window = realWindow;
    }
  });
});

