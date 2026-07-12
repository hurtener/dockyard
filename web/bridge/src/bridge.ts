/**
 * bridge.ts — the BridgeShell.
 *
 * The one piece of client-shaped code Dockyard ships (RFC §7.3): a Svelte
 * library that runs *inside* the App's sandboxed iframe and speaks the View
 * side of the `ui/` postMessage dialect, so an MCP App author never hand-writes
 * protocol code. It:
 *
 *   - performs the `ui/initialize` handshake and waits for
 *     `ui/notifications/initialized` before resolving `ready` (brief 01 §2.4);
 *   - exposes `hostContext` (theme, styles.variables, displayMode, locale,
 *     dimensions) as Svelte stores;
 *   - fans out host → View notifications to typed subscribers;
 *   - offers typed view → host helpers — display-mode negotiation across
 *     inline/fullscreen/pip (RFC §7.2), open-link, message, update-model-
 *     context, and proxied `tools/call`;
 *   - framework-manages `_meta.viewUUID`-keyed view-state (D-060).
 *
 * Display-mode degradation is capability-driven, never a host matrix (RFC §7.5,
 * AGENTS.md §6, D-059): the bridge only offers a mode the host advertised in
 * `availableDisplayModes`.
 */

import type { ContractInput, ContractOutput, ToolContract } from './contracts.js';
import {
  DockyardExtMethod,
  type ElicitationResponseParams,
  type TaskProgressParams,
} from './dockyard-ext.js';
import { HostContextState, type HostContextStores, type StyleTarget } from './host-context.js';
import { NotificationRouter, type Unsubscribe } from './notifications.js';
import {
  HostNotification,
  HostRequest,
  PROTOCOL_VERSION,
  ViewMethod,
  ViewNotification,
  type AppCapabilities,
  type CallToolResult,
  type CompleteResult,
  type ContentBlock,
  type DisplayMode,
  type HostCapabilities,
  type HostContextChangedParams,
  type InitializeParams,
  type InitializeResult,
  type InputRequiredResult,
  type JsonRpcId,
  type RequestDisplayModeResult,
  type SizeChangedParams,
  type ToolCancelledParams,
  type ToolInputParams,
  type UpdateTaskParams,
  type UpdateModelContextParams,
} from './protocol.js';
import {
  Transport,
  type MessageSink,
  type MessageSource,
} from './transport.js';
import {
  newViewUUID,
  ViewStateStore,
  type ViewStateHandle,
} from './view-state.js';
import { writable, type Readable, type Writable } from 'svelte/store';

/** Options for `createBridge`. */
export interface BridgeOptions {
  /** Identifies the App to the host in `ui/initialize`. */
  clientInfo?: { name: string; version: string };
  /**
   * Display modes the App's build supports — the manifest `display_modes`
   * subset (RFC §7.2). Advertised to the host; the host narrows it further.
   */
  displayModes?: DisplayMode[];
  /** Where outbound messages post; defaults to `window.parent`. */
  peer?: MessageSink;
  /** Where inbound messages arrive; defaults to `window`. */
  source?: MessageSource;
  /** Request timeout in ms. Default 30000. */
  requestTimeoutMs?: number;
  /**
   * Element receiving `styles.variables` as CSS custom properties; defaults to
   * `document.documentElement`. Pass `null` to disable host theming.
   */
  styleTarget?: StyleTarget | null;
}

/** Raised when a display-mode request is rejected client-side (RFC §7.5). */
export class DisplayModeUnavailableError extends Error {
  readonly mode: DisplayMode;
  readonly available: DisplayMode[];
  constructor(mode: DisplayMode, available: DisplayMode[]) {
    super(
      `display mode "${mode}" is not offered by the host ` +
        `(available: ${available.join(', ') || 'none'})`,
    );
    this.name = 'DisplayModeUnavailableError';
    this.mode = mode;
    this.available = available;
  }
}

