package migration

import (
	"github.com/MaMissaoui/Finglass/internal/app/findeskapp"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
)

// findeskAppPaths is the injectable seam for the installed FinDesk.app
// locations. Tests replace it so they never look at the real /Applications.
var findeskAppPaths = defaultFinDeskAppPaths

func defaultFinDeskAppPaths() []string {
	return findeskapp.Paths()
}

// bankDirectoryLookup reads the user's own FinDesk bank list. Any error leaves
// the migration running without a lookup.
func bankDirectoryLookup() findeskimport.ServerURLLookup {
	lookup, err := findeskimport.FinDeskBankDirectory(findeskAppPaths())
	if err != nil {
		return nil
	}
	return lookup
}
