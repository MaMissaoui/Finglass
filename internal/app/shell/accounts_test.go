package shell

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type accountsSeed struct {
	bank     model.BankConnectionID
	checking model.AccountID
	savings  model.AccountID
	loan     model.AccountID
	group    model.AccountGroupID
}

func seedAccountsDatabase(t *testing.T, appPaths paths.AppPaths) accountsSeed {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	bank, err := bankconnections.CreateOffline(ctx, database, "Bank Norwegian")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	group, err := accountgroups.Create(ctx, database, model.NewAccountGroup{Name: "Reisen", SortOrder: 1})
	if err != nil {
		t.Fatalf("create account group: %v", err)
	}

	checkingName := "Girokonto"
	checking, err := accounts.Create(ctx, database, model.NewAccount{
		BankConnectionID: &bank.ID,
		Name:             &checkingName,
		IBAN:             stringPointer("DE89370400440532013000"),
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create checking account: %v", err)
	}
	savings, err := accounts.Create(ctx, database, model.NewAccount{
		BankConnectionID: &bank.ID,
		IBAN:             stringPointer("DE44500105175407324931"),
		Currency:         money.EUR,
		AccountType:      model.AccountTypeSavings,
		Balance:          money.Money{Minor: 25_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create savings account: %v", err)
	}
	loan, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE02120300000000202051"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeLoan,
		Balance:     money.Money{Minor: -9_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create loan account: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, isNew bool) {
		name := partner
		if _, err := transactions.InsertPlanned(ctx, database, checking, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &name,
			ImportHash:  fmt.Sprintf("%s-%d-%s", day.String(), minor, partner),
			CreatedAt:   testNow.UTC(),
			IsNew:       isNew,
		}}); err != nil {
			t.Fatalf("insert transaction %s: %v", partner, err)
		}
	}
	insert(testToday().AddDays(-2), 2_000, "Employer", false)
	insert(testToday().AddDays(-1), -500, "Bakery", true)

	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return accountsSeed{
		bank:     bank.ID,
		checking: checking,
		savings:  savings,
		loan:     loan,
		group:    group,
	}
}

func waitForAccounts(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(accountsview.AccountsState) bool,
) accountsview.AccountsState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "accounts" {
				continue
			}
			state := session.AccountsState()
			if predicate(state) {
				return state
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedAccountsSession(t *testing.T) (*Session, accountsSeed) {
	t.Helper()
	appPaths := testPaths(t)
	seed := seedAccountsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	return session, seed
}

func accountsAuditActions(t *testing.T, session *Session) []string {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Action)
	}
	return names
}

func countAccountsAudit(t *testing.T, session *Session, action string) int {
	t.Helper()
	count := 0
	for _, name := range accountsAuditActions(t, session) {
		if name == action {
			count++
		}
	}
	return count
}

func requireAccountsMessage(t *testing.T, got *string, key i18n.Key) {
	t.Helper()
	want := i18n.Text(i18n.En, key)
	if got == nil || *got != want {
		t.Fatalf("message = %v, want %q", got, want)
	}
}

func TestOpeningAccountsLoadsRowsGroupsCardsAndTotals(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.Navigate("accounts")
	if screen := session.CurrentScreen(); screen != "accounts" {
		t.Fatalf("screen = %q, want accounts", screen)
	}
	state := waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	if len(state.BankAccesses) != 1 || state.BankAccesses[0].ID != seed.bank {
		t.Fatalf("banks = %+v", state.BankAccesses)
	}
	if len(state.Groups) != 4 {
		t.Fatalf("groups = %d, want 4 (checking, savings, loan, custom)", len(state.Groups))
	}
	if !reflect.DeepEqual(state.GrandTotal, []money.CurrencyTotal{{
		Currency: money.EUR,
		Total:    116_000,
	}}) {
		t.Fatalf("grand total = %+v", state.GrandTotal)
	}
	if state.Error != nil {
		t.Fatalf("error = %q", *state.Error)
	}
	if state.Detail != nil {
		t.Fatal("a fresh load kept a detail view")
	}
}

