package findeskimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

type sourceFileState struct {
	hash    [32]byte
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func snapshotSource(t *testing.T, root string) map[string]sourceFileState {
	t.Helper()

	files := make(map[string]sourceFileState)
	rootFS := os.DirFS(root)
	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(rootFS, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[path] = sourceFileState{
			hash:    sha256.Sum256(data),
			size:    info.Size(),
			mode:    info.Mode(),
			modTime: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return files
}

func TestImportNeverWritesTheSelectedFolder(t *testing.T) {
	sourceDir := fixtureCopy(t)
	before := snapshotSource(t, sourceDir)

	checkUnchanged := func(step string) {
		t.Helper()
		after := snapshotSource(t, sourceDir)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed the selected FinDesk folder", step)
		}
	}

	correct := passphrase(fixturePassphrase, secret.Unlock)
	if err := VerifyPassphrase(sourceDir, correct); err != nil {
		t.Fatalf("VerifyPassphrase: %v", err)
	}
	checkUnchanged("VerifyPassphrase")

	target, documents := newTarget(t)
	if _, err := Import(target, documents, sourceDir, correct, offset()); err != nil {
		t.Fatalf("successful Import: %v", err)
	}
	checkUnchanged("a successful import")

	wrongTarget, wrongDocuments := newTarget(t)
	if _, err := Import(
		wrongTarget,
		wrongDocuments,
		sourceDir,
		passphrase("wrong passphrase", secret.Unlock),
		offset(),
	); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong-passphrase Import = %v, want ErrWrongPassphrase", err)
	}
	checkUnchanged("a wrong-passphrase import")

	refusedTarget, refusedDocuments := newTarget(t)
	err := refusedTarget.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EUR')")
		return err
	})
	if err != nil {
		t.Fatalf("seed refused target: %v", err)
	}
	if _, err := Import(refusedTarget, refusedDocuments, sourceDir, correct, offset()); !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("refused Import = %v, want ErrTargetNotEmpty", err)
	}
	checkUnchanged("a refused import")
}

func TestTemporaryCopyIsRemoved(t *testing.T) {
	sourceDir := fixtureCopy(t)
	successTarget, successDocuments := newTarget(t)
	wrongTarget, wrongDocuments := newTarget(t)
	refusedTarget, refusedDocuments := newTarget(t)

	err := refusedTarget.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EUR')")
		return err
	})
	if err != nil {
		t.Fatalf("seed refused target: %v", err)
	}

	temporaryRoot := t.TempDir()
	temporaryDir := filepath.Join(temporaryRoot, "tmp")
	if err := os.MkdirAll(temporaryDir, 0o700); err != nil {
		t.Fatalf("create TMPDIR: %v", err)
	}
	t.Setenv("TMPDIR", temporaryDir)

	checkEmpty := func(step string) {
		t.Helper()
		entries, err := os.ReadDir(temporaryDir)
		if err != nil {
			t.Fatalf("%s: read TMPDIR: %v", step, err)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			t.Fatalf("%s left temporary entries behind: %v", step, names)
		}
	}

	correct := passphrase(fixturePassphrase, secret.Unlock)
	if err := VerifyPassphrase(sourceDir, correct); err != nil {
		t.Fatalf("VerifyPassphrase: %v", err)
	}
	checkEmpty("VerifyPassphrase")

	if _, err := Import(successTarget, successDocuments, sourceDir, correct, offset()); err != nil {
		t.Fatalf("successful Import: %v", err)
	}
	checkEmpty("a successful import")

	if _, err := Import(
		wrongTarget,
		wrongDocuments,
		sourceDir,
		passphrase("wrong passphrase", secret.Unlock),
		offset(),
	); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong-passphrase Import = %v, want ErrWrongPassphrase", err)
	}
	checkEmpty("a wrong-passphrase import")

	if _, err := Import(refusedTarget, refusedDocuments, sourceDir, correct, offset()); !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("refused Import = %v, want ErrTargetNotEmpty", err)
	}
	checkEmpty("a refused import")
}

