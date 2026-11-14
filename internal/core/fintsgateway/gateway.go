// Package fintsgateway is the real FinTS adapter behind
// banksync.FinTsGateway.
//
// It runs one read-only dialog per call: it synchronises or initialises the
// dialog, fetches the SEPA account list, fetches balances and booked statements
// and reports the chosen TAN method, the detected capabilities and the system
// id in the outcome.
//
// Not part of this slice: TAN media selection (HKTAB/SelectTanMedium), pending
// lines (camt pending and MT942 are not parsed), the trace switch, and wiring
// into the shell. OnlineSyncAvailable stays false until the user's own probe
// run.
package fintsgateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
	"github.com/MaMissaoui/Finglass/internal/fints/httptransport"
)

const (
	productVersion = "0.4"
	statementParse = "statement-parse"
)

// Fixed kind names used in GatewayErrorOther. They carry no bank value.
const (
	kindInvalidProductID = "invalid-product-id"
	kindInvalidBLZ       = "invalid-blz"
	kindMissingUserID    = "missing-user-id"
	kindInvalidServerURL = "invalid-server-url"
	kindNoTanMethod      = "NoTanMethod"
	kindDialogue         = "dialog"
)

// Option configures a [Gateway].
type Option func(*Gateway)

// WithTransportFactory replaces the transport factory. It is the test seam;
// production uses [httptransport.New].
func WithTransportFactory(factory func(serverURL string) (fints.Transport, error)) Option {
	return func(g *Gateway) {
		if factory != nil {
			g.transportFactory = factory
		}
	}
}

// WithSleeper replaces the sleeper used between decoupled polls.
func WithSleeper(sleeper fints.Sleeper) Option {
	return func(g *Gateway) {
		if sleeper != nil {
			g.sleeper = sleeper
		}
	}
}

// WithClock replaces the clock used for timestamps and a missing balance date.
func WithClock(now func() time.Time) Option {
	return func(g *Gateway) {
		if now != nil {
			g.now = now
		}
	}
}

// Gateway is the read-only FinTS adapter.
type Gateway struct {
	transportFactory func(serverURL string) (fints.Transport, error)
	sleeper          fints.Sleeper
	now              func() time.Time
}

// New builds the gateway. A transport factory, a sleeper and a clock may be
// injected through options.
func New(options ...Option) *Gateway {
	gateway := &Gateway{
		transportFactory: func(serverURL string) (fints.Transport, error) {
			return httptransport.New(serverURL)
		},
		sleeper: realSleeper{},
		now:     time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(gateway)
		}
	}
	return gateway
}

// String returns only presence flags, never a server URL or bank data.
func (g Gateway) String() string {
	return fmt.Sprintf(
		"fintsgateway.Gateway{transport:%t sleeper:%t clock:%t}",
		g.transportFactory != nil, g.sleeper != nil, g.now != nil,
	)
}

// GoString returns only presence flags.
func (g Gateway) GoString() string {
	return g.String()
}

// Format writes only presence flags, for every fmt verb.
func (g Gateway) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(g.String()))
}

// realSleeper is the production fints.Sleeper.
type realSleeper struct{}

