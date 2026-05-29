/**
 * panels.test.ts — the Phase 23 inspector DetailRail panels + Host control.
 *
 * Confirms the Fixtures / Tools / Verdicts / Tasks / Analytics panels and the
 * HostControl render through the four-state PageState and compose the
 * dockyard-ui inventory (CLAUDE.md §20 — empty + error states mandatory).
 */
import { describe, expect, it } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import FixturesPanel from '../lib/FixturesPanel.svelte';
import ToolsPanel from '../lib/ToolsPanel.svelte';
import VerdictsPanel from '../lib/VerdictsPanel.svelte';
import TasksPanel from '../lib/TasksPanel.svelte';
import AnalyticsPanel from '../lib/AnalyticsPanel.svelte';
import HostControl from '../lib/HostControl.svelte';
import { fullCapabilitySet, type CapabilitySet } from '../lib/capability.js';
import type { ToolContract } from '../lib/contracts.js';
import type { ObsEvent } from '../lib/obs.js';
import type { VerdictRow } from '../lib/api.js';

const contract: ToolContract = {
  name: 'revenue',
  outputSchema: {
    type: 'object',
    properties: { total: { type: 'number' } },
  },
};

describe('FixturesPanel', () => {
  it('renders the empty state when no contracts are present', () => {
    const { getByText } = render(FixturesPanel, {
      props: { contracts: [], panelState: 'empty' },
    });
    expect(getByText(/No contracts/)).toBeTruthy();
  });

  it('renders the six fixture chips for a contract', () => {
    const { getByTestId } = render(FixturesPanel, {
      props: { contracts: [contract], panelState: 'ready' },
    });
    for (const kind of ['happy', 'empty', 'error', 'permission', 'slow', 'large']) {
      expect(getByTestId(`fixture-${kind}`)).toBeTruthy();
    }
  });

  it('applies a fixture — selecting a fixture drives the App UI state', async () => {
    let applied = false;
    const { getByTestId } = render(FixturesPanel, {
      props: {
        contracts: [contract],
        panelState: 'ready',
        onApply: () => {
          applied = true;
        },
      },
    });
    await fireEvent.click(getByTestId('fixture-error'));
    expect(applied).toBe(true);
  });
});

describe('ToolsPanel', () => {
  it('renders the empty state when there are no tools', () => {
    const { getByText } = render(ToolsPanel, {
      props: { contracts: [], panelState: 'empty' },
    });
    expect(getByText(/No tools/)).toBeTruthy();
  });

  it('lists the server tools in a DataTable', () => {
    const { getByText } = render(ToolsPanel, {
      props: { contracts: [contract], panelState: 'ready' },
    });
    expect(getByText('revenue')).toBeTruthy();
  });
});

describe('VerdictsPanel', () => {
  it('renders the mandatory empty state', () => {
    const { getByText } = render(VerdictsPanel, {
      props: { verdicts: [], panelState: 'empty' },
    });
    expect(getByText(/No verdicts/)).toBeTruthy();
  });

  it('renders the mandatory error state', () => {
    const { getByText } = render(VerdictsPanel, {
      props: { verdicts: [], panelState: 'error' },
    });
    expect(getByText(/Verdicts unavailable/)).toBeTruthy();
  });

  it('renders verdict rows as StatusChips', () => {
    const verdicts: VerdictRow[] = [
      { check: 'stale-codegen', severity: 'error', message: 'schema stale' },
      { check: 'spec-compliance', severity: 'ok', message: 'spec OK' },
    ];
    const { getAllByTestId, getByText } = render(VerdictsPanel, {
      props: { verdicts, panelState: 'ready' },
    });
    expect(getAllByTestId('verdict-row').length).toBe(2);
    expect(getByText('schema stale')).toBeTruthy();
  });
});

describe('TasksPanel', () => {
  it('renders the empty state when no task events have arrived', () => {
    const { getByText } = render(TasksPanel, {
      props: { events: [], streamState: 'ready' },
    });
    expect(getByText(/No tasks yet/)).toBeTruthy();
  });

  it('renders a task lifecycle Timeline', () => {
    const events: ObsEvent[] = [
      {
        schema_version: 'dockyard.obs/v1',
        id: 'e1',
        timestamp: '2026-05-22T10:00:00Z',
        server_id: 's',
        trace_id: 't',
        span_id: 's',
        kind: 'task.progress',
        phase: 'emit',
        payload: { task_id: 'task-9', status: 'working' },
      },
    ];
    const { getByTestId } = render(TasksPanel, {
      props: { events, streamState: 'ready' },
    });
    expect(getByTestId('task-lifecycle')).toBeTruthy();
  });
});

describe('AnalyticsPanel', () => {
  it('renders the empty state when no tool calls have been observed', () => {
    const { getByText } = render(AnalyticsPanel, {
      props: { events: [], streamState: 'ready' },
    });
    expect(getByText(/No tool calls yet/)).toBeTruthy();
  });

  it('renders per-tool analytics from the obs stream', () => {
    const events: ObsEvent[] = [
      {
        schema_version: 'dockyard.obs/v1',
        id: 'e1',
        timestamp: '2026-05-22T10:00:00Z',
        server_id: 's',
        trace_id: 't',
        span_id: 's',
        kind: 'tool.call',
        phase: 'end',
        duration_ms: 120,
        payload: { tool: 'report' },
      },
    ];
    const { getByText } = render(AnalyticsPanel, {
      props: { events, streamState: 'ready' },
    });
    expect(getByText('report')).toBeTruthy();
  });
});

describe('HostControl', () => {
  it('toggles a capability — Apps off degrades the emulated host', async () => {
    let next: CapabilitySet | undefined;
    const { getByTestId } = render(HostControl, {
      props: {
        capabilities: fullCapabilitySet(),
        onChange: (c: CapabilitySet) => {
          next = c;
        },
      },
    });
    await fireEvent.click(getByTestId('host-trigger'));
    await fireEvent.click(getByTestId('toggle-apps'));
    expect(next?.apps).toBe(false);
  });

  it('toggles the Tasks capability', async () => {
    let next: CapabilitySet | undefined;
    const { getByTestId } = render(HostControl, {
      props: {
        capabilities: fullCapabilitySet(),
        onChange: (c: CapabilitySet) => {
          next = c;
        },
      },
    });
    await fireEvent.click(getByTestId('host-trigger'));
    await fireEvent.click(getByTestId('toggle-tasks'));
    expect(next?.tasks).toBe(false);
  });

  it('applies a preset as a starting toggle-set', async () => {
    let next: CapabilitySet | undefined;
    const { getByTestId } = render(HostControl, {
      props: {
        capabilities: fullCapabilitySet(),
        onChange: (c: CapabilitySet) => {
          next = c;
        },
      },
    });
    await fireEvent.click(getByTestId('host-trigger'));
    await fireEvent.click(getByTestId('preset-No Apps extension'));
    expect(next?.apps).toBe(false);
  });
});
