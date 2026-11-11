// Package credentials keeps secrets out of the database and out of files.
//
// On macOS a [Store] is the login Keychain through the Security framework, via
// github.com/keybase/go-keychain. The package deliberately does not use
// zalando/go-keyring or the security(1) command: both would put a secret in a
// child process's argv, where any local process can read it. Other operating
// systems get an unavailable store.
//
// Secrets are bytes. They are never logged and never appear in an error; the
// Keychain's error codes are mapped to a small fixed vocabulary.
package credentials

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// KeychainService is the service every Finglass credential is filed under: the
// app's bundle identifier.
const KeychainService = "io.github.mamissaoui.finglass"

// ErrUnavailable is returned by a store that this operating system cannot
// provide, such as the Keychain store on Linux.
var ErrUnavailable = errors.New("credential store is not available")

// Store keeps secrets by account key.
type Store interface {
	// IsAvailable reports whether this system has a protected store, so the UI
	// may offer to remember a PIN.
	IsAvailable() bool
	// Read returns the secret and whether it exists. A missing entry is not an
	// error.
	Read(account string) (secret []byte, found bool, err error)
	// Write stores a secret, replacing an existing entry for the same account.
	Write(account string, secret []byte) error
	// Delete removes an entry. Deleting a missing entry succeeds.
	Delete(account string) error
}

// AccountFor is the Keychain account string of a profile-scoped bank access.
//
// It mirrors Rust's account_for: std::path::absolute (which does not resolve
// symlinks or `..`), then "#bank-<id>". The profile folder keeps two profiles
// with the same bank id apart.
func AccountFor(profileDir string, bankID int64) string {
	return fmt.Sprintf("%s#bank-%d", absolutePath(profileDir), bankID)
}

// absolutePath is Rust's std::path::absolute for POSIX paths: collapse
// separators, drop `.` components, keep `..`, keep a trailing separator, and
// start relative paths at the current directory.
func absolutePath(path string) string {
	if path == "" {
		return path
	}

	trailingSeparator := strings.HasSuffix(path, "/")
	absolute := strings.HasPrefix(path, "/")

	root, components := splitPOSIX(path, absolute)
	if !absolute {
		current, err := os.Getwd()
		if err != nil {
			return path
		}
		currentRoot, currentComponents := splitPOSIX(current, strings.HasPrefix(current, "/"))
		root = currentRoot
		components = append(currentComponents, components...)
	}

	result := root + strings.Join(components, "/")
	if trailingSeparator && !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}

// splitPOSIX separates an absolute root from its path components. Repeated
// separators and `.` components disappear; `..` stays because resolving it
// could change the meaning of a symlinked path.
func splitPOSIX(value string, absolute bool) (root string, components []string) {
	if absolute {
		if strings.HasPrefix(value, "//") && !strings.HasPrefix(value, "///") {
			root = "//"
		} else {
			root = "/"
		}
	}

	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." {
			continue
		}
		components = append(components, component)
	}
	return root, components
}