func TestAccountsDialogsValidateCreateAndAuditLikeAccountsview(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})
	createBankBefore := countAccountsAudit(t, session, "bank_connection.create_offline")
	createAccountBefore := countAccountsAudit(t, session, "account.create")

	session.AccountsOpenAddBank()
	if dialog := session.AccountsDialog(); dialog.Kind != "add-bank" {
		t.Fatalf("dialog = %+v", dialog)
	}
	session.AccountsSubmitAddBank("   ")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.BankNameRequired) {
		t.Fatalf("dialog error = %q", dialog.ErrorText)
	}

	session.AccountsSubmitAddBank("New Bank")
	state := waitForAccounts(t, session, events, "the added bank", func(state accountsview.AccountsState) bool {
		return len(state.BankAccesses) == 2
	})
	if state.Message == nil || *state.Message != i18n.Format(i18n.En, i18n.MessageAdded, i18n.Str("New Bank")) {
		t.Fatalf("message = %v", state.Message)
	}
	if dialog := session.AccountsDialog(); dialog.Kind != "" {
		t.Fatalf("dialog stayed open: %+v", dialog)
	}
	if got := countAccountsAudit(t, session, "bank_connection.create_offline") - createBankBefore; got != 1 {
		t.Fatalf("create-offline audit entries = %d, want 1", got)
	}

	session.AccountsOpenAddAccount(int(seed.bank))
	dialog := session.AccountsDialog()
	if dialog.Kind != "add-account" || !dialog.Offline || dialog.AccountType != "credit_card" {
		t.Fatalf("add-account dialog = %+v", dialog)
	}
	session.AccountsSubmitAddAccount(int(seed.bank), "", "1234567812345678", "credit_card", "EUR", "10")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.CardLast4Only) {
		t.Fatalf("dialog error = %q", dialog.ErrorText)
	}

	session.AccountsOpenAddAccount(int(seed.bank))
	session.AccountsSubmitAddAccount(int(seed.bank), "", "9999", "credit_card", "EUR", "not a number")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.BalanceNotANumber) {
		t.Fatalf("balance dialog error = %q", dialog.ErrorText)
	}

	session.AccountsSubmitAddAccount(int(seed.bank), "Card", "4321", "credit_card", "EUR", "10,50")
	state = waitForAccounts(t, session, events, "the added account", func(state accountsview.AccountsState) bool {
		return len(state.Accounts) == 4
	})
	if got := countAccountsAudit(t, session, "account.create") - createAccountBefore; got != 1 {
		t.Fatalf("account.create audit entries = %d, want 1", got)
	}
	var card *accountsview.AccountItem
	for index := range state.Accounts {
		if state.Accounts[index].IBAN == "CARD4321" {
			card = &state.Accounts[index]
		}
	}
	if card == nil {
		t.Fatalf("the card account is missing: %+v", state.Accounts)
	}
	if card.Name == nil || *card.Name != "Card" || card.Balance != (money.Money{Minor: 1_050, Currency: money.EUR}) {
		t.Fatalf("card account = %+v", *card)
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessageAccountAdded) {
		t.Fatalf("message = %v", state.Message)
	}

	session.AccountsOpenAddAccount(int(seed.bank))
	session.AccountsSubmitAddAccount(int(seed.bank), "", "4321", "credit_card", "EUR", "1")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.AccountIbanExists) {
		t.Fatalf("duplicate dialog error = %q", dialog.ErrorText)
	}
}

