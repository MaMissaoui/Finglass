// Package bankconnections is the bank connection repository.
//
// Finglass stores no HBCI version or bank passport file. An offline connection
// never goes online; its statements are imported from files. It ports
// `crates/finglass-core/src/repo/bank_connections.rs`.
package bankconnections

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

// MaxBankNameLength is the length of the `bank_name` column.
const MaxBankNameLength = 255

const columns = "id, bank_name, blz, bic, user_id, server_url, tan_method, sync_from, last_sync, " +
	"capabilities, include_in_sync_all, offline"

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// queryer is satisfied by *sql.Conn and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func fromRow(row scanner) (model.BankConnection, error) {
	var (
		id               int64
		bankName         string
		blz              string
		bic              *string
		userID           string
		serverURL        *string
		tanMethod        *string
		syncFromText     *string
		lastSyncText     *string
		capabilities     *string
		includeInSyncAll int64
		offline          int64
	)
	if err := row.Scan(
		&id,
		&bankName,
		&blz,
		&bic,
		&userID,
		&serverURL,
		&tanMethod,
		&syncFromText,
		&lastSyncText,
		&capabilities,
		&includeInSyncAll,
		&offline,
	); err != nil {
		return model.BankConnection{}, err
	}

	var syncFrom *civil.Date
	if syncFromText != nil {
		parsed, err := repo.ParseDate(*syncFromText)
		if err != nil {
			return model.BankConnection{}, err
		}
		syncFrom = &parsed
	}
	var lastSync *time.Time
	if lastSyncText != nil {
		parsed, err := repo.ParseTimestamp(*lastSyncText)
		if err != nil {
			return model.BankConnection{}, err
		}
		lastSync = &parsed
	}

	return model.BankConnection{
		ID:               model.BankConnectionID(id),
		BankName:         bankName,
		BLZ:              blz,
		BIC:              bic,
		UserID:           userID,
		ServerURL:        serverURL,
		TanMethod:        tanMethod,
		SyncFrom:         syncFrom,
		LastSync:         lastSync,
		Capabilities:     capabilities,
		IncludeInSyncAll: repo.FromIntBool(includeInSyncAll),
		Offline:          repo.FromIntBool(offline),
	}, nil
}

func toItem(connection model.BankConnection) model.BankAccessItem {
	return model.BankAccessItem{
		ID:               connection.ID,
		BankName:         connection.BankName,
		BLZ:              connection.BLZ,
		BIC:              connection.BIC,
		UserID:           connection.UserID,
		TanMethod:        connection.TanMethod,
		ServerURL:        connection.ServerURL,
		LastSync:         connection.LastSync,
		Capabilities:     connection.Capabilities,
		SyncFrom:         connection.SyncFrom,
		IncludeInSyncAll: connection.IncludeInSyncAll,
		Offline:          connection.Offline,
	}
}

func readConnection(ctx context.Context, conn queryer, id int64) (model.BankConnection, bool, error) {
	row := conn.QueryRowContext(ctx, "SELECT "+columns+" FROM bank_connections WHERE id = ?1", id)
	connection, err := fromRow(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return model.BankConnection{}, false, nil
	case err != nil:
		return model.BankConnection{}, false, err
	default:
		return connection, true, nil
	}
}

func readItem(ctx context.Context, conn queryer, id int64) (*model.BankAccessItem, error) {
	connection, ok, err := readConnection(ctx, conn, id)
	if err != nil || !ok {
		return nil, err
	}
	item := toItem(connection)
	return &item, nil
}

func truncateName(name string) string {
	runes := []rune(name)
	if len(runes) <= MaxBankNameLength {
		return name
	}
	return string(runes[:MaxBankNameLength])
}

func insert(
	ctx context.Context,
	tx *sql.Tx,
	new model.NewBankConnection,
	offline bool,
	includeInSyncAll bool,
	action string,
) (model.BankConnection, error) {
	result, err := tx.ExecContext(
		ctx,
		"INSERT INTO bank_connections (\n"+
			"            bank_name, blz, bic, user_id, include_in_sync_all, offline\n"+
			"         ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
		new.BankName,
		new.BLZ,
		optionalString(new.BIC),
		new.UserID,
		repo.ToIntBool(includeInSyncAll),
		repo.ToIntBool(offline),
	)
	if err != nil {
		return model.BankConnection{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.BankConnection{}, err
	}
	if err := audit.LogConn(
		ctx, tx, action, audit.ActorUser, repo.IDAndName(id, new.BankName),
	); err != nil {
		return model.BankConnection{}, err
	}

	connection, ok, err := readConnection(ctx, tx, id)
	if err != nil {
		return model.BankConnection{}, err
	}
	if !ok {
		return model.BankConnection{}, repo.ErrNotFound
	}
	return connection, nil
}

// List lists every connection by id.
func List(ctx context.Context, d *db.Database) ([]model.BankConnection, error) {
	var connections []model.BankConnection

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT "+columns+" FROM bank_connections ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			connection, err := fromRow(rows)
			if err != nil {
				return err
			}
			connections = append(connections, connection)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return connections, nil
}

// Get reads one connection. ok is false when it does not exist.
func Get(ctx context.Context, d *db.Database, id model.BankConnectionID) (connection model.BankConnection, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		loaded, found, readErr := readConnection(ctx, conn, int64(id))
		if readErr != nil {
			return readErr
		}
		connection = loaded
		ok = found
		return nil
	})
	if err != nil {
		return model.BankConnection{}, false, err
	}
	return connection, ok, nil
}

