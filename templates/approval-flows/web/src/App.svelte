<!--
  App.svelte — the single Svelte App the approval-flows template ships.

  Reads `structuredContent.kind` (approval | proposal) from the
  `tool-result` notification and dispatches to the right renderer:
   - approval  → ApprovalCard (the request_approval card)
   - proposal  → EditsForm (the propose_with_edits form)

  request_approval submits its keyed modern task response through
  tasks/update. propose_with_edits uses core MRTR before task creation.
  exposes the typed core-MRTR continuation API.

  Composes only `dockyard-ui` primitives — every renderer routes
  through `PageState` (the four-state page rule, CLAUDE.md §20). The
  only template-local components are ApprovalCard and EditsForm.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { createBridge } from 'dockyard-bridge';
  import {
    PageState,
    type PageStateValue,
  } from 'dockyard-ui';
  import type {
    ProposeWithEditsOutput,
    RequestApprovalOutput,
  } from '../../internal/contracts/contracts.js';

  import ApprovalCard from './ApprovalCard.svelte';
  import EditsForm from './EditsForm.svelte';

  type Payload = RequestApprovalOutput | ProposeWithEditsOutput;

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

  // Advertise the App's supported display modes to the host (sent on the wire as
  // appCapabilities.availableDisplayModes). Keep in sync with dockyard.app.yaml
  // `apps[].display_modes`.
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
    const meta = r._meta as Record<string, unknown> | undefined;
    if (meta) {
      const id = meta['taskId'] as string | undefined;
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
    if (payload?.kind === 'approval' && taskId) {
      void bridge.updateTask({
        taskId,
        inputResponses: { 'approval-decision': { approved, reason } },
      }).catch((err: unknown) => {
        pageState = 'error';
        message = `Could not submit the decision: ${(err as Error)?.message ?? err}`;
      });
    }
  }

  function onDecline() {
    if (!payload) return;
    payload = { ...payload, state: 'rejected', approved: false, reason: 'declined' } as Payload;
    pageState = 'ready';
    if (payload.kind === 'approval' && taskId) {
      void bridge.updateTask({
        taskId,
        inputResponses: { 'approval-decision': { approved: false, reason: 'declined' } },
      });
    }
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
      payload={payload as RequestApprovalOutput}
      onApprove={(reason) => onDecision(true, reason)}
      onReject={(reason) => onDecision(false, reason)}
      onDecline={onDecline}
    />
  {:else if payload?.kind === 'proposal'}
    <EditsForm
      payload={payload as ProposeWithEditsOutput}
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
      errorTitle={permissionMessage ? 'Not authorized' : 'The approval App hit an error'}
      errorDescription={permissionMessage || message || 'Something went wrong.'}
      loadingMessage="Connecting to the host…"
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
