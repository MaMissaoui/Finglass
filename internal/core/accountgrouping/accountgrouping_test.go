package accountgrouping

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func testCurrency(code string) money.Currency {
	currency, err := money.NewCurrency(code)
	if err != nil {
		panic(err)
	}
	return currency
}

func testAccount(
	id int64,
	accountType string,
	currency money.Currency,
	balance int64,
	groupID *int64,
	newCount int64,
) AccountItem {
	var group *model.AccountGroupID
	if groupID != nil {
		value := model.AccountGroupID(*groupID)
		group = &value
	}
	name := fmt.Sprintf("Account %d", id)
	return AccountItem{
		ID:          model.AccountID(id),
		Name:        &name,
		Currency:    currency,
		Balance:     money.Money{Minor: balance, Currency: currency},
		AccountType: accountType,
		GroupID:     group,
		NewCount:    newCount,
	}
}

func simpleAccount(id int64, accountType string) AccountItem {
	return testAccount(id, accountType, money.EUR, 0, nil, 0)
}

func customGroup(id int64, name string) model.AccountGroup {
	return model.AccountGroup{ID: model.AccountGroupID(id), Name: name, SortOrder: id}
}

func groupKeys(views []AccountGroupView) []GroupKey {
	keys := make([]GroupKey, 0, len(views))
	for _, view := range views {
		keys = append(keys, view.Key)
	}
	return keys
}

