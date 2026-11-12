<script lang="ts">
  import { onMount } from 'svelte';
  import logo from './assets/finglass-logo.svg';
  import logoDark from './assets/finglass-logo-dark.svg';
  import { getApi, type AppInfoDTO } from './lib/api';
  import {
    defaultLanguage,
    languages,
    translate,
    type Language,
    type MessageKey,
  } from './lib/i18n';

  let language = $state<Language>(defaultLanguage);
  let info = $state<AppInfoDTO | null>(null);
  let name = $state('');
  let greeting = $state('');
  let greetError = $state('');
  let tick = $state(0);
  let loadError = $state('');

  const t = (key: MessageKey): string => translate(language, key);

  onMount(() => {
    let stop: (() => void) | undefined;
    let disposed = false;

    void (async () => {
      try {
        const api = await getApi();
        if (disposed) {
          return;
        }
        info = await api.info();
        if (disposed) {
          return;
        }
        stop = api.onTick((payload) => {
          tick = payload.count;
        });
      } catch {
        if (!disposed) {
          loadError = t('loadError');
        }
      }
    })();

    return () => {
      disposed = true;
      stop?.();
    };
  });

  async function greet(): Promise<void> {
    greeting = '';
    greetError = '';

    const trimmed = name.trim();
    if (!trimmed) {
      greetError = t('errorEmptyName');
      return;
    }

    try {
      const api = await getApi();
      greeting = await api.greet(trimmed);
    } catch (error) {
      greetError = error instanceof Error ? error.message : String(error);
    }
  }
</script>

<main class="window">
  <header class="masthead">
    <h1 class="wordmark">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset={logoDark} />
        <img src={logo} alt="Finglass" width="280" height="64" />
      </picture>
    </h1>
    <p class="tagline">{t('appTagline')}</p>

    <div class="language" role="group" aria-label={t('languageLabel')}>
      {#each languages as code (code)}
        <button
          type="button"
          class:active={language === code}
          aria-pressed={language === code}
          onclick={() => (language = code)}
        >
          {code.toUpperCase()}
        </button>
      {/each}
    </div>
  </header>

  <section class="surface" aria-labelledby="info-heading">
    <h2 id="info-heading">{t('infoHeading')}</h2>

    {#if info}
      <dl class="info">
        <div>
          <dt>{t('nameLabel')}</dt>
          <dd>{info.name}</dd>
        </div>
        <div>
          <dt>{t('version')}</dt>
          <dd>{info.version}</dd>
        </div>
        <div>
          <dt>{t('goVersion')}</dt>
          <dd>{info.goVersion}</dd>
        </div>
        <div>
          <dt>{t('os')}</dt>
          <dd>{info.os}</dd>
        </div>
        <div>
          <dt>{t('arch')}</dt>
          <dd>{info.arch}</dd>
        </div>
      </dl>
    {:else if loadError}
      <p class="error" role="alert">{loadError}</p>
    {:else}
      <p class="muted">{t('loading')}</p>
    {/if}
  </section>

  <section class="surface" aria-labelledby="greet-heading">
    <h2 id="greet-heading">{t('greetingLabel')}</h2>

    <div class="greet-form">
      <label for="name">{t('nameLabel')}</label>
      <input id="name" type="text" autocomplete="off" bind:value={name} />
      <button type="button" class="primary" onclick={greet}>{t('greetButton')}</button>
    </div>

    {#if greeting}
      <p class="result" data-testid="greeting">
        <span>{t('greetingLabel')}:</span>
        {greeting}
      </p>
    {/if}

    {#if greetError}
      <p class="error" role="alert" data-testid="greet-error">{greetError}</p>
    {/if}
  </section>

  <footer class="tick-line">
    <span>{t('tickLabel')}:</span>
    <span data-testid="tick-count">{tick}</span>
  </footer>
</main>
