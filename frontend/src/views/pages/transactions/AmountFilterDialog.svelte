<script lang="ts">
  import { OutlinedField, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    minimum,
    maximum,
    minLabel,
    maxLabel,
    error,
    t,
    onApply,
    onReset,
    onClose,
  }: {
    minimum: string;
    maximum: string;
    minLabel: string;
    maxLabel: string;
    error: string;
    t: (key: string) => string;
    onApply: (minimum: string, maximum: string) => void;
    onReset: () => void;
    onClose: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let minText = $state(minimum);
  // svelte-ignore state_referenced_locally
  let maxText = $state(maximum);
</script>

<div class="scrim" data-testid="amount-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={t('AmountFilterAny')}>
    <h2>{t('AmountFilterAny')}</h2>
    <OutlinedField label={minLabel} bind:value={minText} />
    <OutlinedField label={maxLabel} bind:value={maxText} />
    {#if error !== ''}
      <p class="error" role="alert">{error}</p>
    {/if}
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <TextButton text={t('Reset')} onclick={onReset} />
      <PrimaryButton text={t('Apply')} onclick={() => onApply(minText, maxText)} />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(420px, 100%);
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
