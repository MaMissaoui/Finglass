package accountpanel

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

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

func testAccount(id int64, accountType string) accountgrouping.AccountItem {
	return accountgrouping.AccountItem{
		ID:          model.AccountID(id),
		Currency:    money.EUR,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
		AccountType: accountType,
	}
}

func testCustomGroup(id int64, name string) model.AccountGroup {
	return model.AccountGroup{
		ID:        model.AccountGroupID(id),
		Name:      name,
		SortOrder: id,
	}
}

func TestSelectionValuesRoundTripLikeFinDesk(t *testing.T) {
	if got := SelectionValue(AllSelection()); got != "all" {
		t.Fatalf("all = %q", got)
	}
	if got := SelectionValue(GroupSelection(accountgrouping.TypeKey("savings"))); got != "group:type:savings" {
		t.Fatalf("type group = %q", got)
	}
	if got := SelectionValue(GroupSelection(accountgrouping.CustomKey(7))); got != "group:custom:7" {
		t.Fatalf("custom group = %q", got)
	}
	if got := SelectionValue(AccountSelectionFor(3)); got != "account:3" {
		t.Fatalf("account = %q", got)
	}
}

func TestAStoredSelectionLoadsAndFallsBackWhenItsTargetIsGone(t *testing.T) {
	database := testDatabase(t)
	accounts := []accountgrouping.AccountItem{testAccount(1, "checking")}
	groups := accountgrouping.Group(accounts, []model.AccountGroup{testCustomGroup(7, "Reisen")}, money.EUR)

	loaded, err := LoadSelection(context.Background(), database, groups, accounts)
	if err != nil {
		t.Fatalf("load empty selection: %v", err)
	}
	if !reflect.DeepEqual(loaded, AllSelection()) {
		t.Fatalf("nothing stored = %+v, want All", loaded)
	}

	cases := []struct {
		stored   string
		expected AccountSelection
	}{
		{"all", AllSelection()},
		{"group:type:checking", GroupSelection(accountgrouping.TypeKey("checking"))},
		{"group:custom:7", GroupSelection(accountgrouping.CustomKey(7))},
		{"account:1", AccountSelectionFor(1)},
		{"group:type:depot", AllSelection()},
		{"group:custom:99", AllSelection()},
		{"account:2", AllSelection()},
		{"nonsense", AllSelection()},
	}
	for _, testCase := range cases {
		if err := settings.Set(context.Background(), database, SelectionKey, testCase.stored); err != nil {
			t.Fatalf("set %q: %v", testCase.stored, err)
		}
		loaded, err := LoadSelection(context.Background(), database, groups, accounts)
		if err != nil {
			t.Fatalf("load %q: %v", testCase.stored, err)
		}
		if !reflect.DeepEqual(loaded, testCase.expected) {
			t.Fatalf("load %q = %+v, want %+v", testCase.stored, loaded, testCase.expected)
		}
	}
}

func TestSavingASelectionStoresItForBothScreens(t *testing.T) {
	database := testDatabase(t)
	selection := GroupSelection(accountgrouping.TypeKey("savings"))
	if err := SaveSelection(context.Background(), database, selection); err != nil {
		t.Fatalf("save selection: %v", err)
	}

	stored, ok, err := settings.Get(context.Background(), database, SelectionKey)
	if err != nil {
		t.Fatalf("get selection: %v", err)
	}
	if !ok || stored != "group:type:savings" {
		t.Fatalf("stored = %q ok=%v", stored, ok)
	}

	if err := StoreSelection(context.Background(), database, AccountSelectionFor(9)); err != nil {
		t.Fatalf("store selection: %v", err)
	}
	stored, ok, err = settings.Get(context.Background(), database, SelectionKey)
	if err != nil {
		t.Fatalf("get selection: %v", err)
	}
	if !ok || stored != "account:9" {
		t.Fatalf("stored = %q ok=%v", stored, ok)
	}
}

func TestResolveFallsBackToAllForADeletedAccountOrGroup(t *testing.T) {
	accounts := []accountgrouping.AccountItem{testAccount(1, "checking")}
	groups := accountgrouping.Group(accounts, nil, money.EUR)

	if got := Resolve(AccountSelectionFor(1), groups, accounts); !reflect.DeepEqual(got, AccountSelectionFor(1)) {
		t.Fatalf("existing account = %+v", got)
	}
	if got := Resolve(AccountSelectionFor(2), groups, accounts); !reflect.DeepEqual(got, AllSelection()) {
		t.Fatalf("missing account = %+v", got)
	}
	if got := Resolve(GroupSelection(accountgrouping.TypeKey("checking")), groups, accounts); !reflect.DeepEqual(
		got,
		GroupSelection(accountgrouping.TypeKey("checking")),
	) {
		t.Fatalf("existing group = %+v", got)
	}
	if got := Resolve(GroupSelection(accountgrouping.TypeKey("depot")), groups, accounts); !reflect.DeepEqual(
		got,
		AllSelection(),
	) {
		t.Fatalf("missing group = %+v", got)
	}
}

