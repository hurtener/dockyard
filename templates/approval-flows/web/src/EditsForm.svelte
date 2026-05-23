<!--
  EditsForm — renders a propose_with_edits proposal as a typed form.

  Each field is composed from the shared @dockyard/ui FieldDiff component
  (Phase 25) — the editable current → proposed pair. The user edits any
  proposed value; on Approve, the App posts the final edits map back
  through the bridge's elicitation-response notification.

  Three modes driven by `payload.state`:
   - awaiting:  the form + Approve / Reject / Decline buttons.
   - approved:  a "Decision: approved" summary listing the final edits.
   - rejected:  a "Decision: rejected" summary with the user's reason.
-->
<script lang="ts">
  import { ActionBar, FieldDiff, StatusChip } from '@dockyard/ui';

  type FieldOption = { value: string; label: string };
  type Field = {
    key: string;
    label: string;
    type: 'string' | 'number' | 'boolean' | 'enum' | 'text';
    current: unknown;
    proposed: unknown;
    options?: FieldOption[];
    helper_text?: string;
  };
  type ProposalPayload = {
    kind: 'proposal';
    title: string;
    description: string;
    fields: Field[];
    category?: string;
    state: 'awaiting' | 'approved' | 'rejected' | 'empty' | 'error' | 'permission';
    approved?: boolean;
    edits?: Record<string, unknown>;
    reason?: string;
    decided_at?: string;
  };

  interface Props {
    payload: ProposalPayload;
    onApprove: (reason: string | undefined, edits: Record<string, unknown>) => void;
    onReject: (reason?: string) => void;
    onDecline: () => void;
  }

  let { payload, onApprove, onReject, onDecline }: Props = $props();

  // The local edits map starts as the proposed values; FieldDiff
  // onChange callbacks update it in place. Initialised in a $effect so
  // a new payload arriving from the host (e.g. the inspector's
  // fixture switcher) re-seeds the form rather than freezing the
  // initial-mount snapshot.
  let edits = $state<Record<string, unknown>>({});
  let reason = $state('');

  $effect(() => {
    // Re-seed when the payload identity changes — the App's dispatcher
    // remounts EditsForm on a kind switch, but a same-kind re-apply
    // keeps the instance.
    edits = initialEdits(payload.fields);
  });

  function initialEdits(fields: Field[]): Record<string, unknown> {
    const m: Record<string, unknown> = {};
    for (const f of fields) m[f.key] = f.proposed;
    return m;
  }

  function handleFieldChange(key: string, value: unknown): void {
    edits = { ...edits, [key]: value };
  }

  const awaiting = $derived(payload.state === 'awaiting');
  const decided = $derived(payload.state === 'approved' || payload.state === 'rejected');
</script>

