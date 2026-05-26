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
import {
  parsePrompts,
  parsePromptGetResponse,
  type PromptInfo,
  type PromptGetResponse,
} from './prompts.js';

export type { PromptInfo, PromptGetResponse, PromptGetMessage, PromptArgumentInfo } from './prompts.js';

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

/**
 * The result of one operator-initiated tools/call, from
 * `POST /api/tools/invoke` (D-131). `structuredContent` is the typed payload
 * the App-frame's `pushToolResult` path consumes — the same path the Fixtures
 * switcher uses (D-129) — so a real invocation re-renders the App preview
 * with the operator's parameters. `isError` mirrors the MCP `isError` flag:
 * a tool that returned a typed error to the host is still a successful RPC,
 * surfaced here so the inspector can render the error state cleanly.
 */
export interface ToolInvokeResult {
  /** Optional model-facing content[] from the tools/call response. */
  content?: unknown;
  /** The tool's typed structured payload, when emitted. */
  structuredContent?: Record<string, unknown>;
  /** True when the tool reported a typed error to the host. */
  isError?: boolean;
}

/**
 * Invokes a tool on the attached server through the inspector's
 * `POST /api/tools/invoke` endpoint (D-131; RFC §12; P4). The operator is the
 * one driving the write through the UI — the inspector itself remains the
 * lone client-shaped surface, dev-mode-gated and localhost-bound. A
 * transport-level failure (the server is unreachable, the tool not found,
 * schema validation rejected the input) is a thrown error the caller
 * surfaces in the panel's error state. A tool-level error is returned as a
 * result with `isError: true`.
 */
