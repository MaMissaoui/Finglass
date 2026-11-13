package documentsview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/testsupport/testpdf"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()

	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), passphrase)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()

	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	return resolved
}

func testAccount(t *testing.T, d *db.Database) model.AccountID {
	t.Helper()

	name := "Account"
	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        &name,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func civilDateOf(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return date
}

func testTransaction(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	partner string,
	hash string,
) model.TransactionID {
	t.Helper()

	purpose := "Receipt"
	_, err := transactions.InsertPlanned(context.Background(), d, accountID, []model.NewTransaction{{
		BookingDate: day,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		PartnerName: &partner,
		Purpose:     &purpose,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("transactions.InsertPlanned: %v", err)
	}

	rows, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("transactions.ListForAccount: %v", err)
	}
	for _, row := range rows {
		if row.ImportHash == hash {
			return row.ID
		}
	}
	t.Fatalf("transaction with hash %q not found", hash)
	return 0
}

func testDocument(
	t *testing.T,
	d *db.Database,
	name string,
	transactionID *model.TransactionID,
) model.DocumentItem {
	t.Helper()

	created, err := documents.Create(context.Background(), d, model.NewDocument{
		FileName:      name,
		StoredPath:    "documents/" + name,
		MIMEType:      "application/pdf",
		TransactionID: transactionID,
	})
	if err != nil {
		t.Fatalf("documents.Create: %v", err)
	}
	item, ok, err := documents.GetItem(context.Background(), d, created.ID)
	if err != nil {
		t.Fatalf("documents.GetItem: %v", err)
	}
	if !ok {
		t.Fatalf("document %d not found", created.ID)
	}
	return item
}

type CountingEngine struct {
	calls atomic.Int64
}

func (e *CountingEngine) Recognize(string) (ocr.Recognition, error) {
	e.calls.Add(1)
	return ocr.Recognition{Text: "", Confidence: 0}, nil
}

func fileNames(items []model.DocumentItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.FileName)
	}
	return names
}

func TestTheWholeBatchUploadsWithProgressAndEndsWhenDone(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	database := testDatabase(t)
	appPaths := testPaths(t)
	first := filepath.Join(temp, "first.png")
	second := filepath.Join(temp, "second.png")
	testpdf.CraftedPNG(first, 2, 2)
	testpdf.CraftedPNG(second, 2, 2)
	engine := &CountingEngine{}
	state := New(i18n.En, true)
	progress := make([][2]int, 0)

	state.Upload(
		ctx,
		database,
		appPaths,
		[]string{first, second},
		nil,
		engine,
		func(done, total int) {
			progress = append(progress, [2]int{done, total})
		},
	)

	if calls := engine.calls.Load(); calls != 2 {
		t.Fatalf("engine calls = %d, want 2", calls)
	}
	wantProgress := [][2]int{{1, 2}, {2, 2}}
	if len(progress) != len(wantProgress) {
		t.Fatalf("progress = %+v, want %+v", progress, wantProgress)
	}
	for index := range progress {
		if progress[index] != wantProgress[index] {
			t.Fatalf("progress = %+v, want %+v", progress, wantProgress)
		}
	}
	if state.IsUploading {
		t.Fatal("the busy section ends with the batch")
	}
	if state.UploadDone != 2 {
		t.Fatalf("upload done = %d, want 2", state.UploadDone)
	}
	if state.UploadTotal != 2 {
		t.Fatalf("upload total = %d, want 2", state.UploadTotal)
	}
	if len(state.Documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(state.Documents))
	}
	if state.Message == nil || *state.Message != "Uploaded 2 files, text extracted from 0" {
		t.Fatalf("message = %v", state.Message)
	}
	if state.Error != nil {
		t.Fatalf("error = %v", *state.Error)
	}
}

func TestAFailedFileIsReportedAndTheRestStillUploads(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	database := testDatabase(t)
	appPaths := testPaths(t)
	png := filepath.Join(temp, "bon.png")
	text := filepath.Join(temp, "notes.txt")
	testpdf.CraftedPNG(png, 2, 2)
	if err := os.WriteFile(text, []byte("not a document"), 0o600); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	state := New(i18n.En, false)

	state.Upload(ctx, database, appPaths, []string{png, text}, nil, nil, func(_, _ int) {})

	if len(state.Documents) != 1 {
		t.Fatalf("documents = %d, want 1", len(state.Documents))
	}
	if state.Documents[0].FileName != "bon.png" {
		t.Fatalf("file name = %q, want bon.png", state.Documents[0].FileName)
	}
	if state.Error == nil || !strings.Contains(*state.Error, "notes.txt") {
		t.Fatalf("error = %v, want the failed file name", state.Error)
	}
	if state.Message == nil || *state.Message != "Uploaded 1 file, text extracted from 0" {
		t.Fatalf("message = %v", state.Message)
	}
}

