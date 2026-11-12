package migration

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

var scenarioNames = []string{"success", "wrong passphrase", "target not empty", "failed move"}

type fileState struct {
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	Hash    [32]byte
}

type fileSnapshot map[string]fileState

func snapshot(t *testing.T, root string) fileSnapshot {
	t.Helper()
	result := make(fileSnapshot)
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.IsDir() {
				walk(path)
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(path) // #nosec G304 -- path is inside the test's FinDesk copy.
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatalf("relative %s: %v", path, err)
			}
			result[relative] = fileState{
				Size:    info.Size(),
				Mode:    info.Mode(),
				ModTime: info.ModTime(),
				Hash:    sha256Sum(append([]byte(relative), data...)),
			}
		}
	}
	walk(root)
	return result
}

func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// runScenario prepares a FinDesk copy and a Finglass target, snapshots the
// FinDesk copy, runs one migration case and snapshots it again.
func runScenario(t *testing.T, name string) (paths.AppPaths, string, fileSnapshot, fileSnapshot) {
	t.Helper()

	root := fixtureCopy(t)
	target := testPaths(t)
	install := Discover(root)
	if install == nil {
		t.Fatal("no install found")
	}
	profile := &install.Profiles[0]

	switch name {
	case "failed move":
		writeNamedProfile(t, root, "work")
		if err := os.WriteFile(
			filepath.Join(root, "profiles.properties"),
			[]byte("name.default=Personal\nname.work=Work\nactive=work\n"),
			0o600,
		); err != nil {
			t.Fatalf("write properties: %v", err)
		}
		install = Discover(root)
		profile = nil
		for index := range install.Profiles {
			if install.Profiles[index].Slug != nil && *install.Profiles[index].Slug == "work" {
				profile = &install.Profiles[index]
				break
			}
		}
		if profile == nil {
			t.Fatal("work profile not found")
		}
	case "target not empty":
		documents := filepath.Join(target.Root(), "documents")
		if err := os.MkdirAll(documents, 0o700); err != nil {
			t.Fatalf("create documents: %v", err)
		}
		if err := os.WriteFile(filepath.Join(documents, "existing.pdf"), []byte("keep me"), 0o600); err != nil {
			t.Fatalf("write existing document: %v", err)
		}
	}

	before := snapshot(t, root)

	switch name {
	case "success":
		if _, err := MigrateProfile(target, profile, findeskPassphrase(t), targetNewPassphrase(t), offset()); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	case "wrong passphrase":
		wrong, err := secret.NewPassphrase([]byte("not the fixture passphrase"), secret.Unlock)
		if err != nil {
			t.Fatalf("new wrong passphrase: %v", err)
		}
		defer wrong.Wipe()
		if _, err := MigrateProfile(target, profile, wrong, targetNewPassphrase(t), offset()); !errors.Is(err, findeskimport.ErrWrongPassphrase) {
			t.Fatalf("error = %v, want a wrong passphrase", err)
		}
	case "target not empty":
		_, err := MigrateProfile(target, profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
		var migrationError *MigrationError
		if !errors.As(err, &migrationError) || migrationError.Kind != MigrationTargetNotEmpty {
			t.Fatalf("error = %v, want target not empty", err)
		}
	case "failed move":
		failNextMoveBeforeDatabase()
		_, err := MigrateProfile(target, profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
		var migrationError *MigrationError
		if !errors.As(err, &migrationError) || migrationError.Kind != MigrationIO {
			t.Fatalf("error = %v, want an I/O error", err)
		}
	default:
		t.Fatalf("unknown scenario %q", name)
	}

	after := snapshot(t, root)
	return target, root, before, after
}

func TestMigrationNeverWritesTheFinDeskRoot(t *testing.T) {
	for _, name := range scenarioNames {
		t.Run(name, func(t *testing.T) {
			_, _, before, after := runScenario(t, name)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("FinDesk root changed\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

func TestStagingIsAlwaysRemoved(t *testing.T) {
	for _, name := range scenarioNames {
		t.Run(name, func(t *testing.T) {
			target, _, _, _ := runScenario(t, name)
			staging := filepath.Join(target.Root(), StagingDir)
			if _, err := os.Stat(staging); !os.IsNotExist(err) {
				t.Errorf("staging still exists: %v", err)
			}
			entries, err := os.ReadDir(target.Root())
			if err != nil {
				t.Fatalf("read root: %v", err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), StagingDir) {
					t.Errorf("staging entry remains: %s", entry.Name())
				}
			}
		})
	}
}

func TestStagingIsPrivate(t *testing.T) {
	target := testPaths(t)
	staging, err := createStaging(target)
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(target.Root(), StagingDir)) })

	for _, path := range []string{staging, filepath.Dir(staging)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if mode := info.Mode().Perm(); mode != 0o700 {
			t.Errorf("%s mode = %#o, want 0700", path, mode)
		}
	}
}

func TestPassphrasesNeverFormatted(t *testing.T) {
	values := []any{
		&MigrationError{Kind: MigrationTargetNotEmpty},
		&MigrationError{Kind: MigrationFinDesk, Err: findeskimport.ErrWrongPassphrase},
		&MigrationError{Kind: MigrationProfile, Err: profiles.ErrEmptyName},
		&MigrationError{Kind: MigrationDatabase, Err: db.ErrUnavailable},
		&MigrationError{Kind: MigrationIO, Err: errors.New("io")},
		FinDeskInstall{Root: "/findesk", Profiles: []FinDeskProfile{{Name: "Work"}}},
		FinDeskProfile{Slug: stringPointer("work"), Name: "Work", DataDir: "/findesk"},
		MigratedProfile{FinglassSlug: stringPointer("work")},
		findesk.NewFindeskImportState(i18n.En),
	}

	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, fixturePassphrase) || strings.Contains(text, targetPassphrase) {
				t.Errorf("%s of %T contains a passphrase: %s", format, value, text)
			}
		}
	}
}

func TestSharedNamesAreLiterals(t *testing.T) {
	if MarkerFile != "findesk-migration" {
		t.Errorf("MarkerFile = %q", MarkerFile)
	}
	if StagingDir != ".findesk-migration-staging" {
		t.Errorf("StagingDir = %q", StagingDir)
	}

	root := t.TempDir()
	first := markerTempPath(root)
	second := markerTempPath(root)
	prefix := fmt.Sprintf(".findesk-migration-%d-", os.Getpid())
	firstCounter := markerCounterFrom(t, filepath.Base(first), prefix)
	secondCounter := markerCounterFrom(t, filepath.Base(second), prefix)
	if secondCounter != firstCounter+1 {
		t.Errorf("counters = %d then %d, want consecutive", firstCounter, secondCounter)
	}

	target := testPaths(t)
	if err := WriteMarker(target, true, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	entries, err := os.ReadDir(target.Root())
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".findesk-migration-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temporary marker remains: %s", entry.Name())
		}
	}
}

func markerCounterFrom(t *testing.T, base, prefix string) uint64 {
	t.Helper()
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, ".tmp") {
		t.Fatalf("temporary marker name = %q, want %s<counter>.tmp", base, prefix)
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".tmp")
	counter, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		t.Fatalf("marker counter %q: %v", digits, err)
	}
	return counter
}
