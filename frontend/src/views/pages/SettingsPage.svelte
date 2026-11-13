<script lang="ts">
  import { onMount } from 'svelte';
  import type {
    Api,
    SettingsCategoryRowDTO,
    SettingsDTO,
    SettingsRuleRowDTO,
    ShellDTO,
  } from '../../lib/api';
  import {
    Icon,
    IconButton,
    MessageBanner,
    PrimaryButton,
    SegmentedButton,
    SelectPill,
    TextButton,
  } from '../../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../../lib/i18n';
  import icon from '../../assets/finglass-icon.png';
  import BackupPassphraseDialog from './settings/BackupPassphraseDialog.svelte';
  import CategoryDialog from './settings/CategoryDialog.svelte';
  import FindeskPassphraseDialog from './settings/FindeskPassphraseDialog.svelte';
  import FindeskReportDialog from './settings/FindeskReportDialog.svelte';
  import ProfileNameDialog from './settings/ProfileNameDialog.svelte';
  import RuleDialog from './settings/RuleDialog.svelte';
  import RuleImportDialog from './settings/RuleImportDialog.svelte';
  import SettingsBanner from './settings/SettingsBanner.svelte';
  import SettingsConfirmDialog from './settings/SettingsConfirmDialog.svelte';
  import TaxKeywordsCard from './settings/TaxKeywordsCard.svelte';

  let {
    api,
    dto: shell,
  }: {
    api: Api;
    dto: ShellDTO;
  } = $props();

  type SectionItem = {
    key: string;
    icon:
      | 'palette'
      | 'person'
      | 'list'
      | 'auto_fix_high'
      | 'storage'
      | 'backup'
      | 'upload_file'
      | 'history'
      | 'info';
  };

  // The nine sections in FinDesk's order. Only appearance, categories, rules
  // and about have content in this port; the others show nothing yet.
  const sections: SectionItem[] = [
    { key: 'Appearance', icon: 'palette' },
    { key: 'Profiles', icon: 'person' },
    { key: 'Categories', icon: 'list' },
    { key: 'AutoCategorizationRules', icon: 'auto_fix_high' },
    { key: 'Security', icon: 'storage' },
    { key: 'BackupAndRestore', icon: 'backup' },
    { key: 'FinDeskImportTitle', icon: 'upload_file' },
    { key: 'AuditLogShort', icon: 'history' },
    { key: 'AboutTitle', icon: 'info' },
  ];

  let dto = $state<SettingsDTO | null>(null);
  let section = $state(0);
  let lastDTOSection = $state(-1);
  let pageWidth = $state(0);
  let loadFailed = $state(false);
  let moreMenuOpen = $state(false);
  let sectionMenuOpen = $state(false);
  let pending = $state(false);
  // svelte-ignore state_referenced_locally
  let lastLanguage = shell.language;

  const language = $derived<Language>(languageFromCode(shell.language));
  const narrow = $derived(pageWidth > 0 && pageWidth < 840);

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function setDTO(next: SettingsDTO): void {
    dto = next;
    if (next.section !== lastDTOSection) {
      section = next.section;
      lastDTOSection = next.section;
    }
  }

  async function run(action: () => Promise<SettingsDTO>): Promise<void> {
    try {
      setDTO(await action());
      loadFailed = false;
    } catch {
      loadFailed = true;
    }
  }

  function dismissMessage(): void {
    void run(() => api.settingsDismissMessage());
  }

  function selectSection(index: number): void {
    section = index;
    sectionMenuOpen = false;
    moreMenuOpen = false;
  }

  function setTheme(index: number): void {
    const code = index === 0 ? 'system' : index === 1 ? 'light' : 'dark';
    if (pending) {
      return;
    }
    pending = true;
    void api
      .setTheme(code)
      .catch(() => {
        loadFailed = true;
      })
      .finally(() => {
        pending = false;
      });
  }

  function setLanguage(index: number): void {
    const code = index === 0 ? 'en' : index === 1 ? 'de' : 'fr';
    if (pending) {
      return;
    }
    pending = true;
    void api
      .setLanguage(code)
      .catch(() => {
        loadFailed = true;
      })
      .finally(() => {
        pending = false;
      });
  }

  function setCurrency(index: number): void {
    const code = dto?.currencyOptions[index];
    if (code !== undefined) {
      void run(() => api.settingsSetPrimaryCurrency(code));
    }
  }

  function switchProfile(index: number): void {
    if (pending) {
      return;
    }
    pending = true;
    void api
      .switchProfile(index)
      .catch(() => {
        loadFailed = true;
      })
      .finally(() => {
        pending = false;
      });
  }

  function themeIndex(): number {
    if (shell.theme === 'light') {
      return 1;
    }
    if (shell.theme === 'dark') {
      return 2;
    }
    return 0;
  }

  function languageIndex(): number {
    if (language === 'de') {
      return 1;
    }
    if (language === 'fr') {
      return 2;
    }
    return 0;
  }

  function rowKey(row: SettingsCategoryRowDTO | SettingsRuleRowDTO): number {
    return row.id;
  }

  onMount(() => {
    let received = false;
    const stop = api.onSettingsChanged((next) => {
      received = true;
      setDTO(next);
    });

    void api
      .settingsState()
      .then((next) => {
        if (!received) {
          setDTO(next);
        }
      })
      .catch(() => {
        loadFailed = true;
      });

    return () => {
      stop();
    };
  });

  // The shell changes the language; a fresh pull renders the DTO's own texts
  // (usage counts, budget amounts, rule restrictions) in it as well.
  $effect(() => {
    const code = shell.language;
    if (code !== lastLanguage) {
      lastLanguage = code;
      void run(() => api.settingsLoad());
    }
  });
