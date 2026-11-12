// Package transactions is the transaction repository.
//
// It ports `crates/finglass-core/src/repo/transactions.rs`.
package transactions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/rustfmt"
)

// chunk is the batch chunk size: SQLite's bound-parameter limit is at least
// 999, so 500 ids per statement is safe.
const chunk = 500

const columns = "id, account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name, " +
	"purpose, category_id, import_hash, created_at, is_new, tax_override"

const insertSQL = "INSERT INTO transactions (\n" +
	"                    account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name,\n" +
	"                    purpose, category_id, import_hash, created_at, is_new, tax_override\n" +
	"                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12)"

// nowUTC is overridable in tests.
var nowUTC = func() time.Time { return time.Now().UTC() }

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.Transaction, error) {
	var (
		id                int64
		accountID         int64
		bookingDateText   string
		amountMinor       int64
		currencyText      string
		balanceAfterMinor *int64
		partnerName       *string
		purpose           *string
		categoryID        *int64
		importHash        string
		createdAtText     string
		isNew             int64
		taxOverrideValue  *int64
	)

	if err := row.Scan(
		&id,
		&accountID,
		&bookingDateText,
		&amountMinor,
		&currencyText,
		&balanceAfterMinor,
		&partnerName,
		&purpose,
		&categoryID,
		&importHash,
		&createdAtText,
		&isNew,
		&taxOverrideValue,
	); err != nil {
		return model.Transaction{}, err
	}

	currency, err := repo.ParseCurrency(currencyText)
	if err != nil {
		return model.Transaction{}, err
	}
	bookingDate, err := repo.ParseDate(bookingDateText)
	if err != nil {
		return model.Transaction{}, err
	}
	createdAt, err := repo.ParseTimestamp(createdAtText)
	if err != nil {
		return model.Transaction{}, err
	}

	var balanceAfter *money.Money
	if balanceAfterMinor != nil {
		balance := money.Money{Minor: *balanceAfterMinor, Currency: currency}
		balanceAfter = &balance
	}
	var categoryIDValue *model.CategoryID
	if categoryID != nil {
		converted := model.CategoryID(*categoryID)
		categoryIDValue = &converted
	}

	return model.Transaction{
		ID:           model.TransactionID(id),
		AccountID:    model.AccountID(accountID),
		BookingDate:  bookingDate,
		Amount:       money.Money{Minor: amountMinor, Currency: currency},
		BalanceAfter: balanceAfter,
		PartnerName:  partnerName,
		Purpose:      purpose,
		CategoryID:   categoryIDValue,
		ImportHash:   importHash,
		CreatedAt:    createdAt,
		IsNew:        repo.FromIntBool(isNew),
		TaxOverride:  repo.FromOptionalIntBool(taxOverrideValue),
	}, nil
}

// ImportOutcome is what happened during an import batch.
type ImportOutcome struct {
	// Inserted is the number of rows that were inserted.
	Inserted int
	// SkippedDuplicate is the number of rows skipped because the account
	// already has the same import hash.
	SkippedDuplicate int
	// SkippedDeleted is the number of rows skipped because the account already
	// recorded the booking as deleted.
	SkippedDeleted int
}

// InsertImported imports a batch of transactions into one account.
//
// Entries whose hash already exists for the account are skipped as duplicates.
// Entries whose hash is recorded as deleted are skipped as deleted. Duplicate
// hashes inside the same batch are all inserted, because identical same-day
// bookings are legitimate.
func InsertImported(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	entries []model.NewTransaction,
) (ImportOutcome, error) {
	var outcome ImportOutcome

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		accountCurrency, err := accountCurrency(ctx, tx, accountID)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.Amount.Currency != accountCurrency {
				return repo.ErrCurrencyMismatch
			}
			if entry.BalanceAfter != nil && entry.BalanceAfter.Currency != accountCurrency {
				return repo.ErrCurrencyMismatch
			}
		}

		existingHashes, err := hashesForAccount(ctx, tx, "SELECT import_hash FROM transactions WHERE account_id = ?1", accountID)
		if err != nil {
			return err
		}
		deletedHashes, err := hashesForAccount(ctx, tx, "SELECT import_hash FROM deleted_bookings WHERE account_id = ?1", accountID)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if _, deleted := deletedHashes[entry.ImportHash]; deleted {
				outcome.SkippedDeleted++
				continue
			}
			if _, existing := existingHashes[entry.ImportHash]; existing {
				outcome.SkippedDuplicate++
				continue
			}

			if err := insertEntry(ctx, tx, accountID, entry); err != nil {
				return err
			}
			outcome.Inserted++
		}

		details := fmt.Sprintf(
			"inserted=%d skipped_duplicate=%d skipped_deleted=%d",
			outcome.Inserted, outcome.SkippedDuplicate, outcome.SkippedDeleted,
		)
		return audit.LogConn(ctx, tx, "transactions.import", audit.ActorUser, details)
	})
	if err != nil {
		return ImportOutcome{}, err
	}
	return outcome, nil
}

