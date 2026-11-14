/**
 * Splits a DTO amount list into the figure's main line and the further
 * currencies. Views order the list primary currency first; when no currency
 * can be identified in the DTO, the first entry stays the main figure.
 */
export type CurrencyAmountSplit<T> = {
  main: T | undefined;
  further: T[];
};

export function splitCurrencyAmounts<T>(rows: readonly T[]): CurrencyAmountSplit<T> {
  if (rows.length === 0) {
    return { main: undefined, further: [] };
  }
  return { main: rows[0], further: rows.slice(1) };
}
