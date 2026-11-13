<script lang="ts">
  import { onMount } from 'svelte';
  import type { AccountsDTO, Api, ShellDTO } from '../../lib/api';
  import { Icon, MessageBanner, PrimaryButton } from '../../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../../lib/i18n';
  import AccountDetail from './AccountDetail.svelte';
  import AccountPanel from './AccountPanel.svelte';
  import AccountsBankCard from './AccountsBankCard.svelte';
  import AccountsDialog from './AccountsDialog.svelte';

  let {
    api,
    dto,
  }: {
    api: Api;
    dto: ShellDTO;
  } = $props();

  let accounts = $state<AccountsDTO | null>(null);
  let loadFailed = $state(false);

  const language = $derived<Language>(languageFromCode(dto.language));
  const panelPreview = $derived(
    import.meta.env.MODE === 'mock' &&
      new URLSearchParams(window.location.search).get('scenario') === 'accounts-panel',
  );

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  onMount(() => {
    let disposed = false;
    const stop = api.onAccountsChanged((next) => {
      if (!disposed) {
        accounts = next;
      }
    });

    void api
      .accountsState()
      .then((next) => {
        if (!disposed) {
          accounts = next;
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
    };
  });

  async function run(action: () => Promise<AccountsDTO>): Promise<void> {
    try {
      accounts = await action();
    } catch {
      loadFailed = true;
    }
  }

  function closeTopLayer(): void {
    if (accounts?.dialog.kind !== '' && accounts !== null) {
      void run(() => api.accountsCloseDialog());
      return;
    }
    if (accounts?.detailOpen === true && accounts !== null) {
      void run(() => api.accountsCloseDetail());
    }
  }

  function noteAmountTone(tone: string, secondary: boolean): string {
    if (secondary) {
      return 'muted';
    }
    return tone === 'negative' ? 'negative' : '';
  }

  function toggleBank(bankID: number): void {
    void run(() => api.accountsToggleBank(bankID));
  }

  function openAddAccount(bankID: number): void {
    void run(() => api.accountsOpenAddAccount(bankID));
  }

  function openDeleteBank(bankID: number): void {
    void run(() => api.accountsOpenDeleteBank(bankID));
  }

  function openDetail(accountID: number): void {
    void run(() => api.accountsOpenDetail(accountID));
  }

  function openTransactions(accountID: number): void {
    void run(() => api.accountsOpenTransactions(accountID));
  }

  function openRename(accountID: number): void {
    void run(() => api.accountsOpenRename(accountID));
  }

  function openBalance(accountID: number): void {
    void run(() => api.accountsOpenBalance(accountID));
  }

  function forgetDeleted(accountID: number): void {
    void run(() => api.accountsForgetDeleted(accountID));
  }

  function openDeleteAccount(accountID: number): void {
    void run(() => api.accountsOpenDeleteAccount(accountID));
  }

  function selectRow(kind: string, groupKind: string, id: number, accountType: string): void {
    void run(() => api.accountsSelect(kind, groupKind, id, accountType));
  }

  function toggleGroup(kind: string, groupKind: string, id: number, accountType: string): void {
    void run(() => api.accountsToggleGroup(kind, groupKind, id, accountType));
  }

  function markRead(kind: string, groupKind: string, id: number, accountType: string): void {
    void run(() => api.accountsMarkRead(kind, groupKind, id, accountType));
  }
</script>

<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      closeTopLayer();
    }
  }}
/>

