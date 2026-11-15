package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	testPassphrase = "correct horse battery staple"
	testPIN        = "987654"
)

var testNow = time.Date(2026, 10, 15, 12, 34, 56, 0, time.UTC)

func stringPointer(value string) *string {
	return &value
}

func makeSyntheticBackup(t *testing.T) string {
	t.Helper()

	profileDir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	passphrase, err := secret.NewPassphrase([]byte(testPassphrase), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	defer passphrase.Wipe()

	database, err := db.Create(filepath.Join(profileDir, backup.DBName), passphrase)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}

	ctx := context.Background()
	accountName := "Girokonto"
	account, err := accounts.Create(ctx, database, model.NewAccount{
		Name:        &accountName,
		IBAN:        stringPointer("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 250_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:  "Food",
		Color: "#FF5722",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	day, ok := civil.New(2026, time.October, 2)
	if !ok {
		t.Fatal("invalid date")
	}
	partner := "Bakery"
	if _, err := transactions.InsertPlanned(ctx, database, account, []model.NewTransaction{{
		BookingDate: day,
		Amount:      money.Money{Minor: -1_250, Currency: money.EUR},
		PartnerName: &partner,
		CategoryID:  &food,
		ImportHash:  "shots-test",
		CreatedAt:   testNow,
	}}); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	if _, err := documents.Create(ctx, database, model.NewDocument{
		FileName:   "receipt.pdf",
		StoredPath: "documents/receipt.pdf",
		MIMEType:   "application/pdf",
	}); err != nil {
		t.Fatalf("create document: %v", err)
	}

	target := filepath.Join(t.TempDir(), "backup.zip")
	if _, err := backup.NewService(profileDir, "test").CreateBackup(database, target); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return target
}

func TestExporterWritesTheSnapshotAndDeletesTheTempHome(t *testing.T) {
	backupPath := makeSyntheticBackup(t)
	outDir := filepath.Join(t.TempDir(), "shots")
	tempParent := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("FINGLASS_HOME", "")

	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--backup", backupPath, "--out", outDir},
		strings.NewReader(testPassphrase+"\n"),
		&stdout,
		&stderr,
		runOptions{tempParent: tempParent, now: func() time.Time { return testNow }},
	)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}

	entries, err := os.ReadDir(tempParent)
	if err != nil {
		t.Fatalf("read temp parent: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the temporary home was not deleted: %v", entries)
	}
	homeEntries, err := os.ReadDir(fakeHome)
	if err != nil {
		t.Fatalf("read fake home: %v", err)
	}
	if len(homeEntries) != 0 {
		t.Fatalf("the exporter wrote into the home folder: %v", homeEntries)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(outDir)
		if err != nil {
			t.Fatalf("stat output folder: %v", err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o700); got != want {
			t.Fatalf("output folder mode = %o, want %o", got, want)
		}
	}

	snapshotPath := filepath.Join(outDir, snapshotName)
	data, err := os.ReadFile(snapshotPath) // #nosec G304 -- the test's temporary output folder.
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(snapshotPath)
		if err != nil {
			t.Fatalf("stat snapshot: %v", err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
			t.Fatalf("snapshot mode = %o, want %o", got, want)
		}
	}
	for _, forbidden := range []string{testPassphrase, testPIN} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("snapshot contains a secret: %q", forbidden)
		}
	}

	var snapshot snapshotFile
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	for _, code := range []string{"en", "de"} {
		language, ok := snapshot.Languages[code]
		if !ok {
			t.Fatalf("snapshot has no %s language", code)
		}
		if !language.Shell.ShellOpen {
			t.Fatalf("%s shell is not open", code)
		}
		if !language.Dashboard.ShowContent {
			t.Fatalf("%s dashboard has no content", code)
		}
		if !language.Accounts.HasContent || !language.Transactions.HasTransactions {
			t.Fatalf("%s accounts or transactions are empty", code)
		}
		if len(language.Documents.Rows) == 0 {
			t.Fatalf("%s document list is empty", code)
		}
		if language.Reports.Tab != "summary" {
			t.Fatalf("%s reports tab = %q, want summary", code, language.Reports.Tab)
		}
		if len(language.Settings.CategoryRows) == 0 {
			t.Fatalf("%s settings have no category", code)
		}
	}
}

func TestExporterDeletesTheTempHomeAfterAFailedRestore(t *testing.T) {
	backupPath := makeSyntheticBackup(t)
	tempParent := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "shots")

	err := run(
		[]string{"--backup", backupPath, "--out", outDir},
		strings.NewReader("a completely wrong passphrase\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		runOptions{tempParent: tempParent, now: func() time.Time { return testNow }},
	)
	if err == nil {
		t.Fatal("run accepted the wrong passphrase")
	}
	entries, readErr := os.ReadDir(tempParent)
	if readErr != nil {
		t.Fatalf("read temp parent: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("the temporary home was not deleted after the error: %v", entries)
	}
}

func TestExporterRefusesAnOutputFolderInsideTheRepository(t *testing.T) {
	repository, ok := findRepositoryRoot(".")
	if !ok {
		t.Fatal("the test is not running inside the repository")
	}
	outDir := filepath.Join(repository, ".finglass-shots-refused-output")
	t.Cleanup(func() { _ = os.RemoveAll(outDir) })

	backupPath := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backupPath, []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("write backup placeholder: %v", err)
	}

	err := run(
		[]string{"--backup", backupPath, "--out", outDir},
		strings.NewReader(testPassphrase+"\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		runOptions{tempParent: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("run error = %v, want an inside-the-repository refusal", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("the refused output folder was created: %v", statErr)
	}
}

func TestExporterRefusesANonEmptyOutputFolder(t *testing.T) {
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "existing.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	backupPath := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(backupPath, []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("write backup placeholder: %v", err)
	}

	err := run(
		[]string{"--backup", backupPath, "--out", outDir},
		strings.NewReader(testPassphrase+"\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		runOptions{tempParent: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("run error = %v, want a not-empty refusal", err)
	}
}

func TestExporterRefusesAMissingBackup(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "shots")

	for name, args := range map[string][]string{
		"flag": {"--out", outDir},
		"file": {"--backup", filepath.Join(t.TempDir(), "missing.zip"), "--out", outDir},
	} {
		t.Run(name, func(t *testing.T) {
			err := run(args, strings.NewReader(testPassphrase+"\n"),
				&bytes.Buffer{}, &bytes.Buffer{}, runOptions{tempParent: t.TempDir()})
			if err == nil {
				t.Fatal("run accepted a missing backup")
			}
		})
	}
}

func TestExporterRefusesTheUsersRealDataFoldersWithAFakeHome(t *testing.T) {
	fakeHome := t.TempDir()
	tempHome := t.TempDir()

	cases := map[string]string{
		"default": filepath.Join(fakeHome, ".finglass"),
		"findesk": filepath.Join(fakeHome, ".findesk", "profile"),
		"outside": filepath.Join(t.TempDir(), "profile"),
	}
	for name, dataDir := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateDataFolder(dataDir, tempHome, fakeHome); err == nil {
				t.Fatalf("validateDataFolder(%q) accepted the folder", dataDir)
			}
		})
	}

	if err := validateDataFolder(filepath.Join(tempHome, "profile"), tempHome, fakeHome); err != nil {
		t.Fatalf("validateDataFolder rejected the temporary profile: %v", err)
	}
}
