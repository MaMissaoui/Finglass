import type {
  Api,
  AppInfoDTO,
  AccountsDTO,
  BankingDTO,
  DashboardDTO,
  DocumentsDTO,
  PromptDTO,
  ShellDTO,
  TickPayload,
  TransactionsDTO,
  ReportsDTO,
  SettingsDTO,
} from './api';
import { createMockAccounts } from './accounts.mock';
import { createMockBanking } from './banking.mock';
import { createMockDocuments } from './documents.mock';
import { languageFromCode, translate, type Language } from './i18n';
import { MockDashboard, type MockDashboardVariant } from './mock/dashboard';
import { MockReports } from './reports.mock';
import { createMockSettings } from './settings.mock';
import { createMockTransactions } from './transactions.mock';

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

// The Transactions reference states. The page's own interactions (a row hover,
// the open More menu) are driven by the browser test; those scenarios only
// choose the same state the reference shows.
type TransactionsScenario = {
  language: Language;
  theme: string;
};

const transactionsScenarios: Record<string, TransactionsScenario> = {
  transactions: { language: 'en', theme: 'light' },
  'transactions-en-light': { language: 'en', theme: 'light' },
  'transactions-de-light': { language: 'de', theme: 'light' },
  'transactions-de-dark': { language: 'de', theme: 'dark' },
  'transactions-month-de-light': { language: 'de', theme: 'light' },
  'transactions-narrow-en': { language: 'en', theme: 'light' },
  'transactions-chart-en': { language: 'en', theme: 'light' },
  'transactions-chart-collapsed-en': { language: 'en', theme: 'light' },
  'transactions-panel-collapsed-en-dark': { language: 'en', theme: 'dark' },
  'transactions-panel-expanded-en-dark': { language: 'en', theme: 'dark' },
  'transactions-row-hover-en-light': { language: 'en', theme: 'light' },
  'transactions-selection-en': { language: 'en', theme: 'light' },
  'transactions-detail-en': { language: 'en', theme: 'light' },
  'transactions-more-menu-en': { language: 'en', theme: 'light' },
  'transactions-custom-range-en': { language: 'en', theme: 'light' },
  'transactions-from-dashboard-category-en-light': { language: 'en', theme: 'light' },
  'transactions-add-en': { language: 'en', theme: 'light' },
  'transactions-duplicate-en': { language: 'en', theme: 'light' },
  'transactions-multi-delete-en': { language: 'en', theme: 'light' },
  'transactions-reapply-en': { language: 'en', theme: 'light' },
  'transactions-rule-dialog-en': { language: 'en', theme: 'light' },
  'transactions-import-csv-en': { language: 'en', theme: 'light' },
  'transactions-import-en-light': { language: 'en', theme: 'light' },
  'transactions-import-de-dark': { language: 'de', theme: 'dark' },
  'transactions-import-mt940-en': { language: 'en', theme: 'light' },
  'transactions-import-warnings-en': { language: 'en', theme: 'light' },
};

// The Reports reference states. A scenario also chooses the tab the page
// opens on, as a dashboard tax link does for the tax tab.
const reportsScenarios: Record<string, { language: Language; theme: string }> = {
  'reports-summary-en-light': { language: 'en', theme: 'light' },
  'reports-summary-de-light': { language: 'de', theme: 'light' },
  'reports-summary-de-dark': { language: 'de', theme: 'dark' },
  'reports-category-en-light': { language: 'en', theme: 'light' },
  'reports-monthly-en-light': { language: 'en', theme: 'light' },
  'reports-monthly-de-dark': { language: 'de', theme: 'dark' },
  'reports-recurring-en-light': { language: 'en', theme: 'light' },
  'reports-recurring-de-light': { language: 'de', theme: 'light' },
  'reports-tax-en-light': { language: 'en', theme: 'light' },
  'reports-tax-de-dark': { language: 'de', theme: 'dark' },
  'reports-tax-missing-en-light': { language: 'en', theme: 'light' },
  'reports-tax-narrow-en-light': { language: 'en', theme: 'light' },
  'reports-export-menu-en-light': { language: 'en', theme: 'light' },
};

