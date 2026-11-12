// Package categories is the category repository.
//
// It ports `crates/finglass-core/src/repo/categories.rs`.
package categories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

// Colors is the 24-colour category palette, in display order: six hues in four
// tones.
//
// Neighbours differ in hue, and every colour sits in the lightness band that
// works on both the light and the dark surface. Green and red are left out on
// purpose: green means money in, red means an alert.
var Colors = [24]string{
	"#E76530", // orange 4
	"#4693F3", // blue 4
	"#C38400", // yellow 4
	"#D56A93", // magenta 4
	"#867DFF", // violet 4
	"#14AC77", // aqua 4
	"#CF4E12", // orange 2
	"#2F7DDB", // blue 2
	"#A77105", // yellow 2
	"#BD547E", // magenta 2
	"#7364EC", // violet 2
	"#029465", // aqua 2
	"#DB5923", // orange 3
	"#3B88E7", // blue 3
	"#B57A03", // yellow 3
	"#C95F88", // magenta 3
	"#7C6FF8", // violet 3
	"#00A06E", // aqua 3
	"#BE4403", // orange 1
	"#2170CD", // blue 1
	"#986603", // yellow 1
	"#AF4872", // magenta 1
	"#6857DE", // violet 1
	"#07865C", // aqua 1
}

// DefaultCategory is one default category: its string key, colour and transfer
// flag.
type DefaultCategory struct {
	// Key is the i18n key of the localized name.
	Key i18n.Key
	// Color is the palette colour, entry i being Colors[i].
	Color string
	// IsTransfer says whether the category marks transfers.
	IsTransfer bool
}

// Entries are the default categories in display order: name key, colour and
// transfer flag.
//
// Entry `i` is coloured `Colors[i]`. The names come from the string keys, so the
// current UI language decides what is stored. Only "Own transfers" is a
// transfer: a card statement payment is the only record of card spending while
// the card's own bookings come from no import, so counting it would erase the
// card's spending altogether.
var Entries = [24]DefaultCategory{
	{Key: i18n.DefaultCategoryGroceries, Color: Colors[0]},
	{Key: i18n.DefaultCategoryDrugstore, Color: Colors[1]},
	{Key: i18n.DefaultCategoryHousing, Color: Colors[2]},
	{Key: i18n.DefaultCategoryEnergy, Color: Colors[3]},
	{Key: i18n.DefaultCategoryPhoneInternetMedia, Color: Colors[4]},
	{Key: i18n.DefaultCategoryInsurance, Color: Colors[5]},
	{Key: i18n.DefaultCategoryLoans, Color: Colors[6]},
	{Key: i18n.DefaultCategorySavings, Color: Colors[7]},
	{Key: i18n.DefaultCategoryTransport, Color: Colors[8]},
	{Key: i18n.DefaultCategoryHealth, Color: Colors[9]},
	{Key: i18n.DefaultCategoryClothing, Color: Colors[10]},
	{Key: i18n.DefaultCategoryOnlineShopping, Color: Colors[11]},
	{Key: i18n.DefaultCategoryLeisure, Color: Colors[12]},
	{Key: i18n.DefaultCategoryRestaurants, Color: Colors[13]},
	{Key: i18n.DefaultCategoryTaxes, Color: Colors[14]},
	{Key: i18n.DefaultCategoryCash, Color: Colors[15]},
	// NOT a transfer: see the note above.
	{Key: i18n.DefaultCategoryCreditCard, Color: Colors[16]},
	{Key: i18n.DefaultCategoryOwnTransfers, Color: Colors[17], IsTransfer: true},
	{Key: i18n.DefaultCategorySalary, Color: Colors[18]},
	{Key: i18n.DefaultCategoryChildBenefit, Color: Colors[19]},
	{Key: i18n.DefaultCategoryInterestFees, Color: Colors[20]},
	{Key: i18n.DefaultCategoryFamily, Color: Colors[21]},
	{Key: i18n.DefaultCategoryChildrenSchool, Color: Colors[22]},
	{Key: i18n.DefaultCategoryOtherIncome, Color: Colors[23]},
}

// DefaultColourFor returns the palette colour of the default category whose
// localized name equals name, or nil.
func DefaultColourFor(name string, lang i18n.Language) *string {
	trimmed := strings.TrimSpace(name)
	for _, entry := range Entries {
		if equalASCIIFold(strings.TrimSpace(i18n.Text(lang, entry.Key)), trimmed) {
			color := entry.Color
			return &color
		}
	}
	return nil
}

// equalASCIIFold matches Rust's `eq_ignore_ascii_case`.
func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := 0; index < len(left); index++ {
		if lowerASCII(left[index]) != lowerASCII(right[index]) {
			return false
		}
	}
	return true
}

func lowerASCII(character byte) byte {
	if character >= 'A' && character <= 'Z' {
		return character + ('a' - 'A')
	}
	return character
}

