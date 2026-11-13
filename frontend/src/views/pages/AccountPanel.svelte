<script lang="ts">
  import type {
    AccountGroupChoiceDTO,
    AccountPanelRowDTO,
  } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import {
    BankBadge,
    ChoiceChip,
    CountBadge,
    Icon,
    OutlinedField,
    SearchField,
    TextButton,
  } from '../../lib/components';

  let {
    rows,
    groups,
    filterText,
    shownText,
    t,
    pageSearch = true,
    onFilter,
    onSelect,
    onToggle,
    onMarkRead,
    onNewGroup,
    onRenameGroup,
    onDeleteGroup,
    onMoveGroup,
    onMoveAccount,
  }: {
    rows: AccountPanelRowDTO[];
    groups: AccountGroupChoiceDTO[];
    filterText: string;
    shownText: string;
    t: (key: string) => string;
    pageSearch?: boolean;
    onFilter: (text: string) => void;
    onSelect: (kind: string, groupKind: string, id: number, accountType: string) => void;
    onToggle: (kind: string, groupKind: string, id: number, accountType: string) => void;
    onMarkRead: (kind: string, groupKind: string, id: number, accountType: string) => void;
    onNewGroup: (name: string) => void;
    onRenameGroup: (id: number, name: string) => void;
    onDeleteGroup: (id: number) => void;
    onMoveGroup: (id: number, up: boolean) => void;
    onMoveAccount: (accountID: number, groupID: number) => void;
  } = $props();

  let openActions = $state('');
  let creating = $state(false);
  let createName = $state('');
  let renamingId = $state(-1);
  let renameName = $state('');
  let deletingId = $state(-1);
  let deletingText = $state('');
  let movingAccountId = $state(-1);
  let moveGroupId = $state(-1);

  function rowKey(row: AccountPanelRowDTO): string {
    return `${row.kind}:${row.groupKind}:${row.id}:${row.accountType}`;
  }

  function resetForm(): void {
    creating = false;
    createName = '';
    renamingId = -1;
    renameName = '';
    deletingId = -1;
    deletingText = '';
    movingAccountId = -1;
    moveGroupId = -1;
  }

  function startRename(id: number, name: string): void {
    resetForm();
    renamingId = id;
    renameName = name;
  }

  function startDelete(id: number, text: string): void {
    resetForm();
    deletingId = id;
    deletingText = text;
  }

  function startMove(id: number): void {
    resetForm();
    movingAccountId = id;
    moveGroupId = -1;
  }

  function saveCreate(): void {
    if (createName !== '') {
      onNewGroup(createName);
      resetForm();
    }
  }

  function saveRename(): void {
    if (renameName !== '') {
      onRenameGroup(renamingId, renameName);
      resetForm();
    }
  }

  function confirmDelete(): void {
    onDeleteGroup(deletingId);
    resetForm();
  }

  function saveMove(): void {
    onMoveAccount(movingAccountId, moveGroupId);
    resetForm();
  }

  function toggleActions(key: string): void {
    openActions = openActions === key ? '' : key;
  }

  function amountClass(row: AccountPanelRowDTO, tone: string): string {
    if (row.selected) {
      return '';
    }
    if (tone === 'negative') {
      return 'negative';
    }
    if (tone === 'loan' || tone === 'plain' || tone === 'zero') {
      return 'muted';
    }
    return '';
  }
</script>