<div class="accounts-page" class:preview-mode={panelPreview} data-testid="accounts-page">
  {#if panelPreview && accounts !== null}
    <div class="preview" data-testid="accounts-panel-preview">
      <div class="preview-panel">
        <AccountPanel
          rows={accounts.panelRows}
          groups={accounts.groupChoices}
          filterText={accounts.filterText}
          shownText={accounts.shownText}
          {t}
          onFilter={(text) => run(() => api.accountsFilterChanged(text))}
          onSelect={selectRow}
          onToggle={toggleGroup}
          onMarkRead={markRead}
          onNewGroup={(name) => run(() => api.accountsNewGroup(name))}
          onRenameGroup={(id, name) => run(() => api.accountsRenameGroup(id, name))}
          onDeleteGroup={(id) => run(() => api.accountsDeleteGroup(id))}
          onMoveGroup={(id, up) => run(() => api.accountsMoveGroup(id, up))}
          onMoveAccount={(accountID, groupID) =>
            run(() => api.accountsMoveAccount(accountID, groupID))}
        />
      </div>
    </div>
  {:else if accounts === null}
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
    {#if accounts.showContent}
      <div class="figures-row">
        <div class="figures">
          {#each accounts.figures as figure (figure.label)}
            <div class="figure">
              <span class="figure-label">{figure.label}</span>
              <div class="figure-values">
                {#each figure.values as amount, index (amount.text)}
                  {#if index > 0}
                    <span class="separator">·</span>
                  {/if}
                  <span
                    class="figure-amount"
                    class:muted={noteAmountTone(amount.tone, figure.secondary) === 'muted'}
                    class:negative={noteAmountTone(amount.tone, figure.secondary) === 'negative'}
                    >{amount.text}</span
                  >
                {/each}
              </div>
            </div>
          {/each}
        </div>
        <PrimaryButton
          text={t('AddBank')}
          icon="add"
          onclick={() => run(() => api.accountsOpenAddBank())}
        />
      </div>
    {/if}

    {#if accounts.hasContent}
      <div class="note">
        <span class="note-icon"><Icon name="info" size={20} /></span>
        <p>{t('OnlineSyncNotYetAvailable')}</p>
      </div>
    {/if}

    {#if accounts.errorText !== '' || accounts.messageText !== ''}
      <MessageBanner
        text={accounts.errorText !== '' ? accounts.errorText : accounts.messageText}
        isError={accounts.errorText !== ''}
        dismissLabel={t('Dismiss')}
        ondismiss={() => run(() => api.accountsDismissMessage())}
      />
    {/if}

    {#if accounts.loading && !accounts.hasContent}
      <div class="loading" aria-hidden="true"><span class="spinner"></span></div>
    {:else if !accounts.loading && !accounts.hasContent}
      <div class="empty" data-testid="accounts-empty">
        <svg class="empty-icon" viewBox="0 0 64 56" aria-hidden="true">
          <path
            d="M 8 24 L 32 8 L 56 24 Z M 14 24 L 14 48 L 20 48 L 20 24 Z M 26 24 L 26 48 L 32 48 L 32 24 Z M 38 24 L 38 48 L 44 48 L 44 24 Z M 50 24 L 50 48 L 56 48 L 56 24 Z M 8 48 L 56 48 L 56 54 L 8 54 Z"
          ></path>
        </svg>
        <h2>{t('NoBanksYet')}</h2>
        <p>{t('NoBanksDescription')}</p>
        <PrimaryButton
          text={t('AddBank')}
          icon="add"
          onclick={() => run(() => api.accountsOpenAddBank())}
        />
      </div>
    {:else}
      <div class="cards">
        {#each accounts.bankCards as card (card.bankId)}
          <AccountsBankCard
            {card}
            {t}
            onToggle={toggleBank}
            onAddAccount={openAddAccount}
            onOpenDeleteBank={openDeleteBank}
            onOpenDetail={openDetail}
            onOpenTransactions={openTransactions}
            onOpenRename={openRename}
            onOpenBalance={openBalance}
            onForgetDeleted={forgetDeleted}
            onOpenDeleteAccount={openDeleteAccount}
          />
        {/each}
      </div>
    {/if}

    {#if accounts.dialog.kind !== ''}
      {#key `${accounts.dialog.kind}:${accounts.dialog.bankId}:${accounts.dialog.accountId}`}
        <AccountsDialog
          dialog={accounts.dialog}
          typeChoices={accounts.typeChoices}
          currencyCodes={accounts.currencyCodes}
          {t}
          onClose={() => run(() => api.accountsCloseDialog())}
          onAddBank={(name) => run(() => api.accountsSubmitAddBank(name))}
          onAddAccount={(bankID, name, identifier, accountType, currency, balance) =>
            run(() =>
              api.accountsSubmitAddAccount(
                bankID,
                name,
                identifier,
                accountType,
                currency,
                balance,
              ),
            )}
          onBalance={(accountID, balance, accountType) =>
            run(() => api.accountsSubmitBalance(accountID, balance, accountType))}
          onRename={(accountID, name) => run(() => api.accountsSubmitRename(accountID, name))}
          onDeleteAccount={(accountID) => run(() => api.accountsConfirmDeleteAccount(accountID))}
          onDeleteBank={(bankID) => run(() => api.accountsConfirmDeleteBank(bankID))}
        />
      {/key}
    {/if}

    {#if accounts.detailOpen}
      <AccountDetail
        {accounts}
        daysChoices={accounts.daysChoices}
        dayLabels={accounts.dayLabels}
        {t}
        onSelectDays={(accountID, days) => run(() => api.accountsSelectDays(accountID, days))}
        onClose={() => run(() => api.accountsCloseDetail())}
        onRename={openRename}
      />
    {/if}
  {/if}
</div>

<style>
  .accounts-page {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 16px;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    padding: 24px;
    background: var(--color-background);
  }

  .accounts-page.preview-mode {
    gap: 0;
    padding: 0;
  }

  .figures-row {
    display: flex;
    flex: none;
    align-items: flex-start;
    gap: 32px;
  }

  .figures {
    display: flex;
    flex: 1;
    min-width: 0;
    align-items: flex-start;
    gap: 32px;
  }

  .figure {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .figure-label {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .figure-values {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .figure-amount {
    color: var(--color-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
    white-space: nowrap;
  }

  .figure-amount.muted {
    color: var(--color-muted-text);
  }

  .figure-amount.negative {
    color: var(--color-error);
  }

  .separator {
    color: var(--color-muted-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
  }

  .note {
    display: flex;
    flex: none;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .note-icon {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  .note p {
    flex: 1;
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .cards {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
    padding-right: 2px;
  }

  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    text-align: center;
  }

  .empty-icon {
    width: 64px;
    height: 56px;
    fill: var(--color-muted-text);
  }

  .empty h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .empty p {
    width: min(620px, 100%);
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.45;
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

  .preview {
    display: flex;
    flex: none;
    align-items: flex-start;
    justify-content: center;
    width: 360px;
    height: 720px;
    padding: 24px 20px;
    background: var(--color-background);
  }

  .preview-panel {
    display: flex;
    width: 320px;
    height: 672px;
  }

  .preview-panel :global(.panel) {
    flex: 1;
  }
</style>