// Sleep waits for d or until ctx is done.
func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// FetchAll runs one read-only dialog for the login.
func (g *Gateway) FetchAll(
	ctx context.Context,
	connection banksync.FinTsConnection,
	interaction banksync.BankingInteraction,
	since func(accountNumber string) *civil.Date,
	to civil.Date,
) (outcome banksync.FetchOutcome, err error) {
	if !validProductID(connection.ProductID) {
		return outcome, banksync.NewGatewayOtherError(kindInvalidProductID)
	}
	if !validBLZ(connection.BLZ) {
		return outcome, banksync.NewGatewayOtherError(kindInvalidBLZ)
	}
	if strings.TrimSpace(connection.UserID) == "" {
		return outcome, banksync.NewGatewayOtherError(kindMissingUserID)
	}
	transport, transportErr := g.transportFactory(connection.ServerURL)
	if transportErr != nil || transport == nil {
		return outcome, banksync.NewGatewayOtherError(kindInvalidServerURL)
	}

	entry := interaction.Pin(connection.BankName, connection.UserID, true)
	if entry == nil || entry.IsEmpty() || entry.Pin() == nil {
		return outcome, banksync.NewGatewayCancelledError()
	}

	remembered := ""
	if connection.TanMethod != nil {
		remembered = strings.TrimSpace(*connection.TanMethod)
	}
	params := paramsFor(connection, remembered)

	customerID := strings.TrimSpace(connection.CustomerID)
	if customerID == "" {
		customerID = connection.UserID
	}
	config := fints.DialogConfig{
		BLZ:            connection.BLZ,
		UserID:         connection.UserID,
		CustomerID:     customerID,
		SystemID:       connection.SystemID,
		ProductID:      connection.ProductID,
		ProductVersion: productVersion,
		PIN:            entry.Pin(),
		Transport:      transport,
		Sleeper:        g.sleeper,
		Source:         fints.Source{Now: g.now},
	}
	if connection.TanMethod != nil {
		config.Params = params
		config.PreferredSecurityFunction = remembered
	}

	dialog, dialogErr := fints.NewDialog(config)
	if dialogErr != nil {
		return outcome, mapError(dialogErr)
	}
	defer func() {
		params := dialog.Params()
		if systemID := dialog.SystemID(); systemID != "" && systemID != "0" {
			value := systemID
			outcome.SystemID = &value
		}
		if securityFunction := params.SelectedSecurityFunction; securityFunction != "" &&
			securityFunction != fints.SecurityFunctionPINOnly {
			value := securityFunction
			outcome.TanMethod = &value
		}
		if method, ok := params.SelectedMethod(); ok {
			outcome.HKTANVersion = method.HKTANVersion
		}
		if err == nil {
			capabilities := detectCapabilities(params)
			outcome.Capabilities = &capabilities
		}
	}()

	needsSync := connection.SystemID == "" || remembered == ""
	if needsSync {
		if syncErr := dialog.Sync(ctx); syncErr != nil {
			return outcome, mapError(syncErr)
		}
		// The bank must resend its parameters on every Init, so the versions
		// stay 0 even though the sync response carried HIBPA and HIUPA.
		dialog.Params().BPDVersion = 0
		dialog.Params().UPDVersion = 0
		if remembered == "" {
			if selectionErr := g.chooseTanMethod(interaction, dialog.Params()); selectionErr != nil {
				_ = dialog.Cancel(ctx)
				return outcome, selectionErr
			}
		}
	}

	initResult, initErr := dialog.Init(ctx)
	if initErr != nil {
		return outcome, mapError(initErr)
	}
	if !initResult.Opened {
		if tanErr := g.completeInitialTAN(ctx, dialog, interaction, initResult); tanErr != nil {
			return outcome, tanErr
		}
	}

	childCtx, cancel := context.WithCancel(ctx)
	prompter := &tanPrompter{interaction: interaction, dialog: dialog, cancel: cancel}
	workflow := fints.NewWorkflow(dialog, prompter)
	defer cancel()

	if _, fetchErr := workflow.FetchSEPAAccounts(childCtx); fetchErr != nil {
		return outcome, mapError(fetchErr)
	}

	for _, account := range dialog.Params().Accounts {
		if !usableAccount(workflow, account) {
			continue
		}
		mapped := mapAccount(account, connection)
		snapshot := banksync.AccountSnapshot{Account: mapped}

		balance, ok, balanceErr := workflow.FetchBalance(childCtx, account)
		if balanceErr != nil {
			return outcome, mapError(balanceErr)
		}
		if ok {
			snapshot.Balance = mapBalance(balance, mapped, g.now())
		}

		if from := since(account.IBAN); from != nil {
			statements, statementErr := workflow.FetchStatements(
				childCtx, account, toFinTSDate(*from), toFinTSDate(to),
			)
			if statementErr != nil {
				return outcome, mapError(statementErr)
			}
			bookings, parseOK := mapBookings(statements, mapped)
			if !parseOK {
				message := statementParse
				snapshot.StatementError = &message
			}
			snapshot.Transactions = bookings
		}

		outcome.Snapshots = append(outcome.Snapshots, snapshot)
	}

	_ = dialog.End(ctx)
	return outcome, nil
}

