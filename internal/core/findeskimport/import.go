// The transactional part of the FinDesk import.
//
// Every insert runs through one target transaction. Rows that FinDesk allowed
// but Finglass rejects are skipped and counted; none of them aborts the import.
package findeskimport

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// settingsAllowlist are copied verbatim. Everything else is dropped;
// ui.dark_theme is translated separately.
var settingsAllowlist = []string{
	"money.primary_currency",
	"tax.suggestionKeywords",
	"transactions.selection",
	"accountGroups.collapsed",
	"accounts.collapsedBanks",
	"transactions.chartCollapsed",
	"ui.sidebarCollapsed",
}

var targetTables = []string{
	"accounts",
	"bank_connections",
	"account_groups",
	"rules",
	"transactions",
	"documents",
}

func run(
	target *db.Database,
	targetDocumentsDir string,
	findeskDataDir string,
	passphrase *secret.Passphrase,
	offset *time.Location,
) (*FinDeskImportReport, error) {
	if err := ensureTargetEmpty(target); err != nil {
		return nil, err
	}

	source, err := openSource(findeskDataDir, passphrase)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = source.Close()
	}()

	data, err := source.read()
	if err != nil {
		return nil, err
	}

	var report *FinDeskImportReport
	err = target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		imported, importErr := importInConnection(
			ctx,
			conn,
			data,
			findeskDataDir,
			targetDocumentsDir,
			offset,
		)
		if importErr != nil {
			return importErr
		}
		report = imported
		return nil
	})
	if err != nil {
		return nil, asImportError(err)
	}
	return report, nil
}

func ensureTargetEmpty(target *db.Database) error {
	var count int64
	err := target.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := targetRowCount(ctx, conn)
		if err != nil {
			return err
		}
		count = rows
		return nil
	})
	if err != nil {
		return asImportError(err)
	}
	return ensureEmptyCount(count)
}

// queryRower is satisfied by *sql.Conn and *sql.Tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// targetRowCount counts the rows in the tables an import replaces or fills.
// Shared by the early check on the target and the check inside the import
// transaction, so both see the same data.
func targetRowCount(ctx context.Context, connection queryRower) (int64, error) {
	parts := make([]string, 0, len(targetTables))
	for _, table := range targetTables {
		parts = append(parts, fmt.Sprintf("(SELECT count(*) FROM %s)", table))
	}
	statement := "SELECT " + strings.Join(parts, " + ")

	var count int64
	if err := connection.QueryRowContext(ctx, statement).Scan(&count); err != nil {
		return 0, newDatabaseError(err)
	}
	return count, nil
}

func ensureEmptyCount(count int64) error {
	if count > 0 {
		return ErrTargetNotEmpty
	}
	return nil
}

