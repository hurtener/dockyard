/**
 * notifications.ts — host → View notification fan-out.
 *
 * The host pushes these notifications to the View (brief 01 §2.4):
 * `tool-input`, `tool-input-partial`, `tool-result`, `tool-cancelled`,
 * `size-changed`, `host-context-changed`, and `task-progress` (a
 * long-running task's mid-flight progress — RFC §8.4). The bridge
 * demultiplexes the single transport notification stream into typed,
 * per-kind subscriber sets so an App author registers `onToolResult(...)`
 * instead of pattern-matching raw methods.
 *
 * `host-context-changed` is dispatched here *and* consumed by `HostContextState`
 * — the bridge wires that internally so the stores stay current.
 */

import {
  DockyardExtMethod,
  type TaskProgressParams,
} from './dockyard-ext.js';
import {
  HostNotification,
  type CallToolResult,
  type HostContextChangedParams,
  type SizeChangedParams,
  type ToolCancelledParams,
  type ToolInputParams,
} from './protocol.js';

/** A typed notification subscriber; returns an unsubscribe function. */
export type Unsubscribe = () => void;

type Listener<T> = (payload: T) => void;

class Topic<T> {
  private readonly listeners = new Set<Listener<T>>();

  subscribe(fn: Listener<T>): Unsubscribe {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  emit(payload: T): void {
    // Snapshot so a handler that unsubscribes mid-emit does not skip a peer.
    for (const fn of [...this.listeners]) {
      fn(payload);
    }
  }

  clear(): void {
    this.listeners.clear();
  }
}

/**
 * Demultiplexes the transport's notification stream into typed topics. The
 * `tool-input` / `tool-result` payloads are generic so an App author binds the
 * contract type at the subscription site.
 */
export class NotificationRouter {
  private readonly toolInput = new Topic<ToolInputParams>();
  private readonly toolInputPartial = new Topic<ToolInputParams>();
  private readonly toolResult = new Topic<CallToolResult>();
  private readonly toolCancelled = new Topic<ToolCancelledParams>();
  private readonly sizeChanged = new Topic<SizeChangedParams>();
  private readonly hostContextChanged = new Topic<HostContextChangedParams>();
  private readonly taskProgress = new Topic<TaskProgressParams>();

  /** Routes one inbound notification to its topic. Unknown methods are no-ops. */
  dispatch(method: string, params: unknown): void {
    switch (method) {
      case HostNotification.toolInput:
        this.toolInput.emit((params ?? {}) as ToolInputParams);
        break;
      case HostNotification.toolInputPartial:
        this.toolInputPartial.emit((params ?? {}) as ToolInputParams);
        break;
      case HostNotification.toolResult:
        this.toolResult.emit((params ?? {}) as CallToolResult);
        break;
      case HostNotification.toolCancelled:
        this.toolCancelled.emit((params ?? {}) as ToolCancelledParams);
        break;
      case HostNotification.sizeChanged:
        this.sizeChanged.emit((params ?? {}) as SizeChangedParams);
        break;
      case HostNotification.hostContextChanged:
        this.hostContextChanged.emit(
          (params ?? {}) as HostContextChangedParams,
        );
        break;
      case DockyardExtMethod.taskProgress:
        this.taskProgress.emit((params ?? {}) as TaskProgressParams);
        break;
      // `ui/resource-teardown` (a host→View request) and any unknown method are
      // intentionally ignored here; teardown is handled by BridgeShell.onRequest.
      default:
        break;
    }
  }

  /** Fires before the tool result — full tool arguments (brief 01 §2.4). */
  onToolInput<I = unknown>(fn: Listener<ToolInputParams<I>>): Unsubscribe {
    return this.toolInput.subscribe(fn as Listener<ToolInputParams>);
  }

  /** Fires for streaming partial tool inputs. */
  onToolInputPartial<I = unknown>(
    fn: Listener<ToolInputParams<I>>,
  ): Unsubscribe {
    return this.toolInputPartial.subscribe(fn as Listener<ToolInputParams>);
  }

  /** Fires with the `CallToolResult`; `S` types `structuredContent`. */
  onToolResult<S = unknown>(fn: Listener<CallToolResult<S>>): Unsubscribe {
    return this.toolResult.subscribe(fn as Listener<CallToolResult>);
  }

  /** Fires when the host cancels the in-flight tool call. */
  onToolCancelled(fn: Listener<ToolCancelledParams>): Unsubscribe {
    return this.toolCancelled.subscribe(fn);
  }

  /** Fires when the iframe container is resized. */
  onSizeChanged(fn: Listener<SizeChangedParams>): Unsubscribe {
    return this.sizeChanged.subscribe(fn);
  }

  /** Fires with a partial `hostContext` patch. */
  onHostContextChanged(
    fn: Listener<HostContextChangedParams>,
  ): Unsubscribe {
    return this.hostContextChanged.subscribe(fn);
  }

  /** Fires with a long-running task's progress point (RFC §8.4). */
  onTaskProgress(fn: Listener<TaskProgressParams>): Unsubscribe {
    return this.taskProgress.subscribe(fn);
  }

  /** Drops every subscriber — used on bridge teardown. */
  clear(): void {
    this.toolInput.clear();
    this.toolInputPartial.clear();
    this.toolResult.clear();
    this.toolCancelled.clear();
    this.sizeChanged.clear();
    this.hostContextChanged.clear();
    this.taskProgress.clear();
  }
}
