import type {
  ReportAmountRowDTO,
  ReportCategoryRowDTO,
  ReportMonthlyRowDTO,
  ReportRecurringRowDTO,
  ReportSuggestionRowDTO,
  ReportTaxRowDTO,
  ReportVendorRowDTO,
  ReportsDTO,
} from './api';
import { translate, type Language } from './i18n';
import { mockDate, mockMoney } from './transactions.mock';

/**
 * The synthetic Reports page the mock serves. Every partner, account and
 * amount is invented; the figures, percentages and texts follow the Go
 * Reports service, so the page can be checked without a database.
 */

type Range = { from: string; to: string };

type Booking = {
  id: number;
  isoDate: string;
  title: string;
  /** The tax-source subtitle, or "" for a category-relevant booking. */
  subtitle: string;
  amount: number;
  hasReceipt: boolean;
  canReset: boolean;
};

type Group = {
  name: string;
  colour: string;
  income: boolean;
  bookings: Booking[];
};

type Suggestion = {
  id: number;
  isoDate: string;
  title: string;
  keyword: string;
  amount: number;
};

type MonthlyRow = { month: number; income: number; expenses: number };

type RecurringRow = {
  name: string;
  times: number;
  cadenceKey: string;
  nextText: string;
  amount: number;
  currency: string;
  yearly: number;
  income: boolean;
  yearlySort: number;
};

const todayIso = '2026-10-15';
const defaultRange: Range = { from: '01.08.2026', to: '15.10.2026' };
const primaryCurrency = 'EUR';

const palette = {
  orange: '#E76530',
  blue: '#4693F3',
  yellow: '#C38400',
  violet: '#867DFF',
  grey: '#9E9E9E',
};

const categoryAmounts = [
  { label: 'Uncategorized', amount: 29431, colour: palette.grey, categoryId: -1, isMore: false },
  { label: 'Handwerk', amount: 25000, colour: palette.orange, categoryId: 21, isMore: false },
  { label: 'Donations', amount: 15000, colour: palette.violet, categoryId: 22, isMore: false },
  { label: 'Food', amount: 12000, colour: palette.yellow, categoryId: 11, isMore: false },
  { label: 'Health', amount: 11400, colour: palette.blue, categoryId: 23, isMore: false },
];

const vendors = [
  { name: 'Beispiel Werk GmbH', amount: 25000 },
  { name: 'Beispiel-Krankenkasse', amount: 18790 },
  { name: 'Beispielhilfe e. V.', amount: 15000 },
  { name: 'Example Market', amount: 12000 },
  { name: 'Apotheke am Ring', amount: 11400 },
  { name: 'Beispiel Energie AG', amount: 9098 },
];

/** Ten salary months and the year's expenses: 25,000.00 in, 928.31 out. */
const monthlyFigures: MonthlyRow[] = [
  { month: 1, income: 250000, expenses: 0 },
  { month: 2, income: 250000, expenses: 0 },
  { month: 3, income: 250000, expenses: 10000 },
  { month: 4, income: 250000, expenses: 8900 },
  { month: 5, income: 250000, expenses: 999 },
  { month: 6, income: 250000, expenses: 28998 },
  { month: 7, income: 250000, expenses: 3998 },
  { month: 8, income: 250000, expenses: 6498 },
  { month: 9, income: 250000, expenses: 11400 },
  { month: 10, income: 250000, expenses: 22038 },
  { month: 11, income: 0, expenses: 0 },
  { month: 12, income: 0, expenses: 0 },
];

