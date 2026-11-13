package shell

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/documentsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/testsupport/testpdf"
)

func seedDocumentsDatabase(t *testing.T, appPaths paths.AppPaths) model.TransactionID {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	account, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	day, ok := civil.New(2026, time.March, 13)
	if !ok {
		t.Fatal("invalid date")
	}
	partner := "Shop"
	if _, err := transactions.InsertPlanned(ctx, database, account, []model.NewTransaction{{
		BookingDate: day,
		Amount:      money.Money{Minor: -378, Currency: money.EUR},
		PartnerName: &partner,
		ImportHash:  "documents-test",
		CreatedAt:   testNow.UTC(),
	}}); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	items, err := transactions.FindItems(ctx, database, &model.TransactionFilter{}, nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("find transactions: %v, %d", err, len(items))
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return items[0].ID
}

func listenForDocuments(session *Session) (<-chan Change, func()) {
	events := make(chan Change, 512)
	remove := session.AddListener(func(change Change) {
		events <- change
	})
	return events, remove
}

func waitForDocuments(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(DocumentsPageState) bool,
) DocumentsPageState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "documents" {
				continue
			}
			page := session.DocumentsState()
			if predicate(page) {
				return page
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedDocumentsSession(t *testing.T) (*Session, model.TransactionID, <-chan Change) {
	t.Helper()
	appPaths := testPaths(t)
	transactionID := seedDocumentsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	session.documentsEngineFactory = func() ocr.Engine { return nil }
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)

	events, remove := listenForDocuments(session)
	t.Cleanup(remove)
	session.Navigate("documents")
	waitForDocuments(t, session, events, "the loaded documents", func(page DocumentsPageState) bool {
		return !page.State.IsLoading
	})
	waitForJobs(t, session)
	return session, transactionID, events
}

func writePNGFixture(t *testing.T, name string, width uint32, height uint32) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	testpdf.CraftedPNG(path, width, height)
	return path
}

func writePDFFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	testpdf.EmptyPDF(path)
	return path
}

func documentByName(t *testing.T, page DocumentsPageState, name string) model.DocumentItem {
	t.Helper()
	for _, document := range page.State.Documents {
		if document.FileName == name {
			return document
		}
	}
	t.Fatalf("document %q is not listed: %+v", name, page.State.Documents)
	return model.DocumentItem{}
}

func documentsAuditCount(t *testing.T, session *Session, action string) int {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Action == action {
			count++
		}
	}
	return count
}

func documentsAuditTotal(t *testing.T, session *Session) int {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	return len(entries)
}

