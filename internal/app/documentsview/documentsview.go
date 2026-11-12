// Package documentsview ports crates/finglass-core/src/app/documents.rs: the
// Documents page state without any UI or coroutine machinery.
//
// It keeps FinDesk's DocumentsViewModel state: the same tabs, search, counts,
// upload batch, selection, linking and OCR actions, but synchronous. Every write
// goes through documents/service, which writes the audit log.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
package documentsview

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

// DocumentTab is one of the Documents page's filter tabs.
type DocumentTab int

const (
	// DocumentTabAll shows every document.
	DocumentTabAll DocumentTab = iota
	// DocumentTabUnlinked shows documents without a linked transaction.
	DocumentTabUnlinked
	// DocumentTabLinked shows documents with a linked transaction.
	DocumentTabLinked
)

// AllDocumentTabs lists the tabs in the order the page shows them.
var AllDocumentTabs = []DocumentTab{DocumentTabAll, DocumentTabUnlinked, DocumentTabLinked}

// LabelKey returns the i18n key for this tab's label.
func (t DocumentTab) LabelKey() i18n.Key {
	switch t {
	case DocumentTabUnlinked:
		return i18n.TabUnlinked
	case DocumentTabLinked:
		return i18n.TabLinked
	default:
		return i18n.TabAllDocuments
	}
}

// DocumentCounts says how many documents each tab shows for the current search.
type DocumentCounts struct {
	// All counts documents in DocumentTabAll.
	All int
	// Unlinked counts documents in DocumentTabUnlinked.
	Unlinked int
	// Linked counts documents in DocumentTabLinked.
	Linked int
}

// Get returns the count for tab.
func (c DocumentCounts) Get(tab DocumentTab) int {
	switch tab {
	case DocumentTabUnlinked:
		return c.Unlinked
	case DocumentTabLinked:
		return c.Linked
	default:
		return c.All
	}
}

// DocumentsState is the Documents page state.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
type DocumentsState struct {
	// Documents holds the documents shown for the current tab and search.
	Documents []model.DocumentItem
	// Counts says how many documents each tab shows for the current search.
	Counts DocumentCounts
	// Tab is the selected tab.
	Tab DocumentTab
	// SearchQuery is the current search text.
	SearchQuery string
	// SelectedDocument is the selected document.
	SelectedDocument *model.DocumentItem
	// SelectedOCRText is the selected document's OCR text.
	SelectedOCRText *string
	// Suggestions holds transactions that plausibly match the selected
	// document.
	Suggestions []service.MatchSuggestion
	// LinkCandidates holds transactions offered in the manual link picker.
	LinkCandidates []model.TransactionItem
	// IsOCRAvailable says whether an OCR engine is available.
	IsOCRAvailable bool
	// IsUploading says whether an upload batch is running.
	IsUploading bool
	// IsLoading says whether a refresh is running.
	IsLoading bool
	// UploadDone is how many files the current or last upload batch processed.
	UploadDone int
	// UploadTotal is how many files the current or last upload batch contains.
	UploadTotal int
	// Error is the last error, already localized.
	Error *string
	// Message is the last success message, already localized.
	Message *string
	// Language is the language for user-visible texts.
	Language i18n.Language
	// Profile is the profile whose documents folder the actions use.
	Profile string
}

// New builds an empty state.
func New(language i18n.Language, isOCRAvailable bool) *DocumentsState {
	return &DocumentsState{
		Documents:        make([]model.DocumentItem, 0),
		Counts:           DocumentCounts{},
		Tab:              DocumentTabAll,
		SearchQuery:      "",
		SelectedDocument: nil,
		SelectedOCRText:  nil,
		Suggestions:      make([]service.MatchSuggestion, 0),
		LinkCandidates:   make([]model.TransactionItem, 0),
		IsOCRAvailable:   isOCRAvailable,
		IsUploading:      false,
		IsLoading:        false,
		UploadDone:       0,
		UploadTotal:      0,
		Error:            nil,
		Message:          nil,
		Language:         language,
		Profile:          "default",
	}
}

// Default builds an empty state in English, like Rust's DocumentsState::default.
func Default() *DocumentsState {
	return New(i18n.En, false)
}

// SetProfile selects the profile whose documents folder the actions use.
func (s *DocumentsState) SetProfile(profile string) {
	s.Profile = profile
}

// Load reloads the list and counts for the current tab and search.
func (s *DocumentsState) Load(ctx context.Context, d *db.Database, appPaths paths.AppPaths) {
	s.IsLoading = true
	s.Error = nil
	s.refreshFrom(ctx, d, appPaths)
}

// SelectTab selects a tab and reloads the list.
func (s *DocumentsState) SelectTab(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	tab DocumentTab,
) {
	s.Tab = tab
	s.Load(ctx, d, appPaths)
}

// Search changes the search and reloads the list synchronously.
//
// FinDesk debounces this call; the synchronous state has nothing to debounce.
func (s *DocumentsState) Search(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	query string,
) {
	s.SearchQuery = query
	s.refreshFrom(ctx, d, appPaths)
}

