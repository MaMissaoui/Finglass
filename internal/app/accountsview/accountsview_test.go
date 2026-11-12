package accountsview

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/accountids"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func createTestDatabase(t *testing.T, file string) *db.Database {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	database, err := db.Create(file, passphrase)
	passphrase.Wipe()
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	file, err := resolved.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	return createTestDatabase(t, file)
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func testDate(t *testing.T, text string) civil.Date {
	t.Helper()
	date, err := civil.ParseISO(text)
	if err != nil {
		t.Fatalf("parse date %q: %v", text, err)
	}
	return date
}

func onlineBank(t *testing.T, database *db.Database, name string) model.BankAccessItem {
	t.Helper()
	bic := "COBADEFFXXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: name,
		BLZ:      "37040044",
		BIC:      &bic,
		UserID:   "user",
	})
	if err != nil {
		t.Fatalf("create online bank: %v", err)
	}
	item, ok, err := bankconnections.GetItem(context.Background(), database, created.ID)
	if err != nil {
		t.Fatalf("get online bank: %v", err)
	}
	if !ok {
		t.Fatal("online bank was not found")
	}
	return item
}

func account(
	t *testing.T,
	database *db.Database,
	bank model.BankConnectionID,
	ibanText string,
	accountType string,
	minor int64,
) model.AccountID {
	t.Helper()
	storedType, ok := model.AccountTypeFromStored(accountType)
	if !ok {
		t.Fatalf("unknown account type %q", accountType)
	}
	created, err := accounts.Create(context.Background(), database, model.NewAccount{
		BankConnectionID: &bank,
		Institution:      nil,
		Name:             nil,
		IBAN:             &ibanText,
		AccountNumber:    nil,
		Currency:         money.EUR,
		AccountType:      storedType,
		Balance:          eur(minor),
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return created
}

func transaction(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	isNew bool,
) model.TransactionID {
	t.Helper()
	partner := "Partner"
	if _, err := transactions.InsertPlanned(context.Background(), database, accountID, []model.NewTransaction{{
		BookingDate:  day,
		Amount:       eur(minor),
		BalanceAfter: nil,
		PartnerName:  &partner,
		Purpose:      nil,
		CategoryID:   nil,
		ImportHash:   fmt.Sprintf("hash-%d-%d-%s", accountID, minor, day.String()),
		CreatedAt:    time.Now().UTC(),
		IsNew:        isNew,
		TaxOverride:  nil,
	}}); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	loaded, err := transactions.ListForAccount(context.Background(), database, accountID, nil, nil)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	return loaded[0].ID
}

func actions(t *testing.T, database *db.Database) []string {
	t.Helper()
	entries, err := audit.Recent(context.Background(), database, 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Action)
	}
	return names
}

func countAction(t *testing.T, database *db.Database, action string) int {
	t.Helper()
	count := 0
	for _, name := range actions(t, database) {
		if name == action {
			count++
		}
	}
	return count
}

func requireMessageText(t *testing.T, got *string, key i18n.Key) {
	t.Helper()
	want := i18n.Text(i18n.En, key)
	if got == nil {
		t.Fatalf("message = nil, want %q", want)
	}
	if *got != want {
		t.Fatalf("message = %q, want %q", *got, want)
	}
}

func groupByKey(groups []accountgrouping.AccountGroupView, key accountgrouping.GroupKey) *accountgrouping.AccountGroupView {
	for index := range groups {
		if groups[index].Key == key {
			return &groups[index]
		}
	}
	return nil
}

func TestLoadAccountsBuildsRowsGroupsAndPerCurrencyTotals(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	checking := account(t, database, bank.ID, "DE89370400440532013000", "checking", 100_000)
	account(t, database, bank.ID, "DE44500105175407324931", "savings", 25_000)
	transaction(t, database, checking, testDate(t, "2026-03-01"), -500, true)

	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if len(state.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(state.Accounts))
	}
	if state.Accounts[0].ID != checking {
		t.Fatalf("accounts[0].ID = %v, want %v", state.Accounts[0].ID, checking)
	}
	if state.Accounts[0].BankName != "Test Bank" {
		t.Fatalf("bank name = %q", state.Accounts[0].BankName)
	}
	if state.Accounts[0].IBAN != "DE89370400440532013000" {
		t.Fatalf("iban = %q", state.Accounts[0].IBAN)
	}
	if state.Accounts[0].NewCount != 1 {
		t.Fatalf("new count = %d, want 1", state.Accounts[0].NewCount)
	}
	if state.PrimaryCurrency != money.EUR {
		t.Fatalf("primary currency = %v", state.PrimaryCurrency)
	}
	wantTotals := []money.CurrencyTotal{{Currency: money.EUR, Total: 125_000}}
	if !reflect.DeepEqual(state.GrandTotal, wantTotals) {
		t.Fatalf("grand total = %+v, want %+v", state.GrandTotal, wantTotals)
	}
	if !reflect.DeepEqual(state.Totals(), wantTotals) {
		t.Fatalf("totals = %+v, want %+v", state.Totals(), wantTotals)
	}

	wantKeys := []accountgrouping.GroupKey{
		accountgrouping.TypeKey("checking"),
		accountgrouping.TypeKey("savings"),
	}
	gotKeys := make([]accountgrouping.GroupKey, 0, len(state.Groups))
	for _, group := range state.Groups {
		gotKeys = append(gotKeys, group.Key)
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("group keys = %+v, want %+v", gotKeys, wantKeys)
	}
	if state.IsLoading {
		t.Fatal("state is still loading")
	}
}

