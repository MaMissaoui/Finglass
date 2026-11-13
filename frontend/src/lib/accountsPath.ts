/**
 * The balance-history geometry the Accounts detail view draws.
 *
 * The Go service sends the finished path, its end point and the axis labels in
 * the DTO; the mock needs the same arithmetic to invent plausible history, and
 * this module keeps that arithmetic testable on its own.
 */
export const chartWidth = 700;
export const chartHeight = 200;
export const chartPadding = 6;

export interface BalancePoint {
  minor: number;
}

export interface BalancePathGeometry {
  /** An SVG path in the 700 × 200 viewbox; empty without points. */
  path: string;
  lowMinor: number;
  highMinor: number;
  endX: number;
  endY: number;
}

/** Builds the SVG path FinDesk's detail chart uses for one balance history. */
export function balancePathGeometry(points: readonly BalancePoint[]): BalancePathGeometry {
  const first = points[0];
  if (first === undefined) {
    return { path: '', lowMinor: 0, highMinor: 0, endX: 0, endY: 0 };
  }

  let low = first.minor;
  let high = first.minor;
  for (let index = 1; index < points.length; index += 1) {
    const point = points[index];
    if (point === undefined) {
      continue;
    }
    if (point.minor < low) {
      low = point.minor;
    }
    if (point.minor > high) {
      high = point.minor;
    }
  }

  const span = high - low;
  const segments: string[] = [];
  let endX = 0;
  let endY = 0;

  for (let index = 0; index < points.length; index += 1) {
    const point = points[index];
    if (point === undefined) {
      continue;
    }
    const x = points.length > 1 ? (index / (points.length - 1)) * chartWidth : chartWidth / 2;
    const ratio = span === 0 ? 0.5 : (point.minor - low) / span;
    const y = chartPadding + (1 - ratio) * (chartHeight - 2 * chartPadding);
    if (index === points.length - 1) {
      endX = x;
      endY = y;
    }
    segments.push(`${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`);
  }

  return {
    path: segments.join(' '),
    lowMinor: low,
    highMinor: high,
    endX,
    endY,
  };
}
