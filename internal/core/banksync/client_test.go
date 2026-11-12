// Banking rules from CLAUDE.md and the tests that cover them:
//
//   - A PIN is kept only when the user ticks "remember" and the dialog
//     succeeded: TestRememberingThePinStoresItAndAuditsItWithoutTheSecret,
//     TestWithoutTheOptInNothingIsStored, TestANewPinIsSavedOnlyAfterTheDialogSucceeds,
//     TestAStoreThatCannotBeWrittenDoesNotFailTheSync.
//   - A stored PIN is deleted when the bank rejects it:
//     TestARejectedStoredPinIsDeletedAndTheUserIsAskedAgain,
//     TestCancellingTheRePromptAfterARejectedStoredPinForgetsIt,
//     TestADialogFailureAfterAStoredPinWasUsedForgetsIt,
//     TestACancelWhileTheStoredPinWasFineKeepsIt.
//   - A stored PIN is deleted when the login changes:
//     TestChangingOnlyTheURLResetsTheRegistrationAndForgetsThePin,
//     TestChangingTheUserIDResetsTheRegistrationAndForgetsThePin,
//     TestChangingTheBLZResetsForgetsThePinAndKeepsTheAccounts,
//     TestAFlagForAnotherLoginForgetsTheEntryAndAsksAgain,
//     TestSavingUnchangedValuesResetsNothing.
//   - A stored PIN is deleted when the bank is removed:
//     TestRemovingTheBankForgetsTheStoredPinAndItsFlag,
//     TestRemovingABankWithoutARememberedPinLogsNoForgottenPin,
//     TestDeletingAProfileForgetsThePinsRememberedInIt.
//   - A TAN is never stored: TestTanIsNeverStored,
//     TestRememberingThePinStoresItAndAuditsItWithoutTheSecret.
//   - Bank text is masked when stored and never reaches the audit log:
//     TestBankTextNeverReachesTheAuditLogAndStoredMessagesAreMasked,
//     TestALoanWithoutABalanceIsCreatedAsALoanAndNotedWithoutLeakingTheIBAN,
//     TestAnAccountWithoutAnIBANIsNotedWithoutExposingItsNumber.
//   - PINs and TANs never appear in audit rows, settings or error text:
//     TestPinNeverInAuditOrErrors, TestRememberingInteractionIsSafeForConcurrentUse.
package banksync

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func TestPinStringsArePinned(t *testing.T) {
	bank := model.BankAccessItem{BLZ: "37050198", UserID: "demo-user"}
	assertString(t, PinFlagKey(7), "fints.pinSaved.7")
	assertString(t, LoginSignature(bank), "37050198|demo-user")
	assertString(t, PinMarkerPath("profile", 7), "profile/credentials/bank-7")

	capabilities := BankCapabilities{Jobs: map[string]struct{}{
		JobBalance:         {},
		JobStatementsMT940: {},
	}}
	assertString(t, capabilities.Serialize(), "KUmsAll,SaldoReq")
	assertString(t, PinKey("/data/profile", 7), "/data/profile#bank-7")
}

func TestRememberingThePinStoresItAndAuditsItWithoutTheSecret(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), true))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "12345")
	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = false")
	}
	flag, ok, err := settings.Get(context.Background(), h.db, PinFlagKey(h.bank))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if !ok || flag != "37050198|demo-user" {
		t.Fatalf("flag = %q, %t, want %q", flag, ok, "37050198|demo-user")
	}
	assertInt(t, auditCount(t, h.db, CodePinSaved), 1)
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
	if h.service.PinNotSaved() {
		t.Fatal("pin not saved is true")
	}

	auditText := make([]string, 0)
	for _, entry := range auditEntries(t, h.db, 1000) {
		auditText = append(auditText, entry.Action+" "+entry.Details)
	}
	haystack := report.Summary() + "\n" + strings.Join(auditText, "\n") + "\n" +
		fmt.Sprintf("%v", NewPinEntry(secret.NewText([]byte("12345")), true))
	for _, forbidden := range []string{"12345", "demo-user", giro} {
		if strings.Contains(haystack, forbidden) {
			t.Fatalf("report, audit or Debug leaked %q: %q", forbidden, haystack)
		}
	}
}