const recurringFigures: RecurringRow[] = [
  {
    name: 'Employer GmbH',
    times: 10,
    cadenceKey: 'CadenceMonthly',
    nextText: '01.11.2026',
    amount: 250000,
    currency: 'EUR',
    yearly: 3000000,
    income: true,
    yearlySort: 3000000,
  },
  {
    name: 'Beispiel Versicherungs- und Vorsorgesellschaft Süddeutschland AG Beitrag',
    times: 4,
    cadenceKey: 'CadenceMonthly',
    nextText: '15.10.2026',
    amount: -2999,
    currency: 'EUR',
    yearly: 35988,
    income: false,
    yearlySort: 35988,
  },
  {
    name: 'Cloud Service',
    times: 5,
    cadenceKey: 'CadenceMonthly',
    nextText: '10.11.2026',
    amount: -1199,
    currency: 'USD',
    yearly: 14388,
    income: false,
    yearlySort: 14388,
  },
  {
    name: 'Spotify',
    times: 5,
    cadenceKey: 'CadenceMonthly',
    nextText: '10.11.2026',
    amount: -999,
    currency: 'EUR',
    yearly: 11988,
    income: false,
    yearlySort: 11988,
  },
];

const monthNames: Record<Language, string[]> = {
  en: [
    'January',
    'February',
    'March',
    'April',
    'May',
    'June',
    'July',
    'August',
    'September',
    'October',
    'November',
    'December',
  ],
  de: [
    'Januar',
    'Februar',
    'März',
    'April',
    'Mai',
    'Juni',
    'Juli',
    'August',
    'September',
    'Oktober',
    'November',
    'Dezember',
  ],
  fr: [
    'janvier',
    'février',
    'mars',
    'avril',
    'mai',
    'juin',
    'juillet',
    'août',
    'septembre',
    'octobre',
    'novembre',
    'décembre',
  ],
};

