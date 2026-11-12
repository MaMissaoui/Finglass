<script lang="ts">
  let {
    text,
    positive = true,
  }: {
    text: string;
    positive?: boolean;
  } = $props();
</script>

<span class="badge" class:positive>{text}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 var(--space-sm);
    border-radius: var(--radius-badge);
    background: var(--color-background);
    color: var(--color-muted-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .badge.positive {
    color: var(--color-success);
  }
</style>
