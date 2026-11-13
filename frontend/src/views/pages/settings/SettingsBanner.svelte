<script lang="ts">
  import { Icon, IconButton } from '../../../lib/components';

  let {
    errorText,
    messageText,
    dismissLabel,
    ondismiss,
  }: {
    errorText: string;
    messageText: string;
    dismissLabel: string;
    ondismiss: () => void;
  } = $props();

  const isError = $derived(errorText !== '');
  const text = $derived(isError ? errorText : messageText);
</script>

{#if errorText !== '' || messageText !== ''}
  <div class="banner" class:error={isError} role={isError ? 'alert' : 'status'}>
    <span class="icon" class:error={isError}>
      <Icon name={isError ? 'warning' : 'check_circle'} size={20} />
    </span>
    <span class="text">{text}</span>
    <IconButton icon="close" label={dismissLabel} iconSize={18} onclick={ondismiss} />
  </div>
{/if}

<style>
  .banner {
    display: flex;
    flex: none;
    align-items: center;
    gap: 12px;
    min-height: 46px;
    padding: 0 12px 0 24px;
    background: var(--color-surface);
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
    min-width: 0;
    color: var(--color-success);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .banner.error .text {
    color: var(--color-error);
  }
</style>
