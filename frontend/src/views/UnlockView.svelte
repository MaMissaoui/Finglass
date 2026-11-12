<script lang="ts">
  import { onMount, tick } from 'svelte';
  import icon from '../assets/finglass-icon.png';
  import type { Api, ShellDTO } from '../lib/api';
  import { Checkbox, Icon, OutlinedField, PrimaryButton, TextButton } from '../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../lib/i18n';
  import MigrationWizard from './MigrationWizard.svelte';

  let {
    dto,
    api,
    onchange,
  }: {
    dto: ShellDTO;
    api: Api;
    onchange: (dto: ShellDTO) => void;
  } = $props();

  type FieldHandle = { focusInput: () => void };

  let passphrase = $state('');
  let repeat = $state('');
  let current = $state('');
  let showPassphrase = $state(false);
  let acknowledged = $state(false);
  let pending = $state(false);
  let localError = $state('');
  let passphraseField = $state<FieldHandle>();
  let currentField = $state<FieldHandle>();

  const language = $derived<Language>(languageFromCode(dto.language));
  const busy = $derived(dto.busy || pending);
  const errorText = $derived(localError || dto.errorText);
  const showFields = $derived(!dto.startupError && dto.mode !== 'migrate');
  const actionLabel = $derived(
    dto.mode === 'unlock'
      ? t('Unlock')
      : dto.mode === 'change'
        ? t('ChangeAndOpen')
        : t('EncryptAndOpen'),
  );
  const busyLabel = $derived(dto.mode === 'unlock' ? t('Unlocking') : t('Encrypting'));

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function heading(): string {
    if (dto.startupError) {
      return t('UnlockWindowTitle');
    }
    if (dto.mode === 'create') {
      return t('CreatePassphraseHeading');
    }
    if (dto.mode === 'change') {
      return t('ChangePassphrase');
    }
    return t('UnlockHeading');
  }

  function body(): string {
    if (dto.startupError) {
      return '';
    }
    if (dto.mode === 'create') {
      return t('CreatePassphraseText');
    }
    if (dto.mode === 'change') {
      return t('ChangePassphraseText');
    }
    return t('UnlockText');
  }

  function clearSecrets(): void {
    passphrase = '';
    repeat = '';
    current = '';
  }

  async function submit(): Promise<void> {
    if (busy) {
      return;
    }
    const mode = dto.mode;
    const passphraseValue = passphrase;
    const repeatValue = repeat;
    const currentValue = current;
    const acknowledgedValue = acknowledged;
    clearSecrets();
    localError = '';
    pending = true;
    try {
      let next: ShellDTO;
      if (mode === 'unlock') {
        next = await api.unlock(passphraseValue);
      } else if (mode === 'change') {
        next = await api.change(currentValue, passphraseValue, repeatValue, acknowledgedValue);
      } else {
        next = await api.create(passphraseValue, repeatValue, acknowledgedValue);
      }
      onchange(next);
    } catch {
      localError = t('ErrorUnexpected');
    } finally {
      clearSecrets();
      pending = false;
    }
  }

  async function showChange(change: boolean): Promise<void> {
    if (busy) {
      return;
    }
    clearSecrets();
    localError = '';
    pending = true;
    try {
      onchange(await api.showChange(change));
      await tick();
      if (change) {
        currentField?.focusInput();
      } else {
        passphraseField?.focusInput();
      }
    } catch {
      localError = t('ErrorUnexpected');
    } finally {
      pending = false;
    }
  }

  onMount(() => {
    if (dto.mode === 'change') {
      currentField?.focusInput();
    } else if (!dto.startupError && dto.mode !== 'migrate') {
      passphraseField?.focusInput();
    }
  });
</script>

