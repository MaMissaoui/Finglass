package shell

import (
	"context"
	"errors"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
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
