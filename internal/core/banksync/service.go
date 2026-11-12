// Bank-connection lifecycle and sync, ported from FinDesk's BankingService.kt
// and crates/finglass-core/src/sync/service.rs, minus every order method.
package banksync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// InitialSyncDays is how far the first sync goes back (many banks require a
// TAN for more than 90 days).
const InitialSyncDays = 90

// SyncOverlapDays is how far later syncs overlap the previous one to catch
// late bookings; duplicates are skipped.
const SyncOverlapDays = 10

// ImportResult is what one account's bank bookings did.
type ImportResult struct {
	// Imported is the number of rows inserted.
	Imported int
	// Duplicates is the number of exact duplicates, probable duplicates and
	// remembered deletions.
	Duplicates int
	// Categorized is the number of inserted rows a rule categorized.
	Categorized int
	// InvalidAmount is the number of bookings skipped because their amount has
	// too many decimal places for the account's currency.
	InvalidAmount int
}

// SyncReport is what a sync did.
type SyncReport struct {
	// AccountsSynced is the number of accounts the bank listed with an IBAN.
	AccountsSynced int
	// AccountsAdded is the number of accounts created locally.
	AccountsAdded int
	// Imported is the number of bookings inserted.
	Imported int
	// Duplicates is the number of bookings already known (including remembered
	// deletions).
	Duplicates int
	// Categorized is the number of bookings a rule categorized.
	Categorized int
	// Warnings are things that should make the sync look incomplete, such as a
	// failed statement job.
	Warnings []string
	// Notes are things the user should know that must not make the sync look
	// failed.
	Notes []string
	// HistoryFrom is the chosen history start the sync used, if any.
	HistoryFrom *civil.Date
}

// Summary returns the summary in English, matching FinDesk's wording.
func (r SyncReport) Summary() string {
	return r.SummaryIn(i18n.En)
}

// SummaryIn returns the summary in language.
func (r SyncReport) SummaryIn(language i18n.Language) string {
	var text strings.Builder
	text.WriteString(i18n.Format(language, i18n.SyncSummary, i18n.Int(int64(r.AccountsSynced))))
	if r.AccountsAdded > 0 {
		text.WriteString(i18n.Format(language, i18n.SyncSummaryNew, i18n.Int(int64(r.AccountsAdded))))
	}
	text.WriteString(i18n.Format(language, i18n.SyncSummaryTransactions, i18n.Int(int64(r.Imported))))
	if r.Categorized > 0 {
		text.WriteString(i18n.Format(language, i18n.SyncSummaryCategorized, i18n.Int(int64(r.Categorized))))
	}
	if r.Duplicates > 0 {
		text.WriteString(i18n.Format(language, i18n.SyncSummaryDuplicates, i18n.Int(int64(r.Duplicates))))
	}
	if r.HistoryFrom != nil {
		text.WriteString(i18n.Format(
			language,
			i18n.SyncSummaryHistoryFrom,
			i18n.Str(formatDateDMY(*r.HistoryFrom)),
		))
	}
	for _, note := range r.Notes {
		text.WriteByte('\n')
		text.WriteString(note)
	}
	return text.String()
}

// BankingService coordinates FinTS work with the local database.
type BankingService struct {
	client   *FinTsClient
	language i18n.Language
}

// NewBankingService builds a service around a client.
func NewBankingService(client *FinTsClient) *BankingService {
	return &BankingService{client: client, language: client.Language()}
}

// Client returns the client, for callers that need CanRememberPin or
// PinNotSaved.
func (s *BankingService) Client() *FinTsClient {
	return s.client
}

// CanRememberPin reports whether the system offers a credential store, so the
// UI may offer to remember the PIN.
func (s *BankingService) CanRememberPin() bool {
	return s.client.CanRememberPin()
}

// PinNotSaved reports whether the last sync's opted-in PIN could not be saved.
func (s *BankingService) PinNotSaved() bool {
	return s.client.PinNotSaved()
}

