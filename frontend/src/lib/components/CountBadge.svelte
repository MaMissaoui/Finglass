<script lang="ts">
  let {
    count,
    selected = false,
  }: {
    count: number;
    selected?: boolean;
  } = $props();

  const label = $derived(count > 99 ? '99+' : String(count));
</script>

{#if count > 0}
  <span class="count" class:selected>{label}</span>
{/if}

<style>
  .count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 18px;
    min-width: 18px;
    padding: 0 4px;
    border: 0;
    border-radius: var(--radius-counter);
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-family: var(--font-sans);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .count.selected {
    border: 1px solid var(--color-on-selected);
    background: transparent;
    color: var(--color-on-selected);
  }
</style>
