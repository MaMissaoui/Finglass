// Package accountsview ports crates/finglass-core/src/app/accounts/: the
// Accounts view's state without any bank login.
//
// This ports the offline part of FinDesk's AccountsViewModel: accounts,
// bank-card groups, selection, collapsed banks and groups, group actions, the
// account form's validation, balance and type changes, renames, deletes,
// "allow deleted bookings again" and the account detail. Everything that needs
// a bank connection is left out.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
package accountsview

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/accountids"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// CollapsedBanksKey is the settings key holding the collapsed bank connection
// ids, as FinDesk stores it.
const CollapsedBanksKey = "accounts.collapsedBanks"

// MaxAccountNameLength is the length of FinDesk's account name column.
const MaxAccountNameLength = 255

// AccountItem is one account as the Accounts page shows it, with the bank name
// and the unread counter.
type AccountItem struct {
	// ID is the row id.
	ID model.AccountID
	// BankAccessID is the bank connection, when this is an online account.
	BankAccessID *model.BankConnectionID
	// BankName is the bank connection's display name; empty when there is
	// none.
	BankName string
	// IBAN is the IBAN or the synthetic card id; empty when the account has
	// neither.
	IBAN string
	// AccountNumber is the account number, when known.
	AccountNumber *string
	// Balance is the latest known balance.
	Balance money.Money
	// Currency is the account currency.
	Currency money.Currency
	// AccountType is the account type text.
	AccountType string
	// LastUpdate is when the balance was last updated.
	LastUpdate *time.Time
	// Name is the user's own label, when set.
	Name *string
	// GroupID is the custom group, when the account sits in one.
	GroupID *model.AccountGroupID
	// NewCount is how many bookings a bank sync brought in that the user has
	// not read yet.
	NewCount int64
}

func (a AccountItem) grouping() accountgrouping.AccountItem {
	return accountgrouping.AccountItem{
		ID:          a.ID,
		Name:        a.Name,
		Currency:    a.Currency,
		Balance:     a.Balance,
		AccountType: a.AccountType,
		GroupID:     a.GroupID,
		NewCount:    a.NewCount,
	}
}

// MatchesFilter reports whether this account matches the panel's free-text
// filter.
func (a AccountItem) MatchesFilter(query string) bool {
	return accountpanel.MatchesFilter(a.Name, a.IBAN, a.BankName, query)
}

// AccountDetail is the detail view: balance history over a window plus recent
// bookings and their period totals.
type AccountDetail struct {
	// Account is the account as it was loaded.
	Account AccountItem
	// Days is the window length in days.
	Days int64
	// History is the end-of-day balance from the first day of the window
	// through today.
	History []balance.BalancePoint
	// Recent holds up to 15 newest bookings inside the window.
	Recent []model.TransactionItem
	// IncomeInPeriod is money in during the window.
	IncomeInPeriod money.Money
	// ExpensesInPeriod is money out during the window, as a positive amount.
	ExpensesInPeriod money.Money
}

// AccountsState is the Accounts page state.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
type AccountsState struct {
	// Accounts holds every account, in repository id order.
	Accounts []AccountItem
	// BankAccesses holds every bank connection.
	BankAccesses []model.BankAccessItem
	// IsLoading says whether a load is running.
	IsLoading bool
	// Detail is the open detail view, if any.
	Detail *AccountDetail
	// Error is the current error text.
	Error *string
	// Message is the current success text.
	Message *string
	// PrimaryCurrency is the profile's primary currency.
	PrimaryCurrency money.Currency
	// CollapsedBanks holds bank ids whose account rows are collapsed.
	CollapsedBanks map[model.BankConnectionID]struct{}
	// Selection is what the account panel filters by; shared with
	// Transactions through the stored selection.
	Selection accountpanel.AccountSelection
	// Groups holds the account panel's groups, in display order.
	Groups []accountgrouping.AccountGroupView
	// GrandTotal is the grand total of every account, per currency, primary
	// first.
	GrandTotal []money.CurrencyTotal
	// Collapsed holds the ids of the collapsed panel groups.
	Collapsed map[string]struct{}
	// AccountFilterText is the free-text filter over the panel's account rows.
	AccountFilterText string
	// CustomGroups holds the custom groups as loaded, for rename, move and
	// delete.
	CustomGroups []model.AccountGroup
	// Language is the language of the state's user-visible texts.
	Language i18n.Language
	// Busy is the profile's busy flag; the app replaces it with its shared one
	// (see Busy).
	Busy Busy
	// panelLoaded says whether the panel's settings were read already.
	panelLoaded bool
	// collapsedBanksLoaded says whether the collapsed bank ids were read
	// already.
	collapsedBanksLoaded bool
}

