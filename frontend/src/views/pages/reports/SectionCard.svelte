<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    title = '',
    children,
  }: {
    title?: string;
    children: Snippet;
  } = $props();
</script>

<section class="section">
  {#if title !== ''}
    <h2>{title}</h2>
  {/if}
  {@render children()}
</section>

<style>
  .section {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 100%;
    min-width: 0;
    padding: 16px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
</style>