const DEFAULT_CLIENT = { name: 'dockyard-bridge', version: '0.1.0' };

/**
 * Builds the default host peer — a sink that posts to `window.parent` with
 * a wildcard `targetOrigin`. The wildcard is required and correct here:
 * an MCP App's bundle runs inside a sandboxed iframe (`sandbox="allow-
 * scripts"`, RFC §7.4) which gives it an opaque (`null`) origin; the host
 * frame has its own origin. Without an explicit target origin,
 * `Window.postMessage(message)` defaults to `'/'` (same-origin only), so
 * every message is silently dropped at the boundary — the bridge's
 * `ui/initialize` would never arrive, the host never answers, and the
 * handshake hangs forever. The host bridge is the trust boundary: it
 * decides whether to honour an inbound message; the View half cannot
 * usefully narrow the target origin because the host iframe is
 * cross-origin from its perspective. See decision D-124 (post-mortem).
 */
function defaultParentSink(): MessageSink | undefined {
  const parent = (globalThis as { window?: { parent?: Window } }).window?.parent;
  if (!parent) return undefined;
  return {
    postMessage(message: unknown): void {
      // Cross-document postMessage requires targetOrigin; '*' is the
      // correct value for a sandboxed iframe whose parent is opaque from
      // the View's perspective.
      parent.postMessage(message, '*');
    },
  };
}

/**
 * The bridge shell. Construct with `createBridge`, then `await connect()` once;
 * after that the stores are live and the typed helpers are usable.
 */
export class BridgeShell {
  private readonly transport: Transport;
  private readonly router = new NotificationRouter();
  private readonly hostCtx = new HostContextState();
  private readonly views = new ViewStateStore();
  private readonly options: BridgeOptions;

  private readonly _ready: Writable<boolean> = writable(false);
  private readonly _hostCapabilities: Writable<HostCapabilities> = writable({});
  // The host identity and the negotiated protocol version from the
  // `ui/initialize` result. The View advertises PROTOCOL_VERSION; the host's
  // value is retained for forward-compatibility (protocol.ts) so an App can
  // read who it is talking to and which revision was negotiated.
  private readonly _hostInfo: Writable<{ name: string; version: string } | undefined> =
    writable(undefined);
  private negotiatedProtocolVersion = '';
  private connectPromise: Promise<void> | undefined;
  private initialized = false;
  private closed = false;
  private unsubTransport: Unsubscribe | undefined;
  private unsubRequests: Unsubscribe | undefined;
  private stopSizeReporting: (() => void) | undefined;

  /** Explicit adapter for the pre-2026 Tasks×Apps relay. */
  readonly legacy = {
    sendElicitationResponse: (
      taskId: string,
      data?: unknown,
      options?: { declined?: boolean },
    ): void => {
      const params: ElicitationResponseParams = { taskId };
      if (data !== undefined) params.data = data;
      if (options?.declined) params.declined = true;
      this.transport.notify(DockyardExtMethod.elicitationResponse, params);
    },
  };

  constructor(options: BridgeOptions = {}) {
    this.options = options;
    const peer = options.peer ?? defaultParentSink();
    if (!peer) {
      throw new Error(
        'BridgeShell: no host peer — pass options.peer outside an iframe',
      );
    }
    this.transport = new Transport({
      peer,
      source: options.source,
      requestTimeoutMs: options.requestTimeoutMs ?? 30000,
    });
    this.unsubTransport = this.transport.onNotification((params, method) =>
      this.onNotification(method, params),
    );
    this.unsubRequests = this.transport.onRequest((_params, method, id) =>
      this.onRequest(method, id),
    );

    const styleTarget =
      options.styleTarget === null
        ? undefined
        : (options.styleTarget ??
          (globalThis as { document?: { documentElement?: StyleTarget } })
            .document?.documentElement);
    this.hostCtx.bindStyleTarget(styleTarget);
  }

