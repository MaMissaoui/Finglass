import { describe, expect, it } from 'vitest';
import { createMockTransactions } from '../../src/lib/transactions.mock';

function fresh(scenario = 'transactions-import-csv-en') {
  return createMockTransactions(scenario, 'en');
}

describe('the transactions mock import fake', () => {
  it('opens the CSV preview the reference shows', () => {
    const mock = fresh('transactions-en-light');
    expect(mock.state().importOpen).toBe(false);
    const dto = mock.openImport();
    expect(dto.importOpen).toBe(true);
    expect(dto.importTitle).toBe('Import 3 files');
    expect(dto.importSummary).toBe('4 bookings');
    expect(dto.importFiles).toEqual([
      {
        name: 'oktober.csv',
        countText: '2 bookings',
        periodText: '02.08.2026–21.10.2026',
        warningText: '',
        failure: false,
      },
      {
        name: 'november.csv',
        countText: '2 bookings',
        periodText: '05.11.2026–09.11.2026',
        warningText: '',
        failure: false,
      },
      {
        name: 'missing.csv',
        countText: '',
        periodText: '',
        warningText: 'File not found: missing.csv',
        failure: true,
      },
    ]);
    expect(dto.importCsvFields.map((field) => [field.field, field.label, field.index])).toEqual([
      ['date', 'Date', 1],
      ['amount', 'Amount', 2],
      ['debit', 'Debit', 0],
      ['credit', 'Credit', 0],
      ['partner', 'Partner', 3],
      ['purpose', 'Purpose', 4],
      ['balance', 'Balance', 5],
    ]);
    expect(dto.importCsvHint).toBe('Column changes apply to the 2 files with these columns.');
    expect(dto.importAccountOptions).toEqual([
      'Gemeinschaftskonto Haushalt und Reisen',
      'Rücklage',
      'Dollar',
    ]);
    expect(dto.importAccountIndex).toBe(0);
    expect(dto.importBalanceAllowed).toBe(true);
    expect(dto.importUpdateBalance).toBe(true);
    expect(dto.importBalanceText).toBe(
      'Set the account balance from the newest booking in the file',
    );
    expect(dto.importNewerText).toBe('');
    expect(dto.importConfirmText).toBe('Import 4');
    expect(dto.importConfirmEnabled).toBe(true);
  });

  it('remaps a CSV field and keeps amount, debit and credit exclusive', () => {
    const mock = fresh();
    mock.openImport();
    let dto = mock.importCsvChanged('partner', 'Saldo');
    const partner = dto.importCsvFields.find((field) => field.field === 'partner');
    expect(partner?.index).toBe(5);
    dto = mock.importCsvChanged('debit', 'Betrag');
    const debit = dto.importCsvFields.find((field) => field.field === 'debit');
    const amount = dto.importCsvFields.find((field) => field.field === 'amount');
    expect(debit?.index).toBe(2);
    expect(amount?.index).toBe(0);
  });

  it('changes the import account and confirms the previewed bookings', () => {
    const mock = fresh();
    mock.openImport();
    const before = mock.state().rows.length;
    let dto = mock.importAccountChanged(2);
    expect(dto.importAccountIndex).toBe(2);
    expect(dto.importConfirmEnabled).toBe(true);
    dto = mock.confirmImport(true);
    expect(dto.importOpen).toBe(false);
    expect(dto.rows.length).toBe(before + 4);
    expect(dto.messageText).toBe('Imported 4 transactions from 3 files');
  });

  it('cancels the preview without importing', () => {
    const mock = fresh();
    mock.openImport();
    const before = mock.state().rows.length;
    const dto = mock.cancelImport();
    expect(dto.importOpen).toBe(false);
    expect(dto.rows.length).toBe(before);
    expect(dto.messageText).toBe('');
  });

  it('offers an MT940 preview with balances and no CSV mapping', () => {
    const dto = fresh('transactions-import-mt940-en').openImport();
    expect(dto.importTitle).toBe('Import kontoauszug.mt940');
    expect(dto.importSummary).toBe('');
    expect(dto.importFiles.map((file) => file.name)).toEqual(['kontoauszug.mt940']);
    expect(dto.importCsvFields).toEqual([]);
    expect(dto.importCsvHint).toBe('');
    expect(dto.importBalanceAllowed).toBe(true);
    expect(dto.importConfirmText).toBe('Import 3');
  });

  it('shows warnings, the newer-bookings note and an unreadable file', () => {
    const dto = fresh('transactions-import-warnings-en').openImport();
    expect(dto.importTitle).toBe('Import 2 files');
    expect(dto.importSummary).toBe('2 bookings');
    expect(dto.importFiles[0]?.warningText).toBe('2 pending left out · 1 unreadable');
    expect(dto.importFiles[1]).toEqual({
      name: 'defekt.csv',
      countText: '',
      periodText: '',
      warningText: 'File is too large',
      failure: true,
    });
    expect(dto.importNewerText).toBe(
      'The account has newer bookings than this file, so its balance is left unchanged.',
    );
  });

  it('shows the export and attach messages', () => {
    const exported = fresh().exportCsv();
    expect(exported.messageText).toBe('Exported 34 transactions to transactions-2026-10-15.csv');
    const mock = fresh('transactions-detail-en');
    const attached = mock.attachReceipt();
    expect(attached.messageText).toBe('Document linked');
  });
});