func TestPassphraseNeverFormatted(t *testing.T) {
	sourceDir := fixtureCopy(t)
	correct := passphrase(fixturePassphrase, secret.Unlock)

	var failures []error

	missingTarget, missingDocuments := newTarget(t)
	if _, err := Import(missingTarget, missingDocuments, t.TempDir(), correct, offset()); err != nil {
		failures = append(failures, err)
	} else {
		t.Fatal("import from a missing folder succeeded")
	}

	wrongTarget, wrongDocuments := newTarget(t)
	if _, err := Import(
		wrongTarget,
		wrongDocuments,
		sourceDir,
		passphrase("wrong passphrase", secret.Unlock),
		offset(),
	); err != nil {
		failures = append(failures, err)
	} else {
		t.Fatal("import with a wrong passphrase succeeded")
	}

	foreign := t.TempDir()
	foreignPath := filepath.Join(foreign, "findesk.db")
	foreignDatabase, err := db.Create(foreignPath, passphrase(fixturePassphrase, secret.New))
	if err != nil {
		t.Fatalf("create foreign database: %v", err)
	}
	_ = foreignDatabase.Close()
	foreignTarget, foreignDocuments := newTarget(t)
	if _, err := Import(foreignTarget, foreignDocuments, foreign, correct, offset()); err != nil {
		failures = append(failures, err)
	} else {
		t.Fatal("import from a foreign database succeeded")
	}

	refusedTarget, refusedDocuments := newTarget(t)
	err = refusedTarget.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "INSERT INTO accounts (currency) VALUES ('EUR')")
		return err
	})
	if err != nil {
		t.Fatalf("seed refused target: %v", err)
	}
	if _, err := Import(refusedTarget, refusedDocuments, sourceDir, correct, offset()); err != nil {
		failures = append(failures, err)
	} else {
		t.Fatal("import into a non-empty target succeeded")
	}

	triggerTarget, triggerDocuments := newTarget(t)
	err = triggerTarget.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			`CREATE TRIGGER fail_audit BEFORE INSERT ON audit_log
			 BEGIN SELECT RAISE(ABORT, 'blocked'); END;`,
		)
		return err
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := Import(triggerTarget, triggerDocuments, sourceDir, correct, offset()); err != nil {
		failures = append(failures, err)
	} else {
		t.Fatal("import with a failing audit trigger succeeded")
	}

	failures = append(failures,
		ErrSourceChanged,
		newIOError(fmt.Errorf("synthetic file error")),
	)

	successTarget, successDocuments := newTarget(t)
	report, err := Import(successTarget, successDocuments, sourceDir, correct, offset())
	if err != nil {
		t.Fatalf("successful Import: %v", err)
	}

	formatted := make([]string, 0)
	for _, failure := range failures {
		formatted = append(
			formatted,
			fmt.Sprintf("%v", failure),
			fmt.Sprintf("%+v", failure),
			fmt.Sprintf("%#v", failure),
		)
	}
	formatted = append(
		formatted,
		fmt.Sprintf("%v", *report),
		fmt.Sprintf("%+v", *report),
		fmt.Sprintf("%#v", *report),
		report.Summary(i18n.En),
	)

	for _, text := range formatted {
		if text == "" {
			t.Error("a formatted error or report is empty")
		}
		if strings.Contains(text, fixturePassphrase) {
			t.Errorf("formatted value leaked the passphrase: %s", text)
		}
	}
}

func TestThemeCodesMatchRust(t *testing.T) {
	if prefs.ThemeKey != "ui.theme" {
		t.Errorf("ThemeKey = %q", prefs.ThemeKey)
	}
	if prefs.LanguageKey != "ui.language" {
		t.Errorf("LanguageKey = %q", prefs.LanguageKey)
	}
	if prefs.SidebarCollapsedKey != "ui.sidebarCollapsed" {
		t.Errorf("SidebarCollapsedKey = %q", prefs.SidebarCollapsedKey)
	}
	codes := []struct {
		theme prefs.ThemePref
		code  string
	}{
		{prefs.ThemeSystem, "system"},
		{prefs.ThemeLight, "light"},
		{prefs.ThemeDark, "dark"},
	}
	for _, entry := range codes {
		if entry.theme.Code() != entry.code {
			t.Errorf("theme code = %q, want %q", entry.theme.Code(), entry.code)
		}
	}
}
