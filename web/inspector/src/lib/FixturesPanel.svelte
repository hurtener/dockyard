<script lang="ts">
  /**
   * FixturesPanel — the inspector DetailRail's Fixtures tab.
   *
   * The fixture switcher (RFC §12): a `happy` / `empty` / `error` /
   * `permission` / `slow` / `large` selector wired to the attached server's
   * generated tool contracts. Selecting a fixture builds synthetic
   * `structuredContent` FROM the contract (P1 — contract-first; `fixtures.ts`)
   * and hands it up so the App's `tools/call` is answered from the fixture —
   * UI states exercised without a backend.
   *
   * Routes through the four-state `PageState`; composes only `@dockyard/ui`.
   */
  import {
    PageState,
    StatusChip,
    JsonInspector,
    type PageStateValue,
  } from '@dockyard/ui';
  import type { ToolContract } from './contracts.js';
  import {
    FIXTURE_KINDS,
    FIXTURE_META,
    buildFixtures,
    type Fixture,
    type FixtureKind,
  } from './fixtures.js';

  interface Props {
    /** The attached server's generated tool contracts. */
    contracts: ToolContract[];
    /** The fetch state — drives the four-state PageState. */
    panelState: PageStateValue;
    /** Called when the user retries a failed contracts fetch. */
    onRetry?: () => void;
    /** Called when a fixture is applied — feeds the App synthetic content. */
    onApply?: (fixture: Fixture, contract: ToolContract) => void;
  }

  let { contracts, panelState, onRetry, onApply }: Props = $props();

  let selectedTool = $state(0);
  let selectedKind = $state<FixtureKind>('happy');

  const contract = $derived(contracts[selectedTool]);
  const fixtures = $derived(
    contract ? buildFixtures(contract) : undefined,
  );
  const active = $derived(fixtures ? fixtures[selectedKind] : undefined);

  function apply(): void {
    if (active && contract) onApply?.(active, contract);
  }

  // Re-apply automatically when the selection changes so the preview tracks
  // the switcher — the fixture *drives* the App's UI state (the acceptance
  // criterion).
  $effect(() => {
    if (active && contract) onApply?.(active, contract);
  });
</script>

<div class="fixtures-panel" data-testid="fixtures-panel">
  <PageState
    state={panelState}
    emptyTitle="No contracts"
    emptyDescription="The attached server registered no generated tool contracts — the fixture switcher derives fixtures from contracts."
    errorTitle="Contracts unavailable"
    errorDescription="The inspector could not load the server's generated contracts. Retry."
    onRetry={onRetry}
  >
    {#if contracts.length > 0}
      <label class="field">
        <span class="field-label">Tool</span>
        <select bind:value={selectedTool} data-testid="fixture-tool-select">
          {#each contracts as c, i (c.name)}
            <option value={i}>{c.name}</option>
          {/each}
        </select>
      </label>

      <div class="fixture-grid" role="radiogroup" aria-label="Fixture">
        {#each FIXTURE_KINDS as kind (kind)}
          <button
            type="button"
            class="fixture-chip"
            class:selected={kind === selectedKind}
            data-testid={`fixture-${kind}`}
            aria-pressed={kind === selectedKind}
            onclick={() => (selectedKind = kind)}
          >
            <span class="fixture-name">{FIXTURE_META[kind].label}</span>
            <span class="fixture-desc">{FIXTURE_META[kind].description}</span>
          </button>
        {/each}
      </div>

      {#if active}
        <div class="fixture-preview" data-testid="fixture-preview">
          <div class="preview-head">
            <StatusChip
              label={active.kind}
              tone={active.isError ? 'error' : 'ok'}
              dot
            />
            <button type="button" class="apply" onclick={apply}>
              Apply to App
            </button>
          </div>
          {#if active.isError}
            <p class="preview-error" data-testid="fixture-error">
              {active.error?.message}
            </p>
          {:else}
            <JsonInspector
              value={active.structuredContent}
              name="structuredContent"
            />
          {/if}
        </div>
      {/if}
    {/if}
  </PageState>
</div>

<style>
  .fixtures-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    min-height: 0;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1);
  }
  .field-label {
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }
  .fixture-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--dy-space-2);
  }
  .fixture-chip {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1);
    padding: var(--dy-space-2);
    text-align: left;
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
    cursor: pointer;
  }
  .fixture-chip.selected {
    border-color: var(--dy-color-accent);
    box-shadow: 0 0 0 1px var(--dy-color-accent);
  }
  .fixture-name {
    font-weight: 600;
    font-size: var(--dy-text-sm);
  }
  .fixture-desc {
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-soft);
  }
  .fixture-preview {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
  }
  .preview-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .apply {
    padding: var(--dy-space-1) var(--dy-space-3);
    border: 1px solid var(--dy-color-accent);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-accent);
    color: var(--dy-color-surface);
    cursor: pointer;
    font-size: var(--dy-text-sm);
  }
  .preview-error {
    margin: 0;
    font-size: var(--dy-text-sm);
    color: var(--dy-state-error-fg);
  }
</style>
