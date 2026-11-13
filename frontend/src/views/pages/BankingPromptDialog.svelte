<script lang="ts">
  import type { PromptDTO } from '../../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
  import { Checkbox, OutlinedField, PrimaryButton, TextButton } from '../../lib/components';

  let {
    prompt,
    t,
    onAnswerPin,
    onCancelPin,
    onAnswerTan,
    onCancelTan,
    onAnswerChoice,
    onCancelChoice,
    onAnswerDecoupled,
  }: {
    prompt: PromptDTO;
    t: (key: string) => string;
    onAnswerPin: (id: number, pin: string, remember: boolean) => Promise<void>;
    onCancelPin: (id: number) => Promise<void>;
    onAnswerTan: (id: number, tan: string) => Promise<void>;
    onCancelTan: (id: number) => Promise<void>;
    onAnswerChoice: (id: number, choice: string) => Promise<void>;
    onCancelChoice: (id: number) => Promise<void>;
    onAnswerDecoupled: (id: number, confirmed: boolean) => Promise<void>;
  } = $props();

  // The secret lives only here while the prompt is open. Every path copies it
  // into a local, clears the field, and clears it again in finally.
  let pin = $state('');
  let tan = $state('');
  let remember = $state(false);
  let selected = $state(-1);

  function clearSecrets(): void {
    pin = '';
    tan = '';
    remember = false;
  }

  async function submitPin(): Promise<void> {
    if (pin === '') {
      return;
    }
    const value = pin;
    const rememberValue = remember;
    clearSecrets();
    try {
      await onAnswerPin(prompt.id, value, rememberValue);
    } finally {
      clearSecrets();
    }
  }

  async function cancelPin(): Promise<void> {
    clearSecrets();
    await onCancelPin(prompt.id);
  }

  async function submitTan(): Promise<void> {
    if (tan === '') {
      return;
    }
    const value = tan;
    clearSecrets();
    try {
      await onAnswerTan(prompt.id, value);
    } finally {
      clearSecrets();
    }
  }

  async function cancelTan(): Promise<void> {
    clearSecrets();
    await onCancelTan(prompt.id);
  }

  async function submitChoice(): Promise<void> {
    const choice = prompt.choices[selected];
    if (choice === undefined) {
      return;
    }
    selected = -1;
    await onAnswerChoice(prompt.id, choice.id);
  }

  async function cancelChoice(): Promise<void> {
    selected = -1;
    await onCancelChoice(prompt.id);
  }
</script>

<div class="scrim">
  <div
    class="prompt"
    role="dialog"
    aria-modal="true"
    aria-label={prompt.title}
    data-testid="banking-prompt"
  >
    <h2>{prompt.title}</h2>

    {#if prompt.text !== ''}
      <p class="body">{prompt.text}</p>
    {/if}

    {#if prompt.kind === 'pin'}
      <div class="fields">
        <OutlinedField
          label={prompt.label}
          bind:value={pin}
          secret
          onaccepted={() => void submitPin()}
        />

        {#if prompt.rememberVisible}
          <div class="remember">
            <Checkbox text={t('RememberPinForSyncs')} bind:checked={remember} />
            <p class="hint">{t('RememberPinExplanation')}</p>
          </div>
        {/if}
      </div>

      <div class="actions">
        <TextButton text={t('Cancel')} onclick={() => void cancelPin()} />
        <PrimaryButton text={t('Ok')} enabled={pin !== ''} onclick={() => void submitPin()} />
      </div>
    {:else if prompt.kind === 'choice'}
      <div class="choices" role="listbox" aria-label={prompt.title}>
        {#each prompt.choices as choice, index (choice.id)}
          <button
            type="button"
            role="option"
            class="choice"
            class:selected={selected === index}
            aria-selected={selected === index}
            onclick={() => (selected = index)}
          >
            {choice.label}
          </button>
        {/each}
      </div>

      <div class="actions">
        <TextButton text={t('Cancel')} onclick={() => void cancelChoice()} />
        <PrimaryButton text={t('Ok')} enabled={selected >= 0} onclick={() => void submitChoice()} />
      </div>
    {:else if prompt.kind === 'tan'}
      <div class="fields">
        <p class="body">{prompt.challenge}</p>

        {#if prompt.imageUrl !== ''}
          <img class="challenge" src={prompt.imageUrl} alt={t('TanChallenge')} />
        {/if}

        {#if prompt.flickerNote !== ''}
          <p class="hint">{prompt.flickerNote}</p>
        {/if}

        <OutlinedField
          label={prompt.label}
          bind:value={tan}
          secret
          onaccepted={() => void submitTan()}
        />
      </div>

      <div class="actions">
        <TextButton text={t('Cancel')} onclick={() => void cancelTan()} />
        <PrimaryButton text={t('Ok')} enabled={tan !== ''} onclick={() => void submitTan()} />
      </div>
    {:else if prompt.kind === 'decoupled'}
      <div class="actions">
        <TextButton text={t('Cancel')} onclick={() => void onAnswerDecoupled(prompt.id, false)} />
        <PrimaryButton
          text={t('IConfirmedIt')}
          onclick={() => void onAnswerDecoupled(prompt.id, true)}
        />
      </div>
    {/if}
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
    padding: 16px;
    overflow-y: auto;
    background: var(--color-scrim);
  }

  .prompt {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 480px;
    max-width: calc(100% - 32px);
    max-height: calc(100% - 32px);
    padding: 24px;
    overflow-y: auto;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  .prompt > * {
    flex: none;
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .body {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .remember {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .choices {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 320px;
    overflow-y: auto;
  }

  .choice {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: 34px;
    padding: 4px 10px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: left;
    cursor: pointer;
  }

  .choice:hover {
    background: var(--color-background);
  }

  .choice.selected {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .choice:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .challenge {
    display: block;
    align-self: center;
    width: 220px;
    height: 220px;
    object-fit: contain;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
