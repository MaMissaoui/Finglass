import { describe, expect, it } from 'vitest';
import { splitCurrencyAmounts } from '../../src/lib/currencyAmounts';

describe('splitCurrencyAmounts', () => {
  it('keeps the first amount as the main figure and the rest as further currencies', () => {
    const rows = [
      { text: '€25.000,00', tone: 'positive' },
      { text: '$35,97', tone: 'positive' },
      { text: 'CHF 12,00', tone: 'positive' },
    ];

    const split = splitCurrencyAmounts(rows);

    expect(split.main).toEqual(rows[0]);
    expect(split.further).toEqual(rows.slice(1));
  });

  it('has no main figure when the list is empty', () => {
    expect(splitCurrencyAmounts([])).toEqual({ main: undefined, further: [] });
  });

  it('has no further line for a single currency', () => {
    const rows = [{ text: '€7,50', tone: 'negative' }];

    expect(splitCurrencyAmounts(rows)).toEqual({ main: rows[0], further: [] });
  });
});