{#if dto.mode === 'migrate'}
  <main class="unlock">
    <div class="panel">
      <img class="logo" src={icon} alt={t('AppLogo')} width="56" height="56" />
      <MigrationWizard {dto} {api} {onchange} />
    </div>
  </main>
{:else}
  <main class="unlock">
    <div class="panel">
      <img class="logo" src={icon} alt={t('AppLogo')} width="56" height="56" />

      <h1 class="heading">{heading()}</h1>

      {#if dto.profileName !== ''}
        <div class="chip">
          <Icon name="person" size={16} />
          <span>{dto.profileName}</span>
        </div>
      {/if}

      {#if body() !== ''}
        <p class="body">{body()}</p>
      {:else if dto.startupError}
        <div class="body-gap" aria-hidden="true"></div>
      {/if}

      {#if dto.restored}
        <p class="caution">{t('RestoredEncryptedHint')}</p>
      {/if}

      {#if dto.leftoverLines.length > 0}
        <div class="leftovers">
          <h2>{t('UnencryptedLeftoversTitle')}</h2>
          <p class="small">{t('UnencryptedLeftoversText')}</p>
          {#each dto.leftoverLines as line (line)}
            <p class="leftover-line">{line}</p>
          {/each}
        </div>
      {/if}

      {#if showFields}
        <div class="fields">
          {#if dto.mode === 'change'}
            <OutlinedField
              bind:this={currentField}
              label={t('CurrentPassphrase')}
              bind:value={current}
              enabled={!busy}
              secret={!showPassphrase}
              error={errorText !== ''}
              onaccepted={() => void submit()}
            />
          {/if}

          <OutlinedField
            bind:this={passphraseField}
            label={dto.mode === 'unlock' ? t('Passphrase') : t('NewPassphrase')}
            bind:value={passphrase}
            enabled={!busy}
            secret={!showPassphrase}
            error={errorText !== ''}
            onaccepted={() => void submit()}
          />

          {#if dto.mode !== 'unlock'}
            <OutlinedField
              label={t('RepeatPassphrase')}
              bind:value={repeat}
              enabled={!busy}
              secret={!showPassphrase}
              error={errorText !== ''}
              onaccepted={() => void submit()}
            />
          {/if}

          <Checkbox text={t('ShowPassphrase')} bind:checked={showPassphrase} enabled={!busy} />

          {#if dto.mode !== 'unlock' && dto.passphraseHint !== ''}
            <p class="small">{dto.passphraseHint}</p>
          {/if}

          {#if dto.mode === 'create'}
            <p class="small">{t('DocumentsNotEncrypted')}</p>
          {/if}

          {#if dto.mode !== 'unlock'}
            <Checkbox
              text={t('PassphraseNoRecovery')}
              bind:checked={acknowledged}
              enabled={!busy}
            />
          {/if}

          {#if errorText !== ''}
            <p class="error" role="alert" data-testid="unlock-error">{errorText}</p>
          {/if}

          <div class="actions">
            <PrimaryButton text={actionLabel} enabled={!busy} onclick={() => void submit()} />
            {#if busy}
              <span class="busy" role="status">
                <span class="spinner" aria-hidden="true"></span>
                <span>{busyLabel}</span>
              </span>
            {/if}
          </div>

          {#if dto.mode === 'unlock' && dto.allowChange}
            <div class="links">
              <TextButton
                text={t('ChangePassphrase')}
                enabled={!busy}
                onclick={() => void showChange(true)}
              />
            </div>
          {/if}

          {#if dto.mode === 'change'}
            <div class="links">
              <TextButton text={t('Back')} enabled={!busy} onclick={() => void showChange(false)} />
            </div>
          {/if}
        </div>
      {/if}

      {#if dto.startupError && errorText !== ''}
        <p class="error" role="alert" data-testid="unlock-error">{errorText}</p>
      {/if}
    </div>
  </main>
{/if}

<style>
  .unlock {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    padding: var(--space-xl);
    background: var(--color-background);
    overflow-y: auto;
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: 440px;
    max-width: 100%;
  }

  .logo {
    display: block;
    width: 56px;
    height: 56px;
    object-fit: contain;
  }

  .heading {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-headline-size);
    font-weight: var(--type-headline-weight);
    line-height: 1.25;
  }

  .chip {
    display: inline-flex;
    align-self: flex-start;
    align-items: center;
    gap: 6px;
    height: 28px;
    padding: 0 12px 0 10px;
    border-radius: 14px;
    background: var(--color-selected);
    color: var(--color-on-selected);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .body {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.3;
  }

  .body-gap {
    height: calc(var(--type-body-size) * 1.3);
  }

  .caution {
    margin: 0;
    color: var(--color-caution);
    font-size: var(--type-body-size);
    line-height: 1.3;
  }

  .leftovers {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .leftovers h2 {
    margin: 0;
    color: var(--color-caution);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    line-height: 1.3;
  }

  .small {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.3;
  }

  .leftover-line {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.3;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .error {
    margin: 0;
    color: var(--color-error);
    font-size: var(--type-body-size);
    line-height: 1.3;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .busy {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
  }

  .spinner {
    display: inline-block;
    width: 20px;
    height: 20px;
    border: 2px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  .links {
    display: flex;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