func accountIDs(accounts []AccountItem) []model.AccountID {
	ids := make([]model.AccountID, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return ids
}

func viewWithKey(t *testing.T, views []AccountGroupView, key GroupKey) AccountGroupView {
	t.Helper()
	for _, view := range views {
		if view.Key == key {
			return view
		}
	}
	t.Fatalf("no view for %s", key.ID())
	return AccountGroupView{}
}

func TestGroupsByTypeInTheFixedOrder(t *testing.T) {
	accounts := []AccountItem{
		simpleAccount(1, "depot"),
		simpleAccount(2, "checking"),
		simpleAccount(3, "savings"),
		simpleAccount(4, "credit_card"),
	}
	views := Group(accounts, nil, money.EUR)
	if got, want := groupKeys(views), []GroupKey{
		TypeKey("checking"),
		TypeKey("savings"),
		TypeKey("credit_card"),
		TypeKey("depot"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	if got, want := accountIDs(views[0].Accounts), []model.AccountID{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("checking accounts = %v, want %v", got, want)
	}
}

func TestALoanGroupSortsAfterCreditCardAndBeforeDepot(t *testing.T) {
	accounts := []AccountItem{
		simpleAccount(1, "depot"),
		simpleAccount(2, "loan"),
		simpleAccount(3, "credit_card"),
		simpleAccount(4, "checking"),
		simpleAccount(5, "savings"),
	}
	views := Group(accounts, nil, money.EUR)
	if got, want := groupKeys(views), []GroupKey{
		TypeKey("checking"),
		TypeKey("savings"),
		TypeKey("credit_card"),
		TypeKey("loan"),
		TypeKey("depot"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	loan := viewWithKey(t, views, TypeKey("loan"))
	if got, want := accountIDs(loan.Accounts), []model.AccountID{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loan accounts = %v, want %v", got, want)
	}
}

func TestAnUnknownTypeFollowsTheFixedOnesAlphabetically(t *testing.T) {
	accounts := []AccountItem{
		simpleAccount(1, "checking"),
		simpleAccount(2, "zebra"),
		simpleAccount(3, "apple"),
	}
	views := Group(accounts, nil, money.EUR)
	if got, want := groupKeys(views), []GroupKey{
		TypeKey("checking"),
		TypeKey("apple"),
		TypeKey("zebra"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
}

func TestACustomGroupOverridesTheAccountType(t *testing.T) {
	groupID := int64(10)
	accounts := []AccountItem{testAccount(1, "savings", money.EUR, 0, &groupID, 0)}
	views := Group(accounts, []model.AccountGroup{customGroup(10, "Reisen")}, money.EUR)
	if got, want := groupKeys(views), []GroupKey{CustomKey(10)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	if views[0].Name == nil || *views[0].Name != "Reisen" {
		t.Fatalf("name = %v, want Reisen", views[0].Name)
	}
	if got, want := accountIDs(views[0].Accounts), []model.AccountID{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("accounts = %v, want %v", got, want)
	}
	for _, view := range views {
		if view.Key.Kind == GroupKeyType {
			t.Fatalf("unexpected type group %s", view.Key.ID())
		}
	}
}

func TestAnUnknownGroupIDFallsBackToTheType(t *testing.T) {
	groupID := int64(99)
	accounts := []AccountItem{testAccount(1, "checking", money.EUR, 0, &groupID, 0)}
	views := Group(accounts, []model.AccountGroup{customGroup(10, "Reisen")}, money.EUR)
	if got, want := groupKeys(views), []GroupKey{
		TypeKey("checking"),
		CustomKey(10),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	if got, want := accountIDs(views[0].Accounts), []model.AccountID{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("checking accounts = %v, want %v", got, want)
	}
	if len(views[1].Accounts) != 0 {
		t.Fatalf("the custom group stays empty, got %v", views[1].Accounts)
	}
}

func TestEmptyTypeGroupsAreHiddenButEmptyCustomGroupsAreShown(t *testing.T) {
	accounts := []AccountItem{simpleAccount(1, "checking")}
	views := Group(accounts, []model.AccountGroup{customGroup(10, "Reisen")}, money.EUR)
	if got, want := groupKeys(views), []GroupKey{
		TypeKey("checking"),
		CustomKey(10),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	custom := viewWithKey(t, views, CustomKey(10))
	if len(custom.Accounts) != 0 {
		t.Fatalf("an empty custom group can receive accounts, got %v", custom.Accounts)
	}
}

func TestMixedCurrencyTotalsAreNeverSummedAndThePrimaryCurrencyComesFirst(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", testCurrency("USD"), 5_000, nil, 0),
		testAccount(2, "checking", testCurrency("EUR"), 10_000, nil, 0),
		testAccount(3, "depot", testCurrency("EUR"), 200_000, nil, 0),
	}
	views := Group(accounts, nil, money.EUR)
	checking := viewWithKey(t, views, TypeKey("checking"))
	currencies := make([]money.Currency, 0, len(checking.Totals))
	for _, total := range checking.Totals {
		currencies = append(currencies, total.Currency)
	}
	if got, want := currencies, []money.Currency{money.EUR, testCurrency("USD")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("total currencies = %v, want %v (primary first)", got, want)
	}
	if checking.Totals[0].Total != 10_000 {
		t.Fatalf("EUR total = %d, want 10000", checking.Totals[0].Total)
	}
	if checking.Totals[1].Total != 5_000 {
		t.Fatalf("USD total = %d, want 5000", checking.Totals[1].Total)
	}
	if len(checking.Totals) != 2 {
		t.Fatalf("EUR and USD are never added up, got %d totals", len(checking.Totals))
	}

	depot := viewWithKey(t, views, TypeKey("depot"))
	if depot.Totals[0].Total != 200_000 {
		t.Fatalf("depot total = %d, want 200000 (a depot is included)", depot.Totals[0].Total)
	}
}

func TestTheGrandTotalSumsEveryAccountPerCurrency(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", testCurrency("EUR"), 10_000, nil, 0),
		testAccount(2, "savings", testCurrency("EUR"), 2_500, nil, 0),
		testAccount(3, "depot", testCurrency("USD"), 1_000, nil, 0),
	}
	total := GrandTotal(accounts, money.EUR)
	currencies := make([]money.Currency, 0, len(total))
	for _, entry := range total {
		currencies = append(currencies, entry.Currency)
	}
	if got, want := currencies, []money.Currency{money.EUR, testCurrency("USD")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("currencies = %v, want %v", got, want)
	}
	if total[0].Total != 12_500 {
		t.Fatalf("EUR total = %d, want 12500", total[0].Total)
	}
	if total[1].Total != 1_000 {
		t.Fatalf("USD total = %d, want 1000", total[1].Total)
	}
	if empty := GrandTotal(nil, money.EUR); len(empty) != 0 {
		t.Fatalf("empty grand total = %v, want empty", empty)
	}
}

func TestGroupKeyIDsAreStable(t *testing.T) {
	if got, want := TypeKey("checking").ID(), "type:checking"; got != want {
		t.Fatalf("type id = %q, want %q", got, want)
	}
	if got, want := CustomKey(7).ID(), "custom:7"; got != want {
		t.Fatalf("custom id = %q, want %q", got, want)
	}
}

func TestAGroupsNewCountIsTheSumOfItsAccounts(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", money.EUR, 0, nil, 2),
		testAccount(2, "checking", money.EUR, 0, nil, 3),
		simpleAccount(3, "savings"),
	}
	views := Group(accounts, nil, money.EUR)
	if got := viewWithKey(t, views, TypeKey("checking")).NewCount; got != 5 {
		t.Fatalf("checking new count = %d, want 5", got)
	}
	if got := viewWithKey(t, views, TypeKey("savings")).NewCount; got != 0 {
		t.Fatalf("savings new count = %d, want 0 (nothing new stays zero)", got)
	}
}

func TestACustomGroupsNewCountSumsTheAccountsMovedIntoIt(t *testing.T) {
	groupID := int64(10)
	accounts := []AccountItem{
		testAccount(1, "savings", money.EUR, 0, &groupID, 4),
		testAccount(2, "savings", money.EUR, 0, &groupID, 1),
	}
	views := Group(accounts, []model.AccountGroup{customGroup(10, "Reisen")}, money.EUR)
	if got, want := views[0].NewCount, int64(5); got != want {
		t.Fatalf("new count = %d, want %d", got, want)
	}
}

func TestOneAccountFollowsTheSignAndTheLoanRule(t *testing.T) {
	cases := []struct {
		name    string
		account AccountItem
		want    BalanceTone
	}{
		{"positive checking", testAccount(1, "checking", money.EUR, 500, nil, 0), BalanceTonePositive},
		{"zero checking", testAccount(1, "checking", money.EUR, 0, nil, 0), BalanceToneZero},
		{"negative checking", testAccount(1, "checking", money.EUR, -1_000, nil, 0), BalanceToneNegative},
		{"loan", testAccount(1, LoanType, money.EUR, -9_000_000, nil, 0), BalanceToneLoan},
	}
	for _, test := range cases {
		if got := BalanceToneFor([]AccountItem{test.account}, money.EUR); got != test.want {
			t.Fatalf("%s tone = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestATotalNegativeOnlyBecauseOfLoansIsALoanTone(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", money.EUR, 245_000, nil, 0),
		testAccount(2, LoanType, money.EUR, -9_650_000, nil, 0),
	}
	if got, want := BalanceToneFor(accounts, money.EUR), BalanceToneLoan; got != want {
		t.Fatalf("tone = %d, want %d", got, want)
	}
}

func TestAnOverdrawnTotalStaysAWarningEvenWithLoansInIt(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", money.EUR, -10_000, nil, 0),
		testAccount(2, LoanType, money.EUR, -100_000, nil, 0),
	}
	if got, want := BalanceToneFor(accounts, money.EUR), BalanceToneNegative; got != want {
		t.Fatalf("tone = %d, want %d", got, want)
	}
}

func TestAPositiveTotalOutweighingALoanIsPositive(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", money.EUR, 10_000, nil, 0),
		testAccount(2, LoanType, money.EUR, -5_000, nil, 0),
	}
	if got, want := BalanceToneFor(accounts, money.EUR), BalanceTonePositive; got != want {
		t.Fatalf("tone = %d, want %d", got, want)
	}
}

func TestCurrenciesAreJudgedSeparately(t *testing.T) {
	accounts := []AccountItem{
		testAccount(1, "checking", money.EUR, -1_000, nil, 0),
		testAccount(2, "checking", testCurrency("USD"), 10_000, nil, 0),
	}
	if got, want := BalanceToneFor(accounts, money.EUR), BalanceToneNegative; got != want {
		t.Fatalf("EUR tone = %d, want %d (the euro total ignores the positive dollar account)", got, want)
	}
	if got, want := BalanceToneFor(accounts, testCurrency("USD")), BalanceTonePositive; got != want {
		t.Fatalf("USD tone = %d, want %d (the dollar total ignores the negative euro account)", got, want)
	}
}

func TestOneBalanceHasAllFourTones(t *testing.T) {
	cases := []struct {
		name   string
		value  money.Money
		isLoan bool
		want   BalanceTone
	}{
		{"positive", money.Money{Minor: 500, Currency: money.EUR}, false, BalanceTonePositive},
		{"zero", money.Money{Minor: 0, Currency: money.EUR}, false, BalanceToneZero},
		{"negative", money.Money{Minor: -1_000, Currency: money.EUR}, false, BalanceToneNegative},
		{"loan", money.Money{Minor: -9_000_000, Currency: money.EUR}, true, BalanceToneLoan},
	}
	for _, test := range cases {
		if got := BalanceToneValue(test.value, test.isLoan); got != test.want {
			t.Fatalf("%s tone = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestANegativeListToneIsExactlyTheWarningFlag(t *testing.T) {
	fixtures := [][]AccountItem{
		{testAccount(1, "checking", money.EUR, -1_000, nil, 0)},
		{testAccount(1, LoanType, money.EUR, -9_000_000, nil, 0)},
		{
			testAccount(1, "checking", money.EUR, 50_000, nil, 0),
			testAccount(2, LoanType, money.EUR, -9_000_000, nil, 0),
		},
		{
			testAccount(1, "checking", money.EUR, -2_000, nil, 0),
			testAccount(2, LoanType, money.EUR, -9_000_000, nil, 0),
		},
		{
			testAccount(1, "credit_card", money.EUR, -30_000, nil, 0),
			testAccount(2, "savings", money.EUR, 100_000, nil, 0),
			testAccount(3, LoanType, money.EUR, -5_000_000, nil, 0),
		},
		{testAccount(1, "checking", money.EUR, 500, nil, 0)},
		{testAccount(1, "checking", money.EUR, 0, nil, 0)},
	}
	for _, accounts := range fixtures {
		warning := IsWarningBalance(accounts, money.EUR)
		negative := BalanceToneFor(accounts, money.EUR) == BalanceToneNegative
		if warning != negative {
			t.Fatalf("tone and warning flag disagree for %#v", accounts)
		}
	}
}

func TestAWarningFreeNegativeTotalIsNeverAWarning(t *testing.T) {
	loan := []AccountItem{testAccount(1, LoanType, money.EUR, -100, nil, 0)}
	if IsWarningBalance(loan, money.EUR) {
		t.Fatal("a loan-only total warns")
	}
	if BalanceToneFor(loan, money.EUR) == BalanceToneNegative {
		t.Fatal("a loan-only total is the negative tone")
	}
}

func TestGroupIDForMapsACustomGroupToItsIDAndATypeGroupToNil(t *testing.T) {
	groupID := GroupIDFor(CustomKey(10))
	if groupID == nil || *groupID != model.AccountGroupID(10) {
		t.Fatalf("custom group id = %v, want 10", groupID)
	}
	if groupID := GroupIDFor(TypeKey("checking")); groupID != nil {
		t.Fatalf("type group id = %v, want nil", groupID)
	}
}
