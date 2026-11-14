package migration

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/db/kdftest"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func blankFirstBankServerURL(t *testing.T, sourceDir string) {
	t.Helper()
	database, err := db.OpenUnmigrated(
		filepath.Join(sourceDir, "findesk.db"),
		findeskPassphrase(t),
	)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() {
		_ = database.Close()
	}()
	if err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "UPDATE bank_access SET blz = '11112222', server_url = NULL WHERE id = 1")
		return err
	}); err != nil {
		t.Fatalf("blank server URL: %v", err)
	}
}

func syntheticFindDeskApp(t *testing.T, blz, serverURL string) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "FinDesk.app")
	appDir := filepath.Join(app, "Contents", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	jar, err := os.Create(filepath.Join(appDir, "hbci4j-core-test.jar")) // #nosec G304 -- test temporary directory.
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}
	writer := zip.NewWriter(jar)
	entry, err := writer.Create("blz.properties")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	line := fmt.Sprintf("%s=Fixture Bank|Town|Bank|X|Extra|%s\n", blz, serverURL)
	if _, err := io.WriteString(entry, "# invented\n"+line); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close jar writer: %v", err)
	}
	if err := jar.Close(); err != nil {
		t.Fatalf("close jar: %v", err)
	}
	return app
}

func TestTheMigrationFillsAServerURLFromTheInstalledFinDeskApp(t *testing.T) {
	kdftest.RealKDF(t)

	root := fixtureCopy(t)
	blankFirstBankServerURL(t, root)

	app := syntheticFindDeskApp(t, "11112222", "https://fints.example.test/from-findesk")
	previous := findeskAppPaths
	findeskAppPaths = func() []string { return []string{app} }
	t.Cleanup(func() { findeskAppPaths = previous })

	install := Discover(root)
	if install == nil || len(install.Profiles) == 0 {
		t.Fatal("no install found")
	}
	target := testPaths(t)
	migrated, err := MigrateProfile(
		target,
		&install.Profiles[0],
		findeskPassphrase(t),
		targetNewPassphrase(t),
		offset(),
	)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migrated.Report.ServerURLsFilled != 1 {
		t.Errorf("filled = %d, want 1", migrated.Report.ServerURLsFilled)
	}
	summary := migrated.Report.Summary(i18n.En)
	if !strings.Contains(summary, "taken from FinDesk's bank list.") {
		t.Errorf("summary = %q", summary)
	}

	file, err := target.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	opened, err := db.Open(file, targetNewPassphrase(t))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer func() {
		_ = opened.Close()
	}()
	var serverURL *string
	if err := opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT server_url FROM bank_connections WHERE id = 1").Scan(&serverURL)
	}); err != nil {
		t.Fatalf("read server URL: %v", err)
	}
	if serverURL == nil || *serverURL != "https://fints.example.test/from-findesk" {
		t.Errorf("stored URL = %v", serverURL)
	}
}

func TestTheMigrationSummaryNamesBanksWithoutAServerURL(t *testing.T) {
	kdftest.RealKDF(t)

	root := fixtureCopy(t)
	blankFirstBankServerURL(t, root)

	previous := findeskAppPaths
	findeskAppPaths = func() []string { return nil }
	t.Cleanup(func() { findeskAppPaths = previous })

	install := Discover(root)
	if install == nil || len(install.Profiles) == 0 {
		t.Fatal("no install found")
	}
	target := testPaths(t)
	migrated, err := MigrateProfile(
		target,
		&install.Profiles[0],
		findeskPassphrase(t),
		targetNewPassphrase(t),
		offset(),
	)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migrated.Report.BankConnectionsWithoutServerURL != 1 {
		t.Errorf("missing = %d, want 1", migrated.Report.BankConnectionsWithoutServerURL)
	}
	summary := migrated.Report.Summary(i18n.En)
	if !strings.Contains(summary, "1 bank has no FinTS server address.") ||
		!strings.Contains(summary, "“Connection settings”") {
		t.Errorf("summary = %q", summary)
	}
}
