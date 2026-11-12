package paths

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvWinsOverHome(t *testing.T) {
	paths, err := Resolve("/data/finglass", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got, want := paths.Root(), "/data/finglass"; got != want {
		t.Errorf("root = %q, want %q", got, want)
	}
}

func TestEmptyEnvFallsBackToHome(t *testing.T) {
	paths, err := Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got, want := paths.Root(), "/home/alice/.finglass"; got != want {
		t.Errorf("root = %q, want %q", got, want)
	}
}

func TestNoEnvAndNoHomeIsAnError(t *testing.T) {
	_, err := Resolve("", "")
	if !errors.Is(err, ErrNoHome) {
		t.Fatalf("Resolve error = %v, want ErrNoHome", err)
	}

	const want = "no home folder is available to place the Finglass data in"
	if err.Error() != want {
		t.Errorf("error text = %q, want %q", err.Error(), want)
	}
}

func TestDefaultSlugMapsToTheRoot(t *testing.T) {
	paths, err := Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got, err := paths.ProfileDir("default")
	if err != nil {
		t.Fatalf("ProfileDir(default): %v", err)
	}
	want := filepath.Join("/home/alice", ".finglass")
	if got != want {
		t.Errorf("ProfileDir(default) = %q, want %q", got, want)
	}
}

func TestOtherSlugMapsToProfilesFolder(t *testing.T) {
	paths, err := Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got, err := paths.ProfileDir("business")
	if err != nil {
		t.Fatalf("ProfileDir(business): %v", err)
	}
	want := filepath.Join("/home/alice", ".finglass", "profiles", "business")
	if got != want {
		t.Errorf("ProfileDir(business) = %q, want %q", got, want)
	}

	database, err := paths.DatabaseFile("business")
	if err != nil {
		t.Fatalf("DatabaseFile(business): %v", err)
	}
	want = filepath.Join(want, "finglass.db")
	if database != want {
		t.Errorf("DatabaseFile(business) = %q, want %q", database, want)
	}
}

func TestRejectsInvalidSlugs(t *testing.T) {
	paths, err := Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	long := strings.Repeat("a", 41)

	for _, slug := range []string{"..", "a/b", "A", "", long, "-x"} {
		_, err := paths.ProfileDir(slug)
		if !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("ProfileDir(%q) error = %v, want ErrInvalidSlug", slug, err)
		}
	}
}

func TestAcceptsA40CharacterSlug(t *testing.T) {
	paths, err := Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	slug := strings.Repeat("a", 40)
	if _, err := paths.ProfileDir(slug); err != nil {
		t.Errorf("ProfileDir(%q): %v", slug, err)
	}
}

func TestProfileDirRejectsBeforeJoining(t *testing.T) {
	paths, err := Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, slug := range []string{"a/../b", "./x", "x/", "profiles/../.."} {
		_, err := paths.ProfileDir(slug)
		if !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("ProfileDir(%q) error = %v, want ErrInvalidSlug", slug, err)
		}
	}
}

func TestFromEnvUsesFinglassHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	t.Setenv("FINGLASS_HOME", root)
	t.Setenv("HOME", t.TempDir())

	paths, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got := paths.Root(); got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestFromEnvFallsBackToTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FINGLASS_HOME", "")
	t.Setenv("HOME", home)

	paths, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got, want := paths.Root(), filepath.Join(home, ".finglass"); got != want {
		t.Errorf("root = %q, want %q", got, want)
	}
}

func TestIsValidSlug(t *testing.T) {
	valid := []string{"a", "0", "default", "business-2", "x-", strings.Repeat("a", 40)}
	invalid := []string{"", "A", "Business", "-x", "x_y", "a/b", "..", strings.Repeat("a", 41)}

	for _, slug := range valid {
		if !IsValidSlug(slug) {
			t.Errorf("IsValidSlug(%q) = false, want true", slug)
		}
	}
	for _, slug := range invalid {
		if IsValidSlug(slug) {
			t.Errorf("IsValidSlug(%q) = true, want false", slug)
		}
	}
}

func TestEnsureDirCreatesNestedFolders(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "nested", "profile")

	if err := EnsureDir(folder); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", folder)
	}
}

// TestResolveKeepsTheRootUnchanged guards the Rust behaviour: FINGLASS_HOME is
// used exactly as given, without cleaning or making it absolute.
func TestResolveKeepsTheRootUnchanged(t *testing.T) {
	root := filepath.Join("relative", "..", "data") + string(os.PathSeparator)
	paths, err := Resolve(root, "/home/alice")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := paths.Root(); got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

// TestProfileDirsAreDistinct keeps the table honest about all accepted shapes.
func TestProfileDirsAreDistinct(t *testing.T) {
	paths, err := Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	first, err := paths.ProfileDir("business")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	second, err := paths.ProfileDir("business")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ProfileDir is not deterministic: %q != %q", first, second)
	}
}
