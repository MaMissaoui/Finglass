//go:build windows

package credstore

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/danieljoos/wincred"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

// windowsStore is Windows Credential Manager (generic credentials).
type windowsStore struct {
	service string
}

// NewSystem returns the Windows Credential Manager store for service.
func NewSystem(service string) credentials.Store {
	return windowsStore{service: service}
}

// IsAvailable reports that Credential Manager exists.
func (windowsStore) IsAvailable() bool {
	return true
}

// target is the credential's target name. The service prefixes the account so
// one Windows user can keep entries for several Finglass services.
func (w windowsStore) target(account string) string {
	return w.service + "/" + account
}

// Read returns a copy of the stored blob, or found=false when the account is
// absent. The caller wipes the copy.
func (w windowsStore) Read(account string) ([]byte, bool, error) {
	credential, err := wincred.GetGenericCredential(w.target(account))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil, false, nil
		}
		return nil, false, windowsError("read", err)
	}

	secret := make([]byte, len(credential.CredentialBlob))
	copy(secret, credential.CredentialBlob)
	clear(credential.CredentialBlob)
	return secret, true, nil
}

// Write stores or replaces the secret. A local-machine credential never roams
// to another computer.
func (w windowsStore) Write(account string, secret []byte) error {
	credential := wincred.NewGenericCredential(w.target(account))
	credential.UserName = account
	credential.Persist = wincred.PersistLocalMachine
	credential.CredentialBlob = append([]byte(nil), secret...)
	err := credential.Write()
	clear(credential.CredentialBlob)
	if err != nil {
		return windowsError("write", err)
	}
	return nil
}

// Delete removes the secret. A missing entry is not an error.
func (w windowsStore) Delete(account string) error {
	err := wincred.NewGenericCredential(w.target(account)).Delete()
	if err == nil || errors.Is(err, wincred.ErrElementNotFound) {
		return nil
	}
	return windowsError("delete", err)
}

// windowsError names the operation and a fixed error kind only. The account
// key and the secret never appear.
func windowsError(operation string, err error) error {
	return fmt.Errorf("credential store %s failed: %s", operation, windowsErrorKind(err))
}

// windowsErrorKind maps Credential Manager errors to fixed words.
func windowsErrorKind(err error) string {
	switch {
	case errors.Is(err, wincred.ErrElementNotFound):
		return "no entry"
	case errors.Is(err, wincred.ErrInvalidParameter):
		return "invalid parameter"
	case errors.Is(err, wincred.ErrBadUsername):
		return "bad user name"
	case errors.Is(err, syscall.ERROR_ACCESS_DENIED):
		return "access denied"
	default:
		return "unknown error"
	}
}
