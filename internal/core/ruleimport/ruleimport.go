// Package ruleimport imports and exports a reviewed rule set, ported from
// `crates/finglass-core/src/rules/import.rs`.
//
// A preview is read-only. Applying a confirmed preview creates the missing
// categories and the new rules and, optionally, categorizes uncategorized
// bookings with the combined auto-apply rule set, all in one transaction with
// one audit entry.
package ruleimport

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	rulefiles "github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/rustfmt"
)

// RuleImportConflict is the same pattern, text field and money flow as an
// imported row, but the file's category would sit beside another.
type RuleImportConflict struct {
	// Line is the 1-based line of the file the new rule was read from.
	Line int
	// Pattern is the pattern.
	Pattern string
	// FileCategory is the category the file names.
	FileCategory string
	// ExistingCategory is the category the existing rule uses.
	ExistingCategory string
}

// RuleImportPreview is what an import of a parsed rule file would change; the
// preview dialog shows it before anything is written.
type RuleImportPreview struct {
	// CategoriesToCreate are categories that do not exist yet, in file order,
	// deduplicated case-insensitively (first spelling wins).
	CategoriesToCreate []string
	// NewRules are rows that create a new rule, in file order.
	NewRules []rulefiles.RuleFileRow
	// Conflicts are rows that would collide with an existing rule or an earlier
	// row of the same file; they are skipped.
	Conflicts []RuleImportConflict
	// Duplicates is the number of rows that repeat an identical rule (same
	// pattern, field, direction and category); they are skipped.
	Duplicates int
	// MatchCount is how many currently uncategorized bookings the combined
	// auto-apply rule set would categorize.
	MatchCount int
	// Errors are row errors passed through from the parse.
	Errors []rulefiles.RuleFileError
}

// RuleImportResult is the counts an applied import changed.
type RuleImportResult struct {
	// Categories is the number of categories created.
	Categories int
	// Rules is the number of rules created.
	Rules int
	// Categorized is the number of bookings categorized.
	Categorized int
}

// RuleImportError is everything that can go wrong in a rule import or export.
//
// It wraps the repository, database or I/O error transparently, like Rust's
// `RuleImportError`, so errors.Is and errors.As see the cause.
type RuleImportError struct {
	// Err is the underlying repository, database or I/O error.
	Err error
}

// Error returns the underlying error text.
func (e *RuleImportError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *RuleImportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return &RuleImportError{Err: err}
}

type ruleKey struct {
	pattern   string
	field     model.RuleField
	direction model.RuleDirection
}

type earlierRule struct {
	categoryKey string
	category    string
}

// Preview is read-only: it classifies every usable row against the existing
// data, leaving categories and rules untouched.
func Preview(ctx context.Context, d *db.Database, parsed *rulefiles.ParsedRuleFile) (*RuleImportPreview, error) {
	existingCategories, err := categories.List(ctx, d)
	if err != nil {
		return nil, wrap(err)
	}
	existingKeys := make(map[string]struct{}, len(existingCategories))
	for _, category := range existingCategories {
		existingKeys[categoryKey(category.Name)] = struct{}{}
	}

	seenInFile := make(map[string]struct{})
	categoriesToCreate := make([]string, 0)
	for _, row := range parsed.Rows {
		key := categoryKey(row.Category)
		if _, exists := existingKeys[key]; exists {
			continue
		}
		if _, seen := seenInFile[key]; seen {
			continue
		}
		seenInFile[key] = struct{}{}
		categoriesToCreate = append(categoriesToCreate, strings.TrimSpace(row.Category))
	}

	// Existing rules indexed by pattern, field and direction; the first kept is
	// like the file's earliest row.
	existingByKey := make(map[ruleKey]string)
	listedRules, err := rulerepo.List(ctx, d)
	if err != nil {
		return nil, wrap(err)
	}
	for _, rule := range listedRules {
		key := ruleKey{pattern: rule.Pattern, field: rule.MatchField, direction: rule.Direction}
		if _, present := existingByKey[key]; present {
			continue
		}
		name := ""
		for _, category := range existingCategories {
			if category.ID == rule.CategoryID {
				name = category.Name
				break
			}
		}
		existingByKey[key] = name
	}

	// An earlier row of the same file counts like an existing rule: a later
	// identical row is a duplicate, a row with the same key but another category
	// a conflict. That earlier row may itself have been skipped.
	earlierByKey := make(map[ruleKey]earlierRule)
	newRules := make([]rulefiles.RuleFileRow, 0)
	conflicts := make([]RuleImportConflict, 0)
	duplicates := 0

	for _, row := range parsed.Rows {
		key := ruleKey{pattern: row.Pattern, field: row.Field, direction: row.Direction}
		rowCategoryKey := categoryKey(row.Category)

		if earlier, present := earlierByKey[key]; present {
			if earlier.categoryKey == rowCategoryKey {
				duplicates++
			} else {
				conflicts = append(conflicts, RuleImportConflict{
					Line:             row.Line,
					Pattern:          row.Pattern,
					FileCategory:     row.Category,
					ExistingCategory: earlier.category,
				})
			}
			continue
		}

		if existingCategory, present := existingByKey[key]; present {
			if categoryKey(existingCategory) == rowCategoryKey {
				duplicates++
			} else {
				conflicts = append(conflicts, RuleImportConflict{
					Line:             row.Line,
					Pattern:          row.Pattern,
					FileCategory:     row.Category,
					ExistingCategory: existingCategory,
				})
			}
			continue
		}

		newRules = append(newRules, row)
		earlierByKey[key] = earlierRule{categoryKey: rowCategoryKey, category: row.Category}
	}

	autoApplyRows := make([]rulefiles.RuleFileRow, 0)
	for _, row := range newRules {
		if row.AutoApply {
			autoApplyRows = append(autoApplyRows, row)
		}
	}
	matched, err := matchCount(ctx, d, autoApplyRows, categoriesToCreate)
	if err != nil {
		return nil, wrap(err)
	}

	return &RuleImportPreview{
		CategoriesToCreate: categoriesToCreate,
		NewRules:           newRules,
		Conflicts:          conflicts,
		Duplicates:         duplicates,
		MatchCount:         matched,
		Errors:             append([]rulefiles.RuleFileError(nil), parsed.Errors...),
	}, nil
}

