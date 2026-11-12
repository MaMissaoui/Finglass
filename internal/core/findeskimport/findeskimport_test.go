package findeskimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	fixturePassphrase = "fixture passphrase 1"
	targetPassphrase  = "target passphrase 1"
)

type importedFixtureResult struct {
	db           *db.Database
	documentsDir string
	sourceDir    string
	report       *FinDeskImportReport
}

func fixtureCopy(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "findesk")
	destination := filepath.Join(t.TempDir(), "findesk")
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return destination
}

func passphrase(value string, purpose secret.Purpose) *secret.Passphrase {
	pass, err := secret.NewPassphrase([]byte(value), purpose)
	if err != nil {
		panic(err)
	}
	return pass
}

func offset() *time.Location {
	return time.FixedZone("", 2*60*60)
}

func newTarget(t *testing.T) (*db.Database, string) {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, "finglass.db")
	opened, err := db.Create(path, passphrase(targetPassphrase, secret.New))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = opened.Close()
	})
	return opened, filepath.Join(root, "documents")
}

func importInto(t *testing.T, target *db.Database, documentsDir, sourceDir string) (*FinDeskImportReport, error) {
	t.Helper()
	return Import(
		target,
		documentsDir,
		sourceDir,
		passphrase(fixturePassphrase, secret.Unlock),
		offset(),
	)
}

func importedFixture(t *testing.T) *importedFixtureResult {
	t.Helper()

	sourceDir := fixtureCopy(t)
	target, documentsDir := newTarget(t)
	report, err := importInto(t, target, documentsDir, sourceDir)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return &importedFixtureResult{
		db:           target,
		documentsDir: documentsDir,
		sourceDir:    sourceDir,
		report:       report,
	}
}

func scalarInt64(t *testing.T, database *db.Database, statement string) int64 {
	t.Helper()

	var value int64
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, statement).Scan(&value)
	})
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	return value
}

func optionalText(t *testing.T, database *db.Database, statement string) *string {
	t.Helper()

	var value *string
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, statement).Scan(&value)
	})
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	return value
}

func tableExists(t *testing.T, database *db.Database, name string) bool {
	t.Helper()
	return scalarInt64(
		t,
		database,
		fmt.Sprintf("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '%s'", name),
	) > 0
}

type storedTransaction struct {
	id                int64
	date              string
	amountMinor       int64
	currency          string
	partner           *string
	purpose           *string
	hash              string
	balanceAfterMinor *int64
	categoryID        *int64
}

func storedTransactions(t *testing.T, database *db.Database) []storedTransaction {
	t.Helper()

	var transactions []storedTransaction
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			`SELECT id, booking_date, amount_minor, currency, partner_name, purpose, import_hash,
			        balance_after_minor, category_id
			 FROM transactions ORDER BY id`,
		)
		if err != nil {
			return err
		}
		defer func() {
			_ = rows.Close()
		}()
		for rows.Next() {
			var transaction storedTransaction
			if err := rows.Scan(
				&transaction.id,
				&transaction.date,
				&transaction.amountMinor,
				&transaction.currency,
				&transaction.partner,
				&transaction.purpose,
				&transaction.hash,
				&transaction.balanceAfterMinor,
				&transaction.categoryID,
			); err != nil {
				return err
			}
			transactions = append(transactions, transaction)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read transactions: %v", err)
	}
	return transactions
}

func decimalFromMinor(minor int64, currencyText string) decimal.Decimal {
	currency, err := money.NewCurrency(currencyText)
	if err != nil {
		panic(err)
	}
	var scale int32
	switch currency.MinorExponent() {
	case 0:
		scale = 0
	case 2:
		scale = 2
	case 3:
		scale = 3
	default:
		panic("unexpected currency exponent")
	}
	return decimal.New(minor, scale)
}

func snapshotFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()

	files := make(map[string][]byte)
	rootFS := os.DirFS(root)
	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(rootFS, path)
		if err != nil {
			return err
		}
		files[path] = data
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return files
}

