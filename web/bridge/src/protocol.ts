/**
 * protocol.ts — the `ui/` postMessage JSON-RPC dialect.
 *
 * Every method name and wire shape of the MCP Apps View↔host channel is
 * centralised here, so a spec revision (the Apps spec is "under active
 * development", brief 01 §4.4) is a single, reviewable diff. The bridge
 * implements the *View* side of this dialect (RFC §7.3).
 *
 * Authoritative source: MCP Apps spec 2026-01-26 / SEP-1865, extension id
 * `io.modelcontextprotocol/ui` (brief 01 §2.4).
 */

/** The Apps extension capability id used in MCP capability negotiation. */
export const EXTENSION_ID = 'io.modelcontextprotocol/ui';

/** The single supported UI resource MIME type (brief 01 §2.2). */
export const RESOURCE_MIME_TYPE = 'text/html;profile=mcp-app';

/**
 * The protocol revision the bridge is built against. The negotiated value from
 * the host's `ui/initialize` result is retained for forward-compatibility, but
 * this is the version the View advertises.
 */
export const PROTOCOL_VERSION = '2026-01-26';

/** View → host request methods. */
export const ViewMethod = {
  initialize: 'ui/initialize',
  openLink: 'ui/open-link',
  message: 'ui/message',
  requestDisplayMode: 'ui/request-display-mode',
  updateModelContext: 'ui/update-model-context',
  callTool: 'tools/call',
} as const;

/** View → host notification methods (no response expected). */
export const ViewNotification = {
  initialized: 'ui/notifications/initialized',
  /**
   * `ui/notifications/elicitation-response` — the View answers a task's
   * `input_required` prompt with the user's reply (RFC §8.4, §8.6;
   * Phase 25 / D-134). The host forwards the reply to the attached server's
   * `tasks/result` endpoint, which resumes the suspended task.
   *
   * This is the App-initiated counterpart of `tasks/result`: a Tasks×Apps
   * tool that calls `TaskHandle.RequireInput` from a handler pauses the
   * task in `input_required`; the App reads the prompt (from the
   * `tool-result` body the host pushes when the elicitation begins),
   * renders a form, and posts the user's reply through this notification.
   * The bridge ships `BridgeShell.sendElicitationResponse(taskId, payload)`
   * as the typed View helper; an App author never hand-builds the wire.
   *
   * Notification, not a request: the View does not wait for the host to
   * acknowledge — the task's terminal status is the truth, and the App
   * sees it through the host's subsequent `tool-result` push or through
   * the inspector's Tasks panel. Keeping it fire-and-forget mirrors the
   * existing JSON-RPC notifications shape (`ui/notifications/initialized`,
   * `ui/notifications/tool-result`) and avoids a second round-trip on the
   * happy path.
   */
  elicitationResponse: 'ui/notifications/elicitation-response',
} as const;

/** Host → View notification methods. */
export const HostNotification = {
  toolInput: 'ui/notifications/tool-input',
  toolInputPartial: 'ui/notifications/tool-input-partial',
  toolResult: 'ui/notifications/tool-result',
  toolCancelled: 'ui/notifications/tool-cancelled',
  sizeChanged: 'ui/notifications/size-changed',
  hostContextChanged: 'ui/notifications/host-context-changed',
  /**
   * `ui/notifications/task-progress` — the host forwards a long-running
   * task's mid-flight progress to the View so an App's card can render a
   * live "62%" (RFC §8.4, the Tasks `TaskHandle.Progress` surface). The
   * App subscribes with `BridgeShell.onTaskProgress`.
   *
   * Host→View only: an App is a View (P4), so progress flows down to it,
   * never up from it. The channel is advisory — a host that does not
   * forward task progress simply never sends it, and `onTaskProgress`
   * never fires (capability-driven degradation, never a host matrix —
   * RFC §7.5). The Dockyard runtime emits each `TaskHandle.Progress`
   * call as an `obs/v1` `task.progress` event; the inspector host-bridge
   * forwards those to the View, so the channel is demoable through
   * `dockyard inspect`.
   */
  taskProgress: 'ui/notifications/task-progress',
  resourceTeardown: 'ui/resource-teardown',
} as const;