// Apply applies the confirmed preview in one transaction: categories, rules
// and, on request, the categorization.
func Apply(
	ctx context.Context,
	d *db.Database,
	preview *RuleImportPreview,
	categorize bool,
	lang i18n.Language,
) (RuleImportResult, error) {
	categoriesToCreate := append([]string(nil), preview.CategoriesToCreate...)
	newRules := append([]rulefiles.RuleFileRow(nil), preview.NewRules...)

	var result RuleImportResult
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		applied, err := applyConn(ctx, tx, categoriesToCreate, newRules, categorize, lang)
		if err != nil {
			return err
		}
		result = applied
		return nil
	})
	if err != nil {
		return RuleImportResult{}, wrap(err)
	}
	return result, nil
}

// Export writes the current rules (auto-apply and manual) to file in the
// reviewable rule-file format.
func Export(ctx context.Context, d *db.Database, file string) (int, error) {
	listedRules, err := rulerepo.List(ctx, d)
	if err != nil {
		return 0, wrap(err)
	}
	listedCategories, err := categories.List(ctx, d)
	if err != nil {
		return 0, wrap(err)
	}
	names := make(map[model.CategoryID]string, len(listedCategories))
	for _, category := range listedCategories {
		names[category.ID] = category.Name
	}

	items := make([]rulefiles.RuleFileItem, 0, len(listedRules))
	for _, rule := range listedRules {
		items = append(items, rulefiles.RuleFileItem{
			Pattern:      rule.Pattern,
			CategoryName: names[rule.CategoryID],
			Priority:     rule.Priority,
			AutoApply:    rule.AutoApply,
			MatchField:   rule.MatchField,
			Direction:    rule.Direction,
		})
	}

	text, skipped := rulefiles.WriteRuleFile(items)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return 0, wrap(err)
	}

	written := len(items) - skipped
	var details string
	if skipped > 0 {
		details = fmt.Sprintf("Exported %d rules, skipped %d", written, skipped)
	} else {
		details = fmt.Sprintf("Exported %d rules", written)
	}
	if err := audit.Log(ctx, d, "EXPORT_RULES", audit.ActorUser, details); err != nil {
		return 0, wrap(err)
	}

	return written, nil
}

func categoryKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// matchCount returns how many currently uncategorized bookings the combined
// auto-apply rule set would categorize.
func matchCount(
	ctx context.Context,
	d *db.Database,
	autoApplyRows []rulefiles.RuleFileRow,
	categoriesToCreate []string,
) (int, error) {
	bookings, err := transactions.FindItems(
		ctx,
		d,
		&model.TransactionFilter{UncategorizedOnly: true},
		nil,
	)
	if err != nil {
		return 0, err
	}
	if len(bookings) == 0 {
		return 0, nil
	}

	listedCategories, err := categories.List(ctx, d)
	if err != nil {
		return 0, err
	}
	existingKeys := make(map[string]int64, len(listedCategories))
	for _, category := range listedCategories {
		existingKeys[categoryKey(category.Name)] = int64(category.ID)
	}
	tempIDs := make(map[string]int64, len(categoriesToCreate))
	for index, name := range categoriesToCreate {
		tempIDs[categoryKey(name)] = -1 - int64(index)
	}

	categoryIDOf := func(name string) (model.CategoryID, error) {
		key := categoryKey(name)
		if id, present := existingKeys[key]; present {
			return model.CategoryID(id), nil
		}
		if id, present := tempIDs[key]; present {
			return model.CategoryID(id), nil
		}
		return 0, fmt.Errorf("every file category exists or is created")
	}

	type mergedRule struct {
		priority int64
		source   uint8
		index    int
		spec     rulefiles.RuleSpec
	}
	merged := make([]mergedRule, 0, len(autoApplyRows)+len(listedCategories))

	listedRules, err := rulerepo.List(ctx, d)
	if err != nil {
		return 0, err
	}
	index := 0
	for _, rule := range listedRules {
		if !rule.AutoApply {
			continue
		}
		merged = append(merged, mergedRule{
			priority: rule.Priority,
			source:   0,
			index:    index,
			spec: rulefiles.RuleSpec{
				Pattern:    rule.Pattern,
				CategoryID: rule.CategoryID,
				Field:      rule.MatchField,
				Direction:  rule.Direction,
			},
		})
		index++
	}
	for rowIndex, row := range autoApplyRows {
		categoryID, err := categoryIDOf(row.Category)
		if err != nil {
			return 0, err
		}
		merged = append(merged, mergedRule{
			priority: row.Priority,
			source:   1,
			index:    rowIndex,
			spec: rulefiles.RuleSpec{
				Pattern:    row.Pattern,
				CategoryID: categoryID,
				Field:      row.Field,
				Direction:  row.Direction,
			},
		})
	}
	sort.Slice(merged, func(left, right int) bool {
		if merged[left].priority != merged[right].priority {
			return merged[left].priority > merged[right].priority
		}
		if merged[left].source != merged[right].source {
			return merged[left].source < merged[right].source
		}
		return merged[left].index < merged[right].index
	})

	specs := make([]rulefiles.RuleSpec, 0, len(merged))
	for _, entry := range merged {
		specs = append(specs, entry.spec)
	}
	matcher := rulefiles.NewRuleMatcher(specs)

	matched := 0
	for _, booking := range bookings {
		amount := moneyToDecimal(booking.Amount)
		if matcher.CategoryFor(booking.Purpose, booking.PartnerName, &amount) != nil {
			matched++
		}
	}
	return matched, nil
}

func moneyToDecimal(value money.Money) decimal.Decimal {
	return decimal.New(value.Minor, decimalScale(value.Currency))
}

func decimalScale(currency money.Currency) int32 {
	switch currency.MinorExponent() {
	case 0:
		return 0
	case 3:
		return 3
	default:
		return 2
	}
}

