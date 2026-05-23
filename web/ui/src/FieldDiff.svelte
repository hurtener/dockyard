<!--
  FieldDiff — a labeled input that shows an original value paired with an
  editable proposed value.

  The reusable primitive added to the shared web/ui inventory in Phase 25 to
  support `propose_with_edits` in the approval-flows template, but designed
  to be reused by any future review / commit / human-in-the-loop UX. Pure
  Svelte 5, token-driven, accessible.

  Layout per design tokens:

      ┌ Label  ──────────────────────────────────────┐
      │ Current: <muted value>                       │
      │ ┌──────────────────────────────────────────┐ │  ← editable
      │ │ <input>                                  │ │
      │ └──────────────────────────────────────────┘ │
      │ <optional helper text>                       │
      └──────────────────────────────────────────────┘

  Field types: `string` (text input), `number` (numeric input), `boolean`
  (checkbox), `enum` (select), `text` (textarea). Anything else falls back
  to a plain `<input>` text type — forward-compatibility: a future contract
  can carry a new type without crashing an older FieldDiff.

  Accessibility:
   - The current value is wrapped in `<output>` so screen readers announce
     it as a derived value (not as a label or a heading).
   - The editable input is linked to the label by `aria-labelledby`.
   - The "differs from original" badge is announced via `aria-describedby`
     so a user navigating with assistive tech hears the change cue.
   - Every interactive element has a visible focus ring (token-driven).
-->
<script lang="ts">
  import type { FieldDiffType } from './types.js';

  interface Props {
    /** A stable id; the input's html id is derived from it. */
    id: string;
    /** The visible field label. */
    label: string;
    /** The field type — drives the input renderer. */
    type: FieldDiffType;
    /** The original (current) value, rendered read-only above the input. */
    current: unknown;
    /** The proposed value the user edits. */
    proposed: unknown;
    /**
     * Allowed options when `type === 'enum'`. Ignored for other types. Each
     * entry is `{ value, label }`; `value` is what reaches `onChange`.
     */
    options?: Array<{ value: string; label: string }>;
    /** Called with the final value whenever the user edits the input. */
    onChange?: (value: unknown) => void;
    /** Optional helper text rendered under the input. */
    helperText?: string;
    /** Extra ids to chain onto `aria-describedby`. */
    ariaDescribedBy?: string;
    /** Disable the input. Defaults to false. */
    disabled?: boolean;
  }

  let {
    id,
    label,
    type,
    current,
    proposed,
    options = [],
    onChange,
    helperText,
    ariaDescribedBy,
    disabled = false,
  }: Props = $props();

  const labelId = $derived(`${id}-label`);
  const helperId = $derived(`${id}-helper`);
  const diffId = $derived(`${id}-diff`);
  const currentId = $derived(`${id}-current`);

  // The describedBy chain — only join non-empty ids so a missing helper or
  // missing extra description does not emit an empty aria-describedby.
  const describedBy = $derived(
    [currentId, helperText ? helperId : '', diffId, ariaDescribedBy ?? '']
      .filter((s) => s.length > 0)
      .join(' '),
  );

  // True when the proposed value differs from the current — drives the
  // visual + accessible "changed" cue.
  const differs = $derived(!deepEqual(current, proposed));

  function formatValue(v: unknown): string {
    if (v === null || v === undefined) return '';
    if (typeof v === 'boolean') return v ? 'true' : 'false';
    if (typeof v === 'object') return JSON.stringify(v);
    return String(v);
  }

  function deepEqual(a: unknown, b: unknown): boolean {
    if (a === b) return true;
    if (typeof a !== typeof b) return false;
    if (a === null || b === null) return false;
    if (typeof a === 'object') {
      try {
        return JSON.stringify(a) === JSON.stringify(b);
      } catch {
        return false;
      }
    }
    return false;
  }

  function handleStringInput(ev: Event): void {
    const value = (ev.target as HTMLInputElement).value;
    onChange?.(value);
  }

  function handleNumberInput(ev: Event): void {
    const raw = (ev.target as HTMLInputElement).value;
    // An empty string stays as null — the user cleared the field; coerce
    // anything else through Number so the contract type is preserved.
    if (raw === '') {
      onChange?.(null);
      return;
    }
    const n = Number(raw);
    onChange?.(Number.isNaN(n) ? raw : n);
  }

  function handleBooleanInput(ev: Event): void {
    const checked = (ev.target as HTMLInputElement).checked;
    onChange?.(checked);
  }

  function handleSelectInput(ev: Event): void {
    const value = (ev.target as HTMLSelectElement).value;
    onChange?.(value);
  }

  function handleTextareaInput(ev: Event): void {
    const value = (ev.target as HTMLTextAreaElement).value;
    onChange?.(value);
  }

  // Coerce the proposed value into the right primitive for each input —
  // a `<textarea>` cannot bind to `undefined`, etc.
  const proposedString = $derived(formatValue(proposed));
  const proposedBool = $derived(Boolean(proposed));
</script>

