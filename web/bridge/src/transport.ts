/**
 * transport.ts — the postMessage JSON-RPC transport.
 *
 * The View↔host channel is a JSON-RPC 2.0 dialect carried over
 * `window.postMessage` (brief 01 §2.4). This module owns framing,
 * request/response correlation, and notification dispatch. It is deliberately
 * window-agnostic: it takes a `MessagePort`-or-`Window`-shaped peer plus an
 * inbound `EventTarget`, so the in-test host harness can drive it over a
 * `MessageChannel` without a real iframe.
 */

import {
  isJsonRpcNotification,
  isJsonRpcRequest,
  isJsonRpcResponse,
  ReservedNotification,
  type JsonRpcId,
  type JsonRpcMessage,
  type JsonRpcNotification,
  type JsonRpcRequest,
  type JsonRpcResponse,
} from './protocol.js';

/** A minimal "post a message" sink — `Window`, `MessagePort`, or a test stub. */
export interface MessageSink {
  postMessage(message: unknown): void;
}

/** An inbound message event — the subset of `MessageEvent` the bridge reads. */
export interface InboundMessageEvent {
  readonly data: unknown;
}

/** A `message`-event listener. */
export type InboundMessageListener = (ev: InboundMessageEvent) => void;

/**
 * A minimal inbound message source — `window`, a `MessagePort`, or a stub.
 * The listener is typed over `InboundMessageEvent` (only `.data` is read); a
 * real `MessageEvent` is structurally assignable, so `window`/`MessagePort`
 * satisfy this interface without a cast.
 */
export interface MessageSource {
  addEventListener(
    type: 'message',
    listener: InboundMessageListener,
  ): void;
  removeEventListener(
    type: 'message',
    listener: InboundMessageListener,
  ): void;
  start?(): void;
}

export interface TransportOptions {
  /** Where outbound messages are posted (the host). */
  peer: MessageSink;
  /** Where inbound messages arrive from (defaults to `globalThis.window`). */
  source?: MessageSource;
  /** Request timeout in ms; 0 disables it. Default 30000. */
  requestTimeoutMs?: number;
}

/** A handler for an inbound host → View notification. */
export type NotificationHandler = (
  params: unknown,
  method: string,
) => void;

interface PendingRequest {
  resolve: (result: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout> | undefined;
}

const RESERVED = new Set<string>(Object.values(ReservedNotification));

/** Raised when the host returns a JSON-RPC error for a request. */
export class JsonRpcRequestError extends Error {
  readonly code: number;
  readonly data: unknown;
  constructor(code: number, message: string, data?: unknown) {
    super(message);
    this.name = 'JsonRpcRequestError';
    this.code = code;
    this.data = data;
  }
}

/**
 * The postMessage JSON-RPC transport. Single-threaded by construction — a
 * browser message channel runs on the event loop, so no locking is needed.
 */
export class Transport {
  private readonly peer: MessageSink;
  private readonly source: MessageSource;
  private readonly requestTimeoutMs: number;
  private readonly pending = new Map<JsonRpcId, PendingRequest>();
  private readonly handlers = new Set<NotificationHandler>();
  private requestHandler:
    | ((params: unknown, method: string, id: JsonRpcId) => void)
    | undefined;
  private nextId = 1;
  private closed = false;
  private readonly boundOnMessage: (ev: { data: unknown }) => void;

  constructor(options: TransportOptions) {
    this.peer = options.peer;
    const src = options.source ?? (globalThis as { window?: MessageSource }).window;
    if (!src) {
      throw new Error(
        'Transport: no message source — pass options.source outside a browser',
      );
    }
    this.source = src;
    this.requestTimeoutMs = options.requestTimeoutMs ?? 30000;
    this.boundOnMessage = (ev) => this.onMessage(ev.data);
    this.source.addEventListener('message', this.boundOnMessage);
    this.source.start?.();
  }

  /** Sends a JSON-RPC request and resolves with the host's result. */
  request<R = unknown>(method: string, params?: unknown): Promise<R> {
    if (this.closed) {
      return Promise.reject(new Error('Transport: closed'));
    }
    const id = this.nextId++;
    const message: JsonRpcRequest = { jsonrpc: '2.0', id, method, params };
    return new Promise<R>((resolve, reject) => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      if (this.requestTimeoutMs > 0) {
        timer = setTimeout(() => {
          this.pending.delete(id);
          reject(
            new Error(
              `Transport: request "${method}" timed out after ` +
                `${this.requestTimeoutMs}ms`,
            ),
          );
        }, this.requestTimeoutMs);
      }
      this.pending.set(id, {
        resolve: resolve as (r: unknown) => void,
        reject,
        timer,
      });
      this.peer.postMessage(message);
    });
  }

