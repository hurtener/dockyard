/**
 * contracts.test.ts — the typed-contract path: a tool-result's
 * `structuredContent` is typed against the `contracts.ts` shape (P1), and the
 * view → host helpers (open-link, message, update-model-context, callContract).
 */
import { afterEach, describe, expect, expectTypeOf, it } from 'vitest';
import { createBridge } from '../bridge.js';
import {
  defineContract,
  type ContractInput,
  type ContractOutput,
  type ToolContract,
} from '../contracts.js';
import { HostNotification } from '../protocol.js';
import { HostHarness } from './harness.js';

/**
 * Stand-in for generated `contracts.ts` (Phase 06 owns generation — D-061).
 * It is hand-written here purely as a test fixture; it must satisfy the
 * `ToolContract` shape the bridge consumes.
 */
interface ListAccountsInput {
  query: string;
}
interface ListAccountsOutput {
  accounts: { id: string; name: string }[];
  total: number;
}
const contracts = {
  list_accounts: defineContract<ListAccountsInput, ListAccountsOutput>(
    'list_accounts',
  ),
} as const;

describe('contracts.ts — typed-contract shape', () => {
  it('a generated contract satisfies the ToolContract shape', () => {
    const c: ToolContract = contracts.list_accounts;
    expect(c.name).toBe('list_accounts');
  });

  it('ContractInput / ContractOutput extract the carried types', () => {
    expectTypeOf<ContractInput<typeof contracts.list_accounts>>().toEqualTypeOf<
      ListAccountsInput
    >();
    expectTypeOf<
      ContractOutput<typeof contracts.list_accounts>
    >().toEqualTypeOf<ListAccountsOutput>();
  });
});

describe('BridgeShell — typed structuredContent from a contract', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });
  function harness() {
    const h = new HostHarness();
    harnesses.push(h);
    return h;
  }

  it('surfaces a tool-result structuredContent typed by the contract', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    let captured: ListAccountsOutput | undefined;
    bridge.onToolResult<ContractOutput<typeof contracts.list_accounts>>((r) => {
      captured = r.structuredContent;
    });

    h.notify(HostNotification.toolResult, {
      content: [{ type: 'text', text: '2 accounts' }],
      structuredContent: {
        accounts: [
          { id: 'a1', name: 'Acme' },
          { id: 'a2', name: 'Globex' },
        ],
        total: 2,
      },
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(captured).toBeDefined();
    expect(captured!.total).toBe(2);
    expect(captured!.accounts).toHaveLength(2);
    // Type-level: the payload is the contract's output, not `unknown`.
    expectTypeOf(captured).toEqualTypeOf<ListAccountsOutput | undefined>();
  });

  it('callContract proxies a typed tools/call to the host', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    h.respondWith('tools/call', (req) => ({
      jsonrpc: '2.0',
      id: req.id,
      result: { structuredContent: { accounts: [], total: 0 } },
    }));

    const result = await bridge.callContract(contracts.list_accounts, {
      query: 'acme',
    });
    expect(result.structuredContent).toEqual({ accounts: [], total: 0 });

    const req = h.lastRequest('tools/call')!;
    expect(req.params).toMatchObject({
      name: 'list_accounts',
      arguments: { query: 'acme' },
    });
  });
});

describe('BridgeShell — view → host helpers', () => {
  let harnesses: HostHarness[] = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });
  function harness() {
    const h = new HostHarness();
    harnesses.push(h);
    return h;
  }

  it('openLink sends ui/open-link', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    await bridge.openLink('https://example.com');
    expect(h.lastRequest('ui/open-link')!.params).toEqual({
      url: 'https://example.com',
    });
  });

  it('sendMessage sends ui/message with role and content', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    await bridge.sendMessage('hello');
    expect(h.lastRequest('ui/message')!.params).toEqual({
      role: 'user',
      content: [{ type: 'text', text: 'hello' }],
    });
  });

  it('updateModelContext sends ui/update-model-context', async () => {
    const h = harness();
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    await bridge.connect();

    await bridge.updateModelContext({ content: [{ type: 'text', text: 'note' }] });
    expect(h.lastRequest('ui/update-model-context')!.params).toEqual({
      content: [{ type: 'text', text: 'note' }],
    });
  });
});
