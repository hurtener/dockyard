/**
 * tasks.ts — the inspector's task-lifecycle model.
 *
 * The inspector's Tasks panel (RFC §12, §8.6) renders the MCP Tasks five-status
 * lifecycle and `input_required` round-trips. Like every inspector signal it is
 * derived from the `obs/v1` event stream the inspector already consumes
 * (CLAUDE.md §6 / P2) — the runtime emits a `task.progress` obs/v1 event on
 * every task status transition; this module folds that stream into a per-task
 * lifecycle and maps each transition onto a web/ui `Timeline` row.
 */

import type { StatusTone, TimelineEvent } from '@dockyard/ui';
import type { ObsEvent } from './obs.js';

/** The MCP Tasks five-status lifecycle (RFC §8.6, protocolcodec.TaskStatus). */
export const TASK_STATUSES = [
  'working',
  'input_required',
  'completed',
  'failed',
  'cancelled',
] as const;

/** One MCP Tasks lifecycle status. */
export type TaskStatus = (typeof TASK_STATUSES)[number];

/** The obs/v1 `kind` carrying a task status transition. */
const TASK_KIND = 'task.progress';

/** The terminal task statuses — no transition leaves them. */
const TERMINAL: ReadonlySet<TaskStatus> = new Set([
  'completed',
  'failed',
  'cancelled',
]);

/** True when `status` is a terminal task status. */
export function isTerminal(status: TaskStatus): boolean {
  return TERMINAL.has(status);
}

/** One observed transition in a task's lifecycle. */
export interface TaskTransition {
  /** The status the task moved into. */
  status: TaskStatus;
  /** The obs/v1 event timestamp (ISO 8601). */
  timestamp: string;
  /** An optional human message carried by the transition. */
  message?: string;
}

/** A task's full observed lifecycle, folded from the obs/v1 stream. */
export interface TaskLifecycle {
  /** The task id. */
  taskId: string;
  /** Every observed transition, in stream order. */
  transitions: TaskTransition[];
  /** The most recent status. */
  current: TaskStatus;
  /** True when the task is currently awaiting an `input_required` round-trip. */
  awaitingInput: boolean;
}

/** Reads a task id out of an obs/v1 event payload, or "" when absent. */
function taskIdOf(event: ObsEvent): string {
  const p = event.payload;
  if (typeof p === 'object' && p !== null) {
    const rec = p as Record<string, unknown>;
    if (typeof rec.task_id === 'string') return rec.task_id;
    if (typeof rec.taskId === 'string') return rec.taskId;
  }
  return '';
}

/** Reads a task status out of an obs/v1 event payload, or null when absent. */
function statusOf(event: ObsEvent): TaskStatus | null {
  const p = event.payload;
  if (typeof p === 'object' && p !== null) {
    const rec = p as Record<string, unknown>;
    const s = rec.status;
    if (typeof s === 'string' && (TASK_STATUSES as readonly string[]).includes(s)) {
      return s as TaskStatus;
    }
  }
  return null;
}

/** Reads an optional human message from an obs/v1 event payload. */
function messageOf(event: ObsEvent): string | undefined {
  const p = event.payload;
  if (typeof p === 'object' && p !== null) {
    const rec = p as Record<string, unknown>;
    if (typeof rec.message === 'string' && rec.message !== '') {
      return rec.message;
    }
  }
  return undefined;
}

/**
 * Folds an obs/v1 event stream into a per-task lifecycle table. Only
 * `task.progress` events with a resolvable task id and status count. The
 * result is sorted by most-recent activity first.
 */
export function foldTasks(events: ObsEvent[]): TaskLifecycle[] {
  const table = new Map<string, TaskLifecycle>();
  for (const ev of events) {
    if (ev.kind !== TASK_KIND) continue;
    const taskId = taskIdOf(ev);
    const status = statusOf(ev);
    if (taskId === '' || status === null) continue;

    const life = table.get(taskId) ?? {
      taskId,
      transitions: [],
      current: status,
      awaitingInput: false,
    };
    life.transitions.push({
      status,
      timestamp: ev.timestamp,
      message: messageOf(ev),
    });
    life.current = status;
    life.awaitingInput = status === 'input_required';
    table.set(taskId, life);
  }
  return [...table.values()].sort((a, b) => {
    const at = a.transitions[a.transitions.length - 1]?.timestamp ?? '';
    const bt = b.transitions[b.transitions.length - 1]?.timestamp ?? '';
    return bt.localeCompare(at);
  });
}

/** The Timeline marker tone for a task status. */
export function toneForStatus(status: TaskStatus): StatusTone {
  switch (status) {
    case 'completed':
      return 'ok';
    case 'failed':
      return 'error';
    case 'cancelled':
      return 'warn';
    case 'input_required':
      return 'info';
    case 'working':
    default:
      return 'neutral';
  }
}

/** Formats an ISO timestamp for a Timeline row. */
function formatTime(iso: string): string {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? iso : new Date(t).toLocaleTimeString();
}

/**
 * Maps one task's lifecycle onto web/ui `Timeline` rows — one row per observed
 * transition, so the Tasks panel renders the lifecycle (and any
 * `input_required` round-trip) as a Timeline (RFC §12, §8.6).
 */
export function lifecycleToTimeline(life: TaskLifecycle): TimelineEvent[] {
  return life.transitions.map((tr, i) => ({
    id: `${life.taskId}-${i}`,
    title: `${life.taskId} → ${tr.status}`,
    timestamp: formatTime(tr.timestamp),
    detail:
      tr.message ??
      (tr.status === 'input_required'
        ? 'awaiting input — an input_required round-trip'
        : undefined),
    tone: toneForStatus(tr.status),
  }));
}
