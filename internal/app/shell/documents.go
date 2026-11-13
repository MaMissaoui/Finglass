package shell

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/documentsview"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// DocumentsPageState is one Documents snapshot: the page state plus the values
// that live on Rust's controller rather than in DocumentsState.
type DocumentsPageState struct {
	// State is a deep copy of the page's state.
	State documentsview.DocumentsState
	// PreviewKind is "none", "pdf" or "image", as Rust's Preview holds it.
	PreviewKind string
	// PreviewDataURL is the image preview as a data URL; empty unless the kind
	// is "image".
	PreviewDataURL string
	// CandidateQuery is the text filtering the detail panel's link picker.
	CandidateQuery string
}

// documentsPreview is Rust's Preview: the kind and, for images, the bytes the
// DTO carries as a data URL.
type documentsPreview struct {
	// Kind is "none", "pdf" or "image".
	Kind string
	// DataURL is "data:<image type>;base64,…" for an image.
	DataURL string
}

// documentsJobWork is one Documents action. progress publishes the worker's
// state while an upload runs; current reports whether the job may still touch
// the profile.
type documentsJobWork func(
	ctx context.Context,
	view *documentsview.DocumentsState,
	database *db.Database,
	progress func(),
	current func() bool,
)

// DocumentsState returns a deep copy of the Documents page state.
func (s *Session) DocumentsState() DocumentsPageState {
	s.mu.Lock()
	defer s.mu.Unlock()

	page := DocumentsPageState{
		PreviewKind:    s.documentsPreview.Kind,
		PreviewDataURL: s.documentsPreview.DataURL,
		CandidateQuery: s.documentsCandidateQuery,
	}
	if page.PreviewKind == "" {
		page.PreviewKind = "none"
	}
	if s.documents == nil {
		page.State = *documentsview.New(s.language, false)
		return page
	}
	page.State = *s.documents.Clone()
	return page
}

