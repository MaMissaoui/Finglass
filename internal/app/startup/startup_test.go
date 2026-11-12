package startup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const pass = "correct horse battery staple"

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	return resolved
}

func newPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(value), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func database(t *testing.T, appPaths paths.AppPaths, slug string) *db.Database {
	t.Helper()
	opened, err := unlock.Create(appPaths, slug, []byte(pass), []byte(pass), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	return opened
}

func account(t *testing.T, opened *db.Database) model.AccountID {
	t.Helper()
	name := "Account"
	id, err := accounts.Create(context.Background(), opened, model.NewAccount{
		Name:        &name,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func addTransaction(t *testing.T, opened *db.Database, accountID model.AccountID, partner, hash string) {
	t.Helper()
	date, ok := civil.New(2026, time.January, 1)
	if !ok {
		t.Fatal("invalid test date")
	}
	_, err := transactions.InsertImported(context.Background(), opened, accountID, []model.NewTransaction{{
		BookingDate: date,
		Amount:      money.Money{Minor: -1_000, Currency: money.EUR},
		PartnerName: &partner,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
}

func partnerNames(t *testing.T, opened *db.Database) []string {
	t.Helper()
	names := make([]string, 0)
	err := opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT partner_name FROM transactions ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var name *string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if name != nil {
				names = append(names, *name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read partner names: %v", err)
	}
	return names
}

func preRestoreFolders(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	folders := make([]string, 0)
	for _, entry := range entries {
		if len(entry.Name()) >= len(backup.PreRestorePrefix) &&
			entry.Name()[:len(backup.PreRestorePrefix)] == backup.PreRestorePrefix {
			folders = append(folders, filepath.Join(dir, entry.Name()))
		}
	}
	return folders
}

func folderWithDocument(t *testing.T, dir string) {
	t.Helper()
	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("create documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "receipt.pdf"), []byte("unencrypted"), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestAFreshRootHasNoLeftovers(t *testing.T) {
	appPaths := testPaths(t)

	if got := Leftovers(appPaths); len(got) != 0 {
		t.Fatalf("leftovers = %v, want none", got)
	}
}

func TestPreRestoreFoldersWithDocumentsInTheRootAndAProfileAreListed(t *testing.T) {
	appPaths := testPaths(t)
	folderWithDocument(t, filepath.Join(appPaths.Root(), "pre-restore-20260101-000000"))

	profile, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	slug := profile.Slug
	folderWithDocument(t, filepath.Join(profile.Dir, "pre-restore-20260102-000000"))

	want := []Leftover{
		{Kind: LeftoverPreRestore, Path: "pre-restore-20260101-000000"},
		{Kind: LeftoverPreRestore, Path: "profiles/" + slug + "/pre-restore-20260102-000000"},
	}
	got := Leftovers(appPaths)
	if len(got) != len(want) {
		t.Fatalf("leftovers = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("leftovers[%d] = %v, want %v", index, got[index], want[index])
		}
	}
}

func TestADeletedProfileWithADocumentIsListed(t *testing.T) {
	appPaths := testPaths(t)
	folderWithDocument(t, filepath.Join(appPaths.Root(), "profiles-deleted", "business-20260101-000000"))

	want := []Leftover{{Kind: LeftoverDeletedProfile, Path: "profiles-deleted/business-20260101-000000"}}
	got := Leftovers(appPaths)
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("leftovers = %v, want %v", got, want)
	}
}

func TestAPendingRestoreWithADocumentIsListed(t *testing.T) {
	appPaths := testPaths(t)
	folderWithDocument(t, filepath.Join(appPaths.Root(), backup.PendingDir))

	want := []Leftover{{Kind: LeftoverPendingRestore, Path: backup.PendingDir}}
	got := Leftovers(appPaths)
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("leftovers = %v, want %v", got, want)
	}
}

func TestAPreRestoreFolderWithoutDocumentsIsNotListed(t *testing.T) {
	appPaths := testPaths(t)
	folder := filepath.Join(appPaths.Root(), "pre-restore-20260101-000000")
	if err := os.MkdirAll(filepath.Join(folder, "documents"), 0o700); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, backup.DBName), []byte("encrypted"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}

	if got := Leftovers(appPaths); len(got) != 0 {
		t.Fatalf("leftovers = %v, want none", got)
	}
}

func TestLeftoversAreSortedByPath(t *testing.T) {
	appPaths := testPaths(t)
	folderWithDocument(t, filepath.Join(appPaths.Root(), backup.PendingDir))
	folderWithDocument(t, filepath.Join(appPaths.Root(), "profiles-deleted", "business-20260101-000000"))
	profile, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	folderWithDocument(t, filepath.Join(profile.Dir, "pre-restore-20260101-000000"))
	folderWithDocument(t, filepath.Join(appPaths.Root(), "pre-restore-20260101-000000"))

	got := make([]string, 0)
	for _, leftover := range Leftovers(appPaths) {
		got = append(got, leftover.Path)
	}
	want := []string{
		"pre-restore-20260101-000000",
		"profiles-deleted/business-20260101-000000",
		"profiles/" + profile.Slug + "/pre-restore-20260101-000000",
		backup.PendingDir,
	}
	if len(got) != len(want) {
		t.Fatalf("listed = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("listed[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestSlugKeyMatchesTheProfileFolderHelpers(t *testing.T) {
	appPaths := testPaths(t)

	if got := SlugKey(nil); got != "default" {
		t.Fatalf("SlugKey(nil) = %q, want default", got)
	}
	if got := SlugKey(stringPointer("business")); got != "business" {
		t.Fatalf("SlugKey(business) = %q", got)
	}

	defaultDir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("default data dir: %v", err)
	}
	if want, _ := appPaths.ProfileDir(SlugKey(nil)); defaultDir != want {
		t.Errorf("default dir = %q, want %q", defaultDir, want)
	}
	businessDir, err := profiles.DataDir(appPaths, "business")
	if err != nil {
		t.Fatalf("business data dir: %v", err)
	}
	if want, _ := appPaths.ProfileDir(SlugKey(stringPointer("business"))); businessDir != want {
		t.Errorf("business dir = %q, want %q", businessDir, want)
	}
}

func TestAStagedRestoreIsAppliedBeforeTheDatabaseCanOpen(t *testing.T) {
	appPaths := testPaths(t)
	opened := database(t, appPaths, "default")
	accountID := account(t, opened)
	addTransaction(t, opened, accountID, "Before backup", "before")

	dir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	service := backup.NewService(dir, "test")
	backupPath := filepath.Join(appPaths.Root(), "startup.zip")
	if _, err := service.CreateBackup(opened, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	addTransaction(t, opened, accountID, "After backup", "after")
	if _, err := service.StageRestore(opened, backupPath, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	prepared, err := Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if prepared.Slug != nil {
		t.Errorf("slug = %v, want nil", *prepared.Slug)
	}
	if prepared.RestoredFrom == nil {
		t.Fatal("the restore moved the live data aside")
	}
	previous := *prepared.RestoredFrom
	if filepath.Base(previous) != "pre-restore-20261015-123456" {
		t.Errorf("previous = %q", filepath.Base(previous))
	}
	if filepath.Dir(previous) != appPaths.Root() {
		t.Errorf("previous parent = %q, want %q", filepath.Dir(previous), appPaths.Root())
	}
	if info, err := os.Stat(filepath.Join(previous, backup.DBName)); err != nil || !info.Mode().IsRegular() {
		t.Errorf("previous database missing: %v", err)
	}
	if service.HasPendingRestore() {
		t.Error("a restore is still pending")
	}

	reopened, err := unlock.Unlock(appPaths, "default", []byte(pass))
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	names := partnerNames(t, reopened)
	if len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partners = %v, want [Before backup]", names)
	}
}

func TestPrepareRemovesALeftoverMigrationStagingFolder(t *testing.T) {
	appPaths := testPaths(t)
	staging := filepath.Join(appPaths.Root(), ".findesk-migration-staging", "1")
	if err := os.MkdirAll(filepath.Join(staging, "documents"), 0o700); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "documents", "copy.pdf"), []byte("unencrypted"), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "finglass.db"), []byte("staged"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}

	if _, err := Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if _, err := os.Stat(filepath.Join(appPaths.Root(), ".findesk-migration-staging")); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
}

func TestWithoutAStagedRestoreNothingMoves(t *testing.T) {
	appPaths := testPaths(t)
	opened := database(t, appPaths, "default")
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	prepared, err := Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if prepared.Slug != nil {
		t.Errorf("slug = %v, want nil", *prepared.Slug)
	}
	if prepared.RestoredFrom != nil {
		t.Errorf("restored from = %v, want nil", *prepared.RestoredFrom)
	}
	if info, err := os.Stat(filepath.Join(appPaths.Root(), "finglass.db")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("database missing: %v", err)
	}
	if folders := preRestoreFolders(t, appPaths.Root()); len(folders) != 0 {
		t.Errorf("pre-restore folders = %v", folders)
	}
}

func TestTheActiveNamedProfileIsPreparedInItsOwnFolder(t *testing.T) {
	appPaths := testPaths(t)
	profile, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	slug := profile.Slug
	if err := profiles.SetActive(appPaths, slug); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appPaths.Root(), "default-data.txt"), []byte("default"), 0o600); err != nil {
		t.Fatalf("write default data: %v", err)
	}

	opened := database(t, appPaths, slug)
	accountID := account(t, opened)
	addTransaction(t, opened, accountID, "Before backup", "before")
	dir, err := profiles.DataDir(appPaths, slug)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	service := backup.NewService(dir, "test")
	backupPath := filepath.Join(appPaths.Root(), "named.zip")
	if _, err := service.CreateBackup(opened, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	addTransaction(t, opened, accountID, "After backup", "after")
	if _, err := service.StageRestore(opened, backupPath, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	prepared, err := Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if prepared.Slug == nil || *prepared.Slug != slug {
		t.Errorf("slug = %v, want %q", prepared.Slug, slug)
	}
	if prepared.RestoredFrom == nil {
		t.Fatal("the named profile's restore was applied")
	}
	if filepath.Dir(*prepared.RestoredFrom) != profile.Dir {
		t.Errorf("previous parent = %q, want %q", filepath.Dir(*prepared.RestoredFrom), profile.Dir)
	}
	data, err := os.ReadFile(filepath.Join(appPaths.Root(), "default-data.txt"))
	if err != nil || string(data) != "default" {
		t.Errorf("default data = %q, %v", data, err)
	}
	if folders := preRestoreFolders(t, appPaths.Root()); len(folders) != 0 {
		t.Errorf("the default profile is untouched, folders = %v", folders)
	}

	reopened, err := unlock.Unlock(appPaths, slug, []byte(pass))
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	names := partnerNames(t, reopened)
	if len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partners = %v, want [Before backup]", names)
	}
}

func TestAFailingRestoreReturnsAnErrorAndLeavesTheLiveDatabaseAlone(t *testing.T) {
	appPaths := testPaths(t)
	opened := database(t, appPaths, "default")
	accountID := account(t, opened)
	addTransaction(t, opened, accountID, "Before backup", "before")

	dir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	service := backup.NewService(dir, "test")
	backupPath := filepath.Join(appPaths.Root(), "failing.zip")
	if _, err := service.CreateBackup(opened, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	addTransaction(t, opened, accountID, "After backup", "after")
	if _, err := service.StageRestore(opened, backupPath, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	// A file where the pre-restore folder must go makes creating that folder
	// fail.
	if err := os.WriteFile(
		filepath.Join(appPaths.Root(), "pre-restore-20261015-123456"),
		[]byte("not a folder"),
		0o600,
	); err != nil {
		t.Fatalf("write conflict: %v", err)
	}

	_, err = Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC))
	if err == nil {
		t.Fatal("prepare succeeded, want an error")
	}
	var startupError *StartupError
	if !errors.As(err, &startupError) || startupError.Kind != StartupBackup {
		t.Fatalf("error = %v, want a backup error", err)
	}
	if !service.HasPendingRestore() {
		t.Error("nothing was applied")
	}

	file, err := appPaths.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	live, err := db.Open(file, newPassphrase(t, pass))
	if err != nil {
		t.Fatalf("open live database: %v", err)
	}
	defer func() { _ = live.Close() }()
	names := partnerNames(t, live)
	if len(names) != 2 || names[0] != "Before backup" || names[1] != "After backup" {
		t.Errorf("partners = %v, want both", names)
	}
}

func TestPassphrasesNeverFormatted(t *testing.T) {
	values := []any{
		&StartupError{Kind: StartupProfile, Err: errors.New("profile failure")},
		&StartupError{Kind: StartupBackup, Err: errors.New("backup failure")},
		&StartupError{Kind: StartupIO},
		Startup{Slug: stringPointer("work"), RestoredFrom: stringPointer("pre-restore-1")},
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, "fixture passphrase 1") || strings.Contains(text, pass) {
				t.Errorf("%s of %T contains a passphrase: %s", format, value, text)
			}
		}
	}
}
