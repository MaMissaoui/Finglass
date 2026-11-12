package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// The committed fixture passphrase of the Rust-made backups. It is a dummy and
// protects nothing.
const fixturePass = "correct horse battery staple"

// otherPass is the passphrase of testdata/rust-backup-other-passphrase.zip.
const otherPass = "a different backup passphrase"

const wrongPass = "a completely wrong passphrase"

func passphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()

	p, err := secret.NewPassphrase([]byte(value), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	return p
}

func profile(t *testing.T) (temp, dir string, d *db.Database) {
	t.Helper()

	temp = t.TempDir()
	dir = filepath.Join(temp, "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll profile: %v", err)
	}

	d, err := db.Create(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return temp, dir, d
}

func createAccount(t *testing.T, d *db.Database, name string) model.AccountID {
	t.Helper()

	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        &name,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func addTransaction(t *testing.T, d *db.Database, accountID model.AccountID, partner, hash string) {
	t.Helper()
	addTransactionAt(t, d, accountID, partner, hash, -1_000, 2026, time.January, 1)
}

func addTransactionAt(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	partner string,
	hash string,
	minor int64,
	year int,
	month time.Month,
	day int,
) {
	t.Helper()

	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	bookingDate, err := repo.FormatDate(date)
	if err != nil {
		t.Fatalf("FormatDate: %v", err)
	}
	createdAt, err := repo.FormatTimestamp(time.Now().UTC())
	if err != nil {
		t.Fatalf("FormatTimestamp: %v", err)
	}

	err = d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO transactions "+
				"(account_id, booking_date, amount_minor, currency, partner_name, import_hash, created_at, is_new) "+
				"VALUES (?1, ?2, ?3, 'EUR', ?4, ?5, ?6, 0)",
			int64(accountID), bookingDate, minor, partner, hash, createdAt,
		)
		return err
	})
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
}

func partnerNames(t *testing.T, d *db.Database) []string {
	t.Helper()

	var names []string
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT partner_name FROM transactions ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var name sql.NullString
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if name.Valid {
				names = append(names, name.String)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read partner names: %v", err)
	}
	return names
}

func countTransactions(t *testing.T, d *db.Database) int64 {
	t.Helper()

	var count int64
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM transactions").Scan(&count)
	})
	if err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return count
}

type zipWriteEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func writeTestZip(t *testing.T, path string, entries []zipWriteEntry) {
	t.Helper()

	file, err := os.Create(path) // #nosec G304 -- path is a test temporary file.
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)

	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(entry.mode)
		destination, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := destination.Write(entry.data); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func openZip(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		t.Fatalf("open zip: %v", err)
	}
	if reader == nil {
		t.Fatalf("open zip: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}

func zipNames(t *testing.T, path string) map[string]bool {
	t.Helper()

	names := make(map[string]bool)
	for _, entry := range openZip(t, path).File {
		names[entry.Name] = true
	}
	return names
}

func orderedZipNames(t *testing.T, path string) []string {
	t.Helper()

	var names []string
	for _, entry := range openZip(t, path).File {
		names = append(names, entry.Name)
	}
	return names
}

func zipEntryBytes(t *testing.T, path, name string) []byte {
	t.Helper()

	var found *zip.File
	for _, entry := range openZip(t, path).File {
		if entry.Name == name {
			found = entry
		}
	}
	if found == nil {
		t.Fatalf("zip has no entry %q", name)
	}

	source, err := found.Open()
	if err != nil {
		t.Fatalf("open zip entry: %v", err)
	}
	defer func() { _ = source.Close() }()

	data, err := io.ReadAll(source)
	if err != nil {
		t.Fatalf("read zip entry: %v", err)
	}
	return data
}

func dirHasSuffix(dir, suffix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			return true
		}
	}
	return false
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	return info.Mode().Perm()
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()

	if mode := fileMode(t, path); mode&0o077 != 0 {
		t.Errorf("%s is not owner-only: %04o", path, mode)
	}
}

func requirePassphraseRequired(t *testing.T, err error, wrongGiven bool) {
	t.Helper()

	var required *PassphraseRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("error is not PassphraseRequiredError: %v", err)
	}
	if required.WrongPassphraseGiven != wrongGiven {
		t.Fatalf("WrongPassphraseGiven = %t, want %t", required.WrongPassphraseGiven, wrongGiven)
	}
}

// backupWithPassphrase builds a backup of a separate profile made with pass.
func backupWithPassphrase(t *testing.T, pass string) string {
	t.Helper()

	temp := t.TempDir()
	dir := filepath.Join(temp, "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	d, err := db.Create(filepath.Join(dir, DBName), passphrase(t, pass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	target := filepath.Join(temp, "backup.zip")
	if _, err := NewService(dir, "0.3.4").CreateBackup(d, target); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return target
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()

	// #nosec G304 -- fixed repository fixture path.
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "fixture-*.zip")
	if err != nil {
		t.Fatalf("create fixture copy: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatalf("write fixture copy: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture copy: %v", err)
	}
	return file.Name()
}

func TestDatabaseNameMatchesAppPaths(t *testing.T) {
	appPaths, err := paths.Resolve("", "/home/alice")
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}

	database, err := appPaths.DatabaseFile("default")
	if err != nil {
		t.Fatalf("DatabaseFile: %v", err)
	}
	if filepath.Base(database) != DBName {
		t.Errorf("database file name = %q, want %q", filepath.Base(database), DBName)
	}
}

