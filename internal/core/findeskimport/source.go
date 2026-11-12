// Reading a copied FinDesk database.
//
// The selected FinDesk folder is never written to: findesk.db and its side
// files (-wal, -shm, -journal) are copied into a private temporary folder
// first, and only that copy is opened.
package findeskimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// requiredTables are the tables every FinDesk database has had since its first
// release.
var requiredTables = []string{
	"bank_access",
	"accounts",
	"categories",
	"rules",
	"transactions",
	"documents",
	"ocr_texts",
	"settings",
}

// Optional tables were added by later FinDesk versions (account_groups,
// deleted_bookings, pending_bookings, payment_orders): a database without one
// simply has no such rows.

type sourceNumberKind int

const (
	sourceInteger sourceNumberKind = iota
	sourceReal
	sourceText
)

// sourceNumber is a number as FinDesk stores monetary values: INTEGER or REAL
// (SQLite NUMERIC affinity), or occasionally TEXT.
type sourceNumber struct {
	kind    sourceNumberKind
	integer int64
	real    float64
	text    string
}

// sourceNumberFromValue mirrors rusqlite's SourceNumber column_result:
// INTEGER, REAL and TEXT are accepted; NULL and BLOB are not.
func sourceNumberFromValue(value any) (sourceNumber, error) {
	switch typed := value.(type) {
	case int64:
		return sourceNumber{kind: sourceInteger, integer: typed}, nil
	case float64:
		return sourceNumber{kind: sourceReal, real: typed}, nil
	case string:
		return sourceNumber{kind: sourceText, text: lossyUTF8([]byte(typed))}, nil
	case []byte:
		// go-sqlcipher hands BLOB values to Scan as []byte. TEXT columns are
		// delivered as string by this driver.
		return sourceNumber{}, fmt.Errorf("invalid source value: BLOB")
	case nil:
		return sourceNumber{}, fmt.Errorf("invalid source value: NULL")
	default:
		return sourceNumber{}, fmt.Errorf("invalid source value: %T", value)
	}
}

// lossyUTF8 decodes bytes like Rust's String::from_utf8_lossy: every invalid
// sequence becomes one U+FFFD, and a truncated sequence at the end becomes one.
func lossyUTF8(bytes []byte) string {
	if len(bytes) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(bytes))
	remaining := bytes
	for len(remaining) > 0 {
		if utf8.Valid(remaining) {
			builder.Write(remaining)
			break
		}
		// Copy the valid prefix.
		index := 0
		for index < len(remaining) {
			r, size := utf8.DecodeRune(remaining[index:])
			if r == utf8.RuneError && size == 1 {
				break
			}
			index += size
		}
		builder.Write(remaining[:index])
		remaining = remaining[index:]
		if len(remaining) == 0 {
			break
		}
		if truncated := truncatedUTF8Sequence(remaining); truncated > 0 {
			builder.WriteRune(utf8.RuneError)
			remaining = remaining[truncated:]
			continue
		}
		builder.WriteRune(utf8.RuneError)
		remaining = remaining[1:]
	}
	return builder.String()
}

// truncatedUTF8Sequence returns the length of an incomplete but otherwise valid
// UTF-8 sequence at the start of bytes, or 0 when the first byte cannot start
// one.
func truncatedUTF8Sequence(bytes []byte) int {
	if len(bytes) == 0 {
		return 0
	}
	needed := 0
	switch first := bytes[0]; {
	case first >= 0xC2 && first <= 0xDF:
		needed = 2
	case first >= 0xE0 && first <= 0xEF:
		needed = 3
	case first >= 0xF0 && first <= 0xF4:
		needed = 4
	default:
		return 0
	}
	if len(bytes) >= needed {
		return 0
	}
	for _, continuation := range bytes[1:] {
		if continuation < 0x80 || continuation > 0xBF {
			return 0
		}
	}
	return len(bytes)
}

type accountGroupRow struct {
	id        int64
	name      *string
	sortOrder *int64
}

