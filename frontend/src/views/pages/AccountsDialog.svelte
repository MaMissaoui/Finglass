<script lang="ts">
  import type {
    AccountTypeChoiceDTO,
    AccountsDialogDTO,
  } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { OutlinedField, PrimaryButton, SelectPill, TextButton } from '../../lib/components';

  let {
    dialog,
    typeChoices,
    currencyCodes,
    t,
    onClose,
    onAddBank,
    onAddAccount,
    onBalance,
    onRename,
    onDeleteAccount,
    onDeleteBank,
  }: {
    dialog: AccountsDialogDTO;
    typeChoices: AccountTypeChoiceDTO[];
    currencyCodes: string[];
    t: (key: string) => string;
    onClose: () => void;
    onAddBank: (name: string) => void;
    onAddAccount: (
      bankID: number,
      name: string,
      identifier: string,
      accountType: string,
      currency: string,
      balance: string,
    ) => void;
    onBalance: (accountID: number, balance: string, accountType: string) => void;
    onRename: (accountID: number, name: string) => void;
    onDeleteAccount: (accountID: number) => void;
    onDeleteBank: (bankID: number) => void;
  } = $props();

  let bankName = $state('');
  // The parent keys this component by dialog identity, so these are the
  // opening values of the current dialog and intentionally read once.
  // svelte-ignore state_referenced_locally
  let accountName = $state(dialog.accountName);
  let identifier = $state('');
  // svelte-ignore state_referenced_locally
  let balance = $state(dialog.balance === '' ? '0' : dialog.balance);
  // svelte-ignore state_referenced_locally
  let accountType = $state(dialog.accountType === '' ? 'checking' : dialog.accountType);
  let currency = $state('EUR');
  // svelte-ignore state_referenced_locally
  let renameName = $state(dialog.accountName);

  const typeLabels = $derived(typeChoices.map((choice) => choice.label));
  const typeLabel = $derived(
    typeChoices.find((choice) => choice.code === accountType)?.label ?? accountType,
  );

  function chooseType(index: number): void {
    const choice = typeChoices[index];
    if (choice !== undefined) {
      accountType = choice.code;
    }
  }

  function submitAddAccount(): void {
    onAddAccount(dialog.bankId, accountName, identifier, accountType, currency, balance);
  }

  function submitBalance(): void {
    onBalance(dialog.accountId, balance, accountType);
  }

  function submitRename(): void {
    onRename(dialog.accountId, renameName);
  }
</script>

<div class="scrim" data-testid="accounts-dialog">
  <dialog
    open
    class="modal"
    aria-modal="true"
    aria-label={dialog.kind === 'add-bank' ? t('AddBank') : dialog.title}
  >
    {#if dialog.kind === 'add-bank'}
      <h2>{t('AddBank')}</h2>
      <OutlinedField
        label={t('BankName')}
        bind:value={bankName}
        onaccepted={(value) => {
          if (value !== '') {
            onAddBank(value);
          }
        }}
      />
      {#if dialog.errorText !== ''}
        <p class="error" role="alert">{dialog.errorText}</p>
      {/if}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={onClose} />
        <PrimaryButton
          text={t('Add')}
          enabled={bankName !== ''}
          onclick={() => onAddBank(bankName)}
        />
      </div>
    {:else if dialog.kind === 'add-account'}
      <h2>{dialog.title}</h2>
      <OutlinedField label={t('AccountName')} bind:value={accountName} />
      <OutlinedField
        label={dialog.offline ? t('IbanOrCardLast4') : t('Iban')}
        bind:value={identifier}
      />
      <div class="picker-row">
        <SelectPill value={typeLabel} options={typeLabels} onchosen={chooseType} />
      </div>
      <div class="picker-row">
        <SelectPill
          labelPrefix={`${t('ColCurrency')}: `}
          value={currency}
          options={currencyCodes}
          onvaluechosen={(value) => (currency = value)}
        />
      </div>
      <OutlinedField label={t('ColBalance')} bind:value={balance} />
      {#if accountType === 'loan'}
        <p class="hint">{t('LoanBalanceHint')}</p>
      {/if}
      {#if dialog.errorText !== ''}
        <p class="error" role="alert">{dialog.errorText}</p>
      {/if}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={onClose} />
        <PrimaryButton
          text={t('Add')}
          enabled={identifier !== '' && balance !== ''}
          onclick={submitAddAccount}
        />
      </div>
    {:else if dialog.kind === 'balance'}
      <h2>{t('UpdateBalance')}</h2>
      <p class="account-label">{dialog.accountLabel}</p>
      <div class="picker-row">
        <SelectPill value={typeLabel} options={typeLabels} onchosen={chooseType} />
      </div>
      <OutlinedField
        label={t('ColBalance')}
        bind:value={balance}
        onaccepted={(value) => {
          if (value !== '') {
            onBalance(dialog.accountId, value, accountType);
          }
        }}
      />
      {#if accountType === 'loan'}
        <p class="hint">{t('LoanBalanceHint')}</p>
      {/if}
      {#if dialog.errorText !== ''}
        <p class="error" role="alert">{dialog.errorText}</p>
      {/if}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={onClose} />
        <PrimaryButton text={t('Save')} enabled={balance !== ''} onclick={submitBalance} />
      </div>
    {:else if dialog.kind === 'rename'}
      <h2>{t('RenameAccount')}</h2>
      <OutlinedField
        label={t('AccountName')}
        bind:value={renameName}
        onaccepted={(value) => onRename(dialog.accountId, value)}
      />
      {#if dialog.errorText !== ''}
        <p class="error" role="alert">{dialog.errorText}</p>
      {/if}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={onClose} />
        <PrimaryButton text={t('Save')} onclick={submitRename} />
      </div>
    {:else if dialog.kind === 'delete-account' || dialog.kind === 'delete-bank'}
      <h2>{dialog.title}</h2>
      <p class="body-text">{dialog.text}</p>
      {#if dialog.errorText !== ''}
        <p class="error" role="alert">{dialog.errorText}</p>
      {/if}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={onClose} />
        {#if dialog.kind === 'delete-account'}
          <PrimaryButton
            text={t('Delete')}
            destructive
            onclick={() => onDeleteAccount(dialog.accountId)}
          />
        {:else}
          <PrimaryButton
            text={t('RemoveBank')}
            destructive
            onclick={() => onDeleteBank(dialog.bankId)}
          />
        {/if}
      </div>
    {/if}
  </dialog>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 40;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    overflow-y: auto;
    background: var(--color-scrim);
  }

  .modal {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: min(540px, 100%);
    max-width: calc(100% - 32px);
    max-height: calc(100% - 32px);
    margin: 0;
    padding: 24px;
    overflow-y: auto;
    border: 0;
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

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }

  .picker-row {
    display: flex;
    align-items: flex-start;
  }

  .hint,
  .account-label,
  .body-text {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .body-text {
    font-size: var(--type-body-size);
  }

  .error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }
</style>