func TestAStoredPinAnswersTheNextSyncWithoutAsking(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	second := newScriptedInteraction(stringPointer("99999"), false)

	if _, err := h.sync(t, second); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := second.pinAskedCount(); got != 0 {
		t.Fatalf("pin asked = %d, want 0 (the stored PIN answers the dialog)", got)
	}
	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "12345")
}

func TestWithoutTheOptInNothingIsStored(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestARejectedStoredPinIsDeletedAndTheUserIsAskedAgain(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.gateway.setPinCalls(2)
	second := newScriptedInteraction(stringPointer("54321"), false)

	report, err := h.sync(t, second)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := second.pinAskedCount(); got != 1 {
		t.Fatalf("pin asked = %d, want 1 (only the re-prompt reaches the user)", got)
	}
	if !h.store.IsEmpty() {
		t.Fatal("the rejected PIN is still stored")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
	assertInt(t, auditCount(t, h.db, CodePinForgotten), 1)
}

func TestANewPinIsSavedOnlyAfterTheDialogSucceeds(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.gateway.setPinCalls(2)
	retry := newScriptedInteraction(stringPointer("54321"), true)

	if _, err := h.sync(t, retry); err != nil {
		t.Fatalf("retry sync: %v", err)
	}

	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "54321")
	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = false")
	}

	h.gateway.setPinCalls(1)
	next := newScriptedInteraction(stringPointer("99999"), false)
	if _, err := h.sync(t, next); err != nil {
		t.Fatalf("next sync: %v", err)
	}
	if got := next.pinAskedCount(); got != 0 {
		t.Fatalf("pin asked = %d, want 0", got)
	}
}