// New builds empty state for language.
func New(language i18n.Language) *AccountsState {
	return &AccountsState{
		Accounts:             make([]AccountItem, 0),
		BankAccesses:         make([]model.BankAccessItem, 0),
		PrimaryCurrency:      money.EUR,
		CollapsedBanks:       make(map[model.BankConnectionID]struct{}),
		Selection:            accountpanel.AllSelection(),
		Groups:               make([]accountgrouping.AccountGroupView, 0),
		GrandTotal:           make([]money.CurrencyTotal, 0),
		Collapsed:            make(map[string]struct{}),
		CustomGroups:         make([]model.AccountGroup, 0),
		Language:             language,
		Busy:                 NewBusy(),
		panelLoaded:          false,
		collapsedBanksLoaded: false,
	}
}

// Default builds empty state in English, like Rust's AccountsState::default.
func Default() *AccountsState {
	return New(i18n.En)
}

// Totals returns balances per currency, primary first; different currencies
// are never added up.
func (s *AccountsState) Totals() []money.CurrencyTotal {
	return money.SumByCurrency(
		s.Accounts,
		s.PrimaryCurrency,
		func(account AccountItem) money.Currency { return account.Currency },
		func(account AccountItem) int64 { return account.Balance.Minor },
	)
}

// FilteredAccounts returns the account rows the free-text filter leaves
// visible.
func (s *AccountsState) FilteredAccounts() []*AccountItem {
	filtered := make([]*AccountItem, 0, len(s.Accounts))
	for index := range s.Accounts {
		if s.Accounts[index].MatchesFilter(s.AccountFilterText) {
			filtered = append(filtered, &s.Accounts[index])
		}
	}
	return filtered
}

// LoadAccounts loads the accounts, groups and totals, like FinDesk's
// loadAccounts.
func (s *AccountsState) LoadAccounts(ctx context.Context, d *db.Database) error {
	s.IsLoading = true
	s.Error = nil
	if err := s.Refresh(ctx, d); err != nil {
		s.recordError(err)
		return err
	}
	return nil
}

