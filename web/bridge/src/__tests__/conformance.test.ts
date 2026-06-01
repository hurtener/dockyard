/**
 * conformance.test.ts — the wire-conformance guard (D-182).
 *
 * Parses the bridge's *emitted* `ui/` wire against the vendored official
 * ext-apps schema (`../spec/ext-apps-schema`). A spec drift in the bridge's
 * outbound shape becomes a failing test here rather than a blank App a partner
 * finds in a real host. This is the regression that would have caught the
 * v1.6.1 handshake bugs (D-179/D-180/D-181).
 *
 * Why `.parse()` AND a round-trip assertion: Zod `.object()` strips unknown
 * keys rather than throwing, so a *renamed* field (e.g. item A's `displayModes`
 * vs the schema's `availableDisplayModes`) would pass a bare `.parse()` — it is
 * caught by asserting the sent value survives the parse under the correct key.
 *
 * Importing the schema as *values* pulls Zod into this test only; the runtime
 * `protocol.ts` imports it `type`-only, so the shipped App bundle stays
 * Zod-free (RFC §7.4).
 */
import { afterEach, describe, expect, it } from 'vitest';
import { createBridge } from '../bridge.js';
import { DOCKYARD_EXT_METHODS } from '../dockyard-ext.js';
import { HostNotification, ViewNotification } from '../protocol.js';
import {
  McpUiInitializeRequestSchema,
  McpUiInitializedNotificationSchema,
  McpUiRequestTeardownNotificationSchema,
  McpUiResourceTeardownResultSchema,
  McpUiSizeChangedNotificationSchema,
} from '../spec/ext-apps-schema.js';
import { HostHarness } from './harness.js';

describe('wire conformance — the bridge emits schema-valid ui/ wire (D-182)', () => {
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

  it('ui/initialize params conform to McpUiInitializeRequestSchema (D-179 regression)', async () => {
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
    // Throws if the params drift from the host's required shape (the original
    // D-179 failure: base-MCP {capabilities, clientInfo} instead of {appInfo,
    // appCapabilities, protocolVersion}).
    const parsed = McpUiInitializeRequestSchema.parse({
      method: 'ui/initialize',
      params: req.params,
    });
    // Round-trip preservation — catches item A: a `displayModes` key would be
    // silently stripped by the parse, leaving availableDisplayModes undefined.
    expect(parsed.params.appCapabilities.availableDisplayModes).toEqual([
      'inline',
      'fullscreen',
    ]);
    expect(parsed.params.appInfo).toMatchObject({
      name: 'demo-app',
      version: '2.0.0',
    });
  });

  it('ui/notifications/initialized conforms to its schema', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    await new Promise((r) => setTimeout(r, 0)); // drain the View→host post

    const note = h.lastNotification('ui/notifications/initialized')!;
    expect(() =>
      McpUiInitializedNotificationSchema.parse({
        method: note.method,
        params: note.params,
      }),
    ).not.toThrow();
  });

  it('ui/notifications/size-changed conforms to its schema', async () => {
    // jsdom defines neither ResizeObserver nor a synchronous rAF — stub both to
    // drive one deterministic size report (see bridge.test.ts for the pattern).
    type ROCtor = new (cb: () => void) => { observe(): void; disconnect(): void };
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

      const note = h.lastNotification('ui/notifications/size-changed')!;
      expect(() =>
        McpUiSizeChangedNotificationSchema.parse({
          method: note.method,
          params: note.params,
        }),
      ).not.toThrow();
      bridge.close();
    } finally {
      g.ResizeObserver = realRO;
      g.requestAnimationFrame = realRAF;
    }
  });

  it('the ui/resource-teardown response conforms (item B)', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    const result = await h.sendRequest('ui/resource-teardown', {});
    // The empty result object must satisfy McpUiResourceTeardownResultSchema.
    expect(() => McpUiResourceTeardownResultSchema.parse(result)).not.toThrow();
  });

  it('Dockyard extension notifications are fenced out of the conformed surface (D-183)', () => {
    // The conformed protocol surface must contain none of the Dockyard
    // extension methods — they live in dockyard-ext, and the fence is exactly
    // these two so it cannot silently grow.
    const conformed = [
      ...Object.values(ViewNotification),
      ...Object.values(HostNotification),
    ];
    for (const m of DOCKYARD_EXT_METHODS) {
      expect(conformed).not.toContain(m);
    }
    expect([...DOCKYARD_EXT_METHODS].sort()).toEqual([
      'ui/notifications/elicitation-response',
      'ui/notifications/task-progress',
    ]);
  });

  it('the ui/notifications/request-teardown notification conforms (item B)', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    bridge.requestTeardown();
    await new Promise((r) => setTimeout(r, 0)); // drain the View→host post

    const note = h.lastNotification('ui/notifications/request-teardown')!;
    expect(() =>
      McpUiRequestTeardownNotificationSchema.parse({
        method: note.method,
        params: note.params,
      }),
    ).not.toThrow();
  });
});