func TestCancellingTheRePromptAfterARejectedStoredPinForgetsIt(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.gateway.setPinCalls(2)

	_, err := h.sync(t, newScriptedInteraction(nil, false))
	if err == nil || bankingCode(err) != CodeCancelled {
		t.Fatalf("error = %v, want code CANCELLED", err)
	}
	if !h.store.IsEmpty() {
		t.Fatal("the credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestADialogFailureAfterAStoredPinWasUsedForgetsIt(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	failure := NewGatewayOtherError("connection lost")
	h.gateway.setFailure(gatewayFailure(failure))

	_, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err == nil || bankingCode(err) != CodeFintsError {
		t.Fatalf("error = %v, want code FINTS_ERROR", err)
	}
	if !h.store.IsEmpty() {
		t.Fatal("a re-prompt is cheap, a locked login is not")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestACancelWhileTheStoredPinWasFineKeepsIt(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	failure := NewGatewayCancelledError()
	h.gateway.setFailure(gatewayFailure(failure))

	_, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err == nil || bankingCode(err) != CodeCancelled {
		t.Fatalf("error = %v, want code CANCELLED", err)
	}
	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "12345")
	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = false")
	}
}

func TestAFlagForAnotherLoginForgetsTheEntryAndAsksAgain(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	if err := settings.Set(context.Background(), h.db, PinFlagKey(h.bank), "37050198|someone-else"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	if _, err := h.sync(t, interaction); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := interaction.pinAskedCount(); got != 1 {
		t.Fatalf("pin asked = %d, want 1", got)
	}
	if !h.store.IsEmpty() {
		t.Fatal("the entry belongs to the old login and is still stored")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestAFlagWhoseStoreEntryIsGoneIsClearedAndTheUserAsked(t *testing.T) {
	h := newHarness(t)
	if err := settings.Set(context.Background(), h.db, PinFlagKey(h.bank), "37050198|demo-user"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	if _, err := h.sync(t, interaction); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := interaction.pinAskedCount(); got != 1 {
		t.Fatalf("pin asked = %d, want 1", got)
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestAStoreThatCannotBeReadIsNotUsedAndNothingIsDeleted(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	h.store.SetFailingReads(true)
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	report, err := h.sync(t, interaction)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := interaction.pinAskedCount(); got != 1 {
		t.Fatalf("pin asked = %d, want 1", got)
	}
	h.store.SetFailingReads(false)
	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "12345")
	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("a read failure deleted the entry")
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
}

func TestAStoreThatCannotBeWrittenDoesNotFailTheSync(t *testing.T) {
	h := newHarness(t)
	h.store.SetFailingWrites(true)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), true))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
	if bank := loadBankItem(t, h.db, h.bank); bank.LastSync == nil {
		t.Fatal("last sync is nil")
	}
	if !h.service.PinNotSaved() {
		t.Fatal("pin not saved = false")
	}
	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestRemovingTheBankForgetsTheStoredPinAndItsFlag(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)

	if _, err := h.service.RemoveBank(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("RemoveBank: %v", err)
	}

	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestRemovingABankWithoutARememberedPinLogsNoForgottenPin(t *testing.T) {
	h := newHarness(t)

	if _, err := h.service.RemoveBank(context.Background(), h.db, h.bank); err != nil {
		t.Fatalf("RemoveBank: %v", err)
	}

	if auditHasAction(t, h.db, CodePinForgotten) {
		t.Fatal("PIN_FORGOTTEN was logged without a remembered PIN")
	}
}

func TestSavingTheSameUserIDWithSurroundingSpacesKeepsThePin(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)

	if _, err := h.service.UpdateConnection(
		context.Background(),
		h.db,
		h.bank,
		"Sparkasse",
		"37050198",
		" demo-user ",
		"https://fints.example/pintan",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = false")
	}
	key := PinKey(h.dataDir, int64(h.bank))
	assertString(t, mustReadStore(t, h.store, key), "12345")
}

func TestChangingTheUserIDForgetsThePin(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)

	if _, err := h.service.UpdateConnection(
		context.Background(), h.db, h.bank, "Sparkasse", "37050198", "other-user", "",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	assertString(t, loadBankItem(t, h.db, h.bank).UserID, "other-user")
	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
}

func TestDeletingAProfileForgetsThePinsRememberedInIt(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	marker := PinMarkerPath(h.dataDir, int64(h.bank))
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker missing: %v", err)
	}

	ForgetPinsForProfile(h.store, h.dataDir)

	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker still exists: %v", err)
	}
}

func TestTwoProfilesWithTheSameBankIDDoNotShareACredentialKey(t *testing.T) {
	h := newHarness(t)
	firstKey := PinKey(h.dataDir, int64(h.bank))
	other := filepath.Join(t.TempDir(), "business")
	secondKey := PinKey(other, int64(h.bank))
	if firstKey == secondKey {
		t.Fatalf("profiles share the key %q", firstKey)
	}

	if err := h.store.Write(firstKey, []byte("12345")); err != nil {
		t.Fatalf("credential store write: %v", err)
	}
	value, found, err := h.store.Read(secondKey)
	if err != nil {
		t.Fatalf("credential store read: %v", err)
	}
	if found {
		t.Fatalf("second profile read %q, want nothing", value)
	}
}

func TestForgettingANamedProfileRemovesOnlyItsPinAndMarker(t *testing.T) {
	root := t.TempDir()
	appPaths, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	workDir, err := appPaths.ProfileDir("work")
	if err != nil {
		t.Fatalf("ProfileDir(work): %v", err)
	}
	defaultDir, err := appPaths.ProfileDir("default")
	if err != nil {
		t.Fatalf("ProfileDir(default): %v", err)
	}
	store := NewMemoryCredentialStore()

	defaultDB := testDatabase(t)
	workDB := testDatabase(t)
	defaultBank := createBank(t, defaultDB, "Default Bank", "12030000", "default-user", true)
	workBank := createBank(t, workDB, "Work Bank", "12030000", "work-user", true)
	assertInt(t, int(defaultBank), 1)
	assertInt(t, int(workBank), 1)

	defaultGateway := newFakeGateway()
	defaultService := NewBankingService(NewFinTsClient(defaultGateway, defaultDir, "TEST-PRODUCT", store))
	today := mustDate(t, 2026, time.October, 4)
	if _, err := defaultService.SyncBankConnection(
		context.Background(), defaultDB, defaultBank,
		newScriptedInteraction(stringPointer("11111"), true), today,
	); err != nil {
		t.Fatalf("default sync: %v", err)
	}

	workGateway := newFakeGateway()
	workService := NewBankingService(NewFinTsClient(workGateway, workDir, "TEST-PRODUCT", store))
	if _, err := workService.SyncBankConnection(
		context.Background(), workDB, workBank,
		newScriptedInteraction(stringPointer("22222"), true), today,
	); err != nil {
		t.Fatalf("work sync: %v", err)
	}

	defaultKey := PinKey(defaultDir, 1)
	workKey := PinKey(workDir, 1)
	if defaultKey == workKey {
		t.Fatalf("the root prefix matched a named profile: %q", defaultKey)
	}
	if !store.Contains(defaultKey) || !store.Contains(workKey) {
		t.Fatal("a profile is missing its PIN")
	}
	defaultMarker := PinMarkerPath(defaultDir, 1)
	workMarker := PinMarkerPath(workDir, 1)
	if _, err := os.Stat(defaultMarker); err != nil {
		t.Fatalf("default marker missing: %v", err)
	}
	if _, err := os.Stat(workMarker); err != nil {
		t.Fatalf("work marker missing: %v", err)
	}

	ForgetPinsForProfile(store, workDir)

	if store.Contains(workKey) {
		t.Fatal("work PIN is still stored")
	}
	if _, err := os.Stat(workMarker); !os.IsNotExist(err) {
		t.Fatalf("work marker still exists: %v", err)
	}
	if !store.Contains(defaultKey) {
		t.Fatal("the default profile lost its PIN")
	}
	if _, err := os.Stat(defaultMarker); err != nil {
		t.Fatalf("default marker missing: %v", err)
	}
}

func TestAMarkerWriteFailureSavesNoPin(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "credentials"), []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	h.gateway.setSnapshots([]AccountSnapshot{snapshot("DE89370400440532013000", "100.00", nil)})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), true))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if !h.service.PinNotSaved() {
		t.Fatal("pin not saved = false")
	}
	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
}

