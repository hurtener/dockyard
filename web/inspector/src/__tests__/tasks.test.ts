/**
 * tasks.test.ts — the task-lifecycle fold from the obs/v1 stream.
 */
import { describe, expect, it } from 'vitest';
import {
  TASK_STATUSES,
  foldTasks,
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
