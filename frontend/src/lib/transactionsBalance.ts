/**
 * The running-balance and chart geometry the Transactions list and its chart
 * use. This is a direct port of internal/core/balance/running.go and
 * history.go plus the chart walk in internal/bind/transactions.go, so the mock
 * and the real page agree on every coordinate.
 */

export type BalanceBooking = {
  id: number;
  /** The booking date as an ISO `YYYY-MM-DD` string. */
  date: string;
  /** The signed amount in minor units. */
  minor: number;
  /** The bank's balance after this booking, when it sent one. */
  balanceAfter: number | null;
};

export type RunningEntry = {
  balance: number;
  /** The bank balance minus the computed balance, when they differ. */
  bankDifference: number | null;
};

export type BalancePoint = { date: string; minor: number };

const MILLIS_PER_DAY = 86_400_000;

/** The epoch day of an ISO date, for day arithmetic. */
export function isoDay(iso: string): number {
  const [year, month, day] = iso.split('-').map((part) => Number.parseInt(part, 10));
  return Math.floor(Date.UTC(year ?? 1970, (month ?? 1) - 1, day ?? 1) / MILLIS_PER_DAY);
}

/** The ISO date of an epoch day. */
export function isoFromDay(day: number): string {
  const date = new Date(day * MILLIS_PER_DAY);
  const year = String(date.getUTCFullYear()).padStart(4, '0');
  const month = String(date.getUTCMonth() + 1).padStart(2, '0');
  const dayOfMonth = String(date.getUTCDate()).padStart(2, '0');
  return `${year}-${month}-${dayOfMonth}`;
}

/** The ISO date `days` after `iso`. */
export function addDays(iso: string, days: number): string {
  return isoFromDay(isoDay(iso) + days);
}

function subtractSaturating(left: number, right: number): number {
  const value = left - right;
  return Math.min(Math.max(value, Number.MIN_SAFE_INTEGER), Number.MAX_SAFE_INTEGER);
}

/**
 * The balance after each booking of one account, by transaction id.
 *
 * `newestFirst` is every booking of the account in list order (date
 * descending, id descending); `current` is the account's balance after all
 * bookings, future-dated ones included.
 */
export function computeRunning(
  current: number,
  newestFirst: BalanceBooking[],
): Map<number, RunningEntry> {
  const result = new Map<number, RunningEntry>();
  let endOfDay = current;

  const days: { date: string; rows: BalanceBooking[] }[] = [];
  for (const booking of newestFirst) {
    const last = days[days.length - 1];
    if (last !== undefined && last.date === booking.date) {
      last.rows.push(booking);
      continue;
    }
    days.push({ date: booking.date, rows: [booking] });
  }

  const hasEndBalance = (rows: BalanceBooking[], end: number): boolean =>
    rows.some((row) => row.balanceAfter !== null && row.balanceAfter === end);
  const hasStartBalance = (rows: BalanceBooking[], start: number): boolean =>
    rows.some(
      (row) =>
        row.balanceAfter !== null && subtractSaturating(row.balanceAfter, row.minor) === start,
    );

  for (const day of days) {
    let startOfDay = endOfDay;
    for (const booking of day.rows) {
      startOfDay = subtractSaturating(startOfDay, booking.minor);
    }

    const withBank = day.rows.filter((booking) => booking.balanceAfter !== null).length;
    let reconciles: boolean;
    if (withBank === 0) {
      reconciles = true;
    } else if (withBank === day.rows.length) {
      reconciles = hasEndBalance(day.rows, endOfDay) && hasStartBalance(day.rows, startOfDay);
    } else {
      reconciles = hasEndBalance(day.rows, endOfDay) || hasStartBalance(day.rows, startOfDay);
    }

    let balance = endOfDay;
    if (reconciles) {
      for (const booking of day.rows) {
        result.set(booking.id, { balance, bankDifference: null });
        balance = subtractSaturating(balance, booking.minor);
      }
    } else {
      for (const booking of day.rows) {
        const computed = balance;
        if (booking.balanceAfter !== null) {
          const difference = subtractSaturating(booking.balanceAfter, computed);
          result.set(booking.id, {
            balance: booking.balanceAfter,
            bankDifference: difference === 0 ? null : difference,
          });
          balance = booking.balanceAfter;
        } else {
          result.set(booking.id, { balance: computed, bankDifference: null });
        }
        balance = subtractSaturating(balance, booking.minor);
      }
    }

    endOfDay = balance;
  }

  return result;
}

