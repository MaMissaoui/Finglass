<script lang="ts">
  import { onMount } from 'svelte';
  import icon from '../assets/finglass-icon.png';
  import type { Api, ShellDTO } from '../lib/api';
  import { Icon, IconButton, SidebarItem } from '../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../lib/i18n';
  import DashboardPage from './pages/DashboardPage.svelte';
  import AccountsPage from './pages/AccountsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import TransactionsPage from './pages/TransactionsPage.svelte';
  import ReportsPage from './pages/ReportsPage.svelte';
  import LanguageFlag from './LanguageFlag.svelte';

  let {
    dto,
    api,
    onchange,
  }: {
    dto: ShellDTO;
    api: Api;
    onchange: (dto: ShellDTO) => void;
  } = $props();

  type Page = {
    screen: string;
    key: string;
    icon: 'dashboard' | 'accounts' | 'transactions' | 'documents' | 'reports' | 'settings';
  };

  const pages: Page[] = [
    { screen: 'dashboard', key: 'NavDashboard', icon: 'dashboard' },
    { screen: 'accounts', key: 'NavAccounts', icon: 'accounts' },
    { screen: 'transactions', key: 'NavTransactions', icon: 'transactions' },
    { screen: 'documents', key: 'NavDocuments', icon: 'documents' },
    { screen: 'reports', key: 'NavReports', icon: 'reports' },
    { screen: 'settings', key: 'NavSettings', icon: 'settings' },
  ];

  // The names are the language endonyms from Language.NativeName in Go and
  // the Slint language menu, not translated copy.
  const languageOptions = [
    { code: 'en', name: 'English' },
    { code: 'de', name: 'Deutsch' },
    { code: 'fr', name: 'Français' },
  ] as const;

  let systemDark = $state(false);
  let languageMenuOpen = $state(false);
  let profileMenuOpen = $state(false);
  let pending = $state(false);

  const language = $derived<Language>(languageFromCode(dto.language));
  const busy = $derived(dto.busy || pending);
  const effectiveDark = $derived(dto.theme === 'dark' || (dto.theme !== 'light' && systemDark));
  const languageLabel = $derived(`${t('ChangeLanguage')} · ${nativeName(language)}`);
  const openProfileName = $derived(dto.currentProfileName);

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function nativeName(code: Language): string {
    return languageOptions.find((option) => option.code === code)?.name ?? 'English';
  }

  async function apply(action: () => Promise<ShellDTO>): Promise<void> {
    if (pending) {
      return;
    }
    pending = true;
    try {
      onchange(await action());
    } finally {
      pending = false;
    }
  }

  function navigate(screen: string): void {
    void apply(() => api.navigate(screen));
  }

  function back(): void {
    void apply(() => api.back());
  }

  function toggleSidebar(): void {
    void apply(() => api.setSidebarCollapsed(!dto.sidebarCollapsed));
  }

  function toggleTheme(): void {
    void apply(() => api.setTheme(effectiveDark ? 'light' : 'dark'));
  }

  function pickLanguage(code: string): void {
    languageMenuOpen = false;
    void apply(() => api.setLanguage(code));
  }

  function pickProfile(index: number, open: boolean): void {
    profileMenuOpen = false;
    if (!open) {
      void apply(() => api.switchProfile(index));
    }
  }

  function manageProfiles(): void {
    profileMenuOpen = false;
    void apply(() => api.manageProfiles());
  }

  onMount(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)');
    systemDark = query.matches;
    const onChange = (event: MediaQueryListEvent) => {
      systemDark = event.matches;
    };
    query.addEventListener('change', onChange);

    const onKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        languageMenuOpen = false;
        profileMenuOpen = false;
      }
    };
    window.addEventListener('keydown', onKeydown);

    return () => {
      query.removeEventListener('change', onChange);
      window.removeEventListener('keydown', onKeydown);
    };
  });
</script>

