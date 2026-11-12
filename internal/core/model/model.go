// Package model holds the plain domain data.
//
// Nothing here holds a database connection. It ports
// `crates/finglass-core/src/model.rs`, so the Go build and the Rust build share
// the same field meanings and stored enum texts.
package model

import (
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// AccountID is an account row id.
type AccountID int64

// AccountGroupID is an account group row id.
type AccountGroupID int64

// BankConnectionID is a bank connection row id.
type BankConnectionID int64

// CategoryID is a category row id.
type CategoryID int64

// RuleID is a rule row id.
type RuleID int64

// TransactionID is a transaction row id.
type TransactionID int64

// AccountType says how an account is used.
type AccountType string

const (
	// AccountTypeChecking is a normal checking account.
	AccountTypeChecking AccountType = "checking"
	// AccountTypeSavings is a savings account.
	AccountTypeSavings AccountType = "savings"
	// AccountTypeCreditCard is a credit card account.
	AccountTypeCreditCard AccountType = "credit_card"
	// AccountTypeLoan is a loan account.
	AccountTypeLoan AccountType = "loan"
	// AccountTypeDepot is a securities depot.
	AccountTypeDepot AccountType = "depot"
)

// AsStr returns the text stored in the database.
func (t AccountType) AsStr() string {
	return string(t)
}

// AccountTypeFromStored reads the text stored in the database.
func AccountTypeFromStored(text string) (AccountType, bool) {
	switch text {
	case string(AccountTypeChecking), string(AccountTypeSavings), string(AccountTypeCreditCard),
		string(AccountTypeLoan), string(AccountTypeDepot):
		return AccountType(text), true
	default:
		return "", false
	}
}

// RuleField says which field a rule matches.
type RuleField string

const (
	// RuleFieldAny matches any field.
	RuleFieldAny RuleField = "any"
	// RuleFieldPartner matches the partner name.
	RuleFieldPartner RuleField = "partner"
	// RuleFieldPurpose matches the purpose text.
	RuleFieldPurpose RuleField = "purpose"
)

// AsStr returns the text stored in the database.
func (f RuleField) AsStr() string {
	return string(f)
}

// RuleFieldFromStored reads the text stored in the database.
func RuleFieldFromStored(text string) (RuleField, bool) {
	switch text {
	case string(RuleFieldAny), string(RuleFieldPartner), string(RuleFieldPurpose):
		return RuleField(text), true
	default:
		return "", false
	}
}

// RuleDirection says which direction a rule applies to.
type RuleDirection string

const (
	// RuleDirectionAny applies to both expenses and income.
	RuleDirectionAny RuleDirection = "any"
	// RuleDirectionExpense applies to expenses only.
	RuleDirectionExpense RuleDirection = "expense"
	// RuleDirectionIncome applies to income only.
	RuleDirectionIncome RuleDirection = "income"
)

// AsStr returns the text stored in the database.
func (d RuleDirection) AsStr() string {
	return string(d)
}

// RuleDirectionFromStored reads the text stored in the database.
func RuleDirectionFromStored(text string) (RuleDirection, bool) {
	switch text {
	case string(RuleDirectionAny), string(RuleDirectionExpense), string(RuleDirectionIncome):
		return RuleDirection(text), true
	default:
		return "", false
	}
}

// AccountGroup is a group that accounts can be sorted into.
type AccountGroup struct {
	// ID is the row id.
	ID AccountGroupID
	// Name is the display name.
	Name string
	// SortOrder is the position in account lists.
	SortOrder int64
}

// BankConnection is a connection to a bank.
type BankConnection struct {
	// ID is the row id.
	ID BankConnectionID
	// BankName is the bank's name.
	BankName string
	// BLZ is the German bank code.
	BLZ string
	// BIC is the BIC, when known.
	BIC *string
	// UserID is the bank's user id.
	UserID string
	// ServerURL is the FinTS server URL.
	ServerURL *string
	// TanMethod is the TAN method.
	TanMethod *string
	// SyncFrom is the first date to sync.
	SyncFrom *civil.Date
	// LastSync is the last successful sync.
	LastSync *time.Time
	// Capabilities is the bank's advertised capabilities.
	Capabilities *string
	// IncludeInSyncAll says whether the connection is part of "sync all".
	IncludeInSyncAll bool
	// Offline says whether this connection never goes online (statements are
	// imported from files).
	Offline bool
}

// BankAccessItem is a bank connection as the UI shows it.
type BankAccessItem struct {
	// ID is the row id.
	ID BankConnectionID
	// BankName is the bank's name.
	BankName string
	// BLZ is the German bank code; empty for an offline connection.
	BLZ string
	// BIC is the BIC, when known. An unknown BIC is stored as an empty string.
	BIC *string
	// UserID is the bank's user id; empty for an offline connection.
	UserID string
	// TanMethod is the TAN method.
	TanMethod *string
	// ServerURL is the FinTS server URL.
	ServerURL *string
	// LastSync is the last successful sync.
	LastSync *time.Time
	// Capabilities is the bank's advertised capabilities.
	Capabilities *string
	// SyncFrom is the first date to sync.
	SyncFrom *civil.Date
	// IncludeInSyncAll says whether the connection is part of "sync all".
	IncludeInSyncAll bool
	// Offline says whether this connection never goes online (statements are
	// imported from files).
	Offline bool
}

// Account is a bank account.
type Account struct {
	// ID is the row id.
	ID AccountID
	// BankConnectionID is the bank connection, when this is an online account.
	BankConnectionID *BankConnectionID
	// Institution is the institution name.
	Institution *string
	// Name is the display name.
	Name *string
	// IBAN is the IBAN.
	IBAN *string
	// AccountNumber is the account number.
	AccountNumber *string
	// Currency is the account currency.
	Currency money.Currency
	// AccountType says how the account is used.
	AccountType AccountType
	// Balance is the latest known balance.
	Balance money.Money
	// BalanceUpdatedAt is when the balance was last updated.
	BalanceUpdatedAt *time.Time
	// GroupID is the account group.
	GroupID *AccountGroupID
}

// Category is a spending category.
type Category struct {
	// ID is the row id.
	ID CategoryID
	// Name is the display name.
	Name string
	// Color is the display color.
	Color string
	// BudgetLimit is the optional monthly budget. The schema stores only minor
	// units, so the base currency is used.
	BudgetLimit *money.Money
	// IsTaxRelevant says whether bookings in this category are tax relevant.
	IsTaxRelevant bool
	// IsTransfer says whether this category marks transfers.
	IsTransfer bool
}

// Rule is a categorization rule.
type Rule struct {
	// ID is the row id.
	ID RuleID
	// Pattern is the text pattern to match.
	Pattern string
	// CategoryID is the category to assign.
	CategoryID CategoryID
	// Priority is the priority, highest first.
	Priority int64
	// AutoApply says whether matching bookings are categorized automatically.
	AutoApply bool
	// MatchField is the field the pattern is matched against.
	MatchField RuleField
	// Direction is the booking direction the rule applies to.
	Direction RuleDirection
	// TaxRelevant says whether matching bookings are tax relevant, when set.
	TaxRelevant *bool
}

// Transaction is a booked transaction.
type Transaction struct {
	// ID is the row id.
	ID TransactionID
	// AccountID is the account the booking belongs to.
	AccountID AccountID
	// BookingDate is the booking date.
	BookingDate civil.Date
	// Amount is the signed amount.
	Amount money.Money
	// BalanceAfter is the balance after the booking, when known.
	BalanceAfter *money.Money
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
	// CategoryID is the assigned category.
	CategoryID *CategoryID
	// ImportHash is the hash that identifies the booking for imports.
	ImportHash string
	// CreatedAt is when the row was created.
	CreatedAt time.Time
	// IsNew says whether the booking is still new to the user.
	IsNew bool
	// TaxOverride is an explicit tax flag, overriding the category's.
	TaxOverride *bool
}

// Direction is the money flow a transaction filter selects.
type Direction string

const (
	// DirectionAll selects both income and expenses.
	DirectionAll Direction = "all"
	// DirectionIncome selects money in.
	DirectionIncome Direction = "income"
	// DirectionExpenses selects money out.
	DirectionExpenses Direction = "expenses"
)

// AsStr returns the stored text.
func (d Direction) AsStr() string {
	return string(d)
}

// DirectionFromStored reads the stored text.
func DirectionFromStored(text string) (Direction, bool) {
	switch text {
	case string(DirectionAll), string(DirectionIncome), string(DirectionExpenses):
		return Direction(text), true
	default:
		return "", false
	}
}

// TaxSource says which layer decided a booking's effective tax relevance.
type TaxSource string

const (
	// TaxSourceManual is the user's stored override
	// (`transactions.tax_override`).
	TaxSourceManual TaxSource = "manual"
	// TaxSourceRule is a rule's tax flag, resolved at read time.
	TaxSourceRule TaxSource = "rule"
	// TaxSourceCategory is the category's `is_tax_relevant` flag.
	TaxSourceCategory TaxSource = "category"
	// TaxSourceNone means no layer said yes or no.
	TaxSourceNone TaxSource = "none"
)

// AsStr returns the stored text.
func (s TaxSource) AsStr() string {
	return string(s)
}

// TaxSourceFromStored reads the stored text.
func TaxSourceFromStored(text string) (TaxSource, bool) {
	switch text {
	case string(TaxSourceManual), string(TaxSourceRule), string(TaxSourceCategory), string(TaxSourceNone):
		return TaxSource(text), true
	default:
		return "", false
	}
}

// AccountIDSet is a set of account ids, matching Rust's `BTreeSet<AccountId>`.
type AccountIDSet map[AccountID]struct{}

// TransactionFilter is the filter behind a transaction list.
type TransactionFilter struct {
	// StartDate is the first booking date, inclusive.
	StartDate *civil.Date
	// EndDate is the last booking date, inclusive.
	EndDate *civil.Date
	// AccountID restricts to one account.
	AccountID *AccountID
	// AccountIDs restricts to these accounts; nil means no restriction and an
	// empty set matches nothing.
	AccountIDs *AccountIDSet
	// CategoryID restricts to one category.
	CategoryID *CategoryID
	// UncategorizedOnly selects only bookings without a category.
	UncategorizedOnly bool
	// TaxRelevantOnly selects only bookings whose effective tax relevance is
	// true.
	TaxRelevantOnly bool
	// Direction restricts to one money flow.
	Direction Direction
	// Text is a text search over partner name and purpose.
	Text *string
	// MinAmount is the smallest absolute amount, inclusive.
	MinAmount *money.Money
	// MaxAmount is the largest absolute amount, inclusive.
	MaxAmount *money.Money
	// Currency restricts to bookings in this currency.
	Currency *money.Currency
}

// TransactionItem is one booking as a list or report shows it.
type TransactionItem struct {
	// ID is the row id.
	ID TransactionID
	// AccountID is the account the booking belongs to.
	AccountID AccountID
	// AccountIBAN is the account's IBAN, when known.
	AccountIBAN *string
	// Date is the booking date.
	Date civil.Date
	// Amount is the signed amount.
	Amount money.Money
	// BalanceAfter is the bank's running balance after this booking, when
	// reported.
	BalanceAfter *money.Money
	// Currency is the account currency.
	Currency money.Currency
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
	// CategoryID is the assigned category.
	CategoryID *CategoryID
	// CategoryName is the category's display name.
	CategoryName *string
	// CategoryColor is the category's display color.
	CategoryColor *string
	// IsTaxRelevant says whether the booking is effectively tax relevant.
	IsTaxRelevant bool
	// DocumentCount is how many documents are linked to the booking.
	DocumentCount int
	// IsNew says whether the booking is still new to the user.
	IsNew bool
	// IsTransfer says whether the booking's category marks a transfer.
	IsTransfer bool
	// TaxSource says which layer decided the effective tax relevance.
	TaxSource TaxSource
	// TaxRulePattern is the deciding rule's pattern, when TaxSourceRule decided.
	TaxRulePattern *string
	// TaxOverride is the stored manual override; nil means automatic.
	TaxOverride *bool
}

// RawDescription is the raw partner (else purpose, else "—"), for matching and
// search only.
func (item TransactionItem) RawDescription() string {
	if item.PartnerName != nil && trimmedNotEmpty(*item.PartnerName) {
		return *item.PartnerName
	}
	if item.Purpose != nil && trimmedNotEmpty(*item.Purpose) {
		return *item.Purpose
	}
	return "—"
}

// IsExpense reports whether the amount is below zero.
func (item TransactionItem) IsExpense() bool {
	return item.Amount.Minor < 0
}

// CountsAsIncome reports money in on a normal category; transfers never count.
func (item TransactionItem) CountsAsIncome() bool {
	return item.Amount.Minor > 0 && !item.IsTransfer
}

// CountsAsSpending reports money out on a normal category; transfers never
// count.
func (item TransactionItem) CountsAsSpending() bool {
	return item.Amount.Minor < 0 && !item.IsTransfer
}

// DeletedBooking is a booking that was deleted and must not be imported again.
type DeletedBooking struct {
	// ID is the row id.
	ID int64
	// AccountID is the account the booking belonged to.
	AccountID AccountID
	// ImportHash is the import hash of the deleted booking.
	ImportHash string
	// DeletedAt is when it was deleted.
	DeletedAt time.Time
}

// PendingBooking is a pending (not yet booked) transaction.
type PendingBooking struct {
	// ID is the row id.
	ID int64
	// AccountID is the account the booking belongs to.
	AccountID AccountID
	// BookingDate is the booking date.
	BookingDate civil.Date
	// ValueDate is the value date, when known.
	ValueDate *civil.Date
	// Amount is the signed amount.
	Amount money.Money
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
	// FetchedAt is when the row was fetched.
	FetchedAt time.Time
}

// PendingBookingItem is one pending line as the UI shows it.
//
// Pending lines are not bookings: the bank may still change or drop them.
type PendingBookingItem struct {
	// AccountID is the account the booking belongs to.
	AccountID AccountID
	// Date is the booking date.
	Date civil.Date
	// ValueDate is the value date, when known.
	ValueDate *civil.Date
	// Amount is the signed amount.
	Amount money.Money
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
	// FetchedAt is when the sync that fetched the line ran.
	FetchedAt time.Time
}

// Document is a stored document.
type Document struct {
	// ID is the row id.
	ID int64
	// FileName is the original file name.
	FileName string
	// StoredPath is the path relative to the data folder.
	StoredPath string
	// MIMEType is the MIME type.
	MIMEType string
	// UploadedAt is when the document was uploaded.
	UploadedAt time.Time
	// TransactionID is the linked transaction.
	TransactionID *TransactionID
}

// DocumentItem is a document as the UI shows it.
type DocumentItem struct {
	// ID is the row id.
	ID int64
	// FileName is the original file name.
	FileName string
	// StoredPath is the path relative to the data folder.
	StoredPath string
	// MIMEType is the MIME type.
	MIMEType string
	// UploadedAt is when the document was uploaded.
	UploadedAt time.Time
	// TransactionID is the linked transaction.
	TransactionID *TransactionID
	// TransactionLabel is a readable label for the linked transaction, when
	// there is one.
	TransactionLabel *string
	// HasOCR says whether an OCR text row exists for this document.
	HasOCR bool
	// OCRConfidence is the OCR confidence, when known. This is not money, so it
	// is a float.
	OCRConfidence *float64
}

// IsPDF reports whether the document is a PDF.
func (item DocumentItem) IsPDF() bool {
	return item.MIMEType == "application/pdf"
}

// IsImage reports whether the document is an image.
func (item DocumentItem) IsImage() bool {
	return strings.HasPrefix(item.MIMEType, "image/")
}

// OCRText is text extracted from a document.
type OCRText struct {
	// ID is the row id.
	ID int64
	// DocumentID is the document it belongs to.
	DocumentID int64
	// Content is the extracted text.
	Content string
	// Language is the OCR language.
	Language string
	// Confidence is the OCR confidence, when known.
	Confidence *float64
}

// NewAccount holds the values needed to create an account.
type NewAccount struct {
	// BankConnectionID is the bank connection, when this is an online account.
	BankConnectionID *BankConnectionID
	// Institution is the institution name.
	Institution *string
	// Name is the display name.
	Name *string
	// IBAN is the IBAN.
	IBAN *string
	// AccountNumber is the account number.
	AccountNumber *string
	// Currency is the account currency.
	Currency money.Currency
	// AccountType says how the account is used.
	AccountType AccountType
	// Balance is the starting balance, in the account's currency.
	Balance money.Money
	// BalanceUpdatedAt is when the balance was last updated.
	BalanceUpdatedAt *time.Time
	// GroupID is the account group.
	GroupID *AccountGroupID
}

// NewAccountGroup holds the values needed to create an account group.
type NewAccountGroup struct {
	// Name is the display name.
	Name string
	// SortOrder is the position in account lists.
	SortOrder int64
}

// NewBankConnection holds the values needed to create an online bank
// connection.
type NewBankConnection struct {
	// BankName is the bank's name.
	BankName string
	// BLZ is the German bank code.
	BLZ string
	// BIC is the BIC, when known.
	BIC *string
	// UserID is the bank's user id.
	UserID string
}

// BankConnectionUpdate holds the changed values of a bank connection. nil
// leaves a value unchanged.
type BankConnectionUpdate struct {
	// ServerURL is the FinTS server URL. A blank value clears it.
	ServerURL *string
	// TanMethod is the TAN method.
	TanMethod *string
	// UserID is the bank's user id.
	UserID *string
	// BankName is the bank's display name. A blank value leaves it unchanged.
	BankName *string
	// BLZ is the German bank code.
	BLZ *string
	// BIC is the BIC. A blank value stores an empty BIC.
	BIC *string
}

// NewPendingBooking holds the values needed to store one pending booking.
type NewPendingBooking struct {
	// Date is the booking date.
	Date civil.Date
	// ValueDate is the value date, when known.
	ValueDate *civil.Date
	// Amount is the signed amount, in the account's currency.
	Amount money.Money
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
}

// NewDocument holds the values needed to create a document.
type NewDocument struct {
	// FileName is the original file name.
	FileName string
	// StoredPath is the path relative to the data folder.
	StoredPath string
	// MIMEType is the MIME type.
	MIMEType string
	// TransactionID is the linked transaction, when already known.
	TransactionID *TransactionID
}

// NewCategory holds the values needed to create or update a category.
type NewCategory struct {
	// Name is the display name.
	Name string
	// Color is the display color.
	Color string
	// BudgetLimit is the optional monthly budget, in the base currency.
	BudgetLimit *money.Money
	// IsTaxRelevant says whether bookings in this category are tax relevant.
	IsTaxRelevant bool
	// IsTransfer says whether this category marks transfers.
	IsTransfer bool
}

// NewRule holds the values needed to create or update a rule.
type NewRule struct {
	// Pattern is the text pattern to match.
	Pattern string
	// CategoryID is the category to assign.
	CategoryID CategoryID
	// Priority is the priority, highest first.
	Priority int64
	// AutoApply says whether matching bookings are categorized automatically.
	AutoApply bool
	// MatchField is the field the pattern is matched against.
	MatchField RuleField
	// Direction is the booking direction the rule applies to.
	Direction RuleDirection
	// TaxRelevant says whether matching bookings are tax relevant, when set.
	TaxRelevant *bool
}

// NewTransaction holds the values needed to import one transaction.
type NewTransaction struct {
	// BookingDate is the booking date.
	BookingDate civil.Date
	// Amount is the signed amount, in the account's currency.
	Amount money.Money
	// BalanceAfter is the balance after the booking, when known.
	BalanceAfter *money.Money
	// PartnerName is the other party.
	PartnerName *string
	// Purpose is the purpose text.
	Purpose *string
	// CategoryID is the category to assign.
	CategoryID *CategoryID
	// ImportHash is the hash that identifies the booking for imports.
	ImportHash string
	// CreatedAt is when the row is created.
	CreatedAt time.Time
	// IsNew says whether the booking is still new to the user.
	IsNew bool
	// TaxOverride is an explicit tax flag, overriding the category's.
	TaxOverride *bool
}

func trimmedNotEmpty(text string) bool {
	return strings.TrimSpace(text) != ""
}