func TestAStoreDeleteFailureKeepsTheMarker(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	marker := PinMarkerPath(h.dataDir, int64(h.bank))
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	h.store.SetFailingDeletes(true)

	h.service.ForgetPin(context.Background(), h.db, h.bank)

	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	if !h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the store entry was deleted")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("a later attempt must still find the entry, marker error: %v", err)
	}
}

func TestDebugRedactsTheUserAndCustomerIDs(t *testing.T) {
	h := newHarness(t)
	bankID := createBank(t, h.db, "Bank", "12030000", "secret-user-42", true)
	bank := loadBankItem(t, h.db, bankID)
	connection, err := h.service.Client().ConnectionFor(context.Background(), h.db, bank)
	if err != nil {
		t.Fatalf("ConnectionFor: %v", err)
	}

	for _, debug := range []string{
		fmt.Sprintf("%v", connection),
		fmt.Sprintf("%v", bank),
	} {
		if strings.Contains(debug, "secret-user-42") {
			t.Fatalf("debug leaked the user id: %s", debug)
		}
		if strings.Contains(debug, "12030000|secret-user-42") {
			t.Fatalf("debug leaked the login signature: %s", debug)
		}
		if !strings.Contains(debug, "***") {
			t.Fatalf("debug does not show a redacted user id: %s", debug)
		}
	}
}