<div class="dy-field-diff" data-testid="field-diff" data-differs={differs}>
  <label class="dy-field-diff__label" id={labelId} for={id}>{label}</label>

  <output class="dy-field-diff__current" id={currentId} for={id}>
    <span class="dy-field-diff__current-prefix">Current</span>
    <span class="dy-field-diff__current-value">{formatValue(current)}</span>
  </output>

  <div class="dy-field-diff__editor">
    {#if type === 'string'}
      <input
        {id}
        type="text"
        class="dy-field-diff__input"
        value={proposedString}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        oninput={handleStringInput}
        {disabled}
      />
    {:else if type === 'number'}
      <input
        {id}
        type="number"
        inputmode="decimal"
        class="dy-field-diff__input"
        value={proposedString}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        oninput={handleNumberInput}
        {disabled}
      />
    {:else if type === 'boolean'}
      <input
        {id}
        type="checkbox"
        class="dy-field-diff__checkbox"
        checked={proposedBool}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        onchange={handleBooleanInput}
        {disabled}
      />
    {:else if type === 'enum'}
      <select
        {id}
        class="dy-field-diff__select"
        value={proposedString}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        onchange={handleSelectInput}
        {disabled}
      >
        {#each options as opt (opt.value)}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    {:else if type === 'text'}
      <textarea
        {id}
        class="dy-field-diff__textarea"
        rows="4"
        value={proposedString}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        oninput={handleTextareaInput}
        {disabled}
      ></textarea>
    {:else}
      <!-- Forward-compat: an unknown type falls back to plain text. -->
      <input
        {id}
        type="text"
        class="dy-field-diff__input"
        value={proposedString}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        oninput={handleStringInput}
        {disabled}
      />
    {/if}
  </div>

  <!-- The diff badge is always present in the DOM so screen readers can
       announce its transitions; visibility is data-attribute-driven so
       a sighted user sees it only when the values differ. -->
  <span
    class="dy-field-diff__badge"
    id={diffId}
    data-testid="field-diff-badge"
    data-visible={differs}
    aria-live="polite"
  >
    {#if differs}Edited{/if}
  </span>

  {#if helperText}
    <p class="dy-field-diff__helper" id={helperId}>{helperText}</p>
  {/if}
</div>

<style>
  .dy-field-diff {
    display: grid;
    grid-template-areas:
      'label   badge'
      'current current'
      'editor  editor'
      'helper  helper';
    grid-template-columns: 1fr auto;
    gap: var(--dy-space-2);
    padding: var(--dy-space-3);
    background: var(--dy-color-surface);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-md);
    font-family: var(--dy-font-sans);
    font-size: var(--dy-text-base);
    color: var(--dy-color-ink);
  }

  .dy-field-diff[data-differs='true'] {
    border-color: var(--dy-state-warn-fg);
    background: var(--dy-state-warn-bg, var(--dy-color-surface));
  }

  .dy-field-diff__label {
    grid-area: label;
    font-weight: 600;
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink);
  }

  .dy-field-diff__current {
    grid-area: current;
    display: flex;
    gap: var(--dy-space-2);
    align-items: baseline;
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }

  .dy-field-diff__current-prefix {
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: var(--dy-text-xs);
    color: var(--dy-color-ink-faint, var(--dy-color-ink-soft));
  }

  .dy-field-diff__current-value {
    font-family: var(--dy-font-mono, monospace);
    word-break: break-word;
  }

  .dy-field-diff__editor {
    grid-area: editor;
  }

  .dy-field-diff__input,
  .dy-field-diff__select,
  .dy-field-diff__textarea {
    width: 100%;
    padding: var(--dy-space-2) var(--dy-space-3);
    background: var(--dy-color-canvas);
    color: var(--dy-color-ink);
    border: 1px solid var(--dy-color-border);
    border-radius: var(--dy-radius-sm);
    font: inherit;
    font-family: var(--dy-font-mono, monospace);
    box-sizing: border-box;
  }

  .dy-field-diff__input:focus-visible,
  .dy-field-diff__select:focus-visible,
  .dy-field-diff__textarea:focus-visible,
  .dy-field-diff__checkbox:focus-visible {
    outline: 2px solid var(--dy-color-primary);
    outline-offset: 2px;
  }

  .dy-field-diff__checkbox {
    width: 1.1em;
    height: 1.1em;
    accent-color: var(--dy-color-primary);
  }

  .dy-field-diff__textarea {
    resize: vertical;
    min-height: 4rem;
  }

  .dy-field-diff__badge {
    grid-area: badge;
    align-self: start;
    padding: 0 var(--dy-space-2);
    border-radius: var(--dy-radius-pill, 999px);
    background: var(--dy-state-warn-fg);
    color: var(--dy-state-warn-bg, var(--dy-color-canvas));
    font-size: var(--dy-text-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    line-height: 1.6;
  }

  .dy-field-diff__badge[data-visible='false'] {
    visibility: hidden;
  }

  .dy-field-diff__helper {
    grid-area: helper;
    margin: 0;
    font-size: var(--dy-text-sm);
    color: var(--dy-color-ink-soft);
  }
</style>
