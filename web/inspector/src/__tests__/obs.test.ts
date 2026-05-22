/**
 * obs.test.ts — the inspector's obs/v1 stream client.
 */
import { describe, expect, it, vi } from 'vitest';
import {
  ObsStream,
  isObsEvent,
  parseObsEvent,
  type EventSourceLike,
  type ObsEvent,
} from '../lib/obs.js';

const sample: ObsEvent = {
  schema_version: 'dockyard.obs/v1',
  id: 'ev1',
  timestamp: '2026-05-22T10:00:00Z',
  server_id: 'srv',
  trace_id: 't',
  span_id: 's',
  kind: 'tool.call',
  phase: 'end',
};

describe('isObsEvent / parseObsEvent', () => {
  it('accepts a well-formed obs/v1 event', () => {
    expect(isObsEvent(sample)).toBe(true);
    expect(parseObsEvent(JSON.stringify(sample))?.id).toBe('ev1');
  });

  it('rejects a non-event and malformed JSON', () => {
    expect(isObsEvent({ foo: 1 })).toBe(false);
    expect(isObsEvent(null)).toBe(false);
    expect(parseObsEvent('{not json')).toBeNull();
    expect(parseObsEvent('{"foo":1}')).toBeNull();
  });
});

/** A controllable fake EventSource. */
function fakeSource(): EventSourceLike & { emit: (data: string) => void } {
  const src: EventSourceLike & { emit: (data: string) => void } = {
    onopen: null,
    onerror: null,
    onmessage: null,
    close: vi.fn(),
    emit(data: string) {
      this.onmessage?.({ data });
    },
  };
  return src;
}

describe('ObsStream', () => {
  it('decodes events from the SSE source and delivers them', () => {
    const src = fakeSource();
    const events: ObsEvent[] = [];
    const stream = new ObsStream(
      '/api/obs/stream',
      { onEvent: (e) => events.push(e) },
      () => src,
    );
    stream.open();
    src.onopen?.();
    src.emit(JSON.stringify(sample));
    src.emit('{garbage'); // malformed — dropped, not delivered.
    expect(events).toHaveLength(1);
    expect(events[0].id).toBe('ev1');
  });

  it('reports open and error and is idempotent on open/close', () => {
    const src = fakeSource();
    const onOpen = vi.fn();
    const onError = vi.fn();
    const stream = new ObsStream(
      '/api/obs/stream',
      { onEvent: () => {}, onOpen, onError },
      () => src,
    );
    stream.open();
    stream.open(); // idempotent
    src.onopen?.();
    src.onerror?.();
    expect(onOpen).toHaveBeenCalledOnce();
    expect(onError).toHaveBeenCalledOnce();
    stream.close();
    stream.close(); // idempotent
    expect(src.close).toHaveBeenCalledOnce();
  });

  it('does not open after close', () => {
    const src = fakeSource();
    const stream = new ObsStream(
      '/api/obs/stream',
      { onEvent: () => {} },
      () => src,
    );
    stream.close();
    stream.open();
    expect(src.close).not.toHaveBeenCalled();
  });
});
