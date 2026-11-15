import type {
  AddBankFieldsDTO,
  BankingBankDTO,
  BankingDTO,
  BankMenuItemDTO,
  BankStatusDTO,
  BankSuggestionDTO,
  ConnectionTestDTO,
  LastTraceDTO,
  PromptDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
import { mockBankDirectory, searchBanks } from './bankDirectory';
import { translate, type Language } from './i18n';
import { mockTanChallengeImage } from './mock/tanChallenge';

/** One bank as the accounts fake knows it, without the online details. */
export interface MockKnownBank {
  id: number;
  name: string;
  offline: boolean;
  blz: string;
  bic: string;
}

/** The fields the connection dialog shows. */
export interface MockConnectionDialog {
  title: string;
  bankId: number;
  offline: boolean;
  name: string;
  blz: string;
  userId: string;
  serverUrl: string;
  hasPin: boolean;
  pinText: string;
  pinForgetAfter: string;
  syncing: boolean;
}

/** The fields the history-from dialog shows. */
export interface MockHistoryDialog {
  title: string;
  bankId: number;
  date: string;
  hasStart: boolean;
}

/**
 * The accounts-side effects of the banking fake. The real bind.Banking service
 * writes into the session, which the Accounts page reads back; the mock routes
 * the same effects into the accounts fake.
 */
export interface MockBankingHooks {
  openAddBank(): void;
  setDialogError(text: string): void;
  closeDialog(): void;
  addOnlineBank(bankId: number, name: string, blz: string, bic: string): void;
  removeBank(bankId: number): void;
  openConnectionDialog(dialog: MockConnectionDialog): void;
  openHistoryDialog(dialog: MockHistoryDialog): void;
  openDeleteBank(bankId: number): void;
  listBanks(): MockKnownBank[];
  /** Re-renders the accounts cards after the banking state changed. */
  republishAccounts(): void;
}

interface MockBankingBank {
  id: number;
  name: string;
  blz: string;
  bic: string;
  userId: string;
  serverUrl: string;
  offline: boolean;
  includeInSyncAll: boolean;
  syncUnlocked: boolean;
  syncUnlockedOn: string | null;
  syncFrom: string | null;
  hasSavedPin: boolean;
  /** Mock only: the user ticked "Remember PIN" in this run. No PIN is kept. */
  rememberedThisRun: boolean;
  /** The bank's stored-PIN removal option: "1" or "2". */
  pinForgetAfter: string;
}

/** A bank as the accounts fake's cards need it, including the status line. */
export interface MockBankingCard extends BankingBankDTO {
  status: BankStatusDTO;
}

interface MockSyncAll {
  position: number;
  total: number;
  bankName: string;
}

type ScriptStep = 'pin' | 'method' | 'medium' | 'tan' | 'decoupled';

type TestStage = '' | 'product-id' | 'pin';

interface PinOptions {
  rememberVisible?: boolean;
}

interface TanOptions {
  imageUrl?: string;
}

/** The prompt scenarios, one per reference, plus the medium and text-only TAN. */
const promptScenarios: Record<string, ScriptStep> = {
  'accounts-pin': 'pin',
  'accounts-pin-remember': 'pin',
  'accounts-tan-method': 'method',
  'accounts-tan-medium': 'medium',
  'accounts-tan-photo': 'tan',
  'accounts-tan-text': 'tan',
  'accounts-decoupled': 'decoupled',
};

const walkthroughScript: ScriptStep[] = ['pin', 'method', 'medium', 'tan', 'decoupled'];

function emptyPrompt(): PromptDTO {
  return {
    id: 0,
    kind: '',
    title: '',
    text: '',
    label: '',
    bankName: '',
    userId: '',
    rememberVisible: false,
    choices: [],
    challenge: '',
    flickerNote: '',
    imageUrl: '',
  };
}

/** The fixed civil date every mock screenshot and connection test uses. */
const mockToday = '2026-11-14';

function formatDate(iso: string): string {
  const parts = iso.split('-');
  const [year, month, day] = parts;
  if (year === undefined || month === undefined || day === undefined) {
    return iso;
  }
  return `${day}.${month}.${year}`;
}

function todayIso(): string {
  return mockToday;
}

function isoBack(iso: string, days: number): string {
  const parts = iso.split('-').map((part) => Number.parseInt(part, 10));
  const [year, month, day] = parts;
  if (year === undefined || month === undefined || day === undefined) {
    return iso;
  }
  const date = new Date(Date.UTC(year, month - 1, day));
  date.setUTCDate(date.getUTCDate() - days);
  return date.toISOString().slice(0, 10);
}

function parseDate(text: string): string | null {
  const match = /^(\d{2})\.(\d{2})\.(\d{4})$/u.exec(text.trim());
  if (!match) {
    return null;
  }
  const [, dayText, monthText, yearText] = match;
  const day = Number.parseInt(dayText ?? '', 10);
  const month = Number.parseInt(monthText ?? '', 10);
  const year = Number.parseInt(yearText ?? '', 10);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return null;
  }
  return `${yearText}-${monthText}-${dayText}`;
}

function isEightDigits(text: string): boolean {
  return /^\d{8}$/u.test(text.trim());
}

function isHttps(url: string): boolean {
  const clean = url.trim();
  return /^https:\/\/.+/iu.test(clean);
}

