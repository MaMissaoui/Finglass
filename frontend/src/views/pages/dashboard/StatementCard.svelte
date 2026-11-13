<script lang="ts">
  import Icon from '../../../lib/components/Icon.svelte';
  import type { DashboardStatementLineDTO } from '../../../lib/api';

  let {
    monthLabel,
    lines,
    notes,
    tone,
    pendingTitle,
    pendingValue,
    pendingLines,
    pendingEnabled,
    onopenpending,
  }: {
    monthLabel: string;
    lines: DashboardStatementLineDTO[];
    notes: string;
    tone: string;
    pendingTitle: string;
    pendingValue: string;
    pendingLines: string[];
    pendingEnabled: boolean;
    onopenpending: () => void;
  } = $props();
</script>

<section class="statement-card">
  <div class="statement">
    <h2>{monthLabel}</h2>
    <div class="lines">
      {#each lines as line, index (index)}
        <div class="line" class:total={line.kind === 'result'}>
          <span class="label" class:carry={line.kind === 'carry' || line.kind === 'movement'}>
            {line.label}
          </span>
          <span
            class="amount"
            class:in={line.kind === 'in'}
            class:out={line.kind === 'out'}
            class:negative={line.kind === 'result' && tone === 'negative'}
          >
            {line.amountText}
          </span>
        </div>
      {/each}
    </div>
    {#if notes !== ''}
      <p class="notes">{notes}</p>
    {/if}
  </div>

  <div class="divider" role="none"></div>

  <div class="pending" data-testid="pending-panel">
    <h2>{pendingTitle}</h2>
    <p class="pending-value" class:enabled={pendingEnabled}>{pendingValue}</p>

    {#if pendingEnabled && pendingLines.length > 0}
      <div class="pending-lines">
        {#each pendingLines as detail, index (index)}
          <button type="button" class="pending-line" onclick={onopenpending}>
            <span>{detail}</span>
            <Icon name="keyboard_arrow_right" size={16} />
          </button>
        {/each}
      </div>
    {:else if pendingLines.length > 0}
      <div class="done-line">
        <Icon name="check_circle" size={16} />
        <span>{pendingLines[0]}</span>
      </div>
    {/if}
  </div>
</section>

<style>
  .statement-card {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
    overflow: hidden;
  }

  .statement {
    display: flex;
    flex: 3 1 0;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
    max-width: 560px;
    padding: 16px;
  }

  .divider {
    flex: none;
    width: 1px;
    background: var(--color-divider);
  }

  .pending {
    display: flex;
    flex: 2 1 0;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
    padding: 16px;
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 26px;
  }

  .line.total {
    position: relative;
    height: 48px;
    padding-top: 10px;
  }

  .line.total::before {
    content: '';
    position: absolute;
    top: 0;
    right: 0;
    width: 200px;
    height: 1px;
    background: var(--color-text);
  }

  .label {
    flex: 1;
    min-width: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .label.carry {
    color: var(--color-muted-text);
  }

  .amount {
    flex: none;
    width: 200px;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: right;
    white-space: nowrap;
  }

  .amount.in {
    color: var(--color-money-in);
  }

  .amount.out {
    color: var(--color-money-out);
  }

  .line.total .label {
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
  }

  .line.total .amount {
    color: var(--color-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
  }

  .line.total .amount.negative {
    color: var(--color-error);
  }

  .notes {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: pre-line;
  }

  .pending-value {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
  }

  .pending-value.enabled {
    color: var(--color-caution);
  }

  .pending-lines {
    display: flex;
    flex-direction: column;
  }

  .pending-line {
    display: flex;
    align-items: center;
    gap: 4px;
    width: 100%;
    height: 28px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    cursor: pointer;
  }

  .pending-line span {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .pending-line :global(.icon) {
    color: var(--color-muted-text);
  }

  .pending-line:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .done-line {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .done-line :global(.icon) {
    color: var(--color-success);
  }

  .done-line span {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  @media (max-width: 900px) {
    .statement-card {
      flex-direction: column;
    }

    .statement {
      flex: none;
      max-width: none;
    }

    .divider {
      width: auto;
      height: 1px;
    }

    .pending {
      flex: none;
    }
  }
</style>
