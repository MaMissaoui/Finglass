<script lang="ts">
  import { OutlinedField, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    folder,
    text,
    wrong,
    busy,
    passphraseLabel,
    wrongText,
    cancelText,
    importText,
    onsubmit,
    oncancel,
  }: {
    title: string;
    folder: string;
    text: string;
    wrong: boolean;
    busy: boolean;
    passphraseLabel: string;
    wrongText: string;
    cancelText: string;
    importText: string;
    onsubmit: (passphrase: string) => void;
    oncancel: () => void;
  } = $props();

  // The secret lives only here while the dialog is open. Every path copies it
  // into a local, clears the field, and clears it again in finally.
  let passphrase = $state('');

  function send(): void {
    if (busy || passphrase === '') {
      return;
    }
    const value = passphrase;
    passphrase = '';
    try {
      onsubmit(value);
    } finally {
      passphrase = '';
    }
  }

  function cancel(): void {
    passphrase = '';
    oncancel();
  }
</script>

<div class="scrim" data-testid="findesk-passphrase-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <p class="folder">{folder}</p>
    <p class="body">{text}</p>

    <OutlinedField
      label={passphraseLabel}
      bind:value={passphrase}
      enabled={!busy}
      secret
      error={wrong}
      supportingText={wrong ? wrongText : ''}
      onaccepted={send}
    />

    <div class="actions">
      <TextButton text={cancelText} enabled={!busy} onclick={cancel} />
      <PrimaryButton text={importText} enabled={!busy && passphrase !== ''} onclick={send} />
      {#if busy}
        <span class="spinner" aria-hidden="true"></span>
      {/if}
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
    width: min(460px, 100%);
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

  .folder {
    margin: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .body {
    margin: 0;
    color: var(--color-muted-text);
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

  .spinner {
    width: 20px;
    height: 20px;
    border: 2px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
