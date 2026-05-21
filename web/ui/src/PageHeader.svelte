<!--
  PageHeader — page title + subtitle, an actions slot, an optional status area.
  A search input never lives here — that is FilterBar's job (design-spec.md §3.2).
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** The page title. */
    title: string;
    /** Optional subtitle / context line. */
    subtitle?: string;
    /** Optional leading slot — e.g. a logo mark. */
    lead?: Snippet;
    /** Optional status slot — e.g. a StatusChip. */
    status?: Snippet;
    /** Optional actions slot — e.g. an ActionBar. */
    actions?: Snippet;
  }

  let { title, subtitle, lead, status, actions }: Props = $props();
</script>

<div class="dy-header" data-testid="page-header">
  <div class="dy-header__lead">
    {#if lead}
      <div class="dy-header__mark">{@render lead()}</div>
    {/if}
    <div class="dy-header__titles">
      <h1 class="dy-header__title">{title}</h1>
      {#if subtitle}
        <p class="dy-header__subtitle">{subtitle}</p>
      {/if}
    </div>
    {#if status}
      <div class="dy-header__status">{@render status()}</div>
    {/if}
  </div>
  {#if actions}
    <div class="dy-header__actions">{@render actions()}</div>
  {/if}
</div>

<style>
  .dy-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--dy-space-4);
    padding: var(--dy-space-4) var(--dy-space-5);
  }

  .dy-header__lead {
    display: flex;
    align-items: center;
    gap: var(--dy-space-3);
    min-width: 0;
  }

  .dy-header__mark {
    display: flex;
    align-items: center;
  }

  .dy-header__titles {
    min-width: 0;
  }

  .dy-header__title {
    margin: 0;
    color: var(--dy-color-ink);
    font-size: var(--dy-text-lg);
    font-weight: var(--dy-weight-semibold);
    line-height: var(--dy-line-tight);
  }

  .dy-header__subtitle {
    margin: 0;
    color: var(--dy-color-ink-soft);
    font-size: var(--dy-text-sm);
  }

  .dy-header__actions {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
    flex-shrink: 0;
  }
</style>
