<script lang="ts">
  import type { DashboardMonthBarDTO } from '../api';
  import {
    PLOT_HEIGHT,
    barPath,
    barPositions,
    barX,
    clampFraction,
    slotWidth,
    tooltipBox,
  } from './geometry';

  let {
    bars,
    axisTop,
    axisMid,
    axisZero,
    emptyText,
    incomeLabel,
    expensesLabel,
    partialNote,
    soFarText,
  }: {
    bars: DashboardMonthBarDTO[];
    axisTop: string;
    axisMid: string;
    axisZero: string;
    emptyText: string;
    incomeLabel: string;
    expensesLabel: string;
    partialNote: string;
    soFarText: string;
  } = $props();

  let plotWidth = $state(0);
  let hover = $state(-1);

  const slot = $derived(slotWidth(plotWidth, bars.length));
  const positions = $derived(barPositions(slot));

  const layout = $derived.by(() =>
    bars.map((bar, index) => {
      const x = barX(plotWidth, bars.length, index);
      const incomeHeight = clampFraction(bar.incomeFraction) * PLOT_HEIGHT;
      const expensesHeight = clampFraction(bar.expensesFraction) * PLOT_HEIGHT;
      return {
        index,
        bar,
        incomeX: x.income,
        expensesX: x.expenses,
        incomePath: barPath(x.income, PLOT_HEIGHT - incomeHeight, positions.bar, incomeHeight),
        expensesPath: barPath(
          x.expenses,
          PLOT_HEIGHT - expensesHeight,
          positions.bar,
          expensesHeight,
        ),
      };
    }),
  );

  const tooltip = $derived.by(() => {
    const bar = bars[hover];
    if (bar === undefined || plotWidth === 0 || slot === 0) {
      return undefined;
    }
    const box = tooltipBox(plotWidth, slot, bars.length, hover, bar.partial);
    const title = box.y + 18;
    const soFar = title + 16;
    const firstRow = title + (bar.partial ? 16 : 0) + 18;
    const secondRow = firstRow + 18;
    return { bar, box, title, soFar, firstRow, secondRow };
  });

  function enter(index: number): void {
    hover = index;
  }

  function leave(): void {
    hover = -1;
  }
</script>