export async function invokeTool(
  request: { tool: string; arguments: Record<string, unknown> },
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<ToolInvokeResult> {
  const resp = await fetchImpl(`${base}/api/tools/invoke`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!resp.ok) {
    let detail = `inspector: /api/tools/invoke returned ${resp.status}`;
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
  if (typeof data !== 'object' || data === null) {
    return {};
  }
  const d = data as {
    content?: unknown;
    structuredContent?: unknown;
    isError?: unknown;
  };
  const result: ToolInvokeResult = {};
  if (d.content !== undefined) result.content = d.content;
  if (typeof d.structuredContent === 'object' && d.structuredContent !== null) {
    result.structuredContent = d.structuredContent as Record<string, unknown>;
  }
  if (typeof d.isError === 'boolean') result.isError = d.isError;
  return result;
}

/**
 * The shape the bridge posts as an elicitation-response notification —
 * mirrors `@dockyard/bridge`'s `ElicitationResponseParams`. The inspector
 * frontend forwards this verbatim to its backend; the backend translates
 * it into a raw `tasks/result` JSON-RPC frame against the attached
 * server.
 */
export interface ElicitationRequest {
  taskId: string;
  data?: unknown;
  declined?: boolean;
}

/**
 * The inspector backend's reply to a successful elicitation delivery
 * (Phase 25 / D-134). `delivered` is true when the attached server
 * accepted the elicitation; false (with a non-empty `error`) when it
 * refused. A transport-level failure (the server is unreachable, the
 * task does not exist) is a thrown error the caller surfaces in the
 * App preview's error state.
 */
export interface ElicitationResponse {
  taskId: string;
  delivered: boolean;
  error?: string;
}

/**
 * Posts an App's elicitation-response to the inspector backend, which
 * forwards it to the attached server's `tasks/result` endpoint
 * (Phase 25 / D-134). The operator is the one driving the write — the
 * App's "Approve" / "Reject" click. A 503 (the inspector is detached)
 * surfaces as a thrown error so the App preview's error state shows an
 * honest message rather than a silent drop.
 */
export async function postElicitationResponse(
  request: ElicitationRequest,
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<ElicitationResponse> {
  const resp = await fetchImpl(`${base}/api/tasks/elicitation`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!resp.ok) {
    let detail = `inspector: /api/tasks/elicitation returned ${resp.status}`;
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
  if (typeof data !== 'object' || data === null) {
    return { taskId: request.taskId, delivered: false };
  }
  const d = data as Partial<ElicitationResponse>;
  return {
    taskId: typeof d.taskId === 'string' ? d.taskId : request.taskId,
    delivered: d.delivered === true,
    error: typeof d.error === 'string' ? d.error : undefined,
  };
}

/**
 * Fetches the project's on-disk fixtures from `GET /api/fixtures` (Phase 24,
 * D-126). When the inspector was attached with `--dir <project>`, the
 * backend reads `<project>/fixtures/<tool>/<kind>.json` and serves them
 * here; the Fixtures switcher prefers these realistic payloads over the
 * schema-derived synthetic fixtures. A detached or fixture-less project
 * yields an empty list and the switcher falls back to synthetic fixtures.
 */
import type { ProjectFixture, FixtureKind } from './fixtures.js';
export async function fetchProjectFixtures(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<ProjectFixture[]> {
  const resp = await fetchImpl(`${base}/api/fixtures`);
  if (!resp.ok) {
    throw new Error(`inspector: /api/fixtures returned ${resp.status}`);
  }
  const data: unknown = await resp.json();
  if (!Array.isArray(data)) return [];
  const knownKinds = new Set<string>([
    'happy', 'empty', 'error', 'permission', 'slow', 'large',
  ]);
  return data
    .filter((d): d is Record<string, unknown> => typeof d === 'object' && d !== null)
    .filter((d) =>
      typeof d.tool === 'string' &&
      typeof d.kind === 'string' &&
      knownKinds.has(d.kind),
    )
    .map((d) => ({
      tool: d.tool as string,
      kind: d.kind as FixtureKind,
      description: typeof d.description === 'string' ? d.description : undefined,
      state: typeof d.state === 'string' ? d.state : (d.kind as string),
      input:
        typeof d.input === 'object' && d.input !== null
          ? (d.input as Record<string, unknown>)
          : undefined,
      structuredContent:
        typeof d.structuredContent === 'object' && d.structuredContent !== null
          ? (d.structuredContent as Record<string, unknown>)
          : undefined,
    }));
}

/**
 * Fetches the attached server's registered MCP Prompts from
 * `GET /api/prompts` (v1.1 Wave A; closes D-151). The backend performs a
 * read-only prompts/list against the attached server (D-163 extends D-103's
 * pattern). A detached inspector yields an empty list and the panel renders
 * its four-state empty state; an unreachable server is a thrown error the
 * panel surfaces in its error state with a working retry.
 */
export async function fetchPrompts(
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<PromptInfo[]> {
  const resp = await fetchImpl(`${base}/api/prompts`);
  if (!resp.ok) {
    let detail = `inspector: /api/prompts returned ${resp.status}`;
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
  return parsePrompts(await resp.json());
}

/**
 * Invokes a prompt on the attached server through the inspector's
 * `POST /api/prompts/get` endpoint (v1.1 Wave A; D-163; RFC §12; P4). The
 * operator is the one driving the request through the UI — the inspector
 * itself remains the lone client-shaped surface, dev-mode-gated and
 * localhost-bound (the listener's loopback gate enforces it). A
 * transport-level failure (the server is unreachable, the prompt not
 * found) is a thrown error the caller surfaces in the panel's error
 * state. A server-side prompts/get error is returned as a result with
 * `error` filled in (the 200-with-error pattern D-131 set for tools).
 */
export async function invokePrompt(
  request: { name: string; arguments?: Record<string, string> },
  base = '',
  fetchImpl: typeof fetch = fetch,
): Promise<PromptGetResponse> {
  const resp = await fetchImpl(`${base}/api/prompts/get`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!resp.ok) {
    let detail = `inspector: /api/prompts/get returned ${resp.status}`;
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
  return parsePromptGetResponse(await resp.json());
}