  /* --- lifecycle ------------------------------------------------------ */

  /**
   * Runs the `ui/initialize` handshake: sends `ui/initialize`, applies the
   * host context from the result, then waits for `ui/notifications/initialized`
   * before resolving. Idempotent — repeated calls return the same promise.
   */
  connect(): Promise<void> {
    if (this.connectPromise) return this.connectPromise;
    this.connectPromise = this.runHandshake();
    return this.connectPromise;
  }

  private async runHandshake(): Promise<void> {
    const appCapabilities: AppCapabilities = {};
    if (this.options.displayModes) {
      // Wire key is `availableDisplayModes` per the ext-apps schema (D-182,
      // item A); the public `displayModes` option name is retained. An earlier
      // `displayModes` wire key was silently stripped by the host's parse, so
      // the host never learned the App's modes and fullscreen/pip never worked.
      appCapabilities.availableDisplayModes = this.options.displayModes;
    }
    // MCP Apps `ui/initialize` params use the ui/ dialect field names —
    // top-level `appCapabilities` + `appInfo` — NOT base-MCP `capabilities` +
    // `clientInfo`. The reference host (@modelcontextprotocol/ext-apps) validates
    // params against a schema requiring `{appInfo, appCapabilities,
    // protocolVersion}` (appInfo REQUIRED); the previous base-MCP shape was
    // rejected by a strict host, so the handshake never completed and the App
    // never rendered.
    const params: InitializeParams = {
      protocolVersion: PROTOCOL_VERSION,
      appCapabilities,
      appInfo: this.options.clientInfo ?? DEFAULT_CLIENT,
    };

    const result = await this.transport.request<InitializeResult>(
      ViewMethod.initialize,
      params,
    );
    this.hostCtx.set(result.hostContext ?? {});
    this._hostCapabilities.set(result.hostCapabilities ?? {});
    // Retain the negotiated protocol version + host identity (protocol.ts:
    // "the negotiated value from the host's ui/initialize result is retained
    // for forward-compatibility"). Both were previously discarded.
    if (typeof result.protocolVersion === 'string' && result.protocolVersion !== '') {
      this.negotiatedProtocolVersion = result.protocolVersion;
    }
    this._hostInfo.set(result.hostInfo);
    // The View is the initiator: per the JSON-RPC/MCP lifecycle (and the
    // @modelcontextprotocol/ext-apps reference view that Claude's host is built
    // on), after the `ui/initialize` result the View *sends*
    // `ui/notifications/initialized` and is immediately ready. It must NOT wait
    // to *receive* that notification — the host never sends a View→host message,
    // so awaiting it deadlocks against a spec-compliant host (blank App, no
    // error). `initialized` is correctly a View→host notification in protocol.ts.
    this.transport.notify(ViewNotification.initialized, {});
    this.initialized = true;
    this._ready.set(true);
    this.startSizeReporting();
  }