func TestDocumentsUploadLinkUnlinkReprocessDeleteAndAudit(t *testing.T) {
	session, transactionID, events := newLoadedDocumentsSession(t)
	pngPath := writePNGFixture(t, "receipt.png", 2, 2)
	pdfPath := writePDFFixture(t, "receipt.pdf")
	fake := &fakeFileDialogs{receipts: []string{pngPath, pdfPath}, receiptsResult: true}
	session.SetFileDialogs(fake)

	beforeUpload := documentsAuditCount(t, session, "DOCUMENT_UPLOADED")
	beforeOCR := documentsAuditCount(t, session, "OCR_PROCESSED")
	beforeTotal := documentsAuditTotal(t, session)

	session.DocumentsUpload()
	page := waitForDocuments(t, session, events, "the uploaded documents", func(page DocumentsPageState) bool {
		return !page.State.IsUploading && page.State.Message != nil && len(page.State.Documents) == 2
	})
	if page.State.Error != nil {
		t.Fatalf("upload error = %q", *page.State.Error)
	}
	if *page.State.Message != i18n.Format(i18n.En, i18n.MessageUploaded, i18n.Int(2), i18n.Int(0)) {
		t.Fatalf("upload message = %q", *page.State.Message)
	}
	fake.mu.Lock()
	title := fake.receiptsTitle
	calls := fake.receiptsCalls
	fake.mu.Unlock()
	if calls != 1 || title != i18n.Text(i18n.En, i18n.UploadDocuments) {
		t.Fatalf("dialog = %d calls, title %q", calls, title)
	}
	if got := documentsAuditCount(t, session, "DOCUMENT_UPLOADED"); got != beforeUpload+2 {
		t.Fatalf("upload audit entries = %d, want %d", got, beforeUpload+2)
	}
	if got := documentsAuditCount(t, session, "OCR_PROCESSED"); got != beforeOCR {
		t.Fatalf("an empty PDF or the missing OCR engine wrote %d OCR entries", got-beforeOCR)
	}

	png := documentByName(t, page, "receipt.png")
	if png.HasOCR {
		t.Fatal("the PNG has an OCR row without an engine")
	}
	pdf := documentByName(t, page, "receipt.pdf")
	if pdf.HasOCR {
		t.Fatal("the empty PDF has an OCR row")
	}

	session.DocumentsSelect(png.ID)
	page = waitForDocuments(t, session, events, "the selected PNG", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == png.ID
	})
	if page.PreviewKind != "image" {
		t.Fatalf("preview kind = %q", page.PreviewKind)
	}
	if !strings.HasPrefix(page.PreviewDataURL, "data:image/png;base64,") {
		t.Fatalf("preview URL = %.40q", page.PreviewDataURL)
	}
	encoded := strings.TrimPrefix(page.PreviewDataURL, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	stored, err := os.ReadFile(png.StoredPath) // #nosec G304 -- the test reads the file its upload stored.
	if err != nil {
		t.Fatalf("read stored document: %v", err)
	}
	if string(decoded) != string(stored) {
		t.Fatal("the preview does not decode back to the stored bytes")
	}

	session.DocumentsSelect(pdf.ID)
	page = waitForDocuments(t, session, events, "the selected PDF", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == pdf.ID
	})
	if page.PreviewKind != "pdf" || page.PreviewDataURL != "" {
		t.Fatalf("PDF preview = %q, %q", page.PreviewKind, page.PreviewDataURL)
	}

	beforeLink := documentsAuditCount(t, session, "DOCUMENT_LINKED")
	session.DocumentsLink(transactionID)
	page = waitForDocuments(t, session, events, "the linked document", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil &&
			page.State.SelectedDocument.TransactionID != nil &&
			page.State.Message != nil
	})
	if *page.State.Message != i18n.Text(i18n.En, i18n.MessageDocumentLinked) {
		t.Fatalf("link message = %q", *page.State.Message)
	}
	if got := documentsAuditCount(t, session, "DOCUMENT_LINKED"); got != beforeLink+1 {
		t.Fatalf("link audit entries = %d, want %d", got, beforeLink+1)
	}

	beforeUnlink := documentsAuditCount(t, session, "DOCUMENT_UNLINKED")
	session.DocumentsUnlink()
	page = waitForDocuments(t, session, events, "the unlinked document", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.TransactionID == nil
	})
	if got := documentsAuditCount(t, session, "DOCUMENT_UNLINKED"); got != beforeUnlink+1 {
		t.Fatalf("unlink audit entries = %d, want %d", got, beforeUnlink+1)
	}

	session.DocumentsReprocess()
	page = waitForDocuments(t, session, events, "the reprocessed document", func(page DocumentsPageState) bool {
		return page.State.Message != nil &&
			*page.State.Message == i18n.Text(i18n.En, i18n.MessageNoTextExtracted)
	})
	if page.State.SelectedDocument == nil || page.State.SelectedDocument.ID != pdf.ID {
		t.Fatalf("the reprocess changed the selection: %+v", page.State.SelectedDocument)
	}

	beforeDelete := documentsAuditCount(t, session, "DOCUMENT_DELETED")
	session.DocumentsConfirmDelete()
	page = waitForDocuments(t, session, events, "the deleted document", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument == nil && len(page.State.Documents) == 1
	})
	if *page.State.Message != i18n.Text(i18n.En, i18n.MessageDocumentDeleted) {
		t.Fatalf("delete message = %q", *page.State.Message)
	}
	if got := documentsAuditCount(t, session, "DOCUMENT_DELETED"); got != beforeDelete+1 {
		t.Fatalf("delete audit entries = %d, want %d", got, beforeDelete+1)
	}
	if _, err := os.Stat(pdf.StoredPath); !os.IsNotExist(err) {
		t.Fatalf("the deleted document's file remains: %v", err)
	}

	// Two uploads (each with its repository create entry), one link, one unlink
	// and one delete: the service's entries and nothing else.
	if got := documentsAuditTotal(t, session) - beforeTotal; got != 10 {
		t.Fatalf("the documents actions wrote %d audit entries, want 10", got)
	}
}

