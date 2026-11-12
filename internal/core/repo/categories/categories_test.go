package categories

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
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

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func newCategory(name string) model.NewCategory {
	return model.NewCategory{
		Name:          name,
		Color:         "#3BC7B0",
		BudgetLimit:   nil,
		IsTaxRelevant: false,
		IsTransfer:    false,
	}
}

func auditActions(t *testing.T, d *db.Database) [][2]string {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	actions := make([][2]string, 0, len(recent))
	for _, entry := range recent {
		actions = append(actions, [2]string{entry.Action, entry.Details})
	}
	return actions
}

func categoryNames(t *testing.T, d *db.Database) []string {
	t.Helper()

	categories, err := List(context.Background(), d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, category.Name)
	}
	return names
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestThePaletteHas24DistinctColoursAndTheDefaultsUseThemInOrder(t *testing.T) {
	if len(Colors) != 24 {
		t.Fatalf("Colors length = %d", len(Colors))
	}
	distinct := make(map[string]struct{})
	for _, color := range Colors {
		distinct[stringsToUpper(color)] = struct{}{}
	}
	if len(distinct) != 24 {
		t.Errorf("the colours must be distinct; got %d", len(distinct))
	}

	if len(Entries) != 24 {
		t.Fatalf("Entries length = %d", len(Entries))
	}
	entryColors := make(map[string]struct{})
	ordered := make([]string, 0, len(Entries))
	for _, entry := range Entries {
		entryColors[entry.Color] = struct{}{}
		ordered = append(ordered, entry.Color)
	}
	if len(entryColors) != 24 {
		t.Errorf("no cycling: every category needs its own colour; got %d", len(entryColors))
	}
	if !slices.Equal(ordered, Colors[:]) {
		t.Errorf("entry colours = %v, want %v", ordered, Colors[:])
	}
}

func TestThePaletteStaysInTheDocumentedOrder(t *testing.T) {
	if got := join(Colors[0:6]); got != "#E76530#4693F3#C38400#D56A93#867DFF#14AC77" {
		t.Errorf("first six = %s", got)
	}
	if got := join(Colors[6:12]); got != "#CF4E12#2F7DDB#A77105#BD547E#7364EC#029465" {
		t.Errorf("second six = %s", got)
	}
	if got := join(Colors[12:18]); got != "#DB5923#3B88E7#B57A03#C95F88#7C6FF8#00A06E" {
		t.Errorf("third six = %s", got)
	}
	if got := join(Colors[18:]); got != "#BE4403#2170CD#986603#AF4872#6857DE#07865C" {
		t.Errorf("last six = %s", got)
	}
}

func TestOnlyOwnTransfersIsATransfer(t *testing.T) {
	transfers := make([]DefaultCategory, 0)
	for _, entry := range Entries {
		if entry.IsTransfer {
			transfers = append(transfers, entry)
		}
	}
	if len(transfers) != 1 {
		t.Fatalf("transfers = %d", len(transfers))
	}
	if transfers[0].Key != i18n.DefaultCategoryOwnTransfers {
		t.Errorf("transfer key = %v", transfers[0].Key)
	}
	for _, entry := range Entries {
		if entry.Key == i18n.DefaultCategoryCreditCard && entry.IsTransfer {
			t.Error("the credit card category is a transfer")
		}
	}
}

func TestSeedingAnEmptyDatabaseAddsAll24DefaultsAndAuditsOnce(t *testing.T) {
	d := database(t)

	added, err := SeedDefaults(context.Background(), d, i18n.En)
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	if added != 24 {
		t.Errorf("added = %d", added)
	}
	names := categoryNames(t, d)
	if len(names) != 24 {
		t.Errorf("names length = %d", len(names))
	}
	for _, want := range []string{"Groceries", "Drugstore & household", "Other income"} {
		if !slices.Contains(names, want) {
			t.Errorf("names do not contain %q", want)
		}
	}

	entries := auditActions(t, d)
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d", len(entries))
	}
	if entries[0][0] != "ADD_DEFAULT_CATEGORIES" || entries[0][1] != "Added 24 default categories" {
		t.Errorf("audit entry = %v", entries[0])
	}
}