func TestBackupAndRestoreRoundTripKeepsTheReplacedData(t *testing.T) {
	temp, dir, d := profile(t)
	accountID := createAccount(t, d, "Account")
	addTransaction(t, d, accountID, "Before backup", "before")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "receipt.pdf"), []byte("pdf-bytes"), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "backup.zip")
	info, err := service.CreateBackup(d, backup)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if info.Transactions != 1 {
		t.Errorf("transactions = %d, want 1", info.Transactions)
	}
	if info.Documents != 1 {
		t.Errorf("documents = %d, want 1", info.Documents)
	}
	inspected, err := service.Inspect(backup)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspected.Transactions != 1 {
		t.Errorf("inspected transactions = %d, want 1", inspected.Transactions)
	}

	// Changes after the backup.
	addTransaction(t, d, accountID, "After backup", "after")
	if err := os.WriteFile(filepath.Join(documents, "new.pdf"), []byte("new"), 0o600); err != nil {
		t.Fatalf("write new document: %v", err)
	}

	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	if !service.HasPendingRestore() {
		t.Fatal("a restore should be pending")
	}
	if names := partnerNames(t, d); len(names) != 2 {
		t.Errorf("nothing changes before restart: got %v", names)
	}

	// Restart.
	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	previous, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if !applied {
		t.Fatal("a restore was pending")
	}
	if service.HasPendingRestore() {
		t.Error("the pending folder must be gone")
	}

	reopened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	if names := partnerNames(t, reopened); len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partner names = %v, want [Before backup]", names)
	}
	receipt, err := os.ReadFile(filepath.Join(documents, "receipt.pdf")) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if string(receipt) != "pdf-bytes" {
		t.Errorf("receipt = %q, want pdf-bytes", receipt)
	}
	if _, err := os.Stat(filepath.Join(documents, "new.pdf")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("new.pdf should be gone, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(previous, "documents", "new.pdf")); err != nil {
		t.Errorf("replaced data is kept: %v", err)
	}
	if !isRegularFile(filepath.Join(previous, DBName)) {
		t.Errorf("%s is not a file", filepath.Join(previous, DBName))
	}
}

func TestRestoreMovesAHotRollbackJournalAside(t *testing.T) {
	temp, dir, d := profile(t)
	accountID := createAccount(t, d, "Account")
	addTransaction(t, d, accountID, "Before backup", "before")

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "journal.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}

	journal := filepath.Join(dir, DBName+"-journal")
	journalBytes := []byte{1, 2, 3}
	if err := os.WriteFile(journal, journalBytes, 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	previous, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if !applied {
		t.Fatal("a restore was pending")
	}

	if _, err := os.Stat(journal); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old journal must not stay next to the restored database: %v", err)
	}
	moved, err := os.ReadFile(filepath.Join(previous, DBName+"-journal")) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read moved journal: %v", err)
	}
	if string(moved) != string(journalBytes) {
		t.Errorf("moved journal = %v, want %v", moved, journalBytes)
	}

	reopened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if names := partnerNames(t, reopened); len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partner names = %v, want [Before backup]", names)
	}
}

func TestABackupSavedInsideDocumentsDoesNotIncludeItself(t *testing.T) {
	_, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "receipt.pdf"), []byte("pdf-bytes"), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	service := NewService(dir, "0.3.4")
	target := filepath.Join(documents, "backup.zip")

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.CreateBackup(d, target); err != nil {
			t.Fatalf("CreateBackup: %v", err)
		}

		entries := zipNames(t, target)
		if len(entries) != 3 {
			t.Errorf("entries = %v, want 3", entries)
		}
		for _, name := range []string{Manifest, DBName, "documents/receipt.pdf"} {
			if !entries[name] {
				t.Errorf("missing entry %q: %v", name, entries)
			}
		}
		for name := range entries {
			if strings.HasSuffix(name, ".zip.tmp") {
				t.Errorf("temporary ZIP entry %q", name)
			}
		}
		if entries["documents/backup.zip"] {
			t.Error("the backup must not include itself")
		}
		if dirHasSuffix(documents, ".zip.tmp") {
			t.Error("no temporary ZIP left in documents/")
		}
	}
}

func TestRestoredFilesAndDirectoriesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}

	_, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "receipt.pdf"), []byte("pdf-bytes"), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(filepath.Dir(dir), "restore-owner.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	pending := filepath.Join(dir, PendingDir)
	assertOwnerOnly(t, pending)
	assertOwnerOnly(t, filepath.Join(pending, "documents"))
	assertOwnerOnly(t, filepath.Join(pending, "documents", "receipt.pdf"))
	assertOwnerOnly(t, filepath.Join(pending, DBName))

	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	if _, _, err := service.ApplyPendingRestore(time.Now()); err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}

	assertOwnerOnly(t, documents)
	assertOwnerOnly(t, filepath.Join(documents, "receipt.pdf"))
	assertOwnerOnly(t, filepath.Join(dir, DBName))
}

func TestRejectsForeignZips(t *testing.T) {
	temp, dir, _ := profile(t)
	service := NewService(dir, "0.3.4")

	foreign := filepath.Join(temp, "random.zip")
	writeTestZip(t, foreign, []zipWriteEntry{{name: "a.txt", data: []byte("x")}})
	if _, err := service.Inspect(foreign); !errors.Is(err, ErrNotABackup) {
		t.Errorf("Inspect(random.zip) = %v, want ErrNotABackup", err)
	}

	noDatabase := filepath.Join(temp, "no-database.zip")
	writeTestZip(t, noDatabase, []zipWriteEntry{{name: Manifest, data: []byte("created=x")}})
	if _, err := service.Inspect(noDatabase); !errors.Is(err, ErrNoDatabase) {
		t.Errorf("Inspect(no-database.zip) = %v, want ErrNoDatabase", err)
	}
}

