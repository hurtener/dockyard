<script lang="ts">
  /**
   * PromptsPanel — the inspector DetailRail's Prompts tab (v1.1 Wave A;
   * closes D-151).
   *
   * Lists the attached server's MCP Prompts, lets an operator pick one, fills
   * its flat string-keyed arguments (D-152 — prompts carry string-only
   * arguments, NOT a structured JSON object), and POSTs to the inspector
   * backend's `/api/prompts/get`. The rendered messages flow into a
   * dedicated message view; a server-side prompts/get error surfaces in
   * the shared `ErrorState` region (CLAUDE.md §20 four-state rule).
   *
   * D-163 extends D-103 + D-131 + D-134 to a third operator-initiated
   * client-shaped surface: same posture (lone client-shaped component,
   * dev-mode-gated, localhost-bound; the listener's loopback gate keeps
   * the endpoint dev-only); same short-lived-per-request pattern (one
   * fresh MCP client session per Invoke click).
   */
  import {
    ActionBar,
    DataTable,
    ErrorState,
    StatusChip,
    type Column,
    type PageStateValue,
    type Row,
  } from 'dockyard-ui';
  import type {
    PromptInfo,
    PromptArgumentInfo,
    PromptGetResponse,
  } from './prompts.js';
  import { invokePrompt } from './api.js';

  interface Props {
    /** The attached server's registered prompts. */
    prompts: PromptInfo[];
    /** Drives the four-state PageState of the prompts table. */
    panelState: PageStateValue;
    /** Called when the user retries a failed prompts fetch. */
    onRetry?: () => void;
    /** API base URL — empty in production (same-origin). Set in tests. */
    base?: string;
    /** Fetch impl override — tests pass a mock. */
    fetchImpl?: typeof fetch;
  }

  let {
    prompts,
    panelState,
    onRetry,
    base = '',
    fetchImpl = fetch,
  }: Props = $props();

  const columns: Column[] = [
    { key: 'name', label: 'Prompt', sortable: true },
    { key: 'title', label: 'Title' },
    { key: 'argsLabel', label: 'Arguments' },
  ];

  const rows = $derived<Row[]>(
    prompts.map((p) => ({
      name: p.name,
      title: p.title ?? '—',
      argsLabel: argSummary(p.arguments),
    })),
  );

  function argSummary(args: PromptArgumentInfo[]): string {
    if (args.length === 0) return '—';
    const required = args.filter((a) => a.required).map((a) => a.name);
    const optional = args.filter((a) => !a.required).map((a) => a.name);
    const parts: string[] = [];
    if (required.length > 0) parts.push(required.join(', '));
    if (optional.length > 0) parts.push('[' + optional.join(', ') + ']');
    return parts.join(' ');
  }

  // -- selection + form state --
  let selectedPromptName = $state<string>('');
  const selectedPrompt = $derived<PromptInfo | undefined>(
    prompts.find((p) => p.name === selectedPromptName),
  );

  // Argument values; keyed by argument.name. Always strings (D-152).
  let values = $state<Record<string, string>>({});
  let errors = $state<Record<string, string>>({});

  // -- invoke flight state --
  let invoking = $state(false);
  let invokeError = $state<string>('');
  let lastResult = $state<PromptGetResponse | undefined>(undefined);

  // onRowClick is the row-pick handler the DataTable invokes. Selecting a
  // prompt resets the form synchronously — mirrors the ToolsPanel pattern
  // (a write-from-effect-into-state would create a reactive cycle).
  function onRowClick(row: Row): void {
    const name = String(row.name);
    if (name === selectedPromptName) return;
    selectedPromptName = name;
    const next = prompts.find((p) => p.name === name);
    values = initialValues(next?.arguments ?? []);
    errors = {};
    invokeError = '';
    lastResult = undefined;
  }

  function initialValues(args: PromptArgumentInfo[]): Record<string, string> {
    const out: Record<string, string> = {};
    for (const a of args) out[a.name] = '';
    return out;
  }

  function validateRequired(args: PromptArgumentInfo[]): Record<string, string> {
    const errs: Record<string, string> = {};
    for (const a of args) {
      if (a.required && (values[a.name] === undefined || values[a.name] === '')) {
        errs[a.name] = a.name + ' is required';
      }
    }
    return errs;
  }

  async function onInvoke(): Promise<void> {
    if (!selectedPrompt) return;
    const errs = validateRequired(selectedPrompt.arguments);
    if (Object.keys(errs).length > 0) {
      errors = errs;
      return;
    }
    errors = {};
    invokeError = '';
    invoking = true;
    try {
      // Trim empty optional fields — the wire shape carries only the
      // arguments the operator actually supplied (matches the MCP host
      // convention; saves the server from filtering empties).
      const args: Record<string, string> = {};
      for (const a of selectedPrompt.arguments) {
        const v = values[a.name];
        if (typeof v === 'string' && v !== '') {
          args[a.name] = v;
        }
      }
      const result = await invokePrompt(
        { name: selectedPrompt.name, arguments: args },
        base,
        fetchImpl,
      );
      lastResult = result;
    } catch (err) {
      invokeError = err instanceof Error ? err.message : 'invoke failed';
    } finally {
      invoking = false;
    }
  }

  function roleTone(role: string): 'ok' | 'warn' | 'error' | 'neutral' {
    switch (role) {
      case 'system':
        return 'warn';
      case 'assistant':
        return 'ok';
      case 'user':
        return 'neutral';
      default:
        return 'neutral';
    }
  }