func TestDocumentsPreviewIsMissingForLargeOrOutsideImages(t *testing.T) {
	session, _, events := newLoadedDocumentsSession(t)

	largePath := writePNGFixture(t, "huge.png", 60_000, 60_000)
	fake := &fakeFileDialogs{receipts: []string{largePath}, receiptsResult: true}
	session.SetFileDialogs(fake)
	session.DocumentsUpload()
	page := waitForDocuments(t, session, events, "the large image", func(page DocumentsPageState) bool {
		return !page.State.IsUploading && page.State.Message != nil && len(page.State.Documents) == 1
	})
	large := documentByName(t, page, "huge.png")
	session.DocumentsSelect(large.ID)
	page = waitForDocuments(t, session, events, "the selected large image", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == large.ID
	})
	if page.PreviewKind != "none" || page.PreviewDataURL != "" {
		t.Fatalf("a large image got a preview: %q, %q", page.PreviewKind, page.PreviewDataURL)
	}

	outsidePath := writePNGFixture(t, "outside.png", 2, 2)
	created, err := documents.Create(context.Background(), sessionDatabase(t, session), model.NewDocument{
		FileName:   "outside.png",
		StoredPath: outsidePath,
		MIMEType:   "image/png",
	})
	if err != nil {
		t.Fatalf("create outside document: %v", err)
	}
	session.documentsReload()
	page = waitForDocuments(t, session, events, "the outside document", func(page DocumentsPageState) bool {
		return !page.State.IsLoading && len(page.State.Documents) == 2
	})
	session.DocumentsSelect(created.ID)
	page = waitForDocuments(t, session, events, "the selected outside image", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == created.ID
	})
	if page.PreviewKind != "none" || page.PreviewDataURL != "" {
		t.Fatalf("an outside image got a preview: %q, %q", page.PreviewKind, page.PreviewDataURL)
	}
}

func TestDocumentsOpenRecordsTheResolvedPath(t *testing.T) {
	session, _, events := newLoadedDocumentsSession(t)
	opener := &fakeFileOpener{}
	session.SetFileOpener(opener)

	pngPath := writePNGFixture(t, "receipt.png", 2, 2)
	fake := &fakeFileDialogs{receipts: []string{pngPath}, receiptsResult: true}
	session.SetFileDialogs(fake)
	session.DocumentsUpload()
	page := waitForDocuments(t, session, events, "the uploaded image", func(page DocumentsPageState) bool {
		return !page.State.IsUploading && page.State.Message != nil && len(page.State.Documents) == 1
	})
	png := documentByName(t, page, "receipt.png")
	session.DocumentsSelect(png.ID)
	waitForDocuments(t, session, events, "the selected image", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == png.ID
	})

	session.DocumentsOpen()
	opened := opener.opened()
	if len(opened) != 1 || opened[0] != png.StoredPath {
		t.Fatalf("opened paths = %v, want %q", opened, png.StoredPath)
	}

	// A relative stored path resolves against the profile folder.
	appPaths := session.paths
	dataDir := defaultDataDir(t, appPaths)
	relativeDir := filepath.Join(dataDir, "documents")
	if err := os.MkdirAll(relativeDir, 0o700); err != nil {
		t.Fatalf("create documents folder: %v", err)
	}
	relativeFile := filepath.Join(relativeDir, "relative.pdf")
	testpdf.EmptyPDF(relativeFile)
	created, err := documents.Create(context.Background(), sessionDatabase(t, session), model.NewDocument{
		FileName:   "relative.pdf",
		StoredPath: filepath.Join("documents", "relative.pdf"),
		MIMEType:   "application/pdf",
	})
	if err != nil {
		t.Fatalf("create relative document: %v", err)
	}
	session.documentsReload()
	waitForDocuments(t, session, events, "the relative document", func(page DocumentsPageState) bool {
		return !page.State.IsLoading && len(page.State.Documents) == 2
	})
	session.DocumentsSelect(created.ID)
	waitForDocuments(t, session, events, "the selected relative document", func(page DocumentsPageState) bool {
		return page.State.SelectedDocument != nil && page.State.SelectedDocument.ID == created.ID
	})
	session.DocumentsOpen()
	opened = opener.opened()
	if len(opened) != 2 || opened[1] != relativeFile {
		t.Fatalf("opened paths = %v, want %q", opened, relativeFile)
	}
}

