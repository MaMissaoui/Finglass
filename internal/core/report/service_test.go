package report

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func TestTaxReportDataClass(t *testing.T) {
	report := EmptyTaxReport(2026)
	report.TotalIncome = eur(3_840_000)
	report.TotalExpenses = eur(2_580_000)
	report.NetIncome = eur(1_260_000)
	report.ExpensesByCategory = []NamedAmount{{Name: "Groceries", Amount: eur(624_000)}}
	report.TaxRelevantExpenses = []NamedAmount{{Name: "Transport", Amount: eur(360_000)}}
	report.TransactionCount = 100

	if report.Year != 2026 {
		t.Fatalf("year = %d, want 2026", report.Year)
	}
	if report.TotalIncome != eur(3_840_000) {
		t.Fatalf("total income = %v, want %v", report.TotalIncome, eur(3_840_000))
	}
	if report.NetIncome != eur(1_260_000) {
		t.Fatalf("net income = %v, want %v", report.NetIncome, eur(1_260_000))
	}
}

func TestTaxReportEmptyFactory(t *testing.T) {
	report := EmptyTaxReport(2025)
	if report.Year != 2025 {
		t.Fatalf("year = %d, want 2025", report.Year)
	}
	if report.TotalIncome != eur(0) {
		t.Fatalf("total income = %v, want 0", report.TotalIncome)
	}
	if report.TotalExpenses != eur(0) {
		t.Fatalf("total expenses = %v, want 0", report.TotalExpenses)
	}
	if report.TransactionCount != 0 {
		t.Fatalf("transaction count = %d, want 0", report.TransactionCount)
	}
}

func TestMonthlyReportDataClass(t *testing.T) {
	report := MonthlyReport{
		Year:             2026,
		Month:            10,
		TotalIncome:      eur(320_000),
		TotalExpenses:    eur(210_000),
		NetAmount:        eur(110_000),
		ByCategory:       []NamedAmount{{Name: "Groceries", Amount: eur(50_000)}},
		TopExpenses:      []ExpenseItem{},
		TransactionCount: 50,
	}
	if report.Year != 2026 {
		t.Fatalf("year = %d, want 2026", report.Year)
	}
	if report.Month != 10 {
		t.Fatalf("month = %d, want 10", report.Month)
	}
}

func TestCategoryTrendDataClass(t *testing.T) {
	trend, err := decimal.Parse("4.8")
	if err != nil {
		t.Fatalf("parse the trend: %v", err)
	}
	value := CategoryTrend{
		CategoryID:   model.CategoryID(1),
		CategoryName: "Groceries",
		MonthlyData: []MonthlyAmount{
			{Year: 2026, Month: 1, Amount: eur(50_000)},
			{Year: 2026, Month: 2, Amount: eur(55_000)},
		},
		AverageMonthly: eur(52_500),
		Trend:          trend,
	}
	if value.CategoryName != "Groceries" {
		t.Fatalf("category name = %q, want Groceries", value.CategoryName)
	}
	if len(value.MonthlyData) != 2 {
		t.Fatalf("monthly data length = %d, want 2", len(value.MonthlyData))
	}
}

