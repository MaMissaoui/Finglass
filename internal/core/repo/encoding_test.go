package repo_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func newDatabase(t *testing.T) *db.Database {
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

func pointer(value string) *string {
	return &value
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()

	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

// TestRowsWrittenByGoMatchRustEncoding creates one row of every domain table
// with every optional field set, then reads the raw columns back and checks the
// stored text.
func TestRowsWrittenByGoMatchRustEncoding(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()

	group, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Group", SortOrder: 5})
	if err != nil {
		t.Fatalf("accountgroups.Create: %v", err)
	}

	bank, err := bankconnections.Create(ctx, d, model.NewBankConnection{
		BankName: "Bank",
		BLZ:      "37040044",
		BIC:      pointer("DEUTDEDBXXX"),
		UserID:   "user",
	})
	if err != nil {
		t.Fatalf("bankconnections.Create: %v", err)
	}
	serverURL := "https://bank.test/fints"
	if _, err := bankconnections.UpdateConnection(ctx, d, bank.ID, model.BankConnectionUpdate{
		ServerURL: &serverURL,
		BIC:       pointer("   "),
	}); err != nil {
		t.Fatalf("bankconnections.UpdateConnection: %v", err)
	}
	tanMethod := "942"
	if _, err := bankconnections.UpdateConnection(ctx, d, bank.ID, model.BankConnectionUpdate{
		TanMethod: &tanMethod,
	}); err != nil {
		t.Fatalf("bankconnections.UpdateConnection(tan): %v", err)
	}
	syncFrom := date(t, 2026, time.January, 1)
	if _, err := bankconnections.SetSyncFrom(ctx, d, bank.ID, &syncFrom); err != nil {
		t.Fatalf("bankconnections.SetSyncFrom: %v", err)
	}
	if _, err := bankconnections.UpdateLastSync(ctx, d, bank.ID); err != nil {
		t.Fatalf("bankconnections.UpdateLastSync: %v", err)
	}
	if _, err := bankconnections.UpdateCapabilities(ctx, d, bank.ID, "SaldoReq"); err != nil {
		t.Fatalf("bankconnections.UpdateCapabilities: %v", err)
	}
	if _, err := bankconnections.SetIncludeInSyncAll(ctx, d, bank.ID, false); err != nil {
		t.Fatalf("bankconnections.SetIncludeInSyncAll: %v", err)
	}
	bankRow, ok, err := bankconnections.Get(ctx, d, bank.ID)
	if err != nil || !ok {
		t.Fatalf("bankconnections.Get: %v, %v", ok, err)
	}
	if bankRow.LastSync == nil {
		t.Fatal("the bank row has no last sync")
	}

	balanceAt := time.Date(2026, time.January, 2, 3, 4, 5, 123456789, time.FixedZone("CET", 60*60))
	institution := "Institution"
	accountName := "Account"
	iban := "DE89370400440532013000"
	accountNumber := "12345678"
	account, err := accounts.Create(ctx, d, model.NewAccount{
		BankConnectionID: &bank.ID,
		Institution:      &institution,
		Name:             &accountName,
		IBAN:             &iban,
		AccountNumber:    &accountNumber,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeCreditCard,
		Balance:          money.Money{Minor: 1234, Currency: money.EUR},
		BalanceUpdatedAt: &balanceAt,
		GroupID:          &group,
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}

	budget := money.Money{Minor: 50_000, Currency: money.EUR}
	category, err := categories.Create(ctx, d, model.NewCategory{
		Name:          "Category",
		Color:         "#000000",
		BudgetLimit:   &budget,
		IsTaxRelevant: true,
		IsTransfer:    true,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}

	taxRelevant := true
	rule, err := rules.Create(ctx, d, model.NewRule{
		Pattern:     "pattern",
		CategoryID:  category,
		Priority:    3,
		AutoApply:   false,
		MatchField:  model.RuleFieldPartner,
		Direction:   model.RuleDirectionExpense,
		TaxRelevant: &taxRelevant,
	})
	if err != nil {
		t.Fatalf("rules.Create: %v", err)
	}

	partner := "Partner"
	purpose := "Purpose"
	valueDate := date(t, 2026, time.October, 2)
	fetchedAt := time.Date(2026, time.October, 4, 9, 30, 0, 0, time.UTC)
	if err := pendingbookings.ReplaceForAccount(ctx, d, account, []model.NewPendingBooking{{
		Date:        date(t, 2026, time.October, 3),
		ValueDate:   &valueDate,
		Amount:      money.Money{Minor: -1000, Currency: money.EUR},
		PartnerName: &partner,
		Purpose:     &purpose,
	}}, fetchedAt); err != nil {
		t.Fatalf("pendingbookings.ReplaceForAccount: %v", err)
	}

	var transactionID int64
	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		result, err := conn.ExecContext(
			ctx,
			"INSERT INTO transactions "+
				"(account_id, booking_date, amount_minor, currency, import_hash, created_at, is_new) "+
				"VALUES (?1, '2026-10-03', -1000, 'EUR', 'hash', '2026-10-03T00:00:00Z', 0)",
			int64(account),
		)
		if err != nil {
			return err
		}
		transactionID, err = result.LastInsertId()
		return err
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	linkedTransaction := model.TransactionID(transactionID)
	document, err := documents.Create(ctx, d, model.NewDocument{
		FileName:      "document.pdf",
		StoredPath:    "documents/document.pdf",
		MIMEType:      "application/pdf",
		TransactionID: &linkedTransaction,
	})
	if err != nil {
		t.Fatalf("documents.Create: %v", err)
	}

	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		var (
			groupName string
			groupSort int64
		)

		if err := conn.QueryRowContext(
			ctx, "SELECT name, sort_order FROM account_groups WHERE id = ?1", int64(group),
		).Scan(&groupName, &groupSort); err != nil {
			return err
		}
		if groupName != "Group" || groupSort != 5 {
			t.Errorf("account group = %q, %d", groupName, groupSort)
		}

		var (
			accountBankConnectionID *int64
			accountInstitution      *string
			accountNameValue        *string
			accountIBAN             *string
			accountNumberValue      *string
			accountCurrency         string
			accountType             string
			balanceMinor            int64
			balanceUpdatedAtText    *string
			accountGroupID          *int64
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT bank_connection_id, institution, name, iban, account_number, currency, account_type, "+
				"balance_minor, balance_updated_at, group_id FROM accounts WHERE id = ?1",
			int64(account),
		).Scan(
			&accountBankConnectionID,
			&accountInstitution,
			&accountNameValue,
			&accountIBAN,
			&accountNumberValue,
			&accountCurrency,
			&accountType,
			&balanceMinor,
			&balanceUpdatedAtText,
			&accountGroupID,
		); err != nil {
			return err
		}
		if accountBankConnectionID == nil || *accountBankConnectionID != int64(bank.ID) {
			t.Errorf("bank connection id = %v", accountBankConnectionID)
		}
		if accountInstitution == nil || *accountInstitution != institution ||
			accountNameValue == nil || *accountNameValue != accountName ||
			accountIBAN == nil || *accountIBAN != iban ||
			accountNumberValue == nil || *accountNumberValue != accountNumber {
			t.Errorf("account text columns = %v, %v, %v, %v",
				accountInstitution, accountNameValue, accountIBAN, accountNumberValue)
		}
		if accountCurrency != "EUR" || accountType != "credit_card" || balanceMinor != 1234 {
			t.Errorf("account encoding = %q, %q, %d", accountCurrency, accountType, balanceMinor)
		}
		wantBalanceAt := balanceAt.Format(time.RFC3339Nano)
		if balanceUpdatedAtText == nil || *balanceUpdatedAtText != wantBalanceAt {
			t.Errorf("balance updated at = %v, want %q", balanceUpdatedAtText, wantBalanceAt)
		}
		if accountGroupID == nil || *accountGroupID != int64(group) {
			t.Errorf("group id = %v", accountGroupID)
		}

		var (
			categoryName     string
			categoryColor    string
			budgetMinor      *int64
			isTaxRelevant    int64
			categoryTransfer int64
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT name, color, budget_limit_minor, is_tax_relevant, is_transfer FROM categories WHERE id = ?1",
			int64(category),
		).Scan(&categoryName, &categoryColor, &budgetMinor, &isTaxRelevant, &categoryTransfer); err != nil {
			return err
		}
		if categoryName != "Category" || categoryColor != "#000000" || budgetMinor == nil || *budgetMinor != 50_000 ||
			isTaxRelevant != 1 || categoryTransfer != 1 {
			t.Errorf("category encoding = %q, %q, %v, %d, %d",
				categoryName, categoryColor, budgetMinor, isTaxRelevant, categoryTransfer)
		}

		var (
			rulePattern    string
			ruleCategory   int64
			rulePriority   int64
			ruleAutoApply  int64
			ruleMatchField string
			ruleDirection  string
			ruleTaxFlag    *int64
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant "+
				"FROM rules WHERE id = ?1",
			int64(rule),
		).Scan(
			&rulePattern, &ruleCategory, &rulePriority, &ruleAutoApply,
			&ruleMatchField, &ruleDirection, &ruleTaxFlag,
		); err != nil {
			return err
		}
		if rulePattern != "pattern" || ruleCategory != int64(category) || rulePriority != 3 ||
			ruleAutoApply != 0 || ruleMatchField != "partner" || ruleDirection != "expense" ||
			ruleTaxFlag == nil || *ruleTaxFlag != 1 {
			t.Errorf("rule encoding = %q, %d, %d, %d, %q, %q, %v",
				rulePattern, ruleCategory, rulePriority, ruleAutoApply, ruleMatchField, ruleDirection, ruleTaxFlag)
		}

		var (
			pendingBookingDate string
			pendingValueDate   *string
			pendingAmount      int64
			pendingCurrency    string
			pendingPartner     *string
			pendingPurpose     *string
			pendingFetchedAt   string
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT booking_date, value_date, amount_minor, currency, partner_name, purpose, fetched_at "+
				"FROM pending_bookings WHERE account_id = ?1",
			int64(account),
		).Scan(
			&pendingBookingDate, &pendingValueDate, &pendingAmount, &pendingCurrency,
			&pendingPartner, &pendingPurpose, &pendingFetchedAt,
		); err != nil {
			return err
		}
		if pendingBookingDate != "2026-10-03" || pendingValueDate == nil || *pendingValueDate != "2026-10-02" ||
			pendingAmount != -1000 || pendingCurrency != "EUR" ||
			pendingPartner == nil || *pendingPartner != "Partner" ||
			pendingPurpose == nil || *pendingPurpose != "Purpose" ||
			pendingFetchedAt != fetchedAt.Format(time.RFC3339Nano) {
			t.Errorf("pending booking encoding = %q, %v, %d, %q, %v, %v, %q",
				pendingBookingDate, pendingValueDate, pendingAmount, pendingCurrency,
				pendingPartner, pendingPurpose, pendingFetchedAt)
		}

		var (
			documentName          string
			documentPath          string
			documentMIMEType      string
			documentUploaded      string
			documentTransactionID *int64
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT file_name, stored_path, mime_type, uploaded_at, transaction_id FROM documents WHERE id = ?1",
			document.ID,
		).Scan(
			&documentName, &documentPath, &documentMIMEType, &documentUploaded, &documentTransactionID,
		); err != nil {
			return err
		}
		if documentName != "document.pdf" || documentPath != "documents/document.pdf" ||
			documentMIMEType != "application/pdf" ||
			documentUploaded != document.UploadedAt.Format(time.RFC3339Nano) ||
			documentTransactionID == nil || *documentTransactionID != transactionID {
			t.Errorf("document encoding = %q, %q, %q, %q, %v",
				documentName, documentPath, documentMIMEType, documentUploaded, documentTransactionID)
		}

		var (
			bankName          string
			bankBLZ           string
			bankBIC           *string
			bankUserID        string
			bankServerURL     *string
			bankTanMethod     *string
			bankSyncFrom      *string
			bankLastSync      *string
			bankCapabilities  *string
			bankIncludeInSync int64
			bankOffline       int64
		)
		if err := conn.QueryRowContext(
			ctx,
			"SELECT bank_name, blz, bic, user_id, server_url, tan_method, sync_from, last_sync, "+
				"capabilities, include_in_sync_all, offline FROM bank_connections WHERE id = ?1",
			int64(bank.ID),
		).Scan(
			&bankName, &bankBLZ, &bankBIC, &bankUserID, &bankServerURL, &bankTanMethod,
			&bankSyncFrom, &bankLastSync, &bankCapabilities, &bankIncludeInSync, &bankOffline,
		); err != nil {
			return err
		}
		if bankName != "Bank" || bankBLZ != "37040044" ||
			bankBIC == nil || *bankBIC != "" || bankUserID != "user" ||
			bankServerURL == nil || *bankServerURL != serverURL ||
			bankTanMethod == nil || *bankTanMethod != "942" ||
			bankSyncFrom == nil || *bankSyncFrom != "2026-01-01" ||
			bankCapabilities == nil || *bankCapabilities != "SaldoReq" ||
			bankIncludeInSync != 0 || bankOffline != 0 {
			t.Errorf("bank connection encoding = %q, %q, %v, %q, %v, %v, %v, %v, %v, %d, %d",
				bankName, bankBLZ, bankBIC, bankUserID, bankServerURL, bankTanMethod,
				bankSyncFrom, bankLastSync, bankCapabilities, bankIncludeInSync, bankOffline)
		}
		if bankLastSync == nil {
			t.Error("last sync is NULL")
		} else {
			parsed, err := time.Parse(time.RFC3339Nano, *bankLastSync)
			if err != nil || !parsed.Equal(*bankRow.LastSync) {
				t.Errorf("last sync = %q, %v, want %v", *bankLastSync, err, *bankRow.LastSync)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read raw columns: %v", err)
	}
}

// TestEveryRepositoryMutationWritesExactlyOneAuditEntryWithoutIBANs is the
// in-scope part of repo/mod.rs's audit test. Transaction mutations belong to
// the transactions repository and are covered by its later spec.
func TestEveryRepositoryMutationWritesExactlyOneAuditEntryWithoutIBANs(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	iban := "DE89370400440532013000"

	countAudit := func() int {
		t.Helper()
		recent, err := audit.Recent(ctx, d, 1000)
		if err != nil {
			t.Fatalf("audit.Recent: %v", err)
		}
		return len(recent)
	}
	expectOne := func(before int) int {
		t.Helper()
		after := countAudit()
		if after != before+1 {
			t.Fatalf("audit entries = %d, want exactly one more than %d", after, before)
		}
		return after
	}

	before := countAudit()

	group, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Group", SortOrder: 0})
	if err != nil {
		t.Fatalf("accountgroups.Create: %v", err)
	}
	before = expectOne(before)

	otherGroup, err := accountgroups.Create(ctx, d, model.NewAccountGroup{Name: "Other", SortOrder: 1})
	if err != nil {
		t.Fatalf("accountgroups.Create: %v", err)
	}
	before = expectOne(before)

	if err := accountgroups.Rename(ctx, d, group, "Renamed group"); err != nil {
		t.Fatalf("accountgroups.Rename: %v", err)
	}
	before = expectOne(before)

	accountName := "Audit account"
	account, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        &accountName,
		IBAN:        &iban,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
		GroupID:     &group,
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	before = expectOne(before)

	renamedAccount := "Audit account 2"
	if err := accounts.Rename(ctx, d, account, &renamedAccount); err != nil {
		t.Fatalf("accounts.Rename: %v", err)
	}
	before = expectOne(before)

	if err := accounts.SetGroup(ctx, d, account, &otherGroup); err != nil {
		t.Fatalf("accounts.SetGroup: %v", err)
	}
	before = expectOne(before)

	if err := accounts.SetBalance(ctx, d, account, money.Money{Minor: 1234, Currency: money.EUR}, time.Now().UTC()); err != nil {
		t.Fatalf("accounts.SetBalance: %v", err)
	}
	before = expectOne(before)

	category, err := categories.Create(ctx, d, model.NewCategory{Name: "Category", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	before = expectOne(before)

	if err := categories.Update(ctx, d, category, model.NewCategory{Name: "Category 2", Color: "#3BC7B0"}); err != nil {
		t.Fatalf("categories.Update: %v", err)
	}
	before = expectOne(before)

	rule, err := rules.Create(ctx, d, model.NewRule{
		Pattern:    "pattern",
		CategoryID: category,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	})
	if err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
	before = expectOne(before)

	taxRelevant := true
	if err := rules.Update(ctx, d, rule, model.NewRule{
		Pattern:     "pattern 2",
		CategoryID:  category,
		Priority:    1,
		AutoApply:   false,
		MatchField:  model.RuleFieldPartner,
		Direction:   model.RuleDirectionExpense,
		TaxRelevant: &taxRelevant,
	}); err != nil {
		t.Fatalf("rules.Update: %v", err)
	}
	before = expectOne(before)

	if err := pendingbookings.ReplaceForAccount(ctx, d, account, nil, time.Now().UTC()); err != nil {
		t.Fatalf("pendingbookings.ReplaceForAccount: %v", err)
	}
	before = expectOne(before)

	document, err := documents.Create(ctx, d, model.NewDocument{
		FileName:   "audit.pdf",
		StoredPath: "documents/audit.pdf",
		MIMEType:   "application/pdf",
	})
	if err != nil {
		t.Fatalf("documents.Create: %v", err)
	}
	before = expectOne(before)

	if _, err := documents.Delete(ctx, d, document.ID); err != nil {
		t.Fatalf("documents.Delete: %v", err)
	}
	before = expectOne(before)

	bank, err := bankconnections.Create(ctx, d, model.NewBankConnection{
		BankName: "Audit Bank",
		BLZ:      "37040044",
		BIC:      pointer("DEUTDEDBXXX"),
		UserID:   "secret-user",
	})
	if err != nil {
		t.Fatalf("bankconnections.Create: %v", err)
	}
	before = expectOne(before)

	if _, err := bankconnections.SetSyncFrom(ctx, d, bank.ID, nil); err != nil {
		t.Fatalf("bankconnections.SetSyncFrom: %v", err)
	}
	before = expectOne(before)

	if _, err := bankconnections.Delete(ctx, d, bank.ID); err != nil {
		t.Fatalf("bankconnections.Delete: %v", err)
	}
	before = expectOne(before)

	if err := rules.Delete(ctx, d, rule); err != nil {
		t.Fatalf("rules.Delete: %v", err)
	}
	before = expectOne(before)

	if err := categories.Delete(ctx, d, category); err != nil {
		t.Fatalf("categories.Delete: %v", err)
	}
	before = expectOne(before)

	if err := accounts.Delete(ctx, d, account); err != nil {
		t.Fatalf("accounts.Delete: %v", err)
	}
	before = expectOne(before)

	if err := accountgroups.Delete(ctx, d, otherGroup); err != nil {
		t.Fatalf("accountgroups.Delete: %v", err)
	}
	before = expectOne(before)

	if err := accountgroups.Delete(ctx, d, group); err != nil {
		t.Fatalf("accountgroups.Delete: %v", err)
	}
	expectOne(before)

	recent, err := audit.Recent(ctx, d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	for _, entry := range recent {
		if strings.Contains(entry.Details, iban) {
			t.Errorf("an audit entry leaked an IBAN: %q", entry.Details)
		}
		if strings.Contains(entry.Details, "secret-user") {
			t.Errorf("an audit entry leaked the bank login: %q", entry.Details)
		}
	}
}
