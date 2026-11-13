package bind

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/documentsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// DocumentsEvent is the name of the event emitted after every Documents change.
const DocumentsEvent = "documents:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[DocumentsDTO](DocumentsEvent)
}

var errDocumentsNotStarted = errors.New("documents is not started")

// DocumentRowDTO is one row of the document list.
type DocumentRowDTO struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Meta     string `json:"meta"`
	IsLinked bool   `json:"isLinked"`
	HasOCR   bool   `json:"hasOcr"`
	IsPDF    bool   `json:"isPdf"`
	Selected bool   `json:"selected"`
}

// DocumentLinkRowDTO is one suggestion or link-candidate row.
type DocumentLinkRowDTO struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Meta  string `json:"meta"`
	Badge string `json:"badge"`
}

// DocumentsDTO is everything the Documents page shows.
//
// Every value Rust's apply sets has a field here. The preview image travels as a
// data URL, as the page's own CSP allows for images; no other file route is
// added. Money and dates are already formatted by the page.
type DocumentsDTO struct {
	Rows []DocumentRowDTO `json:"rows"`

	Tab              string `json:"tab"`
	TabAllLabel      string `json:"tabAllLabel"`
	TabUnlinkedLabel string `json:"tabUnlinkedLabel"`
	TabLinkedLabel   string `json:"tabLinkedLabel"`

	SearchQuery string `json:"searchQuery"`

	OCRAvailable bool   `json:"ocrAvailable"`
	OCRHint      string `json:"ocrHint"`

	Uploading    bool   `json:"uploading"`
	ProgressText string `json:"progressText"`
	Loading      bool   `json:"loading"`

	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	HasSelection       bool   `json:"hasSelection"`
	SelectedName       string `json:"selectedName"`
	SelectedLinked     bool   `json:"selectedLinked"`
	SelectedLinkLabel  string `json:"selectedLinkLabel"`
	SelectedOCRHeading string `json:"selectedOcrHeading"`
	SelectedOCRText    string `json:"selectedOcrText"`

	PreviewKind    string `json:"previewKind"`
	PreviewDataURL string `json:"previewDataUrl"`

	Suggestions []DocumentLinkRowDTO `json:"suggestions"`
	Candidates  []DocumentLinkRowDTO `json:"candidates"`

	CandidateQuery string `json:"candidateQuery"`
	DeleteText     string `json:"deleteText"`
}

// DocumentsOptions is the process wiring the caller supplies.
type DocumentsOptions struct {
	// Emit publishes documents:changed; nil uses the running Wails application.
	Emit func(DocumentsDTO)
}

// Documents is the Documents page's Wails service.
type Documents struct {
	session *shell.Session
	emit    func(DocumentsDTO)
}

// NewDocuments builds the Documents service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewDocuments(session *shell.Session, options DocumentsOptions) *Documents {
	return &Documents{session: session, emit: options.Emit}
}

// ServiceName implements application.ServiceName.
func (d *Documents) ServiceName() string {
	return "Documents"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (d *Documents) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Documents page.
func (d *Documents) publishPage() {
	d.emitDTO(d.snapshot())
}

// State returns the current Documents state without changing anything.
func (d *Documents) State() (DocumentsDTO, error) {
	if d.session == nil {
		return DocumentsDTO{}, nil
	}
	return d.snapshot(), nil
}

// SetTab applies one of the three filter tabs.
func (d *Documents) SetTab(tab string) (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsSetTab(documentTabFromCode(tab)) })
}

// Search applies the page's search text.
func (d *Documents) Search(query string) (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsSearch(query) })
}

// Select selects the document with id and loads its detail.
func (d *Documents) Select(id int) (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsSelect(int64(id)) })
}

// CloseDetail closes the detail panel.
func (d *Documents) CloseDetail() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsCloseDetail() })
}

// CandidateQueryChanged stores the link picker's search text.
func (d *Documents) CandidateQueryChanged(query string) (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsCandidateQueryChanged(query) })
}

// Link links the selected document to transactionID.
func (d *Documents) Link(transactionID int) (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsLink(model.TransactionID(transactionID)) })
}

// Unlink removes the selected document's link.
func (d *Documents) Unlink() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsUnlink() })
}

// Reprocess runs OCR again for the selected document.
func (d *Documents) Reprocess() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsReprocess() })
}

// ConfirmDelete deletes the selected document.
func (d *Documents) ConfirmDelete() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsConfirmDelete() })
}

// Dismiss clears the error and success banner.
func (d *Documents) Dismiss() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsDismiss() })
}

