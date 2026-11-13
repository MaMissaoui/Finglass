package shell

import (
	"context"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/accountids"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// AccountsDialogState is the Accounts page's dialog state without Slint.
//
// Kind is "" when no dialog is open, otherwise one of "add-bank",
// "add-account", "balance", "rename", "delete-account" or "delete-bank".
type AccountsDialogState struct {
	// Kind is the open dialog, or "".
	Kind string
	// Title is the dialog's heading.
	Title string
	// Text is the dialog's body text.
	Text string
	// ErrorText is the dialog's validation error.
	ErrorText string
	// AccountLabel is the account's identifier in the balance dialog.
	AccountLabel string
	// AccountName is the account name in the add and rename dialogs.
	AccountName string
	// Balance is the balance field's text.
	Balance string
	// AccountType is the account type code.
	AccountType string
	// Offline says whether the add-account dialog is for an offline bank.
	Offline bool
	// BankID is the bank the add-account or delete-bank dialog is about.
	BankID int
	// AccountID is the account the balance, rename or delete dialog is about.
	AccountID int
}

// AccountsState returns a deep copy of the Accounts page state.
func (s *Session) AccountsState() accountsview.AccountsState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accounts.Clone()
}

// AccountsDialog returns a copy of the Accounts page's dialog state.
func (s *Session) AccountsDialog() AccountsDialogState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accountsDialog
}

// AccountsLoad loads the Accounts page, as accounts::spawn_load does.
func (s *Session) AccountsLoad() {
	s.mu.Lock()
	job := s.beginAccountsLoadLocked()
	s.mu.Unlock()
	s.startPageJob(job)
}

// beginAccountsLoadLocked prepares the page's load. The caller holds the
// session mutex and starts the returned job after releasing it.
func (s *Session) beginAccountsLoadLocked() *pageJob {
	if s.db == nil {
		return nil
	}
	// Rust closes an open detail view before it begins the load, so a fresh
	// page never shows a stale account.
	s.accounts.CloseDetail()
	return s.beginAccountsJobLocked(true, func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.LoadAccounts(context.Background(), database)
	})
}

// beginAccountsActionLocked prepares one action on a clone of the state. The
// caller holds the session mutex and starts the returned job after releasing
// it.
func (s *Session) beginAccountsActionLocked(
	work func(*accountsview.AccountsState, *db.Database),
) *pageJob {
	return s.beginAccountsJobLocked(false, work)
}

func (s *Session) beginAccountsJobLocked(
	loading bool,
	work func(*accountsview.AccountsState, *db.Database),
) *pageJob {
	if s.db == nil || s.accountsWork.Active() {
		return nil
	}

	busy := s.accountsWork
	busy.Begin()
	generation, ok := s.beginJobLocked()
	if !ok {
		busy.End()
		return nil
	}

	request := s.nextPageRequestLocked("accounts")
	pending := s.accounts.Clone()
	pending.Language = s.language
	if loading {
		pending.IsLoading = true
	}
	// The published loading/action state is a copy: the worker mutates pending
	// without the session mutex, exactly as the Dashboard's job does.
	published := pending.Clone()
	s.accounts = &published
	database := s.db

	return &pageJob{
		page:       "accounts",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			// Rust's BusyGuard ends the busy section when the worker returns,
			// before the event-loop publish.
			defer busy.End()
			work(&pending, open)
		},
		publish: func() {
			s.accounts = &pending
		},
	}
}

// resetAccountsDialogLocked closes any dialog when a profile is replaced.
func (s *Session) resetAccountsDialogLocked() {
	s.accountsDialog = AccountsDialogState{}
}