// Refresh reloads the repository data. The first load reads the panel
// settings; later loads keep the in-memory values.
func (s *AccountsState) Refresh(ctx context.Context, d *db.Database) error {
	loadedAccounts, err := accounts.List(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	sort.SliceStable(loadedAccounts, func(left, right int) bool {
		return loadedAccounts[left].ID < loadedAccounts[right].ID
	})

	bankAccesses, err := bankconnections.ListItems(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}

	customGroups, err := accountgroups.List(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	sort.SliceStable(customGroups, func(left, right int) bool {
		if customGroups[left].SortOrder != customGroups[right].SortOrder {
			return customGroups[left].SortOrder < customGroups[right].SortOrder
		}
		return customGroups[left].ID < customGroups[right].ID
	})

	primary, err := money.Primary(ctx, d)
	if err != nil {
		return accountpanel.DBError(err)
	}
	newCounts, err := transactions.CountNew(ctx, d)
	if err != nil {
		return accountpanel.RepoError(err)
	}

	bankNames := make(map[model.BankConnectionID]string, len(bankAccesses))
	for _, bank := range bankAccesses {
		bankNames[bank.ID] = bank.BankName
	}

	items := make([]AccountItem, 0, len(loadedAccounts))
	for _, account := range loadedAccounts {
		bankName := ""
		if account.BankConnectionID != nil {
			bankName = bankNames[*account.BankConnectionID]
		}
		ibanText := ""
		if account.IBAN != nil {
			ibanText = *account.IBAN
		}
		items = append(items, AccountItem{
			ID:            account.ID,
			BankAccessID:  account.BankConnectionID,
			BankName:      bankName,
			IBAN:          ibanText,
			AccountNumber: account.AccountNumber,
			Balance:       account.Balance,
			Currency:      account.Currency,
			AccountType:   account.AccountType.AsStr(),
			LastUpdate:    account.BalanceUpdatedAt,
			Name:          account.Name,
			GroupID:       account.GroupID,
			NewCount:      newCounts[account.ID],
		})
	}

	groupingAccounts := make([]accountgrouping.AccountItem, 0, len(items))
	for _, account := range items {
		groupingAccounts = append(groupingAccounts, account.grouping())
	}
	groups := accountgrouping.Group(groupingAccounts, customGroups, primary)
	grandTotal := money.SumByCurrency(
		items,
		primary,
		func(account AccountItem) money.Currency { return account.Currency },
		func(account AccountItem) int64 { return account.Balance.Minor },
	)

	firstPanelLoad := !s.panelLoaded
	s.panelLoaded = true
	candidate := s.Selection
	if firstPanelLoad {
		candidate, err = accountpanel.LoadSelection(ctx, d, groups, groupingAccounts)
		if err != nil {
			return err
		}
	}
	selection := accountpanel.Resolve(candidate, groups, groupingAccounts)

	var loadedCollapsed map[string]struct{}
	if firstPanelLoad {
		loadedCollapsed, err = accountpanel.LoadCollapsed(ctx, d)
		if err != nil {
			return err
		}
	}

	var loadedCollapsedBanks map[model.BankConnectionID]struct{}
	if !s.collapsedBanksLoaded {
		s.collapsedBanksLoaded = true
		loadedCollapsedBanks, err = loadCollapsedBanks(ctx, d, bankAccesses)
		if err != nil {
			return err
		}
	}

	s.Accounts = items
	s.BankAccesses = bankAccesses
	s.CustomGroups = customGroups
	s.Groups = groups
	s.GrandTotal = grandTotal
	s.Selection = selection
	if loadedCollapsed != nil {
		s.Collapsed = loadedCollapsed
	}
	if loadedCollapsedBanks != nil {
		s.CollapsedBanks = loadedCollapsedBanks
	}
	s.PrimaryCurrency = primary
	s.IsLoading = false
	return nil
}

// ToggleBankCollapsed collapses or expands a bank connection's account rows;
// view state only, persisted but not audited.
func (s *AccountsState) ToggleBankCollapsed(
	ctx context.Context,
	d *db.Database,
	bankAccessID model.BankConnectionID,
) error {
	if _, collapsed := s.CollapsedBanks[bankAccessID]; collapsed {
		delete(s.CollapsedBanks, bankAccessID)
	} else {
		if s.CollapsedBanks == nil {
			s.CollapsedBanks = make(map[model.BankConnectionID]struct{})
		}
		s.CollapsedBanks[bankAccessID] = struct{}{}
	}
	return s.storeCollapsedBanks(ctx, d)
}

// SelectAccounts selects an account, group or all accounts and stores it for
// both screens. Never marks bookings read.
func (s *AccountsState) SelectAccounts(
	ctx context.Context,
	d *db.Database,
	selection accountpanel.AccountSelection,
) error {
	s.Selection = selection
	if err := accountpanel.SaveSelection(ctx, d, s.Selection); err != nil {
		s.recordError(err)
		return err
	}
	return nil
}

// ToggleCollapsed collapses or expands a panel group; view state only,
// persisted but not audited.
func (s *AccountsState) ToggleCollapsed(
	ctx context.Context,
	d *db.Database,
	key accountgrouping.GroupKey,
) error {
	id := key.ID()
	if _, collapsed := s.Collapsed[id]; collapsed {
		delete(s.Collapsed, id)
	} else {
		if s.Collapsed == nil {
			s.Collapsed = make(map[string]struct{})
		}
		s.Collapsed[id] = struct{}{}
	}
	if err := accountpanel.SaveCollapsed(ctx, d, s.Collapsed); err != nil {
		s.recordError(err)
		return err
	}
	return nil
}

// UpdateAccountFilterText filters the panel's account rows by name, identifier
// or bank.
func (s *AccountsState) UpdateAccountFilterText(text string) {
	s.AccountFilterText = text
}

// CreateGroup creates a custom group and reloads the state.
func (s *AccountsState) CreateGroup(ctx context.Context, d *db.Database, name string) error {
	actions := accountpanel.NewAccountGroupActions()
	if _, err := actions.Create(ctx, d, name); err != nil {
		return s.actionError(err)
	}
	return s.Refresh(ctx, d)
}

// RenameGroup renames a custom group and reloads the state.
func (s *AccountsState) RenameGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	name string,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.Rename(ctx, d, id, name); err != nil {
		return s.actionError(err)
	}
	return s.Refresh(ctx, d)
}

// DeleteGroup deletes a group, resets the selection to All when it was
// selected, and reloads the state.
func (s *AccountsState) DeleteGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
) error {
	actions := accountpanel.NewAccountGroupActions()
	wasSelected, err := actions.Delete(ctx, d, id, s.Selection)
	if err != nil {
		return s.actionError(err)
	}
	if wasSelected {
		s.Selection = accountpanel.AllSelection()
	}
	return s.Refresh(ctx, d)
}

