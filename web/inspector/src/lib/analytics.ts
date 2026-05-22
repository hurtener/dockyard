/**
 * analytics.ts — per-tool latency / error / volume analytics.
 *
 * The inspector's analytics are derived PURELY from the `obs/v1` event stream
 * it already consumes (CLAUDE.md §6 / P2 — observability is a protocol; the
 * inspector never reads runtime internals for a signal, it reads the public
 * obs/v1 contract). This module is the pure fold from a list of {@link
 * ObsEvent}s to a per-tool analytics table.
 *
 * obs/v1 emits a `tool.call` event per tool invocation; the `end`-phase event
 * of a span carries `duration_ms`, and an `error` field marks a failure. The
 * tool name is read from the event payload's `tool` / `name` field.
 */

import type { ObsEvent } from './obs.js';

/** Aggregated analytics for one tool, folded from its obs/v1 events. */
export interface ToolAnalytics {
  /** The tool name. */
  tool: string;
  /** Total call volume — completed tool.call spans. */
  calls: number;
  /** How many of those calls carried an error. */
  errors: number;
  /** The error rate, 0..1. */
  errorRate: number;
  /** Mean latency in ms across calls that reported a duration. */
  avgLatencyMs: number;
  /** The slowest observed latency in ms. */
  maxLatencyMs: number;
}

/** The obs/v1 `kind` the analytics fold attributes to a tool invocation. */
const TOOL_CALL_KIND = 'tool.call';

/** Reads a tool name out of an obs/v1 event's payload, or "" if absent. */
export function toolNameOf(event: ObsEvent): string {
  const p = event.payload;
  if (typeof p === 'object' && p !== null) {
    const rec = p as Record<string, unknown>;
    if (typeof rec.tool === 'string') return rec.tool;
    if (typeof rec.name === 'string') return rec.name;
  }
  return '';
}

/**
 * Folds an obs/v1 event stream into a per-tool analytics table. Only
 * `tool.call` events count toward volume; the `end` phase of each is the
 * completed call. Events with no resolvable tool name are bucketed under
 * `(unknown)` so a partial stream never silently drops signal.
 *
 * The result is sorted by descending call volume — the busiest tool first.
 */
export function foldAnalytics(events: ObsEvent[]): ToolAnalytics[] {
  interface Acc {
    calls: number;
    errors: number;
    latencySum: number;
    latencyCount: number;
    maxLatency: number;
  }
  const table = new Map<string, Acc>();

  for (const ev of events) {
    if (ev.kind !== TOOL_CALL_KIND) continue;
    // A tool call is counted once, at its `end` phase — the completed span.
    // A single-shot `emit`-phase event is also counted (some emitters emit a
    // tool.call without a start/end pair).
    if (ev.phase !== 'end' && ev.phase !== 'emit') continue;

    const tool = toolNameOf(ev) || '(unknown)';
    const acc = table.get(tool) ?? {
      calls: 0,
      errors: 0,
      latencySum: 0,
      latencyCount: 0,
      maxLatency: 0,
    };
    acc.calls += 1;
    if (ev.error) acc.errors += 1;
    if (typeof ev.duration_ms === 'number') {
      acc.latencySum += ev.duration_ms;
      acc.latencyCount += 1;
      acc.maxLatency = Math.max(acc.maxLatency, ev.duration_ms);
    }
    table.set(tool, acc);
  }

  const out: ToolAnalytics[] = [];
  for (const [tool, acc] of table) {
    out.push({
      tool,
      calls: acc.calls,
      errors: acc.errors,
      errorRate: acc.calls > 0 ? acc.errors / acc.calls : 0,
      avgLatencyMs:
        acc.latencyCount > 0
          ? Math.round(acc.latencySum / acc.latencyCount)
          : 0,
      maxLatencyMs: acc.maxLatency,
    });
  }
  out.sort((a, b) => b.calls - a.calls || a.tool.localeCompare(b.tool));
  return out;
}

/** Aggregate totals across every tool — for the analytics summary cards. */
export interface AnalyticsTotals {
  /** Total tool calls across all tools. */
  calls: number;
  /** Total errored calls. */
  errors: number;
  /** Aggregate error rate, 0..1. */
  errorRate: number;
  /** Mean latency across all calls that reported a duration. */
  avgLatencyMs: number;
}

/** Folds a per-tool analytics table into aggregate totals. */
export function totalsOf(rows: ToolAnalytics[]): AnalyticsTotals {
  let calls = 0;
  let errors = 0;
  let latencyWeighted = 0;
  let latencyCalls = 0;
  for (const r of rows) {
    calls += r.calls;
    errors += r.errors;
    if (r.avgLatencyMs > 0) {
      latencyWeighted += r.avgLatencyMs * r.calls;
      latencyCalls += r.calls;
    }
  }
  return {
    calls,
    errors,
    errorRate: calls > 0 ? errors / calls : 0,
    avgLatencyMs: latencyCalls > 0 ? Math.round(latencyWeighted / latencyCalls) : 0,
  };
}

/** Formats a 0..1 rate as a percentage string, e.g. "12.5%". */
export function formatRate(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}
