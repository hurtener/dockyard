<!--
  App.svelte — the single Svelte App the approval-flows template ships.

  Reads `structuredContent.kind` (approval | proposal) from the
  `tool-result` notification and dispatches to the right renderer:
   - approval  → ApprovalCard (the request_approval card)
   - proposal  → EditsForm (the propose_with_edits form)

  When the user decides, the App posts the reply back through the
  bridge's `sendElicitationResponse` View helper (Phase 25 / D-134); the
  inspector's host-half delivers it to the attached server's
  `tasks/result` endpoint and the suspended task resumes. The bridge
  also extracts the related-task id from the `tool-result`'s `_meta`
  (the runtime stamps it via the related-task association — RFC §8.6),
  so the App always answers the right task.

  Composes only `@dockyard/ui` primitives — every renderer routes
  through `PageState` (the four-state page rule, CLAUDE.md §20). The
  only template-local components are ApprovalCard and EditsForm.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { createBridge } from '@dockyard/bridge';
  import {
    PageState,
    type PageStateValue,
  } from '@dockyard/ui';

  import ApprovalCard from './ApprovalCard.svelte';
  import EditsForm from './EditsForm.svelte';

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

  type ApprovalPayload = {
    kind: 'approval';
    task_id?: string;
    title: string;
    description: string;
    category?: string;
    metadata?: Record<string, unknown>;
    state: 'awaiting' | 'approved' | 'rejected' | 'empty' | 'error' | 'permission';
    approved?: boolean;
    reason?: string;
    decided_at?: string;
    message?: string;
  };
  type ProposalPayload = {
    kind: 'proposal';
    task_id?: string;
    title: string;
    description: string;
    fields: Field[];
    category?: string;
    state: 'awaiting' | 'approved' | 'rejected' | 'empty' | 'error' | 'permission';
    approved?: boolean;
    edits?: Record<string, unknown>;
    reason?: string;
    decided_at?: string;
    message?: string;
  };
  type Payload = ApprovalPayload | ProposalPayload;

  let pageState: PageStateValue = $state('loading');
  let payload = $state<Payload | null>(null);
  let message = $state('Waiting for a prompt…');
  let taskId = $state<string>('');
  let permissionMessage = $state('');
  // Re-render trigger — incrementing this forces the template's
  // conditional discriminator to re-evaluate even when Svelte 5's
  // proxy-based reactivity doesn't pick up an assignment dispatched
  // from a cross-iframe message handler.
  let renderTick = $state(0);
  // A plain (non-reactive) flag the connect-then-empty handler checks —
  // a Svelte 5 `$state` proxy read inside a setTimeout closure can race
  // with the dispatch ordering of an inbound notification (the
  // notification's handler returns synchronously, but the proxy's
  // getter chain is invoked AFTER the closure's local snapshot was
  // captured). The plain `let` is captured by-reference and always
  // reflects the latest assignment from `onToolResult`.
  let payloadEverSet = false;

  const bridge = createBridge({ displayModes: ['inline'] });

  // Subscription at module-init — same pattern analytics-widgets uses;
  // a notification arriving before onMount runs is delivered the
  // moment the bridge dispatches it (`bridge.onToolResult` registers
  // the handler immediately, before `connect()` is called).
  const offResult = bridge.onToolResult<Payload>((r) => {
    if (!r.structuredContent) {
      pageState = 'error';
      message = 'The tool returned no structured payload.';
      payload = null;
      return;
    }
    payload = r.structuredContent;
    payloadEverSet = true;
    renderTick = renderTick + 1;
    // Prefer the task_id stamped into the payload by the handler
    // (Phase 25 — handlers stamp it on Create). Fall back to the
    // _meta.taskId the runtime stamps on tasks/result.
    if (payload.task_id) {
      taskId = payload.task_id;
    }
    const meta = r._meta as Record<string, unknown> | undefined;
    if (meta) {
      const id = (meta['taskId'] ?? meta['task_id']) as string | undefined;
      if (id) taskId = id;
    }
    pageState = mapState(payload.state);
    message = payload.message ?? '';
    if (payload.state === 'permission') {
      permissionMessage = payload.message ?? 'You are not authorized to make this decision.';
    }
  });

  function mapState(state: Payload['state'] | string | undefined): PageStateValue {
    switch (state) {
      case 'awaiting':
      case 'approved':
      case 'rejected':
      case 'ready':
        return 'ready';
      case 'empty':
        return 'empty';
      case 'permission':
        return 'error';
      case 'error':
        return 'error';
      default:
        // An unknown state (e.g. a schema-derived synthetic fixture from the
        // inspector that did not understand the contract's enum) falls
        // through to ready — the dispatcher below renders the prompt verbatim
        // rather than the App-frame's red error overlay.
        return 'ready';
    }
  }

  function onDecision(approved: boolean, reason?: string, edits?: Record<string, unknown>) {
    if (!payload) return;
    const decidedAt = new Date().toISOString();
    // Patch the payload in place so the App immediately reflects the
    // decision (the host's terminal tool-result will arrive a moment
    // later and confirm). This keeps the UX snappy and matches the
    // inspector Fixtures-switcher flow.
    if (payload.kind === 'approval') {
      payload = { ...payload, state: approved ? 'approved' : 'rejected', approved, reason, decided_at: decidedAt };
    } else {
      payload = { ...payload, state: approved ? 'approved' : 'rejected', approved, reason, edits, decided_at: decidedAt };
    }
    pageState = 'ready';
    bridge.sendElicitationResponse(taskId, { approved, reason, edits });
  }

  function onDecline() {
    if (!payload) return;
    payload = { ...payload, state: 'rejected', approved: false, reason: 'declined' } as Payload;
    pageState = 'ready';
    bridge.sendElicitationResponse(taskId, undefined, { declined: true });
  }

  onMount(() => {
    // Kick off the `ui/initialize` handshake — the subscription above
    // is already live. The empty-state fallback is intentionally
    // gated on `payload` (read at the timer's tick) so an early
    // tool-result push wins the race; if no push arrives within
    // 500ms, the App settles into "waiting for prompt" so the user
    // never stares at an indefinite spinner.
    bridge.connect().catch((err: unknown) => {
      pageState = 'error';
      message = `Bridge handshake failed: ${(err as Error)?.message ?? err}`;
    });
    setTimeout(() => {
      // Read the reactive `payload` through the proxy at the tick —
      // a fresh read reflects any assignment the tool-result handler
      // performed in the meantime.
      if (payload == null) {
        pageState = 'empty';
        message = 'Connected. Waiting for an approval prompt.';
      }
    }, 500);
  });

  onDestroy(() => {
    offResult();
    bridge.close();
  });
