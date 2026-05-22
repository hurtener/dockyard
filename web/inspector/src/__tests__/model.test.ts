/**
 * model.test.ts — the rpc.ts, timeline.ts, and api.ts pure model modules.
 */
import { describe, expect, it, vi } from 'vitest';
import {
  parseRelayLog,
  methodsIn,
  filterByMethod,
  fromRelayEntry,
  type RpcEntry,
} from '../lib/rpc.js';
import {
  toTimelineEvent,
  toneFor,
  titleFor,
  detailFor,
  kindsIn,
  filterByKind,
  formatTimestamp,
} from '../lib/timeline.js';
import { fetchServerInfo, fetchRpcLog, obsStreamURL } from '../lib/api.js';
import type { ObsEvent } from '../lib/obs.js';

const obs = (over: Partial<ObsEvent>): ObsEvent => ({
  schema_version: 'dockyard.obs/v1',
  id: 'e',
  timestamp: '2026-05-22T10:00:00Z',
  server_id: 'srv',
  trace_id: 't',
  span_id: 's',
  kind: 'tool.call',
  phase: 'end',
  ...over,
});

describe('rpc model', () => {
  it('parses a relay log array and ignores non-objects', () => {
    const log = parseRelayLog([
      { seq: 0, timestamp: '2026-05-22T10:00:00Z', direction: 'inbound', method: 'tools/call' },
      'garbage',
      { seq: 1, timestamp: '2026-05-22T10:00:01Z', direction: 'outbound' },
    ]);
    expect(log).toHaveLength(2);
    expect(log[0].id).toBe('relay-0');
    expect(log[0].method).toBe('tools/call');
  });

  it('parseRelayLog returns [] for a non-array', () => {
    expect(parseRelayLog({})).toEqual([]);
    expect(parseRelayLog(null)).toEqual([]);
  });

  it('fromRelayEntry falls back to now for a bad timestamp', () => {
    const e = fromRelayEntry({ seq: 5, timestamp: 'not-a-date', direction: 'inbound' });
    expect(e.at).toBeGreaterThan(0);
  });

  it('methodsIn and filterByMethod work together', () => {
    const entries: RpcEntry[] = [
      { id: 'a', direction: 'inbound', method: 'tools/call', payload: {}, at: 1 },
      { id: 'b', direction: 'inbound', method: 'ui/initialize', payload: {}, at: 2 },
      { id: 'c', direction: 'outbound', payload: {}, at: 3 },
    ];
    expect(methodsIn(entries)).toEqual(['tools/call', 'ui/initialize']);
    expect(filterByMethod(entries, [])).toHaveLength(3);
    expect(filterByMethod(entries, ['ui/initialize'])).toHaveLength(1);
  });
});

describe('timeline model', () => {
  it('maps an obs event onto a TimelineEvent', () => {
    const t = toTimelineEvent(obs({ id: 'x', kind: 'tool.call', phase: 'end', duration_ms: 12 }));
    expect(t.id).toBe('x');
    expect(t.title).toContain('tool.call');
    expect(t.detail).toBe('12 ms');
    expect(t.tone).toBe('ok');
  });

  it('tones an errored event — and a silent failure — as error', () => {
    expect(toneFor(obs({ error: { type: 'handler_error', message: 'boom' } }))).toBe('error');
    const silent = obs({ error: { type: 'x', message: 'y', silent: true } });
    expect(detailFor(silent)).toContain('(silent)');
  });

  it('tones by phase', () => {
    expect(toneFor(obs({ phase: 'start' }))).toBe('info');
    expect(toneFor(obs({ phase: 'progress' }))).toBe('neutral');
    expect(toneFor(obs({ phase: 'emit' }))).toBe('neutral');
  });

  it('titleFor omits the phase for an emit event', () => {
    expect(titleFor(obs({ kind: 'log', phase: 'emit' }))).toBe('log');
    expect(titleFor(obs({ kind: 'tool.call', phase: 'start' }))).toContain('start');
  });

  it('detailFor falls back to the session id', () => {
    expect(detailFor(obs({ phase: 'start', session_id: 'sess1' }))).toBe('session sess1');
    expect(detailFor(obs({ phase: 'start' }))).toBe('');
  });

  it('formatTimestamp passes a bad value through', () => {
    expect(formatTimestamp('not-a-date')).toBe('not-a-date');
    expect(formatTimestamp('2026-05-22T10:00:00Z')).not.toBe('2026-05-22T10:00:00Z');
  });

  it('kindsIn and filterByKind work together', () => {
    const events = [obs({ kind: 'tool.call' }), obs({ kind: 'app.load' })];
    expect(kindsIn(events)).toEqual(['app.load', 'tool.call']);
    expect(filterByKind(events, [])).toHaveLength(2);
    expect(filterByKind(events, ['app.load'])).toHaveLength(1);
  });
});

describe('api client', () => {
  it('fetchServerInfo decodes the identity', async () => {
    const fake = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ name: 'demo', version: '1.0', transport: 'inmem' }),
    });
    const info = await fetchServerInfo('', fake as unknown as typeof fetch);
    expect(info).toEqual({ name: 'demo', version: '1.0', transport: 'inmem' });
  });

  it('fetchServerInfo throws on a non-ok response', async () => {
    const fake = vi.fn().mockResolvedValue({ ok: false, status: 500 });
    await expect(
      fetchServerInfo('', fake as unknown as typeof fetch),
    ).rejects.toThrow(/500/);
  });

  it('fetchRpcLog parses the relay log', async () => {
    const fake = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        { seq: 0, timestamp: '2026-05-22T10:00:00Z', direction: 'inbound', method: 'tools/call' },
      ],
    });
    const log = await fetchRpcLog('', fake as unknown as typeof fetch);
    expect(log).toHaveLength(1);
  });

  it('fetchRpcLog throws on a non-ok response', async () => {
    const fake = vi.fn().mockResolvedValue({ ok: false, status: 404 });
    await expect(
      fetchRpcLog('', fake as unknown as typeof fetch),
    ).rejects.toThrow(/404/);
  });

  it('obsStreamURL composes the relay path', () => {
    expect(obsStreamURL('')).toBe('/api/obs/stream');
    expect(obsStreamURL('http://127.0.0.1:9')).toBe('http://127.0.0.1:9/api/obs/stream');
  });
});
