/**
 * FieldDiff.test.ts — coverage for the shared web/ui FieldDiff primitive
 * (Phase 25 — the editable current → proposed pair the propose_with_edits
 * App composes).
 *
 * Covers the five native types (string / number / boolean / enum / text),
 * the diff badge that surfaces when the proposed value diverges from the
 * current, the onChange callback wiring, and the accessibility chain
 * (aria-labelledby + aria-describedby).
 */
import { describe, expect, it, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import FieldDiff from '../FieldDiff.svelte';

describe('FieldDiff — rendering', () => {
  it('renders the label and the current value (string type)', () => {
    const { getByTestId, getByText } = render(FieldDiff, {
      id: 'fld-1',
      label: 'Recipient',
      type: 'string',
      current: 'all-hands@acme.com',
      proposed: 'board@acme.com',
    });
    const root = getByTestId('field-diff');
    expect(root.getAttribute('data-differs')).toBe('true');
    expect(getByText('Recipient')).toBeTruthy();
    expect(getByText('all-hands@acme.com')).toBeTruthy();
  });

  it('hides the diff badge when current and proposed are equal', () => {
    const { getByTestId } = render(FieldDiff, {
      id: 'fld-eq',
      label: 'Subject',
      type: 'string',
      current: 'Same',
      proposed: 'Same',
    });
    expect(getByTestId('field-diff').getAttribute('data-differs')).toBe('false');
    expect(getByTestId('field-diff-badge').getAttribute('data-visible')).toBe(
      'false',
    );
  });

  it('renders a text input for type=string with the proposed value', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-str',
      label: 'Subject',
      type: 'string',
      current: 'A',
      proposed: 'B',
    });
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.value).toBe('B');
    expect(input.id).toBe('fld-str');
  });

  it('renders a number input for type=number', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-num',
      label: 'Limit',
      type: 'number',
      current: 100,
      proposed: 250,
    });
    const input = container.querySelector('input[type="number"]') as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.value).toBe('250');
  });

  it('renders a checkbox for type=boolean', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-bool',
      label: 'Send copy',
      type: 'boolean',
      current: false,
      proposed: true,
    });
    const cb = container.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(cb).not.toBeNull();
    expect(cb.checked).toBe(true);
  });

  it('renders a select for type=enum with the supplied options', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-enum',
      label: 'Priority',
      type: 'enum',
      current: 'low',
      proposed: 'high',
      options: [
        { value: 'low', label: 'Low' },
        { value: 'med', label: 'Medium' },
        { value: 'high', label: 'High' },
      ],
    });
    const select = container.querySelector('select') as HTMLSelectElement;
    expect(select).not.toBeNull();
    expect(select.value).toBe('high');
    expect(select.querySelectorAll('option').length).toBe(3);
  });

  it('renders a textarea for type=text', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-text',
      label: 'Body',
      type: 'text',
      current: 'Old body',
      proposed: 'New body line 1\nNew body line 2',
    });
    const ta = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(ta).not.toBeNull();
    expect(ta.value).toBe('New body line 1\nNew body line 2');
  });

  it('falls back to a text input for an unknown type (forward-compat)', () => {
    // Cast through unknown so an unknown type is permitted at runtime.
    const { container } = render(FieldDiff, {
      id: 'fld-future',
      label: 'Date',
      // The V1 type set does not include 'date' — cast to FieldDiffType
      // so we can exercise the runtime forward-compat path.
      type: 'date' as unknown as 'string',
      current: '2025-01-01',
      proposed: '2026-01-01',
    });
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.value).toBe('2026-01-01');
  });
});

