<script lang="ts">
  /**
   * App.svelte — the inspector page.
   *
   * Built to design-spec.md §4 and the approved mockups/inspector.png: one
   * `AppShell` — a `PageHeader` (logo + server name/version/transport + the
   * Host capability-set control + Display-mode), the App preview frame, the
   * tabbed `DetailRail` (Events / RPC / Fixtures / Tools / Verdicts / Tasks /
   * Analytics), and a `ConnectionFooter`.
   *
   * Phase 22 built the Events + RPC tabs and the App frame; Phase 23 fills the
   * Fixtures / Tools / Verdicts / Tasks / Analytics tabs and the Host control.
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
    LoadingState,
    ErrorState,
    type PageStateValue,
  } from '@dockyard/ui';
  import EventsPanel from './lib/EventsPanel.svelte';
  import RpcPanel from './lib/RpcPanel.svelte';
  import AppFrame from './lib/AppFrame.svelte';
  import FixturesPanel from './lib/FixturesPanel.svelte';
  import ToolsPanel from './lib/ToolsPanel.svelte';
  import VerdictsPanel from './lib/VerdictsPanel.svelte';
  import TasksPanel from './lib/TasksPanel.svelte';
  import AnalyticsPanel from './lib/AnalyticsPanel.svelte';
  import HostControl from './lib/HostControl.svelte';
  import {
    fetchServerInfo,
    fetchRpcLog,
    fetchVerdicts,
    fetchContracts,
    fetchApps,
    fetchProjectFixtures,
    obsStreamURL,
    type ServerInfo,
    type VerdictRow,
    type AppPreview,
    type ToolInvokeResult,
  } from './lib/api.js';
  import { ObsStream, type ObsEvent } from './lib/obs.js';
  import type { RpcEntry } from './lib/rpc.js';
  import type { HostRpcLogEntry, CallToolFixtureResult } from './host/host-bridge.js';
  import type { ToolContract } from './lib/contracts.js';
  import type { Fixture, ProjectFixture } from './lib/fixtures.js';
  import {
    fullCapabilitySet,
    hostContextFor,
    hostCapabilitiesFor,
    type CapabilitySet,
  } from './lib/capability.js';
  // The Dockyard wordmark. Imported through Vite's asset pipeline so the
  // bundler emits a hashed URL and the inspector backend serves it from the
  // embedded dist/ tree (no extra HTTP route needed). The asset is the
  // canonical wordmark from docs/design/ (Phase 10a design-system source).
  import dockyardLogo from './assets/dockyard-logo.png';

  interface Props {
    /** API base URL — empty in production (served same-origin); set in tests. */
    base?: string;
    /**
     * A test-only App HTML override. When set, the App-frame previews it
     * directly and the `/api/apps` fetch is skipped. In production this is
     * unset and the inspector loads the attached server's ui:// App(s) from
     * the backend's `/api/apps` endpoint.
     */
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

  // -- verdicts --
  let verdicts = $state<VerdictRow[]>([]);
  let verdictsState = $state<PageStateValue>('loading');

  // -- generated contracts (drive the fixture switcher) --
  let contracts = $state<ToolContract[]>([]);
  let contractsState = $state<PageStateValue>('loading');

  // -- on-disk project fixtures (Phase 24, D-126) --
  let projectFixtures = $state<ProjectFixture[]>([]);

  // -- capability-set emulation (the Host control) --
  let capabilities = $state<CapabilitySet>(fullCapabilitySet());

  // -- fixture switcher state --
  let activeFixture = $state<CallToolFixtureResult | undefined>(undefined);

  // -- operator-initiated invoke result (D-131) --
  // Filled when the ToolsPanel's Invoke succeeds. Threaded into the AppFrame
  // alongside (and superseding) the active fixture so the App preview
  // re-renders with the operator's typed parameters — the same pushToolResult
  // path the fixture switcher uses (D-129).
  let invokeResult = $state<CallToolFixtureResult | undefined>(undefined);

  // -- App preview (the attached server's ui:// Apps) --
  let apps = $state<AppPreview[]>([]);
  let appsState = $state<PageStateValue>('loading');
  let appsError = $state('');
  // The HTML the App-frame renders: the test override when set, otherwise the
  // first App discovered from the attached server's ui:// resources.
  const previewHtml = $derived(appHtml !== '' ? appHtml : (apps[0]?.html ?? ''));
  const previewName = $derived(apps[0]?.name ?? serverInfo?.name);

  // The inspector DetailRail tabs.
  const tabs = ['Events', 'RPC', 'Fixtures', 'Tools', 'Verdicts', 'Tasks', 'Analytics'];
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

  async function loadVerdicts(): Promise<void> {
    verdictsState = 'loading';
    try {
      verdicts = await fetchVerdicts(base);
      verdictsState = verdicts.length > 0 ? 'ready' : 'empty';
    } catch {
      verdictsState = 'error';
    }
  }

  async function loadContracts(): Promise<void> {
    contractsState = 'loading';
    try {
      contracts = await fetchContracts(base);
      contractsState = contracts.length > 0 ? 'ready' : 'empty';
    } catch {
      contractsState = contracts.length > 0 ? 'ready' : 'error';
    }
  }

  async function loadProjectFixtures(): Promise<void> {
    try {
      projectFixtures = await fetchProjectFixtures(base);
    } catch {
      // The on-disk fixtures are an *enhancement* — a fetch failure is a
      // silent fallback to the schema-derived synthetic fixtures the
      // FixturesPanel ships, not a user-facing error.
      projectFixtures = [];
    }
  }

  /**
   * Loads the attached server's renderable Apps from the backend's
   * `/api/apps` endpoint. A test `appHtml` override skips the fetch. The
   * preview region routes a real four-state: loading while the read is in
   * flight, empty when the server registers no ui:// App, error (with a
   * working retry) when discovery fails, ready once an App is rendered.
   */
  async function loadApps(): Promise<void> {
    if (appHtml !== '') {
      appsState = 'ready';
      return;
    }
    appsState = 'loading';
    appsError = '';
    try {
      apps = await fetchApps(base);
      appsState = apps.length > 0 ? 'ready' : 'empty';
    } catch (err) {
      appsError = err instanceof Error ? err.message : 'App discovery failed';
      appsState = 'error';
    }
  }

  /** Applies a fixture from the switcher — feeds the App synthetic content. */
  function applyFixture(fixture: Fixture, _contract: ToolContract): void {
    activeFixture = {
      structuredContent: fixture.structuredContent,
      text: fixture.text,
      error: fixture.error,
    };
    // A new fixture supersedes any prior operator invocation result — the
    // App preview shows one source of truth at a time.
    invokeResult = undefined;
  }

  /**
   * Forwards an operator-initiated tools/call result (D-131) into the App
   * preview. Same shape the fixture switcher feeds through `pushToolResult`
   * (D-129) — so a real invocation re-renders the App with the operator's
   * parameters. A tool-level error sets the error channel; transport failures
   * never reach here (they surface in ToolsPanel's own ErrorState region).
   */
  function applyInvokeResult(result: ToolInvokeResult, _contract: ToolContract): void {
    invokeResult = {
      structuredContent: result.structuredContent,
      text: undefined,
      error: result.isError
        ? { code: -32000, message: 'tool returned isError=true' }
        : undefined,
    };
  }

  /** Re-runs the App handshake against the new emulated capability set. */
  function onCapabilityChange(next: CapabilitySet): void {
    capabilities = next;
  }

  onMount(async () => {
    try {
      serverInfo = await fetchServerInfo(base);
    } catch {
      serverInfo = { name: 'disconnected', version: '', transport: '' };
    }
    startStream();
    await Promise.all([loadRpcLog(), loadVerdicts(), loadContracts(), loadApps(), loadProjectFixtures()]);
  });

  onDestroy(() => stream?.close());

  const connection = $derived(serverInfo ? 'connected' : 'connecting');
  const headerSubtitle = $derived(
    serverInfo ? `${serverInfo.name} v${serverInfo.version}` : 'connecting…',
  );
  const emulatedHostContext = $derived(hostContextFor(capabilities));
  const emulatedHostCapabilities = $derived(hostCapabilitiesFor(capabilities));