func TestTheFixtureImportsWithTheExpectedCounts(t *testing.T) {
	fixture := importedFixture(t)
	report := fixture.report

	if report.AccountGroups.Imported != 1 {
		t.Errorf("account groups = %d, want 1", report.AccountGroups.Imported)
	}
	if report.BankConnections.Imported != 2 {
		t.Errorf("bank connections = %d, want 2", report.BankConnections.Imported)
	}
	if report.Accounts.Imported != 3 {
		t.Errorf("accounts = %d, want 3", report.Accounts.Imported)
	}
	if report.Categories.Imported != 3 {
		t.Errorf("categories = %d, want 3", report.Categories.Imported)
	}
	if report.Rules.Imported != 2 || report.Rules.Skipped != 2 ||
		report.Rules.SkippedOrphan != 1 || report.Rules.SkippedInvalid != 1 {
		t.Errorf("rules = %+v", report.Rules)
	}
	if report.Transactions.Imported != 8 || report.Transactions.Skipped != 2 ||
		report.Transactions.SkippedOrphan != 1 || report.Transactions.SkippedFractionalAmount != 1 {
		t.Errorf("transactions = %+v", report.Transactions)
	}
	if report.DeletedBookings.Imported != 1 {
		t.Errorf("deleted bookings = %d, want 1", report.DeletedBookings.Imported)
	}
	if report.PendingBookings.Imported != 1 {
		t.Errorf("pending bookings = %d, want 1", report.PendingBookings.Imported)
	}
	if report.Documents.Copied != 1 || report.Documents.Missing != 2 || report.Documents.Refused != 0 {
		t.Errorf("documents = %+v", report.Documents)
	}
	if report.OCRTexts.Imported != 1 {
		t.Errorf("OCR texts = %d, want 1", report.OCRTexts.Imported)
	}
	if report.Settings.Imported != 8 || report.Settings.Skipped != 2 {
		t.Errorf("settings = %+v", report.Settings)
	}
	if report.PaymentOrdersLeftBehind != 1 {
		t.Errorf("payment orders left behind = %d, want 1", report.PaymentOrdersLeftBehind)
	}

	counts := map[string]int64{
		"account_groups":   1,
		"bank_connections": 2,
		"accounts":         3,
		"categories":       3,
		"rules":            2,
		"transactions":     8,
		"deleted_bookings": 1,
		"pending_bookings": 1,
		"documents":        1,
		"ocr_texts":        1,
	}
	for table, expected := range counts {
		if got := scalarInt64(t, fixture.db, "SELECT count(*) FROM "+table); got != expected {
			t.Errorf("%s count = %d, want %d", table, got, expected)
		}
	}

	summary := report.Summary(i18n.En)
	if !strings.HasPrefix(summary, "Imported 1 account groups") {
		t.Errorf("summary = %q", summary)
	}
	if summary == report.Summary(i18n.De) {
		t.Error("German summary equals English")
	}
	if got := report.TotalSkipped(); got != 2+2+2+2 {
		t.Errorf("total skipped = %d, want 8", got)
	}
}