func TestRejectsZipSlipAndUnexpectedPaths(t *testing.T) {
	temp, dir, d := profile(t)
	service := NewService(dir, "0.3.4")

	cases := []struct {
		evil        string
		escapedName string
	}{
		{"../escape.txt", "escape.txt"},
		{"/abs.txt", "abs.txt"},
		{"documents/../../escape.txt", "escape.txt"},
		{"documents/a/b.txt", "b.txt"},
		{"other.txt", "other.txt"},
		{"..\\escape.txt", "escape.txt"},
	}

	for index, testCase := range cases {
		path := filepath.Join(temp, fmt.Sprintf("evil-%d.zip", index))
		writeTestZip(t, path, []zipWriteEntry{
			{name: Manifest, data: []byte("created=x")},
			{name: DBName, data: []byte("x")},
			{name: testCase.evil, data: []byte("pwned")},
		})

		_, err := service.StageRestore(d, path, nil)
		var unsafeErr *UnsafePathError
		if !errors.As(err, &unsafeErr) {
			t.Errorf("case %q should be rejected, got %v", testCase.evil, err)
		}
		if service.HasPendingRestore() {
			t.Errorf("case %q left a staging folder", testCase.evil)
		}
		if _, err := os.Stat(filepath.Join(temp, testCase.escapedName)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("case %q wrote outside the staging folder", testCase.evil)
		}
		if _, err := os.Stat(filepath.Join(dir, testCase.escapedName)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("case %q wrote into the profile", testCase.evil)
		}
	}
}

func TestACorruptBackupIsRejectedAndLeavesNothingStaged(t *testing.T) {
	temp, dir, d := profile(t)
	service := NewService(dir, "0.3.4")

	corrupt := filepath.Join(temp, "corrupt.zip")
	writeTestZip(t, corrupt, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: []byte("not a database")},
	})

	if _, err := service.StageRestore(d, corrupt, nil); err == nil {
		t.Fatal("a corrupt backup must be rejected")
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves no staging folder")
	}
}

func TestAStagedRestoreCanBeCancelled(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "cancel.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	service.CancelPendingRestore()

	if service.HasPendingRestore() {
		t.Error("the pending folder must be gone")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	_, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if applied {
		t.Error("nothing was staged")
	}
}

func TestARestoreThatUnpacksMoreThanTheCapIsRejected(t *testing.T) {
	temp, dir, d := profile(t)
	first := make([]byte, 600)
	for index := range first {
		first[index] = 'a'
	}
	second := make([]byte, 600)
	for index := range second {
		second[index] = 'b'
	}

	big := filepath.Join(temp, "big.zip")
	writeTestZip(t, big, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: []byte("x")},
		{name: "documents/a.pdf", data: first},
		{name: "documents/b.pdf", data: second},
	})

	service := NewService(dir, "0.3.4").WithMaxRestoreBytes(1_000)
	_, err := service.StageRestore(d, big, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	if NewService(dir, "0.3.4").HasPendingRestore() {
		t.Error("failed staging leaves nothing behind")
	}
}

func TestAnEntryLargerThanTheCapIsRejectedWhileCopying(t *testing.T) {
	temp, dir, d := profile(t)
	large := make([]byte, 2_000)
	for index := range large {
		large[index] = 'x'
	}

	backup := filepath.Join(temp, "large-entry.zip")
	writeTestZip(t, backup, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: []byte("x")},
		{name: "documents/large.bin", data: large},
	})

	service := NewService(dir, "0.3.4").WithMaxRestoreBytes(1_000)
	_, err := service.StageRestore(d, backup, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	if NewService(dir, "0.3.4").HasPendingRestore() {
		t.Error("staging folder must be removed")
	}
}

func TestABackupThatCouldNotBeRestoredIsRefused(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "scan.pdf"), make([]byte, 64*1024), 0o600); err != nil {
		t.Fatalf("write scan: %v", err)
	}

	target := filepath.Join(temp, "too-large.zip")
	service := NewService(dir, "0.3.4").WithMaxRestoreBytes(32 * 1024)
	if _, err := service.CreateBackup(d, target); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("CreateBackup error = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target must not exist on failure: %v", err)
	}
	if dirHasSuffix(dir, ".db.tmp") {
		t.Error("temporary copy removed")
	}
}

func TestNoTemporaryDatabaseCopyIsLeftBehind(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	target := filepath.Join(temp, "temporary.zip")
	if _, err := NewService(dir, "0.3.4").CreateBackup(d, target); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if !isRegularFile(target) {
		t.Error("target is not a file")
	}
	if dirHasSuffix(dir, ".db.tmp") {
		t.Error("temporary copy removed")
	}
	if dirHasSuffix(dir, ".zip.tmp") {
		t.Error("temporary ZIP removed")
	}
}

func TestTheBackupFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}

	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	target := filepath.Join(temp, "owner-existing.zip")
	oldTarget, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("create old target: %v", err)
	}
	if err := oldTarget.Chmod(0o644); err != nil {
		t.Fatalf("chmod old target: %v", err)
	}
	if _, err := oldTarget.WriteString("old"); err != nil {
		t.Fatalf("write old target: %v", err)
	}
	if err := oldTarget.Close(); err != nil {
		t.Fatalf("close old target: %v", err)
	}

	if _, err := NewService(dir, "0.3.4").CreateBackup(d, target); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if mode := fileMode(t, target); mode != 0o600 {
		t.Errorf("target mode = %04o, want 0600", mode)
	}
}

