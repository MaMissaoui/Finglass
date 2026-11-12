package banksync

import (
	"path/filepath"
	"testing"
)

func TestTheServiceIsTheBundleIdentifier(t *testing.T) {
	if KeychainService != "io.github.mamissaoui.finglass" {
		t.Fatalf("service = %q, want %q", KeychainService, "io.github.mamissaoui.finglass")
	}
}

func TestTheAccountIsTheProfileScopedPinKey(t *testing.T) {
	profileA := filepath.Join(t.TempDir(), "profile-a")
	profileB := filepath.Join(t.TempDir(), "profile-b")

	for _, testCase := range []struct {
		profile string
		bank    int64
	}{
		{profileA, 7},
		{profileA, 8},
		{profileB, 7},
		{profileB, 8},
	} {
		if got, want := AccountFor(testCase.profile, testCase.bank), PinKey(testCase.profile, testCase.bank); got != want {
			t.Fatalf("AccountFor(%q, %d) = %q, want %q", testCase.profile, testCase.bank, got, want)
		}
	}

	if AccountFor(profileA, 7) == AccountFor(profileA, 8) {
		t.Fatal("two bank ids share an account")
	}
	if AccountFor(profileA, 7) == AccountFor(profileB, 7) {
		t.Fatal("two profiles share an account")
	}
	if AccountFor(profileA, 8) == AccountFor(profileB, 8) {
		t.Fatal("two profiles share an account")
	}
}

func TestTheNoneOverrideYieldsAnUnavailableStore(t *testing.T) {
	store := SystemCredentialStoreWith(stringPointer("none"))
	// On macOS this must be the no-op store and never the Keychain branch; only
	// the returned type is asserted here.
	if _, ok := store.(NoCredentialStore); !ok {
		t.Fatalf("store type = %T, want banksync.NoCredentialStore", store)
	}
}
