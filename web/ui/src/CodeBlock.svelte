<!--
  CodeBlock — a read-only monospace block with a copy button. Used for snippets,
  commands, and any literal text a developer copies.
-->
<script lang="ts">
  interface Props {
    /** The code / text to display. */
    code: string;
    /** Optional language label shown in the corner. */
    language?: string;
  }

  let { code, language }: Props = $props();

  let copied = $state(false);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard?.writeText(code);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      // Clipboard unavailable (e.g. insecure context) — fail quietly; the
      // text is still selectable. Never throw from a UI affordance.
      copied = false;
    }
  }
</script>

<div class="dy-codeblock" data-testid="code-block">
  <div class="dy-codeblock__bar">
    {#if language}<span class="dy-codeblock__lang">{language}</span>{/if}
    <button
      type="button"
      class="dy-codeblock__copy"
      onclick={copy}
      aria-label="Copy to clipboard"
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  </div>
  <pre class="dy-codeblock__pre"><code>{code}</code></pre>
</div>

<style>
  .dy-codeblock {
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    background: var(--dy-color-canvas);
    overflow: hidden;
  }

  .dy-codeblock__bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--dy-space-1) var(--dy-space-2);
    border-bottom: 1px solid var(--dy-color-border);
    background: var(--dy-color-surface);
  }

  .dy-codeblock__lang {
    color: var(--dy-color-ink-soft);
    font-family: var(--dy-font-mono);
    font-size: var(--dy-text-xs);
  }

  .dy-codeblock__copy {
    margin-left: auto;
    padding: 2px var(--dy-space-2);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    background: var(--dy-color-surface);
    color: var(--dy-color-ink);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-xs);
    cursor: pointer;
  }

  .dy-codeblock__copy:hover {
    border-color: var(--dy-color-primary);
    color: var(--dy-color-primary-strong);
  }

  .dy-codeblock__copy:focus-visible {
    outline: none;
    box-shadow: var(--dy-focus-ring);
  }

  .dy-codeblock__pre {
    margin: 0;
    padding: var(--dy-space-3);
    overflow-x: auto;
    color: var(--dy-color-ink);
    font-family: var(--dy-font-mono);
    font-size: var(--dy-text-sm);
    line-height: var(--dy-line-normal);
  }
</style>
