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
} as const;

/** Host → View notification methods. */
export const HostNotification = {
  toolInput: 'ui/notifications/tool-input',
  toolInputPartial: 'ui/notifications/tool-input-partial',
  toolResult: 'ui/notifications/tool-result',
  toolCancelled: 'ui/notifications/tool-cancelled',
  sizeChanged: 'ui/notifications/size-changed',
  hostContextChanged: 'ui/notifications/host-context-changed',
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