  /** Sends a JSON-RPC notification (fire-and-forget, no response). */
  notify(method: string, params?: unknown): void {
    if (this.closed) {
      throw new Error('Transport: closed');
    }
    const message: JsonRpcNotification = { jsonrpc: '2.0', method, params };
    this.peer.postMessage(message);
  }

  /**
   * Registers the handler for inbound host → View *requests* (the host half of
   * the dialect makes these; the only one in this revision is
   * `ui/resource-teardown`). The handler must call {@link respond} with the
   * request id. Returns an unsubscribe function. A single handler suffices — the
   * bridge routes by method internally.
   */
  onRequest(
    handler: (params: unknown, method: string, id: JsonRpcId) => void,
  ): () => void {
    this.requestHandler = handler;
    return () => {
      if (this.requestHandler === handler) this.requestHandler = undefined;
    };
  }

  /** Sends a JSON-RPC result for an inbound host → View request. */
  respond(id: JsonRpcId, result: unknown): void {
    if (this.closed) return;
    const message: JsonRpcResponse = { jsonrpc: '2.0', id, result };
    this.peer.postMessage(message);
  }

  /**
   * Registers a host → View notification handler. Returns an unsubscribe
   * function. Reserved sandbox-proxy notifications never reach a handler.
   */
  onNotification(handler: NotificationHandler): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  /** Tears down the transport: rejects pending requests, drops listeners. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.source.removeEventListener('message', this.boundOnMessage);
    for (const [, p] of this.pending) {
      if (p.timer) clearTimeout(p.timer);
      p.reject(new Error('Transport: closed'));
    }
    this.pending.clear();
    this.handlers.clear();
  }

  private onMessage(data: unknown): void {
    if (this.closed || !isJsonRpcEnvelope(data)) return;
    const message = data as JsonRpcMessage;

    if (isJsonRpcResponse(message)) {
      this.resolveResponse(message);
      return;
    }
    if (isJsonRpcNotification(message)) {
      // Forward-compatibility: reserved sandbox-proxy notifications are
      // tolerated and dropped, never an error (brief 01 §4.4).
      if (RESERVED.has(message.method)) return;
      for (const h of this.handlers) {
        h(message.params, message.method);
      }
      return;
    }
    // An inbound *request* from the host (e.g. `ui/resource-teardown`) — route
    // to the registered request handler, which must `respond(id, …)`. If none
    // is registered, ignore it rather than fail (forward-compatibility).
    // (TS over-narrows `message` to `never` here because a request is
    // structurally a superset of a notification, so reach for `data`.)
    const req = data as JsonRpcRequest;
    if (this.requestHandler && isJsonRpcRequest(req)) {
      this.requestHandler(req.params, req.method, req.id);
    }
  }

  private resolveResponse(message: JsonRpcResponse): void {
    const pending = this.pending.get(message.id);
    if (!pending) return; // unknown / already-settled id — ignore.
    this.pending.delete(message.id);
    if (pending.timer) clearTimeout(pending.timer);
    if (message.error) {
      pending.reject(
        new JsonRpcRequestError(
          message.error.code,
          message.error.message,
          message.error.data,
        ),
      );
      return;
    }
    pending.resolve(message.result);
  }
}

/**
 * Adapts a DOM `MessagePort`-or-`Window`-shaped object to a {@link MessageSource}.
 * A real `addEventListener('message', …)` is typed over the broad DOM `Event`;
 * this wrapper narrows it to {@link InboundMessageEvent} so a `MessagePort` or
 * `window` can be passed where a `MessageSource` is expected. The bridge uses it
 * internally for `window`; tests use it for `MessageChannel` ports.
 */
export function portAsMessageSource(port: {
  addEventListener(type: 'message', listener: (ev: MessageEvent) => void): void;
  removeEventListener(
    type: 'message',
    listener: (ev: MessageEvent) => void,
  ): void;
  start?(): void;
}): MessageSource {
  // One stable wrapper per registered listener, so removeEventListener matches.
  const wrappers = new WeakMap<
    InboundMessageListener,
    (ev: MessageEvent) => void
  >();
  return {
    addEventListener(type, listener) {
      const wrapped = (ev: MessageEvent): void => listener({ data: ev.data });
      wrappers.set(listener, wrapped);
      port.addEventListener(type, wrapped);
    },
    removeEventListener(type, listener) {
      const wrapped = wrappers.get(listener);
      if (wrapped) {
        port.removeEventListener(type, wrapped);
        wrappers.delete(listener);
      }
    },
    start: port.start ? () => port.start?.() : undefined,
  };
}

/** True when `data` is a plausible JSON-RPC 2.0 envelope. */
function isJsonRpcEnvelope(data: unknown): boolean {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as { jsonrpc?: unknown }).jsonrpc === '2.0'
  );
}
