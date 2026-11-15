package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/fintsgateway"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

func createOnlineBankForTrace(
	t *testing.T,
	session *Session,
	name,
	blz,
	userID,
	serverURL string,
	unlocked bool,
) model.BankConnectionID {
	t.Helper()
	database := sessionDatabase(t, session)
	bic := "TESTDEFFXXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: name,
		BLZ:      blz,
		BIC:      &bic,
		UserID:   userID,
	})
	if err != nil {
		t.Fatalf("create bank %s: %v", name, err)
	}
	if _, err := bankconnections.UpdateConnection(
		context.Background(),
		database,
		created.ID,
		model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("update bank %s: %v", name, err)
	}
	if unlocked {
		if err := settings.Set(
			context.Background(),
			database,
			banksync.SyncUnlockKey(created.ID),
			banksync.SyncFingerprint(blz, userID, serverURL),
		); err != nil {
			t.Fatalf("unlock bank %s: %v", name, err)
		}
	}
	session.BankingLoad()
	waitForJobs(t, session)
	return created.ID
}

// traceConnectionBank is typedTANConnectionBank with that bank's public
// identifiers, so each trace carries a marker unique to its own dialog.
func traceConnectionBank(t *testing.T, blz, iban, userID string) *connectionTestTransport {
	t.Helper()
	bank := typedTANConnectionBank(t)
	for index := range bank.replies {
		body := bank.replies[index].body
		body = strings.ReplaceAll(body, connectionTestIBAN, iban)
		body = strings.ReplaceAll(body, connectionTestBLZ, blz)
		body = strings.ReplaceAll(body, connectionTestUser, userID)
		bank.replies[index].body = body
	}
	return bank
}

func TestSyncAllKeepsOneRedactedTracePerBank(t *testing.T) {
	const (
		bankBBLZ  = "70020270"
		bankBIBAN = "DE89700202700000000000"
		bankBUser = "user-b"
		bankBURL  = "https://fints.example/bank-b"
	)
	session, bankA, _ := newBankingSessionWithUser(t, banksync.NoCredentialStore{}, nil, "user")
	bankB := createOnlineBankForTrace(t, session, "Zweite Bank", bankBBLZ, bankBUser, bankBURL, true)

	bankATrace := traceConnectionBank(t, connectionTestBLZ, connectionTestIBAN, "user")
	bankBTrace := traceConnectionBank(t, bankBBLZ, bankBIBAN, bankBUser)
	session.syncGatewayFactory = func(_ i18n.Language, sink *syncTraceSink) banksync.FinTsGateway {
		return &testProductGateway{
			inner: fintsgateway.New(
				fintsgateway.WithTransportFactory(func(serverURL string) (fints.Transport, error) {
					if serverURL == bankBURL {
						return bankBTrace, nil
					}
					return bankATrace, nil
				}),
				fintsgateway.WithTransportWrapper(sink.wrap),
			),
			productID: syncTraceProduct,
		}
	}

	session.BankingStartSyncAll()
	for index := 0; index < 2; index++ {
		answerBankingPin(t, session, connectionTestPIN, false)
		answerBankingTan(t, session, connectionTestTAN)
	}
	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("Sync all error = %q", *state.Error)
	}
	if state.Message == nil || !strings.Contains(*state.Message, "\n") {
		t.Fatalf("Sync all summary = %v, want one line per bank", state.Message)
	}
	if len(state.Traces) != 2 {
		t.Fatalf("traces = %+v, want one per bank", state.Traces)
	}
	for _, bankID := range []model.BankConnectionID{bankA, bankB} {
		found := false
		for _, trace := range state.Traces {
			if trace.BankID == bankID && trace.Kind == traceKindSync && trace.HasTrace {
				found = true
			}
		}
		if !found {
			t.Fatalf("bank %d has no sync trace in %+v", bankID, state.Traces)
		}
	}

	session.mu.Lock()
	traceA := append([]byte(nil), session.lastTraces[bankA].trace...)
	bankABLZ := session.lastTraces[bankA].blz
	traceB := append([]byte(nil), session.lastTraces[bankB].trace...)
	bankBBLZValue := session.lastTraces[bankB].blz
	session.mu.Unlock()
	if bankABLZ != connectionTestBLZ || bankBBLZValue != bankBBLZ {
		t.Fatalf("trace BLZs = %q/%q", bankABLZ, bankBBLZValue)
	}
	textA := string(traceA)
	textB := string(traceB)
	if !strings.Contains(textA, connectionTestBLZ) || strings.Contains(textA, bankBBLZ) {
		t.Fatalf("bank A trace carries the wrong dialog: %q", textA)
	}
	if !strings.Contains(textB, bankBBLZ) || strings.Contains(textB, connectionTestBLZ) {
		t.Fatalf("bank B trace carries the wrong dialog: %q", textB)
	}
}

