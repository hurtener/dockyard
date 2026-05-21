<!--
  AppShell — the outer frame every Dockyard surface mounts inside.
  Slots: header, an optional right-hand rail, main content, footer. This is the
  frame the inspector (design-spec.md §4.1) composes; the rail slot holds a
  DetailRail, the footer a ConnectionFooter.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** The page header region — typically a PageHeader. */
    header?: Snippet;
    /** The optional right-hand rail — typically a DetailRail. */
    rail?: Snippet;
    /** The footer region — typically a ConnectionFooter. */
    footer?: Snippet;
    /** The main content. */
    children: Snippet;
    /** Layout density. `compact` tightens the content padding. */
    density?: 'comfortable' | 'compact';
  }

  let { header, rail, footer, children, density = 'comfortable' }: Props =
    $props();
</script>

<div class="dy-shell" data-density={density} data-testid="app-shell">
  {#if header}
    <header class="dy-shell__header">{@render header()}</header>
  {/if}
  <div class="dy-shell__body" class:dy-shell__body--railed={!!rail}>
    <main class="dy-shell__main">{@render children()}</main>
    {#if rail}
      <aside class="dy-shell__rail">{@render rail()}</aside>
    {/if}
  </div>
  {#if footer}
    <footer class="dy-shell__footer">{@render footer()}</footer>
  {/if}
</div>

<style>
  .dy-shell {
    display: flex;
    flex-direction: column;
    min-height: 100%;
    background: var(--dy-color-canvas);
    color: var(--dy-color-ink);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-base);
  }

  .dy-shell__header {
    border-bottom: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
  }

  .dy-shell__body {
    display: flex;
    flex: 1;
    min-height: 0;
  }

  .dy-shell__main {
    flex: 1;
    min-width: 0;
    padding: var(--dy-space-5);
  }

  .dy-shell[data-density='compact'] .dy-shell__main {
    padding: var(--dy-space-3);
  }

  .dy-shell__body--railed .dy-shell__rail {
    width: 340px;
    flex-shrink: 0;
    border-left: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
    overflow-y: auto;
  }

  .dy-shell__footer {
    border-top: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
  }
</style>
