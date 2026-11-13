<script lang="ts">
  let {
    options,
    selected,
    onselect,
    onclose,
  }: {
    options: string[];
    selected: number;
    onselect: (index: number) => void;
    onclose: () => void;
  } = $props();

  const height = $derived(Math.min(options.length * 36 + 16, 340));
</script>

<button type="button" class="backdrop" tabindex="-1" aria-hidden="true" onclick={onclose}></button>
<div class="menu" role="menu" data-testid="month-menu" style:height={`${height}px`}>
  {#each options as option, index (option)}
    <button
      type="button"
      class="item"
      role="menuitemradio"
      aria-checked={index === selected}
      onclick={() => {
        onclose();
        onselect(index);
      }}
    >
      {option}
    </button>
  {/each}
</div>

<style>
  .backdrop {
    position: absolute;
    inset: 0;
    z-index: 20;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .menu {
    position: absolute;
    z-index: 21;
    top: 66px;
    left: 64px;
    display: flex;
    flex-direction: column;
    width: 200px;
    padding: 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-menu);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
    overflow-y: auto;
  }

  .item {
    display: block;
    flex: none;
    width: 100%;
    height: 36px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .item:hover {
    background: var(--color-background);
  }

  .item[aria-checked='true'] {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }
</style>
