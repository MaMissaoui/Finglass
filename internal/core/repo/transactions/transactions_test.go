package transactions

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func database(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()

	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

func stringPointer(value string) *string {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func accountWith(t *testing.T, d *db.Database, name, iban string, currency money.Currency) model.AccountID {
	t.Helper()

	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        stringPointer(name),
		IBAN:        stringPointer(iban),
		Currency:    currency,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: currency},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func account(t *testing.T, d *db.Database) model.AccountID {
	t.Helper()
	return accountWith(t, d, "Account", "DE89370400440532013000", money.EUR)
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func entry(hash string) model.NewTransaction {
	day, _ := civil.New(2026, time.October, 3)
	return model.NewTransaction{
		BookingDate: day,
		Amount:      eur(-450),
		PartnerName: stringPointer("Bäcker"),
		Purpose:     stringPointer("Kaffee"),
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
		IsNew:       false,
	}
}

func newCategory(t *testing.T, d *db.Database, name string, taxRelevant, isTransfer bool) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:          name,
		Color:         "#FF5722",
		IsTaxRelevant: taxRelevant,
		IsTransfer:    isTransfer,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func addFull(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	partner *string,
	purpose *string,
	categoryID *model.CategoryID,
	hash string,
	isNew bool,
	taxOverride *bool,
) model.TransactionID {
	t.Helper()

	inserted, err := InsertPlanned(context.Background(), d, accountID, []model.NewTransaction{{
		BookingDate: day,
		Amount:      eur(minor),
		PartnerName: partner,
		Purpose:     purpose,
		CategoryID:  categoryID,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
		IsNew:       isNew,
		TaxOverride: taxOverride,
	}})
	if err != nil || inserted != 1 {
		t.Fatalf("InsertPlanned = %d, %v", inserted, err)
	}
	transactions, err := ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	for _, transaction := range transactions {
		if transaction.ImportHash == hash {
			return transaction.ID
		}
	}
	t.Fatalf("inserted transaction %q is missing", hash)
	return 0
}

func add(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	partner *string,
	purpose *string,
	categoryID *model.CategoryID,
	hash string,
) model.TransactionID {
	t.Helper()
	return addFull(t, d, accountID, day, minor, partner, purpose, categoryID, hash, false, nil)
}

func addRule(
	t *testing.T,
	d *db.Database,
	pattern string,
	categoryID model.CategoryID,
	priority int64,
	autoApply bool,
	matchField model.RuleField,
	direction model.RuleDirection,
	taxRelevant *bool,
) model.RuleID {
	t.Helper()

	id, err := rules.Create(context.Background(), d, model.NewRule{
		Pattern:     pattern,
		CategoryID:  categoryID,
		Priority:    priority,
		AutoApply:   autoApply,
		MatchField:  matchField,
		Direction:   direction,
		TaxRelevant: taxRelevant,
	})
	if err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
	return id
}

func setRuleTax(t *testing.T, d *db.Database, id model.RuleID, taxRelevant *bool) {
	t.Helper()

	listed, err := rules.List(context.Background(), d)
	if err != nil {
		t.Fatalf("rules.List: %v", err)
	}
	for _, rule := range listed {
		if rule.ID != id {
			continue
		}
		if err := rules.Update(context.Background(), d, id, model.NewRule{
			Pattern:     rule.Pattern,
			CategoryID:  rule.CategoryID,
			Priority:    rule.Priority,
			AutoApply:   rule.AutoApply,
			MatchField:  rule.MatchField,
			Direction:   rule.Direction,
			TaxRelevant: taxRelevant,
		}); err != nil {
			t.Fatalf("rules.Update: %v", err)
		}
		return
	}
	t.Fatalf("rule %d is missing", id)
}

func recentAudit(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	entries, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return entries
}

func findItems(
	t *testing.T,
	d *db.Database,
	filter *model.TransactionFilter,
	limit *int,
) []model.TransactionItem {
	t.Helper()

	items, err := FindItems(context.Background(), d, filter, limit)
	if err != nil {
		t.Fatalf("FindItems: %v", err)
	}
	return items
}

func itemByID(t *testing.T, items []model.TransactionItem, id model.TransactionID) model.TransactionItem {
	t.Helper()

	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("transaction %d is missing", id)
	return model.TransactionItem{}
}

func TestHashAndDuplicateQueriesAreScopedByAccount(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	one := account(t, d)
	two := account(t, d)

	if _, err := InsertPlanned(ctx, d, one, []model.NewTransaction{entry("same-hash")}); err != nil {
		t.Fatalf("InsertPlanned: %v", err)
	}

	found, err := FindByHash(ctx, d, one, "same-hash")
	if err != nil || !found {
		t.Errorf("FindByHash(one) = %v, %v", found, err)
	}
	found, err = FindByHash(ctx, d, two, "same-hash")
	if err != nil || found {
		t.Errorf("FindByHash(two) = %v, %v", found, err)
	}

	same, err := CountSameBooking(ctx, d, one, date(t, 2026, time.October, 3), -450)
	if err != nil || same != 1 {
		t.Errorf("CountSameBooking(one) = %d, %v", same, err)
	}
	same, err = CountSameBooking(ctx, d, two, date(t, 2026, time.October, 3), -450)
	if err != nil || same != 0 {
		t.Errorf("CountSameBooking(two) = %d, %v", same, err)
	}

	bookings, err := ListForAccount(ctx, d, one, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("ListForAccount = %d, %v", len(bookings), err)
	}
	if err := Delete(ctx, d, bookings[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted, err := DeletedHashes(ctx, d, one)
	if err != nil || len(deleted) != 1 {
		t.Errorf("DeletedHashes(one) = %v, %v", deleted, err)
	}
	deleted, err = DeletedHashes(ctx, d, two)
	if err != nil || len(deleted) != 0 {
		t.Errorf("DeletedHashes(two) = %v, %v", deleted, err)
	}
}

func TestInsertPlannedInsertsEveryRowWithoutDedupOrItsOwnAuditEntry(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	before := len(recentAudit(t, d))

	inserted, err := InsertPlanned(ctx, d, accountID, []model.NewTransaction{entry("same"), entry("same")})
	if err != nil || inserted != 2 {
		t.Fatalf("first InsertPlanned = %d, %v", inserted, err)
	}
	inserted, err = InsertPlanned(ctx, d, accountID, []model.NewTransaction{entry("same"), entry("same")})
	if err != nil || inserted != 2 {
		t.Fatalf("second InsertPlanned = %d, %v", inserted, err)
	}
	bookings, err := ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 4 {
		t.Errorf("ListForAccount = %d, %v", len(bookings), err)
	}
	if got := len(recentAudit(t, d)); got != before {
		t.Errorf("audit entries = %d, want %d: the import service writes the single import audit entry itself", got, before)
	}
}

func TestItemsExposeCategoryDataAndAreNewestFirst(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	food := newCategory(t, d, "Food", false, false)
	office := newCategory(t, d, "Office", true, false)
	add(t, d, accountID, date(t, 2026, time.March, 1), -1000,
		stringPointer("REWE"), stringPointer("Groceries"), &food, "items-a")
	add(t, d, accountID, date(t, 2026, time.March, 15), -9900,
		stringPointer("Staples"), stringPointer("Printer paper"), &office, "items-b")
	add(t, d, accountID, date(t, 2026, time.March, 31), 300_000,
		stringPointer("Employer GmbH"), stringPointer("Gehalt März"), nil, "items-c")
	add(t, d, accountID, date(t, 2026, time.April, 1), -500,
		nil, stringPointer("Kartenzahlung Kiosk"), nil, "items-d")

	items := findItems(t, d, &model.TransactionFilter{}, nil)
	var rewe model.TransactionItem
	for _, item := range items {
		if item.PartnerName != nil && *item.PartnerName == "REWE" {
			rewe = item
		}
	}
	if rewe.CategoryName == nil || *rewe.CategoryName != "Food" {
		t.Errorf("category name = %v", rewe.CategoryName)
	}
	if rewe.IsTaxRelevant {
		t.Error("REWE is tax relevant")
	}
	if rewe.TaxSource != model.TaxSourceCategory {
		t.Errorf("tax source = %q", rewe.TaxSource)
	}
	if len(items) == 0 || items[0].Date != date(t, 2026, time.April, 1) {
		t.Error("the items are not newest first")
	}
}

func TestFiltersByDateDirectionCategoryTextUncategorizedAndTax(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	food := newCategory(t, d, "Food", false, false)
	office := newCategory(t, d, "Office", true, false)
	add(t, d, accountID, date(t, 2026, time.March, 1), -1000,
		stringPointer("REWE"), stringPointer("Groceries"), &food, "filter-a")
	add(t, d, accountID, date(t, 2026, time.March, 15), -9900,
		stringPointer("Staples"), stringPointer("Printer paper"), &office, "filter-b")
	add(t, d, accountID, date(t, 2026, time.March, 31), 300_000,
		stringPointer("Employer GmbH"), stringPointer("Gehalt März"), nil, "filter-c")
	add(t, d, accountID, date(t, 2026, time.April, 1), -500,
		nil, stringPointer("Kartenzahlung Kiosk"), nil, "filter-d")

	marchStart := date(t, 2026, time.March, 1)
	marchEnd := date(t, 2026, time.March, 31)
	march := model.TransactionFilter{StartDate: &marchStart, EndDate: &marchEnd}
	if got := len(findItems(t, d, &march, nil)); got != 3 {
		t.Errorf("March = %d", got)
	}
	income := march
	income.Direction = model.DirectionIncome
	if got := len(findItems(t, d, &income, nil)); got != 1 {
		t.Errorf("March income = %d", got)
	}
	expenses := march
	expenses.Direction = model.DirectionExpenses
	if got := len(findItems(t, d, &expenses, nil)); got != 2 {
		t.Errorf("March expenses = %d", got)
	}

	officeFilter := model.TransactionFilter{CategoryID: &office}
	var partners []string
	for _, item := range findItems(t, d, &officeFilter, nil) {
		if item.PartnerName != nil {
			partners = append(partners, *item.PartnerName)
		}
	}
	if !slices.Equal(partners, []string{"Staples"}) {
		t.Errorf("office partners = %v", partners)
	}

	uncategorized := model.TransactionFilter{UncategorizedOnly: true}
	if got := len(findItems(t, d, &uncategorized, nil)); got != 2 {
		t.Errorf("uncategorized = %d", got)
	}
	tax := model.TransactionFilter{TaxRelevantOnly: true}
	if got := len(findItems(t, d, &tax, nil)); got != 1 {
		t.Errorf("tax relevant = %d", got)
	}

	for _, text := range []string{"kiosk", "printer", "KIOSK", "Printer"} {
		textFilter := model.TransactionFilter{Text: stringPointer(text)}
		if got := len(findItems(t, d, &textFilter, nil)); got != 1 {
			t.Errorf("text %q = %d, want 1: searches partner and purpose case-insensitively", text, got)
		}
	}

	limit := 2
	limited := findItems(t, d, &model.TransactionFilter{}, &limit)
	if len(limited) != 2 {
		t.Fatalf("limited = %d", len(limited))
	}
	if limited[0].Amount.Minor != -500 {
		t.Errorf("limit keeps the newest rows: first amount = %d", limited[0].Amount.Minor)
	}
}

func TestAmountBoundsAreAbsoluteAndCurrencyKeepsTotalsApart(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	add(t, d, accountID, date(t, 2026, time.May, 1), -500,
		stringPointer("Small expense"), nil, nil, "amount-small")
	add(t, d, accountID, date(t, 2026, time.May, 2), -1500,
		stringPointer("Medium expense"), nil, nil, "amount-medium")
	add(t, d, accountID, date(t, 2026, time.May, 3), 2000,
		stringPointer("Medium income"), nil, nil, "amount-income")
	add(t, d, accountID, date(t, 2026, time.May, 4), 5000,
		stringPointer("Large income"), nil, nil, "amount-large")

	minAmount := eur(1000)
	maxAmount := eur(2500)
	matched := findItems(t, d, &model.TransactionFilter{
		MinAmount: &minAmount,
		MaxAmount: &maxAmount,
	}, nil)
	names := make([]string, 0, len(matched))
	for _, item := range matched {
		if item.PartnerName != nil {
			names = append(names, *item.PartnerName)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Medium expense", "Medium income"}) {
		t.Errorf("matched = %v: bounds compare absolute amounts, so income and expenses match alike", names)
	}

	smallLimit := eur(500)
	if got := len(findItems(t, d, &model.TransactionFilter{MaxAmount: &smallLimit}, nil)); got != 1 {
		t.Errorf("max 500 = %d", got)
	}

	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	dollar := accountWith(t, d, "Dollar", "DE44500105175407324931", usd)
	if _, err := InsertPlanned(context.Background(), d, dollar, []model.NewTransaction{{
		BookingDate: date(t, 2026, time.May, 5),
		Amount:      money.Money{Minor: -700, Currency: usd},
		PartnerName: stringPointer("US shop"),
		ImportHash:  "amount-usd",
		CreatedAt:   time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("InsertPlanned USD: %v", err)
	}

	onlyUSD := findItems(t, d, &model.TransactionFilter{Currency: &usd}, nil)
	if len(onlyUSD) != 1 {
		t.Fatalf("only USD = %d", len(onlyUSD))
	}
	if onlyUSD[0].Currency != usd {
		t.Errorf("currency = %v", onlyUSD[0].Currency)
	}
}

func TestTextSearchMatchesLikeWildcardsLiterally(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	add(t, d, accountID, date(t, 2026, time.May, 1), -100,
		stringPointer("100% Shop"), nil, nil, "like-percent")
	add(t, d, accountID, date(t, 2026, time.May, 2), -200,
		stringPointer("100x Shop"), nil, nil, "like-x")
	add(t, d, accountID, date(t, 2026, time.May, 3), -300,
		stringPointer("under_score"), nil, nil, "like-underscore")
	add(t, d, accountID, date(t, 2026, time.May, 4), -400,
		stringPointer("underXscore"), nil, nil, "like-other")
	add(t, d, accountID, date(t, 2026, time.May, 5), -500,
		stringPointer("O'Brien"), nil, nil, "like-quote")

	for _, testCase := range []struct {
		text     string
		expected []string
	}{
		{"%", []string{"like-percent"}},
		{"_", []string{"like-underscore"}},
		{"'", []string{"like-quote"}},
		{"O'Brien", []string{"like-quote"}},
	} {
		found := findItems(t, d, &model.TransactionFilter{Text: stringPointer(testCase.text)}, nil)
		got := make(map[model.TransactionID]struct{})
		for _, item := range found {
			got[item.ID] = struct{}{}
		}
		want := make(map[model.TransactionID]struct{})
		bookings, err := ListForAccount(context.Background(), d, accountID, nil, nil)
		if err != nil {
			t.Fatalf("ListForAccount: %v", err)
		}
		for _, hash := range testCase.expected {
			for _, booking := range bookings {
				if booking.ImportHash == hash {
					want[booking.ID] = struct{}{}
				}
			}
		}
		if len(got) != len(want) {
			t.Errorf("text %q found %v, want %v: matched literally", testCase.text, got, want)
			continue
		}
		for id := range want {
			if _, present := got[id]; !present {
				t.Errorf("text %q missed transaction %d", testCase.text, id)
			}
		}
	}
}

func TestAccountIDsNullReturnsAllASetReturnsExactlyThoseAndAnEmptySetNone(t *testing.T) {
	d := database(t)
	first := account(t, d)
	second := account(t, d)
	add(t, d, first, date(t, 2026, time.January, 1), -100,
		stringPointer("One"), nil, nil, "ids-a")
	add(t, d, second, date(t, 2026, time.January, 2), -200,
		stringPointer("Two"), nil, nil, "ids-b")

	all := findItems(t, d, &model.TransactionFilter{}, nil)
	if len(all) != len(findItems(t, d, &model.TransactionFilter{AccountIDs: nil}, nil)) {
		t.Error("nil account ids restrict the result")
	}

	secondSet := model.AccountIDSet{second: {}}
	onlySecond := findItems(t, d, &model.TransactionFilter{AccountIDs: &secondSet}, nil)
	if len(onlySecond) != 1 {
		t.Fatalf("only second = %d", len(onlySecond))
	}
	for _, item := range onlySecond {
		if item.AccountID != second {
			t.Errorf("account id = %d", item.AccountID)
		}
	}

	bothSet := model.AccountIDSet{first: {}, second: {}}
	if got := len(findItems(t, d, &model.TransactionFilter{AccountIDs: &bothSet}, nil)); got != 2 {
		t.Errorf("both = %d", got)
	}

	emptySet := model.AccountIDSet{}
	if got := findItems(t, d, &model.TransactionFilter{AccountIDs: &emptySet}, nil); len(got) != 0 {
		t.Errorf("empty set = %d, want none", len(got))
	}
}

func TestCountNewIsPerAccountAndMarkReadClearsTheRequestedScope(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	first := account(t, d)
	second := account(t, d)
	addFull(t, d, first, date(t, 2020, time.January, 1), -100,
		stringPointer("Old"), nil, nil, "new-old", true, nil)
	addFull(t, d, first, date(t, 2026, time.January, 1), -200,
		stringPointer("Now"), nil, nil, "new-now", true, nil)
	addFull(t, d, second, date(t, 2026, time.January, 1), -300,
		stringPointer("Other"), nil, nil, "new-other", true, nil)
	add(t, d, second, date(t, 2026, time.January, 1), -400,
		stringPointer("Read"), nil, nil, "new-read")

	counts, err := CountNew(ctx, d)
	if err != nil {
		t.Fatalf("CountNew: %v", err)
	}
	if counts[first] != 2 || counts[second] != 1 {
		t.Errorf("counts = %v", counts)
	}

	empty := model.AccountIDSet{}
	if err := MarkRead(ctx, d, &empty); err != nil {
		t.Fatalf("MarkRead empty: %v", err)
	}
	afterEmpty, err := CountNew(ctx, d)
	if err != nil {
		t.Fatalf("CountNew: %v", err)
	}
	if !maps.Equal(afterEmpty, counts) {
		t.Errorf("counts after the empty set = %v, want %v: an empty set clears nothing", afterEmpty, counts)
	}

	firstSet := model.AccountIDSet{first: {}}
	if err := MarkRead(ctx, d, &firstSet); err != nil {
		t.Fatalf("MarkRead first: %v", err)
	}
	afterFirst, err := CountNew(ctx, d)
	if err != nil {
		t.Fatalf("CountNew: %v", err)
	}
	if _, present := afterFirst[first]; present {
		t.Error("the first account still has new bookings")
	}
	if afterFirst[second] != 1 {
		t.Error("only the named account is cleared")
	}

	if err := MarkRead(ctx, d, nil); err != nil {
		t.Fatalf("MarkRead all: %v", err)
	}
	afterAll, err := CountNew(ctx, d)
	if err != nil {
		t.Fatalf("CountNew: %v", err)
	}
	if len(afterAll) != 0 {
		t.Errorf("counts after MarkRead all = %v", afterAll)
	}
}

func TestDeleteAllDeletesExactlyTheIDsUnlinksDocumentsAndIgnoresUnknownIDs(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	keep := add(t, d, accountID, date(t, 2026, time.February, 1), -100,
		stringPointer("Keep"), nil, nil, "delete-keep")
	gone := add(t, d, accountID, date(t, 2026, time.February, 2), -200,
		stringPointer("Gone"), nil, nil, "delete-gone")
	otherGone := add(t, d, accountID, date(t, 2026, time.February, 3), -300,
		stringPointer("Other"), nil, nil, "delete-other")
	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)\n"+
				"                 VALUES ('gone.pdf', 'gone.pdf', 'application/pdf', '2026-02-02T00:00:00Z', ?1),\n"+
				"                        ('keep.pdf', 'keep.pdf', 'application/pdf', '2026-02-02T00:00:00Z', ?2)",
			int64(gone), int64(keep),
		)
		return err
	}); err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	deleted, err := DeleteAll(ctx, d, []model.TransactionID{gone, otherGone, 999_999})
	if err != nil || deleted != 2 {
		t.Errorf("DeleteAll = %d, %v: the unknown id is ignored", deleted, err)
	}
	remaining, err := ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	for _, transaction := range remaining {
		if transaction.ID != keep {
			t.Errorf("transaction %d survived", transaction.ID)
		}
	}
	linked := make([]*int64, 0)
	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT transaction_id FROM documents ORDER BY file_name")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var value *int64
			if err := rows.Scan(&value); err != nil {
				return err
			}
			linked = append(linked, value)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if len(linked) != 2 || linked[0] != nil || linked[1] == nil || *linked[1] != int64(keep) {
		t.Errorf("linked = %v: the deleted booking's receipt is unlinked, not gone", linked)
	}
	if deleted, err := DeleteAll(ctx, d, nil); err != nil || deleted != 0 {
		t.Errorf("DeleteAll empty = %d, %v", deleted, err)
	}
}

func TestDeleteAllChunksMoreThanTheParameterLimitInOneCall(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	ids := make([]model.TransactionID, 0, 1250)
	for index := 0; index < 1250; index++ {
		ids = append(ids, add(t, d, accountID, date(t, 2026, time.January, 1), -100,
			stringPointer("Chunk"), nil, nil, "chunk-"+itoa(index)))
	}
	survivor := add(t, d, accountID, date(t, 2026, time.January, 2), -500,
		stringPointer("Survivor"), nil, nil, "chunk-survivor")

	deleted, err := DeleteAll(context.Background(), d, ids)
	if err != nil || deleted != 1250 {
		t.Fatalf("DeleteAll = %d, %v", deleted, err)
	}
	remaining, err := ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != survivor {
		t.Errorf("remaining = %v", remaining)
	}
}

func TestUpdateCategoriesUpdatesAllGivenRowsAndReturnsTheCount(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	groceries := newCategory(t, d, "Groceries", false, false)
	loans := newCategory(t, d, "Loans", false, false)
	transfer := newCategory(t, d, "Transfer", false, true)
	first := add(t, d, accountID, date(t, 2026, time.June, 1), -990,
		stringPointer("Store"), nil, &groceries, "update-a")
	second := add(t, d, accountID, date(t, 2026, time.June, 2), -4500,
		stringPointer("Sparkasse"), nil, nil, "update-b")
	third := add(t, d, accountID, date(t, 2026, time.June, 3), -12_000,
		stringPointer("Loan"), nil, &loans, "update-c")

	updated, err := UpdateCategories(ctx, d, []CategoryChange{
		{TransactionID: first, CategoryID: &transfer},
		{TransactionID: second, CategoryID: &groceries},
		{TransactionID: third, CategoryID: nil},
	})
	if err != nil || updated != 3 {
		t.Fatalf("UpdateCategories = %d, %v", updated, err)
	}
	rows := findItems(t, d, &model.TransactionFilter{}, nil)
	row := func(id model.TransactionID) model.TransactionItem {
		return itemByID(t, rows, id)
	}
	if row(first).CategoryID == nil || *row(first).CategoryID != transfer {
		t.Errorf("first category = %v", row(first).CategoryID)
	}
	if row(second).CategoryID == nil || *row(second).CategoryID != groceries {
		t.Errorf("second category = %v", row(second).CategoryID)
	}
	if row(third).CategoryID != nil {
		t.Errorf("third category = %v: null restores the uncategorized state", row(third).CategoryID)
	}
}

func TestUpdateCategoriesSkipsTargetsAlreadySetAndUnknownIDs(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	groceries := newCategory(t, d, "Groceries", false, false)
	loans := newCategory(t, d, "Loans", false, false)
	already := add(t, d, accountID, date(t, 2026, time.June, 1), -990,
		stringPointer("Store"), nil, &groceries, "same-a")
	toMove := add(t, d, accountID, date(t, 2026, time.June, 2), -4500,
		stringPointer("Bank"), nil, &loans, "same-b")

	updated, err := UpdateCategories(ctx, d, []CategoryChange{
		{TransactionID: already, CategoryID: &groceries},
		{TransactionID: toMove, CategoryID: &groceries},
	})
	if err != nil || updated != 1 {
		t.Fatalf("UpdateCategories = %d, %v", updated, err)
	}
	updated, err = UpdateCategories(ctx, d, []CategoryChange{
		{TransactionID: already, CategoryID: &groceries},
		{TransactionID: 99_999, CategoryID: &groceries},
	})
	if err != nil || updated != 0 {
		t.Errorf("UpdateCategories with an unknown id = %d, %v", updated, err)
	}
}

func TestDeleteAllRemembersEachDeletedBookingOnceAndForgetDeletedClearsThem(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	first := add(t, d, accountID, date(t, 2026, time.October, 5), -500,
		stringPointer("Cafe"), nil, nil, "remember-a")
	second := add(t, d, accountID, date(t, 2026, time.October, 5), -700,
		stringPointer("Bakery"), nil, nil, "remember-b")

	if _, err := DeleteAll(ctx, d, []model.TransactionID{first, second}); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	deleted, err := DeletedHashes(ctx, d, accountID)
	if err != nil || len(deleted) != 2 {
		t.Fatalf("DeletedHashes = %v, %v", deleted, err)
	}
	if _, present := deleted["remember-a"]; !present {
		t.Error("remember-a is missing")
	}
	if _, present := deleted["remember-b"]; !present {
		t.Error("remember-b is missing")
	}
	if _, err := DeleteAll(ctx, d, []model.TransactionID{first, second}); err != nil {
		t.Fatalf("second DeleteAll: %v", err)
	}
	deleted, err = DeletedHashes(ctx, d, accountID)
	if err != nil || len(deleted) != 2 {
		t.Errorf("DeletedHashes after the repeat = %v, %v: a repeated delete does not record the pairs twice", deleted, err)
	}

	removed, err := ForgetDeleted(ctx, d, accountID)
	if err != nil || removed != 2 {
		t.Fatalf("ForgetDeleted = %d, %v", removed, err)
	}
	deleted, err = DeletedHashes(ctx, d, accountID)
	if err != nil || len(deleted) != 0 {
		t.Errorf("DeletedHashes after ForgetDeleted = %v, %v", deleted, err)
	}
	removed, err = ForgetDeleted(ctx, d, accountID)
	if err != nil || removed != 0 {
		t.Errorf("second ForgetDeleted = %d, %v", removed, err)
	}
}

func TestOldestDateIsOptionalAndFilteredByCurrency(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	oldest, err := OldestDate(ctx, d, nil)
	if err != nil || oldest != nil {
		t.Errorf("OldestDate empty = %v, %v", oldest, err)
	}
	oldest, err = OldestDate(ctx, d, &money.EUR)
	if err != nil || oldest != nil {
		t.Errorf("OldestDate EUR empty = %v, %v", oldest, err)
	}

	euro := account(t, d)
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	dollar := accountWith(t, d, "Dollar", "DE44500105175407324931", usd)
	add(t, d, euro, date(t, 2026, time.August, 2), -500,
		stringPointer("Euro"), nil, nil, "oldest-eur")
	if _, err := InsertPlanned(ctx, d, dollar, []model.NewTransaction{{
		BookingDate: date(t, 2026, time.January, 10),
		Amount:      money.Money{Minor: -500, Currency: usd},
		PartnerName: stringPointer("Dollar"),
		ImportHash:  "oldest-usd",
		CreatedAt:   time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("InsertPlanned USD: %v", err)
	}

	oldest, err = OldestDate(ctx, d, nil)
	if err != nil || oldest == nil || *oldest != date(t, 2026, time.January, 10) {
		t.Errorf("OldestDate = %v, %v", oldest, err)
	}
	oldest, err = OldestDate(ctx, d, &money.EUR)
	if err != nil || oldest == nil || *oldest != date(t, 2026, time.August, 2) {
		t.Errorf("OldestDate EUR = %v, %v", oldest, err)
	}
	gbp, err := money.NewCurrency("GBP")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	oldest, err = OldestDate(ctx, d, &gbp)
	if err != nil || oldest != nil {
		t.Errorf("OldestDate GBP = %v, %v", oldest, err)
	}
}

func TestFindItemsMergesDocumentCountsAcrossChunks(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	ids := make([]model.TransactionID, 0, 1250)
	for index := 0; index < 1250; index++ {
		ids = append(ids, add(t, d, accountID, date(t, 2026, time.January, 1), -100,
			stringPointer("Shop"), nil, nil, "count-"+itoa(index)))
	}
	for _, document := range []struct {
		name string
		id   model.TransactionID
	}{
		{"first-a", ids[0]},
		{"first-b", ids[0]},
		{"middle", ids[600]},
		{"last", ids[1_249]},
	} {
		if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
			_, err := conn.ExecContext(
				ctx,
				"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)\n"+
					"                 VALUES (?1, ?1, 'application/pdf', '2026-01-01T00:00:00Z', ?2)",
				document.name, int64(document.id),
			)
			return err
		}); err != nil {
			t.Fatalf("seed document: %v", err)
		}
	}

	items := findItems(t, d, &model.TransactionFilter{AccountID: &accountID}, nil)
	if len(items) != 1250 {
		t.Fatalf("items = %d", len(items))
	}
	counts := make(map[model.TransactionID]int, len(items))
	total := 0
	for _, item := range items {
		counts[item.ID] = item.DocumentCount
		total += item.DocumentCount
	}
	if counts[ids[0]] != 2 || counts[ids[600]] != 1 || counts[ids[1_249]] != 1 {
		t.Errorf("counts = %d, %d, %d", counts[ids[0]], counts[ids[600]], counts[ids[1_249]])
	}
	if total != 4 {
		t.Errorf("total document count = %d", total)
	}
}

func TestTheCategoryLayerDecidesWhenNoRuleOrOverrideDoes(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	inTax := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &taxCategory, "tax-cat-a")
	inNormal := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Anderer Shop"), nil, &normalCategory, "tax-cat-b")
	uncategorized := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Dritter Shop"), nil, nil, "tax-cat-c")

	items := findItems(t, d, &model.TransactionFilter{}, nil)
	taxItem := itemByID(t, items, inTax)
	if !taxItem.IsTaxRelevant || taxItem.TaxSource != model.TaxSourceCategory {
		t.Errorf("tax item = %+v", taxItem)
	}
	if taxItem.TaxOverride != nil || taxItem.TaxRulePattern != nil {
		t.Errorf("tax item override/pattern = %v, %v", taxItem.TaxOverride, taxItem.TaxRulePattern)
	}
	normalItem := itemByID(t, items, inNormal)
	if normalItem.IsTaxRelevant || normalItem.TaxSource != model.TaxSourceCategory {
		t.Errorf("normal item = %+v", normalItem)
	}
	uncategorizedItem := itemByID(t, items, uncategorized)
	if uncategorizedItem.IsTaxRelevant {
		t.Error("no category means not tax relevant")
	}
	if uncategorizedItem.TaxSource != model.TaxSourceNone {
		t.Errorf("uncategorized tax source = %q", uncategorizedItem.TaxSource)
	}
}

func TestARuleWithATaxFlagDecidesAtReadTimeForBookingsOfItsOwnCategory(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	flagged := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-rule-a")
	addRule(t, d, "Beispiel", normalCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	unflagged := add(t, d, accountID, date(t, 2026, time.June, 14), -2000,
		stringPointer("Anderer Shop"), nil, &taxCategory, "tax-rule-b")
	addRule(t, d, "Anderer", taxCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(false))

	items := findItems(t, d, &model.TransactionFilter{}, nil)
	flaggedItem := itemByID(t, items, flagged)
	if !flaggedItem.IsTaxRelevant {
		t.Error("a true flag makes a booking of a non-tax category relevant")
	}
	if flaggedItem.TaxSource != model.TaxSourceRule ||
		flaggedItem.TaxRulePattern == nil || *flaggedItem.TaxRulePattern != "Beispiel" {
		t.Errorf("flagged item = %+v", flaggedItem)
	}
	unflaggedItem := itemByID(t, items, unflagged)
	if unflaggedItem.IsTaxRelevant {
		t.Error("a false flag makes a booking of a tax category not relevant")
	}
	if unflaggedItem.TaxSource != model.TaxSourceRule ||
		unflaggedItem.TaxRulePattern == nil || *unflaggedItem.TaxRulePattern != "Anderer" {
		t.Errorf("unflagged item = %+v", unflaggedItem)
	}
}

func TestARuleOfAnotherCategoryAManualRuleAndAFlagLessRuleHaveNoEffect(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	normalCategory := newCategory(t, d, "Groceries", false, false)

	otherCategory := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-other-a")
	addRule(t, d, "Beispiel", taxCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), otherCategory)
	if item.TaxSource != model.TaxSourceCategory || item.IsTaxRelevant {
		t.Errorf("other category item = %+v", item)
	}

	manualOnly := add(t, d, accountID, date(t, 2026, time.June, 14), -2000,
		stringPointer("Erika Mustermann"), nil, &normalCategory, "tax-other-b")
	addRule(t, d, "Erika", normalCategory, 10, false,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	item = itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), manualOnly)
	if item.TaxSource != model.TaxSourceCategory {
		t.Errorf("manual rule item source = %q: autoApply = false has no effect", item.TaxSource)
	}

	laterWins := add(t, d, accountID, date(t, 2026, time.June, 14), -3000,
		stringPointer("Beispiel Vertrieb"), nil, &normalCategory, "tax-other-c")
	addRule(t, d, "Beispiel", normalCategory, 10, true,
		model.RuleFieldAny, model.RuleDirectionAny, nil)
	addRule(t, d, "Vertrieb", normalCategory, 5, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	item = itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), laterWins)
	if !item.IsTaxRelevant {
		t.Error("a flag-less rule is skipped")
	}
	if item.TaxSource != model.TaxSourceRule ||
		item.TaxRulePattern == nil || *item.TaxRulePattern != "Vertrieb" {
		t.Errorf("later item = %+v", item)
	}
}

func TestTwoMatchingTaxRulesPickTheHigherPriorityOrTheLowerIDOnTies(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	highPriority := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-prio-a")
	addRule(t, d, "Beispiel", normalCategory, 10, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(false))
	addRule(t, d, "Beispiel", normalCategory, 20, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), highPriority)
	if !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule {
		t.Errorf("high priority item = %+v: priority 20 beats priority 10", item)
	}

	samePriority := add(t, d, accountID, date(t, 2026, time.June, 14), -2000,
		stringPointer("Erika Mustermann"), nil, &normalCategory, "tax-prio-b")
	addRule(t, d, "Erika", normalCategory, 5, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(false))
	addRule(t, d, "Erika", normalCategory, 5, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	item = itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), samePriority)
	if item.IsTaxRelevant {
		t.Error("on equal priority the lower id wins")
	}
	if item.TaxSource != model.TaxSourceRule ||
		item.TaxRulePattern == nil || *item.TaxRulePattern != "Erika" {
		t.Errorf("same priority item = %+v", item)
	}
}

