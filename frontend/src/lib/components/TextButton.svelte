<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../icons';

  let {
    text,
    enabled = true,
    icon,
    onclick,
  }: {
    text: string;
    enabled?: boolean;
    icon?: IconName;
    onclick?: () => void;
  } = $props();
</script>

<button type="button" tabindex="0" class="text" disabled={!enabled} onclick={() => onclick?.()}>
  {#if icon}
    <Icon name={icon} size={18} />
  {/if}
  <span class="label">{text}</span>
</button>

<style>
  .text {
    display: inline-flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-sm);
    height: var(--size-control-height-large);
    min-width: max-content;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .text:hover:not(:disabled) {
    background: var(--color-background);
    color: var(--color-primary-hover);
  }

  .text:disabled {
    color: var(--color-muted-text);
    cursor: default;
  }

  .text:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