type bankAccessRow struct {
	id               int64
	bankName         *string
	blz              *string
	bic              *string
	userID           *string
	tanMethod        *string
	serverURL        *string
	syncFrom         *string
	lastSync         *string
	includeInSyncAll *int64
	offline          *int64
}

type accountRow struct {
	id            int64
	bankAccessID  *int64
	iban          *string
	accountNumber *string
	balance       *sourceNumber
	currency      *string
	accountType   *string
	name          *string
	lastUpdate    *string
	groupID       *int64
}

type categoryRow struct {
	id            int64
	name          *string
	color         *string
	budgetLimit   *sourceNumber
	isTaxRelevant *int64
	isTransfer    *int64
}

type ruleRow struct {
	id          int64
	pattern     *string
	categoryID  *int64
	priority    *int64
	autoApply   *int64
	matchField  *string
	direction   *string
	taxRelevant *int64
}

type transactionRow struct {
	id           int64
	accountID    *int64
	date         *string
	amount       *sourceNumber
	balanceAfter *sourceNumber
	currency     *string
	partnerName  *string
	purpose      *string
	categoryID   *int64
	hash         *string
	createdAt    *string
	isNew        *int64
	taxOverride  *int64
}

type deletedBookingRow struct {
	id        int64
	accountID *int64
	hash      *string
	deletedAt *string
}

type pendingBookingRow struct {
	id          int64
	accountID   *int64
	date        *string
	valueDate   *string
	amount      *sourceNumber
	currency    *string
	partnerName *string
	purpose     *string
	fetchedAt   *string
}

type documentRow struct {
	id                  int64
	fileName            *string
	filePath            *string
	mimeType            *string
	uploadDate          *string
	linkedTransactionID *int64
}

type ocrTextRow struct {
	id         int64
	documentID *int64
	content    *string
	language   *string
	confidence *sourceNumber
}

type settingRow struct {
	key   *string
	value *string
}

// sourceData is everything read from the FinDesk copy.
type sourceData struct {
	accountGroups   []accountGroupRow
	bankAccess      []bankAccessRow
	accounts        []accountRow
	categories      []categoryRow
	rules           []ruleRow
	transactions    []transactionRow
	deletedBookings []deletedBookingRow
	pendingBookings []pendingBookingRow
	documents       []documentRow
	ocrTexts        []ocrTextRow
	settings        []settingRow
	paymentOrders   int
}

// fileStamp is the size and modification time of one source file.
type fileStamp struct {
	size     int64
	modified time.Time
}

// sourceSnapshot holds the source files that must not change while they are
// copied.
type sourceSnapshot struct {
	database *fileStamp
	wal      *fileStamp
}

// take records the size and modification time of the source files in dataDir.
func takeSnapshot(dataDir string) (sourceSnapshot, error) {
	database, err := stamp(filepath.Join(dataDir, "findesk.db"))
	if err != nil {
		return sourceSnapshot{}, err
	}
	wal, err := stamp(filepath.Join(dataDir, "findesk.db-wal"))
	if err != nil {
		return sourceSnapshot{}, err
	}
	return sourceSnapshot{database: database, wal: wal}, nil
}

func stamp(path string) (*fileStamp, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	return &fileStamp{size: info.Size(), modified: info.ModTime()}, nil
}

// sourceChanged reports whether anything changed between two snapshots. A file
// appearing or disappearing counts as a change.
func sourceChanged(before, after sourceSnapshot) bool {
	return !sameStamp(before.database, after.database) || !sameStamp(before.wal, after.wal)
}

func sameStamp(left, right *fileStamp) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.size == right.size && left.modified.Equal(right.modified)
}

// finDeskSource is a FinDesk database copy plus the temporary folder that holds
// it.
type finDeskSource struct {
	conn    *db.Database
	tempDir string
}

