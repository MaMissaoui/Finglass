// Package accountgroups is the account group repository.
//
// It ports `crates/finglass-core/src/repo/account_groups.rs`.
package accountgroups

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.AccountGroup, error) {
	var group model.AccountGroup
	if err := row.Scan(&group.ID, &group.Name, &group.SortOrder); err != nil {
		return model.AccountGroup{}, err
	}
	return group, nil
}

// Create creates an account group.
func Create(ctx context.Context, d *db.Database, new model.NewAccountGroup) (model.AccountGroupID, error) {
	var id model.AccountGroupID

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO account_groups (name, sort_order) VALUES (?1, ?2)",
			new.Name, new.SortOrder,
		)
		if err != nil {
			return err
		}

		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = model.AccountGroupID(inserted)

		return audit.LogConn(ctx, tx, "account_group.create", audit.ActorUser, repo.IDAndName(inserted, new.Name))
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// List lists account groups by sort order, then name.
func List(ctx context.Context, d *db.Database) ([]model.AccountGroup, error) {
	var groups []model.AccountGroup

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT id, name, sort_order FROM account_groups ORDER BY sort_order, name")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			group, err := fromRow(rows)
			if err != nil {
				return err
			}
			groups = append(groups, group)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// Rename changes an account group's name.
func Rename(ctx context.Context, d *db.Database, id model.AccountGroupID, name string) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE account_groups SET name = ?1 WHERE id = ?2",
			name, int64(id),
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
			ctx, tx, "account_group.rename", audit.ActorUser, repo.IDAndName(int64(id), name),
		)
	})
}

// MoveGroup swaps an account group's sort order with its neighbour and returns
// whether it moved.
//
// The groups are ordered by sort order, then id; at either end there is no
// neighbour and nothing changes. The audit entry is written even for a no-op,
// like FinDesk's view action.
func MoveGroup(ctx context.Context, d *db.Database, id model.AccountGroupID, up bool) (bool, error) {
	moved := false

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		groups, err := listBySortOrderAndID(ctx, tx)
		if err != nil {
			return err
		}

		index := -1
		for position := range groups {
			if groups[position].ID == id {
				index = position
				break
			}
		}

		neighbour := -1
		if index >= 0 {
			if up {
				neighbour = index - 1
			} else {
				neighbour = index + 1
			}
			if neighbour < 0 || neighbour >= len(groups) {
				neighbour = -1
			}
		}

		if index >= 0 && neighbour >= 0 {
			currentOrder := groups[index].SortOrder
			neighbourOrder := groups[neighbour].SortOrder
			if _, err := tx.ExecContext(
				ctx,
				"UPDATE account_groups SET sort_order = ?1 WHERE id = ?2",
				neighbourOrder, int64(groups[index].ID),
			); err != nil {
				return err
			}
			if _, err := tx.ExecContext(
				ctx,
				"UPDATE account_groups SET sort_order = ?1 WHERE id = ?2",
				currentOrder, int64(groups[neighbour].ID),
			); err != nil {
				return err
			}
			moved = true
		}

		direction := "down"
		if up {
			direction = "up"
		}
		return audit.LogConn(
			ctx,
			tx,
			"REORDER_ACCOUNT_GROUP",
			audit.ActorUser,
			fmt.Sprintf("Moved account group %d %s", int64(id), direction),
		)
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

func listBySortOrderAndID(ctx context.Context, tx *sql.Tx) ([]model.AccountGroup, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, name, sort_order FROM account_groups ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var groups []model.AccountGroup
	for rows.Next() {
		group, err := fromRow(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

// Delete deletes an account group. Its accounts keep existing without a group.
func Delete(ctx context.Context, d *db.Database, id model.AccountGroupID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM account_groups WHERE id = ?1", int64(id))
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

		return audit.LogConn(ctx, tx, "account_group.delete", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}
