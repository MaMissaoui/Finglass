package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const unmigratedFixturePassphrase = "fixture passphrase 1"

// copyFindeskFixture copies the committed FinDesk fixture into a test temporary
// folder. Tests only ever touch the copy.
func copyFindeskFixture(t *testing.T) string {
	t.Helper()

	source := filepath.Join("..", "findeskimport", "testdata", "findesk")
	destination := filepath.Join(t.TempDir(), "findesk")
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return destination
}

func unlockWith(t *testing.T, value string) *secret.Passphrase {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte(value), secret.Unlock)
	if err != nil {
		t.Fatalf("NewPassphrase(%q): %v", value, err)
	}
	return passphrase
}

func TestOpenUnmigratedOpensTheFinDeskFixture(t *testing.T) {
	sourceDir := copyFindeskFixture(t)
	path := filepath.Join(sourceDir, "findesk.db")

	opened, err := OpenUnmigrated(path, unlockWith(t, unmigratedFixturePassphrase))
	if err != nil {
		t.Fatalf("OpenUnmigrated: %v", err)
	}
	defer func() {
		_ = opened.Close()
	}()

	var count int64
	err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&count)
	})
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	if count == 0 {
		t.Fatal("the fixture has no tables")
	}
}

func TestOpenUnmigratedReportsWrongPassphrasesWithoutSQLSyntaxErrors(t *testing.T) {
	sourceDir := copyFindeskFixture(t)
	path := filepath.Join(sourceDir, "findesk.db")

	passphrases := []string{
		"totally incorrect passphrase",
		`wrong "quoted" passphrase`,
		`wrong 'single' passphrase`,
		`wrong ? question passphrase`,
		`wrong & ampersand passphrase`,
		`wrong % percent passphrase`,
		`wrong = equals passphrase`,
	}
	for _, value := range passphrases {
		t.Run(value, func(t *testing.T) {
			_, err := OpenUnmigrated(path, unlockWith(t, value))
			if !errors.Is(err, ErrWrongPassphraseOrDamaged) {
				t.Fatalf("OpenUnmigrated = %v, want ErrWrongPassphraseOrDamaged", err)
			}
			if err == nil || strings.Contains(strings.ToLower(err.Error()), "syntax") {
				t.Fatalf("OpenUnmigrated returned a SQL syntax error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), value) {
				t.Fatalf("wrong-passphrase error contains the passphrase: %v", err)
			}
		})
	}
}

func TestOpenUnmigratedLeavesUserVersionAlone(t *testing.T) {
	sourceDir := copyFindeskFixture(t)
	path := filepath.Join(sourceDir, "findesk.db")
	passphrase := unlockWith(t, unmigratedFixturePassphrase)

	readVersion := func() int {
		opened, err := OpenUnmigrated(path, passphrase)
		if err != nil {
			t.Fatalf("OpenUnmigrated: %v", err)
		}
		defer func() {
			_ = opened.Close()
		}()

		var version int
		err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
			return conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
		})
		if err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		return version
	}

	before := readVersion()
	after := readVersion()
	if before != after {
		t.Fatalf("user_version changed from %d to %d", before, after)
	}

	// A sentinel proves that opening never resets the version either.
	setVersion := func() {
		opened, err := OpenUnmigrated(path, passphrase)
		if err != nil {
			t.Fatalf("OpenUnmigrated: %v", err)
		}
		defer func() {
			_ = opened.Close()
		}()
		err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
			_, err := conn.ExecContext(ctx, "PRAGMA user_version = 42")
			return err
		})
		if err != nil {
			t.Fatalf("write user_version: %v", err)
		}
	}
	setVersion()
	if got := readVersion(); got != 42 {
		t.Fatalf("user_version = %d after reopen, want 42", got)
	}
}

func TestOpenUnmigratedLeavesTheFileBytesUnchanged(t *testing.T) {
	sourceDir := copyFindeskFixture(t)
	path := filepath.Join(sourceDir, "findesk.db")

	before, err := os.ReadFile(path) // #nosec G304 -- path is inside the test's temporary copy.
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	opened, err := OpenUnmigrated(path, unlockWith(t, unmigratedFixturePassphrase))
	if err != nil {
		t.Fatalf("OpenUnmigrated: %v", err)
	}
	err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		var count int64
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&count)
	})
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	after, err := os.ReadFile(path) // #nosec G304 -- path is inside the test's temporary copy.
	if err != nil {
		t.Fatalf("read fixture after close: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the database file changed after open and close")
	}

	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Fatalf("open and close left %s behind", suffix)
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", suffix, err)
		}
	}
}