func applyConn(
	ctx context.Context,
	tx *sql.Tx,
	categoriesToCreate []string,
	newRules []rulefiles.RuleFileRow,
	categorize bool,
	lang i18n.Language,
) (RuleImportResult, error) {
	// The existing spelling and id win: everything is resolved
	// case-insensitively by trimmed name.
	resolved := make(map[string]int64)
	existingCount := 0

	if err := func() error {
		rows, err := tx.QueryContext(ctx, "SELECT id, name FROM categories ORDER BY name COLLATE NOCASE")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				id   int64
				name string
			)
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			key := categoryKey(name)
			if _, present := resolved[key]; !present {
				resolved[key] = id
			}
			existingCount++
		}
		return rows.Err()
	}(); err != nil {
		return RuleImportResult{}, err
	}

	created := make(map[string]int64)

	// The preview's list is in file order; every name must resolve before any
	// step writes.
	for _, name := range categoriesToCreate {
		if _, err := resolveCategory(ctx, tx, resolved, created, existingCount, name, lang); err != nil {
			return RuleImportResult{}, err
		}
	}

	for _, row := range newRules {
		categoryID, err := resolveCategory(
			ctx, tx, resolved, created, existingCount, row.Category, lang,
		)
		if err != nil {
			return RuleImportResult{}, err
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO rules (pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant)\n"+
				"             VALUES (?1, ?2, ?3, ?4, ?5, ?6, NULL)",
			row.Pattern,
			categoryID,
			row.Priority,
			repo.ToIntBool(row.AutoApply),
			row.Field.AsStr(),
			row.Direction.AsStr(),
		); err != nil {
			return RuleImportResult{}, err
		}
	}

	categorized := 0
	if categorize && len(newRules) > 0 {
		matcher, err := loadMatcher(ctx, tx)
		if err != nil {
			return RuleImportResult{}, err
		}

		type booking struct {
			id          int64
			partnerName *string
			purpose     *string
			amount      money.Money
		}
		bookings := make([]booking, 0)
		if err := func() error {
			rows, err := tx.QueryContext(
				ctx,
				"SELECT id, partner_name, purpose, amount_minor, currency\n"+
					"                 FROM transactions WHERE category_id IS NULL ORDER BY booking_date DESC, id DESC",
			)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()

			for rows.Next() {
				var (
					id           int64
					partnerName  *string
					purpose      *string
					amountMinor  int64
					currencyText string
				)
				if err := rows.Scan(&id, &partnerName, &purpose, &amountMinor, &currencyText); err != nil {
					return err
				}
				currency, err := money.NewCurrency(currencyText)
				if err != nil {
					return err
				}
				bookings = append(bookings, booking{
					id:          id,
					partnerName: partnerName,
					purpose:     purpose,
					amount:      money.Money{Minor: amountMinor, Currency: currency},
				})
			}
			return rows.Err()
		}(); err != nil {
			return RuleImportResult{}, err
		}

		for _, entry := range bookings {
			amount := moneyToDecimal(entry.amount)
			categoryID := matcher.CategoryFor(entry.purpose, entry.partnerName, &amount)
			if categoryID == nil {
				continue
			}
			result, err := tx.ExecContext(
				ctx,
				"UPDATE transactions SET category_id = ?1 WHERE id = ?2 AND category_id IS NULL",
				int64(*categoryID), entry.id,
			)
			if err != nil {
				return RuleImportResult{}, err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return RuleImportResult{}, err
			}
			if changed > 0 {
				categorized++
			}
		}
	}

	result := RuleImportResult{
		Categories:  len(categoriesToCreate),
		Rules:       len(newRules),
		Categorized: categorized,
	}
	if err := audit.LogConn(
		ctx,
		tx,
		"IMPORT_RULES",
		audit.ActorUser,
		fmt.Sprintf(
			"Imported %d rules, created %d categories, categorized %d transactions",
			result.Rules, result.Categories, result.Categorized,
		),
	); err != nil {
		return RuleImportResult{}, err
	}

	return result, nil
}

// resolveCategory resolves one category name, creating it with its default or
// palette colour when it is new.
func resolveCategory(
	ctx context.Context,
	tx *sql.Tx,
	resolved map[string]int64,
	created map[string]int64,
	existingCount int,
	name string,
	lang i18n.Language,
) (int64, error) {
	key := categoryKey(name)
	if id, present := resolved[key]; present {
		return id, nil
	}
	if id, present := created[key]; present {
		return id, nil
	}

	ordinal := existingCount + len(created)
	var color string
	if defaultColor := categories.DefaultColourFor(name, lang); defaultColor != nil {
		color = *defaultColor
	} else {
		color = categories.Colors[ordinal%len(categories.Colors)]
	}
	result, err := tx.ExecContext(
		ctx,
		"INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)\n"+
			"         VALUES (?1, ?2, NULL, 0, 0)",
		name, color,
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	created[key] = id
	return id, nil
}

func loadMatcher(ctx context.Context, tx *sql.Tx) (*rulefiles.RuleMatcher, error) {
	rows, err := tx.QueryContext(
		ctx,
		"SELECT pattern, category_id, match_field, direction\n"+
			"         FROM rules WHERE auto_apply = 1 ORDER BY priority DESC, id",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	specs := make([]rulefiles.RuleSpec, 0)
	for rows.Next() {
		var (
			pattern       string
			categoryID    int64
			fieldText     string
			directionText string
		)
		if err := rows.Scan(&pattern, &categoryID, &fieldText, &directionText); err != nil {
			return nil, err
		}
		field, ok := model.RuleFieldFromStored(fieldText)
		if !ok {
			return nil, invalidColumn(2, "match_field", fieldText)
		}
		direction, ok := model.RuleDirectionFromStored(directionText)
		if !ok {
			return nil, invalidColumn(3, "direction", directionText)
		}
		specs = append(specs, rulefiles.RuleSpec{
			Pattern:    pattern,
			CategoryID: model.CategoryID(categoryID),
			Field:      field,
			Direction:  direction,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rulefiles.NewRuleMatcher(specs), nil
}

func invalidColumn(index int, name, value string) error {
	return fmt.Errorf("unknown %s %s in column %d", name, rustfmt.Quote(value), index)
}
