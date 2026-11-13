<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import type { IconName } from '../../../lib/icons';

  let {
    title,
    value,
    subtitle = '',
    icon,
    accent = 'muted',
    tone = 'text',
    interactive = false,
    compact = false,
    onclick,
    children,
  }: {
    title: string;
    value: string;
    subtitle?: string;
    icon: IconName;
    accent?: 'muted' | 'caution';
    tone?: 'text' | 'in' | 'out' | 'alert' | 'success';
    interactive?: boolean;
    compact?: boolean;
    onclick?: () => void;
    children?: Snippet;
  } = $props();

  function activate(): void {
    if (interactive) {
      onclick?.();
    }
  }

  function onkeydown(event: KeyboardEvent): void {
    if (interactive && (event.key === 'Enter' || event.key === ' ')) {
      event.preventDefault();
      activate();
    }
  }
</script>

{#snippet content()}
  <div class="head">
    <span class="title">{title}</span>
    <span class="icon" class:caution={accent === 'caution'}>
      <Icon name={icon} size={24} />
    </span>
  </div>
  <span
    class="value"
    class:compact
    class:in={tone === 'in'}
    class:alert={tone === 'alert'}
    class:success={tone === 'success'}>{value}</span
  >
  {#if subtitle !== ''}
    <span class="subtitle">{subtitle}</span>
  {/if}
  {#if children}
    {@render children()}
  {/if}
{/snippet}

{#if interactive}
  <button type="button" class="stat interactive" class:compact onclick={activate} {onkeydown}>
    {@render content()}
  </button>
{:else}
  <div class="stat" class:compact>
    {@render content()}
  </div>
{/if}

<style>
  .stat {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
    min-height: 104px;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    margin: 0;
    padding: 16px 16px 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    text-align: left;
  }

  .stat.compact {
    padding: 12px 12px 6px;
  }

  .stat.interactive {
    cursor: pointer;
  }

  .stat.interactive:hover {
    background: var(--color-background);
  }

  .stat.interactive:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .head {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 8px;
  }

  .title {
    flex: 1;
    min-width: 0;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    line-height: 1.3;
    overflow-wrap: anywhere;
  }

  .icon {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  .icon.caution {
    color: var(--color-caution);
  }

  .value {
    overflow: hidden;
    max-width: 100%;
    color: var(--color-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
    line-height: 1;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .value.compact {
    font-size: var(--type-title-large-size);
  }

  .value.in {
    color: var(--color-money-in);
  }

  .value.alert {
    color: var(--color-error);
  }

  .value.success {
    color: var(--color-success);
  }

  .subtitle {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 16px;
  }
</style>
