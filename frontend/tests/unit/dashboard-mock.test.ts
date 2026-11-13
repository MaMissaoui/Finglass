import { describe, expect, it } from 'vitest';
import type { DashboardDTO } from '../../src/lib/api';
import { createMockApi } from '../../src/lib/api.mock';
import { translate } from '../../src/lib/i18n';

function apiFor(scenario: string) {
  return createMockApi(scenario);
}

describe('the mock dashboard fake', () => {
  it('serves the reference month with every card', async () => {
    const api = apiFor('dashboard-en-light');
    const shell = await api.state();
    expect(shell.shellOpen).toBe(true);
    expect(shell.screen).toBe('dashboard');
    expect(shell.language).toBe('en');

    const dto = await api.dashboardState();
    expect(dto.loading).toBe(false);
    expect(dto.firstRun).toBe(false);
    expect(dto.showContent).toBe(true);
    expect(dto.errorText).toBe('');
    expect(dto.monthLabel).toBe('October 2026');
    expect(dto.monthOptions[0]).toBe('October 2026');
    expect(dto.monthSelected).toBe(0);
    expect(dto.previousEnabled).toBe(true);
    expect(dto.nextEnabled).toBe(false);
    expect(dto.isCurrentMonth).toBe(true);

    expect(dto.availableTitle).toBe(translate('en', 'AvailableBalance'));
    expect(dto.availableValue).toBe('€2,450.37');
    expect(dto.availableTone).toBe('positive');
    expect(dto.availableSubtitle).toBe(
      'Loans -€5,000.00 · net -€2,549.63\n+ $500.00 in other currencies',
    );

    expect(dto.statementLines.map((line) => line.kind)).toEqual([
      'carry',
      'in',
      'out',
      'movement',
      'result',
    ]);
    expect(dto.statementLines[0]?.label).toBe('Carried over from September 2026');
    expect(dto.statementLines[1]?.amountText).toBe('+€1,600.00');
    expect(dto.statementLines[2]?.amountText).toBe('-€145.00');
    expect(dto.statementLines[3]?.amountText).toBe('-€190.00');
    expect(dto.statementLines[4]?.amountText).toBe('€2,450.37');

    expect(dto.pendingValue).toBe('6');
    expect(dto.pendingLines).toEqual(['4 uncategorized', '2 unlinked docs']);
    expect(dto.pendingEnabled).toBe(true);

    expect(dto.expenseRows).toHaveLength(2);
    expect(dto.expenseRows[0]).toMatchObject({
      label: 'Food',
      percentText: '83%',
      amountText: '€120.00',
      categoryId: 11,
      isMore: false,
    });
    expect(dto.expenseRows[0]?.fraction).toBeCloseTo(120 / 145, 5);
    expect(dto.expenseRows[1]?.categoryId).toBe(-1);
    expect(dto.transfersNote).toBe('Not counted: transfers €200.00');

    expect(dto.monthBars).toHaveLength(6);
    expect(dto.monthBars.map((bar) => bar.label)).toEqual([
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
    ]);
    expect(dto.monthBars[5]?.partial).toBe(true);
    expect(dto.monthBars.slice(0, 5).some((bar) => bar.partial)).toBe(false);
    expect(dto.monthBars[4]?.title).toBe('September 2026');
    expect(dto.monthBars[4]?.incomeText).toBe('+€1,590.00');
    expect(dto.monthBars[4]?.expensesText).toBe('-€179.99');
    expect(dto.chartAxisTop).toBe('€2,000');
    expect(dto.chartAxisMid).toBe('€1,000');
    expect(dto.chartAxisZero).toBe('€0');
    expect(dto.chartEmptyText).toBe('');

    expect(dto.hasBudgets).toBe(true);
    expect(dto.budgetRows[0]?.amountText).toBe('€120.00 of €300.00');
    expect(dto.budgetRows[0]?.restText).toBe('€180.00 left');
    expect(dto.monthProgress).toBe(0.6);

    expect(dto.taxRows).toHaveLength(1);
    expect(dto.taxValue).toBe('€150.00');

    expect(dto.hasUpcoming).toBe(true);
    expect(dto.upcomingRows[0]?.title).toBe('Example Market');
    expect(dto.upcomingRows[0]?.meta).toBe('Monthly · Food');

    expect(dto.recentRows).toHaveLength(8);
    expect(dto.viewAllLabel).toBe('View all');
  });

  it('speaks German without changing the figures', async () => {
    const dto = await apiFor('dashboard-de-light').dashboardState();
    expect(dto.monthLabel).toBe('Oktober 2026');
    expect(dto.availableTitle).toBe('Verfügbar');
    expect(dto.availableValue).toBe('2.450,37\u00a0€');
    expect(dto.statementLines[0]?.label).toBe('Übertrag aus September 2026');
    expect(dto.statementLines[4]?.amountText).toBe('2.450,37\u00a0€');
    expect(dto.expenseRows[0]?.percentText).toBe('83\u00a0%');
    expect(dto.chartAxisTop).toBe('2.000\u00a0€');
    expect(dto.pendingLines).toEqual(['4 ohne Kategorie', '2 unverknüpfte Dokumente']);
  });

  it('moves through the months and publishes every change', async () => {
    const api = apiFor('dashboard-en-light');
    const seen: DashboardDTO[] = [];
    const stop = api.onDashboardChanged((next) => seen.push(next));

    await expect(api.dashboardPreviousMonth()).resolves.toMatchObject({
      monthLabel: 'September 2026',
      isCurrentMonth: false,
      nextEnabled: true,
    });
    expect(seen).toHaveLength(1);
    expect(seen[0]?.monthLabel).toBe('September 2026');

    await api.dashboardCurrentMonth();
    expect(seen[1]?.monthLabel).toBe('October 2026');

    await api.dashboardSelectMonth(2);
    expect(seen[2]?.monthLabel).toBe('August 2026');
    expect(seen[2]?.monthSelected).toBe(2);

    await api.dashboardNextMonth();
    expect(seen[3]?.monthLabel).toBe('September 2026');

    stop();
    await api.dashboardPreviousMonth();
    expect(seen).toHaveLength(4);
  });

  it('changes the labels of a month before today', async () => {
    const api = apiFor('dashboard-en-light');
    const dto = await api.dashboardSelectMonth(1);
    expect(dto.monthLabel).toBe('September 2026');
    expect(dto.isCurrentMonth).toBe(false);
    expect(dto.availableTitle).toBe('Available on 30.09.2026');
    expect(dto.incomeTitle).toBe('Income in September 2026');
    expect(dto.expensesSectionTitle).toBe('Expenses by category in September 2026');
    expect(dto.budgetsTitle).toBe('Budgets in September 2026');
    expect(dto.chartEmptyText).toBe('');
  });

  it('serves the first run card instead of figures', async () => {
    const dto = await apiFor('dashboard-first-run-en').dashboardState();
    expect(dto.firstRun).toBe(true);
    expect(dto.showContent).toBe(false);
    expect(dto.statementLines).toEqual([]);
  });

  it('serves the loading state', async () => {
    const dto = await apiFor('dashboard-loading-en-light').dashboardState();
    expect(dto.loading).toBe(true);
    expect(dto.showContent).toBe(false);
    expect(dto.firstRun).toBe(false);
  });

  it('serves a month without carry-forward or tasks due', async () => {
    const dto = await apiFor('dashboard-no-carry-en-light').dashboardState();
    expect(dto.monthLabel).toBe('October 2026');
    expect(dto.availableValue).toBe('€1,380.00');
    expect(dto.statementLines.map((line) => line.kind)).toEqual(['carry', 'in', 'out', 'result']);
    expect(dto.statementLines[0]?.amountText).toBe('€0.00');
    expect(dto.statementLines[3]?.amountText).toBe('€1,380.00');
    expect(dto.pendingValue).toBe('1');
    expect(dto.pendingLines).toEqual(['1 uncategorized', '0 unlinked docs']);
    expect(dto.expenseRows).toHaveLength(1);
    expect(dto.expenseRows[0]?.percentText).toBe('100%');
    expect(dto.transfersNote).toBe('');
    expect(dto.monthBars.slice(0, 5).every((bar) => bar.incomeFraction === 0)).toBe(true);
    expect(dto.monthBars[5]?.incomeFraction).toBe(0.75);
    expect(dto.monthBars[5]?.partial).toBe(true);
    expect(dto.taxRows).toEqual([]);
    expect(dto.hasUpcoming).toBe(false);
  });

  it('clears its error through dismiss', async () => {
    const api = apiFor('dashboard-error-en-light');
    const withError = await api.dashboardState();
    expect(withError.errorText).toBe(translate('en', 'ErrorUnexpected'));
    const cleared = await api.dashboardDismissError();
    expect(cleared.errorText).toBe('');
  });

  it('navigates the shell for every Open method', async () => {
    const api = apiFor('dashboard-en-light');
    const screens: string[] = [];
    const stop = api.onShellChanged((next) => screens.push(next.screen));

    await api.dashboardOpenCategory(11);
    expect(screens.at(-1)).toBe('transactions');
    await api.dashboardOpenUncategorized();
    expect(screens.at(-1)).toBe('transactions');
    await api.dashboardOpenAll();
    expect(screens.at(-1)).toBe('transactions');
    await api.dashboardOpenPartner('Example Market');
    expect(screens.at(-1)).toBe('transactions');
    await api.dashboardOpenAccounts();
    expect(screens.at(-1)).toBe('accounts');
    await api.dashboardOpenSettings();
    expect(screens.at(-1)).toBe('settings');
    await api.dashboardOpenTax();
    expect(screens.at(-1)).toBe('reports');

    stop();
  });

  it('keeps the chosen month across a trip through another page', async () => {
    const api = apiFor('dashboard-en-light');
    await api.dashboardPreviousMonth();
    await api.navigate('accounts');
    const shell = await api.navigate('dashboard');
    expect(shell.screen).toBe('dashboard');
    expect((await api.dashboardState()).monthLabel).toBe('September 2026');
  });

  it('retranslates the dashboard on a language change', async () => {
    const api = apiFor('dashboard-en-light');
    const seen: string[] = [];
    const stop = api.onDashboardChanged((next) => seen.push(next.monthLabel));
    const shell = await api.setLanguage('de');
    expect(shell.language).toBe('de');
    expect(seen.at(-1)).toBe('Oktober 2026');
    stop();
  });
});
