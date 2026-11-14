<script lang="ts">
  import type {
    AccountsBankCardDTO,
    BankAccountRowDTO,
    ConnectionTestDTO,
  } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import {
    BankBadge,
    CountBadge,
    Icon,
    IconButton,
    SecondaryButton,
    TextButton,
  } from '../../lib/components';
  import type { IconName } from '../../lib/icons';

  let {
    card,
    t,
    connectionTest = null,
    syncControlsVisible = false,
    onToggle,
    onSync,
    onAddAccount,
    onOpenDetail,
    onOpenTransactions,
    onOpenRename,
    onOpenBalance,
    onForgetDeleted,
    onOpenDeleteAccount,
    onBankMenu,
    onSaveTrace,
  }: {
    card: AccountsBankCardDTO;
    t: (key: string, ...args: (string | number)[]) => string;
    connectionTest?: ConnectionTestDTO | null;
    syncControlsVisible?: boolean;
    onToggle: (bankID: number) => void;
    onSync: (bankID: number) => void;
    onAddAccount: (bankID: number) => void;
    onOpenDetail: (accountID: number) => void;
    onOpenTransactions: (accountID: number) => void;
    onOpenRename: (accountID: number) => void;
    onOpenBalance: (accountID: number) => void;
    onForgetDeleted: (accountID: number) => void;
    onOpenDeleteAccount: (accountID: number) => void;
    onBankMenu: (bankID: number, action: string) => void;
    onSaveTrace: () => void;
  } = $props();

  let bankMenuOpen = $state(false);
  let accountMenu = $state(-1);

  const hasBank = $derived(card.bankId >= 0);
  const showSync = $derived(hasBank && !card.offline && !card.syncing && syncControlsVisible);

  function toneClass(tone: string): string {
    if (tone === 'negative') {
      return 'negative';
    }
    if (tone === 'loan' || tone === 'plain' || tone === 'zero') {
      return 'muted';
    }
    return '';
  }

  function accountTone(account: BankAccountRowDTO): string {
    return toneClass(account.tone);
  }

  function toggleBankMenu(): void {
    accountMenu = -1;
    bankMenuOpen = !bankMenuOpen;
  }

  function toggleAccountMenu(accountID: number): void {
    bankMenuOpen = false;
    accountMenu = accountMenu === accountID ? -1 : accountID;
  }

  function chooseBankAction(action: string): void {
    bankMenuOpen = false;
    onBankMenu(card.bankId, action);
  }

  function chooseAccount(accountID: number, action: () => void): void {
    accountMenu = -1;
    action();
  }
</script>

<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      bankMenuOpen = false;
      accountMenu = -1;
    }
  }}
/>

