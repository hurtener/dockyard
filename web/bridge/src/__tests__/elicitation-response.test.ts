/**
 * elicitation-response.test.ts — the View → host elicitation-response
 * notification round-trip (RFC §8.4, §8.6; Phase 25 / D-134).
 *
 * Proves the bridge's `sendElicitationResponse` View helper posts the
 * `ui/notifications/elicitation-response` notification with the right
 * params shape, and that the host receives it verbatim. The test runs
 * over a real `MessageChannel` — the same wire the inspector's
 * host-bridge will receive over.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { createBridge } from '../bridge.js';
import {
  DockyardExtMethod,
  type ElicitationResponseParams,
} from '../dockyard-ext.js';
import {
  type JsonRpcMessage,
  type JsonRpcNotification,
} from '../protocol.js';
import { portAsMessageSource, type MessageSink } from '../transport.js';
import { HostHarness } from './harness.js';

/**
 * A small harness extension that captures notifications (the base harness
 * captures only requests — notifications are exactly what we want to
 * assert on for D-134).
 */
class NotificationCapturingChannel {
  readonly peer: MessageSink;
  readonly source: ReturnType<typeof portAsMessageSource>;
  readonly notifications: JsonRpcNotification[] = [];
  private readonly channel = new MessageChannel();
  private readonly hostPort: MessagePort;

  constructor() {
    const bridgePort = this.channel.port1;
    this.hostPort = this.channel.port2;
    bridgePort.start();
    this.hostPort.start();
    this.peer = { postMessage: (m: unknown) => bridgePort.postMessage(m) };
    this.source = portAsMessageSource(bridgePort);
    this.hostPort.addEventListener('message', (ev) => this.onMessage(ev.data));
  }

  close(): void {
    this.channel.port1.close();
    this.hostPort.close();
  }

  private onMessage(data: unknown): void {
    if (
      typeof data !== 'object' ||
      data === null ||
      (data as { jsonrpc?: unknown }).jsonrpc !== '2.0'
    ) {
      return;
    }
    const message = data as JsonRpcMessage;
    // A notification has a method and no id.
    if ('method' in message && !('id' in message)) {
      this.notifications.push(message as JsonRpcNotification);
    }
  }
}

describe('BridgeShell.sendElicitationResponse (D-134)', () => {
  let harnesses: Array<HostHarness | NotificationCapturingChannel> = [];
  afterEach(() => {
    harnesses.forEach((h) => h.close());
    harnesses = [];
  });

  it('exposes elicitation-response on DockyardExtMethod (D-183)', () => {
    expect(DockyardExtMethod.elicitationResponse).toBe(
      'ui/notifications/elicitation-response',
    );
  });

  it('posts a typed notification with taskId + data', async () => {
    const harness = new HostHarness();
    harnesses.push(harness);
    const channel = new NotificationCapturingChannel();
    harnesses.push(channel);
    // The bridge needs a working host for the initialize handshake; once
    // ready, the elicitation-response post flows through the channel.
    // We use one channel for both — wire the harness to it.
    const bridge = createBridge({
      peer: channel.peer,
      source: channel.source,
      styleTarget: null,
    });
    // The harness's handshake takes precedence: drive it manually by
    // posting an initialize response when the bridge requests it.
    const hostPort = (channel as unknown as { hostPort: MessagePort }).hostPort;
    hostPort.addEventListener('message', (ev) => {
      const data = ev.data as JsonRpcMessage;
      if ('method' in data && 'id' in data && data.method === 'ui/initialize') {
        hostPort.postMessage({
          jsonrpc: '2.0',
          id: data.id,
          result: {
            protocolVersion: '2026-01-26',
            hostContext: {},
            hostCapabilities: {},
            hostInfo: { name: 'test', version: '0.0.0' },
          },
        });
        // Send initialized to complete handshake.
        queueMicrotask(() =>
          hostPort.postMessage({
            jsonrpc: '2.0',
            method: 'ui/notifications/initialized',
            params: {},
          }),
        );
      }
    });
    await bridge.connect();

    // Now post an elicitation response.
    const taskId = 'task-abc-123';
    const data = { approved: true, decided_at: '2026-05-23T18:00:00Z' };
    bridge.legacy.sendElicitationResponse(taskId, data);

    // Drain microtasks so the message lands.
    await new Promise((resolve) => setTimeout(resolve, 0));

    const elicitation = channel.notifications.find(
      (n) => n.method === 'ui/notifications/elicitation-response',
    );
    expect(elicitation).toBeDefined();
    const params = elicitation!.params as ElicitationResponseParams;
    expect(params.taskId).toBe(taskId);
    expect(params.data).toEqual(data);
    expect(params.declined).toBeUndefined();
  });

  it('posts declined=true when the user declined', async () => {
    const channel = new NotificationCapturingChannel();
    harnesses.push(channel);
    const bridge = createBridge({
      peer: channel.peer,
      source: channel.source,
      styleTarget: null,
    });
    const hostPort = (channel as unknown as { hostPort: MessagePort }).hostPort;
    hostPort.addEventListener('message', (ev) => {
      const d = ev.data as JsonRpcMessage;
      if ('method' in d && 'id' in d && d.method === 'ui/initialize') {
        hostPort.postMessage({
          jsonrpc: '2.0',
          id: d.id,
          result: {
            protocolVersion: '2026-01-26',
            hostContext: {},
            hostCapabilities: {},
            hostInfo: { name: 'test', version: '0.0.0' },
          },
        });
        queueMicrotask(() =>
          hostPort.postMessage({
            jsonrpc: '2.0',
            method: 'ui/notifications/initialized',
            params: {},
          }),
        );
      }
    });
    await bridge.connect();

    bridge.legacy.sendElicitationResponse('task-xyz', undefined, { declined: true });
    await new Promise((resolve) => setTimeout(resolve, 0));

    const elicitation = channel.notifications.find(
      (n) => n.method === 'ui/notifications/elicitation-response',
    );
    expect(elicitation).toBeDefined();
    const params = elicitation!.params as ElicitationResponseParams;
    expect(params.taskId).toBe('task-xyz');
    expect(params.declined).toBe(true);
    expect(params.data).toBeUndefined();
  });

  it('omits data when only declined is supplied (typed shape)', () => {
    // A pure-type sanity test: confirm the ElicitationResponseParams
    // shape allows declined-only.
    const params: ElicitationResponseParams = { taskId: 't', declined: true };
    expect(params.declined).toBe(true);
    expect(params.data).toBeUndefined();
  });
});