/**
 * One balance point per day from `from` to `to`, oldest first, read from the
 * running balances. `newestFirst` is the same list `running` was built from.
 */
export function historyFromRunning(
  newestFirst: BalanceBooking[],
  running: Map<number, RunningEntry>,
  from: string,
  to: string,
): BalancePoint[] {
  if (from > to || newestFirst.length === 0) {
    return [];
  }

  const ascending = [...newestFirst].reverse();
  const oldest = ascending[0];
  if (oldest === undefined) {
    return [];
  }
  const oldestEntry = running.get(oldest.id);
  let current: number | null =
    oldestEntry === undefined ? null : subtractSaturating(oldestEntry.balance, oldest.minor);

  const points: BalancePoint[] = [];
  let index = 0;
  let day = from;
  for (;;) {
    while (index < ascending.length && (ascending[index]?.date ?? '') <= day) {
      const booking = ascending[index];
      if (booking !== undefined) {
        const entry = running.get(booking.id);
        if (entry !== undefined) {
          current = entry.balance;
        }
      }
      index += 1;
    }
    if (current !== null) {
      points.push({ date: day, minor: current });
    }
    if (day === to) {
      break;
    }
    day = addDays(day, 1);
  }

  return points;
}

export type ChartFormat = {
  money: (minor: number) => string;
  wholeMoney: (minor: number) => string;
  date: (iso: string) => string;
};

export type ChartGeometry = {
  line: string;
  area: string;
  axisTop: string;
  axisBottom: string;
  axisStart: string;
  axisMid: string;
  axisEnd: string;
  value: string;
  notFromZero: boolean;
  endX: number;
  endY: number;
  endLabel: string;
};

/** The 700 × 200 drawing box the balance chart scales into. */
export const CHART_WIDTH = 700;
export const CHART_HEIGHT = 200;
const CHART_PADDING = 8;

function fixed(value: number): string {
  return value.toFixed(2);
}

/** The chart geometry and its formatted axis labels, or null with under two points. */
export function chartGeometry(
  points: BalancePoint[],
  currentMinor: number | null,
  format: ChartFormat,
): ChartGeometry | null {
  if (points.length < 2) {
    return null;
  }
  let low = points[0]?.minor ?? 0;
  let high = low;
  for (const point of points.slice(1)) {
    low = Math.min(low, point.minor);
    high = Math.max(high, point.minor);
  }

  const span = high - low;
  const commands: string[] = [];
  let endY = 0;
  points.forEach((point, index) => {
    const x = (index / (points.length - 1)) * CHART_WIDTH;
    const ratio = span === 0 ? 0.5 : (point.minor - low) / span;
    const y = CHART_PADDING + (1 - ratio) * (CHART_HEIGHT - 2 * CHART_PADDING);
    endY = y;
    commands.push(`${index === 0 ? 'M' : 'L'} ${fixed(x)} ${fixed(y)}`);
  });
  const line = commands.join(' ');
  const area = `${line} L ${fixed(CHART_WIDTH)} ${fixed(CHART_HEIGHT)} L 0 ${fixed(CHART_HEIGHT)} Z`;
  const middle = Math.floor(points.length / 2);
  const last = points[points.length - 1];

  return {
    line,
    area,
    axisTop: format.wholeMoney(high),
    axisBottom: format.wholeMoney(low),
    axisStart: format.date(points[0]?.date ?? ''),
    axisMid: format.date(points[middle]?.date ?? ''),
    axisEnd: format.date(last?.date ?? ''),
    value: currentMinor === null ? '' : format.money(currentMinor),
    notFromZero: low > 0 || high < 0,
    endX: CHART_WIDTH,
    endY,
    endLabel: format.money(last?.minor ?? 0),
  };
}