// FindByBLZ lists the connections with this bank code.
func FindByBLZ(ctx context.Context, d *db.Database, blz string) ([]model.BankConnection, error) {
	var connections []model.BankConnection

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT "+columns+" FROM bank_connections WHERE blz = ?1 ORDER BY id",
			blz,
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			connection, err := fromRow(rows)
			if err != nil {
				return err
			}
			connections = append(connections, connection)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return connections, nil
}

// Create creates an online connection.
func Create(ctx context.Context, d *db.Database, new model.NewBankConnection) (model.BankConnection, error) {
	var connection model.BankConnection

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		created, err := insert(ctx, tx, new, false, true, "bank_connection.create")
		if err != nil {
			return err
		}
		connection = created
		return nil
	})
	if err != nil {
		return model.BankConnection{}, err
	}
	return connection, nil
}

// CreateOffline creates a bank that never goes online, for example a credit
// card whose statements are imported from files.
func CreateOffline(ctx context.Context, d *db.Database, bankName string) (model.BankConnection, error) {
	var connection model.BankConnection

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		empty := ""
		created, err := insert(
			ctx,
			tx,
			model.NewBankConnection{
				BankName: bankName,
				BLZ:      "",
				BIC:      &empty,
				UserID:   "",
			},
			true,
			false,
			"bank_connection.create_offline",
		)
		if err != nil {
			return err
		}
		connection = created
		return nil
	})
	if err != nil {
		return model.BankConnection{}, err
	}
	return connection, nil
}