describe('FieldDiff — onChange', () => {
  it('fires onChange with the edited string value', async () => {
    const onChange = vi.fn();
    const { container } = render(FieldDiff, {
      id: 'fld-c1',
      label: 'Subject',
      type: 'string',
      current: 'Original',
      proposed: 'Original',
      onChange,
    });
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: 'Edited!' } });
    expect(onChange).toHaveBeenCalledWith('Edited!');
  });

  it('coerces a number input to a Number', async () => {
    const onChange = vi.fn();
    const { container } = render(FieldDiff, {
      id: 'fld-c2',
      label: 'Limit',
      type: 'number',
      current: 1,
      proposed: 1,
      onChange,
    });
    const input = container.querySelector('input[type="number"]') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: '42' } });
    expect(onChange).toHaveBeenCalledWith(42);
  });

  it('reports null when the number input is cleared', async () => {
    const onChange = vi.fn();
    const { container } = render(FieldDiff, {
      id: 'fld-c3',
      label: 'Limit',
      type: 'number',
      current: 1,
      proposed: 1,
      onChange,
    });
    const input = container.querySelector('input[type="number"]') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it('fires onChange with a boolean for checkbox toggles', async () => {
    const onChange = vi.fn();
    const { container } = render(FieldDiff, {
      id: 'fld-c4',
      label: 'Send',
      type: 'boolean',
      current: false,
      proposed: false,
      onChange,
    });
    const cb = container.querySelector('input[type="checkbox"]') as HTMLInputElement;
    await fireEvent.click(cb);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('fires onChange with the selected enum value', async () => {
    const onChange = vi.fn();
    const { container } = render(FieldDiff, {
      id: 'fld-c5',
      label: 'Priority',
      type: 'enum',
      current: 'low',
      proposed: 'low',
      options: [
        { value: 'low', label: 'Low' },
        { value: 'high', label: 'High' },
      ],
      onChange,
    });
    const select = container.querySelector('select') as HTMLSelectElement;
    await fireEvent.change(select, { target: { value: 'high' } });
    expect(onChange).toHaveBeenCalledWith('high');
  });
});

describe('FieldDiff — accessibility', () => {
  it('links the label to the input via aria-labelledby', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-a1',
      label: 'Subject',
      type: 'string',
      current: 'A',
      proposed: 'A',
    });
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    const labelledBy = input.getAttribute('aria-labelledby');
    expect(labelledBy).toBe('fld-a1-label');
    const label = container.querySelector('#fld-a1-label');
    expect(label).not.toBeNull();
    expect(label!.textContent).toBe('Subject');
  });

  it('chains current + diff ids onto aria-describedby', () => {
    const { container } = render(FieldDiff, {
      id: 'fld-a2',
      label: 'Subject',
      type: 'string',
      current: 'A',
      proposed: 'B',
      helperText: 'A short description',
      ariaDescribedBy: 'extra-1',
    });
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    const describedBy = input.getAttribute('aria-describedby') ?? '';
    const ids = describedBy.split(' ');
    expect(ids).toContain('fld-a2-current');
    expect(ids).toContain('fld-a2-helper');
    expect(ids).toContain('fld-a2-diff');
    expect(ids).toContain('extra-1');
  });

  it('exposes a live diff badge with aria-live=polite', () => {
    const { getByTestId } = render(FieldDiff, {
      id: 'fld-a3',
      label: 'Body',
      type: 'string',
      current: 'A',
      proposed: 'B',
    });
    const badge = getByTestId('field-diff-badge');
    expect(badge.getAttribute('aria-live')).toBe('polite');
    expect(badge.textContent?.trim()).toBe('Edited');
  });
});

describe('FieldDiff — deep equality for object diffs', () => {
  it('treats two structurally-equal objects as not-differing', () => {
    const { getByTestId } = render(FieldDiff, {
      id: 'fld-obj',
      label: 'Config',
      type: 'string',
      current: { a: 1, b: 2 },
      proposed: { a: 1, b: 2 },
    });
    expect(getByTestId('field-diff').getAttribute('data-differs')).toBe('false');
  });

  it('treats two distinct objects as differing', () => {
    const { getByTestId } = render(FieldDiff, {
      id: 'fld-obj2',
      label: 'Config',
      type: 'string',
      current: { a: 1 },
      proposed: { a: 2 },
    });
    expect(getByTestId('field-diff').getAttribute('data-differs')).toBe('true');
  });
});
