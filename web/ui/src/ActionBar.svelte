<!--
  ActionBar — a row of buttons/controls, right- or left-aligned. A layout
  primitive: it positions its slotted controls, it does not style them.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** Horizontal alignment of the controls. Default `end` (right). */
    align?: 'start' | 'end' | 'between';
    /** The controls. */
    children: Snippet;
  }

  let { align = 'end', children }: Props = $props();
</script>

<div class="dy-actionbar" data-align={align} data-testid="action-bar">
  {@render children()}
</div>

<style>
  .dy-actionbar {
    display: flex;
    align-items: center;
    gap: var(--dy-space-2);
  }

  .dy-actionbar[data-align='start'] {
    justify-content: flex-start;
  }

  .dy-actionbar[data-align='end'] {
    justify-content: flex-end;
  }

  .dy-actionbar[data-align='between'] {
    justify-content: space-between;
  }
</style>