<div class="shell">
  <aside class="sidebar" class:collapsed={dto.sidebarCollapsed} data-testid="sidebar">
    <div class="brand">
      <img src={icon} alt={t('AppLogo')} width="36" height="36" />
      {#if !dto.sidebarCollapsed}
        <!-- The product name, as Slint's app-name: not a translated text. -->
        <span class="app-name">Finglass</span>
      {/if}
    </div>

    <div class="brand-gap"></div>

    <nav class="nav">
      {#each pages as page (page.screen)}
        <div class="nav-row" title={dto.sidebarCollapsed ? t(page.key) : undefined}>
          <SidebarItem
            label={t(page.key)}
            icon={page.icon}
            selected={dto.screen === page.screen}
            collapsed={dto.sidebarCollapsed}
            onactivated={() => navigate(page.screen)}
          />
        </div>
      {/each}
    </nav>

    <div class="spacer"></div>

    {#if !dto.sidebarCollapsed}
      <p class="version">{dto.version}</p>
    {/if}
  </aside>

  <div class="divider" role="none"></div>

  <div class="main">
    <header class="topbar">
      <IconButton
        icon={dto.sidebarCollapsed ? 'menu' : 'menu-open'}
        label={dto.sidebarCollapsed ? t('ExpandSidebar') : t('CollapseSidebar')}
        filled
        onclick={toggleSidebar}
      />

      {#if dto.backTitle !== ''}
        <IconButton icon="arrow_back" label={dto.backTitle} filled enabled={!busy} onclick={back} />
      {/if}

      <h1 class="title">{dto.screenTitle}</h1>

      <div class="spacer"></div>

      {#if dto.profiles.length > 1}
        <button
          type="button"
          class="control profile-button"
          data-testid="profile-button"
          title={t('SwitchProfileMenu')}
          onclick={() => {
            profileMenuOpen = !profileMenuOpen;
            languageMenuOpen = false;
          }}
        >
          <span class="control-label">{openProfileName}</span>
          <Icon name="keyboard_arrow_down" size={18} />
        </button>
      {/if}

      <button
        type="button"
        class="control language-button"
        data-testid="language-button"
        title={languageLabel}
        aria-label={languageLabel}
        onclick={() => {
          languageMenuOpen = !languageMenuOpen;
          profileMenuOpen = false;
        }}
      >
        <LanguageFlag {language} />
        <Icon name="keyboard_arrow_down" size={18} />
      </button>

      <IconButton
        icon={effectiveDark ? 'sun' : 'moon'}
        label={t('ToggleTheme')}
        filled
        onclick={toggleTheme}
      />
    </header>

    <div class="title-divider" role="none"></div>

    <section class="page" data-page-area aria-label={dto.screenTitle}>
      {#if dto.screen === 'dashboard'}
        <DashboardPage {api} {language} />
      {:else if dto.screen === 'accounts'}
        <AccountsPage {api} {dto} />
      {:else if dto.screen === 'transactions'}
        <TransactionsPage {api} {dto} />
      {:else if dto.screen === 'reports'}
        <ReportsPage {api} {dto} />
      {:else if dto.screen === 'settings'}
        <SettingsPage {api} {dto} />
      {/if}
    </section>

    {#if languageMenuOpen}
      <button
        type="button"
        class="backdrop"
        tabindex="-1"
        aria-hidden="true"
        onclick={() => (languageMenuOpen = false)}
      ></button>
      <div
        class="menu language-menu"
        role="menu"
        aria-label={languageLabel}
        data-testid="language-menu"
      >
        {#each languageOptions as option (option.code)}
          <button
            type="button"
            class="menu-item language-item"
            role="menuitemradio"
            aria-checked={language === option.code}
            onclick={() => pickLanguage(option.code)}
          >
            <LanguageFlag language={option.code} />
            <span class="menu-label">{option.name}</span>
            {#if language === option.code}
              <Icon name="check" size={18} />
            {/if}
          </button>
        {/each}
      </div>
    {/if}

    {#if profileMenuOpen}
      <button
        type="button"
        class="backdrop"
        tabindex="-1"
        aria-hidden="true"
        onclick={() => (profileMenuOpen = false)}
      ></button>
      <div
        class="menu profile-menu"
        role="menu"
        aria-label={t('SwitchProfileMenu')}
        data-testid="profile-menu"
      >
        {#each dto.profiles as profile, index (profile.slug)}
          <button
            type="button"
            class="menu-item profile-item"
            role="menuitemradio"
            aria-checked={profile.open}
            onclick={() => pickProfile(index, profile.open)}
          >
            <span class="menu-label">{profile.name}</span>
            {#if profile.open}
              <Icon name="check" size={18} />
            {/if}
          </button>
        {/each}
        <div class="menu-divider" role="none"></div>
        <button
          type="button"
          class="menu-item profile-item"
          role="menuitem"
          onclick={manageProfiles}
        >
          <span class="menu-label">{t('ManageProfiles')}</span>
        </button>
      </div>
    {/if}
  </div>
</div>

<style>
  /* A fixed window as Slint's: only the page area scrolls, so the sidebar
     (and its version) and the top bar stay in place. */
  .shell {
    display: flex;
    height: 100vh;
    overflow: hidden;
    background: var(--color-background);
    color: var(--color-text);
  }

  .sidebar {
    display: flex;
    flex: none;
    flex-direction: column;
    width: 240px;
    padding: 16px;
    gap: 4px;
    overflow-y: auto;
    background: var(--color-sidebar);
  }

  .sidebar.collapsed {
    width: 72px;
    padding: 16px 12px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 56px;
  }

  .sidebar.collapsed .brand {
    justify-content: center;
  }

  .brand img {
    display: block;
    object-fit: contain;
  }

  .app-name {
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .brand-gap {
    height: 20px;
  }

  .nav {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .nav-row {
    display: flex;
    width: 100%;
  }

  .spacer {
    flex: 1;
  }

  .version {
    margin: 0;
    padding-left: 48px;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-weight);
  }

  .sidebar.collapsed .version {
    display: none;
  }

  .divider,
  .title-divider {
    flex: none;
    width: 1px;
    background: var(--color-divider);
  }

  .title-divider {
    width: auto;
    height: 1px;
  }

  .main {
    position: relative;
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
    flex-direction: column;
    background: var(--color-background);
  }

  .topbar {
    display: flex;
    flex: none;
    align-items: center;
    gap: 12px;
    height: 64px;
    padding: 0 24px 0 12px;
    background: var(--color-surface);
  }

  .title {
    margin: 0;
    min-height: 48px;
    display: flex;
    align-items: center;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-page-title-size);
    font-weight: var(--type-page-title-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .page {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    overflow-y: auto;
    /* A scrollbar that comes and goes would resize the charts, which resize
       the page, which toggles the scrollbar again. */
    scrollbar-gutter: stable;
    background: var(--color-background);
  }

  .control {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    gap: 8px;
    height: 36px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-control);
    background: var(--color-control-fill);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    cursor: pointer;
  }

  .control:hover {
    background: var(--color-control-fill-hover);
  }

  .control:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .profile-button {
    max-width: 200px;
  }

  .control-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .language-button {
    width: 64px;
    padding: 0;
    gap: 6px;
  }

  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 20;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .menu {
    position: absolute;
    z-index: 21;
    top: 58px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-menu);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .language-menu {
    right: 24px;
    width: 190px;
  }

  .profile-menu {
    right: 204px;
    width: 240px;
  }

  .menu-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    text-align: left;
    cursor: pointer;
  }

  .menu-item:hover {
    background: var(--color-background);
  }

  .menu-item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .language-item {
    height: 40px;
  }

  .profile-item {
    height: 36px;
  }

  .menu-item[aria-checked='true'] {
    background: var(--color-selected);
    color: var(--color-on-selected);
  }

  .menu-item[aria-checked='true']:hover {
    background: var(--color-selected);
  }

  .menu-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu-divider {
    height: 1px;
    margin: 0 -8px;
    background: var(--color-divider);
  }
</style>
