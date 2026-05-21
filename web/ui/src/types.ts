/**
 * types.ts — shared component prop types for the web/ui inventory.
 *
 * Types used by more than one component live here so a consumer imports one
 * vocabulary; per-component prop interfaces live with the component.
 */

/** The four states every async region routes through (CONVENTIONS.md §4). */
export type PageStateValue = 'loading' | 'empty' | 'error' | 'ready';

/** The semantic tone of a chip / state panel — maps to a state token trio. */
export type StatusTone = 'ok' | 'warn' | 'error' | 'info' | 'neutral';

/** A single `DataTable` column descriptor. `key` indexes into a row object. */
export interface Column {
  /** The row-object property this column renders. */
  key: string;
  /** The visible column header. */
  label: string;
  /** Whether the column participates in sort. Default false. */
  sortable?: boolean;
  /** Optional fixed width (any CSS length). */
  width?: string;
  /** Cell text alignment. Default `left`. */
  align?: 'left' | 'center' | 'right';
}

/** The active sort applied to a `DataTable`. */
export interface SortState {
  key: string;
  direction: 'asc' | 'desc';
}

/** One entry in a `Timeline`. */
export interface TimelineEvent {
  /** Stable identity for keyed rendering. */
  id: string;
  /** The event title / summary line. */
  title: string;
  /** A pre-formatted timestamp string. */
  timestamp: string;
  /** Optional secondary detail line. */
  detail?: string;
  /** Optional tone — colours the marker. Default `neutral`. */
  tone?: StatusTone;
}

/** A row object passed to `DataTable`; values are rendered as text. */
export type Row = Record<string, unknown>;
