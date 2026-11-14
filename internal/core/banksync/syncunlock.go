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
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// SyncUnlockKey is the settings key of a bank access's sync unlock.
func SyncUnlockKey(bankID model.BankConnectionID) string {
	return "fints.sync_unlocked." + strconv.FormatInt(int64(bankID), 10)
}

// SyncUnlockedOnKey is the settings key of the local civil date on which a
// bank access's current sync unlock was set.
func SyncUnlockedOnKey(bankID model.BankConnectionID) string {
	return "fints.sync_unlocked_on." + strconv.FormatInt(int64(bankID), 10)
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

// MarkSyncUnlocked stores a GO connection test's fingerprint and the local
// civil date of the test for bankID, then writes the audit entry.
func (s *BankingService) MarkSyncUnlocked(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	fingerprint string,
) error {
	if err := settings.Set(ctx, database, SyncUnlockKey(bankID), fingerprint); err != nil {
		return internalError(err)
	}
	if err := settings.Set(
		ctx,
		database,
		SyncUnlockedOnKey(bankID),
		s.client.today().String(),
	); err != nil {
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

// MarkSyncLocked deletes bankID's unlock and its date and writes the audit
// entry, but only when an unlock existed.
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
		// A date without an unlock is stale; clear it without an audit entry.
		if err := settings.Delete(ctx, database, SyncUnlockedOnKey(bankID)); err != nil {
			return internalError(err)
		}
		return nil
	}
	if err := settings.Delete(ctx, database, SyncUnlockKey(bankID)); err != nil {
		return internalError(err)
	}
	if err := settings.Delete(ctx, database, SyncUnlockedOnKey(bankID)); err != nil {
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

// SyncUnlockedOn returns the local civil date on which bankID's current
// unlock was set, or nil when the bank is locked or an older unlock has no
// stored date.
func (s *BankingService) SyncUnlockedOn(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	connection FinTsConnection,
) *civil.Date {
	if !s.SyncUnlocked(ctx, database, bankID, connection) {
		return nil
	}
	return s.client.SyncUnlockedOn(ctx, database, bankID)
}

// SyncUnlockedOn returns the local civil date stored with bankID's unlock, or
// nil when it is missing or unreadable.
func (c *FinTsClient) SyncUnlockedOn(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
) *civil.Date {
	value, ok, err := settings.Get(ctx, database, SyncUnlockedOnKey(bankID))
	if err != nil || !ok {
		return nil
	}
	date, err := civil.ParseISO(value)
	if err != nil {
		return nil
	}
	return &date
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

// today returns the client's local civil date.
func (c *FinTsClient) today() civil.Date {
	if c == nil || c.now == nil {
		return civil.FromTime(time.Now())
	}
	return civil.FromTime(c.now())
}
