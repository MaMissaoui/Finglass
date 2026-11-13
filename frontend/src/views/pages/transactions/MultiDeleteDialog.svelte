<script lang="ts">
  import { PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    range,
    totals,
    lines,
    more,
    t,
    onConfirm,
    onCancel,
  }: {
    title: string;
    range: string;
    totals: string;
    lines: string[];
    more: string;
    t: (key: string) => string;
    onConfirm: () => void;
    onCancel: () => void;
  } = $props();
</script>

<div class="scrim" data-testid="multi-delete-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>
    <p class="range">{range}</p>
    {#if totals !== ''}
      <p class="totals">{totals}</p>
    {/if}
    {#each lines as line, index (index)}
      <p class="booking">{line}</p>
    {/each}
    {#if more !== ''}
      <p class="more">{more}</p>
    {/if}
    <p class="hint">{t('DeleteSyncedHint')}</p>
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onCancel} />
      <PrimaryButton text={t('Delete')} destructive onclick={onConfirm} />
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
    width: min(560px, 100%);
    max-height: 100%;
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

  .range {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .totals {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .booking {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .more,
  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
