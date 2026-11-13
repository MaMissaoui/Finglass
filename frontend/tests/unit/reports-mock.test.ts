import { describe, expect, it } from 'vitest';
import { createMockApi } from '../../src/lib/api.mock';
import { translate } from '../../src/lib/i18n';

function apiFor(scenario: string) {
  return createMockApi(scenario);
}

describe('the mock reports fake', () => {
  it('serves the summary reference figures and texts', async () => {
    const api = apiFor('reports-summary-en-light');
    const shell = await api.state();
    expect(shell.shellOpen).toBe(true);
    expect(shell.screen).toBe('reports');

    const dto = await api.reportsState();
    expect(dto.tab).toBe('summary');
    expect(dto.yearLabel).toBe('2026');
    expect(dto.loading).toBe(false);
    expect(dto.exporting).toBe(false);
    expect(dto.custom).toBe(false);
    expect(dto.hasReport).toBe(true);
    expect(dto.empty).toBe(false);
    expect(dto.canExportReport).toBe(true);
    expect(dto.exportDisabledReason).toBe('');
    expect(dto.linkable).toBe(true);

    expect(dto.otherCurrencyHint).toBe(translate('en', 'OtherCurrenciesExcluded', 'EUR', 7));
    expect(dto.incomeValue).toBe('€25,000.00');
    expect(dto.expensesValue).toBe('€928.31');
    expect(dto.netValue).toBe('€24,071.69');
    expect(dto.taxValue).toBe('€514.00');
    expect(dto.netTone).toBe('positive');

    expect(dto.taxRelevantRows.map((row) => row.label)).toEqual([
      'Handwerk',
      'Donations',
      'Health',
    ]);
    expect(dto.taxRelevantRows.map((row) => row.amountText)).toEqual([
      '€250.00',
      '€150.00',
      '€114.00',
    ]);
    expect(dto.taxRelevantRows[0]?.fraction).toBe(1);
    expect(dto.taxRelevantRows[1]?.fraction).toBeCloseTo(0.6, 5);
    expect(dto.taxRelevantRows[2]?.fraction).toBeCloseTo(0.456, 5);
    expect(dto.taxRelevantEmpty).toBe('');

    expect(dto.statTransactions).toBe('27');
    expect(dto.statCategories).toBe('5');
    expect(dto.statAverage).toBe('€116.04');
    expect(dto.statHighest).toBe('June €289.98');
  });

  it('serves the category shares and vendors', async () => {
    const dto = await apiFor('reports-category-en-light').reportsState();
    expect(dto.tab).toBe('category');
    expect(dto.categoryRows.map((row) => row.label)).toEqual([
      'Uncategorized',
      'Handwerk',
      'Donations',
      'Food',
      'Health',
    ]);
    expect(dto.categoryRows.map((row) => row.percentText)).toEqual([
      '32%',
      '27%',
      '16%',
      '13%',
      '12%',
    ]);
    expect(dto.categoryRows.map((row) => row.amountText)).toEqual([
      '€294.31',
      '€250.00',
      '€150.00',
      '€120.00',
      '€114.00',
    ]);
    expect(dto.categoryRows.map((row) => row.categoryId)).toEqual([-1, 21, 22, 11, 23]);
    expect(dto.categoryRows.every((row) => row.isMore === false)).toBe(true);
    expect(dto.categoryRows[1]?.fraction).toBeCloseTo(25000 / 92831, 5);
    expect(dto.transfersNote).toBe('');
    expect(dto.vendorRows[0]).toMatchObject({
      rank: '1.',
      name: 'Beispiel Werk GmbH',
      amountText: '€250.00',
    });
    expect(dto.vendorRows).toHaveLength(6);
  });

  it('serves the monthly rows and their totals', async () => {
    const dto = await apiFor('reports-monthly-en-light').reportsState();
    expect(dto.tab).toBe('monthly');
    expect(dto.monthlyTitle).toBe(translate('en', 'MonthlyIncomeExpenses', '2026'));
    expect(dto.monthlyRows).toHaveLength(12);
    expect(dto.monthlyRows[0]).toMatchObject({
      month: 'January',
      incomeText: '€2,500.00',
      expensesText: '€0.00',
      netText: '+€2,500.00',
      incomeZero: false,
      expensesZero: true,
      netTone: 'positive',
    });
    expect(dto.monthlyRows[2]).toMatchObject({
      month: 'March',
      expensesText: '€100.00',
      netText: '+€2,400.00',
    });
    expect(dto.monthlyRows[5]).toMatchObject({
      month: 'June',
      expensesText: '€289.98',
      netText: '+€2,210.02',
    });
    expect(dto.monthlyRows[10]).toMatchObject({
      month: 'November',
      incomeZero: true,
      expensesZero: true,
      netZero: true,
      netTone: 'zero',
    });
    expect(dto.monthlyTotalIncome).toBe('€25,000.00');
    expect(dto.monthlyTotalExpenses).toBe('€928.31');
    expect(dto.monthlyTotalNet).toBe('+€24,071.69');
    expect(dto.monthlyTotalTone).toBe('positive');
  });

  it('serves the recurring payments and per-currency cost lines', async () => {
    const dto = await apiFor('reports-recurring-en-light').reportsState();
    expect(dto.tab).toBe('recurring');
    expect(dto.recurringEmpty).toBe(false);
    expect(dto.recurringCount).toBe('3');
    expect(dto.recurringYearly).toBe('€479.76\n$143.88');
    expect(dto.recurringMonthly).toBe('€39.98\n$11.99');
    expect(dto.recurringRows.map((row) => row.name)).toEqual([
      'Employer GmbH',
      'Beispiel Versicherungs- und Vorsorgesellschaft Süddeutschland AG Beitrag',
      'Cloud Service',
      'Spotify',
    ]);
    expect(dto.recurringRows[0]).toMatchObject({
      meta: '10x seen · Salary',
      cadence: 'Monthly',
      nextText: '01.11.2026',
      amountText: '+€2,500.00',
      yearlyText: '€30,000.00',
      income: true,
    });
    expect(dto.recurringRows[2]).toMatchObject({
      amountText: '-$11.99',
      yearlyText: '$143.88',
      income: false,
    });
  });

  it('serves the tax tab, its suggestions and the missing-receipt filter', async () => {
    const api = apiFor('reports-tax-missing-en-light');
    const dto = await api.reportsState();
    expect(dto.tab).toBe('tax');
    expect(dto.hasTax).toBe(true);
    expect(dto.taxEmpty).toBe(false);
    expect(dto.missingReceiptsOnly).toBe(true);
    expect(dto.taxIncomeValue).toBe('€25,000.00');
    expect(dto.taxDeductionValue).toBe('€514.00');
    expect(dto.taxNetValue).toBe('€24,486.00');
    expect(dto.taxNetTone).toBe('positive');
    expect(dto.taxReceiptsMissing).toBe(true);
    expect(dto.taxReceiptsValue).toBe(translate('en', 'ReceiptsMissing', 14));
    expect(dto.taxReceiptsSubtitle).toBe(translate('en', 'TaxReceiptsOf', 1, 15));
    expect(dto.taxSuggestionsTitle).toBe(translate('en', 'TaxSuggestionsTitle', 2));
    expect(dto.taxSuggestions.map((row) => row.title)).toEqual([
      'Beispiel-Krankenkasse',
      'Apotheke am Ring',
    ]);
    expect(dto.taxSuggestions[0]?.amountText).toBe('-€98.90');
    expect(dto.taxSuggestions[1]?.keywordText).toBe(translate('en', 'MatchesKeyword', 'Apotheke'));

    const sections = dto.taxRows.filter((row) => row.kind === 'section');
    expect(sections.map((row) => row.title)).toEqual([
      translate('en', 'TaxSectionIncome'),
      translate('en', 'TaxSectionDeductions'),
    ]);
    const bookings = dto.taxRows.filter((row) => row.kind === 'booking');
    expect(bookings).toHaveLength(14);
    expect(bookings.some((row) => row.hasReceipt)).toBe(false);
    expect(bookings[0]).toMatchObject({
      dateText: '31.01.2026',
      accountText: '…0000',
      amountText: '+€2,500.00',
      expense: false,
      missingReceipt: true,
    });

    const unfiltered = await api.reportsSetMissingOnly(false);
    expect(unfiltered.missingReceiptsOnly).toBe(false);
    expect(unfiltered.taxRows.filter((row) => row.kind === 'booking')).toHaveLength(15);
    expect(unfiltered.taxRows.some((row) => row.hasReceipt)).toBe(true);
    const reset = await api.reportsSetMissingOnly(true);
    expect(reset.missingReceiptsOnly).toBe(true);
  });

  it('applies the tax actions and reports them through the DTO', async () => {
    const api = apiFor('reports-tax-en-light');
    const initial = await api.reportsState();
    expect(initial.taxSuggestions).toHaveLength(2);

    const accepted = await api.reportsAcceptSuggestion(901);
    expect(accepted.taxSuggestions.map((row) => row.id)).toEqual([902]);
    expect(accepted.messageText).toBe(translate('en', 'MessageTaxOverrideSet', 1));
    expect(accepted.taxDeductionValue).toBe('€612.90');

    const dismissed = await api.reportsDismissSuggestion(902);
    expect(dismissed.taxSuggestions).toEqual([]);

    const rows = dismissed.taxRows.filter((row) => row.kind === 'booking' && row.canReset);
    const resetId = rows[0]?.id ?? -1;
    expect(resetId).toBeGreaterThan(0);
    const reset = await api.reportsResetTaxAutomatic(resetId);
    expect(reset.taxRows.some((row) => row.id === resetId && row.canReset)).toBe(false);
    expect(reset.messageText).toBe(translate('en', 'MessageTaxOverrideSet', 1));

    const marked = await api.reportsMarkNotTaxRelevant(resetId);
    expect(marked.taxRows.some((row) => row.id === resetId)).toBe(false);
    expect(marked.messageText).toBe(translate('en', 'MessageTaxOverrideSet', 1));

    const cleared = await api.reportsDismiss();
    expect(cleared.messageText).toBe('');
    expect(cleared.errorText).toBe('');
  });

  it('validates and applies the custom range', async () => {
    const api = apiFor('reports-summary-en-light');
    const initial = await api.reportsState();
    expect(initial.rangeFrom).toBe('01.08.2026');
    expect(initial.rangeTo).toBe('15.10.2026');

    const badDate = await api.reportsApplyRange('not a date', '31.12.2026');
    expect(badDate.rangeError).toBe(translate('en', 'EnterValidDate'));
    expect(badDate.custom).toBe(false);

    const reversed = await api.reportsApplyRange('31.12.2026', '01.01.2026');
    expect(reversed.rangeError).toBe(translate('en', 'InvalidDateRange'));

    const applied = await api.reportsApplyRange('01.12.2025', '31.01.2026');
    expect(applied.rangeError).toBe('');
    expect(applied.custom).toBe(true);
    expect(applied.rangeFrom).toBe('01.12.2025');
    expect(applied.rangeTo).toBe('31.01.2026');
    expect(applied.linkable).toBe(false);

    const cleared = await api.reportsApplyRange('01.06.2026', '30.06.2026');
    expect(cleared.rangeError).toBe('');
    expect(cleared.custom).toBe(true);

    const calendar = await api.reportsSetCustom(false);
    expect(calendar.custom).toBe(false);
    expect(calendar.rangeFrom).toBe('01.08.2026');
    expect(calendar.rangeTo).toBe('15.10.2026');
  });

  it('changes the selected year', async () => {
    const api = apiFor('reports-summary-de-dark');
    const previous = await api.reportsChangeYear(-1);
    expect(previous.year).toBe(2025);
    expect(previous.yearLabel).toBe('2025');
    expect(previous.linkable).toBe(true);
    const next = await api.reportsChangeYear(1);
    expect(next.yearLabel).toBe('2026');
    const older = await api.reportsChangeYear(-2);
    expect(older.yearLabel).toBe('2024');
    expect(older.linkable).toBe(false);
  });

  it('calls each export and reports its mock message', async () => {
    const api = apiFor('reports-export-menu-en-light');
    const reportPDF = await api.reportsExportReportPDF();
    expect(reportPDF.messageText).toBe(translate('en', 'MessageSavedTo', 'report-2026.pdf'));
    const csv = await api.reportsExportCSV();
    expect(csv.messageText).toBe(translate('en', 'MessageSavedTo', 'finglass-2026.csv'));
    const xlsx = await api.reportsExportTaxXLSX();
    expect(xlsx.messageText).toBe(translate('en', 'MessageSavedTo', 'tax-bookings-2026.xlsx'));
    const pdf = await api.reportsExportTaxPDF();
    expect(pdf.messageText).toBe(translate('en', 'MessageSavedTo', 'tax-bookings-2026.pdf'));
    const printed = await api.reportsPrintTax();
    expect(printed.messageText).toBe(translate('en', 'MessagePrintSent'));
  });

  it('opens Transactions on a linkable category row', async () => {
    const api = apiFor('reports-category-en-light');
    const dto = await api.reportsOpenCategory(21);
    expect(dto.categoryRows[1]?.categoryId).toBe(21);
    const shell = await api.state();
    expect(shell.screen).toBe('transactions');
    expect(shell.backTitle).toBe(translate('en', 'Back'));
  });

  it('publishes every change to the reports listeners', async () => {
    const api = apiFor('reports-summary-en-light');
    const seen: string[] = [];
    const stop = api.onReportsChanged((dto) => seen.push(dto.yearLabel));
    await api.reportsChangeYear(-1);
    await api.reportsChangeYear(1);
    stop();
    await api.reportsChangeYear(-1);
    expect(seen).toEqual(['2025', '2026']);
  });
});
