<script lang="ts">
  import {
    Checkbox,
    OutlinedField,
    PrimaryButton,
    SelectPill,
    TextButton,
  } from '../../../lib/components';

  let {
    title,
    pattern,
    categories,
    categoryIndex,
    priority,
    autoApply,
    fieldOptions,
    fieldIndex,
    directionOptions,
    directionIndex,
    taxOptions,
    taxIndex,
    errorText,
    labels,
    onchanged,
    onclose,
    onsave,
  }: {
    title: string;
    pattern: string;
    categories: string[];
    categoryIndex: number;
    priority: string;
    autoApply: boolean;
    fieldOptions: string[];
    fieldIndex: number;
    directionOptions: string[];
    directionIndex: number;
    taxOptions: string[];
    taxIndex: number;
    errorText: string;
    labels: {
      pattern: string;
      category: string;
      searchIn: string;
      appliesTo: string;
      tax: string;
      priority: string;
      autoApply: string;
      cancel: string;
      save: string;
    };
    onchanged: (pattern: string, priority: string) => void;
    onclose: () => void;
    onsave: (
      pattern: string,
      categoryIndex: number,
      priority: string,
      autoApply: boolean,
      fieldIndex: number,
      flow: number,
      taxIndex: number,
    ) => void;
  } = $props();

  // The parent renders this dialog only while it is open, so these are the
  // fields as they were when it opened.
  // svelte-ignore state_referenced_locally
  let patternText = $state(pattern);
  // svelte-ignore state_referenced_locally
  let priorityText = $state(priority);
  // svelte-ignore state_referenced_locally
  let auto = $state(autoApply);
  // svelte-ignore state_referenced_locally
  let category = $state(categoryIndex);
  // svelte-ignore state_referenced_locally
  let field = $state(fieldIndex);
  // svelte-ignore state_referenced_locally
  let direction = $state(directionIndex);
  // svelte-ignore state_referenced_locally
  let tax = $state(taxIndex);

  function patternChanged(value: string): void {
    patternText = value;
    onchanged(patternText, priorityText);
  }

  function priorityChanged(value: string): void {
    priorityText = value;
    onchanged(patternText, priorityText);
  }

  const categoryValue = $derived(categories[category] ?? '');
  const fieldValue = $derived(fieldOptions[field] ?? '');
  const directionValue = $derived(directionOptions[direction] ?? '');
  const taxValue = $derived(taxOptions[tax] ?? '');
</script>

<div class="scrim" data-testid="rule-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>

    <OutlinedField label={labels.pattern} bind:value={patternText} oninput={patternChanged} />

    <div class="pill-row">
      <SelectPill
        labelPrefix={`${labels.category}: `}
        value={categoryValue}
        options={categories}
        onchosen={(index) => (category = index)}
      />
    </div>

    <div class="pill-row">
      <SelectPill
        labelPrefix={`${labels.searchIn}: `}
        value={fieldValue}
        options={fieldOptions}
        onchosen={(index) => {
          field = index;
          onchanged(patternText, priorityText);
        }}
      />
    </div>

    <div class="pill-row">
      <SelectPill
        labelPrefix={`${labels.appliesTo}: `}
        value={directionValue}
        options={directionOptions}
        onchosen={(index) => (direction = index)}
      />
    </div>

    <div class="pill-row">
      <SelectPill
        labelPrefix={`${labels.tax}: `}
        value={taxValue}
        options={taxOptions}
        onchosen={(index) => (tax = index)}
      />
    </div>

    <OutlinedField label={labels.priority} bind:value={priorityText} oninput={priorityChanged} />

    <Checkbox text={labels.autoApply} bind:checked={auto} />

    {#if errorText !== ''}
      <p class="error" role="alert">{errorText}</p>
    {/if}

    <div class="actions">
      <TextButton text={labels.cancel} onclick={onclose} />
      <PrimaryButton
        text={labels.save}
        onclick={() => onsave(patternText, category, priorityText, auto, field, direction, tax)}
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

  .pill-row {
    display: flex;
    align-items: flex-start;
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