func TestTheRulesTextFieldAndDirectionAreHonoured(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	partnerOnly := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Buchhaltung GmbH"), stringPointer("Beispiel Shop Rechnung"),
		&normalCategory, "tax-field-a")
	addRule(t, d, "Beispiel Shop", normalCategory, 0, true,
		model.RuleFieldPartner, model.RuleDirectionAny, boolPointer(true))
	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), partnerOnly)
	if item.IsTaxRelevant || item.TaxSource != model.TaxSourceCategory {
		t.Errorf("partner only item = %+v: the PARTNER rule only searches the partner name", item)
	}

	income := add(t, d, accountID, date(t, 2026, time.June, 14), 10_000,
		stringPointer("Beispiel Laden"), nil, &normalCategory, "tax-field-b")
	addRule(t, d, "Beispiel Laden", normalCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionExpense, boolPointer(true))
	item = itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), income)
	if item.IsTaxRelevant || item.TaxSource != model.TaxSourceCategory {
		t.Errorf("income item = %+v: the EXPENSE rule never matches an income booking", item)
	}

	partnerMatch := add(t, d, accountID, date(t, 2026, time.June, 14), -2000,
		stringPointer("Beispiel Shop GmbH"), nil, &normalCategory, "tax-field-c")
	expenseMatch := add(t, d, accountID, date(t, 2026, time.June, 14), -3000,
		stringPointer("Beispiel Laden"), nil, &normalCategory, "tax-field-d")
	items := findItems(t, d, &model.TransactionFilter{}, nil)
	if item := itemByID(t, items, partnerMatch); !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule {
		t.Errorf("partner match item = %+v", item)
	}
	if item := itemByID(t, items, expenseMatch); !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule {
		t.Errorf("expense match item = %+v", item)
	}
}

