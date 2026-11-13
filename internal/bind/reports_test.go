package bind

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

type reportsFixture struct {
	session *shell.Session
	service *Reports
	router  *Router
	events  chan ReportsDTO
}

func newReportsFixture(t *testing.T) *reportsFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seedBindTransactions(t, appPaths)

	session, _, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	t.Cleanup(func() { _ = session.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	fixture := &reportsFixture{
		session: session,
		events:  make(chan ReportsDTO, 256),
	}
	fixture.service = NewReports(session, ReportsOptions{
		Emit: func(dto ReportsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("reports", fixture.service)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.service.ServiceShutdown()
	})

	session.Navigate("reports")
	fixture.wait(t, "the loaded reports", func(dto ReportsDTO) bool {
		return !dto.Loading && dto.HasReport
	})
	return fixture
}

func (f *reportsFixture) wait(t *testing.T, description string, predicate func(ReportsDTO) bool) ReportsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.events:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func TestReportsChangedEventEqualsTheReturnedDTO(t *testing.T) {
	fixture := newReportsFixture(t)

	returned, err := fixture.service.ChangeYear(-1)
	if err != nil {
		t.Fatalf("ChangeYear: %v", err)
	}
	event := fixture.wait(t, "the returned DTO's event", func(dto ReportsDTO) bool {
		return reflect.DeepEqual(dto, returned)
	})
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("event = %+v, returned = %+v", event, returned)
	}

	final := fixture.wait(t, "the loaded 2025 report", func(dto ReportsDTO) bool {
		return !dto.Loading && dto.Year == 2025 && dto.HasReport
	})
	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(state, final) {
		t.Fatalf("State = %+v, last event = %+v", state, final)
	}
}

func TestReportsBindingsNeverTakeAPath(t *testing.T) {
	actualNames := reportsParameterNames(t)
	typ := reflect.TypeOf(&Reports{})
	methods := make(map[string]bool, typ.NumMethod())
	for index := 0; index < typ.NumMethod(); index++ {
		method := typ.Method(index)
		methods[method.Name] = true
		allowed, listed := allowedReportsParameters[method.Name]
		names := actualNames[method.Name]
		if !listed && method.Type.NumIn() != 1 {
			t.Fatalf("%s takes arguments but is not in the allowed list", method.Name)
		}
		if len(names) != method.Type.NumIn()-1 {
			t.Fatalf("%s parameter names = %v, but reflection sees %d", method.Name, names, method.Type.NumIn()-1)
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			for _, forbidden := range []string{"path", "file", "dir", "folder", "document", "content"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s parameter %q could carry a path", method.Name, name)
				}
			}
		}
		if !listed {
			continue
		}
		if len(allowed) != len(names) {
			t.Fatalf("%s parameters = %v, want %v", method.Name, names, allowed)
		}
		for position, name := range allowed {
			if names[position] != name {
				t.Fatalf("%s parameter %d = %q, want %q", method.Name, position, names[position], name)
			}
			parameter := method.Type.In(position + 1)
			switch parameter.Kind() {
			case reflect.Int, reflect.Bool, reflect.String:
			default:
				t.Fatalf("%s parameter %q has type %s", method.Name, name, parameter)
			}
		}
	}

	for _, name := range []string{"ExportCSV", "ExportReportPDF", "ExportTaxPDF", "ExportTaxXLSX", "PrintTax"} {
		if !methods[name] {
			t.Fatalf("%s is not exported", name)
		}
		if allowed, listed := allowedReportsParameters[name]; listed && len(allowed) > 0 {
			t.Fatalf("%s must take no arguments", name)
		}
		if len(actualNames[name]) != 0 {
			t.Fatalf("%s takes %v", name, actualNames[name])
		}
	}
}

var allowedReportsParameters = map[string][]string{
	"ChangeYear":         {"delta"},
	"SetCustom":          {"custom"},
	"ApplyRange":         {"from", "to"},
	"SetMissingOnly":     {"only"},
	"AcceptSuggestion":   {"id"},
	"DismissSuggestion":  {"id"},
	"MarkNotTaxRelevant": {"id"},
	"ResetTaxAutomatic":  {"id"},
	"OpenCategory":       {"categoryID"},
}