</script>

<div class="prompts-panel" data-testid="prompts-panel">
  <DataTable
    {columns}
    {rows}
    pageState={panelState}
    {onRowClick}
    {onRetry}
    emptyTitle="No prompts"
    emptyDescription="The attached server registered no MCP prompts. Register a prompt with server.AddPrompt to surface it here."
  />

  {#if selectedPrompt}
    <section class="invoke" data-testid="prompt-invoke-form">
      <header class="invoke-head">
        <h3>Invoke <code>{selectedPrompt.name}</code></h3>
        {#if selectedPrompt.description}
          <p class="invoke-desc">{selectedPrompt.description}</p>
        {/if}
      </header>

      <form
        class="invoke-form"
        onsubmit={(e) => {
          e.preventDefault();
          void onInvoke();
        }}
      >
        {#each selectedPrompt.arguments as arg (arg.name)}
          <label class="field">
            <span class="field-label">
              {arg.title ?? arg.name}
              {#if arg.required}
                <span class="req" aria-label="required">*</span>
              {/if}
            </span>
            {#if arg.description}
              <span class="field-desc">{arg.description}</span>
            {/if}
            <textarea
              rows="2"
              value={values[arg.name] ?? ''}
              data-testid={`prompt-arg-${arg.name}`}
              oninput={(e) =>
                (values[arg.name] = (e.target as HTMLTextAreaElement).value)}
            ></textarea>
            {#if errors[arg.name]}
              <span class="field-error" data-testid={`prompt-arg-${arg.name}-error`}>
                {errors[arg.name]}
              </span>
            {/if}
          </label>
        {/each}

        <ActionBar>
          <button
            type="submit"
            class="invoke-btn"
            disabled={invoking}
            data-testid="prompt-invoke-submit"
          >
            {invoking ? 'Invoking…' : 'Invoke prompts/get'}
          </button>
        </ActionBar>
      </form>

      {#if invokeError}
        <div class="invoke-error-region" data-testid="prompt-invoke-error-region">
          <ErrorState
            title="prompts/get failed"
            description={invokeError}
            retryLabel="Retry"
            onretry={() => void onInvoke()}
          />
        </div>
      {:else if lastResult}
        <div class="invoke-result" data-testid="prompt-invoke-result">
          {#if lastResult.error}
            <ErrorState
              title="Server reported a prompts/get error"
              description={lastResult.error}
              retryLabel="Retry"
              onretry={() => void onInvoke()}
            />
          {:else}
            <div class="result-head">
              <StatusChip
                label={`${lastResult.messages.length} message${lastResult.messages.length === 1 ? '' : 's'}`}
                tone="ok"
                dot
              />
              {#if lastResult.description}
                <span class="result-note">{lastResult.description}</span>
              {/if}
            </div>
            <ol class="messages" data-testid="prompt-messages">
              {#each lastResult.messages as msg, i (i)}
                <li class="message" data-testid={`prompt-message-${i}`}>
                  <header class="message-head">
                    <StatusChip label={msg.role} tone={roleTone(msg.role)} />
                  </header>
                  <pre class="message-body">{msg.text}</pre>
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      {/if}
    </section>
  {:else}
    <p class="prompts-note">
      Pick a prompt to render its argument form. The inspector calls
      <code>prompts/get</code> against the attached server when you press
      Invoke — the rendered messages render below (D-163; localhost-only,
      operator-driven).
    </p>
  {/if}
</div>

<style>
  .prompts-panel {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    min-height: 0;
  }
  .prompts-note {
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
  .field textarea {
    font: inherit;
    padding: var(--dy-space-1) var(--dy-space-2);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
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
  .messages {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
  }
  .message {
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    padding: var(--dy-space-2);
    background: var(--dy-color-surface-muted);
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-1);
  }
  .message-head {
    display: flex;
    align-items: center;
    gap: var(--dy-space-1);
  }
  .message-body {
    margin: 0;
    font-family: var(--dy-font-mono, monospace);
    font-size: var(--dy-text-sm);
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--dy-color-ink);
  }
</style>