func TestSettingARulesTaxFlagChangesExistingBookingsWithoutAnyReapply(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	booking := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-later")
	rule := addRule(t, d, "Beispiel", normalCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, nil)

	source := func() model.TransactionItem {
		return itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), booking)
	}
	if source().IsTaxRelevant || source().TaxSource != model.TaxSourceCategory {
		t.Errorf("initial item = %+v", source())
	}

	setRuleTax(t, d, rule, boolPointer(true))
	item := source()
	if !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule ||
		item.TaxRulePattern == nil || *item.TaxRulePattern != "Beispiel" {
		t.Errorf("with the flag item = %+v: the flag applies the moment it is saved", item)
	}

	setRuleTax(t, d, rule, nil)
	item = source()
	if item.IsTaxRelevant || item.TaxSource != model.TaxSourceCategory || item.TaxRulePattern != nil {
		t.Errorf("after reset item = %+v: resetting to null restores the category layer", item)
	}
}

func TestTheOverrideWinsOverRuleAndCategoryAndNullReturnsToAutomatic(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	booking := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-override")
	addRule(t, d, "Beispiel", normalCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))

	load := func() model.TransactionItem {
		return itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), booking)
	}
	if !load().IsTaxRelevant {
		t.Error("the rule does not make it relevant first")
	}

	changed, err := SetTaxOverride(ctx, d, []model.TransactionID{booking}, boolPointer(false))
	if err != nil || changed != 1 {
		t.Fatalf("SetTaxOverride false = %d, %v", changed, err)
	}
	overridden := load()
	if overridden.IsTaxRelevant {
		t.Error("the override does not beat the rule")
	}
	if overridden.TaxSource != model.TaxSourceManual ||
		overridden.TaxOverride == nil || *overridden.TaxOverride {
		t.Errorf("overridden item = %+v", overridden)
	}
	if overridden.TaxRulePattern != nil {
		t.Error("no rule pattern when the override decides")
	}

	changed, err = SetTaxOverride(ctx, d, []model.TransactionID{booking}, boolPointer(true))
	if err != nil || changed != 1 {
		t.Fatalf("SetTaxOverride true = %d, %v", changed, err)
	}
	if item := load(); !item.IsTaxRelevant || item.TaxSource != model.TaxSourceManual {
		t.Errorf("override true item = %+v", item)
	}

	changed, err = SetTaxOverride(ctx, d, []model.TransactionID{booking}, nil)
	if err != nil || changed != 1 {
		t.Fatalf("SetTaxOverride nil = %d, %v", changed, err)
	}
	automatic := load()
	if !automatic.IsTaxRelevant || automatic.TaxSource != model.TaxSourceRule || automatic.TaxOverride != nil {
		t.Errorf("automatic item = %+v: null returns to the automatic result", automatic)
	}
}