</script>

<div class="approval-app" data-testid="approval-app" data-tick={renderTick}>
  {#if payload?.kind === 'approval'}
    <ApprovalCard
      payload={payload}
      onApprove={(reason) => onDecision(true, reason)}
      onReject={(reason) => onDecision(false, reason)}
      onDecline={onDecline}
    />
  {:else if payload?.kind === 'proposal'}
    <EditsForm
      payload={payload}
      onApprove={(reason, edits) => onDecision(true, reason, edits)}
      onReject={(reason) => onDecision(false, reason)}
      onDecline={onDecline}
    />
  {:else}
    <!-- The four-state fallback: when no payload has arrived (or an
         unknown kind landed), route through PageState so the App still
         honours the loading/empty/error/permission contract
         (CLAUDE.md §20). -->
    <PageState
      state={pageState}
      emptyTitle="No prompt"
      emptyDescription={message || 'No approval prompt has arrived yet.'}
      errorTitle="The approval App hit an error"
      errorDescription={message || 'Something went wrong.'}
      loadingDescription="Connecting to the host…"
      permissionTitle="Not authorized"
      permissionDescription={permissionMessage}
    >
      {#snippet children()}
        <p>Waiting for an approval prompt.</p>
      {/snippet}
    </PageState>
  {/if}
</div>

<style>
  .approval-app {
    width: 100%;
    min-height: 100%;
    padding: var(--dy-space-4);
    background: var(--dy-color-canvas);
    color: var(--dy-color-ink);
    font-family: var(--dy-font-sans);
    box-sizing: border-box;
  }
</style>