// Upload uploads a batch of files.
//
// A failing file is reported in Error and the rest of the batch is still
// uploaded, exactly as FinDesk does. progress is called after every file with
// the number of processed files and the batch size.
func (s *DocumentsState) Upload(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	files []string,
	transactionID *model.TransactionID,
	engine ocr.Engine,
	progress func(done, total int),
) {
	s.IsUploading = true
	s.Error = nil
	s.UploadDone = 0
	s.UploadTotal = len(files)

	documentsService, err := s.newService(d, appPaths, engine)
	if err != nil {
		s.failPath(err)
		return
	}

	failures := make([]string, 0)
	withText := 0
	for index, file := range files {
		result, uploadErr := documentsService.UploadDocument(ctx, file, transactionID, true)
		if uploadErr != nil {
			failures = append(
				failures,
				documentFileName(file)+": "+uploadErr.Message(s.Language),
			)
		} else if result.OCR != nil && strings.TrimSpace(result.OCR.Text) != "" {
			withText++
		}

		s.UploadDone = index + 1
		progress(index+1, len(files))
	}

	if err := s.refresh(ctx, documentsService); err != nil {
		s.failDocument(err)
		return
	}

	uploaded := len(files) - len(failures)
	s.IsUploading = false
	if uploaded > 0 {
		message := i18n.Format(
			s.Language,
			i18n.MessageUploaded,
			i18n.Int(int64(uploaded)),
			i18n.Int(int64(withText)),
		)
		s.Message = &message
	} else {
		s.Message = nil
	}
	if len(failures) != 0 {
		message := strings.Join(failures, "\n")
		s.Error = &message
	} else {
		s.Error = nil
	}
}

// SelectDocument selects a document and loads its OCR text, matching
// suggestions and link candidates.
func (s *DocumentsState) SelectDocument(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	document *model.DocumentItem,
) {
	s.SelectedDocument = cloneDocument(document)
	s.SelectedOCRText = nil
	s.Suggestions = make([]service.MatchSuggestion, 0)
	s.LinkCandidates = make([]model.TransactionItem, 0)

	if document == nil {
		return
	}

	documentsService, err := s.newService(d, appPaths, nil)
	if err != nil {
		s.failPath(err)
		return
	}

	text, documentErr := documentsService.GetOCRText(ctx, document.ID)
	if documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	suggestions, documentErr := documentsService.SuggestTransactions(ctx, document.ID, 5)
	if documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	limit := 200
	filter := model.TransactionFilter{}
	candidates, err := transactions.FindItems(ctx, d, &filter, &limit)
	if err != nil {
		s.failDocument(&wrappedRepositoryError{cause: err})
		return
	}

	if s.SelectedDocument != nil && s.SelectedDocument.ID == document.ID {
		s.SelectedOCRText = text
		s.Suggestions = suggestions
		s.LinkCandidates = candidates
	}
}

// LinkToTransaction links a document to a transaction, reloads the list and
// reports success.
func (s *DocumentsState) LinkToTransaction(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	documentID int64,
	transactionID model.TransactionID,
) {
	documentsService, err := s.newService(d, appPaths, nil)
	if err != nil {
		s.failPath(err)
		return
	}
	if _, documentErr := documentsService.LinkToTransaction(ctx, documentID, transactionID); documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	if documentErr := s.refresh(ctx, documentsService); documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	message := i18n.Text(s.Language, i18n.MessageDocumentLinked)
	s.Message = &message
}

// Unlink removes a document's transaction link and reloads the list.
func (s *DocumentsState) Unlink(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	documentID int64,
) {
	documentsService, err := s.newService(d, appPaths, nil)
	if err != nil {
		s.failPath(err)
		return
	}
	if _, documentErr := documentsService.UnlinkFromTransaction(ctx, documentID); documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	if documentErr := s.refresh(ctx, documentsService); documentErr != nil {
		s.failDocument(documentErr)
	}
}

// ReprocessOCR runs OCR again for a document, reloads the list and reports
// whether text was extracted.
func (s *DocumentsState) ReprocessOCR(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	documentID int64,
	engine ocr.Engine,
) {
	documentsService, err := s.newService(d, appPaths, engine)
	if err != nil {
		s.failPath(err)
		return
	}

	result, documentErr := documentsService.ReprocessOCR(ctx, documentID)
	if documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	if documentErr := s.refresh(ctx, documentsService); documentErr != nil {
		s.failDocument(documentErr)
		return
	}

	hasText := result != nil && strings.TrimSpace(result.Text) != ""
	if s.SelectedDocument != nil && s.SelectedDocument.ID == documentID {
		selected := cloneDocument(s.SelectedDocument)
		s.SelectDocument(ctx, d, appPaths, selected)
	}
	key := i18n.MessageNoTextExtracted
	if hasText {
		key = i18n.TextExtracted
	}
	message := i18n.Text(s.Language, key)
	s.Message = &message
}

