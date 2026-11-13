<script lang="ts">
  import { Icon } from '../../../lib/components';

  let {
    label,
    options,
    primary = false,
    onchosen,
  }: {
    label: string;
    options: string[];
    primary?: boolean;
    onchosen?: (index: number) => void;
  } = $props();

  let open = $state(false);

  function choose(index: number): void {
    open = false;
    onchosen?.(index);
  }
</script>

<div class="picker" style:--picker-width={primary ? '140px' : '150px'}>
  <button
    type="button"
    tabindex="0"
    class="action"
    class:primary
    aria-haspopup="listbox"
    aria-expanded={open}
    onclick={() => (open = !open)}
  >
    <span class="label">{label}</span>
    <Icon name="arrow_drop_down" size={18} />
  </button>

  {#if open}
    <ul class="menu" role="listbox" aria-label={label}>
      {#each options as option, index (option)}
        <li
          role="option"
          aria-selected="false"
          tabindex="-1"
          class="option"
          onclick={() => choose(index)}
          onkeydown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault();
              choose(index);
            }
          }}
        >
          {option}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .picker {
    position: relative;
    flex: none;
    width: var(--picker-width);
  }

  .action {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    height: 32px;
    padding: 0 12px 0 14px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-control-small);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .action:hover {
    background: var(--color-background);
  }

  .action:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .action.primary {
    border-color: var(--color-primary);
    background: var(--color-primary);
    color: var(--color-on-primary);
  }

  .action.primary:hover {
    background: var(--color-primary-hover);
  }

  .action :global(.icon) {
    flex: none;
    color: var(--color-muted-text);
  }

  .action.primary :global(.icon) {
    color: var(--color-on-primary);
  }

  .label {
    flex: 1;
    overflow: hidden;
    white-space: nowrap;
    text-align: center;
    text-overflow: ellipsis;
  }

  .menu {
    position: absolute;
    z-index: 25;
    top: calc(100% + 4px);
    left: 0;
    width: max(100%, 220px);
    max-height: 280px;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
    background: var(--color-surface);
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-menu);
    box-shadow: var(--shadow-menu);
  }

  .option {
    display: flex;
    align-items: center;
    height: 32px;
    padding: 0 12px;
    color: var(--color-text);
    font-size: var(--type-body-size);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .option:hover {
    background: var(--color-background);
  }
</style>
