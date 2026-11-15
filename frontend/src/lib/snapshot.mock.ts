import type {
  AccountsDTO,
  Api,
  AppInfoDTO,
  BankingDTO,
  DashboardDTO,
  DocumentsDTO,
  PromptDTO,
  ReportsDTO,
  SettingsDTO,
  ShellDTO,
  TransactionsDTO,
} from './api';
import { languageFromCode, translate, type Language } from './i18n';

type SnapshotPage =
  'dashboard' | 'accounts' | 'transactions' | 'documents' | 'reports' | 'settings';

type PageDTO = {
  dashboard: DashboardDTO;
  accounts: AccountsDTO;
  transactions: TransactionsDTO;
  documents: DocumentsDTO;
  reports: ReportsDTO;
  settings: SettingsDTO;
};

type SnapshotLanguage = { shell: ShellDTO } & PageDTO;

type SnapshotFile = {
  version: number;
  languages: Partial<Record<Language, SnapshotLanguage>>;
};

const pages: SnapshotPage[] = [
  'dashboard',
  'accounts',
  'transactions',
  'documents',
  'reports',
  'settings',
];

const titleKeys: Record<SnapshotPage, string> = {
  dashboard: 'NavDashboard',
  accounts: 'NavAccounts',
  transactions: 'NavTransactions',
  documents: 'NavDocuments',
  reports: 'NavReports',
  settings: 'NavSettings',
};

const info: AppInfoDTO = {
  name: 'Finglass',
  version: '0.4.0-dev',
  goVersion: 'go1.27.4',
  os: 'darwin',
  arch: 'arm64',
};

/**
 * The snapshot scenario accepts same-origin relative paths only. A scheme,
 * a protocol-relative path, a parent segment or a backslash would let the
 * page read something other than the runner's served snapshot.
 */
export function isValidSnapshotPath(value: string): boolean {
  if (value === '') {
    return false;
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(value) || value.startsWith('//')) {
    return false;
  }
  let decoded: string;
  try {
    decoded = decodeURIComponent(value);
  } catch {
    return false;
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(decoded) || decoded.startsWith('//')) {
    return false;
  }
  if (decoded.includes('\\') || decoded.split('/').some((segment) => segment === '..')) {
    return false;
  }
  return true;
}

function isSnapshotPage(value: string): value is SnapshotPage {
  return (pages as readonly string[]).includes(value);
}

function emptyBanking(): BankingDTO {
  return {
    errorText: '',
    messageText: '',
    busy: false,
    syncing: false,
    syncBankId: 0,
    syncStep: '',
    syncAllText: '',
    syncAllVisible: false,
    syncAllEnabled: false,
    syncControlsVisible: false,
    onlineSyncAvailable: false,
    canRememberPin: false,
    onlineSyncNote: '',
    banks: [],
    connectionTest: null,
    lastTrace: null,
    infoText: '',
  };
}

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

/**
 * A read-only API over one exported snapshot. Page loads and state calls are
 * served from the JSON; actions that would change data return the unchanged
 * state, so the screenshot run can only look.
 */
