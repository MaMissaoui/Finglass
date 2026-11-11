//go:build darwin

package credentials

import (
	"errors"
	"fmt"

	"github.com/keybase/go-keychain"
)

// keychainStore is the macOS login Keychain.
type keychainStore struct {
	service string
}

// NewKeychain returns the login Keychain store for service.
//
// Constructing it does not unlock or prompt the Keychain; only reading and
// writing an entry does.
func NewKeychain(service string) Store {
	return keychainStore{service: service}
}

// IsAvailable reports whether the Keychain can be used.
func (k keychainStore) IsAvailable() bool {
	return true
}

// Read returns a stored secret, or found=false when the account is absent.
func (k keychainStore) Read(account string) ([]byte, bool, error) {
	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(k.service)
	query.SetAccount(account)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)

	results, err := keychain.QueryItem(query)
	if err != nil {
		return nil, false, keychainError("read", err)
	}
	if len(results) == 0 {
		return nil, false, nil
	}
	if len(results) > 1 {
		return nil, false, keychainError("read", keychain.ErrorParam)
	}

	secret := make([]byte, len(results[0].Data))
	copy(secret, results[0].Data)
	return secret, true, nil
}

// Write stores or replaces a secret.
func (k keychainStore) Write(account string, secret []byte) error {
	item := keychain.NewGenericPassword(k.service, account, "", secret, "")
	if err := keychain.AddItem(item); err == nil {
		return nil
	} else if !errors.Is(err, keychain.ErrorDuplicateItem) {
		return keychainError("write", err)
	}

	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(k.service)
	query.SetAccount(account)
	update := keychain.NewItem()
	update.SetData(secret)
	if err := keychain.UpdateItem(query, update); err != nil {
		return keychainError("write", err)
	}
	return nil
}

// Delete removes a secret. A missing entry is not an error.
func (k keychainStore) Delete(account string) error {
	err := keychain.DeleteGenericPasswordItem(k.service, account)
	if err == nil || errors.Is(err, keychain.ErrorItemNotFound) {
		return nil
	}
	return keychainError("delete", err)
}

// keychainError names the operation and a fixed error kind only. The account
// key and the secret never appear.
func keychainError(operation string, err error) error {
	return fmt.Errorf("credential store %s failed: %s", operation, keychainErrorKind(err))
}

// keychainErrorKind maps the Security framework's codes to fixed words.
func keychainErrorKind(err error) string {
	switch {
	case errors.Is(err, keychain.ErrorInteractionNotAllowed):
		return "interaction not allowed"
	case errors.Is(err, keychain.ErrorItemNotFound):
		return "no entry"
	case errors.Is(err, keychain.ErrorAuthFailed):
		return "authentication failed"
	case errors.Is(err, keychain.ErrorNoSuchKeychain):
		return "no such keychain"
	case errors.Is(err, keychain.ErrorInvalidKeychain):
		return "invalid keychain"
	case errors.Is(err, keychain.ErrorNoAccessForItem):
		return "no access for item"
	case errors.Is(err, keychain.ErrorReadOnly):
		return "read only"
	case errors.Is(err, keychain.ErrorUserCanceled):
		return "user canceled"
	case errors.Is(err, keychain.ErrorParam):
		return "invalid parameter"
	case errors.Is(err, keychain.ErrorNotAvailable):
		return "not available"
	case errors.Is(err, keychain.ErrorDecode):
		return "bad encoding"
	default:
		return "unknown error"
	}
}
