//go:build windows

package privatefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSecureDirAndSecureFileGiveAPrivateACL(t *testing.T) {
	dir := t.TempDir()
	before := filepath.Join(dir, "before.txt")
	if err := os.WriteFile(before, []byte("before"), 0o600); err != nil {
		t.Fatalf("write file before SecureDir: %v", err)
	}

	if err := SecureDir(dir); err != nil {
		t.Fatalf("SecureDir: %v", err)
	}
	if err := Check(dir); err != nil {
		t.Fatalf("Check on the secured folder: %v", err)
	}

	// A file created before the call had the old inherited ACEs. The protected
	// DACL re-inherits to it because it has no explicit ACE of its own.
	if err := Check(before); err != nil {
		t.Fatalf("Check on a file created before SecureDir: %v", err)
	}

	after := filepath.Join(dir, "after.txt")
	if err := os.WriteFile(after, []byte("after"), 0o600); err != nil {
		t.Fatalf("write file after SecureDir: %v", err)
	}
	if err := Check(after); err != nil {
		t.Fatalf("Check on a file created after SecureDir: %v", err)
	}

	explicit := filepath.Join(dir, "explicit.txt")
	if err := os.WriteFile(explicit, []byte("explicit"), 0o600); err != nil {
		t.Fatalf("write explicit file: %v", err)
	}
	if err := SecureFile(explicit); err != nil {
		t.Fatalf("SecureFile: %v", err)
	}
	if err := Check(explicit); err != nil {
		t.Fatalf("Check on a SecureFile file: %v", err)
	}
}

// TestSecuredFolderSDDLShowsItsRealForm logs how Windows prints the secured
// DACL, so a CI failure of Check names the form rather than leaving it to a
// guess (the SID may print as an alias such as LA, SY or BA), and asserts that
// Check accepts the folder SecureDir just secured.
func TestSecuredFolderSDDLShowsItsRealForm(t *testing.T) {
	dir := t.TempDir()
	if err := SecureDir(dir); err != nil {
		t.Fatalf("SecureDir: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read secured folder security descriptor: %v", err)
	}
	t.Logf("secured folder SDDL: %s", sd.String())

	if err := Check(dir); err != nil {
		t.Fatalf("Check on the secured folder: %v", err)
	}
}

// TestPrivateDACLMatcherComparesSIDs pins the comparison the fix needs: an ACE
// SID is accepted by identity, so an alias and its full SID are the same
// principal, and neither Everyone nor Users is allowed.
func TestPrivateDACLMatcherComparesSIDs(t *testing.T) {
	allowed, err := privateDACLMatcher()
	if err != nil {
		t.Fatalf("private DACL matcher: %v", err)
	}

	user, err := currentUserSID()
	if err != nil {
		t.Fatalf("current user SID: %v", err)
	}

	tests := []struct {
		name string
		sid  string
		want bool
	}{
		{name: "current user", sid: user.String(), want: true},
		{name: "SYSTEM alias", sid: "SY", want: true},
		{name: "SYSTEM full SID", sid: "S-1-5-18", want: true},
		{name: "Administrators alias", sid: "BA", want: true},
		{name: "Administrators full SID", sid: "S-1-5-32-544", want: true},
		{name: "Everyone alias", sid: "WD"},
		{name: "Everyone full SID", sid: "S-1-1-0"},
		{name: "Users alias", sid: "BU"},
		{name: "Authenticated Users full SID", sid: "S-1-5-11"},
		{name: "text that is not a SID", sid: "not-a-sid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowed(test.sid); got != test.want {
				t.Fatalf("allowed(%q) = %v, want %v", test.sid, got, test.want)
			}
		})
	}
}

func TestCheckReportsAFreshTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.txt")
	if err := os.WriteFile(path, []byte("fresh"), 0o600); err != nil {
		t.Fatalf("write fresh file: %v", err)
	}

	// The temporary folder may already be owner-only, so record the result
	// instead of asserting a particular one.
	t.Logf("Check on a fresh temporary file: %v", Check(path))
}

func TestCheckFailsWhenEveryoneIsAllowed(t *testing.T) {
	dir := t.TempDir()
	setProtectedDACL(t, dir, "D:P(A;OICI;FA;;;WD)")

	if err := Check(dir); err == nil {
		t.Fatal("Check accepted a folder that allows Everyone")
	}
}

func TestSecureDirReplacesAnInheritedEveryoneDACL(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("make parent: %v", err)
	}
	setProtectedDACL(t, parent, everyoneAndUserSDDL(t))

	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatalf("make child: %v", err)
	}
	file := filepath.Join(child, "file.txt")
	if err := os.WriteFile(file, []byte("child"), 0o600); err != nil {
		t.Fatalf("write child file: %v", err)
	}

	if err := Check(child); err == nil {
		t.Fatal("Check accepted a child that inherits Everyone")
	}
	if err := Check(file); err == nil {
		t.Fatal("Check accepted a file that inherits Everyone")
	}

	if err := SecureDir(child); err != nil {
		t.Fatalf("SecureDir: %v", err)
	}
	if err := Check(child); err != nil {
		t.Fatalf("Check on the secured child: %v", err)
	}
	if err := Check(file); err != nil {
		t.Fatalf("Check on a file in the secured child: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(child, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read child security descriptor: %v", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatalf("read child control flags: %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("the secured child's DACL is not protected")
	}
}

func TestSecureFileReplacesAnInheritedEveryoneDACL(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("make parent: %v", err)
	}
	setProtectedDACL(t, parent, everyoneAndUserSDDL(t))

	file := filepath.Join(parent, "file.txt")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := Check(file); err == nil {
		t.Fatal("Check accepted a file that inherits Everyone")
	}

	if err := SecureFile(file); err != nil {
		t.Fatalf("SecureFile: %v", err)
	}
	if err := Check(file); err != nil {
		t.Fatalf("Check on the secured file: %v", err)
	}
}

// everyoneAndUserSDDL builds a protected, inheritable DACL that allows Everyone
// and the current user, the way a broad parent folder grants access.
func everyoneAndUserSDDL(t *testing.T) string {
	t.Helper()
	user, err := currentUserSID()
	if err != nil {
		t.Fatalf("current user SID: %v", err)
	}
	return "D:P(A;OICI;FA;;;WD)(A;OICI;FA;;;" + user.String() + ")"
}

func setProtectedDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("build DACL: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("read DACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatalf("set DACL: %v", err)
	}
}