/**
 * The stateful banking fake behind the mock build's Accounts page. It mirrors
 * bind.Banking's state and prompt model, and its fake bank runs the scripted
 * PIN → TAN method → TAN medium → photoTAN → decoupled walkthrough.
 *
 * PINs and TANs are only checked for emptiness; they are never stored, echoed
 * or written anywhere. A stale answer (an id from a replaced prompt) changes
 * nothing.
 */
export function createMockBanking(
  scenario: string,
  initialLanguage: Language,
  hooks: MockBankingHooks,
) {
  let language = initialLanguage;
  const onlineSyncAvailable =
    scenario === 'banking-walkthrough' ||
    scenario === 'accounts-sync-per-bank' ||
    scenario === 'accounts-bank-menu-en-light' ||
    scenario === 'accounts-bank-menu-de-dark' ||
    scenario === 'accounts-status-strong-sync' ||
    scenario.startsWith('accounts-status-no-server');
  const canRememberPin = true;
  let errorText = '';
  let messageText = '';
  let infoText = '';
  let syncingBankId: number | null = null;
  let syncStep = '';
  let syncAll: MockSyncAll | null = null;
  let prompt = emptyPrompt();
  let nextPromptId = 1;
  let nextBankId = 3;
  let script: ScriptStep[] = [];
  let scriptIndex = 0;
  let testingBankId: number | null = null;
  let testStage: TestStage = '';
  let connectionTest: ConnectionTestDTO | null = null;
  /** The last sync's or connection test's trace slot, as the Go shell keeps it. */
  let lastTrace: LastTraceDTO | null = null;
  /**
   * Mock only: a build with a linked product id asks for no product id. The
   * default asks, so the prompt is part of the walkthrough.
   */
  const linkedProductID = scenario === 'accounts-test-connection-linked';
  /** Mock only: this bank's login is refused with FinTS code 9942. */
  const noGoBankId = scenario.includes('nogo') ? 2 : null;
  /** Mock only: the test is a GO through the strong sync, so the bank stays locked. */
  const strongSyncGoTest = scenario === 'accounts-status-strong-sync';
  /** Mock only: the test uses a PIN the user saved earlier. */
  const savedPinTest = scenario === 'accounts-test-saved-pin';
  /** Mock only: that saved PIN is rejected and removed. */
  const savedPinRejectedTest = scenario === 'accounts-test-saved-pin-rejected-de';

  const banks: MockBankingBank[] = [
    {
      id: 1,
      name: 'Bank Norwegian',
      blz: '',
      bic: '',
      userId: '',
      serverUrl: '',
      offline: true,
      includeInSyncAll: false,
      syncUnlocked: false,
      syncUnlockedOn: null,
      syncFrom: null,
      hasSavedPin: false,
      rememberedThisRun: false,
      pinForgetAfter: '1',
    },
    {
      id: 2,
      name: 'Sparkasse',
      blz: '37040044',
      bic: 'COLSDE33XXX',
      userId: 'meinuser',
      serverUrl: 'https://fints.example/pintan',
      offline: false,
      includeInSyncAll: true,
      syncUnlocked: true,
      syncUnlockedOn: '2026-11-14',
      syncFrom: null,
      hasSavedPin: false,
      rememberedThisRun: false,
      pinForgetAfter: '1',
    },
  ];

  const listeners = new Set<(dto: BankingDTO) => void>();
  const promptListeners = new Set<(dto: PromptDTO) => void>();

  function t(key: string, ...args: (string | number)[]): string {
    return translate(language, key, ...args);
  }

  function centralNote(mapped: string, original: string): string {
    return t('CentralBlzNote', mapped, original);
  }

  /**
   * The mock directory's branch mapping. Its invented branch entry keeps the
   * central BLZ it is shown with and names the branch code it replaces, so both
   * a picked row and a typed branch code map to the central one.
   */
  function mockFintsBlz(blz: string): string {
    const trimmed = blz.trim();
    const branch = mockBankDirectory.find((entry) => entry.branchBlz === trimmed);
    return branch === undefined ? trimmed : branch.blz;
  }

  function searchBankSuggestions(query: string): BankSuggestionDTO[] {
    return searchBanks(mockBankDirectory, query, 15).map((entry) => ({
      name: entry.name,
      detail: entry.detail,
      blz: entry.blz,
      bic: entry.bic,
    }));
  }

  function pickBankSuggestion(name: string, blz: string, bic: string): AddBankFieldsDTO {
    const original = blz.trim();
    const mapped = mockFintsBlz(original);
    let note = '';
    if (mapped !== original) {
      note = centralNote(mapped, original);
    } else {
      const exact = mockBankDirectory.find((entry) => entry.blz === original);
      if (exact !== undefined && exact.branchBlz !== undefined) {
        note = centralNote(mapped, exact.branchBlz);
      }
    }
    return { name: name.trim(), blz: mapped, bic: bic.trim(), centralNote: note };
  }

  function bankBlzChanged(name: string, blz: string, bic: string): AddBankFieldsDTO {
    const trimmed = blz.trim();
    if (!isEightDigits(trimmed)) {
      return { name, blz, bic, centralNote: '' };
    }
    const branch = mockBankDirectory.find((entry) => entry.branchBlz === trimmed);
    if (branch !== undefined && branch.branchBlz !== undefined) {
      return {
        name: name.trim() === '' ? branch.name : name,
        blz: branch.blz,
        bic: bic.trim() === '' ? branch.bic : bic,
        centralNote: centralNote(branch.blz, branch.branchBlz),
      };
    }
    const exact = mockBankDirectory.find((entry) => entry.blz === trimmed);
    if (exact === undefined) {
      return { name, blz, bic, centralNote: '' };
    }
    return {
      name: name.trim() === '' ? exact.name : name,
      blz: exact.blz,
      bic: bic.trim() === '' ? exact.bic : bic,
      centralNote: exact.branchBlz === undefined ? '' : centralNote(exact.blz, exact.branchBlz),
    };
  }

  function bankById(id: number): MockBankingBank | undefined {
    return banks.find((bank) => bank.id === id);
  }

  function isSyncing(): boolean {
    return syncingBankId !== null || syncAll !== null;
  }

  /** Adds and drops the banks the accounts fake owns, as merge_banks does. */
  function alignBanks(): void {
    const known = hooks.listBanks();
    const ids = new Set(known.map((bank) => bank.id));
    for (let index = banks.length - 1; index >= 0; index -= 1) {
      const bank = banks[index];
      if (bank !== undefined && !ids.has(bank.id)) {
        banks.splice(index, 1);
      }
    }
    for (const item of known) {
      if (item.id < 0 || bankById(item.id) !== undefined) {
        continue;
      }
      banks.push({
        id: item.id,
        name: item.name,
        blz: item.blz,
        bic: item.bic,
        userId: '',
        serverUrl: '',
        offline: item.offline,
        includeInSyncAll: false,
        syncUnlocked: false,
        syncUnlockedOn: null,
        syncFrom: null,
        hasSavedPin: false,
        rememberedThisRun: false,
        pinForgetAfter: '1',
      });
    }
  }

  function bankMenu(bank: MockBankingBank): BankMenuItemDTO[] {
    const enabled = !isSyncing();
    const testEnabled = enabled && testingBankId === null;
    const syncEnabled = onlineSyncAvailable && bank.syncUnlocked && enabled;
    const items: BankMenuItemDTO[] = [];
    if (!bank.offline) {
      if (bank.serverUrl.trim() !== '') {
        items.push({
          action: 'test-connection',
          label: t('TestConnection'),
          checked: false,
          danger: false,
          enabled: testEnabled,
          icon: 'network_check',
        });
      }
      items.push(
        {
          action: 'sync',
          label: t('SyncNow'),
          checked: false,
          danger: false,
          enabled: syncEnabled,
          icon: 'sync',
        },
        {
          action: 'history',
          label: t('FetchHistory'),
          checked: false,
          danger: false,
          enabled: syncEnabled,
          icon: 'date_range',
        },
        {
          action: 'include',
          label: t('IncludeInSyncAll'),
          checked: bank.includeInSyncAll,
          danger: false,
          enabled,
          icon: 'check',
        },
        {
          action: 'rename',
          label: t('RenameConnection'),
          checked: false,
          danger: false,
          enabled,
          icon: 'edit',
        },
        {
          action: 'connection',
          label: t('ConnectionSettings'),
          checked: false,
          danger: false,
          enabled,
          icon: 'settings',
        },
      );
      if (bank.syncFrom !== null) {
        items.push({
          action: 'clear-history',
          label: t('ClearHistoryStart'),
          checked: false,
          danger: false,
          enabled,
          icon: 'history',
        });
      }
      if (bank.hasSavedPin) {
        items.push({
          action: 'forget-pin',
          label: t('ForgetPin'),
          checked: false,
          danger: false,
          enabled,
          icon: 'lock',
        });
      }
    } else {
      items.push({
        action: 'connection',
        label: t('RenameConnection'),
        checked: false,
        danger: false,
        enabled,
        icon: 'edit',
      });
    }
    items.push({
      action: 'delete',
      label: t('RemoveBank'),
      checked: false,
      danger: true,
      enabled,
      icon: 'delete',
    });
    return items;
  }

  function pinText(bank: MockBankingBank): string {
    if (bank.offline) {
      return '';
    }
    if (bank.hasSavedPin) {
      return t('PinSavedForSyncs');
    }
    return canRememberPin ? t('PinAskedEverySync') : t('PinNeverStored');
  }

  function bankStatus(bank: MockBankingBank): BankStatusDTO {
    const status: BankStatusDTO = {
      tone: '',
      icon: '',
      text: '',
      action: '',
      actionText: '',
      actionEnabled: false,
    };
    if (bank.offline || !onlineSyncAvailable) {
      return status;
    }
    if (syncingBankId === bank.id || testingBankId === bank.id) {
      return status;
    }
    const canTest = bank.serverUrl.trim() !== '';
    if (!bank.syncUnlocked) {
      status.tone = 'attention';
      status.icon = 'warning';
      if (
        connectionTest !== null &&
        !connectionTest.running &&
        connectionTest.bankId === bank.id &&
        connectionTest.go &&
        connectionTest.strongSync
      ) {
        status.text = t('SyncStatusStrongSyncLocked');
        return status;
      }
      if (!canTest) {
        status.text = t('SyncStatusAddServerURL');
        status.action = 'connection';
        status.actionText = t('ConnectionSettings');
        status.actionEnabled = !isSyncing();
        return status;
      }
      status.text = t('SyncStatusRunTest');
      status.action = 'test-connection';
      status.actionText = t('TestConnection');
      status.actionEnabled = !isSyncing() && testingBankId === null;
      return status;
    }
    status.tone = bank.includeInSyncAll ? 'ready' : 'off';
    status.icon = bank.includeInSyncAll ? 'check_circle' : 'sync';
    const base = bank.includeInSyncAll ? 'SyncStatusReady' : 'SyncStatusNotInAll';
    const tested = bank.includeInSyncAll ? 'SyncStatusReadyTested' : 'SyncStatusNotInAllTested';
    status.text =
      bank.syncUnlockedOn === null ? t(base) : t(tested, formatDate(bank.syncUnlockedOn));
    status.action = 'sync';
    status.actionText = t('SyncNow');
    status.actionEnabled =
      onlineSyncAvailable && bank.syncUnlocked && !isSyncing() && testingBankId === null;
    return status;
  }

  function bankDTO(bank: MockBankingBank): MockBankingCard {
    const syncing = syncingBankId === bank.id;
    return {
      bankId: bank.id,
      syncing,
      syncStep: syncStep === '' ? t('Syncing') : syncStep,
      pinText: pinText(bank),
      canTestConnection: !bank.offline && bank.serverUrl.trim() !== '',
      menu: bankMenu(bank),
      status: bankStatus(bank),
    };
  }

  function banksForCard(): MockBankingCard[] {
    return banks.map(bankDTO);
  }

  function syncAllText(): string {
    if (syncAll === null || syncAll.position === 0) {
      return '';
    }
    return t('SyncAllProgress', syncAll.position, syncAll.total, syncAll.bankName);
  }

  function hasOnlineBank(): boolean {
    return banks.some((bank) => !bank.offline);
  }

  function syncAllReady(): boolean {
    if (isSyncing()) {
      return false;
    }
    return banks.some((bank) => bank.includeInSyncAll && !bank.offline);
  }

  function connectionTestState(): ConnectionTestDTO | null {
    if (connectionTest === null) {
      return null;
    }
    return {
      ...connectionTest,
      codes: [...connectionTest.codes],
      messages: connectionTest.messages.map((message) => ({ ...message })),
    };
  }

  function state(): BankingDTO {
    alignBanks();
    return {
      errorText,
      messageText,
      busy: isSyncing(),
      syncing: syncingBankId !== null,
      syncBankId: syncingBankId ?? 0,
      syncStep,
      syncAllText: syncAllText(),
      syncAllVisible: hasOnlineBank(),
      syncAllEnabled: onlineSyncAvailable && syncAllReady(),
      syncControlsVisible: onlineSyncAvailable && hasOnlineBank(),
      onlineSyncAvailable,
      canRememberPin,
      onlineSyncNote: !onlineSyncAvailable && hasOnlineBank() ? t('OnlineSyncNotYetAvailable') : '',
      banks: banksForCard(),
      connectionTest: connectionTestState(),
      lastTrace: lastTrace === null ? null : { ...lastTrace },
      infoText,
    };
  }

  function promptDTO(): PromptDTO {
    return { ...prompt, choices: prompt.choices.map((choice) => ({ ...choice })) };
  }

  function publish(): BankingDTO {
    const next = state();
    for (const listener of listeners) {
      listener(next);
    }
    hooks.republishAccounts();
    return next;
  }

  function publishPrompt(): PromptDTO {
    const next = promptDTO();
    for (const listener of promptListeners) {
      listener(next);
    }
    return next;
  }

  function setPrompt(next: PromptDTO): void {
    prompt = next;
  }

  function promptFor(step: ScriptStep, options: PinOptions & TanOptions = {}): PromptDTO {
    const bank = bankById(syncingBankId ?? testingBankId ?? 2) ?? banks[1];
    const bankName = bank?.name ?? '';
    const userId = bank?.userId ?? '';
    const id = nextPromptId;
    nextPromptId += 1;
    switch (step) {
      case 'pin':
        return {
          ...emptyPrompt(),
          id,
          kind: 'pin',
          title: t('PinFor', bankName),
          text: t('PinExplanation', userId),
          label: t('Pin'),
          bankName,
          userId,
          rememberVisible: options.rememberVisible ?? canRememberPin,
        };
      case 'method':
        return {
          ...emptyPrompt(),
          id,
          kind: 'choice',
          title: t('ChooseTanProcedure'),
          choices: [
            { id: 'chiptan', label: 'chipTAN optisch' },
            { id: 'pushtan', label: 'pushTAN' },
          ],
        };
      case 'medium':
        return {
          ...emptyPrompt(),
          id,
          kind: 'choice',
          title: t('ChooseTanMedium'),
          choices: [
            { id: 'phone', label: 'Mein Smartphone' },
            { id: 'card', label: 'Karte …4321' },
          ],
        };
      case 'tan':
        return {
          ...emptyPrompt(),
          id,
          kind: 'tan',
          title: t('TanRequired'),
          label: t('TanLabel'),
          challenge: 'Bitte bestätigen Sie den Abruf der Umsätze.',
          imageUrl: options.imageUrl ?? '',
        };
      case 'decoupled':
        return {
          ...emptyPrompt(),
          id,
          kind: 'decoupled',
          title: t('ConfirmInBankingApp'),
          text: 'Confirm the login in your banking app.',
        };
      default:
        return emptyPrompt();
    }
  }

  function finishSync(): void {
    const bank = syncingBankId === null ? undefined : bankById(syncingBankId);
    const wasSyncAll = syncAll !== null;
    prompt = emptyPrompt();
    syncingBankId = null;
    syncStep = '';
    syncAll = null;
    if (bank !== undefined) {
      if (bank.rememberedThisRun) {
        bank.hasSavedPin = true;
      }
      bank.rememberedThisRun = false;
      lastTrace = { kind: 'sync', bankId: bank.id, hasTrace: true };
    }
    const included = banks.filter((bank) => bank.includeInSyncAll && !bank.offline);
    const skipped = included.filter((bank) => !bank.syncUnlocked).map((bank) => bank.name);
    const skippedText = wasSyncAll ? syncAllSkipped(skipped) : '';
    messageText =
      skippedText === '' ? t('SyncSummary', 2) : `${t('SyncSummary', 2)}\n${skippedText}`;
    errorText = '';
    publish();
    publishPrompt();
  }

  function cancelSync(): void {
    prompt = emptyPrompt();
    syncingBankId = null;
    syncStep = '';
    syncAll = null;
    publish();
    publishPrompt();
  }

  function runStep(): void {
    const step = script[scriptIndex];
    if (step === undefined) {
      finishSync();
      return;
    }
    setPrompt(promptFor(step, step === 'tan' ? { imageUrl: mockTanChallengeImage } : {}));
    publish();
    publishPrompt();
  }

  function advance(): void {
    scriptIndex += 1;
    runStep();
  }

  function startSync(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      errorText = t('ErrorBankConnectionNotFound');
      return publish();
    }
    if (bank.offline) {
      errorText = t('ErrorBankOffline');
      return publish();
    }
    if (!bank.syncUnlocked) {
      errorText = t('SyncLockedHint');
      return publish();
    }
    syncingBankId = bankId;
    lastTrace = { kind: 'sync', bankId, hasTrace: false };
    syncStep = t('SyncConnecting');
    errorText = '';
    messageText = '';
    script = [...walkthroughScript];
    scriptIndex = 0;
    runStep();
    return state();
  }

  function syncAllSkipped(names: string[]): string {
    if (names.length === 0) {
      return '';
    }
    return t('SyncAllSkippedLocked', names.length, names.join(', '));
  }

  function startSyncAll(): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const included = banks.filter((bank) => bank.includeInSyncAll && !bank.offline);
    if (included.length === 0) {
      syncAll = null;
      errorText = t('SyncAllNothingIncluded');
      return publish();
    }
    const ready = included.filter((bank) => bank.syncUnlocked);
    const skipped = included.filter((bank) => !bank.syncUnlocked).map((bank) => bank.name);
    if (ready.length === 0) {
      syncAll = null;
      errorText = t('SyncAllNothingReady');
      messageText = syncAllSkipped(skipped);
      return publish();
    }
    const first = ready[0];
    if (first === undefined) {
      return state();
    }
    syncAll = { position: 1, total: ready.length, bankName: first.name };
    syncingBankId = first.id;
    lastTrace = { kind: 'sync', bankId: 0, hasTrace: false };
    syncStep = t('SyncConnecting');
    errorText = '';
    messageText = '';
    script = [...walkthroughScript];
    scriptIndex = 0;
    runStep();
    return state();
  }

  function currentMatches(id: number, kind: string): boolean {
    return prompt.kind === kind && prompt.id === id;
  }

  function answerPin(id: number, pin: string, remember: boolean): void {
    if (!currentMatches(id, 'pin') || pin === '') {
      return;
    }
    if (testingBankId !== null) {
      finishTest(false);
      return;
    }
    const bank = syncingBankId === null ? undefined : bankById(syncingBankId);
    if (remember && bank !== undefined) {
      bank.rememberedThisRun = true;
    }
    advance();
  }

  function answerTan(id: number, tan: string): void {
    if (!currentMatches(id, 'tan') || tan === '') {
      return;
    }
    advance();
  }

  function answerChoice(id: number, choice: string): void {
    if (!currentMatches(id, 'choice')) {
      return;
    }
    if (!prompt.choices.some((entry) => entry.id === choice)) {
      return;
    }
    advance();
  }

  function answerDecoupled(id: number, confirmed: boolean): void {
    if (!currentMatches(id, 'decoupled')) {
      return;
    }
    if (confirmed) {
      advance();
    } else {
      cancelSync();
    }
  }

  function cancelPin(id: number): void {
    if (currentMatches(id, 'pin')) {
      if (testingBankId !== null) {
        finishTest(true);
        return;
      }
      cancelSync();
    }
  }

  function cancelTan(id: number): void {
    if (currentMatches(id, 'tan')) {
      cancelSync();
    }
  }

  function cancelChoice(id: number): void {
    if (currentMatches(id, 'choice')) {
      cancelSync();
    }
  }

  function productIDPrompt(): PromptDTO {
    const id = nextPromptId;
    nextPromptId += 1;
    return {
      ...emptyPrompt(),
      id,
      kind: 'product-id',
      title: t('ProductIDPromptTitle'),
      text: t('ProductIDPromptText'),
      label: t('ProductIDPromptLabel'),
    };
  }

  function startTestPrompt(): void {
    if (linkedProductID) {
      testStage = 'pin';
      setPrompt(promptFor('pin', { rememberVisible: false }));
    } else {
      testStage = 'product-id';
      setPrompt(productIDPrompt());
    }
    publish();
    publishPrompt();
  }

  function testConnection(bankId: number): BankingDTO {
    if (isSyncing() || testingBankId !== null) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      errorText = t('ErrorBankConnectionNotFound');
      return publish();
    }
    if (bank.offline) {
      errorText = t('ErrorBankOffline');
      return publish();
    }
    if (bank.serverUrl.trim() === '') {
      errorText = t('ErrorNoFintsServer', bank.blz);
      return publish();
    }
    if (savedPinTest || savedPinRejectedTest) {
      bank.hasSavedPin = !savedPinRejectedTest;
      connectionTest = savedPinRejectedTest
        ? {
            bankId,
            running: false,
            go: false,
            verdict: 'nogo',
            verdictText: 'NO-GO',
            reason: t('TestConnectionLoginRejected'),
            nextStep: t('TestConnectionNextStepCheckLogin'),
            codes: ['9931'],
            messages: [],
            probed: 0,
            skipped: 0,
            strongSync: false,
            strongSyncTAN: '',
            usedSavedPin: true,
            savedPinForgotten: true,
            savedPinRejectedOnce: false,
          }
        : {
            bankId,
            running: false,
            go: true,
            verdict: 'go',
            verdictText: 'GO',
            reason: t('TestConnectionGo'),
            nextStep: onlineSyncAvailable ? t('TestConnectionSyncNow') : '',
            codes: [],
            messages: [],
            probed: 1,
            skipped: 1,
            strongSync: false,
            strongSyncTAN: '',
            usedSavedPin: true,
            savedPinForgotten: false,
            savedPinRejectedOnce: false,
          };
      lastTrace = { kind: 'test', bankId, hasTrace: true };
      errorText = '';
      messageText = '';
      publish();
      publishPrompt();
      return state();
    }
    testingBankId = bankId;
    testStage = '';
    connectionTest = {
      bankId,
      running: true,
      go: false,
      verdict: '',
      verdictText: '',
      reason: '',
      nextStep: '',
      codes: [],
      messages: [],
      probed: 0,
      skipped: 0,
      strongSync: false,
      strongSyncTAN: '',
      usedSavedPin: false,
      savedPinForgotten: false,
      savedPinRejectedOnce: false,
    };
    lastTrace = { kind: 'test', bankId, hasTrace: false };
    errorText = '';
    messageText = '';
    startTestPrompt();
    return state();
  }

  function finishTest(cancelled: boolean): void {
    const bankId = testingBankId;
    if (bankId === null) {
      return;
    }
    if (cancelled) {
      connectionTest = {
        bankId,
        running: false,
        go: false,
        verdict: 'none',
        verdictText: t('TestConnectionNotCompleted'),
        reason: t('TestConnectionCancelled'),
        nextStep: '',
        codes: [],
        messages: [],
        probed: 0,
        skipped: 0,
        strongSync: false,
        strongSyncTAN: '',
        usedSavedPin: false,
        savedPinForgotten: false,
        savedPinRejectedOnce: false,
      };
      lastTrace = { kind: 'test', bankId, hasTrace: false };
    } else if (strongSyncGoTest) {
      connectionTest = {
        bankId,
        running: false,
        go: true,
        verdict: 'go',
        verdictText: 'GO',
        reason: t('TestConnectionGo'),
        nextStep: t('TestConnectionStrongSyncNoUnlock'),
        codes: [],
        messages: [],
        probed: 1,
        skipped: 0,
        strongSync: true,
        strongSyncTAN: 'typed',
        usedSavedPin: false,
        savedPinForgotten: false,
        savedPinRejectedOnce: false,
      };
      lastTrace = { kind: 'test', bankId, hasTrace: true };
    } else if (noGoBankId === bankId) {
      connectionTest = {
        bankId,
        running: false,
        go: false,
        verdict: 'nogo',
        verdictText: 'NO-GO',
        reason: t('TestConnectionInvalidLogin'),
        nextStep: t('TestConnectionNextStepCheckLogin'),
        codes: ['9942'],
        messages: [
          { code: '9050', segment: '', text: 'Die Nachricht enthält Fehler.' },
          { code: '9210', segment: 'HKTAN', text: 'Invented test message for the mock bank.' },
        ],
        probed: 0,
        skipped: 0,
        strongSync: true,
        strongSyncTAN: 'typed',
        usedSavedPin: false,
        savedPinForgotten: false,
        savedPinRejectedOnce: false,
      };
      lastTrace = { kind: 'test', bankId, hasTrace: true };
    } else {
      connectionTest = {
        bankId,
        running: false,
        go: true,
        verdict: 'go',
        verdictText: 'GO',
        reason: t('TestConnectionGo'),
        nextStep: onlineSyncAvailable ? t('TestConnectionSyncNow') : '',
        codes: [],
        messages: [],
        probed: 1,
        skipped: 1,
        strongSync: false,
        strongSyncTAN: '',
        usedSavedPin: false,
        savedPinForgotten: false,
        savedPinRejectedOnce: false,
      };
      lastTrace = { kind: 'test', bankId, hasTrace: true };
    }
    if (!cancelled) {
      const testedBank = bankById(bankId);
      if (testedBank !== undefined) {
        const strongSyncGo = connectionTest.go && connectionTest.strongSync;
        testedBank.syncUnlocked = noGoBankId !== bankId && !strongSyncGo;
        testedBank.syncUnlockedOn = testedBank.syncUnlocked ? todayIso() : null;
      }
    }
    testingBankId = null;
    testStage = '';
    prompt = emptyPrompt();
    errorText = '';
    messageText = '';
    publish();
    publishPrompt();
  }

  function answerProductID(id: number, value: string): void {
    if (!currentMatches(id, 'product-id') || testStage !== 'product-id') {
      return;
    }
    if (value === '') {
      finishTest(true);
      return;
    }
    testStage = 'pin';
    setPrompt(promptFor('pin', { rememberVisible: false }));
    publish();
    publishPrompt();
  }

  function saveConnectionTrace(): BankingDTO {
    if (lastTrace === null || !lastTrace.hasTrace) {
      errorText = t('ErrorNoConnectionTrace');
      return publish();
    }
    errorText = '';
    messageText = t('MessageConnectionTraceSaved');
    return publish();
  }

  function addOnlineBank(
    name: string,
    blz: string,
    bic: string,
    userId: string,
    serverUrl: string,
  ): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    if (name.trim() === '') {
      hooks.setDialogError(t('BankNameRequired'));
      return state();
    }
    if (!isEightDigits(blz)) {
      hooks.setDialogError(t('BlzMustBe8Digits'));
      return state();
    }
    if (serverUrl.trim() === '') {
      hooks.setDialogError(t('ErrorServerUrlRequired'));
      return state();
    }
    if (!isHttps(serverUrl)) {
      hooks.setDialogError(t('ErrorServerUrlMustBeHttps'));
      return state();
    }
    const cleanName = name.trim();
    const id = nextBankId;
    nextBankId += 1;
    banks.push({
      id,
      name: cleanName,
      blz: blz.trim(),
      bic: bic.trim().toUpperCase(),
      userId: userId.trim(),
      serverUrl: serverUrl.trim(),
      offline: false,
      includeInSyncAll: true,
      syncUnlocked: true,
      syncUnlockedOn: null,
      syncFrom: null,
      hasSavedPin: false,
      rememberedThisRun: false,
      pinForgetAfter: '1',
    });
    hooks.addOnlineBank(id, cleanName, blz.trim(), bic.trim().toUpperCase());
    hooks.closeDialog();
    errorText = '';
    messageText = t('MessageAdded', cleanName);
    return publish();
  }

  function submitConnection(
    bankId: number,
    name: string,
    blz: string,
    userId: string,
    serverUrl: string,
    pinForgetAfter: string,
  ): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      errorText = t('ErrorBankConnectionNotFound');
      return publish();
    }
    if (name.trim() === '') {
      hooks.setDialogError(t('BankNameRequired'));
      return state();
    }
    if (!bank.offline) {
      if (!isEightDigits(blz)) {
        hooks.setDialogError(t('BlzMustBe8Digits'));
        return state();
      }
      if (serverUrl.trim() === '') {
        hooks.setDialogError(t('ErrorServerUrlRequired'));
        return state();
      }
      if (!isHttps(serverUrl)) {
        hooks.setDialogError(t('ErrorServerUrlMustBeHttps'));
        return state();
      }
      bank.blz = blz.trim();
      bank.userId = userId.trim();
      bank.serverUrl = serverUrl.trim();
      bank.pinForgetAfter = pinForgetAfter === '2' ? '2' : '1';
    }
    bank.name = name.trim();
    hooks.closeDialog();
    errorText = '';
    messageText = t('MessageConnectionSaved');
    return publish();
  }

  function forgetPin(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank !== undefined) {
      bank.hasSavedPin = false;
      bank.rememberedThisRun = false;
    }
    return publish();
  }

  function setIncludeInSyncAll(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank !== undefined) {
      bank.includeInSyncAll = !bank.includeInSyncAll;
    }
    return publish();
  }

  function clearHistory(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    hooks.closeDialog();
    const bank = bankById(bankId);
    if (bank !== undefined) {
      bank.syncFrom = null;
    }
    return publish();
  }

  function submitHistory(bankId: number, input: string): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const date = parseDate(input);
    if (date === null) {
      hooks.setDialogError(t('ErrorSyncFromDateInvalid'));
      return state();
    }
    if (date > todayIso()) {
      hooks.setDialogError(t('ErrorSyncFromDateFuture'));
      return state();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      return state();
    }
    hooks.closeDialog();
    bank.syncFrom = date;
    return startSync(bankId);
  }

  function deleteBank(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const index = banks.findIndex((bank) => bank.id === bankId);
    if (index >= 0) {
      banks.splice(index, 1);
      hooks.closeDialog();
      hooks.removeBank(bankId);
    }
    errorText = '';
    messageText = t('MessageBankRemoved');
    return publish();
  }

  function openConnectionDialog(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      return state();
    }
    hooks.openConnectionDialog({
      title: bank.offline ? t('RenameConnection') : t('OnlineBankingFor', bank.name),
      bankId,
      offline: bank.offline,
      name: bank.name,
      blz: bank.blz,
      userId: bank.userId,
      serverUrl: bank.serverUrl,
      hasPin: bank.hasSavedPin,
      pinText: pinText(bank),
      pinForgetAfter: bank.pinForgetAfter,
      syncing: syncingBankId === bank.id,
    });
    return state();
  }

  function openHistoryDialog(bankId: number): BankingDTO {
    if (isSyncing()) {
      errorText = t('ErrorConnectionBusy');
      return publish();
    }
    const bank = bankById(bankId);
    if (bank === undefined) {
      return state();
    }
    const start = bank.syncFrom ?? isoBack(todayIso(), 90);
    hooks.openHistoryDialog({
      title: t('FetchHistoryFor', bank.name),
      bankId,
      date: formatDate(start),
      hasStart: bank.syncFrom !== null,
    });
    return state();
  }

  function handleBankMenu(bankId: number, action: string): BankingDTO {
    switch (action) {
      case 'sync':
        return startSync(bankId);
      case 'test-connection':
        return testConnection(bankId);
      case 'history':
        return openHistoryDialog(bankId);
      case 'clear-history':
        return clearHistory(bankId);
      case 'include':
        return setIncludeInSyncAll(bankId);
      case 'rename':
      case 'connection':
        return openConnectionDialog(bankId);
      case 'forget-pin':
        return forgetPin(bankId);
      case 'delete':
        hooks.openDeleteBank(bankId);
        return state();
      default:
        return state();
    }
  }

  function clearMessages(): BankingDTO {
    errorText = '';
    messageText = '';
    return publish();
  }

  function clearInfo(): void {
    infoText = '';
  }

  function init(): void {
    alignBanks();
    if (scenario === 'accounts-sync-per-bank') {
      hooks.addOnlineBank(3, 'Nordlicht Bank', '20050550', 'NOLADEHHXXX');
      hooks.addOnlineBank(4, 'Rheintal Hypobank', '70020270', 'HYVEDEMM488');
      alignBanks();
      const sparkasse = bankById(2);
      if (sparkasse !== undefined) {
        sparkasse.includeInSyncAll = true;
        sparkasse.syncUnlocked = true;
        sparkasse.syncUnlockedOn = '2026-11-14';
      }
      const locked = bankById(3);
      if (locked !== undefined) {
        locked.includeInSyncAll = true;
        locked.syncUnlocked = false;
        locked.syncUnlockedOn = null;
        locked.serverUrl = 'https://fints.example/nordlicht';
        locked.userId = 'nordlicht-user';
      }
      const excluded = bankById(4);
      if (excluded !== undefined) {
        excluded.includeInSyncAll = false;
        excluded.syncUnlocked = true;
        excluded.syncUnlockedOn = '2026-11-14';
        excluded.serverUrl = 'https://fints.example/rheintal';
        excluded.userId = 'rheintal-user';
      }
      messageText = syncAllSkipped(['Nordlicht Bank']);
      errorText = '';
    }
    if (scenario === 'accounts-status-strong-sync') {
      const bank = bankById(2);
      if (bank !== undefined) {
        bank.syncUnlocked = false;
        bank.syncUnlockedOn = null;
      }
      connectionTest = {
        bankId: 2,
        running: false,
        go: true,
        verdict: 'go',
        verdictText: 'GO',
        reason: t('TestConnectionGo'),
        nextStep: t('TestConnectionStrongSyncNoUnlock'),
        codes: [],
        messages: [],
        probed: 1,
        skipped: 0,
        strongSync: true,
        strongSyncTAN: 'typed',
        usedSavedPin: false,
        savedPinForgotten: false,
        savedPinRejectedOnce: false,
      };
      lastTrace = { kind: 'test', bankId: 2, hasTrace: true };
      errorText = '';
      messageText = '';
    }
    if (scenario.startsWith('accounts-status-no-server')) {
      const bank = bankById(2);
      if (bank !== undefined) {
        bank.syncUnlocked = false;
        bank.syncUnlockedOn = null;
        bank.serverUrl = '';
      }
      errorText = '';
      messageText = '';
    }
    if (scenario === 'accounts-connection') {
      const bank = bankById(2);
      if (bank !== undefined) {
        bank.hasSavedPin = true;
      }
      openConnectionDialog(2);
    } else if (scenario === 'accounts-history') {
      const bank = bankById(2);
      if (bank !== undefined) {
        bank.syncFrom = '2026-01-01';
      }
      openHistoryDialog(2);
    } else if (scenario === 'accounts-syncing') {
      syncingBankId = 2;
      syncStep = t('SyncConnecting');
      messageText = '';
      errorText = '';
    } else if (scenario === 'accounts-sync-all') {
      syncAll = { position: 0, total: 1, bankName: 'Sparkasse' };
      errorText = '';
      messageText = '';
    }
    const step = promptScenarios[scenario];
    if (step !== undefined) {
      syncingBankId = null;
      const options: PinOptions & TanOptions = {};
      if (step === 'pin') {
        options.rememberVisible = scenario === 'accounts-pin-remember';
      }
      if (step === 'tan') {
        options.imageUrl = scenario === 'accounts-tan-text' ? '' : mockTanChallengeImage;
      }
      setPrompt(promptFor(step, options));
    }
  }

  function setLanguage(next: Language): BankingDTO {
    language = next;
    return publish();
  }

  return {
    init,
    state,
    prompt: promptDTO,
    setLanguage,
    startSync,
    startSyncAll,
    testConnection,
    answerPin,
    answerTan,
    answerChoice,
    answerDecoupled,
    answerProductID,
    cancelPin,
    cancelTan,
    cancelChoice,
    addOnlineBank,
    searchBanks: searchBankSuggestions,
    pickBankSuggestion,
    bankBlzChanged,
    submitConnection,
    forgetPin,
    setIncludeInSyncAll,
    clearHistory,
    submitHistory,
    saveConnectionTrace,
    deleteBank,
    openConnectionDialog,
    openHistoryDialog,
    handleBankMenu,
    clearMessages,
    clearInfo,
    bankForCard(bankId: number): MockBankingCard | undefined {
      alignBanks();
      const bank = bankById(bankId);
      return bank === undefined ? undefined : bankDTO(bank);
    },
    subscribe(callback: (dto: BankingDTO) => void): () => void {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
    subscribePrompt(callback: (dto: PromptDTO) => void): () => void {
      promptListeners.add(callback);
      return () => promptListeners.delete(callback);
    },
  };
}

export type MockBanking = ReturnType<typeof createMockBanking>;
