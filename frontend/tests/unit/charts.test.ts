import { describe, expect, it } from 'vitest';
import {
  BAR_GAP,
  BAR_MAX_WIDTH,
  PLOT_HEIGHT,
  TOOLTIP_WIDTH,
  barPath,
  barPositions,
  barWidth,
  barX,
  clampFraction,
  slotWidth,
  splitSegments,
  tooltipBox,
} from '../../src/lib/charts/geometry';

describe('chart fractions', () => {
  it('clamps the DTO fraction to the drawable range', () => {
    expect(clampFraction(0.5)).toBe(0.5);
    expect(clampFraction(0)).toBe(0);
    expect(clampFraction(-0.2)).toBe(0);
    expect(clampFraction(1)).toBe(1);
    expect(clampFraction(1.5)).toBe(1);
    expect(clampFraction(Number.NaN)).toBe(0);
    expect(clampFraction(Number.POSITIVE_INFINITY)).toBe(1);
  });
});

describe('income/expense bar geometry', () => {
  it('gives every month an equal slot', () => {
    expect(slotWidth(600, 6)).toBe(100);
    expect(slotWidth(0, 6)).toBe(0);
    expect(slotWidth(600, 0)).toBe(0);
    expect(slotWidth(100, 3)).toBeCloseTo(33.3333, 4);
  });

  it('caps a bar at 24 px and keeps it at 30 % of a narrow slot', () => {
    expect(barWidth(100)).toBe(BAR_MAX_WIDTH);
    expect(barWidth(60)).toBe(18);
    expect(barWidth(10)).toBe(3);
    expect(barWidth(0)).toBe(0);
  });

  it('centres the two bars with a 2 px gap', () => {
    const positions = barPositions(100);
    expect(positions.bar).toBe(24);
    expect(positions.width).toBe(24 + 24 + BAR_GAP);
    expect(positions.group).toBe((100 - 50) / 2);
  });

  it('places each month group in its slot', () => {
    const first = barX(600, 6, 0);
    const third = barX(600, 6, 2);
    expect(first.income).toBe(25);
    expect(first.expenses).toBe(25 + 24 + 2);
    expect(third.income).toBe(225);
    expect(third.expenses).toBe(251);
  });

  it('draws a rounded data end and a square base', () => {
    const path = barPath(10, 20, 24, 60, 4);
    expect(path.startsWith('M 10 80')).toBe(true);
    expect(path).toContain('Q 10 20 14 20');
    expect(path).toContain('Q 34 20 34 24');
    expect(path.endsWith('Z')).toBe(true);
  });

  it('returns no path for an empty bar and a square path at radius zero', () => {
    expect(barPath(0, 0, 0, 10)).toBe('');
    expect(barPath(0, 0, 10, 0)).toBe('');
    expect(barPath(4, 8, 10, 20, 0)).toBe('M 4 8 L 14 8 L 14 28 L 4 28 Z');
  });

  it('never rounds more than the bar can carry', () => {
    const path = barPath(0, 10, 6, 3, 4);
    expect(path).toContain('Q 0 10 3 10');
    expect(path).toContain('Q 6 10 6 13');
  });
});

describe('category split segments', () => {
  it('reserves the gaps between visible segments', () => {
    const segments = splitSegments(200, [0.5, 0.5]);
    expect(segments).toEqual([
      { x: 0, width: 99 },
      { x: 101, width: 99 },
    ]);
  });

  it('drops zero fractions and keeps the 2 px gaps', () => {
    const segments = splitSegments(202, [0.5, 0, 0.5]);
    expect(segments).toEqual([
      { x: 0, width: 100 },
      { x: 102, width: 100 },
    ]);
  });

  it('leaves the track end uncovered when the shares do not fill it', () => {
    const segments = splitSegments(100, [0.25, 0.25]);
    expect(segments).toEqual([
      { x: 0, width: 24.5 },
      { x: 26.5, width: 24.5 },
    ]);
  });

  it('returns no segments without width or data', () => {
    expect(splitSegments(0, [1])).toEqual([]);
    expect(splitSegments(100, [])).toEqual([]);
    expect(splitSegments(100, [0, 0])).toEqual([]);
  });
});

describe('hover tooltip', () => {
  it('sits on the plot top and aligns by half', () => {
    const firstHalf = tooltipBox(600, 100, 6, 2, false);
    expect(firstHalf).toEqual({ x: 200, y: 8, width: TOOLTIP_WIDTH, height: 74 });

    const secondHalf = tooltipBox(600, 100, 6, 4, false);
    expect(secondHalf).toEqual({ x: 324, y: 8, width: TOOLTIP_WIDTH, height: 74 });
  });

  it('is taller when the month is partial and never leaves the plot', () => {
    expect(tooltipBox(600, 100, 6, 5, true).height).toBe(92);
    expect(tooltipBox(600, 100, 6, 0, false).x).toBe(0);
    expect(tooltipBox(600, 100, 6, 0, true).x).toBe(0);
    expect(tooltipBox(600, 100, 6, 5, false).x).toBe(600 - TOOLTIP_WIDTH);
  });

  it('fits a narrow plot instead of overflowing it', () => {
    const box = tooltipBox(120, 20, 6, 0, false);
    expect(box.width).toBe(120);
    expect(box.x).toBe(0);
  });

  it('uses the plot height constant for both bars and gridlines', () => {
    expect(PLOT_HEIGHT).toBe(160);
  });
});
