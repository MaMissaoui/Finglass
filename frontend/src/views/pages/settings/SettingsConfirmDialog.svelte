<script lang="ts">
  import { PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    text,
    confirmText,
    cancelText,
    testId,
    onconfirm,
    oncancel,
  }: {
    title: string;
    text: string;
    confirmText: string;
    cancelText: string;
    testId: string;
    onconfirm: () => void;
    oncancel: () => void;
  } = $props();
</script>

<div class="scrim" data-testid={testId}>
  <div class="modal" role="alertdialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>
    <p class="body-text">{text}</p>
    <div class="actions">
      <TextButton text={cancelText} onclick={oncancel} />
      <PrimaryButton text={confirmText} destructive onclick={onconfirm} />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(440px, 100%);
    max-height: calc(100vh - 48px);
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow-y: auto;
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
