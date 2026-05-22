/**
 * capability.test.ts — the capability-set emulation model.
 *
 * The binding assertion: capability emulation is a capability TOGGLE SET, not
 * a hardcoded per-host matrix (CLAUDE.md §6 / §13). Flipping a toggle changes
 * the `hostContext` / `hostCapabilities` the handshake advertises.
 */
import { describe, expect, it } from 'vitest';
import {
  ALL_DISPLAY_MODES,
  CAPABILITY_PRESETS,
  fullCapabilitySet,
  hostContextFor,
  hostCapabilitiesFor,
  summarize,
  capabilitySetsEqual,
} from '../lib/capability.js';

describe('fullCapabilitySet', () => {
  it('emulates a fully capable host', () => {
    const set = fullCapabilitySet();
    expect(set.apps).toBe(true);
    expect(set.tasks).toBe(true);
    expect(set.displayModes).toEqual(ALL_DISPLAY_MODES);
  });
});

describe('hostContextFor', () => {
  it('grants the toggled-on display modes', () => {
    const ctx = hostContextFor({
      apps: true,
      tasks: true,
      displayModes: ['inline', 'pip'],
      displayMode: 'inline',
    });
    expect(ctx.availableDisplayModes).toEqual(['inline', 'pip']);
    expect(ctx.displayMode).toBe('inline');
  });

  it('grants NO display modes when Apps is toggled off — the App must degrade', () => {
    const ctx = hostContextFor({
      apps: false,
      tasks: false,
      displayModes: ['inline'],
      displayMode: 'inline',
    });
    expect(ctx.availableDisplayModes).toEqual([]);
  });

  it('falls back when the start mode is not in the granted set', () => {
    const ctx = hostContextFor({
      apps: true,
      tasks: true,
      displayModes: ['fullscreen'],
      displayMode: 'inline',
    });
    expect(ctx.displayMode).toBe('fullscreen');
  });
});

describe('hostCapabilitiesFor', () => {
  it('surfaces the Apps and Tasks toggles as capability flags', () => {
    expect(hostCapabilitiesFor({
      apps: true,
      tasks: false,
      displayModes: ['inline'],
      displayMode: 'inline',
    })).toEqual({ apps: true, tasks: false });
  });

  it('a Tasks-off toggle is reflected — capability-driven degradation', () => {
    const caps = hostCapabilitiesFor({
      apps: true,
      tasks: false,
      displayModes: ['inline'],
      displayMode: 'inline',
    });
    expect(caps.tasks).toBe(false);
  });
});

describe('presets are starting toggle-sets, not a matrix', () => {
  it('every preset is a plain CapabilitySet of boolean/value toggles', () => {
    for (const [, set] of Object.entries(CAPABILITY_PRESETS)) {
      expect(typeof set.apps).toBe('boolean');
      expect(typeof set.tasks).toBe('boolean');
      expect(Array.isArray(set.displayModes)).toBe(true);
    }
  });

  it('the "No Apps extension" preset degrades a fully-capable host', () => {
    const set = CAPABILITY_PRESETS['No Apps extension'];
    expect(set.apps).toBe(false);
    expect(hostContextFor(set).availableDisplayModes).toEqual([]);
  });
});

describe('summarize / capabilitySetsEqual', () => {
  it('summarizes a set for the Host control chip', () => {
    expect(summarize(fullCapabilitySet())).toContain('Apps');
    expect(summarize({
      apps: false,
      tasks: false,
      displayModes: [],
      displayMode: 'inline',
    })).toContain('no Apps');
  });

  it('value-compares two sets', () => {
    expect(capabilitySetsEqual(fullCapabilitySet(), fullCapabilitySet())).toBe(true);
    expect(capabilitySetsEqual(fullCapabilitySet(), {
      ...fullCapabilitySet(),
      apps: false,
    })).toBe(false);
  });
});
