<script lang="ts">
  import type { SettingsColourSwatchDTO } from '../../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { Checkbox, OutlinedField, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    name,
    colour,
    budget,
    taxRelevant,
    transfer,
    errorText,
    swatches,
    labels,
    onchanged,
    onclose,
    onsave,
  }: {
    title: string;
    name: string;
    colour: string;
    budget: string;
    taxRelevant: boolean;
    transfer: boolean;
    errorText: string;
    swatches: SettingsColourSwatchDTO[];
    labels: {
      name: string;
      budget: string;
      taxHint: string;
      transfer: string;
      transferHint: string;
      cancel: string;
      save: string;
    };
    onchanged: (name: string, colour: string) => void;
    onclose: () => void;
    onsave: (
      name: string,
      colour: string,
      budget: string,
      taxRelevant: boolean,
      transfer: boolean,
    ) => void;
  } = $props();

  // The parent renders this dialog only while it is open, so these are the
  // fields as they were when it opened.
  // svelte-ignore state_referenced_locally
  let nameText = $state(name);
  // svelte-ignore state_referenced_locally
  let colourValue = $state(colour);
  // svelte-ignore state_referenced_locally
  let budgetText = $state(budget);
  // svelte-ignore state_referenced_locally
  let tax = $state(taxRelevant);
  // svelte-ignore state_referenced_locally
  let transfers = $state(transfer);

  function nameChanged(value: string): void {
    nameText = value;
    onchanged(nameText, colourValue);
  }

  function budgetChanged(value: string): void {
    budgetText = value;
    onchanged(nameText, colourValue);
  }

  function pick(colourHex: string): void {
    colourValue = colourHex;
    onchanged(nameText, colourValue);
  }
</script>

<div class="scrim" data-testid="category-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <OutlinedField label={labels.name} bind:value={nameText} oninput={nameChanged} />

    <div class="swatches" role="group" aria-label={labels.name}>
      {#each swatches as swatch (swatch.hex)}
        <button
          type="button"
          class="swatch"
          class:selected={swatch.hex.toUpperCase() === colourValue.toUpperCase()}
          style:background={swatch.colour}
          aria-label={swatch.hex}
          aria-pressed={swatch.hex.toUpperCase() === colourValue.toUpperCase()}
          onclick={() => pick(swatch.hex)}
        ></button>
      {/each}
    </div>

    <OutlinedField label={labels.budget} bind:value={budgetText} oninput={budgetChanged} />

    <Checkbox text={labels.taxHint} bind:checked={tax} />
    <div class="transfer">
      <Checkbox text={labels.transfer} bind:checked={transfers} />
      <p class="hint">{labels.transferHint}</p>
    </div>

    {#if errorText !== ''}
      <p class="error" role="alert">{errorText}</p>
    {/if}

    <div class="actions">
      <TextButton text={labels.cancel} onclick={onclose} />
      <PrimaryButton
        text={labels.save}
        onclick={() => onsave(nameText, colourValue, budgetText, tax, transfers)}
      />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(500px, 100%);
    max-height: calc(100vh - 48px);
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow-y: auto;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .swatches {
    display: grid;
    grid-template-columns: repeat(12, 26px);
    gap: 6px;
  }

  .swatch {
    width: 26px;
    height: 26px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-swatch);
    cursor: pointer;
  }

  .swatch.selected {
    box-shadow: inset 0 0 0 3px var(--color-text);
  }

  .swatch:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .transfer {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
