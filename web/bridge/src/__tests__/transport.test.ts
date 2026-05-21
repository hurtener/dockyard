/**
 * transport.test.ts — JSON-RPC framing, request/response correlation,
 * notification dispatch, and forward-compatible handling of reserved methods.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  JsonRpcRequestError,
  portAsMessageSource,
  Transport,
  type MessageSink,
  type MessageSource,
} from '../transport.js';
import { ReservedNotification } from '../protocol.js';

/** A controllable in-test message edge: a sink + a source over one channel. */
function makeChannel(): {
  bridgeSink: MessageSink;
  bridgeSource: MessageSource;
  hostSink: MessageSink;
  hostSource: MessageSource;
  close: () => void;
} {
  const ch = new MessageChannel();
  ch.port1.start();
  ch.port2.start();
  return {
    bridgeSink: { postMessage: (m) => ch.port1.postMessage(m) },
    bridgeSource: portAsMessageSource(ch.port1),
    hostSink: { postMessage: (m) => ch.port2.postMessage(m) },
    hostSource: portAsMessageSource(ch.port2),
    close: () => {
      ch.port1.close();
      ch.port2.close();
    },
  };
}

describe('Transport', () => {
  let cleanup: (() => void)[] = [];
  afterEach(() => {
    cleanup.forEach((fn) => fn());
    cleanup = [];
  });

  it('correlates a request with its response result', async () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    // Host echoes every request id back with a result.
    ch.hostSource.addEventListener('message', (ev) => {
      const req = ev.data as { id: number; method: string };
      ch.hostSink.postMessage({
        jsonrpc: '2.0',
        id: req.id,
        result: { echoed: req.method },
      });
    });

    const result = await transport.request<{ echoed: string }>('demo/ping');
    expect(result.echoed).toBe('demo/ping');
  });

  it('correlates concurrent requests independently', async () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    ch.hostSource.addEventListener('message', (ev) => {
      const req = ev.data as { id: number; params: { n: number } };
      // Reply out of order to prove id-based correlation.
      setTimeout(
        () =>
          ch.hostSink.postMessage({
            jsonrpc: '2.0',
            id: req.id,
            result: req.params.n * 10,
          }),
        req.params.n === 1 ? 5 : 0,
      );
    });

    const [a, b] = await Promise.all([
      transport.request<number>('m', { n: 1 }),
      transport.request<number>('m', { n: 2 }),
    ]);
    expect(a).toBe(10);
    expect(b).toBe(20);
  });

  it('rejects with JsonRpcRequestError on a host error response', async () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    ch.hostSource.addEventListener('message', (ev) => {
      const req = ev.data as { id: number };
      ch.hostSink.postMessage({
        jsonrpc: '2.0',
        id: req.id,
        error: { code: -32601, message: 'method not found', data: { x: 1 } },
      });
    });

    await expect(transport.request('nope')).rejects.toBeInstanceOf(
      JsonRpcRequestError,
    );
    await expect(transport.request('nope')).rejects.toMatchObject({
      code: -32601,
      data: { x: 1 },
    });
  });

  it('dispatches inbound notifications to handlers', () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    const seen: { method: string; params: unknown }[] = [];
    transport.onNotification((params, method) => seen.push({ method, params }));

    ch.hostSink.postMessage({
      jsonrpc: '2.0',
      method: 'ui/notifications/size-changed',
      params: { width: 100, height: 50 },
    });

    return new Promise<void>((resolve) => {
      setTimeout(() => {
        expect(seen).toEqual([
          {
            method: 'ui/notifications/size-changed',
            params: { width: 100, height: 50 },
          },
        ]);
        resolve();
      }, 0);
    });
  });

  it('ignores reserved sandbox-proxy notifications (forward-compat)', () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    const handler = vi.fn();
    transport.onNotification(handler);

    ch.hostSink.postMessage({
      jsonrpc: '2.0',
      method: ReservedNotification.sandboxProxyReady,
      params: { html: '<div/>' },
    });

    return new Promise<void>((resolve) => {
      setTimeout(() => {
        expect(handler).not.toHaveBeenCalled();
        resolve();
      }, 0);
    });
  });

  it('drops non-JSON-RPC envelopes without throwing', () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });
    cleanup.push(() => transport.close());

    const handler = vi.fn();
    transport.onNotification(handler);
    ch.hostSink.postMessage('not json-rpc');
    ch.hostSink.postMessage({ jsonrpc: '1.0', method: 'x' });

    return new Promise<void>((resolve) => {
      setTimeout(() => {
        expect(handler).not.toHaveBeenCalled();
        resolve();
      }, 0);
    });
  });

  it('times out a request when the host never replies', async () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({
      peer: ch.bridgeSink,
      source: ch.bridgeSource,
      requestTimeoutMs: 10,
    });
    cleanup.push(() => transport.close());

    await expect(transport.request('silent')).rejects.toThrow(/timed out/);
  });

  it('rejects pending requests and refuses new ones after close', async () => {
    const ch = makeChannel();
    cleanup.push(ch.close);
    const transport = new Transport({ peer: ch.bridgeSink, source: ch.bridgeSource });

    const pending = transport.request('inflight');
    transport.close();
    await expect(pending).rejects.toThrow(/closed/);
    await expect(transport.request('after')).rejects.toThrow(/closed/);
    expect(() => transport.notify('after')).toThrow(/closed/);
  });

  it('throws when constructed with no message source and no window', () => {
    const g = globalThis as { window?: unknown };
    const savedWindow = g.window;
    // Simulate a non-browser context: no `window`, no explicit source.
    delete g.window;
    try {
      expect(
        () =>
          new Transport({ peer: { postMessage: () => {} }, source: undefined }),
      ).toThrow(/no message source/);
    } finally {
      g.window = savedWindow;
    }
  });
});
