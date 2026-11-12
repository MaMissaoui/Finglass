<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../icons';

  let {
    label,
    icon,
    selected = false,
    collapsed = false,
    onactivated,
  }: {
    label: string;
    icon: IconName;
    selected?: boolean;
    collapsed?: boolean;
    onactivated?: () => void;
  } = $props();
</script>

<button
  type="button"
  tabindex="0"
  class="sidebar-item"
  class:selected
  class:collapsed
  aria-label={label}
  aria-current={selected ? 'page' : undefined}
  onclick={() => onactivated?.()}
>
  <span class="icon">
    <Icon name={icon} size={24} />
  </span>
  {#if !collapsed}
    <span class="label">{label}</span>
  {/if}
</button>

<style>
  .sidebar-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: var(--size-control-height-large);
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-panel);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-navigation-size);
    font-weight: var(--type-navigation-weight);
    text-align: left;
    cursor: pointer;
    overflow: hidden;
  }

  .sidebar-item:hover:not(.selected) {
    background: var(--color-background);
  }

  .sidebar-item.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
    font-weight: var(--type-navigation-selected-weight);
  }

  .sidebar-item.selected::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    width: 3px;
    height: 100%;
    background: var(--color-primary);
  }

  .sidebar-item.collapsed {
    height: 44px;
    justify-content: center;
    padding: 0;
  }

  .icon {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  .sidebar-item.selected .icon {
    color: var(--color-on-selected);
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sidebar-item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }
</style>
