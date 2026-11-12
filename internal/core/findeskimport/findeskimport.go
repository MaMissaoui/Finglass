// Package findeskimport imports a FinDesk profile into an empty Finglass
// profile.
//
// The import copies the FinDesk database into a private temporary folder first
// and opens that copy read-write. It never writes to the selected FinDesk
// folder. All target inserts run in one transaction with one audit entry;
// copied document files are removed again when the transaction fails.
//
// FinDesk's payment orders are never imported. The only access to that table is
// a row count for the report. It ports
// crates/finglass-core/src/findesk_import/.
package findeskimport

import (
	"errors"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// ImportCounts says how many rows of one source table were imported or
// skipped.
type ImportCounts struct {
	// Imported is the number of rows inserted into the target.
	Imported int
	// Skipped is the number of rows skipped for any reason.
	Skipped int
	// SkippedOrphan is the number of rows skipped because a referenced
	// account, category or document does not exist.
	SkippedOrphan int
	// SkippedInvalid is the number of rows skipped because a value is missing
	// or fails a Finglass rule.
	SkippedInvalid int
	// SkippedFractionalAmount is the number of rows skipped because a
	// monetary value is not whole at the currency's exponent.
	SkippedFractionalAmount int
}

func (c *ImportCounts) recordImport() {
	c.Imported++
}

func (c *ImportCounts) recordOrphan() {
	c.Skipped++
	c.SkippedOrphan++
}

func (c *ImportCounts) recordInvalid() {
	c.Skipped++
	c.SkippedInvalid++
}

func (c *ImportCounts) recordFractional() {
	c.Skipped++
	c.SkippedFractionalAmount++
}

// DocumentImportCounts says how many documents were copied, and why the others
// were left behind.
type DocumentImportCounts struct {
	// Copied is the number of documents whose file was copied and whose row
	// was inserted.
	Copied int
	// Skipped is the number of documents skipped for any reason.
	Skipped int
	// Missing is the number of documents whose file was not found inside the
	// FinDesk documents folder.
	Missing int
	// Refused is the number of documents whose file is larger than
	// limits.MaxDocumentBytes.
	Refused int
	// SkippedInvalid is the number of documents skipped because their row
	// could not be converted.
	SkippedInvalid int
}

// SettingsImportCounts says how many settings were imported or dropped by the
// allow-list.
type SettingsImportCounts struct {
	// Imported is the number of settings written to the target.
	Imported int
	// Skipped is the number of settings dropped because they are not on the
	// allow-list.
	Skipped int
}

// FinDeskImportReport is what one FinDesk import did, as counts only.
type FinDeskImportReport struct {
	// AccountGroups counts account groups.
	AccountGroups ImportCounts
	// BankConnections counts bank connections (login state and capabilities
	// are not copied).
	BankConnections ImportCounts
	// Accounts counts accounts.
	Accounts ImportCounts
	// Categories counts categories (existing Finglass categories are
	// replaced).
	Categories ImportCounts
	// Rules counts rules.
	Rules ImportCounts
	// Transactions counts transactions.
	Transactions ImportCounts
	// DeletedBookings counts deleted bookings remembered for duplicate
	// detection.
	DeletedBookings ImportCounts
	// PendingBookings counts pending bookings.
	PendingBookings ImportCounts
	// Documents counts documents and their files.
	Documents DocumentImportCounts
	// OCRTexts counts OCR texts.
	OCRTexts ImportCounts
	// Settings counts settings kept by the allow-list.
	Settings SettingsImportCounts
	// PaymentOrdersLeftBehind counts FinDesk payment orders that were
	// deliberately left behind.
	PaymentOrdersLeftBehind int
}

// TotalSkipped returns rows skipped across all imported tables, documents and
// settings.
func (r FinDeskImportReport) TotalSkipped() int {
	return r.AccountGroups.Skipped +
		r.BankConnections.Skipped +
		r.Accounts.Skipped +
		r.Categories.Skipped +
		r.Rules.Skipped +
		r.Transactions.Skipped +
		r.DeletedBookings.Skipped +
		r.PendingBookings.Skipped +
		r.Documents.Skipped +
		r.OCRTexts.Skipped +
		r.Settings.Skipped
}

// Summary returns a one-line, user-visible summary in language.
func (r FinDeskImportReport) Summary(language i18n.Language) string {
	return i18n.Format(
		language,
		i18n.FinDeskImportSummary,
		i18n.Int(int64(r.AccountGroups.Imported)),
		i18n.Int(int64(r.BankConnections.Imported)),
		i18n.Int(int64(r.Accounts.Imported)),
		i18n.Int(int64(r.Categories.Imported)),
		i18n.Int(int64(r.Rules.Imported)),
		i18n.Int(int64(r.Transactions.Imported)),
		i18n.Int(int64(r.DeletedBookings.Imported)),
		i18n.Int(int64(r.PendingBookings.Imported)),
		i18n.Int(int64(r.Documents.Copied)),
		i18n.Int(int64(r.OCRTexts.Imported)),
		i18n.Int(int64(r.Settings.Imported)),
		i18n.Int(int64(r.TotalSkipped())),
		i18n.Int(int64(r.Documents.Missing)),
		i18n.Int(int64(r.Documents.Refused)),
		i18n.Int(int64(r.PaymentOrdersLeftBehind)),
	)
}

// ErrorKind identifies a FinDesk import failure.
type ErrorKind int

const (
	// KindNotFound means no findesk.db exists in the selected folder.
	KindNotFound ErrorKind = iota
	// KindWrongPassphrase means the FinDesk passphrase does not open the
	// database.
	KindWrongPassphrase
	// KindNotFinDesk means the database does not hold FinDesk's tables.
	KindNotFinDesk
	// KindTargetNotEmpty means the target profile already holds data.
	KindTargetNotEmpty
	// KindSourceChanged means the FinDesk files changed while they were
	// copied; FinDesk is probably still running.
	KindSourceChanged
	// KindDatabase means a database operation failed.
	KindDatabase
	// KindIO means a file operation failed.
	KindIO
)

// FinDeskImportError is everything that can go wrong while importing a FinDesk
// profile. Database and file errors are wrapped transparently, so errors.Is and
// errors.As see the cause.
type FinDeskImportError struct {
	// Kind says which failure this is.
	Kind ErrorKind
	// Err is the underlying database or file error, for KindDatabase and
	// KindIO.
	Err error
}

// The sentinel errors for the user-visible failures. Use errors.Is.
var (
	// ErrNotFound means no findesk.db exists in the selected folder.
	ErrNotFound = &FinDeskImportError{Kind: KindNotFound}
	// ErrWrongPassphrase means the FinDesk passphrase does not open the
	// database.
	ErrWrongPassphrase = &FinDeskImportError{Kind: KindWrongPassphrase}
	// ErrNotFinDesk means the database does not hold FinDesk's tables.
	ErrNotFinDesk = &FinDeskImportError{Kind: KindNotFinDesk}
	// ErrTargetNotEmpty means the target profile already holds data.
	ErrTargetNotEmpty = &FinDeskImportError{Kind: KindTargetNotEmpty}
	// ErrSourceChanged means the FinDesk files changed while they were copied.
	ErrSourceChanged = &FinDeskImportError{Kind: KindSourceChanged}
)

// Error returns the English text, or the wrapped technical error's text.
func (e *FinDeskImportError) Error() string {
	return e.Message(i18n.En)
}

// Unwrap returns the underlying database or file error.
func (e *FinDeskImportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is reports whether target is a FinDesk import error of the same kind.
func (e *FinDeskImportError) Is(target error) bool {
	if e == nil {
		return false
	}
	other, ok := target.(*FinDeskImportError)
	if !ok {
		return false
	}
	return e.Kind == other.Kind
}

// Message returns the user text in language. Technical failures keep their
// English description.
func (e *FinDeskImportError) Message(language i18n.Language) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case KindNotFound:
		return i18n.Format(language, i18n.ErrorFinDeskNotFound)
	case KindWrongPassphrase:
		return i18n.Format(language, i18n.ErrorFinDeskWrongPassphrase)
	case KindNotFinDesk:
		return i18n.Format(language, i18n.ErrorFinDeskNotFinDesk)
	case KindTargetNotEmpty:
		return i18n.Format(language, i18n.ErrorFinDeskTargetNotEmpty)
	case KindSourceChanged:
		return i18n.Format(language, i18n.ErrorFinDeskSourceChanged)
	default:
		if e.Err != nil {
			return e.Err.Error()
		}
		return ""
	}
}

