<script lang="ts">
  import { onMount } from 'svelte';
  import type { Api, DocumentsDTO, ShellDTO } from '../../lib/api';
  import {
    Card,
    MessageBanner,
    PrimaryButton,
    SearchField,
    SegmentedButton,
  } from '../../lib/components';
  import { languageFromCode, translate, type FormatArg, type Language } from '../../lib/i18n';
  import DeleteDocumentDialog from './documents/DeleteDocumentDialog.svelte';
  import DocumentDetail from './documents/DocumentDetail.svelte';
  import DocumentRow from './documents/DocumentRow.svelte';

  let {
    api,
    dto: shell,
  }: {
    api: Api;
    dto: ShellDTO;
  } = $props();

  let dto = $state<DocumentsDTO | null>(null);
  let loadFailed = $state(false);
  let searchText = $state('');
  let deleteOpen = $state(false);
  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  const language = $derived<Language>(languageFromCode(shell.language));
  const libraryEmpty = $derived(
    dto !== null &&
      !dto.loading &&
      dto.rows.length === 0 &&
      dto.searchQuery === '' &&
      dto.tab === 'all',
  );
  const tabIndex = $derived(dto?.tab === 'unlinked' ? 1 : dto?.tab === 'linked' ? 2 : 0);
  const tabs = $derived([
    dto?.tabAllLabel ?? '',
    dto?.tabUnlinkedLabel ?? '',
    dto?.tabLinkedLabel ?? '',
  ]);

  function t(key: string, ...args: FormatArg[]): string {
    return translate(language, key, ...args);
  }

  function apply(next: DocumentsDTO, syncSearch = false): void {
    dto = next;
    if (syncSearch) {
      searchText = next.searchQuery;
    }
  }

  async function run(action: () => Promise<DocumentsDTO>): Promise<void> {
    try {
      apply(await action());
      loadFailed = false;
    } catch {
      loadFailed = true;
    }
  }

  function searchChanged(text: string): void {
    searchText = text;
    if (searchTimer !== undefined) {
      clearTimeout(searchTimer);
    }
    searchTimer = setTimeout(() => {
      searchTimer = undefined;
      void run(() => api.documentsSearch(text));
    }, 250);
  }

  function setTab(index: number): void {
    void run(() => api.documentsSetTab(index === 0 ? 'all' : index === 1 ? 'unlinked' : 'linked'));
  }

  function closeTopLayer(): void {
    if (deleteOpen) {
      deleteOpen = false;
      return;
    }
    if (dto?.hasSelection === true) {
      void run(() => api.documentsCloseDetail());
    }
  }

  onMount(() => {
    let disposed = false;
    const stop = api.onDocumentsChanged((next) => {
      if (!disposed) {
        apply(next);
      }
    });

    void api
      .documentsState()
      .then((next) => {
        if (!disposed) {
          apply(next, true);
        }
      })
      .catch(() => {
        if (!disposed) {
          loadFailed = true;
        }
      });

    return () => {
      disposed = true;
      stop();
      if (searchTimer !== undefined) {
        clearTimeout(searchTimer);
      }
    };
  });
</script>

<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      closeTopLayer();
    }
  }}
/>

