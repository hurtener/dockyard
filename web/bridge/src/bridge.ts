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
import { HostContextState, type HostContextStores, type StyleTarget } from './host-context.js';
import { NotificationRouter, type Unsubscribe } from './notifications.js';
import {
  PROTOCOL_VERSION,
  ViewMethod,
  ViewNotification,
  type AppCapabilities,
  type CallToolResult,
  type DisplayMode,
  type HostCapabilities,
  type HostContextChangedParams,
  type InitializeParams,
  type InitializeResult,
  type MessageRole,
  type RequestDisplayModeResult,
  type SizeChangedParams,
  type ToolCancelledParams,
  type ToolInputParams,
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

const DEFAULT_CLIENT = { name: '@dockyard/bridge', version: '0.1.0' };

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
  private connectPromise: Promise<void> | undefined;
  private initialized = false;
  private closed = false;
  private unsubTransport: Unsubscribe | undefined;

  constructor(options: BridgeOptions = {}) {
    this.options = options;
    const peer =
      options.peer ??
      (globalThis as { window?: { parent?: MessageSink } }).window?.parent;
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
      appCapabilities.displayModes = this.options.displayModes;
    }
    const params: InitializeParams = {
      protocolVersion: PROTOCOL_VERSION,
      capabilities: { appCapabilities },
      clientInfo: this.options.clientInfo ?? DEFAULT_CLIENT,
    };

    // A promise that settles when the host's `initialized` notification lands.
    let resolveInitialized!: () => void;
    const initializedReceived = new Promise<void>((resolve) => {
      resolveInitialized = resolve;
    });
    const offInit = this.transport.onNotification((_p, method) => {
      if (method === ViewNotification.initialized) {
        this.initialized = true;
        resolveInitialized();
      }
    });

    try {
      const result = await this.transport.request<InitializeResult>(
        ViewMethod.initialize,
        params,
      );
      this.hostCtx.set(result.hostContext ?? {});
      this._hostCapabilities.set(result.hostCapabilities ?? {});
      // The View must wait for `ui/notifications/initialized` before assuming
      // readiness (brief 01 §2.4).
      await initializedReceived;
      this._ready.set(true);
    } finally {
      offInit();
    }
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
    this.unsubTransport?.();
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
  async sendMessage(role: MessageRole, content: string): Promise<void> {
    await this.transport.request<void>(ViewMethod.message, { role, content });
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
}

/** Constructs a {@link BridgeShell}. The primary entry point of the library. */
export function createBridge(options: BridgeOptions = {}): BridgeShell {
  return new BridgeShell(options);
}
