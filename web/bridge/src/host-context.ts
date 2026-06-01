/**
 * host-context.ts — `hostContext` exposed as Svelte stores.
 *
 * The host delivers `hostContext` in the `ui/initialize` result and patches it
 * with `ui/notifications/host-context-changed` (brief 01 §2.4). The bridge
 * exposes the moving parts an App reacts to — theme, display mode, host CSS
 * variables, locale, container size — as readable Svelte stores, plus the raw
 * context for anything not promoted to its own store.
 *
 * `styles.variables` are also reflected onto a target element's inline style as
 * CSS custom properties, so an App is themed to the host without the author
 * wiring it by hand (RFC §7.3 / CONVENTIONS.md §5 — non-visual plumbing only;
 * the bridge applies the host's variables, it defines no tokens of its own).
 */

import { writable, type Readable, type Writable } from 'svelte/store';
import type {
  ContainerDimensions,
  DisplayMode,
  HostContext,
  HostContextChangedParams,
  StyleVariables,
} from './protocol.js';

/** A minimal element-style target — satisfied by `HTMLElement`. */
export interface StyleTarget {
  style: {
    setProperty(property: string, value: string): void;
    removeProperty(property: string): void;
  };
}

/** The reactive `hostContext` surface the bridge exposes. */
export interface HostContextStores {
  /** The full, last-known host context. */
  readonly raw: Readable<HostContext>;
  /** The host theme (`"light"` / `"dark"` / a host-defined string). */
  readonly theme: Readable<string | undefined>;
  /** The display mode currently in effect (RFC §7.2). */
  readonly displayMode: Readable<DisplayMode | undefined>;
  /** The display modes the host will grant — the negotiation surface (§7.5). */
  readonly availableDisplayModes: Readable<DisplayMode[]>;
  /** Standardized host CSS custom properties (`styles.variables`). */
  readonly styleVariables: Readable<StyleVariables>;
  /** The host locale (BCP-47). */
  readonly locale: Readable<string | undefined>;
  /** The iframe container dimensions. */
  readonly containerDimensions: Readable<ContainerDimensions | undefined>;
}

/**
 * Owns the `hostContext` stores and applies updates from the handshake result
 * and `host-context-changed` patches. Internal to the bridge; the public
 * surface is the readable `HostContextStores`.
 */
export class HostContextState {
  private readonly _raw: Writable<HostContext> = writable({});
  private readonly _theme: Writable<string | undefined> = writable(undefined);
  private readonly _displayMode: Writable<DisplayMode | undefined> =
    writable(undefined);
  private readonly _availableModes: Writable<DisplayMode[]> = writable([]);
  private readonly _styleVariables: Writable<StyleVariables> = writable({});
  private readonly _locale: Writable<string | undefined> = writable(undefined);
  private readonly _dimensions: Writable<ContainerDimensions | undefined> =
    writable(undefined);

  /** The element whose inline style receives `styles.variables`, if any. */
  private styleTarget: StyleTarget | undefined;
  /** CSS custom-property names currently applied — for clean removal. */
  private appliedVars = new Set<string>();
  /** The current full context, kept in sync with `_raw` for patching. */
  private current: HostContext = {};

  /** Reads the public, readable store surface. */
  get stores(): HostContextStores {
    return {
      raw: this._raw,
      theme: this._theme,
      displayMode: this._displayMode,
      availableDisplayModes: this._availableModes,
      styleVariables: this._styleVariables,
      locale: this._locale,
      containerDimensions: this._dimensions,
    };
  }

  /**
   * Sets the element that receives `styles.variables` as CSS custom
   * properties. Re-applies the current variables immediately.
   */
  bindStyleTarget(target: StyleTarget | undefined): void {
    if (this.styleTarget && this.styleTarget !== target) {
      this.clearStyleVars(this.styleTarget);
    }
    this.styleTarget = target;
    if (target) {
      this.applyStyleVars(target, this.current.styles?.variables ?? {});
    }
  }

  /** Replaces the full context — used for the `ui/initialize` result. */
  set(ctx: HostContext): void {
    this.current = { ...ctx };
    this.publish(this.current);
  }

  /**
   * Applies a `host-context-changed` patch — a shallow merge, since the host
   * sends `Partial<HostContext>` (brief 01 §2.4).
   */
  patch(patch: HostContextChangedParams): void {
    this.current = { ...this.current, ...patch };
    this.publish(this.current);
  }

  /** The current display mode without subscribing — used by negotiation. */
  get currentDisplayMode(): DisplayMode | undefined {
    return this.current.displayMode;
  }

  /** The currently grantable modes without subscribing. */
  get currentAvailableModes(): DisplayMode[] {
    return this.current.availableDisplayModes ?? [];
  }

  private publish(ctx: HostContext): void {
    this._raw.set(ctx);
    this._theme.set(ctx.theme);
    this._displayMode.set(ctx.displayMode);
    this._availableModes.set(ctx.availableDisplayModes ?? []);
    this._locale.set(ctx.locale);
    this._dimensions.set(ctx.containerDimensions);
    const vars = ctx.styles?.variables ?? {};
    this._styleVariables.set(vars);
    if (this.styleTarget) {
      this.applyStyleVars(this.styleTarget, vars);
    }
    this.applyHostFonts(ctx.styles?.css?.fonts);
  }

  /**
   * Injects the host's font CSS (`styles.css.fonts` — `@font-face`/`@import`
   * rules) into the View document so the host's fonts load (D-182, item D — the
   * `applyHostFonts` behaviour the ext-apps reference View performs). Managed in
   * a single marked `<style>` in `document.head`: updated in place when the CSS
   * changes, removed when the host sends no font CSS. No-op without a DOM.
   */
  private applyHostFonts(css: string | undefined): void {
    if (typeof document === 'undefined') return;
    const existing = document.head.querySelector(
      'style[data-dockyard-host-fonts]',
    );
    if (!css) {
      existing?.remove();
      return;
    }
    const el =
      (existing as HTMLStyleElement | null) ?? document.createElement('style');
    if (!existing) {
      el.setAttribute('data-dockyard-host-fonts', '');
      document.head.appendChild(el);
    }
    if (el.textContent !== css) {
      el.textContent = css;
    }
  }

  private applyStyleVars(target: StyleTarget, vars: StyleVariables): void {
    // Remove vars no longer present, then set the current set.
    for (const name of this.appliedVars) {
      if (!(name in vars)) {
        target.style.removeProperty(name);
      }
    }
    const next = new Set<string>();
    for (const [name, value] of Object.entries(vars)) {
      // Host variables are spec-standardized custom properties; ensure the
      // leading `--` so a bare key still lands as a custom property.
      const prop = name.startsWith('--') ? name : `--${name}`;
      target.style.setProperty(prop, value);
      next.add(prop);
    }
    this.appliedVars = next;
  }

  private clearStyleVars(target: StyleTarget): void {
    for (const name of this.appliedVars) {
      target.style.removeProperty(name);
    }
    this.appliedVars = new Set();
  }
}
