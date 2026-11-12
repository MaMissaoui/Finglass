package banking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func savePin(
	t *testing.T,
	database *db.Database,
	store *banksync.MemoryCredentialStore,
	dir string,
	id model.BankConnectionID,
) {
	t.Helper()
	bank, ok, err := bankconnections.GetItem(context.Background(), database, id)
	if err != nil || !ok {
		t.Fatalf("get bank: ok=%t err=%v", ok, err)
	}
	if err := store.Write(banksync.PinKey(dir, int64(id)), []byte("12345")); err != nil {
		t.Fatalf("write PIN: %v", err)
	}
	if err := settings.Set(
		context.Background(),
		database,
		banksync.PinFlagKey(id),
		banksync.LoginSignature(bank),
	); err != nil {
		t.Fatalf("write PIN flag: %v", err)
	}
	marker := banksync.PinMarkerPath(dir, int64(id))
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

// hasStoredPin mirrors Rust's has_pin: the bank row, the credential store entry
// and the settings flag.
func hasStoredPin(
	t *testing.T,
	database *db.Database,
	store *banksync.MemoryCredentialStore,
	dir string,
	id model.BankConnectionID,
) bool {
	t.Helper()
	_, ok, err := bankconnections.GetItem(context.Background(), database, id)
	if err != nil || !ok {
		return false
	}
	_, saved, err := settings.Get(context.Background(), database, banksync.PinFlagKey(id))
	if err != nil {
		return false
	}
	return store.Contains(banksync.PinKey(dir, int64(id))) && saved
}

func TestChangingALoginFieldForgetsThePinButRenamingDoesNot(t *testing.T) {
	cases := []struct {
		blz  string
		user string
		url  string
	}{
		{"37040044", "user", "https://fints.example/pintan"},
		{"37040044", "other", "https://fints.example/pintan"},
		{"37040044", "user", "https://other.example/pintan"},
	}
	for _, testCase := range cases {
		database := testDatabase(t)
		dir := dataDir(t)
		store := banksync.NewMemoryCredentialStore()
		service := serviceWithStore(
			newTestGateway(),
			store,
			dir,
			i18n.En,
		)
		id := onlineBank(t, database, "Sparkasse", "user")
		savePin(t, database, store, dir, id)
		state := New(i18n.En)
		if err := state.Load(context.Background(), database, service); err != nil {
			t.Fatalf("load: %v", err)
		}

		if err := state.UpdateConnection(
			context.Background(),
			database,
			service,
			id,
			"Renamed",
			testCase.blz,
			testCase.user,
			testCase.url,
		); err != nil {
			t.Fatalf("update connection: %v", err)
		}
		changed := testCase.blz != "37040044" ||
			testCase.user != "user" ||
			testCase.url != "https://fints.example/pintan"
		if got := !hasStoredPin(t, database, store, dir, id); got != changed {
			t.Fatalf(
				"blz=%s user=%s url=%s: pin forgotten = %t, want %t",
				testCase.blz,
				testCase.user,
				testCase.url,
				got,
				changed,
			)
		}
	}

	database := testDatabase(t)
	dir := dataDir(t)
	store := banksync.NewMemoryCredentialStore()
	service := serviceWithStore(newTestGateway(), store, dir, i18n.En)
	id := onlineBank(t, database, "Sparkasse", "user")
	savePin(t, database, store, dir, id)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := state.UpdateConnection(
		context.Background(),
		database,
		service,
		id,
		"Only renamed",
		"37040044",
		"user",
		"https://fints.example/pintan",
	); err != nil {
		t.Fatalf("rename connection: %v", err)
	}
	if !hasStoredPin(t, database, store, dir, id) {
		t.Fatal("renaming alone forgot the PIN")
	}
}

func TestDeletingAnOnlineBankForgetsItsPin(t *testing.T) {
	database := testDatabase(t)
	dir := dataDir(t)
	store := banksync.NewMemoryCredentialStore()
	service := serviceWithStore(newTestGateway(), store, dir, i18n.En)
	id := onlineBank(t, database, "Sparkasse", "user")
	savePin(t, database, store, dir, id)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	busy := accountsview.NewBusy()
	if err := state.RemoveBank(context.Background(), database, service, &busy, id); err != nil {
		t.Fatalf("remove bank: %v", err)
	}
	if store.Contains(banksync.PinKey(dir, int64(id))) {
		t.Fatal("the credential store still holds the PIN")
	}
	if _, ok, err := bankconnections.GetItem(context.Background(), database, id); err != nil || ok {
		t.Fatalf("bank still exists: ok=%t err=%v", ok, err)
	}
}

// TestRemoveBankRemovesEveryPinArtifact adds the assertions Rust's test leaves
// out: the settings flag and the marker file go as well.
func TestRemoveBankRemovesEveryPinArtifact(t *testing.T) {
	database := testDatabase(t)
	dir := dataDir(t)
	store := banksync.NewMemoryCredentialStore()
	service := serviceWithStore(newTestGateway(), store, dir, i18n.En)
	id := onlineBank(t, database, "Sparkasse", "user")
	savePin(t, database, store, dir, id)

	if !store.Contains(banksync.PinKey(dir, int64(id))) {
		t.Fatal("test setup: no store entry")
	}
	if _, ok, err := settings.Get(context.Background(), database, banksync.PinFlagKey(id)); err != nil || !ok {
		t.Fatalf("test setup: no flag: ok=%t err=%v", ok, err)
	}
	if _, err := os.Stat(banksync.PinMarkerPath(dir, int64(id))); err != nil {
		t.Fatalf("test setup: no marker: %v", err)
	}

	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}
	busy := accountsview.NewBusy()
	if err := state.RemoveBank(context.Background(), database, service, &busy, id); err != nil {
		t.Fatalf("remove bank: %v", err)
	}

	if store.Contains(banksync.PinKey(dir, int64(id))) {
		t.Fatal("the credential store still holds the PIN")
	}
	if _, ok, err := settings.Get(context.Background(), database, banksync.PinFlagKey(id)); err != nil || ok {
		t.Fatalf("the PIN flag still exists: ok=%t err=%v", ok, err)
	}
	if _, err := os.Stat(banksync.PinMarkerPath(dir, int64(id))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker still exists: %v", err)
	}
}

