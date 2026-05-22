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
  } from '@dockyard/ui';
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
  import type { HostCapabilities, HostContext } from '@dockyard/bridge';

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
  }

  let {
    html,
    appName = 'MCP App',
    onRpc,
    hostContext,
    hostCapabilities,
    fixtureResult,
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
  }

  function retry(): void {
    // Re-key the iframe so srcdoc reloads, then re-wire the bridge.
    mountKey += 1;
  }

  // Wire (or re-wire) whenever the iframe element, the mount key, or the
  // emulated host capabilities change — a capability change re-runs the App's
  // `ui/initialize` handshake against the new host (RFC §12 capability-set
  // emulation).
  $effect(() => {
    void mountKey;
    void hostContext;
    void hostCapabilities;
    if (iframe) wire();
  });

  // Feed the active fixture to the live bridge without re-mounting — selecting
  // a new fixture answers the next `tools/call` from it.
  $effect(() => {
    handle?.bridge.setCallToolResponder(
      fixtureResult ? () => fixtureResult : undefined,
    );
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
      <iframe
        bind:this={iframe}
        title={appName}
        class="preview"
        sandbox={APP_SANDBOX}
        srcdoc={html}
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
