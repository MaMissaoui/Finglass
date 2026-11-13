<script lang="ts">
  import type { AccountsDialogDTO } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { OutlinedField, PrimaryButton, TextButton } from '../../lib/components';

  let {
    dialog,
    t,
    onClose,
    onForgetPin,
    onSave,
  }: {
    dialog: AccountsDialogDTO;
    t: (key: string) => string;
    onClose: () => void;
    onForgetPin: (bankID: number) => void;
    onSave: (bankID: number, name: string, blz: string, userID: string, serverURL: string) => void;
  } = $props();

  // The parent keys this component by dialog identity, so these are the
  // opening values of the current dialog and intentionally read once.
  // svelte-ignore state_referenced_locally
  let name = $state(dialog.connectionName);
  // svelte-ignore state_referenced_locally
  let blz = $state(dialog.connectionBlz);
  // svelte-ignore state_referenced_locally
  let userID = $state(dialog.connectionUserId);
  // svelte-ignore state_referenced_locally
  let serverURL = $state(dialog.connectionServerUrl);

  function save(): void {
    onSave(dialog.bankId, name, blz, userID, serverURL);
  }
</script>

<div class="scrim">
  <dialog
    open
    class="modal connection"
    aria-modal="true"
    aria-label={dialog.title}
    data-testid="accounts-dialog"
  >
    <h2>{dialog.title}</h2>

    <OutlinedField label={t('ConnectionName')} bind:value={name} />
    <p class="hint">{t('ConnectionNameHint')}</p>

    {#if !dialog.offline}
      <OutlinedField label={t('BlzDigits')} bind:value={blz} />
      <OutlinedField label={t('UserIdLogin')} bind:value={userID} />
      <OutlinedField label={t('FintsServerUrl')} bind:value={serverURL} />

      {#if dialog.connectionPinText !== ''}
        <p class="hint">{dialog.connectionPinText}</p>
      {/if}

      {#if dialog.connectionHasPin}
        <div class="forget">
          <TextButton
            text={t('ForgetPin')}
            enabled={!dialog.connectionSyncing}
            onclick={() => onForgetPin(dialog.bankId)}
          />
        </div>
      {/if}
    {/if}

    {#if dialog.errorText !== ''}
      <p class="error" role="alert">{dialog.errorText}</p>
    {/if}

    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <PrimaryButton
        text={t('Save')}
        enabled={name !== '' && !dialog.connectionSyncing}
        onclick={save}
      />
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
    width: 520px;
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

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .forget {
    display: flex;
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