func TestTheOverrideSurvivesACategoryChange(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	booking := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("Shop"), nil, &normalCategory, "tax-survive")

	if changed, err := SetTaxOverride(ctx, d, []model.TransactionID{booking}, boolPointer(true)); err != nil || changed != 1 {
		t.Fatalf("SetTaxOverride = %d, %v", changed, err)
	}
	if err := SetCategory(ctx, d, booking, &taxCategory); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}

	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), booking)
	if !item.IsTaxRelevant || item.TaxSource != model.TaxSourceManual ||
		item.TaxOverride == nil || !*item.TaxOverride {
		t.Errorf("item = %+v", item)
	}
}

func TestSetTaxOverrideChunksAndIgnoresEmptyAndUnknownIDs(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	ids := make([]model.TransactionID, 0, 1300)
	for index := 1; index <= 1300; index++ {
		ids = append(ids, add(t, d, accountID, date(t, 2026, time.June, 14), -int64(index),
			stringPointer("Shop"), nil, &normalCategory, "tax-chunk-"+itoa(index)))
	}
	unknown := []model.TransactionID{999_001, 999_002, 999_003}
	withUnknown := append(append([]model.TransactionID(nil), ids...), unknown...)

	changed, err := SetTaxOverride(ctx, d, withUnknown, boolPointer(true))
	if err != nil || changed != 1300 {
		t.Fatalf("SetTaxOverride = %d, %v", changed, err)
	}
	items := findItems(t, d, &model.TransactionFilter{}, nil)
	manual := 0
	for _, item := range items {
		if item.TaxSource == model.TaxSourceManual && item.TaxOverride != nil && *item.TaxOverride {
			manual++
		}
	}
	if manual != 1300 {
		t.Errorf("manual rows = %d", manual)
	}
	if changed, err := SetTaxOverride(ctx, d, nil, boolPointer(true)); err != nil || changed != 0 {
		t.Errorf("SetTaxOverride empty = %d, %v", changed, err)
	}
	if changed, err := SetTaxOverride(ctx, d, unknown, boolPointer(true)); err != nil || changed != 0 {
		t.Errorf("SetTaxOverride unknown = %d, %v", changed, err)
	}
	manual = 0
	for _, item := range findItems(t, d, &model.TransactionFilter{}, nil) {
		if item.TaxSource == model.TaxSourceManual {
			manual++
		}
	}
	if manual != 1300 {
		t.Errorf("manual rows after the no-ops = %d", manual)
	}
}