func TestDocumentsTicketOrderPublishesTheLaterReload(t *testing.T) {
	session, _, events := newLoadedDocumentsSession(t)

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "documents" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.mu.Lock()
	session.documents.Tab = documentsview.DocumentTabAll
	session.mu.Unlock()
	session.documentsReload()
	first := <-entered

	session.mu.Lock()
	session.documents.Tab = documentsview.DocumentTabLinked
	session.mu.Unlock()
	session.documentsReload()
	second := <-entered

	// The later reload finishes first and must keep its result.
	close(second)
	waitForDocuments(t, session, events, "the later reload", func(page DocumentsPageState) bool {
		return !page.State.IsLoading && page.State.Tab == documentsview.DocumentTabLinked
	})

	close(first)
	waitForJobs(t, session)
	page := session.DocumentsState()
	if page.State.Tab != documentsview.DocumentTabLinked {
		t.Fatalf("the earlier reload overwrote the later one: tab %v", page.State.Tab)
	}
	if page.State.IsLoading {
		t.Fatal("the dropped reload published the loading state")
	}
	if len(page.State.Documents) != 0 {
		t.Fatalf("the linked tab shows %d documents", len(page.State.Documents))
	}
}

func TestADocumentsSupersededLoadIsDropped(t *testing.T) {
	session, _, events := newLoadedDocumentsSession(t)

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "documents" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.DocumentsSearch("no such document")
	first := <-entered
	session.DocumentsSearch("")
	second := <-entered

	close(second)
	waitForDocuments(t, session, events, "the cleared search", func(page DocumentsPageState) bool {
		return !page.State.IsLoading && page.State.SearchQuery == ""
	})
	close(first)
	waitForJobs(t, session)

	page := session.DocumentsState()
	if page.State.SearchQuery != "" {
		t.Fatalf("the superseded search published %q", page.State.SearchQuery)
	}
}

func TestTeardownDuringADocumentsUploadDropsIt(t *testing.T) {
	session, _, _ := newLoadedDocumentsSession(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	fake := &fakeFileDialogs{
		receipts:       []string{writePNGFixture(t, "teardown.png", 2, 2)},
		receiptsResult: true,
	}
	fake.onReceipts = func() {
		close(entered)
		<-release
	}
	session.SetFileDialogs(fake)

	session.DocumentsUpload()
	<-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the documents upload")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the documents upload did not end")
	}

	requireClosed(t, old, "the old database after the documents upload ends")
	page := session.DocumentsState()
	if page.State.IsUploading || len(page.State.Documents) != 0 || page.State.SelectedDocument != nil {
		t.Fatalf("the teardown did not drop the upload: %+v", page.State)
	}
	if page.PreviewKind != "none" {
		t.Fatalf("the teardown kept a preview: %q", page.PreviewKind)
	}
	documentsDir, err := documentsservice.DefaultDocumentsDirectory(session.paths, "default")
	if err != nil {
		t.Fatalf("documents folder: %v", err)
	}
	entries, err := os.ReadDir(documentsDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read documents folder: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the torn-down upload stored %v", entries)
	}
}

func TestDocumentsStateIsSafeWhileActionsRun(t *testing.T) {
	session, _, events := newLoadedDocumentsSession(t)

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.DocumentsState()
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	for index := 0; index < 4; index++ {
		session.DocumentsSetTab(documentsview.DocumentTabUnlinked)
		waitForDocuments(t, session, events, "the unlinked tab", func(page DocumentsPageState) bool {
			return !page.State.IsLoading && page.State.Tab == documentsview.DocumentTabUnlinked
		})
		session.DocumentsSetTab(documentsview.DocumentTabAll)
		waitForDocuments(t, session, events, "the all tab", func(page DocumentsPageState) bool {
			return !page.State.IsLoading && page.State.Tab == documentsview.DocumentTabAll
		})
		session.DocumentsSearch("shop")
		waitForDocuments(t, session, events, "the search", func(page DocumentsPageState) bool {
			return !page.State.IsLoading && page.State.SearchQuery == "shop"
		})
		session.DocumentsSearch("")
		waitForDocuments(t, session, events, "the cleared search", func(page DocumentsPageState) bool {
			return !page.State.IsLoading && page.State.SearchQuery == ""
		})
	}

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadDocumentsWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedDocumentsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	session.documentsEngineFactory = func() ocr.Engine { return nil }

	called := make(chan struct{}, 16)
	session.AddListener(func(Change) {
		_ = session.DocumentsState()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func TestDocumentsTabLabelsCarryTheServiceCountsInTheBindDTO(t *testing.T) {
	// The ported Rust helper test lives in the bind package; this checks that
	// the session loads the counts those labels use.
	session, _, _ := newLoadedDocumentsSession(t)
	page := session.DocumentsState()
	if page.State.Counts.All != 0 || page.State.Counts.Unlinked != 0 || page.State.Counts.Linked != 0 {
		t.Fatalf("counts = %+v", page.State.Counts)
	}
	if page.State.Profile != "default" {
		t.Fatalf("profile = %q", page.State.Profile)
	}
}