func TestSelectingAnAccountPersistsAndKeepsItsBookingsUnread(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	first := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	second := account(t, database, bank.ID, "DE44500105175407324931", "checking", 0)
	transaction(t, database, second, testDate(t, "2026-03-01"), -500, true)

	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	if err := state.SelectAccounts(context.Background(), database, accountpanel.AccountSelectionFor(second)); err != nil {
		t.Fatalf("select account: %v", err)
	}

	if !reflect.DeepEqual(state.Selection, accountpanel.AccountSelectionFor(second)) {
		t.Fatalf("selection = %+v", state.Selection)
	}
	visible := accountpanel.VisibleAccounts(state.Selection, state.Groups)
	if visible == nil || !reflect.DeepEqual(*visible, model.AccountIDSet{second: struct{}{}}) {
		t.Fatalf("visible = %+v", visible)
	}
	counts, err := transactions.CountNew(context.Background(), database)
	if err != nil {
		t.Fatalf("count new: %v", err)
	}
	if !reflect.DeepEqual(counts, map[model.AccountID]int64{second: 1}) {
		t.Fatalf("new counts = %+v", counts)
	}
	for _, item := range state.Accounts {
		if item.ID == second && item.NewCount != 1 {
			t.Fatalf("new count = %d, want 1", item.NewCount)
		}
	}

	restored := New(i18n.En)
	if err := restored.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("restored load: %v", err)
	}
	if !reflect.DeepEqual(restored.Selection, accountpanel.AccountSelectionFor(second)) {
		t.Fatalf("restored selection = %+v", restored.Selection)
	}

	if err := accounts.Delete(context.Background(), database, second); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	afterDelete := New(i18n.En)
	if err := afterDelete.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if !reflect.DeepEqual(afterDelete.Selection, accountpanel.AllSelection()) {
		t.Fatalf("selection after delete = %+v, want All", afterDelete.Selection)
	}
	if len(afterDelete.Accounts) != 1 {
		t.Fatalf("accounts after delete = %d, want 1", len(afterDelete.Accounts))
	}
	if afterDelete.Accounts[0].ID != first {
		t.Fatalf("remaining account = %v, want %v", afterDelete.Accounts[0].ID, first)
	}
}

