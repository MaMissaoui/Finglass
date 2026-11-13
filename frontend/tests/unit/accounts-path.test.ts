import { describe, expect, it } from 'vitest';
import {
  balancePathGeometry,
  chartHeight,
  chartPadding,
  chartWidth,
} from '../../src/lib/accountsPath';

describe('balancePathGeometry', () => {
  it('spreads two points across the chart and puts the high one at the top', () => {
    const geometry = balancePathGeometry([{ minor: 100 }, { minor: 200 }]);
    expect(geometry.path).toBe('M 0.00 194.00 L 700.00 6.00');
    expect(geometry.lowMinor).toBe(100);
    expect(geometry.highMinor).toBe(200);
    expect(geometry.endX).toBe(chartWidth);
    expect(geometry.endY).toBe(chartPadding);
  });

  it('draws a flat history on the middle line with no range', () => {
    const geometry = balancePathGeometry([{ minor: 0 }, { minor: 0 }, { minor: 0 }]);
    expect(geometry.path).toBe('M 0.00 100.00 L 350.00 100.00 L 700.00 100.00');
    expect(geometry.highMinor).toBe(geometry.lowMinor);
    expect(geometry.endY).toBe(chartHeight / 2);
  });

  it('puts a single point in the middle of the chart', () => {
    const geometry = balancePathGeometry([{ minor: 500 }]);
    expect(geometry.path).toBe('M 350.00 100.00');
    expect(geometry.endX).toBe(chartWidth / 2);
    expect(geometry.endY).toBe(chartHeight / 2);
  });

  it('returns an empty geometry without points', () => {
    expect(balancePathGeometry([])).toEqual({
      path: '',
      lowMinor: 0,
      highMinor: 0,
      endX: 0,
      endY: 0,
    });
  });
});