</script>

<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      moreMenuOpen = false;
      sectionMenuOpen = false;
    }
  }}
/>

<div class="settings" bind:clientWidth={pageWidth} data-testid="settings-page">
  {#if dto === null}
    {#if loadFailed}
      <div class="load-error">
        <MessageBanner
          text={t('ErrorUnexpected')}
          isError
          dismissLabel={t('Dismiss')}
          ondismiss={() => (loadFailed = false)}
        />
      </div>
    {:else}
      <div class="loading" aria-hidden="true"><span class="spinner"></span></div>
    {/if}
  {:else if narrow}
    <div class="narrow">
      <SettingsBanner
        errorText={dto.errorText}
        messageText={dto.messageText}
        dismissLabel={t('Dismiss')}
        ondismiss={dismissMessage}
      />

      <button
        type="button"
        class="section-dropdown"
        class:open={sectionMenuOpen}
        aria-haspopup="menu"
        aria-expanded={sectionMenuOpen}
        onclick={() => (sectionMenuOpen = !sectionMenuOpen)}
      >
        <span class="dropdown-label">{t(sections[section]?.key ?? 'Appearance')}</span>
        <Icon name="arrow_drop_down" size={18} />
      </button>

      <div class="narrow-scroll">
        <div class="inner">
          {@render body()}
        </div>
      </div>
    </div>

    {#if sectionMenuOpen}
      <button
        type="button"
        class="menu-backdrop"
        tabindex="-1"
        aria-hidden="true"
        onclick={() => (sectionMenuOpen = false)}
      ></button>
      <div
        class="section-menu"
        class:with-banner={dto.errorText !== '' || dto.messageText !== ''}
        role="menu"
        aria-label={t('NavSettings')}
        data-testid="settings-section-menu"
      >
        {#each sections as item, index (item.key)}
          <button
            type="button"
            class="section-row"
            class:selected={section === index}
            role="menuitemradio"
            aria-checked={section === index}
            onclick={() => selectSection(index)}
          >
            <span class="section-icon"><Icon name={item.icon} size={24} /></span>
            <span class="section-label">{t(item.key)}</span>
          </button>
        {/each}
      </div>
    {/if}
  {:else}
    <div class="wide">
      <nav class="sections" aria-label={t('NavSettings')}>
        {#each sections as item, index (item.key)}
          <button
            type="button"
            class="section-row"
            class:selected={section === index}
            aria-current={section === index ? 'true' : undefined}
            onclick={() => selectSection(index)}
          >
            <span class="section-icon"><Icon name={item.icon} size={24} /></span>
            <span class="section-label">{t(item.key)}</span>
          </button>
        {/each}
      </nav>

      <div class="divider" role="none"></div>

      <div class="content">
        <SettingsBanner
          errorText={dto.errorText}
          messageText={dto.messageText}
          dismissLabel={t('Dismiss')}
          ondismiss={dismissMessage}
        />

        <div class="scroll">
          <div class="inner">
            {@render body()}
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>

{#snippet body()}
  {#if dto !== null && section === 0}
    <section class="section" aria-label={t('Appearance')}>
      <header class="section-header">
        <h2 class="section-title">{t('Appearance')}</h2>
      </header>

      <div class="form">
        <div class="form-row">
          <span class="form-label">{t('Theme')}</span>
          <SegmentedButton
            labels={[t('ThemeSystem'), t('ThemeLight'), t('ThemeDark')]}
            selected={themeIndex()}
            onselect={setTheme}
          />
        </div>
        <div class="form-row">
          <span class="form-label">{t('Language')}</span>
          <SegmentedButton
            labels={['EN', 'DE', 'FR']}
            selected={languageIndex()}
            onselect={setLanguage}
          />
        </div>
        <div class="form-row">
          <span class="form-label">{t('PrimaryCurrency')}</span>
          <div class="currency">
            <SelectPill
              labelPrefix=""
              value={dto.primaryCurrency}
              options={dto.currencyOptions}
              onchosen={setCurrency}
            />
            <p class="hint">{t('PrimaryCurrencyHint')}</p>
          </div>
        </div>
      </div>
    </section>
  {:else if dto !== null && section === 1}
    <section class="section" aria-label={t('Profiles')}>
      <header class="section-header">
        <h2 class="section-title">{t('Profiles')}</h2>
        <PrimaryButton
          text={t('NewProfile')}
          icon="add"
          onclick={() => run(() => api.settingsNewProfile())}
        />
      </header>

      <div class="list-body">
        <p class="muted-text">{t('ProfilesHint')}</p>

        {#each dto.profileRows as row (row.index)}
          <div class="profile-row" data-profile-index={row.index}>
            <span class="row-name">{row.name}</span>
            {#if row.open}
              <span class="tag">{t('ProfileOpen')}</span>
            {/if}
            {#if row.nextStart}
              <span class="small">{t('ProfileNextStart')}</span>
            {/if}
            {#if row.canSwitch}
              <TextButton text={t('OpenProfile')} onclick={() => switchProfile(row.index)} />
            {/if}
            <IconButton
              icon="edit"
              label={t('Edit')}
              iconSize={20}
              onclick={() => run(() => api.settingsRenameProfile(row.index))}
            />
            {#if row.canDelete}
              <IconButton
                icon="delete"
                label={t('Delete')}
                iconSize={20}
                destructive
                onclick={() => run(() => api.settingsDeleteProfile(row.index))}
              />
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {:else if dto !== null && section === 2}
    <section class="section" aria-label={t('Categories')}>
      <header class="section-header">
        <h2 class="section-title">{t('Categories')}</h2>
        {#if !dto.categoriesEmpty}
          <div class="section-actions">
            <PrimaryButton
              text={t('AddCategory')}
              icon="add"
              onclick={() => run(() => api.settingsOpenAddCategory())}
            />
            <div class="menu-wrap">
              <IconButton
                icon="more_vert"
                label={t('More')}
                filled
                onclick={() => (moreMenuOpen = !moreMenuOpen)}
              />
              {#if moreMenuOpen}
                <div class="menu" role="menu" aria-label={t('More')}>
                  {#if dto.hasDuplicateColours}
                    <button
                      type="button"
                      class="menu-item"
                      role="menuitem"
                      onclick={() => {
                        moreMenuOpen = false;
                        void run(() => api.settingsFixDuplicateColours());
                      }}
                    >
                      {t('FixDuplicateColours')}
                    </button>
                  {/if}
                  <button
                    type="button"
                    class="menu-item"
                    role="menuitem"
                    onclick={() => {
                      moreMenuOpen = false;
                      void run(() => api.settingsAddDefaultCategories());
                    }}
                  >
                    {t('AddDefaultCategories')}
                  </button>
                </div>
              {/if}
            </div>
          </div>
        {/if}
      </header>

      <div class="list-body">
        {#if dto.categoriesEmpty}
          <p class="muted-text">{t('NoCategoriesYet')}</p>
          <div class="empty-actions">
            <PrimaryButton
              text={t('AddDefaultCategories')}
              onclick={() => run(() => api.settingsAddDefaultCategories())}
            />
            <TextButton
              text={t('AddCategory')}
              icon="add"
              onclick={() => run(() => api.settingsOpenAddCategory())}
            />
          </div>
        {/if}

        {#each dto.categoryRows as row (rowKey(row))}
          <div class="category-row" data-category-id={row.id}>
            <span class="colour-dot" style:background={row.colour} aria-hidden="true"></span>
            <span class="row-name">{row.name}</span>
            {#if row.transfer}
              <span class="small">{t('CategoryTransferLabel')}</span>
            {/if}
            {#if row.taxRelevant}
              <span class="tax">{t('TaxRelevant')}</span>
            {/if}
            {#if row.budgetText !== ''}
              <span class="small">{row.budgetText}</span>
            {/if}
            <span class="small">{row.usageText}</span>
            <IconButton
              icon="edit"
              label={t('Edit')}
              iconSize={20}
              onclick={() => run(() => api.settingsOpenEditCategory(row.id))}
            />
            <IconButton
              icon="delete"
              label={t('Delete')}
              iconSize={20}
              destructive
              onclick={() => run(() => api.settingsOpenDeleteCategory(row.id))}
            />
          </div>
        {/each}

        {#key dto.taxKeywords}
          <TaxKeywordsCard
            title={t('TaxSuggestionKeywords')}
            keywords={dto.taxKeywords}
            count={dto.taxKeywordsCount}
            saveText={t('Save')}
            resetText={t('ResetToDefaults')}
            onsave={(text) => run(() => api.settingsSaveTaxKeywords(text))}
            onreset={() => run(() => api.settingsResetTaxKeywords())}
          />
        {/key}
      </div>
    </section>
  {:else if dto !== null && section === 3}
    <section class="section" aria-label={t('AutoCategorizationRules')}>
      <header class="section-header">
        <h2 class="section-title">{t('AutoCategorizationRules')}</h2>
        <div class="section-actions">
          <PrimaryButton
            text={t('AddRule')}
            enabled={!dto.categoriesEmpty}
            onclick={() => run(() => api.settingsOpenAddRule())}
          />
          <TextButton
            text={t('ImportRules')}
            onclick={() => run(() => api.settingsImportRules())}
          />
          <TextButton
            text={t('ExportRules')}
            onclick={() => run(() => api.settingsExportRules())}
          />
        </div>
      </header>

      <div class="list-body">
        <p class="muted-text">{t('RulesHint')}</p>
        {#if dto.categoriesEmpty}
          <p class="muted-text">{t('HintNeedsCategory')}</p>
        {:else if dto.ruleRows.length === 0}
          <p class="muted-text">{t('NoRulesYet')}</p>
        {/if}

        {#each dto.ruleRows as row (rowKey(row))}
          <div class="rule-row" data-rule-id={row.id}>
            <span class="marker" style:background={row.colour} aria-hidden="true"></span>
            <div class="rule-main">
              <span class="rule-category">{row.categoryText}</span>
              <span class="small ellipsis">{row.pattern}</span>
              {#if row.detail !== ''}
                <span class="small ellipsis">{row.detail}</span>
              {/if}
            </div>
            <span class="small prio">{row.prioText}</span>
            <IconButton
              icon="edit"
              label={t('Edit')}
              iconSize={20}
              onclick={() => run(() => api.settingsOpenEditRule(row.id))}
            />
            <IconButton
              icon="delete"
              label={t('Delete')}
              iconSize={20}
              destructive
              onclick={() => run(() => api.settingsOpenDeleteRule(row.id))}
            />
          </div>
        {/each}
      </div>
    </section>
  {:else if dto !== null && section === 5}
    <section class="section" aria-label={t('BackupAndRestore')}>
      <header class="section-header">
        <h2 class="section-title">{t('BackupAndRestore')}</h2>
      </header>

      <div class="list-body">
        <p class="muted-text">{t('BackupHint')}</p>

        <div class="backup-actions">
          <PrimaryButton
            text={t('CreateBackup')}
            enabled={!dto.busy}
            onclick={() => run(() => api.settingsCreateBackup())}
          />
          <TextButton
            text={t('RestoreFromBackup')}
            enabled={!dto.busy && !dto.hasPendingRestore}
            onclick={() => run(() => api.settingsRestoreBackup())}
          />
          {#if dto.busy}
            <span class="spinner" aria-hidden="true"></span>
          {/if}
        </div>

        {#if dto.hasPendingRestore}
          <div class="pending-restore">
            <span class="pending-text">{t('RestoreWaiting')}</span>
            <TextButton
              text={t('CancelRestore')}
              enabled={!dto.busy}
              onclick={() => run(() => api.settingsCancelPendingRestore())}
            />
          </div>
        {/if}

        {#if dto.backupRestoredText !== ''}
          <p class="restore-note">{dto.backupRestoredText}</p>
        {/if}
      </div>
    </section>
  {:else if dto !== null && section === 6}
    <section class="section" aria-label={t('FinDeskImportTitle')}>
      <header class="section-header">
        <h2 class="section-title">{t('FinDeskImportTitle')}</h2>
      </header>

      <div class="list-body">
        <p class="muted-text">{t('FinDeskImportHint')}</p>

        <div class="backup-actions">
          <PrimaryButton
            text={t('FinDeskImportChooseFolder')}
            enabled={!dto.findeskBusy}
            onclick={() => run(() => api.settingsImportFindesk())}
          />
          {#if dto.findeskBusy}
            <span class="spinner" aria-hidden="true"></span>
          {/if}
        </div>
      </div>
    </section>
  {:else if dto !== null && section === 8}
    <section class="section" aria-label={t('AboutTitle')}>
      <header class="section-header">
        <h2 class="section-title">{t('AboutTitle')}</h2>
      </header>

      <div class="about">
        <img src={icon} alt="" width="64" height="64" />
        <div class="about-text">
          <span class="product">Finglass</span>
          <span class="version">{shell.aboutVersion}</span>
          <p class="muted-text">{t('AboutText')}</p>
          <p class="muted-text">{t('AboutBankCodesSource')}</p>
        </div>
      </div>
    </section>
  {/if}
{/snippet}

{#if dto !== null && dto.categoryDialogOpen}
  <CategoryDialog
    title={dto.categoryDialogId >= 0 ? t('EditCategory') : t('AddCategory')}
    name={dto.categoryDialogName}
    colour={dto.categoryDialogColour}
    budget={dto.categoryDialogBudget}
    taxRelevant={dto.categoryDialogTax}
    transfer={dto.categoryDialogTransfer}
    errorText={dto.categoryDialogError}
    swatches={dto.categoryDialogSwatches}
    labels={{
      name: t('Name'),
      budget: t('MonthlyBudgetOptional'),
      taxHint: t('TaxRelevantHint'),
      transfer: t('CategoryIsTransfer'),
      transferHint: t('CategoryIsTransferHint'),
      cancel: t('Cancel'),
      save: t('Save'),
    }}
    onchanged={(name, colour) => run(() => api.settingsCategoryChanged(name, colour))}
    onclose={() => run(() => api.settingsCloseCategoryDialog())}
    onsave={(name, colour, budget, taxRelevant, transfer) =>
      run(() => api.settingsSaveCategory(name, colour, budget, taxRelevant, transfer))}
  />
{/if}

{#if dto !== null && dto.categoryDeleteOpen}
  <SettingsConfirmDialog
    title={dto.categoryDeleteTitle}
    text={dto.categoryDeleteText}
    confirmText={t('Delete')}
    cancelText={t('Cancel')}
    testId="category-delete-dialog"
    onconfirm={() => run(() => api.settingsConfirmDeleteCategory())}
    oncancel={() => run(() => api.settingsCancelDeleteCategory())}
  />
{/if}

{#if dto !== null && dto.ruleDialogOpen}
  <RuleDialog
    title={dto.ruleDialogId >= 0 ? t('EditRule') : t('AddRule')}
    pattern={dto.ruleDialogPattern}
    categories={dto.ruleDialogCategories}
    categoryIndex={dto.ruleDialogCategoryIndex}
    priority={dto.ruleDialogPriority}
    autoApply={dto.ruleDialogAutoApply}
    fieldOptions={dto.ruleDialogFieldOptions}
    fieldIndex={dto.ruleDialogFieldIndex}
    directionOptions={dto.ruleDialogDirectionOptions}
    directionIndex={dto.ruleDialogDirectionIndex}
    taxOptions={dto.ruleDialogTaxOptions}
    taxIndex={dto.ruleDialogTaxIndex}
    errorText={dto.ruleDialogError}
    labels={{
      pattern: t('PatternExample'),
      category: t('ColCategory'),
      searchIn: t('RuleSearchIn'),
      appliesTo: t('RuleAppliesTo'),
      tax: t('TaxMenu'),
      priority: t('Priority'),
      autoApply: t('ApplyAutomatically'),
      cancel: t('Cancel'),
      save: t('Save'),
    }}
    onchanged={(pattern, priority) => run(() => api.settingsRuleChanged(pattern, priority))}
    onclose={() => run(() => api.settingsCloseRuleDialog())}
    onsave={(pattern, categoryIndex, priority, autoApply, fieldIndex, flow, taxIndex) =>
      run(() =>
        api.settingsSaveRule(
          pattern,
          categoryIndex,
          priority,
          autoApply,
          fieldIndex,
          flow,
          taxIndex,
        ),
      )}
  />
{/if}

{#if dto !== null && dto.ruleDeleteOpen}
  <SettingsConfirmDialog
    title={dto.ruleDeleteTitle}
    text={dto.ruleDeleteText}
    confirmText={t('Delete')}
    cancelText={t('Cancel')}
    testId="rule-delete-dialog"
    onconfirm={() => run(() => api.settingsConfirmDeleteRule())}
    oncancel={() => run(() => api.settingsCancelDeleteRule())}
  />
{/if}

{#if dto !== null && dto.ruleImportOpen}
  <RuleImportDialog
    title={t('RuleImportTitle')}
    newRulesText={dto.ruleImportNewRulesText}
    categoriesText={dto.ruleImportCategoriesText}
    categoriesList={dto.ruleImportCategoriesList}
    duplicatesText={dto.ruleImportDuplicatesText}
    conflictsText={dto.ruleImportConflictsText}
    conflictsList={dto.ruleImportConflictsList}
    errorsText={dto.ruleImportErrorsText}
    errorsList={dto.ruleImportErrorsList}
    showCategorize={dto.ruleImportShowCategorize}
    categorizeText={dto.ruleImportCategorizeText}
    categorize={dto.ruleImportCategorize}
    busy={dto.ruleImportBusy}
    confirmEnabled={dto.ruleImportConfirmEnabled}
    cancelText={t('Cancel')}
    importText={t('Import')}
    onconfirm={(categorize) => run(() => api.settingsConfirmRuleImport(categorize))}
    oncancel={() => run(() => api.settingsCancelRuleImport())}
  />
{/if}

{#if dto !== null && dto.profileDialogOpen}
  <ProfileNameDialog
    title={dto.profileDialogTitle}
    name={dto.profileDialogName}
    errorText={dto.profileDialogError}
    nameLabel={t('ProfileName')}
    cancelText={t('Cancel')}
    saveText={t('Save')}
    onclose={() => run(() => api.settingsCloseProfileDialog())}
    onsave={(name) => run(() => api.settingsSaveProfile(name))}
  />
{/if}

{#if dto !== null && dto.profileDeleteOpen}
  <SettingsConfirmDialog
    title={dto.profileDeleteTitle}
    text={dto.profileDeleteText}
    confirmText={t('Delete')}
    cancelText={t('Cancel')}
    testId="profile-delete-dialog"
    onconfirm={() => run(() => api.settingsConfirmDeleteProfile())}
    oncancel={() => run(() => api.settingsCancelDeleteProfile())}
  />
{/if}

{#if dto !== null && dto.restoreOpen}
  <SettingsConfirmDialog
    title={dto.restoreTitle}
    text={dto.restoreText}
    confirmText={t('PrepareRestore')}
    cancelText={t('Cancel')}
    testId="restore-dialog"
    busy={dto.busy || dto.findeskBusy}
    onconfirm={() => run(() => api.settingsConfirmRestore())}
    oncancel={() => run(() => api.settingsCancelRestore())}
  />
{/if}

{#if dto !== null && dto.restorePassphraseOpen}
  <BackupPassphraseDialog
    title={t('BackupPassphraseTitle')}
    fileName={dto.restorePassphraseFile}
    text={t('BackupPassphraseText')}
    wrong={dto.restorePassphraseWrong}
    busy={dto.busy || dto.findeskBusy}
    passphraseLabel={t('Passphrase')}
    wrongText={t('WrongPassphrase')}
    cancelText={t('Cancel')}
    confirmText={t('PrepareRestore')}
    onsubmit={(passphrase) => run(() => api.settingsConfirmRestorePassphrase(passphrase))}
    oncancel={() => run(() => api.settingsCancelRestorePassphrase())}
  />
{/if}

{#if dto !== null && dto.findeskPassphraseOpen}
  <FindeskPassphraseDialog
    title={t('FinDeskPassphraseTitle')}
    folder={dto.findeskFolder}
    text={t('FinDeskPassphraseText')}
    wrong={dto.findeskPassphraseWrong}
    busy={dto.busy || dto.findeskBusy}
    passphraseLabel={t('Passphrase')}
    wrongText={t('WrongPassphrase')}
    cancelText={t('Cancel')}
    importText={t('Import')}
    onsubmit={(passphrase) => run(() => api.settingsSubmitFindeskPassphrase(passphrase))}
    oncancel={() => run(() => api.settingsCancelFindeskPassphrase())}
  />
{/if}

{#if dto !== null && dto.findeskReportOpen}
  <FindeskReportDialog
    title={dto.findeskReportTitle}
    text={dto.findeskReportText}
    isError={dto.findeskReportError}
    closeText={t('Close')}
    onclose={() => run(() => api.settingsCloseFindeskReport())}
  />
{/if}

{#if moreMenuOpen}
  <button
    type="button"
    class="menu-backdrop"
    tabindex="-1"
    aria-hidden="true"
    onclick={() => (moreMenuOpen = false)}
  ></button>
{/if}

<style>
  .settings {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  .wide {
    display: flex;
    height: 100%;
    padding: 24px;
  }

  .sections {
    display: flex;
    flex: none;
    align-self: flex-start;
    width: 320px;
    flex-direction: column;
    gap: 4px;
    padding: 8px;
  }

  .divider {
    flex: none;
    width: 1px;
    background: var(--color-divider);
  }

  .content {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
    flex-direction: column;
    gap: 12px;
    padding-left: 24px;
  }

  .scroll,
  .narrow-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-gutter: stable;
  }

  .inner {
    display: flex;
    width: min(100%, 760px);
    margin: 0 auto;
    flex-direction: column;
  }

  .narrow {
    display: flex;
    height: 100%;
    flex-direction: column;
    gap: 12px;
    padding: 24px;
  }

  .section-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 48px;
    padding: 8px;
    border: 0;
    border-radius: var(--radius-panel);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-body-large-size);
    font-weight: var(--type-body-large-weight);
    text-align: left;
    cursor: pointer;
  }

  .section-row:hover:not(.selected) {
    background: var(--color-control-fill);
  }

  .section-row.selected {
    background: var(--color-selected-accent);
  }

  .section-row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .section-icon {
    display: inline-flex;
    flex: none;
    color: var(--color-muted-text);
  }

  .section-row.selected .section-icon {
    color: var(--color-text);
  }

  .section-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .section-dropdown {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
    height: 40px;
    padding: 0 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-background);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    text-align: left;
    cursor: pointer;
  }

  .section-dropdown:hover,
  .section-dropdown.open {
    background: var(--color-surface);
  }

  .section-dropdown:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }

  .dropdown-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .section-dropdown :global(.icon) {
    color: var(--color-muted-text);
  }

  .section-menu {
    position: absolute;
    z-index: 41;
    top: 68px;
    left: 24px;
    right: 24px;
    max-height: calc(100% - 92px);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-menu);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .section-menu.with-banner {
    top: 126px;
  }

  .menu-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    padding: 0;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .menu-wrap {
    position: relative;
  }

  .menu {
    position: absolute;
    z-index: 41;
    top: calc(100% + 4px);
    right: 0;
    display: flex;
    width: 220px;
    flex-direction: column;
    padding: 4px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-menu);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  .menu-item {
    display: flex;
    align-items: center;
    height: 36px;
    padding: 0 12px;
    border: 0;
    border-radius: var(--radius-small);
    background: transparent;
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }

  .menu-item:hover {
    background: var(--color-background);
  }

  .menu-item:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .section-header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    min-height: 48px;
  }

  .section-title {
    flex: 1 1 auto;
    min-width: min-content;
    margin: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .section-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .form {
    display: flex;
    flex-direction: column;
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 40px;
  }

  .form-label {
    flex: none;
    width: 150px;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    text-align: right;
  }

  .currency {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 4px;
  }

  .currency :global(.select) {
    display: block;
  }

  .currency :global(.trigger) {
    width: 100%;
    justify-content: space-between;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .list-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .muted-text {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }

  .empty-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .category-row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 46px;
  }

  .colour-dot {
    flex: none;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-small);
  }

  .row-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .small {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    white-space: nowrap;
  }

  .tax {
    color: var(--color-caution);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .rule-row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 64px;
  }

  .marker {
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: var(--radius-marker);
  }

  .rule-main {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .rule-category {
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .ellipsis {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .prio {
    flex: none;
  }

  .profile-row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 46px;
  }

  .tag {
    display: inline-flex;
    flex: none;
    align-items: center;
    height: 22px;
    padding: 0 8px;
    border-radius: var(--radius-tag);
    background: var(--color-selected);
    color: var(--color-on-selected);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
    white-space: nowrap;
  }

  .backup-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .backup-actions .spinner {
    width: 20px;
    height: 20px;
    border-width: 2px;
  }

  .pending-restore {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .pending-text {
    flex: 1;
    min-width: 0;
    color: var(--color-caution);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .restore-note {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .about {
    display: flex;
    gap: 16px;
  }

  .about img {
    display: block;
    flex: none;
    width: 64px;
    height: 64px;
    object-fit: contain;
    border-radius: var(--radius-icon);
  }

  .about-text {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 6px;
  }

  .product {
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .version {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .loading {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 240px;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .load-error {
    padding: 24px;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
