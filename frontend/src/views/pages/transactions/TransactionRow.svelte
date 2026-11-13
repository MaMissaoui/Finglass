<script lang="ts">
  import type { TransactionRowDTO } from '../../../lib/api';
  import { Icon, TextButton } from '../../../lib/components';

  let {
    row,
    hasBalanceColumn,
    t,
    onOpen,
    onToggleSelect,
    onSelectRange,
    onShowPartner,
    onDelete,
  }: {
    row: TransactionRowDTO;
    hasBalanceColumn: boolean;
    t: (key: string) => string;
    onOpen: (id: number) => void;
    onToggleSelect: (id: number) => void;
    onSelectRange: (id: number) => void;
    onShowPartner: (partner: string) => void;
    onDelete: (id: number) => void;
  } = $props();

  let menuOpen = $state(false);

  function balanceClass(tone: string): string {
    if (tone === 'negative') {
      return 'negative';
    }
    if (tone === 'loan' || tone === 'plain' || tone === 'zero') {
      return 'muted';
    }
    return '';
  }

  function activate(event: MouseEvent): void {
    if (row.pending) {
      return;
    }
    if (event.shiftKey) {
      onSelectRange(row.id);
    } else if (event.ctrlKey || event.metaKey) {
      onToggleSelect(row.id);
    } else {
      onOpen(row.id);
    }
  }
</script>

<div class="row-block" data-testid={`transaction-row-${row.pending ? 'pending' : row.id}`}>
  <div
    class="row"
    class:selected={row.selected}
    class:pending={row.pending}
    class:new={row.isNew}
    class:interactive={!row.pending}
    role="button"
    tabindex={row.pending ? -1 : 0}
    aria-disabled={row.pending || undefined}
    data-id={row.id}
    onclick={activate}
    onkeydown={(event) => {
      if (row.pending) {
        return;
      }
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        onOpen(row.id);
      }
    }}
  >
    {#if row.selected}
      <span class="accent" aria-hidden="true"></span>
    {/if}

    <div class="select-column">
      {#if row.isNew}
        <span class="new-dot" aria-hidden="true"></span>
      {/if}
    </div>

    <span class="date">{row.dateText}</span>

    <div class="description">
      <div class="title-line">
        <span class="title">{row.title}</span>
        {#if row.hasDocuments}
          <span class="paperclip" title={row.documentsText}>
            <Icon name="attach_file" size={16} />
          </span>
        {/if}
      </div>
      {#if row.subtitle !== ''}
        <span class="subtitle">{row.subtitle}</span>
      {/if}
    </div>

    <div class="category">
      <span class="marker" style:background={row.categoryColour} aria-hidden="true"></span>
      {#if !row.pending}
        <span class="category-text">{row.categoryText}</span>
      {/if}
    </div>

    <span class="amount" class:negative={!row.pending && row.expense}>{row.amountText}</span>

    {#if hasBalanceColumn}
      <span
        class="balance"
        class:muted={balanceClass(row.balanceTone) === 'muted'}
        class:negative={balanceClass(row.balanceTone) === 'negative'}
      >
        {#if row.balanceWarning}
          <span class="warning" title={row.balanceMismatch}>
            <Icon name="warning" size={16} />
          </span>
        {/if}
        {row.balanceText}
      </span>
    {/if}

    <button
      type="button"
      tabindex={row.pending ? -1 : 0}
      class="more"
      aria-label={t('More')}
      title={t('More')}
      aria-expanded={row.pending ? undefined : menuOpen}
      disabled={row.pending}
      onclick={(event) => {
        event.stopPropagation();
        menuOpen = !menuOpen;
      }}
    >
      <Icon name="more_vert" size={18} />
    </button>
  </div>

  {#if menuOpen && !row.pending}
    <div class="actions-strip">
      <TextButton
        text={t('OpenDetails')}
        onclick={() => {
          menuOpen = false;
          onOpen(row.id);
        }}
      />
      {#if row.partnerLinkText !== ''}
        <TextButton
          text={row.partnerLinkText}
          onclick={() => {
            menuOpen = false;
            onShowPartner(row.partner);
          }}
        />
      {/if}
      <TextButton
        text={t('Delete')}
        onclick={() => {
          menuOpen = false;
          onDelete(row.id);
        }}
      />
    </div>
  {/if}
</div>

<style>
  .row-block {
    display: flex;
    flex-direction: column;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 46px;
    padding: 0 4px 0 10px;
  }

  .row.interactive {
    cursor: pointer;
  }

  .row.interactive:hover {
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

  .select-column {
    display: flex;
    flex: none;
    width: 24px;
    align-items: center;
  }

  .new-dot {
    display: block;
    width: 6px;
    height: 6px;
    margin-left: 1px;
    border-radius: var(--radius-dot);
    background: var(--color-primary);
  }

  .date {
    flex: none;
    width: 82px;
    color: var(--color-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-body-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .row.pending .date,
  .row.pending .title,
  .row.pending .subtitle,
  .row.pending .amount {
    color: var(--color-muted-text);
  }

  .description {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    justify-content: center;
  }

  .title-line {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }

  .title {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .row:not(.pending) .title {
    font-weight: var(--type-label-weight);
  }

  .row.new .title {
    font-weight: 600;
  }

  .paperclip {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  .subtitle {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .category {
    display: flex;
    flex: none;
    width: 112px;
    align-items: center;
    gap: 5px;
  }

  .marker {
    display: block;
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: var(--radius-marker);
  }

  .category-text {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .amount {
    flex: none;
    width: 106px;
    color: var(--color-money-in);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
  }

  .amount.negative {
    color: var(--color-money-out);
  }

  .row.pending .amount {
    font-style: italic;
  }

  .balance {
    display: inline-flex;
    flex: none;
    width: 106px;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
  }

  .balance.muted {
    color: var(--color-muted-text);
  }

  .balance.negative {
    color: var(--color-error);
  }

  .warning {
    display: inline-flex;
    flex: none;
    color: var(--color-caution);
  }

  .more {
    display: inline-flex;
    flex: none;
    width: 30px;
    height: 30px;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 0;
    border-radius: var(--radius-icon);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .more:hover {
    background: var(--color-divider);
  }

  .more:disabled {
    background: transparent;
    cursor: default;
  }

  .more:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  .actions-strip {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 48px;
    padding: 0 6px 0 100px;
    background: var(--color-background);
  }

  .actions-strip :global(.text) {
    height: 32px;
  }
</style>
