// Package findeskapp locates the installed FinDesk.app.
//
// The import reads FinDesk's bank list from the user's own installation only;
// this package is the one place that knows where that installation lives.
package findeskapp

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths returns the installed FinDesk.app locations. Tests replace it so they
// never look at the real /Applications.
var Paths = DefaultPaths

// DefaultPaths returns the standard FinDesk.app locations on macOS and nothing
// on other systems.
func DefaultPaths() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	paths := []string{"/Applications/FinDesk.app"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, "Applications", "FinDesk.app"))
	}
	return paths
}
