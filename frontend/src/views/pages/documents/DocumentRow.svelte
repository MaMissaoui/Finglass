<script lang="ts">
  import type { DocumentRowDTO } from '../../../lib/api';
  import { Badge, Icon } from '../../../lib/components';

  let {
    row,
    t,
    onselect,
  }: {
    row: DocumentRowDTO;
    t: (key: string) => string;
    onselect: (id: number) => void;
  } = $props();
</script>

<div class="row-block" data-testid={`document-row-${row.id}`}>
  <button
    type="button"
    class="row"
    class:selected={row.selected}
    aria-pressed={row.selected}
    onclick={() => onselect(row.id)}
  >
    {#if row.selected}
      <span class="accent" aria-hidden="true"></span>
    {/if}
    <span class="file-icon" class:pdf={row.isPdf}>
      <Icon name={row.isPdf ? 'documents' : 'image'} size={20} />
    </span>
    <span class="text">
      <span class="name">{row.name}</span>
      <span class="meta">{row.meta}</span>
    </span>
    <Badge text={row.hasOcr ? t('TextExtracted') : t('NoText')} positive={row.hasOcr} />
  </button>
  <div class="divider" role="none"></div>
</div>

<style>
  .row-block {
    display: flex;
    flex: none;
    flex-direction: column;
    height: 65px;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 64px;
    padding: 16px;
    border: 0;
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    text-align: left;
    cursor: pointer;
  }

  .row:hover {
    background: var(--color-background);
  }

  .row.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .accent {
    position: absolute;
    top: 0;
    left: 0;
    width: 3px;
    height: 100%;
    background: var(--color-primary);
  }

  .file-icon {
    display: inline-flex;
    flex: none;
    color: var(--color-document-image);
  }

  .file-icon.pdf {
    color: var(--color-document-pdf);
  }

  .text {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .name,
  .meta {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .name {
    color: inherit;
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .meta {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .row.selected .meta {
    color: var(--color-on-selected);
  }

  .divider {
    flex: none;
    height: 1px;
    background: var(--color-divider);
  }
</style>
