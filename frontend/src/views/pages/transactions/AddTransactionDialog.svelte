<script lang="ts">
  import {
    OutlinedField,
    PrimaryButton,
    SegmentedButton,
    SelectPill,
    TextButton,
  } from '../../../lib/components';

  let {
    initialDate,
    initialAccountIndex,
    accounts,
    categoryNames,
    amountLabel,
    error,
    t,
    onClose,
    onSubmit,
  }: {
    initialDate: string;
    initialAccountIndex: number;
    accounts: string[];
    categoryNames: string[];
    amountLabel: string;
    error: string;
    t: (key: string) => string;
    onClose: () => void;
    onSubmit: (
      accountIndex: number,
      expense: boolean,
      date: string,
      amount: string,
      partner: string,
      purpose: string,
      categoryIndex: number,
    ) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let accountIndex = $state(initialAccountIndex);
  // svelte-ignore state_referenced_locally
  let dateText = $state(initialDate);
  let expense = $state(true);
  let amountText = $state('');
  let partnerText = $state('');
  let purposeText = $state('');
  let categoryIndex = $state(0);

  const accountValue = $derived(accounts[accountIndex] ?? '');
  const categoryValue = $derived(categoryNames[categoryIndex] ?? '');
  const canSubmit = $derived(
    dateText !== '' &&
      amountText !== '' &&
      (partnerText.trim() !== '' || purposeText.trim() !== ''),
  );
</script>

<div class="scrim" data-testid="add-transaction-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={t('AddTransaction')}>
    <h2>{t('AddTransaction')}</h2>

    <div class="row">
      <SelectPill
        value={accountValue}
        options={accounts}
        onchosen={(index) => (accountIndex = index)}
      />
    </div>

    <div class="row">
      <SegmentedButton
        labels={[t('Expense'), t('Income')]}
        selected={expense ? 0 : 1}
        onselect={(index) => (expense = index === 0)}
      />
    </div>

    <div class="row fields">
      <OutlinedField label={t('DateFormat')} bind:value={dateText} />
      <OutlinedField label={amountLabel} bind:value={amountText} />
    </div>

    <OutlinedField label={t('PartnerPayee')} bind:value={partnerText} />
    <OutlinedField label={t('Purpose')} bind:value={purposeText} />

    <div class="row">
      <SelectPill
        value={categoryValue}
        options={categoryNames}
        onchosen={(index) => (categoryIndex = index)}
      />
    </div>

    {#if error !== ''}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <PrimaryButton
        text={t('Add')}
        enabled={canSubmit}
        onclick={() =>
          onSubmit(
            accountIndex,
            expense,
            dateText,
            amountText,
            partnerText,
            purposeText,
            categoryIndex,
          )}
      />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(500px, 100%);
    max-height: 100%;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow-y: auto;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .fields {
    align-items: flex-start;
  }

  .fields :global(.outlined-field) {
    flex: 1;
    min-width: 0;
  }

  .error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