// openSource copies the database and WAL side files into a temporary folder and
// opens the copy read-write.
func openSource(dataDir string, passphrase *secret.Passphrase) (*finDeskSource, error) {
	databasePath := filepath.Join(dataDir, "findesk.db")
	info, err := os.Stat(databasePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, newIOError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotFound
	}

	before, err := takeSnapshot(dataDir)
	if err != nil {
		return nil, newIOError(err)
	}

	temporary, err := os.MkdirTemp("", "finglass-findesk-")
	if err != nil {
		return nil, newIOError(err)
	}
	cleanup := func() {
		_ = os.RemoveAll(temporary)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			cleanup()
		}
	}()

	for _, name := range []string{"findesk.db", "findesk.db-wal", "findesk.db-shm", "findesk.db-journal"} {
		source := filepath.Join(dataDir, name)
		sourceInfo, statErr := os.Stat(source)
		if errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, newIOError(statErr)
		}
		if !sourceInfo.Mode().IsRegular() {
			continue
		}
		if err := copyFile(source, filepath.Join(temporary, name)); err != nil {
			return nil, newIOError(err)
		}
	}

	after, err := takeSnapshot(dataDir)
	if err != nil {
		return nil, newIOError(err)
	}
	if sourceChanged(before, after) {
		return nil, ErrSourceChanged
	}

	// Read-write on the throwaway copy, so SQLite can roll back a hot journal
	// (or replay a WAL) left by a FinDesk that crashed or was still writing; a
	// read-only connection would fail or read half-written pages instead. The
	// selected FinDesk files are never opened.
	copyPath := filepath.Join(temporary, "findesk.db")
	connection, err := db.OpenUnmigrated(copyPath, passphrase)
	if err != nil {
		if errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
			return nil, ErrWrongPassphrase
		}
		return nil, newDatabaseError(err)
	}

	succeeded = true
	return &finDeskSource{conn: connection, tempDir: temporary}, nil
}

// Close closes the copy and removes its temporary folder.
func (s *finDeskSource) Close() error {
	if s == nil {
		return nil
	}
	defer func() {
		_ = os.RemoveAll(s.tempDir)
	}()
	if s.conn != nil {
		connection := s.conn
		s.conn = nil
		return connection.Close()
	}
	return nil
}

