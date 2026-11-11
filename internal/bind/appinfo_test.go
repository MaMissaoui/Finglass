package bind

import (
	"runtime"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	info, err := (&AppInfo{}).Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.Name != "Finglass" {
		t.Errorf("Name = %q, want %q", info.Name, "Finglass")
	}
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}

func TestGreet(t *testing.T) {
	service := &AppInfo{}

	got, err := service.Greet("Ada")
	if err != nil {
		t.Fatalf("Greet(Ada): %v", err)
	}
	if got != "Hello, Ada" {
		t.Errorf("Greet(Ada) = %q, want %q", got, "Hello, Ada")
	}

	got, err = service.Greet("  Ada Lovelace  ")
	if err != nil {
		t.Fatalf("Greet with surrounding space: %v", err)
	}
	if got != "Hello, Ada Lovelace" {
		t.Errorf("Greet with surrounding space = %q, want %q", got, "Hello, Ada Lovelace")
	}
}

func TestGreetRejectsEmptyName(t *testing.T) {
	service := &AppInfo{}

	for _, name := range []string{"", "   ", "\t\n"} {
		got, err := service.Greet(name)
		if err == nil {
			t.Errorf("Greet(%q) returned %q without an error", name, got)
			continue
		}
		if !strings.Contains(err.Error(), "name") {
			t.Errorf("Greet(%q) error = %q, want a name error", name, err)
		}
	}
}
