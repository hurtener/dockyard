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
 * here: it is imported verbatim from `dockyard-bridge`'s `protocol.ts`, so the
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
  isJsonRpcNotification,
  isJsonRpcRequest,
  type DisplayMode,
  type ElicitationResponseParams,
  type HostCapabilities,
  type HostContext,
  type InitializeParams,
  type InitializeResult,
  type JsonRpcMessage,
  type JsonRpcNotification,
  type JsonRpcRequest,
  type RequestDisplayModeParams,
  type RequestDisplayModeResult,
  type TaskProgressParams,
} from 'dockyard-bridge';

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

/**
 * A fixture-backed `tools/call` outcome — the shape the fixture switcher hands
 * the host bridge. A successful fixture carries `structuredContent`; a failed
 * fixture carries an `error`. The bridge answers the App's `tools/call` with
 * whichever is set.
 */
export interface CallToolFixtureResult {
  /** Synthetic structured content — present for a successful fixture. */
  structuredContent?: unknown;
  /** A plain-text content line. */
  text?: string;
  /** A JSON-RPC error — present for a failed fixture. */
  error?: { code: number; message: string };
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

  /**
   * The fixture-backed `tools/call` responder. Phase 23's fixture switcher
   * sets this so a `tools/call` from the App is answered from the active
   * fixture (RFC §12 — the inspector closes the Phase 22 not-wired seam with a
   * fixture, never a live arbitrary-execution proxy). When unset, `tools/call`
   * still answers with the explicit "not wired" JSON-RPC error so an App
   * degrades gracefully rather than hanging.
   */
  private callToolResponder:
    | ((params: unknown) => CallToolFixtureResult)
    | undefined;

  /**
   * The elicitation-response delivery sink (Phase 25 / D-134). The App posts
   * a `ui/notifications/elicitation-response` notification when the user
   * answers a task's `input_required` prompt; the inspector forwards it
   * here. The inspector wires this to a backend POST that opens a
   * short-lived MCP client and calls `tasks/result` against the attached
   * server. When unset, the notification is logged (so it shows in the RPC
   * panel) and dropped — no panic, no thrown error, exactly like the
   * pre-Phase-23 `tools/call` "not wired" posture.
   */
  private elicitationResponder:
    | ((params: ElicitationResponseParams) => void)
    | undefined;

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