func TestAmountsDatesBooleansAndTaxOverridesMatchTheSource(t *testing.T) {
	fixture := importedFixture(t)
	database := fixture.db

	assertInt := func(statement string, expected int64) {
		t.Helper()
		if got := scalarInt64(t, database, statement); got != expected {
			t.Errorf("%s = %d, want %d", statement, got, expected)
		}
	}
	assertText := func(statement string, expected *string) {
		t.Helper()
		got := optionalText(t, database, statement)
		if got == nil || expected == nil {
			if got != nil || expected != nil {
				t.Errorf("%s = %v, want %v", statement, got, expected)
			}
			return
		}
		if *got != *expected {
			t.Errorf("%s = %q, want %q", statement, *got, *expected)
		}
	}
	text := func(value string) *string { return &value }

	assertInt("SELECT balance_minor FROM accounts WHERE id = 1", -123_456)
	assertInt("SELECT balance_minor FROM accounts WHERE id = 2", 15_000)
	assertInt("SELECT balance_minor FROM accounts WHERE id = 3", -20_000_000)
	assertInt("SELECT budget_limit_minor FROM categories WHERE id = 1", 25_050)

	transactions := storedTransactions(t, database)
	byID := func(id int64) storedTransaction {
		t.Helper()
		for _, transaction := range transactions {
			if transaction.id == id {
				return transaction
			}
		}
		t.Fatalf("transaction %d not found", id)
		return storedTransaction{}
	}

	if got := byID(1).amountMinor; got != -10 {
		t.Errorf("id 1 amount = %d, want -10 (-0.10 EUR)", got)
	}
	if got := byID(2).amountMinor; got != -123_456_789 {
		t.Errorf("id 2 amount = %d, want -123456789", got)
	}
	if got := byID(3).amountMinor; got != 10_000 {
		t.Errorf("id 3 amount = %d, want 10000", got)
	}
	if got := byID(4).amountMinor; got != -2_500 {
		t.Errorf("id 4 amount = %d, want -2500", got)
	}
	if got := byID(5).amountMinor; got != -1_230 {
		t.Errorf("id 5 amount = %d, want -1230", got)
	}
	if got := byID(6).amountMinor; got != -1_500 {
		t.Errorf("id 6 amount = %d, want -1500 JPY", got)
	}
	if got := byID(7).amountMinor; got != 50_000 {
		t.Errorf("id 7 amount = %d, want 50000", got)
	}
	if got := byID(5).balanceAfterMinor; got == nil || *got != 98_770 {
		t.Errorf("id 5 balance after = %v, want 98770", got)
	}
	if got := byID(8).amountMinor; got != -300 {
		t.Errorf("id 8 amount = %d, want -300", got)
	}
	if got := byID(8).categoryID; got != nil {
		t.Errorf("id 8 category = %v, want NULL", got)
	}

	for _, transaction := range transactions {
		if transaction.date != "2026-10-01" {
			t.Errorf("transaction %d date = %q", transaction.id, transaction.date)
		}
	}
	assertText("SELECT created_at FROM transactions WHERE id = 1", text("2026-10-18T09:30:15.123+02:00"))
	assertText("SELECT balance_updated_at FROM accounts WHERE id = 1", text("2026-10-18T09:30:15.123+02:00"))
	assertText("SELECT last_sync FROM bank_connections WHERE id = 1", text("2026-10-18T09:30:15.123+02:00"))
	assertText("SELECT sync_from FROM bank_connections WHERE id = 1", text("2025-01-01"))

	assertInt("SELECT is_new FROM transactions WHERE id = 1", 1)
	assertInt("SELECT is_new FROM transactions WHERE id = 2", 0)
	assertInt("SELECT count(*) FROM transactions WHERE id = 1 AND tax_override IS NULL", 1)
	assertInt("SELECT tax_override FROM transactions WHERE id = 2", 0)
	assertInt("SELECT tax_override FROM transactions WHERE id = 3", 1)

	assertInt("SELECT include_in_sync_all FROM bank_connections WHERE id = 1", 0)
	assertInt("SELECT offline FROM bank_connections WHERE id = 1", 0)
	assertInt("SELECT offline FROM bank_connections WHERE id = 2", 1)
	assertInt("SELECT count(*) FROM bank_connections WHERE capabilities IS NOT NULL", 0)
	assertInt("SELECT count(*) FROM accounts WHERE institution IS NOT NULL", 0)

	assertInt("SELECT is_tax_relevant FROM categories WHERE id = 2", 1)
	assertInt("SELECT is_transfer FROM categories WHERE id = 3", 1)
	assertText("SELECT match_field FROM rules WHERE id = 1", text("partner"))
	assertText("SELECT direction FROM rules WHERE id = 1", text("expense"))
	assertInt("SELECT auto_apply FROM rules WHERE id = 2", 0)
	assertInt("SELECT tax_relevant FROM rules WHERE id = 2", 1)
	assertText("SELECT value FROM settings WHERE key = 'ui.theme'", text("dark"))
	assertText("SELECT value FROM settings WHERE key = 'accounts.collapsedBanks'", text("2"))

	assertInt("SELECT amount_minor FROM pending_bookings WHERE id = 1", -999)
	assertText("SELECT value_date FROM pending_bookings WHERE id = 1", nil)
	assertText("SELECT fetched_at FROM pending_bookings WHERE id = 1", text("2026-10-18T09:30:15.123+02:00"))
	assertText("SELECT deleted_at FROM deleted_bookings WHERE id = 1", text("2026-10-18T09:30:15.123+02:00"))
}