func TestTaxReportIncludesCategorizedTransactionsRegression(t *testing.T) {
	d := reportFixture(t)
	report, err := GenerateTaxReport(context.Background(), d, 2026, i18n.En)
	if err != nil {
		t.Fatalf("GenerateTaxReport: %v", err)
	}

	if report.TransactionCount != 3 {
		t.Fatalf("transaction count = %d, want 3", report.TransactionCount)
	}
	if report.TotalIncome != eur(300_000) {
		t.Fatalf("total income = %v, want 300000", report.TotalIncome)
	}
	if report.TotalExpenses != eur(29_000) {
		t.Fatalf("total expenses = %v, want 29000", report.TotalExpenses)
	}
	if report.NetIncome != eur(271_000) {
		t.Fatalf("net income = %v, want 271000", report.NetIncome)
	}
	want := []NamedAmount{{Name: "Arbeitsmittel", Amount: eur(25_000)}}
	if !slices.Equal(report.TaxRelevantExpenses, want) {
		t.Fatalf("tax relevant expenses = %v, want %v", report.TaxRelevantExpenses, want)
	}
	if report.TaxRelevantTotal() != eur(25_000) {
		t.Fatalf("tax relevant total = %v, want 25000", report.TaxRelevantTotal())
	}
	if report.MonthlyBreakdown[1].Expenses != eur(25_000) {
		t.Fatalf("february expenses = %v, want 25000", report.MonthlyBreakdown[1].Expenses)
	}
	if report.MonthlyBreakdown[1].Income != eur(300_000) {
		t.Fatalf("february income = %v, want 300000", report.MonthlyBreakdown[1].Income)
	}
	if report.CategoriesUsed != 2 {
		t.Fatalf("categories used = %d, want 2", report.CategoriesUsed)
	}
}

func TestCategoryTrendAveragesOverTheRequestedMonths(t *testing.T) {
	d := reportFixture(t)
	all, err := categories.List(context.Background(), d)
	if err != nil {
		t.Fatalf("categories.List: %v", err)
	}
	var food model.CategoryID
	found := false
	for _, category := range all {
		if category.Name == "Food" {
			food = category.ID
			found = true
		}
	}
	if !found {
		t.Fatal("the category Food exists")
	}
	trend, err := GetCategoryTrends(
		context.Background(), d, food, 3, testDate(t, 2026, time.March, 15), i18n.En,
	)
	if err != nil {
		t.Fatalf("GetCategoryTrends: %v", err)
	}
	months := make([]uint8, 0, len(trend.MonthlyData))
	for _, month := range trend.MonthlyData {
		months = append(months, month.Month)
	}
	if !slices.Equal(months, []uint8{1, 2, 3}) {
		t.Fatalf("months = %v, want [1 2 3]", months)
	}
	if trend.MonthlyData[0].Amount != eur(4_000) {
		t.Fatalf("january = %v, want 4000", trend.MonthlyData[0].Amount)
	}
	if trend.AverageMonthly != eur(1_333) {
		t.Fatalf("average = %v, want 1333", trend.AverageMonthly)
	}
}

func TestARefundLowersTheTaxRelevantExpensesToTheTaxBookingsDeductionTotal(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -10_000).Category(1, "Handwerk", "").TaxRelevant(),
		item(2, 3_000).Date(testDate(t, 2026, time.June, 2)).Category(1, "Handwerk", "").TaxRelevant(),
	})
	report := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	amount, ok := categoryAmount(report.TaxRelevantExpenses, "Handwerk")
	if !ok || amount != eur(7_000) {
		t.Fatalf("Handwerk = %v (%t), want 7000; the refund lowers its category", amount, ok)
	}
	bookings := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)
	if bookings.DeductionTotal() != report.TaxRelevantTotal() {
		t.Fatalf(
			"deduction total = %v, want %v",
			bookings.DeductionTotal(), report.TaxRelevantTotal(),
		)
	}
}

func TestATaxRelevantIncomeCategoryIsNotAmongTheTaxRelevantExpenses(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, 50_000).Category(2, "Vermietung", "").TaxRelevant(),
		item(2, -5_000).Date(testDate(t, 2026, time.June, 2)).Category(2, "Vermietung", "").TaxRelevant(),
	})
	report := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	if len(report.TaxRelevantExpenses) != 0 {
		t.Fatalf("tax relevant expenses = %v, want empty; money out on a net-income category is no deduction",
			report.TaxRelevantExpenses)
	}
	if report.TaxRelevantTotal() != eur(0) {
		t.Fatalf("tax relevant total = %v, want 0", report.TaxRelevantTotal())
	}
}

