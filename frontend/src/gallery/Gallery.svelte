<script lang="ts">
  import {
    Badge,
    BankBadge,
    Card,
    Checkbox,
    ChoiceChip,
    CountBadge,
    DataTable,
    Dialog,
    Icon,
    IconButton,
    MessageBanner,
    OutlinedField,
    PeriodNavigator,
    PrimaryButton,
    SearchField,
    SecondaryButton,
    SegmentedButton,
    SelectPill,
    SidebarItem,
    TabPill,
    TextButton,
    Tooltip,
    type DataColumn,
    type DataRow,
  } from '../lib/components';
  import en from '../lib/generated/i18n/en.json';

  let choice = $state(1);
  let tab = $state(0);
  let segment = $state(0);
  let selectValue = $state(en.DirectionAll);
  let search = $state('');
  let name = $state('');
  let accountName = $state(en.AccountName);
  let secret = $state('');
  let checked = $state(true);
  let bannerVisible = $state(true);
  let errorBannerVisible = $state(true);
  let dialogOpen = $state(true);
  let selectedRow = $state('row-2');
  let previousCount = $state(0);
  let nextCount = $state(0);
  let disabledCount = $state(0);

  const options = [en.DirectionAll, en.DirectionIncome, en.DirectionExpenses];
  const segmented = [en.ThemeLight, en.ThemeDark, en.ThemeSystem];
  const chips = [en.DirectionAll, en.DirectionIncome, en.DirectionExpenses];
  const tabs = [en.TabAllDocuments, en.TabUnlinked, en.TabLinked];

  const columns: DataColumn[] = [
    { key: 'date', label: en.ColDate, numeric: true, width: '110px' },
    { key: 'description', label: en.ColDescription },
    { key: 'category', label: en.ColCategory, width: '160px' },
    { key: 'amount', label: en.ColAmount, align: 'right', numeric: true, width: '130px' },
  ];

  const rows: DataRow[] = [
    {
      id: 'row-1',
      cells: {
        date: '03.11.2026',
        description: en.DefaultCategoryGroceries,
        category: en.DefaultCategoryGroceries,
        amount: '\u221223,48\u00a0\u20ac',
      },
    },
    {
      id: 'row-2',
      cells: {
        date: '02.11.2026',
        description: en.DefaultCategorySalary,
        category: en.DefaultCategorySalary,
        amount: '+2.450,00\u00a0\u20ac',
      },
    },
    {
      id: 'row-3',
      cells: {
        date: '01.11.2026',
        description: en.DefaultCategoryEnergy,
        category: en.DefaultCategoryEnergy,
        amount: '\u221289,00\u00a0\u20ac',
      },
    },
  ];
</script>

