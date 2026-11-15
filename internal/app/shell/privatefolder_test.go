package shell

import (
	"context"
	"errors"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func TestAFailingDataFolderSecureKeepsStartupGoingAndNotesOnce(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")

	previous := secureDataRoot
	secureDataRoot = func(string) error { return errors.New("the folder cannot hold a DACL") }
	t.Cleanup(func() { secureDataRoot = previous })

	session, offer, refusal := Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("Start refused: %+v", refusal)
	}
	if offer != nil {
		t.Fatal("Start offered a migration without a FinDesk root")
	}
	t.Cleanup(func() { _ = session.Shutdown() })

	session.mu.Lock()
	pending := session.privateFolderUnsecured
	session.mu.Unlock()
	if !pending {
		t.Fatal("the failed SecureDir was not recorded")
	}

	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)

	session.mu.Lock()
	settings := session.settings
	pending = session.privateFolderUnsecured
	session.mu.Unlock()
	if pending {
		t.Fatal("the note was not consumed by the first unlock")
	}
	if settings == nil || settings.Message == nil {
		t.Fatal("the unsecured-folder note was not shown")
	}
	if want := i18n.Text(i18n.En, i18n.PrivateFolderNotSecured); *settings.Message != want {
		t.Fatalf("note = %q, want %q", *settings.Message, want)
	}

	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Action == "PRIVATE_FOLDER_NOT_SECURED" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("private-folder audit entries = %d, want 1", count)
	}

	// The next unlock in this same start must not show the note again.
	session.Teardown(startup.Startup{})
	waitForJobs(t, session)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("second unlock: %v", err)
	}
	waitForJobs(t, session)

	session.mu.Lock()
	settings = session.settings
	session.mu.Unlock()
	if settings != nil && settings.Message != nil {
		t.Fatalf("the note appeared again: %q", *settings.Message)
	}

	entries, err = audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries after the second unlock: %v", err)
	}
	count = 0
	for _, entry := range entries {
		if entry.Action == "PRIVATE_FOLDER_NOT_SECURED" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("private-folder audit entries after the second unlock = %d, want 1", count)
	}
}
