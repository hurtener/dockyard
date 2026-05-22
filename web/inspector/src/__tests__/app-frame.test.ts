/**
 * app-frame.test.ts — the App-frame ↔ host-half-bridge wiring.
 */
import { describe, expect, it } from 'vitest';
import { mountAppFrame, APP_SANDBOX, APP_PREVIEW_CSP } from '../lib/app-frame.js';
import { createBridge, type MessageSink } from '@dockyard/bridge';

describe('app-frame constants', () => {
  it('the App sandbox is deny-by-default (scripts only, no same-origin)', () => {
    expect(APP_SANDBOX).toBe('allow-scripts');
    expect(APP_SANDBOX).not.toContain('allow-same-origin');
  });

  it('the preview CSP is deny-by-default', () => {
    expect(APP_PREVIEW_CSP).toContain("default-src 'none'");
  });
});

describe('mountAppFrame', () => {
  it('drives the host-half bridge so an App completes its handshake', async () => {
    // A MessageChannel stands in for the iframe boundary. The host side uses
    // port1 for BOTH posting and listening; the View side uses port2 — a post
    // on port1 is delivered to port2's listeners and vice versa.
    const channel = new MessageChannel();
    channel.port1.start();
    channel.port2.start();

    // A fake iframe whose contentWindow posts onto the host's port1 — the
    // message is delivered to the View listening on port2.
    const fakeIframe = {
      contentWindow: {
        postMessage: (m: unknown) => channel.port1.postMessage(m),
      },
    } as unknown as HTMLIFrameElement;

    // The host listens on a source backed by port1.
    const hostWindow = {
      addEventListener(_t: 'message', l: (ev: { data: unknown }) => void) {
        channel.port1.addEventListener('message', (ev) => l({ data: ev.data }));
      },
      removeEventListener() {},
    };

    const statuses: string[] = [];
    const handle = mountAppFrame({
      iframe: fakeIframe,
      hostWindow: hostWindow as unknown as Window,
      onStatus: (s) => statuses.push(s),
    });

    // The App's View half connects through port2.
    const view = createBridge({
      peer: channel.port2 as unknown as MessageSink,
      source: {
        addEventListener(_t, l) {
          channel.port2.addEventListener('message', (ev) =>
            l({ data: ev.data }),
          );
        },
        removeEventListener() {},
        start() {
          channel.port2.start();
        },
      },
    });

    await Promise.all([view.connect(), handle.ready()]);
    expect(handle.status()).toBe('ready');
    expect(statuses).toContain('ready');

    handle.close();
    expect(handle.status()).toBe('idle');
    view.close();
  });
});
