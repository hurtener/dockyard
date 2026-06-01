/**
 * notifications.test.ts — host → View notification fan-out, and the
 * host-context-changed → store-patch wiring.
 */
import { afterEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { createBridge } from '../bridge.js';
import { NotificationRouter } from '../notifications.js';
import { DockyardExtMethod } from '../dockyard-ext.js';
import { HostNotification } from '../protocol.js';
import { HostHarness } from './harness.js';

describe('NotificationRouter', () => {
  it('routes each host notification to its typed topic', () => {
    const router = new NotificationRouter();
    const calls: string[] = [];
    router.onToolInput(() => calls.push('tool-input'));
    router.onToolInputPartial(() => calls.push('tool-input-partial'));
    router.onToolResult(() => calls.push('tool-result'));
    router.onToolCancelled(() => calls.push('tool-cancelled'));
    router.onSizeChanged(() => calls.push('size-changed'));
    router.onHostContextChanged(() => calls.push('host-context-changed'));
    router.onTaskProgress(() => calls.push('task-progress'));

    router.dispatch(HostNotification.toolInput, { arguments: {} });
    router.dispatch(HostNotification.toolInputPartial, { arguments: {} });
    router.dispatch(HostNotification.toolResult, {});
    router.dispatch(HostNotification.toolCancelled, {});
    router.dispatch(HostNotification.sizeChanged, { width: 1, height: 1 });
    router.dispatch(HostNotification.hostContextChanged, {});
    router.dispatch(DockyardExtMethod.taskProgress, { taskId: 't1' });

    expect(calls).toEqual([
      'tool-input',
      'tool-input-partial',
      'tool-result',
      'tool-cancelled',
      'size-changed',
      'host-context-changed',
      'task-progress',
    ]);
  });

  it('ignores an unknown method without throwing', () => {
    const router = new NotificationRouter();
    expect(() => router.dispatch('ui/notifications/future', {})).not.toThrow();
  });

  it('unsubscribe stops a handler; mid-emit unsubscribe is safe', () => {
    const router = new NotificationRouter();
    const seen: number[] = [];
    const off1 = router.onSizeChanged(() => {
      seen.push(1);
      off1(); // unsubscribe self mid-emit
    });
    router.onSizeChanged(() => seen.push(2));

    router.dispatch(HostNotification.sizeChanged, { width: 0, height: 0 });
    router.dispatch(HostNotification.sizeChanged, { width: 0, height: 0 });
    // First dispatch fires both (1,2); second fires only the survivor (2).
    expect(seen).toEqual([1, 2, 2]);
  });
});

describe('BridgeShell — notification fan-out end-to-end', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });
  function harness() {
    const h = new HostHarness({
      hostContext: { theme: 'light', displayMode: 'inline' },
    });
    harnesses.push(h);
    return h;
  }

  it('delivers a tool-result with typed structuredContent', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    interface Out {
      total: number;
    }
    const received: Out[] = [];
    bridge.onToolResult<Out>((r) => {
      if (r.structuredContent) received.push(r.structuredContent);
    });

    h.notify(HostNotification.toolResult, {
      content: [{ type: 'text', text: 'done' }],
      structuredContent: { total: 42 },
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(received).toEqual([{ total: 42 }]);
  });

  it('delivers tool-input, tool-input-partial and tool-cancelled', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const events: string[] = [];
    bridge.onToolInput(() => events.push('input'));
    bridge.onToolInputPartial(() => events.push('partial'));
    bridge.onToolCancelled((p) => events.push(`cancelled:${p.reason}`));

    h.notify(HostNotification.toolInputPartial, { arguments: { q: 'ab' } });
    h.notify(HostNotification.toolInput, { arguments: { q: 'abc' } });
    h.notify(HostNotification.toolCancelled, { reason: 'user' });

    await new Promise((r) => setTimeout(r, 0));
    expect(events).toEqual(['partial', 'input', 'cancelled:user']);
  });

  it('host-context-changed patches the hostContext stores', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    expect(get(bridge.hostContext.theme)).toBe('light');

    const patches: unknown[] = [];
    bridge.onHostContextChanged((p) => patches.push(p));

    h.notify(HostNotification.hostContextChanged, { theme: 'dark' });
    await new Promise((r) => setTimeout(r, 0));

    expect(get(bridge.hostContext.theme)).toBe('dark');
    // A partial patch must not clear unrelated fields.
    expect(get(bridge.hostContext.displayMode)).toBe('inline');
    expect(patches).toEqual([{ theme: 'dark' }]);
  });

  it('size-changed fans out to subscribers', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const sizes: { width: number; height: number }[] = [];
    bridge.onSizeChanged((s) => sizes.push(s));
    h.notify(HostNotification.sizeChanged, { width: 320, height: 240 });

    await new Promise((r) => setTimeout(r, 0));
    expect(sizes).toEqual([{ width: 320, height: 240 }]);
  });

  it('delivers task-progress with a typed fraction + message (RFC §8.4)', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const points: { taskId: string; fraction?: number; message?: string }[] = [];
    const off = bridge.onTaskProgress((p) => points.push(p));

    h.notify(DockyardExtMethod.taskProgress, {
      taskId: 't1',
      fraction: 0.62,
      message: 'halfway',
      status: 'working',
    });
    // A status-only point (a phase change a fraction cannot express).
    h.notify(DockyardExtMethod.taskProgress, { taskId: 't1', message: 'finalising' });

    await new Promise((r) => setTimeout(r, 0));
    expect(points).toEqual([
      { taskId: 't1', fraction: 0.62, message: 'halfway', status: 'working' },
      { taskId: 't1', message: 'finalising' },
    ]);

    // Unsubscribe stops further delivery.
    off();
    h.notify(DockyardExtMethod.taskProgress, { taskId: 't1', fraction: 1 });
    await new Promise((r) => setTimeout(r, 0));
    expect(points).toHaveLength(2);
  });

  it('degrades cleanly when the host never forwards task-progress', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    // An App subscribes regardless; with no host forwarding, the subscriber
    // simply never fires — capability-driven degradation (RFC §7.5).
    const points: unknown[] = [];
    bridge.onTaskProgress((p) => points.push(p));
    await new Promise((r) => setTimeout(r, 0));
    expect(points).toEqual([]);
  });

  it('stops delivering notifications after close()', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const seen: unknown[] = [];
    bridge.onSizeChanged((s) => seen.push(s));
    bridge.close();

    h.notify(HostNotification.sizeChanged, { width: 1, height: 1 });
    await new Promise((r) => setTimeout(r, 0));
    expect(seen).toEqual([]);
  });
});

