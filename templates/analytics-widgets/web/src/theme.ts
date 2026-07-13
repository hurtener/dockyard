/**
 * theme.ts — the analytics-widgets template's tiny theming helper.
 *
 * Theming story (Phase 24, decisions D-125/D-127):
 *
 *   - The Dockyard bridge propagates the host's theme via
 *     `hostContext.styles.variables` (RFC §7.3). The default behaviour ("auto")
 *     applies those variables to the App's root element — so a host in dark
 *     mode renders the widgets dark with zero developer effort.
 *
 *   - Each tool's input takes an optional `theme?: "light" | "dark" | "auto"`
 *     override. "light" / "dark" pin the rendered widget; "auto" honours the
 *     host. The handler resolves "auto" / unset to "auto" and leaves the
 *     resolution to the App (this file).
 *
 *   - No theme registry, no skin system. The applied palette is whichever
 *     `--dy-*` CSS custom properties end up on the App root.
 */

import type { StyleVariables } from 'dockyard-bridge';
import type { ThemeMode } from '../../internal/contracts/contracts.js';

/**
 * Resolves the effective theme for one rendered widget.
 *
 *   - "light" / "dark" pin the widget regardless of the host.
 *   - "auto" returns whichever palette the host advertises via the
 *     `data-dy-theme` hint or, as a fallback, `prefers-color-scheme`.
 */
export function resolveTheme(
  perCall: ThemeMode | undefined,
  hostHint?: 'light' | 'dark',
): 'light' | 'dark' {
  if (perCall === 'light' || perCall === 'dark') {
    return perCall;
  }
  if (hostHint) {
    return hostHint;
  }
  if (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  ) {
    return 'dark';
  }
  return 'light';
}

/**
 * Applies the host's `styles.variables` to the App's root element as CSS
 * custom properties. Idempotent — call it again on every host-context change
 * and the previous variables are overwritten in place.
 */
export function applyHostVariables(
  root: HTMLElement,
  vars: StyleVariables | undefined,
): void {
  if (!vars) return;
  for (const [name, value] of Object.entries(vars)) {
    if (typeof value === 'string') {
      // CSS custom properties land as-is; the names already carry the
      // `--dy-*` prefix the design tokens expect.
      root.style.setProperty(name, value);
    }
  }
}

/**
 * Reads the host's preferred theme hint from a `styles.variables` map. The
 * bridge surface carries it on `--dy-host-theme` ("light" | "dark") when the
 * host advertises one — the App treats anything else as "auto".
 */
export function hostThemeHint(vars: StyleVariables | undefined): 'light' | 'dark' | undefined {
  if (!vars) return undefined;
  const hint = (vars as Record<string, string>)['--dy-host-theme'];
  return hint === 'dark' || hint === 'light' ? hint : undefined;
}
