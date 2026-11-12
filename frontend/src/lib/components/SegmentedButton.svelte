<script lang="ts">
  import { tick } from 'svelte';

  let {
    labels,
    selected,
    onselect,
  }: {
    labels: string[];
    selected: number;
    onselect?: (index: number) => void;
  } = $props();

  let group: HTMLDivElement;

  function choose(index: number): void {
    if (index < 0 || index >= labels.length) {
      return;
    }
    onselect?.(index);
  }

  async function move(index: number): Promise<void> {
    const next = Math.min(Math.max(index, 0), labels.length - 1);
    choose(next);
    await tick();
    group?.querySelectorAll<HTMLButtonElement>('button')[next]?.focus();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowLeft') {
      event.preventDefault();
      void move(selected - 1);
    } else if (event.key === 'ArrowRight') {
      event.preventDefault();
      void move(selected + 1);
    } else if (event.key === 'Home') {
      event.preventDefault();
      void move(0);
    } else if (event.key === 'End') {
      event.preventDefault();
      void move(labels.length - 1);
    }
  }
</script>

<div
  class="segmented"
  role="radiogroup"
  tabindex="-1"
  bind:this={group}
  {onkeydown}
  aria-label={labels[selected] ?? ''}
>
  {#each labels as label, index (index)}
    <button
      type="button"
      class="segment"
      class:selected={index === selected}
      role="radio"
      aria-checked={index === selected}
      tabindex="0"
      onclick={() => choose(index)}
    >
      {label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    display: inline-flex;
    height: var(--size-control-height);
    padding: 2px;
    gap: 2px;
    border-radius: var(--radius-control);
    background: var(--color-control-fill);
  }

  .segmented:focus-within {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .segment {
    flex: 1;
    min-width: max-content;
    height: 100%;
    padding: 0 16px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-muted-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .segment.selected {
    background: var(--color-surface);
    border: 1px solid var(--color-divider);
    color: var(--color-text);
    font-weight: var(--type-title-medium-weight);
  }

  .segment:focus-visible {
    outline: none;
  }
</style>