func TestTheBackupsDatabaseEntryIsEncrypted(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	backup := filepath.Join(temp, "encrypted.zip")
	if _, err := NewService(dir, "0.3.4").CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	bytes := zipEntryBytes(t, backup, DBName)
	if len(bytes) < 16 {
		t.Fatalf("database entry has %d bytes, want at least 16", len(bytes))
	}
	if string(bytes[:16]) == "SQLite format 3\x00" {
		t.Error("the backup database must stay encrypted")
	}

	extracted := filepath.Join(temp, "extracted.db")
	if err := os.WriteFile(extracted, bytes, 0o600); err != nil {
		t.Fatalf("write extracted database: %v", err)
	}

	opened, err := db.Open(extracted, passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open with the right passphrase: %v", err)
	}
	_ = opened.Close()

	if _, err := db.Open(extracted, passphrase(t, "another very long passphrase")); !errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
		t.Errorf("a different passphrase must not open the backup database, got %v", err)
	}
}

func TestABackupAfterAPassphraseChangeOnTheSameConnectionUsesTheNewKey(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	newPassphrase := passphrase(t, "another very long passphrase")
	if err := d.ChangePassphrase(newPassphrase); err != nil {
		t.Fatalf("ChangePassphrase: %v", err)
	}

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "after-change.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	extracted := filepath.Join(temp, "after-change.db")
	if err := os.WriteFile(extracted, zipEntryBytes(t, backup, DBName), 0o600); err != nil {
		t.Fatalf("write extracted database: %v", err)
	}
	opened, err := db.Open(extracted, passphrase(t, "another very long passphrase"))
	if err != nil {
		t.Fatalf("db.Open with the new passphrase: %v", err)
	}
	_ = opened.Close()

	if _, err := db.Open(extracted, passphrase(t, fixturePass)); !errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
		t.Errorf("the old passphrase must not open the backup database, got %v", err)
	}

	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	if !service.HasPendingRestore() {
		t.Error("a restore should be pending")
	}
}

func TestABackupMadeBeforeAPassphraseChangeAsksForTheOldOne(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "old-passphrase.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	newPassphrase := passphrase(t, "another very long passphrase")
	if err := d.ChangePassphrase(newPassphrase); err != nil {
		t.Fatalf("ChangePassphrase: %v", err)
	}

	if _, err := service.StageRestore(d, backup, nil); err == nil {
		t.Fatal("stage must ask for a passphrase")
	} else {
		requirePassphraseRequired(t, err, false)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves nothing behind")
	}

	if _, err := service.StageRestore(d, backup, passphrase(t, wrongPass)); err == nil {
		t.Fatal("stage must reject a wrong passphrase")
	} else {
		requirePassphraseRequired(t, err, true)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves nothing behind")
	}

	info, err := service.StageRestore(d, backup, passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("StageRestore with the right passphrase: %v", err)
	}
	if info.Transactions != 0 {
		t.Errorf("transactions = %d, want 0", info.Transactions)
	}
	if !service.HasPendingRestore() {
		t.Error("a restore should be pending")
	}
}

func TestStagingDoesNotChangeTheLiveDatabase(t *testing.T) {
	temp, dir, d := profile(t)
	accountID := createAccount(t, d, "Account")
	addTransaction(t, d, accountID, "Before backup", "before")

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "unchanged.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}

	if names := partnerNames(t, d); len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partner names = %v, want [Before backup]", names)
	}
	if count := countTransactions(t, d); count != 1 {
		t.Errorf("transactions = %d, want 1", count)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	reopened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if names := partnerNames(t, reopened); len(names) != 1 || names[0] != "Before backup" {
		t.Errorf("partner names = %v, want [Before backup]", names)
	}
}

func TestAStagedDatabaseWithANewerSchemaIsRejected(t *testing.T) {
	temp, dir, d := profile(t)
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "PRAGMA user_version = 99")
		return err
	})
	if err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "too-new.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if _, err := service.StageRestore(d, backup, nil); !errors.Is(err, ErrTooNew) {
		t.Fatalf("StageRestore error = %v, want ErrTooNew", err)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves nothing behind")
	}
}

func TestATargetPathWithASingleQuoteWorks(t *testing.T) {
	temp, dir, d := profile(t)
	accountID := createAccount(t, d, "Account")
	addTransaction(t, d, accountID, "Before backup", "before")

	target := filepath.Join(temp, "O'Brien backup.zip")
	info, err := NewService(dir, "0.3.4").CreateBackup(d, target)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if info.Transactions != 1 {
		t.Errorf("transactions = %d, want 1", info.Transactions)
	}
	if !isRegularFile(target) {
		t.Error("target is not a file")
	}
	inspected, err := NewService(dir, "0.3.4").Inspect(target)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspected.Transactions != 1 {
		t.Errorf("inspected transactions = %d, want 1", inspected.Transactions)
	}
}

func TestSymlinksInContentDirectoriesAreSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlinks")
	}

	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}

	outside := filepath.Join(temp, "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(documents, "link.txt")); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(documents, "regular.txt"), []byte("regular"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	target := filepath.Join(temp, "symlink.zip")
	info, err := NewService(dir, "0.3.4").CreateBackup(d, target)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if info.Documents != 1 {
		t.Errorf("documents = %d, want 1", info.Documents)
	}
	entries := zipNames(t, target)
	if !entries["documents/regular.txt"] {
		t.Error("regular.txt should be packed")
	}
	if entries["documents/link.txt"] {
		t.Error("link.txt must not be packed")
	}
}

func TestAFailedBackupLeavesNoTargetAndNoTempZip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}

	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	bad := filepath.Join(documents, "unreadable.pdf")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatalf("write unreadable file: %v", err)
	}

	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatalf("chmod unreadable file: %v", err)
	}
	if _, err := os.ReadFile(bad); err == nil { // #nosec G304 -- test temporary path.
		// Running as root: file permissions do not fail, so there is nothing to
		// assert.
		_ = os.Chmod(bad, 0o600)
		return
	}

	target := filepath.Join(temp, "fail.zip")
	_, result := NewService(dir, "0.3.4").CreateBackup(d, target)
	_ = os.Chmod(bad, 0o600)

	if result == nil {
		t.Fatal("CreateBackup should fail")
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target must not exist on failure: %v", err)
	}
	if dirHasSuffix(filepath.Dir(target), ".zip.tmp") {
		t.Error("no temp ZIP left behind")
	}
}

