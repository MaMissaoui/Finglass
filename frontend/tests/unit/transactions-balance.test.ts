import { describe, expect, it } from 'vitest';
import {
  addDays,
  chartGeometry,
  computeRunning,
  historyFromRunning,
  isoDay,
  isoFromDay,
} from '../../src/lib/transactionsBalance';

function booking(
  id: number,
  date: string,
  minor: number,
  balanceAfter: number | null = null,
): { id: number; date: string; minor: number; balanceAfter: number | null } {
  return { id, date, minor, balanceAfter };
}

describe('transactions balance geometry', () => {
  it('adds and converts epoch days', () => {
    expect(isoFromDay(isoDay('2026-10-15') + 1)).toBe('2026-10-16');
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01');
  });

  it('walks the running balance backwards through the newest-first list', () => {
    const running = computeRunning(1000, [
      booking(2, '2026-10-02', -300),
      booking(1, '2026-10-01', -200),
    ]);
    expect(running.get(2)?.balance).toBe(1000);
    expect(running.get(1)?.balance).toBe(1300);
    expect(running.get(1)?.bankDifference).toBeNull();
  });

  it('keeps a bank balance and records its difference', () => {
    const running = computeRunning(1000, [booking(1, '2026-10-01', -300, 900)]);
    expect(running.get(1)?.balance).toBe(900);
    expect(running.get(1)?.bankDifference).toBe(-100);
  });

  it('reconciles a whole day when the bank balance matches both ends', () => {
    const running = computeRunning(1000, [
      booking(2, '2026-10-01', -200, 1000),
      booking(1, '2026-10-01', -100, 1200),
    ]);
    expect(running.get(2)?.balance).toBe(1000);
    expect(running.get(1)?.balance).toBe(1200);
    expect(running.get(1)?.bankDifference).toBeNull();
    expect(running.get(2)?.bankDifference).toBeNull();
  });

  it('produces one end-of-day point per day, oldest first', () => {
    const newestFirst = [booking(2, '2026-10-02', -300), booking(1, '2026-10-01', -200)];
    const running = computeRunning(1000, newestFirst);
    const points = historyFromRunning(newestFirst, running, '2026-10-01', '2026-10-03');
    expect(points).toEqual([
      { date: '2026-10-01', minor: 1300 },
      { date: '2026-10-02', minor: 1000 },
      { date: '2026-10-03', minor: 1000 },
    ]);
  });

  it('draws a 700 × 200 path with whole-money axes', () => {
    const format = {
      money: (minor: number) => `${minor / 100}`,
      wholeMoney: (minor: number) => `${minor / 100}`,
      date: (iso: string) => iso,
    };
    const chart = chartGeometry(
      [
        { date: '2026-08-02', minor: -100 },
        { date: '2026-09-01', minor: 500 },
        { date: '2026-10-15', minor: 0 },
      ],
      200,
      format,
    );
    expect(chart).not.toBeNull();
    expect(chart?.line.startsWith('M 0.00 ')).toBe(true);
    expect(chart?.line).toContain('L 700.00 ');
    expect(chart?.area.endsWith('L 700.00 200.00 L 0 200.00 Z')).toBe(true);
    expect(chart?.axisTop).toBe('5');
    expect(chart?.axisBottom).toBe('-1');
    expect(chart?.axisStart).toBe('2026-08-02');
    expect(chart?.axisMid).toBe('2026-09-01');
    expect(chart?.axisEnd).toBe('2026-10-15');
    expect(chart?.value).toBe('2');
    expect(chart?.endX).toBe(700);
    expect(chart?.endLabel).toBe('0');
    expect(chart?.notFromZero).toBe(false);
  });

  it('has no chart with under two points', () => {
    expect(
      chartGeometry([], null, {
        money: String,
        wholeMoney: String,
        date: String,
      }),
    ).toBeNull();
  });
});
