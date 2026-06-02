/**
 * host-context.test.ts — the hostContext stores and the application of
 * `styles.variables` as CSS custom properties on a style target.
 */
import { describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { HostContextState, type StyleTarget } from '../host-context.js';

/** A minimal in-test style target recording set/removed custom properties. */
function makeStyleTarget(): StyleTarget & { props: Map<string, string> } {
  const props = new Map<string, string>();
  return {
    props,
    style: {
      setProperty: (name, value) => props.set(name, value),
      removeProperty: (name) => props.delete(name),
    },
  };
}

describe('HostContextState', () => {
  it('publishes the initialize result into the stores', () => {
    const state = new HostContextState();
    state.set({
      theme: 'dark',
      displayMode: 'fullscreen',
      availableDisplayModes: ['inline', 'fullscreen'],
      locale: 'de-DE',
      containerDimensions: { width: 400, height: 300 },
    });

    expect(get(state.stores.theme)).toBe('dark');
    expect(get(state.stores.displayMode)).toBe('fullscreen');
    expect(get(state.stores.availableDisplayModes)).toEqual([
      'inline',
      'fullscreen',
    ]);
    expect(get(state.stores.locale)).toBe('de-DE');
    expect(get(state.stores.containerDimensions)).toEqual({
      width: 400,
      height: 300,
    });
  });

  it('patch() shallow-merges and leaves unrelated fields intact', () => {
    const state = new HostContextState();
    state.set({ theme: 'light', locale: 'en-US' });
    state.patch({ theme: 'dark' });

    expect(get(state.stores.theme)).toBe('dark');
    expect(get(state.stores.locale)).toBe('en-US');
  });

  it('applies styles.variables as CSS custom properties on the target', () => {
    const target = makeStyleTarget();
    const state = new HostContextState();
    state.bindStyleTarget(target);
    state.set({
      styles: { variables: { '--color-bg': '#000', 'font-sans': 'Inter' } },
    });

    expect(target.props.get('--color-bg')).toBe('#000');
    // A bare key is promoted to a custom property.
    expect(target.props.get('--font-sans')).toBe('Inter');
  });

  it('removes variables no longer present on a later patch', () => {
    const target = makeStyleTarget();
    const state = new HostContextState();
    state.bindStyleTarget(target);
    state.set({ styles: { variables: { '--a': '1', '--b': '2' } } });
    state.patch({ styles: { variables: { '--a': '9' } } });

    expect(target.props.get('--a')).toBe('9');
    expect(target.props.has('--b')).toBe(false);
  });

  it('rebinding the style target clears variables off the old target', () => {
    const first = makeStyleTarget();
    const second = makeStyleTarget();
    const state = new HostContextState();
    state.bindStyleTarget(first);
    state.set({ styles: { variables: { '--c': '3' } } });
    expect(first.props.get('--c')).toBe('3');

    state.bindStyleTarget(second);
    expect(first.props.has('--c')).toBe(false);
    expect(second.props.get('--c')).toBe('3');
  });

  it('reflects flexible containerDimensions (maxWidth/maxHeight) — item C', () => {
    const state = new HostContextState();
    state.set({ containerDimensions: { maxHeight: 600, maxWidth: 800 } });
    expect(get(state.stores.containerDimensions)).toEqual({
      maxHeight: 600,
      maxWidth: 800,
    });
    // A later fixed-size patch is reflected without collapsing to undefined.
    state.patch({ containerDimensions: { width: 320, height: 240 } });
    expect(get(state.stores.containerDimensions)).toEqual({
      width: 320,
      height: 240,
    });
  });

  it('injects host font CSS (styles.css.fonts) into document.head and removes it — item D', () => {
    const state = new HostContextState();
    const sel = 'style[data-dockyard-host-fonts]';
    expect(document.head.querySelector(sel)).toBeNull();

    const fontCss = "@font-face{font-family:'Acme';src:url(/acme.woff2)}";
    state.set({ styles: { css: { fonts: fontCss } } });
    const el = document.head.querySelector(sel);
    expect(el).not.toBeNull();
    expect(el!.textContent).toBe(fontCss);

    // A later context with no font CSS removes the injected style.
    state.patch({ styles: {} });
    expect(document.head.querySelector(sel)).toBeNull();
  });

  it('exposes the current display mode and modes without subscribing', () => {
    const state = new HostContextState();
    state.set({
      displayMode: 'pip',
      availableDisplayModes: ['inline', 'pip'],
    });
    expect(state.currentDisplayMode).toBe('pip');
    expect(state.currentAvailableModes).toEqual(['inline', 'pip']);
  });
});
