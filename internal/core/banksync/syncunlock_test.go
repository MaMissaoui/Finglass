package banksync

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func connectionFromItem(bank model.BankAccessItem) FinTsConnection {
	serverURL := ""
	if bank.ServerURL != nil {
		serverURL = *bank.ServerURL
	}
	return FinTsConnection{
		BankName:  bank.BankName,
		BLZ:       bank.BLZ,
		UserID:    bank.UserID,
		ServerURL: serverURL,
	}
}

func markHarnessUnlocked(t *testing.T, h *harness) FinTsConnection {
	t.Helper()
	connection := connectionFromItem(loadBankItem(t, h.db, h.bank))
	fingerprint := SyncFingerprint(connection.BLZ, connection.UserID, connection.ServerURL)
	if err := h.service.MarkSyncUnlocked(context.Background(), h.db, h.bank, fingerprint, h.today); err != nil {
		t.Fatalf("MarkSyncUnlocked: %v", err)
	}
	return connection
}

func TestAGOTestUnlocksTheBankAndANoGoLocksItAgain(t *testing.T) {
	h := newHarness(t)
	connection := markHarnessUnlocked(t, h)
	if !h.service.SyncUnlocked(context.Background(), h.db, h.bank, connection) {
		t.Fatal("the bank is not unlocked after a GO test")
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || !ok {
		t.Fatalf("unlock setting missing: ok=%t err=%v", ok, err)
	}
	if got, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || !ok || got != h.today.String() {
		t.Fatalf("unlock date = %q, ok=%t err=%v; want %q", got, ok, err, h.today)
	}
	if date := h.service.SyncUnlockedOn(context.Background(), h.db, h.bank); date == nil || date.String() != h.today.String() {
		t.Fatalf("SyncUnlockedOn = %v, want %s", date, h.today)
	}
	if got := auditCount(t, h.db, CodeSyncUnlocked); got != 1 {
		t.Fatalf("unlock audit entries = %d, want 1", got)
	}
	entries := auditEntriesWithAction(t, h.db, CodeSyncUnlocked)
	if len(entries) != 1 {
		t.Fatalf("unlock entries = %d", len(entries))
	}
	if strings.Contains(entries[0].Details, "demo-user") || strings.Contains(entries[0].Details, "37050198") {
		t.Fatalf("unlock audit leaked login data: %q", entries[0].Details)
	}

	if err := h.service.MarkSyncLocked(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("MarkSyncLocked: %v", err)
	}
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, connection) {
		t.Fatal("the bank is still unlocked after a NO-GO test")
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || ok {
		t.Fatalf("unlock setting still exists: ok=%t err=%v", ok, err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || ok {
		t.Fatalf("unlock date still exists: ok=%t err=%v", ok, err)
	}
	if date := h.service.SyncUnlockedOn(context.Background(), h.db, h.bank); date != nil {
		t.Fatalf("SyncUnlockedOn after lock = %v, want nil", date)
	}
	if got := auditCount(t, h.db, CodeSyncLocked); got != 1 {
		t.Fatalf("lock audit entries = %d, want 1", got)
	}
	if err := h.service.MarkSyncLocked(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("second MarkSyncLocked: %v", err)
	}
	if got := auditCount(t, h.db, CodeSyncLocked); got != 1 {
		t.Fatalf("a lock without an unlock wrote an audit entry: %d", got)
	}
}

func TestAnUnlockOnlyMatchesTheLoginTheTestUsed(t *testing.T) {
	h := newHarness(t)
	connection := markHarnessUnlocked(t, h)

	other := connection
	other.UserID = "another-user"
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, other) {
		t.Fatal("a changed user id kept the unlock")
	}
	other = connection
	other.BLZ = "12345678"
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, other) {
		t.Fatal("a changed BLZ kept the unlock")
	}
	other = connection
	other.ServerURL = "https://other.example/fints"
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, other) {
		t.Fatal("a changed server URL kept the unlock")
	}
}