// DocumentsSetTab applies one of the three filter tabs, as on_documents_set_tab.
func (s *Session) DocumentsSetTab(tab documentsview.DocumentTab) {
	s.mu.Lock()
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.SelectTab(ctx, database, s.paths, tab)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsSearch applies the page's search text, as on_documents_search.
func (s *Session) DocumentsSearch(query string) {
	s.mu.Lock()
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.Search(ctx, database, s.paths, query)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsSelect selects the document with id and loads its detail, as
// on_documents_select. An unknown id clears the selection, exactly as Rust's
// lookup does.
func (s *Session) DocumentsSelect(id int64) {
	s.mu.Lock()
	var document *model.DocumentItem
	if s.documents != nil {
		for index := range s.documents.Documents {
			if s.documents.Documents[index].ID == id {
				cloned := s.documents.Documents[index]
				document = &cloned
				break
			}
		}
	}
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.SelectDocument(ctx, database, s.paths, document)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsCloseDetail closes the detail panel, as on_documents_close_detail.
func (s *Session) DocumentsCloseDetail() {
	s.mu.Lock()
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.SelectDocument(ctx, database, s.paths, nil)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsCandidateQueryChanged stores the link picker's search text, as
// on_documents_candidate_query_changed. While a worker owns the page the call
// only stores it; that worker's publish picks it up.
func (s *Session) DocumentsCandidateQueryChanged(query string) {
	s.mu.Lock()
	s.documentsCandidateQuery = query
	busy := s.documentsWork.Active()
	s.mu.Unlock()
	if busy {
		return
	}
	s.notify(Change{Page: "documents"})
}

// DocumentsLink links the selected document to transactionID, as
// on_documents_link.
func (s *Session) DocumentsLink(transactionID model.TransactionID) {
	s.mu.Lock()
	var id int64
	found := false
	if s.documents != nil && s.documents.SelectedDocument != nil {
		id = s.documents.SelectedDocument.ID
		found = true
	}
	if !found {
		s.mu.Unlock()
		return
	}
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.LinkToTransaction(ctx, database, s.paths, id, transactionID)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsUnlink removes the selected document's link and reloads its detail,
// as on_documents_unlink.
func (s *Session) DocumentsUnlink() {
	s.mu.Lock()
	var id int64
	found := false
	if s.documents != nil && s.documents.SelectedDocument != nil {
		id = s.documents.SelectedDocument.ID
		found = true
	}
	if !found {
		s.mu.Unlock()
		return
	}
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.Unlink(ctx, database, s.paths, id)
		if view.SelectedDocument != nil {
			selected := *view.SelectedDocument
			view.SelectDocument(ctx, database, s.paths, &selected)
		}
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsReprocess runs OCR again for the selected document, as
// on_documents_reprocess.
func (s *Session) DocumentsReprocess() {
	s.mu.Lock()
	var id int64
	found := false
	if s.documents != nil && s.documents.SelectedDocument != nil {
		id = s.documents.SelectedDocument.ID
		found = true
	}
	if !found {
		s.mu.Unlock()
		return
	}
	engine := s.documentsEngine
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.ReprocessOCR(ctx, database, s.paths, id, engine)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsConfirmDelete deletes the selected document, as
// on_documents_confirm_delete.
func (s *Session) DocumentsConfirmDelete() {
	s.mu.Lock()
	var id int64
	found := false
	if s.documents != nil && s.documents.SelectedDocument != nil {
		id = s.documents.SelectedDocument.ID
		found = true
	}
	if !found {
		s.mu.Unlock()
		return
	}
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.Delete(ctx, database, s.paths, id)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsDismiss clears the error and success banner, as on_documents_dismiss.
// Rust's try_lock means a running worker keeps the messages until it publishes;
// Go does the same through the documents busy flag.
func (s *Session) DocumentsDismiss() {
	s.mu.Lock()
	if s.documents == nil || s.documentsWork.Active() {
		s.mu.Unlock()
		return
	}
	s.documents.ClearMessages()
	s.mu.Unlock()
	s.notify(Change{Page: "documents"})
}

// DocumentsUpload asks for documents and uploads them, as on_documents_upload.
// The native dialog runs inside the job, without the session mutex.
func (s *Session) DocumentsUpload() {
	s.mu.Lock()
	if s.db == nil || s.documents == nil {
		s.mu.Unlock()
		return
	}
	dialogs := s.fileDialogs
	language := s.language
	engine := s.documentsEngine
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		progress func(),
		current func() bool,
	) {
		if dialogs == nil {
			return
		}
		files, ok := dialogs.PickReceipts(i18n.Text(language, i18n.UploadDocuments))
		if !ok || len(files) == 0 {
			return
		}
		if !current() {
			return
		}
		view.Upload(ctx, database, s.paths, files, nil, engine, func(int, int) {
			progress()
		})
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// DocumentsOpen opens the selected document with the system viewer, as
// on_documents_open. The path comes from the profile folder and the stored
// document, never from JavaScript.
func (s *Session) DocumentsOpen() {
	s.mu.Lock()
	var document *model.DocumentItem
	if s.documents != nil && s.documents.SelectedDocument != nil {
		cloned := *s.documents.SelectedDocument
		document = &cloned
	}
	profile := ""
	if s.documents != nil {
		profile = s.documents.Profile
	}
	opener := s.fileOpener
	s.mu.Unlock()

	if document == nil || opener == nil {
		return
	}
	path := documentPath(s.paths, profile, document.StoredPath)
	info, err := os.Stat(path) // #nosec G304 -- the stored document path is resolved from the profile.
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	_ = opener.OpenFile(path)
}

// beginDocumentsOpenLocked prepares the page's load, detecting Tesseract once on
// the first open as open_page does. The caller holds the session mutex.
func (s *Session) beginDocumentsOpenLocked() *pageJob {
	if s.documents == nil {
		return nil
	}
	s.documents.Language = s.language
	if s.db == nil {
		return nil
	}

	first := !s.documentsStarted
	s.documentsStarted = true
	job, _ := s.beginDocumentsJobLocked(true, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.Load(ctx, database, s.paths)
	})

	if first {
		generation, ok := s.beginJobLocked()
		if ok {
			factory := s.documentsEngineFactory
			go s.probeDocumentsEngine(generation, factory)
		}
	}
	return job
}

// probeDocumentsEngine detects the OCR engine once, then reloads the page so the
// hint and the reprocess action see it.
func (s *Session) probeDocumentsEngine(generation uint64, factory func() ocr.Engine) {
	var engine ocr.Engine
	if factory != nil {
		engine = factory()
	} else {
		engine = ocr.CreateOrNone()
	}

	s.mu.Lock()
	current := s.jobs.IsCurrent(generation)
	if current {
		s.documentsEngine = engine
		if s.documents != nil {
			s.documents.IsOCRAvailable = engine != nil
		}
	}
	s.mu.Unlock()
	s.jobs.EndGeneration(generation)
	if !current {
		return
	}
	s.documentsReload()
}

// documentsReload loads the list again for the current tab and search.
func (s *Session) documentsReload() {
	s.mu.Lock()
	job, _ := s.beginDocumentsJobLocked(false, func(
		ctx context.Context,
		view *documentsview.DocumentsState,
		database *db.Database,
		_ func(),
		_ func() bool,
	) {
		view.Load(ctx, database, s.paths)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// beginDocumentsJobLocked prepares one Documents job on a clone of the page
// state. The caller holds the session mutex and starts the returned job after
// releasing it.
//
// Every job takes the page's next request ticket, so a later load or action
// always wins: an earlier result may never overwrite it, whatever order the
// workers finish in. That is Rust's ActionOrder rule expressed with the G8a-1
// page-request counter.
func (s *Session) beginDocumentsJobLocked(loading bool, work documentsJobWork) (*pageJob, bool) {
	if s.db == nil || s.documents == nil {
		return nil, false
	}

	busy := s.documentsWork
	busy.Begin()
	generation, ok := s.beginJobLocked()
	if !ok {
		busy.End()
		return nil, false
	}

	request := s.nextPageRequestLocked("documents")
	pending := s.documents.Clone()
	pending.Language = s.language
	if loading {
		pending.IsLoading = true
	}
	// The published state is a copy: the worker mutates pending without the
	// session mutex.
	published := pending.Clone()
	s.documents = published
	database := s.db
	profile := pending.Profile
	current := func() bool { return s.jobs.IsCurrent(generation) }
	preview := documentsPreview{Kind: "none"}

	progress := func() {
		snapshot := pending.Clone()
		s.mu.Lock()
		snapshot.Language = s.language
		visible := current() && s.isLatestPageRequestLocked("documents", request)
		if visible {
			s.documents = snapshot
		}
		s.mu.Unlock()
		if visible {
			s.notify(Change{Page: "documents"})
		}
	}

	return &pageJob{
		page:       "documents",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			if !current() {
				return
			}
			work(ctx, pending, open, progress, current)
			if current() {
				preview = documentsPreviewFor(pending.SelectedDocument, s.paths, profile)
			}
		},
		publish: func() {
			pending.Language = s.language
			if s.documents != nil {
				pending.IsOCRAvailable = s.documents.IsOCRAvailable
			}
			s.documents = pending
			s.documentsPreview = preview
		},
		finish: func(bool) {
			busy.End()
		},
	}, true
}

// resetDocumentsAuxLocked clears the controller-only values Rust's reset
// clears. The caller holds the session mutex.
func (s *Session) resetDocumentsAuxLocked() {
	s.resetDocumentsPreviewLocked()
	s.documentsCandidateQuery = ""
}

// resetDocumentsPreviewLocked clears the preview as Rust's attach and reset do.
func (s *Session) resetDocumentsPreviewLocked() {
	s.documentsPreview = documentsPreview{Kind: "none"}
}

// documentPath resolves a stored document path, which may be relative to the
// profile folder. It is Rust's document_path.
func documentPath(appPaths paths.AppPaths, profile string, storedPath string) string {
	path := filepath.FromSlash(storedPath)
	if filepath.IsAbs(path) {
		return path
	}
	directory, err := appPaths.ProfileDir(profile)
	if err != nil {
		return filepath.Join(appPaths.Root(), path)
	}
	return filepath.Join(directory, path)
}

// documentsPreviewFor builds the preview for a selected document. Images become
// a data URL only after the size limit passed and only when the file is inside
// the profile's documents folder.
func documentsPreviewFor(
	document *model.DocumentItem,
	appPaths paths.AppPaths,
	profile string,
) documentsPreview {
	none := documentsPreview{Kind: "none"}
	if document == nil {
		return none
	}
	if document.IsPDF() {
		return documentsPreview{Kind: "pdf"}
	}
	if !document.IsImage() {
		return none
	}

	documentsDir, err := documentsservice.DefaultDocumentsDirectory(appPaths, profile)
	if err != nil {
		return none
	}
	path := documentPath(appPaths, profile, document.StoredPath)
	if !documentsPathInside(documentsDir, path) {
		return none
	}
	if limits.CheckImage(path) != nil {
		return none
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is a stored document inside the profile's documents folder.
	if err != nil {
		return none
	}
	mimeType := document.MIMEType
	if !strings.HasPrefix(mimeType, "image/") {
		return none
	}
	return documentsPreview{
		Kind:    "image",
		DataURL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}
}

// documentsPathInside reports whether file is inside directory, comparing path
// components rather than string prefixes and following symlinks when they exist.
func documentsPathInside(directory, file string) bool {
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return false
	}
	absoluteFile, err := filepath.Abs(file)
	if err != nil {
		return false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absoluteDirectory); resolveErr == nil {
		absoluteDirectory = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absoluteFile); resolveErr == nil {
		absoluteFile = resolved
	}
	relative, err := filepath.Rel(absoluteDirectory, absoluteFile)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
