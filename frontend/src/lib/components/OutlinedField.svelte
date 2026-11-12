<script lang="ts">
  const generatedId = $props.id();

  let {
    id = generatedId,
    label,
    value = $bindable(''),
    error = false,
    supportingText = '',
    enabled = true,
    readOnly = false,
    secret = false,
    multiline = false,
    oninput,
    onaccepted,
  }: {
    id?: string;
    label: string;
    value?: string;
    error?: boolean;
    supportingText?: string;
    enabled?: boolean;
    readOnly?: boolean;
    secret?: boolean;
    multiline?: boolean;
    oninput?: (value: string) => void;
    onaccepted?: (value: string) => void;
  } = $props();

  let focused = $state(false);
  let field = $state<HTMLInputElement | HTMLTextAreaElement>();

  const floating = $derived(focused || value !== '' || error);
  const supportId = $derived(`${id}-support`);

  export function focusInput(): void {
    field?.focus();
  }

  function report(next: string): void {
    value = next;
    oninput?.(next);
  }

  function onkeydown(event: KeyboardEvent): void {
    if (!multiline && event.key === 'Enter') {
      onaccepted?.(value);
    }
  }
</script>

<div class="outlined-field">
  <div class="field" class:error class:disabled={!enabled} class:multiline>
    <label for={id} class:floating>{label}</label>
    {#if multiline}
      <textarea
        bind:this={field}
        {id}
        bind:value
        disabled={!enabled}
        readonly={readOnly}
        aria-invalid={error}
        aria-describedby={supportingText ? supportId : undefined}
        onfocus={() => (focused = true)}
        onblur={() => (focused = false)}
        oninput={() => report(value)}
        {onkeydown}></textarea>
    {:else}
      <input
        bind:this={field}
        {id}
        type={secret ? 'password' : 'text'}
        bind:value
        disabled={!enabled}
        readonly={readOnly}
        autocomplete="off"
        spellcheck={secret ? 'false' : undefined}
        aria-invalid={error}
        aria-describedby={supportingText ? supportId : undefined}
        onfocus={() => (focused = true)}
        onblur={() => (focused = false)}
        oninput={() => report(value)}
        {onkeydown}
      />
    {/if}
  </div>

  {#if supportingText}
    <p class="supporting" class:error id={supportId} role={error ? 'alert' : undefined}>
      {supportingText}
    </p>
  {/if}
</div>

<style>
  .outlined-field {
    display: flex;
    flex-direction: column;
    gap: var(--space-xs);
    min-width: 0;
  }

  .field {
    position: relative;
    height: var(--size-field-height);
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-field);
    background: var(--color-surface);
  }

  .field:focus-within {
    border-color: var(--color-primary);
    box-shadow: inset 0 0 0 1px var(--color-primary);
  }

  .field.error {
    border-color: var(--color-error);
  }

  .field.error:focus-within {
    box-shadow: inset 0 0 0 1px var(--color-error);
  }

  .field.disabled {
    opacity: 0.55;
  }

  .field.multiline {
    height: auto;
    min-height: var(--size-field-height);
    padding: 16px;
  }

  label {
    position: absolute;
    z-index: 1;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    padding: 0 4px;
    background: var(--color-surface);
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    pointer-events: none;
    transition:
      top 120ms ease,
      font-size 120ms ease;
  }

  label.floating {
    top: 0;
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .field:focus-within label {
    color: var(--color-primary);
  }

  .field.error label {
    color: var(--color-error);
  }

  input,
  textarea {
    position: absolute;
    inset: 16px;
    width: calc(100% - 32px);
    height: 24px;
    padding: 0;
    border: 0;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .field.multiline textarea {
    position: static;
    width: 100%;
    height: auto;
    min-height: 24px;
  }

  .supporting {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .supporting.error {
    color: var(--color-error);
  }
</style>
