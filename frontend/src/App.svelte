<script lang="ts">
  import { onMount } from 'svelte';
  import type { Api, ShellDTO } from './lib/api';
  import { getApi } from './lib/api';
  import { defaultLanguage, translate } from './lib/i18n';
  import { applyTheme, type ThemeMode } from './lib/theme';
  import MainShell from './views/MainShell.svelte';
  import UnlockView from './views/UnlockView.svelte';

  let api = $state<Api | null>(null);
  let dto = $state<ShellDTO | null>(null);
  let loadError = $state('');

  function themeMode(mode: string): ThemeMode {
    if (mode === 'light' || mode === 'dark') {
      return mode;
    }
    return 'system';
  }

  function pageSearchField(): HTMLElement | null {
    return (
      document.querySelector<HTMLElement>('[data-page-search] input') ??
      document.querySelector<HTMLElement>('[data-page-search]')
    );
  }

  function focusPageSearch(): void {
    pageSearchField()?.focus();
  }

  onMount(() => {
    let disposed = false;
    let stopShell: (() => void) | undefined;
    let stopMenu: (() => void) | undefined;

    void (async () => {
      try {
        const loaded = await getApi();
        if (disposed) {
          return;
        }
        api = loaded;
        dto = await loaded.state();
        if (disposed) {
          return;
        }
        stopShell = loaded.onShellChanged((next) => {
          dto = next;
        });
        stopMenu = loaded.onMenuAction((action) => {
          if (action === 'find') {
            focusPageSearch();
          }
        });
      } catch {
        if (!disposed) {
          loadError = translate(defaultLanguage, 'ErrorUnexpected');
        }
      }
    })();

    const onKeydown = (event: KeyboardEvent) => {
      if (!dto?.shellOpen || event.key.toLowerCase() !== 'f' || !(event.metaKey || event.ctrlKey)) {
        return;
      }
      if (pageSearchField()) {
        event.preventDefault();
        focusPageSearch();
      }
    };
    window.addEventListener('keydown', onKeydown);

    return () => {
      disposed = true;
      stopShell?.();
      stopMenu?.();
      window.removeEventListener('keydown', onKeydown);
    };
  });

  $effect(() => {
    if (dto) {
      applyTheme(themeMode(dto.theme));
    }
  });

  function onchange(next: ShellDTO): void {
    dto = next;
  }
</script>

{#if dto && api}
  {#if dto.shellOpen}
    <MainShell {dto} {api} {onchange} />
  {:else}
    <UnlockView {dto} {api} {onchange} />
  {/if}
{:else if loadError}
  <p class="load-error" role="alert">{loadError}</p>
{:else}
  <div class="loading" aria-hidden="true"></div>
{/if}

<style>
  .load-error {
    margin: var(--space-lg);
    color: var(--color-error);
    font-size: var(--type-body-size);
  }

  .loading {
    min-height: 100vh;
    background: var(--color-background);
  }
</style>
