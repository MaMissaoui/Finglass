<script lang="ts">
  import type {
    AccountsBankCardDTO,
    BankAccountRowDTO,
    ConnectionTestDTO,
    LastTraceDTO,
  } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { BankBadge, CountBadge, Icon, SecondaryButton, TextButton } from '../../lib/components';
  import type { IconName } from '../../lib/icons';

  let {
    card,
    t,
    connectionTest = null,
    lastTrace = null,
    syncControlsVisible = false,
    menuKey = null,
    onMenuChange,
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
    lastTrace?: LastTraceDTO | null;
    syncControlsVisible?: boolean;
    menuKey?: string | null;
    onMenuChange: (key: string | null) => void;
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
    onSaveTrace: (bankID: number) => void;
  } = $props();

  const hasBank = $derived(card.bankId >= 0);
  const bankMenuKey = $derived(`bank:${card.bankId}`);
  const bankMenuOpen = $derived(menuKey === bankMenuKey);
  let bankMenuEl = $state<HTMLDivElement | null>(null);
  let bankTriggerEl = $state<HTMLButtonElement | null>(null);
  let bankMenuStyle = $state('');

  $effect(() => {
    if (!bankMenuOpen) {
      bankMenuStyle = '';
      return;
    }
    const menu = bankMenuEl;
    const trigger = bankTriggerEl;
    if (menu === null || trigger === null) {
      return;
    }
    const rect = trigger.getBoundingClientRect();
    const margin = 8;
    const gap = 4;
    const menuHeight = menu.scrollHeight;
    const below = window.innerHeight - margin - (rect.bottom + gap);
    const above = rect.top - gap - margin;
    const openAbove = menuHeight > below && above > below;
    const maxHeight = Math.max(openAbove ? above : below, 0);
    const right = Math.max(window.innerWidth - rect.right, margin);
    const vertical = openAbove
      ? `bottom: ${window.innerHeight - rect.top + gap}px;`
      : `top: ${rect.bottom + gap}px;`;
    // Fixed positioning keeps the menu above the following cards and out of
    // the scroll container's overflow, while the measured coordinates keep it
    // directly below or above its trigger.
    bankMenuStyle = `position: fixed; right: ${right}px; ${vertical} max-height: ${maxHeight}px;`;
  });

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
    onMenuChange(bankMenuOpen ? null : bankMenuKey);
  }

  function accountMenuOpen(accountID: number): boolean {
    return menuKey === `account:${accountID}`;
  }

  function toggleAccountMenu(accountID: number): void {
    const key = `account:${accountID}`;
    onMenuChange(menuKey === key ? null : key);
  }

  function chooseBankAction(action: string): void {
    onMenuChange(null);
    onBankMenu(card.bankId, action);
  }

  function chooseAccount(accountID: number, action: () => void): void {
    onMenuChange(null);
    action();
  }

  function runStatusAction(): void {
    if (card.status.action === 'sync') {
      onSync(card.bankId);
    } else if (card.status.action === 'test-connection') {
      onBankMenu(card.bankId, 'test-connection');
    } else if (card.status.action === 'connection') {
      onBankMenu(card.bankId, 'connection');
    }
  }
