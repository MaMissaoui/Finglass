package bankconnections

import (
	"context"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
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

func newConnection() model.NewBankConnection {
	bic := "DEUTDEDBXXX"
	return model.NewBankConnection{
		BankName: "Deutsche Bank",
		BLZ:      "37040044",
		BIC:      &bic,
		UserID:   "testuser123",
	}
}

func pointer(value string) *string {
	return &value
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()

	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestCreateBankAccess(t *testing.T) {
	d := database(t)
	connection, err := Create(context.Background(), d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if connection.BankName != "Deutsche Bank" || connection.BLZ != "37040044" ||
		connection.BIC == nil || *connection.BIC != "DEUTDEDBXXX" ||
		connection.UserID != "testuser123" || connection.Offline || !connection.IncludeInSyncAll {
		t.Errorf("connection = %+v", connection)
	}

	loaded, ok, err := Get(context.Background(), d, connection.ID)
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if !reflect.DeepEqual(loaded, connection) {
		t.Errorf("Get = %+v, want %+v", loaded, connection)
	}
}

func TestFindByIdAndByBlz(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	created, err := Create(ctx, d, model.NewBankConnection{
		BankName: "Commerzbank",
		BLZ:      "37040044",
		BIC:      pointer("COBADEFFXXX"),
		UserID:   "user456",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, ok, err := Get(ctx, d, created.ID)
	if err != nil || !ok || loaded.BankName != "Commerzbank" {
		t.Errorf("Get = %+v, %v, %v", loaded, ok, err)
	}

	if _, err := Create(ctx, d, model.NewBankConnection{
		BankName: "Sparkasse",
		BLZ:      "37050198",
		BIC:      pointer("COLSDE33XXX"),
		UserID:   "user789",
	}); err != nil {
		t.Fatalf("Create Sparkasse: %v", err)
	}
	found, err := FindByBLZ(ctx, d, "37050198")
	if err != nil || len(found) != 1 || found[0].BankName != "Sparkasse" {
		t.Errorf("FindByBLZ = %+v, %v", found, err)
	}
	none, err := FindByBLZ(ctx, d, "00000000")
	if err != nil || len(none) != 0 {
		t.Errorf("FindByBLZ(unknown) = %+v, %v", none, err)
	}
}

func TestUpdateBankAccessAndLastSync(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, model.NewBankConnection{
		BankName: "Volksbank",
		BLZ:      "37060190",
		BIC:      pointer("GENODED1BRS"),
		UserID:   "user101",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := Update(ctx, d, created.ID, nil, pointer("942"))
	if err != nil || updated == nil {
		t.Fatalf("Update = %v, %v", updated, err)
	}
	if updated.TanMethod == nil || *updated.TanMethod != "942" {
		t.Errorf("tan method = %v", updated.TanMethod)
	}

	synced, err := UpdateLastSync(ctx, d, created.ID)
	if err != nil || synced == nil {
		t.Fatalf("UpdateLastSync = %v, %v", synced, err)
	}
	if synced.LastSync == nil {
		t.Error("last sync is nil")
	}
	if missing, err := UpdateLastSync(ctx, d, model.BankConnectionID(999999)); err != nil || missing != nil {
		t.Errorf("UpdateLastSync(unknown) = %v, %v", missing, err)
	}
}

func TestDeleteBankAccess(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	deleted, err := Delete(ctx, d, created.ID)
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	if _, ok, err := Get(ctx, d, created.ID); err != nil || ok {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	if deleted, err := Delete(ctx, d, created.ID); err != nil || deleted {
		t.Errorf("second Delete = %v, %v; a second delete finds nothing", deleted, err)
	}
}

func TestFindAllListsEveryConnection(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	if _, err := Create(ctx, d, newConnection()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := Create(ctx, d, model.NewBankConnection{
		BankName: "Bank2",
		BLZ:      "22222222",
		BIC:      pointer("BANK2DEFF"),
		UserID:   "u2",
	}); err != nil {
		t.Fatalf("Create Bank2: %v", err)
	}

	connections, err := List(ctx, d)
	if err != nil || len(connections) != 2 {
		t.Errorf("List length = %d, %v", len(connections), err)
	}
	items, err := ListItems(ctx, d)
	if err != nil || len(items) != 2 {
		t.Errorf("ListItems length = %d, %v", len(items), err)
	}
}

func TestUpdateCapabilitiesAndClearABlankValue(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.Capabilities != nil {
		t.Fatalf("GetItem = %+v, %v", item, err)
	}

	updated, err := UpdateCapabilities(ctx, d, created.ID, "SaldoReq,UebSEPA")
	if err != nil || updated == nil || updated.Capabilities == nil ||
		*updated.Capabilities != "SaldoReq,UebSEPA" {
		t.Fatalf("UpdateCapabilities = %+v, %v", updated, err)
	}
	item, ok, err = GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.Capabilities == nil || *item.Capabilities != "SaldoReq,UebSEPA" {
		t.Errorf("stored capabilities = %+v, %v", item, err)
	}

	if _, err := UpdateCapabilities(ctx, d, created.ID, "  "); err != nil {
		t.Fatalf("UpdateCapabilities(blank): %v", err)
	}
	item, ok, err = GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.Capabilities != nil {
		t.Errorf("capabilities after a blank value = %+v, %v", item, err)
	}
}

func TestUpdateConnectionStoresBlzAndBic(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, model.NewBankConnection{
		BankName: "HVB",
		BLZ:      "79020076",
		BIC:      pointer("HYVEDEMM455"),
		UserID:   "u4",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BLZ: pointer("70020270"),
		BIC: pointer("HYVEDEMMXXX"),
	})
	if err != nil || updated == nil {
		t.Fatalf("UpdateConnection = %v, %v", updated, err)
	}
	if updated.BLZ != "70020270" || updated.BIC == nil || *updated.BIC != "HYVEDEMMXXX" {
		t.Errorf("updated = %+v", updated)
	}

	// A blank BIC stores "" (an unknown BIC); a nil leaves the stored one
	// alone.
	blank, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BIC: pointer("  "),
	})
	if err != nil || blank == nil || blank.BIC == nil || *blank.BIC != "" {
		t.Fatalf("blank BIC = %+v, %v", blank, err)
	}

	renamed, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BLZ: pointer("12030000"),
	})
	if err != nil || renamed == nil || renamed.BIC == nil || *renamed.BIC != "" {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.BLZ != "12030000" {
		t.Errorf("stored BLZ = %+v, %v", item, err)
	}
}

func TestClearLoginStateDropsCapabilitiesAndTanMethod(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := UpdateCapabilities(ctx, d, created.ID, "SaldoReq,UebSEPA"); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	if _, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		TanMethod: pointer("942"),
	}); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.Capabilities == nil || *item.Capabilities != "SaldoReq,UebSEPA" {
		t.Fatalf("capabilities = %+v, %v", item, err)
	}
	if item.TanMethod == nil || *item.TanMethod != "942" {
		t.Fatalf("tan method = %v", item.TanMethod)
	}

	cleared, err := ClearLoginState(ctx, d, created.ID)
	if err != nil || cleared == nil {
		t.Fatalf("ClearLoginState = %v, %v", cleared, err)
	}
	if cleared.Capabilities != nil || cleared.TanMethod != nil {
		t.Errorf("cleared = %+v", cleared)
	}
	item, ok, err = GetItem(ctx, d, created.ID)
	if err != nil || !ok || item.Capabilities != nil || item.TanMethod != nil {
		t.Errorf("stored login state = %+v, %v", item, err)
	}
}

func TestChangingTheLoginClearsTheLoginStateButRenamingDoesNot(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := UpdateCapabilities(ctx, d, created.ID, "SaldoReq"); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	if _, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		TanMethod: pointer("942"),
	}); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	renamed, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BankName: pointer("Renamed"),
	})
	if err != nil || renamed == nil {
		t.Fatalf("rename = %v, %v", renamed, err)
	}
	if renamed.BankName != "Renamed" || renamed.Capabilities == nil || *renamed.Capabilities != "SaldoReq" ||
		renamed.TanMethod == nil || *renamed.TanMethod != "942" {
		t.Errorf("renamed = %+v", renamed)
	}

	changed, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		UserID: pointer("other-user"),
	})
	if err != nil || changed == nil {
		t.Fatalf("login change = %v, %v", changed, err)
	}
	if changed.Capabilities != nil || changed.TanMethod != nil {
		t.Errorf("after a login change = %+v", changed)
	}
}