function isoFor(month: number, day: number): string {
  return `2026-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
}

function lastDayOfMonth(month: number): number {
  return new Date(Date.UTC(2026, month, 0)).getUTCDate();
}

function salaryBookings(): Booking[] {
  return Array.from({ length: 10 }, (_, index) => {
    const month = index + 1;
    return {
      id: index + 1,
      isoDate: isoFor(month, lastDayOfMonth(month)),
      title: 'Employer GmbH',
      subtitle: '',
      amount: 250000,
      hasReceipt: false,
      canReset: false,
    };
  });
}

function deductionGroups(): Group[] {
  return [
    {
      name: 'Handwerk',
      colour: palette.orange,
      income: false,
      bookings: [
        {
          id: 11,
          isoDate: isoFor(3, 12),
          title: 'Beispiel Werk GmbH',
          subtitle: '',
          amount: -20000,
          hasReceipt: true,
          canReset: false,
        },
        {
          id: 12,
          isoDate: isoFor(4, 20),
          title: 'Beispiel Handwerk',
          subtitle: '',
          amount: -5000,
          hasReceipt: false,
          canReset: false,
        },
      ],
    },
    {
      name: 'Donations',
      colour: palette.violet,
      income: false,
      bookings: [
        {
          id: 13,
          isoDate: isoFor(6, 1),
          title: 'Beispielhilfe e. V.',
          subtitle: '',
          amount: -10000,
          hasReceipt: false,
          canReset: false,
        },
        {
          id: 14,
          isoDate: isoFor(7, 15),
          title: 'Beispiel Spende',
          subtitle: '',
          amount: -5000,
          hasReceipt: false,
          canReset: false,
        },
      ],
    },
    {
      name: 'Health',
      colour: palette.blue,
      income: false,
      bookings: [
        {
          id: 15,
          isoDate: isoFor(9, 10),
          title: 'Beispiel-Krankenkasse',
          subtitle: '',
          amount: -11400,
          hasReceipt: false,
          canReset: true,
        },
      ],
    },
  ];
}

function suggestions(): Suggestion[] {
  return [
    {
      id: 901,
      isoDate: '2026-10-08',
      title: 'Beispiel-Krankenkasse',
      keyword: 'Krankenkasse',
      amount: -9890,
    },
    {
      id: 902,
      isoDate: '2026-09-28',
      title: 'Apotheke am Ring',
      keyword: 'Apotheke',
      amount: -2550,
    },
  ];
}

function signedMoney(minor: number, language: Language, currency = primaryCurrency): string {
  return mockMoney(minor, currency, language, true);
}

function plainMoney(minor: number, language: Language, currency = primaryCurrency): string {
  return mockMoney(minor, currency, language, false);
}

function percentText(percent: number, language: Language): string {
  const separator = language === 'en' ? '' : '\u00A0';
  return `${percent}${separator}%`;
}

/** Whole percentages by the largest remainder, as internal/core/format.Shares. */
function shares(amounts: number[]): number[] {
  const total = amounts.reduce((sum, amount) => sum + amount, 0);
  if (total === 0) {
    return amounts.map(() => 0);
  }
  const floors = amounts.map((amount) => Math.floor((amount * 100) / total));
  const remainders = amounts.map((amount) => (amount * 100) % total);
  let missing = 100 - floors.reduce((sum, value) => sum + value, 0);
  const order = remainders
    .map((remainder, index) => ({ remainder, index }))
    .sort((left, right) => right.remainder - left.remainder || left.index - right.index);
  for (const entry of order) {
    if (missing <= 0) {
      break;
    }
    floors[entry.index] = (floors[entry.index] ?? 0) + 1;
    missing -= 1;
  }
  return floors;
}

function parseDate(text: string): { iso: string; sort: number } | null {
  const parts = text.trim().split('.');
  if (parts.length !== 3) {
    return null;
  }
  const [dayText, monthText, yearText] = parts;
  if (
    dayText === undefined ||
    monthText === undefined ||
    yearText === undefined ||
    !/^\d{1,2}$/.test(dayText) ||
    !/^\d{1,2}$/.test(monthText) ||
    !/^\d{4}$/.test(yearText)
  ) {
    return null;
  }
  const day = Number(dayText);
  const month = Number(monthText);
  const year = Number(yearText);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return null;
  }
  return {
    iso: `${yearText}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`,
    sort: year * 10000 + month * 100 + day,
  };
}

function formatDate(iso: string): string {
  return mockDate(iso);
}

function currentRangeText(fromIso: string, toIso: string): string {
  return `${formatDate(fromIso)} – ${formatDate(toIso)}`;
}

function roundHalf(value: number, divisor: number): number {
  return Math.round(value / divisor);
}

export class MockReports {
  private language: Language;
  private tab: string;
  private year = 2026;
  private custom = false;
  private rangeFrom = defaultRange.from;
  private rangeTo = defaultRange.to;
  private rangeFromIso = '2026-08-01';
  private rangeToIso = todayIso;
  private rangeError = '';
  private errorText = '';
  private messageText = '';
  private missingOnly: boolean;
  private incomeGroups: Group[];
  private deductions: Group[];
  private suggestions: Suggestion[];
  private listeners = new Set<(dto: ReportsDTO) => void>();

  constructor(scenario: string, language: Language) {
    this.language = language;
    this.missingOnly = scenario.includes('tax-missing');
    this.tab = 'summary';
    if (scenario.includes('category')) {
      this.tab = 'category';
    } else if (scenario.includes('monthly')) {
      this.tab = 'monthly';
    } else if (scenario.includes('recurring')) {
      this.tab = 'recurring';
    } else if (scenario.includes('tax') || scenario.includes('export-menu')) {
      this.tab = 'tax';
    }
    this.incomeGroups = [
      { name: 'Salary', colour: palette.violet, income: true, bookings: salaryBookings() },
    ];
    this.deductions = deductionGroups();
    this.suggestions = suggestions();
  }

  setLanguage(language: Language): void {
    this.language = language;
  }

  refresh(): ReportsDTO {
    return this.publish();
  }

  subscribe(callback: (dto: ReportsDTO) => void): () => void {
    this.listeners.add(callback);
    return () => this.listeners.delete(callback);
  }

  openTax(year: number): ReportsDTO {
    this.tab = 'tax';
    this.year = year;
    this.custom = false;
    this.resetDefaultRange();
    return this.publish();
  }

  changeYear(delta: number): ReportsDTO {
    this.year += delta;
    this.custom = false;
    this.rangeError = '';
    this.resetDefaultRange();
    return this.publish();
  }

  setCustom(custom: boolean): ReportsDTO {
    this.rangeError = '';
    if (!custom) {
      this.custom = false;
      this.resetDefaultRange();
    }
    return this.publish();
  }

  applyRange(from: string, to: string): ReportsDTO {
    this.rangeFrom = from;
    this.rangeTo = to;
    const start = parseDate(from);
    if (start === null) {
      this.rangeError = translate(this.language, 'EnterValidDate');
      return this.publish();
    }
    const end = parseDate(to);
    if (end === null) {
      this.rangeError = translate(this.language, 'EnterValidDate');
      return this.publish();
    }
    if (start.sort > end.sort) {
      this.rangeError = translate(this.language, 'InvalidDateRange');
      return this.publish();
    }
    this.rangeError = '';
    this.custom = true;
    this.rangeFrom = formatDate(start.iso);
    this.rangeTo = formatDate(end.iso);
    this.rangeFromIso = start.iso;
    this.rangeToIso = end.iso;
    return this.publish();
  }

  setMissingOnly(only: boolean): ReportsDTO {
    this.missingOnly = only;
    return this.publish();
  }

  dismiss(): ReportsDTO {
    this.errorText = '';
    this.messageText = '';
    return this.publish();
  }

  acceptSuggestion(id: number): ReportsDTO {
    const index = this.suggestions.findIndex((suggestion) => suggestion.id === id);
    const suggestion = this.suggestions[index];
    if (suggestion === undefined) {
      return this.state();
    }
    this.suggestions.splice(index, 1);
    const health = this.deductions.find((group) => group.name === 'Health');
    health?.bookings.push({
      id: 1000 + suggestion.id,
      isoDate: suggestion.isoDate,
      title: suggestion.title,
      subtitle: translate(
        this.language,
        'TaxSetByYou',
        translate(this.language, 'TaxRelevantChoice'),
      ),
      amount: suggestion.amount,
      hasReceipt: false,
      canReset: true,
    });
    this.messageText = translate(this.language, 'MessageTaxOverrideSet', 1);
    return this.publish();
  }

  dismissSuggestion(id: number): ReportsDTO {
    const index = this.suggestions.findIndex((suggestion) => suggestion.id === id);
    if (index < 0) {
      return this.state();
    }
    this.suggestions.splice(index, 1);
    this.messageText = translate(this.language, 'MessageTaxOverrideSet', 1);
    return this.publish();
  }

  markNotTaxRelevant(id: number): ReportsDTO {
    for (const group of this.deductions) {
      const index = group.bookings.findIndex((booking) => booking.id === id);
      if (index >= 0) {
        group.bookings.splice(index, 1);
        this.messageText = translate(this.language, 'MessageTaxOverrideSet', 1);
        return this.publish();
      }
    }
    return this.state();
  }

  resetTaxAutomatic(id: number): ReportsDTO {
    for (const group of this.deductions) {
      const booking = group.bookings.find((entry) => entry.id === id);
      if (booking !== undefined) {
        booking.canReset = false;
        booking.subtitle = '';
        this.messageText = translate(this.language, 'MessageTaxOverrideSet', 1);
        return this.publish();
      }
    }
    return this.state();
  }

  exportReportPDF(): ReportsDTO {
    return this.finishExport(`report-${this.fileSuffix()}.pdf`);
  }

  exportCSV(): ReportsDTO {
    return this.finishExport(`finglass-${this.fileSuffix()}.csv`);
  }

  exportTaxXLSX(): ReportsDTO {
    return this.finishExport(`tax-bookings-${this.fileSuffix()}.xlsx`);
  }

  exportTaxPDF(): ReportsDTO {
    return this.finishExport(`tax-bookings-${this.fileSuffix()}.pdf`);
  }

  printTax(): ReportsDTO {
    this.errorText = '';
    this.messageText = translate(this.language, 'MessagePrintSent');
    return this.publish();
  }

  state(): ReportsDTO {
    return this.build();
  }

  private publish(): ReportsDTO {
    const next = this.state();
    for (const listener of this.listeners) {
      listener(next);
    }
    return next;
  }

  private resetDefaultRange(): void {
    this.rangeFrom = defaultRange.from;
    this.rangeTo = defaultRange.to;
    this.rangeFromIso = '2026-08-01';
    this.rangeToIso = todayIso;
  }

  private fileSuffix(): string {
    if (this.custom) {
      return `${this.rangeFromIso}_${this.rangeToIso}`;
    }
    return String(this.year);
  }

  private finishExport(fileName: string): ReportsDTO {
    this.errorText = '';
    this.messageText = translate(this.language, 'MessageSavedTo', fileName);
    return this.publish();
  }

  private periodTitle(): string {
    if (this.custom) {
      return currentRangeText(this.rangeFromIso, this.rangeToIso);
    }
    return String(this.year);
  }

  private monthlyRows(): ReportMonthlyRowDTO[] {
    return monthlyFigures.map((month) => {
      const net = month.income - month.expenses;
      return {
        month: monthNames[this.language][month.month - 1] ?? '',
        incomeText: plainMoney(month.income, this.language),
        expensesText: plainMoney(month.expenses, this.language),
        netText: signedMoney(net, this.language),
        incomeZero: month.income === 0,
        expensesZero: month.expenses === 0,
        netZero: net === 0,
        netTone: net < 0 ? 'negative' : net > 0 ? 'positive' : 'zero',
      };
    });
  }

  private categoryRows(): ReportCategoryRowDTO[] {
    const percentages = shares(categoryAmounts.map((row) => row.amount));
    const total = categoryAmounts.reduce((sum, row) => sum + row.amount, 0);
    return categoryAmounts.map((row, index) => ({
      label: row.categoryId < 0 ? translate(this.language, 'Uncategorized') : row.label,
      percentText: percentText(percentages[index] ?? 0, this.language),
      amountText: plainMoney(row.amount, this.language),
      colour: row.colour,
      categoryId: row.categoryId,
      isMore: row.isMore,
      fraction: total === 0 ? 0 : row.amount / total,
    }));
  }

  private vendorRows(): ReportVendorRowDTO[] {
    return vendors.map((vendor, index) => ({
      rank: `${index + 1}.`,
      name: vendor.name,
      amountText: plainMoney(vendor.amount, this.language),
    }));
  }

  private taxRelevantRows(): ReportAmountRowDTO[] {
    const amounts = this.deductions
      .map((group) => ({
        label: group.name,
        amount: Math.abs(group.bookings.reduce((sum, booking) => sum + booking.amount, 0)),
      }))
      .filter((row) => row.amount > 0);
    const largest = amounts.reduce((max, row) => Math.max(max, row.amount), 0);
    return amounts.map((row) => ({
      label: row.label,
      amountText: plainMoney(row.amount, this.language),
      fraction: largest === 0 ? 0 : row.amount / largest,
    }));
  }

  private recurringRows(): ReportRecurringRowDTO[] {
    const sorted = [...recurringFigures].sort((left, right) => right.yearlySort - left.yearlySort);
    return sorted.map((payment) => ({
      name: payment.name,
      meta:
        payment.name === 'Employer GmbH'
          ? `${translate(this.language, 'TimesSeen', payment.times)} · Salary`
          : translate(this.language, 'TimesSeen', payment.times),
      cadence: translate(this.language, payment.cadenceKey),
      nextText: payment.nextText,
      amountText: signedMoney(payment.amount, this.language, payment.currency),
      yearlyText: plainMoney(payment.yearly, this.language, payment.currency),
      income: payment.income,
    }));
  }

  private recurringTotals(): { yearly: string; monthly: string; count: string } {
    const byCurrency = new Map<string, number>();
    for (const payment of recurringFigures) {
      if (payment.income) {
        continue;
      }
      byCurrency.set(payment.currency, (byCurrency.get(payment.currency) ?? 0) + payment.yearly);
    }
    const codes = [...byCurrency.keys()].sort((left, right) => {
      if (left === primaryCurrency) {
        return -1;
      }
      if (right === primaryCurrency) {
        return 1;
      }
      return left.localeCompare(right);
    });
    const yearly: string[] = [];
    const monthly: string[] = [];
    for (const code of codes) {
      const total = byCurrency.get(code) ?? 0;
      yearly.push(plainMoney(total, this.language, code));
      monthly.push(plainMoney(roundHalf(total, 12), this.language, code));
    }
    return {
      yearly: yearly.join('\n'),
      monthly: monthly.join('\n'),
      count: String(recurringFigures.filter((payment) => !payment.income).length),
    };
  }

  private taxRows(): ReportTaxRowDTO[] {
    const rows: ReportTaxRowDTO[] = [];
    const visible = (booking: Booking) => !this.missingOnly || !booking.hasReceipt;
    const hasVisible = (groups: Group[]) =>
      groups.some((group) => group.bookings.some((booking) => visible(booking)));

    const pushSection = (key: string) => {
      rows.push({
        kind: 'section',
        title: translate(this.language, key),
        subtitle: '',
        colour: '#000000',
        summary: '',
        id: 0,
        dateText: '',
        accountText: '',
        amountText: '',
        expense: false,
        hasReceipt: false,
        missingReceipt: false,
        canReset: false,
      });
    };

    const pushGroups = (groups: Group[]) => {
      for (const group of groups) {
        if (!group.bookings.some((booking) => visible(booking))) {
          continue;
        }
        const net = group.bookings.reduce((sum, booking) => sum + booking.amount, 0);
        rows.push({
          kind: 'group',
          title: group.name,
          subtitle: '',
          colour: group.colour,
          summary: translate(
            this.language,
            'BookingsCountTotal',
            group.bookings.length,
            signedMoney(net, this.language),
          ),
          id: 0,
          dateText: '',
          accountText: '',
          amountText: signedMoney(net, this.language),
          expense: net < 0,
          hasReceipt: false,
          missingReceipt: false,
          canReset: false,
        });
        for (const booking of group.bookings) {
          if (!visible(booking)) {
            continue;
          }
          rows.push({
            kind: 'booking',
            title: booking.title,
            subtitle: booking.subtitle,
            colour: '#000000',
            summary: '',
            id: booking.id,
            dateText: formatDate(booking.isoDate),
            accountText: '…0000',
            amountText: signedMoney(booking.amount, this.language),
            expense: booking.amount < 0,
            hasReceipt: booking.hasReceipt,
            missingReceipt: !booking.hasReceipt,
            canReset: booking.canReset,
          });
        }
      }
    };

    if (hasVisible(this.incomeGroups)) {
      pushSection('TaxSectionIncome');
      pushGroups(this.incomeGroups);
    }
    if (hasVisible(this.deductions)) {
      pushSection('TaxSectionDeductions');
      pushGroups(this.deductions);
    }
    return rows;
  }

  private build(): ReportsDTO {
    const language = this.language;
    const monthly = this.monthlyRows();
    const totalIncome = monthlyFigures.reduce((sum, month) => sum + month.income, 0);
    const totalExpenses = monthlyFigures.reduce((sum, month) => sum + month.expenses, 0);
    const net = totalIncome - totalExpenses;
    const taxIncome = this.incomeGroups.reduce(
      (sum, group) =>
        sum + group.bookings.reduce((groupSum, booking) => groupSum + booking.amount, 0),
      0,
    );
    const taxDeductions = this.deductions.reduce(
      (sum, group) =>
        sum + group.bookings.reduce((groupSum, booking) => groupSum + Math.abs(booking.amount), 0),
      0,
    );
    const taxNet = taxIncome - taxDeductions;
    const bookings = [
      ...this.incomeGroups.flatMap((group) => group.bookings),
      ...this.deductions.flatMap((group) => group.bookings),
    ];
    const missing = bookings.filter((booking) => !booking.hasReceipt).length;
    const withReceipt = bookings.length - missing;
    const taxRelevant = this.taxRelevantRows();
    const recurring = this.recurringTotals();
    const title = this.periodTitle();
    const taxTitle = this.custom
      ? currentRangeText(this.rangeFromIso, this.rangeToIso)
      : String(this.year);
    const taxRows = this.taxRows();

    return {
      tab: this.tab,
      year: this.year,
      yearLabel: String(this.year),
      loading: false,
      exporting: false,
      rangeFrom: this.rangeFrom,
      rangeTo: this.rangeTo,
      rangeError: this.rangeError,
      custom: this.custom,
      errorText: this.errorText,
      messageText: this.messageText,
      hasReport: true,
      empty: false,
      canExportReport: true,
      exportDisabledReason: '',
      canExportTax: bookings.length > 0,
      linkable: !this.custom && (this.year === 2026 || this.year === 2025),
      emptyTitle: translate(language, 'NoTransactionsIn', title),
      emptyHint: translate(language, 'ReportsBuiltFrom'),
      otherCurrencyHint: translate(language, 'OtherCurrenciesExcluded', primaryCurrency, 7),
      incomeValue: plainMoney(totalIncome, language),
      expensesValue: plainMoney(totalExpenses, language),
      netValue: plainMoney(net, language),
      taxValue: plainMoney(taxDeductions, language),
      netTone: net < 0 ? 'negative' : net > 0 ? 'positive' : 'zero',
      taxRelevantRows: taxRelevant,
      taxRelevantEmpty:
        taxRelevant.length === 0 ? translate(language, 'NoTaxRelevantExpenses') : '',
      statTransactions: '27',
      statCategories: '5',
      statAverage: plainMoney(roundHalf(totalExpenses, 8), language),
      statHighest: `${monthNames[language][5] ?? ''} ${plainMoney(28998, language)}`,
      monthlyTitle: translate(language, 'MonthlyIncomeExpenses', title),
      monthlyRows: monthly,
      monthlyTotalIncome: plainMoney(totalIncome, language),
      monthlyTotalExpenses: plainMoney(totalExpenses, language),
      monthlyTotalNet: signedMoney(net, language),
      monthlyTotalTone: net < 0 ? 'negative' : net > 0 ? 'positive' : 'zero',
      monthlyTotalIncomeZero: totalIncome === 0,
      monthlyTotalExpensesZero: totalExpenses === 0,
      monthlyTotalNetZero: net === 0,
      categoryRows: this.categoryRows(),
      transfersNote: '',
      vendorRows: this.vendorRows(),
      recurringEmpty: false,
      recurringCount: recurring.count,
      recurringYearly: recurring.yearly,
      recurringMonthly: recurring.monthly,
      recurringRows: this.recurringRows(),
      hasTax: true,
      missingReceiptsOnly: this.missingOnly,
      taxEmpty: taxRows.length === 0,
      taxEmptyTitle: translate(language, 'NoTaxBookingsIn', taxTitle),
      taxEmptyHint: translate(language, 'TaxBookingsHowTo'),
      taxIncomeValue: plainMoney(taxIncome, language),
      taxDeductionValue: plainMoney(taxDeductions, language),
      taxNetValue: plainMoney(taxNet, language),
      taxNetTone: taxNet < 0 ? 'negative' : taxNet > 0 ? 'positive' : 'zero',
      taxReceiptsMissing: missing > 0,
      taxReceiptsValue:
        missing > 0
          ? translate(language, 'ReceiptsMissing', missing)
          : translate(language, 'ReceiptsComplete'),
      taxReceiptsSubtitle: translate(language, 'TaxReceiptsOf', withReceipt, bookings.length),
      taxOtherHint: translate(language, 'OtherCurrenciesExcluded', primaryCurrency, 1),
      taxSuggestionsTitle: translate(language, 'TaxSuggestionsTitle', this.suggestions.length),
      taxSuggestionsHint: translate(language, 'TaxSuggestionsHint'),
      taxSuggestions: this.suggestionRows(),
      taxRows,
    };
  }

  private suggestionRows(): ReportSuggestionRowDTO[] {
    return this.suggestions.map((suggestion) => ({
      id: suggestion.id,
      dateText: formatDate(suggestion.isoDate),
      title: suggestion.title,
      keywordText: translate(this.language, 'MatchesKeyword', suggestion.keyword),
      amountText: signedMoney(suggestion.amount, this.language),
      expense: suggestion.amount < 0,
    }));
  }
}
