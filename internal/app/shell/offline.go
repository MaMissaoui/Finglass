package shell

import (
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// StartOffline starts a session like Start, but with banking pinned to the
// offline gateway. It never reads the build's FinTS product id and never builds
// the real gateway, so headless tools can open a profile without any chance of
// dialing a bank.
func StartOffline(
	appPaths paths.AppPaths,
	credentials credentials.Store,
	findeskRoot *string,
	clock func() time.Time,
	today func() civil.Date,
	getenv func(string) (string, bool),
) (*Session, *migration.FinDeskInstall, *StartupRefusal) {
	session, offer, refusal := Start(appPaths, credentials, findeskRoot, clock, today, getenv)
	if session != nil {
		session.mu.Lock()
		session.offlineGateway = true
		session.mu.Unlock()
	}
	return session, offer, refusal
}