/**
 * Reserved sandbox-proxy notifications (brief 01 §2.4, §4.4). They signal
 * in-flight spec design; the bridge tolerates receiving them and ignores them
 * rather than crashing — forward-compatibility, never an assumption.
 */
export const ReservedNotification = {
  sandboxProxyReady: 'ui/notifications/sandbox-proxy-ready',
  sandboxResourceReady: 'ui/notifications/sandbox-resource-ready',
} as const;

export type HostNotificationMethod =
  (typeof HostNotification)[keyof typeof HostNotification];

/** The three Apps display modes (RFC §7.2). */
export type DisplayMode = 'inline' | 'fullscreen' | 'pip';

/** A JSON-RPC 2.0 id — string or number, never null for a request. */
export type JsonRpcId = string | number;

export interface JsonRpcRequest<P = unknown> {
  jsonrpc: '2.0';
  id: JsonRpcId;
  method: string;
  params?: P;
}

export interface JsonRpcNotification<P = unknown> {
  jsonrpc: '2.0';
  method: string;
  params?: P;
}

export interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

export interface JsonRpcResponse<R = unknown> {
  jsonrpc: '2.0';
  id: JsonRpcId;
  result?: R;
  error?: JsonRpcError;
}

export type JsonRpcMessage =
  | JsonRpcRequest
  | JsonRpcNotification
  | JsonRpcResponse;

export function isJsonRpcResponse(m: JsonRpcMessage): m is JsonRpcResponse {
  return (
    'id' in m &&
    m.id !== undefined &&
    !('method' in m) &&
    ('result' in m || 'error' in m)
  );
}

export function isJsonRpcRequest(m: JsonRpcMessage): m is JsonRpcRequest {
  return 'method' in m && 'id' in m && m.id !== undefined;
}

export function isJsonRpcNotification(
  m: JsonRpcMessage,
): m is JsonRpcNotification {
  return 'method' in m && !('id' in m);
}

/* --- handshake -------------------------------------------------------- */

/** Capabilities the View advertises in `ui/initialize` (brief 01 §2.4). */
export interface AppCapabilities {
  /** Display modes the App's build supports (manifest `display_modes`). */
  displayModes?: DisplayMode[];
}

export interface InitializeParams {
  protocolVersion: string;
  capabilities: { appCapabilities?: AppCapabilities };
  clientInfo: { name: string; version: string };
}

/** Standardized host CSS custom properties (brief 01 §2.4 — `styles.variables`). */
export type StyleVariables = Record<string, string>;

export interface ContainerDimensions {
  width: number;
  height: number;
}

/**
 * The host-supplied context delivered in the `ui/initialize` result and patched
 * by `ui/notifications/host-context-changed` (brief 01 §2.4).
 */
export interface HostContext {
  theme?: 'light' | 'dark' | string;
  styles?: { variables?: StyleVariables };
  displayMode?: DisplayMode;
  availableDisplayModes?: DisplayMode[];
  locale?: string;
  timeZone?: string;
  containerDimensions?: ContainerDimensions;
  userAgent?: string;
  platform?: string;
  toolInfo?: { name?: string; title?: string };
  safeAreaInsets?: {
    top: number;
    right: number;
    bottom: number;
    left: number;
  };
}

export interface HostCapabilities {
  /** Methods/features the host advertises; the bridge degrades on absence. */
  [key: string]: unknown;
}

export interface InitializeResult {
  protocolVersion: string;
  hostContext: HostContext;
  hostCapabilities?: HostCapabilities;
  hostInfo?: { name: string; version: string };
}

/* --- view → host request params -------------------------------------- */

export interface OpenLinkParams {
  url: string;
}

export type MessageRole = 'user' | 'assistant' | 'system';

export interface MessageParams {
  role: MessageRole;
  content: string;
}

export interface RequestDisplayModeParams {
  mode: DisplayMode;
}

/** The host's reply to `ui/request-display-mode` — grant or deny. */
export interface RequestDisplayModeResult {
  /** The mode actually in effect after the request. */
  mode: DisplayMode;
  /** True when the host granted the requested mode. */
  granted: boolean;
}

