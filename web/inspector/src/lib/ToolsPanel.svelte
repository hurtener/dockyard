<script lang="ts">
  /**
   * ToolsPanel — the inspector DetailRail's Tools/Resources tab.
   *
   * Phase 23 listed the attached server's tools and offered fixture-driven
   * preview. Phase 24-finish (D-131) extends the panel: an operator picks a
   * tool, fills a form generated from the tool's input JSON Schema (P1 —
   * `schema-form.ts`), presses Invoke, and the inspector POSTs to its own
   * backend's `/api/tools/invoke`. The structured result flows through the
   * same `pushToolResult` path the Fixtures switcher uses (D-129) so the App
   * preview re-renders with the operator's parameters. A transport failure is
   * surfaced through the shared `ErrorState` (CLAUDE.md §20 four-state rule).
   *
   * D-131 extends D-099 + D-103: the inspector additionally issues real
   * `tools/call` to the attached server when an operator initiates it through
   * the UI. Still within P4 — the inspector is the lone client-shaped
   * component, dev-mode-gated, localhost-bound; the listener's loopback gate
   * keeps the endpoint dev-only.
   */
  import {
    DataTable,
    ErrorState,
    JsonInspector,
    StatusChip,
    type Column,
    type PageStateValue,
    type Row,
  } from '@dockyard/ui';
  import type { ToolContract } from './contracts.js';
  import {
    fieldsFromSchema,
    initialValues,
    parseFormValues,
    type FormField,
    type FormValues,
  } from './schema-form.js';
  import { invokeTool, type ToolInvokeResult } from './api.js';

  interface Props {
    /** The attached server's generated tool contracts. */
    contracts: ToolContract[];
    /** The fetch state — drives the four-state PageState of the contracts table. */
    panelState: PageStateValue;
    /** Called when the user retries a failed contracts fetch. */
    onRetry?: () => void;
    /**
     * Called with a successful tool result — the inspector hands the typed
     * `structuredContent` back to App.svelte so it can re-render the App
     * preview through the same `pushToolResult` pathway the Fixtures switcher
     * uses (D-129).
     */
    onInvokeResult?: (result: ToolInvokeResult, contract: ToolContract) => void;
    /** The API base URL — empty in production (same-origin). Set in tests. */
    base?: string;
    /** Fetch impl override — tests pass a mock. */
    fetchImpl?: typeof fetch;
  }

  let {
    contracts,
    panelState,
    onRetry,
    onInvokeResult,
    base = '',
    fetchImpl = fetch,
  }: Props = $props();

  const columns: Column[] = [
    { key: 'name', label: 'Tool', sortable: true },
    { key: 'description', label: 'Description' },
    { key: 'output', label: 'Output' },
  ];

  const rows = $derived<Row[]>(
    contracts.map((c) => ({
      name: c.name,
      description: c.description ?? '—',
      output: c.outputSchema?.type ?? 'unknown',
    })),
  );

  // -- selection + form state --
  let selectedToolName = $state<string>('');
  const selectedContract = $derived<ToolContract | undefined>(
    contracts.find((c) => c.name === selectedToolName),
  );
  const fields = $derived<FormField[]>(
    selectedContract ? fieldsFromSchema(selectedContract.inputSchema) : [],
  );
  let values = $state<FormValues>({});
  let errors = $state<Record<string, string>>({});

  // -- invoke flight state --
  let invoking = $state(false);
  let invokeError = $state<string>('');
  let lastResult = $state<ToolInvokeResult | undefined>(undefined);

  // onRowClick is the row-pick handler the DataTable invokes. Selecting a
  // tool resets the form synchronously — we do NOT use a $effect for this
  // reset because the form schema reactively recomputes from selectedContract
  // and a write-from-effect-into-state created an infinite reactive cycle
  // (the iframe's tools/call response loops events back through the
  // host-bridge → the parent re-renders → the effect re-fires → effect
  // update depth exceeded). A click handler is the natural seam: the user
  // chose a new tool exactly once, reset the form exactly once.
  function onRowClick(row: Row): void {
    const name = String(row.name);
    if (name === selectedToolName) return;
    selectedToolName = name;
    const next = contracts.find((c) => c.name === name);
    const nextFields = next ? fieldsFromSchema(next.inputSchema) : [];
    values = initialValues(nextFields);
    errors = {};
    invokeError = '';
    lastResult = undefined;
    // The App preview's "tool already pushed" tag — onInvokeResult was a
    // result-feed callback, not a state. Clearing the inspector-side cached
    // result is a parent concern; the parent's applyInvokeResult is keyed on
    // the contract, so a tool switch is reflected when the next invoke runs.
  }

  async function onInvoke(): Promise<void> {
    if (!selectedContract) return;
    const parsed = parseFormValues(fields, values);
    if (Object.keys(parsed.errors).length > 0) {
      errors = parsed.errors;
      return;
    }
    errors = {};
    invokeError = '';
    invoking = true;
    try {
      const result = await invokeTool(
        { tool: selectedContract.name, arguments: parsed.arguments },
        base,
        fetchImpl,
      );
      lastResult = result;
      // Feed the App-frame preview the same way the Fixtures switcher does
      // (D-129). The parent threads `structuredContent` through to the
      // AppFrame's `pushToolResult` prop.
      onInvokeResult?.(result, selectedContract);
    } catch (err) {
      invokeError = err instanceof Error ? err.message : 'invoke failed';
    } finally {
      invoking = false;
    }
  }
</script>

