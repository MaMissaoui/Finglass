//go:build unix

package programs

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func writeFile(t *testing.T, path string, executable bool) {
	t.Helper()

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	if err := os.Chmod(path, mode); err != nil { // #nosec G302 -- test fixture needs the executable bit.
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func TestFindsAnExecutableRegularFile(t *testing.T) {
	temp := t.TempDir()
	program := filepath.Join(temp, "tesseract")
	writeFile(t, program, true)

	if got, ok := resolveIn("tesseract", []string{temp}); !ok || got != program {
		t.Errorf("resolveIn = %q, %v, want %q, true", got, ok, program)
	}
}

func TestSkipsNonExecutableFilesAndDirectories(t *testing.T) {
	temp := t.TempDir()
	program := filepath.Join(temp, "tesseract")
	writeFile(t, program, false)

	if got, ok := resolveIn("tesseract", []string{temp}); ok {
		t.Errorf("resolveIn = %q, true, want not found", got)
	}

	if err := os.Remove(program); err != nil {
		t.Fatalf("remove %s: %v", program, err)
	}
	if err := os.Mkdir(program, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", program, err)
	}

	if got, ok := resolveIn("tesseract", []string{temp}); ok {
		t.Errorf("resolveIn = %q, true, want not found", got)
	}
}

func TestFollowsASymlinkToAnExecutable(t *testing.T) {
	temp := t.TempDir()
	target := filepath.Join(temp, "real-tesseract")
	writeFile(t, target, true)
	link := filepath.Join(temp, "tesseract")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}

	if got, ok := resolveIn("tesseract", []string{temp}); !ok || got != link {
		t.Errorf("resolveIn = %q, %v, want %q, true", got, ok, link)
	}
}

func TestTheFirstFolderWins(t *testing.T) {
	temp := t.TempDir()
	first := filepath.Join(temp, "first")
	second := filepath.Join(temp, "second")
	if err := os.Mkdir(first, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", first, err)
	}
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", second, err)
	}
	writeFile(t, filepath.Join(first, "tesseract"), true)
	writeFile(t, filepath.Join(second, "tesseract"), true)

	want := filepath.Join(first, "tesseract")
	if got, ok := resolveIn("tesseract", []string{first, second}); !ok || got != want {
		t.Errorf("resolveIn = %q, %v, want %q, true", got, ok, want)
	}
}

func TestRejectsNamesWithASeparatorAndMissingPrograms(t *testing.T) {
	temp := t.TempDir()
	writeFile(t, filepath.Join(temp, "tesseract"), true)

	if got, ok := resolveIn("nested/tesseract", []string{temp}); ok {
		t.Errorf("resolveIn = %q, true, want not found for a name with a separator", got)
	}
	if got, ok := resolveIn("missing", []string{temp}); ok {
		t.Errorf("resolveIn = %q, true, want not found", got)
	}
	if got, ok := resolveIn("missing", nil); ok {
		t.Errorf("resolveIn = %q, true, want not found", got)
	}
}

func TestMacOSFallbacksComeAfterPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS fallback folders only")
	}

	dirs := searchDirsWith("/custom/bin:/usr/bin", true)
	want := []string{
		"/custom/bin",
		"/usr/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/opt/local/bin",
	}
	if !slices.Equal(dirs, want) {
		t.Errorf("searchDirsWith = %v, want %v", dirs, want)
	}
}

func TestRelativePathEntriesAreIgnored(t *testing.T) {
	dirs := searchDirsWith(".:relative/bin::/usr/bin", true)
	if len(dirs) == 0 || dirs[0] != "/usr/bin" {
		t.Fatalf("searchDirsWith = %v, want /usr/bin first", dirs)
	}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			t.Errorf("searchDirsWith includes non-absolute folder %q", dir)
		}
	}
}
