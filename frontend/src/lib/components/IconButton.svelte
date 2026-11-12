<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../icons';

  let {
    icon,
    label,
    iconColor,
    iconSize,
    selected = false,
    enabled = true,
    filled = false,
    outlined = false,
    destructive = false,
    onclick,
  }: {
    icon: IconName;
    label: string;
    iconColor?: string;
    iconSize?: number;
    selected?: boolean;
    enabled?: boolean;
    filled?: boolean;
    outlined?: boolean;
    destructive?: boolean;
    onclick?: () => void;
  } = $props();

  const size = $derived(iconSize ?? (filled ? 20 : 24));
</script>

<button
  type="button"
  tabindex="0"
  class="icon-button"
  class:filled
  class:outlined
  class:selected
  class:destructive
  style:--icon-color={iconColor}
  disabled={!enabled}
  aria-label={label}
  title={label}
  onclick={() => onclick?.()}
>
  <Icon name={icon} {size} />
</button>

<style>
  .icon-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--size-control-height-large);
    height: var(--size-control-height-large);
    padding: 0;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--icon-color, var(--color-muted-text));
    cursor: pointer;
  }

  .icon-button:hover:not(:disabled) {
    background: var(--color-background);
  }

  .icon-button.filled {
    width: var(--size-icon-button);
    height: var(--size-icon-button);
    background: var(--color-control-fill);
    color: var(--color-text);
  }

  .icon-button.filled:hover:not(:disabled) {
    background: var(--color-control-fill-hover);
  }

  .icon-button.outlined:not(.filled) {
    border: 1px solid var(--color-divider);
  }

  .icon-button.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .icon-button.selected:hover:not(:disabled) {
    background: var(--color-selected);
  }

  .icon-button.selected.filled {
    background: var(--color-primary);
    color: var(--color-on-primary);
  }

  .icon-button.destructive {
    color: var(--color-muted-text);
  }

  .icon-button.destructive:hover:not(:disabled) {
    color: var(--color-error);
  }

  .icon-button:disabled {
    opacity: 0.45;
    cursor: default;
  }

  .icon-button:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