<div class="tools-panel" data-testid="tools-panel">
  <DataTable
    {columns}
    {rows}
    pageState={panelState}
    {onRowClick}
    {onRetry}
    emptyTitle="No tools"
    emptyDescription="The attached server registered no tools. Add a typed Go tool handler and regenerate."
  />

  {#if selectedContract}
    <section class="invoke" data-testid="invoke-form">
      <header class="invoke-head">
        <h3>Invoke <code>{selectedContract.name}</code></h3>
        {#if selectedContract.description}
          <p class="invoke-desc">{selectedContract.description}</p>
        {/if}
      </header>

      <form
        class="invoke-form"
        onsubmit={(e) => {
          e.preventDefault();
          void onInvoke();
        }}
      >
        {#each fields as field (field.name)}
          <label class="field">
            <span class="field-label">
              {field.label}
              {#if field.required}
                <span class="req" aria-label="required">*</span>
              {/if}
            </span>
            {#if field.description}
              <span class="field-desc">{field.description}</span>
            {/if}

            {#if field.kind === 'boolean'}
              <input
                type="checkbox"
                checked={values[field.name] === true}
                data-testid={`invoke-${field.name}`}
                onchange={(e) =>
                  (values[field.name] = (e.target as HTMLInputElement).checked)}
              />
            {:else if field.kind === 'enum'}
              <select
                value={String(values[field.name] ?? '')}
                data-testid={`invoke-${field.name}`}
                onchange={(e) =>
                  (values[field.name] = (e.target as HTMLSelectElement).value)}
              >
                <option value="">— select —</option>
                {#each field.choices ?? [] as c (c.value)}
                  <option value={c.value}>{c.label}</option>
                {/each}
              </select>
            {:else if field.kind === 'multiline' || field.kind === 'json' || (field.kind === 'array')}
              <textarea
                rows={field.kind === 'array' ? 2 : 4}
                value={String(values[field.name] ?? '')}
                placeholder={field.kind === 'json'
                  ? '{ … } or a JSON value'
                  : field.kind === 'array'
                  ? 'comma- or newline-separated items'
                  : ''}
                data-testid={`invoke-${field.name}`}
                oninput={(e) =>
                  (values[field.name] = (e.target as HTMLTextAreaElement).value)}
              ></textarea>
            {:else}
              <input
                type={field.kind === 'integer' || field.kind === 'number' ? 'number' : 'text'}
                step={field.kind === 'integer' ? '1' : 'any'}
                value={String(values[field.name] ?? '')}
                data-testid={`invoke-${field.name}`}
                oninput={(e) =>
                  (values[field.name] = (e.target as HTMLInputElement).value)}
              />
            {/if}

            {#if errors[field.name]}
              <span class="field-error" data-testid={`invoke-${field.name}-error`}>
                {errors[field.name]}
              </span>
            {/if}
          </label>
        {/each}

        <div class="actions">
          <button
            type="submit"
            class="invoke-btn"
            disabled={invoking}
            data-testid="invoke-submit"
          >
            {invoking ? 'Invoking…' : 'Invoke'}
          </button>
        </div>
      </form>

      {#if invokeError}
        <div class="invoke-error-region" data-testid="invoke-error-region">
          <ErrorState
            title="Invocation failed"
            description={invokeError}
            retryLabel="Retry"
            onretry={() => void onInvoke()}
          />
        </div>
      {:else if lastResult}
        <div class="invoke-result" data-testid="invoke-result">
          <div class="result-head">
            <StatusChip
              label={lastResult.isError ? 'tool error' : 'ok'}
              tone={lastResult.isError ? 'error' : 'ok'}
              dot
            />
            <span class="result-note">
              {lastResult.isError
                ? 'The tool returned isError=true — the App preview rendered the error state.'
                : 'The App preview was re-rendered with the operator parameters.'}
            </span>
          </div>
          {#if lastResult.structuredContent}
            <JsonInspector
              value={lastResult.structuredContent}
              name="structuredContent"
            />
          {/if}
        </div>
      {/if}
    </section>
  {:else}
    <p class="tools-note">
      Pick a tool to render an invocation form generated from its input
      schema. The inspector calls <code>tools/call</code> against the attached
      server when you press Invoke — the result re-renders the App preview
      (D-131; localhost-only, operator-driven).
    </p>
  {/if}
</div>

<style>
  .tools-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    min-height: 0;
  }
  .tools-note {
    margin: 0;
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-soft);
  }
  .invoke {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    padding: var(--dy-space-3);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
  }
  .invoke-head h3 {
    margin: 0;
    font-size: var(--dy-text-md);
    color: var(--dy-color-ink);
  }
  .invoke-head code {
    background: var(--dy-color-surface-muted);
    padding: 0 var(--dy-space-1);
    border-radius: var(--dy-radius-sm);
  }
  .invoke-desc {
    margin: var(--dy-space-1) 0 0 0;
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }
  .invoke-form {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1);
  }
  .field-label {
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink);
    font-weight: 500;
  }
  .field-desc {
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-soft);
  }
  .field-error {
    font-size: var(--dy-text-xs);
    color: var(--dy-state-error-fg);
  }
  .req {
    color: var(--dy-state-error-fg);
    margin-left: var(--dy-space-1);
  }
  .field input[type='text'],
  .field input[type='number'],
  .field textarea,
  .field select {
    font: inherit;
    padding: var(--dy-space-1) var(--dy-space-2);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  .invoke-btn {
    padding: var(--dy-space-2) var(--dy-space-4);
    border: 1px solid var(--dy-color-accent);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-accent);
    color: var(--dy-color-surface);
    cursor: pointer;
    font-weight: 600;
  }
  .invoke-btn:disabled {
    opacity: 0.6;
    cursor: progress;
  }
  .invoke-result,
  .invoke-error-region {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
  }
  .result-head {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
  }
  .result-note {
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-soft);
  }
</style>