// paramsFor builds the remembered parameters. Without a sync, the remembered
// TAN method is seeded so the first HKTAN already carries its version; Init's
// HITANS then replaces the seed before the TAN decision.
func paramsFor(connection banksync.FinTsConnection, remembered string) *fints.BankParams {
	params := fints.NewBankParams()
	if remembered == "" {
		return params
	}
	params.SelectedSecurityFunction = remembered
	params.PreferredSecurityFunction = remembered
	if connection.SystemID == "" {
		// A sync follows and the bank resends HITANS; no seed is needed.
		return params
	}
	version := connection.HKTANVersion
	if version == 0 {
		version = 7
	}
	params.TANMethods = []fints.TanMethod{{
		SecurityFunction:  remembered,
		HKTANVersion:      version,
		DecoupledMaxPolls: -1,
	}}
	return params
}

// chooseTanMethod picks a security function after a sync. It never offers the
// PIN-only function 999 or a function without a HITANS method.
func (g *Gateway) chooseTanMethod(
	interaction banksync.BankingInteraction,
	params *fints.BankParams,
) error {
	allowed := params.AllowedSecurityFunctions
	usableFunctions := make([]string, 0, len(allowed))
	options := make([]banksync.TanMethod, 0, len(allowed))
	for _, securityFunction := range allowed {
		if securityFunction == fints.SecurityFunctionPINOnly {
			continue
		}
		method, ok := methodForSecurityFunction(params.TANMethods, securityFunction)
		if !ok {
			continue
		}
		usableFunctions = append(usableFunctions, securityFunction)
		options = append(options, banksync.TanMethod{
			ID:   securityFunction,
			Name: method.Name(),
		})
	}
	if len(usableFunctions) == 0 {
		return banksync.NewGatewayOtherError(kindNoTanMethod)
	}
	if params.PreferredSecurityFunction != "" {
		return nil
	}
	if len(usableFunctions) == 1 {
		params.SelectedSecurityFunction = usableFunctions[0]
		params.PreferredSecurityFunction = usableFunctions[0]
		return nil
	}
	answer := interaction.SelectTanMethod(options)
	if answer == nil {
		return banksync.NewGatewayCancelledError()
	}
	params.SelectedSecurityFunction = *answer
	params.PreferredSecurityFunction = *answer
	return nil
}

// completeInitialTAN completes a TAN-pending Init. The challenge text goes to
// the interaction only, never into an error.
func (g *Gateway) completeInitialTAN(
	ctx context.Context,
	dialog *fints.Dialog,
	interaction banksync.BankingInteraction,
	result fints.InitResult,
) error {
	text := result.Challenge.Challenge()
	if result.Decoupled {
		if !interaction.ConfirmDecoupled(text) {
			_ = dialog.Cancel(ctx)
			return banksync.NewGatewayCancelledError()
		}
		if _, pollErr := dialog.Poll(ctx); pollErr != nil {
			return mapError(pollErr)
		}
		return nil
	}

	tan := interaction.Tan(banksync.TanChallenge{Message: text})
	if tan == nil {
		_ = dialog.Cancel(ctx)
		return banksync.NewGatewayCancelledError()
	}
	if _, submitErr := dialog.SubmitTAN(ctx, tan); submitErr != nil {
		return mapError(submitErr)
	}
	return nil
}

// tanPrompter adapts banksync.BankingInteraction to fints.TanPrompter. A
// declined decoupled confirmation cancels the child context so the poll stops.
type tanPrompter struct {
	interaction banksync.BankingInteraction
	dialog      *fints.Dialog
	cancel      context.CancelFunc
}

// Challenge asks for a typed TAN. A nil answer cancels the dialog.
func (p *tanPrompter) Challenge(
	_ context.Context,
	challenge fints.TanChallenge,
	decoupled bool,
) (*secret.Text, error) {
	if decoupled {
		return nil, context.Canceled
	}
	tan := p.interaction.Tan(banksync.TanChallenge{Message: challenge.Challenge()})
	if tan == nil {
		_ = p.dialog.Cancel(context.Background())
		return nil, context.Canceled
	}
	return tan, nil
}

// Decoupled asks the user to confirm in the bank's app. Declining cancels the
// child context, which stops the workflow's poll.
func (p *tanPrompter) Decoupled(_ context.Context, challenge fints.TanChallenge) {
	if !p.interaction.ConfirmDecoupled(challenge.Challenge()) {
		p.cancel()
	}
}

