import { describe, expect, it } from 'vitest';
import {
  createMockBanking,
  type MockBankingHooks,
  type MockKnownBank,
} from '../../src/lib/banking.mock';
import { translate } from '../../src/lib/i18n';

interface Harness {
  banking: ReturnType<typeof createMockBanking>;
  dialogs: string[];
  errors: string[];
  known: MockKnownBank[];
}

function fake(scenario = 'accounts', language: 'en' | 'de' = 'en'): Harness {
  const dialogs: string[] = [];
  const errors: string[] = [];
  const known: MockKnownBank[] = [
    { id: 1, name: 'Bank Norwegian', offline: true, blz: '', bic: '' },
    { id: 2, name: 'Sparkasse', offline: false, blz: '37040044', bic: 'COLSDE33XXX' },
  ];
  const hooks: MockBankingHooks = {
    openAddBank: () => dialogs.push('add-bank'),
    setDialogError: (text) => errors.push(text),
    closeDialog: () => dialogs.push('close'),
    addOnlineBank: (bankId, name, blz, bic) => {
      known.push({ id: bankId, name, offline: false, blz, bic });
    },
    removeBank: (bankId) => {
      const index = known.findIndex((bank) => bank.id === bankId);
      if (index >= 0) {
        known.splice(index, 1);
      }
    },
    openConnectionDialog: (dialog) => dialogs.push(`connection:${dialog.bankId}`),
    openHistoryDialog: (dialog) => dialogs.push(`history:${dialog.bankId}`),
    openDeleteBank: (bankId) => dialogs.push(`delete:${bankId}`),
    listBanks: () => [...known],
    republishAccounts: () => undefined,
  };
  const banking = createMockBanking(scenario, language, hooks);
  banking.init();
  return { banking, dialogs, errors, known };
}

