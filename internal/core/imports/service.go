// Importing a parsed statement into the database, ported from FinDesk's
// StatementImportService and crates/finglass-core/src/import/service.rs.
package imports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// TypedBalanceRecentDays is how recent a file must be to set a non-zero
// balance typed by hand: a monthly card statement is usually imported within
// about six weeks.
const TypedBalanceRecentDays = 45

// ErrAccountNotFound reports that the account does not exist.
//
//nolint:staticcheck // "Account not found" is Rust's exact user-visible text.
var ErrAccountNotFound = errors.New("Account not found")

// SeveralAccountsError reports a file that names several accounts and must be
// split. Its message is Rust's exact text.
type SeveralAccountsError struct {
	// Message is the user-facing error text.
	Message string
}

// Error returns the message.
func (e *SeveralAccountsError) Error() string {
	return e.Message
}

// ImportServiceError is everything that can go wrong while importing a parsed
// statement.
//
// It wraps the repository, money or database error transparently, like Rust's
// ImportServiceError, so errors.Is and errors.As see the cause.
type ImportServiceError struct {
	// Err is the underlying repository, money or database error, or
	// ErrAccountNotFound or a *SeveralAccountsError.
	Err error
}

// Error returns the underlying error text.
func (e *ImportServiceError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ImportServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapImportServiceError(err error) error {
	if err == nil {
		return nil
	}
	return &ImportServiceError{Err: err}
}

// ImportReport is what an import did.
type ImportReport struct {
	// Imported is the number of rows inserted.
	Imported int
	// Duplicates is the number of bookings already known by hash.
	Duplicates int
	// Categorized is the number of inserted rows that a rule categorized.
	Categorized int
	// BalanceUpdated is the balance now stored, when the file was allowed to
	// set it.
	BalanceUpdated *decimal.Decimal
	// ProbableDuplicates is the number of bookings with the same account, date
	// and amount as an existing booking from another source (sync, manual
	// entry).
	ProbableDuplicates int
	// WrongCurrency is the number of bookings in another currency than the
	// account; never mixed in, since amounts can't be converted.
	WrongCurrency int
	// Currency is the account's ISO currency code.
	Currency money.Currency
	// PreviouslyDeleted is the number of bookings the user deleted before;
	// remembered, so they are skipped instead of coming back.
	PreviouslyDeleted int
	// InvalidAmount is the number of bookings whose amount has more decimal
	// places than the account's currency allows.
	InvalidAmount int
}

func plural(count int, singular string, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}

func formatReportMoney(amount decimal.Decimal, currency money.Currency) string {
	minor := int64(0)
	if converted, err := AmountToMoney(amount, currency); err == nil {
		minor = converted.Minor
	}
	return format.Money(money.Money{Minor: minor, Currency: currency}, i18n.En, false)
}

// Summary returns the summary for a single file.
func (r ImportReport) Summary() string {
	return r.SummaryFiles(1)
}

// SummaryFiles returns the summary for several files. One file reads exactly
// as today; several files name the file count first.
func (r ImportReport) SummaryFiles(files int) string {
	var text string
	if files <= 1 {
		text = fmt.Sprintf(
			"Imported %d %s",
			r.Imported,
			plural(r.Imported, "transaction", "transactions"),
		)
	} else {
		text = fmt.Sprintf("Imported %d transactions from %d files", r.Imported, files)
	}

	if r.Categorized > 0 {
		text += fmt.Sprintf(", %d auto-categorized", r.Categorized)
	}
	if r.Duplicates > 0 {
		text += fmt.Sprintf(", %d already known", r.Duplicates)
	}
	if r.ProbableDuplicates > 0 {
		text += fmt.Sprintf(
			", %d probable %s of existing bookings skipped",
			r.ProbableDuplicates,
			plural(r.ProbableDuplicates, "duplicate", "duplicates"),
		)
	}
	if r.PreviouslyDeleted > 0 {
		text += fmt.Sprintf(", %d deleted earlier, skipped", r.PreviouslyDeleted)
	}
	if r.WrongCurrency > 0 {
		text += fmt.Sprintf(
			", %d in another currency than the account skipped",
			r.WrongCurrency,
		)
	}
	if r.InvalidAmount > 0 {
		text += fmt.Sprintf(", %d with too many decimal places skipped", r.InvalidAmount)
	}
	if r.BalanceUpdated != nil {
		text += fmt.Sprintf(". Balance set to %s", formatReportMoney(*r.BalanceUpdated, r.Currency))
	}

	return text
}

// MatchAccount returns the account whose IBAN the statement names, if it
// exists locally.
//
// The statement's IBAN is normalised before the comparison, as FinDesk does.
func MatchAccount(ctx context.Context, d *db.Database, statement ParsedStatement) *model.AccountID {
	if statement.AccountIBAN == nil {
		return nil
	}
	normalised := iban.Normalize(*statement.AccountIBAN)
	listed, err := accounts.List(ctx, d)
	if err != nil {
		return nil
	}
	for _, account := range listed {
		if account.IBAN != nil && *account.IBAN == normalised {
			id := account.ID
			return &id
		}
	}
	return nil
}

func requireImportSingleAccount(statement ParsedStatement) error {
	if len(statement.Accounts) > 1 {
		accounts := make([]string, 0, len(statement.Accounts))
		for _, account := range statement.Accounts {
			accounts = append(accounts, iban.Mask(account))
		}
		return &SeveralAccountsError{Message: fmt.Sprintf(
			"This file holds bookings for %d accounts (%s). Export one file per account and import them one at a time.",
			len(statement.Accounts),
			strings.Join(accounts, ", "),
		)}
	}
	return nil
}

// Import stores the bookings. If updateBalance and the file reports running
// balances, the account balance is set to the balance after the newest
// booking. A CAMT file without running balances but with a reconciled closing
// balance sets the balance from it instead.
func Import(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	statement ParsedStatement,
	updateBalance bool,
	today civil.Date,
) (ImportReport, error) {
	if err := requireImportSingleAccount(statement); err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}

	account, found, err := accounts.Get(ctx, d, accountID)
	if err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}
	if !found {
		return ImportReport{}, wrapImportServiceError(ErrAccountNotFound)
	}

	// Decided before inserting anything, so the balance check and dedup see the
	// account as it was.
	bookings := make([]ImportedBooking, 0, len(statement.Bookings))
	wrongCurrency := make([]ImportedBooking, 0)
	for _, booking := range statement.Bookings {
		if booking.Currency == nil || *booking.Currency == account.Currency.String() {
			bookings = append(bookings, booking)
		} else {
			wrongCurrency = append(wrongCurrency, booking)
		}
	}
	acceptedStatement := statement
	acceptedStatement.Bookings = bookings
	balanceAllowed, err := BalanceUpdateAllowed(ctx, d, accountID, acceptedStatement, today)
	if err != nil {
		return ImportReport{}, err
	}

	// Many bank CSV exports (e.g. DKB) list the newest booking first, but the
	// list orders same-day bookings by insertion order (date DESC, id DESC), so
	// same-day balances only chain if the rows are inserted oldest first.
	// Reversing a newest-first file and then stable-sorting by date makes the
	// insert order the booking order within a day too.
	chronological := bookings
	if IsNewestFirst(chronological) {
		reverseBookings(chronological)
	}
	sort.SliceStable(chronological, func(left, right int) bool {
		return chronological[left].Date.Before(chronological[right].Date)
	})

	// Amounts with more decimal places than the currency allows are skipped and
	// counted, not rounded.
	invalidAmount := 0
	valid := make([]ImportedBooking, 0, len(chronological))
	for _, booking := range chronological {
		if _, err := AmountToMoney(booking.Amount, account.Currency); err == nil {
			valid = append(valid, booking)
		} else {
			invalidAmount++
		}
	}
	chronological = valid

	var latestBalance *decimal.Decimal
	for index := len(chronological) - 1; index >= 0; index-- {
		if chronological[index].BalanceAfter != nil {
			latestBalance = chronological[index].BalanceAfter
			break
		}
	}

	ibanText := ""
	if account.IBAN != nil {
		ibanText = *account.IBAN
	}
	hashes := Hashes(ibanText, chronological)
	candidates := make([]Candidate[ImportedBooking], 0, len(chronological))
	for index, booking := range chronological {
		candidates = append(candidates, Candidate[ImportedBooking]{
			Date:   booking.Date,
			Amount: booking.Amount,
			Hash:   hashes[index],
			Item:   booking,
		})
	}
	plan, err := PlanDuplicates(ctx, d, accountID, account.Currency, candidates)
	if err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}

	exactDuplicates := plan.ExactDuplicates
	probableDuplicates := plan.ProbableDuplicates
	previouslyDeleted := plan.PreviouslyDeleted

	matcher, err := rules.Matcher(ctx, d)
	if err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}
	rows := make([]model.NewTransaction, 0, len(plan.ToImport))
	for _, candidate := range plan.ToImport {
		booking := candidate.Item
		amount, err := AmountToMoney(booking.Amount, account.Currency)
		if err != nil {
			return ImportReport{}, wrapImportServiceError(err)
		}
		var balanceAfter *money.Money
		if booking.BalanceAfter != nil {
			converted, err := AmountToMoney(*booking.BalanceAfter, account.Currency)
			if err != nil {
				return ImportReport{}, wrapImportServiceError(err)
			}
			balanceAfter = &converted
		}
		categoryID := matcher.CategoryFor(
			booking.Purpose,
			booking.PartnerName,
			&booking.Amount,
		)
		rows = append(rows, model.NewTransaction{
			BookingDate:  booking.Date,
			Amount:       amount,
			BalanceAfter: balanceAfter,
			PartnerName:  truncateChars(booking.PartnerName, 255),
			Purpose:      booking.Purpose,
			CategoryID:   categoryID,
			ImportHash:   candidate.Hash,
			CreatedAt:    time.Now().UTC(),
			IsNew:        false,
			TaxOverride:  nil,
		})
	}
	categorized := 0
	for _, row := range rows {
		if row.CategoryID != nil {
			categorized++
		}
	}
	imported, err := transactions.InsertPlanned(ctx, d, accountID, rows)
	if err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}

	// The list is chronological now, so the newest booking with a balance is
	// simply the last one.
	var newBalance *decimal.Decimal
	switch {
	case !updateBalance || !balanceAllowed:
		newBalance = nil
	case latestBalance != nil:
		converted, err := AmountToMoney(*latestBalance, account.Currency)
		if err != nil {
			return ImportReport{}, wrapImportServiceError(err)
		}
		if err := accounts.SetBalanceForImport(ctx, d, accountID, converted, time.Now().UTC()); err != nil {
			return ImportReport{}, wrapImportServiceError(err)
		}
		value := *latestBalance
		newBalance = &value
	case statement.ClosingBalance != nil &&
		(statement.ClosingBalance.Currency == nil ||
			*statement.ClosingBalance.Currency == account.Currency.String()):
		converted, err := AmountToMoney(statement.ClosingBalance.Amount, account.Currency)
		if err != nil {
			return ImportReport{}, wrapImportServiceError(err)
		}
		if err := accounts.SetBalanceForImport(ctx, d, accountID, converted, time.Now().UTC()); err != nil {
			return ImportReport{}, wrapImportServiceError(err)
		}
		value := statement.ClosingBalance.Amount
		newBalance = &value
	default:
		newBalance = nil
	}

	maskedIBAN := ""
	if account.IBAN != nil {
		maskedIBAN = *account.IBAN
	}
	details := fmt.Sprintf(
		"%s into %s: %d new, %d known, %d probable duplicates, %d deleted earlier",
		statement.FormatLabel(),
		iban.Mask(maskedIBAN),
		imported,
		exactDuplicates,
		probableDuplicates,
		previouslyDeleted,
	)
	if len(wrongCurrency) > 0 {
		details += fmt.Sprintf(", %d in another currency skipped", len(wrongCurrency))
	}
	if err := audit.Log(ctx, d, "IMPORT_STATEMENT", audit.ActorUser, details); err != nil {
		return ImportReport{}, wrapImportServiceError(err)
	}

	return ImportReport{
		Imported:           imported,
		Duplicates:         exactDuplicates,
		Categorized:        categorized,
		BalanceUpdated:     newBalance,
		ProbableDuplicates: probableDuplicates,
		WrongCurrency:      len(wrongCurrency),
		Currency:           account.Currency,
		PreviouslyDeleted:  previouslyDeleted,
		InvalidAmount:      invalidAmount,
	}, nil
}

