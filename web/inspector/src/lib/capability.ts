/**
 * capability.ts — the inspector's capability-set emulation model.
 *
 * The inspector's Host control (RFC §12, §7.5) renders an App as a host that
 * does or does NOT negotiate a given capability — Apps, Tasks, a display mode
 * — so a developer can exercise an App's graceful degradation.
 *
 * It is a CAPABILITY TOGGLE SET, never a hardcoded per-host capability matrix
 * (CLAUDE.md §6 / §13 — "do not hardcode a per-host capability matrix; it would
 * always drift"). There is no `ChatGPT supports X, Claude supports Y` table
 * here: the developer flips capability toggles, and the toggles drive the
 * `hostContext` + `hostCapabilities` the host-half bridge advertises in the
 * `ui/initialize` handshake. The named presets below are just convenient
 * starting toggle-sets — selecting one only flips toggles; it never consults
 * a matrix at handshake time.
 */

import type {
  DisplayMode,
  HostCapabilities,
  HostContext,
} from 'dockyard-bridge';
import { defaultHostContext } from '../host/host-bridge.js';

/** The display modes the inspector can emulate granting. */
export const ALL_DISPLAY_MODES: DisplayMode[] = ['inline', 'fullscreen', 'pip'];

/**
 * The capability toggle set. Each field is a single boolean/value the
 * developer flips; the set is the complete emulated-host capability state.
 * NOT a per-host matrix — a `CapabilitySet` describes capabilities directly.
 */
export interface CapabilitySet {
  /** Whether the emulated host negotiates the MCP Apps extension at all. */
  apps: boolean;
  /** Whether the emulated host negotiates the MCP Tasks extension. */
  tasks: boolean;
  /** The display modes the emulated host will grant. */
  displayModes: DisplayMode[];
  /** The display mode the emulated host starts in. */
  displayMode: DisplayMode;
}

/** The inspector's default emulated host — a fully capable host. */
export function fullCapabilitySet(): CapabilitySet {
  return {
    apps: true,
    tasks: true,
    displayModes: [...ALL_DISPLAY_MODES],
    displayMode: 'inline',
  };
}

/**
 * Named starting toggle-sets — convenience, not a matrix. Picking a preset
 * only seeds the toggles; the developer then flips individual toggles, and the
 * handshake is driven by the resulting {@link CapabilitySet}, never the preset
 * name. Adding or removing a preset changes no negotiation logic.
 */
export const CAPABILITY_PRESETS: Record<string, CapabilitySet> = {
  'Fully capable': fullCapabilitySet(),
  'Apps only (no Tasks)': {
    apps: true,
    tasks: false,
    displayModes: [...ALL_DISPLAY_MODES],
    displayMode: 'inline',
  },
  'Inline only': {
    apps: true,
    tasks: true,
    displayModes: ['inline'],
    displayMode: 'inline',
  },
  'No Apps extension': {
    apps: false,
    tasks: false,
    displayModes: ['inline'],
    displayMode: 'inline',
  },
};

/**
 * Derives the `hostContext` the host-half bridge supplies in the
 * `ui/initialize` handshake from a capability set. When `apps` is off, the
 * host grants no display modes — an App's display-mode requests are all
 * refused, and it must degrade to a non-App fallback.
 */
export function hostContextFor(set: CapabilitySet): HostContext {
  const base = defaultHostContext();
  if (!set.apps) {
    return {
      ...base,
      availableDisplayModes: [],
      displayMode: 'inline',
    };
  }
  const inlineOnly: DisplayMode[] = ['inline'];
  const modes: DisplayMode[] =
    set.displayModes.length > 0 ? set.displayModes : inlineOnly;
  return {
    ...base,
    availableDisplayModes: modes,
    displayMode: modes.includes(set.displayMode) ? set.displayMode : modes[0],
  };
}

/**
 * Derives the `hostCapabilities` block the host advertises. The Apps and Tasks
 * toggles surface as explicit capability flags; an App reads them from the
 * handshake result and degrades on absence (capability-driven, never a matrix).
 *
 * `apps`/`tasks` are **Dockyard-private emulation flags**, not keys in the
 * `McpUiHostCapabilities` schema (which a strict `.parse()` would strip). They
 * exist so the inspector can emulate a host with Apps/Tasks toggled on or off;
 * `hostCapabilities` is forwarded to the View as an opaque record, so the flags
 * reach a Dockyard App but carry no meaning to a stock host. (D-182 audit.)
 */
export function hostCapabilitiesFor(set: CapabilitySet): HostCapabilities {
  return {
    apps: set.apps,
    tasks: set.tasks,
  };
}

/** A one-line human summary of a capability set, for the Host control chip. */
export function summarize(set: CapabilitySet): string {
  const parts: string[] = [];
  parts.push(set.apps ? 'Apps' : 'no Apps');
  parts.push(set.tasks ? 'Tasks' : 'no Tasks');
  parts.push(`${set.displayModes.length} display mode(s)`);
  return parts.join(' · ');
}

/** True when two capability sets are value-equal — used to dirty-check. */
export function capabilitySetsEqual(a: CapabilitySet, b: CapabilitySet): boolean {
  return (
    a.apps === b.apps &&
    a.tasks === b.tasks &&
    a.displayMode === b.displayMode &&
    a.displayModes.length === b.displayModes.length &&
    a.displayModes.every((m) => b.displayModes.includes(m))
  );
}
