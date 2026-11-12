package dashboardview

import (
	"reflect"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func cloneTestState() DashboardState {
	month := dashboard.NewYearMonth(2026, 10)
	loaded := dashboard.NewYearMonth(2026, 9)
	other := dashboard.NewYearMonth(2026, 7)
	message := "something went wrong"
	transferNote := "Transferred €20.00"
	chartEmpty := "No bookings"
	carry := money.Money{Minor: 10_000, Currency: money.EUR}
	movements := money.Money{Minor: -2_500, Currency: money.EUR}
	balanceDate, _ := civil.New(2026, time.September, 30)
	category := model.CategoryID(7)
	partner := "Bakery"

	return DashboardState{
		SelectedMonth: month,
		LoadedMonth:   &loaded,
		EarliestMonth: other,
		CurrentMonth:  month,
		Months: []dashboard.YearMonth{
			month,
			loaded,
			other,
		},
		IsLoading:   true,
		Error:       &message,
		HasAccounts: true,
		Language:    i18n.De,
		View: &DashboardView{
			SelectedMonth:           month,
			LoadedMonth:             loaded,
			EarliestMonth:           other,
			CurrentMonth:            month,
			IsCurrentMonth:          true,
			PreviousEnabled:         true,
			NextEnabled:             false,
			MonthLabel:              "Oktober 2026",
			Currency:                money.EUR,
			TotalBalance:            money.Money{Minor: 50_000, Currency: money.EUR},
			AvailableBalance:        money.Money{Minor: 40_000, Currency: money.EUR},
			LoanBalance:             money.Money{Minor: -10_000, Currency: money.EUR},
			HasLoans:                true,
			CarryForward:            &carry,
			OtherMovements:          &movements,
			BalanceDate:             &balanceDate,
			OtherBalances:           []money.Money{{Minor: 12_345, Currency: money.EUR}},
			MonthlyIncome:           money.Money{Minor: 200_000, Currency: money.EUR},
			MonthlyExpenses:         money.Money{Minor: 12_000, Currency: money.EUR},
			UncategorizedCount:      2,
			TaxTotal:                money.Money{Minor: 5_000, Currency: money.EUR},
			ExpensesByCategory:      []CategorySliceView{{Label: "Food", Amount: carry, Color: "#FF5722", CategoryID: &category, Fraction: 0.5}},
			ExpenseRows:             []CategoryRowView{{Label: "Food", PercentText: "50%", AmountText: "€10.00", Color: "#FF5722", CategoryID: &category, IsMore: false, Fraction: 0.5}},
			TaxDeductionsByCategory: []CategorySliceView{{Label: "Donations", Amount: carry, Color: "#3BC7B0", CategoryID: &category, Fraction: 1}},
			TaxRows:                 []CategoryRowView{{Label: "Donations", PercentText: "100%", AmountText: "€10.00", Color: "#3BC7B0", CategoryID: &category, IsMore: false, Fraction: 1}},
			MonthBars: []MonthBarView{{
				Year: 2026, Month: 10, Label: "Oct", Title: "October 2026",
				Income: carry, Expenses: carry, IncomeText: "+€10.00", ExpensesText: "-€10.00",
				IncomeFraction: 0.5, ExpensesFraction: 0.5, Partial: true,
			}},
			MonthProgress:      0.5,
			TransfersThisMonth: money.Money{Minor: 2_000, Currency: money.EUR},
			Budgets: []BudgetRowView{{
				CategoryID: category, Name: "Food", Color: "#FF5722",
				Limit: carry, Spent: carry, Ratio: 0.5, IsOver: false, Warning: true,
				AmountText: "€10.00 of €10.00", RestText: "€0.00 left",
			}},
			Upcoming: []UpcomingRowView{{
				Name: "Spotify", NextDate: balanceDate, DateText: "30.09.2026",
				Amount: movements, AmountText: "-€25.00", Meta: "Monthly", IsIncome: false,
			}},
			RecentTransactions: []TransactionRowView{{
				ID: 11, Date: balanceDate, DateText: "30.09.2026", Title: "Bakery",
				Meta: "Food", Amount: movements, AmountText: "-€25.00", IsExpense: true,
				Partner: &partner,
			}},
			StatementLines: []StatementLineView{{
				Label: "Carried over", AmountText: "€10.00", Kind: StatementLineKindCarry,
			}},
			AvailableTitle:       "Available",
			AvailableValue:       "€40.00",
			AvailableSubtitle:    "Loans …",
			AvailableTone:        BalanceTonePositive,
			IncomeTitle:          "Income this month",
			IncomeValue:          "€2,000.00",
			ExpensesTitle:        "Expenses this month",
			ExpensesValue:        "€120.00",
			TaxTitle:             "Tax deductible",
			TaxValue:             "€50.00",
			TaxHint:              "No tax bookings",
			PendingTitle:         "Pending tasks",
			PendingValue:         "2",
			PendingSubtitle:      "2 uncategorized · 0 unlinked docs",
			PendingLines:         []string{"2 uncategorized", "0 unlinked docs"},
			PendingEnabled:       true,
			ExpensesSectionTitle: "Expenses by category",
			ExpensesEmptyText:    "No expenses",
			TransfersNote:        &transferNote,
			ChartTitle:           "Income vs expenses",
			ChartEmptyText:       &chartEmpty,
			ChartAxisTop:         "€2,000",
			ChartAxisMid:         "€1,000",
			ChartAxisZero:        "€0",
			IncomeLegend:         "Income",
			ExpensesLegend:       "Expenses",
			PartialMonthNote:     "This month so far",
			BudgetsTitle:         "Budgets",
			UpcomingTitle:        "Upcoming",
			RecentTitle:          "Recent",
			RecentEmptyText:      "No transactions",
			ViewAllLabel:         "View all",
		},
	}
}

// TestCloneIsDeep mutates every slice and every pointer reachable from the
// clone, then loads into it. The independently built original must not change.
func TestCloneIsDeep(t *testing.T) {
	state := cloneTestState()
	want := cloneTestState()

	clone := state.Clone()
	mutateEveryMutable(reflect.ValueOf(&clone).Elem())
	clone.BeginLoad(dashboard.NewYearMonth(2026, 8), mustCloneDate(t, 2026, time.November, 8))

	if !reflect.DeepEqual(state, want) {
		t.Fatalf("mutating the clone changed the original:\n original = %#v\n want     = %#v", state, want)
	}
	if reflect.DeepEqual(clone, want) {
		t.Fatal("the clone was not mutated, so the test proves nothing")
	}
}

func mustCloneDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

var cloneDateType = reflect.TypeOf(civil.Date{})

// mutateEveryMutable changes every string, number, pointer target and slice
// element reachable from v, so a shallow copy shares the change.
func mutateEveryMutable(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(v.String() + " changed")
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.CanSet() {
			v.SetInt(v.Int() + 1)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.CanSet() {
			v.SetUint(v.Uint() + 1)
		}
	case reflect.Float32, reflect.Float64:
		if v.CanSet() {
			v.SetFloat(v.Float() + 1)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if v.Elem().Type() == cloneDateType {
			// civil.Date has no exported fields; replacing the whole value is
			// the only mutation, and it still catches a shared pointer.
			v.Elem().Set(reflect.Zero(cloneDateType))
			return
		}
		mutateEveryMutable(v.Elem())
	case reflect.Interface:
		if !v.IsNil() {
			mutateEveryMutable(v.Elem())
		}
	case reflect.Struct:
		for index := 0; index < v.NumField(); index++ {
			mutateEveryMutable(v.Field(index))
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		for index := 0; index < v.Len(); index++ {
			mutateEveryMutable(v.Index(index))
		}
	}
}
