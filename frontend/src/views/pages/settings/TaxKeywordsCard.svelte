<script lang="ts">
  import { Icon, PrimaryButton, TextButton } from '../../../lib/components';

  let {
    title,
    keywords,
    count,
    saveText,
    resetText,
    onsave,
    onreset,
  }: {
    title: string;
    keywords: string;
    count: string;
    saveText: string;
    resetText: string;
    onsave: (text: string) => void;
    onreset: () => void;
  } = $props();

  // The parent keys this card by the DTO's saved text, so a save or a reset
  // starts a fresh draft and typing is never overwritten.
  // svelte-ignore state_referenced_locally
  let draft = $state(keywords);
</script>

<div class="card" data-testid="tax-keywords-card">
  <div class="title">
    <span class="title-icon"><Icon name="transactions" size={20} /></span>
    <h3>{title}</h3>
  </div>
  <p class="count">{count}</p>
  <textarea aria-label={title} bind:value={draft} spellcheck="false"></textarea>
  <div class="actions">
    <PrimaryButton text={saveText} onclick={() => onsave(draft)} />
    <TextButton text={resetText} onclick={onreset} />
  </div>
</div>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .title {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .title-icon {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  h3 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    line-height: 1.3;
  }

  .count {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  textarea {
    height: 150px;
    padding: 12px 14px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-field);
    outline: none;
    resize: none;
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }

  textarea:focus-visible {
    border-color: var(--color-primary);
    box-shadow: inset 0 0 0 1px var(--color-primary);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }
</style>
