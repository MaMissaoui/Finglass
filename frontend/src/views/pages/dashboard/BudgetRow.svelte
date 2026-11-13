<script lang="ts">
  import type { DashboardBudgetRowDTO } from '../../../lib/api';
  import { clampFraction } from '../../../lib/charts/geometry';

  let {
    budget,
    monthProgress,
  }: {
    budget: DashboardBudgetRowDTO;
    monthProgress: number;
  } = $props();

  const ratio = $derived(clampFraction(budget.ratio));
  const progress = $derived(clampFraction(monthProgress));
  const caution = $derived(budget.over || budget.warning);
</script>

<div class="budget">
  <div class="line">
    <span class="name">{budget.name}</span>
    <span class="amount">{budget.amountText}</span>
  </div>

  <div class="track">
    <div class="fill" class:caution style:width={`${ratio * 100}%`} data-testid="budget-fill"></div>
    <div class="marker" style:left={`${progress * 100}%`} data-testid="budget-marker"></div>
  </div>

  <p class="rest" class:caution>{budget.restText}</p>
</div>

<style>
  .budget {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 100%;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .name {
    flex: 1;
    min-width: 0;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .amount {
    flex: none;
    color: var(--color-money-out);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .track {
    position: relative;
    height: 6px;
    border-radius: var(--radius-dot);
    background: var(--color-divider);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    border-radius: var(--radius-dot);
    background: var(--color-primary);
  }

  .fill.caution {
    background: var(--color-caution);
  }

  .marker {
    position: absolute;
    top: 0;
    width: 2px;
    height: 100%;
    margin-left: -1px;
    background: var(--color-text);
  }

  .rest {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .rest.caution {
    color: var(--color-caution);
  }
</style>
