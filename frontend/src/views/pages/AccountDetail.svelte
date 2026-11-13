<script lang="ts">
  import type {
    AccountDaysChoiceDTO,
    AccountDetailRowDTO,
  } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { BankBadge, SegmentedButton, TextButton } from '../../lib/components';

  let {
    accounts,
    daysChoices,
    dayLabels,
    t,
    onSelectDays,
    onClose,
    onRename,
  }: {
    accounts: {
      detailTitle: string;
      detailLogo: string;
      detailLetters: string;
      detailColour: number;
      detailSubtitle: string;
      detailBalance: string;
      detailTone: string;
      detailAccountId: number;
      detailDays: number;
      detailSummary: string;
      detailPath: string;
      detailAxisTop: string;
      detailAxisBottom: string;
      detailEndX: number;
      detailEndY: number;
      detailEndLabel: string;
      detailRows: AccountDetailRowDTO[];
    };
    daysChoices: AccountDaysChoiceDTO[];
    dayLabels: string[];
    t: (key: string) => string;
    onSelectDays: (accountID: number, days: number) => void;
    onClose: () => void;
    onRename: (accountID: number) => void;
  } = $props();

  const selected = $derived(accounts.detailDays === 365 ? 2 : accounts.detailDays === 90 ? 1 : 0);
  const dotX = $derived(Math.min(Math.max(accounts.detailEndX, 6), 694));
  const dotY = $derived(Math.min(Math.max(accounts.detailEndY, 6), 194));
  const labelX = $derived(Math.min(694, dotX + 6));
  const labelY = $derived(Math.min(194, dotY + 20));

  function toneClass(tone: string): string {
    if (tone === 'negative') {
      return 'negative';
    }
    if (tone === 'loan' || tone === 'plain' || tone === 'zero') {
      return 'muted';
    }
    return '';
  }

  function selectDays(index: number): void {
    const choice = daysChoices[index];
    if (choice !== undefined) {
      onSelectDays(accounts.detailAccountId, choice.days);
    }
  }
</script>

<div class="scrim" data-testid="account-detail">
  <dialog open class="detail" aria-modal="true" aria-label={accounts.detailTitle}>
    <header class="title-row">
      <BankBadge
        logo={accounts.detailLogo}
        letters={accounts.detailLetters}
        colourIndex={accounts.detailColour}
        size={40}
      />
      <h2>{accounts.detailTitle}</h2>
    </header>

    <div class="balance-row">
      <div class="balance-text">
        <p class="subtitle">{accounts.detailSubtitle}</p>
        <p
          class="balance"
          class:muted={toneClass(accounts.detailTone) === 'muted'}
          class:negative={toneClass(accounts.detailTone) === 'negative'}
        >
          {accounts.detailBalance}
        </p>
      </div>
      <SegmentedButton labels={dayLabels} {selected} onselect={selectDays} />
    </div>

    <p class="summary">{accounts.detailSummary}</p>

    <div class="chart">
      <div class="axis">
        <span>{accounts.detailAxisTop}</span>
        <span>{accounts.detailAxisBottom}</span>
      </div>
      <svg
        class="plot"
        viewBox="0 0 700 200"
        preserveAspectRatio="none"
        role="presentation"
        aria-hidden="true"
      >
        <path class="line" d={accounts.detailPath} vector-effect="non-scaling-stroke"></path>
        <circle class="dot" cx={dotX} cy={dotY} r="4"></circle>
        <text class="end-label" x={labelX} y={labelY} text-anchor="end">
          {accounts.detailEndLabel}
        </text>
      </svg>
    </div>

    <h3>{t('RecentBookings')}</h3>

    {#if accounts.detailRows.length === 0}
      <p class="none">{t('NoneInPeriod')}</p>
    {/if}

    {#each accounts.detailRows as booking (booking.dateText + booking.title + booking.amountText)}
      <div class="booking-row">
        <span class="date">{booking.dateText}</span>
        <span class="booking-title">{booking.title}</span>
        <span class="amount" class:expense={booking.expense}>{booking.amountText}</span>
      </div>
    {/each}

    <p class="history">{t('HistoryReconstructed')}</p>

    <div class="actions">
      <TextButton text={t('RenameAccount')} onclick={() => onRename(accounts.detailAccountId)} />
      <TextButton text={t('Close')} onclick={onClose} />
    </div>
  </dialog>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 30;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    background: var(--color-scrim);
  }

  .detail {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: min(780px, 100%);
    max-width: calc(100% - 32px);
    max-height: calc(100% - 32px);
    margin: 0;
    padding: 24px;
    overflow-y: auto;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
    color: var(--color-text);
  }

  .title-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  h2 {
    flex: 1;
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .balance-row {
    display: flex;
    align-items: flex-start;
    gap: 16px;
  }

  .balance-text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .subtitle {
    margin: 0;
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .balance {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-headline-size);
    font-weight: var(--type-headline-weight);
  }

  .balance.muted {
    color: var(--color-muted-text);
  }

  .balance.negative {
    color: var(--color-error);
  }

  .summary {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .chart {
    display: flex;
    gap: 8px;
    width: 100%;
    height: 200px;
  }

  .axis {
    display: flex;
    flex: none;
    width: 64px;
    flex-direction: column;
    justify-content: space-between;
    align-items: flex-end;
    color: var(--color-muted-text);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
  }

  .plot {
    flex: 1;
    min-width: 0;
    height: 200px;
    overflow: visible;
  }

  .line {
    fill: none;
    stroke: var(--color-primary);
    stroke-width: 2px;
  }

  .dot {
    fill: var(--color-primary);
    stroke: var(--color-surface);
    stroke-width: 2px;
  }

  .end-label {
    fill: var(--color-text);
    font-family: var(--font-sans);
    font-size: 14px;
    font-weight: 500;
  }

  h3 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .none {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
  }

  .booking-row {
    display: flex;
    align-items: center;
    gap: 12px;
    line-height: 1;
  }

  .date {
    flex: none;
    width: 90px;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .booking-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .amount {
    flex: none;
    color: var(--color-money-in);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
  }

  .amount.expense {
    color: var(--color-money-out);
  }

  .history {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .actions {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    margin-top: 4px;
  }
</style>