// Update changes the display name and/or TAN method. nil leaves a value
// unchanged.
func Update(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
	bankName *string,
	tanMethod *string,
) (*model.BankConnection, error) {
	var updated *model.BankConnection

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		current, ok, err := readConnection(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		if !ok {
			return audit.LogConn(
				ctx, tx, "bank_connection.update", audit.ActorUser, repo.IDOnly(int64(id)),
			)
		}

		resolvedName := current.BankName
		if bankName != nil {
			resolvedName = *bankName
		}
		resolvedTanMethod := current.TanMethod
		if tanMethod != nil {
			resolvedTanMethod = tanMethod
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections SET bank_name = ?1, tan_method = ?2 WHERE id = ?3",
			resolvedName, optionalString(resolvedTanMethod), int64(id),
		); err != nil {
			return err
		}

		if err := audit.LogConn(
			ctx, tx, "bank_connection.update", audit.ActorUser,
			repo.IDAndName(int64(id), resolvedName),
		); err != nil {
			return err
		}

		loaded, found, err := readConnection(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		if found {
			updated = &loaded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateLastSync stores the current time as the last successful sync.
func UpdateLastSync(ctx context.Context, d *db.Database, id model.BankConnectionID) (*model.BankConnection, error) {
	var updated *model.BankConnection

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		at, err := repo.FormatTimestamp(time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections SET last_sync = ?1 WHERE id = ?2",
			at, int64(id),
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.update_last_sync", audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}

		loaded, found, err := readConnection(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		if found {
			updated = &loaded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListItems lists the UI snapshots of every connection.
func ListItems(ctx context.Context, d *db.Database) ([]model.BankAccessItem, error) {
	connections, err := List(ctx, d)
	if err != nil {
		return nil, err
	}
	items := make([]model.BankAccessItem, 0, len(connections))
	for _, connection := range connections {
		items = append(items, toItem(connection))
	}
	return items, nil
}

// GetItem reads one UI snapshot. ok is false when it does not exist.
func GetItem(ctx context.Context, d *db.Database, id model.BankConnectionID) (item model.BankAccessItem, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		loaded, err := readItem(ctx, conn, int64(id))
		if err != nil {
			return err
		}
		if loaded != nil {
			item = *loaded
			ok = true
		}
		return nil
	})
	if err != nil {
		return model.BankAccessItem{}, false, err
	}
	return item, ok, nil
}

// SetSyncFrom stores (or clears, with nil) the start date the next sync fetches
// history from.
func SetSyncFrom(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
	date *civil.Date,
) (*model.BankAccessItem, error) {
	var updated *model.BankAccessItem

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		dateText, err := optionalDate(date)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections SET sync_from = ?1 WHERE id = ?2",
			dateText, int64(id),
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.set_sync_from", audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}

		loaded, err := readItem(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		updated = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetIncludeInSyncAll stores whether "Sync all" syncs this connection.
func SetIncludeInSyncAll(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
	include bool,
) (*model.BankAccessItem, error) {
	var updated *model.BankAccessItem

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections SET include_in_sync_all = ?1 WHERE id = ?2",
			repo.ToIntBool(include), int64(id),
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.set_include_in_sync_all", audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}

		loaded, err := readItem(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		updated = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// clearLoginStateConn clears the login's capabilities and remembered TAN
// method, without its own audit entry.
func clearLoginStateConn(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(
		ctx,
		"UPDATE bank_connections SET capabilities = NULL, tan_method = NULL WHERE id = ?1",
		id,
	)
	return err
}

// ClearLoginState clears the login's capabilities and remembered TAN method
// after the login changed.
func ClearLoginState(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
) (*model.BankAccessItem, error) {
	var updated *model.BankAccessItem

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if err := clearLoginStateConn(ctx, tx, int64(id)); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.clear_login_state", audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}

		loaded, err := readItem(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		updated = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateCapabilities stores the login's supported business transactions. A
// blank value clears the column.
func UpdateCapabilities(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
	capabilities string,
) (*model.BankAccessItem, error) {
	var updated *model.BankAccessItem

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		trimmed := strings.TrimSpace(capabilities)
		var stored *string
		if trimmed != "" {
			stored = &trimmed
		}
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections SET capabilities = ?1 WHERE id = ?2",
			optionalString(stored), int64(id),
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.update_capabilities", audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}

		loaded, err := readItem(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		updated = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateConnection stores the changed connection settings, clearing the login
// state when BLZ, user id or server URL changed.
//
// The name is only a label: renaming changes nothing about the connection. A
// blank BIC stores an empty BIC; a blank BankName leaves the stored name alone.
func UpdateConnection(
	ctx context.Context,
	d *db.Database,
	id model.BankConnectionID,
	update model.BankConnectionUpdate,
) (*model.BankAccessItem, error) {
	var updated *model.BankAccessItem

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		current, ok, err := readConnection(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		if !ok {
			return audit.LogConn(
				ctx, tx, "bank_connection.update_connection", audit.ActorUser, repo.IDOnly(int64(id)),
			)
		}

		bankName := current.BankName
		if update.BankName != nil {
			trimmed := strings.TrimSpace(*update.BankName)
			if trimmed != "" {
				bankName = truncateName(trimmed)
			}
		}
		userID := current.UserID
		if update.UserID != nil {
			userID = strings.TrimSpace(*update.UserID)
		}
		serverURL := current.ServerURL
		if update.ServerURL != nil {
			trimmed := strings.TrimSpace(*update.ServerURL)
			if trimmed == "" {
				serverURL = nil
			} else {
				serverURL = &trimmed
			}
		}
		blz := current.BLZ
		if update.BLZ != nil {
			blz = strings.TrimSpace(*update.BLZ)
		}
		bic := current.BIC
		if update.BIC != nil {
			trimmed := strings.TrimSpace(*update.BIC)
			bic = &trimmed
		}

		loginChanged := current.BLZ != blz ||
			current.UserID != userID ||
			differentStringPointers(current.ServerURL, serverURL)
		if loginChanged {
			if err := clearLoginStateConn(ctx, tx, int64(id)); err != nil {
				return err
			}
		}

		var tanMethod *string
		if loginChanged {
			tanMethod = nil
		} else if update.TanMethod != nil {
			tanMethod = update.TanMethod
		} else {
			tanMethod = current.TanMethod
		}
		var capabilities *string
		if !loginChanged {
			capabilities = current.Capabilities
		}

		if _, err := tx.ExecContext(
			ctx,
			"UPDATE bank_connections\n"+
				"             SET bank_name = ?1, blz = ?2, bic = ?3, user_id = ?4, server_url = ?5, tan_method = ?6,\n"+
				"                 capabilities = ?7\n"+
				"             WHERE id = ?8",
			bankName,
			blz,
			optionalString(bic),
			userID,
			optionalString(serverURL),
			optionalString(tanMethod),
			optionalString(capabilities),
			int64(id),
		); err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, "bank_connection.update_connection", audit.ActorUser,
			repo.IDAndName(int64(id), bankName),
		); err != nil {
			return err
		}

		loaded, err := readItem(ctx, tx, int64(id))
		if err != nil {
			return err
		}
		updated = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func differentStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left != nil || right != nil
	}
	return *left != *right
}

func deleteConn(ctx context.Context, d *db.Database, id model.BankConnectionID, action string) (bool, error) {
	deleted := false

	err := repo.InTx(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM bank_connections WHERE id = ?1", int64(id))
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if err := audit.LogConn(
			ctx, tx, action, audit.ActorUser, repo.IDOnly(int64(id)),
		); err != nil {
			return err
		}
		deleted = changed > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// Delete deletes the connection. Its accounts and their transactions go
// through the foreign keys.
func Delete(ctx context.Context, d *db.Database, id model.BankConnectionID) (bool, error) {
	return deleteConn(ctx, d, id, "bank_connection.delete")
}

// DeleteWithAccounts deletes the connection together with its accounts and
// their transactions.
func DeleteWithAccounts(ctx context.Context, d *db.Database, id model.BankConnectionID) (bool, error) {
	return deleteConn(ctx, d, id, "bank_connection.delete_with_accounts")
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalDate(date *civil.Date) (any, error) {
	if date == nil {
		return nil, nil
	}
	return repo.FormatDate(*date)
}