export function createSnapshotApi(path: string): Api {
  let loaded: SnapshotFile | undefined;
  let pending: Promise<SnapshotFile> | undefined;
  let language: Language = 'en';
  let theme = 'light';
  let screen: SnapshotPage = 'dashboard';
  let sidebarCollapsed: boolean | undefined;
  const shellListeners = new Set<(dto: ShellDTO) => void>();
  const pageListeners: Record<SnapshotPage, Set<(dto: unknown) => void>> = {
    dashboard: new Set(),
    accounts: new Set(),
    transactions: new Set(),
    documents: new Set(),
    reports: new Set(),
    settings: new Set(),
  };

  function ensure(): Promise<SnapshotFile> {
    pending ??= (async () => {
      if (!isValidSnapshotPath(path)) {
        throw new Error('invalid snapshot path');
      }
      const response = await fetch(path, { cache: 'no-store' });
      if (!response.ok) {
        throw new Error(`snapshot request failed with ${response.status}`);
      }
      const parsed = (await response.json()) as Partial<SnapshotFile> | null;
      if (
        parsed === null ||
        typeof parsed !== 'object' ||
        parsed.languages === undefined ||
        typeof parsed.languages !== 'object'
      ) {
        throw new Error('snapshot has no languages');
      }
      loaded = parsed as SnapshotFile;
      return loaded;
    })();
    return pending;
  }

  function data(): SnapshotFile {
    if (loaded === undefined) {
      throw new Error('snapshot is not loaded');
    }
    return loaded;
  }

  function languageData(): SnapshotLanguage {
    const available = data().languages;
    const selected = available[language] ?? available.en ?? Object.values(available)[0];
    if (selected === undefined) {
      throw new Error('snapshot has no language data');
    }
    return selected;
  }

  function pageData<K extends SnapshotPage>(page: K): PageDTO[K] {
    return languageData()[page];
  }

  function shell(): ShellDTO {
    const base = languageData().shell;
    return {
      ...base,
      mode: 'unlock',
      allowChange: true,
      busy: false,
      restored: false,
      startupError: false,
      errorText: '',
      shellOpen: true,
      language,
      theme,
      sidebarCollapsed: sidebarCollapsed ?? base.sidebarCollapsed,
      screen,
      screenTitle: translate(language, titleKeys[screen]),
      backTitle: '',
      findEnabled: screen === 'transactions' || screen === 'documents',
      profiles: base.profiles.map((profile) => ({ ...profile })),
      leftoverLines: [...base.leftoverLines],
    };
  }

  function publishShell(): ShellDTO {
    const dto = shell();
    for (const listener of shellListeners) {
      listener(dto);
    }
    return dto;
  }

  function publishPage(page: SnapshotPage): void {
    const value = languageData()[page];
    if (value === undefined) {
      return;
    }
    for (const listener of pageListeners[page]) {
      listener(value);
    }
  }

  function pageForMethod(name: string): SnapshotPage {
    for (const page of pages) {
      if (name.startsWith(page)) {
        return page;
      }
    }
    return screen;
  }

  function responseFor(name: string): unknown {
    if (name === 'bankingPrompt') {
      return emptyPrompt();
    }
    if (name.startsWith('banking')) {
      return emptyBanking();
    }
    return pageData(pageForMethod(name));
  }

  const handlers: Record<string, (...args: unknown[]) => unknown> = {
    info: () => ({ ...info }),
    greet: (name) => {
      const trimmed = String(name).trim();
      if (trimmed === '') {
        throw new Error('name must not be empty');
      }
      return `Hello, ${trimmed}`;
    },
    onTick: () => () => {},
    state: async () => {
      await ensure();
      return shell();
    },
    unlock: async () => {
      await ensure();
      return publishShell();
    },
    create: async () => {
      await ensure();
      return publishShell();
    },
    change: async () => {
      await ensure();
      return publishShell();
    },
    showChange: async () => {
      await ensure();
      return publishShell();
    },
    navigate: async (value) => {
      await ensure();
      const next = String(value);
      if (isSnapshotPage(next)) {
        screen = next;
      }
      return publishShell();
    },
    navigateMenuItem: async (index) => {
      await ensure();
      const next = pages[Number(index)];
      if (next !== undefined) {
        screen = next;
      }
      return publishShell();
    },
    back: async () => {
      await ensure();
      return publishShell();
    },
    setLanguage: async (code) => {
      await ensure();
      language = languageFromCode(String(code));
      const dto = publishShell();
      for (const page of pages) {
        publishPage(page);
      }
      return dto;
    },
    setTheme: async (code) => {
      await ensure();
      const next = String(code);
      if (next === 'light' || next === 'dark' || next === 'system') {
        theme = next;
      }
      return publishShell();
    },
    setSidebarCollapsed: async (collapsed) => {
      await ensure();
      sidebarCollapsed = Boolean(collapsed);
      return publishShell();
    },
    switchProfile: async () => {
      await ensure();
      return publishShell();
    },
    manageProfiles: async () => {
      await ensure();
      screen = 'settings';
      return publishShell();
    },
    migrationMigrate: async () => {
      await ensure();
      return publishShell();
    },
    migrationStartEmpty: async () => {
      await ensure();
      return publishShell();
    },
    migrationSubmit: async () => {
      await ensure();
      return publishShell();
    },
    migrationSkip: async () => {
      await ensure();
      return publishShell();
    },
    migrationContinue: async () => {
      await ensure();
      return publishShell();
    },
    migrationPassphraseChanged: async () => {
      await ensure();
      return publishShell();
    },
    openGitHub: async () => {
      await ensure();
      return publishShell();
    },
    reportProblem: async () => {
      await ensure();
      return publishShell();
    },
    onShellChanged: (callback) => {
      const listener = callback as (dto: ShellDTO) => void;
      shellListeners.add(listener);
      return () => shellListeners.delete(listener);
    },
    onAccountsChanged: (callback) => subscribe('accounts', callback),
    onBankingChanged: () => () => {},
    onBankingPrompt: () => () => {},
    onMenuAction: () => () => {},
    onDashboardChanged: (callback) => subscribe('dashboard', callback),
    onTransactionsChanged: (callback) => subscribe('transactions', callback),
    onReportsChanged: (callback) => subscribe('reports', callback),
    onSettingsChanged: (callback) => subscribe('settings', callback),
    onDocumentsChanged: (callback) => subscribe('documents', callback),
  };

  function subscribe(page: SnapshotPage, callback: unknown): () => void {
    const listener = callback as (dto: unknown) => void;
    pageListeners[page].add(listener);
    return () => pageListeners[page].delete(listener);
  }

  const api = new Proxy({} as Api, {
    has(_target, property: string | symbol) {
      // Svelte's $state proxy only forwards properties that exist on the
      // wrapped object, so the method surface must report itself here.
      return typeof property === 'string' || Reflect.has({}, property);
    },
    get(_target, property: string | symbol) {
      if (typeof property !== 'string') {
        return undefined;
      }
      // Do not make the API object itself look like a thenable to await.
      if (property === 'then') {
        return undefined;
      }
      const handler = handlers[property];
      if (handler !== undefined) {
        return handler;
      }
      return async () => {
        await ensure();
        return responseFor(property);
      };
    },
  });
  return api;
}

export type { SnapshotFile, SnapshotLanguage };
