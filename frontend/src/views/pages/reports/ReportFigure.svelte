<script lang="ts">
  let {
    label,
    value,
    tone = 'text',
    compact = false,
  }: {
    label: string;
    value: string;
    tone?: 'text' | 'in' | 'out' | 'alert';
    compact?: boolean;
  } = $props();
</script>

<div class="figure">
  <span class="label">{label}</span>
  <span class="value" class:in={tone === 'in'} class:alert={tone === 'alert'} class:compact>
    {value}
  </span>
</div>

<style>
  .figure {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
    flex-direction: column;
    gap: 4px;
    padding: 0 16px;
  }

  .label {
    overflow: hidden;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .value {
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-amount-size);
    font-weight: var(--type-amount-weight);
    line-height: 1.15;
    white-space: pre-line;
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
</style>
