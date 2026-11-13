<script lang="ts">
  let {
    dateText,
    title,
    meta,
    amountText,
    expense,
    interactive = false,
    onclick,
  }: {
    dateText: string;
    title: string;
    meta: string;
    amountText: string;
    expense: boolean;
    interactive?: boolean;
    onclick?: () => void;
  } = $props();
</script>

{#snippet content()}
  <span class="date">{dateText}</span>
  <span class="text">
    <span class="title">{title}</span>
    <span class="meta">{meta}</span>
  </span>
  <span class="amount" class:expense>{amountText}</span>
{/snippet}

{#if interactive}
  <button type="button" class="row interactive" onclick={() => onclick?.()}>
    {@render content()}
  </button>
{:else}
  <div class="row">
    {@render content()}
  </div>
{/if}

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 44px;
    margin: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
  }

  .row.interactive {
    cursor: pointer;
  }

  .row.interactive:hover {
    background: var(--color-background);
  }

  .row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .date {
    flex: none;
    width: 90px;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    white-space: nowrap;
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }

  .title {
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .meta {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .amount {
    flex: none;
    color: var(--color-money-in);
    font-size: var(--type-body-size);
    font-weight: var(--type-title-small-weight);
    text-align: right;
    white-space: nowrap;
  }

  .amount.expense {
    color: var(--color-money-out);
  }
</style>
