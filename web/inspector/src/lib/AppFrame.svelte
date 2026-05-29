<script lang="ts">
  /**
   * AppFrame — the inspector's App preview frame.
   *
   * Renders an MCP App in a sandboxed iframe and drives the host-half bridge
   * against it so the App completes its `ui/initialize` handshake locally
   * (RFC §12). Routes through the four-state `PageState` — a real error state
   * with a working retry when the App fails to render or handshake.
   */
  import { onDestroy } from 'svelte';
  import {
    LoadingState,
    ErrorState,
    StatusChip,
    type PageStateValue,
  } from 'dockyard-ui';
  import {
    mountAppFrame,
    APP_SANDBOX,
    type AppFrameHandle,
    type AppFrameStatus,
  } from './app-frame.js';
  import type {
    CallToolFixtureResult,
    HostRpcLogEntry,
  } from '../host/host-bridge.js';
  import type {
    ElicitationResponseParams,
    HostCapabilities,
    HostContext,
    TaskProgressParams,
  } from 'dockyard-bridge';

  interface Props {
    /** The App's HTML document — set as the iframe `srcdoc`. */
    html: string;
    /** The App's display name, for the frame header. */
    appName?: string;
    /** Called for every `ui/` JSON-RPC message — feeds the RPC panel. */
    onRpc?: (entry: HostRpcLogEntry) => void;
    /**
     * The emulated host context — the capability-set emulation seam. Changing
     * it re-mounts the App so it re-runs `ui/initialize` against the new host.
     */
    hostContext?: HostContext;
    /** The emulated host capabilities (Apps / Tasks toggles). */
    hostCapabilities?: HostCapabilities;
    /**
     * The active fixture's `tools/call` outcome. When set, the App's
     * `tools/call` is answered from it (RFC §12 — the fixture switcher).
     */
    fixtureResult?: CallToolFixtureResult;
    /**
     * The active fixture's structuredContent, pushed to the App as a
     * `tool-result` notification whenever it changes. This is the
     * inspector's emulation of a real host: in production, the model
     * invokes the tool and the host pushes the result. The inspector's
     * fixture switcher pushes the fixture's structuredContent the same
     * way so the App's `onToolResult` handlers fire — without it, an App
     * that only subscribes to `tool-result` (the analytics-widgets
     * template's pattern) would stay on its loading state forever.
     */
    pushToolResult?: unknown;
    /**
     * Called when the App posts a `ui/notifications/elicitation-response`
     * (Phase 25 / D-134). The inspector wires this to its backend's
     * `POST /api/tasks/elicitation` endpoint, which forwards the reply
     * to the attached server's `tasks/result`. The App-frame itself
     * does not call the network — it stays a pure host-half shell.
     */
    onElicitationResponse?: (params: ElicitationResponseParams) => void;
    /**
     * Optional `_meta.taskId` stamped on the pushed tool-result. The
     * runtime's related-task association stamps this on a real
     * `tasks/result` payload (RFC §8.6) so the App can correlate the
     * push with the task it is answering. The fixture switcher does
     * not have a real task id, but operator-initiated invokes and
     * future real task results carry one — when present, it is
     * forwarded so the App's elicitation-response carries the right
     * task id.
     */
    taskIdMeta?: string;
    /**
     * The most recent task-progress point observed on the `obs/v1` stream
     * (RFC §8.4). When set, the host-half forwards it to the App as a
     * `ui/notifications/task-progress` so the App's card renders a live
     * "62%" — the inspector's emulation of a production host forwarding a
     * running task's progress. Absent on a run with no tasks; an App that
     * does not subscribe is unaffected (the channel degrades by absence).
     */
    pushTaskProgress?: TaskProgressParams;
  }

  let {
    html,
    appName = 'MCP App',
    onRpc,
    hostContext,
    hostCapabilities,
    fixtureResult,
    pushToolResult,
    onElicitationResponse,
    taskIdMeta,
    pushTaskProgress,
  }: Props = $props();

  let iframe = $state<HTMLIFrameElement | undefined>(undefined);
  let handle: AppFrameHandle | undefined;
  let frameStatus = $state<AppFrameStatus>('idle');
  let mountKey = $state(0);

  // The PageState the frame routes through, derived from the bridge status.
  const pageState = $derived<PageStateValue>(
    frameStatus === 'ready'
      ? 'ready'
      : frameStatus === 'error'
        ? 'error'
        : 'loading',
  );

  function wire(): void {
    if (!iframe) return;
    handle?.close();
    // Reset the loop-guard for the freshly mounted iframe — a new App
    // instance starts blank and needs the current pushToolResult
    // re-delivered (Phase 25 — `lastSentPayload` retains its value
    // across iframe re-mounts because it sits outside the effect's
    // closure; without this reset, the App that was just remounted
    // never sees the active fixture). The same value will only flow
    // once into the new instance.
    lastSentPayload = '';
    lastSentProgress = '';
    handle = mountAppFrame({
      iframe,
      hostWindow: window,
      hostContext,
      hostCapabilities,
      onRpc,
      onStatus: (s) => (frameStatus = s),
    });
    // Wire the active fixture into the freshly mounted bridge.
    handle.bridge.setCallToolResponder(
      fixtureResult ? () => fixtureResult : undefined,
    );
    // Wire the elicitation-response sink (Phase 25 / D-134). The
    // host-half logs the notification to the RPC panel either way; with
    // a sink wired, it additionally forwards the typed params to the
    // caller — the inspector wires this to the backend POST that
    // delivers the elicitation to the attached server's tasks/result.
    handle.bridge.setElicitationResponder(onElicitationResponse);
  }

  function retry(): void {
    // Re-key the iframe so srcdoc reloads, then re-wire the bridge.
    mountKey += 1;
  }

  // Wire (or re-wire) whenever the iframe element, the mount key, or the
  // emulated host capabilities change — a capability change re-runs the App's
  // `ui/initialize` handshake against the new host (RFC §12 capability-set
  // emulation).
  //
  // IMPORTANT — set the `srcdoc` from the effect rather than at attribute
  // declaration time. Chromium has a long-standing race where an iframe
  // whose srcdoc is set during the same parser tick as the element's
  // insertion sometimes loads the HTML but never runs its scripts. Setting
  // srcdoc here, AFTER `bind:this` has resolved and the iframe is attached
  // to the DOM, makes the navigation deterministic — scripts always run.
  $effect(() => {
    void mountKey;
    void hostContext;
    void hostCapabilities;
    void html;
    if (!iframe) return;
    const localIframe = iframe;
    // Deferred srcdoc assignment via queueMicrotask + a no-op load wait.
    // Chromium has a long-standing race where an iframe whose srcdoc is set
    // during the same parser tick as the element's insertion sometimes
    // loads the HTML but never runs its scripts (the navigation
    // "completes" before the scripts execute). Setting srcdoc in a
    // microtask, AFTER `bind:this` resolves and the iframe is in the DOM,
    // makes the navigation deterministic — scripts always run.
    queueMicrotask(() => {
      if (localIframe.isConnected) {
        localIframe.srcdoc = html;
      }
    });
    wire();
  });

  // Feed the active fixture to the live bridge without re-mounting — selecting
  // a new fixture answers the next `tools/call` from it.
  $effect(() => {
    handle?.bridge.setCallToolResponder(
      fixtureResult ? () => fixtureResult : undefined,
    );
  });

  // Re-wire the elicitation responder when the caller-supplied handler
  // changes — same shape as the fixture responder.
  $effect(() => {
    handle?.bridge.setElicitationResponder(onElicitationResponse);
  });

  // Push the active fixture's structuredContent to the App as a `tool-result`
  // notification, but only AFTER the bridge handshake is complete. The
  // effect tracks both pushToolResult AND frameStatus, so it re-runs when
  // either changes — including the transition handshaking→ready that the
  // initial mount produces. Phase 24 — D-127.
  //
  // `structuredClone` over the raw prop yields a plain object: cross-frame
  // postMessage uses the structured-clone algorithm and refuses to serialise
  // a Svelte $state Proxy (DataCloneError) — a bug discovered in the Phase 24
  // demo. Cloning HERE keeps that detail out of the BridgeShell.
  //
  // `lastSentPayload` is a stable closure variable that guards against an
  // infinite re-fire loop: an operator-initiated tools/call (D-131) result
  // would otherwise trigger a re-render → the iframe's response → another
  // handshake → frameStatus oscillation → this effect re-firing → another
  // sendToolResult → loop (`effect_update_depth_exceeded`). Comparing the
  // serialised payload to the last sent one breaks the cycle: an unchanged
  // value is a no-op, an actual fixture-or-invoke change goes through once.
  let lastSentPayload = '';
  $effect(() => {
    void pushToolResult;
    void frameStatus;
    if (!handle || pushToolResult === undefined) return;
    if (frameStatus !== 'ready') return; // wait for the handshake to finish
    const serialised = (() => {
      try {
        return JSON.stringify(pushToolResult);
      } catch {
        return '__unstringifiable__:' + Math.random();
      }
    })();
    if (serialised === lastSentPayload) return;
    lastSentPayload = serialised;
    // host-bridge clones every outbound message; we don't need to clone here
    // too, but it does no harm and decouples the AppFrame from that detail.
    // Stamp the related-task id in _meta when the caller supplied one
    // (Phase 25 — the App's elicitation-response needs to know which
    // task it is answering; the runtime's related-task association
    // does this on a real tasks/result, but the fixture path needs us
    // to forward it explicitly).
    const meta = taskIdMeta ? { taskId: taskIdMeta } : undefined;
    handle.bridge.sendToolResult({
      content: [],
      structuredContent: pushToolResult,
      ...(meta ? { _meta: meta } : {}),
    });
  });

  // Forward the latest task-progress point to the App as a
  // `ui/notifications/task-progress` once the handshake is complete (RFC §8.4).
  // Same shape as the pushToolResult effect: wait for `ready`, and guard
  // against re-firing on an unchanged value (a serialised compare) so an
  // append-only obs stream that re-derives the same latest point is a no-op.
  let lastSentProgress = '';
  $effect(() => {
    void pushTaskProgress;
    void frameStatus;
    if (!handle || pushTaskProgress === undefined) return;
    if (frameStatus !== 'ready') return; // wait for the handshake to finish
    const serialised = (() => {
      try {
        return JSON.stringify(pushTaskProgress);
      } catch {
        return '__unstringifiable__:' + Math.random();
      }
    })();
    if (serialised === lastSentProgress) return;
    lastSentProgress = serialised;
    handle.bridge.sendTaskProgress(pushTaskProgress);
  });

  onDestroy(() => handle?.close());
