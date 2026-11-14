package banksync

import (
	"context"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// FinTsConnection is everything needed to open a FinTS PIN/TAN dialog with one
// bank login.
type FinTsConnection struct {
	// BankName is the bank's display name.
	BankName string
	// BLZ is the German bank code.
	BLZ string
	// UserID is the online-banking login.
	UserID string
	// CustomerID is the customer id; it defaults to the user id when the bank
	// does not distinguish them.
	CustomerID string
	// ServerURL is the FinTS server URL.
	ServerURL string
	// TanMethod is a remembered TAN procedure id, if any.
	TanMethod *string
	// ProductID is the FinTS product registration id, supplied by the caller.
	ProductID string
	// SystemID is the remembered FinTS system id, or "" when none is known.
	SystemID string
	// HKTANVersion is the HITANS/HKTAN version of the remembered TAN method,
	// or 0 when none is known.
	HKTANVersion uint16
}

// String returns a representation that never contains the bank user id, the
// customer id, the product id or the system id.
func (c FinTsConnection) String() string {
	return fmt.Sprintf(
		"FinTsConnection{BankName:%q BLZ:%q UserID:%q CustomerID:%q ServerURL:%q TanMethod:%v ProductID:%q SystemID:%t HKTANVersion:%d}",
		c.BankName, c.BLZ, "***", "***", c.ServerURL, c.TanMethod, maskedProductID(c.ProductID),
		c.SystemID != "", c.HKTANVersion,
	)
}

// GoString returns a representation that never contains the bank user id, the
// customer id, the product id or the system id.
func (c FinTsConnection) GoString() string {
	return c.String()
}

// Format returns a representation that never contains the bank user id, the
// customer id, the product id or the system id, for every fmt verb.
func (c FinTsConnection) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(c.String()))
}

// maskedProductID hides a non-empty product id and keeps an absent one empty.
func maskedProductID(productID string) string {
	if productID == "" {
		return ""
	}
	return "***"
}

// FinTsAccount is a bank account as the bank reports it.
type FinTsAccount struct {
	// IBAN is the IBAN, possibly empty.
	IBAN string
	// BIC is the BIC reported by the bank.
	BIC string
	// AccountNumber is the account number.
	AccountNumber string
	// SubAccountNumber is a sub-account number, when the bank uses one.
	SubAccountNumber *string
	// BankName is the bank's name.
	BankName string
	// BLZ is the bank code.
	BLZ string
	// AccountType is the account type code translated by the adapter (checking,
	// savings, depot, loan, credit_card).
	AccountType string
	// Currency is the currency as stated by the bank.
	Currency string
	// ProductName is a product name, when the bank reports one.
	ProductName *string
	// CurrencyStated reports whether the bank stated the currency rather than the
	// adapter falling back to EUR.
	CurrencyStated bool
}

// FinTsBalance is a balance reported by the bank.
type FinTsBalance struct {
	// Account is the account the balance belongs to.
	Account FinTsAccount
	// Balance is the amount as delivered. Converted to minor units at the
	// insert; never rounded.
	Balance decimal.Decimal
	// AvailableBalance is an available balance, when the bank reports one.
	AvailableBalance *decimal.Decimal
	// Currency is the balance's currency as stated by the bank.
	Currency string
	// Date is when the bank reported the balance (Rust's OffsetDateTime).
	Date time.Time
}

// FinTsTransaction is one booking or pending line as the bank reports it.
type FinTsTransaction struct {
	// Account is the account the line belongs to.
	Account FinTsAccount
	// Date is the booking date.
	Date civil.Date
	// ValueDate is the value date.
	ValueDate civil.Date
	// Amount is the signed amount as delivered by the bank.
	Amount decimal.Decimal
	// Currency is the amount's currency.
	Currency string
	// PartnerName is the other party.
	PartnerName *string
	// PartnerIBAN is the other party's IBAN.
	PartnerIBAN *string
	// PartnerBIC is the other party's BIC.
	PartnerBIC *string
	// Purpose is the purpose text.
	Purpose *string
	// TransactionCode is the bank's transaction code.
	TransactionCode *string
	// Reference is the bank's reference.
	Reference *string
	// PrimaNota is the prima-nota number, when the bank reports one.
	PrimaNota *string
	// BalanceAfter is the running balance after this booking, when reported.
	BalanceAfter *decimal.Decimal
	// BankID is a bank-side booking id (for example CAMT).
	BankID *string
}

