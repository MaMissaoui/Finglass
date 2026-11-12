package profileswitch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
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

func passphrase(t *testing.T) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(pass), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func database(t *testing.T, appPaths paths.AppPaths, slug string) *db.Database {
	t.Helper()
	file, err := appPaths.DatabaseFile(slug)
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	opened, err := db.Create(file, passphrase(t))
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
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

func switchEntries(t *testing.T, opened *db.Database) []string {
	t.Helper()
	rows, err := audit.Recent(context.Background(), opened, 100)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	entries := make([]string, 0)
	for _, row := range rows {
		if row.Action == "PROFILE_SWITCH" {
			entries = append(entries, row.Details)
		}
	}
	return entries
}

func at() time.Time {
	return time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)
}

func TestPassphrasesNeverFormatted(t *testing.T) {
	values := []any{
		&SwitchError{Kind: SwitchSameProfile},
		&SwitchError{Kind: SwitchUnknownProfile},
		&SwitchError{Kind: SwitchBusy},
		&SwitchError{Kind: SwitchProfile, Err: errors.New("profile failure")},
		&SwitchError{Kind: SwitchStartup, Err: errors.New("startup failure")},
		&SwitchError{Kind: SwitchDatabase, Err: errors.New("io")},
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, pass) || strings.Contains(text, "another very long passphrase") {
				t.Errorf("%s of %T contains a passphrase: %s", format, value, text)
			}
		}
	}
}

func TestASwitchWritesOneEntryIntoTheOldDatabaseAndActivatesTheTarget(t *testing.T) {
	appPaths := testPaths(t)
	old := database(t, appPaths, "default")
	target, err := profiles.Create(appPaths, "Business GmbH")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	slug := target.Slug
	if active, err := profiles.Active(appPaths); err != nil || active.Slug != "" {
		t.Fatalf("active = %v, %v", active.Slug, err)
	}

	prepared, err := BeginSwitch(old, appPaths, nil, &slug, at())
	if err != nil {
		t.Fatalf("begin switch: %v", err)
	}

	if prepared.Slug == nil || *prepared.Slug != slug {
		t.Errorf("slug = %v, want %q", prepared.Slug, slug)
	}
	if prepared.RestoredFrom != nil {
		t.Errorf("restored from = %v, want nil", *prepared.RestoredFrom)
	}
	active, err := profiles.Active(appPaths)
	if err != nil || active.Slug != slug {
		t.Errorf("active = %q, %v, want %q", active.Slug, err, slug)
	}

	entries := switchEntries(t, old)
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want one", entries)
	}
	if !strings.Contains(entries[0], "default") || !strings.Contains(entries[0], slug) {
		t.Errorf("the entry names both slugs: %q", entries[0])
	}
	if strings.Contains(entries[0], "Business GmbH") {
		t.Errorf("the display name is not audited: %q", entries[0])
	}
	if strings.Contains(entries[0], appPaths.Root()) {
		t.Errorf("the path is not audited: %q", entries[0])
	}
}

func TestAStagedRestoreInTheTargetFolderIsAppliedBeforeTheSwitchFinishes(t *testing.T) {
	appPaths := testPaths(t)
	old := database(t, appPaths, "default")
	target, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	slug := target.Slug

	targetDB := database(t, appPaths, slug)
	_ = account(t, targetDB)
	dir, err := profiles.DataDir(appPaths, slug)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	service := backup.NewService(dir, "test")
	backupPath := filepath.Join(appPaths.Root(), "target.zip")
	if _, err := service.CreateBackup(targetDB, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if _, err := service.StageRestore(targetDB, backupPath, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := targetDB.Close(); err != nil {
		t.Fatalf("close target: %v", err)
	}

	prepared, err := BeginSwitch(old, appPaths, nil, &slug, at())
	if err != nil {
		t.Fatalf("begin switch: %v", err)
	}

	if prepared.Slug == nil || *prepared.Slug != slug {
		t.Errorf("slug = %v, want %q", prepared.Slug, slug)
	}
	if prepared.RestoredFrom == nil {
		t.Fatal("the target's restore moved data aside")
	}
	if filepath.Dir(*prepared.RestoredFrom) != target.Dir {
		t.Errorf("previous parent = %q, want %q", filepath.Dir(*prepared.RestoredFrom), target.Dir)
	}
	if service.HasPendingRestore() {
		t.Error("nothing waits for the next start")
	}
	if entries := switchEntries(t, old); len(entries) != 1 {
		t.Errorf("entries = %v, want one", entries)
	}
}

func TestAnUnknownTargetIsRefusedAndNothingChanges(t *testing.T) {
	appPaths := testPaths(t)
	old := database(t, appPaths, "default")
	missing := "missing"

	_, err := BeginSwitch(old, appPaths, nil, &missing, time.Now().UTC())
	if err == nil {
		t.Fatal("switched to an unknown profile")
	}
	var switchError *SwitchError
	if !errors.As(err, &switchError) || switchError.Kind != SwitchUnknownProfile {
		t.Fatalf("error = %v, want unknown profile", err)
	}
	if active, err := profiles.Active(appPaths); err != nil || active.Slug != "" {
		t.Errorf("active = %q, %v", active.Slug, err)
	}
	if entries := switchEntries(t, old); len(entries) != 0 {
		t.Errorf("entries = %v, want none", entries)
	}
}

func TestSwitchingToTheOpenProfileIsRefusedAndNothingChanges(t *testing.T) {
	appPaths := testPaths(t)
	old := database(t, appPaths, "default")

	_, err := BeginSwitch(old, appPaths, nil, nil, time.Now().UTC())
	if err == nil {
		t.Fatal("switched to the open profile")
	}
	var switchError *SwitchError
	if !errors.As(err, &switchError) || switchError.Kind != SwitchSameProfile {
		t.Fatalf("error = %v, want same profile", err)
	}
	if active, err := profiles.Active(appPaths); err != nil || active.Slug != "" {
		t.Errorf("active = %q, %v", active.Slug, err)
	}
	if entries := switchEntries(t, old); len(entries) != 0 {
		t.Errorf("entries = %v, want none", entries)
	}
}

func TestATargetWhoseRestoreCannotBeAppliedIsRefusedAndNothingChanges(t *testing.T) {
	appPaths := testPaths(t)
	old := database(t, appPaths, "default")
	target, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	slug := target.Slug

	targetDB := database(t, appPaths, slug)
	dir, err := profiles.DataDir(appPaths, slug)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	service := backup.NewService(dir, "test")
	backupPath := filepath.Join(appPaths.Root(), "target.zip")
	if _, err := service.CreateBackup(targetDB, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if _, err := service.StageRestore(targetDB, backupPath, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := targetDB.Close(); err != nil {
		t.Fatalf("close target: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(target.Dir, "pre-restore-20261015-123456"),
		[]byte("not a folder"),
		0o600,
	); err != nil {
		t.Fatalf("write conflict: %v", err)
	}

	_, err = BeginSwitch(old, appPaths, nil, &slug, at())
	if err == nil {
		t.Fatal("switch succeeded, want an error")
	}
	var switchError *SwitchError
	if !errors.As(err, &switchError) || switchError.Kind != SwitchStartup {
		t.Fatalf("error = %v, want a startup error", err)
	}
	if active, err := profiles.Active(appPaths); err != nil || active.Slug != "" {
		t.Errorf("the old profile stays active: %q, %v", active.Slug, err)
	}
	if entries := switchEntries(t, old); len(entries) != 0 {
		t.Errorf("nothing was audited: %v", entries)
	}
	if !service.HasPendingRestore() {
		t.Error("the staged restore is untouched")
	}
}
