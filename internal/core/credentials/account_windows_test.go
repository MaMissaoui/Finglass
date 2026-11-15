//go:build windows

package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAccountForUsesWindowsPaths pins the Windows account keys. Before G11 a
// `C:\...` path was treated as relative and joined to the working directory.
func TestAccountForUsesWindowsPaths(t *testing.T) {
	for _, test := range []struct {
		profile string
		bankID  int64
		account string
	}{
		{`C:\Users\x\.finglass\profiles\default`, 7, `C:\Users\x\.finglass\profiles\default#bank-7`},
		{`C:\Users\x\.finglass\profiles\default\`, 7, `C:\Users\x\.finglass\profiles\default#bank-7`},
		{`c:\Users\x\..\x\.finglass\profiles\default`, 3, `c:\Users\x\.finglass\profiles\default#bank-3`},
		{`\\server\share\profiles\default`, 42, `\\server\share\profiles\default#bank-42`},
		// A POSIX-form path keeps the POSIX form, as the golden files expect.
		{"/data/profile", 7, "/data/profile#bank-7"},
	} {
		if got := AccountFor(test.profile, test.bankID); got != test.account {
			t.Errorf("AccountFor(%q, %d) = %q, want %q", test.profile, test.bankID, got, test.account)
		}
	}

	t.Chdir(t.TempDir())
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	want := filepath.Join(current, "profiles", "default") + "#bank-7"
	if got := AccountFor(`profiles\default`, 7); got != want {
		t.Errorf("AccountFor(relative) = %q, want %q", got, want)
	}
}
