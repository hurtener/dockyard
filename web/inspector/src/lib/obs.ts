/**
 * obs.ts — the inspector's obs/v1 stream client.
 *
 * The inspector's Events panel is a PURE CLIENT of the `obs/v1` event contract
 * (CLAUDE.md §6 / P2 — observability is a protocol; the inspector never reads
 * runtime internals). This module connects to the inspector backend's obs relay
 * SSE endpoint (`/api/obs/stream`, served by `internal/inspector`) and decodes
 * each frame as an `obs/v1` event.
 *
 * The `obs/v1` event shape is the versioned public contract (RFC §11.3); the
 * inspector decodes only the fields it renders and tolerates the rest, so a
 * versioned, additive obs/v1 change does not break the inspector.
 */

/** The obs/v1 schema identifier (RFC §11.3 — `dockyard.obs/v1`). */
export const OBS_SCHEMA_VERSION = 'dockyard.obs/v1';

/** An obs/v1 event — the subset of fields the inspector renders. */
export interface ObsEvent {
  schema_version: string;
  id: string;
  timestamp: string;
  server_id: string;
  session_id?: string;
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  kind: string;
  phase: string;
  payload?: unknown;
  duration_ms?: number;
  error?: {
    type: string;
    message: string;
    retryable?: boolean;
    silent?: boolean;
  };
}

/** True when `value` is a structurally plausible obs/v1 event. */
export function isObsEvent(value: unknown): value is ObsEvent {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.id === 'string' &&
    typeof v.kind === 'string' &&
    typeof v.phase === 'string' &&
    typeof v.timestamp === 'string'
  );
}

/** Parses one SSE `data:` payload into an obs/v1 event, or null if malformed. */
export function parseObsEvent(data: string): ObsEvent | null {
  try {
    const value: unknown = JSON.parse(data);
    return isObsEvent(value) ? value : null;
  } catch {
    return null;
  }
}

/** Callbacks for {@link ObsStream}. */
export interface ObsStreamHandlers {
  /** Called for every decoded obs/v1 event. */
  onEvent: (event: ObsEvent) => void;
  /** Called when the stream connection opens. */
  onOpen?: () => void;
  /** Called when the stream errors or closes. */
  onError?: () => void;
}

/** A minimal EventSource-shaped contract — lets tests inject a fake. */
export interface EventSourceLike {
  onopen: (() => void) | null;
  onerror: (() => void) | null;
  onmessage: ((ev: { data: string }) => void) | null;
  close(): void;
}

/** Factory for an {@link EventSourceLike} — the live one wraps `EventSource`. */
export type EventSourceFactory = (url: string) => EventSourceLike;

/** The default factory: a real browser `EventSource`. */
export const browserEventSource: EventSourceFactory = (url) =>
  new EventSource(url) as unknown as EventSourceLike;

/**
 * ObsStream subscribes to the inspector backend's obs/v1 relay and delivers
 * decoded events to a handler. It is an SSE client — the relay reconnects
 * upstream, and `EventSource` reconnects to the relay, so a dev-server restart
 * does not need inspector intervention.
 */
export class ObsStream {
  private readonly url: string;
  private readonly handlers: ObsStreamHandlers;
  private readonly factory: EventSourceFactory;
  private source: EventSourceLike | null = null;
  private closed = false;

  constructor(
    url: string,
    handlers: ObsStreamHandlers,
    factory: EventSourceFactory = browserEventSource,
  ) {
    this.url = url;
    this.handlers = handlers;
    this.factory = factory;
  }

  /** Opens the stream. Idempotent. */
  open(): void {
    if (this.source || this.closed) return;
    const src = this.factory(this.url);
    src.onopen = () => this.handlers.onOpen?.();
    src.onerror = () => this.handlers.onError?.();
    src.onmessage = (ev) => {
      const event = parseObsEvent(ev.data);
      if (event) this.handlers.onEvent(event);
    };
    this.source = src;
  }

  /** Closes the stream. Idempotent. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.source?.close();
    this.source = null;
  }
}