func TestCredentialsAreNeitherBackedUpNorMovedByARestore(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	credentials := filepath.Join(dir, "credentials")
	if err := os.MkdirAll(credentials, 0o700); err != nil {
		t.Fatalf("MkdirAll credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(credentials, "bank-1"), []byte("remembered-pin-marker"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	service := NewService(dir, "0.3.4")
	backup := filepath.Join(temp, "credentials.zip")
	if _, err := service.CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	for name := range zipNames(t, backup) {
		if strings.HasPrefix(name, "credentials/") {
			t.Errorf("credentials must not enter a backup: %s", name)
		}
	}

	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	previous, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if !applied {
		t.Fatal("a restore was pending")
	}

	content, err := os.ReadFile(filepath.Join(credentials, "bank-1")) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if string(content) != "remembered-pin-marker" {
		t.Error("credentials stay in the live profile folder")
	}
	if _, err := os.Stat(filepath.Join(previous, "credentials")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("credentials must not be moved into a pre-restore folder")
	}
}

func TestTheManifestIsReadLeniently(t *testing.T) {
	temp, dir, _ := profile(t)
	service := NewService(dir, "0.3.4")

	backup := filepath.Join(temp, "manifest.zip")
	writeTestZip(t, backup, []zipWriteEntry{
		{
			name: Manifest,
			data: []byte("# Finglass backup\ncreated=2026-01-01T00:00:00Z\nappVersion=9.9.9\n" +
				"transactions=?\ndocuments=not-a-number\n"),
		},
		{name: DBName, data: []byte("x")},
	})

	info, err := service.Inspect(backup)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Created != "2026-01-01T00:00:00Z" {
		t.Errorf("created = %q", info.Created)
	}
	if info.AppVersion != "9.9.9" {
		t.Errorf("appVersion = %q", info.AppVersion)
	}
	if info.Transactions != 0 {
		t.Errorf("transactions = %d, want 0", info.Transactions)
	}
	if info.Documents != 0 {
		t.Errorf("documents = %d, want 0", info.Documents)
	}
}

func TestReadsRustBackup(t *testing.T) {
	// The live database is the one inside the fixture, copied out first. SQLCipher's
	// `ATTACH DATABASE` without a `KEY` clause inherits the main database's salt, so
	// this is the live database a same-passphrase backup can be staged into with a
	// nil passphrase.
	backup := copyFixture(t, "rust-backup.zip")

	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, DBName), zipEntryBytes(t, backup, DBName), 0o600); err != nil {
		t.Fatalf("write live database: %v", err)
	}
	d, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = d.Close() }()

	service := NewService(dir, "0.3.4")

	info, err := service.Inspect(backup)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.AppVersion != "0.3.4" {
		t.Errorf("appVersion = %q, want 0.3.4", info.AppVersion)
	}
	if info.Transactions != 2 {
		t.Errorf("transactions = %d, want 2", info.Transactions)
	}
	if info.Documents != 2 {
		t.Errorf("documents = %d, want 2", info.Documents)
	}
	if _, err := time.Parse(time.RFC3339, info.Created); err != nil {
		t.Errorf("created %q is not RFC 3339: %v", info.Created, err)
	}

	if _, err := service.StageRestore(d, backup, nil); err != nil {
		t.Fatalf("StageRestore: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	previous, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if !applied {
		t.Fatal("a restore was pending")
	}
	if !pathExists(previous) {
		t.Errorf("previous folder %q does not exist", previous)
	}

	reopened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	names := partnerNames(t, reopened)
	if len(names) != 2 || names[0] != "Bakery" || names[1] != "Employer" {
		t.Errorf("partner names = %v, want [Bakery Employer]", names)
	}

	documents := filepath.Join(dir, "documents")
	receipt, err := os.ReadFile(filepath.Join(documents, "a-receipt.txt")) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read a-receipt.txt: %v", err)
	}
	if string(receipt) != "receipt text\n" {
		t.Errorf("a-receipt.txt = %q", receipt)
	}
	scan, err := os.ReadFile(filepath.Join(documents, "b-scan.pdf")) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read b-scan.pdf: %v", err)
	}
	if string(scan) != "%PDF-1.7 probe\n" {
		t.Errorf("b-scan.pdf = %q", scan)
	}
}

