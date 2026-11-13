import type { DocumentLinkRowDTO, DocumentRowDTO, DocumentsDTO } from './api';
import { languageFromCode, translate, type Language } from './i18n';

// The mock preview is a small generated image, not a receipt: a teal frame
// around a pale sheet, the same shape the Slint screenshot fixture draws.
const previewDataUrl =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAHgAAABQCAIAAABd+SbeAAAAnklEQVR42u3dMQ0AIBAEwTeAbwS8VUJChQDKDzRMstWVY+CiZdeDAgFo0AINGvQxjblUDDRo0AINGjRo0KAFGjRo0KBBCzRo0KBBgxZo0KBBgwYt0KBBgwYNWqBBgwYNGrRAgwYNGjRogQYNGjRo0AINGjRo0KAFGjRo0KBBCzRo0KBBgxZo0KBBgwYt0KBBgwb9B7S8VoAWaNCgdaENluFk5Bcq9sIAAAAASUVORK5CYII=';

type MockLinkedTransaction = {
  id: number;
  label: string;
};

type MockDocument = {
  id: number;
  fileName: string;
  uploadedAt: string;
  isPdf: boolean;
  ocrText: string;
  confidence: number | null;
  previewKind: 'image' | 'pdf' | 'none';
  linked: MockLinkedTransaction | null;
};

type MockCandidate = {
  id: number;
  title: string;
  date: string;
  amount: string;
  amountDe: string;
  linkLabel: string;
};

type MockDocumentsVariant =
  | 'normal'
  | 'empty'
  | 'linked'
  | 'candidates'
  | 'uploading'
  | 'message'
  | 'error'
  | 'ocr-unavailable'
  | 'loading'
  | 'no-match';

type MockDocumentsState = {
  documents: MockDocument[];
  tab: 'all' | 'unlinked' | 'linked';
  searchQuery: string;
  selectedId: number | null;
  candidateQuery: string;
  uploading: boolean;
  uploadDone: number;
  uploadTotal: number;
  loading: boolean;
  errorText: string;
  messageText: string;
  ocrAvailable: boolean;
  language: Language;
};

const suggestionCandidate: MockCandidate = {
  id: 1,
  title: 'REWE Markt GmbH',
  date: '13.03.2026',
  amount: '-€3.78',
  amountDe: '-3,78\u00a0€',
  linkLabel: 'REWE Markt GmbH (2026-03-13)',
};

const candidates: MockCandidate[] = [
  suggestionCandidate,
  {
    id: 2,
    title: 'Beispiel Energie AG',
    date: '05.11.2026',
    amount: '-€37.80',
    amountDe: '-37,80\u00a0€',
    linkLabel: 'Beispiel Energie AG (2026-11-05)',
  },
  {
    id: 3,
    title: 'Muster Bäckerei',
    date: '12.11.2026',
    amount: '-€4.20',
    amountDe: '-4,20\u00a0€',
    linkLabel: 'Muster Bäckerei (2026-11-12)',
  },
];

function variantOf(scenario: string): MockDocumentsVariant {
  switch (scenario) {
    case 'documents-empty-en':
      return 'empty';
    case 'documents-linked-en-light':
      return 'linked';
    case 'documents-candidates-en':
      return 'candidates';
    case 'documents-uploading-en':
      return 'uploading';
    case 'documents-message-en':
      return 'message';
    case 'documents-error-en':
      return 'error';
    case 'documents-ocr-unavailable-en':
      return 'ocr-unavailable';
    case 'documents-loading-en':
      return 'loading';
    case 'documents-no-match-en':
      return 'no-match';
    default:
      return 'normal';
  }
}

function normalDocuments(): MockDocument[] {
  return [
    {
      id: 1,
      fileName: 'baeckerei-beleg.png',
      uploadedAt: '12.11.2026 18:46',
      isPdf: false,
      ocrText: '',
      confidence: null,
      previewKind: 'image',
      linked: null,
    },
    {
      id: 2,
      fileName: 'stadtwerke-rechnung.pdf',
      uploadedAt: '12.11.2026 18:46',
      isPdf: true,
      ocrText: 'Stadtwerke Musterstadt\nAbschlag März 2026\nBetrag 37,80 EUR',
      confidence: 91,
      previewKind: 'pdf',
      linked: { id: 7, label: 'Interest (2026-10-07)' },
    },
    {
      id: 3,
      fileName: 'kassenbon.jpg',
      uploadedAt: '12.11.2026 18:46',
      isPdf: false,
      ocrText: 'REWE Markt GmbH\nDatum: 13.03.2026\nSUMME EUR 3,78',
      confidence: 84,
      previewKind: 'image',
      linked: null,
    },
  ];
}