func TestPaypalPurchasesGroupByTheShopNamedInThePurpose(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -1_000).
			Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
			Purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
		item(2, -2_000).
			Date(testDate(t, 2026, time.June, 2)).
			Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
			Purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
		item(3, -4_000).
			Date(testDate(t, 2026, time.June, 3)).
			Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
			Purpose("Ihr Einkauf bei Anderer Shop GmbH"),
		item(4, -3_000).Date(testDate(t, 2026, time.June, 4)).Partner("Beispiel Inc."),
	})
	vendors := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	).TopVendors

	want := []NamedAmount{
		{Name: "PayPal · Anderer Shop GmbH", Amount: eur(4_000)},
		{Name: "PayPal · Beispiel Shop GmbH", Amount: eur(3_000)},
		{Name: "Beispiel Inc.", Amount: eur(3_000)},
	}
	if !slices.Equal(vendors, want) {
		t.Fatalf("vendors = %v, want %v", vendors, want)
	}
}

func TestVendorNamesGroupCaseAndWhitespaceInsensitively(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -1_000).Partner("BEISPIEL MARKT"),
		item(2, -2_000).Date(testDate(t, 2026, time.June, 2)).Partner("Beispiel Markt"),
		item(3, -500).Date(testDate(t, 2026, time.June, 3)).Partner("Beispiel  Markt   "),
	})
	vendors := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	).TopVendors

	want := []NamedAmount{{Name: "Beispiel Markt", Amount: eur(3_500)}}
	if !slices.Equal(vendors, want) {
		t.Fatalf("vendors = %v, want %v; the spellings are one vendor, shown with the most-used spelling", vendors, want)
	}
}

func TestASpellingTieShowsTheMostRecentBookingsSpelling(t *testing.T) {
	laterBeispiel := itemsOf([]itemBuilder{
		item(1, -1_000).Partner("BEISPIEL MARKT"),
		item(2, -2_000).Date(testDate(t, 2026, time.June, 2)).Partner("Beispiel Markt"),
	})
	laterUpper := itemsOf([]itemBuilder{
		item(1, -1_000).Date(testDate(t, 2026, time.June, 2)).Partner("BEISPIEL MARKT"),
		item(2, -2_000).Partner("Beispiel Markt"),
	})

	got := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		laterBeispiel,
		money.EUR,
		i18n.En,
	).TopVendors
	if len(got) == 0 || got[0].Name != "Beispiel Markt" {
		t.Fatalf("first vendor = %v, want Beispiel Markt", got)
	}
	got = BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		laterUpper,
		money.EUR,
		i18n.En,
	).TopVendors
	if len(got) == 0 || got[0].Name != "BEISPIEL MARKT" {
		t.Fatalf("first vendor = %v, want BEISPIEL MARKT", got)
	}
}

func TestAPaypalCardBookingAndAMarkerBookingGroupUnderOneShopIgnoringCase(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -1_000).Purpose("PAYPAL *BEISPIELSHOP"),
		item(2, -2_000).
			Date(testDate(t, 2026, time.June, 2)).
			Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
			Purpose("Ihr Einkauf bei Beispielshop"),
	})
	vendors := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	).TopVendors

	want := []NamedAmount{{Name: "PayPal · Beispielshop", Amount: eur(3_000)}}
	if !slices.Equal(vendors, want) {
		t.Fatalf("vendors = %v, want %v; the card descriptor and the marker name the same shop", vendors, want)
	}
}

func TestAPaypalBookingWhosePurposeHasNoShopStaysUnderItsDescription(t *testing.T) {
	partner := "PayPal Europe S.a.r.l. et Cie S.C.A"
	transactions := itemsOf([]itemBuilder{
		item(1, -1_500).Partner(partner).Purpose("PP.1.PP . 1234"),
		item(2, -3_000).
			Date(testDate(t, 2026, time.June, 2)).
			Partner(partner).
			Purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
	})
	vendors := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	).TopVendors

	want := []NamedAmount{
		{Name: "PayPal · Beispiel Shop GmbH", Amount: eur(3_000)},
		{Name: partner, Amount: eur(1_500)},
	}
	if !slices.Equal(vendors, want) {
		t.Fatalf("vendors = %v, want %v; the shopless PayPal booking keeps its raw description", vendors, want)
	}
}

