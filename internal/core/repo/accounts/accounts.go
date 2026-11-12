// Package accounts is the account repository.
//
// It ports `crates/finglass-core/src/repo/accounts.rs`.
package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/rustfmt"
)

const columns = "a.id, a.bank_connection_id, a.institution, a.name, a.iban, a.account_number, a.currency, " +
	"a.account_type, a.balance_minor, a.balance_updated_at, a.group_id"

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func fromRow(row scanner) (model.Account, error) {
	var (
		id                   int64
		bankConnectionID     *int64
		institution          *string
		name                 *string
		iban                 *string
		accountNumber        *string
		currencyText         string
		accountTypeText      string
		balanceMinor         int64
		balanceUpdatedAtText *string
		groupID              *int64
	)

	if err := row.Scan(
		&id,
		&bankConnectionID,
		&institution,
		&name,
		&iban,
		&accountNumber,
		&currencyText,
		&accountTypeText,
		&balanceMinor,
		&balanceUpdatedAtText,
		&groupID,
	); err != nil {
		return model.Account{}, err
	}

	currency, err := repo.ParseCurrency(currencyText)
	if err != nil {
		return model.Account{}, err
	}
	accountType, ok := model.AccountTypeFromStored(accountTypeText)
	if !ok {
		return model.Account{}, repo.InvalidError{
			Msg: "unknown account type " + rustfmt.Quote(accountTypeText),
		}
	}

	var balanceUpdatedAt *time.Time
	if balanceUpdatedAtText != nil {
		parsed, err := repo.ParseTimestamp(*balanceUpdatedAtText)
		if err != nil {
			return model.Account{}, err
		}
		balanceUpdatedAt = &parsed
	}

	account := model.Account{
		ID:               model.AccountID(id),
		Institution:      institution,
		Name:             name,
		IBAN:             iban,
		AccountNumber:    accountNumber,
		Currency:         currency,
		AccountType:      accountType,
		Balance:          money.Money{Minor: balanceMinor, Currency: currency},
		BalanceUpdatedAt: balanceUpdatedAt,
	}
	if bankConnectionID != nil {
		converted := model.BankConnectionID(*bankConnectionID)
		account.BankConnectionID = &converted
	}
	if groupID != nil {
		converted := model.AccountGroupID(*groupID)
		account.GroupID = &converted
	}
	return account, nil
}

// Create creates an account.
func Create(ctx context.Context, d *db.Database, new model.NewAccount) (model.AccountID, error) {
	if new.Balance.Currency != new.Currency {
		return 0, repo.ErrCurrencyMismatch
	}

	var id model.AccountID
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		balanceUpdatedAt, err := optionalTimestamp(new.BalanceUpdatedAt)
		if err != nil {
			return err
		}

		result, err := tx.ExecContext(
			ctx,
			"INSERT INTO accounts (\n"+
				"                bank_connection_id, institution, name, iban, account_number, currency, account_type,\n"+
				"                balance_minor, balance_updated_at, group_id\n"+
				"             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
			optionalBankConnectionID(new.BankConnectionID),
			optionalString(new.Institution),
			optionalString(new.Name),
			optionalString(new.IBAN),
			optionalString(new.AccountNumber),
			new.Currency.String(),
			new.AccountType.AsStr(),
			new.Balance.Minor,
			balanceUpdatedAt,
			optionalAccountGroupID(new.GroupID),
		)
		if err != nil {
			return err
		}

		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = model.AccountID(inserted)

		details := repo.IDOnly(inserted)
		if new.Name != nil {
			details = repo.IDAndName(inserted, *new.Name)
		}
		return audit.LogConn(ctx, tx, "account.create", audit.ActorUser, details)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Get reads one account. ok is false when the account does not exist.
func Get(ctx context.Context, d *db.Database, id model.AccountID) (account model.Account, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		row := conn.QueryRowContext(ctx, "SELECT "+columns+" FROM accounts a WHERE a.id = ?1", int64(id))
		loaded, scanErr := fromRow(row)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			return nil
		case scanErr != nil:
			return scanErr
		default:
			account = loaded
			ok = true
			return nil
		}
	})
	if err != nil {
		return model.Account{}, false, err
	}
	return account, ok, nil
}

// List lists accounts by group sort order, then name, then id. Accounts
// without a group come last.
func List(ctx context.Context, d *db.Database) ([]model.Account, error) {
	var accounts []model.Account

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT "+columns+"\n"+
				"             FROM accounts a\n"+
				"             LEFT JOIN account_groups g ON g.id = a.group_id\n"+
				"             ORDER BY CASE WHEN a.group_id IS NULL THEN 1 ELSE 0 END,\n"+
				"                      g.sort_order,\n"+
				"                      a.name,\n"+
				"                      a.id",
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			account, err := fromRow(rows)
			if err != nil {
				return err
			}
			accounts = append(accounts, account)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

// Rename changes an account's display name.
func Rename(ctx context.Context, d *db.Database, id model.AccountID, name *string) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE accounts SET name = ?1 WHERE id = ?2",
			optionalString(name), int64(id),
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

		details := repo.IDOnly(int64(id))
		if name != nil {
			details = repo.IDAndName(int64(id), *name)
		}
		return audit.LogConn(ctx, tx, "account.rename", audit.ActorUser, details)
	})
}

