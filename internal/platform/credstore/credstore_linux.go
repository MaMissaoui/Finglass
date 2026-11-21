//go:build linux

package credstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	kdbus "github.com/keybase/dbus"
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

	// defaultCallTimeout bounds every D-Bus call this file makes itself.
	defaultCallTimeout = 2 * time.Second
	// defaultSessionOpenTimeout bounds the library's DH-AES handshake.
	defaultSessionOpenTimeout = 3 * time.Second
	// unavailableCache is how long an unavailable result is remembered, so a
	// hung or absent service is not asked again on every UI refresh.
	unavailableCache = 5 * time.Second
)

// linuxStore is the Linux Secret Service, default collection.
//
// It keeps one private session-bus connection per store for the process
// lifetime, replacing it only after the D-Bus library closed it (the library
// never exposes a close). Every D-Bus call this file makes itself has a
// deadline. A failed availability check costs at most one deadline: a timeout
// drops the session locally and keeps the client, and only an error reply
// closes the session and pings. The library calls it relies on
// (SearchCollection, GetSecret, CreateItem, DeleteItem) have no deadlines;
// that is accepted because they only run after a successful, deadline-bounded
// availability check.
type linuxStore struct {
	service string

	mu      sync.Mutex
	client  *secretservice.SecretService
	session *secretservice.Session

	callTimeout        time.Duration
	sessionOpenTimeout time.Duration

	// unavailableUntil caches an unavailable result for IsAvailable only.
	// Read, Write and Delete always try, so unlocking the keyring is noticed.
	unavailableUntil time.Time
	now              func() time.Time
}

// NewSystem returns the Secret Service store for service.
//
// The D-Bus session and the default collection are opened on the first call,
// so constructing the store cannot prompt or fail.
func NewSystem(service string) credentials.Store {
	return newStore(service, time.Now)
}

// newStore builds the concrete store. Tests inject now to control the
// availability cache.
func newStore(service string, now func() time.Time) *linuxStore {
	if now == nil {
		now = time.Now
	}
	return &linuxStore{
		service:            service,
		callTimeout:        defaultCallTimeout,
		sessionOpenTimeout: defaultSessionOpenTimeout,
		now:                now,
	}
}

