package shell

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/connectiontest"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

func TestStartOfflinePinsBankingToTheOfflineGateway(t *testing.T) {
	appPaths := testPaths(t)
	session, offer, refusal := StartOffline(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("StartOffline refusal = %+v", refusal)
	}
	if offer != nil {
		t.Fatalf("StartOffline offer = %+v, want nil", offer)
	}
	t.Cleanup(func() { _ = session.Shutdown() })

	session.mu.Lock()
	offline := session.offlineGateway
	factoryCalled := false
	session.gatewayFactory = func(i18n.Language) banksync.FinTsGateway {
		factoryCalled = true
		return banksync.NewUnavailableGateway(i18n.En)
	}
	session.mu.Unlock()
	if !offline {
		t.Fatal("StartOffline did not pin the offline gateway")
	}

	service, ok := session.bankingServiceLocked(i18n.En)
	if !ok || service == nil {
		t.Fatal("the Accounts page could not build the banking service")
	}
	if factoryCalled {
		t.Fatal("the offline session built an injected gateway")
	}
	// The offline gateway always fails without opening a socket. This is the
	// gateway the exporter's session can reach.
	_, err := banksync.NewUnavailableGateway(i18n.En).FetchAll(
		context.Background(),
		banksync.FinTsConnection{},
		nil,
		func(string) *civil.Date { return nil },
		civil.Date{},
	)
	var gatewayError banksync.GatewayError
	if !errors.As(err, &gatewayError) {
		t.Fatalf("offline gateway error = %v, want a GatewayError", err)
	}
}

func TestStartOfflineNeverRunsAConnectionTest(t *testing.T) {
	appPaths := testPaths(t)
	bankID := seedBankingDatabase(t, appPaths, "user")
	session, offer, refusal := StartOffline(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("StartOffline refusal = %+v", refusal)
	}
	if offer != nil {
		t.Fatalf("StartOffline offer = %+v, want nil", offer)
	}
	t.Cleanup(func() { _ = session.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	session.BankingLoad()
	waitForJobs(t, session)

	var factoryCalled atomic.Bool
	session.mu.Lock()
	offline := session.offlineGateway
	session.connectionTestFactory = func(string) (fints.Transport, error) {
		factoryCalled.Store(true)
		return nil, errors.New("the offline session built a transport")
	}
	session.mu.Unlock()
	if !offline {
		t.Fatal("StartOffline did not pin the offline gateway")
	}

	state := session.BankingState()
	if len(state.Banks) == 0 {
		t.Fatal("no bank loaded")
	}
	bank := state.Banks[0]
	if bank.ServerURL == nil || strings.TrimSpace(*bank.ServerURL) == "" {
		t.Fatalf("fixture bank has no server URL: %+v", bank)
	}
	if !bank.CanTestConnection {
		t.Fatalf("fixture bank cannot test a connection: %+v", bank)
	}

	session.BankingTestConnection(int(bankID))

	result := session.BankingState().ConnectionTest
	if result == nil {
		t.Fatal("the offline test connection left no result")
	}
	if result.Running {
		t.Fatalf("the offline test connection is still running: %+v", result)
	}
	if result.Other != connectiontest.OtherCancelled ||
		result.Verdict != banking.ConnectionTestVerdictNone ||
		result.Kind != banking.ConnectionTestKindOther ||
		result.Reason != i18n.Text(i18n.En, i18n.TestConnectionCancelled) {
		t.Fatalf("offline result = %+v, want the cancelled not-completed shape", result)
	}
	if factoryCalled.Load() {
		t.Fatal("the offline session built a connection-test transport")
	}
	state = session.BankingState()
	if state.LastTrace == nil || state.LastTrace.Kind != traceKindTest ||
		state.LastTrace.BankID != bankID || state.LastTrace.HasTrace {
		t.Fatalf("offline last trace = %+v, want an empty test trace", state.LastTrace)
	}
}