<main class="gallery" data-gallery="FINGLASS_GALLERY_ONLY_7A">
  <header class="masthead">
    <h1>{en.Appearance}</h1>
    <p>{en.Theme}: {en.ThemeSystem}</p>
  </header>

  <section class="section">
    <h2>PrimaryButton</h2>
    <div class="row">
      <PrimaryButton text={en.Save} onclick={() => {}} />
      <PrimaryButton text={en.Add} icon="add" onclick={() => {}} />
      <PrimaryButton text={en.Delete} destructive onclick={() => {}} />
      <PrimaryButton text={en.ExportPdf} enabled={false} onclick={() => (disabledCount += 1)} />
    </div>
    <output data-testid="disabled-count">{disabledCount}</output>
  </section>

  <section class="section">
    <h2>SecondaryButton</h2>
    <div class="row">
      <SecondaryButton text={en.Cancel} onclick={() => {}} />
      <SecondaryButton text={en.Sync} icon="sync" onclick={() => {}} />
      <SecondaryButton text={en.Reset} enabled={false} onclick={() => {}} />
    </div>
  </section>

  <section class="section">
    <h2>TextButton</h2>
    <div class="row">
      <TextButton text={en.Back} onclick={() => {}} />
      <TextButton text={en.Open} icon="arrow_forward" onclick={() => {}} />
      <TextButton text={en.More} enabled={false} onclick={() => {}} />
    </div>
  </section>

  <section class="section">
    <h2>IconButton</h2>
    <div class="row">
      <IconButton icon="add" label={en.Add} filled onclick={() => {}} />
      <IconButton icon="check" label={en.Save} filled selected onclick={() => {}} />
      <IconButton icon="more_vert" label={en.More} onclick={() => {}} />
      <IconButton icon="arrow_back" label={en.Back} outlined onclick={() => {}} />
      <IconButton icon="delete" label={en.Delete} destructive onclick={() => {}} />
      <IconButton icon="refresh" label={en.Apply} enabled={false} onclick={() => {}} />
    </div>
  </section>

  <section class="section">
    <h2>Icon</h2>
    <div class="row">
      <Icon name="info" size={20} label={en.TaxRelevant} />
      <Icon name="search" size={24} />
      <Icon name="flag-gb" size={20} label={en.Language} />
    </div>
  </section>

  <section class="section">
    <h2>SegmentedButton</h2>
    <div class="row">
      <SegmentedButton
        labels={segmented}
        selected={segment}
        onselect={(index) => (segment = index)}
      />
    </div>
  </section>

  <section class="section">
    <h2>ChoiceChip</h2>
    <div class="row">
      {#each chips as chip, index (chip)}
        <ChoiceChip label={chip} selected={choice === index} onclick={() => (choice = index)} />
      {/each}
    </div>
  </section>

  <section class="section">
    <h2>TabPill</h2>
    <div class="row">
      {#each tabs as label, index (label)}
        <TabPill text={label} selected={tab === index} onclick={() => (tab = index)} />
      {/each}
    </div>
  </section>

  <section class="section">
    <h2>SelectPill</h2>
    <div class="row">
      <SelectPill
        filled
        labelPrefix={`${en.SelectMonth}: `}
        value={selectValue}
        {options}
        onvaluechosen={(value) => (selectValue = value)}
      />
      <SelectPill
        labelPrefix={`${en.PeriodThisMonth}: `}
        value={selectValue}
        {options}
        onvaluechosen={(value) => (selectValue = value)}
      />
      <SelectPill
        filled
        enabled={false}
        labelPrefix={`${en.DirectionAll}: `}
        value={selectValue}
        {options}
      />
    </div>
  </section>

  <section class="section">
    <h2>SearchField</h2>
    <div class="row">
      <SearchField
        bind:value={search}
        placeholder={en.SearchPartnerPurpose}
        clearLabel={en.ClearSearch}
        label={en.SearchPartnerPurpose}
      />
      <SearchField
        value={en.SearchDocuments}
        placeholder={en.SearchDocuments}
        clearLabel={en.ClearSearch}
        label={en.SearchDocuments}
      />
      <SearchField
        value={en.SearchDocuments}
        placeholder={en.SearchDocuments}
        clearLabel={en.ClearSearch}
        label={en.SearchDocuments}
        enabled={false}
      />
    </div>
  </section>

  <section class="section">
    <h2>OutlinedField</h2>
    <div class="grid fields">
      <OutlinedField bind:value={name} label={en.Name} supportingText={en.ConnectionNameHint} />
      <OutlinedField bind:value={accountName} label={en.AccountName} />
      <OutlinedField
        bind:value={secret}
        label={en.Passphrase}
        secret
        supportingText={en.PassphraseNoRecovery}
      />
      <OutlinedField
        value={en.EnterValidAmount}
        label={en.ColAmount}
        error
        supportingText={en.EnterValidAmount}
      />
      <OutlinedField value={en.AccountName} label={en.AccountName} enabled={false} />
      <OutlinedField value={en.ConnectionNameHint} label={en.ConnectionName} multiline />
    </div>
  </section>

  <section class="section">
    <h2>Checkbox</h2>
    <div class="row column">
      <Checkbox bind:checked text={en.TaxRelevant} />
      <Checkbox checked={false} text={en.TaxNotRelevantChoice} />
      <Checkbox checked text={en.ReceiptsComplete} enabled={false} />
    </div>
  </section>

  <section class="section">
    <h2>Card</h2>
    <div class="row">
      <Card>
        <div class="card-body">
          <h3>{en.IncomeThisMonth}</h3>
          <p>2.450,00&nbsp;€</p>
        </div>
      </Card>
    </div>
  </section>

  <section class="section">
    <h2>CountBadge, Badge, BankBadge</h2>
    <div class="row">
      <CountBadge count={1} />
      <CountBadge count={12} />
      <CountBadge count={120} />
      <CountBadge count={7} selected />
      <Badge text={en.TextExtracted} />
      <Badge text={en.NoText} positive={false} />
      <BankBadge logo="sparkasse" />
      <BankBadge letters="VR" colourIndex={1} />
      <BankBadge letters="DKB" colourIndex={3} size={36} />
    </div>
  </section>

  <section class="section">
    <h2>MessageBanner</h2>
    <div class="column">
      {#if bannerVisible}
        <MessageBanner
          text={en.ReceiptsComplete}
          dismissLabel={en.Dismiss}
          ondismiss={() => (bannerVisible = false)}
        />
      {/if}
      {#if errorBannerVisible}
        <MessageBanner
          text={en.ErrorSyncFailed}
          isError
          dismissLabel={en.Dismiss}
          ondismiss={() => (errorBannerVisible = false)}
        />
      {/if}
    </div>
  </section>

  <section class="section">
    <h2>SidebarItem</h2>
    <div class="sidebar-sample">
      <SidebarItem label={en.NavDashboard} icon="dashboard" onactivated={() => {}} />
      <SidebarItem label={en.NavAccounts} icon="accounts" selected onactivated={() => {}} />
      <SidebarItem label={en.NavSettings} icon="settings" collapsed onactivated={() => {}} />
      <SidebarItem label={en.NavReports} icon="reports" selected collapsed onactivated={() => {}} />
    </div>
  </section>

  <section class="section">
    <h2>PeriodNavigator</h2>
    <div class="column">
      <PeriodNavigator
        title={en.PeriodThisMonth}
        previousLabel={en.PreviousMonth}
        nextLabel={en.NextMonth}
        onprevious={() => (previousCount += 1)}
        onnext={() => (nextCount += 1)}
      />
      <output data-testid="period-count">{previousCount} / {nextCount}</output>
      <PeriodNavigator
        title={en.PeriodLastMonth}
        previousLabel={en.PreviousMonth}
        nextLabel={en.NextMonth}
        previousEnabled={false}
        onprevious={() => (previousCount += 1)}
        onnext={() => (nextCount += 1)}
      />
      <PeriodNavigator
        title={en.PeriodThisYear}
        previousLabel={en.PreviousYear}
        nextLabel={en.NextYear}
        nextEnabled={false}
        onprevious={() => (previousCount += 1)}
        onnext={() => (nextCount += 1)}
      />
      <PeriodNavigator
        title={en.CurrentMonth}
        previousLabel={en.PreviousMonth}
        nextLabel={en.NextMonth}
        titleHasMenu
        ontitleclicked={() => {}}
      />
    </div>
  </section>

  <section class="section">
    <h2>Tooltip</h2>
    <div class="row">
      <Tooltip text={en.More}>
        <SecondaryButton text={en.More} icon="more_vert" onclick={() => {}} />
      </Tooltip>
    </div>
  </section>

  <section class="section">
    <h2>DataTable</h2>
    <DataTable
      {columns}
      {rows}
      selectedId={selectedRow}
      onrowselected={(id) => (selectedRow = id)}
    />
  </section>

  <section class="section">
    <h2>Dialog</h2>
    <div class="row">
      <SecondaryButton text={en.Open} onclick={() => (dialogOpen = true)} />
    </div>

    <Dialog bind:open={dialogOpen} title={en.DeleteAccountTitle} closeLabel={en.Close}>
      {#snippet footer()}
        <SecondaryButton text={en.Cancel} onclick={() => (dialogOpen = false)} />
        <PrimaryButton text={en.Delete} destructive onclick={() => (dialogOpen = false)} />
      {/snippet}
      <p>{en.NoAccountsYet}</p>
    </Dialog>
  </section>
</main>

<style>
  .gallery {
    max-width: 1100px;
    margin: 0 auto;
    padding: var(--space-xl);
    display: flex;
    flex-direction: column;
    gap: var(--space-lg);
  }

  .masthead {
    padding-bottom: var(--space-md);
    border-bottom: 1px solid var(--color-rule);
  }

  .masthead h1 {
    margin: 0 0 var(--space-xs);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .masthead p {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
  }

  .section {
    padding: var(--space-md);
    border: 1px solid var(--color-rule);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .section h2 {
    margin: 0 0 var(--space-md);
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-sm);
  }

  .column {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-sm);
  }

  .grid {
    display: grid;
    gap: var(--space-md);
  }

  .fields {
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  }

  .card-body {
    padding: var(--space-md);
  }

  .card-body h3 {
    margin: 0 0 var(--space-xs);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
  }

  .card-body p {
    margin: 0;
    font-size: var(--type-figure-size);
    font-weight: var(--type-figure-weight);
  }

  .sidebar-sample {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-xs);
    max-width: 520px;
  }

  output {
    color: var(--color-muted-text);
    font-size: var(--type-caption-size);
    font-weight: var(--type-caption-weight);
  }
</style>
