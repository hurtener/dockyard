/**
 * Sparkline.test.ts — coverage for the shared web/ui Sparkline primitive
 * (Phase 24, decision D-127).
 *
 * Covers the path generation (the right number of segments for a varied
 * series), the safe handling of degenerate inputs (an empty series, a
 * single-point series, a flat series — none of which should divide by
 * zero or render an empty SVG), the tone attribute (the tokenised
 * colour), and the accessibility shape (role="img" + aria-label).
 */
import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/svelte';
import Sparkline from '../Sparkline.svelte';

describe('Sparkline', () => {
  it('renders an SVG with the expected number of line segments for a varied series', () => {
    const { getByTestId } = render(Sparkline, {
      values: [1, 2, 3, 4, 5],
      ariaLabel: 'Revenue',
    });
    const svg = getByTestId('sparkline');
    expect(svg.tagName.toLowerCase()).toBe('svg');
    const linePath = svg.querySelector('path.dy-sparkline__line');
    expect(linePath).not.toBeNull();
    const d = linePath!.getAttribute('d') ?? '';
    // M + 4 L commands for 5 points.
    expect(d.startsWith('M')).toBe(true);
    expect((d.match(/L/g) ?? []).length).toBe(4);
  });

  it('renders the baseline fallback for an empty series', () => {
    const { getByTestId } = render(Sparkline, {
      values: [],
      ariaLabel: 'No data',
    });
    const svg = getByTestId('sparkline');
    expect(svg.querySelector('path.dy-sparkline__baseline')).not.toBeNull();
    expect(svg.querySelector('path.dy-sparkline__line')).toBeNull();
  });

  it('renders the baseline fallback for a single-point series', () => {
    const { getByTestId } = render(Sparkline, {
      values: [42],
      ariaLabel: 'One sample',
    });
    expect(
      getByTestId('sparkline').querySelector('path.dy-sparkline__baseline'),
    ).not.toBeNull();
  });

  it('renders a centred horizontal line for a flat series without dividing by zero', () => {
    const { getByTestId } = render(Sparkline, {
      values: [7, 7, 7, 7],
      ariaLabel: 'Flat',
    });
    const linePath = getByTestId('sparkline').querySelector('path.dy-sparkline__line');
    expect(linePath).not.toBeNull();
    const d = linePath!.getAttribute('d') ?? '';
    // Every y should be the same (the centred baseline).
    const ys = [...d.matchAll(/([0-9.]+),([0-9.]+)/g)].map((m) => m[2]);
    expect(new Set(ys).size).toBe(1);
  });

  it('reflects the tone token on the SVG root', () => {
    const { getByTestId } = render(Sparkline, {
      values: [1, 2, 3],
      tone: 'error',
    });
    expect(getByTestId('sparkline').getAttribute('data-tone')).toBe('error');
  });

  it('exposes the aria label and the img role for screen readers', () => {
    const { getByTestId } = render(Sparkline, {
      values: [1, 2],
      ariaLabel: 'Customer health over 30d',
    });
    const svg = getByTestId('sparkline');
    expect(svg.getAttribute('role')).toBe('img');
    expect(svg.getAttribute('aria-label')).toBe('Customer health over 30d');
  });

  it('honours custom width / height props on the SVG viewBox', () => {
    const { getByTestId } = render(Sparkline, {
      values: [1, 2, 3],
      width: 200,
      height: 48,
    });
    expect(getByTestId('sparkline').getAttribute('viewBox')).toBe('0 0 200 48');
  });
});
