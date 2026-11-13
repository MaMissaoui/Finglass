<script lang="ts">
  import type { AccountsDialogDTO } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import type { Api, BankSuggestionDTO } from '../../lib/api';
  import { OutlinedField, PrimaryButton, SegmentedButton, TextButton } from '../../lib/components';

  let {
    api,
    dialog,
    t,
    onClose,
    onSubmitOffline,
    onSubmitOnline,
  }: {
    api: Api;
    dialog: AccountsDialogDTO;
    t: (key: string, ...args: (string | number)[]) => string;
    onClose: () => void;
    onSubmitOffline: (name: string) => void;
    onSubmitOnline: (
      name: string,
      blz: string,
      bic: string,
      userID: string,
      serverURL: string,
    ) => void;
  } = $props();

  // Offline is the historical default, as Rust's dialog-online starts false.
  let online = $state(false);
  let name = $state('');
  let blz = $state('');
  let bic = $state('');
  let userID = $state('');
  let serverURL = $state('');
  let search = $state('');
  let suggestions = $state<BankSuggestionDTO[]>([]);
  let centralNote = $state('');
  // The dialog's searches and BLZ changes are asynchronous; a newer edit wins.
  let searchToken = 0;
  let blzToken = 0;

  async function updateSearch(text: string): Promise<void> {
    search = text;
    const token = ++searchToken;
    try {
      const rows = await api.bankingSearchBanks(text);
      if (token === searchToken) {
        suggestions = rows;
      }
    } catch {
      if (token === searchToken) {
        suggestions = [];
      }
    }
  }

  async function chooseSuggestion(suggestion: BankSuggestionDTO): Promise<void> {
    const token = ++searchToken;
    try {
      const fields = await api.bankingPickBankSuggestion(
        suggestion.name,
        suggestion.blz,
        suggestion.bic,
      );
      if (token !== searchToken) {
        return;
      }
      name = fields.name;
      blz = fields.blz;
      bic = fields.bic;
      search = '';
      suggestions = [];
      centralNote = fields.centralNote;
    } catch {
      if (token === searchToken) {
        suggestions = [];
      }
    }
  }

  async function updateBlz(text: string): Promise<void> {
    const token = ++blzToken;
    try {
      const fields = await api.bankingBankBlzChanged(name, text, bic);
      if (token !== blzToken) {
        return;
      }
      name = fields.name;
      blz = fields.blz;
      bic = fields.bic;
      centralNote = fields.centralNote;
    } catch {
      if (token === blzToken) {
        blz = text;
        centralNote = '';
      }
    }
  }

  function submit(): void {
    if (online) {
      onSubmitOnline(name, blz, bic, userID, serverURL);
    } else {
      onSubmitOffline(name);
    }
  }
</script>

<div class="scrim">
  <dialog
    open
    class="modal add-bank"
    aria-modal="true"
    aria-label={t('AddBank')}
    data-testid="accounts-dialog"
  >
    <h2>{t('AddBank')}</h2>

    <div class="modes">
      <SegmentedButton
        labels={[t('BankOnlineMode'), t('BankOfflineMode')]}
        selected={online ? 0 : 1}
        onselect={(index) => (online = index === 0)}
      />
    </div>

    {#if online}
      <OutlinedField
        label={t('BankSearchHint')}
        bind:value={search}
        oninput={(value) => updateSearch(value)}
        onaccepted={(value) => updateSearch(value)}
      />

      {#if search !== ''}
        {#if suggestions.length === 0}
          <p class="hint">{t('BankSearchNoMatch')}</p>
        {:else}
          <div class="suggestions">
            {#each suggestions as suggestion (suggestion.blz)}
              <button type="button" class="suggestion" onclick={() => chooseSuggestion(suggestion)}>
                <span class="suggestion-name">{suggestion.name}</span>
                <span class="suggestion-detail">{suggestion.detail}</span>
              </button>
            {/each}
          </div>
        {/if}
      {/if}

      <p class="hint">{t('BankManualEntry')}</p>
    {/if}

    <OutlinedField
      label={t('BankName')}
      bind:value={name}
      onaccepted={(value) => {
        if (value !== '' && !online) {
          onSubmitOffline(value);
        }
      }}
    />

    {#if online}
      <OutlinedField
        label={t('BlzDigits')}
        bind:value={blz}
        oninput={(value) => updateBlz(value)}
      />

      {#if centralNote !== ''}
        <p class="hint">{centralNote}</p>
      {/if}

      <OutlinedField label={t('BicOptional')} bind:value={bic} />
      <OutlinedField label={t('UserIdLogin')} bind:value={userID} />
      <OutlinedField label={t('FintsServerUrl')} bind:value={serverURL} />

      <p class="hint">{t('AskBankForFints')}</p>
    {/if}

    {#if dialog.errorText !== ''}
      <p class="error" role="alert">{dialog.errorText}</p>
    {/if}

    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <PrimaryButton text={t('Add')} enabled={name !== ''} onclick={submit} />
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
    gap: 10px;
    width: 540px;
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

  .modes {
    display: flex;
    align-items: flex-start;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .suggestions {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 100px;
    overflow-y: auto;
  }

  .suggestion {
    display: flex;
    flex-direction: column;
    gap: 1px;
    width: 100%;
    padding: 6px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    text-align: left;
    cursor: pointer;
  }

  .suggestion:hover {
    background: var(--color-selected);
  }

  .suggestion:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .suggestion-name {
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    line-height: 1.3;
  }

  .suggestion-detail {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.3;
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