describe('banking.mock', () => {
  it('keeps online sync unavailable unless the scenario turns it on', () => {
    const plain = fake().banking.state();
    expect(plain.onlineSyncAvailable).toBe(false);
    expect(plain.syncControlsVisible).toBe(false);
    expect(plain.onlineSyncNote).toBe(translate('en', 'OnlineSyncNotYetAvailable'));
    expect(plain.syncAllVisible).toBe(true);

    const walkthrough = fake('banking-walkthrough').banking.state();
    expect(walkthrough.onlineSyncAvailable).toBe(true);
    expect(walkthrough.syncControlsVisible).toBe(true);
    expect(walkthrough.syncAllEnabled).toBe(true);
    expect(walkthrough.onlineSyncNote).toBe('');
  });

  it('walks the fake bank through every prompt and reports the summary', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSync(2);

    const pin = banking.prompt();
    expect(pin.kind).toBe('pin');
    expect(pin.title).toBe(translate('en', 'PinFor', 'Sparkasse'));
    expect(pin.text).toBe(translate('en', 'PinExplanation', 'meinuser'));
    expect(pin.rememberVisible).toBe(true);

    banking.answerPin(pin.id, '1234', false);
    expect(banking.prompt().title).toBe(translate('en', 'ChooseTanProcedure'));

    banking.answerChoice(banking.prompt().id, 'chiptan');
    expect(banking.prompt().title).toBe(translate('en', 'ChooseTanMedium'));

    banking.answerChoice(banking.prompt().id, 'phone');
    const tan = banking.prompt();
    expect(tan.kind).toBe('tan');
    expect(tan.imageUrl.startsWith('data:image/png;base64,')).toBe(true);

    banking.answerTan(tan.id, '555666');
    const decoupled = banking.prompt();
    expect(decoupled.kind).toBe('decoupled');
    expect(decoupled.title).toBe(translate('en', 'ConfirmInBankingApp'));

    banking.answerDecoupled(decoupled.id, true);
    expect(banking.prompt().kind).toBe('');
    const state = banking.state();
    expect(state.syncing).toBe(false);
    expect(state.messageText).toBe(translate('en', 'SyncSummary', 2));
    expect(state.errorText).toBe('');
  });

  it('ignores an answer from a replaced prompt', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSync(2);
    const pinID = banking.prompt().id;
    banking.answerPin(pinID, '1234', false);
    const method = banking.prompt();
    expect(method.kind).toBe('choice');

    banking.answerPin(pinID, '999999', false);
    expect(banking.prompt().id).toBe(method.id);
    expect(banking.prompt().title).toBe(method.title);

    banking.answerChoice(method.id - 1, 'chiptan');
    expect(banking.prompt().id).toBe(method.id);

    banking.answerChoice(method.id, 'pushtan');
    expect(banking.prompt().title).toBe(translate('en', 'ChooseTanMedium'));
  });

  it('never stores or echoes the PIN or the TAN', () => {
    const { banking } = fake('banking-walkthrough');
    const pin = 'S3cret-PIN-9931';
    const tan = '7F3K-TAN-8812';
    banking.startSync(2);
    banking.answerPin(banking.prompt().id, pin, true);
    banking.answerChoice(banking.prompt().id, 'chiptan');
    banking.answerChoice(banking.prompt().id, 'phone');
    banking.answerTan(banking.prompt().id, tan);
    banking.answerDecoupled(banking.prompt().id, true);

    const published = JSON.stringify(banking.state()) + JSON.stringify(banking.prompt());
    expect(published).not.toContain(pin);
    expect(published).not.toContain(tan);
  });

  it('marks a remembered PIN as saved without keeping the value', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSync(2);
    banking.answerPin(banking.prompt().id, '1234', true);
    banking.answerChoice(banking.prompt().id, 'chiptan');
    banking.answerChoice(banking.prompt().id, 'phone');
    banking.answerTan(banking.prompt().id, '555666');
    banking.answerDecoupled(banking.prompt().id, true);

    const sparkasse = banking.state().banks.find((bank) => bank.bankId === 2);
    expect(sparkasse?.pinText).toBe(translate('en', 'PinSavedForSyncs'));
    expect(JSON.stringify(banking.state())).not.toContain('1234');
  });

  it('cancel ends the sync and closes the prompt', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSync(2);
    banking.cancelPin(banking.prompt().id);
    expect(banking.prompt().kind).toBe('');
    const state = banking.state();
    expect(state.syncing).toBe(false);
    expect(state.errorText).toBe('');
    expect(state.messageText).toBe('');
  });

  it('ignores a cancel from a replaced prompt', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSync(2);
    const pinID = banking.prompt().id;
    banking.answerPin(pinID, '1234', false);
    banking.cancelPin(pinID);
    expect(banking.prompt().kind).toBe('choice');
    expect(banking.state().syncing).toBe(true);
  });

  it('runs Sync all with its progress text', () => {
    const { banking } = fake('banking-walkthrough');
    banking.startSyncAll();
    expect(banking.state().syncAllText).toBe(translate('en', 'SyncAllProgress', 1, 1, 'Sparkasse'));
    banking.cancelPin(banking.prompt().id);
    const after = banking.state();
    expect(after.syncAllText).toBe('');
    expect(after.syncing).toBe(false);
  });

  it('validates an online bank and reports the localized error', () => {
    const { banking, errors, known } = fake();
    banking.addOnlineBank('', '37040044', '', '', 'https://fints.example/pintan');
    expect(errors.at(-1)).toBe(translate('en', 'BankNameRequired'));

    banking.addOnlineBank('Neue Bank', '123', '', '', 'https://fints.example/pintan');
    expect(errors.at(-1)).toBe(translate('en', 'BlzMustBe8Digits'));

    banking.addOnlineBank('Neue Bank', '12345678', '', '', '');
    expect(errors.at(-1)).toBe(translate('en', 'ErrorServerUrlRequired'));

    banking.addOnlineBank('Neue Bank', '12345678', '', '', 'http://fints.example');
    expect(errors.at(-1)).toBe(translate('en', 'ErrorServerUrlMustBeHttps'));

    const added = banking.addOnlineBank(
      'Neue Bank',
      '12345678',
      'ABCDEFGH',
      'user',
      'https://fints.example/pintan',
    );
    expect(added.messageText).toBe(translate('en', 'MessageAdded', 'Neue Bank'));
    expect(known.map((bank) => bank.name)).toContain('Neue Bank');
    expect(added.banks).toHaveLength(3);
  });

  it('builds the bank menus and their enabled state', () => {
    const plain = fake().banking.state();
    const online = plain.banks.find((bank) => bank.bankId === 2);
    expect(online?.menu.map((item) => item.label)).toEqual([
      translate('en', 'TestConnection'),
      translate('en', 'FetchHistory'),
      translate('en', 'IncludeInSyncAll'),
      translate('en', 'RenameConnection'),
      translate('en', 'ConnectionSettings'),
      translate('en', 'RemoveBank'),
    ]);
    expect(online?.menu.find((item) => item.action === 'history')?.enabled).toBe(false);
    expect(online?.menu.find((item) => item.action === 'include')?.checked).toBe(true);

    const offline = plain.banks.find((bank) => bank.bankId === 1);
    expect(offline?.menu.map((item) => item.label)).toEqual([
      translate('en', 'RenameConnection'),
      translate('en', 'RemoveBank'),
    ]);

    const enabled = fake('banking-walkthrough').banking.state();
    expect(
      enabled.banks
        .find((bank) => bank.bankId === 2)
        ?.menu.find((item) => item.action === 'history')?.enabled,
    ).toBe(true);
  });

  it('opens the reference prompts from their scenarios', () => {
    const pin = fake('accounts-pin').banking.prompt();
    expect(pin.kind).toBe('pin');
    expect(pin.rememberVisible).toBe(false);

    const remember = fake('accounts-pin-remember').banking.prompt();
    expect(remember.rememberVisible).toBe(true);

    expect(fake('accounts-tan-method').banking.prompt().title).toBe(
      translate('en', 'ChooseTanProcedure'),
    );
    expect(fake('accounts-tan-medium').banking.prompt().title).toBe(
      translate('en', 'ChooseTanMedium'),
    );

    const photo = fake('accounts-tan-photo').banking.prompt();
    expect(photo.kind).toBe('tan');
    expect(photo.imageUrl.startsWith('data:image/png;base64,')).toBe(true);

    const text = fake('accounts-tan-text').banking.prompt();
    expect(text.kind).toBe('tan');
    expect(text.imageUrl).toBe('');

    expect(fake('accounts-decoupled').banking.prompt().title).toBe(
      translate('en', 'ConfirmInBankingApp'),
    );
  });

  it('deletes an online bank and reports it', () => {
    const { banking, dialogs, known } = fake('banking-walkthrough');
    const state = banking.deleteBank(2);
    expect(state.banks.map((bank) => bank.bankId)).toEqual([1]);
    expect(state.messageText).toBe(translate('en', 'MessageBankRemoved'));
    expect(known.map((bank) => bank.id)).not.toContain(2);
    expect(dialogs).toContain('close');
  });

  it('opens the connection, history and delete dialogs from the menu', () => {
    const connection = fake('accounts-connection');
    expect(connection.dialogs).toContain('connection:2');
    expect(connection.banking.state().banks[1]?.pinText).toBe(translate('en', 'PinSavedForSyncs'));

    const history = fake('accounts-history');
    expect(history.dialogs).toContain('history:2');

    const menu = fake();
    menu.banking.handleBankMenu(2, 'history');
    expect(menu.dialogs.at(-1)).toBe('history:2');
    menu.banking.handleBankMenu(2, 'delete');
    expect(menu.dialogs.at(-1)).toBe('delete:2');
  });

  it('searches the invented bank directory with the detail format', () => {
    const { banking } = fake();
    const rows = banking.searchBanks('hypo');
    expect(rows.map((row) => row.name)).toEqual(['Rheintal Hypobank', 'Alpen Hypothekenbank']);
    expect(rows[0]).toEqual({
      name: 'Rheintal Hypobank',
      detail: '70020270 · München · HYVEDEMM488',
      blz: '70020270',
      bic: 'HYVEDEMM488',
    });
    expect(banking.searchBanks('   ')).toEqual([]);
    expect(banking.searchBanks('zzzzzz')).toEqual([]);
  });

  it('picks a suggestion and maps a branch BLZ to its central code', () => {
    const { banking } = fake();
    const picked = banking.pickBankSuggestion(' Rheintal Hypobank ', '70020270', ' HYVEDEMM488 ');
    expect(picked).toEqual({
      name: 'Rheintal Hypobank',
      blz: '70020270',
      bic: 'HYVEDEMM488',
      centralNote: translate('en', 'CentralBlzNote', '70020270', '10020890'),
    });

    const branch = banking.pickBankSuggestion('Rheintal Hypobank', '10020890', 'HYVEDEMM488');
    expect(branch.blz).toBe('70020270');
    expect(branch.centralNote).toBe(translate('en', 'CentralBlzNote', '70020270', '10020890'));

    const other = banking.pickBankSuggestion('Nordlicht Bank', '20050550', 'NOLADEHHXXX');
    expect(other.blz).toBe('20050550');
    expect(other.centralNote).toBe('');
  });

  it('fills only empty fields from a typed 8-digit BLZ and maps its branch code', () => {
    const { banking } = fake();
    const empty = banking.bankBlzChanged('', '10020890', '');
    expect(empty.name).toBe('Rheintal Hypobank');
    expect(empty.blz).toBe('70020270');
    expect(empty.bic).toBe('HYVEDEMM488');
    expect(empty.centralNote).toBe(translate('en', 'CentralBlzNote', '70020270', '10020890'));

    const filled = banking.bankBlzChanged('My bank', '10020890', 'MYBIC');
    expect(filled.name).toBe('My bank');
    expect(filled.blz).toBe('70020270');
    expect(filled.bic).toBe('MYBIC');

    const partial = banking.bankBlzChanged('My bank', '1002', 'MYBIC');
    expect(partial).toEqual({
      name: 'My bank',
      blz: '1002',
      bic: 'MYBIC',
      centralNote: '',
    });

    const unknown = banking.bankBlzChanged('', '99999999', '');
    expect(unknown).toEqual({ name: '', blz: '99999999', bic: '', centralNote: '' });
  });

  it('localizes the central note from the current language', () => {
    const { banking } = fake('accounts', 'de');
    const picked = banking.pickBankSuggestion('Rheintal Hypobank', '10020890', 'HYVEDEMM488');
    expect(picked.centralNote).toBe(translate('de', 'CentralBlzNote', '70020270', '10020890'));
  });

  it('shows the sync state of its reference scenarios', () => {
    const syncing = fake('accounts-syncing').banking.state();
    expect(syncing.syncing).toBe(true);
    expect(syncing.busy).toBe(true);
    expect(syncing.syncStep).toBe(translate('en', 'SyncConnecting'));
    expect(syncing.banks.find((bank) => bank.bankId === 2)?.syncStep).toBe(
      translate('en', 'SyncConnecting'),
    );

    const all = fake('accounts-sync-all').banking.state();
    expect(all.busy).toBe(true);
    expect(all.syncAllText).toBe('');
  });

  it('runs a connection test through the product id and PIN prompts', () => {
    const { banking } = fake('banking-walkthrough');
    const running = banking.testConnection(2);
    expect(running.connectionTest).toMatchObject({
      bankId: 2,
      running: true,
      go: false,
      probed: 0,
      skipped: 0,
      hasTrace: false,
    });

    const productID = banking.prompt();
    expect(productID.kind).toBe('product-id');
    expect(productID.title).toBe(translate('en', 'ProductIDPromptTitle'));
    expect(productID.text).toBe(translate('en', 'ProductIDPromptText'));
    expect(productID.label).toBe(translate('en', 'ProductIDPromptLabel'));

    banking.answerProductID(productID.id, 'invented-product-id');
    const pin = banking.prompt();
    expect(pin.kind).toBe('pin');
    expect(pin.title).toBe(translate('en', 'PinFor', 'Sparkasse'));
    expect(pin.rememberVisible).toBe(false);

    banking.answerPin(pin.id, 'S3cret-PIN-9931', false);
    expect(banking.prompt().kind).toBe('');
    const done = banking.state();
    expect(done.connectionTest).toMatchObject({
      bankId: 2,
      running: false,
      go: true,
      reason: translate('en', 'TestConnectionGo'),
      codes: [],
      probed: 1,
      skipped: 1,
      hasTrace: true,
    });
    const published = JSON.stringify(done) + JSON.stringify(banking.prompt());
    expect(published).not.toContain('invented-product-id');
    expect(published).not.toContain('S3cret-PIN-9931');
  });

  it('cancels a connection test at the product id prompt with the cancelled reason', () => {
    const { banking } = fake('banking-walkthrough');
    banking.testConnection(2);
    const productID = banking.prompt();
    expect(productID.kind).toBe('product-id');

    banking.answerProductID(productID.id, '');
    expect(banking.prompt().kind).toBe('');
    const done = banking.state();
    expect(done.connectionTest).toMatchObject({
      bankId: 2,
      running: false,
      go: false,
      reason: translate('en', 'TestConnectionCancelled'),
      codes: [],
      probed: 0,
      skipped: 0,
      hasTrace: false,
    });
  });

  it('reports the NO-GO login with the bank return code', () => {
    const { banking } = fake('accounts-test-connection-nogo-en-light');
    banking.testConnection(2);
    banking.answerProductID(banking.prompt().id, 'invented-product-id');
    banking.answerPin(banking.prompt().id, '1234', false);

    const done = banking.state();
    expect(done.connectionTest).toMatchObject({
      bankId: 2,
      running: false,
      go: false,
      reason: translate('en', 'TestConnectionInvalidLogin'),
      codes: ['9942'],
      probed: 0,
      skipped: 0,
      hasTrace: true,
    });
  });

  it('skips the product id prompt when the build has a linked id', () => {
    const { banking } = fake('accounts-test-connection-linked');
    banking.testConnection(2);
    const prompt = banking.prompt();
    expect(prompt.kind).toBe('pin');
    expect(prompt.rememberVisible).toBe(false);
  });

  it('saves the redacted trace only after a test produced one', () => {
    const { banking } = fake('banking-walkthrough');
    const absent = banking.saveConnectionTrace();
    expect(absent.errorText).toBe(translate('en', 'ErrorNoConnectionTrace'));

    banking.testConnection(2);
    banking.answerProductID(banking.prompt().id, 'invented-product-id');
    banking.answerPin(banking.prompt().id, '1234', false);
    const saved = banking.saveConnectionTrace();
    expect(saved.errorText).toBe('');
    expect(saved.messageText).toBe(translate('en', 'MessageConnectionTraceSaved'));
  });
});
