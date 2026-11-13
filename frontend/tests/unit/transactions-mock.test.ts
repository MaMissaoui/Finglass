import { describe, expect, it } from 'vitest';
import { createMockTransactions } from '../../src/lib/transactions.mock';

function fresh(scenario = 'transactions-en-light') {
  return createMockTransactions(scenario, 'en');
}

describe('the transactions mock', () => {
  it('starts with every reference figure and the pending-free list', () => {
    const dto = fresh().state();
    expect(dto.countText).toBe('34');
    expect(dto.incomeAmounts.map((amount) => amount.text)).toEqual(['€6,250.00']);
    expect(dto.expenseAmounts.map((amount) => amount.text)).toEqual(['€1,189.47', '$35.97']);
    expect(dto.hasBalance).toBe(false);
    expect(dto.hasBalanceColumn).toBe(false);
    expect(dto.hasTransactions).toBe(true);
    expect(dto.rows).toHaveLength(34);
    expect(dto.pendingHeader).toBe('');
    expect(dto.hasChart).toBe(false);
    expect(dto.showBack).toBe(false);
  });

  it('builds the shared account panel with all accounts and both groups', () => {
    const dto = fresh().state();
    const labels = dto.panelRows.map((row) => row.label);
    expect(labels).toEqual([
      'All accounts',
      'Checking',
      'Gemeinschaftskonto Haushalt und Reisen',
      'Reisen',
      'Dollar',
      'Rücklage',
    ]);
    const all = dto.panelRows[0];
    expect(all?.selected).toBe(true);
    expect(all?.newCount).toBe(12);
    expect(all?.totals.map((amount) => amount.text)).toEqual(['€10,450.37', '$500.00']);
    expect(dto.groupChoices).toEqual([{ id: 1, name: 'Reisen' }]);
  });

  it('sends only the latest search text to the filter', () => {
    const mock = fresh();
    const dto = mock.search('spotify');
    expect(dto.countText).toBe('3');
    expect(dto.rows.every((row) => row.title === 'Spotify')).toBe(true);
    expect(mock.state().countText).toBe('3');
  });

  it('applies the period, category and direction filters', () => {
    expect(fresh().periodChanged(0).countText).toBe('11');
    expect(fresh().categoryChanged(2).countText).toBe('6');
    expect(fresh().directionChanged(1).countText).toBe('7');
    expect(fresh().directionChanged(2).countText).toBe('27');
    expect(fresh().categoryChanged(1).countText).toBe('13');
  });

  it('validates and applies the custom range', () => {
    const mock = fresh();
    let dto = mock.applyRange('not a date', '01.10.2026');
    expect(dto.rangeError).toBe('Enter a valid date (dd.mm.yyyy)');
    dto = mock.applyRange('15.10.2026', '01.10.2026');
    expect(dto.rangeError).toBe('The end date must be after the start date');
    dto = mock.applyRange('01.10.2026', '15.10.2026');
    expect(dto.rangeError).toBe('');
    expect(dto.rangeOpen).toBe(false);
    expect(dto.countText).toBe('8');
    expect(dto.periodExtra).toBe('01.10.2026 – 15.10.2026');
  });

  it('validates and applies the amount bounds', () => {
    const mock = fresh();
    let dto = mock.applyAmount('oops', '');
    expect(dto.amountError).toBe('Enter a valid amount');
    dto = mock.applyAmount('100', '');
    expect(dto.amountError).toBe('');
    expect(dto.countText).toBe('10');
    expect(dto.amountLabel).toBe('≥ €100.00');
    dto = mock.applyAmount('', '20');
    expect(dto.countText).toBe('9');
    dto = mock.resetAmount();
    expect(dto.amountLabel).toBe('Any amount');
    expect(dto.countText).toBe('34');
  });

  it('selects by click, shift range, all and clear', () => {
    const mock = fresh();
    const ids = mock.state().rows.map((row) => row.id);
    let dto = mock.toggleSelect(ids[0] ?? 0);
    expect(dto.selectionVisible).toBe(true);
    expect(dto.selectionCountText).toBe('1 selected');
    dto = mock.toggleSelect(ids[2] ?? 0);
    expect(dto.selectionCountText).toBe('2 selected');
    dto = mock.selectRange(ids[1] ?? 0);
    expect(dto.selectionCountText).toBe('3 selected');
    expect(dto.selectionTotalsText).toBe('+€145.00');
    dto = mock.selectAll();
    expect(dto.selectionCountText).toBe('34 selected');
    expect(dto.allSelected).toBe(true);
    dto = mock.clearSelection();
    expect(dto.selectionVisible).toBe(false);
  });

  it('opens the single delete confirmation and deletes on confirm', () => {
    const mock = fresh();
    const before = mock.state().rows.length;
    const first = mock.state().rows[0];
    expect(first).toBeDefined();
    let dto = mock.openDelete(first?.id ?? 0);
    expect(dto.deleteOpen).toBe(true);
    expect(dto.deleteTitle).toBe('Delete transaction?');
    expect(dto.deleteText).toContain('Bürobedarf GmbH');
    dto = mock.cancelDelete();
    expect(dto.deleteOpen).toBe(false);
    dto = mock.openDelete(first?.id ?? 0);
    dto = mock.confirmDelete(dto.deleteId);
    expect(dto.rows.length).toBe(before - 1);
    expect(dto.messageText).toBe('Deleted 1 transaction');
  });

  it('shows the running balances, pending lines and chart for one account', () => {
    const mock = fresh('transactions-chart-en');
    const dto = mock.state();
    expect(dto.countText).toBe('30');
    expect(dto.hasBalance).toBe(true);
    expect(dto.balanceText).toBe('€2,450.37');
    expect(dto.hasBalanceColumn).toBe(true);
    expect(dto.pendingHeader).toBe('Pending (not yet booked) · as of 12.11.2026 18:47');
    expect(dto.rows[0]?.pending).toBe(true);
    expect(dto.rows[2]?.balanceText).toBe('€2,450.37');
    expect(dto.rows[3]?.balanceText).toBe('€2,510.37');
    expect(dto.hasChart).toBe(true);
    expect(dto.chartLine).toMatch(/^M /);
    expect(dto.chartAxisTop).toBe('€2,564');
    expect(dto.chartAxisBottom).toBe('-€447');
    expect(dto.chartEndLabel).toBe('€2,305.37');
    expect(dto.chartEndX).toBe(700);
    const collapsed = mock.toggleChart();
    expect(collapsed.chartCollapsed).toBe(true);
  });

  it('fills the detail panel, its mismatch and the tax state', () => {
    const dto = fresh('transactions-detail-en').state();
    expect(dto.detailOpen).toBe(true);
    expect(dto.detailTitle).toBe('Spotify');
    expect(dto.detailAmount).toBe('-€9.99');
    expect(dto.detailExpense).toBe(true);
    expect(dto.detailDate).toBe('10.08.2026');
    expect(dto.detailAccount).toBe('Gemeinschaftskonto Haushalt und Reisen');
    expect(dto.detailCategoryIndex).toBe(0);
    expect(dto.detailTaxIndex).toBe(0);
    expect(dto.detailTaxRelevant).toBe(false);
    expect(dto.detailTaxState).toBe('Not tax relevant');
    expect(dto.detailHasMismatch).toBe(true);
    expect(dto.detailMismatch).toContain('The bank reports €2,450.37 after this booking.');
    expect(dto.detailPartnerLink).toBe('Show all from Spotify');
  });

  it('starts the Dashboard category link with this month and Food', () => {
    const dto = fresh('transactions-from-dashboard-category-en-light').state();
    expect(dto.countText).toBe('2');
    expect(dto.categoryIndex).toBe(2);
    expect(dto.periodIndex).toBe(0);
    expect(dto.expenseAmounts.map((amount) => amount.text)).toEqual(['€165.00']);
    expect(dto.rows.map((row) => row.dateText)).toEqual(['18.10.2026', '02.10.2026']);
  });

  it('auto-categorizes uncategorized bookings and publishes the count', () => {
    const mock = fresh();
    const dto = mock.autoCategorize();
    expect(dto.messageText).toBe('Categorized 4 of 13 uncategorized transactions');
    expect(dto.rows.filter((row) => row.categoryText === 'Food')).toHaveLength(9);
  });

  it('follows a language change on every generated text', () => {
    const mock = fresh();
    mock.setLanguage('de');
    const dto = mock.state();
    expect(dto.rows[2]?.title).toBe('Example Market');
    expect(dto.categoryOptions[1]).toBe('Ohne Kategorie');
    expect(dto.periodOptions[0]).toBe('Dieser Monat');
    expect(dto.amountLabel).toBe('Jeder Betrag');
    expect(dto.expenseAmounts.map((amount) => amount.text)).toEqual([
      '1.189,47\u00a0€',
      '35,97\u00a0$',
    ]);
  });
});
