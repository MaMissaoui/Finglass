import type {
  Api,
  AppInfoDTO,
  AccountsDTO,
  BankingDTO,
  DashboardDTO,
  PromptDTO,
  ShellDTO,
  TickPayload,
} from './api';
import { createMockAccounts } from './accounts.mock';
import { createMockBanking } from './banking.mock';
import { languageFromCode, translate, type Language } from './i18n';
import { MockDashboard, type MockDashboardVariant } from './mock/dashboard';

const info: AppInfoDTO = {
  name: 'Finglass',
  version: '0.4.0-dev',
  goVersion: 'go1.27.4',
  os: 'darwin',
  arch: 'arm64',
};

const correctPassphrase = 'correct horse battery staple';
const versionText = '0.4.0-dev';
const passphraseMinimum = 10;

type MockProfile = {
  name: string;
  slug: string;
  open: boolean;
};

type MockState = {
  mode: string;
  allowChange: boolean;
  busy: boolean;
  restored: boolean;
  startupError: boolean;
  errorText: string;
  profileName: string;
  leftoverLines: string[];
  shellOpen: boolean;
  language: Language;
  theme: string;
  sidebarCollapsed: boolean;
  screen: string;
  backTitle: string;
  backScreen: string;
  profiles: MockProfile[];

  migrationStep: string;
  migrationProfileNames: string[];
  migrationProfileHeading: string;
  migrationProfileName: string;
  migrationProfileIndex: number;
  migrationSamePassphrase: boolean;
  migrationNeedsNewPassphrase: boolean;
  migrationSummaryLines: string[];
};

const pageTargets = ['dashboard', 'accounts', 'transactions', 'documents', 'reports', 'settings'];

const titleKeys: Record<string, string> = {
  dashboard: 'NavDashboard',
  accounts: 'NavAccounts',
  transactions: 'NavTransactions',
  documents: 'NavDocuments',
  reports: 'NavReports',
  settings: 'NavSettings',
};

type DashboardScenario = {
  language: Language;
  theme: string;
  variant: MockDashboardVariant;
};

// The dashboard reference states. The page's own interactions (the month menu
// and a chart hover) are driven by the browser test, so those scenarios only
// choose the same figures as the plain one.
const dashboardScenarios: Record<string, DashboardScenario> = {
  'dashboard-en-light': { language: 'en', theme: 'light', variant: 'normal' },
  'dashboard-de-light': { language: 'de', theme: 'light', variant: 'normal' },
  'dashboard-de-dark': { language: 'de', theme: 'dark', variant: 'normal' },
  'dashboard-full-de-light': { language: 'de', theme: 'light', variant: 'normal' },
  'dashboard-first-run-en': { language: 'en', theme: 'light', variant: 'first-run' },
  'dashboard-no-carry-en-light': { language: 'en', theme: 'light', variant: 'no-carry' },
  'dashboard-month-menu-en-light': { language: 'en', theme: 'light', variant: 'normal' },
  'dashboard-chart-hover-en-light': { language: 'en', theme: 'light', variant: 'normal' },
  'dashboard-error-en-light': { language: 'en', theme: 'light', variant: 'error' },
  'dashboard-loading-en-light': { language: 'en', theme: 'light', variant: 'loading' },
};

// A migration report is runtime data the Go wizard puts into the DTO, not a
// translation key. This is the empty report the reference screenshot shows.
const emptyMigrationReport =
  'Imported 0 account groups, 0 bank connections, 0 accounts, 0 categories, 0 rules, ' +
  '0 transactions, 0 deleted bookings, 0 pending bookings, 0 documents, 0 OCR texts ' +
  'and 0 settings. Skipped 0 rows, 0 documents with a missing file and 0 refused ' +
  'documents. Left 0 payment orders behind.';

function scenarioName(): string {
  if (typeof location === 'undefined') {
    return '';
  }
  return new URLSearchParams(location.search).get('scenario') ?? '';
}

function runeCount(value: string): number {
  return Array.from(value).length;
}

function hasSurroundingSpace(value: string): boolean {
  return /^\s|\s$/u.test(value) || /^\s+$/u.test(value);
}

function sameText(left: string, right: string): boolean {
  return left.normalize('NFC') === right.normalize('NFC');
}