func TestBankTextNeverReachesTheAuditLogAndStoredMessagesAreMasked(t *testing.T) {
	h := newHarness(t)
	const iban = "DE89370400440532013000"
	bankText := fmt.Sprintf("Konto %s gesperrt", iban)
	failure := NewGatewayOtherError(bankText)
	h.gateway.setFailure(gatewayFailure(failure))
	_, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err == nil {
		t.Fatal("sync with bank error succeeded")
	}
	if !strings.Contains(err.Error(), "…3000") {
		t.Fatalf("message = %q, want a masked tail", err.Error())
	}
	if strings.Contains(err.Error(), iban) {
		t.Fatalf("message leaked the IBAN: %q", err.Error())
	}

	h.gateway.setFailure(nil)
	failed := snapshot(iban, "100.00", []FinTsTransaction{
		booking(iban, h.today, "-1.00", &bankText, &bankText, nil),
	})
	failed.StatementError = &bankText
	h.gateway.setSnapshots([]AccountSnapshot{failed})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	warnings := strings.Join(report.Warnings, "\n")
	if strings.Contains(warnings, iban) {
		t.Fatalf("warnings leaked the IBAN: %q", warnings)
	}
	if !strings.Contains(warnings, "…3000") {
		t.Fatalf("warnings = %q, want a masked tail", warnings)
	}
	accountID := h.accounts(t)[0].ID
	stored := transactionsForAccount(t, h.db, accountID)
	if len(stored) == 0 {
		t.Fatal("no booking was stored")
	}
	// Booking texts are the user's own data and stay as the bank sent them (as
	// FinDesk and the CSV import).
	for _, transaction := range stored {
		for _, text := range []*string{transaction.PartnerName, transaction.Purpose} {
			if text == nil || *text != bankText {
				t.Fatalf("stored booking text = %v, want %q", text, bankText)
			}
		}
	}
	h.assertAuditHasNo(t, iban, "gesperrt", "demo-user")
}

