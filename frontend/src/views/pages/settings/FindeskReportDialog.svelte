<script lang="ts">
  import { PrimaryButton } from '../../../lib/components';

  let {
    title,
    text,
    isError,
    closeText,
    onclose,
  }: {
    title: string;
    text: string;
    isError: boolean;
    closeText: string;
    onclose: () => void;
  } = $props();
</script>

<div class="scrim" data-testid="findesk-report-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <p class="body" class:error={isError}>{text}</p>

    <div class="actions">
      <PrimaryButton text={closeText} onclick={onclose} />
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
    padding: 88px 24px 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(560px, 100%);
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

  .body {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
    white-space: pre-line;
  }

  .body.error {
    color: var(--color-error);
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