// HasSavedPin reports whether a PIN is remembered for bankID (settings flag
// only; the credential store is never read here).
func (s *BankingService) HasSavedPin(ctx context.Context, database *db.Database, bankID model.BankConnectionID) bool {
	return s.client.HasSavedPin(ctx, database, bankID)
}

// ForgetPin deletes a remembered PIN for bankID.
func (s *BankingService) ForgetPin(ctx context.Context, database *db.Database, bankID model.BankConnectionID) {
	s.client.ForgetPin(ctx, database, bankID)
}

// AddBankConnection registers a bank login locally (no network). The first
// sync asks for the PIN and fetches the accounts.
func (s *BankingService) AddBankConnection(
	ctx context.Context,
	database *db.Database,
	config FinTsConfig,
) (model.BankConnection, error) {
	bic := config.BIC
	connection, err := bankconnections.Create(ctx, database, model.NewBankConnection{
		BankName: config.BankName,
		BLZ:      config.BLZ,
		BIC:      &bic,
		UserID:   config.UserID,
	})
	if err != nil {
		return model.BankConnection{}, internalError(err)
	}

	if config.ServerURL != nil || config.TANMethod != nil {
		if _, err := bankconnections.UpdateConnection(ctx, database, connection.ID, model.BankConnectionUpdate{
			ServerURL: config.ServerURL,
			TanMethod: config.TANMethod,
		}); err != nil {
			return model.BankConnection{}, internalError(err)
		}
	}

	_ = audit.Log(
		ctx,
		database,
		CodeAddBank,
		audit.ActorUser,
		fmt.Sprintf("Added bank access %d", int64(connection.ID)),
	)
	created, ok, err := bankconnections.Get(ctx, database, connection.ID)
	if err != nil {
		return model.BankConnection{}, internalError(err)
	}
	if !ok {
		return model.BankConnection{}, NewBankingError(
			CodeNotFound,
			i18n.Text(s.language, i18n.ErrorBankConnectionNotFound),
		)
	}
	return created, nil
}

