<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from './Icon.svelte';

  const generatedId = $props.id();

  let {
    id = generatedId,
    labelPrefix = '',
    value,
    options,
    enabled = true,
    filled = false,
    onchosen,
    onvaluechosen,
  }: {
    id?: string;
    labelPrefix?: string;
    value: string;
    options: string[];
    enabled?: boolean;
    filled?: boolean;
    onchosen?: (index: number) => void;
    onvaluechosen?: (value: string) => void;
  } = $props();

  let open = $state(false);
  let active = $state(0);
  let wrapper: HTMLDivElement;
  let trigger: HTMLButtonElement;

  const selectedIndex = $derived(options.indexOf(value));
  const menuId = $derived(`${id}-menu`);

  function openMenu(): void {
    if (!enabled || options.length === 0) {
      return;
    }
    active = selectedIndex >= 0 ? selectedIndex : 0;
    open = true;
    trigger?.focus();
  }

  function closeMenu(): void {
    open = false;
  }

  function onwindowkeydown(event: KeyboardEvent): void {
    if (open && event.key === 'Escape') {
      event.preventDefault();
      closeMenu();
    }
  }

  function choose(index: number): void {
    if (index < 0 || index >= options.length) {
      return;
    }
    const option = options[index];
    closeMenu();
    onchosen?.(index);
    if (option !== undefined) {
      onvaluechosen?.(option);
    }
  }

  function onkeydown(event: KeyboardEvent): void {
    if (!open) {
      if (event.key === 'Enter' || event.key === ' ' || event.key === 'ArrowDown') {
        event.preventDefault();
        openMenu();
      }
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      closeMenu();
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      active = Math.min(active + 1, options.length - 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      active = Math.max(active - 1, 0);
    } else if (event.key === 'Home') {
      event.preventDefault();
      active = 0;
    } else if (event.key === 'End') {
      event.preventDefault();
      active = options.length - 1;
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      choose(active);
    } else if (event.key === 'Tab') {
      closeMenu();
    }
  }

  onMount(() => {
    const onpointerdown = (event: PointerEvent): void => {
      if (wrapper && !wrapper.contains(event.target as Node)) {
        closeMenu();
      }
    };
    document.addEventListener('pointerdown', onpointerdown);
    return () => document.removeEventListener('pointerdown', onpointerdown);
  });
</script>

<svelte:window onkeydown={onwindowkeydown} />

<div class="select" bind:this={wrapper}>
  <button
    bind:this={trigger}
    type="button"
    tabindex="0"
    class="trigger"
    class:filled
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={open ? menuId : undefined}
    aria-label={`${labelPrefix}${value}`}
    disabled={!enabled}
    onclick={() => (open ? closeMenu() : openMenu())}
    {onkeydown}
  >
    <span class="value">{labelPrefix}{value}</span>
    <Icon name="arrow_drop_down" size={18} />
  </button>

  {#if open}
    <ul class="menu" id={menuId} role="listbox" aria-label={`${labelPrefix}${value}`}>
      {#each options as option, index (option)}
        <li
          role="option"
          class="option"
          class:active={index === active}
          class:selected={index === selectedIndex}
          aria-selected={index === selectedIndex}
          tabindex="-1"
          onmouseenter={() => (active = index)}
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
  .select {
    position: relative;
    display: inline-block;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: max-content;
    height: var(--size-control-height-large);
    padding: 0 8px 0 16px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-control);
    background: var(--color-surface);
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .trigger:hover:not(:disabled) {
    background: var(--color-background);
  }

  .trigger.filled {
    height: var(--size-control-height);
    border: 0;
    background: var(--color-control-fill);
    color: var(--color-text);
  }

  .trigger.filled:hover:not(:disabled) {
    background: var(--color-control-fill-hover);
  }

  .trigger.filled :global(.icon) {
    color: var(--color-muted-text);
  }

  .trigger:disabled {
    opacity: 0.55;
    cursor: default;
  }

  .trigger:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .value {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu {
    position: absolute;
    z-index: 20;
    top: calc(100% + 4px);
    left: 0;
    width: max(100%, 220px);
    max-height: 320px;
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
    height: 36px;
    padding: 0 12px;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .option.active {
    background: var(--color-background);
    color: var(--color-primary);
  }

  .option.selected {
    font-weight: var(--type-label-weight);
  }
</style>
