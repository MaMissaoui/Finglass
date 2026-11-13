<script lang="ts">
  import type { TransactionsDTO } from '../../../lib/api';
  import { Icon } from '../../../lib/components';
  import ActionPicker from './ActionPicker.svelte';
  import TransactionRow from './TransactionRow.svelte';

  let {
    dto,
    t,
    onOpen,
    onToggleSelect,
    onSelectRange,
    onShowPartner,
    onOpenDelete,
    onSelectAll,
    onClearSelection,
    onOpenMultiDelete,
    onBatchCategory,
    onBatchTax,
    onOpenAccounts,
  }: {
    dto: TransactionsDTO;
    t: (key: string) => string;
    onOpen: (id: number) => void;
    onToggleSelect: (id: number) => void;
    onSelectRange: (id: number) => void;
    onShowPartner: (partner: string) => void;
    onOpenDelete: (id: number) => void;
    onSelectAll: () => void;
    onClearSelection: () => void;
    onOpenMultiDelete: () => void;
    onBatchCategory: (index: number) => void;
    onBatchTax: (index: number) => void;
    onOpenAccounts: () => void;
  } = $props();
</script>

<div class="list" data-testid="transactions-list">
  {#if dto.selectionVisible}
    <div class="selection-bar" data-testid="selection-bar">
      <span class="selection-count">{dto.selectionCountText}</span>
      <span class="selection-totals">{dto.selectionTotalsText}</span>
      <div class="selection-spacer"></div>
      <ActionPicker
        label={t('CategoryMenu')}
        options={dto.categoryNames}
        primary
        onchosen={onBatchCategory}
      />
      <ActionPicker label={t('TaxMenu')} options={dto.taxOptions} onchosen={onBatchTax} />
      <button type="button" class="text-action" onclick={onSelectAll}>{t('SelectAll')}</button>
      <button type="button" class="text-action" onclick={onClearSelection}>
        {t('ClearSelection')}
      </button>
      <button type="button" class="text-action" onclick={onOpenMultiDelete}>
        {t('DeleteSelected')}
      </button>
    </div>
  {/if}

  <div class="header">
    {#if dto.rows.length > 0}
      <button
        type="button"
        class="select-all"
        role="checkbox"
        aria-checked={dto.allSelected}
        aria-label={t('SelectAll')}
        onclick={() => (dto.allSelected ? onClearSelection() : onSelectAll())}
      >
        <span class="box" class:checked={dto.allSelected}>
          {#if dto.allSelected}
            <Icon name="check" size={14} />
          {/if}
        </span>
      </button>
    {:else}
      <span class="select-all-space"></span>
    {/if}
    <span class="col-date">{t('ColDate')}</span>
    <span class="col-description">{t('ColDescription')}</span>
    <span class="col-category">{t('ColCategory')}</span>
    <span class="col-amount">{t('ColAmount')}</span>
    {#if dto.hasBalanceColumn}
      <span class="col-balance">{t('ColBalance')}</span>
    {/if}
    <span class="col-action"></span>
  </div>

  <div class="divider" role="none"></div>

  {#if dto.pendingHeader !== ''}
    <div class="pending-header">
      <span>{dto.pendingHeader}</span>
    </div>
  {/if}

  <div class="rows">
    {#if dto.rows.length === 0 && !dto.loading}
      <div class="empty" data-testid="transactions-empty">
        <span class="empty-icon" aria-hidden="true">☰</span>
        <p class="empty-title">{t('NoTransactions')}</p>
        <p>{dto.hasAccounts ? t('NothingMatches') : t('NoTransactionsAddBank')}</p>
        {#if !dto.hasAccounts}
          <button type="button" class="add-bank" onclick={onOpenAccounts}>
            <Icon name="add" size={18} />
            <span>{t('AddBank')}</span>
          </button>
        {/if}
      </div>
    {:else if dto.rows.length === 0 && dto.loading}
      <div class="loading" aria-hidden="true"><span class="spinner"></span></div>
    {:else}
      {#each dto.rows as row, index (`${row.pending ? 'p' : 'b'}:${row.id}:${index}`)}
        <TransactionRow
          {row}
          hasBalanceColumn={dto.hasBalanceColumn}
          {t}
          {onOpen}
          {onToggleSelect}
          {onSelectRange}
          {onShowPartner}
          onDelete={onOpenDelete}
        />
      {/each}
    {/if}
  </div>
</div>

<style>
  .list {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    overflow: hidden;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .selection-bar {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
    height: 48px;
    padding: 0 8px 0 12px;
    background: var(--color-background);
  }

  .selection-count {
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
  }

  .selection-totals {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .selection-spacer {
    flex: 1;
  }

  .text-action {
    flex: none;
    height: 32px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    cursor: pointer;
  }

  .text-action:hover {
    background: var(--color-surface);
    color: var(--color-primary-hover);
  }

  .text-action:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .header {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
    height: 34px;
    padding: 0 4px 0 10px;
  }

  .select-all {
    display: inline-flex;
    flex: none;
    width: 24px;
    height: 34px;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: pointer;
  }

  .select-all-space {
    display: block;
    flex: none;
    width: 24px;
  }

  .box {
    display: inline-flex;
    width: 18px;
    height: 18px;
    align-items: center;
    justify-content: center;
    border: 2px solid var(--color-muted-text);
    border-radius: var(--radius-hairline);
    background: transparent;
    color: var(--color-on-primary);
  }

  .box.checked {
    border-color: var(--color-primary);
    background: var(--color-primary);
  }

  .select-all:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .col-date,
  .col-description,
  .col-category,
  .col-amount,
  .col-balance {
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .col-date {
    flex: none;
    width: 82px;
  }

  .col-description {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .col-category {
    flex: none;
    width: 112px;
  }

  .col-amount {
    flex: none;
    width: 106px;
    text-align: right;
  }

  .col-balance {
    flex: none;
    width: 106px;
    text-align: right;
  }

  .col-action {
    flex: none;
    width: 30px;
  }

  .divider {
    flex: none;
    height: 1px;
    background: var(--color-divider);
  }

  .pending-header {
    display: flex;
    flex: none;
    align-items: center;
    height: 34px;
    padding: 0 12px;
    background: var(--color-background);
  }

  .pending-header span {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .rows {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    overflow-y: auto;
  }

  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 24px;
    text-align: center;
  }

  .empty-icon {
    color: var(--color-muted-text);
    font-size: var(--type-amount-size);
  }

  .empty-title {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .empty p {
    max-width: 460px;
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.45;
  }

  .add-bank {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: var(--size-control-height);
    padding: 0 14px;
    border: 0;
    border-radius: var(--radius-control);
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .add-bank:hover {
    background: var(--color-primary-hover);
  }

  .loading {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
  }

  .spinner {
    display: block;
    width: 28px;
    height: 28px;
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