// MoveGroup moves a group up or down and reloads the state.
func (s *AccountsState) MoveGroup(
	ctx context.Context,
	d *db.Database,
	id model.AccountGroupID,
	up bool,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if _, err := actions.MoveUpOrDown(ctx, d, id, up); err != nil {
		return s.actionError(err)
	}
	return s.Refresh(ctx, d)
}

// MoveAccountToGroup moves an account into a custom group, or back to its type
// group with nil.
func (s *AccountsState) MoveAccountToGroup(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	groupID *model.AccountGroupID,
) error {
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.MoveAccount(ctx, d, accountID, groupID); err != nil {
		return s.actionError(err)
	}
	return s.Refresh(ctx, d)
}

// MarkRead is "Mark as read" from the panel's context menu; the repository
// records the change and the state reloads.
func (s *AccountsState) MarkRead(
	ctx context.Context,
	d *db.Database,
	selection accountpanel.AccountSelection,
) error {
	groups := s.Groups
	actions := accountpanel.NewAccountGroupActions()
	if err := actions.MarkRead(ctx, d, selection, groups); err != nil {
		return s.actionError(err)
	}
	return s.Refresh(ctx, d)
}

// ValidateOfflineBank validates an offline bank's name; the only field an
// offline bank has.
func (s *AccountsState) ValidateOfflineBank(name string) *string {
	if strings.TrimSpace(name) == "" {
		message := i18n.Text(s.Language, i18n.BankNameRequired)
		return &message
	}
	return nil
}

// AddOfflineBank adds a bank that never goes online.
func (s *AccountsState) AddOfflineBank(ctx context.Context, d *db.Database, name string) error {
	if message := s.ValidateOfflineBank(name); message != nil {
		s.Error = message
		return nil
	}
	clean := strings.TrimSpace(name)
	if _, err := bankconnections.CreateOffline(ctx, d, clean); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}
	message := i18n.Format(s.Language, i18n.MessageAdded, i18n.Str(clean))
	s.Message = &message
	s.Error = nil
	return nil
}