// SeedDefaults adds the default categories that don't exist yet, in one
// transaction with one audit entry, as FinDesk's "Add default categories" does.
//
// A default is skipped when a category with the same name (trimmed,
// case-insensitive) exists; existing categories are never changed. The audit
// entry is written also when nothing was added. It returns how many categories
// were added.
func SeedDefaults(ctx context.Context, d *db.Database, lang i18n.Language) (int, error) {
	added := 0

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		existing, err := existingNames(ctx, tx)
		if err != nil {
			return err
		}

		for _, entry := range Entries {
			name := strings.TrimSpace(i18n.Text(lang, entry.Key))
			if _, present := existing[strings.ToLower(name)]; present {
				continue
			}
			if _, err := tx.ExecContext(
				ctx,
				"INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)\n"+
					"                 VALUES (?1, ?2, NULL, 0, ?3)",
				name, entry.Color, repo.ToIntBool(entry.IsTransfer),
			); err != nil {
				return err
			}
			added++
		}

		return audit.LogConn(
			ctx, tx, "ADD_DEFAULT_CATEGORIES", audit.ActorUser,
			fmt.Sprintf("Added %d default categories", added),
		)
	})
	if err != nil {
		return 0, err
	}
	return added, nil
}

func existingNames(ctx context.Context, tx *sql.Tx) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM categories")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	existing := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		existing[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return existing, nil
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.Category, error) {
	var (
		id            int64
		name          string
		color         string
		budgetMinor   *int64
		isTaxRelevant int64
		isTransfer    int64
	)
	if err := row.Scan(&id, &name, &color, &budgetMinor, &isTaxRelevant, &isTransfer); err != nil {
		return model.Category{}, err
	}

	var budgetLimit *money.Money
	if budgetMinor != nil {
		budget := money.Money{Minor: *budgetMinor, Currency: money.EUR}
		budgetLimit = &budget
	}

	return model.Category{
		ID:            model.CategoryID(id),
		Name:          name,
		Color:         color,
		BudgetLimit:   budgetLimit,
		IsTaxRelevant: repo.FromIntBool(isTaxRelevant),
		IsTransfer:    repo.FromIntBool(isTransfer),
	}, nil
}

func checkBudgetCurrency(category model.NewCategory) error {
	if category.BudgetLimit != nil && category.BudgetLimit.Currency != money.EUR {
		return repo.ErrCurrencyMismatch
	}
	return nil
}

// Create creates a category.
func Create(ctx context.Context, d *db.Database, new model.NewCategory) (model.CategoryID, error) {
	if err := checkBudgetCurrency(new); err != nil {
		return 0, err
	}

	var id model.CategoryID
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)\n"+
				"             VALUES (?1, ?2, ?3, ?4, ?5)",
			new.Name,
			new.Color,
			optionalBudget(new.BudgetLimit),
			repo.ToIntBool(new.IsTaxRelevant),
			repo.ToIntBool(new.IsTransfer),
		)
		if err != nil {
			return err
		}

		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = model.CategoryID(inserted)

		return audit.LogConn(
			ctx, tx, "category.create", audit.ActorUser, repo.IDAndName(inserted, new.Name),
		)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Get reads one category.
func Get(ctx context.Context, d *db.Database, id model.CategoryID) (category model.Category, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		row := conn.QueryRowContext(
			ctx,
			"SELECT id, name, color, budget_limit_minor, is_tax_relevant, is_transfer FROM categories WHERE id = ?1",
			int64(id),
		)
		loaded, scanErr := fromRow(row)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			return nil
		case scanErr != nil:
			return scanErr
		default:
			category = loaded
			ok = true
			return nil
		}
	})
	if err != nil {
		return model.Category{}, false, err
	}
	return category, ok, nil
}

// List lists categories by name, case-insensitively.
func List(ctx context.Context, d *db.Database) ([]model.Category, error) {
	var categories []model.Category

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT id, name, color, budget_limit_minor, is_tax_relevant, is_transfer\n"+
				"             FROM categories ORDER BY name COLLATE NOCASE",
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			category, err := fromRow(rows)
			if err != nil {
				return err
			}
			categories = append(categories, category)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return categories, nil
}

// Update replaces a category's fields.
func Update(ctx context.Context, d *db.Database, id model.CategoryID, update model.NewCategory) error {
	if err := checkBudgetCurrency(update); err != nil {
		return err
	}

	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE categories\n"+
				"             SET name = ?1, color = ?2, budget_limit_minor = ?3, is_tax_relevant = ?4, is_transfer = ?5\n"+
				"             WHERE id = ?6",
			update.Name,
			update.Color,
			optionalBudget(update.BudgetLimit),
			repo.ToIntBool(update.IsTaxRelevant),
			repo.ToIntBool(update.IsTransfer),
			int64(id),
		)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return repo.ErrNotFound
		}

		return audit.LogConn(
			ctx, tx, "category.update", audit.ActorUser, repo.IDAndName(int64(id), update.Name),
		)
	})
}

// Delete deletes a category. Its bookings stay, uncategorized, and its rules
// are removed.
func Delete(ctx context.Context, d *db.Database, id model.CategoryID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM categories WHERE id = ?1", int64(id))
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return repo.ErrNotFound
		}

		return audit.LogConn(ctx, tx, "category.delete", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

func optionalBudget(budget *money.Money) any {
	if budget == nil {
		return nil
	}
	return budget.Minor
}