<aside class="panel" data-testid="account-panel">
  <div data-page-search={pageSearch ? '' : undefined}>
    <SearchField
      value={filterText}
      placeholder={t('FilterAccounts')}
      clearLabel={t('ClearSearch')}
      oninput={onFilter}
    />
  </div>

  {#if shownText !== ''}
    <p class="shown">{shownText}</p>
  {/if}

  <div class="rows">
    {#each rows as row, index (rowKey(row))}
      <div class="row-block">
        {#if index > 1 && row.kind === 'group'}
          <div class="group-gap" aria-hidden="true"></div>
        {/if}

        <div
          class="row"
          class:account={row.kind === 'account'}
          class:selected={row.selected}
          role="button"
          tabindex="0"
          data-testid={`panel-row-${rowKey(row)}`}
          data-kind={row.kind}
          data-group-kind={row.groupKind}
          data-row-id={row.id}
          data-label={row.label}
          onclick={() => onSelect(row.kind, row.groupKind, row.id, row.accountType)}
          onkeydown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault();
              onSelect(row.kind, row.groupKind, row.id, row.accountType);
            }
          }}
        >
          {#if row.selected}
            <span class="accent" aria-hidden="true"></span>
          {/if}

          {#if row.kind === 'group'}
            <button
              type="button"
              class="chevron"
              aria-label={row.collapsed ? t('Expand') : t('Collapse')}
              title={row.collapsed ? t('Expand') : t('Collapse')}
              onclick={(event) => {
                event.stopPropagation();
                openActions = '';
                onToggle(row.kind, row.groupKind, row.id, row.accountType);
              }}
            >
              <Icon
                name={row.collapsed ? 'keyboard_arrow_right' : 'keyboard_arrow_down'}
                size={20}
              />
            </button>
          {/if}

          {#if row.kind === 'account' && row.logo === '' && row.logoLetters === ''}
            <span class="badge-space" aria-hidden="true"></span>
          {/if}

          {#if row.logo !== '' || row.logoLetters !== ''}
            <BankBadge
              logo={row.logo}
              letters={row.logoLetters}
              colourIndex={row.logoColour}
              size={20}
            />
          {/if}

          <div class="row-text">
            <span class="label" class:group-label={row.kind === 'group'}>{row.label}</span>
            {#if row.secondary !== ''}
              <span class="secondary">{row.secondary}</span>
            {/if}
          </div>

          <span class="count-column" class:empty={row.newCount === 0}>
            <CountBadge count={row.newCount} />
          </span>

          <div class="amounts">
            {#each row.totals as amount (amount.text)}
              <span
                class="amount"
                class:muted={amountClass(row, amount.tone) === 'muted'}
                class:negative={amountClass(row, amount.tone) === 'negative'}>{amount.text}</span
              >
            {/each}
          </div>

          {#if row.newCount > 0 || row.kind === 'account' || (row.kind === 'group' && row.groupKind === 'custom')}
            <button
              type="button"
              class="dot"
              aria-label={t('More')}
              title={t('More')}
              aria-expanded={openActions === rowKey(row)}
              onclick={(event) => {
                event.stopPropagation();
                toggleActions(rowKey(row));
              }}
            >
              <Icon name="more_vert" size={18} />
            </button>
          {/if}

          {#if row.detail !== ''}
            <span class="detail" role="tooltip">{row.detail}</span>
          {/if}
        </div>

        {#if openActions === rowKey(row)}
          <div class="actions-strip" data-testid="panel-actions">
            {#if row.newCount > 0}
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  onMarkRead(row.kind, row.groupKind, row.id, row.accountType);
                }}>{t('MarkAsRead')}</button
              >
            {/if}
            {#if row.kind === 'group' && row.groupKind === 'custom'}
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  startRename(row.id, row.name);
                }}>{t('RenameGroup')}</button
              >
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  onMoveGroup(row.id, true);
                }}>{t('MoveUp')}</button
              >
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  onMoveGroup(row.id, false);
                }}>{t('MoveDown')}</button
              >
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  startDelete(row.id, row.deleteText);
                }}>{t('Delete')}</button
              >
            {/if}
            {#if row.kind === 'account'}
              <button
                type="button"
                class="panel-action"
                onclick={() => {
                  openActions = '';
                  startMove(row.id);
                }}>{t('MoveToGroup')}</button
              >
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>

  {#if creating}
    <div class="form">
      <p class="form-title">{t('NewGroup')}</p>
      <OutlinedField
        label={t('GroupName')}
        bind:value={createName}
        onaccepted={() => saveCreate()}
      />
      <div class="form-actions">
        <button type="button" class="panel-action" onclick={saveCreate}>{t('Save')}</button>
        <button type="button" class="panel-action" onclick={resetForm}>{t('Cancel')}</button>
      </div>
    </div>
  {/if}

  {#if renamingId >= 0}
    <div class="form">
      <p class="form-title">{t('RenameGroup')}</p>
      <OutlinedField
        label={t('GroupName')}
        bind:value={renameName}
        onaccepted={() => saveRename()}
      />
      <div class="form-actions">
        <button type="button" class="panel-action" onclick={saveRename}>{t('Save')}</button>
        <button type="button" class="panel-action" onclick={resetForm}>{t('Cancel')}</button>
      </div>
    </div>
  {/if}

  {#if deletingId >= 0}
    <div class="form">
      <p class="form-text">{deletingText}</p>
      <div class="form-actions">
        <button type="button" class="panel-action" onclick={confirmDelete}>{t('Delete')}</button>
        <button type="button" class="panel-action" onclick={resetForm}>{t('Cancel')}</button>
      </div>
    </div>
  {/if}

  {#if movingAccountId >= 0}
    <div class="form">
      <p class="form-title">{t('MoveToGroup')}</p>
      <ChoiceChip
        label={t('DefaultGroup')}
        selected={moveGroupId < 0}
        onclick={() => (moveGroupId = -1)}
      />
      {#each groups as group (group.id)}
        <ChoiceChip
          label={group.name}
          selected={moveGroupId === group.id}
          onclick={() => (moveGroupId = group.id)}
        />
      {/each}
      <div class="form-actions">
        <button type="button" class="panel-action" onclick={saveMove}>{t('Save')}</button>
        <button type="button" class="panel-action" onclick={resetForm}>{t('Cancel')}</button>
      </div>
    </div>
  {/if}

  <div class="rule" role="none"></div>

  <TextButton
    text={t('NewGroup')}
    icon="add"
    onclick={() => {
      resetForm();
      creating = true;
    }}
  />
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
    padding: 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .shown {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .rows {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    overflow-y: auto;
  }

  .row-block {
    display: flex;
    flex-direction: column;
  }

  .group-gap {
    height: 12px;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 36px;
    padding: 8px 12px;
    border-radius: var(--radius-small);
    cursor: pointer;
  }

  .row.account {
    min-height: 52px;
  }

  .row:hover {
    background: var(--color-background);
  }

  .row.selected {
    background: var(--color-selected);
  }

  .row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .accent {
    position: absolute;
    top: 0;
    left: 0;
    width: 3px;
    height: 100%;
    background: var(--color-primary);
  }

  .chevron,
  .dot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-icon-small);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .dot {
    width: 24px;
    height: 24px;
    border-radius: var(--radius-icon);
  }

  .chevron:hover,
  .dot:hover {
    background: var(--color-divider);
  }

  .chevron:focus-visible,
  .dot:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  .badge-space {
    flex: none;
    width: 20px;
  }

  .row-text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 0;
  }

  .label {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .label.group-label {
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .row.selected .label,
  .row.selected .secondary {
    color: var(--color-on-selected);
  }

  .secondary {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .count-column {
    display: flex;
    flex: none;
    width: 20px;
    align-items: center;
    justify-content: center;
  }

  .count-column.empty {
    width: 0;
  }

  .amounts {
    display: flex;
    flex: none;
    flex-direction: column;
    align-items: flex-end;
    gap: 0;
  }

  .amount {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    white-space: nowrap;
  }

  .amount.muted {
    color: var(--color-muted-text);
  }

  .amount.negative {
    color: var(--color-error);
  }

  .row.selected .amount {
    color: var(--color-on-selected);
  }

  .detail {
    position: absolute;
    right: 8px;
    bottom: 100%;
    z-index: 5;
    display: none;
    max-width: 260px;
    padding: 6px 10px;
    border-radius: var(--radius-panel);
    background: var(--color-ink);
    color: var(--color-paper);
    font-size: var(--type-body-small-size);
    white-space: normal;
  }

  .row:hover .detail {
    display: block;
  }

  .actions-strip {
    display: flex;
    gap: 10px;
    overflow-x: auto;
    padding: 3px 6px 3px 24px;
    background: var(--color-background);
  }

  .panel-action {
    flex: none;
    height: 24px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
    cursor: pointer;
  }

  .panel-action:hover {
    color: var(--color-primary-hover);
  }

  .panel-action:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    border-radius: var(--radius-panel);
    background: var(--color-background);
  }

  .form-title {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .form-text {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .form-actions {
    display: flex;
    gap: 8px;
    align-items: flex-start;
  }

  .rule {
    height: 1px;
    background: var(--color-divider);
  }
</style>
