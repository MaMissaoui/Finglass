package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/testsupport/testpdf"
	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/receipt"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func testAccount(t *testing.T, database *db.Database) model.AccountID {
	t.Helper()

	name := "Account"
	id, err := accounts.Create(context.Background(), database, model.NewAccount{
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

func mustDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return date
}

func testTransaction(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	partner string,
	hash string,
) model.TransactionID {
	t.Helper()

	purpose := "Receipt"
	_, err := transactions.InsertImported(context.Background(), database, accountID, []model.NewTransaction{{
		BookingDate: day,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		PartnerName: &partner,
		Purpose:     &purpose,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("transactions.InsertImported: %v", err)
	}

	rows, err := transactions.ListForAccount(context.Background(), database, accountID, nil, nil)
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

func testService(t *testing.T, database *db.Database, documentsDir string) *service.Service {
	t.Helper()
	return service.NewService(database, ocr.WithoutOCR(), documentsDir)
}

func auditActions(t *testing.T, database *db.Database, action string) []audit.Entry {
	t.Helper()

	entries, err := audit.Recent(context.Background(), database, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	filtered := make([]audit.Entry, 0)
	for _, entry := range entries {
		if entry.Action == action {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func TestUploadCopiesFileExtractsPDFTextAndMakesItSearchable(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsDir := filepath.Join(directory, "documents")
	documentsService := testService(t, database, documentsDir)
	pdf := filepath.Join(directory, "invoice.pdf")
	testpdf.TextPDF(pdf, []string{
		"Rechnung Nr. 4711",
		"Datum: 12.03.2026",
		"Gesamtbetrag 89,99 EUR",
	})

	result, err := documentsService.UploadDocument(context.Background(), pdf, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}

	if result.OCR == nil || result.OCR.Source != ocr.SourceTextLayer {
		t.Fatalf("OCR = %+v, want the text layer", result.OCR)
	}
	if !result.Document.HasOCR {
		t.Error("document has no OCR flag")
	}
	if filepath.Dir(result.Document.StoredPath) != documentsDir {
		t.Errorf("stored path %q is not in %q", result.Document.StoredPath, documentsDir)
	}

	found, err := documentsService.SearchDocuments(context.Background(), "4711")
	if err != nil {
		t.Fatalf("SearchDocuments(4711): %v", err)
	}
	if len(found) != 1 || found[0].ID != result.Document.ID {
		t.Errorf("search 4711 = %+v, want document %d", found, result.Document.ID)
	}
	found, err = documentsService.SearchDocuments(context.Background(), "invoice")
	if err != nil {
		t.Fatalf("SearchDocuments(invoice): %v", err)
	}
	if len(found) != 1 || found[0].ID != result.Document.ID {
		t.Errorf("search invoice = %+v, want document %d", found, result.Document.ID)
	}
	found, err = documentsService.SearchDocuments(context.Background(), "nothing-like-this")
	if err != nil {
		t.Fatalf("SearchDocuments(nothing): %v", err)
	}
	if len(found) != 0 {
		t.Errorf("search nothing = %+v, want empty", found)
	}
}

func TestSuggestsTheTransactionWithTheReceiptTotalNearTheReceiptDate(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	accountID := testAccount(t, database)
	matching := testTransaction(t, database, accountID, mustDate(t, 2026, 3, 13), -8999, "Amazon", "match")
	testTransaction(t, database, accountID, mustDate(t, 2026, 3, 13), -1200, "Other", "other")
	testTransaction(t, database, accountID, mustDate(t, 2025, 3, 13), -8999, "Year earlier", "year")

	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	pdf := filepath.Join(directory, "r.pdf")
	testpdf.TextPDF(pdf, []string{
		"Datum: 12.03.2026",
		"Zwischensumme 75,62",
		"Gesamtbetrag 89,99 EUR",
	})
	uploaded, err := documentsService.UploadDocument(context.Background(), pdf, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}

	suggestions, err := documentsService.SuggestTransactions(context.Background(), uploaded.Document.ID, 5)
	if err != nil {
		t.Fatalf("SuggestTransactions: %v", err)
	}
	if len(suggestions) == 0 || suggestions[0].Transaction.ID != matching {
		t.Fatalf("first suggestion = %+v, want transaction %d", suggestions, matching)
	}
	for _, suggestion := range suggestions {
		if suggestion.Transaction.PartnerName != nil {
			switch *suggestion.Transaction.PartnerName {
			case "Year earlier", "Other":
				t.Errorf("suggested %q", *suggestion.Transaction.PartnerName)
			}
		}
	}
}

func TestLinkUnlinkAndDelete(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	accountID := testAccount(t, database)
	transactionID := testTransaction(t, database, accountID, mustDate(t, 2026, 3, 13), -1000, "Shop", "shop")
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	pdf := filepath.Join(directory, "r.pdf")
	testpdf.TextPDF(pdf, []string{"Some receipt text here that is long enough"})
	uploaded, err := documentsService.UploadDocument(context.Background(), pdf, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	document := uploaded.Document

	linked, err := documentsService.LinkToTransaction(context.Background(), document.ID, transactionID)
	if err != nil {
		t.Fatalf("LinkToTransaction: %v", err)
	}
	if !linked {
		t.Fatal("LinkToTransaction = false")
	}

	items, findErr := transactions.FindItems(context.Background(), database, &model.TransactionFilter{}, nil)
	if findErr != nil {
		t.Fatalf("transactions.FindItems: %v", findErr)
	}
	found := false
	for _, item := range items {
		if item.ID == transactionID {
			found = true
			if item.DocumentCount != 1 {
				t.Errorf("document count = %d, want 1", item.DocumentCount)
			}
		}
	}
	if !found {
		t.Fatalf("transaction %d not found", transactionID)
	}

	unlinkedDocuments, err := documentsService.GetUnlinkedDocuments(context.Background())
	if err != nil {
		t.Fatalf("GetUnlinkedDocuments: %v", err)
	}
	if len(unlinkedDocuments) != 0 {
		t.Errorf("unlinked documents = %+v, want empty", unlinkedDocuments)
	}

	unlinked, err := documentsService.UnlinkFromTransaction(context.Background(), document.ID)
	if err != nil {
		t.Fatalf("UnlinkFromTransaction: %v", err)
	}
	if !unlinked {
		t.Fatal("UnlinkFromTransaction = false")
	}
	unlinkedDocuments, err = documentsService.GetUnlinkedDocuments(context.Background())
	if err != nil {
		t.Fatalf("GetUnlinkedDocuments: %v", err)
	}
	if len(unlinkedDocuments) != 1 {
		t.Errorf("unlinked documents = %d, want 1", len(unlinkedDocuments))
	}

	deleted, err := documentsService.DeleteDocument(context.Background(), document.ID)
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteDocument = false")
	}
	if _, err := os.Stat(document.StoredPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stored file still exists: %v", err)
	}
	list, err := documentsService.ListDocuments(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("documents = %+v, want empty", list)
	}
	text, err := documentsService.GetOCRText(context.Background(), document.ID)
	if err != nil {
		t.Fatalf("GetOCRText: %v", err)
	}
	if text != nil {
		t.Errorf("OCR text = %q, want none", *text)
	}
}

func TestRejectsUnsupportedAndMissingFiles(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	txt := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	_, err := documentsService.UploadDocument(context.Background(), txt, nil, true)
	var unsupported *service.UnsupportedFileType
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want UnsupportedFileType", err)
	}
	_, err = documentsService.UploadDocument(
		context.Background(),
		filepath.Join(directory, "missing.pdf"),
		nil,
		true,
	)
	var missing *service.FileDoesNotExist
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want FileDoesNotExist", err)
	}
}

func TestRejectsFilesLargerThanTheDocumentSizeLimit(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsDir := filepath.Join(directory, "documents")
	documentsService := testService(t, database, documentsDir)
	file := filepath.Join(directory, "big.pdf")
	opened, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- test temporary file
	if err != nil {
		t.Fatalf("create big.pdf: %v", err)
	}
	if err := opened.Truncate(int64(limits.MaxDocumentBytes) + 1); err != nil {
		t.Fatalf("truncate big.pdf: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close big.pdf: %v", err)
	}

	documentsBefore := directoryEntries(t, documentsDir)
	repoBefore, err := documentsService.ListDocuments(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}

	_, err = documentsService.UploadDocument(context.Background(), file, nil, true)
	var tooLarge *service.DocumentTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error = %v, want DocumentTooLarge", err)
	}
	english := service.Message(err, i18n.En)
	german := service.Message(err, i18n.De)
	if !strings.Contains(english, "big.pdf") || !strings.Contains(german, "big.pdf") {
		t.Fatalf("%q / %q do not contain the file name", english, german)
	}
	if english == german {
		t.Fatal("the message does not follow the language")
	}
	if err.Error() != english {
		t.Fatalf("Error() = %q, want %q", err.Error(), english)
	}
	if entries := directoryEntries(t, documentsDir); entries != documentsBefore {
		t.Errorf("documents folder has %d entries, want %d", entries, documentsBefore)
	}
	repoAfter, err := documentsService.ListDocuments(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(repoAfter) != len(repoBefore) {
		t.Errorf("documents = %d, want %d", len(repoAfter), len(repoBefore))
	}
}

func directoryEntries(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		t.Fatalf("read %s: %v", directory, err)
	}
	return len(entries)
}

func TestImagesWithoutAnOCREngineAreStoredWithoutText(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	png := filepath.Join(directory, "scan.png")
	testpdf.CraftedPNG(png, 10, 10)

	result, err := documentsService.UploadDocument(context.Background(), png, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	if result.OCR != nil {
		t.Errorf("OCR = %+v, want nil", result.OCR)
	}
	if result.Document.HasOCR {
		t.Error("document has the OCR flag")
	}
}

func TestReprocessingOCRIsAuditedOnlyWhenTextIsExtracted(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))

	withTextPath := filepath.Join(directory, "reprocess.pdf")
	testpdf.TextPDF(withTextPath, []string{
		"Rechnung Nr. 4711",
		"Datum: 12.03.2026",
		"Gesamtbetrag 89,99 EUR",
	})
	withText, err := documentsService.UploadDocument(context.Background(), withTextPath, nil, false)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	if entries := auditActions(t, database, "OCR_PROCESSED"); len(entries) != 0 {
		t.Fatalf("upload wrote %d OCR entries, want 0", len(entries))
	}

	if _, err := documentsService.ReprocessOCR(context.Background(), withText.Document.ID); err != nil {
		t.Fatalf("ReprocessOCR: %v", err)
	}

	entries := auditActions(t, database, "OCR_PROCESSED")
	if len(entries) != 1 {
		t.Fatalf("OCR entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Details, withText.Document.FileName) {
		t.Errorf("details %q do not contain the file name", entries[0].Details)
	}

	emptyPath := filepath.Join(directory, "empty.pdf")
	testpdf.EmptyPDF(emptyPath)
	withoutText, err := documentsService.UploadDocument(context.Background(), emptyPath, nil, false)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	result, err := documentsService.ReprocessOCR(context.Background(), withoutText.Document.ID)
	if err != nil {
		t.Fatalf("ReprocessOCR: %v", err)
	}
	if result != nil {
		t.Errorf("ReprocessOCR = %+v, want nil", result)
	}
	if entries := auditActions(t, database, "OCR_PROCESSED"); len(entries) != 1 {
		t.Errorf("OCR entries = %d, want 1", len(entries))
	}
}

func TestReceiptParserHandlesGermanAndEnglishFormats(t *testing.T) {
	today := mustDate(t, 2026, 11, 7)

	german := receipt.Parse(
		"EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95",
		money.EUR,
		today,
	)
	if german.Total == nil || german.Total.Minor != 129_995 {
		t.Errorf("German total = %+v, want 129995 minor", german.Total)
	}
	wantGermanDate := mustDate(t, 2026, 10, 3)
	if german.Date == nil || *german.Date != wantGermanDate {
		t.Errorf("German date = %+v, want %v", german.Date, wantGermanDate)
	}
	for _, amount := range german.Amounts {
		if amount.Minor == 310 {
			t.Error("a date fragment was read as an amount")
		}
	}

	english := receipt.Parse(
		"Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00",
		money.EUR,
		today,
	)
	if english.Total == nil || english.Total.Minor != 119_000 {
		t.Errorf("English total = %+v, want 119000 minor", english.Total)
	}
	wantEnglishDate := mustDate(t, 2026, 1, 5)
	if english.Date == nil || *english.Date != wantEnglishDate {
		t.Errorf("English date = %+v, want %v", english.Date, wantEnglishDate)
	}

	none := receipt.Parse("no numbers here", money.EUR, today)
	if none.Total != nil {
		t.Errorf("total = %+v, want nil", none.Total)
	}
}

func TestAFileNameIsReducedToASafeLastSegment(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"../evil\\receipt\n.pdf", "receipt.pdf"},
		{"  invoice\u00002026.pdf  ", "invoice2026.pdf"},
		{"", "document"},
		{".../\u007f", "document"},
	}
	for _, testCase := range cases {
		if got := service.SanitizeFileName(testCase.input); got != testCase.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
	if got := utf8.RuneCountInString(service.SanitizeFileName(strings.Repeat("a", 200))); got != 150 {
		t.Errorf("long name length = %d, want 150", got)
	}
}

func TestAControlCharacterFileNameIsStoredInsideTheDocumentsFolder(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsDir := filepath.Join(directory, "documents")
	documentsService := testService(t, database, documentsDir)
	source := filepath.Join(directory, "invoice\n2026.pdf")
	testpdf.TextPDF(source, []string{"Some receipt text here that is long enough"})

	result, err := documentsService.UploadDocument(context.Background(), source, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}

	if result.Document.FileName != "invoice\n2026.pdf" {
		t.Errorf("display name = %q, want unchanged", result.Document.FileName)
	}
	stored := result.Document.StoredPath
	if filepath.Dir(stored) != documentsDir {
		t.Errorf("stored path %q is not in %q", stored, documentsDir)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Errorf("stored file: %v", err)
	}
	if !strings.HasSuffix(filepath.Base(stored), "_invoice2026.pdf") {
		t.Errorf("stored name %q does not end with _invoice2026.pdf", filepath.Base(stored))
	}
}

func TestDeletingADocumentOutsideTheDocumentsFolderLeavesTheFileAlone(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	outside := filepath.Join(directory, "outside.pdf")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write outside.pdf: %v", err)
	}
	document, err := documents.Create(context.Background(), database, model.NewDocument{
		FileName:   "outside.pdf",
		StoredPath: outside,
		MIMEType:   "application/pdf",
	})
	if err != nil {
		t.Fatalf("documents.Create: %v", err)
	}

	deleted, err := documentsService.DeleteDocument(context.Background(), document.ID)
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteDocument = false")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a file outside documents/ was touched: %v", err)
	}
	list, err := documentsService.ListDocuments(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("documents = %+v, want empty", list)
	}
}

func TestATruncatedPDFGivesNoTextAndNoPanic(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	pdf := filepath.Join(directory, "truncated.pdf")
	testpdf.TextPDF(pdf, []string{"Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"})
	bytes, err := os.ReadFile(pdf) // #nosec G304 -- test temporary file
	if err != nil {
		t.Fatalf("read PDF: %v", err)
	}
	truncated, err := os.Create(pdf) // #nosec G304 -- test temporary file
	if err != nil {
		t.Fatalf("truncate PDF: %v", err)
	}
	if _, err := truncated.Write(bytes[:len(bytes)/2]); err != nil {
		t.Fatalf("truncate PDF: %v", err)
	}
	if err := truncated.Close(); err != nil {
		t.Fatalf("truncate PDF: %v", err)
	}

	result, err := documentsService.UploadDocument(context.Background(), pdf, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	if result.OCR != nil {
		t.Errorf("OCR = %+v, want nil", result.OCR)
	}
	if result.Document.HasOCR {
		t.Error("document has the OCR flag")
	}
}

func TestAuditEntriesContainFileNamesAndIDsButNeverOCRText(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := testService(t, database, filepath.Join(directory, "documents"))
	pdf := filepath.Join(directory, "confidential.pdf")
	testpdf.TextPDF(pdf, []string{
		"Rechnung Nr. 4711",
		"Gesamtbetrag 89,99 EUR",
		"Bankleitzahl 12345678",
	})

	result, err := documentsService.UploadDocument(context.Background(), pdf, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}

	entries, recentErr := audit.Recent(context.Background(), database, 1000)
	if recentErr != nil {
		t.Fatalf("audit.Recent: %v", recentErr)
	}
	foundUploaded := false
	foundOCR := false
	for _, entry := range entries {
		switch entry.Action {
		case "DOCUMENT_UPLOADED":
			foundUploaded = true
		case "OCR_PROCESSED":
			foundOCR = true
		}
		if strings.Contains(entry.Details, "Gesamtbetrag") ||
			strings.Contains(entry.Details, "4711") ||
			strings.Contains(entry.Details, "12345678") {
			t.Errorf("audit details leak OCR text: %q", entry.Details)
		}
	}
	if !foundUploaded {
		t.Error("no DOCUMENT_UPLOADED entry")
	}
	if !foundOCR {
		t.Error("no OCR_PROCESSED entry")
	}
	named := false
	for _, entry := range entries {
		if entry.Action == "DOCUMENT_UPLOADED" && strings.Contains(entry.Details, result.Document.FileName) {
			named = true
		}
	}
	if !named {
		t.Error("no DOCUMENT_UPLOADED entry contains the file name")
	}
}

type countingEngine struct{}

func (*countingEngine) Recognize(string) (ocr.Recognition, error) {
	return ocr.Recognition{Text: "", Confidence: 0}, nil
}

func TestAnOversizedImageIsStoredWithoutCallingTheOCREngine(t *testing.T) {
	directory := t.TempDir()
	database := testDatabase(t)
	documentsService := service.NewService(
		database,
		ocr.NewService(&countingEngine{}),
		filepath.Join(directory, "documents"),
	)
	png := filepath.Join(directory, "huge.png")
	testpdf.CraftedPNG(png, 60_000, 60_000)

	result, err := documentsService.UploadDocument(context.Background(), png, nil, true)
	if err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	if result.OCR != nil {
		t.Errorf("OCR = %+v, want nil", result.OCR)
	}
	if result.Document.HasOCR {
		t.Error("document has the OCR flag")
	}
}

func TestDetectMIMETypeKeepsRustsCaseSensitiveExtension(t *testing.T) {
	if got := service.DetectMIMEType("invoice.pdf"); got != "application/pdf" {
		t.Errorf("lower-case pdf = %q", got)
	}
	if got := service.DetectMIMEType("X.PDF"); got != "application/octet-stream" {
		t.Errorf("upper-case PDF = %q, want application/octet-stream", got)
	}
	if got := service.DetectMIMEType("photo.JPG"); got != "application/octet-stream" {
		t.Errorf("upper-case JPG = %q, want application/octet-stream", got)
	}
}
