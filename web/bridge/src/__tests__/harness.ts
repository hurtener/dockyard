/**
 * harness.ts — the in-test host harness.
 *
 * Plays the *host* half of the `ui/` postMessage dialect so the bridge's View
 * half is exercised end-to-end over a real `MessageChannel`, not a mocked
 * transport. This is the cross-half wiring proof for a TS library (AGENTS.md
 * §17 in spirit) and an executable reference for the host shape the inspector
 * (Phase 22) must agree on.
 *
 * The two `MessageChannel` ports stand in for the View↔host postMessage edges:
 * `bridgePort` is what the bridge posts to / listens on; `hostPort` is the
 * harness side.
 */

import {
  ViewMethod,
  ViewNotification,
  type DisplayMode,
  type HostContext,
  type InitializeResult,
  type JsonRpcMessage,
  type JsonRpcRequest,
  type JsonRpcResponse,
} from '../protocol.js';
import {
  portAsMessageSource,
  type MessageSink,
  type MessageSource,
} from '../transport.js';

/** A request the bridge sent, captured for assertions. */
export interface CapturedRequest {
  method: string;
  params: unknown;
  id: string | number;
}

export interface HarnessOptions {
  /** The `hostContext` returned in the `ui/initialize` result. */
  hostContext?: HostContext;
  /** Host capabilities returned in the result. */
  hostCapabilities?: Record<string, unknown>;
  /** Display modes the host grants for `ui/request-display-mode`. */
  grantModes?: DisplayMode[];
}

/**
 * The host harness. Construct it, pass `harness.peer` / `harness.source` to the
 * bridge, then assert against `harness.requests` or drive the host with
 * `harness.notify(...)`.
 */
export class HostHarness {
  /** The sink the bridge posts outbound messages to. */
  readonly peer: MessageSink;
  /** The source the bridge listens on for inbound messages. */
  readonly source: MessageSource;

  /** Every request the bridge has sent, in order. */
  readonly requests: CapturedRequest[] = [];

  /** Every View→host notification the bridge has sent, in order. */
  readonly notifications: { method: string; params: unknown }[] = [];

  private readonly channel = new MessageChannel();
  private readonly hostPort: MessagePort;
  private readonly options: HarnessOptions;
  private grantModes: DisplayMode[];
  /** Per-method override for the next response, keyed by method name. */
  private readonly responders = new Map<
    string,
    (req: JsonRpcRequest) => JsonRpcResponse
  >();

  constructor(options: HarnessOptions = {}) {
    this.options = options;
    this.grantModes = options.grantModes ?? ['inline', 'fullscreen', 'pip'];

    const bridgePort = this.channel.port1;
    this.hostPort = this.channel.port2;
    bridgePort.start();
    this.hostPort.start();

    // The bridge posts to / listens on port1.
    this.peer = { postMessage: (m) => bridgePort.postMessage(m) };
    this.source = portAsMessageSource(bridgePort);

    this.hostPort.addEventListener('message', (ev) =>
      this.onBridgeMessage(ev.data),
    );
  }

  /** Overrides the host response for one method (e.g. to return an error). */
  respondWith(
    method: string,
    responder: (req: JsonRpcRequest) => JsonRpcResponse,
  ): void {
    this.responders.set(method, responder);
  }

  /** Restricts the modes the host will grant. */
  setGrantModes(modes: DisplayMode[]): void {
    this.grantModes = modes;
  }

  /** Posts a host → View notification (the harness drives the host side). */
  notify(method: string, params?: unknown): void {
    this.hostPort.postMessage({ jsonrpc: '2.0', method, params });
  }

  /**
   * Simulates a NON-spec host that pushes `ui/notifications/initialized`
   * host→View. A spec host never does this; the bridge must ignore it (D-180).
   */
  sendInitialized(): void {
    this.notify(ViewNotification.initialized);
  }

  /** The most recent request for `method`, if any. */
  lastRequest(method: string): CapturedRequest | undefined {
    for (let i = this.requests.length - 1; i >= 0; i--) {
      if (this.requests[i]!.method === method) return this.requests[i];
    }
    return undefined;
  }

  /** The most recent View→host notification for `method`, if any. */
  lastNotification(
    method: string,
  ): { method: string; params: unknown } | undefined {
    for (let i = this.notifications.length - 1; i >= 0; i--) {
      if (this.notifications[i]!.method === method) return this.notifications[i];
    }
    return undefined;
  }

  /** Tears the channel down. */
  close(): void {
    this.channel.port1.close();
    this.hostPort.close();
  }

  private onBridgeMessage(data: unknown): void {
    if (
      typeof data !== 'object' ||
      data === null ||
      (data as { jsonrpc?: unknown }).jsonrpc !== '2.0'
    ) {
      return;
    }
    const message = data as JsonRpcMessage;
    if (!('method' in message)) return; // a response to a host→View request
    if (!('id' in message)) {
      // A View→host notification (initialized, size-changed, elicitation).
      const note = message as { method: string; params?: unknown };
      this.notifications.push({ method: note.method, params: note.params });
      return;
    }
    const req = message as JsonRpcRequest;
    this.requests.push({ method: req.method, params: req.params, id: req.id });

    const override = this.responders.get(req.method);
    if (override) {
      this.hostPort.postMessage(override(req));
      return;
    }
    this.hostPort.postMessage(this.defaultResponse(req));
  }

  private defaultResponse(req: JsonRpcRequest): JsonRpcResponse {
    switch (req.method) {
      case ViewMethod.initialize: {
        const result: InitializeResult = {
          protocolVersion: '2026-01-26',
          hostContext: this.options.hostContext ?? {
            theme: 'light',
            displayMode: 'inline',
            availableDisplayModes: ['inline', 'fullscreen', 'pip'],
          },
          hostCapabilities: this.options.hostCapabilities ?? {},
          hostInfo: { name: 'test-host', version: '0.0.0' },
        };
        // A spec-compliant host replies to `ui/initialize` and then waits; the
        // *View* sends `ui/notifications/initialized` (the host never sends one,
        // and the bridge never waits to receive it — D-180).
        return { jsonrpc: '2.0', id: req.id, result };
      }
      case ViewMethod.requestDisplayMode: {
        const mode = (req.params as { mode: DisplayMode }).mode;
        const granted = this.grantModes.includes(mode);
        return {
          jsonrpc: '2.0',
          id: req.id,
          result: { mode: granted ? mode : 'inline', granted },
        };
      }
      default:
        // open-link, message, update-model-context, tools/call: ack with null.
        return { jsonrpc: '2.0', id: req.id, result: null };
    }
  }
}