function initialState(scenario: string, language: Language): MockDocumentsState {
  const variant = variantOf(scenario);
  const state: MockDocumentsState = {
    documents: variant === 'empty' || variant === 'loading' ? [] : normalDocuments(),
    tab: 'all',
    searchQuery: '',
    selectedId: variant === 'linked' ? 2 : variant === 'empty' || variant === 'loading' ? null : 3,
    candidateQuery: '',
    uploading: false,
    uploadDone: 0,
    uploadTotal: 0,
    loading: false,
    errorText: '',
    messageText: '',
    ocrAvailable: true,
    language,
  };

  switch (variant) {
    case 'candidates':
      state.candidateQuery = 'rewe';
      break;
    case 'uploading':
      state.uploading = true;
      state.uploadDone = 1;
      state.uploadTotal = 2;
      break;
    case 'message':
      state.messageText = translate(language, 'MessageDocumentLinked');
      break;
    case 'error':
      state.errorText = translate(language, 'ErrorUnexpected');
      break;
    case 'ocr-unavailable':
      state.ocrAvailable = false;
      break;
    case 'loading':
      state.loading = true;
      break;
    case 'no-match':
      state.searchQuery = 'nichts wie dies';
      state.selectedId = null;
      break;
    default:
      break;
  }

  return state;
}

function searchMatches(document: MockDocument, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === '') {
    return true;
  }
  return (
    document.fileName.toLowerCase().includes(needle) ||
    document.ocrText.toLowerCase().includes(needle)
  );
}

/**
 * The Documents page's stateful mock fake. It serves the three reference
 * screenshots and the detail states, and publishes documents:changed after
 * every action, as the Go service does.
 */
