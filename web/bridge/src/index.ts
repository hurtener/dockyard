/**
 * dockyard-bridge — the Svelte bridge shell.
 *
 * The View side of the MCP Apps `ui/` postMessage JSON-RPC dialect (RFC §7.2,
 * §7.3). An MCP App author imports `createBridge`, `await`s `connect()`, and
 * binds the resulting Svelte stores — never hand-writing protocol code.
 *
 *   import { createBridge } from 'dockyard-bridge';
 *   const bridge = createBridge({ displayModes: ['inline', 'fullscreen'] });
 *   await bridge.connect();
 *   bridge.onToolResult<MyOutput>((r) => render(r.structuredContent));
 */

export { createBridge, BridgeShell, DisplayModeUnavailableError } from './bridge.js';
export type { BridgeOptions } from './bridge.js';

export type { HostContextStores, StyleTarget } from './host-context.js';
export type { Unsubscribe } from './notifications.js';
export type { ViewStateHandle } from './view-state.js';
export { newViewUUID } from './view-state.js';

export { defineContract } from './contracts.js';
export type {
  ContractInput,
  ContractMap,
  ContractOutput,
  ToolContract,
} from './contracts.js';

export {
  JsonRpcRequestError,
  portAsMessageSource,
  Transport,
} from './transport.js';
export type {
  InboundMessageEvent,
  InboundMessageListener,
  MessageSink,
  MessageSource,
  NotificationHandler,
  TransportOptions,
} from './transport.js';

export {
  EXTENSION_ID,
  HostNotification,
  HostRequest,
  PROTOCOL_VERSION,
  RESOURCE_MIME_TYPE,
  ReservedNotification,
  ViewMethod,
  ViewNotification,
  isJsonRpcNotification,
  isJsonRpcRequest,
  isJsonRpcResponse,
} from './protocol.js';
// Dockyard Tasks×Apps extensions — outside the conformed MCP Apps surface (D-183).
export { DockyardExtMethod, DOCKYARD_EXT_METHODS } from './dockyard-ext.js';
export type {
  ElicitationResponseParams,
  TaskProgressParams,
} from './dockyard-ext.js';
export type {
  AppCapabilities,
  CallToolParams,
  CallToolResult,
  CompleteResult,
  ContainerDimensions,
  ContentBlock,
  DisplayMode,
  HostCapabilities,
  HostContext,
  HostContextChangedParams,
  InitializeParams,
  InitializeResult,
  InputRequiredResult,
  JsonRpcError,
  JsonRpcId,
  JsonRpcMessage,
  JsonRpcNotification,
  JsonRpcRequest,
  JsonRpcResponse,
  MessageParams,
  OpenLinkParams,
  RequestDisplayModeParams,
  RequestDisplayModeResult,
  SizeChangedParams,
  StyleVariables,
  ToolCancelledParams,
  ToolInputParams,
  UpdateModelContextParams,
  UpdateTaskParams,
} from './protocol.js';