func accountCurrency(ctx context.Context, tx *sql.Tx, accountID model.AccountID) (money.Currency, error) {
	var currencyText string
	err := tx.QueryRowContext(ctx, "SELECT currency FROM accounts WHERE id = ?1", int64(accountID)).Scan(&currencyText)
	if errors.Is(err, sql.ErrNoRows) {
		return money.Currency{}, repo.ErrNotFound
	}
	if err != nil {
		return money.Currency{}, err
	}
	return repo.ParseCurrency(currencyText)
}

func hashesForAccount(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	accountID model.AccountID,
) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, query, int64(accountID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	hashes := make(map[string]struct{})
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes[hash] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hashes, nil
}

func insertEntry(ctx context.Context, tx *sql.Tx, accountID model.AccountID, entry model.NewTransaction) error {
	bookingDate, err := repo.FormatDate(entry.BookingDate)
	if err != nil {
		return err
	}
	createdAt, err := repo.FormatTimestamp(entry.CreatedAt)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		insertSQL,
		int64(accountID),
		bookingDate,
		entry.Amount.Minor,
		entry.Amount.Currency.String(),
		optionalMinor(entry.BalanceAfter),
		optionalString(entry.PartnerName),
		optionalString(entry.Purpose),
		optionalCategoryID(entry.CategoryID),
		entry.ImportHash,
		createdAt,
		repo.ToIntBool(entry.IsNew),
		repo.ToOptionalIntBool(entry.TaxOverride),
	)
	return err
}

// FindByHash reports whether the account already stores a booking with this
// import hash.
//
// FinDesk checks a hash alone. Finglass' import hashes include the account's
// IBAN (as FinDesk's file and FinTS hashes do too), so the lookup is scoped by
// account.
func FindByHash(ctx context.Context, d *db.Database, accountID model.AccountID, hash string) (bool, error) {
	found := false

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		var value int64
		err := conn.QueryRowContext(
			ctx,
			"SELECT 1 FROM transactions WHERE account_id = ?1 AND import_hash = ?2 LIMIT 1",
			int64(accountID), hash,
		).Scan(&value)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		default:
			found = true
			return nil
		}
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// DeletedHashes returns the dedup hashes remembered as deleted for an account.
//
// The returned map is unordered; only membership is significant.
func DeletedHashes(ctx context.Context, d *db.Database, accountID model.AccountID) (map[string]struct{}, error) {
	var hashes map[string]struct{}

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT import_hash FROM deleted_bookings WHERE account_id = ?1",
			int64(accountID),
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		loaded := make(map[string]struct{})
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				return err
			}
			loaded[hash] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		hashes = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hashes, nil
}