// reportsParameterNames reads the declared names from the service source.
// Reflection exposes a method's parameter types but not their names.
func reportsParameterNames(t *testing.T) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "reports.go", nil, 0)
	if err != nil {
		t.Fatalf("parse reports.go: %v", err)
	}
	names := make(map[string][]string)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		if receiverType(function.Recv.List[0].Type) != "Reports" {
			continue
		}
		names[function.Name.Name] = parameterNamesOf(function.Type.Params)
	}
	return names
}

func bindReportDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func bindReportTransaction(
	t *testing.T,
	id int64,
	minor int64,
	documentCount int,
	override *bool,
) model.TransactionItem {
	t.Helper()
	iban := "DE89370400440532013000"
	partner := "Beispiel Apotheke"
	categoryID := model.CategoryID(7)
	categoryName := "Health"
	categoryColor := "#123456"
	return model.TransactionItem{
		ID:            model.TransactionID(id),
		AccountID:     1,
		AccountIBAN:   &iban,
		Date:          bindReportDate(t, 2026, time.March, 1),
		Amount:        money.Money{Minor: minor, Currency: money.EUR},
		Currency:      money.EUR,
		PartnerName:   &partner,
		CategoryID:    &categoryID,
		CategoryName:  &categoryName,
		CategoryColor: &categoryColor,
		IsTaxRelevant: true,
		DocumentCount: documentCount,
		TaxSource:     model.TaxSourceManual,
		TaxOverride:   override,
	}
}

func bindReportBookings(t *testing.T) report.TaxBookings {
	t.Helper()
	overrideFalse := false
	withReceipt := bindReportTransaction(t, 1, -2_000, 1, &overrideFalse)
	withoutReceipt := bindReportTransaction(t, 2, -3_000, 0, nil)
	categoryID := model.CategoryID(7)
	return report.TaxBookings{
		From:     bindReportDate(t, 2026, time.January, 1),
		To:       bindReportDate(t, 2026, time.December, 31),
		Currency: money.EUR,
		DeductionGroups: []report.TaxGroup{{
			CategoryID: &categoryID,
			Name:       "Health",
			Color:      "#123456",
			Bookings:   []model.TransactionItem{withReceipt, withoutReceipt},
			Income:     money.Money{Minor: 0, Currency: money.EUR},
			Expenses:   money.Money{Minor: 5_000, Currency: money.EUR},
		}},
		BookingCount: 2,
	}
}

func TestTaxRowsFlattenSectionsGroupsAndTheMissingReceiptsFilter(t *testing.T) {
	all := reportsTaxRows(reportValuePointer(bindReportBookings(t)), false, i18n.En)
	if all[0].Kind != "section" || all[0].Title != "Deductions" {
		t.Fatalf("section row = %+v", all[0])
	}
	if all[1].Kind != "group" || all[1].Title != "Health" {
		t.Fatalf("group row = %+v", all[1])
	}
	if all[1].Summary != "2 bookings · -€50.00" {
		t.Fatalf("group summary = %q", all[1].Summary)
	}
	if len(all) != 4 {
		t.Fatalf("rows = %d, want section + group + two bookings", len(all))
	}
	if all[2].Kind != "booking" ||
		all[2].AccountText != "…3000" ||
		all[2].AmountText != "-€20.00" ||
		!all[2].HasReceipt ||
		all[2].MissingReceipt ||
		!all[2].CanReset {
		t.Fatalf("receipt booking row = %+v", all[2])
	}
	if all[3].CanReset {
		t.Fatal("a booking without an override can reset")
	}

	missing := reportsTaxRows(reportValuePointer(bindReportBookings(t)), true, i18n.En)
	if len(missing) != 3 {
		t.Fatalf("missing rows = %d, want section + group + one booking", len(missing))
	}
	if missing[2].ID != 2 || !missing[2].MissingReceipt {
		t.Fatalf("missing booking row = %+v", missing[2])
	}
	if missing[1].Summary != "2 bookings · -€50.00" {
		t.Fatalf("group totals changed with the filter: %q", missing[1].Summary)
	}
}

func reportValuePointer(value report.TaxBookings) *report.TaxBookings {
	return &value
}

