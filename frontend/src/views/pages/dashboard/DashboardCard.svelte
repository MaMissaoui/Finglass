<script lang="ts">
  import type { Snippet } from 'svelte';
  import TextButton from '../../../lib/components/TextButton.svelte';

  let {
    title,
    actionLabel = '',
    interactive = false,
    label = '',
    onaction,
    onclick,
    children,
  }: {
    title: string;
    actionLabel?: string;
    interactive?: boolean;
    label?: string;
    onaction?: () => void;
    onclick?: () => void;
    children: Snippet;
  } = $props();

  function activate(): void {
    onclick?.();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      activate();
    }
  }
</script>

{#snippet content()}
  <div class="head">
    <h2>{title}</h2>
    {#if actionLabel !== ''}
      <TextButton text={actionLabel} onclick={() => onaction?.()} />
    {/if}
  </div>
  <div class="body">
    {@render children()}
  </div>
{/snippet}

{#if interactive}
  <div
    class="card interactive"
    role="button"
    tabindex="0"
    aria-label={label}
    onclick={activate}
    {onkeydown}
  >
    {@render content()}
  </div>
{:else}
  <section class="card">
    {@render content()}
  </section>
{/if}

<style>
  .card {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 12px;
    width: 100%;
    min-width: 0;
    padding: 16px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .card.interactive {
    cursor: pointer;
  }

  .card.interactive:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
  }

  .head h2 {
    flex: 1;
    min-width: 0;
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .body {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
</style>
