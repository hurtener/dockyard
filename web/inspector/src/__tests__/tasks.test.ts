/**
 * tasks.test.ts — the task-lifecycle fold from the obs/v1 stream.
 */
import { describe, expect, it } from 'vitest';
import {
  TASK_STATUSES,
  foldTasks,
  latestTaskProgress,
  lifecycleToTimeline,
  toneForStatus,
  isTerminal,
} from '../lib/tasks.js';
import type { ObsEvent } from '../lib/obs.js';

function progress(
  taskId: string,
  status: string,
  ts: string,
  message?: string,
): ObsEvent {
  return {
    schema_version: 'dockyard.obs/v1',
    id: `${taskId}-${status}`,
    timestamp: ts,
    server_id: 'srv',
    trace_id: 't',
    span_id: 's',
    kind: 'task.progress',
    phase: 'emit',
    payload: { task_id: taskId, status, message },
  };
}

describe('foldTasks', () => {
  it('folds a five-status lifecycle in stream order', () => {
    const events: ObsEvent[] = [
      progress('task-1', 'working', '2026-05-22T10:00:00Z'),
      progress('task-1', 'input_required', '2026-05-22T10:00:01Z'),
      progress('task-1', 'working', '2026-05-22T10:00:02Z'),
      progress('task-1', 'completed', '2026-05-22T10:00:03Z'),
    ];
    const lifecycles = foldTasks(events);
    expect(lifecycles.length).toBe(1);
    expect(lifecycles[0].taskId).toBe('task-1');
    expect(lifecycles[0].transitions.map((t) => t.status)).toEqual([
      'working',
      'input_required',
      'working',
      'completed',
    ]);
    expect(lifecycles[0].current).toBe('completed');
    expect(lifecycles[0].awaitingInput).toBe(false);
  });

  it('flags a task awaiting an input_required round-trip', () => {
    const lifecycles = foldTasks([
      progress('t', 'working', '2026-05-22T10:00:00Z'),
      progress('t', 'input_required', '2026-05-22T10:00:01Z'),
    ]);
    expect(lifecycles[0].awaitingInput).toBe(true);
  });

  it('ignores non-task.progress events and malformed payloads', () => {
    const events: ObsEvent[] = [
      { ...progress('t', 'working', 'x'), kind: 'tool.call' },
      progress('', 'working', 'x'),
      progress('t', 'bogus-status', 'x'),
    ];
    expect(foldTasks(events)).toEqual([]);
  });

  it('separates multiple tasks', () => {
    const lifecycles = foldTasks([
      progress('a', 'working', '2026-05-22T10:00:00Z'),
      progress('b', 'working', '2026-05-22T10:00:01Z'),
    ]);
    expect(lifecycles.length).toBe(2);
  });
});

describe('lifecycleToTimeline', () => {
  it('maps each transition onto a Timeline row', () => {
    const [life] = foldTasks([
      progress('t', 'working', '2026-05-22T10:00:00Z'),
      progress('t', 'completed', '2026-05-22T10:00:01Z', 'done'),
    ]);
    const rows = lifecycleToTimeline(life);
    expect(rows.length).toBe(2);
    expect(rows[1].title).toContain('completed');
    expect(rows[1].detail).toBe('done');
  });

  it('annotates an input_required row with round-trip copy', () => {
    const [life] = foldTasks([progress('t', 'input_required', 'x')]);
    expect(lifecycleToTimeline(life)[0].detail).toContain('input_required');
  });
});

function progressPoint(
  taskId: string,
  ts: string,
  fraction?: number,
  message?: string,
): ObsEvent {
  return {
    schema_version: 'dockyard.obs/v1',
    id: `${taskId}-${ts}`,
    timestamp: ts,
    server_id: 'srv',
    trace_id: 't',
    span_id: 's',
    kind: 'task.progress',
    phase: 'progress',
    payload: { task_id: taskId, status: 'working', fraction, message },
  };
}

describe('latestTaskProgress', () => {
  it('returns the most recent progress point with fraction + message', () => {
    const p = latestTaskProgress([
      progressPoint('t', '2026-05-22T10:00:00Z', 0.2, 'starting'),
      progressPoint('t', '2026-05-22T10:00:01Z', 0.62, 'halfway'),
    ]);
    expect(p).toEqual({ taskId: 't', fraction: 0.62, message: 'halfway', status: 'working' });
  });

  it('omits an absent fraction (a status-only point)', () => {
    const p = latestTaskProgress([progressPoint('t', 'x', undefined, 'phase change')]);
    expect(p).toEqual({ taskId: 't', message: 'phase change', status: 'working' });
    expect(p && 'fraction' in p).toBe(false);
  });

  it('ignores start/end lifecycle events and non-task kinds', () => {
    const start: ObsEvent = { ...progressPoint('t', 'x', 0.1), phase: 'start' };
    const end: ObsEvent = { ...progressPoint('t', 'y', 1), phase: 'end' };
    const toolCall: ObsEvent = { ...progressPoint('t', 'z', 0.5), kind: 'tool.call' };
    expect(latestTaskProgress([start, end, toolCall])).toBeUndefined();
  });

  it('returns undefined when there is no progress point (clean degradation)', () => {
    expect(latestTaskProgress([])).toBeUndefined();
  });
});

describe('toneForStatus / isTerminal', () => {
  it('maps every status to a tone', () => {
    for (const s of TASK_STATUSES) {
      expect(toneForStatus(s)).toBeTruthy();
    }
  });
  it('identifies the three terminal statuses', () => {
    expect(isTerminal('completed')).toBe(true);
    expect(isTerminal('failed')).toBe(true);
    expect(isTerminal('cancelled')).toBe(true);
    expect(isTerminal('working')).toBe(false);
    expect(isTerminal('input_required')).toBe(false);
  });
});
