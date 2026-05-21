<!--
  JsonInspector — a collapsible, lightly syntax-highlighted JSON tree in the
  mono font. Used for RPC payloads, structuredContent, and schemas. Read-only.

  This is a self-recursive component: each object/array node renders a nested
  JsonInspector for its children, so arbitrarily deep payloads collapse cleanly.
-->
<script lang="ts">
  import Self from './JsonInspector.svelte';

  interface Props {
    /** The value to render — any JSON-serialisable value. */
    value: unknown;
    /** The key this value sits under (for nested rendering). */
    name?: string;
    /** Nesting depth — drives the default collapsed state. */
    depth?: number;
    /** Depth at and below which nodes start collapsed. Default 2. */
    collapseDepth?: number;
  }

  let { value, name, depth = 0, collapseDepth = 2 }: Props = $props();

  const kind = $derived(
    value === null
      ? 'null'
      : Array.isArray(value)
        ? 'array'
        : typeof value,
  );

  const isContainer = $derived(kind === 'array' || kind === 'object');

  const entries = $derived(
    kind === 'array'
      ? (value as unknown[]).map((v, i) => [String(i), v] as const)
      : kind === 'object'
        ? Object.entries(value as Record<string, unknown>)
        : [],
  );

  // `depth`/`collapseDepth` are fixed-at-mount props; the node owns `open` after.
  // svelte-ignore state_referenced_locally
  let open = $state(depth < collapseDepth);

  function toggle(): void {
    if (isContainer) open = !open;
  }

  function preview(): string {
    if (kind === 'array') return `[ ${entries.length} ]`;
    if (kind === 'object') return `{ ${entries.length} }`;
    if (kind === 'string') return `"${String(value)}"`;
    return String(value);
  }
</script>

<div class="dy-json" data-testid="json-inspector" style:--dy-json-indent="{depth * 12}px">
  {#if isContainer}
    <button
      type="button"
      class="dy-json__node"
      aria-expanded={open}
      onclick={toggle}
    >
      <span class="dy-json__chevron" class:dy-json__chevron--open={open} aria-hidden="true">›</span>
      {#if name !== undefined}<span class="dy-json__key">{name}:</span>{/if}
      <span class="dy-json__preview">{preview()}</span>
    </button>
    {#if open}
      <div class="dy-json__children">
        {#each entries as [childKey, childValue] (childKey)}
          <Self
            value={childValue}
            name={childKey}
            depth={depth + 1}
            {collapseDepth}
          />
        {/each}
      </div>
    {/if}
  {:else}
    <div class="dy-json__leaf">
      {#if name !== undefined}<span class="dy-json__key">{name}:</span>{/if}
      <span class="dy-json__value" data-kind={kind}>{preview()}</span>
    </div>
  {/if}
</div>

<style>
  .dy-json {
    font-family: var(--dy-font-mono);
    font-size: var(--dy-text-sm);
    line-height: var(--dy-line-normal);
    color: var(--dy-color-ink);
  }

  .dy-json__node,
  .dy-json__leaf {
    display: flex;
    align-items: baseline;
    gap: var(--dy-space-1);
    padding-left: var(--dy-json-indent);
  }

  .dy-json__node {
    width: 100%;
    border: 0;
    background: transparent;
    font: inherit;
    text-align: left;
    cursor: pointer;
    color: inherit;
  }

  .dy-json__node:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
    border-radius: var(--dy-radius-sm);
  }

  .dy-json__chevron {
    color: var(--dy-color-ink-soft);
    transform: rotate(0deg);
    transition: transform 0.12s ease;
  }

  .dy-json__chevron--open {
    transform: rotate(90deg);
  }

  .dy-json__key {
    color: var(--dy-color-primary-strong);
  }

  .dy-json__preview {
    color: var(--dy-color-ink-soft);
  }

  .dy-json__value[data-kind='string'] {
    color: var(--dy-state-ok-fg);
  }

  .dy-json__value[data-kind='number'] {
    color: var(--dy-state-info-fg);
  }

  .dy-json__value[data-kind='boolean'],
  .dy-json__value[data-kind='null'] {
    color: var(--dy-color-accent);
  }
</style>