<article class="bank-card" data-testid={`bank-card-${card.bankId}`}>
  <header class="bank-head">
    {#if !hasBank}
      <span class="chevron-space" aria-hidden="true"></span>
    {:else}
      <button
        type="button"
        class="chevron"
        aria-label={card.collapsed ? t('Expand') : t('Collapse')}
        title={card.collapsed ? t('Expand') : t('Collapse')}
        onclick={() => onToggle(card.bankId)}
      >
        <Icon name={card.collapsed ? 'keyboard_arrow_right' : 'keyboard_arrow_down'} size={20} />
      </button>
    {/if}

    <BankBadge
      logo={card.logo}
      letters={card.logoLetters}
      colourIndex={card.logoColour}
      size={32}
    />

    <div class="bank-text">
      <div class="bank-title-line">
        <span class="bank-name">{card.name}</span>
        <span class="bank-count">{card.accountCountText}</span>
        {#each card.totals as total (total.text)}
          <span
            class="bank-total"
            class:muted={toneClass(total.tone) === 'muted'}
            class:negative={toneClass(total.tone) === 'negative'}>{total.text}</span
          >
        {/each}
      </div>
      {#if card.subtitle !== ''}
        <p class="bank-subtitle">{card.subtitle}</p>
      {/if}
      {#if card.centralBlzHint !== ''}
        <p class="bank-subtitle">{card.centralBlzHint}</p>
      {/if}
    </div>

    <div class="bank-actions">
      {#if card.syncing}
        <span class="sync-status" data-testid={`bank-sync-${card.bankId}`}>
          <span class="spinner" aria-hidden="true"></span>
          <span class="sync-step">{card.syncStep}</span>
        </span>
      {:else if showSync}
        <SecondaryButton
          text={t('Sync')}
          icon="refresh"
          enabled={card.syncEnabled}
          tooltip={card.syncTooltip}
          onclick={() => onSync(card.bankId)}
        />
      {/if}

      {#if hasBank}
        <TextButton
          text={t('AddAccountShort')}
          icon="add"
          onclick={() => onAddAccount(card.bankId)}
        />
      {/if}

      {#if hasBank}
        <div class="menu-anchor">
          <IconButton icon="more_vert" label={t('More')} filled onclick={toggleBankMenu} />
          {#if bankMenuOpen}
            <div class="bank-menu" role="menu" aria-label={t('More')}>
              {#each card.menu as item (item.action)}
                <button
                  type="button"
                  role={item.action === 'include' ? 'menuitemcheckbox' : 'menuitem'}
                  class="menu-entry"
                  class:danger={item.danger}
                  disabled={!item.enabled}
                  aria-checked={item.action === 'include' ? item.checked : undefined}
                  onclick={() => chooseBankAction(item.action)}
                >
                  <span class="menu-icon">
                    {#if item.icon !== '' && (item.icon !== 'check' || item.checked)}
                      <Icon name={item.icon as IconName} size={24} />
                    {/if}
                  </span>
                  <span class="menu-label">{item.label}</span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </header>

  {#if connectionTest !== null && connectionTest.bankId === card.bankId}
    <div class="connection-test" data-testid={`connection-test-${card.bankId}`} aria-live="polite">
      {#if connectionTest.running}
        <div class="test-line">
          <span class="spinner" aria-hidden="true"></span>
          <span class="test-reason">{t('TestConnectionRunning')}</span>
        </div>
      {:else}
        <div class="test-line" class:warning={!connectionTest.go}>
          <span class="test-icon">
            <Icon name={connectionTest.go ? 'check_circle' : 'warning'} size={20} />
          </span>
          <span class="test-reason">{connectionTest.reason}</span>
          <span class="test-spacer"></span>
          {#if connectionTest.hasTrace}
            <SecondaryButton
              text={t('SaveConnectionTrace')}
              icon="file_download"
              onclick={onSaveTrace}
            />
          {/if}
        </div>

        {#if connectionTest.codes.length > 0}
          <div class="test-codes">
            {#each connectionTest.codes as code (code)}
              <span class="code-chip">{code}</span>
            {/each}
          </div>
        {/if}

        <div class="test-meta">
          <span>{t('TestConnectionProbed', connectionTest.probed)}</span>
          <span>{t('TestConnectionSkipped', connectionTest.skipped)}</span>
        </div>

        <p class="test-help">{t('TestConnectionHelp')}</p>
      {/if}
    </div>
  {/if}

  {#if !card.collapsed}
    <div class="rule" role="none"></div>

    <div class="accounts">
      {#if card.accounts.length === 0}
        <p class="no-accounts">{t('NoAccountsYet')}</p>
      {/if}

      {#each card.accounts as account (account.accountId)}
        <div class="account-block">
          <div
            class="account-row"
            role="button"
            tabindex="0"
            data-testid={`account-row-${account.accountId}`}
            onclick={() => {
              accountMenu = -1;
              bankMenuOpen = false;
              onOpenDetail(account.accountId);
            }}
            onkeydown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                accountMenu = -1;
                onOpenDetail(account.accountId);
              }
            }}
          >
            <BankBadge
              logo={account.logo}
              letters={account.logoLetters}
              colourIndex={account.logoColour}
              size={24}
            />
            <div class="account-text">
              <span class="account-name">{account.name}</span>
              <span class="account-subtitle">{account.subtitle}</span>
            </div>
            <span class="badge-column">
              <CountBadge count={account.newCount} />
            </span>
            <span
              class="account-amount"
              class:muted={accountTone(account) === 'muted'}
              class:negative={accountTone(account) === 'negative'}>{account.balanceText}</span
            >
            <button
              type="button"
              class="row-icon transactions"
              aria-label={t('ShowTransactions')}
              title={t('ShowTransactions')}
              onclick={(event) => {
                event.stopPropagation();
                onOpenTransactions(account.accountId);
              }}
            >
              <Icon name="list" size={18} />
            </button>
            <button
              type="button"
              class="row-icon"
              aria-label={t('More')}
              title={t('More')}
              aria-expanded={accountMenu === account.accountId}
              onclick={(event) => {
                event.stopPropagation();
                toggleAccountMenu(account.accountId);
              }}
            >
              <Icon name="more_vert" size={18} />
            </button>
          </div>

          {#if accountMenu === account.accountId}
            <div class="account-actions" data-testid={`account-actions-${account.accountId}`}>
              <TextButton
                text={t('MenuDetails')}
                onclick={() =>
                  chooseAccount(account.accountId, () => onOpenDetail(account.accountId))}
              />
              <TextButton
                text={t('RenameAccount')}
                onclick={() =>
                  chooseAccount(account.accountId, () => onOpenRename(account.accountId))}
              />
              <TextButton
                text={t('EditBalance')}
                onclick={() =>
                  chooseAccount(account.accountId, () => onOpenBalance(account.accountId))}
              />
              <TextButton
                text={t('AllowDeletedAgain')}
                onclick={() =>
                  chooseAccount(account.accountId, () => onForgetDeleted(account.accountId))}
              />
              <TextButton
                text={t('DeleteAccount')}
                onclick={() =>
                  chooseAccount(account.accountId, () => onOpenDeleteAccount(account.accountId))}
              />
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</article>

<style>
  .bank-card {
    position: relative;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .bank-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 16px;
  }

  .chevron-space {
    flex: none;
    width: 24px;
    height: 24px;
  }

  .chevron,
  .row-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-icon);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .chevron:hover,
  .row-icon:hover {
    background: var(--color-divider);
  }

  .chevron:focus-visible,
  .row-icon:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  .bank-text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    justify-content: center;
    gap: 1px;
  }

  .bank-title-line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .bank-name {
    margin: 0;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    line-height: 1;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .bank-count {
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
  }

  .bank-total {
    flex: none;
    color: var(--color-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .bank-total.muted,
  .account-amount.muted {
    color: var(--color-muted-text);
  }

  .bank-total.negative,
  .account-amount.negative {
    color: var(--color-error);
  }

  .bank-subtitle {
    margin: 0;
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .bank-actions {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
  }

  .sync-status {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 0 4px;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
  }

  .sync-step {
    display: inline-block;
  }

  .spinner {
    display: inline-block;
    width: 18px;
    height: 18px;
    border: 2px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .menu-anchor {
    position: relative;
    display: inline-flex;
  }

  .bank-menu {
    position: absolute;
    z-index: 12;
    right: 0;
    top: 0;
    width: 300px;
    padding: 8px 0;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .menu-entry {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 48px;
    padding: 8px 12px;
    border: 0;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    cursor: pointer;
  }

  .menu-entry:hover:not(:disabled) {
    background: var(--color-background);
  }

  .menu-entry:disabled {
    color: var(--color-muted-text);
    cursor: default;
  }

  .menu-entry:disabled .menu-icon {
    opacity: 0.38;
  }

  .menu-entry.danger:not(:disabled) {
    color: var(--color-error);
  }

  .menu-icon {
    display: inline-flex;
    flex: none;
    width: 24px;
    height: 24px;
    align-items: center;
    justify-content: center;
  }

  .menu-label {
    flex: 1;
  }

  .connection-test {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0 16px 12px;
    padding: 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-background);
  }

  .test-line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    color: var(--color-success);
  }

  .test-line.warning {
    color: var(--color-warning-strong);
  }

  .test-icon {
    display: inline-flex;
    flex: none;
  }

  .test-reason {
    min-width: 0;
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .test-spacer {
    flex: 1;
  }

  .test-codes {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .code-chip {
    display: inline-flex;
    align-items: center;
    height: 22px;
    padding: 0 8px;
    border-radius: var(--radius-tag);
    background: var(--color-control-fill);
    color: var(--color-text);
    font-family: ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace;
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .test-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .test-help {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .rule {
    height: 1px;
    margin: 0 16px;
    background: var(--color-divider);
  }

  .accounts {
    display: flex;
    flex-direction: column;
    padding: 8px 10px 12px;
  }

  .account-block {
    display: flex;
    flex-direction: column;
  }

  .account-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 52px;
    padding: 0 6px;
    border-radius: var(--radius-small);
    cursor: pointer;
  }

  .account-row:hover {
    background: var(--color-background);
  }

  .row-icon.transactions {
    opacity: 0;
  }

  .account-row:hover .row-icon.transactions,
  .account-row:focus-within .row-icon.transactions {
    opacity: 1;
  }

  .account-row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .account-text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 1px;
  }

  .account-name {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .account-subtitle {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .badge-column {
    display: flex;
    flex: none;
    width: 20px;
    align-items: center;
    justify-content: center;
  }

  .account-amount {
    flex: none;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    white-space: nowrap;
  }

  .account-actions {
    display: flex;
    gap: 10px;
    padding: 3px 6px;
    overflow-x: auto;
    background: var(--color-background);
  }

  .no-accounts {
    margin: 0;
    padding: 8px 6px;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
  }
</style>
