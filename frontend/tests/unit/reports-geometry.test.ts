import { describe, expect, it } from 'vitest';
import {
  meterWidth,
  REPORT_METER_HEIGHT,
  REPORT_METER_WIDTH,
} from '../../src/lib/charts/reportGeometry';

describe('the tax-relevant meter geometry', () => {
  it('fills the whole track for the largest category', () => {
    expect(REPORT_METER_WIDTH).toBe(160);
    expect(REPORT_METER_HEIGHT).toBe(6);
    expect(meterWidth(1)).toBe(160);
  });

  it('scales the other categories against the largest', () => {
    expect(meterWidth(0.6)).toBe(96);
    expect(meterWidth(0.456)).toBe(72.96);
  });

  it('clamps and rounds to two decimals', () => {
    expect(meterWidth(-1)).toBe(0);
    expect(meterWidth(Number.NaN)).toBe(0);
    expect(meterWidth(2)).toBe(160);
    expect(meterWidth(1 / 3)).toBe(53.33);
    expect(meterWidth(1, 0)).toBe(0);
  });
});
