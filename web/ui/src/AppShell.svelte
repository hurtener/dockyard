<!--
  AppShell — the outer frame every Dockyard surface mounts inside.
  Slots: header, an optional right-hand rail, main content, footer. This is the
  frame the inspector (design-spec.md §4.1) composes; the rail slot holds a
  DetailRail, the footer a ConnectionFooter.

  Layout modes (Phase 25):
   - The default leaves the shell at `min-height: 100%` and lets it grow with
     content — the original Phase 10a behaviour (docs page, long pages).
   - `fullViewport={true}` pins the shell to exactly `100vh`, makes the body
     `flex: 1; min-height: 0` so the rail and main are fixed-height containers,
     and adds `overflow: auto` to the main column. Inspector + future console-
     style surfaces should enable this; a docs page leaves it off. The change
     is purely additive — existing pages that do not pass the prop see no
     behaviour change.
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
    /**
     * When true, the shell occupies exactly `100vh` and each region (header,
     * footer, main, rail) is a fixed-height container with internal scrolling
     * via `overflow: auto`. Phase 25 — the cosmetic follow-up Phase-24-finish
     * surfaced: the inspector body grew past the viewport because each region
     * grew with its content.
     */
    fullViewport?: boolean;
  }

  let {
    header,
    rail,
    footer,
    children,
    density = 'comfortable',
    fullViewport = false,
  }: Props = $props();
</script>

<div
  class="dy-shell"
  data-density={density}
  data-fullvh={fullViewport ? 'true' : 'false'}
  data-testid="app-shell"
>
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

  /*
   * fullViewport — Phase 25. The shell occupies exactly 100vh and the body
   * stretches to fill the leftover space between header and footer. Each
   * region (main, rail) is then a fixed-height container with overflow:auto
   * internally, so a panel with 200 lines of obs/v1 events does not push
   * the footer past the bottom of the viewport.
   *
   * `min-height: 100vh` + `max-height: 100vh` clamps the shell to the
   * viewport in browsers that disagree about which property wins when
   * content exceeds the viewport. `overflow: hidden` on the body keeps the
   * scroll inside each region.
   */
  .dy-shell[data-fullvh='true'] {
    height: 100vh;
    max-height: 100vh;
    min-height: 100vh;
    overflow: hidden;
  }

  .dy-shell__header {
    border-bottom: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
  }
  .dy-shell[data-fullvh='true'] .dy-shell__header {
    flex-shrink: 0;
  }

  .dy-shell__body {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .dy-shell[data-fullvh='true'] .dy-shell__body {
    overflow: hidden;
  }

  .dy-shell__main {
    flex: 1;
    min-width: 0;
    padding: var(--dy-space-5);
  }
  .dy-shell[data-fullvh='true'] .dy-shell__main {
    min-height: 0;
    overflow: auto;
  }

  .dy-shell[data-density='compact'] .dy-shell__main {
    padding: var(--dy-space-3);
  }

  .dy-shell__body--railed .dy-shell__rail {
    /* Responsive: wide enough for a two-column data panel (the inspector's
       Tools / Resources table), capped so it never dominates a wide
       viewport. A fixed 340px starved the description column. */
    width: clamp(400px, 34vw, 620px);
    flex-shrink: 0;
    border-left: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
    overflow-y: auto;
  }
  .dy-shell[data-fullvh='true'] .dy-shell__rail {
    min-height: 0;
    max-height: 100%;
  }

  .dy-shell__footer {
    border-top: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
  }
  .dy-shell[data-fullvh='true'] .dy-shell__footer {
    flex-shrink: 0;
  }
</style>