func TestStoredHashesAreCopiedVerbatimAndDedupFindsNothingNew(t *testing.T) {
	fixture := importedFixture(t)
	transactions := storedTransactions(t, fixture.db)
	day, ok := civil.New(2026, time.October, 1)
	if !ok {
		t.Fatal("invalid date")
	}

	for _, transaction := range transactions {
		if transaction.id != 1 && transaction.id != 2 && transaction.id != 3 &&
			transaction.id != 6 && transaction.id != 7 {
			continue
		}
		amount := decimalFromMinor(transaction.amountMinor, transaction.currency)
		if got := imports.TransactionHash(day, amount, transaction.partner, transaction.purpose); got != transaction.hash {
			t.Errorf("id %d hash = %s, want %s", transaction.id, got, transaction.hash)
		}
	}

	var bankID storedTransaction
	var balance storedTransaction
	for _, transaction := range transactions {
		switch transaction.id {
		case 4:
			bankID = transaction
		case 5:
			balance = transaction
		}
	}
	bankIDPurpose := stringValue(bankID.purpose) + "#DE02120300000000202051#B-77"
	if got := imports.TransactionHash(
		day,
		decimalFromMinor(bankID.amountMinor, bankID.currency),
		bankID.partner,
		&bankIDPurpose,
	); got != bankID.hash {
		t.Errorf("bank id hash = %s, want %s", got, bankID.hash)
	}
	balancePurpose := stringValue(balance.purpose) + "#DE02120300000000202051#987.70"
	if got := imports.TransactionHash(
		day,
		decimalFromMinor(balance.amountMinor, balance.currency),
		balance.partner,
		&balancePurpose,
	); got != balance.hash {
		t.Errorf("balance hash = %s, want %s", got, balance.hash)
	}

	candidates := make([]imports.Candidate[int64], 0)
	for _, transaction := range transactions {
		if transaction.id != 1 && transaction.id != 2 && transaction.id != 3 &&
			transaction.id != 4 && transaction.id != 5 && transaction.id != 8 {
			continue
		}
		candidates = append(candidates, imports.Candidate[int64]{
			Item:   transaction.id,
			Date:   day,
			Amount: decimalFromMinor(transaction.amountMinor, transaction.currency),
			Hash:   transaction.hash,
		})
	}

	plan, err := imports.PlanDuplicates(
		context.Background(),
		fixture.db,
		model.AccountID(1),
		money.EUR,
		candidates,
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}
	if len(plan.ToImport) != 0 {
		t.Errorf("to import = %d, want 0", len(plan.ToImport))
	}
	if plan.ExactDuplicates != 6 || plan.ProbableDuplicates != 0 || plan.PreviouslyDeleted != 0 {
		t.Errorf("plan = %+v", plan)
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestAWrongPassphraseLeavesTheTargetUnchanged(t *testing.T) {
	sourceDir := fixtureCopy(t)
	target, documentsDir := newTarget(t)
	err := target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO categories (id, name, color) VALUES (1, 'Seed', '#3BC7B0')",
		)
		return err
	})
	if err != nil {
		t.Fatalf("seed category: %v", err)
	}

	_, err = Import(
		target,
		documentsDir,
		sourceDir,
		passphrase("wrong passphrase", secret.Unlock),
		offset(),
	)
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("Import = %v, want ErrWrongPassphrase", err)
	}

	if got := scalarInt64(t, target, "SELECT count(*) FROM categories"); got != 1 {
		t.Errorf("categories = %d, want 1", got)
	}
	if got := optionalText(t, target, "SELECT name FROM categories WHERE id = 1"); got == nil || *got != "Seed" {
		t.Errorf("category name = %v, want Seed", got)
	}
	for _, table := range []string{"accounts", "bank_connections", "transactions", "documents", "audit_log"} {
		if got := scalarInt64(t, target, "SELECT count(*) FROM "+table); got != 0 {
			t.Errorf("%s count = %d, want 0", table, got)
		}
	}
	if _, err := os.Stat(documentsDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("documents folder exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "findesk.db")); err != nil {
		t.Errorf("source database missing: %v", err)
	}
}