function profileLabel(language: Language, name: string): string {
  return translate(language, 'UnlockProfile', name);
}

function heading(language: Language, index: number, count: number, name: string): string {
  return translate(language, 'MigrationProfileHeading', index, count, name);
}

function baseState(): MockState {
  return {
    mode: 'create',
    allowChange: false,
    busy: false,
    restored: false,
    startupError: false,
    errorText: '',
    profileName: 'Personal',
    leftoverLines: [],
    shellOpen: false,
    language: 'en',
    theme: 'light',
    sidebarCollapsed: false,
    screen: 'dashboard',
    backTitle: '',
    backScreen: '',
    profiles: [{ name: 'Personal', slug: '', open: false }],

    migrationStep: '',
    migrationProfileNames: [],
    migrationProfileHeading: '',
    migrationProfileName: '',
    migrationProfileIndex: 0,
    migrationSamePassphrase: true,
    migrationNeedsNewPassphrase: false,
    migrationSummaryLines: [],
  };
}

function openShell(state: MockState): void {
  state.shellOpen = true;
  state.mode = 'unlock';
  state.allowChange = true;
  state.startupError = false;
  state.restored = false;
  state.errorText = '';
  state.screen = 'dashboard';
  state.backTitle = '';
  state.backScreen = '';
  for (const profile of state.profiles) {
    profile.open = profile.slug === '' || profile.name === state.profileName;
  }
  state.profileName = state.profiles.find((profile) => profile.open)?.name ?? state.profileName;
}

function initialState(scenario: string): MockState {
  const state = baseState();

  const dashboard = dashboardScenarios[scenario];
  if (dashboard !== undefined) {
    openShell(state);
    state.language = dashboard.language;
    state.theme = dashboard.theme;
    return state;
  }

  switch (scenario) {
    case 'leftovers':
      state.mode = 'unlock';
      state.allowChange = true;
      state.language = 'en';
      state.leftoverLines = [
        `${translate('en', 'PlaintextLeftoverPreRestore')} · pre-restore-20261015-123456`,
        `${translate('en', 'PlaintextLeftoverDeletedProfile')} · profiles-deleted/business-20261015-123456`,
      ];
      break;
    case 'restored':
      state.mode = 'unlock';
      state.allowChange = true;
      state.restored = true;
      break;
    case 'already-running':
      state.mode = 'unlock';
      state.startupError = true;
      state.errorText = translate('en', 'ErrorAlreadyRunning');
      break;
    case 'target-after-switch':
      state.mode = 'unlock';
      state.allowChange = true;
      state.profileName = 'Business';
      state.profiles = [
        { name: 'Personal', slug: '', open: false },
        { name: 'Business', slug: 'business', open: false },
      ];
      break;
    case 'unlock-wrong-de':
      state.mode = 'unlock';
      state.allowChange = true;
      state.language = 'de';
      break;
    case 'shell':
    case 'shell-collapsed':
    case 'shell-de-dark':
    case 'shell-profiles':
    case 'shell-profiles-de':
    case 'shell-back':
    case 'accounts':
    case 'accounts-empty':
    case 'accounts-add-account':
    case 'accounts-detail':
    case 'accounts-panel':
    case 'accounts-de-light':
    case 'accounts-de-dark':
    case 'accounts-detail-flat-de':
    case 'accounts-add-online-bank':
    case 'accounts-add-online-bank-search':
    case 'accounts-add-online-bank-central-note':
    case 'accounts-add-online-bank-no-match-de-dark':
    case 'accounts-bank-menu-en-light':
    case 'accounts-bank-menu-de-dark':
    case 'accounts-connection':
    case 'accounts-history':
    case 'accounts-sync-all':
    case 'accounts-syncing':
    case 'accounts-pin':
    case 'accounts-pin-remember':
    case 'accounts-tan-method':
    case 'accounts-tan-medium':
    case 'accounts-tan-photo':
    case 'accounts-tan-text':
    case 'accounts-decoupled':
    case 'banking-walkthrough':
      openShell(state);
      state.theme =
        scenario === 'shell-de-dark' ||
        scenario === 'accounts-de-dark' ||
        scenario === 'accounts-bank-menu-de-dark' ||
        scenario === 'accounts-add-online-bank-no-match-de-dark'
          ? 'dark'
          : 'light';
      state.language =
        scenario === 'shell-de-dark' ||
        scenario === 'shell-profiles-de' ||
        scenario === 'accounts-de-light' ||
        scenario === 'accounts-de-dark' ||
        scenario === 'accounts-detail-flat-de' ||
        scenario === 'accounts-add-online-bank-no-match-de-dark' ||
        scenario === 'accounts-bank-menu-de-dark'
          ? 'de'
          : 'en';
      state.sidebarCollapsed = scenario === 'shell-collapsed' || scenario === 'shell-de-dark';
      if (scenario.startsWith('accounts') || scenario === 'banking-walkthrough') {
        state.screen = 'accounts';
      }
      if (scenario === 'shell-profiles' || scenario === 'shell-profiles-de') {
        state.profiles = [
          { name: 'Personal', slug: '', open: true },
          { name: 'Business', slug: 'business', open: false },
        ];
        state.profileName = 'Personal';
      }
      if (scenario === 'shell-back') {
        state.screen = 'reports';
        state.backTitle = translate(state.language, 'NavDashboard');
        state.backScreen = 'dashboard';
      }
      break;
    case 'migration-welcome':
      state.mode = 'migrate';
      state.migrationStep = 'welcome';
      state.migrationProfileNames = ['Personal', 'Business'];
      break;
    case 'migration-profile':
    case 'migration-profile-new-passphrase':
    case 'migration-wrong-passphrase':
      state.mode = 'migrate';
      state.migrationStep = 'profile';
      state.migrationProfileNames = ['Personal', 'Business'];
      state.migrationProfileIndex = 1;
      state.migrationProfileName = 'Business';
      if (scenario === 'migration-profile-new-passphrase') {
        state.migrationSamePassphrase = false;
        state.migrationNeedsNewPassphrase = true;
      }
      if (scenario === 'migration-wrong-passphrase') {
        state.language = 'de';
        state.theme = 'dark';
        state.errorText = translate('de', 'WrongPassphrase');
      }
      state.migrationProfileHeading = heading(state.language, 2, 2, 'Business');
      break;
    case 'migration-summary':
      state.mode = 'migrate';
      state.migrationStep = 'summary';
      state.migrationSummaryLines = [
        translate('en', 'MigrationProfileMigrated', 'Personal', emptyMigrationReport),
        translate('en', 'MigrationProfileSkipped', 'Business'),
      ];
      break;
    default:
      break;
  }

  return state;
}