<div class="documents-page" data-testid="documents-page">
  {#if dto === null}
    {#if loadFailed}
      <MessageBanner
        text={t('ErrorUnexpected')}
        isError
        dismissLabel={t('Dismiss')}
        ondismiss={() => (loadFailed = false)}
      />
    {:else}
      <div class="loading" aria-hidden="true"><span class="spinner"></span></div>
    {/if}
  {:else}
    <div class="toolbar">
      <div class="search-column" data-page-search>
        {#if !libraryEmpty}
          <SearchField
            value={searchText}
            placeholder={t('SearchDocuments')}
            clearLabel={t('ClearSearch')}
            enabled={!dto.uploading}
            oninput={searchChanged}
          />
        {/if}
        {#if !dto.ocrAvailable}
          <p class="ocr-hint">{dto.ocrHint}</p>
        {/if}
      </div>

      {#if dto.uploading}
        <div class="upload-status" role="status" data-testid="documents-upload-progress">
          <span class="spinner" aria-hidden="true"></span>
          <span>{dto.progressText}</span>
        </div>
      {/if}

      {#if !libraryEmpty}
        <PrimaryButton
          text={t('Upload')}
          icon="add"
          enabled={!dto.uploading}
          onclick={() => run(() => api.documentsUpload())}
        />
      {/if}
    </div>

    {#if dto.errorText !== '' || dto.messageText !== ''}
      <MessageBanner
        text={dto.errorText !== '' ? dto.errorText : dto.messageText}
        isError={dto.errorText !== ''}
        dismissLabel={t('Dismiss')}
        ondismiss={() => run(() => api.documentsDismiss())}
      />
    {/if}

    <div class="tabs">
      <SegmentedButton labels={tabs} selected={tabIndex} onselect={setTab} />
    </div>

    <div class="content">
      <div class="list-column" class:with-detail={dto.hasSelection}>
        <Card>
          {#if dto.loading}
            <div class="list-loading" aria-hidden="true"><span class="spinner small"></span></div>
          {:else if dto.rows.length === 0}
            <div class="empty" data-testid="documents-empty">
              <div class="empty-icon" aria-hidden="true">
                <span class="empty-line line-one"></span>
                <span class="empty-line line-two"></span>
                <span class="empty-line line-three"></span>
              </div>
              <h2>{dto.searchQuery === '' ? t('NoDocuments') : t('NoMatches')}</h2>
              <p class="empty-hint">
                {dto.searchQuery === '' ? t('UploadHint') : t('TryAnotherSearch')}
              </p>
              {#if dto.searchQuery === '' && dto.tab === 'all'}
                <div class="empty-upload">
                  <PrimaryButton
                    text={t('Upload')}
                    icon="add"
                    enabled={!dto.uploading}
                    onclick={() => run(() => api.documentsUpload())}
                  />
                </div>
              {/if}
            </div>
          {:else}
            <div class="list-scroll">
              {#each dto.rows as row (row.id)}
                <DocumentRow {row} {t} onselect={(id) => run(() => api.documentsSelect(id))} />
              {/each}
            </div>
          {/if}
        </Card>
      </div>

      {#if dto.hasSelection}
        <div class="detail-column">
          <Card>
            <DocumentDetail
              {dto}
              {t}
              onclose={() => run(() => api.documentsCloseDetail())}
              onopen={() => run(() => api.documentsOpen())}
              onreprocess={() => run(() => api.documentsReprocess())}
              ondelete={() => (deleteOpen = true)}
              onunlink={() => run(() => api.documentsUnlink())}
              onlink={(id) => run(() => api.documentsLink(id))}
              oncandidatequery={(query) => run(() => api.documentsCandidateQueryChanged(query))}
            />
          </Card>
        </div>
      {/if}
    </div>

    {#if deleteOpen && dto.hasSelection}
      <DeleteDocumentDialog
        title={t('DeleteDocumentTitle')}
        text={dto.deleteText}
        {t}
        onconfirm={() => {
          deleteOpen = false;
          void run(() => api.documentsConfirmDelete());
        }}
        oncancel={() => (deleteOpen = false)}
      />
    {/if}
  {/if}
</div>

<style>
  .documents-page {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    min-height: 0;
    padding: 24px;
    overflow: hidden;
    background: var(--color-background);
  }

  .toolbar {
    display: flex;
    flex: none;
    align-items: flex-start;
    gap: 12px;
    min-height: var(--size-control-height);
  }

  .search-column {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 6px;
  }

  .ocr-hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .upload-status {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: 8px;
    height: var(--size-control-height);
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    white-space: nowrap;
  }

  .upload-status .spinner {
    width: 20px;
    height: 20px;
    border-width: 2px;
  }

  .tabs {
    display: flex;
    flex: none;
    align-items: flex-start;
    justify-content: flex-start;
  }

  .content {
    display: flex;
    flex: 1;
    min-height: 0;
    gap: 16px;
  }

  .list-column {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }

  .list-column.with-detail {
    flex: 467 1 0;
  }

  .detail-column {
    display: flex;
    flex: 328 1 0;
    min-width: 0;
    min-height: 0;
  }

  .list-column :global(.card),
  .detail-column :global(.card) {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
    flex-direction: column;
    overflow: hidden;
  }

  .list-scroll {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    overflow-y: auto;
  }

  .list-loading {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
  }

  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 48px;
    text-align: center;
  }

  .empty-icon {
    position: relative;
    width: 48px;
    height: 48px;
    margin-bottom: 8px;
    border: 2px solid var(--color-muted-text);
    border-radius: var(--radius-panel);
  }

  .empty-line {
    position: absolute;
    left: 10px;
    height: 4px;
    border-radius: var(--radius-hairline);
    background: var(--color-muted-text);
  }

  .empty-line.line-one {
    top: 10px;
    width: 24px;
  }

  .empty-line.line-two {
    top: 20px;
    width: 20px;
  }

  .empty-line.line-three {
    top: 30px;
    width: 24px;
  }

  .empty h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .empty-hint {
    width: min(440px, 100%);
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }

  .empty-upload {
    display: flex;
    width: 160px;
    margin-top: 8px;
  }

  .empty-upload :global(.primary) {
    flex: 1;
  }

  .loading {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
  }

  .spinner {
    display: block;
    width: 32px;
    height: 32px;
    border: 3px solid var(--color-divider);
    border-top-color: var(--color-primary);
    border-radius: 50%;
    animation: spin 900ms linear infinite;
  }

  .spinner.small {
    width: 24px;
    height: 24px;
    border-width: 2px;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