// BalanceUpdateAllowed reports whether the file's figures may replace the
// account balance. It may only do so if the file is at least as recent as the
// account's newest booking; back-filling an old statement must not reset
// today's balance.
//
// An account without bookings has no statement history to compare against, so
// its own balance state decides: if it was never set and the balance is zero,
// any file may set it; if it was typed by hand and never updated, only a recent
// file may replace it; if it was updated, only a file at least as recent as
// that update may replace it.
func BalanceUpdateAllowed(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	statement ParsedStatement,
	today civil.Date,
) (bool, error) {
	hasRunningBalances := false
	for _, booking := range statement.Bookings {
		if booking.BalanceAfter != nil {
			hasRunningBalances = true
			break
		}
	}
	if statement.ClosingBalance == nil {
		if !hasRunningBalances {
			return false, nil
		}
		return balanceUpdateAllowedFor(
			ctx, d, accountID, newestBookingDate(statement.Bookings), today,
		)
	}

	fileNewest := statement.ClosingBalance.Date
	if hasRunningBalances {
		fileNewest = newestBookingDate(statement.Bookings)
	}
	return balanceUpdateAllowedFor(ctx, d, accountID, fileNewest, today)
}

func newestBookingDate(bookings []ImportedBooking) civil.Date {
	newest := bookings[0].Date
	for _, booking := range bookings[1:] {
		if booking.Date.After(newest) {
			newest = booking.Date
		}
	}
	return newest
}

