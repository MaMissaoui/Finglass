<script lang="ts">
  import { PrimaryButton, TextButton } from '../../../lib/components';

  let {
    id,
    title,
    text,
    t,
    onConfirm,
    onCancel,
  }: {
    id: number;
    title: string;
    text: string;
    t: (key: string) => string;
    onConfirm: (id: number) => void;
    onCancel: () => void;
  } = $props();
</script>

<div class="scrim" data-testid="delete-transaction-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>
    <p class="body-text">{text}</p>
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onCancel} />
      <PrimaryButton text={t('Delete')} destructive onclick={() => onConfirm(id)} />
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
    width: min(440px, 100%);
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

  .body-text {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