</script>

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
          <button
            type="button"
            class="bank-menu-trigger"
            aria-label={t('More')}
            title={t('More')}
            aria-haspopup="menu"
            aria-expanded={bankMenuOpen}
            data-menu-key={bankMenuKey}
            bind:this={bankTriggerEl}
            onclick={toggleBankMenu}
          >
            <Icon name="more_vert" size={20} />
          </button>
          {#if bankMenuOpen}
            <div
              class="bank-menu"
              role="menu"
              aria-label={t('More')}
              data-menu-key={bankMenuKey}
              bind:this={bankMenuEl}
              style={bankMenuStyle}
            >
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

  {#if card.status.text !== ''}
    <div
      class="bank-status"
      class:ready={card.status.tone === 'ready'}
      class:attention={card.status.tone === 'attention'}
      class:off={card.status.tone === 'off'}
      data-testid={`bank-status-${card.bankId}`}
    >
      <span class="bank-status-icon" aria-hidden="true">
        <Icon name={card.status.icon as IconName} size={20} />
      </span>
      <span class="bank-status-text">{card.status.text}</span>
      {#if card.status.action !== ''}
        <span class="bank-status-action">
          <TextButton
            text={card.status.actionText}
            enabled={card.status.actionEnabled}
            onclick={runStatusAction}
          />
        </span>
      {/if}
    </div>
  {/if}

  {#if connectionTest !== null && connectionTest.bankId === card.bankId}
    <div class="connection-test" data-testid={`connection-test-${card.bankId}`} aria-live="polite">
      {#if connectionTest.running}
        <div class="test-line">
          <span class="spinner" aria-hidden="true"></span>
          <span class="test-reason">{t('TestConnectionRunning')}</span>
        </div>
      {:else}
        <div class="test-line" class:warning={connectionTest.verdict === 'nogo'}>
          <span
            class="verdict-chip"
            class:go={connectionTest.verdict === 'go'}
            class:nogo={connectionTest.verdict === 'nogo'}
            class:none={connectionTest.verdict === 'none'}
          >
            {connectionTest.verdictText}
          </span>
          <span class="test-reason">{connectionTest.reason}</span>
        </div>

        {#if connectionTest.nextStep !== ''}
          <p class="test-next-step">{connectionTest.nextStep}</p>
        {/if}

        {#if connectionTest.messages.length > 0}
          <div class="test-bank-messages">
            <p class="test-bank-messages-title">{t('TestConnectionBankMessages')}</p>
            <ul class="test-bank-message-list">
              {#each connectionTest.messages as message (message)}
                <li class="test-bank-message">
                  <span class="code-chip">{message.code}</span>
                  {#if message.segment !== ''}
                    <span class="test-bank-message-segment">{message.segment}</span>
                  {/if}
                  <span class="test-bank-message-text">{message.text}</span>
                </li>
              {/each}
            </ul>
            <p class="test-bank-messages-note">{t('TestConnectionBankMessagesNote')}</p>
          </div>
        {/if}

        <div class="test-details">
          {#if connectionTest.strongSync}
            <p class="test-path" data-testid={`connection-test-path-${card.bankId}`}>
              {t('TestConnectionPathStrongSync')}
              {#if connectionTest.strongSyncTAN === 'typed'}
                {t('TestConnectionStrongSyncTyped')}
              {:else if connectionTest.strongSyncTAN === 'decoupled'}
                {t('TestConnectionStrongSyncDecoupled')}
              {/if}
            </p>
          {/if}

          {#if connectionTest.usedSavedPin}
            <p class="test-path" data-testid={`connection-test-saved-pin-${card.bankId}`}>
              {t('TestConnectionUsedSavedPin')}
            </p>
            {#if connectionTest.savedPinRejectedOnce}
              <p class="test-path warning">
                {t('TestConnectionSavedPinRejectedOnce')}
              </p>
            {:else if connectionTest.savedPinForgotten}
              <p class="test-path warning">
                {t('TestConnectionSavedPinForgotten')}
              </p>
            {/if}
          {/if}

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

          {#if lastTrace !== null && lastTrace.kind === 'test' && lastTrace.hasTrace}
            <div class="test-trace">
              <SecondaryButton
                text={t('SaveConnectionTrace')}
                icon="file_download"
                onclick={() => onSaveTrace(card.bankId)}
              />
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if lastTrace !== null && lastTrace.kind === 'sync' && lastTrace.hasTrace && syncControlsVisible}
    <div class="sync-trace" data-testid={`sync-trace-${card.bankId}`}>
      <TextButton
        text={t('SaveConnectionTrace')}
        icon="file_download"
        onclick={() => onSaveTrace(card.bankId)}
      />
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
          <div class="account-row" data-testid={`account-row-${account.accountId}`}>
            <button
              type="button"
              class="account-main"
              onclick={() => {
                onMenuChange(null);
                onOpenDetail(account.accountId);
              }}
            >
              <span class="account-text">
                <span class="account-name">{account.name}</span>
                <span class="account-subtitle">{account.subtitle}</span>
              </span>
              <span class="badge-column">
                <CountBadge
                  count={account.newCount}
                  label={t('NewBookingsCount', account.newCount)}
                />
              </span>
              <span
                class="account-amount"
                class:muted={accountTone(account) === 'muted'}
                class:negative={accountTone(account) === 'negative'}>{account.balanceText}</span
              >
            </button>
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
              aria-haspopup="menu"
              aria-expanded={accountMenuOpen(account.accountId)}
              data-menu-key={`account:${account.accountId}`}
              onclick={(event) => {
                event.stopPropagation();
                toggleAccountMenu(account.accountId);
              }}
            >
              <Icon name="more_vert" size={18} />
            </button>
          </div>

          {#if accountMenuOpen(account.accountId)}
            <div
              class="account-actions"
              data-testid={`account-actions-${account.accountId}`}
              data-menu-key={`account:${account.accountId}`}
            >
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

  .bank-status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 0 16px 12px;
    padding: 8px 12px;
    border-radius: var(--radius-panel);
  }

  .bank-status.ready {
    background: var(--color-ready-wash);
    color: var(--color-success);
  }

  .bank-status.attention {
    background: var(--color-attention-wash);
    color: var(--color-caution);
  }

  .bank-status.off {
    background: var(--color-off-wash);
    color: var(--color-muted-text);
  }

  .bank-status-icon {
    display: inline-flex;
    flex: none;
  }

  .bank-status-text {
    flex: 1 1 200px;
    min-width: 0;
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .bank-status-action {
    flex: none;
    margin-left: auto;
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

  .bank-menu-trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--size-icon-button);
    height: var(--size-icon-button);
    padding: 0;
    border: 0;
    border-radius: var(--radius-control);
    background: var(--color-control-fill);
    color: var(--color-text);
    cursor: pointer;
  }

  .bank-menu-trigger:hover {
    background: var(--color-control-fill-hover);
  }

  .bank-menu-trigger:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .bank-menu {
    position: fixed;
    z-index: 20;
    right: 0;
    width: 300px;
    padding: 8px 0;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
    overflow-y: auto;
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

  .verdict-chip {
    display: inline-flex;
    align-items: center;
    flex: none;
    height: 22px;
    padding: 0 10px;
    border-radius: var(--radius-tag);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .verdict-chip.go {
    background: var(--color-ready-wash);
    color: var(--color-success);
  }

  .verdict-chip.nogo {
    background: var(--color-attention-wash);
    color: var(--color-caution);
  }

  .verdict-chip.none {
    background: var(--color-off-wash);
    color: var(--color-muted-text);
  }

  .test-reason {
    min-width: 0;
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .test-next-step {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .test-details {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--color-divider);
  }

  .test-trace {
    display: flex;
  }

  .test-path {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .test-path.warning {
    color: var(--color-warning-strong);
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

  .test-bank-messages {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .test-bank-messages-title {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-label-small-weight);
    line-height: 1.35;
  }

  .test-bank-message-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .test-bank-message {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 6px;
  }

  .test-bank-message-segment {
    color: var(--color-muted-text);
    font-family: ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace;
    font-size: var(--type-label-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .test-bank-message-text {
    flex: 1 1 100%;
    min-width: 0;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .test-bank-messages-note {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .sync-trace {
    display: flex;
    justify-content: flex-end;
    margin: -4px 16px 8px;
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

  .account-main {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 100%;
    align-items: center;
    gap: 8px;
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .row-icon.transactions {
    opacity: 0;
  }

  .account-row:hover .row-icon.transactions,
  .account-row:focus-within .row-icon.transactions {
    opacity: 1;
  }

  .account-main:focus-visible {
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