// ValidateAccount validates an account identifier.
//
// For an online bank a valid IBAN is required. For an offline bank the
// identifier may also be the card's last 4 digits; more than 4 digits (a
// pasted card number) is refused, never truncated.
func (s *AccountsState) ValidateAccount(identifier string, offline bool) *string {
	normalized := iban.Normalize(identifier)
	duplicate := func(id string) bool {
		for _, account := range s.Accounts {
			if account.IBAN == id {
				return true
			}
		}
		return false
	}

	if iban.IsValid(normalized) {
		if duplicate(normalized) {
			message := i18n.Text(s.Language, i18n.AccountIbanExists)
			return &message
		}
		return nil
	}

	if !offline {
		if normalized == "" {
			message := i18n.Text(s.Language, i18n.IbanRequired)
			return &message
		}
		message := i18n.Text(s.Language, i18n.IbanChecksumFailed)
		return &message
	}

	digits := strings.TrimSpace(identifier)
	if normalized == "" {
		message := i18n.Text(s.Language, i18n.IbanRequired)
		return &message
	}

	runes := []rune(digits)
	allDigits := true
	for _, character := range runes {
		if character < '0' || character > '9' {
			allDigits = false
			break
		}
	}
	if len(runes) == 4 && allDigits {
		if duplicate(accountids.Card(digits)) {
			message := i18n.Text(s.Language, i18n.AccountIbanExists)
			return &message
		}
		return nil
	}
	message := i18n.Text(s.Language, i18n.CardLast4Only)
	return &message
}

// AddAccount creates an account; name is an optional display name, already
// trimmed and capped by the repository column.
func (s *AccountsState) AddAccount(
	ctx context.Context,
	d *db.Database,
	bankAccessID model.BankConnectionID,
	identifier string,
	accountType string,
	balance money.Money,
	name *string,
) error {
	offline := false
	for _, bank := range s.BankAccesses {
		if bank.ID == bankAccessID {
			offline = bank.Offline
			break
		}
	}
	if message := s.ValidateAccount(identifier, offline); message != nil {
		s.Error = message
		return nil
	}

	normalized := iban.Normalize(identifier)
	id := normalized
	if offline && !iban.IsValid(normalized) {
		id = accountids.Card(identifier)
	}

	storedType, ok := model.AccountTypeFromStored(accountType)
	if !ok {
		message := i18n.Text(s.Language, i18n.ErrorAccountTypeInvalid)
		s.Error = &message
		return nil
	}

	if _, err := accounts.Create(ctx, d, model.NewAccount{
		BankConnectionID: &bankAccessID,
		Institution:      nil,
		Name:             normalizeAccountName(name),
		IBAN:             &id,
		AccountNumber:    nil,
		Currency:         balance.Currency,
		AccountType:      storedType,
		Balance:          balance,
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	}); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}
	message := i18n.Text(s.Language, i18n.MessageAccountAdded)
	s.Message = &message
	s.Error = nil
	return nil
}

// UpdateBalance updates an account's balance and its update time.
func (s *AccountsState) UpdateBalance(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	balance money.Money,
	now time.Time,
) error {
	if err := accounts.SetBalance(ctx, d, accountID, balance, now); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	return s.Refresh(ctx, d)
}

// SetAccountType changes an account's type by hand; only one of
// accountgrouping.AccountTypes is accepted.
func (s *AccountsState) SetAccountType(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	accountType string,
) error {
	known := false
	for _, candidate := range accountgrouping.AccountTypes {
		if candidate == accountType {
			known = true
			break
		}
	}
	if !known {
		message := i18n.Text(s.Language, i18n.ErrorAccountTypeInvalid)
		s.Error = &message
		return nil
	}

	found := false
	for _, account := range s.Accounts {
		if account.ID == accountID {
			found = true
			break
		}
	}
	if !found {
		message := i18n.Text(s.Language, i18n.ErrorAccountNotFound)
		s.Error = &message
		return nil
	}

	storedType, ok := model.AccountTypeFromStored(accountType)
	if !ok {
		message := i18n.Text(s.Language, i18n.ErrorAccountTypeInvalid)
		s.Error = &message
		return nil
	}

	if _, err := accounts.SetAccountType(ctx, d, accountID, storedType); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	return s.Refresh(ctx, d)
}

