package documents

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func database(t *testing.T) *db.Database {
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

func account(t *testing.T, d *db.Database) model.AccountID {
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

func transaction(t *testing.T, d *db.Database) model.TransactionID {
	t.Helper()

	accountID := account(t, d)
	var id int64
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		result, err := conn.ExecContext(
			ctx,
			"INSERT INTO transactions "+
				"(account_id, booking_date, amount_minor, currency, partner_name, purpose, import_hash, created_at, is_new) "+
				"VALUES (?1, '2026-03-13', -8999, 'EUR', 'Shop', 'Receipt', 'doc-transaction', '2026-03-13T00:00:00Z', 0)",
			int64(accountID),
		)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	return model.TransactionID(id)
}

func document(t *testing.T, d *db.Database, name string, transactionID *model.TransactionID) model.Document {
	t.Helper()

	created, err := Create(context.Background(), d, model.NewDocument{
		FileName:      name,
		StoredPath:    "documents/" + name,
		MIMEType:      "application/pdf",
		TransactionID: transactionID,
	})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return created
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func ids(items []model.DocumentItem) []int64 {
	result := make([]int64, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID)
	}
	return result
}

func names(items []model.DocumentItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.FileName)
	}
	return result
}

func TestCreateStoresTheDocumentAndItsUploadTime(t *testing.T) {
	d := database(t)
	created := document(t, d, "invoice.pdf", nil)

	if created.FileName != "invoice.pdf" || created.StoredPath != "documents/invoice.pdf" ||
		created.MIMEType != "application/pdf" || created.TransactionID != nil {
		t.Errorf("created = %+v", created)
	}
	loaded, ok, err := Get(context.Background(), d, created.ID)
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if loaded.ID != created.ID || loaded.FileName != created.FileName ||
		loaded.StoredPath != created.StoredPath || loaded.MIMEType != created.MIMEType ||
		loaded.TransactionID != nil || !loaded.UploadedAt.Equal(created.UploadedAt) {
		t.Errorf("loaded = %+v, created = %+v", loaded, created)
	}
	listed, err := List(context.Background(), d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID ||
		!listed[0].UploadedAt.Equal(created.UploadedAt) {
		t.Errorf("List = %+v", listed)
	}
}

func TestLinkUnlinkAndDelete(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	transactionID := transaction(t, d)
	created := document(t, d, "receipt.pdf", nil)

	linked, err := LinkToTransaction(ctx, d, created.ID, transactionID)
	if err != nil || linked == nil {
		t.Fatalf("LinkToTransaction = %v, %v", linked, err)
	}
	if linked.TransactionID == nil || *linked.TransactionID != transactionID {
		t.Errorf("linked transaction = %v", linked.TransactionID)
	}

	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetItem = %v, %v", item, err)
	}
	if item.TransactionID == nil || *item.TransactionID != transactionID {
		t.Errorf("item transaction = %v", item.TransactionID)
	}
	if item.TransactionLabel == nil || *item.TransactionLabel != "Shop (2026-03-13)" {
		t.Errorf("transaction label = %v", item.TransactionLabel)
	}
	if item.HasOCR {
		t.Error("has OCR")
	}
	if item.OCRConfidence != nil {
		t.Errorf("OCR confidence = %v", item.OCRConfidence)
	}
	if !item.IsPDF() || item.IsImage() {
		t.Errorf("mime type flags = %v, %v", item.IsPDF(), item.IsImage())
	}

	unlinked, err := UnlinkFromTransaction(ctx, d, created.ID)
	if err != nil || unlinked == nil {
		t.Fatalf("UnlinkFromTransaction = %v, %v", unlinked, err)
	}
	if unlinked.TransactionID != nil {
		t.Errorf("unlinked transaction = %v", unlinked.TransactionID)
	}
	item, ok, err = GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.TransactionLabel != nil {
		t.Errorf("label after unlink = %v, %v", item, err)
	}

	if deleted, err := Delete(ctx, d, created.ID); err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	if _, ok, err := Get(ctx, d, created.ID); err != nil || ok {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	if deleted, err := Delete(ctx, d, created.ID); err != nil || deleted {
		t.Errorf("second Delete = %v, %v; a second delete finds nothing", deleted, err)
	}
}

func TestFindItemsSearchesFileNameAndOCRTextAndFilters(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	transactionID := transaction(t, d)
	invoice := document(t, d, "invoice.pdf", nil)
	notes := document(t, d, "notes.pdf", nil)
	linked := document(t, d, "linked.pdf", &transactionID)
	confidence := 0.94
	if _, err := SaveOCRText(ctx, d, notes.ID, "Rechnung Nr. 4711", "de", &confidence); err != nil {
		t.Fatalf("SaveOCRText: %v", err)
	}

	query := "4711"
	byOCR, err := FindItems(ctx, d, &query, false, nil)
	if err != nil {
		t.Fatalf("FindItems by OCR: %v", err)
	}
	if got := ids(byOCR); !slices.Equal(got, []int64{notes.ID}) {
		t.Errorf("by OCR = %v", got)
	}

	nameQuery := "invoice"
	byName, err := FindItems(ctx, d, &nameQuery, false, nil)
	if err != nil {
		t.Fatalf("FindItems by name: %v", err)
	}
	if got := ids(byName); !slices.Equal(got, []int64{invoice.ID}) {
		t.Errorf("by name = %v", got)
	}
	missingQuery := "nothing-like-this"
	missing, err := FindItems(ctx, d, &missingQuery, false, nil)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing query = %v, %v", missing, err)
	}

	unlinked, err := FindItems(ctx, d, nil, true, nil)
	if err != nil {
		t.Fatalf("FindItems unlinked: %v", err)
	}
	if len(unlinked) != 2 {
		t.Errorf("unlinked length = %d", len(unlinked))
	}
	for _, item := range unlinked {
		if item.TransactionID != nil {
			t.Errorf("unlinked item has a transaction: %v", item.TransactionID)
		}
	}

	onlyLinked, err := FindItems(ctx, d, nil, false, &transactionID)
	if err != nil {
		t.Fatalf("FindItems linked: %v", err)
	}
	if got := ids(onlyLinked); !slices.Equal(got, []int64{linked.ID}) {
		t.Errorf("linked = %v", got)
	}

	notesItem, ok, err := GetItem(ctx, d, notes.ID)
	if err != nil || !ok {
		t.Fatalf("GetItem(notes) = %v, %v", notesItem, err)
	}
	if !notesItem.HasOCR {
		t.Error("notes has no OCR")
	}
	if notesItem.OCRConfidence == nil || *notesItem.OCRConfidence != 0.94 {
		t.Errorf("OCR confidence = %v", notesItem.OCRConfidence)
	}
}

