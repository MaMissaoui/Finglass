<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../icons';

  let {
    text,
    enabled = true,
    icon,
    destructive = false,
    tooltip = '',
    onclick,
  }: {
    text: string;
    enabled?: boolean;
    icon?: IconName;
    destructive?: boolean;
    tooltip?: string;
    onclick?: () => void;
  } = $props();
</script>

<button
  type="button"
  tabindex="0"
  class="primary"
  class:destructive
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
  .primary {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-sm);
    height: var(--size-control-height);
    min-width: max-content;
    padding: 0 14px;
    border: 0;
    border-radius: var(--radius-control);
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .primary:hover:not(:disabled) {
    background: var(--color-primary-hover);
  }

  .primary.destructive {
    background: var(--color-error);
  }

  .primary.destructive:hover:not(:disabled) {
    background: var(--color-error-hover);
  }

  .primary:disabled {
    background: var(--color-divider);
    color: var(--color-muted-text);
    cursor: default;
  }

  .primary:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
