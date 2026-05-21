/**
 * view-state.ts — framework-managed `_meta.viewUUID`-keyed view-state.
 *
 * An MCP App is re-rendered by the host (a result re-push, a display-mode
 * change, a re-mount). `_meta.viewUUID` is the spec hook that identifies "the
 * same view" across those re-renders (brief 01 §2.6). RFC §7.3 settles that the
 * bridge *framework-manages* this — resolving brief 01 open question Q-9 in
 * favour of the framework, so App authors never hand-roll persistence (D-060).
 *
 * The store keeps one snapshot per `viewUUID`. A re-render of the same view
 * recovers its snapshot; a different `viewUUID` is fully isolated. The store is
 * an in-memory map: it survives a re-render within one bridge session, which is
 * the lifetime `viewUUID` is defined over — it is not a persistence layer
 * across host sessions (that is the host's durable store, RFC §13).
 */

import {
  get,
  writable,
  type Readable,
  type Writable,
} from 'svelte/store';

/**
 * A handle to one view's persisted state, keyed by its `viewUUID`. The handle
 * is a Svelte store, so an App component binds to it reactively; writes are
 * retained for the next re-render of the same `viewUUID`.
 */
export interface ViewStateHandle<T> {
  /** The `_meta.viewUUID` this handle is scoped to. */
  readonly uuid: string;
  /** The reactive snapshot store. */
  readonly state: Readable<T | undefined>;
  /** Replaces the snapshot. */
  set(value: T): void;
  /** Updates the snapshot from its current value. */
  update(fn: (current: T | undefined) => T): void;
  /** The current snapshot without subscribing. */
  current(): T | undefined;
  /** Drops this view's snapshot. */
  clear(): void;
}

/**
 * Owns the `viewUUID` → snapshot map. One instance per bridge; `handle(uuid)`
 * is idempotent — the same `uuid` always returns the same backing store, which
 * is what makes view-state survive a re-render.
 */
export class ViewStateStore {
  private readonly stores = new Map<string, Writable<unknown>>();

  /**
   * Returns the handle for a `viewUUID`. Calling it again with the same
   * `uuid` returns a handle over the *same* snapshot — that round-trip across
   * re-renders is the whole point (RFC §7.3).
   */
  handle<T>(uuid: string): ViewStateHandle<T> {
    if (!uuid) {
      throw new Error('ViewStateStore: viewUUID must be a non-empty string');
    }
    let store = this.stores.get(uuid);
    if (!store) {
      store = writable<unknown>(undefined);
      this.stores.set(uuid, store);
    }
    const backing = store as Writable<T | undefined>;
    return {
      uuid,
      state: backing,
      set: (value: T) => backing.set(value),
      update: (fn) => backing.update((c) => fn(c)),
      current: () => get(backing),
      clear: () => {
        backing.set(undefined);
        this.stores.delete(uuid);
      },
    };
  }

  /** True when a snapshot exists for `uuid`. */
  has(uuid: string): boolean {
    return this.stores.has(uuid);
  }

  /** The `viewUUID`s with a live snapshot. */
  keys(): string[] {
    return [...this.stores.keys()];
  }

  /** Drops every view's snapshot — used on bridge teardown. */
  clear(): void {
    for (const store of this.stores.values()) {
      store.set(undefined);
    }
    this.stores.clear();
  }
}

/**
 * Generates a `viewUUID`. The bridge mints one when the host has not supplied a
 * `viewUUID` in `_meta`, so view-state still has a stable key within a session.
 * Uses `crypto.randomUUID` when available, with a non-crypto fallback (the UUID
 * is a correlation key, not a security token).
 */
export function newViewUUID(): string {
  const c = (globalThis as { crypto?: { randomUUID?: () => string } }).crypto;
  if (c?.randomUUID) {
    return c.randomUUID();
  }
  // Fallback: RFC-4122-shaped, non-cryptographic. Adequate as a session key.
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (ch) => {
    const r = (Math.random() * 16) | 0;
    const v = ch === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}