func TestANonEmptyTargetIsRefusedAndSeededCategoriesAreReplaced(t *testing.T) {
	sourceDir := fixtureCopy(t)

	target, documentsDir := newTarget(t)
	err := target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EUR')")
		return err
	})
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}

	if _, err := importInto(t, target, documentsDir, sourceDir); !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("Import = %v, want ErrTargetNotEmpty", err)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM accounts"); got != 1 {
		t.Errorf("accounts = %d, want 1", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM categories"); got != 0 {
		t.Errorf("categories = %d, want 0", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM audit_log"); got != 0 {
		t.Errorf("audit entries = %d, want 0", got)
	}
	if _, err := os.Stat(documentsDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("documents folder exists: %v", err)
	}

	seeded, seededDocuments := newTarget(t)
	err = seeded.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO categories (id, name, color) VALUES (1, 'Seeded default', '#3BC7B0')",
		)
		return err
	})
	if err != nil {
		t.Fatalf("seed category: %v", err)
	}

	report, err := importInto(t, seeded, seededDocuments, sourceDir)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Categories.Imported != 3 {
		t.Errorf("categories imported = %d, want 3", report.Categories.Imported)
	}

	var names []string
	err = seeded.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT name FROM categories ORDER BY id")
		if err != nil {
			return err
		}
		defer func() {
			_ = rows.Close()
		}()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read categories: %v", err)
	}
	want := []string{"Food", "Donations", "Own transfers"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("categories = %v, want %v", names, want)
	}
	for _, name := range names {
		if name == "Seeded default" {
			t.Error("seeded category was not replaced")
		}
	}
}