describe('BridgeShell — handshake retention + resource teardown (wiring audit)', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });
  function harness() {
    const h = new HostHarness({ hostContext: { theme: 'light' } });
    harnesses.push(h);
    return h;
  }

  it('retains the negotiated protocolVersion and hostInfo from ui/initialize', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    // protocol.ts promises the negotiated value is retained for forward-compat;
    // it was previously discarded.
    expect(bridge.protocolVersion).toBe('2026-01-26');
    expect(get(bridge.hostInfo)).toEqual({ name: 'test-host', version: '0.0.0' });
  });

  it('responds to the ui/resource-teardown request, then closes (D-182, item B)', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();
    expect(get(bridge.ready)).toBe(true);

    const seen: unknown[] = [];
    bridge.onSizeChanged((s) => seen.push(s));

    // The host tears the resource down as a REQUEST and waits for the View's
    // response before tearing the iframe down. The View must respond (not just
    // close) — a spec host blocks on it.
    const result = await h.sendRequest('ui/resource-teardown', {});
    expect(result).toEqual({}); // McpUiResourceTeardownResult — empty object

    expect(get(bridge.ready)).toBe(false);
    // A notification after teardown reaches no subscriber.
    h.notify(HostNotification.sizeChanged, { width: 1, height: 1 });
    await new Promise((r) => setTimeout(r, 0));
    expect(seen).toEqual([]);
  });
});
