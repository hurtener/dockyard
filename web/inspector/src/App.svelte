<script lang="ts">
  /**
   * App.svelte — the inspector page.
   *
   * Built to design-spec.md §4 and the approved mockups/inspector.png: one
   * `AppShell` — a `PageHeader` (logo + server name/version/transport +
   * Host/Display controls), the App preview frame, the tabbed `DetailRail`
   * (Events / RPC working in Phase 22; Fixtures / Tools / Verdicts / Tasks are
   * Phase 23 placeholders), and a `ConnectionFooter`.
   *
   * Every component is composed from `@dockyard/ui` — none is re-implemented
   * (CLAUDE.md §20). Every async region routes through `PageState`.
   */
  import { onMount, onDestroy } from 'svelte';
  import {
    AppShell,
    PageHeader,
    DetailRail,
    RailCard,
    ConnectionFooter,
    StatusChip,
    EmptyState,
    type PageStateValue,
  } from '@dockyard/ui';
  import EventsPanel from './lib/EventsPanel.svelte';
  import RpcPanel from './lib/RpcPanel.svelte';
  import AppFrame from './lib/AppFrame.svelte';
  import { fetchServerInfo, fetchRpcLog, obsStreamURL, type ServerInfo } from './lib/api.js';
  import { ObsStream, type ObsEvent } from './lib/obs.js';
  import type { RpcEntry } from './lib/rpc.js';
  import type { HostRpcLogEntry } from './host/host-bridge.js';

  interface Props {
    /** API base URL — empty in production (served same-origin); set in tests. */
    base?: string;
    /** The App HTML to preview. Empty shows the App-frame empty state. */
    appHtml?: string;
  }

  let { base = '', appHtml = '' }: Props = $props();

  // -- server identity --
  let serverInfo = $state<ServerInfo | undefined>(undefined);

  // -- obs/v1 event stream --
  let events = $state<ObsEvent[]>([]);
  let eventsState = $state<PageStateValue>('loading');
  let errorCount = $state(0);
  let liveDot = $state(false);
  let stream: ObsStream | undefined;

  // -- JSON-RPC log --
  let rpcEntries = $state<RpcEntry[]>([]);
  let rpcState = $state<PageStateValue>('loading');

  // The inspector DetailRail tabs. Events + RPC are built in Phase 22; the rest
  // are scaffolded as Phase 23 placeholders so the rail is cleanly extensible.
  const tabs = ['Events', 'RPC', 'Fixtures', 'Tools', 'Verdicts', 'Tasks'];
  let activeTab = $state(0);

  function onHostRpc(entry: HostRpcLogEntry): void {
    rpcEntries = [
      ...rpcEntries,
      {
        id: `ui-${entry.at}-${rpcEntries.length}`,
        direction: entry.direction,
        method: entry.method,
        payload: entry.message,
        at: entry.at,
      },
    ];
    rpcState = 'ready';
  }

  function startStream(): void {
    eventsState = 'loading';
    stream?.close();
    stream = new ObsStream(obsStreamURL(base), {
      onEvent: (ev) => {
        events = [...events, ev];
        if (ev.error) errorCount += 1;
        eventsState = 'ready';
        liveDot = true;
      },
      onOpen: () => {
        // The stream is open but may carry no events yet — empty, not ready.
        if (events.length === 0) eventsState = 'empty';
      },
      onError: () => {
        eventsState = events.length > 0 ? 'ready' : 'error';
        liveDot = false;
      },
    });
    stream.open();
  }

  async function loadRpcLog(): Promise<void> {
    rpcState = 'loading';
    try {
      const log = await fetchRpcLog(base);
      rpcEntries = [...log, ...rpcEntries.filter((e) => e.id.startsWith('ui-'))];
      rpcState = rpcEntries.length > 0 ? 'ready' : 'empty';
    } catch {
      rpcState = rpcEntries.length > 0 ? 'ready' : 'error';
    }
  }

  onMount(async () => {
    try {
      serverInfo = await fetchServerInfo(base);
    } catch {
      serverInfo = { name: 'disconnected', version: '', transport: '' };
    }
    startStream();
    await loadRpcLog();
  });

  onDestroy(() => stream?.close());

  const connection = $derived(serverInfo ? 'connected' : 'connecting');
  const headerSubtitle = $derived(
    serverInfo
      ? `${serverInfo.name} v${serverInfo.version}`
      : 'connecting…',
  );
</script>

<AppShell>
  {#snippet header()}
    <PageHeader title="Dockyard Inspector" subtitle={headerSubtitle}>
      {#snippet status()}
        <StatusChip
          label={connection}
          tone={connection === 'connected' ? 'ok' : 'warn'}
          dot
        />
      {/snippet}
      {#snippet actions()}
        <span class="header-meta" data-testid="transport-label">
          {serverInfo?.transport ?? '—'}
        </span>
        <!-- Host / Display-mode controls — the deep behaviour is Phase 23
             (capability emulation, mode negotiation UI). The chips are shown
             read-only so the layout matches the approved mockup. -->
        <StatusChip label="Host: ChatGPT compatible" tone="neutral" />
        <StatusChip label="Display: inline" tone="neutral" />
      {/snippet}
    </PageHeader>
  {/snippet}

  {#snippet rail()}
    <DetailRail {tabs} active={activeTab} onTabChange={(i) => (activeTab = i)}>
      {#snippet children(index: number)}
        {#if index === 0}
          <RailCard title="Events">
            <EventsPanel
              {events}
              streamState={eventsState}
              onRetry={startStream}
            />
          </RailCard>
        {:else if index === 1}
          <RailCard title="RPC">
            <RpcPanel
              entries={rpcEntries}
              logState={rpcState}
              onRetry={loadRpcLog}
            />
          </RailCard>
        {:else}
          <RailCard title={tabs[index]}>
            <EmptyState
              title={`${tabs[index]} — coming in Phase 23`}
              description="The fixture switcher, tool list, drift verdicts, and task rendering land in the inspector's advanced phase."
            />
          </RailCard>
        {/if}
      {/snippet}
    </DetailRail>
  {/snippet}

  {#snippet footer()}
    <ConnectionFooter
      connection={connection}
      label={serverInfo?.name ?? 'inspector'}
      transport={serverInfo?.transport ?? ''}
      live={liveDot}
    >
      {events.length} events · {errorCount} errors
    </ConnectionFooter>
  {/snippet}

  <div class="preview-region">
    {#if appHtml}
      <AppFrame html={appHtml} appName={serverInfo?.name} onRpc={onHostRpc} />
    {:else}
      <EmptyState
        title="No App attached"
        description="Attach an MCP server that registers a ui:// App to preview it here."
      />
    {/if}
  </div>
</AppShell>

<style>
  .preview-region {
    height: 100%;
    min-height: 0;
  }
  .header-meta {
    font-size: var(--dy-font-size-sm, 0.875rem);
    color: var(--dy-color-text-muted, #71717a);
  }
</style>