</script>

<AppShell>
  {#snippet header()}
    <PageHeader title="Dockyard Inspector" subtitle={headerSubtitle}>
      {#snippet lead()}
        <img
          class="header-logo"
          src={dockyardLogo}
          alt="Dockyard"
          data-testid="header-logo"
        />
      {/snippet}
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
        <HostControl
          capabilities={capabilities}
          onChange={onCapabilityChange}
        />
        <StatusChip
          label={`Display: ${capabilities.displayMode}`}
          tone="neutral"
        />
      {/snippet}
    </PageHeader>
  {/snippet}

  {#snippet rail()}
    <DetailRail {tabs} active={activeTab} onTabChange={(i) => (activeTab = i)}>
      {#snippet children(index: number)}
        {#if index === 0}
          <RailCard title="Events">
            <EventsPanel {events} streamState={eventsState} onRetry={startStream} />
          </RailCard>
        {:else if index === 1}
          <RailCard title="RPC">
            <RpcPanel entries={rpcEntries} logState={rpcState} onRetry={loadRpcLog} />
          </RailCard>
        {:else if index === 2}
          <RailCard title="Fixtures">
            <FixturesPanel
              {contracts}
              {projectFixtures}
              panelState={contractsState}
              onRetry={loadContracts}
              onApply={applyFixture}
            />
          </RailCard>
        {:else if index === 3}
          <RailCard title="Tools / Resources">
            <ToolsPanel
              {contracts}
              panelState={contractsState}
              onRetry={loadContracts}
              onInvokeResult={applyInvokeResult}
              {base}
            />
          </RailCard>
        {:else if index === 4}
          <RailCard title="Verdicts">
            <VerdictsPanel
              {verdicts}
              panelState={verdictsState}
              onRetry={loadVerdicts}
            />
          </RailCard>
        {:else if index === 5}
          <RailCard title="Tasks">
            <TasksPanel {events} streamState={eventsState} onRetry={startStream} />
          </RailCard>
        {:else if index === 6}
          <RailCard title="Analytics">
            <AnalyticsPanel {events} streamState={eventsState} onRetry={startStream} />
          </RailCard>
        {:else}
          <RailCard title={tabs[index]}>
            <EmptyState title="Nothing here" description="No panel for this tab." />
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

  <div class="preview-region" data-state={appsState} data-testid="preview-state">
    {#if previewHtml !== ''}
      <AppFrame
        html={previewHtml}
        appName={previewName}
        onRpc={onHostRpc}
        hostContext={emulatedHostContext}
        hostCapabilities={emulatedHostCapabilities}
        fixtureResult={invokeResult ?? activeFixture}
        pushToolResult={invokeResult?.structuredContent ?? activeFixture?.structuredContent}
      />
    {:else if appsState === 'loading'}
      <LoadingState message="Reading the attached server's ui:// App resources…" />
    {:else if appsState === 'error'}
      <ErrorState
        title="Could not load the server's Apps"
        description={appsError ||
          'The inspector could not read the attached server. Retry.'}
        onretry={loadApps}
      />
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
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }
  /*
   * The Dockyard wordmark sits in PageHeader's `lead` slot. Its height tracks
   * the design system's title scale so the mark and the title sit on the same
   * visual baseline; width is intrinsic to preserve the wordmark's aspect.
   */
  .header-logo {
    display: block;
    height: 32px;
    width: auto;
    object-fit: contain;
    /* The PNG is rasterised — render it crisply on HiDPI displays. */
    image-rendering: -webkit-optimize-contrast;
  }
</style>