func balanceUpdateAllowedFor(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	fileNewest civil.Date,
	today civil.Date,
) (bool, error) {
	listed, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil {
		return false, wrapImportServiceError(err)
	}
	if len(listed) > 0 {
		return !fileNewest.Before(listed[0].BookingDate), nil
	}

	account, found, err := accounts.Get(ctx, d, accountID)
	if err != nil {
		return false, wrapImportServiceError(err)
	}
	if !found {
		return false, nil
	}
	if account.BalanceUpdatedAt == nil {
		if account.Balance.Minor == 0 {
			return true, nil
		}
		return !fileNewest.Before(today.AddDays(-TypedBalanceRecentDays)), nil
	}
	return !fileNewest.Before(civil.FromTime(*account.BalanceUpdatedAt)), nil
}

// IsNewestFirst reports whether the file lists the newest booking first. Its
// dates decide; a file covering a single day has only its running balances to
// go by: newest first, each row's balance is the next row's balance plus this
// row's amount.
func IsNewestFirst(bookings []ImportedBooking) bool {
	if len(bookings) < 2 {
		return false
	}
	first := bookings[0].Date
	last := bookings[len(bookings)-1].Date
	if first != last {
		return first.After(last)
	}

	newestFirst := 0
	oldestFirst := 0
	for index := 0; index+1 < len(bookings); index++ {
		a := bookings[index]
		b := bookings[index+1]
		if a.BalanceAfter == nil || b.BalanceAfter == nil {
			continue
		}
		if a.BalanceAfter.Cmp(b.BalanceAfter.Add(a.Amount)) == 0 {
			newestFirst++
		}
		if b.BalanceAfter.Cmp(a.BalanceAfter.Add(b.Amount)) == 0 {
			oldestFirst++
		}
	}
	return newestFirst > oldestFirst
}

func reverseBookings(bookings []ImportedBooking) {
	for left, right := 0, len(bookings)-1; left < right; left, right = left+1, right-1 {
		bookings[left], bookings[right] = bookings[right], bookings[left]
	}
}

func truncateChars(value *string, limit int) *string {
	if value == nil {
		return nil
	}
	characters := []rune(*value)
	if len(characters) <= limit {
		return value
	}
	truncated := string(characters[:limit])
	return &truncated
}
