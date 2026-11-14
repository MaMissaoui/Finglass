// Online sync per bank: a bank is unlocked by a GO connection test and locked
// again by a NO-GO or a login change.
package banksync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// SyncUnlockKey is the settings key of a bank access's sync unlock.
func SyncUnlockKey(bankID model.BankConnectionID) string {
	return "fints.sync_unlocked." + strconv.FormatInt(int64(bankID), 10)
}

// SyncFingerprint is the value stored by a GO connection test: the lowercase
// hex SHA-256 of the trimmed login the probe used.
func SyncFingerprint(blz, userID, serverURL string) string {
	sum := sha256.Sum256([]byte(
		strings.TrimSpace(blz) + "\x00" +
			strings.TrimSpace(userID) + "\x00" +
			strings.TrimSpace(serverURL),
	))
	return hex.EncodeToString(sum[:])
}

// MarkSyncUnlocked stores a GO connection test's fingerprint for bankID and
// writes the audit entry.
func (s *BankingService) MarkSyncUnlocked(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	fingerprint string,
) error {
	if err := settings.Set(ctx, database, SyncUnlockKey(bankID), fingerprint); err != nil {
		return internalError(err)
	}
	_ = audit.Log(
		ctx,
		database,
		CodeSyncUnlocked,
		audit.ActorUser,
		fmt.Sprintf("Bank access %d: sync unlocked after a GO connection test", int64(bankID)),
	)
	return nil
}

// MarkSyncLocked deletes bankID's unlock and writes the audit entry, but only
// when an unlock existed.
func (s *BankingService) MarkSyncLocked(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
) error {
	_, existed, err := settings.Get(ctx, database, SyncUnlockKey(bankID))
	if err != nil {
		return internalError(err)
	}
	if !existed {
		return nil
	}
	if err := settings.Delete(ctx, database, SyncUnlockKey(bankID)); err != nil {
		return internalError(err)
	}
	_ = audit.Log(
		ctx,
		database,
		CodeSyncLocked,
		audit.ActorUser,
		fmt.Sprintf("Bank access %d: sync locked after a NO-GO connection test", int64(bankID)),
	)
	return nil
}

// SyncUnlocked reports whether bankID's stored unlock still belongs to the
// connection's current BLZ, user id and server URL.
func (s *BankingService) SyncUnlocked(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	connection FinTsConnection,
) bool {
	return s.client.SyncUnlocked(ctx, database, bankID, connection)
}

// SyncUnlocked reports whether bankID's stored unlock still belongs to the
// connection's current BLZ, user id and server URL.
func (c *FinTsClient) SyncUnlocked(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	connection FinTsConnection,
) bool {
	value, ok, err := settings.Get(ctx, database, SyncUnlockKey(bankID))
	if err != nil || !ok {
		return false
	}
	return value == SyncFingerprint(connection.BLZ, connection.UserID, connection.ServerURL)
}
