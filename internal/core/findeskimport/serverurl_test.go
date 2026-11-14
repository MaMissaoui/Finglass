package findeskimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const lookedUpServerURL = "https://fints.example.test/looked-up"

func updateSource(t *testing.T, sourceDir, statement string) {
	t.Helper()
	path := filepath.Join(sourceDir, "findesk.db")
	database, err := db.OpenUnmigrated(path, passphrase(fixturePassphrase, secret.Unlock))
	if err != nil {
		t.Fatalf("open source copy: %v", err)
	}
	defer func() {
		_ = database.Close()
	}()
	if err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, statement)
		return err
	}); err != nil {
		t.Fatalf("update source copy: %v", err)
	}
}

func lookupForTests(blz string) (string, bool) {
	if blz == "11112222" {
		return lookedUpServerURL, true
	}
	return "", false
}

func TestALookupFillsOnlyEmptyServerURLs(t *testing.T) {
	sourceDir := fixtureCopy(t)
	updateSource(t, sourceDir, "UPDATE bank_access SET blz = '11112222', server_url = NULL WHERE id = 1")
	target, documentsDir := newTarget(t)

	report, err := Import(
		target,
		documentsDir,
		sourceDir,
		passphrase(fixturePassphrase, secret.Unlock),
		offset(),
		lookupForTests,
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.ServerURLsFilled != 1 {
		t.Errorf("filled = %d, want 1", report.ServerURLsFilled)
	}
	if report.BankConnectionsWithoutServerURL != 0 {
		t.Errorf("missing = %d, want 0", report.BankConnectionsWithoutServerURL)
	}
	if got := optionalText(t, target, "SELECT server_url FROM bank_connections WHERE id = 1"); got == nil || *got != lookedUpServerURL {
		t.Errorf("empty URL = %v, want %q", got, lookedUpServerURL)
	}
	if got := optionalText(t, target, "SELECT server_url FROM bank_connections WHERE id = 2"); got != nil {
		t.Errorf("offline URL = %v, want NULL", got)
	}

	entries, err := audit.Recent(context.Background(), target, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 ||
		!strings.Contains(entries[0].Details, "server_urls_filled=1") ||
		!strings.Contains(entries[0].Details, "bank_connections_without_server_url=0") {
		t.Errorf("audit details = %+v", entries)
	}
}

func TestAStoredServerURLIsNeverReplaced(t *testing.T) {
	sourceDir := fixtureCopy(t)
	target, documentsDir := newTarget(t)

	report, err := Import(
		target,
		documentsDir,
		sourceDir,
		passphrase(fixturePassphrase, secret.Unlock),
		offset(),
		lookupForTests,
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.ServerURLsFilled != 0 {
		t.Errorf("filled = %d, want 0", report.ServerURLsFilled)
	}
	if report.BankConnectionsWithoutServerURL != 0 {
		t.Errorf("missing = %d, want 0", report.BankConnectionsWithoutServerURL)
	}
	if got := optionalText(t, target, "SELECT server_url FROM bank_connections WHERE id = 1"); got == nil ||
		*got != "https://banking.example.invalid/fints" {
		t.Errorf("stored URL = %v, want the FinDesk URL", got)
	}
}

func TestWithoutALookupEmptyURLsStayEmptyAndAreCounted(t *testing.T) {
	sourceDir := fixtureCopy(t)
	updateSource(t, sourceDir, "UPDATE bank_access SET server_url = NULL WHERE id = 1")
	target, documentsDir := newTarget(t)

	report, err := Import(
		target,
		documentsDir,
		sourceDir,
		passphrase(fixturePassphrase, secret.Unlock),
		offset(),
		nil,
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.ServerURLsFilled != 0 {
		t.Errorf("filled = %d, want 0", report.ServerURLsFilled)
	}
	if report.BankConnectionsWithoutServerURL != 1 {
		t.Errorf("missing = %d, want 1 (the offline bank does not count)", report.BankConnectionsWithoutServerURL)
	}
	if got := optionalText(t, target, "SELECT server_url FROM bank_connections WHERE id = 1"); got != nil {
		t.Errorf("URL = %v, want NULL", got)
	}

	entries, err := audit.Recent(context.Background(), target, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 ||
		!strings.Contains(entries[0].Details, "server_urls_filled=0") ||
		!strings.Contains(entries[0].Details, "bank_connections_without_server_url=1") {
		t.Errorf("audit details = %+v", entries)
	}
}

func TestAnOfflineBankGetsAURLAndCountsAsFilledOnly(t *testing.T) {
	sourceDir := fixtureCopy(t)
	updateSource(
		t,
		sourceDir,
		"UPDATE bank_access SET blz = '11112222', server_url = NULL WHERE id = 2",
	)
	target, documentsDir := newTarget(t)

	report, err := Import(
		target,
		documentsDir,
		sourceDir,
		passphrase(fixturePassphrase, secret.Unlock),
		offset(),
		lookupForTests,
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.ServerURLsFilled != 1 {
		t.Errorf("filled = %d, want 1", report.ServerURLsFilled)
	}
	if report.BankConnectionsWithoutServerURL != 0 {
		t.Errorf("missing = %d, want 0", report.BankConnectionsWithoutServerURL)
	}
	if got := optionalText(t, target, "SELECT server_url FROM bank_connections WHERE id = 2"); got == nil || *got != lookedUpServerURL {
		t.Errorf("offline URL = %v, want %q", got, lookedUpServerURL)
	}
}