func TestGroupActionsChangeTheDatabaseAndWriteOneAuditEntryEach(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	ctx := context.Background()

	if err := state.CreateGroup(ctx, database, "Reisen"); err != nil {
		t.Fatalf("create Reisen: %v", err)
	}
	first := state.CustomGroups[0].ID
	if err := state.CreateGroup(ctx, database, "Haushalt"); err != nil {
		t.Fatalf("create Haushalt: %v", err)
	}
	if err := state.RenameGroup(ctx, database, first, "Urlaub"); err != nil {
		t.Fatalf("rename group: %v", err)
	}
	if err := state.MoveGroup(ctx, database, first, false); err != nil {
		t.Fatalf("move group: %v", err)
	}
	if err := state.MoveAccountToGroup(ctx, database, accountID, &first); err != nil {
		t.Fatalf("move account: %v", err)
	}

	names := make([]string, 0, len(state.CustomGroups))
	for _, group := range state.CustomGroups {
		names = append(names, group.Name)
	}
	if !reflect.DeepEqual(names, []string{"Haushalt", "Urlaub"}) {
		t.Fatalf("group names = %v", names)
	}
	for _, item := range state.Accounts {
		if item.ID == accountID {
			if item.GroupID == nil || *item.GroupID != first {
				t.Fatalf("account group = %v, want %v", item.GroupID, first)
			}
		}
	}
	customGroup := groupByKey(state.Groups, accountgrouping.CustomKey(first))
	if customGroup == nil {
		t.Fatal("custom group not found")
	}
	groupAccountIDs := make([]model.AccountID, 0, len(customGroup.Accounts))
	for _, item := range customGroup.Accounts {
		groupAccountIDs = append(groupAccountIDs, item.ID)
	}
	if !reflect.DeepEqual(groupAccountIDs, []model.AccountID{accountID}) {
		t.Fatalf("custom group accounts = %v", groupAccountIDs)
	}

	if err := state.SelectAccounts(
		ctx,
		database,
		accountpanel.GroupSelection(accountgrouping.CustomKey(first)),
	); err != nil {
		t.Fatalf("select group: %v", err)
	}
	if err := state.DeleteGroup(ctx, database, first); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if !reflect.DeepEqual(state.Selection, accountpanel.AllSelection()) {
		t.Fatalf("selection after group delete = %+v, want All", state.Selection)
	}
	storedSelection, ok, err := settings.Get(ctx, database, accountpanel.SelectionKey)
	if err != nil {
		t.Fatalf("get selection: %v", err)
	}
	if !ok || storedSelection != "all" {
		t.Fatalf("stored selection = %q ok=%v", storedSelection, ok)
	}
	for _, item := range state.Accounts {
		if item.ID == accountID && item.GroupID != nil {
			t.Fatalf("account still in group %v", *item.GroupID)
		}
	}

	if err := state.CreateGroup(ctx, database, "   "); err != nil {
		t.Fatalf("blank group: %v", err)
	}
	requireMessageText(t, state.Error, i18n.ErrorNameRequired)

	if got := countAction(t, database, "account_group.create"); got != 2 {
		t.Fatalf("create entries = %d, want 2", got)
	}
	if got := countAction(t, database, "account_group.rename"); got != 1 {
		t.Fatalf("rename entries = %d, want 1", got)
	}
	if got := countAction(t, database, "REORDER_ACCOUNT_GROUP"); got != 1 {
		t.Fatalf("reorder entries = %d, want 1", got)
	}
	if got := countAction(t, database, "account.set_group"); got != 1 {
		t.Fatalf("set_group entries = %d, want 1", got)
	}
	if got := countAction(t, database, "account_group.delete"); got != 1 {
		t.Fatalf("delete entries = %d, want 1", got)
	}
}

func TestVisibleAccountsFollowTheSelection(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	first := account(t, database, bank.ID, "DE89370400440532013000", "checking", 100_000)
	second := account(t, database, bank.ID, "DE44500105175407324931", "checking", 0)
	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if got := accountpanel.VisibleAccounts(accountpanel.AllSelection(), state.Groups); got != nil {
		t.Fatalf("all = %+v, want nil", got)
	}
	if got := accountpanel.VisibleAccounts(accountpanel.AccountSelectionFor(second), state.Groups); got == nil ||
		!reflect.DeepEqual(*got, model.AccountIDSet{second: struct{}{}}) {
		t.Fatalf("account = %+v", got)
	}
	if got := accountpanel.VisibleAccounts(
		accountpanel.GroupSelection(accountgrouping.TypeKey("checking")),
		state.Groups,
	); got == nil || !reflect.DeepEqual(*got, model.AccountIDSet{first: struct{}{}, second: struct{}{}}) {
		t.Fatalf("checking = %+v", got)
	}

	if err := state.CreateGroup(context.Background(), database, "Leer"); err != nil {
		t.Fatalf("create empty group: %v", err)
	}
	empty := state.CustomGroups[0].ID
	if got := accountpanel.VisibleAccounts(
		accountpanel.GroupSelection(accountgrouping.CustomKey(empty)),
		state.Groups,
	); got == nil || !reflect.DeepEqual(*got, model.AccountIDSet{}) {
		t.Fatalf("empty group = %+v", got)
	}
	if got := accountpanel.VisibleAccounts(
		accountpanel.GroupSelection(accountgrouping.TypeKey("depot")),
		state.Groups,
	); got == nil || !reflect.DeepEqual(*got, model.AccountIDSet{}) {
		t.Fatalf("vanished group = %+v", got)
	}
}