</script>

<div class="app-frame" data-testid="app-frame">
  <div class="frame-header">
    <span class="frame-title">{appName}</span>
    <StatusChip
      label={frameStatus}
      tone={frameStatus === 'ready'
        ? 'ok'
        : frameStatus === 'error'
          ? 'error'
          : 'info'}
      dot
    />
  </div>
  <!--
    The four-state region (CLAUDE.md §20). The iframe is ALWAYS mounted so the
    host-half bridge can wire to its contentWindow; the loading and error states
    render as an overlay on top of it, never by un-mounting the frame.
  -->
  <div class="frame-region" data-state={pageState} data-testid="page-state">
    {#key mountKey}
      <!--
        `srcdoc` is set by the $effect above, not as a Svelte attribute, to
        work around the Chromium iframe-srcdoc / mount-race that drops script
        execution when the attribute and the DOM insertion land in the same
        parser tick.
      -->
      <iframe
        bind:this={iframe}
        title={appName}
        class="preview"
        sandbox={APP_SANDBOX}
      ></iframe>
    {/key}
    {#if pageState === 'loading'}
      <div class="frame-overlay">
        <LoadingState
          message="Rendering the App and completing its handshake…"
        />
      </div>
    {:else if pageState === 'error'}
      <div class="frame-overlay">
        <ErrorState
          title="App failed to render"
          description="The App did not complete its ui/initialize handshake. Retry."
          onretry={retry}
        />
      </div>
    {/if}
  </div>
</div>

<style>
  .app-frame {
    display: flex;
    flex-direction: column;
    gap: var(--dy-space-2);
    min-height: 0;
    height: 100%;
  }
  .frame-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--dy-space-2);
  }
  .frame-title {
    font-weight: 600;
    font-size: var(--dy-text-sm);
  }
  .frame-region {
    position: relative;
    flex: 1;
    min-height: 22rem;
  }
  .preview {
    width: 100%;
    height: 100%;
    min-height: 22rem;
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-surface);
  }
  .frame-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--dy-color-surface);
    border-radius: var(--dy-radius-md);
  }
</style>