export interface UpdateModelContextParams {
  content?: string;
  structuredContent?: unknown;
}

export interface CallToolParams<I = unknown> {
  name: string;
  arguments?: I;
  /** `_meta` — carries `viewUUID` for view-state correlation (brief 01 §2.6). */
  _meta?: Record<string, unknown>;
}

/* --- content / tool-result shape ------------------------------------- */

export interface ContentBlock {
  type: string;
  text?: string;
  [key: string]: unknown;
}

/**
 * A standard MCP `CallToolResult` (brief 01 §2.6). `content` is model-facing;
 * `structuredContent` is the typed, UI-only payload; `_meta` carries `viewUUID`.
 */
export interface CallToolResult<S = unknown> {
  content?: ContentBlock[];
  structuredContent?: S;
  isError?: boolean;
  _meta?: Record<string, unknown>;
}

/* --- host → view notification params --------------------------------- */

export interface ToolInputParams<I = unknown> {
  arguments: I;
}

export interface ToolCancelledParams {
  reason?: string;
}

export interface SizeChangedParams {
  width: number;
  height: number;
}

/** `host-context-changed` delivers a partial patch of `HostContext`. */
export type HostContextChangedParams = Partial<HostContext>;

/**
 * `ui/notifications/task-progress` params — one progress point of a
 * long-running task (RFC §8.4). Mirrors the Dockyard runtime's `obs/v1`
 * `task.progress` payload: a `TaskHandle.Progress(fraction, message)` call
 * carries both; a `TaskHandle.Status(message)` call carries the message and
 * omits the fraction (a phase change a fraction cannot express).
 *
 * Every field but `taskId` is optional so a host can forward whatever it
 * has — an App reading the value renders defensively (no fraction ⇒ render
 * the message alone; no message ⇒ render the percentage alone).
 */
export interface TaskProgressParams {
  /** The task this progress point belongs to. */
  taskId: string;
  /**
   * The completion fraction in [0, 1], when known. Absent for a
   * status-only update. An App renders `Math.round(fraction * 100)` as the
   * percentage.
   */
  fraction?: number;
  /** An optional human-readable progress note. */
  message?: string;
  /** The task's lifecycle status at this point (e.g. `working`). */
  status?: string;
}

/* --- view → host elicitation-response (D-134) ------------------------ */

/**
 * `ui/notifications/elicitation-response` params — the App's reply to a
 * task's `input_required` prompt. The host forwards `data` to the attached
 * server's `tasks/result` endpoint (the elicited-input payload the MCP
 * Tasks experimental spec specifies; RFC §8.4).
 *
 * `data` is opaque to the bridge — it is the App's contract with its
 * server-side handler. A `request_approval` App posts
 * `{ approved: boolean, reason?: string, decided_at: string }`; a
 * `propose_with_edits` App posts
 * `{ approved: boolean, edits: object | null, decided_at: string }`.
 * The Dockyard runtime's `TaskHandle.RequireInput` returns this verbatim
 * as the `InputResponse.Data` raw JSON for the handler to decode against
 * its own contract.
 *
 * `declined` is the explicit "the user declined to provide input rather
 * than supplying it" signal (the runtime's `InputResponse.Declined`).
 * A declined response is not the same as `approved=false` on a
 * request_approval — declining is the user closing the prompt without
 * deciding; rejecting is a real decision. Handlers route them
 * differently.
 */
export interface ElicitationResponseParams {
  /**
   * The task id whose `input_required` prompt this response answers.
   * Read by the App from the `tool-result` push that opened the
   * elicitation (the runtime stamps it via the related-task `_meta` key).
   */
  taskId: string;
  /**
   * The user's reply, an opaque JSON value the handler decodes. Absent
   * when `declined` is true.
   */
  data?: unknown;
  /**
   * True when the user explicitly declined to answer. The Dockyard runtime
   * receives this as `InputResponse.Declined=true`; the handler decides
   * how to proceed.
   */
  declined?: boolean;
}
