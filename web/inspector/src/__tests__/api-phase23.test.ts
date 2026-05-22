/**
 * api-phase23.test.ts — the Phase 23 inspector API client (verdicts +
 * generated contracts).
 */
import { describe, expect, it, vi } from 'vitest';
import { fetchVerdicts, fetchContracts } from '../lib/api.js';

describe('fetchVerdicts', () => {
  it('decodes verdict rows from /api/verdicts', async () => {
    const fake = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        { check: 'stale-codegen', severity: 'error', message: 'stale' },
        { check: 'spec-compliance', severity: 'ok', message: 'OK' },
      ],
    });
    const verdicts = await fetchVerdicts('', fake as unknown as typeof fetch);
    expect(verdicts).toHaveLength(2);
    expect(verdicts[0].check).toBe('stale-codegen');
    expect(verdicts[0].severity).toBe('error');
  });

  it('returns an empty list for a non-array body', async () => {
    const fake = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    expect(await fetchVerdicts('', fake as unknown as typeof fetch)).toEqual([]);
  });

  it('throws on a non-ok response', async () => {
    const fake = vi.fn().mockResolvedValue({ ok: false, status: 500 });
    await expect(
      fetchVerdicts('', fake as unknown as typeof fetch),
    ).rejects.toThrow(/500/);
  });

  it('tolerates a malformed verdict row', async () => {
    const fake = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{ check: 42 }, null, { message: 'x' }],
    });
    const verdicts = await fetchVerdicts('', fake as unknown as typeof fetch);
    expect(verdicts).toHaveLength(2);
    expect(verdicts[0].check).toBe('unknown');
  });
});

describe('fetchContracts', () => {
  it('parses generated tool contracts from /api/contracts', async () => {
    const fake = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        { name: 'report', outputSchema: { type: 'object' } },
        { name: 'fetch' },
      ],
    });
    const contracts = await fetchContracts('', fake as unknown as typeof fetch);
    expect(contracts.map((c) => c.name)).toEqual(['report', 'fetch']);
  });

  it('throws on a non-ok response', async () => {
    const fake = vi.fn().mockResolvedValue({ ok: false, status: 404 });
    await expect(
      fetchContracts('', fake as unknown as typeof fetch),
    ).rejects.toThrow(/404/);
  });
});
