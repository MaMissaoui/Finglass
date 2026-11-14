package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/fintsgateway"
	"github.com/MaMissaoui/Finglass/internal/core/fintsproduct"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	syncTraceProduct = "TESTPRODUCT0000000000000"
	syncTraceUser    = "USER-ID-MUST-NEVER-APPEAR"
)

func TestSelectGatewayOnlyBuildsTheRealGatewayWhenBothHold(t *testing.T) {
	real := newBankingTestGateway()
	tests := []struct {
		name          string
		available     bool
		productExists bool
		wantReal      bool
	}{
		{name: "both", available: true, productExists: true, wantReal: true},
		{name: "no flag", available: false, productExists: true},
		{name: "no product", available: true, productExists: false},
		{name: "neither", available: false, productExists: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			gateway := selectGateway(test.available, test.productExists, i18n.En, func() banksync.FinTsGateway {
				calls++
				return real
			})
			if test.wantReal {
				if gateway != real || calls != 1 {
					t.Fatalf("gateway = %T calls = %d, want the real one", gateway, calls)
				}
				return
			}
			if _, ok := gateway.(banksync.UnavailableGateway); !ok {
				t.Fatalf("gateway = %T, want UnavailableGateway", gateway)
			}
			if calls != 0 {
				t.Fatalf("the real constructor ran %d times", calls)
			}
		})
	}
}

func TestTheProductionSelectionPassesOnlineSyncAvailable(t *testing.T) {
	if OnlineSyncAvailable {
		t.Fatal("OnlineSyncAvailable must stay false")
	}
	gateway := selectGateway(OnlineSyncAvailable, fintsproduct.Present(), i18n.En, func() banksync.FinTsGateway {
		t.Fatal("the real gateway was built while OnlineSyncAvailable is false")
		return nil
	})
	if _, ok := gateway.(banksync.UnavailableGateway); !ok {
		t.Fatalf("gateway = %T, want UnavailableGateway", gateway)
	}
}

// testProductGateway fills the product id the test build does not link, then
// runs the real adapter with the shell's trace wrapper.
type testProductGateway struct {
	inner     *fintsgateway.Gateway
	productID string
}

func (g *testProductGateway) FetchAll(
	ctx context.Context,
	connection banksync.FinTsConnection,
	interaction banksync.BankingInteraction,
	since func(string) *civil.Date,
	to civil.Date,
) (banksync.FetchOutcome, error) {
	connection.ProductID = g.productID
	return g.inner.FetchAll(ctx, connection, interaction, since, to)
}

func TestASyncRecordsARedactedTraceInTheOneSlot(t *testing.T) {
	session, bankID, _ := newBankingSessionWithUser(t, banksync.NoCredentialStore{}, nil, syncTraceUser)
	session.syncGatewayFactory = func(_ i18n.Language, sink *syncTraceSink) banksync.FinTsGateway {
		transport := typedTANConnectionBank(t)
		return &testProductGateway{
			inner: fintsgateway.New(
				fintsgateway.WithTransportFactory(func(string) (fints.Transport, error) {
					return transport, nil
				}),
				fintsgateway.WithTransportWrapper(sink.wrap),
			),
			productID: syncTraceProduct,
		}
	}

	session.BankingStartSync(int(bankID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("sync error = %q", *state.Error)
	}
	if state.LastTrace == nil || state.LastTrace.Kind != traceKindSync ||
		state.LastTrace.BankID != bankID || !state.LastTrace.HasTrace {
		t.Fatalf("last trace = %+v", state.LastTrace)
	}

	session.mu.Lock()
	trace := append([]byte(nil), session.lastTrace.trace...)
	blz := session.lastTrace.blz
	session.mu.Unlock()
	if blz != connectionTestBLZ {
		t.Fatalf("trace BLZ = %q, want %q", blz, connectionTestBLZ)
	}
	text := string(trace)
	if !strings.Contains(text, "Finglass FinTS trace") {
		t.Fatalf("trace content = %q", text)
	}
	for _, marker := range []string{
		connectionTestPIN,
		connectionTestTAN,
		syncTraceProduct,
		syncTraceUser,
		connectionTestIBAN,
		"98765,43",
		connectionTestBooked,
	} {
		if strings.Contains(text, marker) {
			t.Fatalf("trace leaked %q", marker)
		}
	}

	// The sync's trace is what "Save redacted trace…" writes, with mode 0600.
	path := filepath.Join(t.TempDir(), "sync-trace.txt")
	session.SetFileDialogs(&fakeFileDialogs{tracePath: path, traceResult: true})
	session.BankingSaveConnectionTrace()
	waitForJobs(t, session)
	// #nosec G304 -- path is a test temporary file.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved trace: %v", err)
	}
	if string(saved) != text {
		t.Fatal("the saved file is not the sync's trace")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved trace: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("trace mode = %o, want 600", info.Mode().Perm())
	}

	// A connection test replaces the sync's trace in the one slot.
	runTypedTANConnectionTest(t, session, typedTANConnectionBank(t))
	state = session.BankingState()
	if state.LastTrace == nil || state.LastTrace.Kind != traceKindTest {
		t.Fatalf("last trace after a test = %+v", state.LastTrace)
	}
	session.mu.Lock()
	kind := session.lastTrace.kind
	replaced := string(session.lastTrace.trace) != text
	session.mu.Unlock()
	if kind != traceKindTest || !replaced {
		t.Fatalf("slot kind = %q, replaced = %t", kind, replaced)
	}
}
