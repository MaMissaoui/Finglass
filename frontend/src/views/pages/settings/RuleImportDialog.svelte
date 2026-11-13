<script lang="ts">
  import { Checkbox, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    newRulesText,
    categoriesText,
    categoriesList,
    duplicatesText,
    conflictsText,
    conflictsList,
    errorsText,
    errorsList,
    showCategorize,
    categorizeText,
    categorize,
    busy,
    confirmEnabled,
    cancelText,
    importText,
    onconfirm,
    oncancel,
  }: {
    title: string;
    newRulesText: string;
    categoriesText: string;
    categoriesList: string;
    duplicatesText: string;
    conflictsText: string;
    conflictsList: string;
    errorsText: string;
    errorsList: string;
    showCategorize: boolean;
    categorizeText: string;
    categorize: boolean;
    busy: boolean;
    confirmEnabled: boolean;
    cancelText: string;
    importText: string;
    onconfirm: (categorize: boolean) => void;
    oncancel: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let checkbox = $state(categorize);
</script>

<div class="scrim" data-testid="rule-import-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <div class="scroll">
      <p class="body">{newRulesText}</p>
      {#if categoriesText !== ''}
        <p class="body">{categoriesText}</p>
      {/if}
      {#if categoriesList !== ''}
        <p class="small">{categoriesList}</p>
      {/if}
      {#if duplicatesText !== ''}
        <p class="body">{duplicatesText}</p>
      {/if}
      {#if conflictsText !== ''}
        <p class="body">{conflictsText}</p>
      {/if}
      {#if conflictsList !== ''}
        <p class="small">{conflictsList}</p>
      {/if}
      {#if errorsText !== ''}
        <p class="body">{errorsText}</p>
      {/if}
      {#if errorsList !== ''}
        <p class="small">{errorsList}</p>
      {/if}
      {#if showCategorize}
        <Checkbox text={categorizeText} bind:checked={checkbox} />
      {/if}
    </div>

    <div class="actions">
      <TextButton text={cancelText} enabled={!busy} onclick={oncancel} />
      <PrimaryButton
        text={importText}
        enabled={confirmEnabled && !busy}
        onclick={() => onconfirm(checkbox)}
      />
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
    width: min(580px, 100%);
    max-height: calc(100vh - 48px);
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

  .scroll {
    display: flex;
    height: 300px;
    flex-direction: column;
    gap: 8px;
    overflow-y: auto;
  }

  .body,
  .small {
    margin: 0;
    white-space: pre-line;
  }

  .body {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .small {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
