<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../icons';

  let {
    text,
    enabled = true,
    icon,
    tooltip = '',
    onclick,
  }: {
    text: string;
    enabled?: boolean;
    icon?: IconName;
    tooltip?: string;
    onclick?: () => void;
  } = $props();
</script>

<button
  type="button"
  tabindex="0"
  class="secondary"
  disabled={!enabled}
  title={tooltip || undefined}
  onclick={() => onclick?.()}
>
  {#if icon}
    <Icon name={icon} size={18} />
  {/if}
  <span class="label">{text}</span>
</button>

<style>
  .secondary {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-sm);
    height: var(--size-control-height);
    min-width: max-content;
    padding: 0 14px;
    border: 0;
    border-radius: var(--radius-control);
    background: var(--color-control-fill);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .secondary:hover:not(:disabled) {
    background: var(--color-control-fill-hover);
  }

  .secondary:disabled {
    color: var(--color-muted-text);
    opacity: 0.38;
    cursor: default;
  }

  .secondary:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