// IsAvailable reports whether a Secret Service is reachable and its default
// collection is unlocked. It never prompts. An unavailable result is cached
// for a few seconds so a hung service cannot block every caller.
func (l *linuxStore) IsAvailable() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.clock().Before(l.unavailableUntil) {
		return false
	}
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
		l.handleErrorLocked(err)
		return nil, false, linuxError("read")
	}
	if len(items) == 0 {
		return nil, false, nil
	}

	plaintext, err := l.client.GetSecret(items[0], *l.session)
	if err != nil {
		l.handleErrorLocked(err)
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
		l.handleErrorLocked(err)
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
		l.handleErrorLocked(err)
		return linuxError("delete")
	}
	for _, item := range items {
		if err := l.client.DeleteItem(item); err != nil {
			l.handleErrorLocked(err)
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
// exists and is unlocked, then maintains the availability cache. A missing
// service or a locked collection means the store is unavailable; this never
// prompts.
func (l *linuxStore) prepareLocked() error {
	if err := l.prepare(); err != nil {
		l.unavailableUntil = l.clock().Add(unavailableCache)
		return err
	}
	l.unavailableUntil = time.Time{}
	return nil
}

// prepare does the work behind prepareLocked without touching the cache.
func (l *linuxStore) prepare() error {
	if l.client == nil {
		client, err := secretservice.NewService()
		if err != nil {
			return credentials.ErrUnavailable
		}
		client.SetSessionOpenTimeout(l.sessionOpenTimeout)
		l.client = client
	}
	if l.session == nil {
		session, err := openSession(l.client)
		if err != nil {
			l.handleOpenSessionErrorLocked(err)
			return credentials.ErrUnavailable
		}
		l.session = session
	}

	locked, err := l.collectionLocked()
	if err != nil {
		// The alias does not exist or the daemon went away: drop the stale
		// session, but keep a live bus connection.
		l.handleErrorLocked(err)
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
// default collection with a deadline. An error means the alias does not
// exist, which is as unusable as a locked collection.
func (l *linuxStore) collectionLocked() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), l.callTimeout)
	defer cancel()

	var locked bool
	err := l.client.Obj(secretservice.DefaultCollection).
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.Secret.Collection", "Locked").
		Store(&locked)
	if err != nil {
		return false, err
	}
	return locked, nil
}

// openSession opens a DH-AES encrypted session. Plaintext is used only when
// the service refuses DH-AES with an error reply; a timeout, a closed
// connection or any other failure returns the error, never a plaintext session.
func openSession(client *secretservice.SecretService) (*secretservice.Session, error) {
	session, err := client.OpenSession(secretservice.AuthenticationDHAES)
	if err == nil {
		return session, nil
	}
	if errorKind(err) != reply {
		return nil, err
	}
	return client.OpenSession(secretservice.AuthenticationInsecurePlain)
}

// kind classifies a D-Bus failure by what it proves about the connection.
type kind int

const (
	// other is a failure that says nothing about the connection.
	other kind = iota
	// closed means the D-Bus library closed the connection.
	closed
	// reply is an error reply from the service: the connection is open.
	reply
	// timeout is a deadline reached while waiting. A closed connection fails
	// at once, so a timeout proves the connection is open.
	timeout
)

// errorKind classifies err. The library wraps its errors with %w and returns
// D-Bus error replies as either a value or a pointer, so both are matched.
func errorKind(err error) kind {
	if err == nil {
		return other
	}
	if errors.Is(err, kdbus.ErrClosed) {
		return closed
	}
	var replyValue kdbus.Error
	if errors.As(err, &replyValue) {
		return reply
	}
	var replyPointer *kdbus.Error
	if errors.As(err, &replyPointer) {
		return reply
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return timeout
	}
	return other
}

// handleErrorLocked cleans up after a failed D-Bus call with one deadline at
// most. A closed connection has nothing left to close or ping. A timeout
// proves the connection is open, so the client stays and needs no ping; the
// session is dropped locally because closing it would wait for the same slow
// service. An error reply or an unclassified failure keeps the current
// cleanup: close the session, then ping to notice a closed connection.
func (l *linuxStore) handleErrorLocked(err error) {
	switch errorKind(err) {
	case closed:
		l.wipeSessionLocked()
		l.client = nil
	case timeout:
		l.wipeSessionLocked()
	default:
		l.resetSessionLocked()
		l.dropClientIfClosedLocked(err)
	}
}

// handleOpenSessionErrorLocked cleans up after openSession fails. The library
// reports its own session-open timeout as a plain error, while a D-Bus deadline
// is context.DeadlineExceeded; both mean the same here: keep the client and do
// not ping it.
func (l *linuxStore) handleOpenSessionErrorLocked(err error) {
	if errorKind(err) == other {
		l.wipeSessionLocked()
		return
	}
	l.handleErrorLocked(err)
}

// wipeSessionLocked forgets the session and wipes its key without calling the
// service. It is for a connection that is closed or too slow to answer.
func (l *linuxStore) wipeSessionLocked() {
	if l.session == nil {
		return
	}
	clear(l.session.AESKey)
	l.session.Private = nil
	l.session = nil
}

// resetSessionLocked closes the session with a deadline and wipes its key. The
// bus connection stays: it is healthy, and the D-Bus library only closes it when the
// transport dies.
func (l *linuxStore) resetSessionLocked() {
	if l.session == nil {
		return
	}
	if l.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), l.callTimeout)
		// A session that cannot be closed is already gone; its error is
		// deliberately ignored.
		_ = l.client.Obj(l.session.Path).
			CallWithContext(ctx, "org.freedesktop.Secret.Session.Close", 0).Err
		cancel()
	}
	l.wipeSessionLocked()
}

// dropClientIfClosedLocked pings the service and drops the client only when
// the D-Bus client has already closed the connection. A D-Bus error reply
// (for example no service on the bus) or a timeout keeps the client, so a live
// connection is never replaced. Dropping a connection the library closed leaks
// nothing.
func (l *linuxStore) dropClientIfClosedLocked(err error) {
	if l.client == nil || err == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), l.callTimeout)
	pingErr := l.client.Obj(secretservice.DefaultCollection).
		CallWithContext(ctx, "org.freedesktop.DBus.Peer.Ping", 0).Err
	cancel()

	// keybase/go-keychain talks D-Bus through the keybase fork of godbus,
	// whose connections close themselves on a transport error and then
	// return its ErrClosed.
	if errors.Is(pingErr, kdbus.ErrClosed) {
		l.resetSessionLocked()
		l.client = nil
	}
}

// clock returns the store's current time.
func (l *linuxStore) clock() time.Time {
	if l.now == nil {
		return time.Now()
	}
	return l.now()
}

// linuxError names the operation and a fixed kind only. D-Bus error names vary
// between daemons and can carry an object path, so they are not echoed.
func linuxError(operation string) error {
	return fmt.Errorf("credential store %s failed: secret service error", operation)
}
