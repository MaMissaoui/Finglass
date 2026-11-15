package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// repoRoot finds the checkout root from this package's test directory.
func repoRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above the test directory")
		}
		directory = parent
	}
}

type accountGolden struct {
	ProfileDir string `json:"profile_dir"`
	BankID     int64  `json:"bank_id"`
	Account    string `json:"account"`
}

func TestAccountForMatchesTheV034Golden(t *testing.T) {
	path := filepath.Join(repoRoot(t), "testdata", "golden", "keychain_accounts.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var golden []accountGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(golden) != 6 {
		t.Fatalf("%s has %d entries, want 3 profile dirs x 2 bank ids", path, len(golden))
	}

	for _, entry := range golden {
		if got := AccountFor(entry.ProfileDir, entry.BankID); got != entry.Account {
			t.Errorf("AccountFor(%q, %d) = %q, want %q", entry.ProfileDir, entry.BankID, got, entry.Account)
		}
	}
}

func TestAccountForNormalisesLikeV034(t *testing.T) {
	for _, test := range []struct {
		profile string
		bankID  int64
		account string
	}{
		{"/tmp//profile/./one", 7, "/tmp/profile/one#bank-7"},
		{"/tmp/profile/../two", 42, "/tmp/profile/../two#bank-42"},
		{"/tmp/profile/three/", 3, "/tmp/profile/three/#bank-3"},
	} {
		if got := AccountFor(test.profile, test.bankID); got != test.account {
			t.Errorf("AccountFor(%q, %d) = %q, want %q", test.profile, test.bankID, got, test.account)
		}
	}
}

func TestMemoryStoreCopiesSecrets(t *testing.T) {
	store := NewMemory()
	secret := []byte("first")
	if err := store.Write("account", secret); err != nil {
		t.Fatalf("Write: %v", err)
	}
	secret[0] = 'X'

	got, found, err := store.Read("account")
	if err != nil || !found {
		t.Fatalf("Read = found %t, err %v", found, err)
	}
	if string(got) != "first" {
		t.Fatalf("stored secret = %q, want %q", got, "first")
	}

	got[0] = 'Y'
	again, _, err := store.Read("account")
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if string(again) != "first" {
		t.Fatalf("second Read = %q, want %q", again, "first")
	}
}
