<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    text,
    children,
  }: {
    text: string;
    children: Snippet;
  } = $props();

  let visible = $state(false);
</script>

<span
  class="anchor"
  role="presentation"
  onmouseenter={() => (visible = true)}
  onmouseleave={() => (visible = false)}
  onfocusin={() => (visible = true)}
  onfocusout={() => (visible = false)}
>
  {@render children()}
  {#if visible && text}
    <span class="bubble" role="tooltip">{text}</span>
  {/if}
</span>

<style>
  .anchor {
    position: relative;
    display: inline-flex;
  }

  .bubble {
    position: absolute;
    z-index: 30;
    bottom: calc(100% + 6px);
    left: 0;
    max-width: 240px;
    padding: 6px 10px;
    border-radius: var(--radius-panel);
    background: var(--color-ink);
    color: var(--color-paper);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.3;
    white-space: normal;
    box-shadow: var(--shadow-menu);
    pointer-events: none;
  }
</style>
