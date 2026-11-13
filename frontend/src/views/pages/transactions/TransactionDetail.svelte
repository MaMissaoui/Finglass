<script lang="ts">
  import type { TransactionsDTO } from '../../../lib/api';
  import { Icon, SelectPill, TextButton } from '../../../lib/components';

  let {
    dto,
    t,
    onClose,
    onCategory,
    onTax,
    onShowPartner,
    onCreateRule,
  }: {
    dto: TransactionsDTO;
    t: (key: string) => string;
    onClose: () => void;
    onCategory: (index: number) => void;
    onTax: (index: number) => void;
    onShowPartner: (partner: string) => void;
    onCreateRule: () => void;
  } = $props();

  const categoryValue = $derived(dto.categoryNames[dto.detailCategoryIndex] ?? '');
  const taxValue = $derived(dto.taxOptions[dto.detailTaxIndex] ?? '');
</script>

<div class="scrim" data-testid="transaction-detail">
  <div class="modal" role="dialog" aria-modal="true" aria-label={dto.detailTitle}>
    <div class="content">
      <h2>{dto.detailTitle}</h2>
      <p class="amount" class:expense={dto.detailExpense}>{dto.detailAmount}</p>

      <div class="detail-row">
        <span class="detail-label">{t('DetailDate')}</span>
        <span class="detail-value">{dto.detailDate}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">{t('DetailAccount')}</span>
        <span class="detail-value">{dto.detailAccount}</span>
      </div>
      {#if dto.detailPartner !== ''}
        <div class="detail-row">
          <span class="detail-label">{t('DetailPartner')}</span>
          <span class="detail-value">{dto.detailPartner}</span>
        </div>
      {/if}
      {#if dto.detailShop !== ''}
        <div class="detail-row">
          <span class="detail-label">{t('DetailShop')}</span>
          <span class="detail-value">{dto.detailShop}</span>
        </div>
      {/if}
      {#if dto.detailPurpose !== ''}
        <div class="detail-row">
          <span class="detail-label">{t('DetailPurpose')}</span>
          <span class="detail-value">{dto.detailPurpose}</span>
        </div>
      {/if}

      <div class="pill-row">
        <SelectPill
          labelPrefix={`${t('DetailCategory')}: `}
          value={categoryValue}
          options={dto.categoryNames}
          onchosen={onCategory}
        />
        {#if dto.detailTaxRelevant}
          <span class="tax-tag">{t('TaxRelevant')}</span>
        {/if}
      </div>

      <div class="tax-row">
        <SelectPill
          labelPrefix={`${t('TaxMenu')}: `}
          value={taxValue}
          options={dto.taxOptions}
          onchosen={onTax}
        />
        <span class="tax-state">{dto.detailTaxState}</span>
      </div>

      {#if dto.detailHasMismatch}
        <div class="mismatch">
          <span class="mismatch-icon"><Icon name="warning" size={20} /></span>
          <span>{dto.detailMismatch}</span>
        </div>
      {/if}

      {#if dto.detailPartner !== ''}
        <div class="partner-link">
          <TextButton
            text={dto.detailPartnerLink}
            onclick={() => onShowPartner(dto.detailPartner)}
          />
        </div>
      {/if}

      <div class="rule" role="none"></div>

      <div class="footer">
        {#if dto.detailCanCreateRule}
          <TextButton text={t('CreateRule')} onclick={onCreateRule} />
        {/if}
        <TextButton text={t('Close')} onclick={onClose} />
      </div>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 45;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    max-height: 100%;
    width: min(560px, 100%);
    overflow-y: auto;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .amount {
    margin: 0;
    color: var(--color-money-in);
    font-size: var(--type-headline-size);
    font-weight: var(--type-headline-weight);
  }

  .amount.expense {
    color: var(--color-money-out);
  }

  .detail-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .detail-label {
    flex: none;
    width: 88px;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .detail-value {
    flex: 1;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    overflow-wrap: anywhere;
  }

  .pill-row,
  .tax-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .tax-row {
    align-items: flex-start;
  }

  .tax-tag {
    display: inline-flex;
    align-items: center;
    height: 22px;
    padding: 0 8px;
    border-radius: var(--radius-tag);
    background: var(--color-warning-pale);
    color: var(--color-warning-strong);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .tax-state {
    flex: 1;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .mismatch {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 10px;
    border: 1px solid var(--color-caution);
    border-radius: var(--radius-panel);
    background: var(--color-background);
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .mismatch-icon {
    display: inline-flex;
    flex: none;
    color: var(--color-caution);
  }

  .partner-link {
    display: flex;
    justify-content: flex-start;
    margin: 0 0 0 -12px;
  }

  .rule {
    height: 1px;
    background: var(--color-divider);
  }

  .footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