func TestTaxRelevantOnlyReturnsExactlyTheEffectiveBookingsWithTheLimitAfterTheFilter(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	byCategory := add(t, d, accountID, date(t, 2026, time.June, 11), -1000,
		stringPointer("Steuer Shop"), nil, &taxCategory, "tax-only-a")
	byRule := add(t, d, accountID, date(t, 2026, time.June, 12), -2000,
		stringPointer("Beispiel Shop"), nil, &normalCategory, "tax-only-b")
	addRule(t, d, "Beispiel", normalCategory, 0, true,
		model.RuleFieldAny, model.RuleDirectionAny, boolPointer(true))
	overriddenFalse := add(t, d, accountID, date(t, 2026, time.June, 13), -3000,
		stringPointer("Widerspruch Shop"), nil, &taxCategory, "tax-only-c")
	plain := add(t, d, accountID, date(t, 2026, time.June, 14), -4000,
		stringPointer("Normaler Shop"), nil, &normalCategory, "tax-only-d")
	if changed, err := SetTaxOverride(ctx, d, []model.TransactionID{overriddenFalse}, boolPointer(false)); err != nil || changed != 1 {
		t.Fatalf("SetTaxOverride = %d, %v", changed, err)
	}

	relevant := findItems(t, d, &model.TransactionFilter{TaxRelevantOnly: true}, nil)
	ids := make(map[model.TransactionID]struct{}, len(relevant))
	for _, item := range relevant {
		ids[item.ID] = struct{}{}
	}
	if len(ids) != 2 {
		t.Fatalf("relevant = %v", ids)
	}
	if _, present := ids[byCategory]; !present {
		t.Error("the category-flagged booking is missing")
	}
	if _, present := ids[byRule]; !present {
		t.Error("the rule-flagged booking is missing")
	}
	for _, item := range relevant {
		if item.ID == plain {
			t.Error("the plain booking is relevant")
		}
	}

	limit := 1
	limited := findItems(t, d, &model.TransactionFilter{TaxRelevantOnly: true}, &limit)
	if len(limited) != 1 {
		t.Fatalf("limited = %d: the limit applies after the filter", len(limited))
	}
	if limited[0].ID != relevant[0].ID {
		t.Errorf("limited first = %d, want the newest relevant booking %d", limited[0].ID, relevant[0].ID)
	}
}