func TestCollapsedGroupsAndBanksPersistAndRefreshKeepsTheToggle(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	account(t, database, bank.ID, "DE89370400440532013000", "savings", 0)
	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	ctx := context.Background()

	if err := state.ToggleCollapsed(ctx, database, accountgrouping.TypeKey("savings")); err != nil {
		t.Fatalf("toggle collapsed: %v", err)
	}
	if err := state.Refresh(ctx, database); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !reflect.DeepEqual(state.Collapsed, map[string]struct{}{"type:savings": {}}) {
		t.Fatalf("collapsed = %+v", state.Collapsed)
	}
	if len(state.CollapsedBanks) != 0 {
		t.Fatalf("collapsed banks = %+v", state.CollapsedBanks)
	}

	if err := state.ToggleBankCollapsed(ctx, database, bank.ID); err != nil {
		t.Fatalf("toggle bank: %v", err)
	}
	if !reflect.DeepEqual(state.CollapsedBanks, map[model.BankConnectionID]struct{}{bank.ID: {}}) {
		t.Fatalf("collapsed banks = %+v", state.CollapsedBanks)
	}

	restored := New(i18n.En)
	if err := restored.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("restored load: %v", err)
	}
	if !reflect.DeepEqual(restored.Collapsed, map[string]struct{}{"type:savings": {}}) {
		t.Fatalf("restored collapsed = %+v", restored.Collapsed)
	}
	if !reflect.DeepEqual(restored.CollapsedBanks, map[model.BankConnectionID]struct{}{bank.ID: {}}) {
		t.Fatalf("restored collapsed banks = %+v", restored.CollapsedBanks)
	}

	if err := restored.ToggleCollapsed(ctx, database, accountgrouping.TypeKey("savings")); err != nil {
		t.Fatalf("restored toggle collapsed: %v", err)
	}
	if err := restored.ToggleBankCollapsed(ctx, database, bank.ID); err != nil {
		t.Fatalf("restored toggle bank: %v", err)
	}
	if len(restored.Collapsed) != 0 {
		t.Fatalf("restored collapsed = %+v", restored.Collapsed)
	}
	if len(restored.CollapsedBanks) != 0 {
		t.Fatalf("restored collapsed banks = %+v", restored.CollapsedBanks)
	}
}

func TestACollapsedBankWhoseBankWasDeletedIsIgnoredOnLoad(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Gone Bank")
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	if err := state.ToggleBankCollapsed(ctx, database, bank.ID); err != nil {
		t.Fatalf("toggle bank: %v", err)
	}
	if _, err := bankconnections.DeleteWithAccounts(ctx, database, bank.ID); err != nil {
		t.Fatalf("delete bank: %v", err)
	}

	restored := New(i18n.En)
	if err := restored.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("restored load: %v", err)
	}
	if len(restored.CollapsedBanks) != 0 {
		t.Fatalf("collapsed banks = %+v, want none", restored.CollapsedBanks)
	}
	if restored.Error != nil {
		t.Fatalf("error = %q, want nil", *restored.Error)
	}
}

func TestAnOfflineBankAccountAcceptsAnIBANOrExactlyFourDigits(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	grocery := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	offline, err := bankconnections.CreateOffline(ctx, database, "Norwegian Card")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	if err := state.Refresh(ctx, database); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got := state.ValidateAccount("4321", true); got != nil {
		t.Fatalf("4321 = %q", *got)
	}
	if got := state.ValidateAccount(" 4321 ", true); got != nil {
		t.Fatalf("trimmed 4321 = %q", *got)
	}
	if got := state.ValidateAccount("NO9386011117947", true); got != nil {
		t.Fatalf("Norwegian IBAN = %q", *got)
	}
	if got := state.ValidateAccount("DE44500105175407324931", true); got != nil {
		t.Fatalf("German IBAN = %q", *got)
	}
	requireMessageText(t, state.ValidateAccount("5555 44xx xxxx 4321", true), i18n.CardLast4Only)
	requireMessageText(t, state.ValidateAccount("1234567812345678", true), i18n.CardLast4Only)
	requireMessageText(t, state.ValidateAccount("4321", false), i18n.IbanChecksumFailed)
	requireMessageText(t, state.ValidateAccount("DE89370400440532013000", false), i18n.AccountIbanExists)

	if state.ValidateOfflineBank("") == nil {
		t.Fatal("empty bank name was accepted")
	}
	if state.ValidateOfflineBank("   ") == nil {
		t.Fatal("blank bank name was accepted")
	}
	if got := state.ValidateOfflineBank("Card"); got != nil {
		t.Fatalf("valid bank name = %q", *got)
	}
	if offline.BankName != "Norwegian Card" {
		t.Fatalf("offline bank name = %q", offline.BankName)
	}
	if grocery <= 0 {
		t.Fatalf("grocery account id = %v", grocery)
	}
}

