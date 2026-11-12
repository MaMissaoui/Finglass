// Package rules is the rule repository.
//
// It ports `crates/finglass-core/src/repo/rules.rs`.
package rules

import (
	"context"
	"database/sql"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/rustfmt"
)

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.Rule, error) {
	var (
		id               int64
		pattern          string
		categoryID       int64
		priority         int64
		autoApply        int64
		matchFieldText   string
		directionText    string
		taxRelevantValue *int64
	)
	if err := row.Scan(
		&id,
		&pattern,
		&categoryID,
		&priority,
		&autoApply,
		&matchFieldText,
		&directionText,
		&taxRelevantValue,
	); err != nil {
		return model.Rule{}, err
	}

	matchField, ok := model.RuleFieldFromStored(matchFieldText)
	if !ok {
		return model.Rule{}, repo.InvalidError{
			Msg: "unknown rule match field " + rustfmt.Quote(matchFieldText),
		}
	}
	direction, ok := model.RuleDirectionFromStored(directionText)
	if !ok {
		return model.Rule{}, repo.InvalidError{
			Msg: "unknown rule direction " + rustfmt.Quote(directionText),
		}
	}

	return model.Rule{
		ID:          model.RuleID(id),
		Pattern:     pattern,
		CategoryID:  model.CategoryID(categoryID),
		Priority:    priority,
		AutoApply:   repo.FromIntBool(autoApply),
		MatchField:  matchField,
		Direction:   direction,
		TaxRelevant: repo.FromOptionalIntBool(taxRelevantValue),
	}, nil
}

// Create creates a rule.
func Create(ctx context.Context, d *db.Database, new model.NewRule) (model.RuleID, error) {
	var id model.RuleID

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO rules (pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant)\n"+
				"             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
			new.Pattern,
			int64(new.CategoryID),
			new.Priority,
			repo.ToIntBool(new.AutoApply),
			new.MatchField.AsStr(),
			new.Direction.AsStr(),
			repo.ToOptionalIntBool(new.TaxRelevant),
		)
		if err != nil {
			return err
		}

		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = model.RuleID(inserted)

		return audit.LogConn(ctx, tx, "rule.create", audit.ActorUser, repo.IDOnly(inserted))
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// List lists rules by priority, highest first, then id.
func List(ctx context.Context, d *db.Database) ([]model.Rule, error) {
	var rules []model.Rule

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant\n"+
				"             FROM rules ORDER BY priority DESC, id",
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			rule, err := fromRow(rows)
			if err != nil {
				return err
			}
			rules = append(rules, rule)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// Update replaces a rule's fields.
func Update(ctx context.Context, d *db.Database, id model.RuleID, update model.NewRule) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE rules\n"+
				"             SET pattern = ?1, category_id = ?2, priority = ?3, auto_apply = ?4,\n"+
				"                 match_field = ?5, direction = ?6, tax_relevant = ?7\n"+
				"             WHERE id = ?8",
			update.Pattern,
			int64(update.CategoryID),
			update.Priority,
			repo.ToIntBool(update.AutoApply),
			update.MatchField.AsStr(),
			update.Direction.AsStr(),
			repo.ToOptionalIntBool(update.TaxRelevant),
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

		return audit.LogConn(ctx, tx, "rule.update", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

// Delete deletes a rule.
func Delete(ctx context.Context, d *db.Database, id model.RuleID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM rules WHERE id = ?1", int64(id))
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

		return audit.LogConn(ctx, tx, "rule.delete", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}