func TestOfflineBanksNeverGoOnlineOrJoinSyncAll(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := CreateOffline(ctx, d, "Credit card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}

	if created.BankName != "Credit card" || created.BLZ != "" ||
		created.BIC == nil || *created.BIC != "" || created.UserID != "" ||
		!created.Offline || created.IncludeInSyncAll {
		t.Errorf("created = %+v", created)
	}
	item, ok, err := GetItem(ctx, d, created.ID)
	if err != nil || !ok || !item.Offline {
		t.Errorf("GetItem = %+v, %v", item, err)
	}
}

func TestDeleteWithAccountsCascadesThroughTheAccounts(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	bank, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	name := "Account"
	accountID, err := accounts.Create(ctx, d, model.NewAccount{
		BankConnectionID: &bank.ID,
		Name:             &name,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}

	deleted, err := DeleteWithAccounts(ctx, d, bank.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteWithAccounts = %v, %v", deleted, err)
	}
	if _, ok, err := Get(ctx, d, bank.ID); err != nil || ok {
		t.Errorf("Get(bank) = %v, %v", ok, err)
	}
	if _, ok, err := accounts.Get(ctx, d, accountID); err != nil || ok {
		t.Errorf("Get(account) = %v, %v", ok, err)
	}
}

func TestSetSyncFromAndIncludeInSyncAll(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	value := date(t, 2026, time.January, 1)
	dated, err := SetSyncFrom(ctx, d, created.ID, &value)
	if err != nil || dated == nil || dated.SyncFrom == nil || *dated.SyncFrom != value {
		t.Fatalf("SetSyncFrom = %+v, %v", dated, err)
	}

	cleared, err := SetSyncFrom(ctx, d, created.ID, nil)
	if err != nil || cleared == nil || cleared.SyncFrom != nil {
		t.Fatalf("SetSyncFrom(nil) = %+v, %v", cleared, err)
	}

	excluded, err := SetIncludeInSyncAll(ctx, d, created.ID, false)
	if err != nil || excluded == nil || excluded.IncludeInSyncAll {
		t.Errorf("SetIncludeInSyncAll = %+v, %v", excluded, err)
	}
}

func TestEveryMutationWritesOneAuditEntryWithoutTheUserId(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	before := len(entries(t, d))
	expected := before

	created, err := Create(ctx, d, model.NewBankConnection{
		BankName: "Audit Bank",
		BLZ:      "37040044",
		BIC:      pointer("DEUTDEDBXXX"),
		UserID:   "secret-user",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	expected++

	if _, err := Update(ctx, d, created.ID, pointer("Audit Bank 2"), nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	expected++
	if _, err := UpdateLastSync(ctx, d, created.ID); err != nil {
		t.Fatalf("UpdateLastSync: %v", err)
	}
	expected++
	syncFrom := date(t, 2026, time.January, 1)
	if _, err := SetSyncFrom(ctx, d, created.ID, &syncFrom); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}
	expected++
	if _, err := SetIncludeInSyncAll(ctx, d, created.ID, false); err != nil {
		t.Fatalf("SetIncludeInSyncAll: %v", err)
	}
	expected++
	if _, err := UpdateCapabilities(ctx, d, created.ID, "SaldoReq"); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	expected++
	if _, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BLZ: pointer("12030000"),
	}); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	expected++
	if _, err := ClearLoginState(ctx, d, created.ID); err != nil {
		t.Fatalf("ClearLoginState: %v", err)
	}
	expected++
	if _, err := DeleteWithAccounts(ctx, d, created.ID); err != nil {
		t.Fatalf("DeleteWithAccounts: %v", err)
	}
	expected++

	offline, err := CreateOffline(ctx, d, "Credit card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}
	expected++
	if _, err := Delete(ctx, d, offline.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	expected++

	recent := entries(t, d)
	if len(recent) != expected {
		t.Fatalf("audit entries = %d, want %d", len(recent), expected)
	}
	details := make(map[string]string)
	for _, entry := range recent {
		if entry.Actor != audit.ActorUser {
			t.Errorf("audit actor = %q", entry.Actor)
		}
		for _, secret := range []string{"secret-user", "37040044", "12030000", "DEUTDEDBXXX"} {
			if strings.Contains(entry.Details, secret) {
				t.Errorf("an audit entry leaked %q: %q", secret, entry.Details)
			}
		}
		details[entry.Action] = entry.Details
	}
	for action, want := range map[string]string{
		"bank_connection.create":                  "id=" + itoa(int64(created.ID)) + " name=Audit Bank",
		"bank_connection.update":                  "id=" + itoa(int64(created.ID)) + " name=Audit Bank 2",
		"bank_connection.update_last_sync":        "id=" + itoa(int64(created.ID)),
		"bank_connection.set_sync_from":           "id=" + itoa(int64(created.ID)),
		"bank_connection.set_include_in_sync_all": "id=" + itoa(int64(created.ID)),
		"bank_connection.update_capabilities":     "id=" + itoa(int64(created.ID)),
		"bank_connection.update_connection":       "id=" + itoa(int64(created.ID)) + " name=Audit Bank 2",
		"bank_connection.clear_login_state":       "id=" + itoa(int64(created.ID)),
		"bank_connection.delete_with_accounts":    "id=" + itoa(int64(created.ID)),
		"bank_connection.create_offline":          "id=" + itoa(int64(offline.ID)) + " name=Credit card",
		"bank_connection.delete":                  "id=" + itoa(int64(offline.ID)),
	} {
		if got := details[action]; got != want {
			t.Errorf("%s details = %q, want %q", action, got, want)
		}
	}
}

func TestUnknownConnectionsReturnNilAndAreAudited(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	missing := model.BankConnectionID(999999)

	if connection, ok, err := Get(ctx, d, missing); err != nil || ok {
		t.Errorf("Get(unknown) = %+v, %v, %v", connection, ok, err)
	}
	if item, ok, err := GetItem(ctx, d, missing); err != nil || ok {
		t.Errorf("GetItem(unknown) = %v, %v", item, err)
	}

	steps := []struct {
		action func() error
		name   string
	}{
		{
			action: func() error {
				_, err := Update(ctx, d, missing, pointer("Name"), nil)
				return err
			},
			name: "bank_connection.update",
		},
		{
			action: func() error {
				_, err := UpdateLastSync(ctx, d, missing)
				return err
			},
			name: "bank_connection.update_last_sync",
		},
		{
			action: func() error {
				_, err := SetSyncFrom(ctx, d, missing, nil)
				return err
			},
			name: "bank_connection.set_sync_from",
		},
		{
			action: func() error {
				_, err := SetIncludeInSyncAll(ctx, d, missing, true)
				return err
			},
			name: "bank_connection.set_include_in_sync_all",
		},
		{
			action: func() error {
				_, err := ClearLoginState(ctx, d, missing)
				return err
			},
			name: "bank_connection.clear_login_state",
		},
		{
			action: func() error {
				_, err := UpdateCapabilities(ctx, d, missing, "SaldoReq")
				return err
			},
			name: "bank_connection.update_capabilities",
		},
		{
			action: func() error {
				_, err := UpdateConnection(ctx, d, missing, model.BankConnectionUpdate{BLZ: pointer("12030000")})
				return err
			},
			name: "bank_connection.update_connection",
		},
	}
	for _, step := range steps {
		before := len(entries(t, d))
		if err := step.action(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		recent := entries(t, d)
		if len(recent) != before+1 {
			t.Fatalf("audit entries after %s = %d, want %d", step.name, len(recent), before+1)
		}
		if recent[0].Action != step.name || recent[0].Actor != audit.ActorUser ||
			recent[0].Details != "id="+itoa(int64(missing)) {
			t.Errorf("audit entry for %s = %+v", step.name, recent[0])
		}
	}
}

func TestUpdateConnectionTruncatesTheBankNameByRunes(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	created, err := Create(ctx, d, newConnection())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	longName := strings.Repeat("ä", MaxBankNameLength+10)
	updated, err := UpdateConnection(ctx, d, created.ID, model.BankConnectionUpdate{
		BankName: &longName,
	})
	if err != nil || updated == nil {
		t.Fatalf("UpdateConnection = %v, %v", updated, err)
	}
	if got := len([]rune(updated.BankName)); got != MaxBankNameLength {
		t.Errorf("bank name rune length = %d, want %d", got, MaxBankNameLength)
	}
}

func TestDeleteUnknownConnectionIsFalseButAudited(t *testing.T) {
	d := database(t)
	missing := model.BankConnectionID(999999)
	before := len(entries(t, d))

	deleted, err := Delete(context.Background(), d, missing)
	if err != nil || deleted {
		t.Fatalf("Delete(unknown) = %v, %v", deleted, err)
	}
	recent := entries(t, d)
	if len(recent) != before+1 || recent[0].Action != "bank_connection.delete" {
		t.Errorf("audit entry = %+v", recent[0])
	}
}
