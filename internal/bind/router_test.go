package bind

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

type countingPublisher struct {
	calls int
}

func (p *countingPublisher) publishPage() {
	p.calls++
}

func TestTheRouterSendsOneChangeToExactlyOnePublisher(t *testing.T) {
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	session, _, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	t.Cleanup(func() { _ = session.Shutdown() })

	dashboard := &countingPublisher{}
	accounts := &countingPublisher{}
	shellPublisher := &countingPublisher{}
	router := NewRouter(session)
	t.Cleanup(func() { _ = router.Shutdown() })
	router.Register("dashboard", dashboard)
	router.Register("accounts", accounts)
	router.RegisterShell(shellPublisher)

	assertCalls := func(name string, dashboardWant, accountsWant, shellWant int) {
		t.Helper()
		if dashboard.calls != dashboardWant || accounts.calls != accountsWant || shellPublisher.calls != shellWant {
			t.Fatalf(
				"%s: dashboard=%d accounts=%d shell=%d, want %d/%d/%d",
				name, dashboard.calls, accounts.calls, shellPublisher.calls,
				dashboardWant, accountsWant, shellWant,
			)
		}
	}

	session.DashboardDismissError()
	assertCalls("dashboard change", 1, 0, 0)

	session.AccountsFilterChanged("haus")
	assertCalls("accounts change", 1, 1, 0)

	session.Navigate("accounts")
	assertCalls("shell change", 1, 1, 1)
}
