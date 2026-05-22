/**
 * analytics.test.ts — per-tool latency / error / volume from the obs/v1 stream.
 */
import { describe, expect, it } from 'vitest';
import {
  foldAnalytics,
  totalsOf,
  toolNameOf,
  formatRate,
} from '../lib/analytics.js';
import type { ObsEvent } from '../lib/obs.js';

function ev(over: Partial<ObsEvent>): ObsEvent {
  return {
    schema_version: 'dockyard.obs/v1',
    id: Math.random().toString(36).slice(2),
    timestamp: '2026-05-22T10:00:00Z',
    server_id: 'srv',
    trace_id: 't',
    span_id: 's',
    kind: 'tool.call',
    phase: 'end',
    ...over,
  };
}

describe('foldAnalytics', () => {
  it('folds per-tool call volume, error rate, and latency', () => {
    const events: ObsEvent[] = [
      ev({ payload: { tool: 'report' }, duration_ms: 100 }),
      ev({ payload: { tool: 'report' }, duration_ms: 300 }),
      ev({
        payload: { tool: 'report' },
        duration_ms: 50,
        error: { type: 'x', message: 'boom' },
      }),
      ev({ payload: { tool: 'fetch' }, duration_ms: 20 }),
    ];
    const rows = foldAnalytics(events);
    expect(rows.length).toBe(2);
    // Sorted busiest-first.
    expect(rows[0].tool).toBe('report');
    expect(rows[0].calls).toBe(3);
    expect(rows[0].errors).toBe(1);
    expect(rows[0].errorRate).toBeCloseTo(1 / 3);
    expect(rows[0].avgLatencyMs).toBe(150);
    expect(rows[0].maxLatencyMs).toBe(300);
  });

  it('ignores non-tool.call events and start-phase events', () => {
    const events: ObsEvent[] = [
      ev({ kind: 'server.log', payload: { tool: 'report' } }),
      ev({ phase: 'start', payload: { tool: 'report' } }),
    ];
    expect(foldAnalytics(events)).toEqual([]);
  });

  it('buckets an unnamed tool under (unknown), never dropping signal', () => {
    const rows = foldAnalytics([ev({ payload: {} })]);
    expect(rows[0].tool).toBe('(unknown)');
  });
});

describe('totalsOf', () => {
  it('aggregates totals across tools', () => {
    const rows = foldAnalytics([
      ev({ payload: { tool: 'a' }, duration_ms: 100 }),
      ev({
        payload: { tool: 'b' },
        duration_ms: 200,
        error: { type: 'x', message: 'e' },
      }),
    ]);
    const totals = totalsOf(rows);
    expect(totals.calls).toBe(2);
    expect(totals.errors).toBe(1);
    expect(totals.errorRate).toBeCloseTo(0.5);
  });

  it('reports zeroes for an empty table', () => {
    expect(totalsOf([])).toEqual({
      calls: 0,
      errors: 0,
      errorRate: 0,
      avgLatencyMs: 0,
    });
  });
});

describe('toolNameOf / formatRate', () => {
  it('reads the tool name from a name field too', () => {
    expect(toolNameOf(ev({ payload: { name: 'reso' } }))).toBe('reso');
  });
  it('formats a rate as a percentage', () => {
    expect(formatRate(0.125)).toBe('12.5%');
    expect(formatRate(0)).toBe('0.0%');
  });
});
