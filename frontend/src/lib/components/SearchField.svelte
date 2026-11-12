<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    value = $bindable(''),
    placeholder = '',
    label,
    clearLabel,
    enabled = true,
    oninput,
  }: {
    value?: string;
    placeholder?: string;
    label?: string;
    clearLabel: string;
    enabled?: boolean;
    oninput?: (value: string) => void;
  } = $props();

  let input: HTMLInputElement;

  function report(value: string): void {
    oninput?.(value);
  }

  function clear(): void {
    if (value === '') {
      return;
    }
    value = '';
    report(value);
    input?.focus();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && value !== '') {
      event.preventDefault();
      clear();
    }
  }
</script>

<div class="search" class:disabled={!enabled}>
  <Icon name="search" size={20} />
  <input
    bind:this={input}
    type="text"
    bind:value
    {placeholder}
    disabled={!enabled}
    aria-label={label || placeholder}
    oninput={() => report(value)}
    {onkeydown}
  />
  {#if value !== '' && enabled}
    <button
      type="button"
      tabindex="0"
      class="clear"
      aria-label={clearLabel}
      title={clearLabel}
      onclick={clear}
    >
      <Icon name="close" size={18} />
    </button>
  {/if}
</div>

<style>
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    height: var(--size-control-height);
    padding: 0 6px 0 14px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-control);
    background: var(--color-surface);
    color: var(--color-muted-text);
  }

  .search:focus-within {
    border-color: var(--color-primary);
    box-shadow: inset 0 0 0 1px var(--color-primary);
  }

  .search.disabled {
    opacity: 0.55;
  }

  input {
    flex: 1;
    min-width: 0;
    height: 100%;
    border: 0;
    outline: none;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  input::placeholder {
    color: var(--color-muted-text);
  }

  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--color-muted-text);
    cursor: pointer;
  }

  .clear:hover {
    background: var(--color-background);
  }

  .clear:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }
</style>
