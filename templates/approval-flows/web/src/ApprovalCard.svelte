<!--
  ApprovalCard — renders a request_approval prompt and captures the
  user's decision.

  Three modes driven by `payload.state`:
   - awaiting:  the prompt + Approve / Reject buttons (and a free-text
                reason field).
   - approved:  a "Decision: approved" summary.
   - rejected:  a "Decision: rejected" summary.

  Composes web/ui StatusChip / ActionBar; the card itself is template-
  local (it is shaped to one tool's UI, not a reusable inventory
  component — CLAUDE.md §20).
-->
<script lang="ts">
  import { StatusChip, ActionBar } from 'dockyard-ui';

  type ApprovalPayload = {
    kind: 'approval';
    title: string;
    description: string;
    category?: string;
    metadata?: Record<string, unknown>;
    state: 'awaiting' | 'approved' | 'rejected' | 'empty' | 'error' | 'permission';
    approved?: boolean;
    reason?: string;
    decided_at?: string;
  };

  interface Props {
    payload: ApprovalPayload;
    onApprove: (reason?: string) => void;
    onReject: (reason?: string) => void;
    onDecline: () => void;
  }

  let { payload, onApprove, onReject, onDecline }: Props = $props();

  let reason = $state('');
  const awaiting = $derived(payload.state === 'awaiting');
  const decided = $derived(payload.state === 'approved' || payload.state === 'rejected');
</script>

<article class="approval-card" data-testid="approval-card" data-state={payload.state}>
  <header class="approval-card__header">
    <div class="approval-card__title-row">
      <h2 class="approval-card__title">{payload.title}</h2>
      {#if payload.category}
        <StatusChip label={payload.category} tone="info" />
      {/if}
    </div>
    {#if payload.description}
      <p class="approval-card__description">{payload.description}</p>
    {/if}
  </header>

  {#if payload.metadata && Object.keys(payload.metadata).length > 0}
    <dl class="approval-card__metadata">
      {#each Object.entries(payload.metadata) as [k, v] (k)}
        <div class="approval-card__metadata-row">
          <dt>{k}</dt>
          <dd>{formatValue(v)}</dd>
        </div>
      {/each}
    </dl>
  {/if}

  {#if awaiting}
    <label class="approval-card__reason">
      <span>Reason (optional)</span>
      <textarea
        rows="2"
        placeholder="Add a short note about your decision."
        bind:value={reason}
        data-testid="approval-card-reason"
      ></textarea>
    </label>
    <ActionBar>
      {#snippet children()}
        <button
          type="button"
          class="approval-card__btn approval-card__btn--reject"
          data-testid="approval-card-reject"
          onclick={() => onReject(reason.trim() || undefined)}
        >Reject</button>
        <button
          type="button"
          class="approval-card__btn approval-card__btn--decline"
          data-testid="approval-card-decline"
          onclick={() => onDecline()}
        >Decline</button>
        <button
          type="button"
          class="approval-card__btn approval-card__btn--approve"
          data-testid="approval-card-approve"
          onclick={() => onApprove(reason.trim() || undefined)}
        >Approve</button>
      {/snippet}
    </ActionBar>
  {/if}

  {#if decided}
    <div class="approval-card__verdict" data-testid="approval-card-verdict">
      <StatusChip
        label={payload.state === 'approved' ? 'Approved' : 'Rejected'}
        tone={payload.state === 'approved' ? 'ok' : 'warn'}
      />
      {#if payload.reason}
        <p class="approval-card__reason-display">"{payload.reason}"</p>
      {/if}
      {#if payload.decided_at}
        <p class="approval-card__timestamp">{formatTime(payload.decided_at)}</p>
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
  function formatTime(iso: string): string {
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }
</script>

<style>
  .approval-card {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-4);
    max-width: 640px;
    margin: 0 auto;
    padding: var(--dy-space-5);
    background: var(--dy-color-surface);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-lg);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  }
  .approval-card__title-row {
    display: flex;
    gap: var(--dy-space-3);
    align-items: center;
    flex-wrap: wrap;
  }
  .approval-card__title {
    margin: 0;
    font-size: var(--dy-text-xl, 1.25rem);
    color: var(--dy-color-ink);
  }
  .approval-card__description {
    margin: var(--dy-space-2) 0 0 0;
    color: var(--dy-color-ink-soft);
    line-height: 1.5;
  }
  .approval-card__metadata {
    margin: 0;
    padding: var(--dy-space-3);
    background: var(--dy-color-canvas);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    display: grid;
    gap: var(--dy-space-2);
  }
  .approval-card__metadata-row {
    display: grid;
    grid-template-columns: 1fr 2fr;
    gap: var(--dy-space-3);
    font-size: var(--dy-text-sm);
  }
  .approval-card__metadata-row dt {
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--dy-color-ink-faint, var(--dy-color-ink-soft));
    font-size: var(--dy-text-xs);
  }
  .approval-card__metadata-row dd {
    margin: 0;
    color: var(--dy-color-ink);
    font-family: var(--dy-font-mono, monospace);
    word-break: break-word;
  }
  .approval-card__reason {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
    font-size: var(--dy-text-sm);
  }
  .approval-card__reason textarea {
    padding: var(--dy-space-2);
    background: var(--dy-color-canvas);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    font: inherit;
    font-family: var(--dy-font-sans);
    resize: vertical;
  }
  .approval-card__btn {
    padding: var(--dy-space-2) var(--dy-space-4);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .approval-card__btn:focus-visible {
    outline: 2px solid var(--dy-color-primary);
    outline-offset: 2px;
  }
  .approval-card__btn--approve {
    background: var(--dy-state-ok-fg);
    color: var(--dy-state-ok-bg, var(--dy-color-canvas));
    border-color: var(--dy-state-ok-fg);
  }
  .approval-card__btn--reject {
    background: var(--dy-state-warn-fg);
    color: var(--dy-state-warn-bg, var(--dy-color-canvas));
    border-color: var(--dy-state-warn-fg);
  }
  .approval-card__btn--decline {
    background: transparent;
    color: var(--dy-color-ink-soft);
  }
  .approval-card__verdict {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
    padding: var(--dy-space-4);
    background: var(--dy-color-canvas);
    border: 1px dashed var(--dy-color-border);
    border-radius: var(--dy-radius-md);
  }
  .approval-card__reason-display {
    margin: 0;
    color: var(--dy-color-ink);
    font-style: italic;
  }
  .approval-card__timestamp {
    margin: 0;
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-faint, var(--dy-color-ink-soft));
  }
</style>