// Delete deletes a document, clears it from the selection and reloads the list.
func (s *DocumentsState) Delete(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
	documentID int64,
) {
	documentsService, err := s.newService(d, appPaths, nil)
	if err != nil {
		s.failPath(err)
		return
	}
	if _, documentErr := documentsService.DeleteDocument(ctx, documentID); documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	if s.SelectedDocument != nil && s.SelectedDocument.ID == documentID {
		s.SelectedDocument = nil
		s.SelectedOCRText = nil
		s.Suggestions = make([]service.MatchSuggestion, 0)
	}
	if documentErr := s.refresh(ctx, documentsService); documentErr != nil {
		s.failDocument(documentErr)
		return
	}
	message := i18n.Text(s.Language, i18n.MessageDocumentDeleted)
	s.Message = &message
}

// ClearMessages clears the error and success message.
func (s *DocumentsState) ClearMessages() {
	s.Error = nil
	s.Message = nil
}

func (s *DocumentsState) refreshFrom(
	ctx context.Context,
	d *db.Database,
	appPaths paths.AppPaths,
) {
	documentsService, err := s.newService(d, appPaths, nil)
	if err != nil {
		s.failPath(err)
		return
	}
	if documentErr := s.refresh(ctx, documentsService); documentErr != nil {
		s.failDocument(documentErr)
	}
}

func (s *DocumentsState) refresh(
	ctx context.Context,
	documentsService *service.Service,
) service.DocumentError {
	query := s.SearchQuery
	all, documentErr := documentsService.ListDocuments(ctx, &query, false)
	if documentErr != nil {
		return documentErr
	}
	unlinked, documentErr := documentsService.ListDocuments(ctx, &query, true)
	if documentErr != nil {
		return documentErr
	}

	linked := make([]model.DocumentItem, 0)
	for _, document := range all {
		if document.TransactionID != nil {
			linked = append(linked, document)
		}
	}

	counts := DocumentCounts{
		All:      len(all),
		Unlinked: len(unlinked),
		Linked:   len(linked),
	}
	var documents []model.DocumentItem
	switch s.Tab {
	case DocumentTabUnlinked:
		documents = unlinked
	case DocumentTabLinked:
		documents = linked
	default:
		documents = all
	}

	var selected *model.DocumentItem
	if s.SelectedDocument != nil {
		for index := range documents {
			if documents[index].ID == s.SelectedDocument.ID {
				selected = cloneDocument(&documents[index])
				break
			}
		}
	}

	s.Documents = documents
	s.Counts = counts
	s.SelectedDocument = selected
	s.IsLoading = false
	return nil
}

func (s *DocumentsState) newService(
	d *db.Database,
	appPaths paths.AppPaths,
	engine ocr.Engine,
) (*service.Service, error) {
	documentsDir, err := service.DefaultDocumentsDirectory(appPaths, s.Profile)
	if err != nil {
		return nil, err
	}
	return service.NewService(d, ocr.NewService(engine), documentsDir), nil
}

func (s *DocumentsState) failDocument(err service.DocumentError) {
	s.IsLoading = false
	s.IsUploading = false
	message := err.Message(s.Language)
	s.Error = &message
}

func (s *DocumentsState) failPath(err error) {
	s.IsLoading = false
	s.IsUploading = false
	message := err.Error()
	s.Error = &message
}

// wrappedRepositoryError turns a repository error into a document error, like
// Rust's DocumentError::from.
type wrappedRepositoryError struct {
	cause error
}

func (e *wrappedRepositoryError) Error() string {
	return e.cause.Error()
}

func (e *wrappedRepositoryError) Unwrap() error {
	return e.cause
}

func (e *wrappedRepositoryError) Message(i18n.Language) string {
	return e.cause.Error()
}

func cloneDocument(document *model.DocumentItem) *model.DocumentItem {
	if document == nil {
		return nil
	}
	copied := *document
	return &copied
}

// documentFileName is Rust's file_name helper: the last normal component, or
// "document".
func documentFileName(file string) string {
	if name, ok := rustFileName(file); ok {
		return name
	}
	return "document"
}

// rustFileName returns the last normal path component, like Rust's
// Path::file_name.
func rustFileName(path string) (string, bool) {
	components := make([]string, 0)
	for _, component := range splitRuns(path, byte(filepath.Separator)) {
		switch component {
		case "", ".":
			continue
		default:
			components = append(components, component)
		}
	}
	if len(components) == 0 {
		return "", false
	}
	last := components[len(components)-1]
	if last == ".." {
		return "", false
	}
	return last, true
}

func splitRuns(path string, separator byte) []string {
	parts := make([]string, 0)
	start := 0
	for index := 0; index <= len(path); index++ {
		if index == len(path) || path[index] == separator {
			parts = append(parts, path[start:index])
			start = index + 1
		}
	}
	return parts
}