func TestAddingACardAccountStoresTheSyntheticIdentifier(t *testing.T) {
	database := testDatabase(t)
	offline, err := bankconnections.CreateOffline(context.Background(), database, "Norwegian Card")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.AddAccount(ctx, database, offline.ID, " 4321 ", "credit_card", eur(5_000), nil); err != nil {
		t.Fatalf("add card account: %v", err)
	}
	if len(state.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(state.Accounts))
	}
	item := state.Accounts[0]
	if item.IBAN != "CARD4321" {
		t.Fatalf("iban = %q, want CARD4321", item.IBAN)
	}
	if !accountids.IsCard(item.IBAN) {
		t.Fatalf("%q is not a card id", item.IBAN)
	}
	if !reflect.DeepEqual(item.Balance, eur(5_000)) {
		t.Fatalf("balance = %+v", item.Balance)
	}
	if state.Message == nil || *state.Message != "Account added" {
		t.Fatalf("message = %v", state.Message)
	}
	if got := countAction(t, database, "account.create"); got != 1 {
		t.Fatalf("create entries = %d, want 1", got)
	}
	requireMessageText(t, state.ValidateAccount("4321", true), i18n.AccountIbanExists)

	if err := state.AddAccount(ctx, database, offline.ID, "1234567812345678", "credit_card", eur(0), nil); err != nil {
		t.Fatalf("add pasted card: %v", err)
	}
	requireMessageText(t, state.Error, i18n.CardLast4Only)
	if len(state.Accounts) != 1 {
		t.Fatalf("accounts after refused card = %d, want 1", len(state.Accounts))
	}
}

func TestAddingAnOfflineBankCreatesItOfflineWithoutSyncParticipation(t *testing.T) {
	database := testDatabase(t)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.AddOfflineBank(ctx, database, "Bank Norwegian"); err != nil {
		t.Fatalf("add offline bank: %v", err)
	}
	if len(state.BankAccesses) != 1 {
		t.Fatalf("banks = %d, want 1", len(state.BankAccesses))
	}
	bank := state.BankAccesses[0]
	if !bank.Offline {
		t.Fatal("bank is not offline")
	}
	if bank.IncludeInSyncAll {
		t.Fatal("offline bank participates in sync all")
	}
	if bank.BankName != "Bank Norwegian" {
		t.Fatalf("bank name = %q", bank.BankName)
	}
	if state.Message == nil || *state.Message != "Added Bank Norwegian" {
		t.Fatalf("message = %v", state.Message)
	}
	if got := countAction(t, database, "bank_connection.create_offline"); got != 1 {
		t.Fatalf("create entries = %d, want 1", got)
	}

	if err := state.AddOfflineBank(ctx, database, "   "); err != nil {
		t.Fatalf("add blank bank: %v", err)
	}
	requireMessageText(t, state.Error, i18n.BankNameRequired)
}

