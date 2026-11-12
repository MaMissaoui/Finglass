//go:build unix

package profiles

import (
	"os"
	"testing"
)

func TestTheRegistryFileIsPrivate(t *testing.T) {
	p := testPaths(t)

	if _, err := Create(p, "Business"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	info, err := os.Stat(registryPath(p))
	if err != nil {
		t.Fatalf("Stat registry: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("registry mode = %#o, want 0600", mode)
	}
}