// TestRustBackupIntoAFreshProfile is the restore on a new machine: a fresh
// database with the same passphrase has another KDF salt, so the live key
// cannot read the backup and the passphrase is asked for, exactly as Rust
// 0.3.4 does (checked with testdata/rust_probe.rs.txt).
func TestRustBackupIntoAFreshProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, err := db.Create(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	defer func() { _ = d.Close() }()

	service := NewService(dir, "0.3.4")
	backup := copyFixture(t, "rust-backup.zip")

	_, err = service.StageRestore(d, backup, nil)
	var required *PassphraseRequiredError
	if !errors.As(err, &required) || required.WrongPassphraseGiven {
		t.Fatalf("StageRestore without a passphrase = %v, want PassphraseRequired{false}", err)
	}
	if service.HasPendingRestore() {
		t.Fatal("a failed stage leaves nothing behind")
	}

	if _, err := service.StageRestore(d, backup, passphrase(t, fixturePass)); err != nil {
		t.Fatalf("StageRestore with the passphrase: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	if _, applied, err := service.ApplyPendingRestore(time.Now()); err != nil || !applied {
		t.Fatalf("ApplyPendingRestore = %v, %v", applied, err)
	}

	reopened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	names := partnerNames(t, reopened)
	if len(names) != 2 || names[0] != "Bakery" || names[1] != "Employer" {
		t.Errorf("partner names = %v, want [Bakery Employer]", names)
	}
}

func TestRustBackupWithAnotherPassphrase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, err := db.Create(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	defer func() { _ = d.Close() }()

	service := NewService(dir, "0.3.4")
	backup := copyFixture(t, "rust-backup-other-passphrase.zip")

	if _, err := service.StageRestore(d, backup, nil); err == nil {
		t.Fatal("stage must ask for a passphrase")
	} else {
		requirePassphraseRequired(t, err, false)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves nothing behind")
	}

	if _, err := service.StageRestore(d, backup, passphrase(t, wrongPass)); err == nil {
		t.Fatal("stage must reject a wrong passphrase")
	} else {
		requirePassphraseRequired(t, err, true)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves nothing behind")
	}

	if _, err := service.StageRestore(d, backup, passphrase(t, otherPass)); err != nil {
		t.Fatalf("StageRestore with the backup's passphrase: %v", err)
	}
	if !service.HasPendingRestore() {
		t.Fatal("a restore should be pending")
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close live database: %v", err)
	}
	previous, applied, err := service.ApplyPendingRestore(time.Now())
	if err != nil {
		t.Fatalf("ApplyPendingRestore: %v", err)
	}
	if !applied {
		t.Fatal("a restore was pending")
	}
	if !pathExists(previous) {
		t.Errorf("previous folder %q does not exist", previous)
	}

	opened, err := db.Open(filepath.Join(dir, DBName), passphrase(t, otherPass))
	if err != nil {
		t.Fatalf("db.Open with the backup's passphrase: %v", err)
	}
	_ = opened.Close()

	if _, err := db.Open(filepath.Join(dir, DBName), passphrase(t, fixturePass)); !errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
		t.Errorf("the live passphrase must not open the restored database, got %v", err)
	}
}

