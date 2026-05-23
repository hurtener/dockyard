/**
 * fixtures.ts — the inspector's fixture switcher.
 *
 * The fixture switcher (RFC §12) lets a developer exercise an App's UI states
 * — happy / empty / error / permission / slow / large — WITHOUT a running
 * backend. Each fixture is derived FROM the tool's generated output contract
 * (`contracts.ts`): the inspector owns the contracts (P1 — contract-first), so
 * a fixture is always a structurally valid instance of the schema it stands
 * for, never hand-written drift.
 *
 * Selecting a fixture feeds the App synthetic `structuredContent` of the
 * chosen shape via the host-half bridge's `sendToolResult` — closing Phase
 * 22's `tools/call` not-wired seam: a `tools/call` in the inspector is
 * answered from the active fixture.
 */

import type { ContractSchema, ToolContract } from './contracts.js';

/** The six fixture kinds the switcher offers (RFC §12). */
export const FIXTURE_KINDS = [
  'happy',
  'empty',
  'error',
  'permission',
  'slow',
  'large',
] as const;

/** One of the six fixture kinds. */
export type FixtureKind = (typeof FIXTURE_KINDS)[number];

/** A fixture's human label + one-line description, for the switcher UI. */
export const FIXTURE_META: Record<
  FixtureKind,
  { label: string; description: string }
> = {
  happy: {
    label: 'Happy path',
    description: 'A fully populated, valid result — the App renders its ready state.',
  },
  empty: {
    label: 'Empty',
    description: 'A structurally valid but empty result — the App renders its empty state.',
  },
  error: {
    label: 'Error',
    description: 'A tool error — the App renders its error state with retry.',
  },
  permission: {
    label: 'Permission denied',
    description: 'A permission-denied error — the App renders its permission state.',
  },
  slow: {
    label: 'Slow',
    description: 'A delayed result — the App renders its loading state before resolving.',
  },
  large: {
    label: 'Large',
    description: 'A high-volume result — the App renders under a large dataset.',
  },
};

/** The synthetic `tools/call` outcome a fixture feeds the App. */
export interface Fixture {
  /** Which of the six kinds this is. */
  kind: FixtureKind;
  /** True when the fixture stands for a failed tool call. */
  isError: boolean;
  /** The synthetic `structuredContent` — present for a successful fixture. */
  structuredContent?: unknown;
  /** A plain-text content line, mirroring a tool Result's `Text`. */
  text: string;
  /** An error message — present when `isError`. */
  error?: { code: number; message: string };
  /** An artificial delay (ms) before the result resolves — the slow fixture. */
  delayMs: number;
}

/** The artificial delay the `slow` fixture injects before resolving. */
export const SLOW_FIXTURE_DELAY_MS = 1500;

/** The row count the `large` fixture generates for an array-typed field. */
export const LARGE_FIXTURE_ROWS = 250;

/**
 * A project-on-disk fixture loaded from `fixtures/<tool>/<kind>.json` (Phase
 * 24, decision D-126). The inspector backend's `/api/fixtures` endpoint
 * serves these; when present, [buildFixtures] uses them in preference to the
 * schema-derived synthetic payloads so the App renders realistic data
 * (real chart values, real table rows, a real metric card) rather than the
 * `"sample-value"` placeholders the schema instantiator can produce.
 */
export interface ProjectFixture {
  tool: string;
  kind: FixtureKind;
  description?: string;
  state: string;
  input?: Record<string, unknown>;
  structuredContent?: Record<string, unknown>;
}

/**
 * Builds the six fixtures for a tool contract. When a project on-disk fixture
 * is provided in `overrides`, its `structuredContent` (and description) take
 * precedence over the schema-derived synthetic payload — the inspector's
 * /api/fixtures loader (Phase 24) wires this. With no override, every fixture
 * is generated from `contract.outputSchema` so it remains a structurally
 * valid instance of the tool's generated output contract (P1).
 */
