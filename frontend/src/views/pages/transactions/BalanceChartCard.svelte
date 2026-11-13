<script lang="ts">
  import { Icon } from '../../../lib/components';

  let {
    dto,
    t,
    onToggle,
  }: {
    dto: {
      chartCollapsed: boolean;
      chartLine: string;
      chartArea: string;
      chartAxisTop: string;
      chartAxisBottom: string;
      chartAxisStart: string;
      chartAxisMid: string;
      chartAxisEnd: string;
      chartNotFromZero: boolean;
      chartEndX: number;
      chartEndY: number;
      chartEndLabel: string;
    };
    t: (key: string) => string;
    onToggle: () => void;
  } = $props();

  const dotLeft = $derived(`${Math.min(99, Math.max(1, (dto.chartEndX / 700) * 100))}%`);
  const dotTop = $derived(`${Math.min(97, Math.max(3, (dto.chartEndY / 200) * 100))}%`);
</script>

<div class="chart-card" class:collapsed={dto.chartCollapsed} data-testid="balance-chart">
  <div class="chart-head">
    <button
      type="button"
      class="toggle"
      aria-label={t('BalanceHistory')}
      title={t('BalanceHistory')}
      onclick={onToggle}
    >
      <Icon name={dto.chartCollapsed ? 'keyboard_arrow_right' : 'keyboard_arrow_down'} size={20} />
    </button>
    <span class="chart-title">{t('BalanceHistory')}</span>
    {#if !dto.chartCollapsed && dto.chartNotFromZero}
      <span class="not-zero">{t('AxisNotFromZero')}</span>
    {/if}
  </div>

  {#if !dto.chartCollapsed}
    <div class="chart-body">
      <div class="axis" aria-hidden="true">
        <span>{dto.chartAxisTop}</span>
        <span>{dto.chartAxisBottom}</span>
      </div>
      <div class="plot-area">
        <div class="plot">
          <svg viewBox="0 0 700 200" preserveAspectRatio="none" aria-hidden="true">
            <path class="area" d={dto.chartArea}></path>
            <path class="line" d={dto.chartLine}></path>
          </svg>
          <span class="end-dot" style:left={dotLeft} style:top={dotTop}></span>
          <span class="end-label" style:top={dotTop}>{dto.chartEndLabel}</span>
        </div>
        <div class="dates">
          <span>{dto.chartAxisStart}</span>
          <span class="mid">{dto.chartAxisMid}</span>
          <span class="end">{dto.chartAxisEnd}</span>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .chart-card {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 4px;
    height: 200px;
    padding: 4px 12px 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
    box-sizing: border-box;
  }

  .chart-card.collapsed {
    height: 44px;
  }

  .chart-head {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
    height: 36px;
  }

  .toggle {
    display: inline-flex;
    flex: none;
    width: 28px;
    height: 28px;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 0;
    border-radius: var(--radius-icon-medium);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .toggle:hover {
    background: var(--color-divider);
  }

  .toggle:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  .chart-title {
    flex: 1;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-small-size);
    font-weight: var(--type-title-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .not-zero {
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
  }

  .chart-body {
    display: flex;
    flex: 1;
    min-height: 0;
    gap: 6px;
  }

  .axis {
    display: flex;
    flex: none;
    width: 66px;
    flex-direction: column;
    justify-content: space-between;
    color: var(--color-muted-text);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
    text-align: right;
  }

  .axis span {
    overflow: hidden;
    line-height: 1;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .plot-area {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .plot {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  .plot svg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    overflow: hidden;
  }

  .area {
    fill: var(--color-primary);
    fill-opacity: 0.1;
    stroke: none;
  }

  .line {
    fill: none;
    stroke: var(--color-primary);
    stroke-width: 2;
    vector-effect: non-scaling-stroke;
  }

  .end-dot {
    position: absolute;
    width: 12px;
    height: 12px;
    border: 2px solid var(--color-surface);
    border-radius: 50%;
    background: var(--color-primary);
    transform: translate(-50%, -50%);
    pointer-events: none;
  }

  .end-label {
    position: absolute;
    right: 0;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    transform: translateY(4px);
    pointer-events: none;
  }

  .dates {
    display: flex;
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .dates span {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .dates .mid {
    text-align: center;
  }

  .dates .end {
    text-align: right;
  }
</style>
