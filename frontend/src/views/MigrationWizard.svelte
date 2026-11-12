<script lang="ts">
  import { untrack } from 'svelte';
  import type { Api, ShellDTO } from '../lib/api';
  import { Checkbox, OutlinedField, PrimaryButton, TextButton } from '../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../lib/i18n';

  let {
    dto,
    api,
    onchange,
  }: {
    dto: ShellDTO;
    api: Api;
    onchange: (dto: ShellDTO) => void;
  } = $props();

  let acknowledged = $state(false);
  let findeskPassphrase = $state('');
  let newPassphrase = $state('');
  let repeat = $state('');
  let samePassphrase = $state(untrack(() => dto.migrationSamePassphrase));
  let showPassphrase = $state(false);
  let pending = $state(false);
  let localError = $state('');
  let changeRequest = 0;

  const language = $derived<Language>(languageFromCode(dto.language));
  const busy = $derived(dto.busy || pending);
  const errorText = $derived(localError || dto.errorText);
  const showNewFields = $derived(dto.migrationNewPassphraseRequired || !samePassphrase);

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function clearSecrets(): void {
    findeskPassphrase = '';
    newPassphrase = '';
    repeat = '';
  }

  function passphraseChanged(value: string): void {
    const request = ++changeRequest;
    void (async () => {
      try {
        const next = await api.migrationPassphraseChanged(value);
        if (request === changeRequest) {
          onchange(next);
        }
      } catch {
        // The wizard keeps the field local; a transport error shows nothing new.
      }
    })();
  }

  async function run(action: () => Promise<ShellDTO>): Promise<void> {
    if (busy) {
      return;
    }
    localError = '';
    pending = true;
    try {
      onchange(await action());
    } catch {
      localError = t('ErrorUnexpected');
    } finally {
      pending = false;
    }
  }

  async function submitProfile(): Promise<void> {
    if (busy) {
      return;
    }
    const findeskValue = findeskPassphrase;
    const newValue = newPassphrase;
    const repeatValue = repeat;
    const sameValue = samePassphrase;
    clearSecrets();
    localError = '';
    pending = true;
    try {
      onchange(await api.migrationSubmit(findeskValue, newValue, repeatValue, sameValue));
    } catch {
      localError = t('ErrorUnexpected');
    } finally {
      clearSecrets();
      pending = false;
    }
  }

  function startMigration(): void {
    void run(() => api.migrationMigrate(acknowledged));
  }

  function startEmpty(): void {
    void run(() => api.migrationStartEmpty());
  }

  function skip(): void {
    clearSecrets();
    void run(() => api.migrationSkip());
  }

  function continueMigration(): void {
    clearSecrets();
    void run(() => api.migrationContinue());
  }
</script>

{#if dto.migrationStep === 'welcome'}
  <div class="step">
    <h1>{t('MigrationWelcomeTitle')}</h1>
    <p>{t('MigrationWelcomeText')}</p>
    <p class="label">{t('MigrationProfilesLabel')}</p>
    {#each dto.migrationProfileNames as name (name)}
      <p class="muted">• {name}</p>
    {/each}
    <p class="small">{t('MigrationCopiedText')}</p>
    <p class="caution">{t('MigrationQuitHint')}</p>
    <Checkbox text={t('PassphraseNoRecovery')} bind:checked={acknowledged} enabled={!busy} />
    {#if errorText !== ''}
      <p class="error" role="alert" data-testid="migration-error">{errorText}</p>
    {/if}
    <div class="actions">
      <PrimaryButton
        text={t('MigrationStart')}
        enabled={!busy && acknowledged}
        onclick={startMigration}
      />
      <TextButton text={t('MigrationStartEmpty')} enabled={!busy} onclick={startEmpty} />
    </div>
  </div>
{:else if dto.migrationStep === 'profile'}
  <div class="step">
    <h1>{dto.migrationProfileHeading}</h1>
    <p class="small">{t('MigrationCopiedText')}</p>

    <OutlinedField
      label={t('MigrationFindeskPassphrase')}
      bind:value={findeskPassphrase}
      enabled={!busy}
      secret={!showPassphrase}
      oninput={passphraseChanged}
      onaccepted={() => void submitProfile()}
    />

    <Checkbox text={t('MigrationSamePassphrase')} bind:checked={samePassphrase} enabled={!busy} />

    {#if showNewFields}
      <div class="new-fields">
        <OutlinedField
          label={t('NewPassphrase')}
          bind:value={newPassphrase}
          enabled={!busy}
          secret={!showPassphrase}
          onaccepted={() => void submitProfile()}
        />
        <OutlinedField
          label={t('RepeatPassphrase')}
          bind:value={repeat}
          enabled={!busy}
          secret={!showPassphrase}
          onaccepted={() => void submitProfile()}
        />
      </div>
    {/if}

    <Checkbox text={t('ShowPassphrase')} bind:checked={showPassphrase} enabled={!busy} />

    {#if dto.passphraseHint !== ''}
      <p class="small">{dto.passphraseHint}</p>
    {/if}

    {#if errorText !== ''}
      <p class="error" role="alert" data-testid="migration-error">{errorText}</p>
    {/if}

    <div class="actions">
      <PrimaryButton
        text={t('MigrationMigrateProfile')}
        enabled={!busy}
        onclick={() => void submitProfile()}
      />
      <TextButton text={t('MigrationSkip')} enabled={!busy} onclick={skip} />
      {#if busy}
        <span class="busy" role="status">
          <span class="spinner" aria-hidden="true"></span>
          <span>{t('MigrationBusy')}</span>
        </span>
      {/if}
    </div>
  </div>
{:else if dto.migrationStep === 'summary'}
  <div class="step">
    <h1>{t('MigrationSummaryTitle')}</h1>
    <p>{t('MigrationSummaryText')}</p>
    {#each dto.migrationSummaryLines as line (line)}
      <p class="muted">{line}</p>
    {/each}
    {#if errorText !== ''}
      <p class="error" role="alert" data-testid="migration-error">{errorText}</p>
    {/if}
    <div class="actions">
      <PrimaryButton text={t('MigrationContinue')} enabled={!busy} onclick={continueMigration} />
      {#if busy}
        <span class="busy" role="status">
          <span class="spinner" aria-hidden="true"></span>
          <span>{t('MigrationBusy')}</span>
        </span>
      {/if}
    </div>
  </div>
{/if}

<style>
  .step {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  h1 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  p {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    line-height: 1.3;
  }

  .label {
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .muted {
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
  }

  .small {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .caution {
    color: var(--color-caution);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .error {
    color: var(--color-error);
  }

  .new-fields {
    display: flex;
    flex-direction: column;
    gap: 6px;
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

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