func TestSyncAllKeepsASkippedBanksEarlierTrace(t *testing.T) {
	const (
		bankBBLZ  = "70020270"
		bankBUser = "user-b"
		bankBURL  = "https://fints.example/bank-b"
	)
	session, bankA, _ := newBankingSessionWithUser(t, banksync.NoCredentialStore{}, nil, "user")
	bankB := createOnlineBankForTrace(t, session, "Gesperrte Bank", bankBBLZ, bankBUser, bankBURL, false)

	oldTrace := []byte("trace from the earlier test")
	session.mu.Lock()
	session.setLastTraceLocked(lastTraceState{
		trace:  oldTrace,
		blz:    bankBBLZ,
		kind:   traceKindTest,
		bankID: bankB,
	})
	session.banking.Traces = session.lastTraceListLocked()
	session.mu.Unlock()

	bankATrace := traceConnectionBank(t, connectionTestBLZ, connectionTestIBAN, "user")
	session.syncGatewayFactory = func(_ i18n.Language, sink *syncTraceSink) banksync.FinTsGateway {
		return &testProductGateway{
			inner: fintsgateway.New(
				fintsgateway.WithTransportFactory(func(string) (fints.Transport, error) {
					return bankATrace, nil
				}),
				fintsgateway.WithTransportWrapper(sink.wrap),
			),
			productID: syncTraceProduct,
		}
	}

	session.BankingStartSyncAll()
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("Sync all error = %q", *state.Error)
	}

	session.mu.Lock()
	keptA, hasA := session.lastTraces[bankA]
	keptB, hasB := session.lastTraces[bankB]
	session.mu.Unlock()
	if !hasA || len(keptA.trace) == 0 {
		t.Fatal("the synced bank did not get a trace")
	}
	if !hasB || string(keptB.trace) != string(oldTrace) || keptB.blz != bankBBLZ ||
		keptB.kind != traceKindTest {
		t.Fatalf("skipped bank trace = %+v, want the earlier slot", keptB)
	}
}

func TestRemovingABankDropsItsTrace(t *testing.T) {
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	trace := []byte("trace to wipe")
	session.mu.Lock()
	session.setLastTraceLocked(lastTraceState{
		trace:  trace,
		blz:    connectionTestBLZ,
		kind:   traceKindSync,
		bankID: bankID,
	})
	session.banking.Traces = session.lastTraceListLocked()
	session.mu.Unlock()

	session.BankingRemoveBank(int(bankID))
	waitForJobs(t, session)

	session.mu.Lock()
	_, kept := session.lastTraces[bankID]
	session.mu.Unlock()
	if kept {
		t.Fatal("the removed bank kept its trace slot")
	}
	for index, value := range trace {
		if value != 0 {
			t.Fatalf("trace byte %d = %d after removing the bank", index, value)
		}
	}
}

func TestProfileCloseClearsEveryBanksTrace(t *testing.T) {
	session, bankA, _ := newBankingSessionWithUser(t, banksync.NoCredentialStore{}, nil, "user")
	bankB := createOnlineBankForTrace(
		t,
		session,
		"Zweite Bank",
		"70020270",
		"user-b",
		"https://fints.example/bank-b",
		true,
	)

	traceA := []byte("bank A trace")
	traceB := []byte("bank B trace")
	session.mu.Lock()
	session.setLastTraceLocked(lastTraceState{
		trace: traceA, blz: connectionTestBLZ, kind: traceKindSync, bankID: bankA,
	})
	session.setLastTraceLocked(lastTraceState{
		trace: traceB, blz: "70020270", kind: traceKindSync, bankID: bankB,
	})
	session.banking.Traces = session.lastTraceListLocked()
	session.mu.Unlock()

	session.Teardown(startup.Startup{})

	session.mu.Lock()
	slots := len(session.lastTraces)
	session.mu.Unlock()
	if slots != 0 {
		t.Fatalf("the profile kept %d trace slots", slots)
	}
	for _, trace := range [][]byte{traceA, traceB} {
		for index, value := range trace {
			if value != 0 {
				t.Fatalf("trace byte %d = %d after the profile switch", index, value)
			}
		}
	}
}