// Upload asks for documents with the native dialog and uploads them.
func (d *Documents) Upload() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsUpload() })
}

// Open opens the selected document with the system viewer.
func (d *Documents) Open() (DocumentsDTO, error) {
	return d.action(func() { d.session.DocumentsOpen() })
}

func (d *Documents) action(run func()) (DocumentsDTO, error) {
	if d.session == nil {
		return DocumentsDTO{}, errDocumentsNotStarted
	}
	run()
	return d.snapshot(), nil
}

func (d *Documents) emitDTO(dto DocumentsDTO) {
	if d.emit != nil {
		d.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(DocumentsEvent, dto)
	}
}

func (d *Documents) snapshot() DocumentsDTO {
	if d.session == nil {
		return DocumentsDTO{}
	}
	return documentsDTO(d.session.DocumentsState())
}

// documentsDTO maps one Documents snapshot to the page DTO, as Rust's apply does.
func documentsDTO(page shell.DocumentsPageState) DocumentsDTO {
	state := page.State
	language := state.Language
	selected := state.SelectedDocument

	dto := DocumentsDTO{
		Rows:               documentRows(state),
		Tab:                documentTabCode(state.Tab),
		TabAllLabel:        documentTabLabel(state, documentsview.DocumentTabAll),
		TabUnlinkedLabel:   documentTabLabel(state, documentsview.DocumentTabUnlinked),
		TabLinkedLabel:     documentTabLabel(state, documentsview.DocumentTabLinked),
		SearchQuery:        state.SearchQuery,
		OCRAvailable:       state.IsOCRAvailable,
		OCRHint:            documentOCRHint(language, state.IsOCRAvailable),
		Uploading:          state.IsUploading,
		ProgressText:       documentProgressText(state),
		Loading:            state.IsLoading,
		HasSelection:       selected != nil,
		SelectedOCRHeading: documentOCRHeading(state),
		PreviewKind:        page.PreviewKind,
		PreviewDataURL:     page.PreviewDataURL,
		Suggestions:        documentSuggestionRows(state.Suggestions, language),
		Candidates:         documentCandidateRows(state.LinkCandidates, page.CandidateQuery, language),
		CandidateQuery:     page.CandidateQuery,
		DeleteText:         documentDeleteText(state),
	}
	if dto.PreviewKind == "" {
		dto.PreviewKind = "none"
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}
	if selected != nil {
		dto.SelectedName = selected.FileName
		dto.SelectedLinked = selected.TransactionID != nil
		if selected.TransactionLabel != nil {
			dto.SelectedLinkLabel = *selected.TransactionLabel
		}
		dto.SelectedOCRText = ""
		if state.SelectedOCRText != nil {
			dto.SelectedOCRText = *state.SelectedOCRText
		}
	}
	return dto
}

// documentTabFromCode maps a page tab name to the state's tab.
func documentTabFromCode(value string) documentsview.DocumentTab {
	switch value {
	case "unlinked":
		return documentsview.DocumentTabUnlinked
	case "linked":
		return documentsview.DocumentTabLinked
	default:
		return documentsview.DocumentTabAll
	}
}

// documentTabCode maps a state tab to the page tab name.
func documentTabCode(tab documentsview.DocumentTab) string {
	switch tab {
	case documentsview.DocumentTabUnlinked:
		return "unlinked"
	case documentsview.DocumentTabLinked:
		return "linked"
	default:
		return "all"
	}
}

// documentTabLabel is one tab's label with the count for the current search.
func documentTabLabel(state documentsview.DocumentsState, tab documentsview.DocumentTab) string {
	return i18n.Format(
		state.Language,
		i18n.TabWithCount,
		i18n.Str(i18n.Text(state.Language, tab.LabelKey())),
		i18n.Int(int64(state.Counts.Get(tab))),
	)
}

// documentRows is one list row per document.
func documentRows(state documentsview.DocumentsState) []DocumentRowDTO {
	rows := make([]DocumentRowDTO, 0, len(state.Documents))
	for _, document := range state.Documents {
		label := i18n.Text(state.Language, i18n.NotLinked)
		if document.TransactionLabel != nil {
			label = *document.TransactionLabel
		}
		rows = append(rows, DocumentRowDTO{
			ID:       documentID(document.ID),
			Name:     document.FileName,
			Meta:     documentDateTimeText(document.UploadedAt) + " · " + label,
			IsLinked: document.TransactionID != nil,
			HasOCR:   document.HasOCR,
			IsPDF:    document.IsPDF(),
			Selected: state.SelectedDocument != nil && state.SelectedDocument.ID == document.ID,
		})
	}
	return rows
}

