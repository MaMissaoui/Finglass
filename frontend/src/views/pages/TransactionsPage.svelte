<script lang="ts">
  import { onMount } from 'svelte';
  import type { Api, ShellDTO, TransactionsDTO } from '../../lib/api';
  import {
    Icon,
    IconButton,
    MessageBanner,
    PrimaryButton,
    SearchField,
  } from '../../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../../lib/i18n';
  import AccountPanel from './AccountPanel.svelte';
  import AddTransactionDialog from './transactions/AddTransactionDialog.svelte';
  import BalanceChartCard from './transactions/BalanceChartCard.svelte';
  import AmountFilterDialog from './transactions/AmountFilterDialog.svelte';
  import CustomRangeDialog from './transactions/CustomRangeDialog.svelte';
  import DeleteTransactionDialog from './transactions/DeleteTransactionDialog.svelte';
  import DuplicateTransactionDialog from './transactions/DuplicateTransactionDialog.svelte';
  import FilterPill from './transactions/FilterPill.svelte';
  import ImportDialog from './transactions/ImportDialog.svelte';
  import MultiDeleteDialog from './transactions/MultiDeleteDialog.svelte';
  import ReapplyRulesDialog from './transactions/ReapplyRulesDialog.svelte';
  import RuleDialog from './transactions/RuleDialog.svelte';
  import TransactionDetail from './transactions/TransactionDetail.svelte';
  import TransactionList from './transactions/TransactionList.svelte';

  let {
    api,
    dto: shell,
  }: {
    api: Api;
    dto: ShellDTO;
  } = $props();

  let dto = $state<TransactionsDTO | null>(null);
  let loadFailed = $state(false);
  let searchText = $state('');
  let moreMenuOpen = $state(false);
  let windowWidth = $state(1440);
  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  const language = $derived<Language>(languageFromCode(shell.language));
  const panelShown = $derived(windowWidth >= 1000);
  const panelWide = $derived(windowWidth >= 1280);

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  async function run(action: () => Promise<TransactionsDTO>): Promise<void> {
    try {
      dto = await action();
      loadFailed = false;
    } catch {
      loadFailed = true;
    }
  }

  function searchChanged(text: string): void {
    searchText = text;
    if (searchTimer !== undefined) {
      clearTimeout(searchTimer);
    }
    searchTimer = setTimeout(() => {
      searchTimer = undefined;
      void run(() => api.transactionsSearch(text));
    }, 250);
  }

  function closeTopLayer(): void {
    if (dto === null) {
      return;
    }
    if (dto.ruleOpen) {
      void run(() => api.transactionsCloseRule());
      return;
    }
    if (dto.duplicateOpen) {
      void run(() => api.transactionsDismissDuplicate());
      return;
    }
    if (dto.addOpen) {
      void run(() => api.transactionsCloseAdd());
      return;
    }
    if (dto.multiDeleteOpen) {
      void run(() => api.transactionsCancelMultiDelete());
      return;
    }
    if (dto.reapplyOpen) {
      void run(() => api.transactionsCancelReapplyRules());
      return;
    }
    if (dto.importOpen) {
      void run(() => api.transactionsCancelImport());
      return;
    }
    if (dto.rangeOpen) {
      void run(() => api.transactionsCloseRange());
      return;
    }
    if (dto.amountOpen) {
      void run(() => api.transactionsCloseAmount());
      return;
    }
    if (dto.deleteOpen) {
      void run(() => api.transactionsCancelDelete());
      return;
    }
    if (dto.detailOpen) {
      void run(() => api.transactionsCloseDetail());
      return;
    }
    moreMenuOpen = false;
  }

  function openPanelAccounts(): void {
    void api.navigate('accounts');
  }

  function noteTone(tone: string, flow: boolean): string {
    if (flow) {
      return tone === 'positive' ? 'in' : '';
    }
    if (tone === 'negative') {
      return 'negative';
    }
    if (tone === 'loan' || tone === 'plain' || tone === 'zero') {
      return 'muted';
    }
    return '';
  }

  onMount(() => {
    let disposed = false;
    const stop = api.onTransactionsChanged((next) => {
      if (!disposed) {
        dto = next;
      }
    });

    void api
      .transactionsState()
      .then((next) => {
        if (!disposed) {
          dto = next;
        }
      })
      .catch(() => {
        if (!disposed) {
          loadFailed = true;
        }
      });

    return () => {
      disposed = true;
      stop();
      if (searchTimer !== undefined) {
        clearTimeout(searchTimer);
      }
    };
  });