func TestATaxFlaggedShopRuleMakesPaypalBookingsOfItsCategoryRelevant(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	partner := "PayPal Europe S.a.r.l. et Cie S.C.A"
	byRule := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer(partner),
		stringPointer("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		&normalCategory, "tax-paypal-a")
	otherShop := add(t, d, accountID, date(t, 2026, time.June, 14), -2000,
		stringPointer(partner),
		stringPointer("EREF+R2 SVWZ+PP.472.PPX . Anderer Shop GmbH, Ihr Einkauf bei Anderer Shop GmbH"),
		&normalCategory, "tax-paypal-b")
	addRule(t, d, "Beispiel Shop", normalCategory, 0, true,
		model.RuleFieldPartner, model.RuleDirectionAny, boolPointer(true))

	items := findItems(t, d, &model.TransactionFilter{}, nil)
	byRuleItem := itemByID(t, items, byRule)
	if !byRuleItem.IsTaxRelevant || byRuleItem.TaxSource != model.TaxSourceRule ||
		byRuleItem.TaxRulePattern == nil || *byRuleItem.TaxRulePattern != "Beispiel Shop" {
		t.Errorf("by rule item = %+v: the PARTNER rule matches the shop extracted from the purpose", byRuleItem)
	}
	otherItem := itemByID(t, items, otherShop)
	if otherItem.IsTaxRelevant || otherItem.TaxSource != model.TaxSourceCategory {
		t.Errorf("other shop item = %+v", otherItem)
	}
}

func TestAProviderRuleGivesWayToASameCategoryShopTaxRule(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	taxCategory := newCategory(t, d, "Office supplies", true, false)
	booking := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		stringPointer("PayPal Europe S.a.r.l. et Cie S.C.A"),
		stringPointer("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		&taxCategory, "tax-paypal-provider")
	addRule(t, d, "PayPal", taxCategory, 10, true,
		model.RuleFieldPartner, model.RuleDirectionAny, boolPointer(false))
	addRule(t, d, "Beispiel Shop", taxCategory, 0, true,
		model.RuleFieldPartner, model.RuleDirectionAny, boolPointer(true))

	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), booking)
	if !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule ||
		item.TaxRulePattern == nil || *item.TaxRulePattern != "Beispiel Shop" {
		t.Errorf("item = %+v: the shop rule, not the PayPal rule, decides the tax flag", item)
	}
}