// The Settings reference states. The section, language and theme are what the
// screenshot shows; the dialogs are part of the state because the page opens
// them through the DTO.
type SettingsScenario = {
  language: Language;
  theme: string;
  section: number;
  emptyCategories?: boolean;
  categoryDialogFood?: boolean;
  ruleDialogFood?: boolean;
  ruleImportPreview?: boolean;
  profileDelete?: boolean;
  pendingRestore?: boolean;
  restoreDialog?: boolean;
  restorePassphrase?: boolean;
  findeskPassphrase?: boolean;
  findeskReport?: boolean;
  findeskTargetNotEmpty?: boolean;
  findeskNotAFolder?: boolean;
  findeskBusy?: boolean;
  backupRestored?: boolean;
};

const settingsScenarios: Record<string, SettingsScenario> = {
  settings: { language: 'en', theme: 'light', section: 0 },
  'settings-empty': { language: 'en', theme: 'light', section: 2, emptyCategories: true },
  'settings-appearance-de-light': { language: 'de', theme: 'light', section: 0 },
  'settings-appearance-de-dark': { language: 'de', theme: 'dark', section: 0 },
  'settings-currency-en-light': { language: 'en', theme: 'light', section: 0 },
  'settings-currency-de-dark': { language: 'de', theme: 'dark', section: 0 },
  'settings-categories-en-light': { language: 'en', theme: 'light', section: 2 },
  'settings-categories-de-light': { language: 'de', theme: 'light', section: 2 },
  'settings-categories-de-dark': { language: 'de', theme: 'dark', section: 2 },
  'settings-categories-de-narrow': { language: 'de', theme: 'light', section: 2 },
  'settings-category-dialog-en': {
    language: 'en',
    theme: 'light',
    section: 2,
    categoryDialogFood: true,
  },
  'settings-rules-en-light': { language: 'en', theme: 'light', section: 3 },
  'settings-rules-de-light': { language: 'de', theme: 'light', section: 3 },
  'settings-rules-de-dark': { language: 'de', theme: 'dark', section: 3 },
  'settings-rule-dialog-en': {
    language: 'en',
    theme: 'light',
    section: 3,
    ruleDialogFood: true,
  },
  'settings-rule-import-en': {
    language: 'en',
    theme: 'light',
    section: 3,
    ruleImportPreview: true,
  },
  'settings-tax-keywords-en-light': { language: 'en', theme: 'light', section: 2 },
  'settings-tax-keywords-de-dark': { language: 'de', theme: 'dark', section: 2 },
  'settings-about-bank-codes-en': { language: 'en', theme: 'light', section: 8 },
  'settings-section-menu-en-light': { language: 'en', theme: 'light', section: 0 },
  'settings-narrow-en-light': { language: 'en', theme: 'light', section: 0 },
  'settings-fr-light': { language: 'fr', theme: 'light', section: 0 },
  'settings-profiles-en-light': { language: 'en', theme: 'light', section: 1 },
  'settings-profiles-de-dark': { language: 'de', theme: 'dark', section: 1 },
  'settings-profile-delete-en': {
    language: 'en',
    theme: 'light',
    section: 1,
    profileDelete: true,
  },
  'settings-backup-en-light': { language: 'en', theme: 'light', section: 5 },
  'settings-backup-de-dark': { language: 'de', theme: 'dark', section: 5 },
  'settings-backup-pending-en': {
    language: 'en',
    theme: 'light',
    section: 5,
    pendingRestore: true,
  },
  'settings-backup-restored-en': {
    language: 'en',
    theme: 'light',
    section: 5,
    backupRestored: true,
  },
  'settings-restore-dialog-en': {
    language: 'en',
    theme: 'light',
    section: 5,
    restoreDialog: true,
  },
  'settings-restore-passphrase-en': {
    language: 'en',
    theme: 'light',
    section: 5,
    restorePassphrase: true,
  },
  'settings-findesk-import-en-light': { language: 'en', theme: 'light', section: 6 },
  'settings-findesk-import-de-dark': { language: 'de', theme: 'dark', section: 6 },
  'settings-findesk-passphrase-en': {
    language: 'en',
    theme: 'light',
    section: 6,
    findeskPassphrase: true,
  },
  'settings-findesk-importing-en': {
    language: 'en',
    theme: 'light',
    section: 6,
    findeskBusy: true,
  },
  'settings-findesk-report-en': {
    language: 'en',
    theme: 'light',
    section: 6,
    findeskReport: true,
  },
  'settings-findesk-target-not-empty-en': {
    language: 'en',
    theme: 'light',
    section: 6,
    findeskTargetNotEmpty: true,
  },
  'settings-findesk-not-a-folder-en': {
    language: 'en',
    theme: 'light',
    section: 6,
    findeskNotAFolder: true,
  },
};