</script>

<svelte:window
  bind:innerWidth={windowWidth}
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      closeTopLayer();
    }
  }}
/>

<div class="transactions-page" data-testid="transactions-page">
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
    {#if panelShown && dto.hasAccounts}
      <div class="panel-column" class:wide={panelWide}>
        <AccountPanel
          rows={dto.panelRows}
          groups={dto.groupChoices}
          filterText={dto.panelFilterText}
          shownText={dto.panelShownText}
          {t}
          pageSearch={false}
          onFilter={(text) => run(() => api.transactionsPanelFilterChanged(text))}
          onSelect={(kind, groupKind, id, accountType) =>
            run(() => api.transactionsPanelSelect(kind, groupKind, id, accountType))}
          onToggle={(kind, groupKind, id, accountType) =>
            run(() => api.transactionsPanelToggle(kind, groupKind, id, accountType))}
          onMarkRead={(kind, groupKind, id, accountType) =>
            run(() => api.transactionsPanelMarkRead(kind, groupKind, id, accountType))}
          onNewGroup={(name) => run(() => api.transactionsPanelNewGroup(name))}
          onRenameGroup={(id, name) => run(() => api.transactionsPanelRenameGroup(id, name))}
          onDeleteGroup={(id) => run(() => api.transactionsPanelDeleteGroup(id))}
          onMoveGroup={(id, up) => run(() => api.transactionsPanelMoveGroup(id, up))}
          onMoveAccount={(accountID, groupID) =>
            run(() => api.transactionsPanelMoveAccount(accountID, groupID))}
        />
      </div>
    {/if}

    <div class="main">
      <div class="summary">
        <div class="figures">
          <div class="figure">
            <span class="figure-label">{t('SummaryCount')}</span>
            <span class="figure-count">{dto.countText}</span>
          </div>
          <div class="figure">
            <span class="figure-label">{t('SummaryIn')}</span>
            {#each dto.incomeAmounts as amount (amount.text)}
              <span class="figure-amount in">{amount.text}</span>
            {/each}
          </div>
          <div class="figure">
            <span class="figure-label">{t('SummaryOut')}</span>
            {#each dto.expenseAmounts as amount (amount.text)}
              <span class="figure-amount">{amount.text}</span>
            {/each}
          </div>
          {#if dto.hasBalance}
            <div class="figure">
              <span class="figure-label">{t('SummaryBalance')}</span>
              <span
                class="figure-amount"
                class:negative={noteTone(dto.balanceTone, false) === 'negative'}
                class:muted={noteTone(dto.balanceTone, false) === 'muted'}>{dto.balanceText}</span
              >
            </div>
          {/if}
        </div>
        <div class="summary-spacer"></div>
        <span class="more-button" data-testid="transactions-more-button">
          <IconButton
            icon="more_vert"
            label={t('More')}
            filled
            onclick={() => (moreMenuOpen = !moreMenuOpen)}
          />
        </span>
        {#if dto.hasAccounts}
          <PrimaryButton
            text={t('Add')}
            icon="add"
            onclick={() => run(() => api.transactionsOpenAdd())}
          />
        {/if}
      </div>

      {#if dto.errorText !== '' || dto.messageText !== ''}
        <MessageBanner
          text={dto.errorText !== '' ? dto.errorText : dto.messageText}
          isError={dto.errorText !== ''}
          dismissLabel={t('Dismiss')}
          ondismiss={() => run(() => api.transactionsDismissMessage())}
        />
      {/if}

      <div class="filters-area">
        <div class="search-wrap" data-page-search>
          <SearchField
            value={searchText}
            placeholder={t('SearchPartnerPurpose')}
            clearLabel={t('ClearSearch')}
            oninput={searchChanged}
          />
        </div>
        <div class="filters">
          <FilterPill
            label={dto.periodExtra !== ''
              ? dto.periodExtra
              : (dto.periodOptions[dto.periodIndex] ?? '')}
            options={dto.periodOptions}
            selected={dto.periodExtra !== '' ? -1 : dto.periodIndex}
            icon="date_range"
            onchosen={(index) => {
              if (index === (dto?.periodOptions.length ?? 0) - 1) {
                void run(() => api.transactionsOpenRange());
              } else {
                void run(() => api.transactionsPeriodChanged(index));
              }
            }}
          />
          <FilterPill
            label={dto.categoryOptions[dto.categoryIndex] ?? ''}
            options={dto.categoryOptions}
            selected={dto.categoryIndex}
            onchosen={(index) => run(() => api.transactionsCategoryChanged(index))}
          />
          {#if !panelShown && dto.hasAccounts}
            <FilterPill
              label={dto.accountOptions[dto.accountIndex] ?? ''}
              options={dto.accountOptions}
              selected={dto.accountIndex}
              onchosen={(index) => run(() => api.transactionsAccountChanged(index))}
            />
          {/if}
          <FilterPill
            label={dto.directionOptions[dto.directionIndex] ?? ''}
            options={dto.directionOptions}
            selected={dto.directionIndex}
            onchosen={(index) => run(() => api.transactionsDirectionChanged(index))}
          />
          <FilterPill
            label={dto.amountLabel}
            options={[]}
            opensMenu={false}
            onactivate={() => run(() => api.transactionsOpenAmount())}
          />
        </div>
      </div>

      {#if dto.showBack}
        <button
          type="button"
          class="back"
          onclick={() => {
            searchText = '';
            void run(() => api.transactionsBack());
          }}
        >
          <Icon name="arrow_back" size={18} />
          <span>{t('Back')}</span>
        </button>
      {/if}

      <TransactionList
        {dto}
        {t}
        onOpen={(id) => run(() => api.transactionsOpenDetail(id))}
        onToggleSelect={(id) => run(() => api.transactionsToggleSelect(id))}
        onSelectRange={(id) => run(() => api.transactionsSelectRange(id))}
        onShowPartner={(partner) => run(() => api.transactionsShowPartner(partner))}
        onOpenDelete={(id) => run(() => api.transactionsOpenDelete(id))}
        onSelectAll={() => run(() => api.transactionsSelectAll())}
        onClearSelection={() => run(() => api.transactionsClearSelection())}
        onOpenMultiDelete={() => run(() => api.transactionsOpenMultiDelete())}
        onBatchCategory={(index) => run(() => api.transactionsBatchCategory(index))}
        onBatchTax={(index) => run(() => api.transactionsBatchTax(index))}
        onOpenAccounts={openPanelAccounts}
      />

      {#if dto.hasChart}
        <BalanceChartCard {dto} {t} onToggle={() => run(() => api.transactionsToggleChart())} />
      {/if}
    </div>

    {#if dto.detailOpen}
      <TransactionDetail
        {dto}
        {t}
        onClose={() => run(() => api.transactionsDetailClose())}
        onCategory={(index) => run(() => api.transactionsDetailCategory(index))}
        onTax={(index) => run(() => api.transactionsDetailTax(index))}
        onShowPartner={(partner) => run(() => api.transactionsDetailShowPartner(partner))}
        onAttachReceipt={() => run(() => api.transactionsAttachReceipt())}
        onCreateRule={() => run(() => api.transactionsDetailCreateRule())}
      />
    {/if}

    {#if dto.rangeOpen}
      {#key `${dto.rangeFrom}:${dto.rangeTo}`}
        <CustomRangeDialog
          from={dto.rangeFrom}
          to={dto.rangeTo}
          error={dto.rangeError}
          {t}
          onApply={(from, to) => run(() => api.transactionsApplyRange(from, to))}
          onClose={() => run(() => api.transactionsCloseRange())}
        />
      {/key}
    {/if}

    {#if dto.amountOpen}
      {#key `${dto.amountMin}:${dto.amountMax}`}
        <AmountFilterDialog
          minimum={dto.amountMin}
          maximum={dto.amountMax}
          minLabel={dto.amountMinLabel}
          maxLabel={dto.amountMaxLabel}
          error={dto.amountError}
          {t}
          onApply={(minimum, maximum) => run(() => api.transactionsApplyAmount(minimum, maximum))}
          onReset={() => run(() => api.transactionsResetAmount())}
          onClose={() => run(() => api.transactionsCloseAmount())}
        />
      {/key}
    {/if}

    {#if dto.addOpen}
      <AddTransactionDialog
        initialDate={dto.addDate}
        initialAccountIndex={dto.addAccountIndex}
        accounts={dto.addAccountOptions}
        categoryNames={dto.addCategoryNames}
        amountLabel={dto.addAmountLabel}
        error={dto.addError}
        {t}
        onClose={() => run(() => api.transactionsCloseAdd())}
        onSubmit={(accountIndex, expense, date, amount, partner, purpose, categoryIndex) =>
          run(() =>
            api.transactionsAddSubmit(
              accountIndex,
              expense,
              date,
              amount,
              partner,
              purpose,
              categoryIndex,
            ),
          )}
      />
    {/if}

    {#if dto.duplicateOpen}
      <DuplicateTransactionDialog
        title={dto.duplicateTitle}
        text={dto.duplicateText}
        {t}
        onConfirm={() => run(() => api.transactionsConfirmDuplicate())}
        onDismiss={() => run(() => api.transactionsDismissDuplicate())}
      />
    {/if}

    {#if dto.multiDeleteOpen}
      <MultiDeleteDialog
        title={dto.multiDeleteTitle}
        range={dto.multiDeleteRange}
        totals={dto.multiDeleteTotals}
        lines={dto.multiDeleteLines}
        more={dto.multiDeleteMore}
        {t}
        onConfirm={() => run(() => api.transactionsConfirmMultiDelete())}
        onCancel={() => run(() => api.transactionsCancelMultiDelete())}
      />
    {/if}

    {#if dto.ruleOpen}
      <RuleDialog
        pattern={dto.rulePattern}
        categories={dto.ruleCategories}
        categoryIndex={dto.ruleCategoryIndex}
        error={dto.ruleError}
        {t}
        onSave={(pattern, categoryIndex) =>
          run(() => api.transactionsSaveRule(pattern, categoryIndex))}
        onClose={() => run(() => api.transactionsCloseRule())}
      />
    {/if}

    {#if dto.reapplyOpen}
      <ReapplyRulesDialog
        summary={dto.reapplySummary}
        warning={dto.reapplyWarning}
        groups={dto.reapplyGroups}
        confirmText={dto.reapplyConfirmText}
        confirmEnabled={dto.reapplyConfirmEnabled}
        {t}
        onToggle={(from, to) => run(() => api.transactionsToggleReapplyGroup(from, to))}
        onConfirm={() => run(() => api.transactionsConfirmReapplyRules())}
        onCancel={() => run(() => api.transactionsCancelReapplyRules())}
      />
    {/if}

    {#if dto.importOpen}
      <ImportDialog
        title={dto.importTitle}
        summary={dto.importSummary}
        files={dto.importFiles}
        csvFields={dto.importCsvFields}
        csvHint={dto.importCsvHint}
        accountOptions={dto.importAccountOptions}
        accountIndex={dto.importAccountIndex}
        balanceAllowed={dto.importBalanceAllowed}
        updateBalance={dto.importUpdateBalance}
        balanceText={dto.importBalanceText}
        newerText={dto.importNewerText}
        importing={dto.importing}
        confirmText={dto.importConfirmText}
        confirmEnabled={dto.importConfirmEnabled}
        {t}
        onAccount={(index) => run(() => api.transactionsImportAccountChanged(index))}
        onCsvChanged={(field, value) => run(() => api.transactionsImportCSVChanged(field, value))}
        onConfirm={(updateBalance) => run(() => api.transactionsConfirmImport(updateBalance))}
        onCancel={() => run(() => api.transactionsCancelImport())}
      />
    {/if}

    {#if dto.deleteOpen}
      {#key dto.deleteId}
        <DeleteTransactionDialog
          id={dto.deleteId}
          title={dto.deleteTitle}
          text={dto.deleteText}
          {t}
          onConfirm={(id) => run(() => api.transactionsConfirmDelete(id))}
          onCancel={() => run(() => api.transactionsCancelDelete())}
        />
      {/key}
    {/if}

    {#if moreMenuOpen}
      <div class="more-overlay">
        <button
          type="button"
          class="more-backdrop"
          tabindex="-1"
          aria-hidden="true"
          onclick={() => (moreMenuOpen = false)}
        ></button>
        <div
          class="more-menu"
          role="menu"
          aria-label={t('More')}
          data-testid="transactions-more-menu"
        >
          <button
            type="button"
            role="menuitem"
            class="more-item"
            disabled={!dto.hasAccounts}
            onclick={() => {
              moreMenuOpen = false;
              void run(() => api.transactionsOpenImport());
            }}
          >
            {t('Import')}
          </button>
          <button
            type="button"
            role="menuitem"
            class="more-item"
            onclick={() => {
              moreMenuOpen = false;
              void run(() => api.transactionsAutoCategorize());
            }}
          >
            {t('AutoCategorize')}
          </button>
          <button
            type="button"
            role="menuitem"
            class="more-item"
            disabled={!dto.hasTransactions}
            onclick={() => {
              moreMenuOpen = false;
              void run(() => api.transactionsReapplyRules());
            }}
          >
            {t('ReapplyRules')}
          </button>
          {#if dto.canUndoReapply}
            <button
              type="button"
              role="menuitem"
              class="more-item"
              onclick={() => {
                moreMenuOpen = false;
                void run(() => api.transactionsUndoReapply());
              }}
            >
              {t('UndoReapplyRules')}
            </button>
          {/if}
          <button
            type="button"
            role="menuitem"
            class="more-item"
            disabled={!dto.hasTransactions}
            onclick={() => {
              moreMenuOpen = false;
              void run(() => api.transactionsExportCSV());
            }}
          >
            {t('ExportCsv')}
          </button>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .transactions-page {
    position: relative;
    display: flex;
    height: 100%;
    min-height: 0;
    gap: 16px;
    padding: 20px;
    overflow: hidden;
    background: var(--color-background);
  }

  .panel-column {
    display: flex;
    flex: none;
    width: 300px;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }

  .panel-column.wide {
    width: 320px;
  }

  .panel-column :global(.panel) {
    flex: 1;
    min-width: 0;
  }

  .main {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
    flex-direction: column;
    gap: 10px;
  }

  .summary {
    display: flex;
    flex: none;
    align-items: flex-start;
    gap: 8px;
  }

  .figures {
    display: flex;
    min-width: 0;
    align-items: flex-start;
    gap: 28px;
  }

  .figure {
    display: flex;
    min-width: 64px;
    flex-direction: column;
    gap: 1px;
  }

  .figure-label {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .figure-count,
  .figure-amount {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .figure-amount.in {
    color: var(--color-money-in);
  }

  .figure-amount.negative {
    color: var(--color-error);
  }

  .figure-amount.muted {
    color: var(--color-muted-text);
  }

  .summary-spacer {
    flex: 1;
  }

  .more-button {
    display: inline-flex;
    flex: none;
  }

  .filters-area {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    gap: 8px;
  }

  .search-wrap {
    flex: 1 1 240px;
    min-width: 240px;
  }

  .filters {
    display: flex;
    flex: 0 1 auto;
    flex-wrap: wrap;
    gap: 8px;
  }

  .back {
    display: inline-flex;
    flex: none;
    align-items: center;
    align-self: flex-start;
    gap: 8px;
    height: var(--size-control-height-large);
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .back:hover {
    background: var(--color-surface);
    color: var(--color-primary-hover);
  }

  .back:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .more-overlay {
    position: absolute;
    inset: 0;
    z-index: 30;
  }

  .more-backdrop {
    position: absolute;
    inset: 0;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .more-menu {
    position: absolute;
    top: 52px;
    right: 28px;
    display: flex;
    width: 260px;
    flex-direction: column;
    padding: 4px;
    overflow: hidden;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .more-item {
    display: flex;
    align-items: center;
    height: 36px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .more-item:hover:not(:disabled) {
    background: var(--color-background);
  }

  .more-item:disabled {
    color: var(--color-muted-text);
    cursor: default;
  }

  .more-item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .loading {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
  }

  .spinner {
    display: block;
    width: 32px;
    height: 32px;
    border: 3px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
