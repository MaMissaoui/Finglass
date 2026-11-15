//go:build !windows

package credentials

import (
	"os"
	"testing"
)

// TestAccountForKeepsTheExactDarwinAndLinuxStrings pins today's account keys.
// PINs saved by 0.4.3/0.4.4 must still be found, so these strings may never
// change on macOS or Linux.
func TestAccountForKeepsTheExactDarwinAndLinuxStrings(t *testing.T) {
	for _, test := range []struct {
		profile string
		bankID  int64
		account string
	}{
		{"/Users/x/.finglass/profiles/default", 7, "/Users/x/.finglass/profiles/default#bank-7"},
		{"/home/x/.finglass/profiles/default", 42, "/home/x/.finglass/profiles/default#bank-42"},
		{"/Users/x/.finglass/profiles/default/", 9, "/Users/x/.finglass/profiles/default/#bank-9"},
	} {
		if got := AccountFor(test.profile, test.bankID); got != test.account {
			t.Errorf("AccountFor(%q, %d) = %q, want %q", test.profile, test.bankID, got, test.account)
		}
	}

	directory := t.TempDir()
	t.Chdir(directory)
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// A relative path starts at the working directory, exactly as before.
	if got, want := AccountFor("profiles/default", 7), current+"/profiles/default#bank-7"; got != want {
		t.Errorf("AccountFor(relative) = %q, want %q", got, want)
	}
	if got, want := AccountFor("./profiles/./default", 7), current+"/profiles/default#bank-7"; got != want {
		t.Errorf("AccountFor(dotted relative) = %q, want %q", got, want)
	}
}