// AccountSnapshot is one account's state as reported in a sync.
type AccountSnapshot struct {
	// Account is the account.
	Account FinTsAccount
	// Balance is the balance, when the bank sent one.
	Balance *FinTsBalance
	// Transactions are the booked lines.
	Transactions []FinTsTransaction
	// StatementError explains why statements could not be fetched, if they could
	// not.
	StatementError *string
	// BalanceCode is the bank's return code(s) when it sent no balance. Never its
	// message text.
	BalanceCode *string
	// Pending holds pending lines the statement job reported. A nil slice means
	// not fetched (keep what is stored); an empty non-nil slice means fetched and
	// none pending (clear what is stored).
	Pending []FinTsTransaction
}

// FetchOutcome is the result of one dialog: everything the caller must persist,
// without reading state off a shared gateway.
type FetchOutcome struct {
	// Snapshots are accounts, balances and bookings.
	Snapshots []AccountSnapshot
	// TanMethod is the TAN method chosen during the dialog, so it can be
	// remembered.
	TanMethod *string
	// Capabilities are the login's supported business transactions, detected
	// during the dialog.
	Capabilities *BankCapabilities
	// SystemID is the system id the dialog got from the bank, when it got one.
	// It is remembered for the login and is never printed.
	SystemID *string
	// HKTANVersion is the HITANS/HKTAN version of the selected TAN method.
	HKTANVersion uint16
}

// String returns counts and presence flags only, never an IBAN, a system id,
// an account owner or any booking text.
func (o FetchOutcome) String() string {
	return fmt.Sprintf(
		"FetchOutcome{snapshots:%d tanMethod:%t capabilities:%t systemID:%t hktanVersion:%d}",
		len(o.Snapshots), o.TanMethod != nil, o.Capabilities != nil,
		o.SystemID != nil, o.HKTANVersion,
	)
}

// GoString returns counts and presence flags only.
func (o FetchOutcome) GoString() string {
	return o.String()
}

// Format writes counts and presence flags only, for every fmt verb.
func (o FetchOutcome) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(o.String()))
}

// FinTsGateway is the port to the bank. Tests use fakes; the real adapter is
// internal/core/fintsgateway.
type FinTsGateway interface {
	// FetchAll fetches all accounts of the login with balances and bookings from
	// since (per IBAN; nil skips statements for that account) up to to, within a
	// single dialog.
	//
	// The chosen TAN method and the detected capabilities travel in the returned
	// [FetchOutcome], so overlapping syncs never read another dialog's state off
	// the gateway. The outcome is returned together with an error, so a system id
	// obtained before a later failure is not lost.
	FetchAll(
		ctx context.Context,
		connection FinTsConnection,
		interaction BankingInteraction,
		since func(accountNumber string) *civil.Date,
		to civil.Date,
	) (FetchOutcome, error)
}

// UnavailableGateway is the gateway wired in until the real FinTS adapter (M5b)
// exists. It never touches the network and always fails with a localized "not
// available in this build" message.
type UnavailableGateway struct {
	language i18n.Language
}

// NewUnavailableGateway builds the placeholder gateway with the language of its
// user-visible message.
func NewUnavailableGateway(language i18n.Language) UnavailableGateway {
	return UnavailableGateway{language: language}
}

// DefaultUnavailableGateway is Rust's UnavailableGateway::default: English.
func DefaultUnavailableGateway() UnavailableGateway {
	return NewUnavailableGateway(i18n.En)
}

// Language returns the language of the message.
func (g UnavailableGateway) Language() i18n.Language {
	return g.language
}

// FetchAll always fails without touching the network.
func (g UnavailableGateway) FetchAll(
	_ context.Context,
	_ FinTsConnection,
	_ BankingInteraction,
	_ func(accountNumber string) *civil.Date,
	_ civil.Date,
) (FetchOutcome, error) {
	return FetchOutcome{}, NewGatewayOtherError(i18n.Text(g.language, i18n.ErrorOnlineSyncUnavailable))
}

// Ensure the placeholder satisfies the port.
var _ FinTsGateway = UnavailableGateway{}
