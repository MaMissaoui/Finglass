package banksync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAMemoryStoreRoundTripsAndDeletes(t *testing.T) {
	store := NewMemoryCredentialStore()
	if err := store.Write("key", []byte("12345")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !store.Contains("key") {
		t.Fatal("store does not contain the key")
	}
	secret, found, err := store.Read("key")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found || string(secret) != "12345" {
		t.Fatalf("Read = %q, %t, want %q, true", secret, found, "12345")
	}
	if err := store.Delete("key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.Contains("key") {
		t.Fatal("store still contains the deleted key")
	}
	if _, found, err := store.Read("key"); err != nil || found {
		t.Fatalf("Read after Delete = found %t, error %v", found, err)
	}
}

func TestTheNoOpStoreNeverOffersRemembering(t *testing.T) {
	store := NoCredentialStore{}
	if store.IsAvailable() {
		t.Fatal("no-op store is available")
	}
	if _, found, err := store.Read("key"); err != nil || found {
		t.Fatalf("Read = found %t, error %v", found, err)
	}
	if err := store.Write("key", []byte("x")); err == nil {
		t.Fatal("Write succeeded")
	}
	if err := store.Delete("key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestAReadFailureIsReportedWithoutTheSecret(t *testing.T) {
	store := NewMemoryCredentialStore().FailingReads()
	_, _, err := store.Read("key")
	if err == nil {
		t.Fatal("Read succeeded, want an error")
	}
	if strings.Contains(err.Error(), "12345") {
		t.Fatalf("error contains the secret: %q", err)
	}
}

func TestTwoDataDirectoriesNeverShareAKey(t *testing.T) {
	left := PinKey(filepath.Join(t.TempDir(), "profile-a"), 7)
	right := PinKey(filepath.Join(t.TempDir(), "profile-b"), 7)
	if left == right {
		t.Fatalf("keys are equal: %q", left)
	}
}

func TestForgettingAProfileFindsItsBanksByMarkerFile(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "business")
	credentialsDir := filepath.Join(profile, "credentials")
	if err := os.MkdirAll(credentialsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(credentialsDir, "bank-7")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	notes := filepath.Join(credentialsDir, "notes.txt")
	if err := os.WriteFile(notes, []byte("not a marker"), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	store := NewMemoryCredentialStore()
	if err := store.Write(PinKey(profile, 7), []byte("12345")); err != nil {
		t.Fatalf("write PIN 7: %v", err)
	}
	if err := store.Write(PinKey(profile, 8), []byte("67890")); err != nil {
		t.Fatalf("write PIN 8: %v", err)
	}

	ForgetPinsForProfile(store, profile)

	if store.Contains(PinKey(profile, 7)) {
		t.Fatal("PIN 7 was not forgotten")
	}
	if !store.Contains(PinKey(profile, 8)) {
		t.Fatal("PIN 8 was forgotten")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker still exists: %v", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Fatalf("other files were removed: %v", err)
	}
}

func TestForgettingAProfileWithoutACredentialsFolderIsANoOp(t *testing.T) {
	store := NewMemoryCredentialStore()
	ForgetPinsForProfile(store, t.TempDir())
}

func TestAFailedStoreDeleteKeepsTheMarker(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "business")
	credentialsDir := filepath.Join(profile, "credentials")
	if err := os.MkdirAll(credentialsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := filepath.Join(credentialsDir, "bank-7")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	store := NewMemoryCredentialStore()
	if err := store.Write(PinKey(profile, 7), []byte("12345")); err != nil {
		t.Fatalf("write PIN: %v", err)
	}
	store.SetFailingDeletes(true)

	ForgetPinsForProfile(store, profile)

	if !store.Contains(PinKey(profile, 7)) {
		t.Fatal("PIN was forgotten")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker was removed: %v", err)
	}
}
