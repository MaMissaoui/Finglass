<script lang="ts">
  import { onMount } from 'svelte';
  import type { Api, DashboardDTO } from '../../lib/api';
  import { Icon, IconButton, PeriodNavigator, TextButton } from '../../lib/components';
  import CategorySplit from '../../lib/charts/CategorySplit.svelte';
  import IncomeExpenseChart from '../../lib/charts/IncomeExpenseChart.svelte';
  import { translate, type Language } from '../../lib/i18n';
  import BudgetRow from './dashboard/BudgetRow.svelte';
  import DashboardCard from './dashboard/DashboardCard.svelte';
  import FirstRunCard from './dashboard/FirstRunCard.svelte';
  import LedgerRow from './dashboard/LedgerRow.svelte';
  import MonthMenu from './dashboard/MonthMenu.svelte';
  import StatementCard from './dashboard/StatementCard.svelte';

  let {
    api,
    language,
  }: {
    api: Api;
    language: Language;
  } = $props();

  let dto = $state<DashboardDTO | null>(null);
  let localError = $state('');
  let monthMenuOpen = $state(false);
  let pending = $state(false);
  let alive = true;

  const errorText = $derived(dto?.errorText || localError);

  function t(key: string): string {
    return translate(language, key);
  }

  async function apply(action: () => Promise<DashboardDTO>): Promise<void> {
    if (pending) {
      return;
    }
    pending = true;
    try {
      const next = await action();
      if (alive) {
        dto = next;
      }
    } catch {
      if (alive) {
        localError = t('ErrorUnexpected');
      }
    } finally {
      pending = false;
    }
  }

  async function go(action: () => Promise<DashboardDTO>): Promise<void> {
    try {
      await action();
    } catch {
      if (alive) {
        localError = t('ErrorUnexpected');
      }
    }
  }

  function previous(): void {
    void apply(() => api.dashboardPreviousMonth());
  }

  function next(): void {
    void apply(() => api.dashboardNextMonth());
  }

  function current(): void {
    monthMenuOpen = false;
    void apply(() => api.dashboardCurrentMonth());
  }

  function selectMonth(index: number): void {
    monthMenuOpen = false;
    void apply(() => api.dashboardSelectMonth(index));
  }

  function dismissError(): void {
    localError = '';
    if (dto?.errorText) {
      void apply(() => api.dashboardDismissError());
    }
  }

  function openCategory(categoryID: number): void {
    void go(() => api.dashboardOpenCategory(categoryID));
  }

  function openUncategorized(): void {
    void go(() => api.dashboardOpenUncategorized());
  }

  function openAll(): void {
    void go(() => api.dashboardOpenAll());
  }

  function openPartner(partner: string): void {
    if (partner === '') {
      openAll();
      return;
    }
    void go(() => api.dashboardOpenPartner(partner));
  }

  function openAccounts(): void {
    void go(() => api.dashboardOpenAccounts());
  }

  function openSettings(): void {
    void go(() => api.dashboardOpenSettings());
  }

  function openTax(): void {
    void go(() => api.dashboardOpenTax());
  }

  onMount(() => {
    let received = false;
    const stop = api.onDashboardChanged((next) => {
      received = true;
      dto = next;
    });

    void (async () => {
      try {
        const initial = await api.dashboardState();
        // A change that arrived while the pull was in flight is newer than
        // the pulled state, so it wins.
        if (alive && !received) {
          dto = initial;
        }
      } catch {
        if (alive) {
          localError = t('ErrorUnexpected');
        }
      }
    })();

    const onKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        monthMenuOpen = false;
      }
    };
    window.addEventListener('keydown', onKeydown);

    return () => {
      alive = false;
      stop();
      window.removeEventListener('keydown', onKeydown);
    };
  });
</script>

