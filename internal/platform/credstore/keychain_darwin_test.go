//go:build darwin

package credstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/keybase/go-keychain"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

func TestKeychainErrorsNameOnlyTheOperationAndKind(t *testing.T) {
	secret := "12345"
	account := credentials.AccountFor("/tmp/profile-a", 7)

	for _, test := range []struct {
		err  error
		want string
	}{
		{keychain.ErrorInteractionNotAllowed, "interaction not allowed"},
		{keychain.ErrorItemNotFound, "no entry"},
		{keychain.ErrorAuthFailed, "authentication failed"},
		{keychain.ErrorNoSuchKeychain, "no such keychain"},
		{keychain.ErrorInvalidKeychain, "invalid keychain"},
		{keychain.ErrorNoAccessForItem, "no access for item"},
		{keychain.ErrorReadOnly, "read only"},
		{keychain.ErrorUserCanceled, "user canceled"},
		{keychain.ErrorParam, "invalid parameter"},
		{keychain.ErrorNotAvailable, "not available"},
		{keychain.ErrorDecode, "bad encoding"},
		{errors.New("wrapped"), "unknown error"},
	} {
		message := keychainError("read", test.err).Error()
		if !strings.Contains(message, "read") || !strings.Contains(message, test.want) {
			t.Errorf("message = %q, want the operation read and %q", message, test.want)
		}
		if strings.Contains(message, account) {
			t.Errorf("message %q contains the account", message)
		}
		if strings.Contains(message, secret) {
			t.Errorf("message %q contains the secret", message)
		}
	}
}