func TestMonthlyReportTitlesAPaypalTopExpenseWithTheShop(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -25_000).
			Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
			Purpose("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH").
			Category(5, "Online shopping", ""),
		item(2, -1_000).Partner("Beispiel Bäckerei").Purpose("Brötchen"),
	})
	report := MonthlyReportOf(2026, 6, transactions, money.EUR, i18n.En)
	if len(report.TopExpenses) == 0 || report.TopExpenses[0].Description != "PayPal · Beispiel Shop GmbH" {
		t.Fatalf("top expenses = %v, want the shop title", report.TopExpenses)
	}
}

func TestDashboardAndReportAgreeAndLeaveTheTransferOutOfTheTotals(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -50_000).
			Partner("Erika Musterfrau").
			Category(9, "Own transfers", "#4CAF50").
			Transfer(),
		item(2, 50_000).
			Partner("Erika Musterfrau").
			Category(9, "Own transfers", "#4CAF50").
			Transfer(),
		item(3, -4_000).
			Date(testDate(t, 2026, time.June, 16)).
			Partner("BEISPIEL MARKT").
			Category(10, "Groceries", "#FF5722"),
		item(4, 100_000).Date(testDate(t, 2026, time.June, 17)).Partner("Erika Mustermann"),
	})
	report := BuildReport(
		testDate(t, 2026, time.June, 1),
		testDate(t, 2026, time.June, 30),
		transactions,
		money.EUR,
		i18n.En,
	)

	if report.TotalIncome != eur(100_000) {
		t.Fatalf("total income = %v, want 100000", report.TotalIncome)
	}
	if report.TotalExpenses != eur(4_000) {
		t.Fatalf("total expenses = %v, want 4000", report.TotalExpenses)
	}
	if report.NetIncome != eur(96_000) {
		t.Fatalf("net income = %v, want 96000", report.NetIncome)
	}
	if report.TransferTotal != eur(50_000) {
		t.Fatalf("transfer total = %v, want 50000", report.TransferTotal)
	}
	vendorNames := make([]string, 0, len(report.TopVendors))
	for _, vendor := range report.TopVendors {
		vendorNames = append(vendorNames, vendor.Name)
	}
	if !slices.Equal(vendorNames, []string{"BEISPIEL MARKT"}) {
		t.Fatalf("top vendors = %v, want [BEISPIEL MARKT]; the transfer partner is not a vendor", vendorNames)
	}
	categoryNames := make([]string, 0, len(report.ExpensesByCategory))
	for _, category := range report.ExpensesByCategory {
		categoryNames = append(categoryNames, category.Name)
	}
	if !slices.Equal(categoryNames, []string{"Groceries"}) {
		t.Fatalf("expenses by category = %v, want [Groceries]", categoryNames)
	}
	month := single(t, report.MonthlyBreakdown)
	if month.Income != eur(100_000) {
		t.Fatalf("month income = %v, want 100000", month.Income)
	}
	if month.Expenses != eur(4_000) {
		t.Fatalf("month expenses = %v, want 4000", month.Expenses)
	}
	share := single(t, report.CategoryShares)
	if share.Name != "Groceries" {
		t.Fatalf("share name = %q, want Groceries", share.Name)
	}
	if share.CategoryID == nil || *share.CategoryID != model.CategoryID(10) {
		t.Fatalf("share category = %v, want 10", share.CategoryID)
	}
	if share.Amount != eur(4_000) {
		t.Fatalf("share amount = %v, want 4000", share.Amount)
	}
	if share.Color != "#FF5722" {
		t.Fatalf("share color = %q, want #FF5722", share.Color)
	}
}

