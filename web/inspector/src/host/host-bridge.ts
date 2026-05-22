/**
 * host-bridge.ts — the HOST half of the `ui/` postMessage bridge.
 *
 * `web/bridge` ships the *View* half — the code that runs inside an MCP App's
 * sandboxed iframe (RFC §7.3). This module is its counterpart: the *host* half,
 * the side that renders an App and drives the bridge. The inspector is the one
 * client-shaped host Dockyard ships, and it is the consumer of this module
 * (RFC §12, §7.3).
 *
 * The protocol contract — every method name and wire shape — is NOT redefined
 * here: it is imported verbatim from `@dockyard/bridge`'s `protocol.ts`, so the
 * host half and the View half can never drift (CLAUDE.md §6 / P3 — the `ui/`
 * dialect lives in one place). This module owns only the host-side *behaviour*:
 *
 *   - it answers the View's `ui/initialize` request with an `InitializeResult`
 *     carrying `hostContext` and `hostCapabilities`;
 *   - it sends `ui/notifications/initialized` so the View resolves `ready`;
 *   - it fans host→view notifications (`tool-input`, `tool-result`, …);
 *   - it answers `ui/request-display-mode`, granting only modes in
 *     `availableDisplayModes` — capability-driven, never a host matrix
 *     (RFC §7.5);
 *   - it answers `ui/open-link` / `ui/message` / `ui/update-model-context` /
 *     `tools/call` with a benign result — the inspector is read-only and is
 *     never an arbitrary-execution proxy (RFC §12); a real `tools/call` proxy
 *     is wired by Phase 23.
 *
 * It is transport-agnostic: it takes a `MessageSink` (where it posts to the
 * View) and a `MessageSource` (where View messages arrive), exactly like the
 * bridge's `Transport`, so tests drive it over a `MessageChannel` and the live
 * inspector drives it over the App iframe's `contentWindow`.
 */

import {
  HostNotification,
  ViewMethod,
  ViewNotification,
  isJsonRpcRequest,
  type DisplayMode,
  type HostCapabilities,
  type HostContext,
  type InitializeParams,
  type InitializeResult,
  type JsonRpcMessage,
  type JsonRpcRequest,
  type RequestDisplayModeParams,
  type RequestDisplayModeResult,
} from '@dockyard/bridge';

/** The protocol revision the host half advertises (matches the View half). */
export const HOST_PROTOCOL_VERSION = '2026-01-26';

/** A "post a message" sink — `Window` / `MessagePort` / a test stub. */
export interface HostMessageSink {
  postMessage(message: unknown): void;
}

/** An inbound message event — the `.data` subset of `MessageEvent`. */
export interface HostInboundEvent {
  readonly data: unknown;
}

/** An inbound message source — `window` / a `MessagePort` / a stub. */
export interface HostMessageSource {
  addEventListener(type: 'message', listener: (ev: HostInboundEvent) => void): void;
  removeEventListener(
    type: 'message',
    listener: (ev: HostInboundEvent) => void,
  ): void;
  start?(): void;
}

/** A logged JSON-RPC message, surfaced in the inspector's RPC panel. */
export interface HostRpcLogEntry {
  /** `inbound` — a View → host message; `outbound` — a host → View message. */
  direction: 'inbound' | 'outbound';
  /** The JSON-RPC method, when the message is a request or notification. */
  method?: string;
  /** The raw JSON-RPC message. */
  message: JsonRpcMessage;
  /** When the entry was logged (epoch ms). */
  at: number;
}

/** Options for {@link HostBridge}. */
export interface HostBridgeOptions {
  /** Where host → View messages are posted (the App iframe's window). */
  peer: HostMessageSink;
  /** Where View → host messages arrive. */
  source: HostMessageSource;
  /** The host context delivered in the `ui/initialize` result. */
  hostContext?: HostContext;
  /** Host capabilities advertised in the handshake result. */
  hostCapabilities?: HostCapabilities;
  /** Host identity in the `ui/initialize` result. */
  hostInfo?: { name: string; version: string };
  /** Called for every JSON-RPC message in either direction (the RPC log). */
  onRpc?: (entry: HostRpcLogEntry) => void;
}

/** The default inspector host context — the inspector emulates a capable host. */
export function defaultHostContext(): HostContext {
  return {
    theme: 'light',
    displayMode: 'inline',
    availableDisplayModes: ['inline', 'fullscreen', 'pip'],
    locale: 'en-US',
    timeZone: 'UTC',
  };
}

