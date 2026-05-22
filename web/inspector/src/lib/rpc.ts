/**
 * rpc.ts — the inspector's JSON-RPC log model.
 *
 * The RPC panel surfaces the JSON-RPC wire traffic (`tools/call`,
 * `resources/read`, `ui/*`) of the attached MCP server, read-only (RFC §12).
 * Two sources feed it:
 *
 *   - the backend relay's `/api/rpc/log` endpoint (`internal/inspector`) — the
 *     MCP transport's JSON-RPC traffic;
 *   - the host-half bridge's `onRpc` callback — the `ui/` postMessage traffic.
 *
 * This module is the shared shape and the merge — the panel renders the merged,
 * method-filterable log.
 */

/** A logged JSON-RPC message direction. */
export type RpcDirection = 'inbound' | 'outbound';

/** One entry in the inspector's JSON-RPC log. */
export interface RpcEntry {
  /** A stable identity for keyed rendering. */
  id: string;
  /** Direction relative to the MCP server (or the App, for `ui/` traffic). */
  direction: RpcDirection;
  /** The JSON-RPC method, when the message is a request or notification. */
  method?: string;
  /** The raw JSON-RPC message payload. */
  payload: unknown;
  /** When the entry was logged (epoch ms). */
  at: number;
}

/** The backend relay's `/api/rpc/log` entry shape (`internal/inspector`). */
interface RelayRpcEntry {
  seq: number;
  timestamp: string;
  direction: RpcDirection;
  method?: string;
  payload?: unknown;
}

/** Maps a backend relay RPC entry into an {@link RpcEntry}. */
export function fromRelayEntry(e: RelayRpcEntry): RpcEntry {
  return {
    id: `relay-${e.seq}`,
    direction: e.direction,
    method: e.method,
    payload: e.payload,
    at: Date.parse(e.timestamp) || Date.now(),
  };
}

/** Parses a `/api/rpc/log` JSON array into {@link RpcEntry}s. */
export function parseRelayLog(json: unknown): RpcEntry[] {
  if (!Array.isArray(json)) return [];
  return json
    .filter((e): e is RelayRpcEntry => typeof e === 'object' && e !== null)
    .map(fromRelayEntry);
}

/** The set of distinct methods present in a log — for the method filter. */
export function methodsIn(entries: RpcEntry[]): string[] {
  const seen = new Set<string>();
  for (const e of entries) {
    if (e.method) seen.add(e.method);
  }
  return [...seen].sort();
}

/** Filters a log to entries whose method is in `methods` (empty = all). */
export function filterByMethod(
  entries: RpcEntry[],
  methods: string[],
): RpcEntry[] {
  if (methods.length === 0) return entries;
  const want = new Set(methods);
  return entries.filter((e) => e.method !== undefined && want.has(e.method));
}
