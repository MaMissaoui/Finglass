<script lang="ts">
  import type { ReportAmountRowDTO } from '../../../lib/api';
  import {
    meterWidth,
    REPORT_METER_HEIGHT,
    REPORT_METER_WIDTH,
  } from '../../../lib/charts/reportGeometry';

  let {
    rows,
    emptyText,
  }: {
    rows: ReportAmountRowDTO[];
    emptyText: string;
  } = $props();
</script>

{#if emptyText !== ''}
  <p class="empty">{emptyText}</p>
{/if}

{#if rows.length > 0}
  <div class="rows">
    {#each rows as row (row.label)}
      <div class="row">
        <span class="label">{row.label}</span>
        <svg
          class="meter"
          width={REPORT_METER_WIDTH}
          height={REPORT_METER_HEIGHT}
          aria-hidden="true"
        >
          <rect
            x="0"
            y="0"
            width={REPORT_METER_WIDTH}
            height={REPORT_METER_HEIGHT}
            rx={REPORT_METER_HEIGHT / 2}
            fill="var(--color-divider)"
          />
          <rect
            x="0"
            y="0"
            width={meterWidth(row.fraction)}
            height={REPORT_METER_HEIGHT}
            rx={REPORT_METER_HEIGHT / 2}
            fill="var(--color-chart-out)"
          />
        </svg>
        <span class="amount">{row.amountText}</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .rows {
    display: flex;
    max-width: 720px;
    flex-direction: column;
    gap: 4px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 28px;
  }

  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .meter {
    display: block;
    flex: none;
  }

  .amount {
    flex: none;
    width: 120px;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .empty {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }
</style>
