/**
 * The geometry the two Dashboard charts share. Everything is in CSS pixels:
 * the charts measure their container and pass the width here, so the SVG can
 * stay a plain, container-sized element.
 */

/** Gap between the two bars of a month, and between split segments. */
export const BAR_GAP = 2;

/** The widest a single month bar may get. */
export const BAR_MAX_WIDTH = 24;

/** The chart plot height, as in charts.slint. */
export const PLOT_HEIGHT = 160;

/** The tooltip width, as in charts.slint. */
export const TOOLTIP_WIDTH = 176;

/** Clamps a DTO fraction to the drawable range; non-finite values are zero. */
export function clampFraction(value: number): number {
  if (Number.isNaN(value) || value <= 0) {
    return 0;
  }
  if (value >= 1) {
    return 1;
  }
  return value;
}

/** One month slot is an equal share of the plot. */
export function slotWidth(plotWidth: number, count: number): number {
  if (plotWidth <= 0 || count <= 0) {
    return 0;
  }
  return plotWidth / count;
}

/** The bar width for a slot: 30 % of it, never above 24 px. */
export function barWidth(slot: number): number {
  if (slot <= 0) {
    return 0;
  }
  return Math.min(slot * 0.3, BAR_MAX_WIDTH);
}

export type BarPositions = {
  /** The left edge of the bar group inside its slot. */
  group: number;
  /** The group's total width: income bar, gap, expenses bar. */
  width: number;
  /** The bar width itself. */
  bar: number;
};

/** The group geometry for one slot. */
export function barPositions(slot: number): BarPositions {
  const bar = barWidth(slot);
  return {
    group: Math.max(0, (slot - (bar * 2 + BAR_GAP)) / 2),
    width: bar * 2 + BAR_GAP,
    bar,
  };
}

/** The left x of the income and expenses bars in slot index. */
export function barX(
  plotWidth: number,
  count: number,
  index: number,
): {
  income: number;
  expenses: number;
} {
  const slot = slotWidth(plotWidth, count);
  const positions = barPositions(slot);
  const start = slot * index + positions.group;
  return { income: start, expenses: start + positions.bar + BAR_GAP };
}

function round(value: number): number {
  return Math.round(value * 100) / 100;
}

/**
 * A bar path with a rounded data end (the top) and a square base, as
 * docs/design.md requires. A zero or negative height has no path.
 */
export function barPath(x: number, y: number, width: number, height: number, radius = 4): string {
  if (!(width > 0) || !(height > 0)) {
    return '';
  }
  const r = Math.max(0, Math.min(radius, width / 2, height));
  const left = round(x);
  const top = round(y);
  const right = round(x + width);
  const bottom = round(y + height);
  if (r === 0) {
    return `M ${left} ${top} L ${right} ${top} L ${right} ${bottom} L ${left} ${bottom} Z`;
  }
  const rounded = round(r);
  return [
    `M ${left} ${bottom}`,
    `L ${left} ${round(top + rounded)}`,
    `Q ${left} ${top} ${round(left + rounded)} ${top}`,
    `L ${round(right - rounded)} ${top}`,
    `Q ${right} ${top} ${right} ${round(top + rounded)}`,
    `L ${right} ${bottom}`,
    'Z',
  ].join(' ');
}

export type Segment = { x: number; width: number };

/**
 * Lays the CategorySplit segments out across the measured width. Fractions stay
 * shares of the whole, so a sum below one leaves the track's end uncovered.
 */
export function splitSegments(width: number, fractions: number[], gap = BAR_GAP): Segment[] {
  const visible = fractions.filter((fraction) => clampFraction(fraction) > 0);
  if (width <= 0 || visible.length === 0) {
    return [];
  }
  const gaps = gap * Math.max(0, visible.length - 1);
  const available = Math.max(0, width - gaps);
  const segments: Segment[] = [];
  let x = 0;
  for (const fraction of visible) {
    const segmentWidth = clampFraction(fraction) * available;
    segments.push({ x: round(x), width: round(segmentWidth) });
    x += segmentWidth + gap;
  }
  return segments;
}

export type TooltipBox = { x: number; y: number; width: number; height: number };

/**
 * The hover tooltip's box: 8 px from the plot's top, left-aligned in the first
 * half and right-aligned in the second, as in charts.slint.
 */
export function tooltipBox(
  plotWidth: number,
  slot: number,
  count: number,
  index: number,
  partial: boolean,
): TooltipBox {
  const width = Math.min(TOOLTIP_WIDTH, Math.max(0, plotWidth));
  const preferred = index < count / 2 ? slot * index : slot * (index + 1) - width;
  const x = Math.max(0, Math.min(plotWidth - width, preferred));
  const height = partial ? 92 : 74;
  return { x: round(x), y: 8, width: round(width), height };
}