func TestFindItemsReturnsTheNewestFirst(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at)\n"+
				"                 VALUES ('old.pdf', 'old.pdf', 'application/pdf', '2026-01-01T00:00:00Z')",
		); err != nil {
			return err
		}
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at)\n"+
				"                 VALUES ('new.pdf', 'new.pdf', 'application/pdf', '2026-02-01T00:00:00Z')",
		)
		return err
	})
	if err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	items, err := FindItems(ctx, d, nil, false, nil)
	if err != nil {
		t.Fatalf("FindItems: %v", err)
	}
	if got := names(items); !slices.Equal(got, []string{"new.pdf", "old.pdf"}) {
		t.Errorf("names = %v", got)
	}
}

func TestSearchMatchesLikeWildcardsLiterally(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	document(t, d, "100% Shop.pdf", nil)
	document(t, d, "100x Shop.pdf", nil)
	document(t, d, "under_score.pdf", nil)
	document(t, d, "underXscore.pdf", nil)

	percent := "100%"
	items, err := FindItems(ctx, d, &percent, false, nil)
	if err != nil {
		t.Fatalf("FindItems(100%%): %v", err)
	}
	if got := names(items); !slices.Equal(got, []string{"100% Shop.pdf"}) {
		t.Errorf("100%% = %v", got)
	}
	underscore := "under_score"
	items, err = FindItems(ctx, d, &underscore, false, nil)
	if err != nil {
		t.Fatalf("FindItems(under_score): %v", err)
	}
	if got := names(items); !slices.Equal(got, []string{"under_score.pdf"}) {
		t.Errorf("under_score = %v", got)
	}
}

