<script lang="ts">
  let {
    label,
    selected = false,
    onclick,
  }: {
    label: string;
    selected?: boolean;
    onclick?: () => void;
  } = $props();
</script>

<button
  type="button"
  tabindex="0"
  class="chip"
  class:selected
  aria-pressed={selected}
  onclick={() => onclick?.()}
>
  {label}
</button>

<style>
  .chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 32px;
    min-width: 84px;
    padding: 0 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-control-small);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .chip:hover:not(.selected) {
    background: var(--color-background);
  }

  .chip.selected {
    background: var(--color-selected);
    border-color: var(--color-primary);
    color: var(--color-on-selected);
  }

  .chip:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
