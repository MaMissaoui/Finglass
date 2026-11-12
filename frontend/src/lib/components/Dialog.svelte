<script lang="ts">
  import { tick } from 'svelte';
  import type { Snippet } from 'svelte';
  import IconButton from './IconButton.svelte';

  const titleId = $props.id();

  let {
    open = $bindable(false),
    title,
    closeLabel,
    children,
    footer,
    onclose,
  }: {
    open?: boolean;
    title: string;
    closeLabel: string;
    children: Snippet;
    footer?: Snippet;
    onclose?: () => void;
  } = $props();

  let dialog: HTMLDialogElement;
  let previousFocus: HTMLElement | null = null;

  function requestClose(): void {
    open = false;
    onclose?.();
  }

  function restoreFocus(): void {
    previousFocus?.focus();
    previousFocus = null;
  }

  $effect(() => {
    if (!dialog) {
      return;
    }

    if (open && !dialog.open) {
      previousFocus = document.activeElement as HTMLElement | null;
      dialog.showModal();
      void tick().then(() => {
        dialog.querySelector<HTMLElement>('.dialog-close button')?.focus();
      });
    } else if (!open && dialog.open) {
      dialog.close();
    }
  });
</script>

<dialog
  bind:this={dialog}
  class="dialog"
  aria-labelledby={titleId}
  onclose={restoreFocus}
  oncancel={(event) => {
    event.preventDefault();
    requestClose();
  }}
>
  <div class="content">
    <header class="header">
      <h2 id={titleId}>{title}</h2>
      <span class="dialog-close">
        <IconButton icon="close" label={closeLabel} onclick={requestClose} />
      </span>
    </header>
    <div class="body">
      {@render children()}
    </div>
    {#if footer}
      <footer>{@render footer()}</footer>
    {/if}
  </div>
</dialog>

<style>
  .dialog {
    width: min(480px, calc(100vw - 32px));
    max-width: calc(100vw - 32px);
    max-height: calc(100vh - 64px);
    padding: 0;
    border: 0;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
    overflow: auto;
  }

  .dialog::backdrop {
    background: var(--color-scrim);
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: var(--space-md);
    padding: var(--space-lg);
  }

  .header {
    display: flex;
    align-items: flex-start;
    gap: var(--space-md);
  }

  .header h2 {
    flex: 1;
    margin: 0;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .dialog-close {
    display: inline-flex;
    flex: none;
    margin: -8px -8px 0 0;
  }

  .body {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-sm);
  }
</style>