// RenameAccount sets the account's display name (blank clears it). Only a
// change to the stored name is audit-logged.
func (s *AccountsState) RenameAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	name string,
) error {
	stored, ok, err := accounts.Get(ctx, d, accountID)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	var previous *string
	if ok {
		previous = stored.Name
	}
	normalized := normalizeAccountName(&name)
	if !equalOptionalString(normalized, previous) {
		if err := accounts.Rename(ctx, d, accountID, normalized); err != nil {
			return accountpanel.RepoError(err)
		}
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}

	if s.Detail != nil {
		for _, account := range s.Accounts {
			if account.ID == accountID {
				s.Detail.Account = account
				break
			}
		}
	}
	return nil
}

// DeleteAccount deletes an account and its bookings, unless work that may
// still insert rows is running.
func (s *AccountsState) DeleteAccount(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
) error {
	if s.Busy.Active() {
		message := i18n.Text(s.Language, i18n.ErrorDeleteWhileBusy)
		s.Error = &message
		return nil
	}
	if err := accounts.Delete(ctx, d, accountID); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}
	message := i18n.Text(s.Language, i18n.MessageAccountDeleted)
	s.Message = &message
	s.Error = nil
	return nil
}

// ForgetDeletedBookings forgets the bookings of accountID remembered as
// deleted, so a later import may bring them back.
func (s *AccountsState) ForgetDeletedBookings(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
) error {
	count, err := transactions.ForgetDeletedBookings(ctx, d, accountID)
	if err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}
	if count > 0 {
		message := i18n.Format(s.Language, i18n.MessageDeletedForgotten, i18n.Int(int64(count)))
		s.Message = &message
	} else {
		message := i18n.Text(s.Language, i18n.MessageNoDeletedRemembered)
		s.Message = &message
	}
	s.Error = nil
	return nil
}

// DeleteBankAccess deletes an offline bank and its accounts. Online
// connections belong to the banking service, which also forgets its PIN.
func (s *AccountsState) DeleteBankAccess(
	ctx context.Context,
	d *db.Database,
	bankAccessID model.BankConnectionID,
) error {
	var bank *model.BankAccessItem
	for index := range s.BankAccesses {
		if s.BankAccesses[index].ID == bankAccessID {
			bank = &s.BankAccesses[index]
			break
		}
	}
	if bank == nil {
		return nil
	}
	if !bank.Offline {
		return nil
	}
	if s.Busy.Active() {
		message := i18n.Text(s.Language, i18n.ErrorDeleteWhileBusy)
		s.Error = &message
		return nil
	}

	if _, err := bankconnections.DeleteWithAccounts(ctx, d, bankAccessID); err != nil {
		return s.actionError(accountpanel.RepoError(err))
	}
	if err := s.Refresh(ctx, d); err != nil {
		return err
	}
	message := i18n.Text(s.Language, i18n.MessageBankRemoved)
	s.Message = &message
	s.Error = nil
	return nil
}

