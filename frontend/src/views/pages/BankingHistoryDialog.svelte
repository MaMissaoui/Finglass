<script lang="ts">
  import type { AccountsDialogDTO } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { OutlinedField, PrimaryButton, TextButton } from '../../lib/components';

  let {
    dialog,
    t,
    onClose,
    onClear,
    onFetch,
  }: {
    dialog: AccountsDialogDTO;
    t: (key: string) => string;
    onClose: () => void;
    onClear: (bankID: number) => void;
    onFetch: (bankID: number, date: string) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let date = $state(dialog.historyDate);

  function fetchHistory(): void {
    onFetch(dialog.bankId, date);
  }
</script>

<div class="scrim">
  <dialog
    open
    class="modal history"
    aria-modal="true"
    aria-label={dialog.title}
    data-testid="accounts-dialog"
  >
    <h2>{dialog.title}</h2>

    <OutlinedField
      label={t('FetchHistoryDate')}
      bind:value={date}
      error={dialog.errorText !== ''}
      supportingText={dialog.errorText !== '' ? dialog.errorText : t('FetchHistoryHint')}
      onaccepted={(value) => {
        if (value !== '') {
          onFetch(dialog.bankId, value);
        }
      }}
    />

    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      {#if dialog.historyHasStart}
        <TextButton text={t('ClearHistoryStart')} onclick={() => onClear(dialog.bankId)} />
      {/if}
      <PrimaryButton text={t('Fetch')} enabled={date !== ''} onclick={fetchHistory} />
    </div>
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
    width: 500px;
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

  .modal > * {
    flex: none;
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
</style>
