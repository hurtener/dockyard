<!--
  ConnectionFooter — the app-shell status bar: connection state, server id,
  transport, and live counts. The inspector (design-spec.md §4.2) shows the
  `accent` "live" dot here when obs/v1 events are streaming.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  type Connection = 'connected' | 'connecting' | 'disconnected';

  interface Props {
    /** The connection state — drives the status dot colour. */
    connection: Connection;
    /** A short connection label, e.g. the server id or session id. */
    label?: string;
    /** Optional transport descriptor, e.g. `stdio` or `http`. */
    transport?: string;
    /** When true the dot pulses in `accent` — events are streaming. */
    live?: boolean;
    /** Optional trailing slot — e.g. event/error counts. */
    children?: Snippet;
  }

  let { connection, label, transport, live = false, children }: Props =
    $props();
</script>

<div class="dy-footer" data-testid="connection-footer">
  <span
    class="dy-footer__dot"
    class:dy-footer__dot--live={live}
    data-connection={connection}
    aria-hidden="true"
  ></span>
  <span class="dy-footer__state">{connection}</span>
  {#if label}
    <span class="dy-footer__sep" aria-hidden="true">·</span>
    <span class="dy-footer__label">{label}</span>
  {/if}
  {#if transport}
    <span class="dy-footer__sep" aria-hidden="true">·</span>
    <span class="dy-footer__transport">{transport}</span>
  {/if}
  {#if children}
    <span class="dy-footer__extra">{@render children()}</span>
  {/if}
</div>

<style>
  .dy-footer {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
    padding: var(--dy-space-2) var(--dy-space-5);
    color: var(--dy-color-ink-soft);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-sm);
  }

  .dy-footer__dot {
    width: 8px;
    height: 8px;
    border-radius: var(--dy-radius-full);
    background: var(--dy-color-ink-soft);
  }

  .dy-footer__dot[data-connection='connected'] {
    background: var(--dy-state-ok-fg);
  }

  .dy-footer__dot[data-connection='connecting'] {
    background: var(--dy-state-warn-fg);
  }

  .dy-footer__dot[data-connection='disconnected'] {
    background: var(--dy-state-error-fg);
  }

  .dy-footer__dot--live {
    background: var(--dy-color-accent);
    animation: dy-pulse 1.4s ease-in-out infinite;
  }

  .dy-footer__state {
    text-transform: capitalize;
    color: var(--dy-color-ink);
    font-weight: var(--dy-weight-medium);
  }

  .dy-footer__extra {
    margin-left: auto;
  }

  @keyframes dy-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dy-footer__dot--live {
      animation: none;
    }
  }
</style>