// OpenDetail opens the detail view: balance history over days days plus recent
// bookings.
func (s *AccountsState) OpenDetail(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	days int64,
	today civil.Date,
) error {
	var account *AccountItem
	for index := range s.Accounts {
		if s.Accounts[index].ID == accountID {
			account = &s.Accounts[index]
			break
		}
	}
	if account == nil {
		return nil
	}

	from := today.AddDays(int(-(days - 1)))
	filter := &model.TransactionFilter{
		StartDate: &from,
		AccountID: &accountID,
	}
	loaded, err := transactions.FindItems(ctx, d, filter, nil)
	if err != nil {
		return accountpanel.RepoError(err)
	}
	history := balance.ComputeHistory(account.Balance, loaded, from, today)

	inWindow := make([]model.TransactionItem, 0, len(loaded))
	for _, transaction := range loaded {
		if !transaction.Date.After(today) {
			inWindow = append(inWindow, transaction)
		}
	}

	currency := account.Balance.Currency
	income := int64(0)
	expenses := int64(0)
	for _, transaction := range inWindow {
		if transaction.Amount.Minor > 0 {
			income += transaction.Amount.Minor
		}
		if transaction.Amount.Minor < 0 {
			expenses += absInt64(transaction.Amount.Minor)
		}
	}

	recent := inWindow
	if len(recent) > 15 {
		recent = recent[:15]
	}
	copied := *account
	s.Detail = &AccountDetail{
		Account:          copied,
		Days:             days,
		History:          history,
		Recent:           recent,
		IncomeInPeriod:   money.Money{Minor: income, Currency: currency},
		ExpensesInPeriod: money.Money{Minor: expenses, Currency: currency},
	}
	return nil
}

// OpenDetailDefault opens the detail view with FinDesk's default window of 90
// days.
func (s *AccountsState) OpenDetailDefault(
	ctx context.Context,
	d *db.Database,
	accountID model.AccountID,
	today civil.Date,
) error {
	return s.OpenDetail(ctx, d, accountID, 90, today)
}

// CloseDetail closes the detail view.
func (s *AccountsState) CloseDetail() {
	s.Detail = nil
}

// ClearMessages clears the error and success message.
func (s *AccountsState) ClearMessages() {
	s.Error = nil
	s.Message = nil
}

func (s *AccountsState) storeCollapsedBanks(ctx context.Context, d *db.Database) error {
	ids := make([]int64, 0, len(s.CollapsedBanks))
	for id := range s.CollapsedBanks {
		ids = append(ids, int64(id))
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}
	if err := settings.Set(ctx, d, CollapsedBanksKey, strings.Join(values, ",")); err != nil {
		appError := accountpanel.DBError(err)
		s.recordError(appError)
		return appError
	}
	return nil
}

func (s *AccountsState) actionError(err error) error {
	var appError *accountpanel.AppError
	if errors.As(err, &appError) {
		if key, ok := appError.Message(); ok {
			message := i18n.Text(s.Language, key)
			s.Error = &message
			return nil
		}
	}
	s.recordError(err)
	return err
}

func (s *AccountsState) recordError(err error) {
	s.IsLoading = false
	key := i18n.ErrorUnexpected
	var appError *accountpanel.AppError
	if errors.As(err, &appError) {
		if messageKey, ok := appError.Message(); ok {
			key = messageKey
		}
	}
	message := i18n.Text(s.Language, key)
	s.Error = &message
}

func loadCollapsedBanks(
	ctx context.Context,
	d *db.Database,
	bankAccesses []model.BankAccessItem,
) (map[model.BankConnectionID]struct{}, error) {
	raw, ok, err := settings.Get(ctx, d, CollapsedBanksKey)
	if err != nil {
		return nil, accountpanel.DBError(err)
	}
	collapsed := make(map[model.BankConnectionID]struct{})
	if !ok {
		return collapsed, nil
	}

	known := make(map[model.BankConnectionID]struct{}, len(bankAccesses))
	for _, bank := range bankAccesses {
		known[bank.ID] = struct{}{}
	}
	for _, value := range strings.Split(raw, ",") {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		id := model.BankConnectionID(parsed)
		if _, ok := known[id]; ok {
			collapsed[id] = struct{}{}
		}
	}
	return collapsed, nil
}

func normalizeAccountName(name *string) *string {
	if name == nil {
		return nil
	}
	clean := []rune(strings.TrimSpace(*name))
	if len(clean) == 0 {
		return nil
	}
	if len(clean) > MaxAccountNameLength {
		clean = clean[:MaxAccountNameLength]
	}
	value := string(clean)
	return &value
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// AccountTypeCodes is re-exported for callers that only need the account-type
// list.
var AccountTypeCodes = accountgrouping.AccountTypes
