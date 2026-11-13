<script lang="ts">
  import { OutlinedField, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    from,
    to,
    error,
    t,
    onApply,
    onClose,
  }: {
    from: string;
    to: string;
    error: string;
    t: (key: string) => string;
    onApply: (from: string, to: string) => void;
    onClose: () => void;
  } = $props();

  // The parent keys this dialog by its opening values, so these are read once.
  // svelte-ignore state_referenced_locally
  let fromText = $state(from);
  // svelte-ignore state_referenced_locally
  let toText = $state(to);
</script>

<div class="scrim" data-testid="custom-range-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={t('CustomRange')}>
    <h2>{t('CustomRange')}</h2>
    <div class="fields">
      <OutlinedField label={t('FromDate')} bind:value={fromText} />
      <OutlinedField label={t('ToDate')} bind:value={toText} />
    </div>
    {#if error !== ''}
      <p class="error" role="alert">{error}</p>
    {/if}
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <PrimaryButton
        text={t('Apply')}
        enabled={fromText !== '' && toText !== ''}
        onclick={() => onApply(fromText, toText)}
      />
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

  .fields {
    display: flex;
    gap: 8px;
  }

  .fields :global(.outlined-field) {
    flex: 1;
    min-width: 0;
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
