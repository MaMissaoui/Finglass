//go:build linux

package credstore

import (
	"errors"
	"fmt"
	"sync"

	"github.com/keybase/go-keychain/secretservice"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

const (
	// secretServiceLabel is the human-readable item label in the collection.
	secretServiceLabel = "Finglass PIN"
	// attributeService and attributeAccount scope an item to one Finglass
	// service and one account key, like the Keychain's service and account.
	attributeService = "service"
	attributeAccount = "account"
)

// linuxStore is the Linux Secret Service, default collection.
type linuxStore struct {
	service string

	mu      sync.Mutex
	client  *secretservice.SecretService
	session *secretservice.Session
}

// NewSystem returns the Secret Service store for service.
//
// The D-Bus session and the default collection are opened on the first call,
// so constructing the store cannot prompt or fail.
func NewSystem(service string) credentials.Store {
	return &linuxStore{service: service}
}

// IsAvailable reports whether a Secret Service is reachable and its default
// collection is unlocked. It never prompts.
func (l *linuxStore) IsAvailable() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prepareLocked() == nil
}

// Read returns a copy of the stored secret, or found=false when the account is
// absent.
func (l *linuxStore) Read(account string) ([]byte, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.prepareLocked(); err != nil {
		return nil, false, err
	}
	items, err := l.client.SearchCollection(secretservice.DefaultCollection, l.attributes(account))
	if err != nil {
		l.resetLocked()
		return nil, false, linuxError("read")
	}
	if len(items) == 0 {
		return nil, false, nil
	}

	plaintext, err := l.client.GetSecret(items[0], *l.session)
	if err != nil {
		l.resetLocked()
		return nil, false, linuxError("read")
	}
	secret := make([]byte, len(plaintext))
	copy(secret, plaintext)
	clear(plaintext)
	return secret, true, nil
}

// Write stores or replaces the secret.
func (l *linuxStore) Write(account string, secret []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.prepareLocked(); err != nil {
		return err
	}

	value := append([]byte(nil), secret...)
	payload, err := l.session.NewSecret(value)
	if err == nil {
		properties := secretservice.NewSecretProperties(secretServiceLabel, l.attributes(account))
		_, err = l.client.CreateItem(
			secretservice.DefaultCollection,
			properties,
			payload,
			secretservice.ReplaceBehaviorReplace,
		)
	}
	clear(value)
	if err != nil {
		l.resetLocked()
		return linuxError("write")
	}
	return nil
}

// Delete removes the secret. A missing entry is not an error.
func (l *linuxStore) Delete(account string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.prepareLocked(); err != nil {
		return err
	}
	items, err := l.client.SearchCollection(secretservice.DefaultCollection, l.attributes(account))
	if err != nil {
		l.resetLocked()
		return linuxError("delete")
	}
	for _, item := range items {
		if err := l.client.DeleteItem(item); err != nil {
			l.resetLocked()
			return linuxError("delete")
		}
	}
	return nil
}

// attributes is the item identity: one service and one account key.
func (l *linuxStore) attributes(account string) secretservice.Attributes {
	return secretservice.Attributes{
		attributeService: l.service,
		attributeAccount: account,
	}
}

// prepareLocked opens the D-Bus session and checks that the default collection
// exists and is unlocked. A missing service or a locked collection means the
// store is unavailable; this never prompts.
func (l *linuxStore) prepareLocked() error {
	if l.client == nil {
		client, err := secretservice.NewService()
		if err != nil {
			return credentials.ErrUnavailable
		}
		l.client = client
	}
	if l.session == nil {
		session, err := openSession(l.client)
		if err != nil {
			l.resetLocked()
			return credentials.ErrUnavailable
		}
		l.session = session
	}

	locked, err := l.collectionLocked()
	if err != nil {
		// The alias does not exist or the daemon went away: drop the stale
		// session.
		l.resetLocked()
		return credentials.ErrUnavailable
	}
	if locked {
		// Keep the session: every call reports unavailable until the person
		// unlocks the collection themselves, and no call prompts.
		return credentials.ErrUnavailable
	}
	return nil
}

// collectionLocked reads org.freedesktop.Secret.Collection.Locked from the
// default collection. An error means the alias does not exist, which is as
// unusable as a locked collection.
func (l *linuxStore) collectionLocked() (bool, error) {
	property, err := l.client.Obj(secretservice.DefaultCollection).
		GetProperty("org.freedesktop.Secret.Collection.Locked")
	if err != nil {
		return false, err
	}
	locked, ok := property.Value().(bool)
	if !ok {
		return false, errors.New("locked property is not a boolean")
	}
	return locked, nil
}

// openSession opens a DH-AES encrypted session. plain is only for a service
// that refuses the DH handshake; every daemon Finglass has met accepts DH-AES,
// so it stays the default.
func openSession(client *secretservice.SecretService) (*secretservice.Session, error) {
	session, err := client.OpenSession(secretservice.AuthenticationDHAES)
	if err == nil {
		return session, nil
	}
	return client.OpenSession(secretservice.AuthenticationInsecurePlain)
}

// resetLocked drops a dead session so the next call opens a new one. The
// session's AES key is wiped first.
func (l *linuxStore) resetLocked() {
	if l.session != nil {
		clear(l.session.AESKey)
		if l.client != nil {
			l.client.CloseSession(l.session)
		}
	}
	l.session = nil
	l.client = nil
}

// linuxError names the operation and a fixed kind only. D-Bus error names vary
// between daemons and can carry an object path, so they are not echoed.
func linuxError(operation string) error {
	return fmt.Errorf("credential store %s failed: secret service error", operation)
}