func TestTaxRelevantTotalsAndBudgetsStayAsTheyWere(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -50_000).Category(9, "Own transfers", "#4CAF50").Transfer(),
		item(2, 50_000).Category(9, "Own transfers", "#4CAF50").Transfer(),
		item(3, -3_000).Category(11, "Transport", "#2196F3").TaxRelevant(),
	})
	report := BuildReport(
		testDate(t, 2026, time.June, 1),
		testDate(t, 2026, time.June, 30),
		transactions,
		money.EUR,
		i18n.En,
	)
	if report.TaxRelevantTotal() != eur(3_000) {
		t.Fatalf("tax relevant total = %v, want 3000", report.TaxRelevantTotal())
	}
	want := []NamedAmount{{Name: "Transport", Amount: eur(3_000)}}
	if !slices.Equal(report.TaxRelevantExpenses, want) {
		t.Fatalf("tax relevant expenses = %v, want %v", report.TaxRelevantExpenses, want)
	}
}

func TestReportsCountBookingsInOtherCurrenciesInsteadOfAddingThem(t *testing.T) {
	d := testDatabase(t)
	eurAccount := addAccount(t, d, money.EUR)
	usdAccount := addAccount(t, d, usd(0).Currency)
	addTransaction(
		t, d, eurAccount, testDate(t, 2026, time.March, 1), eur(-3_000),
		strPtr("REWE"), nil, nil,
	)
	addTransaction(
		t, d, usdAccount, testDate(t, 2026, time.March, 2), usd(-7_000),
		strPtr("Walmart"), nil, nil,
	)

	report := BuildTaxReport(2026, allItems(t, d), money.EUR, i18n.En)
	if report.Currency != money.EUR {
		t.Fatalf("currency = %v, want EUR", report.Currency)
	}
	if report.TotalExpenses != eur(3_000) {
		t.Fatalf("total expenses = %v, want 3000", report.TotalExpenses)
	}
	if report.OtherCurrencyBookings != 1 {
		t.Fatalf("other currency bookings = %d, want 1", report.OtherCurrencyBookings)
	}
	if report.TransactionCount != 1 {
		t.Fatalf("transaction count = %d, want 1", report.TransactionCount)
	}
}

func TestPrimaryCurrencySettingSwitchesWhatTheReportsTotal(t *testing.T) {
	d := testDatabase(t)
	eurAccount := addAccount(t, d, money.EUR)
	usdAccount := addAccount(t, d, usd(0).Currency)
	addTransaction(
		t, d, eurAccount, testDate(t, 2026, time.June, 2), eur(200_000),
		strPtr("Employer"), nil, nil,
	)
	addTransaction(
		t, d, usdAccount, testDate(t, 2026, time.June, 3), usd(80_000),
		strPtr("US client"), nil, nil,
	)
	if err := settings.Set(context.Background(), d, money.PrimaryCurrencyKey, "USD"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	report, err := GenerateTaxReport(context.Background(), d, 2026, i18n.En)
	if err != nil {
		t.Fatalf("GenerateTaxReport: %v", err)
	}
	if report.Currency.String() != "USD" {
		t.Fatalf("currency = %q, want USD", report.Currency.String())
	}
	if report.TotalIncome != usd(80_000) {
		t.Fatalf("total income = %v, want 80000 USD", report.TotalIncome)
	}
	if report.OtherCurrencyBookings != 1 {
		t.Fatalf("other currency bookings = %d, want 1", report.OtherCurrencyBookings)
	}
}

func TestTheReportCountsARuleFlaggedBookingOfANonTaxCategoryAmongTheTaxRelevantExpenses(t *testing.T) {
	d := testDatabase(t)
	account := addAccount(t, d, money.EUR)
	category := addCategory(t, d, "Groceries", "#FF5722", false, false)
	addTransaction(
		t, d, account, testDate(t, 2026, time.June, 14), eur(-10_000),
		strPtr("Beispiel Shop"), nil, &category,
	)
	taxFlag := true
	if _, err := rulesrepo.Create(context.Background(), d, model.NewRule{
		Pattern:     "Beispiel",
		CategoryID:  category,
		Priority:    0,
		AutoApply:   true,
		MatchField:  model.RuleFieldAny,
		Direction:   model.RuleDirectionAny,
		TaxRelevant: &taxFlag,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}

	report := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		allItems(t, d),
		money.EUR,
		i18n.En,
	)
	found := false
	for _, transaction := range report.TaxRelevantTransactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Beispiel Shop" {
			found = true
		}
	}
	if !found {
		t.Fatal("the report reads the effective value")
	}
	if report.TaxRelevantTotal() != eur(10_000) {
		t.Fatalf("tax relevant total = %v, want 10000", report.TaxRelevantTotal())
	}
}