func TestALoginChangeLocksTheBankAndRemovingItDeletesTheKey(t *testing.T) {
	h := newHarness(t)
	markHarnessUnlocked(t, h)

	updated, err := h.service.UpdateConnection(
		context.Background(), h.db, h.bank, "Sparkasse", "37050198", "new-user",
		"https://fints.example/pintan",
	)
	if err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	connection := connectionFromItem(updated)
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, connection) {
		t.Fatal("a login change kept the unlock")
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || ok {
		t.Fatalf("a login change kept the unlock key: ok=%t err=%v", ok, err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || ok {
		t.Fatalf("a login change kept the unlock date: ok=%t err=%v", ok, err)
	}

	markHarnessUnlocked(t, h)
	if _, err := h.service.RemoveBank(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("RemoveBank: %v", err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || ok {
		t.Fatalf("the removed bank kept its unlock key: ok=%t err=%v", ok, err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || ok {
		t.Fatalf("the removed bank kept its unlock date: ok=%t err=%v", ok, err)
	}
}

func TestAnUnlockWithoutDateStaysUnlockedButHasNoDate(t *testing.T) {
	h := newHarness(t)
	connection := connectionFromItem(loadBankItem(t, h.db, h.bank))
	fingerprint := SyncFingerprint(connection.BLZ, connection.UserID, connection.ServerURL)
	if err := settings.Set(context.Background(), h.db, SyncUnlockKey(h.bank), fingerprint); err != nil {
		t.Fatalf("set old unlock: %v", err)
	}
	if !h.service.SyncUnlocked(context.Background(), h.db, h.bank, connection) {
		t.Fatal("the old unlock is not accepted")
	}
	if date := h.service.SyncUnlockedOn(context.Background(), h.db, h.bank); date != nil {
		t.Fatalf("SyncUnlockedOn = %v, want nil for an older unlock", date)
	}
}

func TestAnIDReusedByAnotherLoginStaysLocked(t *testing.T) {
	h := newHarness(t)
	markHarnessUnlocked(t, h)

	// The id survives, the login changes. The stored fingerprint belongs to the
	// old login and must not unlock the new one.
	updated, err := h.service.UpdateConnection(
		context.Background(), h.db, h.bank, "Sparkasse", "37050198", "demo-user",
		"https://other.example/fints",
	)
	if err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	connection := connectionFromItem(updated)
	if h.service.SyncUnlocked(context.Background(), h.db, h.bank, connection) {
		t.Fatal("the reused id unlocked the new login")
	}
}

func execSettingStatement(t *testing.T, h *harness, statement string) {
	t.Helper()
	err := h.db.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, statement)
		return err
	})
	if err != nil {
		t.Fatalf("exec setting statement: %v", err)
	}
}

func failSettingWrite(t *testing.T, h *harness, key string, operation string) {
	t.Helper()
	execSettingStatement(t, h, "CREATE TRIGGER fail_setting BEFORE "+operation+" ON settings "+
		"WHEN NEW.key = '"+key+"' BEGIN SELECT RAISE(ABORT, 'invented failure'); END")
}

func failSettingDelete(t *testing.T, h *harness, key string) {
	t.Helper()
	execSettingStatement(t, h, "CREATE TRIGGER fail_setting_delete BEFORE DELETE ON settings "+
		"WHEN OLD.key = '"+key+"' BEGIN SELECT RAISE(ABORT, 'invented failure'); END")
}

func TestMarkSyncUnlockedWritesTheDateBeforeTheUnlockKey(t *testing.T) {
	h := newHarness(t)
	connection := connectionFromItem(loadBankItem(t, h.db, h.bank))
	failSettingWrite(t, h, SyncUnlockKey(h.bank), "INSERT")

	err := h.service.MarkSyncUnlocked(
		context.Background(),
		h.db,
		h.bank,
		SyncFingerprint(connection.BLZ, connection.UserID, connection.ServerURL),
		h.today,
	)
	if err == nil {
		t.Fatal("MarkSyncUnlocked succeeded although the unlock key write failed")
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || ok {
		t.Fatalf("unlock key exists after the failed second write: ok=%t err=%v", ok, err)
	}
	if got, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil ||
		!ok || got != h.today.String() {
		t.Fatalf("date after the failed second write = %q, ok=%t err=%v; want %q", got, ok, err, h.today)
	}
	if got := auditCount(t, h.db, CodeSyncUnlocked); got != 0 {
		t.Fatalf("unlock audit entries after a failed write = %d, want 0", got)
	}
}

func TestMarkSyncLockedWritesTheAuditEntryAfterTheUnlockKeyIsGone(t *testing.T) {
	h := newHarness(t)
	markHarnessUnlocked(t, h)
	failSettingDelete(t, h, SyncUnlockedOnKey(h.bank))

	if err := h.service.MarkSyncLocked(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("MarkSyncLocked: %v", err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockKey(h.bank)); err != nil || ok {
		t.Fatalf("unlock key exists after lock: ok=%t err=%v", ok, err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || !ok {
		t.Fatalf("stale date was not left for cleanup: ok=%t err=%v", ok, err)
	}
	if got := auditCount(t, h.db, CodeSyncLocked); got != 1 {
		t.Fatalf("lock audit entries = %d, want 1 even when the date delete failed", got)
	}
}

func TestARepeatedGoOverwritesTheDate(t *testing.T) {
	h := newHarness(t)
	connection := connectionFromItem(loadBankItem(t, h.db, h.bank))
	fingerprint := SyncFingerprint(connection.BLZ, connection.UserID, connection.ServerURL)
	first := mustDate(t, 2026, time.October, 4)
	second := mustDate(t, 2026, time.November, 15)

	if err := h.service.MarkSyncUnlocked(context.Background(), h.db, h.bank, fingerprint, first); err != nil {
		t.Fatalf("first MarkSyncUnlocked: %v", err)
	}
	if err := h.service.MarkSyncUnlocked(context.Background(), h.db, h.bank, fingerprint, second); err != nil {
		t.Fatalf("second MarkSyncUnlocked: %v", err)
	}
	if got, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil ||
		!ok || got != second.String() {
		t.Fatalf("date after a repeated GO = %q, ok=%t err=%v; want %q", got, ok, err, second)
	}
	if got := auditCount(t, h.db, CodeSyncUnlocked); got != 2 {
		t.Fatalf("unlock audit entries = %d, want 2", got)
	}
}

func TestMarkSyncLockedClearsAStaleDateWithoutAnUnlock(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(context.Background(), h.db, SyncUnlockedOnKey(h.bank), "2026-11-14"); err != nil {
		t.Fatalf("set stale date: %v", err)
	}
	if err := h.service.MarkSyncLocked(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("MarkSyncLocked: %v", err)
	}
	if _, ok, err := settings.Get(context.Background(), h.db, SyncUnlockedOnKey(h.bank)); err != nil || ok {
		t.Fatalf("stale date still exists: ok=%t err=%v", ok, err)
	}
	if got := auditCount(t, h.db, CodeSyncLocked); got != 0 {
		t.Fatalf("a stale-date cleanup wrote an audit entry: %d", got)
	}
}

func TestTheFingerprintIsLowercaseHexSHA256OfTheTrimmedLogin(t *testing.T) {
	fingerprint := SyncFingerprint(" 37050198 ", " demo-user ", " https://fints.example/pintan ")
	want := SyncFingerprint("37050198", "demo-user", "https://fints.example/pintan")
	if fingerprint != want {
		t.Fatalf("trimmed fingerprints differ: %q vs %q", fingerprint, want)
	}
	if len(fingerprint) != 64 || strings.ToLower(fingerprint) != fingerprint {
		t.Fatalf("fingerprint is not lowercase hex: %q", fingerprint)
	}
	if fingerprint == SyncFingerprint("37050198", "demo-user", "https://fints.example/other") {
		t.Fatal("different logins share a fingerprint")
	}
}