func TestBackupLayoutMatchesRust(t *testing.T) {
	temp, dir, d := profile(t)

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	accountID := createAccount(t, d, "Probe account")
	addTransactionAt(t, d, accountID, "Bakery", "probe-1", -350, 2026, time.January, 2)
	addTransactionAt(t, d, accountID, "Employer", "probe-2", 250_000, 2026, time.January, 2)
	if err := os.WriteFile(filepath.Join(documents, "a-receipt.txt"), []byte("receipt text\n"), 0o600); err != nil {
		t.Fatalf("write a-receipt.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "b-scan.pdf"), []byte("%PDF-1.7 probe\n"), 0o600); err != nil {
		t.Fatalf("write b-scan.pdf: %v", err)
	}

	goBackup := filepath.Join(temp, "go-backup.zip")
	info, err := NewService(dir, "0.3.4").CreateBackup(d, goBackup)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if info.Transactions != 2 {
		t.Errorf("transactions = %d, want 2", info.Transactions)
	}
	if info.Documents != 2 {
		t.Errorf("documents = %d, want 2", info.Documents)
	}

	rustBackup := filepath.Join("testdata", "rust-backup.zip")
	goNames := orderedZipNames(t, goBackup)
	rustNames := orderedZipNames(t, rustBackup)
	if len(goNames) != len(rustNames) {
		t.Fatalf("entry count = %d, want %d (%v vs %v)", len(goNames), len(rustNames), goNames, rustNames)
	}
	for index := range goNames {
		if goNames[index] != rustNames[index] {
			t.Errorf("entry %d = %q, want %q", index, goNames[index], rustNames[index])
		}
	}

	goFiles := openZip(t, goBackup).File
	rustFiles := openZip(t, rustBackup).File
	for index := range goFiles {
		if goFiles[index].Method != zip.Deflate {
			t.Errorf("%q method = %d, want Deflate", goFiles[index].Name, goFiles[index].Method)
		}
		if rustFiles[index].Method != zip.Deflate {
			t.Errorf("Rust %q method = %d, want Deflate", rustFiles[index].Name, rustFiles[index].Method)
		}
		if mode := goFiles[index].Mode().Perm(); mode != 0o600 {
			t.Errorf("%q mode = %04o, want 0600", goFiles[index].Name, mode)
		}
		if mode := rustFiles[index].Mode().Perm(); mode != 0o600 {
			t.Errorf("Rust %q mode = %04o, want 0600", rustFiles[index].Name, mode)
		}
	}

	goManifest := strings.Split(string(zipEntryBytes(t, goBackup, Manifest)), "\n")
	rustManifest := strings.Split(string(zipEntryBytes(t, rustBackup, Manifest)), "\n")
	if len(goManifest) != len(rustManifest) {
		t.Fatalf("manifest line count = %d, want %d", len(goManifest), len(rustManifest))
	}
	for index := range goManifest {
		if strings.HasPrefix(goManifest[index], "created=") {
			if !strings.HasPrefix(rustManifest[index], "created=") {
				t.Errorf("manifest line %d = %q, want a created line", index, rustManifest[index])
			}
			continue
		}
		if goManifest[index] != rustManifest[index] {
			t.Errorf("manifest line %d = %q, want %q", index, goManifest[index], rustManifest[index])
		}
	}

	for _, name := range []string{"documents/a-receipt.txt", "documents/b-scan.pdf"} {
		if got, want := zipEntryBytes(t, goBackup, name), zipEntryBytes(t, rustBackup, name); string(got) != string(want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestBackupDatabaseEntryIsEncrypted(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	backup := filepath.Join(temp, "encrypted-additional.zip")
	if _, err := NewService(dir, "0.3.4").CreateBackup(d, backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	bytes := zipEntryBytes(t, backup, DBName)
	if len(bytes) < 16 {
		t.Fatalf("database entry has %d bytes, want at least 16", len(bytes))
	}
	if string(bytes[:16]) == "SQLite format 3\x00" {
		t.Error("the backup database entry must stay encrypted")
	}

	extracted := filepath.Join(temp, "encrypted-additional.db")
	if err := os.WriteFile(extracted, bytes, 0o600); err != nil {
		t.Fatalf("write extracted database: %v", err)
	}
	if _, err := db.Open(extracted, passphrase(t, wrongPass)); !errors.Is(err, db.ErrWrongPassphraseOrDamaged) {
		t.Errorf("a wrong passphrase must not open the backup database, got %v", err)
	}
}

func TestUnsafeEntryNames(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name string
		safe bool
	}{
		{"finglass.db", true},
		{"documents/a", true},
		{"documents/a.txt", true},
		{"documents\\a", true},
		{"../escape.txt", false},
		{"/abs.txt", false},
		{"documents/../../escape.txt", false},
		{"documents/a/b.txt", false},
		{"other.txt", false},
		{"..\\escape.txt", false},
		{"..", false},
		{"/abs", false},
		{"documents/../../x", false},
		{"documents\\..\\x", false},
		{"documents\\\\a", false},
		{"documents/a/b", false},
		{"documents/", false},
		{"documents/.", false},
		{"C:/x", false},
		{"finglass.db/x", false},
	}

	for _, testCase := range cases {
		target, err := safeTarget(root, testCase.name)
		if !testCase.safe {
			var unsafeErr *UnsafePathError
			if !errors.As(err, &unsafeErr) {
				t.Errorf("safeTarget(%q) error = %v, want UnsafePathError", testCase.name, err)
				continue
			}
			if unsafeErr.Name != testCase.name {
				t.Errorf("UnsafePathError.Name = %q, want the original %q", unsafeErr.Name, testCase.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("safeTarget(%q) = %v, want a safe target", testCase.name, err)
			continue
		}
		normalized := strings.ReplaceAll(testCase.name, "\\", "/")
		if target != filepath.Join(root, normalized) {
			t.Errorf("safeTarget(%q) = %q, want %q", testCase.name, target, filepath.Join(root, normalized))
		}
	}

	// A symlink-mode entry is staged as a regular file.
	temp, dir, d := profile(t)
	service := NewService(dir, "0.3.4")

	validBackup := filepath.Join(temp, "valid.zip")
	if _, err := service.CreateBackup(d, validBackup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	database := zipEntryBytes(t, validBackup, DBName)

	symlinkZip := filepath.Join(temp, "symlink-entry.zip")
	writeTestZip(t, symlinkZip, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: database},
		{name: "documents/link", data: []byte("link-target"), mode: os.ModeSymlink | 0o777},
	})
	if _, err := service.StageRestore(d, symlinkZip, nil); err != nil {
		t.Fatalf("StageRestore(symlink entry): %v", err)
	}
	stagedLink := filepath.Join(dir, PendingDir, "documents", "link")
	linkInfo, err := os.Lstat(stagedLink)
	if err != nil {
		t.Fatalf("Lstat staged link: %v", err)
	}
	if !linkInfo.Mode().IsRegular() {
		t.Errorf("staged link mode = %v, want a regular file", linkInfo.Mode())
	}
	linkBytes, err := os.ReadFile(stagedLink) // #nosec G304 -- test temporary path.
	if err != nil {
		t.Fatalf("read staged link: %v", err)
	}
	if string(linkBytes) != "link-target" {
		t.Errorf("staged link = %q, want link-target", linkBytes)
	}
	service.CancelPendingRestore()

	evilZip := filepath.Join(temp, "evil-entry.zip")
	writeTestZip(t, evilZip, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: database},
		{name: "../evil", data: []byte("pwned")},
	})
	_, err = service.StageRestore(d, evilZip, nil)
	var unsafeErr *UnsafePathError
	if !errors.As(err, &unsafeErr) {
		t.Fatalf("StageRestore(../evil) error = %v, want UnsafePathError", err)
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves no staging folder")
	}
	for _, outside := range []string{
		filepath.Join(dir, "evil"),
		filepath.Join(filepath.Dir(dir), "evil"),
	} {
		if _, err := os.Stat(outside); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s must not be written", outside)
		}
	}
}

type rawZipEntry struct {
	name     string
	data     []byte
	declared uint64
}

func writeRawZip(t *testing.T, path string, entries []rawZipEntry) {
	t.Helper()

	file, err := os.Create(path) // #nosec G304 -- path is a test temporary file.
	if err != nil {
		t.Fatalf("create raw zip: %v", err)
	}
	writer := zip.NewWriter(file)

	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:               entry.name,
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE(entry.data),
			CompressedSize64:   uint64(len(entry.data)),
			UncompressedSize64: entry.declared,
		}
		destination, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatalf("create raw zip entry: %v", err)
		}
		if _, err := destination.Write(entry.data); err != nil {
			t.Fatalf("write raw zip entry: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close raw zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close raw zip file: %v", err)
	}
}