func TestOnlyEffectivelyTaxRelevantBookingsOfThePrimaryCurrencyAreIncluded(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -3_000).Category(10, "Spenden", "#9C27B0").TaxRelevant(),
		item(2, -1_000).Category(11, "Lebensmittel", ""),
		item(3, -4_500).Category(12, "Spenden", "#9C27B0").TaxRelevant().Currency(usd(0).Currency),
	})
	report := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	if report.BookingCount != 1 {
		t.Fatalf("booking count = %d, want 1", report.BookingCount)
	}
	if report.OtherCurrencyBookings != 1 {
		t.Fatalf("other currency bookings = %d, want 1", report.OtherCurrencyBookings)
	}
	if report.DeductionTotal() != eur(3_000) {
		t.Fatalf("deduction total = %v, want 3000", report.DeductionTotal())
	}
	if report.IncomeTotal() != eur(0) {
		t.Fatalf("income total = %v, want 0", report.IncomeTotal())
	}
	ids := make([]*model.CategoryID, 0, len(report.DeductionGroups))
	for _, group := range report.DeductionGroups {
		ids = append(ids, group.CategoryID)
	}
	if len(ids) != 1 || ids[0] == nil || *ids[0] != model.CategoryID(10) {
		t.Fatalf("deduction groups = %v, want [10]", ids)
	}
}

func TestASalaryCategoryLandsInIncomeAndAnExpenseCategoryInDeductions(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, 200_000).Date(testDate(t, 2026, time.May, 29)).Category(10, "Gehalt", "#4CAF50").TaxRelevant(),
		item(2, 220_000).Category(10, "Gehalt", "#4CAF50").TaxRelevant(),
		item(3, -30_000).Category(20, "Handwerk", "#FF5722").TaxRelevant(),
		item(4, -8_000).Date(testDate(t, 2026, time.June, 6)).Category(20, "Handwerk", "#FF5722").TaxRelevant(),
	})
	report := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	salary := single(t, report.IncomeGroups)
	if salary.CategoryID == nil || *salary.CategoryID != model.CategoryID(10) {
		t.Fatalf("salary category = %v, want 10", salary.CategoryID)
	}
	if salary.Name != "Gehalt" {
		t.Fatalf("salary name = %q, want Gehalt", salary.Name)
	}
	if salary.Color != "#4CAF50" {
		t.Fatalf("salary color = %q, want #4CAF50", salary.Color)
	}
	if salary.Income != eur(420_000) {
		t.Fatalf("salary income = %v, want 420000", salary.Income)
	}
	if salary.Expenses != eur(0) {
		t.Fatalf("salary expenses = %v, want 0", salary.Expenses)
	}
	if salary.Net() != eur(420_000) {
		t.Fatalf("salary net = %v, want 420000", salary.Net())
	}

	handwerk := single(t, report.DeductionGroups)
	if handwerk.CategoryID == nil || *handwerk.CategoryID != model.CategoryID(20) {
		t.Fatalf("handwerk category = %v, want 20", handwerk.CategoryID)
	}
	if handwerk.Income != eur(0) {
		t.Fatalf("handwerk income = %v, want 0", handwerk.Income)
	}
	if handwerk.Expenses != eur(38_000) {
		t.Fatalf("handwerk expenses = %v, want 38000", handwerk.Expenses)
	}
	if handwerk.Net() != eur(-38_000) {
		t.Fatalf("handwerk net = %v, want -38000", handwerk.Net())
	}

	if report.IncomeTotal() != salary.Net() {
		t.Fatalf("income total = %v, want %v", report.IncomeTotal(), salary.Net())
	}
	if report.DeductionTotal() != eur(38_000) {
		t.Fatalf("deduction total = %v, want 38000", report.DeductionTotal())
	}
	if report.BookingCount != 4 {
		t.Fatalf("booking count = %d, want 4", report.BookingCount)
	}
	salaryIDs := make([]int64, 0, len(salary.Bookings))
	for _, booking := range salary.Bookings {
		salaryIDs = append(salaryIDs, int64(booking.ID))
	}
	if !slices.Equal(salaryIDs, []int64{1, 2}) {
		t.Fatalf("salary bookings = %v, want [1 2]; salary bookings date ascending", salaryIDs)
	}
	handwerkIDs := make([]int64, 0, len(handwerk.Bookings))
	for _, booking := range handwerk.Bookings {
		handwerkIDs = append(handwerkIDs, int64(booking.ID))
	}
	if !slices.Equal(handwerkIDs, []int64{3, 4}) {
		t.Fatalf("handwerk bookings = %v, want [3 4]; deduction bookings date ascending", handwerkIDs)
	}
}