func TestAccountsCloseDialogClearsTheValidationError(t *testing.T) {
	session, _ := newLoadedAccountsSession(t)

	session.AccountsOpenAddBank()
	session.AccountsSubmitAddBank("")
	if dialog := session.AccountsDialog(); dialog.ErrorText == "" {
		t.Fatal("the blank bank name was accepted")
	}
	session.AccountsCloseDialog()
	if dialog := session.AccountsDialog(); dialog.Kind != "" || dialog.ErrorText != "" {
		t.Fatalf("dialog after close = %+v", dialog)
	}
}

func TestAccountsBalanceRenameDeleteAndBankActionsChangeTheState(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	session.AccountsOpenBalance(int(seed.checking))
	dialog := session.AccountsDialog()
	if dialog.Kind != "balance" || dialog.AccountType != "checking" || dialog.AccountLabel != "DE89 3704 0044 0532 0130 00" {
		t.Fatalf("balance dialog = %+v", dialog)
	}
	session.AccountsSubmitBalance(int(seed.checking), "not a number", "checking")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.EnterValidBalance) {
		t.Fatalf("balance dialog error = %q", dialog.ErrorText)
	}

	session.AccountsSubmitBalance(int(seed.checking), "2.000,00", "savings")
	state := waitForAccounts(t, session, events, "the changed balance and type", func(state accountsview.AccountsState) bool {
		for _, account := range state.Accounts {
			if account.ID == seed.checking {
				return account.Balance.Minor == 200_000 && account.AccountType == "savings"
			}
		}
		return false
	})
	_ = state
	if got := countAccountsAudit(t, session, "account.set_balance"); got != 1 {
		t.Fatalf("set_balance audit entries = %d, want 1", got)
	}
	if got := countAccountsAudit(t, session, "CHANGE_ACCOUNT_TYPE"); got != 1 {
		t.Fatalf("change-type audit entries = %d, want 1", got)
	}

	session.AccountsOpenRename(int(seed.checking))
	session.AccountsSubmitRename(int(seed.checking), "Haushalt")
	waitForAccounts(t, session, events, "the renamed account", func(state accountsview.AccountsState) bool {
		for _, account := range state.Accounts {
			if account.ID == seed.checking {
				return account.Name != nil && *account.Name == "Haushalt"
			}
		}
		return false
	})
	if got := countAccountsAudit(t, session, "account.rename"); got != 1 {
		t.Fatalf("rename audit entries = %d, want 1", got)
	}

	session.AccountsOpenDeleteAccount(int(seed.savings))
	if dialog := session.AccountsDialog(); dialog.Kind != "delete-account" ||
		dialog.Text != i18n.Format(i18n.En, i18n.DeleteAccountText, i18n.Str("DE44 5001 0517 5407 3249 31")) {
		t.Fatalf("delete-account dialog = %+v", dialog)
	}
	session.AccountsConfirmDeleteAccount(int(seed.savings))
	waitForAccounts(t, session, events, "the deleted account", func(state accountsview.AccountsState) bool {
		return len(state.Accounts) == 2
	})
	if got := countAccountsAudit(t, session, "account.delete"); got != 1 {
		t.Fatalf("delete audit entries = %d, want 1", got)
	}

	session.AccountsOpenDeleteBank(int(seed.bank))
	if dialog := session.AccountsDialog(); dialog.Kind != "delete-bank" {
		t.Fatalf("delete-bank dialog = %+v", dialog)
	}
	session.AccountsConfirmDeleteBank(int(seed.bank))
	waitForAccounts(t, session, events, "the deleted bank", func(state accountsview.AccountsState) bool {
		return len(state.BankAccesses) == 0 && len(state.Accounts) == 1
	})
	if got := countAccountsAudit(t, session, "bank_connection.delete_with_accounts"); got != 1 {
		t.Fatalf("delete-bank audit entries = %d, want 1", got)
	}
}