// CountSameBooking returns the number of bookings on an account with this
// booking date and amount (in minor units).
func CountSameBooking(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	date civil.Date,
	amountMinor int64,
) (int64, error) {
	var count int64

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		dateText, err := repo.FormatDate(date)
		if err != nil {
			return err
		}
		return conn.QueryRowContext(
			ctx,
			"SELECT count(*) FROM transactions\n"+
				"             WHERE account_id = ?1 AND booking_date = ?2 AND amount_minor = ?3",
			int64(accountID), dateText, amountMinor,
		).Scan(&count)
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// InsertPlanned inserts already-planned rows in one transaction, without
// duplicate or deletion filtering.
//
// The import service writes the single `IMPORT_STATEMENT` audit entry after
// this, with FinDesk's import detail wording, so this function deliberately
// does not add one of its own.
func InsertPlanned(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	rows []model.NewTransaction,
) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		accountCurrency, err := accountCurrency(ctx, tx, accountID)
		if err != nil {
			return err
		}

		for _, entry := range rows {
			if entry.Amount.Currency != accountCurrency ||
				(entry.BalanceAfter != nil && entry.BalanceAfter.Currency != accountCurrency) {
				return repo.ErrCurrencyMismatch
			}
		}

		for _, entry := range rows {
			if err := insertEntry(ctx, tx, accountID, entry); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ListForAccount lists an account's transactions by booking date, newest first,
// then id, newest first.
func ListForAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	from *civil.Date,
	to *civil.Date,
) ([]model.Transaction, error) {
	var transactions []model.Transaction

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		fromText, err := optionalDateText(from)
		if err != nil {
			return err
		}
		toText, err := optionalDateText(to)
		if err != nil {
			return err
		}

		rows, err := conn.QueryContext(
			ctx,
			"SELECT "+columns+"\n"+
				"             FROM transactions\n"+
				"             WHERE account_id = ?1\n"+
				"               AND (?2 IS NULL OR booking_date >= ?2)\n"+
				"               AND (?3 IS NULL OR booking_date <= ?3)\n"+
				"             ORDER BY booking_date DESC, id DESC",
			int64(accountID), fromText, toText,
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			transaction, err := fromRow(rows)
			if err != nil {
				return err
			}
			transactions = append(transactions, transaction)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

// SetCategory assigns a category to a transaction, or removes it.
func SetCategory(ctx context.Context, d *db.Database, id model.TransactionID, categoryID *model.CategoryID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE transactions SET category_id = ?1 WHERE id = ?2",
			optionalCategoryID(categoryID), int64(id),
		)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return repo.ErrNotFound
		}

		return audit.LogConn(ctx, tx, "transaction.set_category", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

// Delete deletes a transaction and records it so a re-import does not bring it
// back.
func Delete(ctx context.Context, d *db.Database, id model.TransactionID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		var (
			accountID  int64
			importHash string
		)
		err := tx.QueryRowContext(
			ctx,
			"SELECT account_id, import_hash FROM transactions WHERE id = ?1",
			int64(id),
		).Scan(&accountID, &importHash)
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		if err != nil {
			return err
		}

		deletedAt, err := repo.FormatTimestamp(nowUTC())
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO deleted_bookings (account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3)",
			accountID, importHash, deletedAt,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM transactions WHERE id = ?1", int64(id)); err != nil {
			return err
		}

		return audit.LogConn(ctx, tx, "transaction.delete", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

// taxRuleSpec is one auto-apply rule with a tax flag, loaded once per
// [FindItems] call.
type taxRuleSpec struct {
	spec        rules.RuleSpec
	taxRelevant bool
	compiled    *rules.Pattern
}

func taxRules(ctx context.Context, conn *sql.Conn) ([]taxRuleSpec, error) {
	rows, err := conn.QueryContext(
		ctx,
		"SELECT pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant\n"+
			"         FROM rules\n"+
			"         WHERE auto_apply = 1 AND tax_relevant IS NOT NULL\n"+
			"         ORDER BY priority DESC, id ASC",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var loaded []taxRuleSpec
	for rows.Next() {
		var (
			pattern         string
			categoryID      int64
			priority        int64
			autoApply       int64
			matchFieldText  string
			directionText   string
			taxRelevantText int64
		)
		if err := rows.Scan(
			&pattern,
			&categoryID,
			&priority,
			&autoApply,
			&matchFieldText,
			&directionText,
			&taxRelevantText,
		); err != nil {
			return nil, err
		}
		matchField, ok := model.RuleFieldFromStored(matchFieldText)
		if !ok {
			return nil, repo.InvalidError{
				Msg: "unknown rule match field " + rustfmt.Quote(matchFieldText),
			}
		}
		direction, ok := model.RuleDirectionFromStored(directionText)
		if !ok {
			return nil, repo.InvalidError{
				Msg: "unknown rule direction " + rustfmt.Quote(directionText),
			}
		}
		_ = priority
		_ = autoApply

		loaded = append(loaded, taxRuleSpec{
			compiled: rules.Compile(pattern),
			spec: rules.RuleSpec{
				Pattern:    pattern,
				CategoryID: model.CategoryID(categoryID),
				Field:      matchField,
				Direction:  direction,
			},
			taxRelevant: repo.FromIntBool(taxRelevantText),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return loaded, nil
}

// escapeLike escapes SQL `LIKE` wildcards so user text is matched literally.
func escapeLike(text string) string {
	var escaped strings.Builder
	escaped.Grow(len(text))
	for _, character := range text {
		switch character {
		case '\\', '%', '_':
			escaped.WriteByte('\\')
			escaped.WriteRune(character)
		default:
			escaped.WriteRune(character)
		}
	}
	return escaped.String()
}

func placeholders(count int) string {
	var text strings.Builder
	text.WriteString("?")
	for index := 1; index < count; index++ {
		text.WriteString(", ?")
	}
	return text.String()
}

func absoluteMinor(minor int64) int64 {
	const (
		minInt64 = -1 << 63
		maxInt64 = 1<<63 - 1
	)
	if minor == minInt64 {
		return maxInt64
	}
	if minor < 0 {
		return -minor
	}
	return minor
}

// findItemsSQL builds the query and bound values of [FindItems].
func findItemsSQL(filter *model.TransactionFilter, limit *int) (string, []any, error) {
	var sqlText strings.Builder
	sqlText.WriteString(
		"SELECT t.id, t.account_id, t.booking_date, t.amount_minor, t.currency, t.balance_after_minor,\n" +
			"                    t.partner_name, t.purpose, t.category_id, t.import_hash, t.created_at, t.is_new, t.tax_override,\n" +
			"                    a.iban, c.name, c.color, c.is_tax_relevant, c.is_transfer\n" +
			"             FROM transactions t\n" +
			"             JOIN accounts a ON a.id = t.account_id\n" +
			"             LEFT JOIN categories c ON c.id = t.category_id\n" +
			"             WHERE 1 = 1",
	)
	values := make([]any, 0, 16)

	if filter.StartDate != nil {
		start, err := repo.FormatDate(*filter.StartDate)
		if err != nil {
			return "", nil, err
		}
		sqlText.WriteString(" AND t.booking_date >= ?")
		values = append(values, start)
	}
	if filter.EndDate != nil {
		end, err := repo.FormatDate(*filter.EndDate)
		if err != nil {
			return "", nil, err
		}
		sqlText.WriteString(" AND t.booking_date <= ?")
		values = append(values, end)
	}
	if filter.AccountID != nil {
		sqlText.WriteString(" AND t.account_id = ?")
		values = append(values, int64(*filter.AccountID))
	}
	if filter.AccountIDs != nil {
		accountIDs := sortedAccountIDs(*filter.AccountIDs)
		if len(accountIDs) == 0 {
			sqlText.WriteString(" AND 1 = 0")
		} else {
			sqlText.WriteString(" AND t.account_id IN (")
			sqlText.WriteString(placeholders(len(accountIDs)))
			sqlText.WriteString(")")
			for _, accountID := range accountIDs {
				values = append(values, int64(accountID))
			}
		}
	}
	if filter.CategoryID != nil {
		sqlText.WriteString(" AND t.category_id = ?")
		values = append(values, int64(*filter.CategoryID))
	}
	if filter.Currency != nil {
		sqlText.WriteString(" AND t.currency = ?")
		values = append(values, filter.Currency.String())
	}
	if filter.UncategorizedOnly {
		sqlText.WriteString(" AND t.category_id IS NULL")
	}
	switch filter.Direction {
	case model.DirectionIncome:
		sqlText.WriteString(" AND t.amount_minor > 0")
	case model.DirectionExpenses:
		sqlText.WriteString(" AND t.amount_minor < 0")
	case model.DirectionAll:
	}
	if filter.MinAmount != nil {
		sqlText.WriteString(" AND abs(t.amount_minor) >= ?")
		values = append(values, absoluteMinor(filter.MinAmount.Minor))
	}
	if filter.MaxAmount != nil {
		sqlText.WriteString(" AND abs(t.amount_minor) <= ?")
		values = append(values, absoluteMinor(filter.MaxAmount.Minor))
	}
	if filter.Text != nil {
		text := strings.TrimSpace(*filter.Text)
		if text != "" {
			pattern := "%" + escapeLike(text) + "%"
			sqlText.WriteString(" AND (t.partner_name LIKE ? ESCAPE '\\' OR t.purpose LIKE ? ESCAPE '\\')")
			values = append(values, pattern)
			values = append(values, pattern)
		}
	}

	sqlText.WriteString(" ORDER BY t.booking_date DESC, t.id DESC")
	if limit != nil && !filter.TaxRelevantOnly {
		sqlText.WriteString(" LIMIT ?")
		values = append(values, int64(*limit))
	}

	return sqlText.String(), values, nil
}

func documentCounts(
	ctx context.Context,
	conn *sql.Conn,
	ids []model.TransactionID,
) (map[model.TransactionID]int, error) {
	counts := make(map[model.TransactionID]int)
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		query := "SELECT transaction_id FROM documents WHERE transaction_id IN (" + placeholders(len(part)) + ")" // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
		values := make([]any, 0, len(part))
		for _, id := range part {
			values = append(values, int64(id))
		}

		if err := func() error {
			rows, err := conn.QueryContext(ctx, query, values...)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()

			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				counts[model.TransactionID(id)]++
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

type baseRow struct {
	transaction         model.Transaction
	iban                *string
	categoryName        *string
	categoryColor       *string
	categoryTaxRelevant bool
	isTransfer          bool
}

func scanBaseRows(ctx context.Context, conn *sql.Conn, query string, values []any) ([]baseRow, error) {
	rows, err := conn.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	bases := make([]baseRow, 0, 32)
	for rows.Next() {
		var (
			id                int64
			accountID         int64
			bookingDateText   string
			amountMinor       int64
			currencyText      string
			balanceAfterMinor *int64
			partnerName       *string
			purpose           *string
			categoryID        *int64
			importHash        string
			createdAtText     string
			isNew             int64
			taxOverrideValue  *int64
			iban              *string
			categoryName      *string
			categoryColor     *string
			categoryTaxValue  *int64
			isTransferValue   *int64
		)
		if err := rows.Scan(
			&id,
			&accountID,
			&bookingDateText,
			&amountMinor,
			&currencyText,
			&balanceAfterMinor,
			&partnerName,
			&purpose,
			&categoryID,
			&importHash,
			&createdAtText,
			&isNew,
			&taxOverrideValue,
			&iban,
			&categoryName,
			&categoryColor,
			&categoryTaxValue,
			&isTransferValue,
		); err != nil {
			return nil, err
		}

		currency, err := repo.ParseCurrency(currencyText)
		if err != nil {
			return nil, err
		}
		bookingDate, err := repo.ParseDate(bookingDateText)
		if err != nil {
			return nil, err
		}
		createdAt, err := repo.ParseTimestamp(createdAtText)
		if err != nil {
			return nil, err
		}

		var balanceAfter *money.Money
		if balanceAfterMinor != nil {
			balance := money.Money{Minor: *balanceAfterMinor, Currency: currency}
			balanceAfter = &balance
		}
		var categoryIDValue *model.CategoryID
		if categoryID != nil {
			converted := model.CategoryID(*categoryID)
			categoryIDValue = &converted
		}

		categoryTaxRelevant := false
		if categoryTaxValue != nil {
			categoryTaxRelevant = repo.FromIntBool(*categoryTaxValue)
		}
		isTransfer := false
		if isTransferValue != nil {
			isTransfer = repo.FromIntBool(*isTransferValue)
		}

		bases = append(bases, baseRow{
			transaction: model.Transaction{
				ID:           model.TransactionID(id),
				AccountID:    model.AccountID(accountID),
				BookingDate:  bookingDate,
				Amount:       money.Money{Minor: amountMinor, Currency: currency},
				BalanceAfter: balanceAfter,
				PartnerName:  partnerName,
				Purpose:      purpose,
				CategoryID:   categoryIDValue,
				ImportHash:   importHash,
				CreatedAt:    createdAt,
				IsNew:        repo.FromIntBool(isNew),
				TaxOverride:  repo.FromOptionalIntBool(taxOverrideValue),
			},
			iban:                iban,
			categoryName:        categoryName,
			categoryColor:       categoryColor,
			categoryTaxRelevant: categoryTaxRelevant,
			isTransfer:          isTransfer,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return bases, nil
}

// FindItems returns filtered, newest-first snapshots safe to use outside a
// transaction.
//
// The text search matches partner name and purpose literally (LIKE wildcards in
// the user text are escaped), and the tax-relevant filter uses the effective
// value: override, rule, then category. When `taxRelevantOnly` is set, `limit`
// applies after that filter.
func FindItems(
	ctx context.Context,
	d *db.Database,
	filter *model.TransactionFilter,
	limit *int,
) ([]model.TransactionItem, error) {
	var items []model.TransactionItem

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		sqlText, values, err := findItemsSQL(filter, limit)
		if err != nil {
			return err
		}

		loadedRules, err := taxRules(ctx, conn)
		if err != nil {
			return err
		}

		bases, err := scanBaseRows(ctx, conn, sqlText, values)
		if err != nil {
			return err
		}

		ids := make([]model.TransactionID, 0, len(bases))
		for _, base := range bases {
			ids = append(ids, base.transaction.ID)
		}
		counts, err := documentCounts(ctx, conn, ids)
		if err != nil {
			return err
		}

		items = make([]model.TransactionItem, 0, len(bases))
		for _, base := range bases {
			transaction := base.transaction

			type categoryRuleResult struct {
				taxRelevant bool
				pattern     string
			}
			var categoryRule *categoryRuleResult
			if transaction.TaxOverride == nil {
				categoryRules := make([]taxRuleSpec, 0)
				for _, rule := range loadedRules {
					if transaction.CategoryID != nil && rule.spec.CategoryID == *transaction.CategoryID {
						categoryRules = append(categoryRules, rule)
					}
				}
				specs := make([]rules.RuleSpec, 0, len(categoryRules))
				tests := make([]*rules.Pattern, 0, len(categoryRules))
				for _, rule := range categoryRules {
					specs = append(specs, rule.spec)
					tests = append(tests, rule.compiled)
				}
				shopMatch := rules.ShopMatchOf(transaction.Purpose, transaction.PartnerName)
				amount := decimal.FromInt(transaction.Amount.Minor)
				selected := rules.Select(
					specs,
					tests,
					transaction.Purpose,
					transaction.PartnerName,
					&amount,
					shopMatch,
				)
				if selected != nil {
					categoryRule = &categoryRuleResult{
						taxRelevant: categoryRules[*selected].taxRelevant,
						pattern:     categoryRules[*selected].spec.Pattern,
					}
				}
			}

			var (
				isTaxRelevant  bool
				taxSource      model.TaxSource
				taxRulePattern *string
			)
			switch {
			case transaction.TaxOverride != nil:
				isTaxRelevant = *transaction.TaxOverride
				taxSource = model.TaxSourceManual
			case categoryRule != nil:
				isTaxRelevant = categoryRule.taxRelevant
				taxSource = model.TaxSourceRule
				pattern := categoryRule.pattern
				taxRulePattern = &pattern
			case transaction.CategoryID != nil:
				isTaxRelevant = base.categoryTaxRelevant
				taxSource = model.TaxSourceCategory
			default:
				isTaxRelevant = false
				taxSource = model.TaxSourceNone
			}

			items = append(items, model.TransactionItem{
				ID:             transaction.ID,
				AccountID:      transaction.AccountID,
				AccountIBAN:    base.iban,
				Date:           transaction.BookingDate,
				Amount:         transaction.Amount,
				BalanceAfter:   transaction.BalanceAfter,
				Currency:       transaction.Amount.Currency,
				PartnerName:    transaction.PartnerName,
				Purpose:        transaction.Purpose,
				CategoryID:     transaction.CategoryID,
				CategoryName:   base.categoryName,
				CategoryColor:  base.categoryColor,
				IsTaxRelevant:  isTaxRelevant,
				DocumentCount:  counts[transaction.ID],
				IsNew:          transaction.IsNew,
				IsTransfer:     base.isTransfer,
				TaxSource:      taxSource,
				TaxRulePattern: taxRulePattern,
				TaxOverride:    transaction.TaxOverride,
			})
		}

		if filter.TaxRelevantOnly {
			filtered := make([]model.TransactionItem, 0, len(items))
			for _, item := range items {
				if item.IsTaxRelevant {
					filtered = append(filtered, item)
				}
			}
			items = filtered
			if limit != nil && *limit < len(items) {
				items = items[:*limit]
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// CategoryChange assigns a category to a transaction, or removes it when
// CategoryID is nil.
type CategoryChange struct {
	// TransactionID is the booking to change.
	TransactionID model.TransactionID
	// CategoryID is the target category; nil removes the category.
	CategoryID *model.CategoryID
}

// UpdateCategories applies every given category in one batch and returns how
// many rows actually changed.
//
// A booking already sitting on its target category is skipped, not overwritten.
func UpdateCategories(ctx context.Context, d *db.Database, changes []CategoryChange) (int, error) {
	updated := 0

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		for _, change := range changes {
			var current *int64
			err := tx.QueryRowContext(
				ctx,
				"SELECT category_id FROM transactions WHERE id = ?1",
				int64(change.TransactionID),
			).Scan(&current)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}

			target := optionalCategoryID(change.CategoryID)
			if !sameOptionalInt64(current, target) {
				if _, err := tx.ExecContext(
					ctx,
					"UPDATE transactions SET category_id = ?1 WHERE id = ?2",
					target, int64(change.TransactionID),
				); err != nil {
					return err
				}
				updated++
			}
		}

		return audit.LogConn(
			ctx, tx, "transaction.update_categories", audit.ActorUser,
			fmt.Sprintf("count=%d", updated),
		)
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}

// DeleteAll deletes every given booking, remembering each hash so a re-import
// cannot bring it back.
//
// Documents linked to a deleted booking are unlinked, not deleted. Unknown ids
// are ignored.
func DeleteAll(ctx context.Context, d *db.Database, ids []model.TransactionID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	deleted := 0
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		for start := 0; start < len(ids); start += chunk {
			end := start + chunk
			if end > len(ids) {
				end = len(ids)
			}
			part := ids[start:end]

			selectSQL := "SELECT account_id, import_hash FROM transactions WHERE id IN (" + // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
				placeholders(len(part)) + ")"
			values := make([]any, 0, len(part))
			for _, id := range part {
				values = append(values, int64(id))
			}
			type bookingPair struct {
				accountID  int64
				importHash string
			}
			pairs := make(map[bookingPair]struct{})
			if err := func() error {
				rows, err := tx.QueryContext(ctx, selectSQL, values...)
				if err != nil {
					return err
				}
				defer func() { _ = rows.Close() }()

				for rows.Next() {
					var pair bookingPair
					if err := rows.Scan(&pair.accountID, &pair.importHash); err != nil {
						return err
					}
					pairs[pair] = struct{}{}
				}
				return rows.Err()
			}(); err != nil {
				return err
			}

			for pair := range pairs {
				var already int64
				err := tx.QueryRowContext(
					ctx,
					"SELECT 1 FROM deleted_bookings WHERE account_id = ?1 AND import_hash = ?2 LIMIT 1",
					pair.accountID, pair.importHash,
				).Scan(&already)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					deletedAt, err := repo.FormatTimestamp(nowUTC())
					if err != nil {
						return err
					}
					if _, err := tx.ExecContext(
						ctx,
						"INSERT INTO deleted_bookings (account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3)",
						pair.accountID, pair.importHash, deletedAt,
					); err != nil {
						return err
					}
				case err != nil:
					return err
				}
			}

			idPlaceholders := "(" + placeholders(len(part)) + ")"
			if _, err := tx.ExecContext(
				ctx,
				"UPDATE documents SET transaction_id = NULL WHERE transaction_id IN "+idPlaceholders, // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
				values...,
			); err != nil {
				return err
			}
			result, err := tx.ExecContext(
				ctx,
				"DELETE FROM transactions WHERE id IN "+idPlaceholders, // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
				values...,
			)
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			deleted += int(changed)
		}

		return audit.LogConn(
			ctx, tx, "transaction.delete_all", audit.ActorUser,
			fmt.Sprintf("count=%d", deleted),
		)
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// SetTaxOverride sets the manual tax override of every given booking and
// returns how many rows changed.
//
// `value = nil` resets them to automatic. Unknown ids are ignored.
func SetTaxOverride(
	ctx context.Context,
	d *db.Database,
	ids []model.TransactionID,
	value *bool,
) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	changed := 0
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		for start := 0; start < len(ids); start += chunk {
			end := start + chunk
			if end > len(ids) {
				end = len(ids)
			}
			part := ids[start:end]

			query := "UPDATE transactions SET tax_override = ?1 WHERE id IN (" + // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
				placeholders(len(part)) + ")"
			values := make([]any, 0, len(part)+1)
			if value == nil {
				values = append(values, nil)
			} else {
				values = append(values, repo.ToIntBool(*value))
			}
			for _, id := range part {
				values = append(values, int64(id))
			}

			result, err := tx.ExecContext(ctx, query, values...)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			changed += int(affected)
		}

		return audit.LogConn(
			ctx, tx, "transaction.set_tax_override", audit.ActorUser,
			fmt.Sprintf("count=%d value=%s", changed, debugOptionalBool(value)),
		)
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

func debugOptionalBool(value *bool) string {
	if value == nil {
		return "None"
	}
	if *value {
		return "Some(true)"
	}
	return "Some(false)"
}

// ForgetDeleted lets the bookings deleted in an account be imported again,
// returning how many were remembered.
func ForgetDeleted(ctx context.Context, d *db.Database, accountID model.AccountID) (int, error) {
	removed := 0

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx, "DELETE FROM deleted_bookings WHERE account_id = ?1", int64(accountID),
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		removed = int(affected)

		return audit.LogConn(
			ctx, tx, "transaction.forget_deleted", audit.ActorUser,
			fmt.Sprintf("id=%d count=%d", int64(accountID), removed),
		)
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// ForgetDeletedBookings lets an account's remembered deleted bookings be
// imported again, as the Accounts view does.
//
// Ported from FinDesk's `AccountsViewModel.forgetDeletedBookings`: one
// transaction deletes the remembered pairs and names the count in one audit
// entry with the account's last four characters. When nothing is remembered,
// nothing is deleted and no audit entry is written; the caller shows the
// "nothing remembered" message.
func ForgetDeletedBookings(ctx context.Context, d *db.Database, accountID model.AccountID) (int, error) {
	removed := 0

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx, "DELETE FROM deleted_bookings WHERE account_id = ?1", int64(accountID),
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		removed = int(affected)

		if removed > 0 {
			var iban *string
			err := tx.QueryRowContext(
				ctx, "SELECT iban FROM accounts WHERE id = ?1", int64(accountID),
			).Scan(&iban)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			last4 := ""
			if iban != nil {
				last4 = lastFour(*iban)
			}
			details := fmt.Sprintf(
				"Account %s: %d deleted bookings may be imported again",
				last4, removed,
			)
			if err := audit.LogConn(ctx, tx, "FORGET_DELETED_BOOKINGS", audit.ActorUser, details); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func lastFour(text string) string {
	runes := []rune(text)
	if len(runes) <= 4 {
		return text
	}
	return string(runes[len(runes)-4:])
}

// HasBookings reports whether an account has any booking yet.
func HasBookings(ctx context.Context, d *db.Database, accountID model.AccountID) (bool, error) {
	found := false

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		var value int64
		err := conn.QueryRowContext(
			ctx, "SELECT 1 FROM transactions WHERE account_id = ?1 LIMIT 1", int64(accountID),
		).Scan(&value)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		default:
			found = true
			return nil
		}
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// CountNew returns new (unread) bookings per account; accounts with none are
// absent from the map.
//
// The map is unordered. Rust callers only look entries up by account id.
func CountNew(ctx context.Context, d *db.Database) (map[model.AccountID]int64, error) {
	var counts map[model.AccountID]int64

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT account_id, count(*) FROM transactions WHERE is_new = 1 GROUP BY account_id",
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		loaded := make(map[model.AccountID]int64)
		for rows.Next() {
			var (
				accountID int64
				count     int64
			)
			if err := rows.Scan(&accountID, &count); err != nil {
				return err
			}
			loaded[model.AccountID(accountID)] = count
		}
		if err := rows.Err(); err != nil {
			return err
		}
		counts = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}

// MarkRead marks every booking of accountIDs as read (`nil` = all accounts; an
// empty set does nothing).
func MarkRead(ctx context.Context, d *db.Database, accountIDs *model.AccountIDSet) error {
	if accountIDs != nil && len(*accountIDs) == 0 {
		return nil
	}

	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		var changed int64
		if accountIDs != nil {
			ids := sortedAccountIDs(*accountIDs)
			query := "UPDATE transactions SET is_new = 0 WHERE is_new = 1 AND account_id IN (" + // #nosec G202 -- SQL is built from placeholders only, values are bound parameters
				placeholders(len(ids)) + ")"
			values := make([]any, 0, len(ids))
			for _, id := range ids {
				values = append(values, int64(id))
			}
			result, err := tx.ExecContext(ctx, query, values...)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			changed = affected
		} else {
			result, err := tx.ExecContext(ctx, "UPDATE transactions SET is_new = 0 WHERE is_new = 1")
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			changed = affected
		}

		return audit.LogConn(
			ctx, tx, "transaction.mark_read", audit.ActorUser,
			fmt.Sprintf("count=%d", changed),
		)
	})
}

// OldestDate returns the oldest booking date, optionally only in one currency.
// It returns nil when there is no booking.
func OldestDate(ctx context.Context, d *db.Database, currency *money.Currency) (*civil.Date, error) {
	var oldest *civil.Date

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		var oldestText *string
		var err error
		if currency != nil {
			err = conn.QueryRowContext(
				ctx,
				"SELECT min(booking_date) FROM transactions WHERE currency = ?1",
				currency.String(),
			).Scan(&oldestText)
		} else {
			err = conn.QueryRowContext(
				ctx,
				"SELECT min(booking_date) FROM transactions",
			).Scan(&oldestText)
		}
		if err != nil {
			return err
		}
		if oldestText == nil {
			return nil
		}
		parsed, err := repo.ParseDate(*oldestText)
		if err != nil {
			return err
		}
		oldest = &parsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return oldest, nil
}

func sortedAccountIDs(set model.AccountIDSet) []model.AccountID {
	ids := make([]model.AccountID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool {
		return ids[left] < ids[right]
	})
	return ids
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalMinor(value *money.Money) any {
	if value == nil {
		return nil
	}
	return value.Minor
}

func optionalCategoryID(id *model.CategoryID) any {
	if id == nil {
		return nil
	}
	return int64(*id)
}

func optionalDateText(date *civil.Date) (any, error) {
	if date == nil {
		return nil, nil
	}
	return repo.FormatDate(*date)
}

func sameOptionalInt64(current *int64, target any) bool {
	if target == nil {
		return current == nil
	}
	targetValue, ok := target.(int64)
	if !ok || current == nil {
		return false
	}
	return *current == targetValue
}