// mapError translates a dialog failure into the gateway's failure kinds. The
// bank's text is never copied.
func mapError(err error) banksync.GatewayError {
	var dialogError *fints.DialogError
	if errors.As(err, &dialogError) {
		switch dialogError.Kind {
		case fints.ErrorKindPinRejected:
			if includesPinRejection(dialogError.Codes) {
				return banksync.NewGatewayPinRejectedError()
			}
			return banksync.NewGatewayBankError(dialogError.Codes)
		case fints.ErrorKindAccountLocked, fints.ErrorKindBankError, fints.ErrorKindTanRejected:
			return banksync.NewGatewayBankError(dialogError.Codes)
		case fints.ErrorKindTransport:
			if dialogError.HTTPStatus != 0 {
				if status, ok := gatewayHTTPStatus(dialogError.HTTPStatus); ok {
					return banksync.NewGatewayHTTPStatusError(status)
				}
			}
			return banksync.NewGatewayNetworkError()
		case fints.ErrorKindCancelled:
			return banksync.NewGatewayCancelledError()
		case fints.ErrorKindProtocol, fints.ErrorKindPollLimit,
			fints.ErrorKindTouchdownLimit, fints.ErrorKindInvalidState:
			return banksync.NewGatewayOtherError(dialogError.Kind.String())
		default:
			return banksync.NewGatewayOtherError(kindDialogue)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return banksync.NewGatewayCancelledError()
	}
	return banksync.NewGatewayOtherError(kindDialogue)
}

var pinRejectionCodes = map[string]struct{}{
	"9340": {},
	"9910": {},
	"9930": {},
	"9931": {},
	"9942": {},
}

// gatewayHTTPStatus narrows an HTTP status to the gateway's uint16. An
// out-of-range value is treated as a network failure instead of wrapping.
func gatewayHTTPStatus(status int) (uint16, bool) {
	if status < 0 || status > 65535 {
		return 0, false
	}
	return uint16(status), true
}

func includesPinRejection(codes []string) bool {
	for _, code := range codes {
		if _, ok := pinRejectionCodes[code]; ok {
			return true
		}
	}
	return false
}

func methodForSecurityFunction(methods []fints.TanMethod, securityFunction string) (fints.TanMethod, bool) {
	for _, method := range methods {
		if method.SecurityFunction == securityFunction {
			return method, true
		}
	}
	return fints.TanMethod{}, false
}

// usableAccount is the probe's account filter: an IBAN, a buildable request
// and, when HIUPD lists allowed transactions, both a balance and a statement
// job.
func usableAccount(workflow *fints.Workflow, account fints.Account) bool {
	if account.IBAN == "" {
		return false
	}
	if len(account.AllowedBusinessTransactions) > 0 {
		hasBalance := false
		hasStatement := false
		for _, transaction := range account.AllowedBusinessTransactions {
			switch transaction {
			case "HKSAL":
				hasBalance = true
			case "HKKAZ", "HKCAZ":
				hasStatement = true
			}
		}
		if !hasBalance || !hasStatement {
			return false
		}
	}
	return workflow.CanRequest(account)
}

func mapAccount(account fints.Account, connection banksync.FinTsConnection) banksync.FinTsAccount {
	var subAccount *string
	if account.SubAccount != "" {
		value := account.SubAccount
		subAccount = &value
	}
	var productName *string
	if name := account.ProductName(); name != "" {
		value := name
		productName = &value
	}
	return banksync.FinTsAccount{
		IBAN:             account.IBAN,
		BIC:              account.BIC,
		AccountNumber:    account.AccountNumber,
		SubAccountNumber: subAccount,
		BankName:         connection.BankName,
		BLZ:              connection.BLZ,
		AccountType:      accountType(account.AccountType),
		Currency:         account.Currency,
		ProductName:      productName,
		CurrencyStated:   account.Currency != "",
	}
}

// accountType maps the HIUPD Kontoart. Values outside the known ranges are a
// checking account.
func accountType(code string) string {
	value, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return model.AccountTypeChecking.AsStr()
	}
	switch {
	case value >= 1 && value <= 9:
		return model.AccountTypeChecking.AsStr()
	case value >= 10 && value <= 29:
		return model.AccountTypeSavings.AsStr()
	case value >= 30 && value <= 39, value >= 60 && value <= 69:
		return model.AccountTypeDepot.AsStr()
	case value >= 40 && value <= 49:
		return model.AccountTypeLoan.AsStr()
	case value >= 50 && value <= 59:
		return model.AccountTypeCreditCard.AsStr()
	case value >= 70 && value <= 79:
		return model.AccountTypeSavings.AsStr()
	default:
		return model.AccountTypeChecking.AsStr()
	}
}

