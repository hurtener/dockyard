<script lang="ts">
  /**
   * HostControl — the inspector PageHeader's Host capability-set control.
   *
   * Capability-set emulation (RFC §12, §7.5): a set of capability TOGGLES —
   * Apps on/off, Tasks on/off, which display modes the host grants — that the
   * developer flips to render an App as a host that does or does not negotiate
   * a capability, exercising graceful degradation.
   *
   * It is a capability toggle set, NEVER a hardcoded per-host capability matrix
   * (CLAUDE.md §6 / §13). The named presets are convenience starting points
   * that only seed the toggles; the handshake is driven by the resulting
   * `CapabilitySet`, never a host name. Composes only `@dockyard/ui`.
   */
  import { StatusChip } from '@dockyard/ui';
  import {
    ALL_DISPLAY_MODES,
    CAPABILITY_PRESETS,
    summarize,
    type CapabilitySet,
  } from './capability.js';
  import type { DisplayMode } from '@dockyard/bridge';

  interface Props {
    /** The current emulated capability set. */
    capabilities: CapabilitySet;
    /** Called when any toggle changes — re-runs the App handshake. */
    onChange: (next: CapabilitySet) => void;
  }

  let { capabilities, onChange }: Props = $props();

  let open = $state(false);

  function toggleApps(): void {
    onChange({ ...capabilities, apps: !capabilities.apps });
  }
  function toggleTasks(): void {
    onChange({ ...capabilities, tasks: !capabilities.tasks });
  }
  function toggleMode(mode: DisplayMode): void {
    const has = capabilities.displayModes.includes(mode);
    const next = has
      ? capabilities.displayModes.filter((m) => m !== mode)
      : [...capabilities.displayModes, mode];
    onChange({
      ...capabilities,
      displayModes: next,
      displayMode: next.includes(capabilities.displayMode)
        ? capabilities.displayMode
        : (next[0] ?? 'inline'),
    });
  }
  function applyPreset(name: string): void {
    const preset = CAPABILITY_PRESETS[name];
    if (preset) onChange({ ...preset, displayModes: [...preset.displayModes] });
  }
</script>

<div class="host-control" data-testid="host-control">
  <button
    type="button"
    class="host-trigger"
    aria-expanded={open}
    data-testid="host-trigger"
    onclick={() => (open = !open)}
  >
    Host: {summarize(capabilities)} ▾
  </button>

  {#if open}
    <div class="host-menu" data-testid="host-menu">
      <fieldset class="toggles">
        <legend>Capabilities</legend>
        <label>
          <input
            type="checkbox"
            checked={capabilities.apps}
            data-testid="toggle-apps"
            onchange={toggleApps}
          />
          Negotiate Apps extension
        </label>
        <label>
          <input
            type="checkbox"
            checked={capabilities.tasks}
            data-testid="toggle-tasks"
            onchange={toggleTasks}
          />
          Negotiate Tasks extension
        </label>
      </fieldset>

      <fieldset class="toggles">
        <legend>Display modes granted</legend>
        {#each ALL_DISPLAY_MODES as mode (mode)}
          <label>
            <input
              type="checkbox"
              checked={capabilities.displayModes.includes(mode)}
              data-testid={`toggle-mode-${mode}`}
              disabled={!capabilities.apps}
              onchange={() => toggleMode(mode)}
            />
            {mode}
          </label>
        {/each}
      </fieldset>

      <div class="presets">
        <span class="presets-label">Start from</span>
        {#each Object.keys(CAPABILITY_PRESETS) as name (name)}
          <button
            type="button"
            class="preset"
            data-testid={`preset-${name}`}
            onclick={() => applyPreset(name)}
          >
            {name}
          </button>
        {/each}
      </div>

      <div class="status">
        <StatusChip
          label={capabilities.apps ? 'Apps on' : 'Apps off'}
          tone={capabilities.apps ? 'ok' : 'warn'}
        />
        <StatusChip
          label={capabilities.tasks ? 'Tasks on' : 'Tasks off'}
          tone={capabilities.tasks ? 'ok' : 'warn'}
        />
      </div>
    </div>
  {/if}
</div>

<style>
  .host-control {
    position: relative;
  }
  .host-trigger {
    padding: var(--dy-space-1, 0.25rem) var(--dy-space-2, 0.5rem);
    border: 1px solid var(--dy-color-border, #d4d4d8);
    border-radius: var(--dy-radius-sm, 0.25rem);
    background: var(--dy-color-surface, #ffffff);
    cursor: pointer;
    font-size: var(--dy-font-size-sm, 0.875rem);
  }
  .host-menu {
    position: absolute;
    right: 0;
    top: calc(100% + var(--dy-space-1, 0.25rem));
    z-index: 10;
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3, 0.75rem);
    min-width: 16rem;
    padding: var(--dy-space-3, 0.75rem);
    border: 1px solid var(--dy-color-border, #d4d4d8);
    border-radius: var(--dy-radius-md, 0.5rem);
    background: var(--dy-color-surface, #ffffff);
    box-shadow: var(--dy-elevation-raised, 0 4px 12px rgba(0, 0, 0, 0.1));
  }
  .toggles {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1, 0.25rem);
    border: none;
    margin: 0;
    padding: 0;
    font-size: var(--dy-font-size-sm, 0.875rem);
  }
  .toggles legend {
    font-weight: 600;
    padding: 0;
    margin-bottom: var(--dy-space-1, 0.25rem);
  }
  .presets {
    display: flex;
    flex-wrap: wrap;
    gap: var(--dy-space-1, 0.25rem);
    align-items: center;
  }
  .presets-label {
    font-size: var(--dy-font-size-xs, 0.75rem);
    color: var(--dy-color-text-muted, #71717a);
  }
  .preset {
    padding: var(--dy-space-1, 0.25rem) var(--dy-space-2, 0.5rem);
    border: 1px solid var(--dy-color-border, #d4d4d8);
    border-radius: var(--dy-radius-sm, 0.25rem);
    background: var(--dy-color-bg, #fafafa);
    cursor: pointer;
    font-size: var(--dy-font-size-xs, 0.75rem);
  }
  .status {
    display: flex;
    gap: var(--dy-space-1, 0.25rem);
  }
</style>
