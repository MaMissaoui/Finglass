import type {
  DashboardBudgetRowDTO,
  DashboardCategoryRowDTO,
  DashboardDTO,
  DashboardMonthBarDTO,
  DashboardStatementLineDTO,
  DashboardTransactionRowDTO,
  DashboardUpcomingRowDTO,
} from '../api';
import { translate, type FormatArg, type Language } from '../i18n';

/**
 * The synthetic Dashboard the mock serves. Every name and every amount is
 * invented; the shapes, texts and fractions follow the Go Dashboard service so
 * the page can be checked without a database.
 */
export type MockDashboardVariant = 'normal' | 'no-carry' | 'first-run' | 'error' | 'loading';

type YearMonth = { year: number; month: number };

type Figures = {
  carry: number | null;
  income: number;
  expenses: number;
  /** Null or zero means the statement has no transfers-and-loans line. */
  movements: number | null;
};

const currentMonth: YearMonth = { year: 2026, month: 10 };
const earliestMonth: YearMonth = { year: 2026, month: 3 };

const orange = '#E8590C';
const blue = '#1D4ED8';
const teal = '#0E7490';
const uncategorizedColour = '#7A8795';
const foodCategory = 11;
const donationsCategory = 21;

/** The reference figures, keyed by `year-month`. */
const octoberFigures: Figures = {
  carry: 118537,
  income: 160000,
  expenses: 14500,
  movements: -19000,
};

const previousFigures: Figures = {
  carry: 98000,
  income: 159000,
  expenses: 17999,
  movements: -5000,
};

const noCarryFigures: Figures = {
  carry: 0,
  income: 150000,
  expenses: 12000,
  movements: null,
};

/** Income and expenses per chart month, in minor units. */
const chartFigures: Record<string, { income: number; expenses: number }> = {
  '2026-06': { income: 159000, expenses: 17999 },
  '2026-07': { income: 159000, expenses: 17999 },
  '2026-08': { income: 159000, expenses: 17999 },
  '2026-09': { income: 159000, expenses: 17999 },
  '2026-10': { income: 160000, expenses: 14500 },
};

const noCarryChartFigures: Record<string, { income: number; expenses: number }> = {
  '2026-10': { income: 150000, expenses: 12000 },
};

function key(month: YearMonth): string {
  return `${month.year}-${String(month.month).padStart(2, '0')}`;
}

function addMonths(month: YearMonth, delta: number): YearMonth {
  const index = month.year * 12 + (month.month - 1) + delta;
  return { year: Math.floor(index / 12), month: (index % 12) + 1 };
}

function compare(left: YearMonth, right: YearMonth): number {
  return left.year - right.year || left.month - right.month;
}

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

function monthName(month: YearMonth, language: Language, width: 'long' | 'short'): string {
  return new Intl.DateTimeFormat(localeOf(language), {
    month: width,
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(month.year, month.month - 1, 1)));
}

function monthLabel(month: YearMonth, language: Language): string {
  return `${monthName(month, language, 'long')} ${month.year}`;
}

function dayText(day: number, month: YearMonth): string {
  return `${String(day).padStart(2, '0')}.${String(month.month).padStart(2, '0')}.${month.year}`;
}

function lastDay(month: YearMonth): number {
  return new Date(Date.UTC(month.year, month.month, 0)).getUTCDate();
}