<div class="dashboard" data-testid="dashboard-page">
  <div class="scroll">
    <div class="stack">
      {#if errorText !== ''}
        <div class="error-card" role="alert" data-testid="dashboard-error">
          <span class="error-icon">
            <Icon name="warning" size={20} />
          </span>
          <p>{errorText}</p>
          <IconButton icon="close" label={t('Dismiss')} iconSize={18} onclick={dismissError} />
        </div>
      {/if}

      {#if dto}
        {#if dto.firstRun}
          <FirstRunCard {language} onaddbank={openAccounts} onopensettings={openSettings} />
        {:else if dto.showContent}
          <div class="content">
            <div class="header">
              <PeriodNavigator
                title={dto.monthLabel}
                previousEnabled={dto.previousEnabled}
                nextEnabled={dto.nextEnabled}
                previousLabel={t('PreviousMonth')}
                nextLabel={t('NextMonth')}
                titleHasMenu
                onprevious={previous}
                onnext={next}
                ontitleclicked={() => (monthMenuOpen = !monthMenuOpen)}
              />
              {#if !dto.isCurrentMonth}
                <TextButton text={t('CurrentMonth')} onclick={current} />
              {/if}
            </div>

            <StatementCard
              monthLabel={dto.monthLabel}
              lines={dto.statementLines}
              notes={dto.availableSubtitle}
              tone={dto.availableTone}
              pendingTitle={dto.pendingTitle}
              pendingValue={dto.pendingValue}
              pendingLines={dto.pendingLines}
              pendingEnabled={dto.pendingEnabled}
              onopenpending={openUncategorized}
            />

            <div class="row ratio-row">
              <div class="ratio-left">
                <DashboardCard title={dto.expensesSectionTitle}>
                  {#if dto.expenseRows.length === 0}
                    <p class="empty">{dto.expensesEmptyText}</p>
                  {:else}
                    <CategorySplit
                      rows={dto.expenseRows}
                      transfersNote={dto.transfersNote}
                      onselect={openCategory}
                    />
                  {/if}
                </DashboardCard>
              </div>
              <div class="ratio-right">
                <DashboardCard title={dto.chartTitle}>
                  <IncomeExpenseChart
                    bars={dto.monthBars}
                    axisTop={dto.chartAxisTop}
                    axisMid={dto.chartAxisMid}
                    axisZero={dto.chartAxisZero}
                    emptyText={dto.chartEmptyText}
                    incomeLabel={dto.incomeLegend}
                    expensesLabel={dto.expensesLegend}
                    partialNote={dto.partialNote}
                    soFarText={t('ChartSoFar')}
                  />
                </DashboardCard>
              </div>
            </div>

            {#if dto.hasUpcoming}
              <DashboardCard title={dto.upcomingTitle}>
                {#each dto.upcomingRows as payment, index (index)}
                  <LedgerRow
                    dateText={payment.dateText}
                    title={payment.title}
                    meta={payment.meta}
                    amountText={payment.amountText}
                    expense={payment.expense}
                  />
                {/each}
              </DashboardCard>
            {/if}

            <div class="row budget-row">
              {#if dto.hasBudgets}
                <div class="budgets">
                  <DashboardCard title={dto.budgetsTitle}>
                    {#each dto.budgetRows as budget, index (index)}
                      <BudgetRow {budget} monthProgress={dto.monthProgress} />
                    {/each}
                  </DashboardCard>
                </div>
              {/if}
              <div class="tax">
                <DashboardCard
                  title={dto.taxTitle}
                  interactive
                  label={dto.taxTitle}
                  onclick={openTax}
                >
                  {#if dto.taxRows.length > 0}
                    <p class="headline">{dto.taxValue}</p>
                    <CategorySplit rows={dto.taxRows} selectable={false} />
                  {:else}
                    <p class="empty">{dto.taxHint}</p>
                  {/if}
                </DashboardCard>
              </div>
            </div>

            <DashboardCard
              title={dto.recentTitle}
              actionLabel={dto.viewAllLabel}
              onaction={openAll}
            >
              {#if dto.recentRows.length === 0}
                <p class="empty">{dto.recentEmptyText}</p>
              {:else}
                {#each dto.recentRows as booking, index (index)}
                  <LedgerRow
                    dateText={booking.dateText}
                    title={booking.title}
                    meta={booking.meta}
                    amountText={booking.amountText}
                    expense={booking.expense}
                    interactive
                    onclick={() => openPartner(booking.partner)}
                  />
                {/each}
              {/if}
            </DashboardCard>
          </div>
        {:else if dto.loading}
          <div class="loading" data-testid="dashboard-loading" aria-hidden="true">
            <div class="spinner"></div>
          </div>
        {/if}
      {/if}
    </div>
  </div>

  {#if monthMenuOpen && dto}
    <MonthMenu
      options={dto.monthOptions}
      selected={dto.monthSelected}
      onselect={selectMonth}
      onclose={() => (monthMenuOpen = false)}
    />
  {/if}
</div>

<style>
  .dashboard {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  .scroll {
    height: 100%;
    overflow-y: auto;
  }

  .stack {
    display: flex;
    flex-direction: column;
    gap: 24px;
    padding: 20px;
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .header {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    min-height: 40px;
  }

  .row {
    display: flex;
    align-items: stretch;
    gap: 12px;
    min-width: 0;
  }

  .ratio-left {
    display: flex;
    flex: 3 1 300px;
    min-width: 0;
  }

  .ratio-right {
    display: flex;
    flex: 2 1 300px;
    min-width: 0;
  }

  .budgets {
    display: flex;
    flex: 1 1 230px;
    min-width: 0;
  }

  .tax {
    display: flex;
    flex: 1 1 416px;
    min-width: 0;
  }

  .error-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--color-error);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .error-icon {
    display: flex;
    flex: none;
    color: var(--color-error);
  }

  .error-card p {
    flex: 1;
    min-width: 0;
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .empty {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .headline {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-headline-size);
    font-weight: var(--type-headline-weight);
  }

  .loading {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 240px;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