func TestAccountsSelectionGroupsFilterReadAndDetailActions(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	state := waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})
	if state.Accounts[0].NewCount != 1 {
		t.Fatalf("new count = %d, want 1", state.Accounts[0].NewCount)
	}

	session.AccountsSelect("account", "", int(seed.checking), "")
	waitForAccounts(t, session, events, "the selected account", func(state accountsview.AccountsState) bool {
		return state.Selection == accountpanel.AccountSelectionFor(seed.checking)
	})

	session.AccountsToggleGroup("group", "type", -1, "savings")
	waitForAccounts(t, session, events, "the collapsed savings group", func(state accountsview.AccountsState) bool {
		_, ok := state.Collapsed["type:savings"]
		return ok
	})
	session.AccountsToggleGroup("group", "type", -1, "savings")
	waitForAccounts(t, session, events, "the expanded savings group", func(state accountsview.AccountsState) bool {
		_, ok := state.Collapsed["type:savings"]
		return !ok
	})

	session.AccountsToggleBank(int(seed.bank))
	waitForAccounts(t, session, events, "the collapsed bank", func(state accountsview.AccountsState) bool {
		_, ok := state.CollapsedBanks[seed.bank]
		return ok
	})

	session.AccountsFilterChanged("giro")
	state = waitForAccounts(t, session, events, "the filtered rows", func(state accountsview.AccountsState) bool {
		return state.AccountFilterText == "giro"
	})
	if len(state.FilteredAccounts()) != 1 {
		t.Fatalf("filtered accounts = %d, want 1", len(state.FilteredAccounts()))
	}
	session.AccountsFilterChanged("")
	waitForAccounts(t, session, events, "the cleared filter", func(state accountsview.AccountsState) bool {
		return state.AccountFilterText == ""
	})

	session.AccountsMarkRead("all", "", -1, "")
	waitForAccounts(t, session, events, "the read bookings", func(state accountsview.AccountsState) bool {
		for _, account := range state.Accounts {
			if account.NewCount != 0 {
				return false
			}
		}
		return true
	})

	session.AccountsOpenDetail(int(seed.checking))
	state = waitForAccounts(t, session, events, "the detail view", func(state accountsview.AccountsState) bool {
		return state.Detail != nil && state.Detail.Days == 90
	})
	if state.Detail.Account.ID != seed.checking || len(state.Detail.History) == 0 || len(state.Detail.Recent) != 2 {
		t.Fatalf("detail = %+v", state.Detail)
	}
	session.AccountsSelectDays(int(seed.checking), 30)
	waitForAccounts(t, session, events, "the 30-day detail", func(state accountsview.AccountsState) bool {
		return state.Detail != nil && state.Detail.Days == 30
	})
	session.AccountsCloseDetail()
	waitForAccounts(t, session, events, "the closed detail", func(state accountsview.AccountsState) bool {
		return state.Detail == nil
	})
}

