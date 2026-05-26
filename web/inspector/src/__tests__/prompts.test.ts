/**
 * prompts.test.ts — covers the v1.1 Wave A Prompts panel surface
 * (closes D-151; D-163):
 *
 *   - the typed `parsePrompts` / `parsePromptGetResponse` parsers;
 *   - the `fetchPrompts` / `invokePrompt` API clients (fetch mock at
 *     the boundary);
 *   - the PromptsPanel.svelte four-state flow + the argument-form
 *     generation + the invoke flow + the error region.
 *
 * Mirrors the patterns in panels.test.ts and invoke.test.ts so a future
 * inspector panel addition (Phase N+1) finds the same shape.
 */
import { describe, expect, it, vi } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import PromptsPanel from '../lib/PromptsPanel.svelte';
import {
  parsePrompts,
  parsePromptGetResponse,
  type PromptInfo,
} from '../lib/prompts.js';
import { fetchPrompts, invokePrompt } from '../lib/api.js';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('parsePrompts', () => {
  it('returns the empty list for non-array input', () => {
    expect(parsePrompts(null)).toEqual([]);
    expect(parsePrompts({})).toEqual([]);
    expect(parsePrompts(42)).toEqual([]);
  });

  it('skips entries without a name', () => {
    const got = parsePrompts([
      { title: 'no name' },
      { name: '' },
      { name: 'ok' },
    ]);
    expect(got.length).toBe(1);
    expect(got[0].name).toBe('ok');
  });

  it('parses the full PromptInfo shape including arguments', () => {
    const got = parsePrompts([
      {
        name: 'summarise',
        title: 'Summarise',
        description: 'd',
        arguments: [
          { name: 'passage', required: true, description: 'p' },
          { name: 'audience' },
        ],
      },
    ]);
    expect(got).toEqual<PromptInfo[]>([
      {
        name: 'summarise',
        title: 'Summarise',
        description: 'd',
        arguments: [
          { name: 'passage', title: undefined, description: 'p', required: true },
          { name: 'audience', title: undefined, description: undefined, required: false },
        ],
      },
    ]);
  });
});

describe('parsePromptGetResponse', () => {
  it('returns {messages: []} for a non-object input', () => {
    expect(parsePromptGetResponse(null)).toEqual({ messages: [] });
    expect(parsePromptGetResponse('nope')).toEqual({ messages: [] });
  });

  it('parses messages + description + error', () => {
    const got = parsePromptGetResponse({
      description: 'rendered',
      messages: [
        { role: 'system', text: 'You are careful.' },
        { role: 'user', text: 'Summarise: hello' },
        { role: 42, text: 'ignored role becomes blank' },
      ],
      error: 'some warning',
    });
    expect(got.description).toBe('rendered');
    expect(got.error).toBe('some warning');
    expect(got.messages.length).toBe(3);
    expect(got.messages[2].role).toBe('');
  });
});

describe('fetchPrompts', () => {
  it('parses a 200 array response', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse([{ name: 'p', arguments: [{ name: 'a', required: true }] }]),
    );
    const got = await fetchPrompts('', fetchImpl as unknown as typeof fetch);
    expect(fetchImpl).toHaveBeenCalledWith('/api/prompts');
    expect(got.length).toBe(1);
    expect(got[0].arguments[0].required).toBe(true);
  });

  it('throws on a non-200, prefering the typed error message', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: 'detached' }, 502));
    await expect(
      fetchPrompts('', fetchImpl as unknown as typeof fetch),
    ).rejects.toThrow(/detached/);
  });
});

describe('invokePrompt', () => {
  it('POSTs the request body and returns the parsed result', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse({
        description: 'd',
        messages: [{ role: 'user', text: 'hi' }],
      }),
    );
    const got = await invokePrompt(
      { name: 'greet', arguments: { who: 'world' } },
      '',
      fetchImpl as unknown as typeof fetch,
    );
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/prompts/get',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'greet', arguments: { who: 'world' } }),
      }),
    );
    expect(got.messages.length).toBe(1);
    expect(got.messages[0].role).toBe('user');
  });

  it('throws on a 502 transport-level failure', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: 'server down' }, 502));
    await expect(
      invokePrompt({ name: 'x' }, '', fetchImpl as unknown as typeof fetch),
    ).rejects.toThrow(/server down/);
  });

  it('mirrors a server-side error back without conflating it with transport failure', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse({ messages: [], error: 'unknown prompt' }),
    );
    const got = await invokePrompt(
      { name: 'missing' },
      '',
      fetchImpl as unknown as typeof fetch,
    );
    expect(got.error).toBe('unknown prompt');
  });
});

