<script lang="ts">
  import { onMount } from 'svelte';
  import type { Api, ReportsDTO, ShellDTO } from '../../lib/api';
  import {
    Checkbox,
    MessageBanner,
    PeriodNavigator,
    PrimaryButton,
    SecondaryButton,
    SegmentedButton,
    TextButton,
  } from '../../lib/components';
  import CategorySplit from '../../lib/charts/CategorySplit.svelte';
  import { languageFromCode, translate, type FormatArg, type Language } from '../../lib/i18n';
  import ExportMenu from './reports/ExportMenu.svelte';
  import ReportEmpty from './reports/ReportEmpty.svelte';
  import ReportFigure from './reports/ReportFigure.svelte';
  import ReportStat from './reports/ReportStat.svelte';
  import SectionCard from './reports/SectionCard.svelte';
  import TaxRelevantBars from './reports/TaxRelevantBars.svelte';

  let {
    api,
    dto: shell,
  }: {
    api: Api;
    dto: ShellDTO;
  } = $props();

  const tabs = [
    { id: 'summary', key: 'TabTaxSummary' },
    { id: 'tax', key: 'TabTaxBookings' },
    { id: 'monthly', key: 'TabMonthlyBreakdown' },
    { id: 'category', key: 'TabCategoryAnalysis' },
    { id: 'recurring', key: 'TabRecurring' },
  ] as const;

  let dto = $state<ReportsDTO | null>(null);
  let activeTab = $state('summary');
  let customEdit = $state(false);
  let fromEdit = $state('');
  let toEdit = $state('');
  let exportOpen = $state(false);
  let pending = $state(false);
  let loadFailed = $state(false);
  let windowWidth = $state(1440);
  let lastCustom = false;
  let lastFrom = '';
  let lastTo = '';
  let alive = true;

  const language = $derived<Language>(languageFromCode(shell.language));
  const compact = $derived(windowWidth < 1000);
  const busy = $derived(dto?.loading === true || dto?.exporting === true || pending);
  const canExport = $derived(dto?.canExportReport === true && !busy);
  const exportReason = $derived(dto?.exportDisabledReason ?? '');

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function apply(next: ReportsDTO): void {
    if (next.rangeFrom !== lastFrom) {
      fromEdit = next.rangeFrom;
    }
    if (next.rangeTo !== lastTo) {
      toEdit = next.rangeTo;
    }
    if (next.custom !== lastCustom) {
      customEdit = next.custom;
    }
    lastCustom = next.custom;
    lastFrom = next.rangeFrom;
    lastTo = next.rangeTo;
    dto = next;
  }

  function setInitial(next: ReportsDTO): void {
    activeTab = next.tab;
    customEdit = next.custom;
    fromEdit = next.rangeFrom;
    toEdit = next.rangeTo;
    lastCustom = next.custom;
    lastFrom = next.rangeFrom;
    lastTo = next.rangeTo;
    dto = next;
  }

  async function run(action: () => Promise<ReportsDTO>): Promise<void> {
    if (pending) {
      return;
    }
    pending = true;
    try {
      const next = await action();
      if (alive) {
        apply(next);
      }
    } catch {
      if (alive) {
        loadFailed = true;
      }
    } finally {
      pending = false;
    }
  }

  function selectTab(id: string): void {
    activeTab = id;
    exportOpen = false;
  }

  function selectMode(index: number): void {
    customEdit = index === 1;
    exportOpen = false;
    void run(() => api.reportsSetCustom(index === 1));
  }

  function previousYear(): void {
    void run(() => api.reportsChangeYear(-1));
  }

  function nextYear(): void {
    void run(() => api.reportsChangeYear(1));
  }

  function applyRange(): void {
    void run(() => api.reportsApplyRange(fromEdit, toEdit));
  }

  function dismissMessage(): void {
    void run(() => api.reportsDismiss());
  }

  function setMissingOnly(only: boolean): void {
    void run(() => api.reportsSetMissingOnly(only));
  }

  function acceptSuggestion(id: number): void {
    void run(() => api.reportsAcceptSuggestion(id));
  }

  function dismissSuggestion(id: number): void {
    void run(() => api.reportsDismissSuggestion(id));
  }

  function markNotTaxRelevant(id: number): void {
    void run(() => api.reportsMarkNotTaxRelevant(id));
  }

  function resetTaxAutomatic(id: number): void {
    void run(() => api.reportsResetTaxAutomatic(id));
  }

  function openCategory(categoryID: number): void {
    void run(() => api.reportsOpenCategory(categoryID));
  }

  function pickExport(action: string): void {
    exportOpen = false;
    switch (action) {
      case 'taxXLSX':
        void run(() => api.reportsExportTaxXLSX());
        break;
      case 'taxPDF':
        void run(() => api.reportsExportTaxPDF());
        break;
      case 'print':
        void run(() => api.reportsPrintTax());
        break;
      case 'reportPDF':
        void run(() => api.reportsExportReportPDF());
        break;
      case 'csv':
        void run(() => api.reportsExportCSV());
        break;
      default:
        break;
    }
  }

  onMount(() => {
    let received = false;
    const stop = api.onReportsChanged((next) => {
      received = true;
      if (alive) {
        apply(next);
      }
    });

    void (async () => {
      try {
        const initial = await api.reportsState();
        // A change that arrived while the pull was in flight is newer than
        // the pulled state, so it wins.
        if (alive && !received) {
          setInitial(initial);
        }
      } catch {
        if (alive) {
          loadFailed = true;
        }
      }
    })();

    return () => {
      alive = false;
      stop();
    };
  });
