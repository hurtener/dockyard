/**
 * view-state.test.ts — framework-managed `_meta.viewUUID`-keyed view-state:
 * the round-trip across a simulated re-render and isolation between UUIDs.
 */
import { afterEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { createBridge } from '../bridge.js';
import { newViewUUID, ViewStateStore } from '../view-state.js';
import { HostHarness } from './harness.js';

describe('ViewStateStore', () => {
  it('round-trips a snapshot across a simulated re-render', () => {
    const store = new ViewStateStore();
    const uuid = 'view-1';

    // First render: the component obtains its handle and writes state.
    const first = store.handle<{ count: number }>(uuid);
    first.set({ count: 7 });

    // Re-render: a fresh handle for the SAME viewUUID recovers the snapshot.
    const second = store.handle<{ count: number }>(uuid);
    expect(second.current()).toEqual({ count: 7 });
    expect(get(second.state)).toEqual({ count: 7 });
  });

  it('isolates snapshots between different viewUUIDs', () => {
    const store = new ViewStateStore();
    store.handle<number>('view-a').set(1);
    store.handle<number>('view-b').set(2);

    expect(store.handle<number>('view-a').current()).toBe(1);
    expect(store.handle<number>('view-b').current()).toBe(2);
  });

  it('update() mutates from the current snapshot', () => {
    const store = new ViewStateStore();
    const h = store.handle<number>('v');
    h.set(10);
    h.update((c) => (c ?? 0) + 5);
    expect(h.current()).toBe(15);
  });

  it('handle() is idempotent — same uuid, same backing store', () => {
    const store = new ViewStateStore();
    const a = store.handle('v');
    const b = store.handle('v');
    a.set('x');
    expect(b.current()).toBe('x');
  });

  it('clear() on a handle drops that view only', () => {
    const store = new ViewStateStore();
    store.handle<number>('keep').set(1);
    const drop = store.handle<number>('drop');
    drop.set(2);
    drop.clear();

    expect(store.has('drop')).toBe(false);
    expect(store.has('keep')).toBe(true);
    expect(store.keys()).toEqual(['keep']);
  });

  it('rejects an empty viewUUID', () => {
    const store = new ViewStateStore();
    expect(() => store.handle('')).toThrow(/non-empty/);
  });

  it('clear() drops every view', () => {
    const store = new ViewStateStore();
    store.handle('a').set(1);
    store.handle('b').set(2);
    store.clear();
    expect(store.keys()).toEqual([]);
  });
});

describe('newViewUUID', () => {
  it('mints unique, RFC-4122-shaped ids', () => {
    const a = newViewUUID();
    const b = newViewUUID();
    expect(a).not.toBe(b);
    expect(a).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });
});

describe('BridgeShell — view-state integration', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });

  it('viewState(uuid) round-trips across a re-render via the bridge', async () => {
    const h = new HostHarness();
    harnesses.push(h);
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const uuid = 'app-view-42';
    bridge.viewState<{ tab: string }>(uuid).set({ tab: 'details' });

    // Simulate a re-render: a new component asks for the same viewUUID.
    expect(bridge.hasViewState(uuid)).toBe(true);
    expect(bridge.viewState<{ tab: string }>(uuid).current()).toEqual({
      tab: 'details',
    });
  });

  it('viewState() with no uuid mints a fresh, isolated view', async () => {
    const h = new HostHarness();
    harnesses.push(h);
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const a = bridge.viewState<number>();
    const b = bridge.viewState<number>();
    expect(a.uuid).not.toBe(b.uuid);
    a.set(1);
    expect(b.current()).toBeUndefined();
  });

  it('callTool attaches _meta.viewUUID when a view is given', async () => {
    const h = new HostHarness();
    harnesses.push(h);
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    const view = bridge.viewState('v-meta');
    await bridge.callTool('refresh', { id: 1 }, view);

    const req = h.lastRequest('tools/call')!;
    expect(req.params).toMatchObject({
      name: 'refresh',
      arguments: { id: 1 },
      _meta: { viewUUID: 'v-meta' },
    });
  });
});