func importInConnection(
	ctx context.Context,
	conn *sql.Conn,
	data *sourceData,
	findeskDataDir string,
	targetDocumentsDir string,
	offset *time.Location,
) (*FinDeskImportReport, error) {
	report := &FinDeskImportReport{PaymentOrdersLeftBehind: data.paymentOrders}
	copies := &copiedFiles{}

	transaction, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, newDatabaseError(err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
		copies.cleanup()
	}()

	// The target may have gained data while the FinDesk copy was read; check
	// again before the first write.
	count, err := targetRowCount(ctx, transaction)
	if err != nil {
		return nil, err
	}
	if err := ensureEmptyCount(count); err != nil {
		return nil, err
	}

	// Seeded categories are replaced, not merged.
	if _, err := transaction.ExecContext(ctx, "DELETE FROM categories"); err != nil {
		return nil, newDatabaseError(err)
	}

	primaryCurrency := primaryCurrency(data.settings)
	importedGroups := make(map[int64]struct{})
	importedBanks := make(map[int64]struct{})
	importedAccounts := make(map[int64]struct{})
	importedCategories := make(map[int64]struct{})
	importedTransactions := make(map[int64]struct{})
	copiedDocuments := make(map[int64]struct{})

	for _, row := range data.accountGroups {
		if row.name == nil || row.sortOrder == nil {
			report.AccountGroups.recordInvalid()
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			"INSERT INTO account_groups (id, name, sort_order) VALUES (?1, ?2, ?3)",
			row.id,
			*row.name,
			*row.sortOrder,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		importedGroups[row.id] = struct{}{}
		report.AccountGroups.recordImport()
	}

	for _, row := range data.bankAccess {
		values, err := bankValuesFrom(row, offset)
		if err != nil {
			report.BankConnections.recordInvalid()
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO bank_connections (
			    id, bank_name, blz, bic, user_id, server_url, tan_method, sync_from, last_sync,
			    capabilities, include_in_sync_all, offline
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, NULL, ?10, ?11)`,
			row.id,
			values.bankName,
			values.blz,
			values.bic,
			values.userID,
			values.serverURL,
			values.tanMethod,
			values.syncFrom,
			values.lastSync,
			repo.ToIntBool(values.includeInSyncAll),
			repo.ToIntBool(values.offline),
		); err != nil {
			return nil, newDatabaseError(err)
		}
		importedBanks[row.id] = struct{}{}
		report.BankConnections.recordImport()
	}

	for _, row := range data.accounts {
		values, err := accountValuesFrom(row, offset)
		if err != nil {
			report.Accounts.recordInvalid()
			continue
		}

		bankConnectionID := filterImported(values.bankConnectionID, importedBanks)
		groupID := filterImported(values.groupID, importedGroups)

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO accounts (
			    id, bank_connection_id, institution, name, iban, account_number, currency, account_type,
			    balance_minor, balance_updated_at, group_id
			 ) VALUES (?1, ?2, NULL, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
			row.id,
			bankConnectionID,
			values.name,
			values.iban,
			values.accountNumber,
			values.currency.String(),
			values.accountType.AsStr(),
			values.balanceMinor,
			values.balanceUpdatedAt,
			groupID,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		importedAccounts[row.id] = struct{}{}
		report.Accounts.recordImport()
	}

	for _, row := range data.categories {
		values, err := categoryValuesFrom(row, primaryCurrency)
		if err != nil {
			if errors.Is(err, errFractionalAmount) {
				report.Categories.recordFractional()
			} else {
				report.Categories.recordInvalid()
			}
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO categories (
			    id, name, color, budget_limit_minor, is_tax_relevant, is_transfer
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
			row.id,
			values.name,
			values.color,
			values.budgetLimitMinor,
			repo.ToIntBool(values.isTaxRelevant),
			repo.ToIntBool(values.isTransfer),
		); err != nil {
			return nil, newDatabaseError(err)
		}
		importedCategories[row.id] = struct{}{}
		report.Categories.recordImport()
	}

	for _, row := range data.rules {
		values, err := ruleValuesFrom(row)
		if err != nil {
			report.Rules.recordInvalid()
			continue
		}
		if _, present := importedCategories[values.categoryID]; !present {
			report.Rules.recordOrphan()
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO rules (
			    id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
			row.id,
			values.pattern,
			values.categoryID,
			values.priority,
			repo.ToIntBool(values.autoApply),
			values.matchField.AsStr(),
			values.direction.AsStr(),
			repo.ToOptionalIntBool(values.taxRelevant),
		); err != nil {
			return nil, newDatabaseError(err)
		}
		report.Rules.recordImport()
	}

	for _, row := range data.transactions {
		values, err := transactionValuesFrom(row, offset)
		if err != nil {
			if errors.Is(err, errFractionalAmount) {
				report.Transactions.recordFractional()
			} else {
				report.Transactions.recordInvalid()
			}
			continue
		}
		if _, present := importedAccounts[values.accountID]; !present {
			report.Transactions.recordOrphan()
			continue
		}

		categoryID := filterImported(values.categoryID, importedCategories)

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO transactions (
			    id, account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name,
			    purpose, category_id, import_hash, created_at, is_new, tax_override
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13)`,
			row.id,
			values.accountID,
			values.bookingDate,
			values.amountMinor,
			values.currency.String(),
			values.balanceAfterMinor,
			values.partnerName,
			values.purpose,
			categoryID,
			values.importHash,
			values.createdAt,
			repo.ToIntBool(values.isNew),
			repo.ToOptionalIntBool(values.taxOverride),
		); err != nil {
			return nil, newDatabaseError(err)
		}
		importedTransactions[row.id] = struct{}{}
		report.Transactions.recordImport()
	}

	for _, row := range data.deletedBookings {
		values, err := deletedValuesFrom(row, offset)
		if err != nil {
			report.DeletedBookings.recordInvalid()
			continue
		}
		if _, present := importedAccounts[values.accountID]; !present {
			report.DeletedBookings.recordOrphan()
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			"INSERT INTO deleted_bookings (id, account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3, ?4)",
			row.id,
			values.accountID,
			values.importHash,
			values.deletedAt,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		report.DeletedBookings.recordImport()
	}

	for _, row := range data.pendingBookings {
		values, err := pendingValuesFrom(row, offset)
		if err != nil {
			if errors.Is(err, errFractionalAmount) {
				report.PendingBookings.recordFractional()
			} else {
				report.PendingBookings.recordInvalid()
			}
			continue
		}
		if _, present := importedAccounts[values.accountID]; !present {
			report.PendingBookings.recordOrphan()
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO pending_bookings (
			    id, account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose,
			    fetched_at
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
			row.id,
			values.accountID,
			values.bookingDate,
			values.valueDate,
			values.amountMinor,
			values.currency.String(),
			values.partnerName,
			values.purpose,
			values.fetchedAt,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		report.PendingBookings.recordImport()
	}

	var documentsRoot *string
	if root, err := filepath.EvalSymlinks(filepath.Join(findeskDataDir, "documents")); err == nil {
		documentsRoot = &root
	}

	for _, row := range data.documents {
		values, err := documentValuesFrom(row, offset)
		if err != nil {
			report.Documents.Skipped++
			report.Documents.SkippedInvalid++
			continue
		}

		file, status := resolveDocumentFile(documentsRoot, row.filePath)
		switch status {
		case documentMissing:
			report.Documents.Skipped++
			report.Documents.Missing++
			continue
		case documentRefused:
			report.Documents.Skipped++
			report.Documents.Refused++
			continue
		}

		if err := paths.EnsureDir(targetDocumentsDir); err != nil {
			return nil, newIOError(err)
		}
		identifier, err := newUUID()
		if err != nil {
			return nil, newIOError(err)
		}
		destination := filepath.Join(
			targetDocumentsDir,
			identifier+"_"+service.SanitizeFileName(values.fileName),
		)
		if err := copyFile(file, destination); err != nil {
			return nil, newIOError(err)
		}
		copies.paths = append(copies.paths, destination)
		if err := secureFile(destination); err != nil {
			return nil, newIOError(err)
		}

		transactionID := filterImported(values.linkedTransactionID, importedTransactions)

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO documents (
			    id, file_name, stored_path, mime_type, uploaded_at, transaction_id
			 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
			row.id,
			values.fileName,
			destination,
			values.mimeType,
			values.uploadedAt,
			transactionID,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		copiedDocuments[row.id] = struct{}{}
		report.Documents.Copied++
	}

	for _, row := range data.ocrTexts {
		values, err := ocrValuesFrom(row)
		if err != nil {
			report.OCRTexts.recordInvalid()
			continue
		}

		documentID := int64(0)
		if values.documentID == nil {
			report.OCRTexts.recordOrphan()
			continue
		}
		documentID = *values.documentID
		if _, present := copiedDocuments[documentID]; !present {
			report.OCRTexts.recordOrphan()
			continue
		}

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO ocr_texts (id, document_id, content, language, confidence)
			 VALUES (?1, ?2, ?3, ?4, ?5)`,
			row.id,
			documentID,
			values.content,
			values.language,
			values.confidence,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		report.OCRTexts.recordImport()
	}

	for _, row := range data.settings {
		if row.key == nil || row.value == nil {
			report.Settings.Skipped++
			continue
		}

		key := *row.key
		value := *row.value
		if key == "ui.dark_theme" {
			theme, ok := convertTheme(value)
			if !ok {
				report.Settings.Skipped++
				continue
			}
			key = prefs.ThemeKey
			value = theme
		} else if !containsSetting(settingsAllowlist, key) {
			report.Settings.Skipped++
			continue
		}

		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO settings (key, value) VALUES (?1, ?2)
			 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		); err != nil {
			return nil, newDatabaseError(err)
		}
		report.Settings.Imported++
	}

	details := fmt.Sprintf(
		"account_groups=%d bank_connections=%d accounts=%d categories=%d rules=%d transactions=%d "+
			"deleted_bookings=%d pending_bookings=%d documents=%d ocr_texts=%d settings=%d skipped=%d "+
			"documents_missing=%d documents_refused=%d payment_orders_left=%d",
		report.AccountGroups.Imported,
		report.BankConnections.Imported,
		report.Accounts.Imported,
		report.Categories.Imported,
		report.Rules.Imported,
		report.Transactions.Imported,
		report.DeletedBookings.Imported,
		report.PendingBookings.Imported,
		report.Documents.Copied,
		report.OCRTexts.Imported,
		report.Settings.Imported,
		report.TotalSkipped(),
		report.Documents.Missing,
		report.Documents.Refused,
		report.PaymentOrdersLeftBehind,
	)
	if err := audit.LogConn(ctx, transaction, "FINDESK_IMPORTED", audit.ActorImport, details); err != nil {
		return nil, newDatabaseError(err)
	}

	if err := transaction.Commit(); err != nil {
		return nil, newDatabaseError(err)
	}
	committed = true
	copies.keep()

	return report, nil
}

func filterImported(id *int64, imported map[int64]struct{}) *int64 {
	if id == nil {
		return nil
	}
	if _, present := imported[*id]; !present {
		return nil
	}
	converted := *id
	return &converted
}

func containsSetting(allowlist []string, key string) bool {
	for _, allowed := range allowlist {
		if allowed == key {
			return true
		}
	}
	return false
}

func primaryCurrency(settings []settingRow) money.Currency {
	for _, row := range settings {
		if row.key == nil || *row.key != "money.primary_currency" {
			continue
		}
		if row.value == nil {
			continue
		}
		currency, err := money.NewCurrency(strings.TrimSpace(*row.value))
		if err == nil {
			return currency
		}
	}
	return money.EUR
}

type bankValues struct {
	bankName         string
	blz              string
	bic              *string
	userID           string
	tanMethod        *string
	serverURL        *string
	syncFrom         *string
	lastSync         *string
	includeInSyncAll bool
	offline          bool
}

func bankValuesFrom(row bankAccessRow, offset *time.Location) (bankValues, error) {
	if row.bankName == nil || row.blz == nil || row.userID == nil {
		return bankValues{}, errInvalid
	}
	syncFrom, err := optionalDateText(row.syncFrom)
	if err != nil {
		return bankValues{}, err
	}
	lastSync, err := optionalTimestamp(row.lastSync, offset)
	if err != nil {
		return bankValues{}, err
	}
	includeInSyncAll, err := requiredBool(row.includeInSyncAll)
	if err != nil {
		return bankValues{}, err
	}
	offline, err := requiredBool(row.offline)
	if err != nil {
		return bankValues{}, err
	}
	return bankValues{
		bankName:         *row.bankName,
		blz:              *row.blz,
		bic:              row.bic,
		userID:           *row.userID,
		tanMethod:        row.tanMethod,
		serverURL:        row.serverURL,
		syncFrom:         syncFrom,
		lastSync:         lastSync,
		includeInSyncAll: includeInSyncAll,
		offline:          offline,
	}, nil
}

type accountValues struct {
	bankConnectionID *int64
	name             *string
	iban             *string
	accountNumber    *string
	currency         money.Currency
	accountType      model.AccountType
	balanceMinor     int64
	balanceUpdatedAt *string
	groupID          *int64
}

func accountValuesFrom(row accountRow, offset *time.Location) (accountValues, error) {
	currency, err := convertCurrency(row.currency)
	if err != nil {
		return accountValues{}, err
	}
	accountType, err := convertAccountType(row.accountType)
	if err != nil {
		return accountValues{}, err
	}
	balanceMinor, err := requiredMinor(row.balance, currency)
	if err != nil {
		return accountValues{}, err
	}
	balanceUpdatedAt, err := optionalTimestamp(row.lastUpdate, offset)
	if err != nil {
		return accountValues{}, err
	}
	return accountValues{
		bankConnectionID: row.bankAccessID,
		name:             row.name,
		iban:             row.iban,
		accountNumber:    row.accountNumber,
		currency:         currency,
		accountType:      accountType,
		balanceMinor:     balanceMinor,
		balanceUpdatedAt: balanceUpdatedAt,
		groupID:          row.groupID,
	}, nil
}

type categoryValues struct {
	name             string
	color            string
	budgetLimitMinor *int64
	isTaxRelevant    bool
	isTransfer       bool
}

func categoryValuesFrom(row categoryRow, currency money.Currency) (categoryValues, error) {
	if row.name == nil || row.color == nil {
		return categoryValues{}, errInvalid
	}
	// A budget that isn't whole at the primary currency's exponent is dropped,
	// never the category itself (that would also drop its rules and
	// uncategorise its bookings).
	budgetLimitMinor, _ := optionalMinor(row.budgetLimit, currency)
	isTaxRelevant, err := requiredBool(row.isTaxRelevant)
	if err != nil {
		return categoryValues{}, err
	}
	isTransfer, err := requiredBool(row.isTransfer)
	if err != nil {
		return categoryValues{}, err
	}
	return categoryValues{
		name:             *row.name,
		color:            *row.color,
		budgetLimitMinor: budgetLimitMinor,
		isTaxRelevant:    isTaxRelevant,
		isTransfer:       isTransfer,
	}, nil
}

type ruleValues struct {
	pattern     string
	categoryID  int64
	priority    int64
	autoApply   bool
	matchField  model.RuleField
	direction   model.RuleDirection
	taxRelevant *bool
}

func ruleValuesFrom(row ruleRow) (ruleValues, error) {
	if row.pattern == nil || row.categoryID == nil || row.priority == nil {
		return ruleValues{}, errInvalid
	}
	autoApply, err := requiredBool(row.autoApply)
	if err != nil {
		return ruleValues{}, err
	}
	matchField, err := convertRuleField(row.matchField)
	if err != nil {
		return ruleValues{}, err
	}
	direction, err := convertRuleDirection(row.direction)
	if err != nil {
		return ruleValues{}, err
	}
	taxRelevant, err := optionalBool(row.taxRelevant)
	if err != nil {
		return ruleValues{}, err
	}
	return ruleValues{
		pattern:     *row.pattern,
		categoryID:  *row.categoryID,
		priority:    *row.priority,
		autoApply:   autoApply,
		matchField:  matchField,
		direction:   direction,
		taxRelevant: taxRelevant,
	}, nil
}

type transactionValues struct {
	accountID         int64
	bookingDate       string
	amountMinor       int64
	currency          money.Currency
	balanceAfterMinor *int64
	partnerName       *string
	purpose           *string
	categoryID        *int64
	importHash        string
	createdAt         string
	isNew             bool
	taxOverride       *bool
}

func transactionValuesFrom(row transactionRow, offset *time.Location) (transactionValues, error) {
	currency, err := convertCurrency(row.currency)
	if err != nil {
		return transactionValues{}, err
	}
	date, err := requiredDate(row.date)
	if err != nil {
		return transactionValues{}, err
	}
	bookingDate, err := dateText(date)
	if err != nil {
		return transactionValues{}, err
	}
	if row.accountID == nil || row.hash == nil {
		return transactionValues{}, errInvalid
	}
	amountMinor, err := requiredMinor(row.amount, currency)
	if err != nil {
		return transactionValues{}, err
	}
	balanceAfterMinor, err := optionalMinor(row.balanceAfter, currency)
	if err != nil {
		return transactionValues{}, err
	}
	createdAt, err := requiredTimestamp(row.createdAt, offset)
	if err != nil {
		return transactionValues{}, err
	}
	isNew, err := requiredBool(row.isNew)
	if err != nil {
		return transactionValues{}, err
	}
	taxOverride, err := optionalBool(row.taxOverride)
	if err != nil {
		return transactionValues{}, err
	}
	return transactionValues{
		accountID:         *row.accountID,
		bookingDate:       bookingDate,
		amountMinor:       amountMinor,
		currency:          currency,
		balanceAfterMinor: balanceAfterMinor,
		partnerName:       row.partnerName,
		purpose:           row.purpose,
		categoryID:        row.categoryID,
		importHash:        *row.hash,
		createdAt:         createdAt,
		isNew:             isNew,
		taxOverride:       taxOverride,
	}, nil
}

type deletedValues struct {
	accountID  int64
	importHash string
	deletedAt  string
}

func deletedValuesFrom(row deletedBookingRow, offset *time.Location) (deletedValues, error) {
	if row.accountID == nil || row.hash == nil {
		return deletedValues{}, errInvalid
	}
	deletedAt, err := requiredTimestamp(row.deletedAt, offset)
	if err != nil {
		return deletedValues{}, err
	}
	return deletedValues{
		accountID:  *row.accountID,
		importHash: *row.hash,
		deletedAt:  deletedAt,
	}, nil
}

type pendingValues struct {
	accountID   int64
	bookingDate string
	valueDate   *string
	amountMinor int64
	currency    money.Currency
	partnerName *string
	purpose     *string
	fetchedAt   string
}

func pendingValuesFrom(row pendingBookingRow, offset *time.Location) (pendingValues, error) {
	currency, err := convertCurrency(row.currency)
	if err != nil {
		return pendingValues{}, err
	}
	date, err := requiredDate(row.date)
	if err != nil {
		return pendingValues{}, err
	}
	bookingDate, err := dateText(date)
	if err != nil {
		return pendingValues{}, err
	}
	if row.accountID == nil {
		return pendingValues{}, errInvalid
	}
	valueDate, err := optionalDateText(row.valueDate)
	if err != nil {
		return pendingValues{}, err
	}
	amountMinor, err := requiredMinor(row.amount, currency)
	if err != nil {
		return pendingValues{}, err
	}
	fetchedAt, err := requiredTimestamp(row.fetchedAt, offset)
	if err != nil {
		return pendingValues{}, err
	}
	return pendingValues{
		accountID:   *row.accountID,
		bookingDate: bookingDate,
		valueDate:   valueDate,
		amountMinor: amountMinor,
		currency:    currency,
		partnerName: row.partnerName,
		purpose:     row.purpose,
		fetchedAt:   fetchedAt,
	}, nil
}

type documentValues struct {
	fileName            string
	mimeType            string
	uploadedAt          string
	linkedTransactionID *int64
}

func documentValuesFrom(row documentRow, offset *time.Location) (documentValues, error) {
	if row.fileName == nil || row.mimeType == nil {
		return documentValues{}, errInvalid
	}
	uploadedAt, err := requiredTimestamp(row.uploadDate, offset)
	if err != nil {
		return documentValues{}, err
	}
	return documentValues{
		fileName:            *row.fileName,
		mimeType:            *row.mimeType,
		uploadedAt:          uploadedAt,
		linkedTransactionID: row.linkedTransactionID,
	}, nil
}

type ocrValues struct {
	documentID *int64
	content    string
	language   string
	confidence *float64
}

func ocrValuesFrom(row ocrTextRow) (ocrValues, error) {
	if row.content == nil || row.language == nil {
		return ocrValues{}, errInvalid
	}
	confidence, err := convertConfidence(row.confidence)
	if err != nil {
		return ocrValues{}, err
	}
	return ocrValues{
		documentID: row.documentID,
		content:    *row.content,
		language:   *row.language,
		confidence: confidence,
	}, nil
}

func optionalDateText(value *string) (*string, error) {
	date, err := optionalDate(value)
	if err != nil {
		return nil, err
	}
	if date == nil {
		return nil, nil
	}
	text, err := dateText(*date)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

type documentFileStatus int

const (
	documentFound documentFileStatus = iota
	documentMissing
	documentRefused
)

// resolveDocumentFile resolves a stored FinDesk path by its base name inside
// the FinDesk documents folder only.
func resolveDocumentFile(documentsRoot *string, storedPath *string) (string, documentFileStatus) {
	if documentsRoot == nil || storedPath == nil {
		return "", documentMissing
	}
	base, ok := baseName(*storedPath)
	if !ok {
		return "", documentMissing
	}

	candidate, err := filepath.EvalSymlinks(filepath.Join(*documentsRoot, base))
	if err != nil {
		return "", documentMissing
	}
	if !pathWithin(*documentsRoot, candidate) {
		return "", documentMissing
	}

	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", documentMissing
	}
	if info.Size() > int64(limits.MaxDocumentBytes) {
		return "", documentRefused
	}
	return candidate, documentFound
}

func baseName(path string) (string, bool) {
	index := strings.LastIndexAny(path, `/\`)
	name := path
	if index >= 0 {
		name = path[index+1:]
	}
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	return name, true
}

// pathWithin reports whether candidate stays inside root, compared by path
// components.
func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type copiedFiles struct {
	paths []string
	kept  bool
}

func (c *copiedFiles) keep() {
	c.kept = true
	c.paths = nil
}

func (c *copiedFiles) cleanup() {
	if c.kept {
		return
	}
	for _, path := range c.paths {
		_ = os.Remove(path)
	}
}

// newUUID returns a random RFC 4122 version 4 UUID.
func newUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0F) | 0x40
	bytes[8] = (bytes[8] & 0x3F) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}
