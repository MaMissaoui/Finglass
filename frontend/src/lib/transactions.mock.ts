import type {
  AccountAmountDTO,
  AccountPanelRowDTO,
  ImportCSVFieldDTO,
  ImportFileRowDTO,
  ReapplyGroupRowDTO,
  TransactionRowDTO,
  TransactionsDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
import { translate, type Language } from './i18n';
import {
  chartGeometry,
  computeRunning,
  historyFromRunning,
  type BalanceBooking,
  type RunningEntry,
} from './transactionsBalance';

type AccountType = 'checking' | 'savings' | 'credit_card' | 'loan' | 'depot';
type Direction = 'all' | 'income' | 'expenses';
type Period = 'this-month' | 'last-month' | 'last-3' | 'this-year' | 'last-year' | 'all';
type Tone = 'positive' | 'negative' | 'loan' | 'zero' | 'plain';
type CategoryFilter = { kind: 'any' } | { kind: 'none' } | { kind: 'only'; id: number };
type Selection =
  | { kind: 'all' }
  | { kind: 'group'; groupKind: 'type' | 'custom'; id: number; accountType: string }
  | { kind: 'account'; id: number };

type MockBadge = { logo: string; letters: string; colour: number };

type MockAccount = {
  id: number;
  name: string;
  iban: string;
  bankName: string;
  type: AccountType;
  currency: string;
  minor: number;
  lastFetched: string;
  newCount: number;
  groupId: number | null;
  badge: MockBadge;
};

type MockCategory = {
  id: number;
  name: string;
  colour: string;
  taxRelevant: boolean;
};

type MockTransaction = {
  id: number;
  date: string;
  partner: string;
  purpose: string;
  categoryId: number | null;
  minor: number;
  currency: string;
  accountId: number;
  isNew: boolean;
  documents: number;
  taxOverride: boolean | null;
  taxSource: '' | 'manual';
  taxRulePattern: string;
  balanceAfter: number | null;
};

type MockPending = {
  date: string;
  partner: string;
  purpose: string;
  minor: number;
  currency: string;
  accountId: number;
  fetchedAt: string;
};

type MockGroup = { id: number; name: string };

type MockRange = { from: string; to: string };

type MockDuplicate = { date: string; minor: number; currency: string };

type MockImportWarning = { key: string; count: number };

type MockImportEntry = {
  file: string;
  count: number;
  from: string;
  to: string;
  warnings: MockImportWarning[];
};

type MockImportFailure = { file: string; messageKey: string; messageArg: string };

type MockCsvMapping = {
  date: number;
  amount: number;
  debit: number;
  credit: number;
  partner: number;
  purpose: number;
  balance: number;
};

type MockImportKind = 'csv' | 'mt940' | 'warnings';

type MockImportPreview = {
  kind: MockImportKind;
  files: string[];
  entries: MockImportEntry[];
  failures: MockImportFailure[];
  csvHeaders: string[];
  csvMapping: MockCsvMapping;
  accountId: number | null;
  updateBalance: boolean;
  balanceUpdateAllowed: boolean;
  hasBalances: boolean;
  bookingCount: number;
};

type MockReapplyGroup = {
  fromId: number;
  toId: number;
  fromName: string;
  toName: string;
  count: number;
  sum: number | null;
  currency: string;
  ticked: boolean;
};

const accountTypes: AccountType[] = ['checking', 'savings', 'credit_card', 'loan', 'depot'];
const defaultCurrency = 'EUR';
const today = '2026-10-15';
const pendingFetchedAt = '2026-11-12T18:47:00Z';
const uncategorizedColour = '#9e9e9e';
const uncategorizedLabelKey = 'Uncategorized';

const checkingBadge: MockBadge = { logo: '', letters: 'S', colour: 0 };
const savingsBadge: MockBadge = { logo: '', letters: 'S', colour: 0 };
const dollarBadge: MockBadge = { logo: '', letters: 'N', colour: 3 };

function localeOf(language: Language): string {
  switch (language) {
    case 'de':
      return 'de-DE';
    case 'fr':
      return 'fr-FR';
    default:
      return 'en-US';
  }
}

function currencySymbol(currency: string, language: Language): string {
  const parts = new Intl.NumberFormat(localeOf(language), {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
  }).formatToParts(0);
  return parts.find((part) => part.type === 'currency')?.value ?? currency;
}

/** Signed money as the Go formatter writes it; money is never parsed back. */
export function mockMoney(
  minor: number,
  currency: string,
  language: Language,
  signed = false,
): string {
  const formatted = new Intl.NumberFormat(localeOf(language), {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(Math.abs(minor) / 100);
  if (minor < 0) {
    return `-${formatted}`;
  }
  if (signed && minor > 0) {
    return `+${formatted}`;
  }
  return formatted;
}

function mockWholeMoney(minor: number, currency: string, language: Language): string {
  const formatted = new Intl.NumberFormat(localeOf(language), {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(Math.abs(minor) / 100);
  return minor < 0 ? `-${formatted}` : formatted;
}

/** `dd.mm.yyyy` from an ISO date. */
export function mockDate(iso: string): string {
  if (iso === '') {
    return '';
  }
  const [year, month, day] = iso.split('-');
  if (year === undefined || month === undefined || day === undefined) {
    return iso;
  }
  return `${day}.${month}.${year}`;
}

function mockDateTime(iso: string): string {
  const [datePart, timePart = ''] = iso.split('T');
  const clock = timePart.slice(0, 5);
  const date = mockDate(datePart ?? '');
  return clock === '' ? date : `${date} ${clock}`;
}

function isoDate(year: number, month: number, day: number): string {
  return `${String(year).padStart(4, '0')}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
}

function monthRange(year: number, month: number): MockRange {
  const last = new Date(Date.UTC(year, month, 0)).getUTCDate();
  return { from: isoDate(year, month, 1), to: isoDate(year, month, last) };
}

function typeKey(type: AccountType): string {
  switch (type) {
    case 'savings':
      return 'TypeSavings';
    case 'credit_card':
      return 'TypeCreditCard';
    case 'loan':
      return 'TypeLoan';
    case 'depot':
      return 'TypeDepot';
    default:
      return 'TypeChecking';
  }
}

function lastFour(account: MockAccount): string {
  const characters = Array.from(account.iban.replace(/\s/gu, ''));
  return characters.slice(Math.max(0, characters.length - 4)).join('');
}

function balanceToneFor(accounts: MockAccount[], currency: string): Tone {
  let total = 0;
  let withoutLoans = 0;
  for (const account of accounts) {
    if (account.currency !== currency) {
      continue;
    }
    total += account.minor;
    if (account.type !== 'loan') {
      withoutLoans += account.minor;
    }
  }
  if (total > 0) {
    return 'positive';
  }
  if (total === 0) {
    return 'zero';
  }
  return withoutLoans < 0 ? 'negative' : 'loan';
}

function currencyTotals(accounts: MockAccount[], language: Language): AccountAmountDTO[] {
  const totals = new Map<string, number>();
  for (const account of accounts) {
    totals.set(account.currency, (totals.get(account.currency) ?? 0) + account.minor);
  }
  const ordered = [...totals.entries()].sort((left, right) => {
    if (left[0] === defaultCurrency) {
      return -1;
    }
    if (right[0] === defaultCurrency) {
      return 1;
    }
    return left[0].localeCompare(right[0]);
  });
  const plain = ordered.length > 1;
  return ordered.map(([currency, minor]) => ({
    text: mockMoney(minor, currency, language),
    tone: plain ? 'plain' : balanceToneFor(accounts, currency),
  }));
}

function sameBadge(left: MockBadge, right: MockBadge): boolean {
  return left.logo === right.logo && left.letters === right.letters && left.colour === right.colour;
}

function groupBadge(accounts: MockAccount[]): MockBadge {
  const first = accounts[0];
  if (first === undefined) {
    return { logo: '', letters: '', colour: 0 };
  }
  for (const account of accounts.slice(1)) {
    if (!sameBadge(first.badge, account.badge)) {
      return { logo: '', letters: '', colour: 0 };
    }
  }
  return first.badge;
}

function accountLabel(account: MockAccount): string {
  return account.name;
}

function parseDay(text: string): string | null {
  const match = /^(\d{2})\.(\d{2})\.(\d{4})$/u.exec(text.trim());
  if (match === null) {
    return null;
  }
  const day = Number.parseInt(match[1] ?? '', 10);
  const month = Number.parseInt(match[2] ?? '', 10);
  const year = Number.parseInt(match[3] ?? '', 10);
  if (month < 1 || month > 12 || day < 1) {
    return null;
  }
  const last = new Date(Date.UTC(year, month, 0)).getUTCDate();
  if (day > last) {
    return null;
  }
  return isoDate(year, month, day);
}

function parseAmount(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === '') {
    return null;
  }
  const match = /^([+-])?(\d+)(?:[.,](\d{1,2}))?$/u.exec(trimmed);
  if (match === null) {
    return null;
  }
  const integer = Number.parseInt(match[2] ?? '', 10);
  const fraction = Number.parseInt((match[3] ?? '').padEnd(2, '0'), 10);
  const minor = integer * 100 + fraction;
  return Math.abs(match[1] === '-' ? -minor : minor);
}

function sameSelection(left: Selection, right: Selection): boolean {
  if (left.kind !== right.kind) {
    return false;
  }
  if (left.kind === 'group' && right.kind === 'group') {
    return (
      left.groupKind === right.groupKind &&
      left.accountType === right.accountType &&
      (left.groupKind === 'custom' ? left.id === right.id : true)
    );
  }
  if (left.kind === 'account' && right.kind === 'account') {
    return left.id === right.id;
  }
  return left.kind === 'all';
}

function sampleAccounts(): MockAccount[] {
  return [
    {
      id: 1,
      name: 'Gemeinschaftskonto Haushalt und Reisen',
      iban: 'DE00 0000 0000 0000 0000 01',
      bankName: 'Beispielbank',
      type: 'checking',
      currency: 'EUR',
      minor: 245_037,
      lastFetched: '2026-11-12',
      newCount: 12,
      groupId: null,
      badge: checkingBadge,
    },
    {
      id: 2,
      name: 'Rücklage',
      iban: 'DE00 0000 0000 0000 0000 02',
      bankName: 'Beispielbank',
      type: 'savings',
      currency: 'EUR',
      minor: 800_000,
      lastFetched: '2026-11-12',
      newCount: 0,
      groupId: 1,
      badge: savingsBadge,
    },
    {
      id: 3,
      name: 'Dollar',
      iban: 'DE00 0000 0000 0000 0000 03',
      bankName: 'Beispielbank',
      type: 'savings',
      currency: 'USD',
      minor: 50_000,
      lastFetched: '',
      newCount: 0,
      groupId: 1,
      badge: dollarBadge,
    },
  ];
}

function sampleCategories(): MockCategory[] {
  return [
    { id: 11, name: 'Food', colour: '#FFC107', taxRelevant: false },
    { id: 12, name: 'Salary', colour: '#4CAF50', taxRelevant: false },
    { id: 13, name: 'Health', colour: '#2196F3', taxRelevant: true },
    { id: 14, name: 'Office', colour: '#FF5722', taxRelevant: true },
  ];
}

type SeedRow = {
  day: number;
  minor: number;
  partner: string;
  categoryId: number | null;
  isNew: boolean;
};

function checkingSeed(): SeedRow[] {
  return [
    { day: 2, minor: -12_000, partner: 'Example Market', categoryId: 11, isNew: true },
    { day: 3, minor: 150_000, partner: 'Employer GmbH', categoryId: 12, isNew: true },
    { day: 5, minor: -1_500, partner: 'Corner Kiosk', categoryId: null, isNew: false },
    { day: 8, minor: -8_900, partner: 'Beispiel-Krankenkasse', categoryId: 13, isNew: false },
    { day: 10, minor: -999, partner: 'Spotify', categoryId: null, isNew: false },
    { day: 12, minor: -3_200, partner: 'Bürobedarf GmbH', categoryId: 14, isNew: true },
    { day: 15, minor: -2_550, partner: 'Apotheke am Ring', categoryId: 13, isNew: false },
    { day: 18, minor: -4_500, partner: 'Example Market', categoryId: 11, isNew: false },
    { day: 22, minor: 25_000, partner: 'Beispielhilfe e. V.', categoryId: null, isNew: false },
    { day: 26, minor: -6_000, partner: 'Bürobedarf GmbH', categoryId: 14, isNew: true },
  ];
}

function sampleTransactions(): MockTransaction[] {
  const rows: MockTransaction[] = [];
  let id = 1;
  for (const month of [8, 9, 10]) {
    for (const seed of checkingSeed()) {
      rows.push({
        id,
        date: isoDate(2026, month, seed.day),
        partner: seed.partner,
        purpose: '',
        categoryId: seed.categoryId,
        minor: seed.minor,
        currency: 'EUR',
        accountId: 1,
        isNew: seed.isNew,
        documents: 0,
        taxOverride: null,
        taxSource: '',
        taxRulePattern: '',
        balanceAfter: month === 8 && seed.day === 10 ? 245_037 : null,
      });
      id += 1;
    }
  }
  for (const month of [8, 9, 10]) {
    rows.push({
      id,
      date: isoDate(2026, month, 14),
      partner: 'Cloud Service',
      purpose: '',
      categoryId: null,
      minor: -1_199,
      currency: 'USD',
      accountId: 3,
      isNew: false,
      documents: 0,
      taxOverride: null,
      taxSource: '',
      taxRulePattern: '',
      balanceAfter: null,
    });
    id += 1;
  }
  rows.push({
    id,
    date: isoDate(2026, 9, 28),
    partner: 'Transfer from Haushalt',
    purpose: '',
    categoryId: null,
    minor: 100_000,
    currency: 'EUR',
    accountId: 2,
    isNew: false,
    documents: 0,
    taxOverride: null,
    taxSource: '',
    taxRulePattern: '',
    balanceAfter: null,
  });
  return rows;
}

function samplePending(): MockPending[] {
  return [
    {
      date: isoDate(2026, 10, 15),
      partner: 'Beispiel AG',
      purpose: 'Gehalt Oktober',
      minor: 12_000,
      currency: 'EUR',
      accountId: 1,
      fetchedAt: pendingFetchedAt,
    },
    {
      date: isoDate(2026, 10, 14),
      partner: 'Bäckerei Muster',
      purpose: 'Brötchen',
      minor: -2_000,
      currency: 'EUR',
      accountId: 1,
      fetchedAt: pendingFetchedAt,
    },
  ];
}

function sampleImportPreview(kind: MockImportKind, accountId: number): MockImportPreview {
  if (kind === 'mt940') {
    return {
      kind,
      files: ['kontoauszug.mt940'],
      entries: [
        {
          file: 'kontoauszug.mt940',
          count: 3,
          from: isoDate(2026, 10, 1),
          to: isoDate(2026, 10, 31),
          warnings: [],
        },
      ],
      failures: [],
      csvHeaders: [],
      csvMapping: { date: 0, amount: 0, debit: 0, credit: 0, partner: 0, purpose: 0, balance: 0 },
      accountId,
      updateBalance: true,
      balanceUpdateAllowed: true,
      hasBalances: true,
      bookingCount: 3,
    };
  }
  if (kind === 'warnings') {
    return {
      kind,
      files: ['alt.csv', 'defekt.csv'],
      entries: [
        {
          file: 'alt.csv',
          count: 2,
          from: isoDate(2026, 9, 1),
          to: isoDate(2026, 9, 30),
          warnings: [
            { key: 'PendingLeftOut', count: 2 },
            { key: 'UnreadableCount', count: 1 },
          ],
        },
      ],
      failures: [{ file: 'defekt.csv', messageKey: 'ErrorFileTooLarge', messageArg: '' }],
      csvHeaders: [],
      csvMapping: { date: 0, amount: 0, debit: 0, credit: 0, partner: 0, purpose: 0, balance: 0 },
      accountId,
      updateBalance: false,
      balanceUpdateAllowed: false,
      hasBalances: true,
      bookingCount: 2,
    };
  }
  return {
    kind: 'csv',
    files: ['oktober.csv', 'november.csv', 'missing.csv'],
    entries: [
      {
        file: 'oktober.csv',
        count: 2,
        from: isoDate(2026, 8, 2),
        to: isoDate(2026, 10, 21),
        warnings: [],
      },
      {
        file: 'november.csv',
        count: 2,
        from: isoDate(2026, 11, 5),
        to: isoDate(2026, 11, 9),
        warnings: [],
      },
    ],
    failures: [{ file: 'missing.csv', messageKey: 'ErrorFileNotFound', messageArg: 'missing.csv' }],
    csvHeaders: ['Buchung', 'Betrag', 'Auftraggeber/Empfänger', 'Verwendungszweck', 'Saldo'],
    csvMapping: { date: 1, amount: 2, debit: 0, credit: 0, partner: 3, purpose: 4, balance: 5 },
    accountId,
    updateBalance: true,
    balanceUpdateAllowed: true,
    hasBalances: true,
    bookingCount: 4,
  };
}

function bookingsOf(transactions: MockTransaction[]): BalanceBooking[] {
  return transactions.map((transaction) => ({
    id: transaction.id,
    date: transaction.date,
    minor: transaction.minor,
    balanceAfter: transaction.balanceAfter,
  }));
}

function directionOf(transaction: MockTransaction): Direction {
  return transaction.minor > 0 ? 'income' : 'expenses';
}

/**
 * The stateful transactions fake behind the mock build's Transactions page.
 * Every name, IBAN, partner and amount is invented.
 */
export function createMockTransactions(scenario: string, initialLanguage: Language) {
  let language = initialLanguage;
  let accounts = sampleAccounts();
  const categories = sampleCategories();
  let transactions = sampleTransactions();
  const pending = samplePending();
  let groups: MockGroup[] = [{ id: 1, name: 'Reisen' }];

  let query = '';
  let period: Period = 'all';
  let customRange: MockRange | null = null;
  let direction: Direction = 'all';
  let categoryFilter: CategoryFilter = { kind: 'any' };
  let selection: Selection = { kind: 'all' };
  let panelFilterText = '';
  let collapsed = new Set<string>();
  let chartCollapsed = false;
  let minAmount: number | null = null;
  let maxAmount: number | null = null;

  let selectedIDs = new Set<number>();
  let anchorID: number | null = null;
  let detailID: number | null = null;
  let canUndoReapply = false;

  let errorText = '';
  let messageText = '';

  let rangeOpen = false;
  let rangeFrom = '';
  let rangeTo = '';
  let rangeError = '';
  let amountOpen = false;
  let amountMin = '';
  let amountMax = '';
  let amountError = '';
  let deleteOpen = false;
  let deleteID = -1;
  let deleteTitle = '';
  let deleteText = '';
  let addOpen = false;
  let addError = '';
  let ruleOpen = false;
  let rulePattern = '';
  let ruleCategoryIndex = 0;
  let ruleError = '';
  let pendingDuplicate: MockDuplicate | null = null;
  let pendingAdd: Omit<MockTransaction, 'id'> | null = null;
  let multiDeleteOpen = false;
  let multiDeleteIDs = new Set<number>();
  let addDate = mockDate(today);

  let importPreview: MockImportPreview | null = null;
  let importKind: MockImportKind = 'csv';

  let reapplyOpen = false;
  let reapplyGroups: MockReapplyGroup[] = [];
  let reapplyShownCount = 0;
  let lastReapply: { id: number; categoryId: number | null }[] = [];

  let nextGroupID = 2;

  const listeners = new Set<(dto: TransactionsDTO) => void>();

  function accountById(id: number): MockAccount | undefined {
    return accounts.find((account) => account.id === id);
  }

  function categoryById(id: number | null): MockCategory | undefined {
    return id === null ? undefined : categories.find((category) => category.id === id);
  }

  function transactionById(id: number): MockTransaction | undefined {
    return transactions.find((transaction) => transaction.id === id);
  }

  function typeAccounts(type: string): MockAccount[] {
    return accounts.filter((account) => account.groupId === null && account.type === type);
  }

  function customAccounts(groupID: number): MockAccount[] {
    return accounts.filter((account) => account.groupId === groupID);
  }

  function groupAccounts(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): MockAccount[] {
    return groupKind === 'custom' ? customAccounts(id) : typeAccounts(accountType);
  }

  function groupKey(groupKind: 'type' | 'custom', id: number, accountType: string): string {
    return groupKind === 'custom' ? `custom:${id}` : `type:${accountType}`;
  }

  function isGroupCollapsed(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): boolean {
    return collapsed.has(groupKey(groupKind, id, accountType));
  }

  function matchesPanelFilter(account: MockAccount): boolean {
    const needle = panelFilterText.trim().toLowerCase();
    if (needle === '') {
      return true;
    }
    return (
      account.name.toLowerCase().includes(needle) ||
      account.iban.toLowerCase().includes(needle) ||
      account.bankName.toLowerCase().includes(needle)
    );
  }

  function selectedAccount(): MockAccount | undefined {
    return selection.kind === 'account' ? accountById(selection.id) : undefined;
  }

  function periodRange(): MockRange | null {
    if (customRange !== null) {
      return customRange;
    }
    switch (period) {
      case 'this-month':
        return monthRange(2026, 10);
      case 'last-month':
        return monthRange(2026, 9);
      case 'last-3':
        return { from: isoDate(2026, 8, 1), to: isoDate(2026, 10, 31) };
      case 'this-year':
        return { from: isoDate(2026, 1, 1), to: isoDate(2026, 12, 31) };
      case 'last-year':
        return { from: isoDate(2025, 1, 1), to: isoDate(2025, 12, 31) };
      default:
        return null;
    }
  }

  function matchesAccount(transaction: MockTransaction): boolean {
    if (selection.kind === 'account') {
      return transaction.accountId === selection.id;
    }
    if (selection.kind === 'group') {
      const members = groupAccounts(selection.groupKind, selection.id, selection.accountType);
      return members.some((account) => account.id === transaction.accountId);
    }
    return true;
  }

  function matchesFilters(transaction: MockTransaction): boolean {
    if (!matchesAccount(transaction)) {
      return false;
    }
    const range = periodRange();
    if (range !== null && (transaction.date < range.from || transaction.date > range.to)) {
      return false;
    }
    if (direction !== 'all' && directionOf(transaction) !== direction) {
      return false;
    }
    if (categoryFilter.kind === 'none' && transaction.categoryId !== null) {
      return false;
    }
    if (categoryFilter.kind === 'only' && transaction.categoryId !== categoryFilter.id) {
      return false;
    }
    const needle = query.trim().toLowerCase();
    if (needle !== '') {
      const haystack = `${transaction.partner} ${transaction.purpose}`.toLowerCase();
      if (!haystack.includes(needle)) {
        return false;
      }
    }
    const absolute = Math.abs(transaction.minor);
    if (minAmount !== null && absolute < minAmount) {
      return false;
    }
    if (maxAmount !== null && absolute > maxAmount) {
      return false;
    }
    return true;
  }

  function visibleTransactions(): MockTransaction[] {
    return transactions
      .filter(matchesFilters)
      .sort((left, right) => right.date.localeCompare(left.date) || right.id - left.id);
  }

  function visiblePending(): MockPending[] {
    const account = selectedAccount();
    if (account === undefined) {
      return [];
    }
    const range = periodRange();
    const includesToday =
      (range === null || range.from <= today) && (range === null || range.to >= today);
    if (
      !includesToday ||
      query.trim() !== '' ||
      direction !== 'all' ||
      minAmount !== null ||
      maxAmount !== null ||
      categoryFilter.kind !== 'any'
    ) {
      return [];
    }
    return pending
      .filter((line) => line.accountId === account.id && line.currency === account.currency)
      .sort((left, right) => right.date.localeCompare(left.date));
  }

  function clearBookingSelection(): void {
    selectedIDs = new Set();
    anchorID = null;
  }

  function markAccountRead(account: MockAccount): void {
    account.newCount = 0;
  }

  function panelAccountRow(account: MockAccount): AccountPanelRowDTO {
    const secondary = `…${lastFour(account)}`;
    const groupMembers =
      account.groupId !== null ? customAccounts(account.groupId) : typeAccounts(account.type);
    return {
      kind: 'account',
      groupKind: '',
      accountType: account.type,
      id: account.id,
      label: accountLabel(account),
      secondary,
      detail:
        account.lastFetched === ''
          ? ''
          : translate(language, 'LastFetched', mockDate(account.lastFetched)),
      name: '',
      deleteText: '',
      selected: selection.kind === 'account' && selection.id === account.id,
      collapsed: false,
      newCount: account.newCount,
      totals: [
        {
          text: mockMoney(account.minor, account.currency, language),
          tone: balanceToneFor(groupMembers, account.currency),
        },
      ],
      indent: true,
      logo: account.badge.logo,
      logoLetters: account.badge.letters,
      logoColour: account.badge.colour,
    };
  }

  function panelAllRow(): AccountPanelRowDTO {
    let allNew = 0;
    for (const account of accounts) {
      allNew += account.newCount;
    }
    return {
      kind: 'all',
      groupKind: '',
      accountType: '',
      id: -1,
      label: translate(language, 'AllAccounts'),
      secondary: '',
      detail: '',
      name: '',
      deleteText: '',
      selected: selection.kind === 'all',
      collapsed: false,
      newCount: allNew,
      totals: currencyTotals(accounts, language),
      indent: false,
      logo: '',
      logoLetters: '',
      logoColour: 0,
    };
  }

  function panelGroupRow(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
    name: string,
  ): AccountPanelRowDTO {
    const members = groupAccounts(groupKind, id, accountType);
    let newCount = 0;
    for (const account of members) {
      newCount += account.newCount;
    }
    const badge = groupBadge(members);
    return {
      kind: 'group',
      groupKind,
      accountType,
      id: groupKind === 'custom' ? id : -1,
      label:
        groupKind === 'custom' ? name : translate(language, typeKey(accountType as AccountType)),
      secondary: '',
      detail: '',
      name: groupKind === 'custom' ? name : '',
      deleteText: groupKind === 'custom' ? translate(language, 'DeleteGroupText', name) : '',
      selected: sameSelection(selection, {
        kind: 'group',
        groupKind,
        id,
        accountType,
      }),
      collapsed: isGroupCollapsed(groupKind, id, accountType),
      newCount,
      totals: currencyTotals(members, language),
      indent: false,
      logo: groupKind === 'custom' ? '' : badge.logo,
      logoLetters: groupKind === 'custom' ? '' : badge.letters,
      logoColour: groupKind === 'custom' ? 0 : badge.colour,
    };
  }

  function filteredMembers(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): MockAccount[] {
    return groupAccounts(groupKind, id, accountType)
      .filter(matchesPanelFilter)
      .sort((left, right) => left.name.localeCompare(right.name));
  }

  function panelRows(): AccountPanelRowDTO[] {
    const rows: AccountPanelRowDTO[] = [panelAllRow()];
    const filtering = panelFilterText.trim() !== '';
    for (const type of accountTypes) {
      const members = typeAccounts(type);
      if (members.length === 0) {
        continue;
      }
      const matching = filteredMembers('type', -1, type);
      if (filtering && matching.length === 0) {
        continue;
      }
      rows.push(panelGroupRow('type', -1, type, ''));
      if (isGroupCollapsed('type', -1, type)) {
        continue;
      }
      for (const account of matching) {
        rows.push(panelAccountRow(account));
      }
    }
    for (const group of groups) {
      const matching = filteredMembers('custom', group.id, '');
      if (filtering && matching.length === 0) {
        continue;
      }
      rows.push(panelGroupRow('custom', group.id, '', group.name));
      if (isGroupCollapsed('custom', group.id, '')) {
        continue;
      }
      for (const account of matching) {
        rows.push(panelAccountRow(account));
      }
    }
    return rows;
  }

  function accountOptions(): { labels: string[]; index: number } {
    const labels = [translate(language, 'AllAccounts')];
    const choices: Selection[] = [{ kind: 'all' }];
    for (const type of accountTypes) {
      const members = typeAccounts(type);
      if (members.length === 0) {
        continue;
      }
      labels.push(translate(language, typeKey(type)));
      choices.push({ kind: 'group', groupKind: 'type', id: -1, accountType: type });
    }
    for (const group of groups) {
      labels.push(group.name);
      choices.push({ kind: 'group', groupKind: 'custom', id: group.id, accountType: '' });
    }
    for (const account of accounts) {
      labels.push(accountLabel(account));
      choices.push({ kind: 'account', id: account.id });
    }
    const index = choices.findIndex((choice) => sameSelection(choice, selection));
    return { labels, index: index < 0 ? 0 : index };
  }

  function periodIndex(): number {
    switch (period) {
      case 'this-month':
        return 0;
      case 'last-month':
        return 1;
      case 'last-3':
        return 2;
      case 'this-year':
        return 3;
      case 'last-year':
        return 4;
      default:
        return 5;
    }
  }

  function categoryIndex(): number {
    if (categoryFilter.kind === 'none') {
      return 1;
    }
    if (categoryFilter.kind === 'only') {
      const id = categoryFilter.id;
      const index = categories.findIndex((category) => category.id === id);
      return index < 0 ? 0 : index + 2;
    }
    return 0;
  }

  function amountLabel(): string {
    if (minAmount !== null && maxAmount !== null) {
      return `${mockMoney(minAmount, defaultCurrency, language)}–${mockMoney(maxAmount, defaultCurrency, language)}`;
    }
    if (minAmount !== null) {
      return `≥ ${mockMoney(minAmount, defaultCurrency, language)}`;
    }
    if (maxAmount !== null) {
      return `≤ ${mockMoney(maxAmount, defaultCurrency, language)}`;
    }
    return translate(language, 'AmountFilterAny');
  }

  function categoryText(transaction: MockTransaction): string {
    const category = categoryById(transaction.categoryId);
    return category === undefined ? translate(language, uncategorizedLabelKey) : category.name;
  }

  function categoryColour(transaction: MockTransaction): string {
    return categoryById(transaction.categoryId)?.colour ?? uncategorizedColour;
  }

  function isTaxRelevant(transaction: MockTransaction): boolean {
    if (transaction.taxOverride !== null) {
      return transaction.taxOverride;
    }
    return categoryById(transaction.categoryId)?.taxRelevant ?? false;
  }

  function taxStateLine(transaction: MockTransaction): string {
    const base = isTaxRelevant(transaction)
      ? translate(language, 'TaxRelevantChoice')
      : translate(language, 'TaxNotRelevantChoice');
    if (transaction.taxOverride !== null) {
      return translate(language, 'TaxSetByYou', base);
    }
    const category = categoryById(transaction.categoryId);
    if (category !== undefined) {
      return translate(language, 'TaxFromCategory', base, category.name);
    }
    return base;
  }

  function runChart(
    account: MockAccount,
    running: Map<number, RunningEntry>,
    accountBookings: MockTransaction[],
  ): Partial<TransactionsDTO> {
    const range = periodRange();
    let from = range?.from;
    if (from === undefined) {
      const oldest = accountBookings.reduce<string | undefined>(
        (current, booking) =>
          current === undefined || booking.date < current ? booking.date : current,
        undefined,
      );
      from = oldest;
    }
    if (from === undefined || accountBookings.length === 0) {
      return { hasChart: false };
    }
    let to = today;
    if (range !== null && range.to < today) {
      to = range.to;
    }
    const points = historyFromRunning(bookingsOf(accountBookings), running, from, to);
    const geometry = chartGeometry(points, account.minor, {
      money: (minor) => mockMoney(minor, account.currency, language),
      wholeMoney: (minor) => mockWholeMoney(minor, account.currency, language),
      date: mockDate,
    });
    if (geometry === null) {
      return { hasChart: false };
    }
    return {
      hasChart: true,
      chartCollapsed,
      chartLine: geometry.line,
      chartArea: geometry.area,
      chartAxisTop: geometry.axisTop,
      chartAxisBottom: geometry.axisBottom,
      chartAxisStart: geometry.axisStart,
      chartAxisMid: geometry.axisMid,
      chartAxisEnd: geometry.axisEnd,
      chartValue: geometry.value,
      chartNotFromZero: geometry.notFromZero,
      chartEndX: geometry.endX,
      chartEndY: geometry.endY,
      chartEndLabel: geometry.endLabel,
    };
  }

  function pendingRow(line: MockPending): TransactionRowDTO {
    const title = line.partner !== '' ? line.partner : line.purpose !== '' ? line.purpose : '—';
    const subtitle = line.partner !== '' ? line.purpose : '';
    return {
      pending: true,
      id: -1,
      dateText: mockDate(line.date),
      title,
      subtitle,
      categoryText: '',
      categoryColour: uncategorizedColour,
      partner: '',
      partnerLinkText: '',
      amountText: mockMoney(line.minor, line.currency, language, true),
      expense: line.minor < 0,
      isNew: false,
      selected: false,
      hasDocuments: false,
      documentsText: '',
      balanceText: '',
      balanceTone: 'zero',
      balanceWarning: false,
      balanceMismatch: '',
    };
  }

  function bookingRow(
    transaction: MockTransaction,
    running: Map<number, RunningEntry> | undefined,
    withBalance: boolean,
  ): TransactionRowDTO {
    const entry = running?.get(transaction.id);
    const mismatch =
      entry?.bankDifference !== null && entry?.bankDifference !== undefined
        ? translate(
            language,
            'BalanceBankMismatch',
            mockMoney(entry.balance, transaction.currency, language),
            mockMoney(-entry.bankDifference, transaction.currency, language, true),
          )
        : '';
    return {
      pending: false,
      id: transaction.id,
      dateText: mockDate(transaction.date),
      title: transaction.partner !== '' ? transaction.partner : transaction.purpose,
      subtitle: transaction.partner !== '' ? transaction.purpose : '',
      categoryText: categoryText(transaction),
      categoryColour: categoryColour(transaction),
      partner: transaction.partner,
      partnerLinkText:
        transaction.partner === ''
          ? ''
          : translate(language, 'ShowAllFromPartner', transaction.partner),
      amountText: mockMoney(transaction.minor, transaction.currency, language, true),
      expense: transaction.minor < 0,
      isNew: transaction.isNew,
      selected: selectedIDs.has(transaction.id),
      hasDocuments: transaction.documents > 0,
      documentsText:
        transaction.documents > 0
          ? translate(language, 'DocumentsCount', transaction.documents)
          : '',
      balanceText:
        withBalance && entry !== undefined
          ? mockMoney(entry.balance, transaction.currency, language)
          : '',
      balanceTone:
        withBalance && entry !== undefined
          ? entry.balance > 0
            ? 'positive'
            : entry.balance === 0
              ? 'zero'
              : selectedAccount()?.type === 'loan'
                ? 'loan'
                : 'negative'
          : 'zero',
      balanceWarning: withBalance && mismatch !== '',
      balanceMismatch: withBalance ? mismatch : '',
    };
  }

  function selectionTotalsText(): string {
    const totals = new Map<string, number>();
    for (const transaction of visibleTransactions()) {
      if (!selectedIDs.has(transaction.id)) {
        continue;
      }
      totals.set(transaction.currency, (totals.get(transaction.currency) ?? 0) + transaction.minor);
    }
    const ordered = [...totals.entries()].sort((left, right) => {
      if (left[0] === defaultCurrency) {
        return -1;
      }
      if (right[0] === defaultCurrency) {
        return 1;
      }
      return left[0].localeCompare(right[0]);
    });
    return ordered
      .map(([currency, minor]) => mockMoney(minor, currency, language, true))
      .join(' · ');
  }

  type DetailState = Pick<
    TransactionsDTO,
    | 'detailOpen'
    | 'detailCanCreateRule'
    | 'detailTitle'
    | 'detailAmount'
    | 'detailExpense'
    | 'detailDate'
    | 'detailAccount'
    | 'detailPartner'
    | 'detailShop'
    | 'detailPurpose'
    | 'detailCategoryIndex'
    | 'detailTaxRelevant'
    | 'detailTaxState'
    | 'detailTaxIndex'
    | 'detailHasMismatch'
    | 'detailMismatch'
    | 'detailPartnerLink'
    | 'duplicateOpen'
    | 'duplicateTitle'
    | 'duplicateText'
  >;

  function duplicateFields(): Pick<
    TransactionsDTO,
    'duplicateOpen' | 'duplicateTitle' | 'duplicateText'
  > {
    if (pendingDuplicate === null) {
      return { duplicateOpen: false, duplicateTitle: '', duplicateText: '' };
    }
    return {
      duplicateOpen: true,
      duplicateTitle: translate(language, 'DuplicateTransactionTitle'),
      duplicateText: translate(
        language,
        'DuplicateTransactionText',
        mockDate(pendingDuplicate.date),
        mockMoney(pendingDuplicate.minor, pendingDuplicate.currency, language),
      ),
    };
  }

  function detailFields(): DetailState {
    const transaction = detailID === null ? undefined : transactionById(detailID);
    if (transaction === undefined) {
      return {
        detailOpen: false,
        detailCanCreateRule: categories.length > 0,
        detailTitle: '',
        detailAmount: '',
        detailExpense: false,
        detailDate: '',
        detailAccount: '',
        detailPartner: '',
        detailShop: '',
        detailPurpose: '',
        detailCategoryIndex: 0,
        detailTaxRelevant: false,
        detailTaxState: '',
        detailTaxIndex: 0,
        detailHasMismatch: false,
        detailMismatch: '',
        detailPartnerLink: '',
        ...duplicateFields(),
      };
    }
    const account = accountById(transaction.accountId);
    const category = categoryById(transaction.categoryId);
    const categoryPosition =
      category === undefined ? 0 : categories.findIndex((entry) => entry.id === category.id) + 1;
    const taxIndex =
      transaction.taxOverride === null ? 0 : transaction.taxOverride === true ? 1 : 2;
    let mismatch = '';
    if (account !== undefined) {
      const accountBookings = transactions
        .filter((entry) => entry.accountId === account.id)
        .sort((left, right) => right.date.localeCompare(left.date) || right.id - left.id);
      const running = computeRunning(account.minor, bookingsOf(accountBookings));
      const entry = running.get(transaction.id);
      if (entry?.bankDifference !== null && entry?.bankDifference !== undefined) {
        mismatch = translate(
          language,
          'BalanceBankMismatch',
          mockMoney(entry.balance, transaction.currency, language),
          mockMoney(-entry.bankDifference, transaction.currency, language, true),
        );
      }
    }
    return {
      detailOpen: true,
      detailCanCreateRule: categories.length > 0,
      detailTitle: transaction.partner !== '' ? transaction.partner : transaction.purpose,
      detailAmount: mockMoney(transaction.minor, transaction.currency, language, true),
      detailExpense: transaction.minor < 0,
      detailDate: mockDate(transaction.date),
      detailAccount: account === undefined ? '' : accountLabel(account),
      detailPartner: transaction.partner,
      detailShop: '',
      detailPurpose: transaction.purpose,
      detailCategoryIndex: categoryPosition,
      detailTaxRelevant: isTaxRelevant(transaction),
      detailTaxState: taxStateLine(transaction),
      detailTaxIndex: taxIndex,
      detailHasMismatch: mismatch !== '',
      detailMismatch: mismatch,
      detailPartnerLink:
        transaction.partner === ''
          ? ''
          : translate(language, 'ShowAllFromPartner', transaction.partner),
      ...duplicateFields(),
    };
  }

  function importCsvOptions(preview: MockImportPreview): string[] {
    return [translate(language, 'NoneDash'), ...preview.csvHeaders];
  }

  function importFileRows(preview: MockImportPreview): ImportFileRowDTO[] {
    const rows: ImportFileRowDTO[] = [];
    for (const entry of preview.entries) {
      const warnings = entry.warnings.map((warning) =>
        translate(language, warning.key, warning.count),
      );
      rows.push({
        name: entry.file,
        countText: translate(language, 'BookingCount', entry.count),
        periodText:
          entry.from === entry.to
            ? mockDate(entry.from)
            : `${mockDate(entry.from)}–${mockDate(entry.to)}`,
        warningText: warnings.join(' · '),
        failure: false,
      });
    }
    for (const failure of preview.failures) {
      rows.push({
        name: failure.file,
        countText: '',
        periodText: '',
        warningText:
          failure.messageArg === ''
            ? translate(language, failure.messageKey)
            : translate(language, failure.messageKey, failure.messageArg),
        failure: true,
      });
    }
    return rows;
  }

  function importCsvFieldRows(preview: MockImportPreview): ImportCSVFieldDTO[] {
    if (preview.csvHeaders.length === 0) {
      return [];
    }
    const options = importCsvOptions(preview);
    const mapping = preview.csvMapping;
    const fields: { field: string; label: string; index: number }[] = [
      { field: 'date', label: translate(language, 'ColDate'), index: mapping.date },
      { field: 'amount', label: translate(language, 'ColAmount'), index: mapping.amount },
      { field: 'debit', label: translate(language, 'ColDebit'), index: mapping.debit },
      { field: 'credit', label: translate(language, 'ColCredit'), index: mapping.credit },
      { field: 'partner', label: translate(language, 'ColPartner'), index: mapping.partner },
      { field: 'purpose', label: translate(language, 'ColPurpose'), index: mapping.purpose },
      { field: 'balance', label: translate(language, 'ColBalance'), index: mapping.balance },
    ];
    return fields.map((field) => ({
      field: field.field,
      label: field.label,
      options: [...options],
      index: field.index,
    }));
  }

  function importCsvHint(preview: MockImportPreview): string {
    if (preview.csvHeaders.length === 0 || preview.entries.length <= 1) {
      return '';
    }
    return translate(language, 'ColumnsApplyToSame', preview.entries.length);
  }

  function importFields(): Pick<
    TransactionsDTO,
    | 'importOpen'
    | 'importTitle'
    | 'importSummary'
    | 'importFiles'
    | 'importCsvFields'
    | 'importCsvHint'
    | 'importAccountOptions'
    | 'importAccountIndex'
    | 'importBalanceAllowed'
    | 'importUpdateBalance'
    | 'importBalanceText'
    | 'importNewerText'
    | 'importing'
    | 'importConfirmText'
    | 'importConfirmEnabled'
  > {
    const preview = importPreview;
    if (preview === null) {
      return {
        importOpen: false,
        importTitle: '',
        importSummary: '',
        importFiles: [],
        importCsvFields: [],
        importCsvHint: '',
        importAccountOptions: [],
        importAccountIndex: 0,
        importBalanceAllowed: false,
        importUpdateBalance: false,
        importBalanceText: '',
        importNewerText: '',
        importing: false,
        importConfirmText: '',
        importConfirmEnabled: false,
      };
    }
    const index =
      preview.accountId === null
        ? 0
        : Math.max(
            0,
            accounts.findIndex((account) => account.id === preview.accountId),
          );
    return {
      importOpen: true,
      importTitle:
        preview.files.length <= 1
          ? translate(language, 'ImportFile', preview.files[0] ?? '')
          : translate(language, 'ImportFiles', preview.files.length),
      importSummary:
        preview.files.length > 1 ? translate(language, 'BookingCount', preview.bookingCount) : '',
      importFiles: importFileRows(preview),
      importCsvFields: importCsvFieldRows(preview),
      importCsvHint: importCsvHint(preview),
      importAccountOptions: accounts.map(accountLabel),
      importAccountIndex: index,
      importBalanceAllowed: preview.balanceUpdateAllowed,
      importUpdateBalance: preview.updateBalance,
      importBalanceText: translate(language, 'SetAccountBalance'),
      importNewerText:
        preview.hasBalances && !preview.balanceUpdateAllowed
          ? translate(language, 'NewerBookingsUnchanged')
          : '',
      importing: false,
      importConfirmText: translate(language, 'ImportCount', preview.bookingCount),
      importConfirmEnabled: preview.accountId !== null && preview.bookingCount > 0,
    };
  }

  function reapplyFields(): Pick<
    TransactionsDTO,
    | 'reapplyOpen'
    | 'reapplySummary'
    | 'reapplyWarning'
    | 'reapplyGroups'
    | 'reapplyConfirmText'
    | 'reapplyConfirmEnabled'
  > {
    let totalCount = 0;
    let tickedCount = 0;
    const rows: ReapplyGroupRowDTO[] = [];
    for (const group of reapplyGroups) {
      totalCount += group.count;
      if (group.ticked) {
        tickedCount += group.count;
      }
      rows.push({
        fromId: group.fromId,
        toId: group.toId,
        title: `${group.fromName} → ${group.toName}`,
        detail:
          group.sum === null
            ? String(group.count)
            : `${group.count} · ${mockMoney(group.sum, group.currency, language, true)}`,
        ticked: group.ticked,
      });
    }
    return {
      reapplyOpen,
      reapplySummary: translate(language, 'ReapplyRulesSummary', totalCount, reapplyShownCount),
      reapplyWarning: translate(language, 'ReapplyRulesWarning'),
      reapplyGroups: rows,
      reapplyConfirmText: translate(language, 'ReapplyRulesConfirm', tickedCount),
      reapplyConfirmEnabled: tickedCount > 0,
    };
  }

  function multiDeleteFields(): Pick<
    TransactionsDTO,
    | 'multiDeleteOpen'
    | 'multiDeleteTitle'
    | 'multiDeleteRange'
    | 'multiDeleteTotals'
    | 'multiDeleteLines'
    | 'multiDeleteMore'
  > {
    if (!multiDeleteOpen) {
      return {
        multiDeleteOpen: false,
        multiDeleteTitle: '',
        multiDeleteRange: '',
        multiDeleteTotals: '',
        multiDeleteLines: [],
        multiDeleteMore: '',
      };
    }
    const bookings = visibleTransactions().filter((transaction) =>
      multiDeleteIDs.has(transaction.id),
    );
    if (bookings.length === 0) {
      return {
        multiDeleteOpen: false,
        multiDeleteTitle: '',
        multiDeleteRange: '',
        multiDeleteTotals: '',
        multiDeleteLines: [],
        multiDeleteMore: '',
      };
    }
    let from = bookings[0]?.date ?? today;
    let to = from;
    const totals = new Map<string, number>();
    for (const booking of bookings) {
      if (booking.date < from) {
        from = booking.date;
      }
      if (booking.date > to) {
        to = booking.date;
      }
      totals.set(booking.currency, (totals.get(booking.currency) ?? 0) + booking.minor);
    }
    const ordered = [...totals.entries()].sort((left, right) => {
      if (left[0] === defaultCurrency) {
        return -1;
      }
      if (right[0] === defaultCurrency) {
        return 1;
      }
      return left[0].localeCompare(right[0]);
    });
    const lines = bookings
      .slice(0, 5)
      .map(
        (booking) =>
          `${mockDate(booking.date)} ${booking.partner !== '' ? booking.partner : booking.purpose} ${mockMoney(booking.minor, booking.currency, language)}`,
      );
    const extra = bookings.length - 5;
    return {
      multiDeleteOpen: true,
      multiDeleteTitle: translate(language, 'DeleteTransactionsTitle', bookings.length),
      multiDeleteRange: from === to ? mockDate(from) : `${mockDate(from)}–${mockDate(to)}`,
      multiDeleteTotals: ordered
        .map(([currency, minor]) => mockMoney(minor, currency, language, true))
        .join(' · '),
      multiDeleteLines: lines,
      multiDeleteMore: extra > 0 ? translate(language, 'AndMore', extra) : '',
    };
  }

  function state(): TransactionsDTO {
    const visible = visibleTransactions();
    const pendingLines = visiblePending();
    const account = selectedAccount();
    const withBalance = account !== undefined && account.type !== 'depot';

    let running: Map<number, RunningEntry> | undefined;
    let accountBookings: MockTransaction[] = [];
    if (account !== undefined) {
      accountBookings = transactions
        .filter((transaction) => transaction.accountId === account.id)
        .sort((left, right) => right.date.localeCompare(left.date) || right.id - left.id);
      running = computeRunning(account.minor, bookingsOf(accountBookings));
    }

    const income = new Map<string, number>();
    const expenses = new Map<string, number>();
    for (const transaction of visible) {
      if (transaction.minor > 0) {
        income.set(
          transaction.currency,
          (income.get(transaction.currency) ?? 0) + transaction.minor,
        );
      }
      if (transaction.minor < 0) {
        expenses.set(
          transaction.currency,
          (expenses.get(transaction.currency) ?? 0) + Math.abs(transaction.minor),
        );
      }
    }
    const orderedEntries = (totals: Map<string, number>): [string, number][] =>
      [...totals.entries()].sort((left, right) => {
        if (left[0] === defaultCurrency) {
          return -1;
        }
        if (right[0] === defaultCurrency) {
          return 1;
        }
        return left[0].localeCompare(right[0]);
      });

    const selectedShown = visible.filter((transaction) => selectedIDs.has(transaction.id));
    const allSelected = visible.length > 0 && selectedShown.length === visible.length;
    const showBack =
      query.trim() !== '' &&
      selection.kind === 'all' &&
      period === 'all' &&
      customRange === null &&
      direction === 'all' &&
      categoryFilter.kind === 'any' &&
      minAmount === null &&
      maxAmount === null;

    const options = accountOptions();
    const chart =
      account === undefined ? { hasChart: false } : runChart(account, running!, accountBookings);

    const taxChoices = [
      translate(language, 'TaxAutomatic'),
      translate(language, 'TaxRelevantChoice'),
      translate(language, 'TaxNotRelevantChoice'),
    ];
    const categoryNames = [
      translate(language, 'Uncategorized'),
      ...categories.map((category) =>
        category.taxRelevant ? `${category.name} (${translate(language, 'Tax')})` : category.name,
      ),
    ];

    const categoryOptions = [
      translate(language, 'AllCategories'),
      translate(language, 'Uncategorized'),
      ...categories.map((category) => category.name),
    ];
    const directionOptions = [
      translate(language, 'DirectionAnyLabel'),
      translate(language, 'DirectionIncome'),
      translate(language, 'DirectionExpenses'),
    ];
    const periodOptions = [
      translate(language, 'PeriodThisMonth'),
      translate(language, 'PeriodLastMonth'),
      translate(language, 'PeriodLast3Months'),
      translate(language, 'PeriodThisYear'),
      translate(language, 'PeriodLastYear'),
      translate(language, 'PeriodAllTime'),
      translate(language, 'PeriodCustomRange'),
    ];

    let periodExtra = '';
    if (customRange !== null) {
      periodExtra = `${mockDate(customRange.from)} – ${mockDate(customRange.to)}`;
    }

    const rows: TransactionRowDTO[] = [
      ...pendingLines.map(pendingRow),
      ...visible.map((transaction) => bookingRow(transaction, running, withBalance)),
    ];

    return {
      ...importFields(),
      loading: false,
      hasAccounts: accounts.length > 0,
      errorText,
      messageText,
      countText: String(visible.length),
      incomeAmounts: orderedEntries(income).map(([currency, minor]) => ({
        text: mockMoney(minor, currency, language),
        tone: 'positive',
      })),
      expenseAmounts: orderedEntries(expenses).map(([currency, minor]) => ({
        text: mockMoney(minor, currency, language),
        tone: 'negative',
      })),
      hasBalance: account !== undefined,
      balanceText:
        account === undefined ? '' : mockMoney(account.minor, account.currency, language),
      balanceTone:
        account === undefined
          ? ''
          : account.minor > 0
            ? 'positive'
            : account.minor === 0
              ? 'zero'
              : account.type === 'loan'
                ? 'loan'
                : 'negative',
      showBack,
      periodOptions,
      periodIndex: periodIndex(),
      periodExtra,
      categoryOptions,
      categoryIndex: categoryIndex(),
      directionOptions,
      directionIndex: direction === 'income' ? 1 : direction === 'expenses' ? 2 : 0,
      amountLabel: amountLabel(),
      accountOptions: options.labels,
      accountIndex: options.index,
      panelRows: panelRows(),
      groupChoices: groups.map((group) => ({ id: group.id, name: group.name })),
      panelFilterText,
      panelShownText:
        panelFilterText.trim() === ''
          ? ''
          : translate(
              language,
              'AccountsShown',
              accounts.filter(matchesPanelFilter).length,
              accounts.length,
            ),
      pendingHeader:
        pendingLines.length === 0
          ? ''
          : translate(language, 'PendingHeader', mockDateTime(pendingLines[0]?.fetchedAt ?? '')),
      rows,
      hasTransactions: visible.length > 0,
      hasBalanceColumn: withBalance,
      addCategoryNames: [
        translate(language, 'AutoRules'),
        ...categories.map((category) => category.name),
      ],
      addDate,
      addAmountLabel: translate(language, 'AmountIn', currencySymbol(defaultCurrency, language)),
      addAccountOptions: accounts.map((entry) => `${accountLabel(entry)} · ${entry.currency}`),
      addAccountIndex:
        account === undefined
          ? 0
          : Math.max(
              0,
              accounts.findIndex((entry) => entry.id === account.id),
            ),
      hasChart: chart.hasChart ?? false,
      chartCollapsed: chart.chartCollapsed ?? false,
      chartLine: chart.chartLine ?? '',
      chartArea: chart.chartArea ?? '',
      chartAxisTop: chart.chartAxisTop ?? '',
      chartAxisBottom: chart.chartAxisBottom ?? '',
      chartAxisStart: chart.chartAxisStart ?? '',
      chartAxisMid: chart.chartAxisMid ?? '',
      chartAxisEnd: chart.chartAxisEnd ?? '',
      chartValue: chart.chartValue ?? '',
      chartNotFromZero: chart.chartNotFromZero ?? false,
      chartEndX: chart.chartEndX ?? 0,
      chartEndY: chart.chartEndY ?? 0,
      chartEndLabel: chart.chartEndLabel ?? '',
      selectionVisible: selectedIDs.size > 0,
      allSelected,
      selectionCountText: translate(language, 'SelectedCount', selectedIDs.size),
      selectionTotalsText: selectionTotalsText(),
      categoryNames,
      taxOptions: taxChoices,
      ...detailFields(),
      canUndoReapply,
      ...reapplyFields(),
      rangeOpen,
      rangeFrom,
      rangeTo,
      rangeError,
      amountOpen,
      amountMin,
      amountMax,
      amountMinLabel: translate(language, 'MinAmountIn', currencySymbol(defaultCurrency, language)),
      amountMaxLabel: translate(language, 'MaxAmountIn', currencySymbol(defaultCurrency, language)),
      amountError,
      addOpen,
      addError,
      ruleOpen,
      rulePattern,
      ruleCategories: categories.map((category) => category.name),
      ruleCategoryIndex,
      ruleError,
      deleteOpen,
      deleteId: deleteID,
      deleteTitle,
      deleteText,
      ...multiDeleteFields(),
    };
  }

  function publish(): TransactionsDTO {
    const next = state();
    for (const listener of listeners) {
      listener(next);
    }
    return next;
  }

  function clearMessages(): void {
    errorText = '';
    messageText = '';
  }

  function openRange(): TransactionsDTO {
    const range = periodRange() ?? { from: isoDate(2026, 1, 1), to: today };
    rangeOpen = true;
    rangeFrom = mockDate(customRange?.from ?? range.from);
    rangeTo = mockDate(customRange?.to ?? range.to);
    rangeError = '';
    return publish();
  }

  function applyRange(from: string, to: string): TransactionsDTO {
    const parsedFrom = parseDay(from);
    const parsedTo = parseDay(to);
    if (parsedFrom === null || parsedTo === null) {
      rangeError = translate(language, 'EnterValidDate');
      return publish();
    }
    if (parsedFrom > parsedTo) {
      rangeError = translate(language, 'InvalidDateRange');
      return publish();
    }
    customRange = { from: parsedFrom, to: parsedTo };
    rangeOpen = false;
    rangeError = '';
    clearBookingSelection();
    return publish();
  }

  function closeRange(): TransactionsDTO {
    rangeOpen = false;
    rangeError = '';
    return publish();
  }

  function openAmount(): TransactionsDTO {
    amountOpen = true;
    amountMin = minAmount === null ? '' : (minAmount / 100).toFixed(2);
    amountMax = maxAmount === null ? '' : (maxAmount / 100).toFixed(2);
    amountError = '';
    return publish();
  }

  function applyAmount(minimum: string, maximum: string): TransactionsDTO {
    const parsedMin = minimum.trim() === '' ? null : parseAmount(minimum);
    const parsedMax = maximum.trim() === '' ? null : parseAmount(maximum);
    if (
      (minimum.trim() !== '' && parsedMin === null) ||
      (maximum.trim() !== '' && parsedMax === null)
    ) {
      amountError = translate(language, 'EnterValidAmount');
      return publish();
    }
    minAmount = parsedMin;
    maxAmount = parsedMax;
    amountOpen = false;
    amountError = '';
    clearBookingSelection();
    return publish();
  }

  function resetAmount(): TransactionsDTO {
    minAmount = null;
    maxAmount = null;
    amountOpen = false;
    amountError = '';
    clearBookingSelection();
    return publish();
  }

  function closeAmount(): TransactionsDTO {
    amountOpen = false;
    amountError = '';
    return publish();
  }

  function selectAccount(account: MockAccount): TransactionsDTO {
    selection = { kind: 'account', id: account.id };
    markAccountRead(account);
    clearBookingSelection();
    return publish();
  }

  function select(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): TransactionsDTO {
    if (kind === 'all') {
      selection = { kind: 'all' };
    } else if (kind === 'group' && (groupKind === 'type' || groupKind === 'custom')) {
      selection = { kind: 'group', groupKind, id, accountType };
    } else if (kind === 'account') {
      const account = accountById(id);
      if (account !== undefined) {
        return selectAccount(account);
      }
      selection = { kind: 'account', id };
    }
    clearBookingSelection();
    return publish();
  }

  function toggleGroup(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): TransactionsDTO {
    if (kind !== 'group' || (groupKind !== 'type' && groupKind !== 'custom')) {
      return state();
    }
    const key = groupKey(groupKind, id, accountType);
    if (collapsed.has(key)) {
      collapsed.delete(key);
    } else {
      collapsed.add(key);
    }
    return publish();
  }

  function markRead(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): TransactionsDTO {
    if (kind === 'all') {
      for (const account of accounts) {
        markAccountRead(account);
      }
    } else if (kind === 'group' && (groupKind === 'type' || groupKind === 'custom')) {
      for (const account of groupAccounts(groupKind, id, accountType)) {
        markAccountRead(account);
      }
    } else if (kind === 'account') {
      const account = accountById(id);
      if (account !== undefined) {
        markAccountRead(account);
      }
    }
    return publish();
  }

  function accountChanged(index: number): TransactionsDTO {
    const options = accountOptions();
    const label = options.labels[index];
    if (label === undefined) {
      return state();
    }
    if (index === 0) {
      selection = { kind: 'all' };
      clearBookingSelection();
      return publish();
    }
    const direct = accounts.find((account) => accountLabel(account) === label);
    if (direct !== undefined) {
      return selectAccount(direct);
    }
    const group = groups.find((entry) => entry.name === label);
    if (group !== undefined) {
      selection = { kind: 'group', groupKind: 'custom', id: group.id, accountType: '' };
      clearBookingSelection();
      return publish();
    }
    for (const type of accountTypes) {
      if (translate(language, typeKey(type)) === label) {
        selection = { kind: 'group', groupKind: 'type', id: -1, accountType: type };
        clearBookingSelection();
        return publish();
      }
    }
    return state();
  }

  function search(text: string): TransactionsDTO {
    query = text;
    clearBookingSelection();
    return publish();
  }

  function periodChanged(index: number): TransactionsDTO {
    const periods: Period[] = [
      'this-month',
      'last-month',
      'last-3',
      'this-year',
      'last-year',
      'all',
    ];
    const next = periods[index];
    if (next === undefined) {
      return state();
    }
    period = next;
    customRange = null;
    clearBookingSelection();
    return publish();
  }

  function categoryChanged(index: number): TransactionsDTO {
    if (index <= 0) {
      categoryFilter = { kind: 'any' };
    } else if (index === 1) {
      categoryFilter = { kind: 'none' };
    } else {
      const category = categories[index - 2];
      if (category === undefined) {
        return state();
      }
      categoryFilter = { kind: 'only', id: category.id };
    }
    clearBookingSelection();
    return publish();
  }

  function directionChanged(index: number): TransactionsDTO {
    direction = index === 1 ? 'income' : index === 2 ? 'expenses' : 'all';
    clearBookingSelection();
    return publish();
  }

  function back(): TransactionsDTO {
    query = '';
    clearBookingSelection();
    return publish();
  }

  function toggleChart(): TransactionsDTO {
    chartCollapsed = !chartCollapsed;
    return publish();
  }

  function openDetail(id: number): TransactionsDTO {
    if (transactionById(id) === undefined) {
      return state();
    }
    detailID = id;
    return publish();
  }

  function closeDetail(): TransactionsDTO {
    detailID = null;
    return publish();
  }

  function detailCategory(index: number): TransactionsDTO {
    const transaction = detailID === null ? undefined : transactionById(detailID);
    if (transaction === undefined) {
      return state();
    }
    if (index <= 0) {
      transaction.categoryId = null;
    } else {
      const category = categories[index - 1];
      if (category === undefined) {
        return state();
      }
      transaction.categoryId = category.id;
    }
    return publish();
  }

  function detailTax(index: number): TransactionsDTO {
    const transaction = detailID === null ? undefined : transactionById(detailID);
    if (transaction === undefined) {
      return state();
    }
    transaction.taxOverride = index === 1 ? true : index === 2 ? false : null;
    transaction.taxSource = index === 0 ? '' : 'manual';
    return publish();
  }

  function toggleSelect(id: number): TransactionsDTO {
    if (!transactions.some((transaction) => transaction.id === id)) {
      return state();
    }
    if (selectedIDs.has(id)) {
      selectedIDs.delete(id);
    } else {
      selectedIDs.add(id);
    }
    anchorID = id;
    return publish();
  }

  function selectRange(id: number): TransactionsDTO {
    if (anchorID === null) {
      return toggleSelect(id);
    }
    const visible = visibleTransactions();
    const from = visible.findIndex((transaction) => transaction.id === anchorID);
    const to = visible.findIndex((transaction) => transaction.id === id);
    if (from < 0 || to < 0) {
      return toggleSelect(id);
    }
    const low = Math.min(from, to);
    const high = Math.max(from, to);
    for (let index = low; index <= high; index += 1) {
      const transaction = visible[index];
      if (transaction !== undefined) {
        selectedIDs.add(transaction.id);
      }
    }
    anchorID = id;
    return publish();
  }

  function selectAll(): TransactionsDTO {
    selectedIDs = new Set(visibleTransactions().map((transaction) => transaction.id));
    return publish();
  }

  function clearSelection(): TransactionsDTO {
    clearBookingSelection();
    return publish();
  }

  function showPartner(partner: string): TransactionsDTO {
    query = partner;
    detailID = null;
    clearBookingSelection();
    return publish();
  }

  function openDelete(id: number): TransactionsDTO {
    const visible = visibleTransactions().find((transaction) => transaction.id === id);
    if (visible === undefined) {
      return state();
    }
    deleteID = id;
    deleteOpen = true;
    deleteTitle = translate(language, 'DeleteTransactionTitle');
    deleteText = `${mockDate(visible.date)} ${visible.partner !== '' ? visible.partner : visible.purpose} ${mockMoney(visible.minor, visible.currency, language)}`;
    return publish();
  }

  function confirmDelete(id: number): TransactionsDTO {
    transactions = transactions.filter((transaction) => transaction.id !== id);
    selectedIDs.delete(id);
    if (detailID === id) {
      detailID = null;
    }
    deleteOpen = false;
    deleteID = -1;
    messageText = translate(language, 'MessageTransactionsDeleted', 1);
    return publish();
  }

  function cancelDelete(): TransactionsDTO {
    deleteOpen = false;
    deleteID = -1;
    return publish();
  }

  function openMultiDelete(): TransactionsDTO {
    const shown = visibleTransactions().filter((transaction) => selectedIDs.has(transaction.id));
    if (shown.length === 0) {
      return state();
    }
    multiDeleteOpen = true;
    multiDeleteIDs = new Set(shown.map((transaction) => transaction.id));
    return publish();
  }

  function confirmMultiDelete(): TransactionsDTO {
    const count = multiDeleteIDs.size;
    transactions = transactions.filter((transaction) => !multiDeleteIDs.has(transaction.id));
    multiDeleteIDs = new Set();
    multiDeleteOpen = false;
    clearBookingSelection();
    messageText = translate(language, 'MessageTransactionsDeleted', count);
    return publish();
  }

  function cancelMultiDelete(): TransactionsDTO {
    multiDeleteOpen = false;
    multiDeleteIDs = new Set();
    return publish();
  }

  function batchCategory(index: number): TransactionsDTO {
    const category = index <= 0 ? null : (categories[index - 1]?.id ?? null);
    for (const transaction of visibleTransactions()) {
      if (selectedIDs.has(transaction.id)) {
        transaction.categoryId = category;
      }
    }
    return publish();
  }

  function batchTax(index: number): TransactionsDTO {
    const override = index === 1 ? true : index === 2 ? false : null;
    for (const transaction of visibleTransactions()) {
      if (selectedIDs.has(transaction.id)) {
        transaction.taxOverride = override;
        transaction.taxSource = index === 0 ? '' : 'manual';
      }
    }
    return publish();
  }

  function autoCategorize(): TransactionsDTO {
    const rules: [string, number][] = [
      ['Example Market', 11],
      ['Corner Kiosk', 11],
      ['Employer GmbH', 12],
      ['Transfer from Haushalt', 12],
    ];
    let changed = 0;
    let candidates = 0;
    for (const transaction of transactions) {
      if (transaction.categoryId !== null) {
        continue;
      }
      candidates += 1;
      const rule = rules.find(([pattern]) =>
        transaction.partner.toLowerCase().includes(pattern.toLowerCase()),
      );
      if (rule !== undefined) {
        transaction.categoryId = rule[1];
        changed += 1;
      }
    }
    messageText = translate(language, 'MessageAutoCategorized', changed, candidates);
    return publish();
  }

  function reapplyRules(): TransactionsDTO {
    const shown = visibleTransactions();
    reapplyShownCount = shown.length;
    const food = categories.find((category) => category.name === 'Food');
    const health = categories.find((category) => category.name === 'Health');
    reapplyGroups = [];
    if (shown.length === 0 || food === undefined || health === undefined) {
      messageText = translate(language, 'MessageReapplyNothing');
      reapplyOpen = false;
      return publish();
    }
    const members = shown.filter((transaction) => transaction.categoryId === food.id);
    if (members.length === 0) {
      messageText = translate(language, 'MessageReapplyNothing');
      reapplyOpen = false;
      return publish();
    }
    const currency = members[0]?.currency ?? defaultCurrency;
    const sum = members.reduce((total, transaction) => total + transaction.minor, 0);
    reapplyGroups = [
      {
        fromId: food.id,
        toId: health.id,
        fromName: food.name,
        toName: health.name,
        count: members.length,
        sum,
        currency,
        ticked: true,
      },
    ];
    reapplyOpen = true;
    return publish();
  }

  function toggleReapplyGroup(from: number, to: number): TransactionsDTO {
    reapplyGroups = reapplyGroups.map((group) =>
      group.fromId === from && group.toId === to ? { ...group, ticked: !group.ticked } : group,
    );
    return publish();
  }

  function confirmReapplyRules(): TransactionsDTO {
    const snapshot: { id: number; categoryId: number | null }[] = [];
    let changed = 0;
    for (const group of reapplyGroups) {
      if (!group.ticked) {
        continue;
      }
      for (const transaction of visibleTransactions()) {
        const matchesFrom =
          group.fromId < 0
            ? transaction.categoryId === null
            : transaction.categoryId === group.fromId;
        if (!matchesFrom) {
          continue;
        }
        snapshot.push({ id: transaction.id, categoryId: transaction.categoryId });
        transaction.categoryId = group.toId;
        changed += 1;
      }
    }
    lastReapply = snapshot;
    canUndoReapply = changed > 0;
    reapplyOpen = false;
    reapplyGroups = [];
    messageText =
      changed > 0
        ? translate(language, 'MessageReapplied', changed)
        : translate(language, 'MessageReapplyNothing');
    return publish();
  }

  function cancelReapplyRules(): TransactionsDTO {
    reapplyOpen = false;
    reapplyGroups = [];
    return publish();
  }

  function undoReapply(): TransactionsDTO {
    const count = lastReapply.length;
    for (const entry of lastReapply) {
      const transaction = transactionById(entry.id);
      if (transaction !== undefined) {
        transaction.categoryId = entry.categoryId;
      }
    }
    lastReapply = [];
    canUndoReapply = false;
    messageText = translate(language, 'MessageReapplyUndone', count);
    return publish();
  }

  function openAdd(): TransactionsDTO {
    clearMessages();
    addOpen = true;
    addError = '';
    addDate = mockDate(today);
    return publish();
  }

  function closeAdd(): TransactionsDTO {
    addOpen = false;
    addError = '';
    return publish();
  }

  function addSubmit(
    accountIndexValue: number,
    expense: boolean,
    date: string,
    amount: string,
    partner: string,
    purpose: string,
    categoryIndexValue: number,
  ): TransactionsDTO {
    const account = accounts[accountIndexValue];
    if (account === undefined) {
      addError = translate(language, 'ErrorUnexpected');
      return publish();
    }
    const parsedDate = parseDay(date);
    if (parsedDate === null) {
      addError = translate(language, 'EnterValidDate');
      return publish();
    }
    const parsedAmount = parseAmount(amount);
    if (parsedAmount === null) {
      addError = translate(language, 'EnterValidAmount');
      return publish();
    }
    const category =
      categoryIndexValue <= 0 ? null : (categories[categoryIndexValue - 1]?.id ?? null);
    const cleanPartner = partner.trim();
    const cleanPurpose = purpose.trim();
    const minor = expense ? -parsedAmount : parsedAmount;
    const existing = transactions.find(
      (transaction) =>
        transaction.accountId === account.id &&
        transaction.date === parsedDate &&
        transaction.minor === minor &&
        transaction.partner === cleanPartner &&
        transaction.purpose === cleanPurpose,
    );
    const entry: Omit<MockTransaction, 'id'> = {
      date: parsedDate,
      partner: cleanPartner,
      purpose: cleanPurpose,
      categoryId: category,
      minor,
      currency: account.currency,
      accountId: account.id,
      isNew: true,
      documents: 0,
      taxOverride: null,
      taxSource: '',
      taxRulePattern: '',
      balanceAfter: null,
    };
    addOpen = false;
    addError = '';
    if (existing !== undefined) {
      pendingAdd = entry;
      pendingDuplicate = {
        date: existing.date,
        minor: existing.minor,
        currency: existing.currency,
      };
      return publish();
    }
    const id = Math.max(...transactions.map((transaction) => transaction.id), 0) + 1;
    transactions.push({ id, ...entry });
    messageText = translate(language, 'MessageTransactionAdded');
    return publish();
  }

  function confirmDuplicate(): TransactionsDTO {
    if (pendingAdd !== null) {
      const id = Math.max(...transactions.map((transaction) => transaction.id), 0) + 1;
      transactions.push({ id, ...pendingAdd });
      messageText = translate(language, 'MessageTransactionAdded');
    }
    pendingAdd = null;
    pendingDuplicate = null;
    return publish();
  }

  function dismissDuplicate(): TransactionsDTO {
    pendingAdd = null;
    pendingDuplicate = null;
    return publish();
  }

  function showDuplicate(date: string, minor: number, currency: string): void {
    pendingAdd = null;
    pendingDuplicate = { date, minor, currency };
  }

  function openRule(): TransactionsDTO {
    const transaction = detailID === null ? undefined : transactionById(detailID);
    if (transaction === undefined) {
      return state();
    }
    ruleOpen = true;
    rulePattern = transaction.partner;
    ruleCategoryIndex = Math.max(0, categoryIndexFrom(transaction));
    ruleError = '';
    return publish();
  }

  function categoryIndexFrom(transaction: MockTransaction): number {
    if (transaction.categoryId === null) {
      return 0;
    }
    return categories.findIndex((category) => category.id === transaction.categoryId);
  }

  function closeRule(): TransactionsDTO {
    ruleOpen = false;
    ruleError = '';
    return publish();
  }

  function saveRule(pattern: string, categoryIndexValue: number): TransactionsDTO {
    if (pattern.trim() === '') {
      ruleError = translate(language, 'ErrorPatternRequired');
      return publish();
    }
    if (categories[categoryIndexValue] === undefined) {
      return state();
    }
    ruleOpen = false;
    ruleError = '';
    messageText = translate(language, 'MessageRuleCreated', 0);
    return publish();
  }

  function dismissMessage(): TransactionsDTO {
    clearMessages();
    return publish();
  }

  function openImport(): TransactionsDTO {
    clearMessages();
    importKind = scenario.includes('mt940')
      ? 'mt940'
      : scenario.includes('warnings')
        ? 'warnings'
        : 'csv';
    importPreview = sampleImportPreview(importKind, accounts[0]?.id ?? 1);
    return publish();
  }

  function importAccountChanged(index: number): TransactionsDTO {
    if (importPreview === null) {
      return state();
    }
    const account = accounts[index];
    if (account === undefined) {
      return state();
    }
    importPreview.accountId = account.id;
    return publish();
  }

  function importCsvChanged(field: string, value: string): TransactionsDTO {
    if (importPreview === null || importPreview.csvHeaders.length === 0) {
      return state();
    }
    const options = importCsvOptions(importPreview);
    const index = options.indexOf(value);
    if (index < 0) {
      return state();
    }
    const mapping = importPreview.csvMapping;
    switch (field) {
      case 'date':
        mapping.date = index;
        break;
      case 'amount':
        mapping.amount = index;
        if (index > 0) {
          mapping.debit = 0;
          mapping.credit = 0;
        }
        break;
      case 'debit':
        mapping.debit = index;
        if (index > 0) {
          mapping.amount = 0;
        }
        break;
      case 'credit':
        mapping.credit = index;
        if (index > 0) {
          mapping.amount = 0;
        }
        break;
      case 'partner':
        mapping.partner = index;
        break;
      case 'purpose':
        mapping.purpose = index;
        break;
      case 'balance':
        mapping.balance = index;
        break;
      default:
        return state();
    }
    return publish();
  }

  function confirmImport(updateBalance: boolean): TransactionsDTO {
    const preview = importPreview;
    if (
      preview === null ||
      preview.accountId === null ||
      preview.bookingCount === 0 ||
      accounts.every((account) => account.id !== preview.accountId)
    ) {
      return state();
    }
    const account = accountById(preview.accountId);
    if (account === undefined) {
      return state();
    }
    preview.updateBalance = updateBalance;
    let nextID = Math.max(...transactions.map((transaction) => transaction.id), 0) + 1;
    const imported: MockTransaction[] = [];
    for (const entry of preview.entries) {
      for (let index = 0; index < entry.count; index += 1) {
        imported.push({
          id: nextID,
          date: index === 0 ? entry.from : index === 1 ? entry.to : entry.from,
          partner: entry.file,
          purpose: '',
          categoryId: null,
          minor: index % 2 === 0 ? -1_500 : 1_500,
          currency: account.currency,
          accountId: account.id,
          isNew: true,
          documents: 0,
          taxOverride: null,
          taxSource: '',
          taxRulePattern: '',
          balanceAfter: null,
        });
        nextID += 1;
      }
    }
    transactions = [...transactions, ...imported];
    if (updateBalance && preview.balanceUpdateAllowed) {
      account.minor += imported.reduce((total, entry) => total + entry.minor, 0);
    }
    importPreview = null;
    messageText =
      preview.files.length > 1
        ? translate(language, 'ImportSummaryFiles', preview.bookingCount, preview.files.length)
        : translate(language, 'ImportSummary', preview.bookingCount);
    return publish();
  }

  function cancelImport(): TransactionsDTO {
    importPreview = null;
    return publish();
  }

  function exportCsv(): TransactionsDTO {
    const name = `transactions-${today}.csv`;
    messageText = translate(language, 'MessageExportedCount', visibleTransactions().length, name);
    return publish();
  }

  function attachReceipt(): TransactionsDTO {
    const transaction = detailID === null ? undefined : transactionById(detailID);
    if (transaction === undefined) {
      return state();
    }
    transaction.documents += 1;
    messageText = translate(language, 'MessageDocumentLinked');
    return publish();
  }

  function panelFilterChanged(text: string): TransactionsDTO {
    panelFilterText = text;
    return publish();
  }

  function newGroup(name: string): TransactionsDTO {
    const clean = name.trim();
    if (clean === '') {
      errorText = translate(language, 'ErrorNameRequired');
      return publish();
    }
    groups = [...groups, { id: nextGroupID, name: clean }];
    nextGroupID += 1;
    return publish();
  }

  function renameGroup(id: number, name: string): TransactionsDTO {
    const clean = name.trim();
    if (clean === '') {
      errorText = translate(language, 'ErrorNameRequired');
      return publish();
    }
    groups = groups.map((group) => (group.id === id ? { ...group, name: clean } : group));
    return publish();
  }

  function deleteGroup(id: number): TransactionsDTO {
    groups = groups.filter((group) => group.id !== id);
    accounts = accounts.map((account) =>
      account.groupId === id ? { ...account, groupId: null } : account,
    );
    if (selection.kind === 'group' && selection.groupKind === 'custom' && selection.id === id) {
      selection = { kind: 'all' };
    }
    return publish();
  }

  function moveGroup(id: number, up: boolean): TransactionsDTO {
    const index = groups.findIndex((group) => group.id === id);
    if (index < 0) {
      return state();
    }
    const target = up ? index - 1 : index + 1;
    const current = groups[index];
    const neighbour = groups[target];
    if (current === undefined || neighbour === undefined) {
      return publish();
    }
    const reordered = [...groups];
    reordered[index] = neighbour;
    reordered[target] = current;
    groups = reordered;
    return publish();
  }

  function moveAccount(accountID: number, groupID: number): TransactionsDTO {
    accounts = accounts.map((account) =>
      account.id === accountID ? { ...account, groupId: groupID < 0 ? null : groupID } : account,
    );
    return publish();
  }

  function setLanguage(next: Language): TransactionsDTO {
    language = next;
    return publish();
  }

  function setPeriod(next: Period): void {
    period = next;
    customRange = null;
    clearBookingSelection();
  }

  function setCategoryFilter(next: CategoryFilter): void {
    categoryFilter = next;
    clearBookingSelection();
  }

  function renameChecking(name: string): void {
    const checking = accounts.find((account) => account.id === 1);
    if (checking !== undefined) {
      checking.name = name;
    }
  }

  function selectAccountID(id: number): void {
    const account = accountById(id);
    if (account !== undefined) {
      selection = { kind: 'account', id };
      markAccountRead(account);
      clearBookingSelection();
    }
  }

  function setQuery(text: string): void {
    query = text;
    clearBookingSelection();
  }

  function setCollapsedAll(): void {
    collapsed = new Set<string>();
    for (const type of accountTypes) {
      for (const account of typeAccounts(type)) {
        collapsed.add(`type:${account.type}`);
      }
    }
    for (const group of groups) {
      collapsed.add(`custom:${group.id}`);
    }
  }

  function selectFirst(count: number): void {
    selectedIDs = new Set(
      visibleTransactions()
        .slice(0, count)
        .map((transaction) => transaction.id),
    );
  }

  function openSpotifyDetail(): void {
    const spotify = transactions.find(
      (transaction) => transaction.partner === 'Spotify' && transaction.date === '2026-08-10',
    );
    if (spotify !== undefined) {
      detailID = spotify.id;
    }
  }

  // The scenarios that open a view before the first interaction.
  switch (scenario) {
    case 'transactions-de-light':
    case 'transactions-de-dark':
      // The German reference shows the short account name; the English one
      // shows the full joint-account name.
      renameChecking('Girokonto');
      break;
    case 'transactions-month-de-light':
      renameChecking('Girokonto');
      setPeriod('this-month');
      break;
    case 'transactions-chart-en':
      selectAccountID(1);
      break;
    case 'transactions-chart-collapsed-en':
      selectAccountID(1);
      chartCollapsed = true;
      break;
    case 'transactions-detail-en':
      selectAccountID(1);
      openSpotifyDetail();
      break;
    case 'transactions-selection-en':
      selectFirst(3);
      break;
    case 'transactions-more-menu-en':
      canUndoReapply = true;
      break;
    case 'transactions-custom-range-en':
      rangeOpen = true;
      rangeFrom = '01.08.2026';
      rangeTo = '31.10.2026';
      rangeError = '';
      break;
    case 'transactions-panel-collapsed-en-dark':
      setCollapsedAll();
      break;
    case 'transactions-from-dashboard-category-en-light':
      setPeriod('this-month');
      setCategoryFilter({ kind: 'only', id: 11 });
      break;
    case 'transactions-add-en':
      openAdd();
      break;
    case 'transactions-duplicate-en':
      showDuplicate(isoDate(2026, 10, 15), -1_200, 'EUR');
      break;
    case 'transactions-multi-delete-en':
      selectFirst(3);
      openMultiDelete();
      break;
    case 'transactions-reapply-en':
      setPeriod('this-month');
      reapplyRules();
      break;
    case 'transactions-rule-dialog-en':
      selectAccountID(1);
      openSpotifyDetail();
      openRule();
      rulePattern = 'Example Market';
      ruleCategoryIndex = 0;
      break;
    case 'transactions-import-csv-en':
    case 'transactions-import-en-light':
    case 'transactions-import-de-dark':
    case 'transactions-import-mt940-en':
    case 'transactions-import-warnings-en':
      openImport();
      break;
    default:
      break;
  }

  return {
    state,
    subscribe: (callback: (dto: TransactionsDTO) => void): (() => void) => {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
    setLanguage,
    setPeriod,
    setCategoryFilter,
    selectAccountID,
    setQuery,
    load: () => state(),
    openWithAdd: () => openAdd(),
    search,
    periodChanged,
    openRange,
    applyRange,
    closeRange,
    categoryChanged,
    directionChanged,
    accountChanged,
    openAmount,
    applyAmount,
    resetAmount,
    closeAmount,
    toggleChart,
    autoCategorize,
    reapplyRules,
    toggleReapplyGroup,
    confirmReapplyRules,
    cancelReapplyRules,
    undoReapply,
    openImport,
    importAccountChanged,
    importCsvChanged,
    confirmImport,
    cancelImport,
    exportCsv,
    attachReceipt,
    openRule,
    closeRule,
    saveRule,
    back,
    openDetail,
    toggleSelect,
    selectRange,
    selectAll,
    clearSelection,
    showPartner,
    openDelete,
    confirmDelete,
    cancelDelete,
    openMultiDelete,
    confirmMultiDelete,
    cancelMultiDelete,
    batchCategory,
    batchTax,
    openAdd,
    addSubmit,
    closeAdd,
    confirmDuplicate,
    dismissDuplicate,
    closeDetail,
    detailCreateRule: () => openRule(),
    detailCategory,
    detailTax,
    detailShowPartner: (partner: string) => showPartner(partner),
    dismissMessage,
    panelSelect: select,
    panelToggle: toggleGroup,
    panelMarkRead: markRead,
    panelNewGroup: newGroup,
    panelRenameGroup: renameGroup,
    panelDeleteGroup: deleteGroup,
    panelMoveGroup: moveGroup,
    panelMoveAccount: moveAccount,
    panelFilterChanged,
    // Read-only accessors used by the Dashboard and Accounts links.
    openAll: (): void => {
      setPeriod('all');
      setCategoryFilter({ kind: 'any' });
      setQuery('');
    },
    openUncategorized: (): void => {
      setPeriod('this-month');
      setCategoryFilter({ kind: 'none' });
    },
    openCategory: (categoryID: number): void => {
      setPeriod('this-month');
      setCategoryFilter({ kind: 'only', id: categoryID });
    },
    openPartner: (partner: string): void => {
      setPeriod('all');
      setCategoryFilter({ kind: 'any' });
      setQuery(partner);
    },
  };
}

export type MockTransactions = ReturnType<typeof createMockTransactions>;