// The Documents reference states. The page's own interactions (a selected row,
// the delete dialog) are driven by the browser test; these scenarios choose the
// reference state and the detail states the fake can serve.
type DocumentsScenario = {
  language: Language;
  theme: string;
};

const documentsScenarios: Record<string, DocumentsScenario> = {
  documents: { language: 'en', theme: 'light' },
  'documents-en-light': { language: 'en', theme: 'light' },
  'documents-de-light': { language: 'de', theme: 'light' },
  'documents-de-dark': { language: 'de', theme: 'dark' },
  'documents-empty-en': { language: 'en', theme: 'light' },
  'documents-linked-en-light': { language: 'en', theme: 'light' },
  'documents-candidates-en': { language: 'en', theme: 'light' },
  'documents-uploading-en': { language: 'en', theme: 'light' },
  'documents-message-en': { language: 'en', theme: 'light' },
  'documents-error-en': { language: 'en', theme: 'light' },
  'documents-ocr-unavailable-en': { language: 'en', theme: 'light' },
  'documents-loading-en': { language: 'en', theme: 'light' },
  'documents-no-match-en': { language: 'en', theme: 'light' },
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

  const transactions = transactionsScenarios[scenario];
  if (transactions !== undefined) {
    openShell(state);
    state.language = transactions.language;
    state.theme = transactions.theme;
    state.screen = 'transactions';
    if (scenario === 'transactions-from-dashboard-category-en-light') {
      state.backTitle = translate(state.language, 'Back');
      state.backScreen = 'dashboard';
    }
    return state;
  }

  const reports = reportsScenarios[scenario];
  if (reports !== undefined) {
    openShell(state);
    state.language = reports.language;
    state.theme = reports.theme;
    state.screen = 'reports';
    return state;
  }

  const settingsScenario = settingsScenarios[scenario];
  if (settingsScenario !== undefined) {
    openShell(state);
    state.language = settingsScenario.language;
    state.theme = settingsScenario.theme;
    state.screen = 'settings';
    // The reference window shows the top bar's profile menu, which appears
    // once a second profile exists.
    state.profiles = [
      { name: 'Personal', slug: '', open: true },
      { name: 'Business', slug: 'business', open: false },
    ];
    state.profileName = 'Personal';
    return state;
  }

  const documents = documentsScenarios[scenario];
  if (documents !== undefined) {
    openShell(state);
    state.language = documents.language;
    state.theme = documents.theme;
    state.screen = 'documents';
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
    case 'accounts-test-connection-en-dark':
    case 'accounts-test-connection-nogo-en-light':
    case 'accounts-test-connection-nogo-en-dark':
    case 'banking-walkthrough':
      openShell(state);
      state.theme =
        scenario === 'shell-de-dark' ||
        scenario === 'accounts-de-dark' ||
        scenario === 'accounts-bank-menu-de-dark' ||
        scenario === 'accounts-add-online-bank-no-match-de-dark' ||
        scenario === 'accounts-test-connection-en-dark' ||
        scenario === 'accounts-test-connection-nogo-en-dark'
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
  const transactions = createMockTransactions(scenario, state.language);
  const reports = new MockReports(scenario, state.language);
  const settingsScenario = settingsScenarios[scenario];
  const settings = createMockSettings({
    language: state.language,
    section: settingsScenario?.section ?? 0,
    emptyCategories: settingsScenario?.emptyCategories ?? false,
    categoryDialogFood: settingsScenario?.categoryDialogFood ?? false,
    ruleDialogFood: settingsScenario?.ruleDialogFood ?? false,
    ruleDialogPriority: scenario === 'settings-rule-dialog-en' ? '5' : undefined,
    ruleImportPreview: settingsScenario?.ruleImportPreview ?? false,
    profileDelete: settingsScenario?.profileDelete ?? false,
    pendingRestore: settingsScenario?.pendingRestore ?? false,
    restoreDialog: settingsScenario?.restoreDialog ?? false,
    restorePassphrase: settingsScenario?.restorePassphrase ?? false,
    findeskPassphrase: settingsScenario?.findeskPassphrase ?? false,
    findeskReport: settingsScenario?.findeskReport ?? false,
    findeskTargetNotEmpty: settingsScenario?.findeskTargetNotEmpty ?? false,
    findeskNotAFolder: settingsScenario?.findeskNotAFolder ?? false,
    findeskBusy: settingsScenario?.findeskBusy ?? false,
    backupRestored: settingsScenario?.backupRestored ?? false,
  });
  const documents = createMockDocuments(scenario, state.language);
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
    transactionsStateCalls: 0,
    transactionsSearchCalls: [] as string[],
    reportsStateCalls: 0,
    reportsCalls: [] as string[],
    reportsExportCalls: [] as string[],
    settingsStateCalls: 0,
    settingsLoadCalls: 0,
    documentsStateCalls: 0,
    documentsSearchCalls: [] as string[],
    documentsLinkCalls: [] as number[],
    documentsUploadCalls: 0,
    documentsOpenCalls: 0,
    settingsConfirmRestorePassphraseCalls: 0,
    settingsSubmitFindeskPassphraseCalls: 0,
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

  function openTransactionsScreen(): DashboardDTO {
    state.screen = 'transactions';
    // The Back key, not "Back to Dashboard": the sidebar has a Dashboard
    // button and two controls with that name would be ambiguous.
    state.backTitle = translate(state.language, 'Back');
    state.backScreen = 'dashboard';
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
      transactions.setLanguage(state.language);
      reports.setLanguage(state.language);
      documents.setLanguage(state.language);
      const next = publish();
      publishDashboard();
      reports.refresh();
      settings.setLanguage(state.language);
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
      settings.setSection(1);
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
    async dashboardOpenCategory(categoryID: number) {
      transactions.openCategory(categoryID);
      return openTransactionsScreen();
    },
    async dashboardOpenUncategorized() {
      transactions.openUncategorized();
      return openTransactionsScreen();
    },
    async dashboardOpenAll() {
      transactions.openAll();
      return openTransactionsScreen();
    },
    async dashboardOpenPartner(partner: string) {
      transactions.openPartner(partner);
      return openTransactionsScreen();
    },
    async dashboardOpenAccounts() {
      return openScreen('accounts');
    },
    async dashboardOpenSettings() {
      // Rust's dashboard_open_settings opens the categories section.
      settings.setSection(2);
      return openScreen('settings');
    },
    async dashboardOpenTax() {
      reports.openTax(2026);
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
      transactions.selectAccountID(accountID);
      const result = accounts.state();
      state.screen = 'transactions';
      state.backTitle = translate(state.language, 'Back');
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
    bankingSearchBanks(query: string) {
      return Promise.resolve(banking.searchBanks(query));
    },
    bankingPickBankSuggestion(name: string, blz: string, bic: string) {
      return Promise.resolve(banking.pickBankSuggestion(name, blz, bic));
    },
    bankingBankBlzChanged(name: string, blz: string, bic: string) {
      return Promise.resolve(banking.bankBlzChanged(name, blz, bic));
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
    bankingTestConnection(bankID: number) {
      return Promise.resolve(banking.testConnection(bankID));
    },
    bankingSaveConnectionTrace() {
      return Promise.resolve(banking.saveConnectionTrace());
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
    bankingAnswerProductID(id: number, value: string) {
      banking.answerProductID(id, value);
      return Promise.resolve();
    },
    bankingAnswerDecoupled(id: number, confirmed: boolean) {
      banking.answerDecoupled(id, confirmed);
      return Promise.resolve();
    },
    transactionsState() {
      debug.transactionsStateCalls += 1;
      return Promise.resolve(transactions.state());
    },
    transactionsLoad() {
      return Promise.resolve(transactions.load());
    },
    transactionsOpenWithAdd() {
      return Promise.resolve(transactions.openWithAdd());
    },
    transactionsSearch(query: string) {
      debug.transactionsSearchCalls.push(query);
      return Promise.resolve(transactions.search(query));
    },
    transactionsPeriodChanged(index: number) {
      return Promise.resolve(transactions.periodChanged(index));
    },
    transactionsOpenRange() {
      return Promise.resolve(transactions.openRange());
    },
    transactionsApplyRange(from: string, to: string) {
      return Promise.resolve(transactions.applyRange(from, to));
    },
    transactionsCloseRange() {
      return Promise.resolve(transactions.closeRange());
    },
    transactionsCategoryChanged(index: number) {
      return Promise.resolve(transactions.categoryChanged(index));
    },
    transactionsDirectionChanged(index: number) {
      return Promise.resolve(transactions.directionChanged(index));
    },
    transactionsAccountChanged(index: number) {
      return Promise.resolve(transactions.accountChanged(index));
    },
    transactionsOpenAmount() {
      return Promise.resolve(transactions.openAmount());
    },
    transactionsApplyAmount(minimum: string, maximum: string) {
      return Promise.resolve(transactions.applyAmount(minimum, maximum));
    },
    transactionsResetAmount() {
      return Promise.resolve(transactions.resetAmount());
    },
    transactionsCloseAmount() {
      return Promise.resolve(transactions.closeAmount());
    },
    transactionsToggleChart() {
      return Promise.resolve(transactions.toggleChart());
    },
    transactionsAutoCategorize() {
      return Promise.resolve(transactions.autoCategorize());
    },
    transactionsReapplyRules() {
      return Promise.resolve(transactions.reapplyRules());
    },
    transactionsToggleReapplyGroup(from: number, to: number) {
      return Promise.resolve(transactions.toggleReapplyGroup(from, to));
    },
    transactionsConfirmReapplyRules() {
      return Promise.resolve(transactions.confirmReapplyRules());
    },
    transactionsCancelReapplyRules() {
      return Promise.resolve(transactions.cancelReapplyRules());
    },
    transactionsUndoReapply() {
      return Promise.resolve(transactions.undoReapply());
    },
    transactionsOpenImport() {
      return Promise.resolve(transactions.openImport());
    },
    transactionsOpenWithImport() {
      return Promise.resolve(transactions.openImport());
    },
    transactionsImportAccountChanged(index: number) {
      return Promise.resolve(transactions.importAccountChanged(index));
    },
    transactionsImportCSVChanged(field: string, value: string) {
      return Promise.resolve(transactions.importCsvChanged(field, value));
    },
    transactionsConfirmImport(updateBalance: boolean) {
      return Promise.resolve(transactions.confirmImport(updateBalance));
    },
    transactionsCancelImport() {
      return Promise.resolve(transactions.cancelImport());
    },
    transactionsExportCSV() {
      return Promise.resolve(transactions.exportCsv());
    },
    transactionsAttachReceipt() {
      return Promise.resolve(transactions.attachReceipt());
    },
    transactionsOpenRule() {
      return Promise.resolve(transactions.openRule());
    },
    transactionsCloseRule() {
      return Promise.resolve(transactions.closeRule());
    },
    transactionsSaveRule(pattern: string, categoryIndex: number) {
      return Promise.resolve(transactions.saveRule(pattern, categoryIndex));
    },
    transactionsBack() {
      return Promise.resolve(transactions.back());
    },
    transactionsOpenDetail(id: number) {
      return Promise.resolve(transactions.openDetail(id));
    },
    transactionsToggleSelect(id: number) {
      return Promise.resolve(transactions.toggleSelect(id));
    },
    transactionsSelectRange(id: number) {
      return Promise.resolve(transactions.selectRange(id));
    },
    transactionsSelectAll() {
      return Promise.resolve(transactions.selectAll());
    },
    transactionsClearSelection() {
      return Promise.resolve(transactions.clearSelection());
    },
    transactionsShowPartner(partner: string) {
      return Promise.resolve(transactions.showPartner(partner));
    },
    transactionsOpenDelete(id: number) {
      return Promise.resolve(transactions.openDelete(id));
    },
    transactionsConfirmDelete(id: number) {
      return Promise.resolve(transactions.confirmDelete(id));
    },
    transactionsCancelDelete() {
      return Promise.resolve(transactions.cancelDelete());
    },
    transactionsOpenMultiDelete() {
      return Promise.resolve(transactions.openMultiDelete());
    },
    transactionsConfirmMultiDelete() {
      return Promise.resolve(transactions.confirmMultiDelete());
    },
    transactionsCancelMultiDelete() {
      return Promise.resolve(transactions.cancelMultiDelete());
    },
    transactionsBatchCategory(index: number) {
      return Promise.resolve(transactions.batchCategory(index));
    },
    transactionsBatchTax(index: number) {
      return Promise.resolve(transactions.batchTax(index));
    },
    transactionsOpenAdd() {
      return Promise.resolve(transactions.openAdd());
    },
    transactionsAddSubmit(
      accountIndex: number,
      expense: boolean,
      date: string,
      amount: string,
      partner: string,
      purpose: string,
      categoryIndex: number,
    ) {
      return Promise.resolve(
        transactions.addSubmit(
          accountIndex,
          expense,
          date,
          amount,
          partner,
          purpose,
          categoryIndex,
        ),
      );
    },
    transactionsCloseAdd() {
      return Promise.resolve(transactions.closeAdd());
    },
    transactionsConfirmDuplicate() {
      return Promise.resolve(transactions.confirmDuplicate());
    },
    transactionsDismissDuplicate() {
      return Promise.resolve(transactions.dismissDuplicate());
    },
    transactionsCloseDetail() {
      return Promise.resolve(transactions.closeDetail());
    },
    transactionsDetailClose() {
      return Promise.resolve(transactions.closeDetail());
    },
    transactionsDetailCreateRule() {
      return Promise.resolve(transactions.detailCreateRule());
    },
    transactionsDetailCategory(index: number) {
      return Promise.resolve(transactions.detailCategory(index));
    },
    transactionsDetailTax(index: number) {
      return Promise.resolve(transactions.detailTax(index));
    },
    transactionsDetailShowPartner(partner: string) {
      return Promise.resolve(transactions.detailShowPartner(partner));
    },
    transactionsDismissMessage() {
      return Promise.resolve(transactions.dismissMessage());
    },
    transactionsPanelSelect(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(transactions.panelSelect(kind, groupKind, id, accountType));
    },
    transactionsPanelToggle(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(transactions.panelToggle(kind, groupKind, id, accountType));
    },
    transactionsPanelMarkRead(kind: string, groupKind: string, id: number, accountType: string) {
      return Promise.resolve(transactions.panelMarkRead(kind, groupKind, id, accountType));
    },
    transactionsPanelNewGroup(name: string) {
      return Promise.resolve(transactions.panelNewGroup(name));
    },
    transactionsPanelRenameGroup(id: number, name: string) {
      return Promise.resolve(transactions.panelRenameGroup(id, name));
    },
    transactionsPanelDeleteGroup(id: number) {
      return Promise.resolve(transactions.panelDeleteGroup(id));
    },
    transactionsPanelMoveGroup(id: number, up: boolean) {
      return Promise.resolve(transactions.panelMoveGroup(id, up));
    },
    transactionsPanelMoveAccount(accountID: number, groupID: number) {
      return Promise.resolve(transactions.panelMoveAccount(accountID, groupID));
    },
    transactionsPanelFilterChanged(text: string) {
      return Promise.resolve(transactions.panelFilterChanged(text));
    },

    reportsState() {
      debug.reportsStateCalls += 1;
      return Promise.resolve(reports.state());
    },
    reportsChangeYear(delta: number) {
      debug.reportsCalls.push(`changeYear:${delta}`);
      return Promise.resolve(reports.changeYear(delta));
    },
    reportsSetCustom(custom: boolean) {
      debug.reportsCalls.push(`setCustom:${String(custom)}`);
      return Promise.resolve(reports.setCustom(custom));
    },
    reportsApplyRange(from: string, to: string) {
      debug.reportsCalls.push(`applyRange:${from}:${to}`);
      return Promise.resolve(reports.applyRange(from, to));
    },
    reportsSetMissingOnly(only: boolean) {
      debug.reportsCalls.push(`setMissingOnly:${String(only)}`);
      return Promise.resolve(reports.setMissingOnly(only));
    },
    reportsDismiss() {
      debug.reportsCalls.push('dismiss');
      return Promise.resolve(reports.dismiss());
    },
    reportsAcceptSuggestion(id: number) {
      debug.reportsCalls.push(`acceptSuggestion:${id}`);
      return Promise.resolve(reports.acceptSuggestion(id));
    },
    reportsDismissSuggestion(id: number) {
      debug.reportsCalls.push(`dismissSuggestion:${id}`);
      return Promise.resolve(reports.dismissSuggestion(id));
    },
    reportsMarkNotTaxRelevant(id: number) {
      debug.reportsCalls.push(`markNotTaxRelevant:${id}`);
      return Promise.resolve(reports.markNotTaxRelevant(id));
    },
    reportsResetTaxAutomatic(id: number) {
      debug.reportsCalls.push(`resetTaxAutomatic:${id}`);
      return Promise.resolve(reports.resetTaxAutomatic(id));
    },
    reportsExportReportPDF() {
      debug.reportsExportCalls.push('reportPDF');
      return Promise.resolve(reports.exportReportPDF());
    },
    reportsExportCSV() {
      debug.reportsExportCalls.push('csv');
      return Promise.resolve(reports.exportCSV());
    },
    reportsExportTaxXLSX() {
      debug.reportsExportCalls.push('taxXLSX');
      return Promise.resolve(reports.exportTaxXLSX());
    },
    reportsExportTaxPDF() {
      debug.reportsExportCalls.push('taxPDF');
      return Promise.resolve(reports.exportTaxPDF());
    },
    reportsPrintTax() {
      debug.reportsExportCalls.push('print');
      return Promise.resolve(reports.printTax());
    },
    reportsOpenCategory(categoryID: number) {
      debug.reportsCalls.push(`openCategory:${categoryID}`);
      transactions.openCategory(categoryID);
      state.screen = 'transactions';
      state.backTitle = translate(state.language, 'Back');
      state.backScreen = 'reports';
      publish();
      return Promise.resolve(reports.state());
    },
    settingsState() {
      debug.settingsStateCalls += 1;
      return Promise.resolve(settings.state());
    },
    settingsLoad() {
      debug.settingsLoadCalls += 1;
      return Promise.resolve(settings.load());
    },
    settingsDismissMessage() {
      return Promise.resolve(settings.dismissMessage());
    },
    settingsSetPrimaryCurrency(code: string) {
      return Promise.resolve(settings.setPrimaryCurrency(code));
    },
    settingsAddDefaultCategories() {
      return Promise.resolve(settings.addDefaultCategories());
    },
    settingsFixDuplicateColours() {
      return Promise.resolve(settings.fixDuplicateColours());
    },
    settingsOpenAddCategory() {
      return Promise.resolve(settings.openAddCategory());
    },
    settingsOpenEditCategory(id: number) {
      return Promise.resolve(settings.openEditCategory(id));
    },
    settingsOpenDeleteCategory(id: number) {
      return Promise.resolve(settings.openDeleteCategory(id));
    },
    settingsCategoryChanged(name: string, colour: string) {
      return Promise.resolve(settings.categoryChanged(name, colour));
    },
    settingsSaveCategory(
      name: string,
      colour: string,
      budget: string,
      taxRelevant: boolean,
      transfer: boolean,
    ) {
      return Promise.resolve(settings.saveCategory(name, colour, budget, taxRelevant, transfer));
    },
    settingsCloseCategoryDialog() {
      return Promise.resolve(settings.closeCategoryDialog());
    },
    settingsConfirmDeleteCategory() {
      return Promise.resolve(settings.confirmDeleteCategory());
    },
    settingsCancelDeleteCategory() {
      return Promise.resolve(settings.cancelDeleteCategory());
    },
    settingsOpenAddRule() {
      return Promise.resolve(settings.openAddRule());
    },
    settingsOpenEditRule(id: number) {
      return Promise.resolve(settings.openEditRule(id));
    },
    settingsOpenDeleteRule(id: number) {
      return Promise.resolve(settings.openDeleteRule(id));
    },
    settingsRuleChanged(pattern: string, priority: string) {
      return Promise.resolve(settings.ruleChanged(pattern, priority));
    },
    settingsSaveRule(
      pattern: string,
      categoryIndex: number,
      priority: string,
      autoApply: boolean,
      fieldIndex: number,
      flow: number,
      taxIndex: number,
    ) {
      return Promise.resolve(
        settings.saveRule(pattern, categoryIndex, priority, autoApply, fieldIndex, flow, taxIndex),
      );
    },
    settingsCloseRuleDialog() {
      return Promise.resolve(settings.closeRuleDialog());
    },
    settingsConfirmDeleteRule() {
      return Promise.resolve(settings.confirmDeleteRule());
    },
    settingsCancelDeleteRule() {
      return Promise.resolve(settings.cancelDeleteRule());
    },
    settingsSaveTaxKeywords(text: string) {
      return Promise.resolve(settings.saveTaxKeywords(text));
    },
    settingsResetTaxKeywords() {
      return Promise.resolve(settings.resetTaxKeywords());
    },
    settingsExportRules() {
      return Promise.resolve(settings.exportRules());
    },
    settingsImportRules() {
      return Promise.resolve(settings.importRules());
    },
    settingsConfirmRuleImport(categorize: boolean) {
      return Promise.resolve(settings.confirmRuleImport(categorize));
    },
    settingsCancelRuleImport() {
      return Promise.resolve(settings.cancelRuleImport());
    },
    documentsState() {
      debug.documentsStateCalls += 1;
      return Promise.resolve(documents.state());
    },
    documentsSetTab(tab: string) {
      return Promise.resolve(documents.setTab(tab));
    },
    documentsSearch(query: string) {
      debug.documentsSearchCalls.push(query);
      return Promise.resolve(documents.search(query));
    },
    documentsSelect(id: number) {
      return Promise.resolve(documents.select(id));
    },
    documentsCloseDetail() {
      return Promise.resolve(documents.closeDetail());
    },
    documentsCandidateQueryChanged(query: string) {
      return Promise.resolve(documents.candidateQueryChanged(query));
    },
    documentsLink(transactionID: number) {
      debug.documentsLinkCalls.push(transactionID);
      return Promise.resolve(documents.link(transactionID));
    },
    documentsUnlink() {
      return Promise.resolve(documents.unlink());
    },
    documentsReprocess() {
      return Promise.resolve(documents.reprocess());
    },
    documentsConfirmDelete() {
      return Promise.resolve(documents.confirmDelete());
    },
    documentsDismiss() {
      return Promise.resolve(documents.dismiss());
    },
    documentsUpload() {
      debug.documentsUploadCalls += 1;
      return Promise.resolve(documents.upload());
    },
    documentsOpen() {
      debug.documentsOpenCalls += 1;
      return Promise.resolve(documents.open());
    },
    settingsCreateBackup() {
      return Promise.resolve(settings.createBackup());
    },
    settingsRestoreBackup() {
      return Promise.resolve(settings.restoreBackup());
    },
    settingsConfirmRestore() {
      return Promise.resolve(settings.confirmRestore());
    },
    settingsCancelRestore() {
      return Promise.resolve(settings.cancelRestore());
    },
    settingsConfirmRestorePassphrase(passphrase: string) {
      // Only the call count is recorded, never the passphrase.
      debug.settingsConfirmRestorePassphraseCalls += 1;
      return Promise.resolve(settings.confirmRestorePassphrase(passphrase));
    },
    settingsCancelRestorePassphrase() {
      return Promise.resolve(settings.cancelRestorePassphrase());
    },
    settingsCancelPendingRestore() {
      return Promise.resolve(settings.cancelPendingRestore());
    },
    settingsImportFindesk() {
      return Promise.resolve(settings.importFindesk());
    },
    settingsSubmitFindeskPassphrase(passphrase: string) {
      // Only the call count is recorded, never the passphrase.
      debug.settingsSubmitFindeskPassphraseCalls += 1;
      return Promise.resolve(settings.submitFindeskPassphrase(passphrase));
    },
    settingsCancelFindeskPassphrase() {
      return Promise.resolve(settings.cancelFindeskPassphrase());
    },
    settingsCloseFindeskReport() {
      return Promise.resolve(settings.closeFindeskReport());
    },
    settingsNewProfile() {
      return Promise.resolve(settings.newProfile());
    },
    settingsRenameProfile(index: number) {
      return Promise.resolve(settings.renameProfile(index));
    },
    settingsCloseProfileDialog() {
      return Promise.resolve(settings.closeProfileDialog());
    },
    settingsSaveProfile(name: string) {
      return Promise.resolve(settings.saveProfile(name));
    },
    settingsDeleteProfile(index: number) {
      return Promise.resolve(settings.deleteProfile(index));
    },
    settingsCancelDeleteProfile() {
      return Promise.resolve(settings.cancelDeleteProfile());
    },
    settingsConfirmDeleteProfile() {
      return Promise.resolve(settings.confirmDeleteProfile());
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
    onTransactionsChanged(callback: (next: TransactionsDTO) => void) {
      return transactions.subscribe(callback);
    },
    onReportsChanged(callback: (next: ReportsDTO) => void) {
      return reports.subscribe(callback);
    },
    onSettingsChanged(callback: (next: SettingsDTO) => void) {
      return settings.subscribe(callback);
    },
    onDocumentsChanged(callback: (next: DocumentsDTO) => void) {
      return documents.subscribe(callback);
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