// UpdateConnection saves a bank login's connection settings.
//
// The name is only a label. When the BLZ, user id or server URL changed, the
// stored login is reset (registration and PIN) so the next sync registers
// again instead of reusing the old values. A PIN is bound to the bank code and
// user id; changing the server URL also forgets it, so a kept PIN is never
// sent to a different host.
func (s *BankingService) UpdateConnection(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	name,
	blz,
	userID,
	serverURL string,
) (model.BankAccessItem, error) {
	previous, ok, err := bankconnections.GetItem(ctx, database, bankID)
	if err != nil {
		return model.BankAccessItem{}, internalError(err)
	}
	if !ok {
		return model.BankAccessItem{}, NewBankingError(
			CodeNotFound,
			i18n.Text(s.language, i18n.ErrorBankConnectionNotFound),
		)
	}

	// An offline bank has no FinTS login: only its name may change, and never
	// its registration or PIN.
	if previous.Offline {
		updated, err := bankconnections.UpdateConnection(ctx, database, bankID, model.BankConnectionUpdate{
			BankName: &name,
		})
		if err != nil {
			return model.BankAccessItem{}, internalError(err)
		}
		if updated == nil {
			return model.BankAccessItem{}, NewBankingError(
				CodeNotFound,
				i18n.Text(s.language, i18n.ErrorBankConnectionNotFound),
			)
		}
		return *updated, nil
	}

	newBLZ := strings.TrimSpace(blz)
	newUserID := strings.TrimSpace(userID)
	newURLText := strings.TrimSpace(serverURL)
	var newURL *string
	if newURLText != "" {
		newURL = &newURLText
	}
	blzChanged := previous.BLZ != newBLZ
	userChanged := previous.UserID != newUserID
	urlChanged := differentStringPointers(previous.ServerURL, newURL)
	loginChanged := blzChanged || userChanged || urlChanged

	// Reset before saving: a failed save then only costs a fresh registration,
	// while a failed reset after the save would leave the old registration in
	// use with the new settings shown. Any login change forgets the PIN.
	if loginChanged {
		if err := s.client.ResetLogin(ctx, database, bankID, true); err != nil {
			return model.BankAccessItem{}, err
		}
	}

	updated, err := bankconnections.UpdateConnection(ctx, database, bankID, model.BankConnectionUpdate{
		ServerURL: &serverURL,
		UserID:    &userID,
		BankName:  &name,
		BLZ:       &blz,
		// The BIC follows the BLZ only when the bank directory knows the new
		// one. The directory is not ported, so the old BIC stays.
		BIC: nil,
	})
	if err != nil {
		return model.BankAccessItem{}, internalError(err)
	}
	if updated == nil {
		return model.BankAccessItem{}, NewBankingError(
			CodeNotFound,
			i18n.Text(s.language, i18n.ErrorBankConnectionNotFound),
		)
	}

	if loginChanged {
		fields := make([]string, 0, 3)
		if blzChanged {
			fields = append(fields, "bank code")
		}
		if userChanged {
			fields = append(fields, "user ID")
		}
		if urlChanged {
			fields = append(fields, "server URL")
		}
		_ = audit.Log(
			ctx,
			database,
			CodeResetFintsLogin,
			audit.ActorUser,
			fmt.Sprintf(
				"Bank access %d: %s changed, registration reset",
				int64(bankID),
				strings.Join(fields, ", "),
			),
		)
	}
	if blzChanged {
		_ = audit.Log(
			ctx,
			database,
			CodeChangeBankCode,
			audit.ActorUser,
			fmt.Sprintf("Bank access %d: BLZ %s -> %s", int64(bankID), previous.BLZ, newBLZ),
		)
	}

	return *updated, nil
}

// RemoveBank removes a bank login with its accounts, transactions and stored
// FinTS registration/PIN.
func (s *BankingService) RemoveBank(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
) (bool, error) {
	// PIN and flag first, so a later failure can never leave a secret behind
	// for a bank that no longer exists.
	s.client.DeleteStoredCredentials(ctx, database, bankID)

	removed, err := bankconnections.DeleteWithAccounts(ctx, database, bankID)
	if err != nil {
		return false, internalError(err)
	}
	if removed {
		_ = audit.Log(
			ctx,
			database,
			CodeDeleteBankAccess,
			audit.ActorUser,
			fmt.Sprintf("Deleted bank access %d", int64(bankID)),
		)
	}
	return removed, nil
}

// SyncBankConnection syncs one bank login: accounts (matched by IBAN, new ones
// are created), balances and bookings.
func (s *BankingService) SyncBankConnection(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	interaction BankingInteraction,
	today civil.Date,
) (SyncReport, error) {
	bank, ok, err := bankconnections.GetItem(ctx, database, bankID)
	if err != nil {
		return SyncReport{}, internalError(err)
	}
	if !ok {
		return SyncReport{}, NewBankingError(
			CodeNotFound,
			i18n.Text(s.language, i18n.ErrorBankConnectionNotFound),
		)
	}
	if bank.Offline {
		return SyncReport{}, NewBankingError(
			CodeOffline,
			i18n.Text(s.language, i18n.ErrorBankOffline),
		)
	}

	allAccounts, err := accounts.List(ctx, database)
	if err != nil {
		return SyncReport{}, internalError(err)
	}
	known := make([]model.Account, 0, len(allAccounts))
	for _, account := range allAccounts {
		if account.BankConnectionID != nil && *account.BankConnectionID == bankID {
			known = append(known, account)
		}
	}
	defaultStart := today.AddDays(-InitialSyncDays)
	historyFrom := bank.SyncFrom

	since := func(accountNumber string) *civil.Date {
		normal := defaultStart
		for _, account := range known {
			if account.IBAN != nil && equalASCIIFold(*account.IBAN, accountNumber) {
				if bank.LastSync != nil {
					overlap := civil.FromTime(*bank.LastSync).AddDays(-SyncOverlapDays)
					if overlap.After(defaultStart) {
						normal = overlap
					}
				}
				break
			}
		}
		if historyFrom != nil && historyFrom.Before(normal) {
			value := *historyFrom
			return &value
		}
		value := normal
		return &value
	}

	snapshots, err := s.client.FetchAll(ctx, database, bank, interaction, since, today)
	if err != nil {
		return SyncReport{}, err
	}
	return s.storeSnapshots(ctx, database, &bank, known, snapshots, historyFrom, today)
}

