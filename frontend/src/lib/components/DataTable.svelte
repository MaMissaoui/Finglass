<script module lang="ts">
  export interface DataColumn {
    key: string;
    label: string;
    align?: 'left' | 'right';
    numeric?: boolean;
    width?: string;
  }

  export interface DataRow {
    id: string;
    cells: Record<string, string>;
  }
</script>

<script lang="ts">
  let {
    columns,
    rows,
    selectedId = undefined,
    onrowselected,
  }: {
    columns: DataColumn[];
    rows: DataRow[];
    selectedId?: string;
    onrowselected?: (id: string) => void;
  } = $props();

  function select(id: string): void {
    onrowselected?.(id);
  }

  function onkeydown(event: KeyboardEvent, id: string): void {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      select(id);
    }
  }
</script>

<div class="wrap">
  <table>
    <thead>
      <tr>
        {#each columns as column (column.key)}
          <th
            scope="col"
            class:right={column.align === 'right'}
            class:numeric={column.numeric}
            style:width={column.width ?? undefined}
          >
            {column.label}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each rows as row (row.id)}
        <tr
          tabindex="0"
          class:selected={row.id === selectedId}
          aria-selected={row.id === selectedId}
          onclick={() => select(row.id)}
          onkeydown={(event) => onkeydown(event, row.id)}
        >
          {#each columns as column (column.key)}
            <td class:right={column.align === 'right'} class:numeric={column.numeric}>
              {row.cells[column.key] ?? ''}
            </td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .wrap {
    width: 100%;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
    overflow: hidden;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-family: var(--font-sans);
  }

  th {
    height: var(--size-control-height-large);
    padding: 0 12px;
    border-bottom: 1px solid var(--color-rule);
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    text-align: left;
    white-space: nowrap;
  }

  td {
    height: 48px;
    padding: 0 12px;
    border-bottom: 1px solid var(--color-rule);
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
  }

  tbody tr:last-child td {
    border-bottom: 0;
  }

  tbody tr {
    cursor: pointer;
  }

  tbody tr:hover {
    background: var(--color-background);
  }

  tbody tr.selected {
    background: var(--color-selected-fill);
  }

  tbody tr.selected td:first-child {
    box-shadow: inset 3px 0 0 var(--color-primary);
  }

  tbody tr:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .right {
    text-align: right;
  }

  .numeric {
    font-variant-numeric: tabular-nums;
  }
</style>