<article class="edits-form" data-testid="edits-form" data-state={payload.state}>
  <header class="edits-form__header">
    <div class="edits-form__title-row">
      <h2 class="edits-form__title">{payload.title}</h2>
      {#if payload.category}
        <StatusChip label={payload.category} tone="info" />
      {/if}
    </div>
    {#if payload.description}
      <p class="edits-form__description">{payload.description}</p>
    {/if}
  </header>

  {#if awaiting}
    <div class="edits-form__fields" data-testid="edits-form-fields">
      {#each payload.fields as field (field.key)}
        <FieldDiff
          id={`field-${field.key}`}
          label={field.label}
          type={field.type}
          current={field.current}
          proposed={edits[field.key]}
          options={field.options}
          helperText={field.helper_text}
          onChange={(v) => handleFieldChange(field.key, v)}
        />
      {/each}
    </div>

    <label class="edits-form__reason">
      <span>Reason (optional)</span>
      <textarea
        rows="2"
        placeholder="Add a short note about your decision."
        bind:value={reason}
        data-testid="edits-form-reason"
      ></textarea>
    </label>

    <ActionBar>
      {#snippet children()}
        <button
          type="button"
          class="edits-form__btn edits-form__btn--reject"
          data-testid="edits-form-reject"
          onclick={() => onReject(reason.trim() || undefined)}
        >Reject</button>
        <button
          type="button"
          class="edits-form__btn edits-form__btn--decline"
          data-testid="edits-form-decline"
          onclick={() => onDecline()}
        >Decline</button>
        <button
          type="button"
          class="edits-form__btn edits-form__btn--approve"
          data-testid="edits-form-approve"
          onclick={() => onApprove(reason.trim() || undefined, edits)}
        >Approve with edits</button>
      {/snippet}
    </ActionBar>
  {/if}

  {#if decided}
    <div class="edits-form__verdict" data-testid="edits-form-verdict">
      <StatusChip
        label={payload.state === 'approved' ? 'Approved with edits' : 'Rejected'}
        tone={payload.state === 'approved' ? 'ok' : 'warn'}
      />
      {#if payload.reason}
        <p class="edits-form__reason-display">"{payload.reason}"</p>
      {/if}
      {#if payload.edits}
        <table class="edits-form__edits-table">
          <thead>
            <tr><th>Field</th><th>Final value</th></tr>
          </thead>
          <tbody>
            {#each Object.entries(payload.edits) as [k, v] (k)}
              <tr>
                <td>{k}</td>
                <td><code>{formatValue(v)}</code></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</article>

<script module lang="ts">
  function formatValue(v: unknown): string {
    if (v === null || v === undefined) return '—';
    if (typeof v === 'object') return JSON.stringify(v);
    return String(v);
  }
</script>

<style>
  .edits-form {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-4);
    max-width: 720px;
    margin: 0 auto;
    padding: var(--dy-space-5);
    background: var(--dy-color-surface);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-lg);
  }
  .edits-form__title-row {
    display: flex;
    gap: var(--dy-space-3);
    align-items: center;
    flex-wrap: wrap;
  }
  .edits-form__title {
    margin: 0;
    font-size: var(--dy-text-xl, 1.25rem);
    color: var(--dy-color-ink);
  }
  .edits-form__description {
    margin: var(--dy-space-2) 0 0 0;
    color: var(--dy-color-ink-soft);
    line-height: 1.5;
  }
  .edits-form__fields {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
  }
  .edits-form__reason {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
    font-size: var(--dy-text-sm);
  }
  .edits-form__reason textarea {
    padding: var(--dy-space-2);
    background: var(--dy-color-canvas);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    font: inherit;
    font-family: var(--dy-font-sans);
    resize: vertical;
  }
  .edits-form__btn {
    padding: var(--dy-space-2) var(--dy-space-4);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .edits-form__btn:focus-visible {
    outline: 2px solid var(--dy-color-primary);
    outline-offset: 2px;
  }
  .edits-form__btn--approve {
    background: var(--dy-state-ok-fg);
    color: var(--dy-state-ok-bg, var(--dy-color-canvas));
    border-color: var(--dy-state-ok-fg);
  }
  .edits-form__btn--reject {
    background: var(--dy-state-warn-fg);
    color: var(--dy-state-warn-bg, var(--dy-color-canvas));
    border-color: var(--dy-state-warn-fg);
  }
  .edits-form__btn--decline {
    background: transparent;
    color: var(--dy-color-ink-soft);
  }
  .edits-form__verdict {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-3);
    padding: var(--dy-space-4);
    background: var(--dy-color-canvas);
    border: 1px dashed var(--dy-color-border);
    border-radius: var(--dy-radius-md);
  }
  .edits-form__reason-display {
    margin: 0;
    color: var(--dy-color-ink);
    font-style: italic;
  }
  .edits-form__edits-table {
    border-collapse: collapse;
    width: 100%;
    font-size: var(--dy-text-sm);
  }
  .edits-form__edits-table th,
  .edits-form__edits-table td {
    text-align: left;
    padding: var(--dy-space-2);
    border-bottom: 1px solid var(--dy-color-border);
  }
  .edits-form__edits-table code {
    font-family: var(--dy-font-mono, monospace);
    word-break: break-word;
  }
</style>
