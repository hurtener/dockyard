<script>
  // The Svelte source for the combined-patterns App — Phase 28 worked
  // example. The runtime uses the simpler hand-written
  // cmd/server/index.html so the example builds without `npm install`,
  // but the Dockyard manifest entry must be a .svelte file (V1 supports
  // the Svelte UI framework only), so this file is the validate-side
  // contract: it carries every renderer + the four UI states the
  // four-state page rule (CLAUDE.md §20) requires.
  //
  // A developer wiring this up against Vite would compile this file
  // (and the per-renderer components) into web/dist/index.html and
  // swap the //go:embed target in cmd/server/main.go. See
  // templates/analytics-widgets for the Vite pattern.

  export let payload = null;

  $: structured = payload && payload.structuredContent;
  $: kind = structured && structured.kind;
  $: state = structured && structured.state;
</script>

{#if !structured}
  <!-- empty: no payload yet -->
  <div class="card"><span class="label">no payload</span></div>
{:else if state === 'loading'}
  <!-- loading: while the tool is in flight -->
  <div class="card"><span class="label">loading…</span></div>
{:else if state === 'empty'}
  <!-- empty: the handler returned an empty payload -->
  <div class="card"><span class="label">{kind} — empty</span><div class="meta">{structured.message || ''}</div></div>
{:else if state === 'error'}
  <!-- error: the handler hit a failure mode -->
  <div class="card"><span class="label chip critical">error</span><div class="meta">{structured.message || ''}</div></div>
{:else if state === 'permission'}
  <!-- permission: the requestor was not authorised -->
  <div class="card"><span class="label chip warn">permission</span><div class="meta">{structured.message || ''}</div></div>
{:else if kind === 'metric_card'}
  <div class="card">
    <span class="label">{structured.label || 'metric'}</span>
    <div class="row">
      <span class="value">{Number(structured.value).toFixed(2)}</span>
      <span>{structured.unit || ''}</span>
      <span class="chip {structured.tone}">{structured.tone}</span>
    </div>
    {#if structured.suggested_action}
      <div class="meta">Suggested next action: {structured.suggested_action}</div>
    {/if}
  </div>
{:else if kind === 'approval'}
  <div class="card">
    <h2>{structured.title || 'Approval required'}</h2>
    <div class="meta">{structured.description || ''}</div>
    {#if state === 'awaiting'}
      <div class="actions">
        <button class="btn" on:click={() => postReply(structured.task_id, true)}>Approve</button>
        <button class="btn secondary" on:click={() => postReply(structured.task_id, false)}>Reject</button>
      </div>
    {:else}
      <div class="meta">{state}{structured.reason ? ' — ' + structured.reason : ''}</div>
    {/if}
  </div>
{:else}
  <div class="card"><span class="label">unknown kind: {kind}</span></div>
{/if}

<script context="module">
  function postReply(taskID, approved) {
    try {
      window.parent.postMessage({
        type: 'dockyard.elicitation-response',
        taskId: taskID,
        data: { approved },
      }, '*');
    } catch (e) { /* outside a bridge — ignored */ }
  }
</script>

<style>
  .card { border: 1px solid color-mix(in srgb, currentColor 15%, transparent); border-radius: 8px; padding: 12px 16px; }
  .label { color: color-mix(in srgb, currentColor 60%, transparent); font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; }
  .value { font-size: 28px; font-weight: 600; }
  .chip { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 12px; margin-left: 8px; }
  .chip.ok { background: rgba(31, 122, 58, 0.12); color: rgb(31, 122, 58); }
  .chip.warn { background: rgba(184, 92, 0, 0.12); color: rgb(184, 92, 0); }
  .chip.critical { background: rgba(179, 0, 22, 0.12); color: rgb(179, 0, 22); }
  .row { display: flex; align-items: baseline; gap: 6px; }
  .meta { color: color-mix(in srgb, currentColor 60%, transparent); font-size: 12px; margin-top: 6px; }
  .btn { background: currentColor; color: white; border: 0; border-radius: 6px; padding: 6px 12px; cursor: pointer; }
  .btn.secondary { background: transparent; color: currentColor; border: 1px solid color-mix(in srgb, currentColor 35%, transparent); }
  .actions { display: flex; gap: 8px; margin-top: 12px; }
</style>