func TestATaxFlaggedShopRuleAppliesToABlankPartnerCardBooking(t *testing.T) {
	d := database(t)
	accountID := account(t, d)
	normalCategory := newCategory(t, d, "Groceries", false, false)
	booking := add(t, d, accountID, date(t, 2026, time.June, 14), -1000,
		nil, stringPointer("PAYPAL *BEISPIELSHOP"), &normalCategory, "tax-paypal-card")
	addRule(t, d, "BEISPIELSHOP", normalCategory, 0, true,
		model.RuleFieldPartner, model.RuleDirectionAny, boolPointer(true))

	item := itemByID(t, findItems(t, d, &model.TransactionFilter{}, nil), booking)
	if !item.IsTaxRelevant || item.TaxSource != model.TaxSourceRule ||
		item.TaxRulePattern == nil || *item.TaxRulePattern != "BEISPIELSHOP" {
		t.Errorf("item = %+v: the PARTNER rule matches the shop in the card descriptor", item)
	}
}

func TestTheNewMutationsWriteExactlyOneAuditEntryEach(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	category := newCategory(t, d, "Food", false, false)
	first := add(t, d, accountID, date(t, 2026, time.June, 1), -100,
		stringPointer("One"), nil, nil, "audit-new-a")
	second := add(t, d, accountID, date(t, 2026, time.June, 2), -200,
		stringPointer("Two"), nil, nil, "audit-new-b")
	before := len(recentAudit(t, d))
	expected := before

	if _, err := UpdateCategories(ctx, d, []CategoryChange{{TransactionID: first, CategoryID: &category}}); err != nil {
		t.Fatalf("UpdateCategories: %v", err)
	}
	expected++
	if got := len(recentAudit(t, d)); got != expected {
		t.Errorf("audit after UpdateCategories = %d, want %d", got, expected)
	}

	if _, err := SetTaxOverride(ctx, d, []model.TransactionID{first}, boolPointer(true)); err != nil {
		t.Fatalf("SetTaxOverride: %v", err)
	}
	expected++
	if got := len(recentAudit(t, d)); got != expected {
		t.Errorf("audit after SetTaxOverride = %d, want %d", got, expected)
	}

	if err := MarkRead(ctx, d, nil); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	expected++
	if got := len(recentAudit(t, d)); got != expected {
		t.Errorf("audit after MarkRead = %d, want %d", got, expected)
	}

	if _, err := DeleteAll(ctx, d, []model.TransactionID{first, second}); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	expected++
	if got := len(recentAudit(t, d)); got != expected {
		t.Errorf("audit after DeleteAll = %d, want %d", got, expected)
	}

	if _, err := ForgetDeleted(ctx, d, accountID); err != nil {
		t.Fatalf("ForgetDeleted: %v", err)
	}
	expected++
	if got := len(recentAudit(t, d)); got != expected {
		t.Errorf("audit after ForgetDeleted = %d, want %d", got, expected)
	}

	for _, entry := range recentAudit(t, d) {
		if contains(entry.Details, "One") || contains(entry.Details, "Two") {
			t.Errorf("audit details leaked a partner name: %q", entry.Details)
		}
	}
}

func TestForgetDeletedBookingsNamesTheCountAndWritesNothingWhenNoneAreRemembered(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	booking := add(t, d, accountID, date(t, 2026, time.October, 5), -500,
		stringPointer("Cafe"), nil, nil, "forget-view-a")
	if _, err := DeleteAll(ctx, d, []model.TransactionID{booking}); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	before := len(recentAudit(t, d))

	removed, err := ForgetDeletedBookings(ctx, d, accountID)
	if err != nil || removed != 1 {
		t.Fatalf("ForgetDeletedBookings = %d, %v", removed, err)
	}
	entries := recentAudit(t, d)
	if len(entries) != before+1 {
		t.Fatalf("audit entries = %d", len(entries))
	}
	if entries[0].Action != "FORGET_DELETED_BOOKINGS" {
		t.Errorf("action = %q", entries[0].Action)
	}
	if entries[0].Details != "Account 3000: 1 deleted bookings may be imported again" {
		t.Errorf("details = %q", entries[0].Details)
	}

	removed, err = ForgetDeletedBookings(ctx, d, accountID)
	if err != nil || removed != 0 {
		t.Errorf("second ForgetDeletedBookings = %d, %v", removed, err)
	}
	if got := len(recentAudit(t, d)); got != before+1 {
		t.Errorf("audit entries after the no-op = %d, want %d: nothing remembered means no mutation and no audit entry", got, before+1)
	}
}

func TestFindItemsMatchesRustSQL(t *testing.T) {
	start := date(t, 2026, time.January, 2)
	end := date(t, 2026, time.March, 4)
	accountID := model.AccountID(7)
	accountIDs := model.AccountIDSet{9: {}, 8: {}}
	categoryID := model.CategoryID(3)
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	minAmount := eur(1000)
	maxAmount := eur(2500)
	text := " 50%_\\O'Brien  "
	limit := 5
	filter := &model.TransactionFilter{
		StartDate:         &start,
		EndDate:           &end,
		AccountID:         &accountID,
		AccountIDs:        &accountIDs,
		CategoryID:        &categoryID,
		UncategorizedOnly: true,
		TaxRelevantOnly:   true,
		Direction:         model.DirectionExpenses,
		Text:              &text,
		MinAmount:         &minAmount,
		MaxAmount:         &maxAmount,
		Currency:          &usd,
	}

	sqlText, values, err := findItemsSQL(filter, &limit)
	if err != nil {
		t.Fatalf("findItemsSQL: %v", err)
	}

	wantSQL := "SELECT t.id, t.account_id, t.booking_date, t.amount_minor, t.currency, t.balance_after_minor,\n" +
		"                    t.partner_name, t.purpose, t.category_id, t.import_hash, t.created_at, t.is_new, t.tax_override,\n" +
		"                    a.iban, c.name, c.color, c.is_tax_relevant, c.is_transfer\n" +
		"             FROM transactions t\n" +
		"             JOIN accounts a ON a.id = t.account_id\n" +
		"             LEFT JOIN categories c ON c.id = t.category_id\n" +
		"             WHERE 1 = 1 AND t.booking_date >= ? AND t.booking_date <= ? AND t.account_id = ? " +
		"AND t.account_id IN (?, ?) AND t.category_id = ? AND t.currency = ? AND t.category_id IS NULL " +
		"AND t.amount_minor < 0 AND abs(t.amount_minor) >= ? AND abs(t.amount_minor) <= ? " +
		"AND (t.partner_name LIKE ? ESCAPE '\\' OR t.purpose LIKE ? ESCAPE '\\') " +
		"ORDER BY t.booking_date DESC, t.id DESC"
	if sqlText != wantSQL {
		t.Errorf("SQL =\n%s\nwant\n%s", sqlText, wantSQL)
	}

	wantValues := []any{
		"2026-01-02",
		"2026-03-04",
		int64(7),
		int64(8),
		int64(9),
		int64(3),
		"USD",
		int64(1000),
		int64(2500),
		"%50\\%\\_\\\\O'Brien%",
		"%50\\%\\_\\\\O'Brien%",
	}
	if len(values) != len(wantValues) {
		t.Fatalf("values = %#v, want %#v", values, wantValues)
	}
	for index := range wantValues {
		if values[index] != wantValues[index] {
			t.Errorf("value %d = %#v, want %#v", index, values[index], wantValues[index])
		}
	}
}