// ImportTransactions imports bank bookings, skipping ones already imported
// (stored or remembered as deleted) and applying rules.
func (s *BankingService) ImportTransactions(
	ctx context.Context,
	database *db.Database,
	accountID model.AccountID,
	bookings []FinTsTransaction,
) (ImportResult, error) {
	account, ok, err := accounts.Get(ctx, database, accountID)
	if err != nil {
		return ImportResult{}, internalError(err)
	}
	if !ok {
		return ImportResult{}, NewBankingError(
			CodeNotFound,
			i18n.Text(s.language, i18n.ErrorAccountNotFound),
		)
	}

	candidates := make([]imports.Candidate[FinTsTransaction], 0, len(bookings))
	for _, transaction := range bookings {
		candidates = append(candidates, imports.Candidate[FinTsTransaction]{
			Date:   transaction.Date,
			Amount: transaction.Amount,
			Hash: imports.ImportHash(
				transaction.Date,
				transaction.Amount,
				transaction.PartnerName,
				transaction.Purpose,
				transaction.Account.IBAN,
				transaction.BankID,
				transaction.BalanceAfter,
				transaction.PrimaNota,
			),
			Item: transaction,
		})
	}

	plan, err := imports.PlanDuplicates(ctx, database, accountID, account.Currency, candidates)
	if err != nil {
		return ImportResult{}, internalError(err)
	}
	hasBookings, err := transactions.HasBookings(ctx, database, accountID)
	if err != nil {
		return ImportResult{}, internalError(err)
	}
	isFirstImport := !hasBookings
	matcher, err := rules.Matcher(ctx, database)
	if err != nil {
		return ImportResult{}, internalError(err)
	}

	invalidAmount := 0
	rows := make([]model.NewTransaction, 0, len(plan.ToImport))
	for _, candidate := range plan.ToImport {
		transaction := candidate.Item
		amount, err := imports.AmountToMoney(transaction.Amount, account.Currency)
		if err != nil {
			invalidAmount++
			continue
		}
		var balanceAfter *money.Money
		if transaction.BalanceAfter != nil {
			converted, err := imports.AmountToMoney(*transaction.BalanceAfter, account.Currency)
			if err != nil {
				invalidAmount++
				continue
			}
			balanceAfter = &converted
		}
		// Booking texts are the user's own data and are stored as the bank sent
		// them, as FinDesk and the CSV import do (rules and search may match an
		// IBAN in the purpose). Only error, warning and note texts are masked.
		partnerName := truncateChars(transaction.PartnerName, 255)
		purpose := transaction.Purpose
		categoryID := matcher.CategoryFor(purpose, partnerName, &transaction.Amount)
		rows = append(rows, model.NewTransaction{
			BookingDate:  transaction.Date,
			Amount:       amount,
			BalanceAfter: balanceAfter,
			PartnerName:  partnerName,
			Purpose:      purpose,
			CategoryID:   categoryID,
			ImportHash:   candidate.Hash,
			CreatedAt:    time.Now().UTC(),
			IsNew:        !isFirstImport,
			TaxOverride:  nil,
		})
	}

	categorized := 0
	for _, row := range rows {
		if row.CategoryID != nil {
			categorized++
		}
	}
	imported, err := transactions.InsertPlanned(ctx, database, accountID, rows)
	if err != nil {
		return ImportResult{}, internalError(err)
	}

	// A remembered deleted booking comes back through no new field: it is not
	// new, so it counts as duplicate.
	duplicates := plan.ExactDuplicates + plan.ProbableDuplicates + plan.PreviouslyDeleted
	return ImportResult{
		Imported:      imported,
		Duplicates:    duplicates,
		Categorized:   categorized,
		InvalidAmount: invalidAmount,
	}, nil
}