func TestSaveOCRTextReplacesAndDeleteCascadesButLeavesTheFile(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "receipt.pdf")
	if err := os.WriteFile(file, []byte("pdf bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	created, err := Create(ctx, d, model.NewDocument{
		FileName:   "receipt.pdf",
		StoredPath: file,
		MIMEType:   "application/pdf",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	half := 0.5
	if _, err := SaveOCRText(ctx, d, created.ID, "first", "de", &half); err != nil {
		t.Fatalf("first SaveOCRText: %v", err)
	}
	if _, err := SaveOCRText(ctx, d, created.ID, "second", "en", nil); err != nil {
		t.Fatalf("second SaveOCRText: %v", err)
	}

	content, ok, err := GetOCRText(ctx, d, created.ID)
	if err != nil || !ok || content != "second" {
		t.Fatalf("GetOCRText = %q, %v, %v", content, ok, err)
	}
	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok || !item.HasOCR || item.OCRConfidence != nil {
		t.Fatalf("GetItem = %+v, %v", item, err)
	}
	var ocrRows int64
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(
			ctx, "SELECT count(*) FROM ocr_texts WHERE document_id = ?1", created.ID,
		).Scan(&ocrRows)
	})
	if err != nil {
		t.Fatalf("count OCR rows: %v", err)
	}
	if ocrRows != 1 {
		t.Errorf("OCR rows = %d; saving replaces the previous OCR text", ocrRows)
	}

	if deleted, err := Delete(ctx, d, created.ID); err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	var ocrAfter int64
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM ocr_texts").Scan(&ocrAfter)
	})
	if err != nil {
		t.Fatalf("count OCR rows after delete: %v", err)
	}
	if ocrAfter != 0 {
		t.Errorf("OCR rows after delete = %d; the foreign key removes the OCR text", ocrAfter)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("deleting a row deleted the file: %v", err)
	}
}

func TestUnknownDocumentsAreNotFound(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	if _, ok, err := Get(ctx, d, 999); err != nil || ok {
		t.Errorf("Get(999) = %v, %v", ok, err)
	}
	if item, ok, err := GetItem(ctx, d, 999); err != nil || ok {
		t.Errorf("GetItem(999) = %v, %v", item, err)
	}
	if _, err := SaveOCRText(ctx, d, 999, "text", "de", nil); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SaveOCRText(999) = %v, want ErrNotFound", err)
	}
	if _, ok, err := GetOCRText(ctx, d, 999); err != nil || ok {
		t.Errorf("GetOCRText(999) = %v, %v", ok, err)
	}
	if document, err := LinkToTransaction(ctx, d, 999, 1); err != nil || document != nil {
		t.Errorf("LinkToTransaction(999) = %v, %v", document, err)
	}
	if document, err := UnlinkFromTransaction(ctx, d, 999); err != nil || document != nil {
		t.Errorf("UnlinkFromTransaction(999) = %v, %v", document, err)
	}
	if deleted, err := Delete(ctx, d, 999); err != nil || deleted {
		t.Errorf("Delete(999) = %v, %v", deleted, err)
	}
}

func TestEveryDocumentMutationWritesOneAuditEntry(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	transactionID := transaction(t, d)
	before := len(entries(t, d))
	created := document(t, d, "audit.pdf", nil)

	steps := []struct {
		action  func() error
		name    string
		details string
	}{
		{
			action:  func() error { _, err := LinkToTransaction(ctx, d, created.ID, transactionID); return err },
			name:    "document.link_to_transaction",
			details: "id=" + itoa(created.ID),
		},
		{
			action:  func() error { _, err := UnlinkFromTransaction(ctx, d, created.ID); return err },
			name:    "document.unlink_from_transaction",
			details: "id=" + itoa(created.ID),
		},
		{
			action:  func() error { _, err := SaveOCRText(ctx, d, created.ID, "text", "de", nil); return err },
			name:    "document.save_ocr_text",
			details: "id=" + itoa(created.ID),
		},
		{
			action:  func() error { _, err := Delete(ctx, d, created.ID); return err },
			name:    "document.delete",
			details: "id=" + itoa(created.ID),
		},
	}

	createEntries := entries(t, d)
	if len(createEntries) != before+1 {
		t.Fatalf("audit entries after create = %d, want %d", len(createEntries), before+1)
	}
	if createEntries[0].Action != "document.create" ||
		createEntries[0].Actor != audit.ActorUser ||
		createEntries[0].Details != "id="+itoa(created.ID)+" name=audit.pdf" {
		t.Errorf("create audit = %+v", createEntries[0])
	}

	for _, step := range steps {
		before := len(entries(t, d))
		if err := step.action(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		recent := entries(t, d)
		if len(recent) != before+1 {
			t.Fatalf("audit entries after %s = %d, want %d", step.name, len(recent), before+1)
		}
		if recent[0].Action != step.name || recent[0].Actor != audit.ActorUser ||
			recent[0].Details != step.details {
			t.Errorf("audit entry for %s = %+v", step.name, recent[0])
		}
	}
}