</script>

<svelte:window
  bind:innerWidth={windowWidth}
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      exportOpen = false;
    }
  }}
/>

<div class="reports-page" data-testid="reports-page">
  {#if dto === null}
    {#if loadFailed}
      <MessageBanner
        text={t('ErrorUnexpected')}
        isError
        dismissLabel={t('Dismiss')}
        ondismiss={() => (loadFailed = false)}
      />
    {:else}
      <div class="loading" aria-hidden="true"><span class="spinner"></span></div>
    {/if}
  {:else}
    <div class="header">
      {#if activeTab !== 'recurring'}
        <div class="mode">
          <SegmentedButton
            labels={[t('TaxYear'), t('CustomRange')]}
            selected={customEdit ? 1 : 0}
            onselect={selectMode}
          />
        </div>

        {#if !customEdit}
          <PeriodNavigator
            title={dto.yearLabel}
            previousEnabled={!busy}
            nextEnabled={!busy}
            previousLabel={t('PreviousYear')}
            nextLabel={t('NextYear')}
            onprevious={previousYear}
            onnext={nextYear}
          />
        {:else}
          <div class="custom-range">
            <input
              class="date-field"
              type="text"
              aria-label={t('FromDate')}
              placeholder={t('FromDate')}
              spellcheck="false"
              disabled={busy}
              bind:value={fromEdit}
            />
            <input
              class="date-field"
              type="text"
              aria-label={t('ToDate')}
              placeholder={t('ToDate')}
              spellcheck="false"
              disabled={busy}
              bind:value={toEdit}
            />
            <SecondaryButton text={t('Apply')} enabled={!busy} onclick={applyRange} />
          </div>
        {/if}
      {:else}
        <span class="recurring-title">{t('TabRecurring')}</span>
      {/if}

      <span class="load-slot">
        {#if dto.loading}
          <span class="spinner-small" aria-hidden="true"></span>
        {/if}
      </span>

      <span class="spacer"></span>

      {#if dto.exporting}
        <span class="spinner-small" aria-hidden="true"></span>
      {/if}
    </div>

    {#if dto.rangeError !== ''}
      <p class="range-error" role="alert">{dto.rangeError}</p>
    {/if}

    {#if dto.errorText !== '' || dto.messageText !== ''}
      <MessageBanner
        text={dto.errorText !== '' ? dto.errorText : dto.messageText}
        isError={dto.errorText !== ''}
        dismissLabel={t('Dismiss')}
        ondismiss={dismissMessage}
      />
    {/if}

    <div class="tabbar" role="tablist" aria-label={t('NavReports')}>
      {#each tabs as tab (tab.id)}
        <button
          type="button"
          role="tab"
          class="tab"
          class:selected={activeTab === tab.id}
          aria-selected={activeTab === tab.id}
          onclick={() => selectTab(tab.id)}
        >
          {t(tab.key)}
        </button>
      {/each}
      <span class="tab-divider" role="none"></span>
      <span class="export-wrap">
        <PrimaryButton
          text={t('Export')}
          icon="file_download"
          enabled={canExport}
          tooltip={exportReason}
          onclick={() => (exportOpen = !exportOpen)}
        />
        {#if exportOpen}
          <ExportMenu
            tab={activeTab}
            canReport={dto.canExportReport}
            canTax={dto.canExportTax}
            {busy}
            {t}
            onpick={pickExport}
          />
        {/if}
      </span>
    </div>

    {#if exportOpen}
      <button
        type="button"
        class="backdrop"
        tabindex="-1"
        aria-hidden="true"
        onclick={() => (exportOpen = false)}
      ></button>
    {/if}

    <div class="content">
      {#if activeTab === 'recurring'}
        {#if dto.recurringEmpty}
          <ReportEmpty title={t('NoRecurringFound')} hint={t('RecurringHint')} />
        {:else}
          <div class="band start">
            <ReportFigure label={t('RecurringPayments')} value={dto.recurringCount} {compact} />
            <span class="figure-divider" role="none"></span>
            <ReportFigure
              label={t('CostPerYear')}
              value={dto.recurringYearly}
              tone="out"
              {compact}
            />
            <span class="figure-divider" role="none"></span>
            <ReportFigure label={t('PerMonth')} value={dto.recurringMonthly} tone="out" {compact} />
          </div>

          <SectionCard title={t('SubscriptionsTitle')}>
            <div class="recurring-head">
              <span class="recurring-name">{t('ColName')}</span>
              <span class="recurring-every">{t('ColEvery')}</span>
              <span class="recurring-next">{t('ColNext')}</span>
              <span class="recurring-amount">{t('ColAmount')}</span>
              <span class="recurring-yearly">{t('ColPerYear')}</span>
            </div>
            <div class="rule" role="none"></div>
            {#each dto.recurringRows as row (row.name)}
              <div class="recurring-row">
                <div class="recurring-title-cell">
                  <span class="recurring-row-name">{row.name}</span>
                  <span class="recurring-meta">{row.meta}</span>
                </div>
                <span class="recurring-every">{row.cadence}</span>
                <span class="recurring-next">{row.nextText}</span>
                <span class="recurring-amount" class:in={row.income}>{row.amountText}</span>
                <span class="recurring-yearly">{row.yearlyText}</span>
              </div>
            {/each}
          </SectionCard>
        {/if}
      {:else if !dto.hasReport}
        <div class="loading" aria-hidden="true"><span class="spinner small"></span></div>
      {:else if dto.empty}
        <ReportEmpty title={dto.emptyTitle} hint={dto.emptyHint} />
      {:else if activeTab === 'summary'}
        {#if dto.otherCurrencyHint !== ''}
          <p class="hint">{dto.otherCurrencyHint}</p>
        {/if}

        <div class="band">
          <ReportFigure label={t('TotalIncome')} value={dto.incomeValue} tone="in" {compact} />
          <span class="figure-divider" role="none"></span>
          <ReportFigure label={t('TotalExpenses')} value={dto.expensesValue} {compact} />
          <span class="figure-divider" role="none"></span>
          <ReportFigure
            label={t('NetIncome')}
            value={dto.netValue}
            tone={dto.netTone === 'negative' ? 'alert' : 'in'}
            {compact}
          />
          <span class="figure-divider" role="none"></span>
          <ReportFigure label={t('TaxRelevantAmount')} value={dto.taxValue} {compact} />
        </div>

        <SectionCard title={t('TaxRelevantByCategory')}>
          <TaxRelevantBars rows={dto.taxRelevantRows} emptyText={dto.taxRelevantEmpty} />
        </SectionCard>

        <div class="band">
          <ReportFigure label={t('StatTransactions')} value={dto.statTransactions} {compact} />
          <span class="figure-divider" role="none"></span>
          <ReportFigure label={t('CategoriesUsed')} value={dto.statCategories} {compact} />
          <span class="figure-divider" role="none"></span>
          <ReportFigure label={t('AvgMonthlyExpenses')} value={dto.statAverage} {compact} />
          <span class="figure-divider" role="none"></span>
          <ReportFigure label={t('HighestMonth')} value={dto.statHighest} {compact} />
        </div>
      {:else if activeTab === 'monthly'}
        <SectionCard title={dto.monthlyTitle}>
          <div class="monthly-head">
            <span>{t('ColMonth')}</span>
            <span>{t('ColIncome')}</span>
            <span>{t('ColExpenses')}</span>
            <span>{t('ColNet')}</span>
          </div>
          <div class="rule" role="none"></div>
          {#each dto.monthlyRows as row (row.month)}
            <div class="monthly-row">
              <span class="monthly-month">{row.month}</span>
              <span class="monthly-value" class:in={!row.incomeZero}>{row.incomeText}</span>
              <span class="monthly-value">{row.expensesText}</span>
              <span
                class="monthly-value"
                class:alert={row.netTone === 'negative'}
                class:in={row.netTone !== 'negative'}>{row.netText}</span
              >
            </div>
          {/each}
          <div class="monthly-row total">
            <span class="monthly-month">{t('ReportTotal')}</span>
            <span class="monthly-value" class:in={!dto.monthlyTotalIncomeZero}
              >{dto.monthlyTotalIncome}</span
            >
            <span class="monthly-value">{dto.monthlyTotalExpenses}</span>
            <span
              class="monthly-value"
              class:alert={dto.monthlyTotalTone === 'negative'}
              class:in={dto.monthlyTotalTone !== 'negative'}>{dto.monthlyTotalNet}</span
            >
          </div>
          <div class="rule" role="none"></div>
        </SectionCard>
      {:else if activeTab === 'category'}
        <SectionCard title={t('SpendingByCategory')}>
          <CategorySplit
            rows={dto.categoryRows}
            transfersNote={dto.transfersNote}
            selectable={dto.linkable}
            onselect={openCategory}
          />
        </SectionCard>

        <SectionCard title={t('TopVendors')}>
          <div class="vendor-list">
            {#each dto.vendorRows as row (row.rank)}
              <div class="vendor-row">
                <span class="vendor-rank">{row.rank}</span>
                <span class="vendor-name">{row.name}</span>
                <span class="vendor-amount">{row.amountText}</span>
              </div>
            {/each}
          </div>
        </SectionCard>
      {:else if activeTab === 'tax'}
        {#if dto.hasTax}
          <div class="stats">
            <ReportStat
              title={t('TaxIncomeTotal')}
              value={dto.taxIncomeValue}
              icon="add_circle_outline"
              tone="in"
              {compact}
            />
            <ReportStat
              title={t('TaxDeductionTotal')}
              value={dto.taxDeductionValue}
              icon="remove_circle_outline"
              {compact}
            />
            <ReportStat
              title={t('NetIncome')}
              value={dto.taxNetValue}
              icon="reports"
              tone={dto.taxNetTone === 'negative' ? 'alert' : 'in'}
              {compact}
            />
            <ReportStat
              title={t('TaxReceipts')}
              value={dto.taxReceiptsValue}
              subtitle={dto.taxReceiptsSubtitle}
              icon={dto.taxReceiptsMissing ? 'warning' : 'check_circle'}
              accent={dto.taxReceiptsMissing ? 'caution' : 'muted'}
              tone={dto.taxReceiptsMissing ? 'text' : 'success'}
              interactive={dto.taxReceiptsMissing}
              {compact}
              onclick={() => setMissingOnly(true)}
            />
          </div>

          {#if dto.taxOtherHint !== ''}
            <p class="hint">{dto.taxOtherHint}</p>
          {/if}

          {#if dto.taxSuggestions.length > 0}
            <SectionCard title={dto.taxSuggestionsTitle}>
              <p class="hint">{dto.taxSuggestionsHint}</p>
              {#each dto.taxSuggestions as suggestion (suggestion.id)}
                <div class="suggestion" data-testid="reports-suggestion">
                  <span class="suggestion-date">{suggestion.dateText}</span>
                  <div class="suggestion-text">
                    <span class="suggestion-title">{suggestion.title}</span>
                    <span class="suggestion-keyword">{suggestion.keywordText}</span>
                  </div>
                  <span class="suggestion-amount" class:in={!suggestion.expense}
                    >{suggestion.amountText}</span
                  >
                  <TextButton
                    text={t('Accept')}
                    enabled={!busy}
                    onclick={() => acceptSuggestion(suggestion.id)}
                  />
                  <TextButton
                    text={t('DismissSuggestion')}
                    enabled={!busy}
                    onclick={() => dismissSuggestion(suggestion.id)}
                  />
                </div>
              {/each}
            </SectionCard>
          {/if}

          <Checkbox
            text={t('MissingReceiptsOnly')}
            checked={dto.missingReceiptsOnly}
            enabled={!busy}
            ontoggled={setMissingOnly}
          />

          {#if dto.taxEmpty}
            <ReportEmpty title={dto.taxEmptyTitle} hint={dto.taxEmptyHint} />
          {:else}
            <div class="tax-rows">
              {#each dto.taxRows as row, index (index)}
                {#if row.kind === 'section'}
                  <div class="tax-section">{row.title}</div>
                {:else if row.kind === 'group'}
                  <div class="tax-group">
                    <span class="tax-group-dot" style:background={row.colour}></span>
                    <span class="tax-group-title">{row.title}</span>
                    <span class="tax-group-summary">{row.summary}</span>
                  </div>
                {:else}
                  <div class="tax-booking" data-tax-id={row.id}>
                    <span class="tax-booking-date">{row.dateText}</span>
                    <div class="tax-booking-body">
                      <div class="tax-booking-head">
                        <span class="tax-booking-title">{row.title}</span>
                        {#if row.accountText !== ''}
                          <span class="tax-booking-account">{row.accountText}</span>
                        {/if}
                      </div>
                      <div class="tax-booking-meta">
                        <span class="tax-booking-subtitle">{row.subtitle}</span>
                        {#if row.hasReceipt}
                          <span class="receipt-mark" aria-hidden="true">
                            <span></span>
                            <span></span>
                          </span>
                        {:else if row.missingReceipt}
                          <span class="no-receipt">{t('NoReceipt')}</span>
                        {/if}
                      </div>
                    </div>
                    <span class="tax-booking-amount" class:in={!row.expense}>{row.amountText}</span>
                    <div class="tax-booking-actions">
                      <TextButton
                        text={t('MarkNotTaxRelevant')}
                        enabled={!busy}
                        onclick={() => markNotTaxRelevant(row.id)}
                      />
                      {#if row.canReset}
                        <TextButton
                          text={t('ResetTaxAutomatic')}
                          enabled={!busy}
                          onclick={() => resetTaxAutomatic(row.id)}
                        />
                      {/if}
                    </div>
                  </div>
                {/if}
              {/each}
            </div>
          {/if}
        {:else}
          <ReportEmpty title={dto.taxEmptyTitle} hint={dto.taxEmptyHint} />
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .reports-page {
    position: relative;
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    gap: 12px;
    padding: 24px;
    background: var(--color-background);
  }

  .header {
    display: flex;
    flex: none;
    align-items: center;
    gap: 16px;
    height: 40px;
  }

  .mode {
    display: flex;
    flex: none;
    width: clamp(253px, calc(28px + 25vw), 380px);
    min-width: 0;
  }

  .mode :global(.segmented) {
    width: 100%;
  }

  .mode :global(.segment) {
    flex: 1 1 auto;
  }

  .custom-range {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .date-field {
    width: 128px;
    height: 34px;
    padding: 0 10px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-small);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .date-field::placeholder {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
  }

  .date-field:focus-visible {
    outline: none;
    border: 2px solid var(--color-primary);
  }

  .date-field:disabled {
    color: var(--color-muted-text);
  }

  .recurring-title {
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
  }

  .load-slot {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
  }

  .spacer {
    flex: 1;
  }

  .spinner,
  .spinner-small {
    display: block;
    border: 2px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border-width: 3px;
  }

  .spinner-small {
    width: 20px;
    height: 20px;
  }

  .spinner.small {
    width: 24px;
    height: 24px;
  }

  .loading {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    padding: 40px;
  }

  .range-error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .tabbar {
    position: relative;
    display: flex;
    flex: none;
    height: 44px;
    align-items: stretch;
    padding-right: 8px;
    overflow: visible;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .tab {
    position: relative;
    display: flex;
    flex: 1 1 auto;
    align-items: center;
    justify-content: center;
    min-width: 0;
    padding: 0 8px;
    border: 0;
    background: transparent;
    color: var(--color-muted-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .tab:first-child {
    border-top-left-radius: var(--radius-panel);
    border-bottom-left-radius: var(--radius-panel);
  }

  .tab.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .tab.selected::after {
    position: absolute;
    right: 0;
    bottom: 0;
    left: 0;
    height: 2px;
    background: var(--color-primary);
    content: '';
  }

  .tab:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .tab-divider {
    flex: none;
    align-self: center;
    width: 1px;
    height: 24px;
    background: var(--color-divider);
  }

  .export-wrap {
    position: relative;
    display: flex;
    flex: none;
    align-items: center;
    height: 36px;
    margin-left: 8px;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    z-index: 30;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .content {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
    scrollbar-gutter: stable;
    padding-bottom: 4px;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.45;
  }

  .band {
    display: flex;
    flex: none;
    align-items: center;
    padding: 16px 0;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .band.start {
    align-items: flex-start;
  }

  .figure-divider {
    flex: none;
    width: 1px;
    height: 44px;
    background: var(--color-divider);
  }

  .stats {
    display: grid;
    flex: none;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
  }

  .rule {
    flex: none;
    height: 1px;
    background: var(--color-divider);
  }

  .recurring-head,
  .recurring-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .recurring-head {
    height: 32px;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .recurring-row {
    height: 52px;
  }

  .recurring-row + .recurring-row {
    border-top: 1px solid var(--color-divider);
  }

  .recurring-name,
  .recurring-title-cell {
    flex: 1 1 0;
    min-width: 0;
  }

  .recurring-title-cell {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 2px;
  }

  .recurring-row-name {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .recurring-meta {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .recurring-every {
    flex: none;
    width: 110px;
    overflow: hidden;
    font-size: var(--type-body-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .recurring-next {
    flex: none;
    width: 100px;
    overflow: hidden;
    font-size: var(--type-body-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .recurring-amount {
    flex: none;
    width: 130px;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .recurring-amount.in {
    color: var(--color-money-in);
  }

  .recurring-yearly {
    flex: none;
    width: 130px;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .monthly-head,
  .monthly-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .monthly-head {
    height: 36px;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .monthly-head > span,
  .monthly-row > span {
    flex: 1 1 0;
    min-width: 0;
  }

  .monthly-head > span:not(:first-child) {
    text-align: right;
  }

  .monthly-row {
    height: 40px;
    border-bottom: 1px solid var(--color-divider);
  }

  .monthly-month {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .monthly-value {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .monthly-value.in {
    color: var(--color-money-in);
  }

  .monthly-value.alert {
    color: var(--color-error);
  }

  .monthly-row.total {
    font-weight: var(--type-title-medium-weight);
  }

  .monthly-row.total > span {
    color: var(--color-text);
    font-weight: var(--type-title-medium-weight);
  }

  .monthly-row.total .monthly-value.in {
    color: var(--color-money-in);
  }

  .monthly-row.total .monthly-value.alert {
    color: var(--color-error);
  }

  .vendor-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .vendor-row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 40px;
    border-bottom: 1px solid var(--color-divider);
  }

  .vendor-row:last-child {
    border-bottom: 0;
  }

  .vendor-rank {
    flex: none;
    width: 28px;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
  }

  .vendor-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .vendor-amount {
    flex: none;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
  }

  .suggestion {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    border-bottom: 1px solid var(--color-divider);
  }

  .suggestion:last-child {
    border-bottom: 0;
  }

  .suggestion-date {
    flex: none;
    width: 88px;
    color: var(--color-text);
    font-size: var(--type-body-size);
  }

  .suggestion-text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
    padding: 8px 0;
  }

  .suggestion-title {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .suggestion-keyword {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .suggestion-amount {
    flex: none;
    width: 110px;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .suggestion-amount.in {
    color: var(--color-money-in);
  }

  .tax-rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .tax-section {
    display: flex;
    height: 32px;
    align-items: flex-end;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
  }

  .tax-group {
    display: flex;
    height: 52px;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .tax-group-dot {
    display: block;
    flex: none;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-small);
  }

  .tax-group-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .tax-group-summary {
    flex: none;
    color: var(--color-text);
    font-size: var(--type-body-size);
  }

  .tax-booking {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .tax-booking-date {
    flex: none;
    width: 88px;
    color: var(--color-text);
    font-size: var(--type-body-size);
  }

  .tax-booking-body {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .tax-booking-head,
  .tax-booking-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .tax-booking-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .tax-booking-account {
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
  }

  .tax-booking-subtitle {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .no-receipt {
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
  }

  .receipt-mark {
    position: relative;
    display: block;
    flex: none;
    width: 14px;
    height: 16px;
    border: 1px solid var(--color-primary);
    border-radius: var(--radius-dot);
  }

  .receipt-mark span {
    position: absolute;
    left: 3px;
    display: block;
    width: 6px;
    height: 2px;
    background: var(--color-primary);
  }

  .receipt-mark span:first-child {
    top: 4px;
  }

  .receipt-mark span:last-child {
    top: 8px;
  }

  .tax-booking-amount {
    flex: none;
    width: 110px;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .tax-booking-amount.in {
    color: var(--color-money-in);
  }

  .tax-booking-actions {
    display: flex;
    flex: none;
    width: 180px;
    flex-direction: column;
  }

  .tax-booking-actions :global(.text) {
    width: 100%;
    min-width: 0;
    padding: 0 8px;
    justify-content: flex-start;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
