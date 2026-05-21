/**
 * tokens.test.ts — the typed token tree stays consistent with the `--dy-*`
 * naming contract, and theming toggles the documented attribute.
 */
import { describe, expect, it } from 'vitest';
import { tokens, tokenVar, applyTheme } from '../tokens.js';

describe('design tokens', () => {
  it('every leaf is a --dy-* custom-property name', () => {
    const leaves: string[] = [];
    const walk = (node: unknown): void => {
      if (typeof node === 'string') {
        leaves.push(node);
      } else if (node && typeof node === 'object') {
        Object.values(node).forEach(walk);
      }
    };
    walk(tokens);

    expect(leaves.length).toBeGreaterThan(20);
    for (const name of leaves) {
      expect(name).toMatch(/^--dy-/);
    }
  });

  it('exposes the palette, state trios, spacing, type, radius and elevation', () => {
    expect(tokens.color.primary).toBe('--dy-color-primary');
    expect(tokens.state.error.fg).toBe('--dy-state-error-fg');
    expect(tokens.state.ok.bg).toBe('--dy-state-ok-bg');
    expect(tokens.space[4]).toBe('--dy-space-4');
    expect(tokens.text.base).toBe('--dy-text-base');
    expect(tokens.radius.md).toBe('--dy-radius-md');
    expect(tokens.elevation.raised).toBe('--dy-elevation-raised');
    expect(tokens.focusRing).toBe('--dy-focus-ring');
  });

  it('tokenVar wraps a token name in a CSS var() reference', () => {
    expect(tokenVar(tokens.color.primary)).toBe('var(--dy-color-primary)');
  });

  it('applyTheme toggles the data-dy-theme attribute', () => {
    const el = document.createElement('div');
    applyTheme('dark', el);
    expect(el.getAttribute('data-dy-theme')).toBe('dark');
    applyTheme('light', el);
    expect(el.getAttribute('data-dy-theme')).toBe('light');
  });
});
