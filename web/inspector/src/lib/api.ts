/**
 * api.ts — the inspector frontend's client of the inspector backend.
 *
 * The frontend talks only to the `internal/inspector` localhost backend over
 * its read-only HTTP API — never directly to the MCP server (P4: the inspector
 * is the lone client-shaped surface, and even it routes through its own
 * loopback backend). This module is the thin typed client.
 */

import { parseRelayLog, type RpcEntry } from './rpc.js';

/** The attached MCP server's identity, from `GET /api/info`. */
export interface ServerInfo {
  name: string;
  version: string;
  transport: string;
}

/** Fetches the attached server's identity. */
export async function fetchServerInfo(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<ServerInfo> {
  const resp = await fetchImpl(`${base}/api/info`);
  if (!resp.ok) {
    throw new Error(`inspector: /api/info returned ${resp.status}`);
  }
  const data: unknown = await resp.json();
  const d = (data ?? {}) as Partial<ServerInfo>;
  return {
    name: d.name ?? 'unknown',
    version: d.version ?? '',
    transport: d.transport ?? '',
  };
}

/** Fetches the current JSON-RPC log from the backend relay. */
export async function fetchRpcLog(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<RpcEntry[]> {
  const resp = await fetchImpl(`${base}/api/rpc/log`);
  if (!resp.ok) {
    throw new Error(`inspector: /api/rpc/log returned ${resp.status}`);
  }
  return parseRelayLog(await resp.json());
}

/** The obs/v1 relay SSE stream URL. */
export function obsStreamURL(base = ''): string {
  return `${base}/api/obs/stream`;
}
