<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    title,
    previousEnabled = true,
    nextEnabled = true,
    previousLabel,
    nextLabel,
    titleHasMenu = false,
    onprevious,
    onnext,
    ontitleclicked,
  }: {
    title: string;
    previousEnabled?: boolean;
    nextEnabled?: boolean;
    previousLabel: string;
    nextLabel: string;
    titleHasMenu?: boolean;
    onprevious?: () => void;
    onnext?: () => void;
    ontitleclicked?: () => void;
  } = $props();
</script>

<div class="period">
  <button
    type="button"
    tabindex="0"
    class="arrow"
    disabled={!previousEnabled}
    aria-label={previousLabel}
    title={previousLabel}
    onclick={() => onprevious?.()}
  >
    <Icon name="keyboard_arrow_left" size={20} />
  </button>

  {#if titleHasMenu}
    <button
      type="button"
      tabindex="0"
      class="title title-button"
      onclick={() => ontitleclicked?.()}
    >
      <span>{title}</span>
      <Icon name="arrow_drop_down" size={16} />
    </button>
  {:else}
    <span class="title">{title}</span>
  {/if}

  <button
    type="button"
    tabindex="0"
    class="arrow"
    disabled={!nextEnabled}
    aria-label={nextLabel}
    title={nextLabel}
    onclick={() => onnext?.()}
  >
    <Icon name="keyboard_arrow_right" size={20} />
  </button>
</div>

<style>
  .period {
    display: inline-flex;
    align-items: center;
    gap: var(--space-sm);
    height: var(--size-control-height-large);
  }

  .arrow {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--size-control-height-large);
    height: var(--size-control-height-large);
    padding: 0;
    border: 1px solid var(--color-divider);
    border-radius: 50%;
    background: transparent;
    color: var(--color-primary);
    cursor: pointer;
  }

  .arrow:hover:not(:disabled) {
    background: var(--color-control-fill);
  }

  .arrow:disabled {
    color: var(--color-muted-text);
    opacity: 0.4;
    cursor: default;
  }

  .arrow:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-xs);
    height: var(--size-control-height-large);
    padding: 0 var(--space-xs);
    border: 0;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-figure-size);
    font-weight: var(--type-figure-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .title-button {
    cursor: pointer;
    border-radius: var(--radius-control);
  }

  .title-button:hover {
    background: var(--color-background);
  }

  .title-button :global(.icon) {
    color: var(--color-muted-text);
  }

  .title-button:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