func TestRefreshCountsEveryFilterForTheCurrentSearch(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	appPaths := testPaths(t)
	account := testAccount(t, database)
	linkedTransaction := testTransaction(
		t, database, account, civilDateOf(t, 2026, time.March, 13), -1000, "Shop", "shop",
	)
	testDocument(t, database, "alpha.pdf", nil)
	testDocument(t, database, "beta.pdf", nil)
	testDocument(t, database, "linked.pdf", &linkedTransaction)
	state := New(i18n.En, false)

	state.Load(ctx, database, appPaths)

	if state.Counts != (DocumentCounts{All: 3, Unlinked: 2, Linked: 1}) {
		t.Fatalf("counts = %+v", state.Counts)
	}
	if len(state.Documents) != 3 {
		t.Fatalf("documents = %d, want the ALL list to show every document", len(state.Documents))
	}

	state.Search(ctx, database, appPaths, "beta")

	if state.Counts != (DocumentCounts{All: 1, Unlinked: 1, Linked: 0}) {
		t.Fatalf("counts = %+v", state.Counts)
	}
	names := fileNames(state.Documents)
	if len(names) != 1 || names[0] != "beta.pdf" {
		t.Fatalf("documents = %+v, want beta.pdf", names)
	}
}

func TestChangingTabsListsTheLatestTabAndCountsEveryFilter(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	appPaths := testPaths(t)
	account := testAccount(t, database)
	linkedTransaction := testTransaction(
		t, database, account, civilDateOf(t, 2026, time.March, 13), -1000, "Shop", "shop",
	)
	testDocument(t, database, "unlinked.pdf", nil)
	testDocument(t, database, "linked.pdf", &linkedTransaction)
	state := New(i18n.En, false)

	state.Load(ctx, database, appPaths)
	state.SelectTab(ctx, database, appPaths, DocumentTabUnlinked)
	names := fileNames(state.Documents)
	if len(names) != 1 || names[0] != "unlinked.pdf" {
		t.Fatalf("documents = %+v, want unlinked.pdf", names)
	}

	state.SelectTab(ctx, database, appPaths, DocumentTabLinked)

	if state.Tab != DocumentTabLinked {
		t.Fatalf("tab = %v, want linked", state.Tab)
	}
	names = fileNames(state.Documents)
	if len(names) != 1 || names[0] != "linked.pdf" {
		t.Fatalf("documents = %+v, want linked.pdf", names)
	}
	if state.Counts != (DocumentCounts{All: 2, Unlinked: 1, Linked: 1}) {
		t.Fatalf("counts = %+v", state.Counts)
	}
}

func TestReceiptsAttachedFromTheDetailPanelAreLinkedToTheTransaction(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	database := testDatabase(t)
	appPaths := testPaths(t)
	account := testAccount(t, database)
	rewe := testTransaction(
		t, database, account, civilDateOf(t, 2026, time.March, 13), -1000, "REWE", "rewe",
	)
	png := filepath.Join(temp, "bon.png")
	testpdf.CraftedPNG(png, 5, 5)
	state := New(i18n.En, false)
	state.Load(ctx, database, appPaths)

	if len(state.Documents) != 0 {
		t.Fatalf("documents = %d, want empty", len(state.Documents))
	}
	state.Upload(ctx, database, appPaths, []string{png}, &rewe, nil, func(_, _ int) {})

	names := fileNames(state.Documents)
	if len(names) != 1 || names[0] != "bon.png" {
		t.Fatalf("documents = %+v, want bon.png", names)
	}
	if state.Documents[0].TransactionID == nil || *state.Documents[0].TransactionID != rewe {
		t.Fatalf("transaction id = %+v, want %d", state.Documents[0].TransactionID, rewe)
	}
	filter := model.TransactionFilter{}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	found := false
	for _, item := range items {
		if item.ID == rewe {
			found = true
			if item.DocumentCount != 1 {
				t.Fatalf("document count = %d, want 1", item.DocumentCount)
			}
		}
	}
	if !found {
		t.Fatalf("transaction %d not found", rewe)
	}
}

func TestLinkUnlinkAndDeleteUpdateTheListAndMessages(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	database := testDatabase(t)
	appPaths := testPaths(t)
	account := testAccount(t, database)
	linkedTransaction := testTransaction(
		t, database, account, civilDateOf(t, 2026, time.March, 13), -1000, "Shop", "shop",
	)
	png := filepath.Join(temp, "receipt.png")
	testpdf.CraftedPNG(png, 2, 2)
	state := New(i18n.En, false)
	state.Upload(ctx, database, appPaths, []string{png}, nil, nil, func(_, _ int) {})
	document := state.Documents[0]
	state.SelectDocument(ctx, database, appPaths, &document)

	state.LinkToTransaction(ctx, database, appPaths, document.ID, linkedTransaction)

	if state.Message == nil || *state.Message != "Document linked" {
		t.Fatalf("message = %v", state.Message)
	}
	if state.Documents[0].TransactionID == nil ||
		*state.Documents[0].TransactionID != linkedTransaction {
		t.Fatalf("transaction id = %+v", state.Documents[0].TransactionID)
	}

	state.Unlink(ctx, database, appPaths, document.ID)
	if state.Documents[0].TransactionID != nil {
		t.Fatalf("transaction id = %+v, want nil", state.Documents[0].TransactionID)
	}

	state.Delete(ctx, database, appPaths, document.ID)
	if state.Message == nil || *state.Message != "Document deleted" {
		t.Fatalf("message = %v", state.Message)
	}
	if len(state.Documents) != 0 {
		t.Fatalf("documents = %d, want empty", len(state.Documents))
	}
	if state.SelectedDocument != nil {
		t.Fatalf("selected document = %+v, want nil", state.SelectedDocument)
	}
	if state.SelectedOCRText != nil {
		t.Fatalf("selected OCR text = %v, want nil", state.SelectedOCRText)
	}
	if len(state.Suggestions) != 0 {
		t.Fatalf("suggestions = %+v, want empty", state.Suggestions)
	}
}

