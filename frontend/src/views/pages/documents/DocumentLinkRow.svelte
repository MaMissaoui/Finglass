<script lang="ts">
  import type { DocumentLinkRowDTO } from '../../../lib/api';
  import { Badge, TextButton } from '../../../lib/components';

  let {
    row,
    t,
    onlink,
  }: {
    row: DocumentLinkRowDTO;
    t: (key: string) => string;
    onlink: (id: number) => void;
  } = $props();
</script>

<div class="row">
  <span class="text">
    <span class="title">{row.title}</span>
    <span class="meta">{row.meta}</span>
  </span>
  {#if row.badge !== ''}
    <Badge text={row.badge} positive />
  {/if}
  <TextButton text={t('Link')} onclick={() => onlink(row.id)} />
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 44px;
    min-width: 0;
  }

  .text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .title,
  .meta {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .title {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .meta {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }
</style>
