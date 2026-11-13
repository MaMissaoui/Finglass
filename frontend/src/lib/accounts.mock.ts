import type {
  AccountAmountDTO,
  AccountDaysChoiceDTO,
  AccountDetailRowDTO,
  AccountGroupChoiceDTO,
  AccountPanelRowDTO,
  AccountTypeChoiceDTO,
  AccountsBankCardDTO,
  AccountsDTO,
  AccountsDialogDTO,
  AccountsFigureDTO,
  BankingBankDTO,
  BankAccountRowDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
import { balancePathGeometry, type BalancePoint } from './accountsPath';
import { translate, type Language } from './i18n';

type AccountType = 'checking' | 'savings' | 'credit_card' | 'loan' | 'depot';
type Tone = 'positive' | 'negative' | 'loan' | 'zero' | 'plain';

interface MockBadge {
  logo: string;
  letters: string;
  colour: number;
}

interface MockAccount {
  id: number;
  name: string | null;
  bankId: number | null;
  identifier: string;
  display: string;
  type: AccountType;
  currency: string;
  minor: number;
  lastFetched: string;
  newCount: number;
  groupId: number | null;
  badge: MockBadge;
}

interface MockBank {
  id: number;
  name: string;
  offline: boolean;
  blz: string;
  bic: string;
  collapsed: boolean;
  badge: MockBadge;
}

interface MockGroup {
  id: number;
  name: string;
}

type Selection =
  | { kind: 'all' }
  | { kind: 'group'; groupKind: 'type' | 'custom'; id: number; accountType: string }
  | { kind: 'account'; id: number };

interface MockDialog {
  kind: string;
  title: string;
  text: string;
  errorText: string;
  accountLabel: string;
  accountName: string;
  balance: string;
  accountType: string;
  offline: boolean;
  bankId: number;
  accountId: number;
  connectionName: string;
  connectionBlz: string;
  connectionUserId: string;
  connectionServerUrl: string;
  connectionHasPin: boolean;
  connectionPinText: string;
  connectionSyncing: boolean;
  historyDate: string;
  historyHasStart: boolean;
}

/** The dialog fields the banking fake opens through the accounts state. */
export type MockBankingDialogFields = Partial<MockDialog> & { kind: string };

/** One bank as the banking fake needs it to build its menus. */
export interface MockBankSnapshot {
  id: number;
  name: string;
  offline: boolean;
  blz: string;
  bic: string;
}

const accountTypes: AccountType[] = ['checking', 'savings', 'credit_card', 'loan', 'depot'];
const currencyCodes = ['EUR', 'USD', 'GBP', 'CHF', 'JPY', 'CAD'];
const detailDayWindows = [30, 90, 365];

const bnBadge: MockBadge = { logo: '', letters: 'BN', colour: 2 };
const sparkasseBadge: MockBadge = { logo: 'sparkasse', letters: 'S', colour: 0 };
const noBadge: MockBadge = { logo: '', letters: '', colour: 0 };

function emptyDialog(): MockDialog {
  return {
    kind: '',
    title: '',
    text: '',
    errorText: '',
    accountLabel: '',
    accountName: '',
    balance: '',
    accountType: '',
    offline: false,
    bankId: 0,
    accountId: 0,
    connectionName: '',
    connectionBlz: '',
    connectionUserId: '',
    connectionServerUrl: '',
    connectionHasPin: false,
    connectionPinText: '',
    connectionSyncing: false,
    historyDate: '',
    historyHasStart: false,
  };
}

function sampleAccounts(): MockAccount[] {
  return [
    {
      id: 1,
      name: 'Haushalt',
      bankId: 1,
      identifier: 'DE89 3704 0044 0532 0130 00',
      display: 'DE89 3704 0044 0532 0130 00',
      type: 'checking',
      currency: 'EUR',
      minor: 245037,
      lastFetched: '2026-11-12',
      newCount: 1,
      groupId: null,
      badge: bnBadge,
    },
    {
      id: 2,
      name: 'Dollar',
      bankId: 1,
      identifier: 'DE44 5001 0517 5407 3249 31',
      display: 'DE44 5001 0517 5407 3249 31',
      type: 'savings',
      currency: 'USD',
      minor: 50000,
      lastFetched: '',
      newCount: 0,
      groupId: 1,
      badge: bnBadge,
    },
    {
      id: 3,
      name: 'Festgeld',
      bankId: 1,
      identifier: 'DE89 3704 0044 0532 0139 99',
      display: 'DE89 3704 0044 0532 0139 99',
      type: 'savings',
      currency: 'EUR',
      minor: 100,
      lastFetched: '2026-11-12',
      newCount: 0,
      groupId: null,
      badge: bnBadge,
    },
    {
      id: 4,
      name: 'Girokonto',
      bankId: 2,
      identifier: 'DE89 3704 0044 0532 0130 01',
      display: 'DE89 3704 0044 0532 0130 01',
      type: 'checking',
      currency: 'EUR',
      minor: 312540,
      lastFetched: '2026-11-12',
      newCount: 0,
      groupId: null,
      badge: sparkasseBadge,
    },
    {
      id: 5,
      name: 'Tagesgeld',
      bankId: 2,
      identifier: 'DE44 5001 0517 5407 3249 32',
      display: 'DE44 5001 0517 5407 3249 32',
      type: 'checking',
      currency: 'EUR',
      minor: 800000,
      lastFetched: '2026-11-12',
      newCount: 0,
      groupId: null,
      badge: sparkasseBadge,
    },
    {
      id: 6,
      name: null,
      bankId: null,
      identifier: 'DE44 5001 0517 5407 3249 30',
      display: 'DE44 5001 0517 5407 3249 30',
      type: 'loan',
      currency: 'EUR',
      minor: -500000,
      lastFetched: '',
      newCount: 0,
      groupId: null,
      badge: bnBadge,
    },
    {
      id: 7,
      name: null,
      bankId: null,
      identifier: '4321',
      display: '•••• 4321',
      type: 'credit_card',
      currency: 'EUR',
      minor: -4510,
      lastFetched: '',
      newCount: 0,
      groupId: null,
      badge: noBadge,
    },
  ];
}

function sampleBanks(): MockBank[] {
  return [
    {
      id: 1,
      name: 'Bank Norwegian',
      offline: true,
      blz: '',
      bic: '',
      collapsed: false,
      badge: bnBadge,
    },
    {
      id: 2,
      name: 'Sparkasse',
      offline: false,
      blz: '37040044',
      bic: 'COLSDE33XXX',
      collapsed: false,
      badge: sparkasseBadge,
    },
  ];
}

function sampleGroups(): MockGroup[] {
  return [{ id: 1, name: 'Reisen' }];
}

function digitsOf(account: MockAccount): string {
  const source = account.identifier.replace(/\s/gu, '');
  const characters = Array.from(source);
  return characters.slice(Math.max(0, characters.length - 4)).join('');
}

function lastFour(account: MockAccount): string {
  return digitsOf(account);
}

function accountLabel(account: MockAccount, language: Language): string {
  const name = account.name?.trim() ?? '';
  if (name !== '') {
    return name;
  }
  const type = translate(language, typeKey(account.type));
  const digits = lastFour(account);
  return digits === '' ? type : `${type} …${digits}`;
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

function formatAmount(minor: number, currency: string, language: Language, signed = false): string {
  const locale = language === 'de' ? 'de-DE' : language === 'fr' ? 'fr-FR' : 'en-US';
  const value = minor / 100;
  const formatted = new Intl.NumberFormat(locale, {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(Math.abs(value));
  if (minor < 0) {
    return `-${formatted}`;
  }
  if (signed && minor > 0) {
    return `+${formatted}`;
  }
  return formatted;
}

function formatWhole(minor: number, currency: string, language: Language): string {
  const locale = language === 'de' ? 'de-DE' : language === 'fr' ? 'fr-FR' : 'en-US';
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(minor / 100);
}

function formatPlain(minor: number): string {
  return (minor / 100).toFixed(2);
}

function formatDate(iso: string): string {
  if (iso === '') {
    return '';
  }
  const parts = iso.split('-');
  const [year, month, day] = parts;
  if (year === undefined || month === undefined || day === undefined) {
    return iso;
  }
  return `${day}.${month}.${year}`;
}

function parseBalance(text: string): number | null {
  let value = text.trim();
  if (value === '' || !/^[+-]?[\d.,]+$/u.test(value)) {
    return null;
  }
  let negative = false;
  if (value.startsWith('-')) {
    negative = true;
    value = value.slice(1);
  } else if (value.startsWith('+')) {
    value = value.slice(1);
  }
  if (value === '') {
    return null;
  }
  const comma = value.lastIndexOf(',');
  const dot = value.lastIndexOf('.');
  let separator = '';
  if (comma >= 0 && dot >= 0) {
    separator = comma > dot ? ',' : '.';
  } else if (comma >= 0) {
    separator = value.length - comma - 1 <= 2 ? ',' : '';
  } else if (dot >= 0) {
    separator = value.length - dot - 1 <= 2 ? '.' : '';
  }

  let integerPart = value;
  let fractionPart = '';
  if (separator !== '') {
    const index = value.lastIndexOf(separator);
    integerPart = value.slice(0, index);
    fractionPart = value.slice(index + 1);
  }
  const integerDigits = integerPart.replace(/[.,]/gu, '');
  if (!/^\d+$/u.test(integerDigits) || (fractionPart !== '' && !/^\d{1,2}$/u.test(fractionPart))) {
    return null;
  }
  const minor = Number.parseInt(integerDigits, 10) * 100;
  const fraction = fractionPart === '' ? 0 : Number.parseInt(fractionPart.padEnd(2, '0'), 10);
  const total = minor + fraction;
  return negative ? -total : total;
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

function balanceTone(account: MockAccount): Tone {
  if (account.minor > 0) {
    return 'positive';
  }
  if (account.minor === 0) {
    return 'zero';
  }
  return account.type === 'loan' ? 'loan' : 'negative';
}

function currencyTotals(accounts: MockAccount[], language: Language): AccountAmountDTO[] {
  const totals = new Map<string, number>();
  for (const account of accounts) {
    totals.set(account.currency, (totals.get(account.currency) ?? 0) + account.minor);
  }
  const ordered = [...totals.entries()].sort((left, right) => {
    if (left[0] === 'EUR') {
      return -1;
    }
    if (right[0] === 'EUR') {
      return 1;
    }
    return left[0].localeCompare(right[0]);
  });
  const plain = ordered.length > 1;
  return ordered.map(([currency, minor]) => ({
    text: formatAmount(minor, currency, language),
    tone: plain ? 'plain' : balanceToneFor(accounts, currency),
  }));
}

function sameBadge(left: MockBadge, right: MockBadge): boolean {
  return left.logo === right.logo && left.letters === right.letters && left.colour === right.colour;
}

function groupBadge(accounts: MockAccount[]): MockBadge {
  const first = accounts[0];
  if (first === undefined) {
    return noBadge;
  }
  for (const account of accounts.slice(1)) {
    if (!sameBadge(first.badge, account.badge)) {
      return noBadge;
    }
  }
  return first.badge;
}

function matchesFilter(account: MockAccount, bank: MockBank | undefined, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === '') {
    return true;
  }
  const name = account.name?.toLowerCase() ?? '';
  const bankName = bank?.name.toLowerCase() ?? '';
  return (
    name.includes(needle) ||
    account.identifier.toLowerCase().includes(needle) ||
    bankName.includes(needle)
  );
}

interface HistorySpec {
  points: BalancePoint[];
  income: number;
  expenses: number;
  rows: AccountDetailRowDTO[];
}

function historyFor(account: MockAccount, language: Language): HistorySpec {
  if (account.id === 1) {
    const values = [
      -38300,
      -38300,
      -38300,
      -38300,
      -38300,
      -38300,
      -38300,
      131500,
      131500,
      125000,
      125000,
      125000,
      249700,
      account.minor,
    ];
    return {
      points: values.map((minor) => ({ minor })),
      income: 300000,
      expenses: 16700,
      rows: [
        {
          dateText: formatDate('2026-10-09'),
          title: 'Example Market',
          amountText: formatAmount(-3200, account.currency, language, true),
          expense: true,
        },
        {
          dateText: formatDate('2026-10-05'),
          title: 'Corner Kiosk',
          amountText: formatAmount(-1500, account.currency, language, true),
          expense: true,
        },
        {
          dateText: formatDate('2026-10-02'),
          title: 'Employer GmbH',
          amountText: formatAmount(150000, account.currency, language, true),
          expense: false,
        },
        {
          dateText: formatDate('2026-09-12'),
          title: 'Example Market',
          amountText: formatAmount(-12000, account.currency, language, true),
          expense: true,
        },
        {
          dateText: formatDate('2026-09-02'),
          title: 'Employer GmbH',
          amountText: formatAmount(150000, account.currency, language, true),
          expense: false,
        },
      ],
    };
  }

  return {
    points: [0, 1, 2, 3, 4].map(() => ({ minor: account.minor })),
    income: 0,
    expenses: 0,
    rows: [],
  };
}

/** The stateful accounts fake behind the mock build's Accounts page. */
export function createMockAccounts(scenario: string, initialLanguage: Language) {
  const empty = scenario === 'accounts-empty';
  let language = initialLanguage;
  let banks: MockBank[] = empty ? [] : sampleBanks();
  let accounts: MockAccount[] = empty ? [] : sampleAccounts();
  let groups: MockGroup[] = empty ? [] : sampleGroups();
  let selection: Selection = { kind: 'all' };
  const collapsedGroups = new Set<string>();
  let filterText = '';
  let errorText = '';
  let messageText = '';
  let dialog = emptyDialog();
  let detailAccountId: number | null = null;
  let detailDays = 90;
  let nextBankId = 3;
  let nextAccountId = 8;
  let nextGroupId = 2;
  // The banking fake fills the online fields of the bank cards, as the Go
  // AccountsDTO's applyBankingCard does.
  let bankingSource: ((bankId: number) => BankingBankDTO | undefined) | null = null;

  const listeners = new Set<(dto: AccountsDTO) => void>();

  function bankFor(account: MockAccount): MockBank | undefined {
    return account.bankId === null ? undefined : banks.find((bank) => bank.id === account.bankId);
  }

  function accountById(id: number): MockAccount | undefined {
    return accounts.find((account) => account.id === id);
  }

  function bankById(id: number): MockBank | undefined {
    return banks.find((bank) => bank.id === id);
  }

  function typeAccounts(type: string): MockAccount[] {
    return accounts.filter((account) => account.groupId === null && account.type === type);
  }

  function customAccounts(groupId: number): MockAccount[] {
    return accounts.filter((account) => account.groupId === groupId);
  }

  function groupAccounts(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): MockAccount[] {
    return groupKind === 'custom' ? customAccounts(id) : typeAccounts(accountType);
  }

  function filteredAccounts(): MockAccount[] {
    return accounts.filter((account) => matchesFilter(account, bankFor(account), filterText));
  }

  function isSelected(
    kind: 'all' | 'group' | 'account',
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): boolean {
    if (selection.kind !== kind) {
      return false;
    }
    if (selection.kind === 'group' && kind === 'group') {
      return (
        selection.groupKind === groupKind &&
        selection.accountType === accountType &&
        (groupKind === 'custom' ? selection.id === id : true)
      );
    }
    if (selection.kind === 'account' && kind === 'account') {
      return selection.id === id;
    }
    return kind === 'all';
  }

  function groupCollapsed(groupKind: 'type' | 'custom', id: number, accountType: string): boolean {
    return collapsedGroups.has(groupKind === 'custom' ? `custom:${id}` : `type:${accountType}`);
  }

  function buildFigures(): AccountsFigureDTO[] {
    const figures: AccountsFigureDTO[] = [];
    if (accounts.length > 0) {
      const loans = accounts.filter((account) => account.type === 'loan');
      const label = translate(
        language,
        loans.length > 0 ? 'AccountsNetLabel' : 'AccountsTotalLabel',
        accounts.length,
      );
      figures.push({
        label,
        values: currencyTotals(accounts, language),
        secondary: false,
      });
      if (loans.length > 0) {
        figures.push({
          label: translate(language, 'AccountsLoansLabel'),
          values: loans.map((account) => ({
            text: formatAmount(account.minor, account.currency, language),
            tone: 'plain' as Tone,
          })),
          secondary: true,
        });
      }
    }
    return figures;
  }

  function bankRows(bank: MockBank): MockAccount[] {
    return accounts.filter((account) => account.bankId === bank.id);
  }

  function accountRow(account: MockAccount): BankAccountRowDTO {
    const type = translate(language, typeKey(account.type));
    let subtitle = account.display;
    if (account.name !== null) {
      subtitle = account.display === '' ? type : `${type} · ${account.display}`;
    }
    if (account.lastFetched !== '') {
      const fetched = translate(language, 'LastFetched', formatDate(account.lastFetched));
      subtitle = subtitle === '' ? fetched : `${subtitle} · ${fetched}`;
    }
    return {
      accountId: account.id,
      name: accountLabel(account, language),
      subtitle,
      balanceText: formatAmount(account.minor, account.currency, language),
      tone: balanceTone(account),
      newCount: account.newCount,
      logo: account.badge.logo,
      logoLetters: account.badge.letters,
      logoColour: account.badge.colour,
    };
  }

  function bankCard(bank: MockBank): AccountsBankCardDTO {
    const rows = bankRows(bank);
    const online = bankingSource?.(bank.id);
    return {
      bankId: bank.id,
      name: bank.name,
      subtitle: bank.offline
        ? translate(language, 'BankOfflineHint')
        : `${translate(language, 'BlzPrefix', bank.blz)} · ${translate(language, 'BicPrefix', bank.bic)}`,
      centralBlzHint: '',
      offline: bank.offline,
      collapsed: bank.collapsed,
      accountCountText: translate(language, 'AccountCount', rows.length),
      totals: currencyTotals(rows, language),
      accounts: rows.map(accountRow),
      syncing: online?.syncing ?? false,
      syncStep: online?.syncStep ?? '',
      syncEnabled: online?.syncEnabled ?? false,
      syncTooltip: online?.syncTooltip ?? '',
      pinText: online?.pinText ?? '',
      menu: online?.menu ?? [],
      logo: bank.badge.logo,
      logoLetters: bank.badge.letters,
      logoColour: bank.badge.colour,
    };
  }

  function withoutBankCard(): AccountsBankCardDTO {
    const rows = accounts.filter((account) => account.bankId === null);
    return {
      bankId: -1,
      name: translate(language, 'AccountsWithoutBank'),
      subtitle: '',
      centralBlzHint: '',
      offline: false,
      collapsed: false,
      accountCountText: translate(language, 'AccountCount', rows.length),
      totals: currencyTotals(rows, language),
      accounts: rows.map(accountRow),
      syncing: false,
      syncStep: '',
      syncEnabled: false,
      syncTooltip: '',
      pinText: '',
      menu: [],
      logo: '',
      logoLetters: '',
      logoColour: 0,
    };
  }

  function bankCards(): AccountsBankCardDTO[] {
    const cards = banks.map(bankCard);
    if (accounts.some((account) => account.bankId === null)) {
      cards.push(withoutBankCard());
    }
    return cards;
  }

  function allRow(): AccountPanelRowDTO {
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

  function filteredGroupAccounts(
    groupKind: 'type' | 'custom',
    id: number,
    accountType: string,
  ): MockAccount[] {
    const matching = filteredAccounts();
    return groupAccounts(groupKind, id, accountType).filter((account) =>
      matching.some((visible) => visible.id === account.id),
    );
  }

  function groupRow(
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
      selected: isSelected('group', groupKind, id, accountType),
      collapsed: groupCollapsed(groupKind, id, accountType),
      newCount,
      totals: currencyTotals(members, language),
      indent: false,
      logo: groupKind === 'custom' ? '' : badge.logo,
      logoLetters: groupKind === 'custom' ? '' : badge.letters,
      logoColour: groupKind === 'custom' ? 0 : badge.colour,
    };
  }

  function panelAccountRow(account: MockAccount): AccountPanelRowDTO {
    const secondary =
      account.name !== null && lastFour(account) !== '' ? `…${lastFour(account)}` : '';
    const detail =
      account.lastFetched === ''
        ? ''
        : translate(language, 'LastFetched', formatDate(account.lastFetched));
    const groupMembers =
      account.groupId !== null ? customAccounts(account.groupId) : typeAccounts(account.type);
    return {
      kind: 'account',
      groupKind: '',
      accountType: account.type,
      id: account.id,
      label: accountLabel(account, language),
      secondary,
      detail,
      name: '',
      deleteText: '',
      selected: selection.kind === 'account' && selection.id === account.id,
      collapsed: false,
      newCount: account.newCount,
      totals: [
        {
          text: formatAmount(account.minor, account.currency, language),
          tone: balanceToneFor(groupMembers, account.currency),
        },
      ],
      indent: true,
      logo: account.badge.logo,
      logoLetters: account.badge.letters,
      logoColour: account.badge.colour,
    };
  }

  function panelRows(): AccountPanelRowDTO[] {
    const rows: AccountPanelRowDTO[] = [allRow()];
    const filtering = filterText.trim() !== '';
    for (const type of accountTypes) {
      const members = groupAccounts('type', -1, type);
      if (members.length === 0) {
        continue;
      }
      const matching = filteredGroupAccounts('type', -1, type);
      if (filtering && matching.length === 0) {
        continue;
      }
      rows.push(groupRow('type', -1, type, ''));
      if (groupCollapsed('type', -1, type)) {
        continue;
      }
      for (const account of matching) {
        rows.push(panelAccountRow(account));
      }
    }
    for (const group of groups) {
      const matching = filteredGroupAccounts('custom', group.id, '');
      if (filtering && matching.length === 0) {
        continue;
      }
      rows.push(groupRow('custom', group.id, '', group.name));
      if (groupCollapsed('custom', group.id, '')) {
        continue;
      }
      for (const account of matching) {
        rows.push(panelAccountRow(account));
      }
    }
    return rows;
  }

  function groupChoices(): AccountGroupChoiceDTO[] {
    return groups.map((group) => ({ id: group.id, name: group.name }));
  }

  function shownText(): string {
    if (filterText.trim() === '') {
      return '';
    }
    return translate(language, 'AccountsShown', filteredAccounts().length, accounts.length);
  }

  function typeChoices(): AccountTypeChoiceDTO[] {
    return accountTypes.map((code) => ({
      code,
      label: translate(language, typeKey(code)),
    }));
  }

  function daysChoices(): AccountDaysChoiceDTO[] {
    return detailDayWindows.map((days) => ({
      days,
      label: days === 365 ? translate(language, 'OneYear') : translate(language, 'DaysCount', days),
    }));
  }

  function dayLabels(): string[] {
    return daysChoices().map((choice) => choice.label);
  }

  function detailDTO(): Partial<AccountsDTO> {
    if (detailAccountId === null) {
      return {
        detailOpen: false,
        detailRows: [],
        detailDays: 90,
      };
    }
    const account = accountById(detailAccountId);
    if (account === undefined) {
      return { detailOpen: false, detailRows: [], detailDays: 90 };
    }
    const bank = bankFor(account);
    const label = accountLabel(account, language);
    const history = historyFor(account, language);
    const geometry = balancePathGeometry(history.points);
    const dailyIncome = formatAmount(history.income, account.currency, language);
    const dailyExpenses = formatAmount(history.expenses, account.currency, language);
    return {
      detailOpen: true,
      detailTitle: bank === undefined ? label : `${bank.name} · ${label}`,
      detailLogo: account.badge.logo,
      detailLetters: account.badge.letters,
      detailColour: account.badge.colour,
      detailSubtitle: account.display,
      detailBalance: formatAmount(account.minor, account.currency, language),
      detailTone: balanceTone(account),
      detailAccountId: account.id,
      detailDays,
      detailSummary: translate(language, 'LastDaysSummary', detailDays, dailyIncome, dailyExpenses),
      detailPath: geometry.path,
      detailAxisTop: formatWhole(geometry.highMinor, account.currency, language),
      detailAxisBottom:
        geometry.highMinor === geometry.lowMinor
          ? ''
          : formatWhole(geometry.lowMinor, account.currency, language),
      detailEndX: geometry.endX,
      detailEndY: geometry.endY,
      detailEndLabel: formatAmount(account.minor, account.currency, language),
      detailRows: history.rows,
    };
  }

  function dialogDTO(): AccountsDialogDTO {
    return { ...dialog };
  }

  function hasContent(): boolean {
    return banks.length > 0 || accounts.length > 0;
  }

  function state(): AccountsDTO {
    const detail = detailDTO();
    return {
      loading: false,
      showContent: hasContent(),
      hasContent: hasContent(),
      errorText,
      messageText,
      figures: buildFigures(),
      bankCards: bankCards(),
      panelRows: panelRows(),
      groupChoices: groupChoices(),
      filterText,
      shownText: shownText(),
      typeChoices: typeChoices(),
      currencyCodes: [...currencyCodes],
      daysChoices: daysChoices(),
      dayLabels: dayLabels(),
      dialog: dialogDTO(),
      detailOpen: detail.detailOpen ?? false,
      detailTitle: detail.detailTitle ?? '',
      detailLogo: detail.detailLogo ?? '',
      detailLetters: detail.detailLetters ?? '',
      detailColour: detail.detailColour ?? 0,
      detailSubtitle: detail.detailSubtitle ?? '',
      detailBalance: detail.detailBalance ?? '',
      detailTone: detail.detailTone ?? '',
      detailAccountId: detail.detailAccountId ?? 0,
      detailDays: detail.detailDays ?? 90,
      detailSummary: detail.detailSummary ?? '',
      detailPath: detail.detailPath ?? '',
      detailAxisTop: detail.detailAxisTop ?? '',
      detailAxisBottom: detail.detailAxisBottom ?? '',
      detailEndX: detail.detailEndX ?? 0,
      detailEndY: detail.detailEndY ?? 0,
      detailEndLabel: detail.detailEndLabel ?? '',
      detailRows: detail.detailRows ?? [],
    };
  }

  function publish(): AccountsDTO {
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

  function closeDialog(): void {
    dialog = emptyDialog();
  }

  /** Opens a banking-owned dialog (connection, history or add-bank). */
  function openBankingDialog(fields: MockBankingDialogFields): AccountsDTO {
    clearMessages();
    dialog = { ...emptyDialog(), ...fields };
    return publish();
  }

  /** Shows a validation error inside the open dialog, as the session does. */
  function setDialogError(text: string): AccountsDTO {
    if (dialog.kind === '') {
      return state();
    }
    dialog.errorText = text;
    return publish();
  }

  /** Adds an online bank the banking fake stored. */
  function addOnlineBank(id: number, name: string, blz: string, bic: string): AccountsDTO {
    if (bankById(id) !== undefined) {
      return state();
    }
    banks = [
      ...banks,
      {
        id,
        name,
        offline: false,
        blz,
        bic,
        collapsed: false,
        badge: { logo: '', letters: name.slice(0, 2).toUpperCase(), colour: 0 },
      },
    ];
    if (id >= nextBankId) {
      nextBankId = id + 1;
    }
    return publish();
  }

  /** Removes a bank the banking fake deleted, with its accounts. */
  function removeBankById(id: number): AccountsDTO {
    banks = banks.filter((bank) => bank.id !== id);
    accounts = accounts.filter((account) => account.bankId !== id);
    if (detailAccountId !== null && accountById(detailAccountId) === undefined) {
      detailAccountId = null;
    }
    if (selection.kind === 'account' && accountById(selection.id) === undefined) {
      selection = { kind: 'all' };
    }
    return publish();
  }

  function setBankingSource(source: (bankId: number) => BankingBankDTO | undefined): void {
    bankingSource = source;
  }

  function listBanks(): MockBankSnapshot[] {
    return banks.map((bank) => ({
      id: bank.id,
      name: bank.name,
      offline: bank.offline,
      blz: bank.blz,
      bic: bank.bic,
    }));
  }

  /** Re-renders the cards after the banking state changed. */
  function republish(): AccountsDTO {
    return publish();
  }

  function openAddBank(): AccountsDTO {
    clearMessages();
    dialog = { ...emptyDialog(), kind: 'add-bank' };
    return publish();
  }

  function openAddAccount(bankId: number): AccountsDTO {
    const bank = bankById(bankId);
    if (bank === undefined) {
      return state();
    }
    clearMessages();
    dialog = {
      ...emptyDialog(),
      kind: 'add-account',
      title: translate(language, 'AddAccountAt', bank.name),
      accountType: bank.offline ? 'credit_card' : 'checking',
      offline: bank.offline,
      bankId,
      accountId: -1,
      balance: '0',
    };
    return publish();
  }

  function openBalance(accountId: number): AccountsDTO {
    const account = accountById(accountId);
    if (account === undefined) {
      return state();
    }
    clearMessages();
    dialog = {
      ...emptyDialog(),
      kind: 'balance',
      accountId,
      accountLabel: account.display,
      balance: formatPlain(account.minor),
      accountType: account.type,
    };
    return publish();
  }

  function openRename(accountId: number): AccountsDTO {
    const account = accountById(accountId);
    if (account === undefined) {
      return state();
    }
    clearMessages();
    dialog = {
      ...emptyDialog(),
      kind: 'rename',
      accountId,
      accountName: account.name ?? '',
    };
    return publish();
  }

  function openDeleteAccount(accountId: number): AccountsDTO {
    const account = accountById(accountId);
    if (account === undefined) {
      return state();
    }
    clearMessages();
    dialog = {
      ...emptyDialog(),
      kind: 'delete-account',
      title: translate(language, 'DeleteAccountTitle'),
      text: translate(language, 'DeleteAccountText', account.display),
      accountId,
    };
    return publish();
  }

  function openDeleteBank(bankId: number): AccountsDTO {
    const bank = bankById(bankId);
    if (bank === undefined) {
      return state();
    }
    clearMessages();
    dialog = {
      ...emptyDialog(),
      kind: 'delete-bank',
      title: translate(language, 'RemoveBankTitle', bank.name),
      text: translate(language, 'RemoveBankText'),
      bankId,
    };
    return publish();
  }

  function submitAddBank(name: string): AccountsDTO {
    if (name.trim() === '') {
      dialog.errorText = translate(language, 'BankNameRequired');
      return publish();
    }
    const clean = name.trim();
    banks = [
      ...banks,
      {
        id: nextBankId,
        name: clean,
        offline: true,
        blz: '',
        bic: '',
        collapsed: false,
        badge: { logo: '', letters: clean.slice(0, 2).toUpperCase(), colour: 0 },
      },
    ];
    nextBankId += 1;
    closeDialog();
    errorText = '';
    messageText = translate(language, 'MessageAdded', clean);
    return publish();
  }

  function submitAddAccount(
    bankId: number,
    name: string,
    identifier: string,
    accountType: string,
    currency: string,
    balanceText: string,
  ): AccountsDTO {
    const bank = bankById(bankId);
    const offline = bank?.offline ?? false;
    const normalized = identifier.replace(/\s/gu, '');
    const duplicate = accounts.some(
      (account) =>
        account.identifier.replace(/\s/gu, '') === normalized ||
        (offline && lastFour(account) === normalized),
    );
    if (normalized === '') {
      dialog.errorText = translate(language, 'IbanRequired');
      return publish();
    }
    if (duplicate) {
      dialog.errorText = translate(language, 'AccountIbanExists');
      return publish();
    }
    if (offline) {
      if (!/^\d{4}$/u.test(normalized)) {
        dialog.errorText = translate(language, 'CardLast4Only');
        return publish();
      }
    } else if (!/^[A-Z]{2}\d{2}[A-Z0-9]{10,30}$/u.test(normalized)) {
      dialog.errorText = translate(language, 'IbanChecksumFailed');
      return publish();
    }
    const minor = parseBalance(balanceText);
    if (minor === null) {
      dialog.errorText = translate(language, 'BalanceNotANumber');
      return publish();
    }
    if (!accountTypes.includes(accountType as AccountType)) {
      dialog.errorText = translate(language, 'ErrorUnexpected');
      return publish();
    }
    const cleanName = name.trim() === '' ? null : name.trim();
    const badge = bank?.offline === true ? bank.badge : (bank?.badge ?? noBadge);
    accounts = [
      ...accounts,
      {
        id: nextAccountId,
        name: cleanName,
        bankId,
        identifier,
        display: offline && !identifier.includes(' ') ? `•••• ${normalized}` : identifier,
        type: accountType as AccountType,
        currency,
        minor,
        lastFetched: '',
        newCount: 0,
        groupId: null,
        badge,
      },
    ];
    nextAccountId += 1;
    closeDialog();
    errorText = '';
    messageText = translate(language, 'MessageAccountAdded');
    return publish();
  }

  function submitBalance(accountId: number, balanceText: string, accountType: string): AccountsDTO {
    const account = accountById(accountId);
    if (account === undefined) {
      return state();
    }
    const minor = parseBalance(balanceText);
    if (minor === null) {
      dialog.errorText = translate(language, 'EnterValidBalance');
      return publish();
    }
    accounts = accounts.map((entry) =>
      entry.id === accountId ? { ...entry, minor, type: accountType as AccountType } : entry,
    );
    closeDialog();
    errorText = '';
    return publish();
  }

  function submitRename(accountId: number, name: string): AccountsDTO {
    const clean = name.trim();
    accounts = accounts.map((entry) =>
      entry.id === accountId ? { ...entry, name: clean === '' ? null : clean } : entry,
    );
    closeDialog();
    errorText = '';
    return publish();
  }

  function confirmDeleteAccount(accountId: number): AccountsDTO {
    accounts = accounts.filter((entry) => entry.id !== accountId);
    if (detailAccountId === accountId) {
      detailAccountId = null;
    }
    if (selection.kind === 'account' && selection.id === accountId) {
      selection = { kind: 'all' };
    }
    closeDialog();
    errorText = '';
    messageText = translate(language, 'MessageAccountDeleted');
    return publish();
  }

  function confirmDeleteBank(bankId: number): AccountsDTO {
    const bank = bankById(bankId);
    if (bank === undefined || !bank.offline) {
      closeDialog();
      return publish();
    }
    banks = banks.filter((entry) => entry.id !== bankId);
    accounts = accounts.filter((entry) => entry.bankId !== bankId);
    if (detailAccountId !== null && accountById(detailAccountId) === undefined) {
      detailAccountId = null;
    }
    if (selection.kind === 'account' && accountById(selection.id) === undefined) {
      selection = { kind: 'all' };
    }
    closeDialog();
    errorText = '';
    messageText = translate(language, 'MessageBankRemoved');
    return publish();
  }

  function openDetail(accountId: number): AccountsDTO {
    if (accountById(accountId) === undefined) {
      return state();
    }
    detailAccountId = accountId;
    detailDays = 90;
    return publish();
  }

  function selectDays(accountId: number, days: number): AccountsDTO {
    detailAccountId = accountId;
    detailDays = days;
    return publish();
  }

  function closeDetail(): AccountsDTO {
    detailAccountId = null;
    return publish();
  }

  function filterChanged(text: string): AccountsDTO {
    filterText = text;
    return publish();
  }

  function dismissMessage(): AccountsDTO {
    clearMessages();
    return publish();
  }

  function parseSelection(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Selection | null {
    if (kind === 'all') {
      return { kind: 'all' };
    }
    if (kind === 'group' && (groupKind === 'custom' || groupKind === 'type')) {
      return { kind: 'group', groupKind, id, accountType };
    }
    if (kind === 'account') {
      return { kind: 'account', id };
    }
    return null;
  }

  function select(kind: string, groupKind: string, id: number, accountType: string): AccountsDTO {
    const parsed = parseSelection(kind, groupKind, id, accountType);
    if (parsed !== null) {
      selection = parsed;
    }
    return publish();
  }

  function toggleGroup(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): AccountsDTO {
    const parsed = parseSelection(kind, groupKind, id, accountType);
    if (parsed === null || parsed.kind !== 'group') {
      return state();
    }
    const key = parsed.groupKind === 'custom' ? `custom:${parsed.id}` : `type:${accountType}`;
    if (collapsedGroups.has(key)) {
      collapsedGroups.delete(key);
    } else {
      collapsedGroups.add(key);
    }
    return publish();
  }

  function markRead(kind: string, groupKind: string, id: number, accountType: string): AccountsDTO {
    const parsed = parseSelection(kind, groupKind, id, accountType);
    if (parsed === null) {
      return state();
    }
    const mark = (account: MockAccount): MockAccount => ({ ...account, newCount: 0 });
    if (parsed.kind === 'all') {
      accounts = accounts.map(mark);
    } else if (parsed.kind === 'group') {
      const members = groupAccounts(parsed.groupKind, parsed.id, parsed.accountType);
      const ids = new Set(members.map((account) => account.id));
      accounts = accounts.map((account) => (ids.has(account.id) ? mark(account) : account));
    } else {
      accounts = accounts.map((account) => (account.id === parsed.id ? mark(account) : account));
    }
    return publish();
  }

  function toggleBank(bankId: number): AccountsDTO {
    banks = banks.map((bank) =>
      bank.id === bankId ? { ...bank, collapsed: !bank.collapsed } : bank,
    );
    return publish();
  }

  function forgetDeleted(accountId: number): AccountsDTO {
    if (accountById(accountId) === undefined) {
      return state();
    }
    errorText = '';
    messageText = translate(language, 'MessageNoDeletedRemembered');
    return publish();
  }

  function newGroup(name: string): AccountsDTO {
    const clean = name.trim();
    if (clean === '') {
      errorText = translate(language, 'ErrorNameRequired');
      return publish();
    }
    groups = [...groups, { id: nextGroupId, name: clean }];
    nextGroupId += 1;
    errorText = '';
    return publish();
  }

  function renameGroup(id: number, name: string): AccountsDTO {
    const clean = name.trim();
    if (clean === '') {
      errorText = translate(language, 'ErrorNameRequired');
      return publish();
    }
    groups = groups.map((group) => (group.id === id ? { ...group, name: clean } : group));
    errorText = '';
    return publish();
  }

  function deleteGroup(id: number): AccountsDTO {
    groups = groups.filter((group) => group.id !== id);
    accounts = accounts.map((account) =>
      account.groupId === id ? { ...account, groupId: null } : account,
    );
    if (selection.kind === 'group' && selection.groupKind === 'custom' && selection.id === id) {
      selection = { kind: 'all' };
    }
    return publish();
  }

  function moveGroup(id: number, up: boolean): AccountsDTO {
    const index = groups.findIndex((group) => group.id === id);
    if (index < 0) {
      return state();
    }
    const target = up ? index - 1 : index + 1;
    if (target < 0 || target >= groups.length) {
      return publish();
    }
    const reordered = [...groups];
    const current = reordered[index];
    const neighbour = reordered[target];
    if (current === undefined || neighbour === undefined) {
      return publish();
    }
    reordered[index] = neighbour;
    reordered[target] = current;
    groups = reordered;
    return publish();
  }

  function moveAccount(accountId: number, groupId: number): AccountsDTO {
    accounts = accounts.map((account) =>
      account.id === accountId ? { ...account, groupId: groupId < 0 ? null : groupId } : account,
    );
    return publish();
  }

  function setLanguage(next: Language): AccountsDTO {
    language = next;
    return publish();
  }

  // The scenarios that open a view before the first interaction.
  if (scenario === 'accounts-panel') {
    collapsedGroups.add('type:checking');
  }
  if (scenario === 'accounts-add-account') {
    openAddAccount(1);
    listeners.clear();
  } else if (scenario === 'accounts-detail') {
    openDetail(1);
    listeners.clear();
  } else if (scenario === 'accounts-detail-flat-de') {
    openDetail(3);
    listeners.clear();
  } else if (scenario.startsWith('accounts-add-online-bank')) {
    openAddBank();
    if (scenario === 'accounts-add-online-bank') {
      dialog.errorText = translate(language, 'BlzMustBe8Digits');
    }
    listeners.clear();
  }

  return {
    state,
    load: () => state(),
    openAddBank,
    closeDialog: (): AccountsDTO => {
      dialog.errorText = '';
      dialog.kind = '';
      return publish();
    },
    submitAddBank,
    openAddAccount,
    submitAddAccount,
    openBalance,
    submitBalance,
    openRename,
    submitRename,
    openDeleteAccount,
    confirmDeleteAccount,
    openDeleteBank,
    confirmDeleteBank,
    openDetail,
    selectDays,
    closeDetail,
    filterChanged,
    dismissMessage,
    select,
    toggleGroup,
    markRead,
    toggleBank,
    forgetDeleted,
    newGroup,
    renameGroup,
    deleteGroup,
    moveGroup,
    moveAccount,
    setLanguage,
    setBankingSource,
    listBanks,
    republish,
    openBankingDialog,
    setDialogError,
    addOnlineBank,
    removeBankById,
    subscribe: (callback: (dto: AccountsDTO) => void): (() => void) => {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
  };
}

export type MockAccounts = ReturnType<typeof createMockAccounts>;