/**
 * The host half of the `ui/` bridge. Construct it with a peer + source, then
 * call {@link HostBridge.start}; it answers the View's `ui/initialize`
 * handshake and stays live until {@link HostBridge.close}.
 *
 * It is single-threaded by construction — a browser message channel runs on the
 * event loop, so no locking is needed.
 */
export class HostBridge {
  private readonly peer: HostMessageSink;
  private readonly source: HostMessageSource;
  private hostContext: HostContext;
  private readonly hostCapabilities: HostCapabilities;
  private readonly hostInfo: { name: string; version: string };
  private readonly onRpc: ((entry: HostRpcLogEntry) => void) | undefined;

  private readonly boundOnMessage: (ev: HostInboundEvent) => void;
  private started = false;
  private closed = false;
  private initializeReceived = false;
  private viewReady = false;

  /** Resolves once the View has sent `ui/notifications/initialized`. */
  private resolveReady!: () => void;
  private readonly readyPromise: Promise<void>;

  constructor(options: HostBridgeOptions) {
    this.peer = options.peer;
    this.source = options.source;
    this.hostContext = options.hostContext ?? defaultHostContext();
    this.hostCapabilities = options.hostCapabilities ?? {};
    this.hostInfo = options.hostInfo ?? {
      name: 'dockyard-inspector',
      version: '0.1.0',
    };
    this.onRpc = options.onRpc;
    this.boundOnMessage = (ev) => this.onMessage(ev.data);
    this.readyPromise = new Promise<void>((resolve) => {
      this.resolveReady = resolve;
    });
  }

  /** Begins listening for View messages. Idempotent. */
  start(): void {
    if (this.started || this.closed) return;
    this.started = true;
    this.source.addEventListener('message', this.boundOnMessage);
    this.source.start?.();
  }

  /**
   * Resolves once the `ui/initialize` handshake is complete — the View sent
   * `ui/initialize`, the host answered it, and the host sent
   * `ui/notifications/initialized` (the host→View direction of the dialect,
   * brief 01 §2.4). At that point the App has rendered and its bridge is up —
   * the "the App completed its handshake" signal the inspector waits on.
   */
  ready(): Promise<void> {
    return this.readyPromise;
  }

  /** True once the `ui/initialize` handshake is complete. */
  get isReady(): boolean {
    return this.viewReady;
  }

  /** True once the View has sent `ui/initialize`. */
  get handshakeStarted(): boolean {
    return this.initializeReceived;
  }

  /** The display mode currently in effect. */
  get displayMode(): DisplayMode | undefined {
    return this.hostContext.displayMode;
  }

  /** The display modes the host will grant. */
  get availableDisplayModes(): DisplayMode[] {
    return this.hostContext.availableDisplayModes ?? [];
  }

  /* --- host → View notifications -------------------------------------- */

  /** Notifies the View of a tool's resolved input arguments. */
  sendToolInput(args: unknown): void {
    this.notify(HostNotification.toolInput, { arguments: args });
  }

  /** Notifies the View of a tool result (`structuredContent` + content). */
  sendToolResult(result: unknown): void {
    this.notify(HostNotification.toolResult, result);
  }

  /** Notifies the View that a host-context field changed (a partial patch). */
  patchHostContext(patch: Partial<HostContext>): void {
    this.hostContext = { ...this.hostContext, ...patch };
    this.notify(HostNotification.hostContextChanged, patch);
  }

  /** The current, full host context. */
  currentHostContext(): HostContext {
    return { ...this.hostContext };
  }

  /** Tears the bridge down: drops the message listener. Idempotent. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    if (this.started) {
      this.source.removeEventListener('message', this.boundOnMessage);
    }
  }

  /* --- internal ------------------------------------------------------- */

  private onMessage(data: unknown): void {
    if (this.closed || !isEnvelope(data)) return;
    const message = data as JsonRpcMessage;
    this.log('inbound', message);

    if (isJsonRpcRequest(message)) {
      this.handleRequest(message);
      return;
    }
    // A View notification (or a response — unexpected, the host sends no
    // requests in this revision) is tolerated and dropped: forward-
    // compatibility, never an assumption (brief 01 §4.4). The host's own
    // handshake completes when it has *sent* `ui/notifications/initialized`
    // (see handleInitialize) — there is no inbound View `initialized` to wait
    // on; that notification is host→View only.
  }

