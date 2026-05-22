/**
 * fixtures.test.ts — the fixture switcher's contract-derived fixtures.
 *
 * Asserts the six fixtures are generated FROM a tool's generated output
 * contract (P1 — contract-first), and each fixture drives the UI state it
 * stands for.
 */
import { describe, expect, it } from 'vitest';
import {
  FIXTURE_KINDS,
  buildFixtures,
  instantiate,
  LARGE_FIXTURE_ROWS,
  SLOW_FIXTURE_DELAY_MS,
} from '../lib/fixtures.js';
import type { ToolContract } from '../lib/contracts.js';

const contract: ToolContract = {
  name: 'revenue',
  description: 'revenue report',
  outputSchema: {
    type: 'object',
    properties: {
      total: { type: 'number' },
      currency: { type: 'string' },
      ready: { type: 'boolean' },
      rows: {
        type: 'array',
        items: {
          type: 'object',
          properties: { label: { type: 'string' }, value: { type: 'number' } },
        },
      },
    },
  },
};

describe('buildFixtures', () => {
  it('builds all six fixture kinds', () => {
    const fixtures = buildFixtures(contract);
    for (const kind of FIXTURE_KINDS) {
      expect(fixtures[kind]).toBeDefined();
      expect(fixtures[kind].kind).toBe(kind);
    }
    expect(FIXTURE_KINDS.length).toBe(6);
  });

  it('derives happy structuredContent from the generated contract shape', () => {
    const happy = buildFixtures(contract).happy;
    expect(happy.isError).toBe(false);
    const sc = happy.structuredContent as Record<string, unknown>;
    // Every contract property is present — the fixture is a valid instance.
    expect(sc).toHaveProperty('total');
    expect(sc).toHaveProperty('currency');
    expect(sc).toHaveProperty('rows');
    expect(typeof sc.total).toBe('number');
    expect(Array.isArray(sc.rows)).toBe(true);
  });

  it('empty fixture yields a structurally valid but empty result', () => {
    const empty = buildFixtures(contract).empty;
    const sc = empty.structuredContent as Record<string, unknown>;
    expect(sc.total).toBe(0);
    expect(sc.currency).toBe('');
    expect(sc.rows).toEqual([]);
  });

  it('error and permission fixtures carry a JSON-RPC error', () => {
    const fixtures = buildFixtures(contract);
    expect(fixtures.error.isError).toBe(true);
    expect(fixtures.error.error?.code).toBe(-32000);
    expect(fixtures.permission.isError).toBe(true);
    expect(fixtures.permission.error?.code).toBe(-32003);
  });

  it('slow fixture injects an artificial delay', () => {
    expect(buildFixtures(contract).slow.delayMs).toBe(SLOW_FIXTURE_DELAY_MS);
  });

  it('large fixture generates a high-volume array', () => {
    const large = buildFixtures(contract).large;
    const sc = large.structuredContent as Record<string, unknown>;
    expect((sc.rows as unknown[]).length).toBe(LARGE_FIXTURE_ROWS);
  });
});

describe('instantiate', () => {
  it('is total — an undefined schema yields null, never a throw', () => {
    expect(instantiate(undefined, 'happy')).toBeNull();
  });

  it('seeds an enum-typed field with its first member', () => {
    expect(instantiate({ type: 'string', enum: ['a', 'b'] }, 'happy')).toBe('a');
  });

  it('renders a date-time string for a formatted string field', () => {
    const v = instantiate({ type: 'string', format: 'date-time' }, 'happy');
    expect(typeof v).toBe('string');
    expect(() => new Date(v as string)).not.toThrow();
  });

  it('tolerates an unconstrained schema', () => {
    expect(instantiate({}, 'happy')).toBeNull();
  });
});
