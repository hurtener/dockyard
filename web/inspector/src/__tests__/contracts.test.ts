/**
 * contracts.test.ts — the inspector's read-only generated-contract model.
 */
import { describe, expect, it } from 'vitest';
import {
  parseToolContract,
  parseContracts,
  isContractSchema,
} from '../lib/contracts.js';

describe('parseToolContract', () => {
  it('parses a well-formed contract', () => {
    const c = parseToolContract({
      name: 'report',
      description: 'a report',
      outputSchema: { type: 'object' },
    });
    expect(c).not.toBeNull();
    expect(c?.name).toBe('report');
    expect(c?.outputSchema?.type).toBe('object');
  });

  it('rejects a value with no name', () => {
    expect(parseToolContract({ description: 'x' })).toBeNull();
    expect(parseToolContract(null)).toBeNull();
    expect(parseToolContract('string')).toBeNull();
  });

  it('tolerates a missing schema', () => {
    const c = parseToolContract({ name: 'minimal' });
    expect(c?.name).toBe('minimal');
    expect(c?.outputSchema).toBeUndefined();
  });
});

describe('parseContracts', () => {
  it('parses an array, dropping malformed entries', () => {
    const cs = parseContracts([
      { name: 'a' },
      { description: 'no name' },
      { name: 'b' },
    ]);
    expect(cs.map((c) => c.name)).toEqual(['a', 'b']);
  });

  it('returns an empty list for a non-array', () => {
    expect(parseContracts({})).toEqual([]);
    expect(parseContracts(null)).toEqual([]);
  });
});

describe('isContractSchema', () => {
  it('accepts an object, rejects a primitive', () => {
    expect(isContractSchema({ type: 'object' })).toBe(true);
    expect(isContractSchema(null)).toBe(false);
    expect(isContractSchema('x')).toBe(false);
  });
});
