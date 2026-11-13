import { describe, expect, it } from 'vitest';
import type { DocumentsDTO } from '../../src/lib/api';
import { createMockDocuments } from '../../src/lib/documents.mock';
import { translate } from '../../src/lib/i18n';

function fake(scenario = 'documents-en-light', language = 'en') {
  return createMockDocuments(scenario, language);
}

describe('documents.mock', () => {
  it('serves the reference rows, tabs and selected receipt', () => {
    const dto = fake().state();
    expect(dto.rows.map((row) => row.name)).toEqual([
      'baeckerei-beleg.png',
      'stadtwerke-rechnung.pdf',
      'kassenbon.jpg',
    ]);
    expect(dto.rows.map((row) => row.isPdf)).toEqual([false, true, false]);
    expect(dto.rows.map((row) => row.hasOcr)).toEqual([false, true, true]);
    expect(dto.rows[0]?.meta).toBe('12.11.2026 18:46 · not linked');
    expect(dto.rows[1]?.meta).toBe('12.11.2026 18:46 · Interest (2026-10-07)');
    expect(dto.rows.map((row) => row.selected)).toEqual([false, false, true]);

    expect(dto.tab).toBe('all');
    expect(dto.tabAllLabel).toBe('All documents (3)');
    expect(dto.tabUnlinkedLabel).toBe('Unlinked (2)');
    expect(dto.tabLinkedLabel).toBe('Linked to transaction (1)');

    expect(dto.hasSelection).toBe(true);
    expect(dto.selectedName).toBe('kassenbon.jpg');
    expect(dto.selectedLinked).toBe(false);
    expect(dto.selectedOcrHeading).toBe('Extracted text (confidence 84%)');
    expect(dto.selectedOcrText).toBe('REWE Markt GmbH\nDatum: 13.03.2026\nSUMME EUR 3,78');
    expect(dto.previewKind).toBe('image');
    expect(dto.previewDataUrl).toMatch(/^data:image\/png;base64,/);
    expect(dto.deleteText).toBe('kassenbon.jpg and its stored copy will be deleted.');

    expect(dto.suggestions).toHaveLength(1);
    expect(dto.suggestions[0]).toMatchObject({
      id: 1,
      title: 'REWE Markt GmbH',
      meta: '13.03.2026 · -€3.78',
      badge: 'match 80%',
    });
    expect(dto.candidates).toEqual([]);
    expect(dto.candidateQuery).toBe('');
  });

  it('searches names and OCR text, and counts the current search', () => {
    const documents = fake();
    const text = documents.search('rewe');
    expect(text.rows.map((row) => row.name)).toEqual(['kassenbon.jpg']);
    expect(text.tabAllLabel).toBe('All documents (1)');
    expect(text.tabUnlinkedLabel).toBe('Unlinked (1)');
    expect(text.tabLinkedLabel).toBe('Linked to transaction (0)');

    const file = documents.search('stadtwerke');
    expect(file.rows.map((row) => row.name)).toEqual(['stadtwerke-rechnung.pdf']);
    expect(file.hasSelection).toBe(false);

    const none = documents.search('nothing-like-this');
    expect(none.rows).toEqual([]);
    expect(none.tabAllLabel).toBe('All documents (0)');
  });

  it('selects, filters by tab and closes the detail', () => {
    const documents = fake();

    const unlinked = documents.setTab('unlinked');
    expect(unlinked.rows.map((row) => row.name)).toEqual(['baeckerei-beleg.png', 'kassenbon.jpg']);
    expect(unlinked.hasSelection).toBe(true);

    const linked = documents.setTab('linked');
    expect(linked.rows.map((row) => row.name)).toEqual(['stadtwerke-rechnung.pdf']);
    expect(linked.hasSelection).toBe(false);
    expect(linked.previewKind).toBe('none');

    documents.setTab('all');
    const selected = documents.select(2);
    expect(selected.selectedName).toBe('stadtwerke-rechnung.pdf');
    expect(selected.selectedLinked).toBe(true);
    expect(selected.selectedLinkLabel).toBe('Interest (2026-10-07)');
    expect(selected.previewKind).toBe('pdf');
    expect(selected.previewDataUrl).toBe('');
    expect(selected.suggestions).toEqual([]);

    const closed = documents.closeDetail();
    expect(closed.hasSelection).toBe(false);
    expect(closed.previewKind).toBe('none');
  });

  it('links, unlinks, reprocesses and deletes the selected document', () => {
    const documents = fake();

    const linked = documents.link(1);
    expect(linked.selectedLinked).toBe(true);
    expect(linked.selectedLinkLabel).toBe('REWE Markt GmbH (2026-03-13)');
    expect(linked.rows[2]?.isLinked).toBe(true);
    expect(linked.messageText).toBe(translate('en', 'MessageDocumentLinked'));
    expect(linked.suggestions).toEqual([]);

    const unlinked = documents.unlink();
    expect(unlinked.selectedLinked).toBe(false);
    expect(unlinked.rows[2]?.isLinked).toBe(false);
    expect(unlinked.suggestions).toHaveLength(1);

    const reprocessed = documents.reprocess();
    expect(reprocessed.messageText).toBe(translate('en', 'TextExtracted'));

    const deleted = documents.confirmDelete();
    expect(deleted.rows.map((row) => row.name)).toEqual([
      'baeckerei-beleg.png',
      'stadtwerke-rechnung.pdf',
    ]);
    expect(deleted.hasSelection).toBe(false);
    expect(deleted.messageText).toBe(translate('en', 'MessageDocumentDeleted'));
  });

  it('filters the link candidates by the candidate query', () => {
    const documents = fake('documents-candidates-en');
    const rewe = documents.state();
    expect(rewe.candidateQuery).toBe('rewe');
    expect(rewe.candidates.map((row) => row.title)).toEqual(['REWE Markt GmbH']);

    const energy = documents.candidateQueryChanged('energie');
    expect(energy.candidates.map((row) => row.title)).toEqual(['Beispiel Energie AG']);

    const empty = documents.candidateQueryChanged('   ');
    expect(empty.candidates).toEqual([]);
  });

  it('uploads a synthetic document and opens the selection', () => {
    const documents = fake();
    expect(documents.state().rows).toHaveLength(3);

    const uploaded = documents.upload();
    expect(uploaded.rows).toHaveLength(4);
    expect(uploaded.rows[3]?.name).toBe('einkauf-quittung.png');
    expect(uploaded.messageText).toBe(translate('en', 'MessageUploaded', 1, 1));

    const opened = documents.open();
    expect(opened.messageText).toBe(translate('en', 'OpenPreviewNamed', 'kassenbon.jpg'));

    const dismissed = documents.dismiss();
    expect(dismissed.messageText).toBe('');
  });

  it('serves the empty, loading, uploading, message and OCR states', () => {
    const empty = fake('documents-empty-en').state();
    expect(empty.rows).toEqual([]);
    expect(empty.hasSelection).toBe(false);
    expect(empty.tabAllLabel).toBe('All documents (0)');

    const loading = fake('documents-loading-en').state();
    expect(loading.loading).toBe(true);
    expect(loading.rows).toEqual([]);

    const uploading = fake('documents-uploading-en').state();
    expect(uploading.uploading).toBe(true);
    expect(uploading.progressText).toBe('Uploading 1 of 2…');

    const message = fake('documents-message-en').state();
    expect(message.messageText).toBe(translate('en', 'MessageDocumentLinked'));

    const error = fake('documents-error-en').state();
    expect(error.errorText).toBe(translate('en', 'ErrorUnexpected'));

    const withoutOcr = fake('documents-ocr-unavailable-en').state();
    expect(withoutOcr.ocrAvailable).toBe(false);
    expect(withoutOcr.ocrHint).toBe(
      translate(
        'en',
        'OcrNotInstalled',
        `${translate('en', 'NavSettings')} → ${translate('en', 'DataAndOcr')}`,
      ),
    );

    const noMatch = fake('documents-no-match-en').state();
    expect(noMatch.rows).toEqual([]);
    expect(noMatch.searchQuery).not.toBe('');
  });

  it('speaks German without changing the reference data', () => {
    const dto = fake('documents-de-dark', 'de').state();
    expect(dto.tabAllLabel).toBe('Alle Dokumente (3)');
    expect(dto.tabUnlinkedLabel).toBe('Unverknüpft (2)');
    expect(dto.tabLinkedLabel).toBe('Mit Umsatz verknüpft (1)');
    expect(dto.rows[0]?.meta).toBe('12.11.2026 18:46 · nicht verknüpft');
    expect(dto.selectedOcrHeading).toBe('Erkannter Text (Konfidenz 84%)');
    expect(dto.suggestions[0]?.badge).toBe('Treffer 80\u00a0%');
    expect(dto.suggestions[0]?.meta).toBe('13.03.2026 · -3,78\u00a0€');
    expect(dto.deleteText).toBe('kassenbon.jpg und die gespeicherte Kopie werden gelöscht.');
  });

  it('publishes every action on documents:changed and retranslates on a language change', () => {
    const documents = fake();
    const seen: DocumentsDTO[] = [];
    const stop = documents.subscribe((next) => seen.push(next));

    documents.setTab('unlinked');
    documents.search('kassenbon');
    documents.select(3);
    documents.candidateQueryChanged('rewe');
    documents.link(1);
    documents.unlink();
    documents.reprocess();
    documents.confirmDelete();
    documents.upload();
    documents.open();
    documents.dismiss();

    expect(seen).toHaveLength(11);
    expect(seen[0]?.tab).toBe('unlinked');
    expect(seen[1]?.searchQuery).toBe('kassenbon');

    const translated = documents.setLanguage('de');
    expect(translated.tabAllLabel).toContain('Alle Dokumente');
    expect(seen).toHaveLength(12);
    expect(seen.at(-1)?.tabAllLabel).toContain('Alle Dokumente');

    stop();
    documents.setTab('all');
    expect(seen).toHaveLength(12);
  });
});
