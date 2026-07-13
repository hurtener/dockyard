/**
 * invoke.test.ts — covers the operator-initiated tools/call surface (D-131):
 * the `invokeTool` API client and the ToolsPanel form's invoke flow.
 *
 * The fetch boundary is the mock target — exercises a realistic POST body
 * and a realistic 200 / 502 / 400 response shape.
 */
import { describe, expect, it, vi } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import ToolsPanel from '../lib/ToolsPanel.svelte';
import { invokeTool, postElicitationResponse } from '../lib/api.js';
import type { ToolContract } from '../lib/contracts.js';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('invokeTool', () => {
  it('POSTs the request body and returns the parsed result', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        jsonResponse({ structuredContent: { ok: true }, isError: false }),
      );
    const result = await invokeTool(
      { tool: 'echo', arguments: { name: 'world' } },
      '',
      fetchImpl as unknown as typeof fetch,
    );
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/tools/invoke',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tool: 'echo', arguments: { name: 'world' } }),
      }),
    );
    expect(result.structuredContent).toEqual({ ok: true });
    expect(result.isError).toBe(false);
  });

  it('throws with the typed error message on a non-200', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: 'tool not found' }, 502));
    await expect(
      invokeTool({ tool: 'missing', arguments: {} }, '', fetchImpl as unknown as typeof fetch),
    ).rejects.toThrow(/tool not found/);
  });

  it('mirrors isError back without conflating it with a transport failure', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        jsonResponse({ structuredContent: { err: 1 }, isError: true }),
      );
    const result = await invokeTool(
      { tool: 'failing', arguments: {} },
      '',
      fetchImpl as unknown as typeof fetch,
    );
    expect(result.isError).toBe(true);
    expect(result.structuredContent).toEqual({ err: 1 });
  });

  it('preserves arbitrary structured content and MRTR continuation fields', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse({
      structuredContent: ['arbitrary', 7],
      inputRequests: { approval: { method: 'elicitation/create' } },
      requestState: 'opaque-state',
    }));
    const result = await invokeTool(
      { tool: 'approve', arguments: {}, inputResponses: { approval: true }, requestState: 'opaque-state' },
      '', fetchImpl as unknown as typeof fetch,
    );
    expect(result.structuredContent).toEqual(['arbitrary', 7]);
    expect(result.inputRequests).toEqual({ approval: { method: 'elicitation/create' } });
    expect(result.requestState).toBe('opaque-state');
    expect(JSON.parse(fetchImpl.mock.calls[0][1].body as string)).toMatchObject({
      inputResponses: { approval: true }, requestState: 'opaque-state',
    });
  });
});

describe('task input delivery', () => {
  it('sends modern protocol and keyed inputResponses', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse({ taskId: 'task-1', delivered: true }));
    await postElicitationResponse({
      taskId: 'task-1',
      protocol: '2026-07-28',
      inputResponses: { approval: { action: 'accept', content: { approved: true } } },
    }, '', fetchImpl as unknown as typeof fetch);
    expect(JSON.parse(fetchImpl.mock.calls[0][1].body as string)).toEqual({
      taskId: 'task-1',
      protocol: '2026-07-28',
      inputResponses: { approval: { action: 'accept', content: { approved: true } } },
    });
  });
});

describe('ToolsPanel — invoke flow (D-131)', () => {
  const contract: ToolContract = {
    name: 'greet',
    description: 'Greet the supplied name.',
    inputSchema: {
      type: 'object',
      required: ['greeting'],
      properties: { greeting: { type: 'string' } },
    },
    outputSchema: {
      type: 'object',
      properties: { greeted: { type: 'string' } },
    },
  };

  it('renders a form generated from the selected tool input schema', async () => {
    const { getByText, getByTestId, queryByTestId } = render(ToolsPanel, {
      props: { contracts: [contract], panelState: 'ready', fetchImpl: fetch },
    });
    // Pre-selection, the panel shows the row but no form yet.
    expect(queryByTestId('invoke-form')).toBeNull();
    await fireEvent.click(getByText('greet'));
    expect(getByTestId('invoke-form')).toBeTruthy();
    expect(getByTestId('invoke-greeting')).toBeTruthy();
    expect(getByTestId('invoke-submit')).toBeTruthy();
  });

  it('blocks invoke and surfaces a per-field error on a missing required field', async () => {
    const fetchImpl = vi.fn();
    const { getByText, getByTestId, container } = render(ToolsPanel, {
      props: {
        contracts: [contract],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('greet'));
    const form = container.querySelector('form');
    expect(form).not.toBeNull();
    await fireEvent.submit(form!);
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(getByTestId('invoke-greeting-error')).toBeTruthy();
  });

  it('invokes the tool and threads the result through onInvokeResult', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        jsonResponse({
          structuredContent: { greeted: 'hello, operator' },
          isError: false,
        }),
      );
    const onInvokeResult = vi.fn();
    const { getByText, getByTestId, container } = render(ToolsPanel, {
      props: {
        contracts: [contract],
        panelState: 'ready',
        onInvokeResult,
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('greet'));
    const greeting = getByTestId('invoke-greeting') as HTMLInputElement;
    await fireEvent.input(greeting, { target: { value: 'operator' } });
    await fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(onInvokeResult).toHaveBeenCalled());
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/tools/invoke',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          tool: 'greet',
          arguments: { greeting: 'operator' },
        }),
      }),
    );
    const [result, passedContract] = onInvokeResult.mock.calls[0];
    expect(result.structuredContent).toEqual({ greeted: 'hello, operator' });
    expect(passedContract.name).toBe('greet');
  });

  it('renders ErrorState on a transport failure', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: 'server unreachable' }, 502));
    const { getByText, getByTestId, container } = render(ToolsPanel, {
      props: {
        contracts: [contract],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('greet'));
    await fireEvent.input(getByTestId('invoke-greeting'), {
      target: { value: 'world' },
    });
    await fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByTestId('invoke-error-region')).toBeTruthy());
    expect(getByTestId('invoke-error-region').textContent).toMatch(/server unreachable/);
  });

  it('renders MRTR input and retries with inputResponses and requestState', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ inputRequests: { approval: { prompt: 'Approve?' } }, requestState: 'state-1' }))
      .mockResolvedValueOnce(jsonResponse({ structuredContent: 'approved' }));
    const { getByText, getByTestId, container } = render(ToolsPanel, {
      props: { contracts: [contract], panelState: 'ready', fetchImpl: fetchImpl as unknown as typeof fetch },
    });
    await fireEvent.click(getByText('greet'));
    await fireEvent.input(getByTestId('invoke-greeting'), { target: { value: 'operator' } });
    await fireEvent.submit(container.querySelector('form')!);
    await waitFor(() => expect(getByTestId('mrtr-ready')).toBeTruthy());
    await fireEvent.input(getByTestId('mrtr-response-approval'), { target: { value: '{"approved":true}' } });
    await fireEvent.click(getByTestId('mrtr-retry'));
    await waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(2));
    expect(JSON.parse(fetchImpl.mock.calls[1][1].body as string)).toMatchObject({
      inputResponses: { approval: { approved: true } }, requestState: 'state-1',
    });
  });
});
