<script lang="ts">
  import type { ReportAmountRowDTO } from '../../../lib/api';
  import CategorySplit from '../../../lib/charts/CategorySplit.svelte';

  let {
    rows,
    emptyText,
    colourFor,
  }: {
    rows: ReportAmountRowDTO[];
    emptyText: string;
    colourFor: (label: string) => string;
  } = $props();

  type TaxCategoryRow = {
    label: string;
    percentText: string;
    amountText: string;
    colour: string;
    categoryId: number;
    isMore: boolean;
    fraction: number;
  };

  // Bar segments are each row's part of the whole; the DTO's fraction is
  // relative to the largest row, so it is rescaled. The shares come from Go.
  const splitRows = $derived.by<TaxCategoryRow[]>(() => {
    const total = rows.reduce((sum, row) => sum + row.fraction, 0);
    const fractions = rows.map((row) => (total > 0 ? row.fraction / total : 0));
    return rows.map((row, index) => ({
      label: row.label,
      percentText: row.percentText,
      amountText: row.amountText,
      colour: colourFor(row.label),
      categoryId: -1,
      isMore: false,
      fraction: fractions[index] ?? 0,
    }));
  });
</script>

{#if emptyText !== ''}
  <p class="empty">{emptyText}</p>
{/if}

{#if splitRows.length > 0}
  <CategorySplit rows={splitRows} selectable={false} />
{/if}

<style>
  .empty {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }
</style>
