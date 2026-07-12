import { afterEach, describe, expect, it } from 'vitest';
import { createBridge } from '../bridge.js';
import { HostHarness } from './harness.js';

describe('modern input lifecycles', () => {
  const harnesses: HostHarness[] = [];
  afterEach(() => harnesses.splice(0).forEach((h) => h.close()));

  function setup() {
    const h = new HostHarness();
    harnesses.push(h);
    const bridge = createBridge({ peer: h.peer, source: h.source, styleTarget: null });
    return { h, bridge };
  }

  it('retries tools/call with core MRTR continuation fields', async () => {
    const { h, bridge } = setup();
    await bridge.connect();
    h.respondWith('tools/call', (req) => ({ jsonrpc: '2.0', id: req.id, result: { content: [] } }));
    const result = bridge.retryToolCall(
      'review',
      { title: 'Ship?' },
      { requestState: 'opaque-state' },
      { approval: { action: 'accept' } },
    );
    await result;
    expect(h.lastRequest('tools/call')?.params).toEqual({
      name: 'review',
      arguments: { title: 'Ship?' },
      requestState: 'opaque-state',
      inputResponses: { approval: { action: 'accept' } },
    });
  });

  it('submits task input through tasks/update, not tools/call', async () => {
    const { h, bridge } = setup();
    await bridge.connect();
    h.respondWith('tasks/update', (req) => ({
      jsonrpc: '2.0', id: req.id, result: { resultType: 'complete' },
    }));
    await bridge.updateTask({
      taskId: 'task-1',
      inputResponses: { approval: { approved: true } },
    });
    expect(h.lastRequest('tasks/update')?.params).toEqual({
      taskId: 'task-1',
      inputResponses: { approval: { approved: true } },
    });
  });
});