func TestRecurringTotalsArePerCurrencyWithThePrimaryFirst(t *testing.T) {
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("USD currency: %v", err)
	}
	payments := []recurring.RecurringPayment{
		bindRecurringPayment(t, "Spotify", money.EUR, -999, false),
		bindRecurringPayment(t, "Rent", usd, -1_200, false),
		bindRecurringPayment(t, "Salary", money.EUR, 300_000, false),
		bindRecurringPayment(t, "Transfer", money.EUR, -50_000, true),
	}

	totals := reportsRecurringCostTotals(payments, usd)
	want := []money.CurrencyTotal{{Currency: usd, Total: 14_400}, {Currency: money.EUR, Total: 11_988}}
	if !reflect.DeepEqual(totals, want) {
		t.Fatalf("totals with USD primary = %+v, want %+v", totals, want)
	}

	totals = reportsRecurringCostTotals(payments, money.EUR)
	want = []money.CurrencyTotal{{Currency: money.EUR, Total: 11_988}, {Currency: usd, Total: 14_400}}
	if !reflect.DeepEqual(totals, want) {
		t.Fatalf("totals with EUR primary = %+v, want %+v", totals, want)
	}
	if got := reportsYearlyCostText(totals, i18n.En); got != "€119.88\n$144.00" {
		t.Fatalf("yearly text = %q", got)
	}
	if got := reportsMonthlyCostText(totals, i18n.En); got != "€9.99\n$12.00" {
		t.Fatalf("monthly text = %q", got)
	}
}

func bindRecurringPayment(
	t *testing.T,
	name string,
	currency money.Currency,
	minor int64,
	transfer bool,
) recurring.RecurringPayment {
	t.Helper()
	category := "Media"
	return recurring.RecurringPayment{
		Name:          name,
		Cadence:       recurring.Monthly,
		TypicalAmount: money.Money{Minor: minor, Currency: currency},
		FixedAmount:   true,
		LastDate:      bindReportDate(t, 2026, time.September, 10),
		NextDate:      bindReportDate(t, 2026, time.October, 10),
		Occurrences:   4,
		CategoryName:  &category,
		Currency:      currency,
		IsTransfer:    transfer,
	}
}

func TestMonthlyRowsUseTheCoreMonthNamesAndZeroTones(t *testing.T) {
	taxReport := report.TaxReport{
		Year:          2026,
		From:          bindReportDate(t, 2026, time.January, 1),
		To:            bindReportDate(t, 2026, time.March, 31),
		TotalIncome:   money.Money{Minor: 10_000, Currency: money.EUR},
		TotalExpenses: money.Money{Minor: 5_000, Currency: money.EUR},
		NetIncome:     money.Money{Minor: 5_000, Currency: money.EUR},
		MonthlyBreakdown: []report.MonthlyData{
			{
				Year:     2026,
				Month:    1,
				Income:   money.Money{Minor: 0, Currency: money.EUR},
				Expenses: money.Money{Minor: 0, Currency: money.EUR},
			},
			{
				Year:     2026,
				Month:    2,
				Income:   money.Money{Minor: 10_000, Currency: money.EUR},
				Expenses: money.Money{Minor: 5_000, Currency: money.EUR},
			},
		},
		Currency:           money.EUR,
		TransferTotal:      money.Money{Minor: 0, Currency: money.EUR},
		CategoryShares:     []report.CategoryShare{},
		TopVendors:         []report.NamedAmount{},
		ExpensesByCategory: []report.NamedAmount{},
	}

	rows := reportsMonthlyRows(&taxReport, i18n.De)
	if rows[0].Month != "Januar 2026" {
		t.Fatalf("first month = %q", rows[0].Month)
	}
	if !rows[0].IncomeZero || !rows[0].NetZero || rows[0].NetTone != "zero" {
		t.Fatalf("zero month = %+v", rows[0])
	}
	if rows[1].Month != "Februar 2026" {
		t.Fatalf("second month = %q", rows[1].Month)
	}
	if rows[1].IncomeText != "100,00\u00a0€" {
		t.Fatalf("income text = %q", rows[1].IncomeText)
	}
	if rows[1].NetText != "+50,00\u00a0€" || rows[1].NetTone != "positive" {
		t.Fatalf("net row = %+v", rows[1])
	}
	if got := reportsAverageMonthly(&taxReport); got.Minor != 5_000 {
		t.Fatalf("average = %+v", got)
	}
	if got := reportsHighestMonth(&taxReport, i18n.En); got != "February €50.00" {
		t.Fatalf("highest month = %q", got)
	}
}

func TestAnEmptyReportsDTOSendsArraysNotNull(t *testing.T) {
	dto := reportsDTO(shell.ReportsPageState{})
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), ":null") {
		t.Fatalf("the reports DTO carries a null list: %s", data)
	}
}
