/**
 * api.ts — the inspector frontend's client of the inspector backend.
 *
 * The frontend talks only to the `internal/inspector` localhost backend over
 * its read-only HTTP API — never directly to the MCP server (P4: the inspector
 * is the lone client-shaped surface, and even it routes through its own
 * loopback backend). This module is the thin typed client.
 */

import { parseRelayLog, type RpcEntry } from './rpc.js';
import { parseContracts, type ToolContract } from './contracts.js';

/** One verdict row from the backend `GET /api/verdicts` endpoint. */
export interface VerdictRow {
  /** The check class — "stale-codegen", "schema", "spec-compliance", … */
  check: string;
  /** The rendered tone: "ok" | "warn" | "error". */
  severity: string;
  /** The human-facing, actionable message. */
  message: string;
}

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

/**
 * Fetches the inspector's Verdicts — contract-drift, schema-validation, and
 * spec-compliance results — from the backend `GET /api/verdicts` endpoint.
 * The endpoint always answers with an array (empty when no source is wired),
 * so the panel routes a clean four-state.
 */
export async function fetchVerdicts(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<VerdictRow[]> {
  const resp = await fetchImpl(`${base}/api/verdicts`);
  if (!resp.ok) {
    throw new Error(`inspector: /api/verdicts returned ${resp.status}`);
  }
  const data: unknown = await resp.json();
  if (!Array.isArray(data)) return [];
  return data
    .filter((d): d is Record<string, unknown> => typeof d === 'object' && d !== null)
    .map((d) => ({
      check: typeof d.check === 'string' ? d.check : 'unknown',
      severity: typeof d.severity === 'string' ? d.severity : 'warn',
      message: typeof d.message === 'string' ? d.message : '',
    }));
}

/**
 * Fetches the attached server's generated tool contracts from
 * `GET /api/contracts`. The fixture switcher derives its fixtures from these
 * (P1). A backend with no contract endpoint yields an empty list and the
 * Fixtures panel renders its empty state.
 */
export async function fetchContracts(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<ToolContract[]> {
  const resp = await fetchImpl(`${base}/api/contracts`);
  if (!resp.ok) {
    throw new Error(`inspector: /api/contracts returned ${resp.status}`);
  }
  return parseContracts(await resp.json());
}

/** One MCP App the inspector can render, from `GET /api/apps`. */
export interface AppPreview {
  /** The App's ui:// resource URI. */
  uri: string;
  /** The App's display name. */
  name: string;
  /** The App's HTML document — the App-frame's iframe srcdoc. */
  html: string;
}

/**
 * Fetches the attached server's renderable MCP Apps from `GET /api/apps`. The
 * backend obtains each App's HTML by a read-only `resources/read` of the
 * server's ui:// resources (RFC §12 — the inspector renders the server's
 * Apps). A detached inspector yields an empty list and the App-frame renders
 * its "No App attached" empty state; an unreachable server is a thrown error
 * the App-frame surfaces as its error state.
 */
export async function fetchApps(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<AppPreview[]> {
  const resp = await fetchImpl(`${base}/api/apps`);
  if (!resp.ok) {
    let detail = `inspector: /api/apps returned ${resp.status}`;
    try {
      const body: unknown = await resp.json();
      if (
        typeof body === 'object' &&
        body !== null &&
        typeof (body as { error?: unknown }).error === 'string'
      ) {
        detail = (body as { error: string }).error;
      }
    } catch {
      // A non-JSON error body — keep the status-code detail.
    }
    throw new Error(detail);
  }
  const data: unknown = await resp.json();
  if (!Array.isArray(data)) return [];
  return data
    .filter((d): d is Record<string, unknown> => typeof d === 'object' && d !== null)
    .map((d) => ({
      uri: typeof d.uri === 'string' ? d.uri : '',
      name: typeof d.name === 'string' ? d.name : '',
      html: typeof d.html === 'string' ? d.html : '',
    }))
    .filter((a) => a.html !== '');
}
