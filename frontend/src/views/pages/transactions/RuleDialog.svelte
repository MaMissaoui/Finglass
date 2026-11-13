<script lang="ts">
  import { OutlinedField, PrimaryButton, SelectPill, TextButton } from '../../../lib/components';

  let {
    pattern,
    categories,
    categoryIndex,
    error,
    t,
    onSave,
    onClose,
  }: {
    pattern: string;
    categories: string[];
    categoryIndex: number;
    error: string;
    t: (key: string) => string;
    onSave: (pattern: string, categoryIndex: number) => void;
    onClose: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let patternText = $state(pattern);
  // svelte-ignore state_referenced_locally
  let selected = $state(categoryIndex);

  const categoryValue = $derived(categories[selected] ?? '');
</script>

<div class="scrim" data-testid="rule-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={t('AddRule')}>
    <h2>{t('AddRule')}</h2>
    <OutlinedField label={t('PatternExample')} bind:value={patternText} />
    <div class="row">
      <SelectPill
        labelPrefix={`${t('ColCategory')}: `}
        value={categoryValue}
        options={categories}
        onchosen={(index) => (selected = index)}
      />
    </div>
    {#if error !== ''}
      <p class="error" role="alert">{error}</p>
    {/if}
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onClose} />
      <PrimaryButton text={t('Save')} onclick={() => onSave(patternText, selected)} />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(500px, 100%);
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

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
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