func TestSizeLimitsCountDecompressedBytes(t *testing.T) {
	t.Run("entry header lies", func(t *testing.T) {
		temp, dir, d := profile(t)

		realData := make([]byte, 50_000)
		for index := range realData {
			realData[index] = 'z'
		}

		// Build one archive with normal entries and one lying raw entry.
		backup := filepath.Join(temp, "lying-entry.zip")
		file, err := os.Create(backup) // #nosec G304 -- path is a test temporary file.
		if err != nil {
			t.Fatalf("create zip: %v", err)
		}
		writer := zip.NewWriter(file)
		for _, entry := range []zipWriteEntry{
			{name: Manifest, data: []byte("created=x")},
			{name: DBName, data: []byte("x")},
		} {
			header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
			destination, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatalf("create zip entry: %v", err)
			}
			if _, err := destination.Write(entry.data); err != nil {
				t.Fatalf("write zip entry: %v", err)
			}
		}
		header := &zip.FileHeader{
			Name:               "documents/bomb.bin",
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE(realData),
			CompressedSize64:   uint64(len(realData)),
			UncompressedSize64: 10_000,
		}
		destination, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatalf("create raw zip entry: %v", err)
		}
		if _, err := destination.Write(realData); err != nil {
			t.Fatalf("write raw zip entry: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close zip: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close zip file: %v", err)
		}

		service := NewService(dir, "0.3.4").WithMaxRestoreBytes(8_000)
		if _, err := service.StageRestore(d, backup, nil); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("StageRestore error = %v, want ErrTooLarge", err)
		}
		if service.HasPendingRestore() {
			t.Error("a failed stage leaves nothing behind")
		}
	})

	t.Run("total budget", func(t *testing.T) {
		temp, dir, d := profile(t)

		first := make([]byte, 600)
		for index := range first {
			first[index] = 'a'
		}
		second := make([]byte, 600)
		for index := range second {
			second[index] = 'b'
		}

		backup := filepath.Join(temp, "budget.zip")
		writeRawZip(t, backup, []rawZipEntry{
			{name: Manifest, data: []byte("created=x"), declared: uint64(len("created=x"))},
			{name: DBName, data: []byte("x"), declared: 1},
			{name: "documents/a.bin", data: first, declared: uint64(len(first))},
			{name: "documents/b.bin", data: second, declared: uint64(len(second))},
		})

		service := NewService(dir, "0.3.4").WithMaxRestoreBytes(1_000)
		if _, err := service.StageRestore(d, backup, nil); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("StageRestore error = %v, want ErrTooLarge", err)
		}
		if service.HasPendingRestore() {
			t.Error("a failed stage leaves nothing behind")
		}
	})
}

func assertNoPassphrase(t *testing.T, err error, secrets ...string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected an error")
	}
	texts := []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err)}
	for _, value := range secrets {
		for _, text := range texts {
			if strings.Contains(text, value) {
				t.Fatal("an error text reveals a passphrase")
			}
		}
	}
}

func TestErrorsNeverContainThePassphrase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, err := db.Create(filepath.Join(dir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	defer func() { _ = d.Close() }()

	service := NewService(dir, "0.3.4")
	otherBackup := backupWithPassphrase(t, otherPass)

	if _, err := service.StageRestore(d, otherBackup, nil); err == nil {
		t.Fatal("stage must ask for a passphrase")
	} else {
		assertNoPassphrase(t, err, fixturePass, otherPass, wrongPass)
		requirePassphraseRequired(t, err, false)
	}

	if _, err := service.StageRestore(d, otherBackup, passphrase(t, wrongPass)); err == nil {
		t.Fatal("stage must reject a wrong passphrase")
	} else {
		assertNoPassphrase(t, err, fixturePass, otherPass, wrongPass)
		requirePassphraseRequired(t, err, true)
	}

	tooNewDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(tooNewDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	tooNew, err := db.Create(filepath.Join(tooNewDir, DBName), passphrase(t, fixturePass))
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	defer func() { _ = tooNew.Close() }()
	err = tooNew.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "PRAGMA user_version = 99")
		return err
	})
	if err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	tooNewService := NewService(tooNewDir, "0.3.4")
	tooNewBackup := filepath.Join(t.TempDir(), "too-new.zip")
	if _, err := tooNewService.CreateBackup(tooNew, tooNewBackup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if _, err := tooNewService.StageRestore(tooNew, tooNewBackup, passphrase(t, fixturePass)); err == nil {
		t.Fatal("stage must reject a newer schema")
	} else {
		if !errors.Is(err, ErrTooNew) {
			t.Fatalf("error = %v, want ErrTooNew", err)
		}
		assertNoPassphrase(t, err, fixturePass, otherPass, wrongPass)
	}
}

func TestFailedStageAndBackupLeaveNothing(t *testing.T) {
	temp, dir, d := profile(t)
	createAccount(t, d, "Account")

	documents := filepath.Join(dir, "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("MkdirAll documents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documents, "receipt.pdf"), []byte("pdf-bytes"), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	targetDir := filepath.Join(temp, "targets")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("MkdirAll targets: %v", err)
	}
	target := filepath.Join(targetDir, "backup.zip")

	service := NewService(dir, "0.3.4").WithMaxRestoreBytes(1)
	if _, err := service.CreateBackup(d, target); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("CreateBackup error = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("target must not exist: %v", err)
	}
	for _, checkDir := range []string{dir, targetDir} {
		if dirHasSuffix(checkDir, ".zip.tmp") {
			t.Errorf("a .zip.tmp is left in %s", checkDir)
		}
		if dirHasSuffix(checkDir, ".db.tmp") {
			t.Errorf("a .db.tmp is left in %s", checkDir)
		}
	}

	corrupt := filepath.Join(temp, "corrupt.zip")
	writeTestZip(t, corrupt, []zipWriteEntry{
		{name: Manifest, data: []byte("created=x")},
		{name: DBName, data: []byte("not a database")},
	})
	if _, err := service.StageRestore(d, corrupt, nil); err == nil {
		t.Fatal("a corrupt backup must be rejected")
	}
	if service.HasPendingRestore() {
		t.Error("a failed stage leaves no pending folder")
	}
}
