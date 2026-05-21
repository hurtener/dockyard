<!--
  LoadingState — the loading panel of the four-state PageState family.
  A calm, centred busy indicator with an optional message. Plain Svelte 5.
-->
<script lang="ts">
  interface Props {
    /** Optional message shown under the spinner. */
    message?: string;
  }

  let { message = 'Loading…' }: Props = $props();
</script>

<div class="dy-state" role="status" aria-live="polite" data-testid="loading-state">
  <span class="dy-state__spinner" aria-hidden="true"></span>
  <p class="dy-state__message">{message}</p>
</div>

<style>
  .dy-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--dy-space-3);
    padding: var(--dy-space-7) var(--dy-space-5);
    color: var(--dy-color-ink-soft);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-base);
    text-align: center;
  }

  .dy-state__spinner {
    width: 20px;
    height: 20px;
    border-radius: var(--dy-radius-full);
    border: 2px solid var(--dy-color-border);
    border-top-color: var(--dy-color-primary);
    animation: dy-spin 0.7s linear infinite;
  }

  .dy-state__message {
    margin: 0;
  }

  @keyframes dy-spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dy-state__spinner {
      animation-duration: 2s;
    }
  }
</style>