func (s *BankingService) storeSnapshots(
	ctx context.Context,
	database *db.Database,
	bank *model.BankAccessItem,
	known []model.Account,
	snapshots []AccountSnapshot,
	historyFrom *civil.Date,
	today civil.Date,
) (SyncReport, error) {
	accountsAdded := 0
	imported := 0
	duplicates := 0
	categorized := 0
	warnings := make([]string, 0)
	notes := make([]string, 0)
	stored := make([]AccountSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.Account.IBAN) != "" {
			stored = append(stored, snapshot)
		}
	}
	now := time.Now().UTC()
	language := s.language

	defaultStart := today.AddDays(-InitialSyncDays)
	if bank.LastSync != nil {
		gapStart := civil.FromTime(*bank.LastSync).AddDays(-SyncOverlapDays)
		if gapStart.Before(defaultStart) &&
			(historyFrom == nil || historyFrom.After(gapStart)) &&
			hasKnownAccount(stored, known) {
			gapEndStart := defaultStart
			if historyFrom != nil && historyFrom.Before(defaultStart) {
				gapEndStart = *historyFrom
			}
			gapEnd := gapEndStart.AddDays(-1)
			notes = append(notes, i18n.Format(
				language,
				i18n.SyncGapNotFetched,
				i18n.Str(formatDateDMY(gapStart)),
				i18n.Str(formatDateDMY(gapEnd)),
				i18n.Int(InitialSyncDays),
			))
		}
	}

	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.Account.IBAN) == "" {
			notes = append(notes, i18n.Format(
				language,
				i18n.SyncAccountWithoutIban,
				i18n.Str(noteSuffix(snapshot.Account)),
			))
		}
	}

	for _, snapshot := range stored {
		normalizedIBAN := iban.Normalize(snapshot.Account.IBAN)
		var existing *model.Account
		for index := range known {
			account := &known[index]
			if account.IBAN != nil && equalASCIIFold(*account.IBAN, normalizedIBAN) {
				existing = account
				break
			}
		}
		accountCurrency := parseCurrency(snapshot.Account.Currency)
		if existing != nil {
			accountCurrency = existing.Currency
		}
		balance := snapshot.Balance

		// Older versions created accounts as EUR when the bank's account data
		// had no currency. If the bank now states the balance in another
		// currency and the account has no EUR booking to lose, correct it
		// instead of noting the balance as belonging elsewhere on every sync.
		if existing != nil && !snapshot.Account.CurrencyStated && balance != nil {
			reported := strings.ToUpper(strings.TrimSpace(balance.Currency))
			if isCurrencyCode(reported) &&
				reported != existing.Currency.String() &&
				existing.Currency == money.EUR {
				currency := existing.Currency
				accountID := existing.ID
				limit := 1
				found, err := transactions.FindItems(
					ctx,
					database,
					&model.TransactionFilter{AccountID: &accountID, Currency: &currency},
					&limit,
				)
				if err != nil {
					return SyncReport{}, internalError(err)
				}
				if len(found) == 0 {
					newCurrency, currencyErr := money.NewCurrency(reported)
					if currencyErr == nil {
						balanceMoney, moneyErr := imports.AmountToMoney(balance.Balance, newCurrency)
						if moneyErr == nil {
							if _, err := accounts.SetCurrency(
								ctx, database, existing.ID, newCurrency, balanceMoney, now,
							); err != nil {
								return SyncReport{}, internalError(err)
							}
							notes = append(notes, i18n.Format(
								language,
								i18n.SyncCurrencyCorrected,
								i18n.Str(tailFour(normalizedIBAN)),
								i18n.Str(existing.Currency.String()),
								i18n.Str(reported),
							))
							accountCurrency = newCurrency
						}
					}
				}
			}
		}

		if existing != nil &&
			existing.AccountType == model.AccountTypeDepot &&
			snapshot.Account.AccountType == model.AccountTypeLoan.AsStr() {
			if _, err := accounts.SetAccountType(ctx, database, existing.ID, model.AccountTypeLoan); err != nil {
				return SyncReport{}, internalError(err)
			}
		}

		balanceMatchesCurrency := balance == nil ||
			equalASCIIFold(balance.Currency, accountCurrency.String())

		var accountID model.AccountID
		if existing != nil {
			accountID = existing.ID
		} else {
			startingBalance := money.Money{Minor: 0, Currency: accountCurrency}
			if balance != nil && equalASCIIFold(balance.Currency, snapshot.Account.Currency) {
				if converted, err := imports.AmountToMoney(balance.Balance, accountCurrency); err == nil {
					startingBalance = converted
				}
			}
			accountType, foundType := model.AccountTypeFromStored(snapshot.Account.AccountType)
			if !foundType {
				accountType = model.AccountTypeChecking
			}
			var accountNumber *string
			if snapshot.Account.AccountNumber != "" {
				value := snapshot.Account.AccountNumber
				accountNumber = &value
			}
			ibanValue := normalizedIBAN
			institution := bank.BankName
			created, err := accounts.Create(ctx, database, model.NewAccount{
				BankConnectionID: &bank.ID,
				Institution:      &institution,
				Name:             nil,
				IBAN:             &ibanValue,
				AccountNumber:    accountNumber,
				Currency:         accountCurrency,
				AccountType:      accountType,
				Balance:          startingBalance,
				BalanceUpdatedAt: nil,
				GroupID:          nil,
			})
			if err != nil {
				return SyncReport{}, internalError(err)
			}
			accountsAdded++
			accountID = created
		}

		if balance != nil {
			if balanceMatchesCurrency {
				if converted, err := imports.AmountToMoney(balance.Balance, accountCurrency); err == nil {
					if err := accounts.SetBalanceForImport(ctx, database, accountID, converted, now); err != nil {
						return SyncReport{}, internalError(err)
					}
				}
			} else {
				notes = append(notes, i18n.Format(
					language,
					i18n.SyncBalanceOtherCurrency,
					i18n.Str(tailFour(normalizedIBAN)),
					i18n.Str(balance.Currency),
					i18n.Str(accountCurrency.String()),
				))
			}
		}

		if snapshot.StatementError != nil {
			warnings = append(warnings, fmt.Sprintf(
				"%s: %s",
				tailFour(normalizedIBAN),
				MaskAccountNumbers(*snapshot.StatementError),
			))
		}

		if balance == nil && (existing == nil || existing.Balance.Minor == 0) {
			key := i18n.BalanceNotProvidedNoCode
			args := []i18n.Arg{i18n.Str(tailFour(normalizedIBAN))}
			if snapshot.BalanceCode != nil {
				key = i18n.BalanceNotProvided
				args = append(args, i18n.Str(*snapshot.BalanceCode))
			}
			notes = append(notes, i18n.Format(language, key, args...))
		}

		result, err := s.ImportTransactions(ctx, database, accountID, snapshot.Transactions)
		if err != nil {
			return SyncReport{}, err
		}
		imported += result.Imported
		duplicates += result.Duplicates
		categorized += result.Categorized

		// Pending (vorgemerkte) lines stay apart from the bookings: fetched
		// means replace all of the account's rows (an empty list clears them),
		// nil keeps what is stored. A failure here never fails the sync.
		if snapshot.Pending != nil {
			items := make([]model.NewPendingBooking, 0, len(snapshot.Pending))
			for _, line := range snapshot.Pending {
				if item, ok := pendingItem(line, accountCurrency); ok {
					items = append(items, item)
				}
			}
			_ = pendingbookings.ReplaceForAccount(ctx, database, accountID, items, now)
		}
	}

	// If statements failed for any account, keep the old sync date so the next
	// sync re-fetches that window.
	if len(warnings) == 0 {
		if _, err := bankconnections.UpdateLastSync(ctx, database, bank.ID); err != nil {
			return SyncReport{}, internalError(err)
		}
		// The chosen history start is one-shot: a fully successful sync clears
		// it, a failure keeps it.
		if historyFrom != nil {
			if _, err := bankconnections.SetSyncFrom(ctx, database, bank.ID, nil); err != nil {
				return SyncReport{}, internalError(err)
			}
		}
	}

	report := SyncReport{
		AccountsSynced: len(stored),
		AccountsAdded:  accountsAdded,
		Imported:       imported,
		Duplicates:     duplicates,
		Categorized:    categorized,
		Warnings:       warnings,
		Notes:          notes,
		HistoryFrom:    historyFrom,
	}
	_ = audit.Log(
		ctx,
		database,
		CodeSyncBank,
		audit.ActorUser,
		fmt.Sprintf(
			"Bank access %d: %d accounts synced, %d imported, %d duplicates, %d categorized",
			int64(bank.ID),
			report.AccountsSynced,
			report.Imported,
			report.Duplicates,
			report.Categorized,
		),
	)
	return report, nil
}

