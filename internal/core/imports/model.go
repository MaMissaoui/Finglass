// Package imports reads bank statement files: CSV, CAMT.052/053, MT940, OFX,
// QIF and XLSX. It ports crates/finglass-core/src/import/.
package imports

import (
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

// ImportedBooking is one booking read from a statement file, before it is stored.
type ImportedBooking struct {
	Date          civil.Date
	ValueDate     *civil.Date
	Amount        decimal.Decimal
	Currency      *string
	PartnerName   *string
	PartnerIBAN   *string
	Purpose       *string
	BalanceAfter  *decimal.Decimal
	BankReference *string
}

// StatementFormat is the statement format Finglass can read. Its values are
// Rust's Debug names.
type StatementFormat string

// The statement formats.
const (
	FormatCSV     StatementFormat = "Csv"
	FormatCAMT053 StatementFormat = "Camt053"
	FormatMT940   StatementFormat = "Mt940"
	FormatOFX     StatementFormat = "Ofx"
	FormatQIF     StatementFormat = "Qif"
	FormatXLSX    StatementFormat = "Xlsx"
)

// FormatLabel returns the format's display label.
func (f StatementFormat) FormatLabel() string {
	switch f {
	case FormatCSV:
		return "CSV"
	case FormatCAMT053:
		return "CAMT XML"
	case FormatMT940:
		return "MT940"
	case FormatOFX:
		return "OFX"
	case FormatQIF:
		return "QIF"
	case FormatXLSX:
		return "XLSX"
	default:
		return string(f)
	}
}

// ClosingBalance is the statement's closing balance, already checked against
// its entries.
type ClosingBalance struct {
	Amount          decimal.Decimal
	Date            civil.Date
	Currency        *string
	PendingExcluded uint32
}

// ParsedStatement is the result of reading a file: bookings plus what could be
// detected about it.
type ParsedStatement struct {
	Format   StatementFormat
	Bookings []ImportedBooking
	// AccountIBAN is the IBAN of the statement's own account, if the file names it.
	AccountIBAN *string
	// Accounts holds the distinct accounts the file's statements belong to (IBAN,
	// or the raw account id when it isn't one), in file order. More than one is
	// refused by Read.
	Accounts []string
	// Skipped holds rows that couldn't be read, with the reason (1-based
	// line/entry number).
	Skipped []string
	// PendingSkipped counts pending (not yet booked) entries that were left out.
	PendingSkipped uint32
	// Csv is CSV only: header and detected mapping, so the user can adjust it.
	Csv *CsvLayout
	// FormatDetail is the message type for display, e.g. "CAMT.052".
	FormatDetail *string
	// ClosingBalance is CAMT only: the report's closing balance when it matches
	// the file's entries.
	ClosingBalance *ClosingBalance
}

// FormatLabel returns the format label for the preview and the audit log, e.g.
// "CAMT.052 XML" or "CSV".
func (s ParsedStatement) FormatLabel() string {
	if s.FormatDetail != nil {
		return *s.FormatDetail + " XML"
	}
	return s.Format.FormatLabel()
}

// CsvMapping says which CSV column holds which field (indexes into the headers).
type CsvMapping struct {
	Date      *int
	ValueDate *int
	Amount    *int
	// Debit and Credit are used when a bank splits amounts into debit/credit
	// columns instead of a signed amount.
	Debit   *int
	Credit  *int
	Partner *int
	// Payer is a separate payer column (DKB): used as partner for incoming
	// money, Partner for outgoing.
	Payer       *int
	PartnerIBAN *int
	Purpose     *int
	Currency    *int
	Balance     *int
}

// IsComplete reports whether the mapping names a date and at least one amount
// column.
func (m CsvMapping) IsComplete() bool {
	return m.Date != nil && (m.Amount != nil || m.Debit != nil || m.Credit != nil)
}

// CsvLayout describes how a CSV file was laid out.
type CsvLayout struct {
	Delimiter   rune
	CharsetName string
	HeaderLine  int
	Headers     []string
	Mapping     CsvMapping
}

// fixedError is a fixed ImportError text. It is a distinct comparable type so
// errors.Is works for the fixed kinds.
type fixedError string

// Error implements error.
func (e fixedError) Error() string {
	return string(e)
}

// Fixed import errors. Their texts are copied from Rust's ImportError.
var (
	// ErrFileTooLarge reports a file above MAX_FILE_BYTES.
	ErrFileTooLarge fixedError = "File is too large"
	// ErrXlsxTooLarge reports an XLSX package above the size, entry or cell limits.
	ErrXlsxTooLarge fixedError = "The Excel file is too large or damaged."
	// ErrXlsxUnreadable reports a malformed or unreadable XLSX package.
	ErrXlsxUnreadable fixedError = "The Excel file could not be read."
)

// InvalidError is ImportError::Invalid: the file cannot be read as the expected
// format. Its Error text is exactly the message.
type InvalidError struct {
	Message string
}

// Error implements error.
func (e *InvalidError) Error() string {
	return e.Message
}

// Invalid returns an InvalidError with the message.
func Invalid(message string) error {
	return &InvalidError{Message: message}
}

func intPointer(value int) *int {
	return &value
}

func stringPointer(value string) *string {
	return &value
}

func datePointer(value civil.Date) *civil.Date {
	return &value
}

func decimalPointer(value decimal.Decimal) *decimal.Decimal {
	return &value
}
