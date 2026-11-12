package recurring

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func testToday(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, 10, 4)
	if !ok {
		t.Fatal("invalid test date")
	}
	return date
}

func moneyEUR(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func transaction(id int64, day civil.Date, minor int64, partner string, purpose *string, category *string) model.TransactionItem {
	iban := "DE1"
	return model.TransactionItem{
		ID:           model.TransactionID(id),
		AccountID:    model.AccountID(1),
		AccountIBAN:  &iban,
		Date:         day,
		Amount:       moneyEUR(minor),
		Currency:     money.EUR,
		PartnerName:  stringPointer(partner),
		Purpose:      purpose,
		CategoryName: category,
		TaxSource:    model.TaxSourceNone,
	}
}

func monthly(t *testing.T, start civil.Date, count int, amount func(int) int64, partner func(int) string, jitter func(int) int64) []model.TransactionItem {
	t.Helper()
	transactions := make([]model.TransactionItem, 0, count)
	for index := 0; index < count; index++ {
		day := addMonths(start, index)
		day = day.AddDays(int(jitter(index)))
		transactions = append(transactions, transaction(int64(index+1), day, amount(index), partner(index), nil, nil))
	}
	return transactions
}

func paypal(t *testing.T, id int64, day civil.Date, minor int64, shop *string) model.TransactionItem {
	t.Helper()
	var purpose string
	if shop != nil {
		purpose = fmt.Sprintf("PP.1234.PP . %s, Ihr Einkauf bei %s", *shop, *shop)
	} else {
		purpose = "PP.1234.PP . Lastschrift"
	}
	return transaction(id, day, minor, "PayPal Europe S.a.r.l. et Cie S.C.A", &purpose, nil)
}

func date(t *testing.T, year int, month int, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, timeMonth(month), day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return value
}

func timeMonth(month int) (value time.Month) {
	return time.Month(month)
}

func byPrefix(t *testing.T, found []RecurringPayment, prefix string) RecurringPayment {
	t.Helper()
	for _, payment := range found {
		if len(payment.Name) >= len(prefix) && payment.Name[:len(prefix)] == prefix {
			return payment
		}
	}
	t.Fatalf("no recurring payment with prefix %q in %v", prefix, found)
	return RecurringPayment{}
}

func single(t *testing.T, payments []RecurringPayment) RecurringPayment {
	t.Helper()
	if len(payments) != 1 {
		t.Fatalf("expected exactly one element, got %d", len(payments))
	}
	return payments[0]
}

func findByName(t *testing.T, payments []RecurringPayment, name string) RecurringPayment {
	t.Helper()
	for _, payment := range payments {
		if payment.Name == name {
			return payment
		}
	}
	t.Fatalf("no recurring payment named %q in %v", name, payments)
	return RecurringPayment{}
}

func stringPointer(value string) *string {
	return &value
}

func TestDetectsSubscriptionsSalaryAndVaryingBills(t *testing.T) {
	var transactions []model.TransactionItem
	transactions = append(transactions, monthly(t, date(t, 2026, 4, 15), 6,
		func(int) int64 { return -1299 },
		func(index int) string { return fmt.Sprintf("NETFLIX.COM %d", 1000+index) },
		func(int) int64 { return 0 })...)
	transactions = append(transactions, monthly(t, date(t, 2026, 4, 30), 6,
		func(int) int64 { return 320_000 },
		func(int) string { return "Employer GmbH" },
		func(index int) int64 {
			if index%2 == 0 {
				return 0
			}
			return -2
		})...)
	power := []int64{-6120, -5890, -6400, -6010, -5950, -6230}
	transactions = append(transactions, monthly(t, date(t, 2026, 4, 5), 6,
		func(index int) int64 { return power[index] },
		func(int) string { return "Stadtwerke Köln" },
		func(int) int64 { return 0 })...)
	transactions = append(transactions,
		transaction(100, date(t, 2025, 11, 1), -24_000, "HUK Coburg", nil, nil),
		transaction(101, date(t, 2024, 11, 2), -24_000, "HUK Coburg", nil, nil),
	)

	found := (RecurringDetector{}).Detect(transactions, testToday(t))

	netflix := byPrefix(t, found, "NETFLIX.COM")
	if netflix.Cadence != Monthly {
		t.Errorf("netflix.cadence = %q, want %q", netflix.Cadence, Monthly)
	}
	if !netflix.FixedAmount {
		t.Error("netflix.fixed_amount = false, want true")
	}
	if want := date(t, 2026, 10, 15); netflix.NextDate != want {
		t.Errorf("netflix.next_date = %v, want %v", netflix.NextDate, want)
	}
	if want := moneyEUR(-15588); netflix.YearlyAmount() != want {
		t.Errorf("netflix.yearly_amount = %+v, want %+v", netflix.YearlyAmount(), want)
	}

	if !byPrefix(t, found, "Employer").IsIncome() {
		t.Error("Employer should be income")
	}
	if byPrefix(t, found, "Stadtwerke").FixedAmount {
		t.Error("Stadtwerke should not be fixed")
	}
	if cadence := byPrefix(t, found, "Stadtwerke").Cadence; cadence != Monthly {
		t.Errorf("Stadtwerke.cadence = %q, want %q", cadence, Monthly)
	}
	if cadence := byPrefix(t, found, "HUK").Cadence; cadence != Yearly {
		t.Errorf("HUK.cadence = %q, want %q", cadence, Yearly)
	}
	if want := date(t, 2026, 11, 1); byPrefix(t, found, "HUK").NextDate != want {
		t.Errorf("HUK.next_date = %v, want %v", byPrefix(t, found, "HUK").NextDate, want)
	}
}

func TestIrregularTooRareAndCancelledBookingsAreNotRecurring(t *testing.T) {
	days := []int{1, 3, 9, 10, 22, 40, 41, 70}
	groceries := make([]model.TransactionItem, 0, len(days))
	for index, day := range days {
		groceries = append(groceries, transaction(
			int64(index+1),
			testToday(t).AddDays(-day),
			-2300,
			"REWE",
			nil,
			nil,
		))
	}
	twice := monthly(t, date(t, 2026, 8, 1), 2, func(int) int64 { return -999 }, func(int) string { return "Spotify" }, func(int) int64 { return 0 })
	cancelled := monthly(t, date(t, 2026, 1, 10), 4, func(int) int64 { return -799 }, func(int) string { return "Disney Plus" }, func(int) int64 { return 0 })

	all := append(append(groceries, twice...), cancelled...)
	names := make([]string, 0)
	for _, payment := range (RecurringDetector{}).Detect(all, testToday(t)) {
		names = append(names, payment.Name)
	}

	if contains(names, "REWE") {
		t.Error("REWE should not be recurring")
	}
	if contains(names, "Spotify") {
		t.Error("two monthly bookings aren't enough")
	}
	if contains(names, "Disney Plus") {
		t.Error("stopped months ago")
	}
}

func TestALateBookingStaysDueAndUpcomingListsTheNext30Days(t *testing.T) {
	var transactions []model.TransactionItem
	transactions = append(transactions, monthly(t, date(t, 2026, 5, 1), 5,
		func(int) int64 { return 300_000 },
		func(int) string { return "Employer" },
		func(int) int64 { return 0 })...)
	transactions = append(transactions, monthly(t, date(t, 2026, 5, 20), 5,
		func(int) int64 { return -90_000 },
		func(int) string { return "Vermieter" },
		func(int) int64 { return 0 })...)
	transactions = append(transactions,
		transaction(100, date(t, 2025, 3, 1), -10_000, "ADAC", nil, nil),
		transaction(101, date(t, 2024, 3, 1), -10_000, "ADAC", nil, nil),
	)

	recurring := (RecurringDetector{}).Detect(transactions, testToday(t))
	employer := findByName(t, recurring, "Employer")
	if want := date(t, 2026, 10, 1); employer.NextDate != want {
		t.Errorf("Employer.next_date = %v, want %v", employer.NextDate, want)
	}

	upcoming := make([]string, 0)
	for _, payment := range (RecurringDetector{}).Upcoming(recurring, testToday(t), 30) {
		upcoming = append(upcoming, payment.Name)
	}
	want := []string{"Employer", "Vermieter"}
	if len(upcoming) != len(want) || upcoming[0] != want[0] || upcoming[1] != want[1] {
		t.Errorf("upcoming = %v, want %v", upcoming, want)
	}
}

func TestAPaypalSubscriptionIsDetectedByItsShopDespiteOtherPurchases(t *testing.T) {
	transactions := make([]model.TransactionItem, 0)
	for index := 0; index < 5; index++ {
		day := addMonths(date(t, 2026, 5, 10), index)
		shop := "Beispiel Abo GmbH"
		transactions = append(transactions, paypal(t, int64(index+1), day, -999, &shop))
	}
	for index, days := range []int{63, 40, 30, 2} {
		shop := "Beispiel Shop GmbH"
		amounts := []int64{-2400, -5900, -3100, -1800}
		transactions = append(transactions, paypal(t, int64(index+20), testToday(t).AddDays(-days), amounts[index], &shop))
	}
	for index, days := range []int{80, 45, 20} {
		shop := "Anderer Shop GmbH"
		amounts := []int64{-1200, -4400, -700}
		transactions = append(transactions, paypal(t, int64(index+30), testToday(t).AddDays(-days), amounts[index], &shop))
	}
	for index, days := range []int{70, 10} {
		shop := "Dritter Laden GmbH"
		amounts := []int64{-500, -600}
		transactions = append(transactions, paypal(t, int64(index+40), testToday(t).AddDays(-days), amounts[index], &shop))
	}

	found := (RecurringDetector{}).Detect(transactions, testToday(t))
	abo := single(t, found)
	if abo.Name != "PayPal · Beispiel Abo GmbH" {
		t.Errorf("name = %q, want %q", abo.Name, "PayPal · Beispiel Abo GmbH")
	}
	if abo.Cadence != Monthly {
		t.Errorf("cadence = %q, want %q", abo.Cadence, Monthly)
	}
	if abo.Occurrences != 5 {
		t.Errorf("occurrences = %d, want 5", abo.Occurrences)
	}
	if want := moneyEUR(-999); abo.TypicalAmount != want {
		t.Errorf("typical_amount = %+v, want %+v", abo.TypicalAmount, want)
	}
}

func TestAPartnerNamedLikeAPaypalShopStaysASeparatePayment(t *testing.T) {
	transactions := make([]model.TransactionItem, 0)
	for index := 0; index < 5; index++ {
		day := addMonths(date(t, 2026, 5, 10), index)
		shop := "Beispiel Abo GmbH"
		transactions = append(transactions, paypal(t, int64(index+1), day, -999, &shop))
	}
	transactions = append(transactions, monthly(t, date(t, 2026, 5, 12), 5,
		func(int) int64 { return -999 },
		func(int) string { return "Beispiel Abo GmbH" },
		func(int) int64 { return 0 })...)

	found := (RecurringDetector{}).Detect(transactions, testToday(t))
	names := make([]string, 0, len(found))
	for _, payment := range found {
		names = append(names, payment.Name)
	}
	sort.Strings(names)
	want := []string{"Beispiel Abo GmbH", "PayPal · Beispiel Abo GmbH"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("names = %v, want %v", names, want)
	}
	for _, payment := range found {
		if payment.Occurrences != 5 {
			t.Errorf("%s occurrences = %d, want 5", payment.Name, payment.Occurrences)
		}
	}
}

func TestFiltersAndRecurringPaymentsArePerCurrency(t *testing.T) {
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatal(err)
	}
	transactions := make([]model.TransactionItem, 0)
	for month := 6; month <= 9; month++ {
		day := date(t, 2026, 6, 5).AddDays(30 * (month - 6))
		transactions = append(transactions, transaction(int64(month), day, -999, "Spotify", nil, nil))
		dollar := transaction(int64(month+10), day, -1199, "Spotify", nil, nil)
		dollar.Amount.Currency = usd
		dollar.Currency = usd
		transactions = append(transactions, dollar)
	}

	recurring := (RecurringDetector{}).Detect(transactions, date(t, 2026, 9, 20))
	payments := make(map[money.Currency]int64)
	for _, payment := range recurring {
		payments[payment.Currency] = payment.TypicalAmount.Minor
	}
	if len(payments) != 2 {
		t.Fatalf("payments = %v, want two recurring payments", payments)
	}
	if payments[money.EUR] != -999 {
		t.Errorf("EUR amount = %d, want -999", payments[money.EUR])
	}
	if payments[usd] != -1199 {
		t.Errorf("USD amount = %d, want -1199", payments[usd])
	}
}

func TestPaypalPaymentsWithoutAShopKeepGroupingUnderThePartner(t *testing.T) {
	payments := make([]model.TransactionItem, 0)
	for index := 0; index < 5; index++ {
		day := addMonths(date(t, 2026, 5, 10), index)
		payments = append(payments, paypal(t, int64(index+1), day, -450, nil))
	}

	recurring := (RecurringDetector{}).Detect(payments, testToday(t))
	payment := single(t, recurring)
	if payment.Name != "PayPal Europe S.a.r.l. et Cie S.C.A" {
		t.Errorf("name = %q, want %q", payment.Name, "PayPal Europe S.a.r.l. et Cie S.C.A")
	}
	if payment.Cadence != Monthly {
		t.Errorf("cadence = %q, want %q", payment.Cadence, Monthly)
	}
	if payment.Occurrences != 5 {
		t.Errorf("occurrences = %d, want 5", payment.Occurrences)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
