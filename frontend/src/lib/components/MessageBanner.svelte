<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    text,
    isError = false,
    dismissLabel,
    ondismiss,
  }: {
    text: string;
    isError?: boolean;
    dismissLabel: string;
    ondismiss?: () => void;
  } = $props();
</script>

<div class="banner" class:error={isError} role={isError ? 'alert' : 'status'}>
  <span class="icon" class:error={isError}>
    <Icon name={isError ? 'warning' : 'check_circle'} size={20} />
  </span>
  <span class="text">{text}</span>
  <button
    type="button"
    tabindex="0"
    class="dismiss"
    aria-label={dismissLabel}
    title={dismissLabel}
    onclick={() => ondismiss?.()}
  >
    <Icon name="close" size={18} />
  </button>
</div>

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 42px;
    padding: 4px 4px 4px 14px;
    border: 1px solid var(--color-success);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
  }

  .banner.error {
    border-color: var(--color-error);
  }

  .icon {
    display: inline-flex;
    flex: none;
    color: var(--color-success);
  }

  .icon.error {
    color: var(--color-error);
  }

  .text {
    flex: 1;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .banner.error .text {
    color: var(--color-error);
  }

  .dismiss {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .dismiss:hover {
    background: var(--color-background);
  }

  .dismiss:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }
</style>
