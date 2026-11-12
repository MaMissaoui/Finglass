<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    text,
    checked = $bindable(false),
    enabled = true,
    ontoggled,
  }: {
    text: string;
    checked?: boolean;
    enabled?: boolean;
    ontoggled?: (checked: boolean) => void;
  } = $props();
</script>

<label class="checkbox" class:disabled={!enabled}>
  <span class="box-area">
    <input
      type="checkbox"
      bind:checked
      disabled={!enabled}
      onchange={(event) => ontoggled?.(event.currentTarget.checked)}
    />
    <span class="box" class:checked>
      {#if checked}
        <Icon name="check" size={14} />
      {/if}
    </span>
  </span>
  <span class="text">{text}</span>
</label>

<style>
  .checkbox {
    display: inline-flex;
    align-items: center;
    gap: var(--space-sm);
    min-height: var(--size-control-height-large);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    cursor: pointer;
  }

  .checkbox.disabled {
    color: var(--color-muted-text);
    cursor: default;
  }

  .box-area {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--size-control-height-large);
    height: var(--size-control-height-large);
    border-radius: var(--radius-control);
  }

  .checkbox:not(.disabled):hover .box-area {
    background: var(--color-background);
  }

  .box-area:has(input:focus-visible) {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  input {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    opacity: 0;
    cursor: inherit;
  }

  .box {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border: 2px solid var(--color-muted-text);
    border-radius: var(--radius-hairline);
    background: transparent;
    color: var(--color-on-primary);
    pointer-events: none;
  }

  .checkbox:not(.disabled):hover .box {
    border-color: var(--color-text);
  }

  .box.checked {
    border-color: var(--color-primary);
    background: var(--color-primary);
  }

  .text {
    line-height: 1.3;
  }
</style>