func newDatabaseError(err error) *FinDeskImportError {
	return &FinDeskImportError{Kind: KindDatabase, Err: err}
}

func newIOError(err error) *FinDeskImportError {
	return &FinDeskImportError{Kind: KindIO, Err: err}
}

func asImportError(err error) error {
	if err == nil {
		return nil
	}
	var importError *FinDeskImportError
	if errors.As(err, &importError) {
		return importError
	}
	return newDatabaseError(err)
}

// VerifyPassphrase checks that passphrase opens the FinDesk database in
// findeskDataDir.
//
// This is the import's own copy-and-open, so nothing is written to the FinDesk
// folder; the copy is dropped again.
func VerifyPassphrase(dataDir string, passphrase *secret.Passphrase) error {
	source, err := openSource(dataDir, passphrase)
	if err != nil {
		return err
	}
	_ = source.Close()
	return nil
}

// Import imports a FinDesk profile into an empty Finglass profile database.
//
// targetDocumentsDir is `<Finglass profile>/documents`; findeskDataDir is the
// folder holding FinDesk's findesk.db and documents/. offset is used for
// FinDesk's zone-less dates and datetimes.
func Import(
	target *db.Database,
	targetDocumentsDir string,
	findeskDataDir string,
	passphrase *secret.Passphrase,
	offset *time.Location,
) (*FinDeskImportReport, error) {
	if offset == nil {
		offset = time.UTC
	}
	return run(target, targetDocumentsDir, findeskDataDir, passphrase, offset)
}