func mapBalance(balance fints.Balance, account banksync.FinTsAccount, now time.Time) *banksync.FinTsBalance {
	date := now
	if balance.HasDate {
		date = time.Date(
			balance.Date.Year, time.Month(balance.Date.Month), balance.Date.Day,
			0, 0, 0, 0, time.UTC,
		)
	}
	return &banksync.FinTsBalance{
		Account:          account,
		Balance:          balance.Amount,
		AvailableBalance: balance.Available,
		Currency:         balance.Currency,
		Date:             date,
	}
}

// mapBookings parses only booked statement bytes, never pending ones. It keeps
// the bookings parsed before a failure and reports the failure so the caller
// can set the fixed StatementError kind.
func mapBookings(statements fints.Statements, account banksync.FinTsAccount) ([]banksync.FinTsTransaction, bool) {
	transactions := make([]banksync.FinTsTransaction, 0)
	if statements.Kind == fints.StatementCamt {
		for _, document := range statements.Camt.Booked {
			parsed, err := imports.ParseCAMT(document)
			if err != nil {
				return transactions, false
			}
			for _, booking := range parsed.Bookings {
				transactions = append(transactions, mapBooking(booking, account, true))
			}
		}
		return transactions, true
	}

	if len(statements.MT940.Booked) == 0 {
		return transactions, true
	}
	parsed, err := imports.ParseMT940(statements.MT940.Booked)
	if err != nil {
		return transactions, false
	}
	for _, booking := range parsed.Bookings {
		transactions = append(transactions, mapBooking(booking, account, false))
	}
	return transactions, true
}

func mapBooking(
	booking imports.ImportedBooking,
	account banksync.FinTsAccount,
	camt bool,
) banksync.FinTsTransaction {
	valueDate := booking.Date
	if booking.ValueDate != nil {
		valueDate = *booking.ValueDate
	}
	currency := account.Currency
	if booking.Currency != nil && *booking.Currency != "" {
		currency = *booking.Currency
	}
	if currency == "" {
		currency = "EUR"
	}
	transaction := banksync.FinTsTransaction{
		Account:      account,
		Date:         booking.Date,
		ValueDate:    valueDate,
		Amount:       booking.Amount,
		Currency:     currency,
		PartnerName:  booking.PartnerName,
		PartnerIBAN:  booking.PartnerIBAN,
		Purpose:      booking.Purpose,
		Reference:    booking.BankReference,
		BalanceAfter: booking.BalanceAfter,
	}
	if camt {
		transaction.BankID = booking.BankReference
	}
	return transaction
}

// detectCapabilities maps the BPD parameter segments to banksync's read jobs.
func detectCapabilities(params *fints.BankParams) banksync.BankCapabilities {
	return banksync.DetectCapabilities(func(job string) bool {
		switch job {
		case banksync.JobBalance:
			return params.HighestVersion(fints.BusinessHKSAL) != 0
		case banksync.JobStatementsMT940:
			return params.HighestVersion(fints.BusinessHKKAZ) != 0
		case banksync.JobStatementsCAMT:
			return offersAcceptedCamt(params.CamtFormats)
		case banksync.JobAccounts:
			return params.HighestVersion(fints.BusinessHKSPA) != 0
		case banksync.JobTANMedia:
			return params.HighestVersion(fints.BusinessHKTAB) != 0
		default:
			return false
		}
	})
}

func offersAcceptedCamt(formats []string) bool {
	for _, format := range formats {
		lower := strings.ToLower(format)
		switch {
		case strings.HasSuffix(lower, "camt.052.001.02"),
			strings.HasSuffix(lower, "camt.052.001.08"),
			strings.Contains(lower, "camt.053"):
			return true
		}
	}
	return false
}

func validProductID(value string) bool {
	if len(value) < 1 || len(value) > 30 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validBLZ(value string) bool {
	if len(value) != 8 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func toFinTSDate(date civil.Date) fints.Date {
	return fints.Date{Year: date.Year(), Month: int(date.Month()), Day: date.Day()}
}

// Ensure the adapter satisfies the port.
var _ banksync.FinTsGateway = (*Gateway)(nil)