export function createMockDocuments(scenario = '', languageCode = 'en') {
  const state = initialState(scenario, languageFromCode(languageCode));
  const listeners = new Set<(dto: DocumentsDTO) => void>();

  function visibleDocuments(): MockDocument[] {
    return state.documents.filter((document) => {
      if (!searchMatches(document, state.searchQuery)) {
        return false;
      }
      if (state.tab === 'unlinked') {
        return document.linked === null;
      }
      if (state.tab === 'linked') {
        return document.linked !== null;
      }
      return true;
    });
  }

  function selectedDocument(): MockDocument | null {
    if (state.selectedId === null) {
      return null;
    }
    return visibleDocuments().find((document) => document.id === state.selectedId) ?? null;
  }

  function tabLabel(key: string, count: number): string {
    return translate(state.language, 'TabWithCount', translate(state.language, key), count);
  }

  function row(document: MockDocument): DocumentRowDTO {
    const linkLabel = document.linked?.label ?? translate(state.language, 'NotLinked');
    return {
      id: document.id,
      name: document.fileName,
      meta: `${document.uploadedAt} · ${linkLabel}`,
      isLinked: document.linked !== null,
      hasOcr: document.ocrText !== '',
      isPdf: document.isPdf,
      selected: state.selectedId === document.id,
    };
  }

  function linkRow(candidate: MockCandidate, badge: string): DocumentLinkRowDTO {
    const amount = state.language === 'en' ? candidate.amount : candidate.amountDe;
    return {
      id: candidate.id,
      title: candidate.title,
      meta: `${candidate.date} · ${amount}`,
      badge,
    };
  }

  function suggestionRows(): DocumentLinkRowDTO[] {
    const selected = selectedDocument();
    if (selected === null || selected.linked !== null) {
      return [];
    }
    const percent = state.language === 'en' ? '80%' : '80\u00a0%';
    return [linkRow(suggestionCandidate, translate(state.language, 'MatchPercent', percent))];
  }

  function candidateRows(): DocumentLinkRowDTO[] {
    const needle = state.candidateQuery.trim().toLowerCase();
    if (needle === '') {
      return [];
    }
    return candidates
      .filter(
        (candidate) =>
          candidate.title.toLowerCase().includes(needle) ||
          candidate.amount.toLowerCase().includes(needle) ||
          candidate.amountDe.toLowerCase().includes(needle),
      )
      .slice(0, 8)
      .map((candidate) => linkRow(candidate, ''));
  }

  function progressText(): string {
    if (!state.uploading || state.uploadTotal === 0) {
      return '';
    }
    return translate(state.language, 'UploadProgress', state.uploadDone, state.uploadTotal);
  }

  function ocrHint(): string {
    if (state.ocrAvailable) {
      return '';
    }
    const settings =
      translate(state.language, 'NavSettings') + ' → ' + translate(state.language, 'DataAndOcr');
    return translate(state.language, 'OcrNotInstalled', settings);
  }

  function dto(): DocumentsDTO {
    const documents = visibleDocuments();
    const searched = state.documents.filter((document) =>
      searchMatches(document, state.searchQuery),
    );
    const selected = selectedDocument();
    return {
      rows: documents.map(row),
      tab: state.tab,
      tabAllLabel: tabLabel('TabAllDocuments', searched.length),
      tabUnlinkedLabel: tabLabel(
        'TabUnlinked',
        searched.filter((document) => document.linked === null).length,
      ),
      tabLinkedLabel: tabLabel(
        'TabLinked',
        searched.filter((document) => document.linked !== null).length,
      ),
      searchQuery: state.searchQuery,
      ocrAvailable: state.ocrAvailable,
      ocrHint: ocrHint(),
      uploading: state.uploading,
      progressText: progressText(),
      loading: state.loading,
      errorText: state.errorText,
      messageText: state.messageText,
      hasSelection: selected !== null,
      selectedName: selected?.fileName ?? '',
      selectedLinked: selected?.linked != null,
      selectedLinkLabel: selected?.linked?.label ?? '',
      selectedOcrHeading:
        selected === null
          ? ''
          : selected.confidence !== null
            ? translate(state.language, 'ExtractedTextConfidence', selected.confidence)
            : translate(state.language, 'ExtractedText'),
      selectedOcrText: selected?.ocrText ?? '',
      previewKind: selected?.previewKind ?? 'none',
      previewDataUrl: selected?.previewKind === 'image' ? previewDataUrl : '',
      suggestions: suggestionRows(),
      candidates: candidateRows(),
      candidateQuery: state.candidateQuery,
      deleteText:
        selected === null ? '' : translate(state.language, 'DeleteDocumentText', selected.fileName),
    };
  }

  function publish(): DocumentsDTO {
    const next = dto();
    for (const listener of listeners) {
      listener(next);
    }
    return next;
  }

  function action(change: () => void): DocumentsDTO {
    change();
    return publish();
  }

  function setLanguage(code: string): DocumentsDTO {
    state.language = languageFromCode(code);
    return publish();
  }

  return {
    state(): DocumentsDTO {
      return dto();
    },
    setLanguage,
    subscribe(callback: (dto: DocumentsDTO) => void): () => void {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
    setTab(tab: string): DocumentsDTO {
      return action(() => {
        state.tab = tab === 'unlinked' || tab === 'linked' ? tab : 'all';
      });
    },
    search(query: string): DocumentsDTO {
      return action(() => {
        state.searchQuery = query;
      });
    },
    select(id: number): DocumentsDTO {
      return action(() => {
        const document = visibleDocuments().find((entry) => entry.id === id);
        state.selectedId = document?.id ?? null;
        state.candidateQuery = '';
      });
    },
    closeDetail(): DocumentsDTO {
      return action(() => {
        state.selectedId = null;
        state.candidateQuery = '';
      });
    },
    candidateQueryChanged(query: string): DocumentsDTO {
      return action(() => {
        state.candidateQuery = query;
      });
    },
    link(transactionID: number): DocumentsDTO {
      return action(() => {
        const selected = selectedDocument();
        if (selected === null) {
          return;
        }
        const candidate = candidates.find((entry) => entry.id === transactionID);
        selected.linked = candidate
          ? { id: candidate.id, label: candidate.linkLabel }
          : { id: transactionID, label: `#${transactionID}` };
        state.messageText = translate(state.language, 'MessageDocumentLinked');
        state.errorText = '';
      });
    },
    unlink(): DocumentsDTO {
      return action(() => {
        const selected = selectedDocument();
        if (selected === null) {
          return;
        }
        selected.linked = null;
        state.candidateQuery = '';
      });
    },
    reprocess(): DocumentsDTO {
      return action(() => {
        const selected = selectedDocument();
        if (selected === null) {
          return;
        }
        state.messageText = translate(
          state.language,
          selected.ocrText.trim() === '' ? 'MessageNoTextExtracted' : 'TextExtracted',
        );
        state.errorText = '';
      });
    },
    confirmDelete(): DocumentsDTO {
      return action(() => {
        const selected = selectedDocument();
        if (selected === null) {
          return;
        }
        state.documents = state.documents.filter((document) => document.id !== selected.id);
        state.selectedId = null;
        state.candidateQuery = '';
        state.messageText = translate(state.language, 'MessageDocumentDeleted');
        state.errorText = '';
      });
    },
    dismiss(): DocumentsDTO {
      return action(() => {
        state.messageText = '';
        state.errorText = '';
      });
    },
    upload(): DocumentsDTO {
      return action(() => {
        const id = Math.max(0, ...state.documents.map((document) => document.id)) + 1;
        state.documents.push({
          id,
          fileName: 'einkauf-quittung.png',
          uploadedAt: '12.11.2026 19:04',
          isPdf: false,
          ocrText: 'Musterladen\nDatum: 12.11.2026\nSUMME EUR 12,40',
          confidence: 87,
          previewKind: 'image',
          linked: null,
        });
        state.uploading = false;
        state.uploadDone = 0;
        state.uploadTotal = 0;
        state.messageText = translate(state.language, 'MessageUploaded', 1, 1);
        state.errorText = '';
      });
    },
    open(): DocumentsDTO {
      return action(() => {
        const selected = selectedDocument();
        if (selected === null) {
          return;
        }
        state.messageText = translate(state.language, 'OpenPreviewNamed', selected.fileName);
        state.errorText = '';
      });
    },
  };
}
