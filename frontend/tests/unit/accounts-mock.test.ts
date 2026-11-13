import { describe, expect, it } from 'vitest';
import { createMockAccounts } from '../../src/lib/accounts.mock';
import { translate } from '../../src/lib/i18n';

function fake(scenario = 'accounts') {
  return createMockAccounts(scenario, 'en');
}

describe('accounts.mock', () => {
  it('builds the reference page, its figures and its cards', () => {
    const state = fake().state();
    expect(state.hasContent).toBe(true);
    expect(state.figures.map((figure) => figure.label)).toEqual([
      translate('en', 'AccountsNetLabel', 7),
      translate('en', 'AccountsLoansLabel'),
    ]);
    expect(state.figures[0]?.values.map((value) => value.text)).toEqual(['€8,531.67', '$500.00']);
    expect(state.figures[1]?.values[0]?.text).toBe('-€5,000.00');
    expect(state.bankCards.map((card) => card.name)).toEqual([
      'Bank Norwegian',
      'Sparkasse',
      translate('en', 'AccountsWithoutBank'),
    ]);
    expect(state.bankCards[0]?.accounts.map((row) => row.name)).toEqual([
      'Haushalt',
      'Dollar',
      'Festgeld',
    ]);
    expect(state.bankCards[2]?.totals[0]?.text).toBe('-€5,045.10');
  });

  it('builds the panel rows and the saved selection', () => {
    const state = fake().state();
    expect(state.panelRows.map((row) => row.label)).toEqual([
      'All accounts',
      'Checking',
      'Haushalt',
      'Girokonto',
      'Tagesgeld',
      'Savings',
      'Festgeld',
      'Credit card',
      'Credit card …4321',
      'Loan',
      'Loan …4930',
      'Reisen',
      'Dollar',
    ]);
    expect(state.panelRows[0]?.selected).toBe(true);
    expect(state.panelRows[0]?.newCount).toBe(1);
    expect(state.groupChoices).toEqual([{ id: 1, name: 'Reisen' }]);
  });

  it('filters the panel and reports how many accounts are shown', () => {
    const accounts = fake();
    const state = accounts.filterChanged('Dollar');
    expect(state.shownText).toBe('1 of 7');
    expect(state.panelRows.filter((row) => row.kind === 'account').map((row) => row.label)).toEqual(
      ['Dollar'],
    );
    expect(state.panelRows[0]?.label).toBe('All accounts');

    const none = accounts.filterChanged('nothing matches this');
    expect(none.shownText).toBe('0 of 7');
    expect(none.panelRows.filter((row) => row.kind === 'account')).toEqual([]);
  });

  it('validates and adds an offline bank', () => {
    const accounts = fake();
    accounts.openAddBank();
    const invalid = accounts.submitAddBank('   ');
    expect(invalid.dialog.errorText).toBe(translate('en', 'BankNameRequired'));
    expect(invalid.dialog.kind).toBe('add-bank');

    const added = accounts.submitAddBank('  Test Bank  ');
    expect(added.dialog.kind).toBe('');
    expect(added.messageText).toBe(translate('en', 'MessageAdded', 'Test Bank'));
    expect(added.bankCards.map((card) => card.name)).toContain('Test Bank');
  });

  it('validates and adds an account, keeping the localized error', () => {
    const accounts = fake();
    accounts.openAddAccount(1);
    const short = accounts.submitAddAccount(1, 'Testkonto', '123', 'credit_card', 'EUR', '0');
    expect(short.dialog.errorText).toBe(translate('en', 'CardLast4Only'));
    expect(short.dialog.kind).toBe('add-account');

    const badBalance = accounts.submitAddAccount(
      1,
      'Testkonto',
      '9999',
      'credit_card',
      'EUR',
      'abc',
    );
    expect(badBalance.dialog.errorText).toBe(translate('en', 'BalanceNotANumber'));

    const added = accounts.submitAddAccount(1, 'Testkonto', '9999', 'credit_card', 'EUR', '12.34');
    expect(added.dialog.kind).toBe('');
    expect(added.messageText).toBe(translate('en', 'MessageAccountAdded'));
    expect(added.bankCards[0]?.accounts.map((row) => row.name)).toContain('Testkonto');
  });

  it('validates and stores a balance and account type', () => {
    const accounts = fake();
    accounts.openBalance(1);
    const invalid = accounts.submitBalance(1, 'nope', 'checking');
    expect(invalid.dialog.errorText).toBe(translate('en', 'EnterValidBalance'));

    const saved = accounts.submitBalance(1, '2500.00', 'savings');
    expect(saved.dialog.kind).toBe('');
    expect(saved.bankCards[0]?.accounts[0]?.balanceText).toBe('€2,500.00');
    expect(saved.panelRows.some((row) => row.label === 'Savings')).toBe(true);
    const labels = saved.panelRows.map((row) => row.label);
    expect(labels.indexOf('Haushalt')).toBeGreaterThan(labels.indexOf('Savings'));
  });

  it('renames and deletes an account', () => {
    const accounts = fake();
    accounts.openRename(1);
    const renamed = accounts.submitRename(1, 'Zuhause');
    expect(renamed.bankCards[0]?.accounts[0]?.name).toBe('Zuhause');

    accounts.openDeleteAccount(1);
    const deleted = accounts.confirmDeleteAccount(1);
    expect(deleted.messageText).toBe(translate('en', 'MessageAccountDeleted'));
    expect(deleted.bankCards[0]?.accounts.map((row) => row.name)).not.toContain('Zuhause');
  });

  it('removes an offline bank with its accounts', () => {
    const accounts = fake();
    const opened = accounts.openDeleteBank(1);
    expect(opened.dialog.title).toBe(translate('en', 'RemoveBankTitle', 'Bank Norwegian'));
    const deleted = accounts.confirmDeleteBank(1);
    expect(deleted.messageText).toBe(translate('en', 'MessageBankRemoved'));
    expect(deleted.bankCards.map((card) => card.name)).not.toContain('Bank Norwegian');
  });

  it('creates, renames, moves and deletes a custom group', () => {
    const accounts = fake();
    accounts.newGroup('Urlaub');
    expect(accounts.state().panelRows.map((row) => row.label)).toContain('Urlaub');

    accounts.renameGroup(2, 'Urlaub 2');
    expect(accounts.state().panelRows.map((row) => row.label)).toContain('Urlaub 2');

    accounts.moveGroup(2, true);
    const labels = accounts.state().panelRows.map((row) => row.label);
    expect(labels.indexOf('Urlaub 2')).toBeLessThan(labels.indexOf('Reisen'));

    accounts.deleteGroup(1);
    const deleted = accounts.state();
    expect(deleted.panelRows.map((row) => row.label)).not.toContain('Reisen');
    expect(deleted.panelRows.map((row) => row.label)).toContain('Dollar');
  });

  it('moves an account into a custom group and back', () => {
    const accounts = fake();
    const moved = accounts.moveAccount(1, 1);
    const labels = moved.panelRows.map((row) => row.label);
    expect(labels.indexOf('Haushalt')).toBeGreaterThan(labels.indexOf('Reisen'));

    const back = accounts.moveAccount(1, -1);
    const backLabels = back.panelRows.map((row) => row.label);
    expect(backLabels.indexOf('Haushalt')).toBeLessThan(backLabels.indexOf('Reisen'));
  });

  it('marks rows read and collapses groups', () => {
    const accounts = fake();
    const read = accounts.markRead('all', '', -1, '');
    expect(read.panelRows[0]?.newCount).toBe(0);

    const collapsed = accounts.toggleGroup('group', 'type', -1, 'checking');
    expect(collapsed.panelRows.map((row) => row.label)).not.toContain('Haushalt');
    expect(collapsed.panelRows.find((row) => row.label === 'Checking')?.collapsed).toBe(true);
  });

  it('opens, resizes and closes the detail view', () => {
    const accounts = fake();
    const opened = accounts.openDetail(1);
    expect(opened.detailOpen).toBe(true);
    expect(opened.detailDays).toBe(90);
    expect(opened.detailPath.startsWith('M ')).toBe(true);
    expect(opened.detailRows).toHaveLength(5);

    const thirty = accounts.selectDays(1, 30);
    expect(thirty.detailDays).toBe(30);
    expect(thirty.detailSummary).toContain('30');

    expect(accounts.closeDetail().detailOpen).toBe(false);
  });

  it('draws the flat reference detail without a bottom axis label', () => {
    const state = createMockAccounts('accounts-detail-flat-de', 'de').state();
    expect(state.detailOpen).toBe(true);
    expect(state.detailDays).toBe(90);
    expect(state.detailAxisTop).toBe('1\u00a0€');
    expect(state.detailAxisBottom).toBe('');
    expect(state.detailRows).toEqual([]);
  });

  it('starts the empty page without content', () => {
    const state = fake('accounts-empty').state();
    expect(state.hasContent).toBe(false);
    expect(state.showContent).toBe(false);
    expect(state.bankCards).toEqual([]);
    expect(state.figures).toEqual([]);
  });

  it('shows the no-deleted message and dismisses it', () => {
    const accounts = fake();
    const shown = accounts.forgetDeleted(1);
    expect(shown.messageText).toBe(translate('en', 'MessageNoDeletedRemembered'));
    const dismissed = accounts.dismissMessage();
    expect(dismissed.messageText).toBe('');
  });

  it('publishes every state change to its listeners', () => {
    const accounts = fake();
    const seen: string[] = [];
    const stop = accounts.subscribe((state) => seen.push(state.filterText));
    accounts.filterChanged('Dollar');
    accounts.filterChanged('Reisen');
    stop();
    accounts.filterChanged('Festgeld');
    expect(seen).toEqual(['Dollar', 'Reisen']);
  });
});