func TestDeletingAnAccountIsRefusedWhileWorkIsBusy(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	state.Busy.Begin()
	outcome := state.DeleteAccount(ctx, database, accountID)
	state.Busy.End()

	if outcome != nil {
		t.Fatalf("delete while busy = %v, want nil", outcome)
	}
	requireMessageText(t, state.Error, i18n.ErrorDeleteWhileBusy)
	if _, ok, err := accounts.Get(ctx, database, accountID); err != nil || !ok {
		t.Fatalf("account after refused delete: ok=%v err=%v", ok, err)
	}

	if err := state.DeleteAccount(ctx, database, accountID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, ok, err := accounts.Get(ctx, database, accountID); err != nil || ok {
		t.Fatalf("account after delete: ok=%v err=%v", ok, err)
	}
	if state.Message == nil || *state.Message != "Account deleted" {
		t.Fatalf("message = %v", state.Message)
	}
}

func TestDeletingAnOfflineBankRemovesItsAccounts(t *testing.T) {
	database := testDatabase(t)
	offline, err := bankconnections.CreateOffline(context.Background(), database, "Card")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	accountID := account(t, database, offline.ID, "CARD4321", "credit_card", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.DeleteBankAccess(ctx, database, offline.ID); err != nil {
		t.Fatalf("delete bank: %v", err)
	}
	if len(state.BankAccesses) != 0 {
		t.Fatalf("banks = %d, want 0", len(state.BankAccesses))
	}
	if len(state.Accounts) != 0 {
		t.Fatalf("accounts = %d, want 0", len(state.Accounts))
	}
	if _, ok, err := accounts.Get(ctx, database, accountID); err != nil || ok {
		t.Fatalf("account after delete: ok=%v err=%v", ok, err)
	}
	if state.Message == nil || *state.Message != "Bank removed" {
		t.Fatalf("message = %v", state.Message)
	}
}

func TestRenameAccountStoresTheNameAuditsOnlyAChangeAndUpdatesTheDetail(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.RenameAccount(ctx, database, accountID, " Haushalt "); err != nil {
		t.Fatalf("rename account: %v", err)
	}
	stored, ok, err := accounts.Get(ctx, database, accountID)
	if err != nil || !ok {
		t.Fatalf("get account: ok=%v err=%v", ok, err)
	}
	if stored.Name == nil || *stored.Name != "Haushalt" {
		t.Fatalf("stored name = %v", stored.Name)
	}
	if got := countAction(t, database, "account.rename"); got != 1 {
		t.Fatalf("rename entries = %d, want 1", got)
	}

	if err := state.RenameAccount(ctx, database, accountID, "Haushalt"); err != nil {
		t.Fatalf("rename to same name: %v", err)
	}
	if got := countAction(t, database, "account.rename"); got != 1 {
		t.Fatalf("rename entries after same name = %d, want 1", got)
	}

	if err := state.RenameAccount(ctx, database, accountID, "   "); err != nil {
		t.Fatalf("clear name: %v", err)
	}
	stored, ok, err = accounts.Get(ctx, database, accountID)
	if err != nil || !ok {
		t.Fatalf("get account: ok=%v err=%v", ok, err)
	}
	if stored.Name != nil {
		t.Fatalf("stored name = %q, want nil", *stored.Name)
	}
	if got := countAction(t, database, "account.rename"); got != 2 {
		t.Fatalf("rename entries after clear = %d, want 2", got)
	}

	if err := state.OpenDetailDefault(ctx, database, accountID, testDate(t, "2026-03-31")); err != nil {
		t.Fatalf("open detail: %v", err)
	}
	if err := state.RenameAccount(ctx, database, accountID, "Reisekasse"); err != nil {
		t.Fatalf("rename with detail open: %v", err)
	}
	if state.Detail == nil || state.Detail.Account.Name == nil || *state.Detail.Account.Name != "Reisekasse" {
		t.Fatalf("detail account name = %v", state.Detail)
	}
}

func TestSetAccountTypeStoresAndAuditsAValidTypeAndRefusesAnUnknownOne(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.SetAccountType(ctx, database, accountID, "loan"); err != nil {
		t.Fatalf("set type: %v", err)
	}
	stored, ok, err := accounts.Get(ctx, database, accountID)
	if err != nil || !ok {
		t.Fatalf("get account: ok=%v err=%v", ok, err)
	}
	if stored.AccountType != model.AccountTypeLoan {
		t.Fatalf("account type = %q, want loan", stored.AccountType)
	}
	entries, err := audit.Recent(ctx, database, 1)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if entries[0].Action != "CHANGE_ACCOUNT_TYPE" {
		t.Fatalf("action = %q", entries[0].Action)
	}
	if !strings.Contains(
		entries[0].Details,
		fmt.Sprintf("Account %d: checking -> loan", accountID),
	) {
		t.Fatalf("details = %q", entries[0].Details)
	}

	if err := state.SetAccountType(ctx, database, accountID, "zebra"); err != nil {
		t.Fatalf("set unknown type: %v", err)
	}
	stored, ok, err = accounts.Get(ctx, database, accountID)
	if err != nil || !ok {
		t.Fatalf("get account: ok=%v err=%v", ok, err)
	}
	if stored.AccountType != model.AccountTypeLoan {
		t.Fatalf("account type after invalid = %q, want loan", stored.AccountType)
	}
	if got := countAction(t, database, "CHANGE_ACCOUNT_TYPE"); got != 1 {
		t.Fatalf("type entries = %d, want 1", got)
	}
	requireMessageText(t, state.Error, i18n.ErrorAccountTypeInvalid)
}

func TestForgettingDeletedBookingsAuditsTheCountAndMessagesTheZeroCase(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013020", "checking", 0)
	booking := transaction(t, database, accountID, testDate(t, "2026-10-05"), -500, false)
	if _, err := transactions.DeleteAll(context.Background(), database, []model.TransactionID{booking}); err != nil {
		t.Fatalf("delete booking: %v", err)
	}

	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	if err := state.ForgetDeletedBookings(ctx, database, accountID); err != nil {
		t.Fatalf("forget deleted: %v", err)
	}
	entries, err := audit.Recent(ctx, database, 1)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if entries[0].Action != "FORGET_DELETED_BOOKINGS" {
		t.Fatalf("action = %q", entries[0].Action)
	}
	if entries[0].Details != "Account 3020: 1 deleted bookings may be imported again" {
		t.Fatalf("details = %q", entries[0].Details)
	}
	wantMessage := i18n.Format(i18n.En, i18n.MessageDeletedForgotten, i18n.Int(1))
	if state.Message == nil || *state.Message != wantMessage {
		t.Fatalf("message = %v, want %q", state.Message, wantMessage)
	}

	other := account(t, database, bank.ID, "DE89370400440532013021", "checking", 0)
	if err := state.Refresh(ctx, database); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := state.ForgetDeletedBookings(ctx, database, other); err != nil {
		t.Fatalf("forget nothing: %v", err)
	}
	if got := countAction(t, database, "FORGET_DELETED_BOOKINGS"); got != 1 {
		t.Fatalf("forget entries = %d, want 1", got)
	}
	requireMessageText(t, state.Message, i18n.MessageNoDeletedRemembered)
}

func TestOpenDetailShowsHistoryRecentBookingsAndPeriodTotals(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 11_500)
	transaction(t, database, accountID, testDate(t, "2026-03-29"), 2_000, false)
	transaction(t, database, accountID, testDate(t, "2026-03-30"), -500, false)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.OpenDetail(ctx, database, accountID, 5, testDate(t, "2026-03-31")); err != nil {
		t.Fatalf("open detail: %v", err)
	}
	if state.Detail == nil {
		t.Fatal("detail is nil")
	}
	detail := state.Detail
	if detail.Account.ID != accountID {
		t.Fatalf("detail account = %v", detail.Account.ID)
	}
	if detail.Days != 5 {
		t.Fatalf("days = %d, want 5", detail.Days)
	}
	if len(detail.History) != 5 {
		t.Fatalf("history = %d, want 5", len(detail.History))
	}
	if len(detail.Recent) != 2 {
		t.Fatalf("recent = %d, want 2", len(detail.Recent))
	}
	if !reflect.DeepEqual(detail.IncomeInPeriod, eur(2_000)) {
		t.Fatalf("income = %+v", detail.IncomeInPeriod)
	}
	if !reflect.DeepEqual(detail.ExpensesInPeriod, eur(500)) {
		t.Fatalf("expenses = %+v", detail.ExpensesInPeriod)
	}
	last := detail.History[len(detail.History)-1]
	if !reflect.DeepEqual(last.Balance, eur(11_500)) {
		t.Fatalf("last history balance = %+v, want %+v", last.Balance, eur(11_500))
	}

	state.CloseDetail()
	if state.Detail != nil {
		t.Fatal("detail was not closed")
	}
	kept := "kept"
	state.Message = &kept
	state.Error = &kept
	state.ClearMessages()
	if state.Message != nil || state.Error != nil {
		t.Fatalf("messages were not cleared: message=%v error=%v", state.Message, state.Error)
	}
}

func TestAccountFilterUsesTheTrimmedQuery(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	first := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	account(t, database, bank.ID, "DE44500105175407324931", "savings", 0)
	name := "Haushalt"
	if err := accounts.Rename(context.Background(), database, first, &name); err != nil {
		t.Fatalf("rename account: %v", err)
	}
	state := New(i18n.En)
	if err := state.LoadAccounts(context.Background(), database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if got := len(state.FilteredAccounts()); got != 2 {
		t.Fatalf("filtered = %d, want 2", got)
	}
	state.UpdateAccountFilterText(" haus ")
	if got := len(state.FilteredAccounts()); got != 1 {
		t.Fatalf("haus filtered = %d, want 1", got)
	}
	state.UpdateAccountFilterText("Test Bank")
	if got := len(state.FilteredAccounts()); got != 2 {
		t.Fatalf("bank filtered = %d, want 2", got)
	}
	state.UpdateAccountFilterText("4450")
	if got := len(state.FilteredAccounts()); got != 1 {
		t.Fatalf("identifier filtered = %d, want 1", got)
	}
}

func TestCollapsingAnOfflineBankIsNotAudited(t *testing.T) {
	database := testDatabase(t)
	offline, err := bankconnections.CreateOffline(context.Background(), database, "Card")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	before := len(actions(t, database))
	if err := state.ToggleBankCollapsed(ctx, database, offline.ID); err != nil {
		t.Fatalf("toggle bank: %v", err)
	}
	if after := len(actions(t, database)); after != before {
		t.Fatalf("audit entries = %d, want %d", after, before)
	}
}

func TestMarkReadClearsAGroupsAccountsAndAllClearsEverything(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	checking := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	savings := account(t, database, bank.ID, "DE44500105175407324931", "savings", 0)
	transaction(t, database, checking, testDate(t, "2026-03-01"), -100, true)
	transaction(t, database, savings, testDate(t, "2026-03-02"), -200, true)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	if err := state.MarkRead(
		ctx,
		database,
		accountpanel.GroupSelection(accountgrouping.TypeKey("checking")),
	); err != nil {
		t.Fatalf("mark group read: %v", err)
	}
	counts, err := transactions.CountNew(ctx, database)
	if err != nil {
		t.Fatalf("count new: %v", err)
	}
	if !reflect.DeepEqual(counts, map[model.AccountID]int64{savings: 1}) {
		t.Fatalf("new counts = %+v", counts)
	}
	for _, item := range state.Accounts {
		if item.ID == checking && item.NewCount != 0 {
			t.Fatalf("checking new count = %d, want 0", item.NewCount)
		}
	}

	if err := state.MarkRead(ctx, database, accountpanel.AllSelection()); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	counts, err = transactions.CountNew(ctx, database)
	if err != nil {
		t.Fatalf("count new: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("new counts = %+v, want empty", counts)
	}
	if state.Error != nil {
		t.Fatalf("error = %q, want nil", *state.Error)
	}
	if got := countAction(t, database, "transaction.mark_read"); got == 0 {
		t.Fatal("mark read wrote no audit entry")
	}
}

func TestDeletingAnOfflineBankIsRefusedWhileWorkIsBusy(t *testing.T) {
	database := testDatabase(t)
	offline, err := bankconnections.CreateOffline(context.Background(), database, "Card")
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	accountID := account(t, database, offline.ID, "CARD4321", "credit_card", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	state.Busy.Begin()
	outcome := state.DeleteBankAccess(ctx, database, offline.ID)
	state.Busy.End()

	if outcome != nil {
		t.Fatalf("delete while busy = %v, want nil", outcome)
	}
	requireMessageText(t, state.Error, i18n.ErrorDeleteWhileBusy)
	if _, ok, err := accounts.Get(ctx, database, accountID); err != nil || !ok {
		t.Fatalf("account after refused delete: ok=%v err=%v", ok, err)
	}
	if len(state.BankAccesses) != 1 {
		t.Fatalf("banks = %d, want 1", len(state.BankAccesses))
	}

	if err := state.DeleteBankAccess(ctx, database, offline.ID); err != nil {
		t.Fatalf("delete bank: %v", err)
	}
	if len(state.BankAccesses) != 0 {
		t.Fatalf("banks = %d, want 0", len(state.BankAccesses))
	}
}

func TestDeletingAnOnlineBankAccessLeavesItAndItsSettingsUntouched(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Test Bank")
	accountID := account(t, database, bank.ID, "DE89370400440532013000", "checking", 0)
	state := New(i18n.En)
	ctx := context.Background()
	if err := state.LoadAccounts(ctx, database); err != nil {
		t.Fatalf("load accounts: %v", err)
	}

	selectionValue := "account:" + strconv.FormatInt(int64(accountID), 10)
	collapsedValue := strconv.FormatInt(int64(bank.ID), 10)
	if err := settings.Set(ctx, database, accountpanel.SelectionKey, selectionValue); err != nil {
		t.Fatalf("set selection: %v", err)
	}
	if err := settings.Set(ctx, database, CollapsedBanksKey, collapsedValue); err != nil {
		t.Fatalf("set collapsed banks: %v", err)
	}
	before := len(actions(t, database))

	if err := state.DeleteBankAccess(ctx, database, bank.ID); err != nil {
		t.Fatalf("delete online bank access: %v", err)
	}

	if item, ok, err := bankconnections.GetItem(ctx, database, bank.ID); err != nil || !ok {
		t.Fatalf("connection after delete: ok=%v err=%v item=%+v", ok, err, item)
	}
	if _, ok, err := accounts.Get(ctx, database, accountID); err != nil || !ok {
		t.Fatalf("account after delete: ok=%v err=%v", ok, err)
	}
	storedSelection, ok, err := settings.Get(ctx, database, accountpanel.SelectionKey)
	if err != nil || !ok || storedSelection != selectionValue {
		t.Fatalf("selection after delete: %q ok=%v err=%v", storedSelection, ok, err)
	}
	storedCollapsed, ok, err := settings.Get(ctx, database, CollapsedBanksKey)
	if err != nil || !ok || storedCollapsed != collapsedValue {
		t.Fatalf("collapsed banks after delete: %q ok=%v err=%v", storedCollapsed, ok, err)
	}
	if after := len(actions(t, database)); after != before {
		t.Fatalf("audit entries = %d, want %d", after, before)
	}
	if len(state.BankAccesses) != 1 || len(state.Accounts) != 1 {
		t.Fatalf("state after delete: banks=%d accounts=%d", len(state.BankAccesses), len(state.Accounts))
	}
}

func TestCollapsedBanksKeyIsPinnedAndDefaultUsesEnglish(t *testing.T) {
	if CollapsedBanksKey != "accounts.collapsedBanks" {
		t.Fatalf("CollapsedBanksKey = %q", CollapsedBanksKey)
	}
	state := Default()
	if state.Language != i18n.En {
		t.Fatalf("default language = %v, want English", state.Language)
	}
	if state.PrimaryCurrency != money.EUR {
		t.Fatalf("default primary currency = %v, want EUR", state.PrimaryCurrency)
	}
	if state.Selection.Kind != accountpanel.SelectionAll {
		t.Fatalf("default selection = %+v, want All", state.Selection)
	}
}

func TestBusyValuesCompareByIdentity(t *testing.T) {
	first := NewBusy()
	second := NewBusy()
	if first == second {
		t.Fatal("two fresh Busy values compare equal")
	}
	copied := first
	if first != copied {
		t.Fatal("a copied Busy value does not share its identity")
	}
}