func pendingItem(line FinTsTransaction, currency money.Currency) (model.NewPendingBooking, bool) {
	amount, err := imports.AmountToMoney(line.Amount, currency)
	if err != nil {
		return model.NewPendingBooking{}, false
	}
	valueDate := line.ValueDate
	return model.NewPendingBooking{
		Date:        line.Date,
		ValueDate:   &valueDate,
		Amount:      amount,
		PartnerName: truncateChars(line.PartnerName, 255),
		Purpose:     line.Purpose,
	}, true
}

func parseCurrency(text string) money.Currency {
	currency, err := money.NewCurrency(text)
	if err != nil {
		return money.EUR
	}
	return currency
}

func isCurrencyCode(text string) bool {
	if len(text) != 3 {
		return false
	}
	for index := 0; index < len(text); index++ {
		character := text[index]
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func tailFour(text string) string {
	characters := []rune(text)
	start := len(characters) - 4
	if start < 0 {
		start = 0
	}
	return string(characters[start:])
}

func formatDateDMY(date civil.Date) string {
	return fmt.Sprintf("%02d.%02d.%04d", date.Day(), int(date.Month()), date.Year())
}

// noteSuffix returns at most the last four characters of an account number,
// else the product name or type as the identifier.
func noteSuffix(account FinTsAccount) string {
	if account.AccountNumber != "" {
		return "…" + tailFour(account.AccountNumber)
	}
	if account.ProductName != nil {
		name := strings.TrimSpace(*account.ProductName)
		if name != "" {
			return MaskAccountNumbers(name)
		}
	}
	if account.AccountType != "" {
		return account.AccountType
	}
	return "?"
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

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := 0; index < len(left); index++ {
		a := left[index]
		b := right[index]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func differentStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left != nil || right != nil
	}
	return *left != *right
}

func hasKnownAccount(snapshots []AccountSnapshot, known []model.Account) bool {
	for _, snapshot := range snapshots {
		normalized := iban.Normalize(snapshot.Account.IBAN)
		for _, account := range known {
			if account.IBAN != nil && equalASCIIFold(*account.IBAN, normalized) {
				return true
			}
		}
	}
	return false
}
