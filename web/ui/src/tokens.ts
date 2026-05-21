/**
 * tokens.ts — the typed view of the Dockyard design tokens.
 *
 * `tokens.css` is the runtime source of truth (the `--dy-*` CSS custom
 * properties). This module is its *typed* companion: it names every token so
 * TypeScript code and component props refer to tokens by a checked identifier
 * rather than a raw string, and it exposes `tokenVar()` to build a
 * `var(--dy-…)` reference. Importing this module also side-effect-imports the
 * stylesheet, so a consumer gets both the values and the types from one import.
 *
 * Why both forms (design-spec.md §2, D-065): the CSS layer is what a browser —
 * and an MCP host theme override — actually reads; the TS layer is what keeps
 * component code from hard-coding a token name that does not exist.
 */

import './tokens.css';

/** A `--dy-*` custom-property name. */
export type TokenName = `--dy-${string}`;

/** Builds a `var(--dy-…)` reference for use in inline styles or CSS strings. */
export function tokenVar(name: TokenName): string {
  return `var(${name})`;
}

/**
 * The token tree. Every leaf is the `--dy-*` custom-property name (not the
 * value): the value lives in `tokens.css` and is theme-dependent, so code reads
 * it through `var()`. `colorVar('primary')` etc. give an ergonomic accessor.
 */
export const tokens = {
  color: {
    ink: '--dy-color-ink',
    inkSoft: '--dy-color-ink-soft',
    primary: '--dy-color-primary',
    primaryStrong: '--dy-color-primary-strong',
    mint: '--dy-color-mint',
    accent: '--dy-color-accent',
    surface: '--dy-color-surface',
    canvas: '--dy-color-canvas',
    border: '--dy-color-border',
  },
  state: {
    ok: { fg: '--dy-state-ok-fg', bg: '--dy-state-ok-bg', border: '--dy-state-ok-border' },
    warn: { fg: '--dy-state-warn-fg', bg: '--dy-state-warn-bg', border: '--dy-state-warn-border' },
    error: { fg: '--dy-state-error-fg', bg: '--dy-state-error-bg', border: '--dy-state-error-border' },
    info: { fg: '--dy-state-info-fg', bg: '--dy-state-info-bg', border: '--dy-state-info-border' },
  },
  space: {
    0: '--dy-space-0',
    1: '--dy-space-1',
    2: '--dy-space-2',
    3: '--dy-space-3',
    4: '--dy-space-4',
    5: '--dy-space-5',
    6: '--dy-space-6',
    7: '--dy-space-7',
    8: '--dy-space-8',
  },
  font: {
    sans: '--dy-font-sans',
    mono: '--dy-font-mono',
  },
  text: {
    xs: '--dy-text-xs',
    sm: '--dy-text-sm',
    base: '--dy-text-base',
    md: '--dy-text-md',
    lg: '--dy-text-lg',
    xl: '--dy-text-xl',
  },
  weight: {
    regular: '--dy-weight-regular',
    medium: '--dy-weight-medium',
    semibold: '--dy-weight-semibold',
  },
  radius: {
    sm: '--dy-radius-sm',
    md: '--dy-radius-md',
    lg: '--dy-radius-lg',
    full: '--dy-radius-full',
  },
  elevation: {
    flat: '--dy-elevation-flat',
    raised: '--dy-elevation-raised',
    overlay: '--dy-elevation-overlay',
  },
  focusRing: '--dy-focus-ring',
} as const;

/** The two semantic theme names. V1 ships `light`; `dark` is a token swap. */
export type ThemeName = 'light' | 'dark';

/**
 * Sets the active theme on an element (default `<html>`) by toggling the
 * `data-dy-theme` attribute the token blocks in `tokens.css` are scoped to.
 */
export function applyTheme(theme: ThemeName, target?: HTMLElement): void {
  const el = target ?? (typeof document !== 'undefined' ? document.documentElement : undefined);
  el?.setAttribute('data-dy-theme', theme);
}