  /**
   * Forwards a long-running task's progress point to the View (RFC §8.4).
   * The inspector wires the attached server's `obs/v1` `task.progress`
   * stream to this, so an App's card renders a live "62%" through
   * `dockyard inspect`. It is host→View only and advisory — an App that
   * does not subscribe (or a run with no tasks) is unaffected.
   */
  sendTaskProgress(params: TaskProgressParams): void {
    this.notify(HostNotification.taskProgress, params);
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

  /**
   * Sets the fixture-backed `tools/call` responder (RFC §12 — the fixture
   * switcher). After this, a `tools/call` request from the App is answered
   * from the fixture the responder returns: a successful fixture resolves the
   * call with synthetic `structuredContent`, a failed fixture resolves it with
   * a JSON-RPC error. Passing `undefined` reverts to the "not wired" error.
   * It is a dev-test response, never a live arbitrary-execution proxy.
   */
  setCallToolResponder(
    responder: ((params: unknown) => CallToolFixtureResult) | undefined,
  ): void {
    this.callToolResponder = responder;
  }

  /**
   * Sets the elicitation-response delivery sink (Phase 25 / D-134). The
   * inspector wires this to a backend POST that forwards the App's reply
   * to the attached server's `tasks/result` endpoint, resuming the
   * `input_required` task. The notification is fire-and-forget — the
   * sink returns no value; the App observes the task's terminal status
   * through the subsequent `tool-result` push the host (or another
   * subscriber) delivers, and through the inspector's Tasks panel.
   * Passing `undefined` reverts to the log-and-drop posture so an App
   * still runs in a detached inspector.
   */
  setElicitationResponder(
    responder: ((params: ElicitationResponseParams) => void) | undefined,
  ): void {
    this.elicitationResponder = responder;
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
    if (isJsonRpcNotification(message)) {
      this.handleNotification(message);
      return;
    }
    // A bare JSON-RPC response from the View is unexpected (the host sends
    // no requests in this revision); tolerate and drop, forward-compatibility
    // — never an assumption (brief 01 §4.4).
  }

  /**
   * Routes an inbound View notification. The View → host direction carries
   * a small, fixed set of notifications in this revision; an unknown
   * method is tolerated and dropped (forward-compatibility, brief 01
   * §4.4) — never an error.
   */
  private handleNotification(notification: JsonRpcNotification): void {
    switch (notification.method) {
      case ViewNotification.elicitationResponse: {
        // Phase 25 / D-134 — the App's reply to a task's input_required
        // prompt. Forward to the wired responder; the inspector wires
        // this to its backend POST that calls tasks/result on the
        // attached server. With no responder, the notification stays
        // visible in the RPC log so a developer sees an unwired pipe.
        const params = (notification.params ?? {}) as ElicitationResponseParams;
        if (!params || typeof params.taskId !== 'string' || params.taskId === '') {
          // Malformed payload — keep the log entry, drop the dispatch.
          return;
        }
        if (this.elicitationResponder) {
          // The responder is fire-and-forget; the App observes the task's
          // terminal status through subsequent host activity, not a
          // synchronous reply on this channel.
          try {
            this.elicitationResponder(params);
          } catch (err) {
            // eslint-disable-next-line no-console
            console.warn('[host-bridge] elicitation responder threw', err);
          }
        }
        return;
      }
      default:
        // Unknown View → host notification — forward-compatibility, drop.
        return;
    }
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
        // Phase 23 closes the Phase 22 seam: when the fixture switcher has set
        // a responder, the host answers `tools/call` from the active fixture
        // — a dev-test response, never a live arbitrary-execution proxy
        // (RFC §12). With no responder set, the host still answers with the
        // explicit "not wired" error so an App degrades gracefully.
        this.handleCallTool(req);
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

  private handleCallTool(req: JsonRpcRequest): void {
    if (!this.callToolResponder) {
      // No fixture responder wired — the explicit, graceful "not wired" error.
      this.respondError(
        req.id,
        -32601,
        'tools/call has no fixture wired — select a fixture in the inspector',
      );
      return;
    }
    const fixture = this.callToolResponder(req.params);
    if (fixture.error) {
      this.respondError(req.id, fixture.error.code, fixture.error.message);
      return;
    }
    // The MCP `tools/call` result shape: `content` + `structuredContent`.
    const content =
      fixture.text !== undefined
        ? [{ type: 'text', text: fixture.text }]
        : [];
    this.respond(req.id, {
      content,
      structuredContent: fixture.structuredContent,
    });
  }

  // Every outbound message flows through one of the three helpers below.
  // postSafe() applies a JSON round-trip at the boundary to unwrap any
  // Svelte 5 $state Proxy that has snuck into the payload (capabilities,
  // hostContext, fixture content all originate from $state). Window's
  // structured-clone serialiser rejects Proxies with a DataCloneError;
  // a JSON round-trip is cheap, deterministic, and safe for the values
  // this protocol carries (plain JSON-shaped objects with no functions,
  // Maps or Dates). Using JSON rather than structuredClone also avoids
  // triggering Svelte's $derived re-evaluation cycle that a deep
  // structuredClone walk would cause. Phase 24 — D-127.
  private postSafe(message: JsonRpcMessage): void {
    let plain: unknown;
    try {
      plain = JSON.parse(JSON.stringify(message));
    } catch {
      // A genuinely non-serialisable payload — drop with a one-line log
      // so the developer notices; the inspector is a dev surface and a
      // logged-and-dropped is preferable to a thrown that crashes the
      // bridge.
      // eslint-disable-next-line no-console
      console.warn('[host-bridge] dropping non-cloneable message', message);
      return;
    }
    this.peer.postMessage(plain);
  }

  private notify(method: string, params: unknown): void {
    if (this.closed) return;
    const message = { jsonrpc: '2.0' as const, method, params };
    this.log('outbound', message);
    this.postSafe(message);
  }

  private respond(id: JsonRpcRequest['id'], result: unknown): void {
    if (this.closed) return;
    const message = { jsonrpc: '2.0' as const, id, result };
    this.log('outbound', message);
    this.postSafe(message);
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
    this.postSafe(message);
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
