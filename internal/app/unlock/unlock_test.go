package unlock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	testSlug          = "default"
	testPassphrase    = "correct horse battery staple"
	testNewPassphrase = "another very long passphrase"
)

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	return resolved
}

func directPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(value), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func createDatabase(t *testing.T, path string) *db.Database {
	t.Helper()
	created, err := db.Create(path, directPassphrase(t, testPassphrase))
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	return created
}

func TestModeDependsOnTheDatabaseFile(t *testing.T) {
	resolved := testPaths(t)
	file, err := resolved.DatabaseFile(testSlug)
	if err != nil {
		t.Fatalf("database file: %v", err)
	}

	if got := ModeFor(file); got != ModeCreate {
		t.Fatalf("mode before create = %v, want %v", got, ModeCreate)
	}

	database := createDatabase(t, file)
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if got := ModeFor(file); got != ModeUnlock {
		t.Fatalf("mode after create = %v, want %v", got, ModeUnlock)
	}
}

func TestValidateNewReportsEveryError(t *testing.T) {
	if _, err := ValidateNew([]byte("short"), []byte("short"), true); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short = %v, want %v", err, ErrTooShort)
	}
	if _, err := ValidateNew(
		[]byte(" correct horse battery "),
		[]byte(" correct horse battery "),
		true,
	); !errors.Is(err, ErrSpaces) {
		t.Fatalf("spaces = %v, want %v", err, ErrSpaces)
	}
	if _, err := ValidateNew(
		[]byte(testPassphrase),
		[]byte(testNewPassphrase),
		true,
	); !errors.Is(err, ErrMismatch) {
		t.Fatalf("mismatch = %v, want %v", err, ErrMismatch)
	}
	if _, err := ValidateNew(
		[]byte(testPassphrase),
		[]byte(testPassphrase),
		false,
	); !errors.Is(err, ErrNotAcknowledged) {
		t.Fatalf("not acknowledged = %v, want %v", err, ErrNotAcknowledged)
	}

	created, err := ValidateNew([]byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("valid passphrase = %v", err)
	}
	created.Wipe()
}

func TestCreateThenUnlockWorksAndAWrongPassphraseIsReported(t *testing.T) {
	resolved := testPaths(t)

	database, err := Create(resolved, testSlug, []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close created database: %v", err)
	}

	opened, err := Unlock(resolved, testSlug, []byte(testPassphrase))
	if err != nil {
		t.Fatalf("unlock = %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close unlocked database: %v", err)
	}

	if _, err := Unlock(resolved, testSlug, []byte("not the passphrase")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase = %v, want %v", err, ErrWrongPassphrase)
	}
}