func TestARefundLowersADeductionCategoryAndACategoryWhoseRefundsExceedItsCostsMovesToIncome(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -30_000).Category(20, "Handwerk", "#FF5722").TaxRelevant(),
		item(2, -8_000).Category(20, "Handwerk", "#FF5722").TaxRelevant(),
		item(3, 10_000).Date(testDate(t, 2026, time.May, 22)).Category(20, "Handwerk", "#FF5722").TaxRelevant(),
		item(4, -10_000).Category(30, "Kleidung", "").TaxRelevant(),
		item(5, 25_000).Date(testDate(t, 2026, time.May, 31)).Category(30, "Kleidung", "").TaxRelevant(),
	})
	report := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	handwerk := single(t, report.DeductionGroups)
	if handwerk.Income != eur(10_000) {
		t.Fatalf("handwerk income = %v, want 10000; the refund's income is inside the category", handwerk.Income)
	}
	if handwerk.Expenses != eur(38_000) {
		t.Fatalf("handwerk expenses = %v, want 38000", handwerk.Expenses)
	}
	if handwerk.Net() != eur(-28_000) {
		t.Fatalf("handwerk net = %v, want -28000", handwerk.Net())
	}

	kleidung := single(t, report.IncomeGroups)
	if kleidung.CategoryID == nil || *kleidung.CategoryID != model.CategoryID(30) {
		t.Fatalf("kleidung category = %v, want 30", kleidung.CategoryID)
	}
	if kleidung.Income != eur(25_000) {
		t.Fatalf("kleidung income = %v, want 25000", kleidung.Income)
	}
	if kleidung.Expenses != eur(10_000) {
		t.Fatalf("kleidung expenses = %v, want 10000", kleidung.Expenses)
	}
	if kleidung.Net() != eur(15_000) {
		t.Fatalf("kleidung net = %v, want 15000", kleidung.Net())
	}

	if report.DeductionTotal() != eur(28_000) {
		t.Fatalf("deduction total = %v, want 28000", report.DeductionTotal())
	}
	if report.IncomeTotal() != eur(15_000) {
		t.Fatalf("income total = %v, want 15000", report.IncomeTotal())
	}
	if report.BookingCount != 5 {
		t.Fatalf("booking count = %d, want 5", report.BookingCount)
	}
}