func TestClearMessagesClearsTheErrorAndMessage(t *testing.T) {
	state := New(i18n.En, false)
	errorText := "error"
	message := "message"
	state.Error = &errorText
	state.Message = &message

	state.ClearMessages()

	if state.Error != nil {
		t.Fatalf("error = %v, want nil", state.Error)
	}
	if state.Message != nil {
		t.Fatalf("message = %v, want nil", state.Message)
	}
}

func TestCloneCopiesEverySliceAndPointedToField(t *testing.T) {
	transactionID := model.TransactionID(7)
	label := "Shop (2026-03-13)"
	confidence := 88.0
	ocrText := "SUMME 3,78"
	errorText := "error"
	message := "message"
	taxOverride := false
	categoryID := model.CategoryID(3)
	partner := "Shop"

	state := New(i18n.En, true)
	state.Documents = []model.DocumentItem{{
		ID:               1,
		FileName:         "bon.png",
		StoredPath:       "documents/bon.png",
		MIMEType:         "image/png",
		TransactionID:    &transactionID,
		TransactionLabel: &label,
		HasOCR:           true,
		OCRConfidence:    &confidence,
	}}
	state.SelectedDocument = &state.Documents[0]
	state.SelectedOCRText = &ocrText
	state.Suggestions = []service.MatchSuggestion{{
		Transaction: model.TransactionItem{
			ID:             transactionID,
			PartnerName:    &partner,
			CategoryID:     &categoryID,
			TaxOverride:    &taxOverride,
			TaxRulePattern: &partner,
		},
		Score: 0.8,
	}}
	state.LinkCandidates = []model.TransactionItem{{
		ID:          transactionID,
		PartnerName: &partner,
		Purpose:     &ocrText,
	}}
	state.Error = &errorText
	state.Message = &message

	cloned := state.Clone()
	if cloned == state {
		t.Fatal("Clone returned the same state pointer")
	}

	// Mutating the clone must not touch the original, and the other way round.
	cloned.Documents[0].FileName = "changed.png"
	*cloned.Documents[0].TransactionLabel = "changed"
	*cloned.Documents[0].OCRConfidence = 10
	cloned.SelectedDocument.FileName = "changed.png"
	*cloned.SelectedOCRText = "changed"
	cloned.Suggestions[0].Transaction.ID = 99
	*cloned.Suggestions[0].Transaction.PartnerName = "changed"
	*cloned.Suggestions[0].Transaction.CategoryID = 99
	*cloned.Suggestions[0].Transaction.TaxOverride = true
	*cloned.Suggestions[0].Transaction.TaxRulePattern = "changed"
	cloned.LinkCandidates[0].ID = 99
	*cloned.LinkCandidates[0].Purpose = "changed"
	*cloned.Error = "changed"
	*cloned.Message = "changed"

	if state.Documents[0].FileName != "bon.png" ||
		*state.Documents[0].TransactionLabel != label ||
		*state.Documents[0].OCRConfidence != confidence {
		t.Fatalf("the clone shares the document: %+v", state.Documents[0])
	}
	if state.SelectedDocument.FileName != "bon.png" || *state.SelectedOCRText != ocrText {
		t.Fatalf("the clone shares the selection: %+v %q", state.SelectedDocument, *state.SelectedOCRText)
	}
	if state.Suggestions[0].Transaction.ID != transactionID ||
		*state.Suggestions[0].Transaction.PartnerName != partner ||
		*state.Suggestions[0].Transaction.CategoryID != categoryID ||
		*state.Suggestions[0].Transaction.TaxOverride ||
		*state.Suggestions[0].Transaction.TaxRulePattern != partner {
		t.Fatalf("the clone shares the suggestion: %+v", state.Suggestions[0])
	}
	if state.LinkCandidates[0].ID != transactionID || *state.LinkCandidates[0].Purpose != ocrText {
		t.Fatalf("the clone shares the candidate: %+v", state.LinkCandidates[0])
	}
	if *state.Error != errorText || *state.Message != message {
		t.Fatalf("the clone shares the messages: %q %q", *state.Error, *state.Message)
	}
}