// read reads every table that the import needs.
func (s *finDeskSource) read() (*sourceData, error) {
	var data sourceData

	err := s.conn.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		tables, err := requireTables(ctx, conn)
		if err != nil {
			return err
		}

		if _, present := tables["payment_orders"]; present {
			count, err := paymentOrderCount(ctx, conn)
			if err != nil {
				return newDatabaseError(err)
			}
			data.paymentOrders = count
		}

		if _, present := tables["account_groups"]; present {
			rows, err := queryAll(ctx, conn,
				"SELECT id, name, sort_order FROM account_groups ORDER BY id",
				func(values []any) (accountGroupRow, error) {
					return accountGroupRow{
						id:        mustInt64(values[0]),
						name:      mustOptionalString(values[1]),
						sortOrder: mustOptionalInt64(values[2]),
					}, nil
				},
			)
			if err != nil {
				return newDatabaseError(err)
			}
			data.accountGroups = rows
		}

		data.bankAccess, err = queryAll(ctx, conn,
			`SELECT id, bank_name, blz, bic, user_id, tan_method, server_url, sync_from, last_sync,
			        include_in_sync_all, offline
			 FROM bank_access ORDER BY id`,
			func(values []any) (bankAccessRow, error) {
				return bankAccessRow{
					id:               mustInt64(values[0]),
					bankName:         mustOptionalString(values[1]),
					blz:              mustOptionalString(values[2]),
					bic:              mustOptionalString(values[3]),
					userID:           mustOptionalString(values[4]),
					tanMethod:        mustOptionalString(values[5]),
					serverURL:        mustOptionalString(values[6]),
					syncFrom:         mustOptionalString(values[7]),
					lastSync:         mustOptionalString(values[8]),
					includeInSyncAll: mustOptionalInt64(values[9]),
					offline:          mustOptionalInt64(values[10]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.accounts, err = queryAll(ctx, conn,
			`SELECT id, bank_access_id, iban, account_number, balance, currency, account_type, name,
			        last_update, group_id
			 FROM accounts ORDER BY id`,
			func(values []any) (accountRow, error) {
				return accountRow{
					id:            mustInt64(values[0]),
					bankAccessID:  mustOptionalInt64(values[1]),
					iban:          mustOptionalString(values[2]),
					accountNumber: mustOptionalString(values[3]),
					balance:       mustOptionalSourceNumber(values[4]),
					currency:      mustOptionalString(values[5]),
					accountType:   mustOptionalString(values[6]),
					name:          mustOptionalString(values[7]),
					lastUpdate:    mustOptionalString(values[8]),
					groupID:       mustOptionalInt64(values[9]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.categories, err = queryAll(ctx, conn,
			"SELECT id, name, color, budget_limit, is_tax_relevant, is_transfer FROM categories ORDER BY id",
			func(values []any) (categoryRow, error) {
				return categoryRow{
					id:            mustInt64(values[0]),
					name:          mustOptionalString(values[1]),
					color:         mustOptionalString(values[2]),
					budgetLimit:   mustOptionalSourceNumber(values[3]),
					isTaxRelevant: mustOptionalInt64(values[4]),
					isTransfer:    mustOptionalInt64(values[5]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.rules, err = queryAll(ctx, conn,
			`SELECT id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
			 FROM rules ORDER BY id`,
			func(values []any) (ruleRow, error) {
				return ruleRow{
					id:          mustInt64(values[0]),
					pattern:     mustOptionalString(values[1]),
					categoryID:  mustOptionalInt64(values[2]),
					priority:    mustOptionalInt64(values[3]),
					autoApply:   mustOptionalInt64(values[4]),
					matchField:  mustOptionalString(values[5]),
					direction:   mustOptionalString(values[6]),
					taxRelevant: mustOptionalInt64(values[7]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.transactions, err = queryAll(ctx, conn,
			`SELECT id, account_id, date, amount, balance_after, currency, partner_name, purpose, category_id,
			        hash, created_at, is_new, tax_override
			 FROM transactions ORDER BY id`,
			func(values []any) (transactionRow, error) {
				return transactionRow{
					id:           mustInt64(values[0]),
					accountID:    mustOptionalInt64(values[1]),
					date:         mustOptionalString(values[2]),
					amount:       mustOptionalSourceNumber(values[3]),
					balanceAfter: mustOptionalSourceNumber(values[4]),
					currency:     mustOptionalString(values[5]),
					partnerName:  mustOptionalString(values[6]),
					purpose:      mustOptionalString(values[7]),
					categoryID:   mustOptionalInt64(values[8]),
					hash:         mustOptionalString(values[9]),
					createdAt:    mustOptionalString(values[10]),
					isNew:        mustOptionalInt64(values[11]),
					taxOverride:  mustOptionalInt64(values[12]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		if _, present := tables["deleted_bookings"]; present {
			rows, err := queryAll(ctx, conn,
				"SELECT id, account_id, hash, deleted_at FROM deleted_bookings ORDER BY id",
				func(values []any) (deletedBookingRow, error) {
					return deletedBookingRow{
						id:        mustInt64(values[0]),
						accountID: mustOptionalInt64(values[1]),
						hash:      mustOptionalString(values[2]),
						deletedAt: mustOptionalString(values[3]),
					}, nil
				},
			)
			if err != nil {
				return newDatabaseError(err)
			}
			data.deletedBookings = rows
		}

		if _, present := tables["pending_bookings"]; present {
			rows, err := queryAll(ctx, conn,
				`SELECT id, account_id, date, value_date, amount, currency, partner_name, purpose, fetched_at
				 FROM pending_bookings ORDER BY id`,
				func(values []any) (pendingBookingRow, error) {
					return pendingBookingRow{
						id:          mustInt64(values[0]),
						accountID:   mustOptionalInt64(values[1]),
						date:        mustOptionalString(values[2]),
						valueDate:   mustOptionalString(values[3]),
						amount:      mustOptionalSourceNumber(values[4]),
						currency:    mustOptionalString(values[5]),
						partnerName: mustOptionalString(values[6]),
						purpose:     mustOptionalString(values[7]),
						fetchedAt:   mustOptionalString(values[8]),
					}, nil
				},
			)
			if err != nil {
				return newDatabaseError(err)
			}
			data.pendingBookings = rows
		}

		data.documents, err = queryAll(ctx, conn,
			`SELECT id, file_name, file_path, mime_type, upload_date, linked_transaction_id
			 FROM documents ORDER BY id`,
			func(values []any) (documentRow, error) {
				return documentRow{
					id:                  mustInt64(values[0]),
					fileName:            mustOptionalString(values[1]),
					filePath:            mustOptionalString(values[2]),
					mimeType:            mustOptionalString(values[3]),
					uploadDate:          mustOptionalString(values[4]),
					linkedTransactionID: mustOptionalInt64(values[5]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.ocrTexts, err = queryAll(ctx, conn,
			"SELECT id, document_id, content, language, confidence FROM ocr_texts ORDER BY id",
			func(values []any) (ocrTextRow, error) {
				return ocrTextRow{
					id:         mustInt64(values[0]),
					documentID: mustOptionalInt64(values[1]),
					content:    mustOptionalString(values[2]),
					language:   mustOptionalString(values[3]),
					confidence: mustOptionalSourceNumber(values[4]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		data.settings, err = queryAll(ctx, conn, "SELECT key, value FROM settings ORDER BY id",
			func(values []any) (settingRow, error) {
				return settingRow{
					key:   mustOptionalString(values[0]),
					value: mustOptionalString(values[1]),
				}, nil
			},
		)
		if err != nil {
			return newDatabaseError(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func requireTables(ctx context.Context, conn *sql.Conn) (map[string]struct{}, error) {
	rows, err := conn.QueryContext(
		ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'",
	)
	if err != nil {
		return nil, newDatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, newDatabaseError(err)
		}
		names[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, newDatabaseError(err)
	}

	for _, table := range requiredTables {
		if _, present := names[table]; !present {
			return nil, ErrNotFinDesk
		}
	}
	return names, nil
}

func paymentOrderCount(ctx context.Context, conn *sql.Conn) (int, error) {
	var count int64
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM payment_orders").Scan(&count); err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, nil
	}
	return int(count), nil
}

func queryAll[T any](
	ctx context.Context,
	conn *sql.Conn,
	statement string,
	scan func([]any) (T, error),
) (result []T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if scanErr, ok := recovered.(rowScanError); ok {
				result = nil
				err = scanErr.err
				return
			}
			panic(recovered)
		}
	}()

	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result = make([]T, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		item, err := scan(values)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// rowScanError carries a type conversion failure out of a row mapper.
type rowScanError struct {
	err error
}

func (e rowScanError) Error() string {
	return e.err.Error()
}

// The helpers below convert the dynamic values database/sql hands back. They
// mirror the FromSql behaviour of the Rust row types.

func mustInt64(value any) int64 {
	converted, err := int64Value(value)
	if err != nil {
		panic(rowScanError{err: err})
	}
	return converted
}

func mustOptionalInt64(value any) *int64 {
	converted, err := optionalInt64Value(value)
	if err != nil {
		panic(rowScanError{err: err})
	}
	return converted
}

func mustOptionalString(value any) *string {
	converted, err := optionalStringValue(value)
	if err != nil {
		panic(rowScanError{err: err})
	}
	return converted
}

func mustOptionalSourceNumber(value any) *sourceNumber {
	if value == nil {
		return nil
	}
	converted, err := sourceNumberFromValue(value)
	if err != nil {
		panic(rowScanError{err: err})
	}
	return &converted
}

func int64Value(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid integer column: %T", value)
	}
}

func optionalInt64Value(value any) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := int64Value(value)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func optionalStringValue(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		if !utf8.ValidString(typed) {
			return nil, fmt.Errorf("invalid text column: invalid UTF-8")
		}
		converted := typed
		return &converted, nil
	default:
		return nil, fmt.Errorf("invalid text column: %T", value)
	}
}

// copyFile copies source to destination with owner-only permissions.
func copyFile(source, destination string) error {
	input, err := os.Open(source) // #nosec G304 -- source is inside the selected FinDesk folder.
	if err != nil {
		return err
	}
	defer func() {
		_ = input.Close()
	}()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- destination is inside the private temporary copy.
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
