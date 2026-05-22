/**
 * timeline.ts — mapping obs/v1 events onto the web/ui `Timeline`.
 *
 * The Events panel renders the obs/v1 stream with the shared `Timeline`
 * component (web/ui — composed, never re-implemented, CLAUDE.md §20). This
 * module is the pure mapping from an {@link ObsEvent} to the `Timeline`'s
 * `TimelineEvent` shape, plus event filtering by kind.
 */

import type { TimelineEvent } from '@dockyard/ui';
import type { ObsEvent } from './obs.js';

/** The `StatusTone` web/ui's Timeline marker uses. */
type Tone = 'ok' | 'warn' | 'error' | 'info' | 'neutral';

/** Maps an obs/v1 event to the marker tone its Timeline row shows. */
export function toneFor(event: ObsEvent): Tone {
  if (event.error) {
    // A protocol-masked ("silent") failure is the highest-signal case
    // (RFC §11.3, brief 05 §2.2) — surface it as an error.
    return 'error';
  }
  switch (event.phase) {
    case 'start':
      return 'info';
    case 'end':
      return 'ok';
    case 'progress':
      return 'neutral';
    default:
      return 'neutral';
  }
}

/** A short, human title for an obs/v1 event row. */
export function titleFor(event: ObsEvent): string {
  const phase = event.phase === 'emit' ? '' : ` · ${event.phase}`;
  return `${event.kind}${phase}`;
}

/** A secondary detail line for an obs/v1 event row. */
export function detailFor(event: ObsEvent): string {
  if (event.error) {
    const silent = event.error.silent ? ' (silent)' : '';
    return `${event.error.type}: ${event.error.message}${silent}`;
  }
  if (typeof event.duration_ms === 'number') {
    return `${event.duration_ms} ms`;
  }
  return event.session_id ? `session ${event.session_id}` : '';
}

/** Formats an obs/v1 timestamp for display. */
export function formatTimestamp(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return new Date(t).toLocaleTimeString();
}

/** Maps one obs/v1 event onto a web/ui `TimelineEvent`. */
export function toTimelineEvent(event: ObsEvent): TimelineEvent {
  return {
    id: event.id,
    title: titleFor(event),
    timestamp: formatTimestamp(event.timestamp),
    detail: detailFor(event) || undefined,
    tone: toneFor(event),
  };
}

/** The distinct event kinds present in a stream — for the kind filter. */
export function kindsIn(events: ObsEvent[]): string[] {
  const seen = new Set<string>();
  for (const e of events) seen.add(e.kind);
  return [...seen].sort();
}

/** Filters events to those whose kind is in `kinds` (empty = all). */
export function filterByKind(events: ObsEvent[], kinds: string[]): ObsEvent[] {
  if (kinds.length === 0) return events;
  const want = new Set(kinds);
  return events.filter((e) => want.has(e.kind));
}