func TestAccountsGroupAndAccountMovesAuditAndReload(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	session.AccountsNewGroup("Urlaub")
	state := waitForAccounts(t, session, events, "the new group", func(state accountsview.AccountsState) bool {
		for _, group := range state.CustomGroups {
			if group.Name == "Urlaub" {
				return true
			}
		}
		return false
	})
	var urlaub model.AccountGroupID
	for _, group := range state.CustomGroups {
		if group.Name == "Urlaub" {
			urlaub = group.ID
		}
	}

	session.AccountsRenameGroup(int(urlaub), "Reisen")
	waitForAccounts(t, session, events, "the renamed group", func(state accountsview.AccountsState) bool {
		for _, group := range state.CustomGroups {
			if group.ID == urlaub && group.Name == "Reisen" {
				return true
			}
		}
		return false
	})

	session.AccountsMoveGroup(int(urlaub), true)
	waitForAccounts(t, session, events, "the moved group", func(state accountsview.AccountsState) bool {
		return len(state.CustomGroups) >= 2 && state.CustomGroups[0].ID == urlaub
	})

	session.AccountsMoveAccount(int(seed.checking), int(urlaub))
	state = waitForAccounts(t, session, events, "the moved account", func(state accountsview.AccountsState) bool {
		for _, account := range state.Accounts {
			if account.ID == seed.checking {
				return account.GroupID != nil && *account.GroupID == urlaub
			}
		}
		return false
	})
	if state.Error != nil {
		t.Fatalf("move error = %q", *state.Error)
	}

	if got := countAccountsAudit(t, session, "account_group.create"); got != 2 {
		t.Fatalf("group create entries = %d, want 2", got)
	}
	if got := countAccountsAudit(t, session, "account_group.rename"); got != 1 {
		t.Fatalf("group rename entries = %d, want 1", got)
	}
	if got := countAccountsAudit(t, session, "REORDER_ACCOUNT_GROUP"); got != 1 {
		t.Fatalf("group reorder entries = %d, want 1", got)
	}
	if got := countAccountsAudit(t, session, "account.set_group"); got != 1 {
		t.Fatalf("account set_group entries = %d, want 1", got)
	}

	session.AccountsDeleteGroup(int(urlaub))
	waitForAccounts(t, session, events, "the deleted group", func(state accountsview.AccountsState) bool {
		for _, group := range state.CustomGroups {
			if group.ID == urlaub {
				return false
			}
		}
		return true
	})
	if got := countAccountsAudit(t, session, "account_group.delete"); got != 1 {
		t.Fatalf("group delete entries = %d, want 1", got)
	}

	session.AccountsForgetDeleted(int(seed.checking))
	state = waitForAccounts(t, session, events, "the forget-deleted message", func(state accountsview.AccountsState) bool {
		return state.Message != nil
	})
	requireAccountsMessage(t, state.Message, i18n.MessageNoDeletedRemembered)
	session.AccountsDismissMessage()
	waitForAccounts(t, session, events, "the dismissed message", func(state accountsview.AccountsState) bool {
		return state.Message == nil && state.Error == nil
	})
}

func TestASecondAccountsActionIsRefusedWhileOneRuns(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "accounts" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	session.AccountsOpenDetail(int(seed.checking))
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first action did not start")
	}

	session.AccountsNewGroup("Refused")
	close(release)
	waitForAccounts(t, session, events, "the first action", func(state accountsview.AccountsState) bool {
		return state.Detail != nil
	})
	state := session.AccountsState()
	for _, group := range state.CustomGroups {
		if group.Name == "Refused" {
			t.Fatal("the refused action changed the state")
		}
	}
	if session.accountsWork.Active() {
		t.Fatal("the busy section did not end")
	}
}

func TestASupersededAccountsLoadIsDropped(t *testing.T) {
	session, _ := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "accounts" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.AccountsLoad()
	first := <-entered
	// Release the busy section by hand so the test can start a second load
	// while the first one is still held in the gate.
	session.accountsWork.End()
	session.AccountsLoad()
	second := <-entered

	close(second)
	waitForAccounts(t, session, events, "the second load", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	close(first)
	waitForJobs(t, session)
	state := session.AccountsState()
	if state.IsLoading {
		t.Fatalf("the superseded load published: %+v", state)
	}
	if len(state.Accounts) != 3 {
		t.Fatalf("the superseded load changed the rows: %+v", state.Accounts)
	}
}

func TestTeardownDuringAnAccountsLoadDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session, _ := newLoadedAccountsSession(t)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		if page != "accounts" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.AccountsLoad()
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the accounts load")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the accounts job did not end")
	}

	requireClosed(t, old, "the old database after the accounts job ends")
	state := session.AccountsState()
	if len(state.Accounts) != 0 || state.Detail != nil {
		t.Fatalf("the teardown did not drop the load: %+v", state)
	}
	if dialog := session.AccountsDialog(); dialog.Kind != "" {
		t.Fatalf("the teardown kept a dialog: %+v", dialog)
	}
}