<div class="chart">
  {#if emptyText !== ''}
    <p class="empty">{emptyText}</p>
  {:else}
    <div class="plot-row">
      <div class="axis" aria-hidden="true">
        <span>{axisTop}</span>
        <span>{axisMid}</span>
        <span>{axisZero}</span>
      </div>

      <div class="plot" bind:clientWidth={plotWidth}>
        <svg width={plotWidth} height={PLOT_HEIGHT} role="group">
          <line x1="0" y1="0.5" x2={plotWidth} y2="0.5" stroke="var(--color-divider)" />
          <line
            x1="0"
            y1={PLOT_HEIGHT / 2}
            x2={plotWidth}
            y2={PLOT_HEIGHT / 2}
            stroke="var(--color-divider)"
          />
          <line
            x1="0"
            y1={PLOT_HEIGHT - 0.5}
            x2={plotWidth}
            y2={PLOT_HEIGHT - 0.5}
            stroke="var(--color-divider)"
          />

          {#if hover >= 0}
            <rect
              x={slot * hover}
              y="0"
              width={slot}
              height={PLOT_HEIGHT}
              fill="var(--color-background)"
            />
          {/if}

          {#each layout as item (item.index)}
            <path
              d={item.incomePath}
              fill="var(--color-chart-in)"
              fill-opacity={item.bar.partial ? 0.45 : 1}
            />
            <path
              d={item.expensesPath}
              fill="var(--color-chart-out)"
              fill-opacity={item.bar.partial ? 0.45 : 1}
            />
          {/each}

          {#each layout as item (item.index)}
            <rect
              class="month-slot"
              x={slot * item.index}
              y="0"
              width={slot}
              height={PLOT_HEIGHT}
              fill="transparent"
              pointer-events="all"
              data-chart-slot={item.index}
              role="img"
              aria-label={item.bar.title}
              onmouseenter={() => enter(item.index)}
              onmouseleave={leave}
            />
          {/each}

          {#if tooltip}
            <g class="tooltip" data-testid="chart-tooltip" pointer-events="none">
              <rect
                x={tooltip.box.x}
                y={tooltip.box.y}
                width={tooltip.box.width}
                height={tooltip.box.height}
                rx="8"
                fill="var(--color-text)"
              />
              <text
                x={tooltip.box.x + 12}
                y={tooltip.title}
                fill="var(--color-surface)"
                font-size="13"
                font-weight="600"
              >
                {tooltip.bar.title}
              </text>
              {#if tooltip.bar.partial}
                <text
                  x={tooltip.box.x + 12}
                  y={tooltip.soFar}
                  fill="var(--color-surface)"
                  fill-opacity="0.75"
                  font-size="12"
                  font-weight="500"
                >
                  {soFarText}
                </text>
              {/if}
              <circle
                cx={tooltip.box.x + 15}
                cy={tooltip.firstRow - 4}
                r="3.5"
                fill="var(--color-chart-in)"
              />
              <text
                x={tooltip.box.x + 22}
                y={tooltip.firstRow}
                fill="var(--color-surface)"
                font-size="12"
                font-weight="500"
              >
                {incomeLabel}
              </text>
              <text
                x={tooltip.box.x + tooltip.box.width - 12}
                y={tooltip.firstRow}
                fill="var(--color-surface)"
                font-size="13"
                font-weight="500"
                text-anchor="end"
              >
                {tooltip.bar.incomeText}
              </text>
              <circle
                cx={tooltip.box.x + 15}
                cy={tooltip.secondRow - 4}
                r="3.5"
                fill="var(--color-chart-out)"
              />
              <text
                x={tooltip.box.x + 22}
                y={tooltip.secondRow}
                fill="var(--color-surface)"
                font-size="12"
                font-weight="500"
              >
                {expensesLabel}
              </text>
              <text
                x={tooltip.box.x + tooltip.box.width - 12}
                y={tooltip.secondRow}
                fill="var(--color-surface)"
                font-size="13"
                font-weight="500"
                text-anchor="end"
              >
                {tooltip.bar.expensesText}
              </text>
            </g>
          {/if}
        </svg>
      </div>
    </div>

    <div class="months">
      {#each bars as bar, index (index)}
        <span class="month-label">{bar.label}</span>
      {/each}
    </div>

    <div class="legend">
      <span class="key">
        <span class="dot in"></span>
        <span>{incomeLabel}</span>
      </span>
      <span class="key">
        <span class="dot out"></span>
        <span>{expensesLabel}</span>
      </span>
      {#if partialNote !== ''}
        <span class="note">{partialNote}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .chart {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .empty {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .plot-row {
    display: flex;
    align-items: stretch;
    gap: 8px;
    min-width: 0;
  }

  .axis {
    display: flex;
    flex: none;
    flex-direction: column;
    justify-content: space-between;
    width: 64px;
    height: 160px;
    color: var(--color-muted-text);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
    text-align: right;
  }

  .axis span {
    display: block;
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .plot {
    flex: 1;
    min-width: 0;
    height: 160px;
  }

  .plot svg {
    display: block;
  }

  .month-slot {
    cursor: pointer;
  }

  .months {
    display: flex;
    padding-left: 72px;
  }

  .month-label {
    flex: 1;
    min-width: 0;
    color: var(--color-muted-text);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
    text-align: center;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .key {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text);
    white-space: nowrap;
  }

  .dot {
    display: block;
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: var(--radius-bar);
  }

  .dot.in {
    background: var(--color-chart-in);
  }

  .dot.out {
    background: var(--color-chart-out);
  }

  .note {
    color: var(--color-muted-text);
    white-space: nowrap;
  }
</style>
