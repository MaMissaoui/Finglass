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

	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatalf("build Everyone DACL: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("read Everyone DACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatalf("set Everyone DACL: %v", err)
	}

	if err := Check(dir); err == nil {
		t.Fatal("Check accepted a folder that allows Everyone")
	}
}
