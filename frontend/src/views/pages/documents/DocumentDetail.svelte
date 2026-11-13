<script lang="ts">
  import type { DocumentsDTO } from '../../../lib/api';
  import { SecondaryButton, SearchField, TextButton } from '../../../lib/components';
  import DocumentLinkRow from './DocumentLinkRow.svelte';

  let {
    dto,
    t,
    onclose,
    onopen,
    onreprocess,
    ondelete,
    onunlink,
    onlink,
    oncandidatequery,
  }: {
    dto: DocumentsDTO;
    t: (key: string, ...args: (string | number)[]) => string;
    onclose: () => void;
    onopen: () => void;
    onreprocess: () => void;
    ondelete: () => void;
    onunlink: () => void;
    onlink: (id: number) => void;
    oncandidatequery: (query: string) => void;
  } = $props();
</script>

<div class="detail" data-testid="documents-detail">
  <div class="header">
    <h2>{dto.selectedName}</h2>
    <TextButton text={t('Close')} onclick={onclose} />
  </div>

  <div class="actions">
    <div class="action-row">
      <SecondaryButton text={t('Open')} onclick={onopen} />
      <SecondaryButton text={t('ReExtractText')} enabled={!dto.uploading} onclick={onreprocess} />
    </div>
    <div class="action-row">
      <SecondaryButton text={t('Delete')} onclick={ondelete} />
    </div>
  </div>

  {#if dto.previewKind === 'image' && dto.previewDataUrl !== ''}
    <div class="preview-image">
      <img src={dto.previewDataUrl} alt={t('PreviewOf', dto.selectedName)} />
    </div>
  {:else if dto.previewKind === 'pdf'}
    <div class="pdf-preview" data-testid="documents-pdf-preview" aria-hidden="true">
      <div class="pdf-page">
        <span class="pdf-line line-one"></span>
        <span class="pdf-line line-two"></span>
        <span class="pdf-label"></span>
      </div>
    </div>
  {/if}

  <span class="linked-label">{t('LinkedTransaction')}</span>

  {#if dto.selectedLinked}
    <div class="linked-row">
      <span class="linked-name">{dto.selectedLinkLabel}</span>
      <TextButton text={t('Unlink')} onclick={onunlink} />
    </div>
  {:else}
    <div class="unlinked">
      {#if dto.suggestions.length > 0}
        <p class="suggestions-label">{t('SuggestedMatches')}</p>
      {/if}
      {#each dto.suggestions as suggestion (suggestion.id)}
        <DocumentLinkRow row={suggestion} {t} {onlink} />
      {/each}

      <div class="candidate-search">
        <SearchField
          value={dto.candidateQuery}
          placeholder={t('FindTransactionToLink')}
          clearLabel={t('ClearSearch')}
          oninput={oncandidatequery}
        />
      </div>

      {#if dto.candidateQuery !== ''}
        <div class="candidates" data-testid="documents-candidates">
          {#each dto.candidates as candidate (candidate.id)}
            <DocumentLinkRow row={candidate} {t} {onlink} />
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <span class="ocr-heading">{dto.selectedOcrHeading}</span>
  <div class="ocr-box">
    <p class="ocr-text">
      {dto.selectedOcrText === '' ? t('NoTextExtracted') : dto.selectedOcrText}
    </p>
  </div>
</div>

<style>
  .detail {
    display: flex;
    flex: 1;
    min-height: 0;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    overflow-y: auto;
    scrollbar-width: none;
  }

  .detail::-webkit-scrollbar {
    width: 0;
    height: 0;
  }

  .header {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
  }

  h2 {
    flex: 1;
    min-width: 0;
    margin: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-title-medium-size);
    font-weight: var(--type-title-medium-weight);
    line-height: 1.3;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .actions {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 8px;
    align-items: flex-start;
  }

  .action-row {
    display: flex;
    gap: 8px;
    align-items: flex-start;
  }

  .preview-image {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    height: 220px;
  }

  .preview-image img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .pdf-preview {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    height: 120px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-background);
  }

  .pdf-page {
    position: relative;
    display: block;
    width: 52px;
    height: 66px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-thumbnail);
    background: var(--color-surface);
  }

  .pdf-line {
    position: absolute;
    left: 8px;
    width: 34px;
    height: 3px;
    border-radius: var(--radius-hairline);
    background: var(--color-muted-text);
  }

  .line-one {
    top: 10px;
  }

  .line-two {
    top: 18px;
  }

  .pdf-label {
    position: absolute;
    right: 8px;
    bottom: 8px;
    left: 8px;
    height: 10px;
    border-radius: var(--radius-hairline);
    background: var(--color-document-pdf);
  }

  .linked-label {
    flex: none;
    color: var(--color-muted-text);
    font-size: var(--type-label-small-size);
    font-weight: var(--type-label-small-weight);
  }

  .linked-row {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
  }

  .linked-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .unlinked {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 8px;
  }

  .suggestions-label {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.35;
  }

  .candidate-search {
    flex: none;
  }

  .candidates {
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .ocr-heading {
    flex: none;
    color: var(--color-text);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .ocr-box {
    flex: none;
    padding: 12px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-background);
  }

  .ocr-text {
    margin: 0;
    color: var(--color-text);
    font-family: monospace;
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