func TestVisibleAccountsReturnsNilForAllAndNothingForAVanishedGroup(t *testing.T) {
	accounts := []accountgrouping.AccountItem{testAccount(1, "checking"), testAccount(2, "checking")}
	groups := accountgrouping.Group(accounts, []model.AccountGroup{testCustomGroup(7, "Leer")}, money.EUR)

	if got := VisibleAccounts(AllSelection(), groups); got != nil {
		t.Fatalf("all = %+v, want nil", got)
	}
	if got := VisibleAccounts(AccountSelectionFor(2), groups); got == nil ||
		!reflect.DeepEqual(*got, model.AccountIDSet{2: struct{}{}}) {
		t.Fatalf("account = %+v", got)
	}
	if got := VisibleAccounts(GroupSelection(accountgrouping.TypeKey("checking")), groups); got == nil ||
		!reflect.DeepEqual(*got, model.AccountIDSet{1: struct{}{}, 2: struct{}{}}) {
		t.Fatalf("checking = %+v", got)
	}
	if got := VisibleAccounts(GroupSelection(accountgrouping.CustomKey(7)), groups); got == nil ||
		!reflect.DeepEqual(*got, model.AccountIDSet{}) {
		t.Fatalf("empty custom group = %+v", got)
	}
	if got := VisibleAccounts(GroupSelection(accountgrouping.TypeKey("depot")), groups); got == nil ||
		!reflect.DeepEqual(*got, model.AccountIDSet{}) {
		t.Fatalf("vanished group = %+v", got)
	}
}

func TestCollapsedGroupsRoundTripSortedAndRejectEmptyPieces(t *testing.T) {
	database := testDatabase(t)
	collapsed, err := LoadCollapsed(context.Background(), database)
	if err != nil {
		t.Fatalf("load empty collapsed: %v", err)
	}
	if len(collapsed) != 0 {
		t.Fatalf("empty collapsed = %+v", collapsed)
	}

	if err := settings.Set(context.Background(), database, CollapsedKey, " type:b ,, custom:7 "); err != nil {
		t.Fatalf("set collapsed: %v", err)
	}
	collapsed, err = LoadCollapsed(context.Background(), database)
	if err != nil {
		t.Fatalf("load collapsed: %v", err)
	}
	if !reflect.DeepEqual(collapsed, map[string]struct{}{"custom:7": {}, "type:b": {}}) {
		t.Fatalf("collapsed = %+v", collapsed)
	}

	toSave := map[string]struct{}{"type:z": {}, "type:a": {}}
	if err := SaveCollapsed(context.Background(), database, toSave); err != nil {
		t.Fatalf("save collapsed: %v", err)
	}
	stored, ok, err := settings.Get(context.Background(), database, CollapsedKey)
	if err != nil {
		t.Fatalf("get collapsed: %v", err)
	}
	if !ok || stored != "type:a,type:z" {
		t.Fatalf("stored = %q ok=%v", stored, ok)
	}
}

func TestTheAccountFilterSearchesNameIdentifierAndBank(t *testing.T) {
	haushalt := "Haushalt"
	if !MatchesFilter(&haushalt, "DE89", "Test Bank", "") {
		t.Fatal("empty query should match")
	}
	if !MatchesFilter(&haushalt, "DE89", "Test Bank", "haus") {
		t.Fatal("name should match")
	}
	if !MatchesFilter(nil, "DE89370400440532013000", "Test Bank", "3201") {
		t.Fatal("identifier should match")
	}
	if !MatchesFilter(nil, "CARD4321", "Test Bank", "test") {
		t.Fatal("bank should match")
	}
	if MatchesFilter(nil, "CARD4321", "Test Bank", "reisen") {
		t.Fatal("unrelated query matched")
	}
}

func TestGroupNamesAreTrimmedCappedAndNeverBlank(t *testing.T) {
	clean, err := NormalizeGroupName("  Reisen  ")
	if err != nil {
		t.Fatalf("normalize name: %v", err)
	}
	if clean != "Reisen" {
		t.Fatalf("clean = %q", clean)
	}

	long := make([]rune, 150)
	for index := range long {
		long[index] = 'x'
	}
	clean, err = NormalizeGroupName(string(long))
	if err != nil {
		t.Fatalf("normalize long name: %v", err)
	}
	if len([]rune(clean)) != MaxGroupNameLength {
		t.Fatalf("length = %d, want %d", len([]rune(clean)), MaxGroupNameLength)
	}

	_, err = NormalizeGroupName("   ")
	var appError *AppError
	if !errors.As(err, &appError) {
		t.Fatalf("blank name error = %v, want *AppError", err)
	}
	key, ok := appError.Message()
	if !ok || key != i18n.ErrorNameRequired {
		t.Fatalf("blank name key = %v ok=%v", key, ok)
	}
}

func TestSettingsKeysArePinned(t *testing.T) {
	if SelectionKey != "transactions.selection" {
		t.Fatalf("SelectionKey = %q", SelectionKey)
	}
	if CollapsedKey != "accountGroups.collapsed" {
		t.Fatalf("CollapsedKey = %q", CollapsedKey)
	}
}