// SetGroup moves an account into a group, or removes it from its group.
func SetGroup(ctx context.Context, d *db.Database, id model.AccountID, groupID *model.AccountGroupID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			"UPDATE accounts SET group_id = ?1 WHERE id = ?2",
			optionalAccountGroupID(groupID), int64(id),
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

		return audit.LogConn(ctx, tx, "account.set_group", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

// SetBalance sets an account's balance. The currency must match the account's
// currency.
func SetBalance(ctx context.Context, d *db.Database, id model.AccountID, balance money.Money, at time.Time) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if err := checkBalanceCurrency(ctx, tx, id, balance); err != nil {
			return err
		}

		atText, err := repo.FormatTimestamp(at)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE accounts SET balance_minor = ?1, balance_updated_at = ?2 WHERE id = ?3",
			balance.Minor, atText, int64(id),
		); err != nil {
			return err
		}

		return audit.LogConn(ctx, tx, "account.set_balance", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

// SetBalanceForImport sets an account's balance for the import service only,
// without writing its own audit entry.
//
// The import service writes the single `IMPORT_STATEMENT` audit entry after the
// balance update, with FinDesk's import detail wording. [SetBalance] keeps its
// own audit entry for direct user changes.
func SetBalanceForImport(ctx context.Context, d *db.Database, id model.AccountID, balance money.Money, at time.Time) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if err := checkBalanceCurrency(ctx, tx, id, balance); err != nil {
			return err
		}

		atText, err := repo.FormatTimestamp(at)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(
			ctx,
			"UPDATE accounts SET balance_minor = ?1, balance_updated_at = ?2 WHERE id = ?3",
			balance.Minor, atText, int64(id),
		)
		return err
	})
}

func checkBalanceCurrency(ctx context.Context, tx *sql.Tx, id model.AccountID, balance money.Money) error {
	var currencyText string
	err := tx.QueryRowContext(ctx, "SELECT currency FROM accounts WHERE id = ?1", int64(id)).Scan(&currencyText)
	if errors.Is(err, sql.ErrNoRows) {
		return repo.ErrNotFound
	}
	if err != nil {
		return err
	}

	currency, err := repo.ParseCurrency(currencyText)
	if err != nil {
		return err
	}
	if currency != balance.Currency {
		return repo.ErrCurrencyMismatch
	}
	return nil
}

// SetAccountType changes an account's type and returns whether the stored type
// changed.
//
// An unchanged type stores nothing and writes no audit entry. The details use
// FinDesk's wording, because the Accounts view shows them in the audit log.
func SetAccountType(ctx context.Context, d *db.Database, id model.AccountID, accountType model.AccountType) (bool, error) {
	changed := false

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT account_type FROM accounts WHERE id = ?1", int64(id)).Scan(&previous)
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		if err != nil {
			return err
		}

		if previous == accountType.AsStr() {
			return nil
		}

		if _, err := tx.ExecContext(
			ctx,
			"UPDATE accounts SET account_type = ?1 WHERE id = ?2",
			accountType.AsStr(), int64(id),
		); err != nil {
			return err
		}

		changed = true
		return audit.LogConn(
			ctx,
			tx,
			"CHANGE_ACCOUNT_TYPE",
			audit.ActorUser,
			fmt.Sprintf("Account %d: %s -> %s", int64(id), previous, accountType.AsStr()),
		)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// SetCurrency changes an account's currency and its balance in one
// transaction, with one audit entry. It returns whether the stored currency
// changed.
//
// The sync uses this when the old EUR fallback stored the wrong currency. An
// unchanged currency stores nothing and writes no audit entry. The details use
// the account id only, never an IBAN.
func SetCurrency(
	ctx context.Context,
	d *db.Database,
	id model.AccountID,
	currency money.Currency,
	balance money.Money,
	at time.Time,
) (bool, error) {
	if balance.Currency != currency {
		return false, repo.ErrCurrencyMismatch
	}

	changed := false
	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT currency FROM accounts WHERE id = ?1", int64(id)).Scan(&previous)
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		if err != nil {
			return err
		}

		previousCurrency, err := repo.ParseCurrency(previous)
		if err != nil {
			return err
		}
		if previousCurrency == currency {
			return nil
		}

		atText, err := repo.FormatTimestamp(at)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE accounts SET currency = ?1, balance_minor = ?2, balance_updated_at = ?3 WHERE id = ?4",
			currency.String(), balance.Minor, atText, int64(id),
		); err != nil {
			return err
		}

		changed = true
		return audit.LogConn(
			ctx,
			tx,
			"CORRECT_ACCOUNT_CURRENCY",
			audit.ActorUser,
			fmt.Sprintf("Account %d: %s -> %s", int64(id), previous, currency.String()),
		)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// Delete deletes an account and, through the foreign keys, its bookings.
func Delete(ctx context.Context, d *db.Database, id model.AccountID) error {
	return repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?1", int64(id))
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

		return audit.LogConn(ctx, tx, "account.delete", audit.ActorUser, repo.IDOnly(int64(id)))
	})
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalTimestamp(value *time.Time) (any, error) {
	if value == nil {
		return nil, nil
	}
	return repo.FormatTimestamp(*value)
}

func optionalBankConnectionID(id *model.BankConnectionID) any {
	if id == nil {
		return nil
	}
	return int64(*id)
}

func optionalAccountGroupID(id *model.AccountGroupID) any {
	if id == nil {
		return nil
	}
	return int64(*id)
}