func TestSectionsSortByAbsoluteNetThenNameAndWithReceiptCountsPerGroupAndInTotal(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -40_000).Category(20, "Handwerk", "#FF5722").TaxRelevant().Documents(1),
		item(2, -40_000).Category(21, "Zahnarzt", "").TaxRelevant(),
		item(3, -5_000).Category(22, "Krankenhaus", "").TaxRelevant(),
		item(4, 120_000).Category(10, "Gehalt", "#4CAF50").TaxRelevant(),
		item(5, 120_000).Date(testDate(t, 2026, time.June, 21)).Category(10, "Gehalt", "#4CAF50").TaxRelevant(),
		item(6, 90_000).Date(testDate(t, 2026, time.June, 2)).
			Category(40, "Abmahnkosten", "").TaxRelevant().Documents(1),
	})
	report := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	deductionIDs := make([]*model.CategoryID, 0, len(report.DeductionGroups))
	for _, group := range report.DeductionGroups {
		deductionIDs = append(deductionIDs, group.CategoryID)
	}
	if len(deductionIDs) != 3 ||
		deductionIDs[0] == nil || *deductionIDs[0] != model.CategoryID(20) ||
		deductionIDs[1] == nil || *deductionIDs[1] != model.CategoryID(21) ||
		deductionIDs[2] == nil || *deductionIDs[2] != model.CategoryID(22) {
		t.Fatalf("deduction groups = %v, want [20 21 22]; by absolute net, then name", deductionIDs)
	}
	incomeIDs := make([]*model.CategoryID, 0, len(report.IncomeGroups))
	for _, group := range report.IncomeGroups {
		incomeIDs = append(incomeIDs, group.CategoryID)
	}
	if len(incomeIDs) != 2 ||
		incomeIDs[0] == nil || *incomeIDs[0] != model.CategoryID(10) ||
		incomeIDs[1] == nil || *incomeIDs[1] != model.CategoryID(40) {
		t.Fatalf("income groups = %v, want [10 40]", incomeIDs)
	}
	for _, group := range report.IncomeGroups {
		if group.CategoryID != nil && *group.CategoryID == model.CategoryID(40) && group.WithReceipt() != 1 {
			t.Fatalf("Abmahnkosten receipts = %d, want 1", group.WithReceipt())
		}
	}
	for _, group := range report.DeductionGroups {
		if group.CategoryID != nil && *group.CategoryID == model.CategoryID(20) && group.WithReceipt() != 1 {
			t.Fatalf("Handwerk receipts = %d, want 1", group.WithReceipt())
		}
	}
	if report.WithReceipt() != 2 {
		t.Fatalf("with receipt = %d, want 2; one receipt in income plus one in deductions", report.WithReceipt())
	}
	if report.BookingCount != 6 {
		t.Fatalf("booking count = %d, want 6", report.BookingCount)
	}
}

func TestUncategorizedTaxRelevantBookingsFormTheirOwnGroupWithTheLocalizedLabel(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -4_900).Tax(model.TaxSourceManual).OverrideTax(true),
		item(2, 1_500).Date(testDate(t, 2026, time.June, 2)).Tax(model.TaxSourceManual).OverrideTax(true),
	})
	report := BuildTaxBookings(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)

	uncategorized := single(t, report.DeductionGroups)
	if uncategorized.CategoryID != nil {
		t.Fatalf("category = %v, want nil", uncategorized.CategoryID)
	}
	if uncategorized.Name != i18n.Text(i18n.En, i18n.Uncategorized) {
		t.Fatalf("name = %q, want %q", uncategorized.Name, i18n.Text(i18n.En, i18n.Uncategorized))
	}
	ids := make([]int64, 0, len(uncategorized.Bookings))
	for _, booking := range uncategorized.Bookings {
		ids = append(ids, int64(booking.ID))
	}
	if !slices.Equal(ids, []int64{1, 2}) {
		t.Fatalf("bookings = %v, want [1 2]; date ascending, then id", ids)
	}
	if report.BookingCount != 2 {
		t.Fatalf("booking count = %d, want 2", report.BookingCount)
	}
}
