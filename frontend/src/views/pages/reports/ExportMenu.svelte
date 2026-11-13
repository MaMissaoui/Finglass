<script lang="ts">
  type Action = 'taxXLSX' | 'taxPDF' | 'print' | 'reportPDF' | 'csv';

  type Item = {
    key: string;
    action: Action;
    tax: boolean;
  };

  let {
    tab,
    canReport,
    canTax,
    busy,
    t,
    onpick,
  }: {
    tab: string;
    canReport: boolean;
    canTax: boolean;
    busy: boolean;
    t: (key: string) => string;
    onpick: (action: Action) => void;
  } = $props();

  const items: Item[] = [
    { key: 'ExportExcel', action: 'taxXLSX', tax: true },
    { key: 'ExportPdf', action: 'taxPDF', tax: true },
    { key: 'Print', action: 'print', tax: true },
    { key: 'ExportReport', action: 'reportPDF', tax: false },
    { key: 'ExportCsv', action: 'csv', tax: false },
  ];

  const shown = $derived(items.filter((item) => tab === 'tax' || !item.tax));

  function enabled(item: Item): boolean {
    if (busy) {
      return false;
    }
    return item.tax ? canTax : canReport;
  }
</script>

<div class="menu" role="menu" aria-label={t('Export')} data-testid="reports-export-menu">
  {#each shown as item, index (item.key)}
    {#if tab === 'tax' && index === 3}
      <div class="divider" role="none"></div>
    {/if}
    <button
      type="button"
      role="menuitem"
      class="item"
      disabled={!enabled(item)}
      onclick={() => onpick(item.action)}
    >
      {t(item.key)}
    </button>
  {/each}
</div>

<style>
  .menu {
    position: absolute;
    z-index: 31;
    top: calc(100% + 4px);
    right: 0;
    display: flex;
    width: 220px;
    flex-direction: column;
    padding: 4px;
    overflow: hidden;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .divider {
    height: 1px;
    background: var(--color-divider);
  }

  .item {
    display: flex;
    align-items: center;
    flex: none;
    height: 36px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }

  .item:hover:not(:disabled) {
    background: var(--color-background);
  }

  .item:disabled {
    color: var(--color-muted-text);
    cursor: default;
  }

  .item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }
</style>
