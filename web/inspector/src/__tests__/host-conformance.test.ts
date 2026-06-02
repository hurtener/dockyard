/**
 * host-conformance.test.ts — the inspector host's OUTBOUND (host→View) wire is
 * schema-conformant (D-182).
 *
 * The inspector is the reference local host. The bridge's own conformance test
 * guards the View's outbound; this is the symmetric guard for the host's
 * outbound — it `.parse()`s the messages the host SENDS (the `ui/initialize`
 * result, `request-display-mode` result, and the host→View notifications)
 * against the vendored ext-apps schema (`dockyard-bridge/spec`), so the
 * reference host can never emit wire a real App would choke on.
 */
import { describe, expect, it } from 'vitest';
import type { MessageSink } from 'dockyard-bridge';
import {
  McpUiHostContextChangedNotificationSchema,
  McpUiInitializeResultSchema,
  McpUiRequestDisplayModeResultSchema,
  McpUiToolInputNotificationSchema,
  McpUiToolResultNotificationSchema,
} from 'dockyard-bridge/spec';
import { HostBridge } from '../host/host-bridge.js';

function portSource(port: MessagePort) {
  return {
    addEventListener(_t: 'message', l: (ev: { data: unknown }) => void) {
      port.addEventListener('message', (ev) => l({ data: ev.data }));
    },
    removeEventListener() {},
    start() {
      port.start();
    },
  };
}

interface AnyMsg {
  id?: number;
  method?: string;
  result?: unknown;
  params?: unknown;
}

describe('HostBridge outbound conformance (D-182)', () => {
  function setup() {
    const channel = new MessageChannel();
    const outbound: AnyMsg[] = [];
    channel.port2.addEventListener('message', (ev) =>
      outbound.push(ev.data as AnyMsg),
    );
    channel.port2.start();
    const host = new HostBridge({
      peer: channel.port1 as unknown as MessageSink,
      source: portSource(channel.port1),
    });
    host.start();
    const send = (m: unknown): void => channel.port2.postMessage(m);
    return { host, outbound, send, channel };
  }

  const validInitParams = {
    appInfo: { name: 'app', version: '1.0.0' },
    appCapabilities: { availableDisplayModes: ['inline', 'fullscreen'] },
    protocolVersion: '2026-01-26',
  };

  it('the ui/initialize result conforms to McpUiInitializeResultSchema', async () => {
    const { outbound, send, channel } = setup();
    send({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: validInitParams });
    await new Promise((r) => setTimeout(r, 20));
    const res = outbound.find((m) => m.id === 1 && m.result);
    expect(res).toBeDefined();
    expect(() => McpUiInitializeResultSchema.parse(res!.result)).not.toThrow();
    channel.port1.close();
    channel.port2.close();
  });

  it('the request-display-mode result conforms to its schema', async () => {
    const { outbound, send, channel } = setup();
    send({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: validInitParams });
    await new Promise((r) => setTimeout(r, 10));
    send({
      jsonrpc: '2.0',
      id: 2,
      method: 'ui/request-display-mode',
      params: { mode: 'fullscreen' },
    });
    await new Promise((r) => setTimeout(r, 10));
    const res = outbound.find((m) => m.id === 2 && m.result);
    expect(res).toBeDefined();
    expect(() =>
      McpUiRequestDisplayModeResultSchema.parse(res!.result),
    ).not.toThrow();
    channel.port1.close();
    channel.port2.close();
  });

  it('tool-input / tool-result notifications conform', async () => {
    const { host, outbound, send, channel } = setup();
    send({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: validInitParams });
    await new Promise((r) => setTimeout(r, 10));

    host.sendToolInput({ city: 'oslo' });
    host.sendToolResult({
      content: [{ type: 'text', text: 'done' }],
      structuredContent: { total: 7 },
    });
    await new Promise((r) => setTimeout(r, 10));

    const input = outbound.find((m) => m.method === 'ui/notifications/tool-input');
    const result = outbound.find((m) => m.method === 'ui/notifications/tool-result');
    expect(() =>
      McpUiToolInputNotificationSchema.parse({
        method: input!.method,
        params: input!.params,
      }),
    ).not.toThrow();
    expect(() =>
      McpUiToolResultNotificationSchema.parse({
        method: result!.method,
        params: result!.params,
      }),
    ).not.toThrow();
    channel.port1.close();
    channel.port2.close();
  });

  it('a host-context-changed patch conforms', async () => {
    const { host, outbound, send, channel } = setup();
    send({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: validInitParams });
    await new Promise((r) => setTimeout(r, 10));

    host.patchHostContext({ theme: 'dark', displayMode: 'inline' });
    await new Promise((r) => setTimeout(r, 10));

    const patch = outbound.find(
      (m) => m.method === 'ui/notifications/host-context-changed',
    );
    expect(() =>
      McpUiHostContextChangedNotificationSchema.parse({
        method: patch!.method,
        params: patch!.params,
      }),
    ).not.toThrow();
    channel.port1.close();
    channel.port2.close();
  });
});