func TestFindItemsAddsTheLimitOnlyWithoutTheTaxFilter(t *testing.T) {
	limit := 5
	plain, plainValues, err := findItemsSQL(&model.TransactionFilter{}, &limit)
	if err != nil {
		t.Fatalf("findItemsSQL: %v", err)
	}
	wantPlain := "SELECT t.id, t.account_id, t.booking_date, t.amount_minor, t.currency, t.balance_after_minor,\n" +
		"                    t.partner_name, t.purpose, t.category_id, t.import_hash, t.created_at, t.is_new, t.tax_override,\n" +
		"                    a.iban, c.name, c.color, c.is_tax_relevant, c.is_transfer\n" +
		"             FROM transactions t\n" +
		"             JOIN accounts a ON a.id = t.account_id\n" +
		"             LEFT JOIN categories c ON c.id = t.category_id\n" +
		"             WHERE 1 = 1 ORDER BY t.booking_date DESC, t.id DESC LIMIT ?"
	if plain != wantPlain {
		t.Errorf("plain SQL =\n%s\nwant\n%s", plain, wantPlain)
	}
	if !slices.Equal(plainValues, []any{int64(5)}) {
		t.Errorf("plain values = %#v", plainValues)
	}

	taxOnly, taxValues, err := findItemsSQL(&model.TransactionFilter{TaxRelevantOnly: true}, &limit)
	if err != nil {
		t.Fatalf("findItemsSQL: %v", err)
	}
	if taxOnly != wantPlain[:len(wantPlain)-len(" LIMIT ?")] {
		t.Errorf("tax-only SQL =\n%s", taxOnly)
	}
	if len(taxValues) != 0 {
		t.Errorf("tax-only values = %#v", taxValues)
	}
}

func TestTransactionRowsMatchRustEncoding(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	category := newCategory(t, d, "Food", true, false)
	createdAt := time.Date(2026, time.October, 4, 9, 30, 0, 123456789, time.UTC)
	balance := eur(-1234)
	taxOverride := true

	outcome, err := InsertImported(ctx, d, accountID, []model.NewTransaction{{
		BookingDate:  date(t, 2026, time.October, 3),
		Amount:       eur(-450),
		BalanceAfter: &balance,
		PartnerName:  stringPointer("Bäcker"),
		Purpose:      stringPointer("Kaffee"),
		CategoryID:   &category,
		ImportHash:   "hash",
		CreatedAt:    createdAt,
		IsNew:        true,
		TaxOverride:  &taxOverride,
	}})
	if err != nil || outcome.Inserted != 1 {
		t.Fatalf("InsertImported = %+v, %v", outcome, err)
	}

	var (
		bookingDate  string
		createdText  string
		isNew        int64
		taxValue     *int64
		currencyText string
		importHash   string
	)
	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(
			ctx,
			"SELECT booking_date, created_at, is_new, tax_override, currency, import_hash "+
				"FROM transactions WHERE import_hash = ?1",
			"hash",
		).Scan(&bookingDate, &createdText, &isNew, &taxValue, &currencyText, &importHash)
	}); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if bookingDate != "2026-10-03" {
		t.Errorf("booking_date = %q", bookingDate)
	}
	if createdText != createdAt.Format(time.RFC3339Nano) {
		t.Errorf("created_at = %q, want %q", createdText, createdAt.Format(time.RFC3339Nano))
	}
	if isNew != 1 {
		t.Errorf("is_new = %d", isNew)
	}
	if taxValue == nil || *taxValue != 1 {
		t.Errorf("tax_override = %v", taxValue)
	}
	if currencyText != "EUR" {
		t.Errorf("currency = %q", currencyText)
	}
	if importHash != "hash" {
		t.Errorf("import_hash = %q", importHash)
	}
}

func TestExportedFunctionsRoundTripNotFoundAndAudit(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d)
	category := newCategory(t, d, "Food", false, false)

	// InsertImported round trip, audit entry and not-found account.
	outcome, err := InsertImported(ctx, d, accountID, []model.NewTransaction{entry("round-trip")})
	if err != nil || outcome.Inserted != 1 {
		t.Fatalf("InsertImported = %+v, %v", outcome, err)
	}
	entries := recentAudit(t, d)
	if entries[0].Action != "transactions.import" || entries[0].Actor != audit.ActorUser ||
		entries[0].Details != "inserted=1 skipped_duplicate=0 skipped_deleted=0" {
		t.Errorf("import audit = %+v", entries[0])
	}
	if _, err := InsertImported(ctx, d, 999_999, []model.NewTransaction{entry("missing")}); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("InsertImported of an unknown account = %v, want ErrNotFound", err)
	}

	// InsertPlanned round trip, no audit entry and not-found account.
	before := len(recentAudit(t, d))
	inserted, err := InsertPlanned(ctx, d, accountID, []model.NewTransaction{entry("planned")})
	if err != nil || inserted != 1 {
		t.Fatalf("InsertPlanned = %d, %v", inserted, err)
	}
	if got := len(recentAudit(t, d)); got != before {
		t.Errorf("InsertPlanned wrote an audit entry: %d -> %d", before, got)
	}
	if _, err := InsertPlanned(ctx, d, 999_999, []model.NewTransaction{entry("missing")}); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("InsertPlanned of an unknown account = %v, want ErrNotFound", err)
	}

	// ListForAccount round trip and empty range.
	bookings, err := ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 2 {
		t.Fatalf("ListForAccount = %d, %v", len(bookings), err)
	}
	start := date(t, 2026, time.October, 3)
	end := start
	between, err := ListForAccount(ctx, d, accountID, &start, &end)
	if err != nil || len(between) != 2 {
		t.Errorf("ListForAccount range = %d, %v", len(between), err)
	}

	// HasBookings true and false.
	if has, err := HasBookings(ctx, d, accountID); err != nil || !has {
		t.Errorf("HasBookings = %v, %v", has, err)
	}
	emptyAccount := accountWith(t, d, "Empty", "DE02120300000000202051", money.EUR)
	if has, err := HasBookings(ctx, d, emptyAccount); err != nil || has {
		t.Errorf("HasBookings empty = %v, %v", has, err)
	}

	// SetCategory round trip, not found and audit entry.
	bookingID := bookings[0].ID
	if err := SetCategory(ctx, d, bookingID, &category); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}
	entries = recentAudit(t, d)
	if entries[0].Action != "transaction.set_category" || entries[0].Actor != audit.ActorUser ||
		entries[0].Details != repo.IDOnly(int64(bookingID)) {
		t.Errorf("set category audit = %+v", entries[0])
	}
	if err := SetCategory(ctx, d, 999_999, &category); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("SetCategory of an unknown id = %v, want ErrNotFound", err)
	}

	// Delete round trip, not found and audit entry.
	if err := Delete(ctx, d, bookingID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entries = recentAudit(t, d)
	if entries[0].Action != "transaction.delete" || entries[0].Actor != audit.ActorUser ||
		entries[0].Details != repo.IDOnly(int64(bookingID)) {
		t.Errorf("delete audit = %+v", entries[0])
	}
	if err := Delete(ctx, d, bookingID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}

	// DeletedHashes round trip.
	deleted, err := DeletedHashes(ctx, d, accountID)
	if err != nil || len(deleted) != 1 {
		t.Errorf("DeletedHashes = %v, %v", deleted, err)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