describe('PromptsPanel — four-state flow', () => {
  const prompt: PromptInfo = {
    name: 'summarize_for_review',
    title: 'Summarise for engineering review',
    description: 'Two-sentence summary for a peer reviewer.',
    arguments: [
      { name: 'passage', required: true, description: 'The passage' },
      { name: 'audience', required: false },
    ],
  };

  it('renders the empty state when no prompts are present', () => {
    const { getByText } = render(PromptsPanel, {
      props: { prompts: [], panelState: 'empty' },
    });
    expect(getByText(/No prompts/)).toBeTruthy();
  });

  it('lists the server prompts in a DataTable', () => {
    const { getByText } = render(PromptsPanel, {
      props: { prompts: [prompt], panelState: 'ready' },
    });
    expect(getByText('summarize_for_review')).toBeTruthy();
  });

  it('renders the argument form when a prompt is selected', async () => {
    const { getByText, getByTestId, queryByTestId } = render(PromptsPanel, {
      props: { prompts: [prompt], panelState: 'ready', fetchImpl: fetch },
    });
    expect(queryByTestId('prompt-invoke-form')).toBeNull();
    await fireEvent.click(getByText('summarize_for_review'));
    expect(getByTestId('prompt-invoke-form')).toBeTruthy();
    expect(getByTestId('prompt-arg-passage')).toBeTruthy();
    expect(getByTestId('prompt-arg-audience')).toBeTruthy();
    expect(getByTestId('prompt-invoke-submit')).toBeTruthy();
  });

  it('blocks invoke and surfaces a per-field error on a missing required field', async () => {
    const fetchImpl = vi.fn();
    const { getByText, getByTestId, container } = render(PromptsPanel, {
      props: {
        prompts: [prompt],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('summarize_for_review'));
    const form = container.querySelector('form');
    expect(form).not.toBeNull();
    await fireEvent.submit(form!);
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(getByTestId('prompt-arg-passage-error')).toBeTruthy();
  });

  it('invokes the prompt and renders the messages', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse({
        messages: [
          { role: 'system', text: 'You are careful.' },
          { role: 'user', text: 'Please summarise: hello' },
        ],
      }),
    );
    const { getByText, getByTestId, container } = render(PromptsPanel, {
      props: {
        prompts: [prompt],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('summarize_for_review'));
    const passage = getByTestId('prompt-arg-passage') as HTMLTextAreaElement;
    await fireEvent.input(passage, { target: { value: 'hello' } });
    const form = container.querySelector('form');
    await fireEvent.submit(form!);
    await waitFor(() => expect(getByTestId('prompt-invoke-result')).toBeTruthy());
    expect(getByTestId('prompt-messages')).toBeTruthy();
    expect(getByTestId('prompt-message-0')).toBeTruthy();
    expect(getByTestId('prompt-message-1')).toBeTruthy();
  });

  it('renders the ErrorState region on a transport failure (the four-state error)', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: 'server down' }, 502));
    const { getByText, getByTestId, container } = render(PromptsPanel, {
      props: {
        prompts: [prompt],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('summarize_for_review'));
    const passage = getByTestId('prompt-arg-passage') as HTMLTextAreaElement;
    await fireEvent.input(passage, { target: { value: 'hello' } });
    const form = container.querySelector('form');
    await fireEvent.submit(form!);
    await waitFor(() =>
      expect(getByTestId('prompt-invoke-error-region')).toBeTruthy(),
    );
  });

  it('surfaces a server-side prompts/get error as a result-region error (200-with-error)', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse({ messages: [], error: 'argument validation failed' }),
    );
    const { getByText, getByTestId, container } = render(PromptsPanel, {
      props: {
        prompts: [prompt],
        panelState: 'ready',
        fetchImpl: fetchImpl as unknown as typeof fetch,
      },
    });
    await fireEvent.click(getByText('summarize_for_review'));
    const passage = getByTestId('prompt-arg-passage') as HTMLTextAreaElement;
    await fireEvent.input(passage, { target: { value: 'hello' } });
    const form = container.querySelector('form');
    await fireEvent.submit(form!);
    await waitFor(() => expect(getByTestId('prompt-invoke-result')).toBeTruthy());
    expect(getByTestId('prompt-invoke-result').textContent).toMatch(/argument validation/);
  });
});