func TestSeedingStoresTheTransferFlag(t *testing.T) {
	d := database(t)
	if _, err := SeedDefaults(context.Background(), d, i18n.En); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	saved := make(map[string]bool)
	categories, err := List(context.Background(), d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, category := range categories {
		saved[category.Name] = category.IsTransfer
	}
	if saved["Groceries"] {
		t.Error("Groceries is a transfer")
	}
	if saved["Credit card"] {
		t.Error("Credit card is a transfer")
	}
	if !saved["Own transfers"] {
		t.Error("Own transfers is not a transfer")
	}
}

func TestSeedingUsesTheLanguageNamesAndDefaultColours(t *testing.T) {
	d := database(t)
	if _, err := SeedDefaults(context.Background(), d, i18n.De); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	categories, err := List(context.Background(), d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var groceries *model.Category
	for index := range categories {
		if categories[index].Name == "Lebensmittel" {
			groceries = &categories[index]
			break
		}
	}
	if groceries == nil {
		t.Fatal("Lebensmittel is missing")
	}
	if groceries.Color != "#E76530" {
		t.Errorf("colour = %q", groceries.Color)
	}
}

func TestExistingCategoriesAreKeptAndMatchedCaseInsensitively(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	existingNew := newCategory("groceries")
	existingNew.Color = "#123456"
	existing, err := Create(ctx, d, existingNew)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	added, err := SeedDefaults(ctx, d, i18n.En)
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if added != 23 {
		t.Errorf("added = %d", added)
	}
	categories, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(categories) != 24 {
		t.Fatalf("categories = %d", len(categories))
	}
	var groceries *model.Category
	for index := range categories {
		if categories[index].ID == existing {
			groceries = &categories[index]
			break
		}
	}
	if groceries == nil {
		t.Fatal("the existing category is missing")
	}
	if groceries.Name != "groceries" || groceries.Color != "#123456" {
		t.Errorf("existing categories are never changed: %+v", *groceries)
	}

	added, err = SeedDefaults(ctx, d, i18n.En)
	if err != nil || added != 0 {
		t.Errorf("second SeedDefaults = %d, %v", added, err)
	}
	if got := categoryNames(t, d); len(got) != 24 {
		t.Errorf("categories after the second seed = %d", len(got))
	}
}

func TestTwoSeedCallsCreateTheDefaultsOnce(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	if added, err := SeedDefaults(ctx, d, i18n.En); err != nil || added != 24 {
		t.Fatalf("first SeedDefaults = %d, %v", added, err)
	}
	if added, err := SeedDefaults(ctx, d, i18n.En); err != nil || added != 0 {
		t.Fatalf("second SeedDefaults = %d, %v", added, err)
	}

	names := categoryNames(t, d)
	if len(names) != 24 {
		t.Errorf("names length = %d", len(names))
	}
	unique := make(map[string]struct{})
	for _, name := range names {
		unique[name] = struct{}{}
	}
	if len(unique) != len(names) {
		t.Error("a category was created twice")
	}
	if got := len(auditActions(t, d)); got != 2 {
		t.Errorf("audit entries = %d, want 2; each call is audited", got)
	}
}

func TestDefaultColoursMatchLocalizedDefaultNames(t *testing.T) {
	if got := DefaultColourFor("  groceries ", i18n.En); got == nil || *got != "#E76530" {
		t.Errorf("DefaultColourFor(groceries, En) = %v", got)
	}
	if got := DefaultColourFor("Lebensmittel", i18n.De); got == nil || *got != "#E76530" {
		t.Errorf("DefaultColourFor(Lebensmittel, De) = %v", got)
	}
	if got := DefaultColourFor("not a default", i18n.En); got != nil {
		t.Errorf("DefaultColourFor(not a default) = %v", got)
	}
}

func TestGetUpdateAndDeleteRoundTrip(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	created, err := Create(ctx, d, newCategory("Food"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	loaded, ok, err := Get(ctx, d, created)
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if loaded.Name != "Food" || loaded.Color != "#3BC7B0" || loaded.BudgetLimit != nil ||
		loaded.IsTaxRelevant || loaded.IsTransfer {
		t.Errorf("loaded = %+v", loaded)
	}

	update := newCategory("Renamed")
	update.Color = "#000000"
	budget := eur(50_000)
	update.BudgetLimit = &budget
	update.IsTaxRelevant = true
	update.IsTransfer = true
	if err := Update(ctx, d, created, update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	loaded, ok, err = Get(ctx, d, created)
	if err != nil || !ok {
		t.Fatalf("Get after Update = %v, %v", ok, err)
	}
	if loaded.Name != "Renamed" || loaded.Color != "#000000" || loaded.BudgetLimit == nil ||
		*loaded.BudgetLimit != budget || !loaded.IsTaxRelevant || !loaded.IsTransfer {
		t.Errorf("updated = %+v", loaded)
	}

	entries, err := audit.Recent(ctx, d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d", len(entries))
	}
	if entries[1].Action != "category.create" ||
		entries[1].Actor != audit.ActorUser ||
		entries[1].Details != "id="+itoa(int64(created))+" name=Food" {
		t.Errorf("create audit = %+v", entries[1])
	}
	if entries[0].Action != "category.update" ||
		entries[0].Actor != audit.ActorUser ||
		entries[0].Details != "id="+itoa(int64(created))+" name=Renamed" {
		t.Errorf("update audit = %+v", entries[0])
	}

	if err := Delete(ctx, d, created); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := Get(ctx, d, created); err != nil || ok {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	entries, err = audit.Recent(ctx, d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	if entries[0].Action != "category.delete" || entries[0].Actor != audit.ActorUser ||
		entries[0].Details != "id="+itoa(int64(created)) {
		t.Errorf("delete audit = %+v", entries[0])
	}

	if err := Delete(ctx, d, created); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if err := Update(ctx, d, model.CategoryID(999999), newCategory("Missing")); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("Update of an unknown category = %v, want ErrNotFound", err)
	}
	if _, _, err := Get(ctx, d, model.CategoryID(999999)); err != nil {
		t.Errorf("Get of an unknown category = %v", err)
	}
}

func TestBudgetsMustUseTheBaseCurrency(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	budget := money.Money{Minor: 1, Currency: usd}

	new := newCategory("Food")
	new.BudgetLimit = &budget
	if _, err := Create(ctx, d, new); !errors.Is(err, repo.ErrCurrencyMismatch) {
		t.Errorf("Create with a USD budget = %v, want ErrCurrencyMismatch", err)
	}

	id, err := Create(ctx, d, newCategory("Food"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Update(ctx, d, id, new); !errors.Is(err, repo.ErrCurrencyMismatch) {
		t.Errorf("Update with a USD budget = %v, want ErrCurrencyMismatch", err)
	}
}

func TestDeletingACategoryUncategorizesBookingsAndDeletesItsRules(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	accountID, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        pointer("Account"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     eur(0),
	})
	if err != nil {
		t.Fatalf("Create account: %v", err)
	}
	category, err := Create(ctx, d, newCategory("Food"))
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	rule, err := rules.Create(ctx, d, model.NewRule{
		Pattern:    "market",
		CategoryID: category,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldPartner,
		Direction:  model.RuleDirectionExpense,
	})
	if err != nil {
		t.Fatalf("Create rule: %v", err)
	}

	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO transactions "+
				"(account_id, booking_date, amount_minor, currency, partner_name, purpose, category_id, import_hash, created_at, is_new) "+
				"VALUES (?1, '2024-01-01', -500, 'EUR', 'Partner', 'Purpose', ?2, 'hash', '2024-01-01T00:00:00Z', 0)",
			int64(accountID), int64(category),
		)
		return err
	})
	if err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	if err := Delete(ctx, d, category); err != nil {
		t.Fatalf("Delete category: %v", err)
	}

	if _, ok, err := Get(ctx, d, category); err != nil || ok {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	listedRules, err := rules.List(ctx, d)
	if err != nil {
		t.Fatalf("rules.List: %v", err)
	}
	if len(listedRules) != 0 {
		t.Errorf("rules after deleting the category = %d", len(listedRules))
	}
	if rule != 1 {
		t.Errorf("rule id = %d", rule)
	}

	var categoryID *int64
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT category_id FROM transactions").Scan(&categoryID)
	})
	if err != nil {
		t.Fatalf("read transaction category: %v", err)
	}
	if categoryID != nil {
		t.Errorf("transaction category = %v, want nil", *categoryID)
	}
}

func pointer(value string) *string {
	return &value
}

func join(values []string) string {
	result := ""
	for _, value := range values {
		result += value
	}
	return result
}

func stringsToUpper(value string) string {
	bytes := []byte(value)
	for index := range bytes {
		if bytes[index] >= 'a' && bytes[index] <= 'z' {
			bytes[index] -= 'a' - 'A'
		}
	}
	return string(bytes)
}