func TestChangeThenReopenWithTheNewPassphrase(t *testing.T) {
	resolved := testPaths(t)

	database, err := Create(resolved, testSlug, []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	if err := Change(database, []byte(testNewPassphrase), []byte(testNewPassphrase), true); err != nil {
		t.Fatalf("change = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if _, err := Unlock(resolved, testSlug, []byte(testPassphrase)); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("old passphrase = %v, want %v", err, ErrWrongPassphrase)
	}
	opened, err := Unlock(resolved, testSlug, []byte(testNewPassphrase))
	if err != nil {
		t.Fatalf("unlock with new passphrase = %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func TestAuditEntriesExistAndNeverContainAPassphrase(t *testing.T) {
	resolved := testPaths(t)

	database, err := Create(resolved, testSlug, []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Change(database, []byte(testNewPassphrase), []byte(testNewPassphrase), true); err != nil {
		t.Fatalf("change = %v", err)
	}

	entries, err := audit.Recent(context.Background(), database, 10)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	if !contains(actions, "DATABASE_CREATED") {
		t.Fatalf("actions = %v, want DATABASE_CREATED", actions)
	}
	if !contains(actions, "DATABASE_PASSPHRASE_CHANGED") {
		t.Fatalf("actions = %v, want DATABASE_PASSPHRASE_CHANGED", actions)
	}

	for _, entry := range entries {
		text := entry.Action + " / " + entry.Details + " / " + string(entry.Actor)
		if strings.Contains(text, testPassphrase) {
			t.Fatal("audit entry leaked the passphrase")
		}
		if strings.Contains(text, testNewPassphrase) {
			t.Fatal("audit entry leaked the new passphrase")
		}
	}
}

func TestCreateMakesTheProfileFolder(t *testing.T) {
	resolved := testPaths(t)

	database, err := Create(resolved, "business", []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	profileDir, err := resolved.ProfileDir("business")
	if err != nil {
		t.Fatalf("profile dir: %v", err)
	}
	info, err := os.Stat(profileDir)
	if err != nil {
		t.Fatalf("stat profile dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", profileDir)
	}
}

func TestDecomposedRepeatMatchesTheComposedPassphrase(t *testing.T) {
	composed := "Café au lait long enough"
	decomposed := "Cafe\u0301 au lait long enough"

	created, err := ValidateNew([]byte(composed), []byte(decomposed), true)
	if err != nil {
		t.Fatalf("validate decomposed repeat = %v", err)
	}
	created.Wipe()
}

func TestFormattingNeverRevealsAPassphrase(t *testing.T) {
	errorsToCheck := []error{
		ErrWrongPassphrase,
		ErrTooShort,
		ErrSpaces,
		ErrMismatch,
		ErrNotAcknowledged,
		ErrNotEncrypted,
		ErrTooNew,
		ErrFailed,
	}
	for _, err := range errorsToCheck {
		text := fmt.Sprintf("%v %+v %#v", err, err, err)
		if strings.Contains(text, testPassphrase) || strings.Contains(text, testNewPassphrase) {
			t.Fatalf("formatting %v leaked a passphrase", err)
		}
	}

	created, err := ValidateNew([]byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("validate = %v", err)
	}
	text := fmt.Sprintf("%v %+v %#v", created, created, created)
	created.Wipe()
	if strings.Contains(text, testPassphrase) {
		t.Fatal("formatting the returned passphrase leaked it")
	}

	if _, err := ValidateNew([]byte("hidden"), []byte("hidden"), true); err != nil {
		text := fmt.Sprintf("%v %+v %#v", err, err, err)
		if strings.Contains(text, "hidden") {
			t.Fatalf("formatting the validation error leaked the input: %s", text)
		}
	}
}

func TestAuditTextsAreLiterals(t *testing.T) {
	resolved := testPaths(t)

	database, err := Create(resolved, testSlug, []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	entries, err := audit.Recent(context.Background(), database, 10)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries after create = %d, want 1", len(entries))
	}
	if entries[0].Action != "DATABASE_CREATED" {
		t.Fatalf("create action = %q", entries[0].Action)
	}
	if entries[0].Details != "New encrypted database" {
		t.Fatalf("create details = %q", entries[0].Details)
	}
	if entries[0].Actor != audit.ActorUser {
		t.Fatalf("create actor = %q", entries[0].Actor)
	}

	if err := Change(database, []byte(testNewPassphrase), []byte(testNewPassphrase), true); err != nil {
		t.Fatalf("change = %v", err)
	}
	entries, err = audit.Recent(context.Background(), database, 10)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries after change = %d, want 2", len(entries))
	}
	if entries[0].Action != "DATABASE_PASSPHRASE_CHANGED" {
		t.Fatalf("change action = %q", entries[0].Action)
	}
	if entries[0].Details != "Database passphrase changed" {
		t.Fatalf("change details = %q", entries[0].Details)
	}
	if entries[0].Actor != audit.ActorUser {
		t.Fatalf("change actor = %q", entries[0].Actor)
	}
}

func TestUnlockMapsEveryDatabaseError(t *testing.T) {
	t.Run("not encrypted", func(t *testing.T) {
		resolved := testPaths(t)
		file, err := resolved.DatabaseFile(testSlug)
		if err != nil {
			t.Fatalf("database file: %v", err)
		}
		contents := append([]byte("SQLite format 3\x00"), make([]byte, 100)...)
		if err := os.WriteFile(file, contents, 0o600); err != nil {
			t.Fatalf("write plain database: %v", err)
		}
		if _, err := Unlock(resolved, testSlug, []byte(testPassphrase)); !errors.Is(err, ErrNotEncrypted) {
			t.Fatalf("plain database = %v, want %v", err, ErrNotEncrypted)
		}
	})

	t.Run("too new", func(t *testing.T) {
		resolved := testPaths(t)
		file, err := resolved.DatabaseFile(testSlug)
		if err != nil {
			t.Fatalf("database file: %v", err)
		}
		database := createDatabase(t, file)
		if err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
			_, err := conn.ExecContext(ctx, "PRAGMA user_version = 99")
			return err
		}); err != nil {
			t.Fatalf("set user_version: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		if _, err := Unlock(resolved, testSlug, []byte(testPassphrase)); !errors.Is(err, ErrTooNew) {
			t.Fatalf("too-new database = %v, want %v", err, ErrTooNew)
		}
	})

	t.Run("failed path", func(t *testing.T) {
		resolved := testPaths(t)
		if _, err := Unlock(resolved, "Not A Slug", []byte(testPassphrase)); !errors.Is(err, ErrFailed) {
			t.Fatalf("invalid slug = %v, want %v", err, ErrFailed)
		}
	})
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