export function buildFixtures(
  contract: ToolContract,
  overrides?: Partial<Record<FixtureKind, ProjectFixture>>,
): Record<FixtureKind, Fixture> {
  const schema = contract.outputSchema;
  const pick = (kind: FixtureKind, fallback: unknown): unknown => {
    const o = overrides?.[kind];
    if (o?.structuredContent && Object.keys(o.structuredContent).length > 0) {
      return o.structuredContent;
    }
    return fallback;
  };
  return {
    happy: {
      kind: 'happy',
      isError: false,
      structuredContent: pick('happy', instantiate(schema, 'happy')),
      text: `${contract.name}: result ready`,
      delayMs: 0,
    },
    empty: {
      kind: 'empty',
      isError: false,
      structuredContent: pick('empty', instantiate(schema, 'empty')),
      text: `${contract.name}: no results`,
      delayMs: 0,
    },
    error: {
      kind: 'error',
      isError: true,
      structuredContent: overrides?.error?.structuredContent,
      text: `${contract.name}: tool error`,
      error: { code: -32000, message: 'the tool handler returned an error' },
      delayMs: 0,
    },
    permission: {
      kind: 'permission',
      isError: true,
      structuredContent: overrides?.permission?.structuredContent,
      text: `${contract.name}: permission denied`,
      error: { code: -32003, message: 'permission denied for this tool' },
      delayMs: 0,
    },
    slow: {
      kind: 'slow',
      isError: false,
      structuredContent: pick('slow', instantiate(schema, 'happy')),
      text: `${contract.name}: result ready (delayed)`,
      delayMs: SLOW_FIXTURE_DELAY_MS,
    },
    large: {
      kind: 'large',
      isError: false,
      structuredContent: pick('large', instantiate(schema, 'large')),
      text: `${contract.name}: ${LARGE_FIXTURE_ROWS} results`,
      delayMs: 0,
    },
  };
}

/** Fixture-generation mode, varying the volume an array-typed field carries. */
type Mode = 'happy' | 'empty' | 'large';

/**
 * Instantiates a JSON-Schema as a concrete value. It is a pure, total mapping
 * — every schema yields a value, an unknown schema yields null — so a fixture
 * is always producible from any generated contract.
 */
export function instantiate(
  schema: ContractSchema | undefined,
  mode: Mode,
): unknown {
  if (!schema) return null;
  switch (schema.type) {
    case 'object':
      return instantiateObject(schema, mode);
    case 'array':
      return instantiateArray(schema, mode);
    case 'string':
      return instantiateString(schema, mode);
    case 'number':
    case 'integer':
      return mode === 'empty' ? 0 : sampleNumber(mode);
    case 'boolean':
      return mode !== 'empty';
    default:
      // An unconstrained schema (`true` — e.g. json.RawMessage) or an
      // unrecognised type: a tolerant null, never a throw.
      return schema.enum && schema.enum.length > 0 ? schema.enum[0] : null;
  }
}

function instantiateObject(
  schema: ContractSchema,
  mode: Mode,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const props = schema.properties ?? {};
  for (const [key, propSchema] of Object.entries(props)) {
    out[key] = instantiate(propSchema, mode);
  }
  return out;
}

function instantiateArray(schema: ContractSchema, mode: Mode): unknown[] {
  if (mode === 'empty') return [];
  const count = mode === 'large' ? LARGE_FIXTURE_ROWS : 3;
  const item = schema.items;
  const out: unknown[] = [];
  for (let i = 0; i < count; i++) {
    out.push(instantiate(item, mode === 'large' ? 'large' : 'happy'));
  }
  return out;
}

function instantiateString(schema: ContractSchema, mode: Mode): string {
  if (schema.enum && schema.enum.length > 0) {
    return String(schema.enum[0]);
  }
  if (mode === 'empty') return '';
  if (schema.format === 'date-time') {
    return new Date(0).toISOString();
  }
  return mode === 'large' ? 'sample-value-large' : 'sample-value';
}

function sampleNumber(mode: Mode): number {
  return mode === 'large' ? 999999 : 42;
}
