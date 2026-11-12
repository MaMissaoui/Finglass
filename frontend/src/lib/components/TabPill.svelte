<script lang="ts">
  let {
    text,
    selected = false,
    onclick,
  }: {
    text: string;
    selected?: boolean;
    onclick?: () => void;
  } = $props();
</script>

<button
  type="button"
  tabindex="0"
  class="tab"
  class:selected
  aria-pressed={selected}
  onclick={() => onclick?.()}
>
  {text}
</button>

<style>
  .tab {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: var(--size-control-height);
    min-width: 120px;
    padding: 0 14px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-small);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .tab:hover:not(.selected) {
    background: var(--color-background);
  }

  .tab.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .tab:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