// TestALoginFieldChangeRemovesEveryPinArtifact checks each login field and the
// flag and marker file Rust's test leaves out.
func TestALoginFieldChangeRemovesEveryPinArtifact(t *testing.T) {
	cases := []struct {
		name string
		blz  string
		user string
		url  string
	}{
		{"bank code", "37050198", "user", "https://fints.example/pintan"},
		{"user id", "37040044", "other", "https://fints.example/pintan"},
		{"server URL", "37040044", "user", "https://other.example/pintan"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database := testDatabase(t)
			dir := dataDir(t)
			store := banksync.NewMemoryCredentialStore()
			service := serviceWithStore(newTestGateway(), store, dir, i18n.En)
			id := onlineBank(t, database, "Sparkasse", "user")
			savePin(t, database, store, dir, id)
			state := New(i18n.En)
			if err := state.Load(context.Background(), database, service); err != nil {
				t.Fatalf("load: %v", err)
			}

			if err := state.UpdateConnection(
				context.Background(),
				database,
				service,
				id,
				"Renamed",
				testCase.blz,
				testCase.user,
				testCase.url,
			); err != nil {
				t.Fatalf("update connection: %v", err)
			}

			if store.Contains(banksync.PinKey(dir, int64(id))) {
				t.Fatal("the credential store still holds the PIN")
			}
			if _, ok, err := settings.Get(context.Background(), database, banksync.PinFlagKey(id)); err != nil || ok {
				t.Fatalf("the PIN flag still exists: ok=%t err=%v", ok, err)
			}
			if _, err := os.Stat(banksync.PinMarkerPath(dir, int64(id))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the marker still exists: %v", err)
			}
		})
	}
}