func TestChangingOnlyTheURLResetsTheRegistrationAndForgetsThePin(t *testing.T) {
	// This deliberately differs from FinDesk, which kept the PIN on a URL
	// change. The PIN flag is <blz>|<userId>, so a kept PIN would be sent to
	// whatever host the user typed, including a mistyped one.
	h := newHarness(t)
	storePin(t, h)
	if _, err := bankconnections.UpdateCapabilities(
		context.Background(), h.db, h.bank, "SaldoReq,KUmsAll",
	); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	if _, err := bankconnections.Update(
		context.Background(), h.db, h.bank, nil, stringPointer("942"),
	); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, err := h.service.UpdateConnection(
		context.Background(),
		h.db,
		h.bank,
		"Sparkasse",
		"37050198",
		"demo-user",
		"https://new.example/fints",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	bank := loadBankItem(t, h.db, h.bank)
	if bank.Capabilities != nil {
		t.Fatalf("capabilities = %v, want nil", bank.Capabilities)
	}
	if bank.TanMethod != nil {
		t.Fatalf("tan method = %v, want nil", bank.TanMethod)
	}
	if bank.ServerURL == nil || *bank.ServerURL != "https://new.example/fints" {
		t.Fatalf("server URL = %v, want https://new.example/fints", bank.ServerURL)
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	assertInt(t, auditCount(t, h.db, CodeResetFintsLogin), 1)
}

func TestChangingTheUserIDResetsTheRegistrationAndForgetsThePin(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)

	if _, err := h.service.UpdateConnection(
		context.Background(), h.db, h.bank, "Sparkasse", "37050198", "new-user", "",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	assertInt(t, auditCount(t, h.db, CodeResetFintsLogin), 1)
}

func TestChangingTheBLZResetsForgetsThePinAndKeepsTheAccounts(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	const giro = "DE89370400440532013000"
	accountID := h.accounts(t)[0].ID
	booked := snapshot(giro, "100.00", []FinTsTransaction{
		booking(giro, h.today.AddDays(-1), "-5.00", stringPointer("Shop"), stringPointer("A"), stringPointer("100.00")),
	})
	booked.Pending = nil
	h.gateway.setSnapshots([]AccountSnapshot{booked})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if _, err := h.service.UpdateConnection(
		context.Background(), h.db, h.bank, "Sparkasse", "70020270", "demo-user", "",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	bank := loadBankItem(t, h.db, h.bank)
	assertString(t, bank.BLZ, "70020270")
	changeFound := false
	for _, entry := range auditEntries(t, h.db, 1000) {
		if entry.Action == CodeChangeBankCode && strings.Contains(entry.Details, "37050198 -> 70020270") {
			changeFound = true
		}
	}
	if !changeFound {
		t.Fatal("missing CHANGE_BANK_CODE audit entry")
	}
	foundAccount := false
	for _, account := range h.accounts(t) {
		if account.ID == accountID {
			foundAccount = true
		}
	}
	if !foundAccount {
		t.Fatal("the account was deleted")
	}
	if len(transactionsForAccount(t, h.db, accountID)) == 0 {
		t.Fatal("the account lost its transactions")
	}
}

func TestSavingUnchangedValuesResetsNothing(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)

	if _, err := h.service.UpdateConnection(
		context.Background(),
		h.db,
		h.bank,
		"Sparkasse KölnBonn",
		"37050198",
		"demo-user",
		"https://fints.example/pintan",
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	if h.store.IsEmpty() {
		t.Fatal("credential store is empty")
	}
	if !h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = false")
	}
	if auditHasAction(t, h.db, CodeResetFintsLogin) {
		t.Fatal("RESET_FINTS_LOGIN was logged")
	}
	if auditHasAction(t, h.db, CodeChangeBankCode) {
		t.Fatal("CHANGE_BANK_CODE was logged")
	}
}

func TestTestsNeverReachARealCredentialStore(t *testing.T) {
	if (NoCredentialStore{}).IsAvailable() {
		t.Fatal("NoCredentialStore is available")
	}
	value, found, err := (NoCredentialStore{}).Read("key")
	if err != nil || found || value != nil {
		t.Fatalf("Read = %v, %t, %v; want nil, false, nil", value, found, err)
	}
}

func TestConnectionForRefusesAnOfflineBank(t *testing.T) {
	h := newHarness(t)
	offline, err := bankconnections.CreateOffline(context.Background(), h.db, "Norwegian Card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}
	bank := loadBankItem(t, h.db, offline.ID)

	_, err = h.service.Client().ConnectionFor(context.Background(), h.db, bank)
	if err == nil || bankingCode(err) != CodeOffline {
		t.Fatalf("error = %v, want code OFFLINE", err)
	}
}

func TestRememberingOrForgettingAPinForAnOfflineBankTouchesNoCredentials(t *testing.T) {
	h := newHarness(t)
	offline, err := bankconnections.CreateOffline(context.Background(), h.db, "Norwegian Card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}

	h.service.ForgetPin(context.Background(), h.db, offline.ID)

	if h.service.HasSavedPin(context.Background(), h.db, offline.ID) {
		t.Fatal("has saved pin = true")
	}
	if !h.store.IsEmpty() {
		t.Fatal("an offline bank touched the credential store")
	}
}

func TestAFailedLoginResetKeepsTheOldSettingsAndStillForgetsThePin(t *testing.T) {
	h := newHarness(t)
	storePin(t, h)
	if _, err := bankconnections.UpdateCapabilities(
		context.Background(), h.db, h.bank, "SaldoReq,KUmsAll",
	); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	// Make clearing the login state fail, as a full disk or a locked database
	// would.
	err := h.db.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx,
			"CREATE TRIGGER refuse_clear BEFORE UPDATE OF capabilities ON bank_connections "+
				"WHEN NEW.capabilities IS NULL BEGIN SELECT RAISE(ABORT, 'refused'); END;",
		)
		return err
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, updateErr := h.service.UpdateConnection(
		context.Background(),
		h.db,
		h.bank,
		"Sparkasse",
		"37050198",
		"demo-user",
		"https://new.example/fints",
	)
	if updateErr == nil {
		t.Fatal("UpdateConnection succeeded")
	}
	bank := loadBankItem(t, h.db, h.bank)
	if bank.ServerURL == nil || *bank.ServerURL == "https://new.example/fints" {
		t.Fatalf("the edit was saved: %v", bank.ServerURL)
	}
	if h.service.HasSavedPin(context.Background(), h.db, h.bank) {
		t.Fatal("has saved pin = true")
	}
	if !h.store.IsEmpty() {
		t.Fatal("credential store is not empty")
	}
}