func TestATargetThatFillsAfterTheEarlyCheckIsRefusedInsideTheTransaction(t *testing.T) {
	sourceDir := fixtureCopy(t)
	target, documentsDir := newTarget(t)

	source, err := openSource(sourceDir, passphrase(fixturePassphrase, secret.Unlock))
	if err != nil {
		t.Fatalf("openSource: %v", err)
	}
	data, err := source.read()
	if err != nil {
		_ = source.Close()
		t.Fatalf("read: %v", err)
	}

	err = target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EUR')"); err != nil {
			return err
		}
		_, err := importInConnection(ctx, conn, data, sourceDir, documentsDir, offset())
		if !errors.Is(err, ErrTargetNotEmpty) {
			t.Errorf("importInConnection = %v, want ErrTargetNotEmpty", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithConn: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := scalarInt64(t, target, "SELECT count(*) FROM categories"); got != 0 {
		t.Errorf("categories = %d, want 0", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM accounts"); got != 1 {
		t.Errorf("accounts = %d, want 1", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM audit_log"); got != 0 {
		t.Errorf("audit entries = %d, want 0", got)
	}
	if _, err := os.Stat(documentsDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("documents folder exists: %v", err)
	}
}

func TestNoPaymentDataReachesTheTarget(t *testing.T) {
	fixture := importedFixture(t)
	database := fixture.db

	if tableExists(t, database, "payment_orders") {
		t.Error("the target has a payment_orders table")
	}
	if fixture.report.PaymentOrdersLeftBehind != 1 {
		t.Errorf("payment orders left behind = %d, want 1", fixture.report.PaymentOrdersLeftBehind)
	}

	var settings []struct{ key, value string }
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT key, value FROM settings")
		if err != nil {
			return err
		}
		defer func() {
			_ = rows.Close()
		}()
		for rows.Next() {
			var entry struct{ key, value string }
			if err := rows.Scan(&entry.key, &entry.value); err != nil {
				return err
			}
			settings = append(settings, entry)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	forbidden := []string{"Payee", "TRANSFER", "fixture-user"}
	for _, entry := range settings {
		if strings.HasPrefix(entry.key, "fints.pinSaved.") {
			t.Errorf("saved PIN setting leaked: %s", entry.key)
		}
		for _, marker := range forbidden {
			if strings.Contains(entry.key, marker) {
				t.Errorf("setting key leaked %s", marker)
			}
			if strings.Contains(entry.value, marker) {
				t.Errorf("setting value leaked %s", marker)
			}
		}
	}

	// Scan every target table, not just settings, for personal payment data.
	tables := make([]string, 0)
	err = database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'",
		)
		if err != nil {
			return err
		}
		defer func() {
			_ = rows.Close()
		}()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read tables: %v", err)
	}

	for _, table := range tables {
		err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
			rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM "%s"`, table))
			if err != nil {
				return err
			}
			defer func() {
				_ = rows.Close()
			}()
			columns, err := rows.Columns()
			if err != nil {
				return err
			}
			for rows.Next() {
				values := make([]any, len(columns))
				destinations := make([]any, len(columns))
				for index := range values {
					destinations[index] = &values[index]
				}
				if err := rows.Scan(destinations...); err != nil {
					return err
				}
				for index, value := range values {
					var text string
					switch typed := value.(type) {
					case nil:
						continue
					case string:
						text = typed
					case []byte:
						text = string(typed)
					default:
						text = fmt.Sprint(typed)
					}
					for _, marker := range forbidden {
						// The bank connection's login user id is imported
						// on purpose, exactly as Rust imports it; it is not
						// payment data.
						if table == "bank_connections" && columns[index] == "user_id" && marker == "fixture-user" {
							continue
						}
						if strings.Contains(text, marker) {
							t.Errorf("table %s leaked %s: %q", table, marker, text)
						}
					}
				}
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("scan table %s: %v", table, err)
		}
	}

	entries, err := audit.Recent(context.Background(), database, 100)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	for _, entry := range entries {
		for _, marker := range forbidden {
			if strings.Contains(entry.Details, marker) {
				t.Errorf("audit leaked %s: %s", marker, entry.Details)
			}
		}
	}
}

func TestOnlyTheReceiptDocumentIsCopiedAndItsOCRTextIsKept(t *testing.T) {
	fixture := importedFixture(t)
	database := fixture.db

	var (
		id            int64
		fileName      string
		storedPath    string
		mimeType      string
		uploadedAt    string
		transactionID *int64
	)
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(
			ctx,
			`SELECT id, file_name, stored_path, mime_type, uploaded_at, transaction_id
			 FROM documents WHERE id = 1`,
		).Scan(&id, &fileName, &storedPath, &mimeType, &uploadedAt, &transactionID)
	})
	if err != nil {
		t.Fatalf("read document: %v", err)
	}

	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
	if fileName != "receipt.pdf" {
		t.Errorf("file name = %q", fileName)
	}
	if mimeType != "application/pdf" {
		t.Errorf("mime type = %q", mimeType)
	}
	if uploadedAt != "2026-10-18T09:30:15.123+02:00" {
		t.Errorf("uploaded at = %q", uploadedAt)
	}
	if transactionID == nil || *transactionID != 1 {
		t.Errorf("transaction id = %v, want 1", transactionID)
	}

	stored := storedPath
	if filepath.Dir(stored) != fixture.documentsDir {
		t.Errorf("stored parent = %q, want %q", filepath.Dir(stored), fixture.documentsDir)
	}
	if info, err := os.Stat(stored); err != nil || !info.Mode().IsRegular() {
		t.Errorf("stored file %q: %v", stored, err)
	}
	if !strings.HasSuffix(filepath.Base(stored), "_receipt.pdf") {
		t.Errorf("stored name = %q", filepath.Base(stored))
	}
	if filepath.Base(stored) == "11111111-2222-3333-4444-555555555555_receipt.pdf" {
		t.Error("the FinDesk file name was reused")
	}
	if mode := fileMode(t, stored); mode != 0o600 {
		t.Errorf("stored mode = %o, want 600", mode)
	}

	if got := optionalText(t, database, "SELECT content FROM ocr_texts WHERE document_id = 1"); got == nil || *got != "REWE Summe 0,10" {
		t.Errorf("OCR content = %v", got)
	}
	if got := optionalText(t, database, "SELECT language FROM ocr_texts WHERE document_id = 1"); got == nil || *got != "de" {
		t.Errorf("OCR language = %v", got)
	}
	var confidence float64
	err = database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(
			ctx,
			"SELECT confidence FROM ocr_texts WHERE document_id = 1",
		).Scan(&confidence)
	})
	if err != nil {
		t.Fatalf("read confidence: %v", err)
	}
	if confidence != 87.5 {
		t.Errorf("confidence = %v, want 87.5", confidence)
	}

	entries, err := os.ReadDir(fixture.documentsDir)
	if err != nil {
		t.Fatalf("read documents dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("documents dir has %d entries, want 1", len(entries))
	}
	if fixture.report.Documents.Copied != 1 || fixture.report.Documents.Missing != 2 {
		t.Errorf("documents = %+v", fixture.report.Documents)
	}
	if fixture.report.OCRTexts.Imported != 1 {
		t.Errorf("OCR texts = %d, want 1", fixture.report.OCRTexts.Imported)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestTheAuditLogGainsExactlyOneEntryWithoutPersonalData(t *testing.T) {
	fixture := importedFixture(t)
	entries, err := audit.Recent(context.Background(), fixture.db, 100)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	if entries[0].Action != "FINDESK_IMPORTED" {
		t.Errorf("action = %q", entries[0].Action)
	}
	if entries[0].Actor != audit.ActorImport {
		t.Errorf("actor = %v, want import", entries[0].Actor)
	}
	for _, forbidden := range []string{
		"DE02120300000000202051",
		"fixture-user",
		"Fixture Bank",
		"Cash box",
		"REWE Markt",
		"Kassenbon",
		"REWE Summe",
		"receipt.pdf",
		"findesk",
	} {
		if strings.Contains(entries[0].Details, forbidden) {
			t.Errorf("audit details leaked %s: %s", forbidden, entries[0].Details)
		}
	}
}

func TestTheSourceFolderIsByteIdenticalAfterTheImport(t *testing.T) {
	sourceDir := fixtureCopy(t)
	before := snapshotFiles(t, sourceDir)

	target, documentsDir := newTarget(t)
	if _, err := importInto(t, target, documentsDir, sourceDir); err != nil {
		t.Fatalf("Import: %v", err)
	}

	after := snapshotFiles(t, sourceDir)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("the source folder changed")
	}
	for relative := range before {
		if string(before[relative]) != string(after[relative]) {
			t.Errorf("source file %s changed", relative)
		}
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "outside-secret.txt")); err != nil {
		t.Errorf("outside-secret.txt missing: %v", err)
	}
	if info, err := os.Stat(filepath.Join(sourceDir, "documents")); err != nil || !info.IsDir() {
		t.Errorf("documents folder missing: %v", err)
	}
}

func TestAFailedTransactionRemovesCopiedFiles(t *testing.T) {
	sourceDir := fixtureCopy(t)
	target, documentsDir := newTarget(t)

	err := target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			`CREATE TRIGGER fail_audit BEFORE INSERT ON audit_log
			 BEGIN SELECT RAISE(ABORT, 'blocked'); END;`,
		)
		return err
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err = importInto(t, target, documentsDir, sourceDir)
	var importError *FinDeskImportError
	if !errors.As(err, &importError) || importError.Kind != KindDatabase {
		t.Fatalf("Import = %v, want a database error", err)
	}

	for _, table := range []string{"accounts", "transactions", "documents", "ocr_texts", "categories", "audit_log"} {
		if got := scalarInt64(t, target, "SELECT count(*) FROM "+table); got != 0 {
			t.Errorf("%s count = %d, want 0", table, got)
		}
	}
	entries, err := os.ReadDir(documentsDir)
	if err != nil {
		t.Fatalf("read documents dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("documents dir has %d entries, want 0", len(entries))
	}
}

func TestTheErrorMessagesFollowTheLanguage(t *testing.T) {
	for _, importError := range []*FinDeskImportError{
		ErrNotFound,
		ErrWrongPassphrase,
		ErrNotFinDesk,
		ErrTargetNotEmpty,
	} {
		english := importError.Message(i18n.En)
		german := importError.Message(i18n.De)
		french := importError.Message(i18n.Fr)
		if english == "" {
			t.Errorf("%v has an empty English message", importError.Kind)
		}
		if english == german {
			t.Errorf("%v German equals English", importError.Kind)
		}
		if english == french {
			t.Errorf("%v French equals English", importError.Kind)
		}
	}
}

func TestAMissingSourceIsNotFoundAndAForeignDatabaseIsNotFinDesk(t *testing.T) {
	target, documentsDir := newTarget(t)

	missing := t.TempDir()
	if _, err := importInto(t, target, documentsDir, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Import(missing) = %v, want ErrNotFound", err)
	}

	foreign := t.TempDir()
	path := filepath.Join(foreign, "findesk.db")
	foreignDatabase, err := db.Create(path, passphrase(fixturePassphrase, secret.New))
	if err != nil {
		t.Fatalf("Create foreign database: %v", err)
	}
	if err := foreignDatabase.Close(); err != nil {
		t.Fatalf("close foreign database: %v", err)
	}

	if _, err := importInto(t, target, documentsDir, foreign); !errors.Is(err, ErrNotFinDesk) {
		t.Fatalf("Import(foreign) = %v, want ErrNotFinDesk", err)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM audit_log"); got != 0 {
		t.Errorf("audit entries = %d, want 0", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM accounts"); got != 0 {
		t.Errorf("accounts = %d, want 0", got)
	}
}

func TestADatabaseFromBeforeGroupsPendingBookingsAndPaymentsStillImports(t *testing.T) {
	sourceDir := fixtureCopy(t)
	execOnSource(t, sourceDir,
		"UPDATE accounts SET group_id = NULL",
		"DROP TABLE account_groups",
		"DROP TABLE deleted_bookings",
		"DROP TABLE pending_bookings",
		"DROP TABLE payment_orders",
	)

	target, documentsDir := newTarget(t)
	report, err := importInto(t, target, documentsDir, sourceDir)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if report.AccountGroups.Imported != 0 {
		t.Errorf("account groups = %d, want 0", report.AccountGroups.Imported)
	}
	if report.DeletedBookings.Imported != 0 {
		t.Errorf("deleted bookings = %d, want 0", report.DeletedBookings.Imported)
	}
	if report.PendingBookings.Imported != 0 {
		t.Errorf("pending bookings = %d, want 0", report.PendingBookings.Imported)
	}
	if report.PaymentOrdersLeftBehind != 0 {
		t.Errorf("payment orders left behind = %d, want 0", report.PaymentOrdersLeftBehind)
	}
	if report.Accounts.Imported != 3 {
		t.Errorf("accounts = %d, want 3", report.Accounts.Imported)
	}
	if report.Transactions.Imported == 0 {
		t.Error("transactions imported = 0")
	}
}

func execOnSource(t *testing.T, sourceDir string, statements ...string) {
	t.Helper()

	opened, err := db.OpenUnmigrated(
		filepath.Join(sourceDir, "findesk.db"),
		passphrase(fixturePassphrase, secret.Unlock),
	)
	if err != nil {
		t.Fatalf("OpenUnmigrated: %v", err)
	}
	defer func() {
		_ = opened.Close()
	}()

	err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		for _, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("exec on source: %v", err)
	}
}

func TestTheSourceSnapshotComparisonDetectsAChange(t *testing.T) {
	stamp := func(size int64, seconds int64) *fileStamp {
		return &fileStamp{size: size, modified: time.Unix(seconds, 0).UTC()}
	}
	before := sourceSnapshot{
		database: stamp(100, 10),
		wal:      stamp(20, 11),
	}

	if sourceChanged(before, before) {
		t.Error("an unchanged snapshot was reported as changed")
	}

	grown := before
	grown.database = stamp(101, 10)
	if !sourceChanged(before, grown) {
		t.Error("a grown database was not detected")
	}

	touched := before
	touched.wal = stamp(20, 12)
	if !sourceChanged(before, touched) {
		t.Error("a touched WAL was not detected")
	}

	disappeared := before
	disappeared.wal = nil
	if !sourceChanged(before, disappeared) {
		t.Error("a disappeared WAL was not detected")
	}
}

func TestABudgetWithTooManyDecimalsKeepsItsCategory(t *testing.T) {
	sourceDir := fixtureCopy(t)
	execOnSource(t, sourceDir, "UPDATE categories SET budget_limit = 250.505 WHERE id = 1")

	target, documentsDir := newTarget(t)
	report, err := importInto(t, target, documentsDir, sourceDir)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Categories.Imported != 3 {
		t.Errorf("categories = %d, want 3", report.Categories.Imported)
	}
	if got := optionalText(t, target, "SELECT CAST(budget_limit_minor AS TEXT) FROM categories WHERE id = 1"); got != nil {
		t.Errorf("budget limit = %v, want NULL", got)
	}
	if got := scalarInt64(t, target, "SELECT count(*) FROM rules WHERE category_id = 1"); got != 1 {
		t.Errorf("rules for category 1 = %d, want 1", got)
	}
}