  /**
   * Reports the View's content size to the host via `ui/notifications/size-changed`
   * (View→host) so the host sizes the App iframe to fit the content. The MCP Apps
   * reference view (`@modelcontextprotocol/ext-apps`, autoResize) does exactly this
   * with a ResizeObserver; WITHOUT it a spec-compliant host (Claude Desktop) gives
   * the iframe a collapsed (~0px) height and the App looks blank even though it
   * rendered. Mirrors the reference: measure documentElement under `fit-content`,
   * add the scrollbar gutter, send once immediately then on each de-duplicated
   * change. No-op outside a DOM (unit tests).
   */
  private startSizeReporting(): void {
    if (
      typeof document === 'undefined' ||
      typeof ResizeObserver === 'undefined' ||
      typeof requestAnimationFrame === 'undefined'
    ) {
      return;
    }
    let pending = false;
    let lastW = -1;
    let lastH = -1;
    const measure = (): void => {
      if (pending) return;
      pending = true;
      requestAnimationFrame(() => {
        pending = false;
        if (this.closed) return;
        const el = document.documentElement;
        const savedW = el.style.width;
        const savedH = el.style.height;
        el.style.width = 'fit-content';
        el.style.height = 'fit-content';
        const rect = el.getBoundingClientRect();
        el.style.width = savedW;
        el.style.height = savedH;
        const scrollbar = window.innerWidth - el.clientWidth;
        const width = Math.ceil(rect.width + scrollbar);
        const height = Math.ceil(rect.height);
        if (width === lastW && height === lastH) return;
        lastW = width;
        lastH = height;
        this.transport.notify(HostNotification.sizeChanged, { width, height });
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(document.documentElement);
    observer.observe(document.body);
    this.stopSizeReporting = () => observer.disconnect();
  }

  /** Resolves once the handshake completed and the bridge is ready. */
  get ready(): Readable<boolean> {
    return this._ready;
  }

  /** True once `ui/notifications/initialized` has been received. */
  get isInitialized(): boolean {
    return this.initialized;
  }

  /** Tears the bridge down: drops subscribers, closes the transport. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.stopSizeReporting?.();
    this.unsubTransport?.();
    this.unsubRequests?.();
    this.router.clear();
    this.views.clear();
    this.transport.close();
    this._ready.set(false);
  }

  /* --- hostContext ---------------------------------------------------- */

  /** The reactive `hostContext` stores (theme, displayMode, variables, …). */
  get hostContext(): HostContextStores {
    return this.hostCtx.stores;
  }

  /** The host capabilities advertised in the handshake result. */
  get hostCapabilities(): Readable<HostCapabilities> {
    return this._hostCapabilities;
  }

  /** The host identity from the `ui/initialize` result, or undefined. */
  get hostInfo(): Readable<{ name: string; version: string } | undefined> {
    return this._hostInfo;
  }

  /**
   * The protocol version the host negotiated in `ui/initialize` (retained for
   * forward-compatibility), or "" before the handshake completes. The View
   * advertises {@link PROTOCOL_VERSION}; this is what the host answered with.
   */
  get protocolVersion(): string {
    return this.negotiatedProtocolVersion;
  }

  /* --- host → View notification subscriptions ------------------------- */

  onToolInput<I = unknown>(
    fn: (p: ToolInputParams<I>) => void,
  ): Unsubscribe {
    return this.router.onToolInput<I>(fn);
  }

  onToolInputPartial<I = unknown>(
    fn: (p: ToolInputParams<I>) => void,
  ): Unsubscribe {
    return this.router.onToolInputPartial<I>(fn);
  }

  /**
   * Subscribes to `tool-result`. `S` types `structuredContent`; pass a
   * `ToolContract`'s output type so the typed UI payload cannot drift from the
   * Go output struct (P1, brief 01 §2.6).
   */
  onToolResult<S = unknown>(
    fn: (p: CallToolResult<S>) => void,
  ): Unsubscribe {
    return this.router.onToolResult<S>(fn);
  }

  onToolCancelled(fn: (p: ToolCancelledParams) => void): Unsubscribe {
    return this.router.onToolCancelled(fn);
  }

  onSizeChanged(fn: (p: SizeChangedParams) => void): Unsubscribe {
    return this.router.onSizeChanged(fn);
  }

  onHostContextChanged(
    fn: (p: HostContextChangedParams) => void,
  ): Unsubscribe {
    return this.router.onHostContextChanged(fn);
  }

  /**
   * Subscribes to a long-running task's progress (RFC §8.4). Fires once per
   * `ui/notifications/task-progress` the host forwards — the Dockyard
   * runtime emits one per `TaskHandle.Progress` / `TaskHandle.Status` call,
   * and the host (the inspector, or a production host) pushes it to the
   * View. An App renders the live percentage from `p.fraction` and/or the
   * `p.message`.
   *
   * Degrades cleanly: a host that does not forward task progress simply
   * never triggers this subscriber — capability-driven, never a host matrix
   * (RFC §7.5). Subscribe regardless and render whatever arrives.
   */
  onTaskProgress(fn: (p: TaskProgressParams) => void): Unsubscribe {
    return this.router.onTaskProgress(fn);
  }

  /* --- view → host helpers ------------------------------------------- */

  /**
   * Negotiates a display mode (RFC §7.2). A mode the host did not advertise in
   * `availableDisplayModes` is rejected *client-side* without a round trip —
   * capability-driven degradation, never a host matrix (RFC §7.5, D-059).
   * Returns the host's grant/deny verdict.
   */
  async requestDisplayMode(
    mode: DisplayMode,
  ): Promise<RequestDisplayModeResult> {
    const available = this.hostCtx.currentAvailableModes;
    if (!available.includes(mode)) {
      throw new DisplayModeUnavailableError(mode, available);
    }
    const result = await this.transport.request<RequestDisplayModeResult>(
      ViewMethod.requestDisplayMode,
      { mode },
    );
    // Reflect the granted mode immediately; the host also sends a
    // `host-context-changed`, but the result is authoritative for the caller.
    if (result.granted) {
      this.hostCtx.patch({ displayMode: result.mode });
    }
    return result;
  }

  /** The display modes the host will currently grant (no subscription). */
  availableDisplayModes(): DisplayMode[] {
    return this.hostCtx.currentAvailableModes;
  }

  /** Asks the host to open a URL outside the iframe (`ui/open-link`). */
  async openLink(url: string): Promise<void> {
    await this.transport.request<void>(ViewMethod.openLink, { url });
  }

  /** Sends a message into the host chat (`ui/message`). */
  async sendMessage(content: string | ContentBlock[]): Promise<void> {
    // `ui/message` is always a `user` message of content blocks
    // (McpUiMessageRequestSchema, D-182); a bare string is wrapped as one text
    // block for ergonomics.
    const blocks: ContentBlock[] =
      typeof content === 'string' ? [{ type: 'text', text: content }] : content;
    await this.transport.request<void>(ViewMethod.message, {
      role: 'user',
      content: blocks,
    });
  }

  /** Updates the model's context (`ui/update-model-context`). */
  async updateModelContext(patch: UpdateModelContextParams): Promise<void> {
    await this.transport.request<void>(ViewMethod.updateModelContext, patch);
  }

  /**
   * Calls a tool, proxied by the host to the MCP server over the normal MCP
   * transport (brief 01 §2.4). `view` correlates the call with a `viewUUID`
   * for view-state — when given, `_meta.viewUUID` is attached.
   */
  async callTool<I = unknown, O = unknown>(
    name: string,
    args?: I,
    view?: { uuid: string },
  ): Promise<CallToolResult<O>> {
    const meta = view ? { viewUUID: view.uuid } : undefined;
    return this.transport.request<CallToolResult<O>>(ViewMethod.callTool, {
      name,
      arguments: args,
      ...(meta ? { _meta: meta } : {}),
    });
  }

  /** Retries the original tools/call for a core MRTR input-required result. */
  async retryToolCall<I = unknown, O = unknown>(
    name: string,
    args: I | undefined,
    continuation: Pick<InputRequiredResult, 'requestState'>,
    inputResponses: Record<string, unknown>,
    view?: { uuid: string },
  ): Promise<CallToolResult<O> | InputRequiredResult> {
    const meta = view ? { viewUUID: view.uuid } : undefined;
    return this.transport.request(ViewMethod.callTool, {
      name,
      arguments: args,
      inputResponses,
      ...(continuation.requestState ? { requestState: continuation.requestState } : {}),
      ...(meta ? { _meta: meta } : {}),
    });
  }

  /** Submits keyed responses to a modern task without retrying tools/call. */
  updateTask(params: UpdateTaskParams): Promise<CompleteResult> {
    return this.transport.request<CompleteResult>(ViewMethod.updateTask, params);
  }

  /**
   * Calls a tool through its generated `ToolContract`, so input and
   * `structuredContent` output are typed end-to-end (P1, contract-first).
   */
  callContract<C extends ToolContract>(
    contract: C,
    args: ContractInput<C>,
    view?: { uuid: string },
  ): Promise<CallToolResult<ContractOutput<C>>> {
    return this.callTool<ContractInput<C>, ContractOutput<C>>(
      contract.name,
      args,
      view,
    );
  }

  /**
   * Posts the user's reply to a task's `input_required` prompt
   * (RFC §8.4, §8.6; Phase 25 / D-134). `taskId` is the id of the task
   * the App is answering — read from the `tool-result` push that opened
   * the elicitation, where the runtime stamps it via the related-task
   * `_meta` key. `data` is the App's opaque reply (the contract is
   * between the App and its handler); `declined` answers with an
   * explicit "no input" signal that the handler routes differently
   * from a real decision.
   *
   * The legacy notification is fire-and-forget. Modern peers must use
   * `updateTask`, which returns the standards-shaped acknowledgement.
   */
  /** @deprecated Use `legacy.sendElicitationResponse` only for a legacy peer. */
  sendElicitationResponse(
    taskId: string,
    data?: unknown,
    options?: { declined?: boolean },
  ): void {
    this.legacy.sendElicitationResponse(taskId, data, options);
  }

  /**
   * Asks the host to tear this App down — the View-initiated
   * `ui/notifications/request-teardown` (D-182, item B). Fire-and-forget: the
   * host responds by sending the `ui/resource-teardown` request, which the
   * bridge answers and then closes. Use when an App is done and wants its
   * iframe released (the host-initiated teardown is the common path).
   */
  requestTeardown(): void {
    this.transport.notify(ViewNotification.requestTeardown, {});
  }

  /* --- framework-managed view-state ---------------------------------- */

  /**
   * Returns the framework-managed view-state handle for a `viewUUID` (D-060).
   * Calling it again with the same `uuid` recovers the same snapshot — that is
   * how view-state round-trips across a re-render (RFC §7.3). With no argument,
   * a fresh `viewUUID` is minted.
   */
  viewState<T = unknown>(uuid?: string): ViewStateHandle<T> {
    return this.views.handle<T>(uuid ?? newViewUUID());
  }

  /** True when a view-state snapshot exists for `uuid`. */
  hasViewState(uuid: string): boolean {
    return this.views.has(uuid);
  }

  /* --- internal ------------------------------------------------------- */

  private onNotification(method: string, params: unknown): void {
    this.router.dispatch(method, params);
    // `host-context-changed` also patches the hostContext stores.
    if (
      method === 'ui/notifications/host-context-changed' &&
      params &&
      typeof params === 'object'
    ) {
      this.hostCtx.patch(params as HostContextChangedParams);
    }
  }

  /**
   * Handles an inbound host → View *request*. The only one is
   * `ui/resource-teardown` (D-182, item B): a spec host asks the View to clean
   * up and **waits for the View's result** before tearing down the iframe — so
   * the bridge responds first (an empty result object per
   * `McpUiResourceTeardownResultSchema`), then closes (drop subscribers, close
   * the transport, set ready=false). Responding before `close()` matters:
   * `close()` shuts the transport, after which `respond` is a no-op. `close()`
   * is idempotent, so a duplicate teardown is safe. An unknown request method is
   * ignored (forward-compatibility).
   */
  private onRequest(method: string, id: JsonRpcId): void {
    if (method === HostRequest.resourceTeardown) {
      this.transport.respond(id, {});
      this.close();
    }
  }
}

/** Constructs a {@link BridgeShell}. The primary entry point of the library. */
export function createBridge(options: BridgeOptions = {}): BridgeShell {
  return new BridgeShell(options);
}