// AccountsOpenAddBank opens the add-offline-bank dialog.
func (s *Session) AccountsOpenAddBank() {
	s.mu.Lock()
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{Kind: "add-bank"}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsCloseDialog closes the current dialog and clears its error.
func (s *Session) AccountsCloseDialog() {
	s.mu.Lock()
	s.accountsDialog.ErrorText = ""
	s.accountsDialog.Kind = ""
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsOpenAddAccount opens the add-account dialog for one bank.
func (s *Session) AccountsOpenAddAccount(bankID int) {
	s.mu.Lock()
	bank := s.accountsBankLocked(bankID)
	if bank == nil {
		s.mu.Unlock()
		return
	}
	accountType := "checking"
	if bank.Offline {
		accountType = "credit_card"
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:         "add-account",
		Title:        i18n.Format(s.language, i18n.AddAccountAt, i18n.Str(bank.BankName)),
		AccountType:  accountType,
		Offline:      bank.Offline,
		BankID:       bankID,
		AccountID:    -1,
		Balance:      "0",
		AccountName:  "",
		Text:         "",
		AccountLabel: "",
		ErrorText:    "",
	}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsOpenBalance opens the balance-and-type dialog for one account.
func (s *Session) AccountsOpenBalance(accountID int) {
	s.mu.Lock()
	account := s.accountsAccountLocked(accountID)
	if account == nil {
		s.mu.Unlock()
		return
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:         "balance",
		AccountID:    accountID,
		AccountLabel: accountsAccountIdentifier(*account),
		Balance:      money.FormatPlain(account.Balance),
		AccountType:  account.AccountType,
	}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsOpenRename opens the rename dialog for one account.
func (s *Session) AccountsOpenRename(accountID int) {
	s.mu.Lock()
	account := s.accountsAccountLocked(accountID)
	if account == nil {
		s.mu.Unlock()
		return
	}
	name := ""
	if account.Name != nil {
		name = *account.Name
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:        "rename",
		AccountID:   accountID,
		AccountName: name,
	}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsOpenDeleteAccount opens the delete-account confirmation.
func (s *Session) AccountsOpenDeleteAccount(accountID int) {
	s.mu.Lock()
	account := s.accountsAccountLocked(accountID)
	if account == nil {
		s.mu.Unlock()
		return
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:      "delete-account",
		Title:     i18n.Text(s.language, i18n.DeleteAccountTitle),
		Text:      i18n.Format(s.language, i18n.DeleteAccountText, i18n.Str(accountsAccountIdentifier(*account))),
		AccountID: accountID,
	}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsOpenDeleteBank opens the delete-bank confirmation.
func (s *Session) AccountsOpenDeleteBank(bankID int) {
	s.mu.Lock()
	bank := s.accountsBankLocked(bankID)
	if bank == nil {
		s.mu.Unlock()
		return
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:   "delete-bank",
		Title:  i18n.Format(s.language, i18n.RemoveBankTitle, i18n.Str(bank.BankName)),
		Text:   i18n.Text(s.language, i18n.RemoveBankText),
		BankID: bankID,
	}
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsSubmitAddBank creates the offline bank, or keeps the dialog open with
// the validation message.
func (s *Session) AccountsSubmitAddBank(name string) {
	s.mu.Lock()
	if message := s.accounts.ValidateOfflineBank(name); message != nil {
		s.accountsDialog.ErrorText = *message
		s.mu.Unlock()
		s.notify(Change{Page: "accounts"})
		return
	}
	s.closeAccountsDialogLocked()
	clean := strings.TrimSpace(name)
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.AddOfflineBank(context.Background(), database, clean)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsSubmitAddAccount creates the account, or keeps the dialog open with
// the validation message.
func (s *Session) AccountsSubmitAddAccount(
	bankID int,
	name string,
	identifier string,
	accountType string,
	currencyCode string,
	balanceText string,
) {
	s.mu.Lock()
	offline := false
	if bank := s.accountsBankLocked(bankID); bank != nil {
		offline = bank.Offline
	}
	if message := s.accounts.ValidateAccount(identifier, offline); message != nil {
		s.accountsDialog.ErrorText = *message
		s.mu.Unlock()
		s.notify(Change{Page: "accounts"})
		return
	}

	currency, err := money.NewCurrency(currencyCode)
	if err != nil {
		s.accountsDialog.ErrorText = i18n.Text(s.language, i18n.ErrorUnexpected)
		s.mu.Unlock()
		s.notify(Change{Page: "accounts"})
		return
	}
	balance, err := money.ParseDecimal(balanceText, currency)
	if err != nil {
		s.accountsDialog.ErrorText = i18n.Text(s.language, i18n.BalanceNotANumber)
		s.mu.Unlock()
		s.notify(Change{Page: "accounts"})
		return
	}

	var storedName *string
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		storedName = &trimmed
	}
	s.closeAccountsDialogLocked()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.AddAccount(
			context.Background(),
			database,
			model.BankConnectionID(bankID),
			identifier,
			accountType,
			balance,
			storedName,
		)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsSubmitBalance stores the balance and/or type, or keeps the dialog
// open with the validation message.
func (s *Session) AccountsSubmitBalance(accountID int, balanceText string, accountType string) {
	s.mu.Lock()
	account := s.accountsAccountLocked(accountID)
	if account == nil {
		s.mu.Unlock()
		return
	}
	previousBalance := account.Balance
	previousType := account.AccountType

	balance, err := money.ParseDecimal(balanceText, previousBalance.Currency)
	if err != nil {
		s.accountsDialog.ErrorText = i18n.Text(s.language, i18n.EnterValidBalance)
		s.mu.Unlock()
		s.notify(Change{Page: "accounts"})
		return
	}

	now := s.clock().UTC()
	s.closeAccountsDialogLocked()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		ctx := context.Background()
		if balance != previousBalance {
			_ = pending.UpdateBalance(ctx, database, model.AccountID(accountID), balance, now)
		}
		if accountType != previousType {
			_ = pending.SetAccountType(ctx, database, model.AccountID(accountID), accountType)
		}
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsSubmitRename renames the account; a blank name clears it.
func (s *Session) AccountsSubmitRename(accountID int, name string) {
	s.mu.Lock()
	s.closeAccountsDialogLocked()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.RenameAccount(context.Background(), database, model.AccountID(accountID), name)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsConfirmDeleteAccount confirms deleting an account.
func (s *Session) AccountsConfirmDeleteAccount(accountID int) {
	s.mu.Lock()
	s.closeAccountsDialogLocked()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.DeleteAccount(context.Background(), database, model.AccountID(accountID))
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsConfirmDeleteBank confirms deleting a bank. Offline banks go through
// the Accounts state; online banks belong to the banking service (G8b-2).
func (s *Session) AccountsConfirmDeleteBank(bankID int) {
	s.mu.Lock()
	offline := false
	if bank := s.accountsBankLocked(bankID); bank != nil {
		offline = bank.Offline
	}
	s.closeAccountsDialogLocked()
	var job *pageJob
	if offline {
		job = s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
			_ = pending.DeleteBankAccess(context.Background(), database, model.BankConnectionID(bankID))
		})
	}
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsOpenDetail opens the detail view with FinDesk's default window.
func (s *Session) AccountsOpenDetail(accountID int) {
	today := s.today()
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.OpenDetailDefault(context.Background(), database, model.AccountID(accountID), today)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsSelectDays reopens the detail view for another number of days.
func (s *Session) AccountsSelectDays(accountID int, days int) {
	today := s.today()
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.OpenDetail(
			context.Background(),
			database,
			model.AccountID(accountID),
			int64(days),
			today,
		)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsCloseDetail closes the detail view.
func (s *Session) AccountsCloseDetail() {
	s.mu.Lock()
	s.accounts.CloseDetail()
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsFilterChanged stores the panel's free-text filter.
func (s *Session) AccountsFilterChanged(text string) {
	s.mu.Lock()
	s.accounts.UpdateAccountFilterText(text)
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsDismissMessage clears the error and success banner.
func (s *Session) AccountsDismissMessage() {
	s.mu.Lock()
	s.accounts.ClearMessages()
	s.mu.Unlock()
	s.notify(Change{Page: "accounts"})
}

// AccountsSelect selects an account, group or all accounts in the shared panel.
func (s *Session) AccountsSelect(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok {
		return
	}
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.SelectAccounts(context.Background(), database, selection)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsToggleGroup collapses or expands one panel group.
func (s *Session) AccountsToggleGroup(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok || selection.Kind != accountpanel.SelectionGroup {
		return
	}
	key := selection.Group
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.ToggleCollapsed(context.Background(), database, key)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsMarkRead marks the bookings behind one panel row as read.
func (s *Session) AccountsMarkRead(kind string, groupKind string, id int, accountType string) {
	selection, ok := accountsSelectionFrom(kind, groupKind, id, accountType)
	if !ok {
		return
	}
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.MarkRead(context.Background(), database, selection)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsToggleBank collapses or expands a bank's account rows.
func (s *Session) AccountsToggleBank(bankID int) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.ToggleBankCollapsed(context.Background(), database, model.BankConnectionID(bankID))
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsForgetDeleted forgets the deleted bookings of one account.
func (s *Session) AccountsForgetDeleted(accountID int) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.ForgetDeletedBookings(context.Background(), database, model.AccountID(accountID))
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsNewGroup creates a custom group.
func (s *Session) AccountsNewGroup(name string) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.CreateGroup(context.Background(), database, name)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsRenameGroup renames a custom group.
func (s *Session) AccountsRenameGroup(id int, name string) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.RenameGroup(context.Background(), database, model.AccountGroupID(id), name)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsDeleteGroup deletes a custom group.
func (s *Session) AccountsDeleteGroup(id int) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.DeleteGroup(context.Background(), database, model.AccountGroupID(id))
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsMoveGroup moves a custom group up or down.
func (s *Session) AccountsMoveGroup(id int, up bool) {
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.MoveGroup(context.Background(), database, model.AccountGroupID(id), up)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsMoveAccount moves an account into a custom group, or to its type
// group when groupID is negative.
func (s *Session) AccountsMoveAccount(accountID int, groupID int) {
	var group *model.AccountGroupID
	if groupID >= 0 {
		id := model.AccountGroupID(groupID)
		group = &id
	}
	s.mu.Lock()
	job := s.beginAccountsActionLocked(func(pending *accountsview.AccountsState, database *db.Database) {
		_ = pending.MoveAccountToGroup(context.Background(), database, model.AccountID(accountID), group)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// AccountsOpenTransactions opens Transactions filtered to one account.
func (s *Session) AccountsOpenTransactions(accountID int) {
	id := model.AccountID(accountID)
	s.FollowLink("transactions", PageArgs{
		Transactions: transactionsview.StartArgs{AccountID: &id},
	})
}

func (s *Session) accountsBankLocked(bankID int) *model.BankAccessItem {
	for index := range s.accounts.BankAccesses {
		if int64(s.accounts.BankAccesses[index].ID) == int64(bankID) {
			return &s.accounts.BankAccesses[index]
		}
	}
	return nil
}

func (s *Session) accountsAccountLocked(accountID int) *accountsview.AccountItem {
	for index := range s.accounts.Accounts {
		if int64(s.accounts.Accounts[index].ID) == int64(accountID) {
			return &s.accounts.Accounts[index]
		}
	}
	return nil
}

func (s *Session) closeAccountsDialogLocked() {
	s.accountsDialog.ErrorText = ""
	s.accountsDialog.Kind = ""
}

func accountsSelectionFrom(
	kind string,
	groupKind string,
	id int,
	accountType string,
) (accountpanel.AccountSelection, bool) {
	switch kind {
	case "all":
		return accountpanel.AllSelection(), true
	case "group":
		if groupKind == "custom" {
			return accountpanel.GroupSelection(
				accountgrouping.CustomKey(model.AccountGroupID(id)),
			), true
		}
		return accountpanel.GroupSelection(accountgrouping.TypeKey(accountType)), true
	case "account":
		return accountpanel.AccountSelectionFor(model.AccountID(id)), true
	default:
		return accountpanel.AccountSelection{}, false
	}
}

// accountsAccountIdentifier is Rust's account_identifier: the formatted IBAN,
// or the account number when there is no IBAN.
func accountsAccountIdentifier(account accountsview.AccountItem) string {
	if trimmed := strings.TrimSpace(account.IBAN); trimmed != "" {
		return accountids.Display(trimmed)
	}
	if account.AccountNumber != nil {
		if trimmed := strings.TrimSpace(*account.AccountNumber); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
