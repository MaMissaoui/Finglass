<script lang="ts">
  import { OutlinedField, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    name,
    errorText,
    nameLabel,
    cancelText,
    saveText,
    onclose,
    onsave,
  }: {
    title: string;
    name: string;
    errorText: string;
    nameLabel: string;
    cancelText: string;
    saveText: string;
    onclose: () => void;
    onsave: (name: string) => void;
  } = $props();

  // The parent renders this dialog only while it is open, so this is the name
  // as it was when the dialog opened.
  // svelte-ignore state_referenced_locally
  let nameText = $state(name);
</script>

<div class="scrim" data-testid="profile-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <OutlinedField label={nameLabel} bind:value={nameText} />

    {#if errorText !== ''}
      <p class="error" role="alert">{errorText}</p>
    {/if}

    <div class="actions">
      <TextButton text={cancelText} onclick={onclose} />
      <PrimaryButton text={saveText} enabled={nameText !== ''} onclick={() => onsave(nameText)} />
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