func TestAccountsStateIsSafeWhileActionsRun(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.AccountsState()
				_ = session.AccountsDialog()
			}
		}
	}()

	for index := 0; index < 4; index++ {
		session.AccountsFilterChanged(fmt.Sprintf("filter-%d", index))
		waitForAccounts(t, session, events, "a filter while reading state", func(state accountsview.AccountsState) bool {
			return state.AccountFilterText == fmt.Sprintf("filter-%d", index)
		})
		session.AccountsToggleBank(int(seed.bank))
		waitForAccounts(t, session, events, "a toggle while reading state", func(accountsview.AccountsState) bool {
			return true
		})
	}
	session.AccountsFilterChanged("")

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadAccountsWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedAccountsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)

	called := make(chan struct{}, 8)
	session.AddListener(func(Change) {
		_ = session.AccountsState()
		_ = session.AccountsDialog()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func TestAccountsOpenTransactionsLeavesABackEntry(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.Navigate("accounts")
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	session.AccountsOpenTransactions(int(seed.checking))
	if screen := session.CurrentScreen(); screen != "transactions" {
		t.Fatalf("screen = %q, want transactions", screen)
	}
	args := session.NavCurrent().Transactions
	if args.AccountID == nil || *args.AccountID != seed.checking {
		t.Fatalf("account id = %+v", args.AccountID)
	}
	target, ok := session.NavTarget()
	if !ok || target.Screen != "accounts" {
		t.Fatalf("back target = %+v, %v", target, ok)
	}
}

func TestAccountsBusyProfileRefusesDeletingAnAccount(t *testing.T) {
	session, seed := newLoadedAccountsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.AccountsLoad()
	waitForAccounts(t, session, events, "the loaded accounts", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 3
	})

	session.profileBusy.Begin()
	defer session.profileBusy.End()
	session.AccountsConfirmDeleteAccount(int(seed.checking))
	state := waitForAccounts(t, session, events, "the busy refusal", func(state accountsview.AccountsState) bool {
		return state.Error != nil
	})
	requireAccountsMessage(t, state.Error, i18n.ErrorDeleteWhileBusy)
	if len(state.Accounts) != 3 {
		t.Fatalf("the busy refusal deleted an account: %+v", state.Accounts)
	}
}

func TestAccountsGroupsAndTotalsNeverMixCurrencies(t *testing.T) {
	appPaths := testPaths(t)
	seed := seedAccountsDatabase(t, appPaths)
	ctx := context.Background()

	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)

	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("USD currency: %v", err)
	}
	open := sessionDatabase(t, session)
	if _, err := accounts.Create(ctx, open, model.NewAccount{
		BankConnectionID: &seed.bank,
		IBAN:             stringPointer("DE89370400440532013001"),
		Currency:         usd,
		AccountType:      model.AccountTypeSavings,
		Balance:          money.Money{Minor: 500, Currency: usd},
	}); err != nil {
		t.Fatalf("create USD account: %v", err)
	}

	events, remove := listenForChanges(session)
	defer remove()
	session.AccountsLoad()
	state := waitForAccounts(t, session, events, "the loaded mixed currencies", func(state accountsview.AccountsState) bool {
		return !state.IsLoading && len(state.Accounts) == 4
	})
	if len(state.GrandTotal) != 2 {
		t.Fatalf("grand total = %+v, want one row per currency", state.GrandTotal)
	}
	if state.GrandTotal[0].Currency != money.EUR || state.GrandTotal[1].Currency != usd {
		t.Fatalf("grand total order = %+v", state.GrandTotal)
	}
	for _, group := range state.Groups {
		seen := make(map[money.Currency]struct{}, len(group.Totals))
		for _, total := range group.Totals {
			if _, duplicate := seen[total.Currency]; duplicate {
				t.Fatalf("group %+v has two totals in %s", group.Key, total.Currency)
			}
			seen[total.Currency] = struct{}{}
		}
	}
}