function money(
  minor: number,
  language: Language,
  options: {
    signed?: boolean;
    whole?: boolean;
    currency?: string;
  } = {},
): string {
  const currency = options.currency ?? 'EUR';
  const fractionDigits = options.whole ? 0 : 2;
  const formatted = new Intl.NumberFormat(localeOf(language), {
    style: 'currency',
    currency,
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(minor / 100);
  if (options.signed && minor > 0) {
    return `+${formatted}`;
  }
  return formatted;
}

function percent(fraction: number, language: Language): string {
  return new Intl.NumberFormat(localeOf(language), {
    style: 'percent',
    maximumFractionDigits: 0,
  }).format(fraction);
}

function availableOf(figures: Figures): number {
  return figures.carry === null
    ? figures.income - figures.expenses + (figures.movements ?? 0)
    : figures.carry + figures.income - figures.expenses + (figures.movements ?? 0);
}

function figuresFor(month: YearMonth, variant: MockDashboardVariant): Figures {
  if (variant === 'no-carry') {
    return noCarryFigures;
  }
  if (compare(month, currentMonth) === 0) {
    return octoberFigures;
  }
  if (compare(month, addMonths(currentMonth, -1)) === 0) {
    return previousFigures;
  }
  return {
    carry: 89000,
    income: 159000,
    expenses: 17999,
    movements: null,
  };
}

function niceCeiling(value: number): number {
  if (value <= 0) {
    return 0;
  }
  let magnitude = 1;
  while (magnitude <= value / 10) {
    magnitude *= 10;
  }
  for (const factor of [1, 2, 5, 10]) {
    const candidate = magnitude * factor;
    if (candidate >= value) {
      return candidate;
    }
  }
  return value;
}

function tone(available: number): string {
  if (available > 0) {
    return 'positive';
  }
  if (available < 0) {
    return 'negative';
  }
  return 'zero';
}

function categoryRow(
  language: Language,
  label: string,
  fraction: number,
  amount: number,
  colour: string,
  categoryID: number,
): DashboardCategoryRowDTO {
  return {
    label,
    percentText: percent(fraction, language),
    amountText: money(amount, language),
    colour,
    categoryId: categoryID,
    isMore: false,
    fraction,
  };
}

function normalExpenseRows(language: Language): DashboardCategoryRowDTO[] {
  return [
    categoryRow(language, 'Food', 12000 / 14500, 12000, orange, foodCategory),
    categoryRow(
      language,
      translate(language, 'Uncategorized'),
      2500 / 14500,
      2500,
      uncategorizedColour,
      -1,
    ),
  ];
}

function noCarryExpenseRows(language: Language): DashboardCategoryRowDTO[] {
  return [categoryRow(language, 'Food', 1, 12000, orange, foodCategory)];
}

function taxRows(language: Language): DashboardCategoryRowDTO[] {
  return [categoryRow(language, 'Donations', 1, 15000, teal, donationsCategory)];
}

function budgetRows(language: Language): DashboardBudgetRowDTO[] {
  return [
    {
      name: 'Food',
      amountText: translate(
        language,
        'SpentOfLimit',
        money(12000, language),
        money(30000, language),
      ),
      restText: translate(language, 'LeftBudget', money(18000, language)),
      ratio: 0.4,
      over: false,
      warning: false,
      colour: orange,
    },
    {
      name: 'Health',
      amountText: translate(language, 'SpentOfLimit', money(0, language), money(20000, language)),
      restText: translate(language, 'LeftBudget', money(20000, language)),
      ratio: 0,
      over: false,
      warning: false,
      colour: blue,
    },
  ];
}

function upcomingRows(language: Language): DashboardUpcomingRowDTO[] {
  return [
    {
      dateText: dayText(2, addMonths(currentMonth, 1)),
      title: 'Example Market',
      meta: `${translate(language, 'CadenceMonthly')} · Food`,
      amountText: money(-12000, language, { signed: true }),
      expense: true,
    },
    {
      dateText: dayText(3, addMonths(currentMonth, 1)),
      title: 'Employer GmbH',
      meta: `${translate(language, 'CadenceMonthly')} · ${translate(language, 'AmountVaries')}`,
      amountText: money(158000, language, { signed: true }),
      expense: false,
    },
    {
      dateText: dayText(10, addMonths(currentMonth, 1)),
      title: 'Spotify',
      meta: translate(language, 'CadenceMonthly'),
      amountText: money(-999, language, { signed: true }),
      expense: true,
    },
  ];
}

function recentRows(language: Language): DashboardTransactionRowDTO[] {
  return [
    {
      dateText: '07.10.2026',
      title: 'Interest',
      meta: translate(language, 'Uncategorized'),
      amountText: money(-1000, language, { signed: true }),
      expense: true,
      partner: 'Interest',
    },
    {
      dateText: '06.10.2026',
      title: 'US shop',
      meta: translate(language, 'Uncategorized'),
      amountText: money(-3000, language, { signed: true, currency: 'USD' }),
      expense: true,
      partner: 'US shop',
    },
    {
      dateText: '05.10.2026',
      title: 'Corner Kiosk',
      meta: translate(language, 'Uncategorized'),
      amountText: money(-1500, language, { signed: true }),
      expense: true,
      partner: 'Corner Kiosk',
    },
    {
      dateText: '04.10.2026',
      title: 'Own transfer',
      meta: 'Own transfers',
      amountText: money(-20000, language, { signed: true }),
      expense: true,
      partner: '',
    },
    {
      dateText: '03.10.2026',
      title: 'Employer GmbH',
      meta: translate(language, 'Uncategorized'),
      amountText: money(160000, language, { signed: true }),
      expense: false,
      partner: 'Employer GmbH',
    },
    {
      dateText: '02.10.2026',
      title: 'Example Market',
      meta: 'Food',
      amountText: money(-12000, language, { signed: true }),
      expense: true,
      partner: 'Example Market',
    },
    {
      dateText: '10.09.2026',
      title: 'Spotify',
      meta: translate(language, 'Uncategorized'),
      amountText: money(-999, language, { signed: true }),
      expense: true,
      partner: 'Spotify',
    },
    {
      dateText: '06.09.2026',
      title: 'Example Aid',
      meta: 'Donations',
      amountText: money(-5000, language, { signed: true }),
      expense: true,
      partner: 'Example Aid',
    },
  ];
}

function statementLines(
  month: YearMonth,
  figures: Figures,
  availableTitle: string,
  language: Language,
): DashboardStatementLineDTO[] {
  const lines: DashboardStatementLineDTO[] = [];
  if (figures.carry !== null) {
    lines.push({
      label: translate(language, 'CarryForwardLine', monthLabel(addMonths(month, -1), language)),
      amountText: money(figures.carry, language),
      kind: 'carry',
    });
  }
  lines.push({
    label: translate(language, 'DirectionIncome'),
    amountText: money(figures.income, language, { signed: true }),
    kind: 'in',
  });
  lines.push({
    label: translate(language, 'DirectionExpenses'),
    amountText: money(-figures.expenses, language),
    kind: 'out',
  });
  if (figures.movements !== null && figures.movements !== 0) {
    lines.push({
      label: translate(language, 'OtherMovementsLabel'),
      amountText: money(figures.movements, language, { signed: true }),
      kind: 'movement',
    });
  }
  lines.push({
    label: availableTitle,
    amountText: money(availableOf(figures), language),
    kind: 'result',
  });
  return lines;
}

function axisLabel(minor: number, language: Language): string {
  return minor >= 1000 ? money(minor, language, { whole: true }) : money(minor, language);
}

function chartBars(
  month: YearMonth,
  language: Language,
  variant: MockDashboardVariant,
): {
  bars: DashboardMonthBarDTO[];
  axisTop: string;
  axisMid: string;
  axisZero: string;
  emptyText: string;
} {
  const source = variant === 'no-carry' ? noCarryChartFigures : chartFigures;
  const months: YearMonth[] = [];
  for (let index = 5; index >= 0; index -= 1) {
    months.push(addMonths(month, -index));
  }
  let top = 0;
  for (const entry of months) {
    const figures = source[key(entry)] ?? { income: 0, expenses: 0 };
    top = Math.max(top, figures.income, figures.expenses);
  }
  top = niceCeiling(top);

  const bars: DashboardMonthBarDTO[] = months.map((entry) => {
    const figures = source[key(entry)] ?? { income: 0, expenses: 0 };
    return {
      label: monthName(entry, language, 'short'),
      title: monthLabel(entry, language),
      incomeText: money(figures.income, language, { signed: true }),
      expensesText: money(-figures.expenses, language),
      incomeFraction: top === 0 ? 0 : figures.income / top,
      expensesFraction: top === 0 ? 0 : figures.expenses / top,
      partial: key(entry) === key(currentMonth),
    };
  });

  let emptyText = '';
  if (top === 0) {
    emptyText =
      compare(month, currentMonth) === 0
        ? translate(language, 'NoBookingsSixMonths')
        : translate(language, 'NoBookingsSixMonthsTo', monthLabel(month, language));
  }

  return {
    bars,
    axisTop: top === 0 ? '' : axisLabel(top, language),
    axisMid: top === 0 ? '' : axisLabel(top / 2, language),
    axisZero:
      top === 0 ? '' : top >= 1000 ? money(0, language, { whole: true }) : money(0, language),
    emptyText,
  };
}

function emptyDTO(): DashboardDTO {
  return {
    loading: false,
    firstRun: false,
    showContent: false,
    errorText: '',
    monthOptions: [],
    monthSelected: 0,
    monthLabel: '',
    previousEnabled: false,
    nextEnabled: false,
    isCurrentMonth: true,
    availableTitle: '',
    availableValue: '',
    availableSubtitle: '',
    availableTone: 'zero',
    statementLines: [],
    incomeTitle: '',
    incomeValue: '',
    expensesTitle: '',
    expensesValue: '',
    pendingTitle: '',
    pendingValue: '',
    pendingSubtitle: '',
    pendingLines: [],
    pendingEnabled: false,
    expensesSectionTitle: '',
    expensesEmptyText: '',
    expenseRows: [],
    transfersNote: '',
    chartTitle: '',
    chartEmptyText: '',
    chartAxisTop: '',
    chartAxisMid: '',
    chartAxisZero: '',
    monthBars: [],
    incomeLegend: '',
    expensesLegend: '',
    partialNote: '',
    budgetsTitle: '',
    budgetRows: [],
    hasBudgets: false,
    monthProgress: 0,
    taxTitle: '',
    taxHint: '',
    taxValue: '',
    taxRows: [],
    upcomingTitle: '',
    upcomingRows: [],
    hasUpcoming: false,
    recentTitle: '',
    recentEmptyText: '',
    recentRows: [],
    viewAllLabel: '',
  };
}

/**
 * MockDashboard is the stateful stand-in for `internal/bind.Dashboard`. Month
 * navigation changes the DTO; the shell navigation a click causes stays in the
 * mock API, which owns the screen.
 */
export class MockDashboard {
  private readonly variant: MockDashboardVariant;
  private language: Language;
  private selected = 0;
  private errorText = '';

  constructor(variant: MockDashboardVariant, language: Language) {
    this.variant = variant;
    this.language = language;
    if (variant === 'error') {
      this.errorText = translate(language, 'ErrorUnexpected');
    }
  }

  setLanguage(language: Language): void {
    if (this.variant === 'error' && this.errorText !== '') {
      this.errorText = translate(language, 'ErrorUnexpected');
    }
    this.language = language;
  }

  private months(): YearMonth[] {
    const months: YearMonth[] = [];
    for (let index = 0; index <= 12; index += 1) {
      const month = addMonths(currentMonth, -index);
      months.push(month);
      if (compare(month, earliestMonth) === 0) {
        break;
      }
    }
    return months;
  }

  previous(): DashboardDTO {
    this.selected = Math.min(this.selected + 1, this.months().length - 1);
    return this.dto();
  }

  next(): DashboardDTO {
    this.selected = Math.max(this.selected - 1, 0);
    return this.dto();
  }

  current(): DashboardDTO {
    this.selected = 0;
    return this.dto();
  }

  select(index: number): DashboardDTO {
    if (index >= 0 && index < this.months().length) {
      this.selected = index;
    }
    return this.dto();
  }

  dismissError(): DashboardDTO {
    this.errorText = '';
    return this.dto();
  }

  dto(): DashboardDTO {
    if (this.variant === 'first-run') {
      return { ...emptyDTO(), firstRun: true };
    }
    if (this.variant === 'loading') {
      return { ...emptyDTO(), loading: true };
    }

    const language = this.language;
    const months = this.months();
    const selected = months[this.selected] ?? currentMonth;
    const figures = figuresFor(selected, this.variant);
    const isCurrent = compare(selected, currentMonth) === 0;
    const previousEnabled = compare(selected, earliestMonth) > 0;
    const nextEnabled = compare(selected, currentMonth) < 0;
    const available = availableOf(figures);
    const availableTitle = isCurrent
      ? translate(language, 'AvailableBalance')
      : translate(language, 'AvailableAsOf', dayText(lastDay(selected), selected));

    const subtitles: string[] = [];
    if (this.variant !== 'no-carry') {
      subtitles.push(
        translate(language, 'LoansSubtitle', money(-500000, language), money(-254963, language)),
      );
      subtitles.push(
        translate(language, 'PlusOtherCurrencies', money(50000, language, { currency: 'USD' })),
      );
    }

    const pending: number = this.variant === 'no-carry' ? 1 : 6;
    const uncategorized = this.variant === 'no-carry' ? 1 : 4;
    const unlinked = this.variant === 'no-carry' ? 0 : 2;
    let pendingLines: string[] = [];
    let pendingSubtitle: string;
    if (pending === 0) {
      pendingSubtitle = translate(language, 'NothingPending');
    } else {
      pendingSubtitle = translate(language, 'PendingSubtitle', uncategorized, unlinked);
      pendingLines = pendingSubtitle.split(' · ');
    }

    const chart = chartBars(selected, language, this.variant);
    const expenseRows =
      this.variant === 'no-carry' ? noCarryExpenseRows(language) : normalExpenseRows(language);
    const tax = this.variant === 'no-carry' ? [] : taxRows(language);
    const budgets = budgetRows(language);
    const upcoming = this.variant === 'no-carry' ? [] : upcomingRows(language);
    const recent = recentRows(language);

    const input: FormatArg[] = [monthLabel(selected, language)];
    const dto: DashboardDTO = {
      loading: false,
      firstRun: false,
      showContent: true,
      errorText: this.errorText,
      monthOptions: months.map((month) => monthLabel(month, language)),
      monthSelected: this.selected,
      monthLabel: monthLabel(selected, language),
      previousEnabled,
      nextEnabled,
      isCurrentMonth: isCurrent,
      availableTitle,
      availableValue: money(available, language),
      availableSubtitle: subtitles.join('\n'),
      availableTone: tone(available),
      statementLines: statementLines(selected, figures, availableTitle, language),
      incomeTitle: translate(language, isCurrent ? 'IncomeThisMonth' : 'IncomeInMonth', ...input),
      incomeValue: money(figures.income, language),
      expensesTitle: translate(
        language,
        isCurrent ? 'ExpensesThisMonth' : 'ExpensesInMonth',
        ...input,
      ),
      expensesValue: money(figures.expenses, language),
      pendingTitle: translate(language, 'PendingTasks'),
      pendingValue: String(pending),
      pendingSubtitle,
      pendingLines,
      pendingEnabled: pending > 0,
      expensesSectionTitle: translate(
        language,
        isCurrent ? 'ExpensesByCategory' : 'ExpensesByCategoryInMonth',
        ...input,
      ),
      expensesEmptyText: translate(
        language,
        isCurrent ? 'NoExpensesThisMonth' : 'NoExpensesInMonth',
        ...input,
      ),
      expenseRows,
      transfersNote:
        this.variant === 'no-carry'
          ? ''
          : translate(language, 'CategorySplitTransfers', money(20000, language)),
      chartTitle: translate(language, 'IncomeVsExpenses'),
      chartEmptyText: chart.emptyText,
      chartAxisTop: chart.axisTop,
      chartAxisMid: chart.axisMid,
      chartAxisZero: chart.axisZero,
      monthBars: chart.bars,
      incomeLegend: translate(language, 'ChartIncome'),
      expensesLegend: translate(language, 'ChartExpenses'),
      partialNote: translate(language, 'ChartPartialMonthNote'),
      budgetsTitle: translate(
        language,
        isCurrent ? 'BudgetsThisMonth' : 'BudgetsInMonth',
        ...input,
      ),
      budgetRows: budgets,
      hasBudgets: budgets.length > 0,
      monthProgress: 0.6,
      taxTitle: translate(language, 'TaxDeductibleYtd'),
      taxHint: translate(language, 'TaxDeductibleHint'),
      taxValue: money(15000, language),
      taxRows: tax,
      upcomingTitle: translate(language, 'Upcoming30Days'),
      upcomingRows: upcoming,
      hasUpcoming: upcoming.length > 0,
      recentTitle: translate(language, 'RecentTransactions'),
      recentEmptyText: translate(language, 'NoTransactionsYet'),
      recentRows: recent,
      viewAllLabel: translate(language, 'ViewAll'),
    };
    return dto;
  }
}