  private handleRequest(req: JsonRpcRequest): void {
    switch (req.method) {
      case ViewMethod.initialize:
        this.handleInitialize(req);
        return;
      case ViewMethod.requestDisplayMode:
        this.handleRequestDisplayMode(req);
        return;
      case ViewMethod.openLink:
      case ViewMethod.message:
      case ViewMethod.updateModelContext:
        // The inspector is read-only — it acknowledges these but performs no
        // host-side side effect (RFC §12). A real implementation is Phase 23+.
        this.respond(req.id, {});
        return;
      case ViewMethod.callTool:
        // Phase 22 seam: a real `tools/call` proxy to the running MCP server is
        // Phase 23 (the fixture switcher / live invocation). Until then the
        // host answers with an explicit "not wired" JSON-RPC error so an App
        // degrades gracefully rather than hanging.
        this.respondError(
          req.id,
          -32601,
          'tools/call proxy is not wired in the Phase 22 inspector core',
        );
        return;
      default:
        // An unknown method — answer with a method-not-found error.
        this.respondError(req.id, -32601, `unknown ui/ method "${req.method}"`);
    }
  }

  private handleInitialize(req: JsonRpcRequest): void {
    this.initializeReceived = true;
    const params = (req.params ?? {}) as Partial<InitializeParams>;
    // The View advertises the display modes its build supports; the host
    // narrows `availableDisplayModes` to that intersection — capability-driven
    // negotiation, never a host matrix (RFC §7.5).
    const appModes = params.capabilities?.appCapabilities?.displayModes;
    if (appModes && appModes.length > 0) {
      const granted = (this.hostContext.availableDisplayModes ?? []).filter(
        (m) => appModes.includes(m),
      );
      this.hostContext = {
        ...this.hostContext,
        availableDisplayModes: granted,
        displayMode: granted.includes(this.hostContext.displayMode ?? 'inline')
          ? this.hostContext.displayMode
          : (granted[0] ?? 'inline'),
      };
    }
    const result: InitializeResult = {
      protocolVersion: HOST_PROTOCOL_VERSION,
      hostContext: this.hostContext,
      hostCapabilities: this.hostCapabilities,
      hostInfo: this.hostInfo,
    };
    this.respond(req.id, result);
    // Tell the View the host is ready — the View waits on this before
    // resolving its own `ready` (brief 01 §2.4). Sending it completes the
    // host's side of the `ui/initialize` handshake.
    this.notify(ViewNotification.initialized, {});
    this.viewReady = true;
    this.resolveReady();
  }

  private handleRequestDisplayMode(req: JsonRpcRequest): void {
    const params = (req.params ?? {}) as Partial<RequestDisplayModeParams>;
    const requested = params.mode;
    const available = this.hostContext.availableDisplayModes ?? [];
    const granted = requested !== undefined && available.includes(requested);
    const mode: DisplayMode = granted
      ? (requested as DisplayMode)
      : (this.hostContext.displayMode ?? 'inline');
    if (granted) {
      this.patchHostContext({ displayMode: mode });
    }
    const result: RequestDisplayModeResult = { mode, granted };
    this.respond(req.id, result);
  }

  private notify(method: string, params: unknown): void {
    if (this.closed) return;
    const message = { jsonrpc: '2.0' as const, method, params };
    this.log('outbound', message);
    this.peer.postMessage(message);
  }

  private respond(id: JsonRpcRequest['id'], result: unknown): void {
    if (this.closed) return;
    const message = { jsonrpc: '2.0' as const, id, result };
    this.log('outbound', message);
    this.peer.postMessage(message);
  }

  private respondError(
    id: JsonRpcRequest['id'],
    code: number,
    text: string,
  ): void {
    if (this.closed) return;
    const message = {
      jsonrpc: '2.0' as const,
      id,
      error: { code, message: text },
    };
    this.log('outbound', message);
    this.peer.postMessage(message);
  }

  private log(direction: 'inbound' | 'outbound', message: JsonRpcMessage): void {
    if (!this.onRpc) return;
    const method =
      'method' in message && typeof message.method === 'string'
        ? message.method
        : undefined;
    this.onRpc({ direction, method, message, at: Date.now() });
  }
}

/** True when `data` is a plausible JSON-RPC 2.0 envelope. */
function isEnvelope(data: unknown): boolean {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as { jsonrpc?: unknown }).jsonrpc === '2.0'
  );
}