// documentOCRHeading is the detail panel's OCR heading, with the confidence when
// the state knows it.
func documentOCRHeading(state documentsview.DocumentsState) string {
	document := state.SelectedDocument
	if document == nil {
		return ""
	}
	if document.OCRConfidence != nil {
		return i18n.Format(
			state.Language,
			i18n.ExtractedTextConfidence,
			i18n.Int(int64(math.Round(*document.OCRConfidence))),
		)
	}
	return i18n.Text(state.Language, i18n.ExtractedText)
}

// documentDeleteText is the delete confirmation's text.
func documentDeleteText(state documentsview.DocumentsState) string {
	document := state.SelectedDocument
	if document == nil {
		return ""
	}
	return i18n.Format(state.Language, i18n.DeleteDocumentText, i18n.Str(document.FileName))
}

// documentProgressText is the upload progress line.
func documentProgressText(state documentsview.DocumentsState) string {
	if !state.IsUploading || state.UploadTotal == 0 {
		return ""
	}
	return i18n.Format(
		state.Language,
		i18n.UploadProgress,
		i18n.Int(int64(state.UploadDone)),
		i18n.Int(int64(state.UploadTotal)),
	)
}

// documentOCRHint is the OCR-unavailable hint, with the Settings path FinDesk
// names.
func documentOCRHint(language i18n.Language, available bool) string {
	if available {
		return ""
	}
	settings := i18n.Text(language, i18n.NavSettings) + " → " + i18n.Text(language, i18n.DataAndOcr)
	return i18n.Format(language, i18n.OcrNotInstalled, i18n.Str(settings))
}

// documentMatchBadge is the badge text for a match suggestion.
func documentMatchBadge(language i18n.Language, score float64) string {
	return i18n.Format(language, i18n.MatchPercent, i18n.Str(format.Percent(score, language)))
}

// documentSuggestionRows are the suggestion rows, with the match score badge.
func documentSuggestionRows(
	suggestions []service.MatchSuggestion,
	language i18n.Language,
) []DocumentLinkRowDTO {
	rows := make([]DocumentLinkRowDTO, 0, len(suggestions))
	for _, suggestion := range suggestions {
		transaction := suggestion.Transaction
		rows = append(rows, DocumentLinkRowDTO{
			ID:    documentTransactionID(int64(transaction.ID)),
			Title: transaction.RawDescription(),
			Meta: documentDateText(transaction.Date) + " · " +
				documentMoneyText(transaction.Amount, language),
			Badge: documentMatchBadge(language, suggestion.Score),
		})
	}
	return rows
}

// documentCandidateRows are the link candidates filtered by the typed query.
func documentCandidateRows(
	candidates []model.TransactionItem,
	query string,
	language i18n.Language,
) []DocumentLinkRowDTO {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return []DocumentLinkRowDTO{}
	}
	rows := make([]DocumentLinkRowDTO, 0, 8)
	for _, transaction := range candidates {
		description := strings.ToLower(transaction.RawDescription())
		purposeMatches := transaction.Purpose != nil &&
			strings.Contains(strings.ToLower(*transaction.Purpose), needle)
		amountText := strings.ToLower(documentMoneyText(transaction.Amount, language))
		if !strings.Contains(description, needle) &&
			!purposeMatches &&
			!strings.Contains(amountText, needle) {
			continue
		}
		rows = append(rows, DocumentLinkRowDTO{
			ID:    documentTransactionID(int64(transaction.ID)),
			Title: transaction.RawDescription(),
			Meta: documentDateText(transaction.Date) + " · " +
				documentMoneyText(transaction.Amount, language),
			Badge: "",
		})
		if len(rows) == 8 {
			break
		}
	}
	return rows
}

// documentDateTimeText is "dd.MM.yyyy HH:mm", the format FinDesk's document rows
// use.
func documentDateTimeText(value time.Time) string {
	return format.DateTime(value)
}

// documentDateText is "dd.MM.yyyy", the format FinDesk's document rows use for
// transaction dates.
func documentDateText(value civil.Date) string {
	return format.Date(value)
}

// documentMoneyText is an amount in the app's money format, as the Documents
// page shows it.
func documentMoneyText(amount money.Money, language i18n.Language) string {
	return format.Money(amount, language, false)
}

// documentID narrows a row id to the page's 32-bit range, as Rust does.
func documentID(value int64) int {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(value)
}

// documentTransactionID narrows a transaction id the same way.
func documentTransactionID(value int64) int {
	return documentID(value)
}
