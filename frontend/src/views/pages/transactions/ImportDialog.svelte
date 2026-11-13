<script lang="ts">
  import { Checkbox, PrimaryButton, SelectPill, TextButton } from '../../../lib/components';
  import type { ImportCSVFieldDTO, ImportFileRowDTO } from '../../../lib/api';

  let {
    title,
    summary,
    files,
    csvFields,
    csvHint,
    accountOptions,
    accountIndex,
    balanceAllowed,
    updateBalance,
    balanceText,
    newerText,
    importing,
    confirmText,
    confirmEnabled,
    t,
    onAccount,
    onCsvChanged,
    onConfirm,
    onCancel,
  }: {
    title: string;
    summary: string;
    files: ImportFileRowDTO[];
    csvFields: ImportCSVFieldDTO[];
    csvHint: string;
    accountOptions: string[];
    accountIndex: number;
    balanceAllowed: boolean;
    updateBalance: boolean;
    balanceText: string;
    newerText: string;
    importing: boolean;
    confirmText: string;
    confirmEnabled: boolean;
    t: (key: string) => string;
    onAccount: (index: number) => void;
    onCsvChanged: (field: string, value: string) => void;
    onConfirm: (updateBalance: boolean) => void;
    onCancel: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let update = $state(updateBalance);

  const accountValue = $derived(accountOptions[accountIndex] ?? '');
</script>

<div class="scrim" data-testid="import-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <div class="body">
      <h2>{title}</h2>
      {#if summary !== ''}
        <p class="summary">{summary}</p>
      {/if}

      <div class="files">
        {#each files as file (file.name)}
          <div class="file" class:failure={file.failure}>
            <span class="file-name">{file.name}</span>
            <span class="file-count">{file.countText}</span>
            <span class="file-period">{file.periodText}</span>
            <span class="file-warning">{file.warningText}</span>
          </div>
        {/each}
      </div>

      <div class="row">
        <SelectPill
          labelPrefix={t('IntoAccount')}
          value={accountValue}
          options={accountOptions}
          onchosen={onAccount}
        />
      </div>

      {#if csvFields.length > 0}
        <div class="columns">
          <span class="columns-title">{t('Columns')}</span>
          {#if csvHint !== ''}
            <span class="columns-hint">{csvHint}</span>
          {/if}
          <div class="grid">
            {#each csvFields as field (field.field)}
              <div class="cell">
                <SelectPill
                  labelPrefix={`${field.label}: `}
                  value={field.options[field.index] ?? ''}
                  options={field.options}
                  onchosen={(index) => onCsvChanged(field.field, field.options[index] ?? '')}
                />
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if balanceAllowed}
        <Checkbox text={balanceText} bind:checked={update} />
      {/if}

      {#if newerText !== ''}
        <p class="hint">{newerText}</p>
      {/if}

      <p class="hint">{t('BookingsSkippedHint')}</p>
    </div>

    <div class="actions">
      <TextButton text={t('Cancel')} enabled={!importing} onclick={onCancel} />
      <PrimaryButton
        text={confirmText}
        enabled={confirmEnabled && !importing}
        onclick={() => onConfirm(update)}
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
    padding: 16px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(720px, 100%);
    max-height: 100%;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow: hidden;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  .body {
    display: flex;
    min-height: 0;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .summary {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .files {
    display: flex;
    flex-direction: column;
  }

  .file {
    display: grid;
    min-height: 30px;
    grid-template-columns: minmax(0, 2fr) 90px 150px minmax(0, 1fr);
    align-items: center;
    gap: 10px;
  }

  .file-name {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .file.failure .file-name {
    color: var(--color-error);
  }

  .file.failure .file-count,
  .file.failure .file-period {
    display: none;
  }

  .file.failure .file-warning {
    grid-column: 2 / 5;
    justify-self: end;
    padding-right: 56px;
    white-space: nowrap;
  }

  .file-count {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    text-align: right;
    white-space: nowrap;
  }

  .file-period {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    text-align: right;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .file-warning {
    color: var(--color-caution);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .columns {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .columns-title {
    color: var(--color-text);
    font-size: var(--type-title-small-size);
    font-weight: var(--type-title-small-weight);
  }

  .columns-hint {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 8px;
    padding-top: 2px;
  }

  .cell {
    min-width: 0;
  }

  .cell :global(.select) {
    display: block;
    width: 100%;
  }

  .cell :global(.trigger) {
    width: 100%;
    justify-content: space-between;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .actions {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
