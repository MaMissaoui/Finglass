<script lang="ts">
  import Icon from '../components/Icon.svelte';
  import type { DashboardCategoryRowDTO } from '../api';
  import { clampFraction, splitSegments } from './geometry';

  let {
    rows,
    transfersNote = '',
    selectable = true,
    onselect,
  }: {
    rows: DashboardCategoryRowDTO[];
    transfersNote?: string;
    selectable?: boolean;
    onselect?: (categoryID: number) => void;
  } = $props();

  let width = $state(0);

  const visible = $derived(
    rows
      .map((row, index) => ({ row, index }))
      .filter((entry) => clampFraction(entry.row.fraction) > 0),
  );
  const segments = $derived.by(() => {
    const layout = splitSegments(
      width,
      visible.map((entry) => entry.row.fraction),
    );
    return layout.map((segment, index) => {
      const entry = visible[index];
      return {
        ...segment,
        colour: entry ? colourOf(entry.row) : 'transparent',
      };
    });
  });

  function colourOf(row: DashboardCategoryRowDTO): string {
    if (row.isMore || row.colour === '') {
      return 'var(--color-divider)';
    }
    return row.colour;
  }

  function clickable(row: DashboardCategoryRowDTO): boolean {
    return selectable && !row.isMore;
  }

  function select(row: DashboardCategoryRowDTO): void {
    if (clickable(row)) {
      onselect?.(row.categoryId);
    }
  }
</script>

{#snippet rowContent(row: DashboardCategoryRowDTO)}
  <span class="dot" style:background={colourOf(row)}></span>
  <span class="label">{row.label}</span>
  <span class="percent">{row.percentText}</span>
  <span class="amount">{row.amountText}</span>
  {#if clickable(row)}
    <Icon name="keyboard_arrow_right" size={18} />
  {:else}
    <span class="chevron-space" aria-hidden="true"></span>
  {/if}
{/snippet}

<div class="split">
  <div class="track" bind:clientWidth={width}>
    <svg {width} height="8" aria-hidden="true">
      {#each segments as segment (segment.x)}
        <rect x={segment.x} y="0" width={segment.width} height="8" fill={segment.colour} />
      {/each}
    </svg>
  </div>

  <div class="rows">
    {#each visible as entry (entry.index)}
      {#if clickable(entry.row)}
        <button
          type="button"
          class="row clickable"
          data-category-id={entry.row.categoryId}
          onclick={() => select(entry.row)}
        >
          {@render rowContent(entry.row)}
        </button>
      {:else}
        <div class="row" class:more={entry.row.isMore} data-category-id={entry.row.categoryId}>
          {@render rowContent(entry.row)}
        </div>
      {/if}
    {/each}
  </div>

  {#if transfersNote !== ''}
    <p class="note">{transfersNote}</p>
  {/if}
</div>

<style>
  .split {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .track {
    height: 8px;
    border-radius: 4px;
    background: var(--color-divider);
    overflow: hidden;
  }

  .track svg {
    display: block;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    height: 28px;
    margin: 0;
    padding: 0 4px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
  }

  button.row {
    cursor: pointer;
  }

  .row.clickable:hover {
    background: var(--color-background);
  }

  .row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .row.more {
    color: var(--color-muted-text);
  }

  .dot {
    display: block;
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: var(--radius-bar);
  }

  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .percent {
    flex: none;
    width: 48px;
    text-align: right;
  }

  .amount {
    flex: none;
    width: 96px;
    text-align: right;
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
  }

  .more .percent,
  .more .amount {
    color: var(--color-muted-text);
  }

  .chevron-space {
    display: block;
    flex: none;
    width: 18px;
    height: 18px;
  }

  .note {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }
</style>
