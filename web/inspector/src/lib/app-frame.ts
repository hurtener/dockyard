/**
 * app-frame.ts — wiring the App preview iframe to the host-half bridge.
 *
 * The App preview frame renders an MCP App in a SANDBOXED iframe under a
 * deny-by-default CSP (CLAUDE.md §7 — MCP Apps render in a sandboxed iframe;
 * the host may further restrict but never loosen). This module owns the
 * non-visual wiring: it builds the sandboxed iframe attributes, drives the
 * {@link HostBridge} against the iframe's `contentWindow`, and reports the
 * handshake outcome. The Svelte `AppFrame.svelte` component composes it.
 */

import { HostBridge, type HostRpcLogEntry } from '../host/host-bridge.js';
import type { HostCapabilities, HostContext } from 'dockyard-bridge';

/**
 * The iframe `sandbox` token set for an MCP App preview. It is deny-by-default:
 * scripts run (an App is a Svelte bundle) but the frame has no same-origin
 * privilege, cannot navigate the top window, and cannot open popups. The
 * inspector host may not loosen this (CLAUDE.md §7 / RFC §7.4).
 */
export const APP_SANDBOX = 'allow-scripts';

/**
 * The deny-by-default Content-Security-Policy applied to the App preview
 * document. It mirrors runtime/apps's secure default: no external network, no
 * framing of other origins. Phase 23 wires per-App manifest opt-ins.
 */
export const APP_PREVIEW_CSP =
  "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:";

/** The outcome of mounting an App in the preview frame. */
export type AppFrameStatus =
  | 'idle'
  | 'loading'
  | 'handshaking'
  | 'ready'
  | 'error';

/** Options for {@link mountAppFrame}. */
export interface MountAppFrameOptions {
  /** The iframe element rendering the App. Its `contentWindow` is the peer. */
  iframe: HTMLIFrameElement;
  /** The window receiving View → host messages (usually the inspector window). */
  hostWindow: Pick<Window, 'addEventListener' | 'removeEventListener'>;
  /** The host context the host half supplies in the handshake. */
  hostContext?: HostContext;
  /**
   * The host capabilities the host half advertises in the handshake — the
   * capability-set emulation seam (RFC §12, §7.5). When the emulated host has
   * Apps or Tasks toggled off, the App reads that from the handshake result
   * and degrades.
   */
  hostCapabilities?: HostCapabilities;
  /** Called for every JSON-RPC message — feeds the RPC panel. */
  onRpc?: (entry: HostRpcLogEntry) => void;
  /** Called whenever the frame status changes. */
  onStatus?: (status: AppFrameStatus) => void;
  /** Called when the View reports its content size (`size-changed`; D-182). */
  onViewSize?: (size: { width?: number; height?: number }) => void;
  /** Called when the App asks to be torn down (`request-teardown`; D-182). */
  onViewRequestTeardown?: () => void;
}

/** A handle to a mounted App frame — drive and tear it down. */
export interface AppFrameHandle {
  /** The host-half bridge driving the App. */
  bridge: HostBridge;
  /** The current status. */
  status(): AppFrameStatus;
  /** Resolves when the App completes its `ui/initialize` handshake. */
  ready(): Promise<void>;
  /** Tears the frame wiring down. */
  close(): void;
}

/**
 * Mounts the host-half bridge against an App preview iframe. The caller has
 * already set the iframe's `srcdoc`/`src` to the App's HTML; this wires the
 * bridge so the App's View half can complete its `ui/initialize` handshake.
 *
 * The returned handle's `ready()` resolves once the App has rendered and
 * completed its handshake — the binding RFC §12 acceptance criterion.
 */
export function mountAppFrame(opts: MountAppFrameOptions): AppFrameHandle {
  let status: AppFrameStatus = 'loading';
  const setStatus = (s: AppFrameStatus): void => {
    status = s;
    opts.onStatus?.(s);
  };

  const peer = {
    postMessage(message: unknown): void {
      // The App's View half listens on its own `window`; the host posts into
      // the iframe's content window. A missing contentWindow (the frame is not
      // yet loaded) drops the message — the View re-sends `ui/initialize`.
      const target = opts.iframe.contentWindow;
      if (target) {
        // The inspector serves the App from its own loopback origin, so a
        // `*` target origin is acceptable here — the frame is sandboxed and
        // same-document. Phase 23 narrows this with per-App origin profiles.
        target.postMessage(message, '*');
      }
    },
  };

  const bridge = new HostBridge({
    peer,
    source: opts.hostWindow as unknown as {
      addEventListener(t: 'message', l: (ev: { data: unknown }) => void): void;
      removeEventListener(
        t: 'message',
        l: (ev: { data: unknown }) => void,
      ): void;
    },
    hostContext: opts.hostContext,
    hostCapabilities: opts.hostCapabilities,
    onRpc: opts.onRpc,
    onViewSize: opts.onViewSize,
    onViewRequestTeardown: opts.onViewRequestTeardown,
  });

  setStatus('handshaking');
  bridge.start();
  bridge
    .ready()
    .then(() => setStatus('ready'))
    .catch(() => setStatus('error'));

  return {
    bridge,
    status: () => status,
    ready: () => bridge.ready(),
    close: () => {
      bridge.close();
      setStatus('idle');
    },
  };
}