export function createMockApi(scenario = scenarioName()): Api {
  const state = initialState(scenario);
  const dashboard = new MockDashboard(
    dashboardScenarios[scenario]?.variant ?? 'normal',
    state.language,
  );
  const shellListeners = new Set<(dto: ShellDTO) => void>();
  const menuListeners = new Set<(action: string) => void>();
  const dashboardListeners = new Set<(dto: DashboardDTO) => void>();
  const accounts = createMockAccounts(scenario, state.language);
  const banking = createMockBanking(scenario, state.language, {
    openAddBank: () => {
      accounts.openAddBank();
    },
    setDialogError: (text: string) => {
      accounts.setDialogError(text);
    },
    closeDialog: () => {
      accounts.closeDialog();
    },
    addOnlineBank: (bankId, name, blz, bic) => {
      accounts.addOnlineBank(bankId, name, blz, bic);
    },
    removeBank: (bankId) => {
      accounts.removeBankById(bankId);
    },
    openConnectionDialog: (dialog) => {
      accounts.openBankingDialog({
        kind: 'connection',
        title: dialog.title,
        bankId: dialog.bankId,
        offline: dialog.offline,
        accountName: dialog.name,
        connectionName: dialog.name,
        connectionBlz: dialog.blz,
        connectionUserId: dialog.userId,
        connectionServerUrl: dialog.serverUrl,
        connectionHasPin: dialog.hasPin,
        connectionPinText: dialog.pinText,
        connectionSyncing: dialog.syncing,
      });
    },
    openHistoryDialog: (dialog) => {
      accounts.openBankingDialog({
        kind: 'history',
        title: dialog.title,
        bankId: dialog.bankId,
        historyDate: dialog.date,
        historyHasStart: dialog.hasStart,
      });
    },
    openDeleteBank: (bankId) => {
      accounts.openDeleteBank(bankId);
    },
    listBanks: () => accounts.listBanks(),
    republishAccounts: () => {
      accounts.republish();
    },
  });
  accounts.setBankingSource((bankId) => banking.bankForCard(bankId));
  banking.init();
  const debug = {
    accountsStateCalls: 0,
    bankingStateCalls: 0,
    bankingPromptCalls: 0,
    bankingPromptId: () => banking.prompt().id,
    bankingAnswerPin: (id: number, pin: string) => banking.answerPin(id, pin, false),
    bankingAnswerTan: (id: number, tan: string) => banking.answerTan(id, tan),
    bankingAnswerChoice: (id: number, choice: string) => banking.answerChoice(id, choice),
    bankingAnswerDecoupled: (id: number, confirmed: boolean) =>
      banking.answerDecoupled(id, confirmed),
  };
  if (typeof window !== 'undefined') {
    (window as unknown as { __finglassMock?: typeof debug }).__finglassMock = debug;
  }

  function dto(): ShellDTO {
    const language = state.language;
    const openProfile = state.profiles.find((profile) => profile.open);
    return {
      mode: state.mode,
      allowChange: state.allowChange,
      busy: state.busy,
      restored: state.restored,
      startupError: state.startupError,
      errorText: state.errorText,
      profileLabel: profileLabel(language, state.profileName),
      profileName: state.profileName,
      passphraseHint: translate(language, 'PassphraseHint', passphraseMinimum),
      leftoverLines: [...state.leftoverLines],
      shellOpen: state.shellOpen,
      language,
      theme: state.theme,
      sidebarCollapsed: state.sidebarCollapsed,
      screen: state.screen,
      screenTitle: translate(language, titleKeys[state.screen] ?? 'NavDashboard'),
      backTitle: state.backTitle,
      currentProfileName: state.shellOpen
        ? (openProfile?.name ?? state.profileName)
        : state.profileName,
      profiles: state.profiles.map((profile) => ({ ...profile })),
      version: versionText,
      aboutVersion: translate(language, 'AboutVersion', versionText),
      findEnabled: state.screen === 'transactions' || state.screen === 'documents',
      migrationStep: state.migrationStep,
      migrationProfileNames: [...state.migrationProfileNames],
      migrationProfileHeading: state.migrationProfileHeading,
      migrationProfileName: state.migrationProfileName,
      migrationProfileCount: state.migrationProfileNames.length,
      migrationSamePassphrase: state.migrationSamePassphrase,
      migrationNewPassphraseRequired:
        !state.migrationSamePassphrase || state.migrationNeedsNewPassphrase,
      migrationSummaryLines: [...state.migrationSummaryLines],
    };
  }

  function publish(): ShellDTO {
    const next = dto();
    for (const listener of shellListeners) {
      listener(next);
    }
    return next;
  }

  function publishDashboardWith(change: () => DashboardDTO): DashboardDTO {
    const next = change();
    for (const listener of dashboardListeners) {
      listener(next);
    }
    return next;
  }

  function publishDashboard(): DashboardDTO {
    return publishDashboardWith(() => dashboard.dto());
  }

  function openScreen(screen: string): DashboardDTO {
    state.screen = screen;
    state.backTitle = '';
    state.backScreen = '';
    publish();
    return dashboard.dto();
  }

  function validateNew(passphrase: string, repeat: string, acknowledged: boolean): string {
    if (runeCount(passphrase) < passphraseMinimum) {
      return translate(state.language, 'PassphraseTooShort', passphraseMinimum);
    }
    if (hasSurroundingSpace(passphrase)) {
      return translate(state.language, 'PassphraseSpaces');
    }
    if (!sameText(passphrase, repeat)) {
      return translate(state.language, 'PassphraseMismatch');
    }
    if (!acknowledged) {
      return translate(state.language, 'PassphraseAcknowledge');
    }
    return '';
  }

  function advanceMigration(outcome: 'migrated' | 'skipped'): void {
    const name = state.migrationProfileName;
    if (name !== '') {
      state.migrationSummaryLines.push(
        outcome === 'skipped'
          ? translate(state.language, 'MigrationProfileSkipped', name)
          : translate(state.language, 'MigrationProfileMigrated', name, emptyMigrationReport),
      );
    }
    state.migrationProfileIndex += 1;
    state.migrationSamePassphrase = true;
    state.migrationNeedsNewPassphrase = false;
    state.errorText = '';
    if (state.migrationProfileIndex >= state.migrationProfileNames.length) {
      state.migrationStep = 'summary';
      state.migrationProfileHeading = '';
      state.migrationProfileName = '';
      return;
    }
    state.migrationStep = 'profile';
    state.migrationProfileName = state.migrationProfileNames[state.migrationProfileIndex] ?? '';
    state.migrationProfileHeading = heading(
      state.language,
      state.migrationProfileIndex + 1,
      state.migrationProfileNames.length,
      state.migrationProfileName,
    );
  }

  const api: Api = {
    async info() {
      return { ...info };
    },
    async greet(name: string) {
      const trimmed = name.trim();
      if (!trimmed) {
        throw new Error('name must not be empty');
      }
      return `Hello, ${trimmed}`;
    },
    onTick(callback: (payload: TickPayload) => void) {
      let count = 0;
      const timer = setInterval(() => {
        count += 1;
        callback({ count });
      }, 1000);
      return () => clearInterval(timer);
    },

    async state() {
      return dto();
    },
    async unlock(passphrase: string) {
      if (sameText(passphrase, correctPassphrase)) {
        openShell(state);
        state.errorText = '';
        state.profileName =
          state.profiles.find((profile) => profile.open)?.name ?? state.profileName;
      } else {
        state.errorText = translate(state.language, 'WrongPassphrase');
      }
      return publish();
    },
    async create(passphrase: string, repeat: string, acknowledged: boolean) {
      const error = validateNew(passphrase, repeat, acknowledged);
      if (error !== '') {
        state.errorText = error;
        state.shellOpen = false;
        return publish();
      }
      openShell(state);
      state.errorText = '';
      return publish();
    },
    async change(current: string, passphrase: string, repeat: string, acknowledged: boolean) {
      if (!sameText(current, correctPassphrase)) {
        state.errorText = translate(state.language, 'WrongPassphrase');
        return publish();
      }
      const error = validateNew(passphrase, repeat, acknowledged);
      if (error !== '') {
        state.errorText = error;
        return publish();
      }
      openShell(state);
      state.errorText = '';
      return publish();
    },
    async showChange(change: boolean) {
      state.mode = change ? 'change' : 'unlock';
      state.errorText = '';
      return publish();
    },
    async navigate(screen: string) {
      if (pageTargets.includes(screen)) {
        state.screen = screen;
        state.backTitle = '';
        state.backScreen = '';
      }
      return publish();
    },
    async navigateMenuItem(index: number) {
      const screen = pageTargets[index];
      if (screen !== undefined) {
        state.screen = screen;
        state.backTitle = '';
        state.backScreen = '';
      }
      return publish();
    },
    async back() {
      if (state.backTitle !== '') {
        state.screen = state.backScreen || 'dashboard';
        state.backTitle = '';
        state.backScreen = '';
      }
      return publish();
    },
    async setLanguage(code: string) {
      state.language = languageFromCode(code);
      dashboard.setLanguage(state.language);
      accounts.setLanguage(state.language);
      banking.setLanguage(state.language);
      const next = publish();
      publishDashboard();
      return next;
    },
    async setTheme(code: string) {
      if (code === 'light' || code === 'dark' || code === 'system') {
        state.theme = code;
      }
      return publish();
    },
    async setSidebarCollapsed(collapsed: boolean) {
      state.sidebarCollapsed = collapsed;
      return publish();
    },
    async switchProfile(index: number) {
      const target = state.profiles[index];
      if (target === undefined || !state.shellOpen) {
        return dto();
      }
      if (target.open) {
        return dto();
      }
      for (const profile of state.profiles) {
        profile.open = false;
      }
      target.open = true;
      state.profileName = target.name;
      state.mode = 'unlock';
      state.allowChange = true;
      state.shellOpen = false;
      state.screen = 'dashboard';
      state.backTitle = '';
      state.backScreen = '';
      state.errorText = '';
      return publish();
    },
    async manageProfiles() {
      state.screen = 'settings';
      state.backTitle = '';
      state.backScreen = '';
      return publish();
    },

    async migrationMigrate(acknowledged: boolean) {
      if (acknowledged && state.migrationStep === 'welcome') {
        state.migrationStep = 'profile';
        state.migrationProfileIndex = 0;
        state.migrationProfileName = state.migrationProfileNames[0] ?? '';
        state.migrationProfileHeading = heading(
          state.language,
          1,
          state.migrationProfileNames.length,
          state.migrationProfileName,
        );
        state.errorText = '';
      }
      return publish();
    },
    async migrationStartEmpty() {
      state.mode = 'create';
      state.migrationStep = '';
      state.migrationProfileNames = [];
      state.migrationSummaryLines = [];
      state.errorText = '';
      return publish();
    },
    async migrationSubmit(
      passphrase: string,
      newPassphrase: string,
      repeat: string,
      samePassphrase: boolean,
    ) {
      if (state.migrationStep !== 'profile') {
        return dto();
      }
      state.migrationSamePassphrase = samePassphrase;
      if (passphrase === '') {
        state.errorText = translate(state.language, 'WrongPassphrase');
        return publish();
      }
      if (samePassphrase) {
        if (runeCount(passphrase) < passphraseMinimum) {
          state.migrationNeedsNewPassphrase = true;
          state.errorText = translate(state.language, 'MigrationNewPassphraseNeeded');
          return publish();
        }
      } else {
        const error = validateNew(newPassphrase, repeat, true);
        if (error !== '') {
          state.errorText = error;
          return publish();
        }
      }
      state.migrationNeedsNewPassphrase = false;
      advanceMigration('migrated');
      return publish();
    },
    async migrationSkip() {
      if (state.migrationStep === 'profile') {
        advanceMigration('skipped');
      }
      return publish();
    },
    async migrationContinue() {
      state.mode = 'create';
      state.migrationStep = '';
      state.migrationProfileNames = [];
      state.migrationSummaryLines = [];
      state.profileName = 'Personal';
      state.profiles = [{ name: 'Personal', slug: '', open: false }];
      state.shellOpen = false;
      state.allowChange = false;
      state.errorText = '';
      return publish();
    },
    async migrationPassphraseChanged(text: string) {
      if (state.migrationStep !== 'profile' || !state.migrationSamePassphrase) {
        return publish();
      }
      state.migrationNeedsNewPassphrase = text !== '' && runeCount(text) < passphraseMinimum;
      return publish();
    },

    async openGitHub() {
      return dto();
    },
    async reportProblem() {
      return dto();
    },

    async dashboardState() {
      return dashboard.dto();
    },
    async dashboardPreviousMonth() {
      return publishDashboardWith(() => dashboard.previous());
    },
    async dashboardNextMonth() {
      return publishDashboardWith(() => dashboard.next());
    },
    async dashboardCurrentMonth() {
      return publishDashboardWith(() => dashboard.current());
    },
    async dashboardSelectMonth(index: number) {
      return publishDashboardWith(() => dashboard.select(index));
    },
    async dashboardDismissError() {
      return publishDashboardWith(() => dashboard.dismissError());
    },
    async dashboardOpenCategory() {
      return openScreen('transactions');
    },
    async dashboardOpenUncategorized() {
      return openScreen('transactions');
    },
    async dashboardOpenAll() {
      return openScreen('transactions');
    },
    async dashboardOpenPartner() {
      return openScreen('transactions');
    },
    async dashboardOpenAccounts() {
      return openScreen('accounts');
    },
    async dashboardOpenSettings() {
      return openScreen('settings');
    },
    async dashboardOpenTax() {
      return openScreen('reports');
    },
    accountsState() {
      debug.accountsStateCalls += 1;
      return Promise.resolve(accounts.state());
    },
    accountsLoad() {
      return Promise.resolve(accounts.load());
    },
    accountsOpenAddBank() {
      return Promise.resolve(accounts.openAddBank());
    },
    accountsCloseDialog() {
      return Promise.resolve(accounts.closeDialog());
    },
    accountsSubmitAddBank(name: string) {
      return Promise.resolve(accounts.submitAddBank(name));
    },
    accountsOpenAddAccount(bankID: number) {
      return Promise.resolve(accounts.openAddAccount(bankID));
    },
    accountsSubmitAddAccount(
      bankID: number,
      name: string,
      identifier: string,
      accountType: string,
      currency: string,
      balance: string,
    ) {
      return Promise.resolve(
        accounts.submitAddAccount(bankID, name, identifier, accountType, currency, balance),
      );
    },
    accountsOpenBalance(accountID: number) {
      return Promise.resolve(accounts.openBalance(accountID));
    },
    accountsSubmitBalance(accountID: number, balance: string, accountType: string) {
      return Promise.resolve(accounts.submitBalance(accountID, balance, accountType));
    },
    accountsOpenRename(accountID: number) {
      return Promise.resolve(accounts.openRename(accountID));
    },
    accountsSubmitRename(accountID: number, name: string) {
      return Promise.resolve(accounts.submitRename(accountID, name));
    },
    accountsOpenDeleteAccount(accountID: number) {
      return Promise.resolve(accounts.openDeleteAccount(accountID));
    },
    accountsConfirmDeleteAccount(accountID: number) {
      return Promise.resolve(accounts.confirmDeleteAccount(accountID));
    },
    accountsOpenDeleteBank(bankID: number) {
      return Promise.resolve(accounts.openDeleteBank(bankID));
    },
    accountsConfirmDeleteBank(bankID: number) {
      const bank = accounts.listBanks().find((entry) => entry.id === bankID);
      if (bank !== undefined && !bank.offline) {
        banking.deleteBank(bankID);
        return Promise.resolve(accounts.state());
      }
      return Promise.resolve(accounts.confirmDeleteBank(bankID));
    },
    accountsOpenDetail(accountID: number) {
      return Promise.resolve(accounts.openDetail(accountID));
    },
    accountsSelectDays(accountID: number, days: number) {
      return Promise.resolve(accounts.selectDays(accountID, days));
    },
    accountsCloseDetail() {
      return Promise.resolve(accounts.closeDetail());
    },
    accountsFilterChanged(text: string) {
      return Promise.resolve(accounts.filterChanged(text));
    },
    accountsDismissMessage() {
      return Promise.resolve(accounts.dismissMessage());
    },
    accountsSelect(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(accounts.select(kind, groupKind, id, accountType));
    },
    accountsToggleGroup(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(accounts.toggleGroup(kind, groupKind, id, accountType));
    },
    accountsMarkRead(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(accounts.markRead(kind, groupKind, id, accountType));
    },
    accountsToggleBank(bankID: number) {
      return Promise.resolve(accounts.toggleBank(bankID));
    },
    accountsForgetDeleted(accountID: number) {
      return Promise.resolve(accounts.forgetDeleted(accountID));
    },
    accountsNewGroup(name: string) {
      return Promise.resolve(accounts.newGroup(name));
    },
    accountsRenameGroup(id: number, name: string) {
      return Promise.resolve(accounts.renameGroup(id, name));
    },
    accountsDeleteGroup(id: number) {
      return Promise.resolve(accounts.deleteGroup(id));
    },
    accountsMoveGroup(id: number, up: boolean) {
      return Promise.resolve(accounts.moveGroup(id, up));
    },
    accountsMoveAccount(accountID: number, groupID: number) {
      return Promise.resolve(accounts.moveAccount(accountID, groupID));
    },
    accountsOpenTransactions(accountID: number) {
      void accountID;
      const result = accounts.state();
      state.screen = 'transactions';
      state.backTitle = translate(state.language, 'NavAccounts');
      state.backScreen = 'accounts';
      publish();
      return Promise.resolve(result);
    },

    bankingState() {
      debug.bankingStateCalls += 1;
      return Promise.resolve(banking.state());
    },
    bankingPrompt() {
      debug.bankingPromptCalls += 1;
      return Promise.resolve(banking.prompt());
    },
    bankingLoad() {
      return Promise.resolve(banking.state());
    },
    bankingRefresh() {
      return Promise.resolve(banking.state());
    },
    bankingOpenAddBank() {
      accounts.openAddBank();
      return Promise.resolve(banking.state());
    },
    bankingSubmitAddOnlineBank(
      name: string,
      blz: string,
      bic: string,
      userID: string,
      serverURL: string,
    ) {
      return Promise.resolve(banking.addOnlineBank(name, blz, bic, userID, serverURL));
    },
    bankingSubmitConnection(
      bankID: number,
      name: string,
      blz: string,
      userID: string,
      serverURL: string,
    ) {
      return Promise.resolve(banking.submitConnection(bankID, name, blz, userID, serverURL));
    },
    bankingForgetPin(bankID: number) {
      return Promise.resolve(banking.forgetPin(bankID));
    },
    bankingSetIncludeInSyncAll(bankID: number) {
      return Promise.resolve(banking.setIncludeInSyncAll(bankID));
    },
    bankingClearHistory(bankID: number) {
      return Promise.resolve(banking.clearHistory(bankID));
    },
    bankingSubmitHistory(bankID: number, date: string) {
      return Promise.resolve(banking.submitHistory(bankID, date));
    },
    bankingStartSync(bankID: number) {
      return Promise.resolve(banking.startSync(bankID));
    },
    bankingStartSyncAll() {
      return Promise.resolve(banking.startSyncAll());
    },
    bankingDeleteBank(bankID: number) {
      return Promise.resolve(banking.deleteBank(bankID));
    },
    bankingOpenConnectionDialog(bankID: number) {
      return Promise.resolve(banking.openConnectionDialog(bankID));
    },
    bankingOpenHistoryDialog(bankID: number) {
      return Promise.resolve(banking.openHistoryDialog(bankID));
    },
    bankingHandleBankMenu(bankID: number, action: string) {
      return Promise.resolve(banking.handleBankMenu(bankID, action));
    },
    bankingClearMessages() {
      return Promise.resolve(banking.clearMessages());
    },
    bankingClearInfo() {
      banking.clearInfo();
      return Promise.resolve();
    },
    bankingAnswerPin(id: number, pin: string, remember: boolean) {
      banking.answerPin(id, pin, remember);
      return Promise.resolve();
    },
    bankingCancelPin(id: number) {
      banking.cancelPin(id);
      return Promise.resolve();
    },
    bankingAnswerTan(id: number, tan: string) {
      banking.answerTan(id, tan);
      return Promise.resolve();
    },
    bankingCancelTan(id: number) {
      banking.cancelTan(id);
      return Promise.resolve();
    },
    bankingAnswerChoice(id: number, choice: string) {
      banking.answerChoice(id, choice);
      return Promise.resolve();
    },
    bankingCancelChoice(id: number) {
      banking.cancelChoice(id);
      return Promise.resolve();
    },
    bankingAnswerDecoupled(id: number, confirmed: boolean) {
      banking.answerDecoupled(id, confirmed);
      return Promise.resolve();
    },

    onShellChanged(callback: (next: ShellDTO) => void) {
      shellListeners.add(callback);
      return () => shellListeners.delete(callback);
    },
    onAccountsChanged(callback: (next: AccountsDTO) => void) {
      return accounts.subscribe(callback);
    },
    onBankingChanged(callback: (next: BankingDTO) => void) {
      return banking.subscribe(callback);
    },
    onBankingPrompt(callback: (next: PromptDTO) => void) {
      return banking.subscribePrompt(callback);
    },
    onMenuAction(callback: (action: string) => void) {
      menuListeners.add(callback);
      return () => menuListeners.delete(callback);
    },
    onDashboardChanged(callback: (next: DashboardDTO) => void) {
      dashboardListeners.add(callback);
      return () => dashboardListeners.delete(callback);
    },
  };

  if (typeof window !== 'undefined') {
    // Mock-build only: lets the end-to-end tests deliver a menu:action event
    // without a native menu.
    window.addEventListener('finglass:mock-menu-action', (event) => {
      const action = (event as CustomEvent<string>).detail;
      for (const listener of menuListeners) {
        listener(action);
      }
    });
  }

  return api;
}
