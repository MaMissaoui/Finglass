// PIN lifecycle and the fetch entry point, ported from FinDesk's
// FinTSClient.kt and crates/finglass-core/src/sync/client.rs.
//
// A stored PIN is used at most once per dialog, only for the login it was
// saved for, and is dropped before the user is asked again after a rejection.
// A PIN is saved only when the user ticked "remember" and the dialog
// succeeded.
package banksync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// FinTsClient turns a stored bank access into a FinTS connection and runs the
// PIN lifecycle around one read-only dialog.
type FinTsClient struct {
	gateway     FinTsGateway
	dataDir     string
	productID   string
	credentials credentials.Store
	language    i18n.Language
	pinNotSaved atomic.Bool
}

// NewFinTsClient builds a client. productID is the caller's FinTS registration
// id; core never hard-codes it.
//
// dataDir must be the profile's canonical folder, as built by
// paths.AppPaths.ProfileDir; remembered PINs are scoped by that folder, and
// ForgetPinsForProfile uses the same value.
func NewFinTsClient(gateway FinTsGateway, dataDir, productID string, store credentials.Store) *FinTsClient {
	return &FinTsClient{
		gateway:     gateway,
		dataDir:     dataDir,
		productID:   productID,
		credentials: store,
		language:    i18n.En,
	}
}

// WithLanguage sets the language used for user-visible error text.
func (c *FinTsClient) WithLanguage(language i18n.Language) *FinTsClient {
	c.language = language
	return c
}

// Language returns the language used for user-visible error text.
func (c *FinTsClient) Language() i18n.Language {
	return c.language
}

// DataDir returns the profile's data directory, which scopes remembered PINs.
func (c *FinTsClient) DataDir() string {
	return c.dataDir
}

// ProductID returns the configured FinTS product id.
func (c *FinTsClient) ProductID() string {
	return c.productID
}

// CanRememberPin reports whether the system offers a credential store, so the
// UI may offer to remember the PIN.
func (c *FinTsClient) CanRememberPin() bool {
	return c.credentials.IsAvailable()
}

// PinNotSaved reports whether the last sync's opted-in PIN could not be saved.
// The view model shows a short, non-secret note.
func (c *FinTsClient) PinNotSaved() bool {
	return c.pinNotSaved.Load()
}

// ConnectionFor builds the connection for a stored bank access.
//
// An offline bank never reaches FinTS. A missing server URL is an error here:
// the bank directory is not ported (the data licence is still pending), so the
// caller must have set the URL.
func (c *FinTsClient) ConnectionFor(_ context.Context, _ *db.Database, bank model.BankAccessItem) (FinTsConnection, error) {
	if bank.Offline {
		return FinTsConnection{}, NewBankingError(
			CodeOffline,
			i18n.Text(c.language, i18n.ErrorBankOffline),
		)
	}
	serverURL := ""
	if bank.ServerURL != nil {
		serverURL = strings.TrimSpace(*bank.ServerURL)
	}
	if serverURL == "" {
		return FinTsConnection{}, NewBankingError(
			CodeNoServer,
			i18n.Format(c.language, i18n.ErrorNoFintsServer, i18n.Str(bank.BLZ)),
		)
	}
	if strings.TrimSpace(bank.UserID) == "" {
		return FinTsConnection{}, NewBankingError(
			CodeNoUser,
			i18n.Format(c.language, i18n.ErrorEnterUserId, i18n.Str(bank.BankName)),
		)
	}

	return FinTsConnection{
		BankName:   bank.BankName,
		BLZ:        bank.BLZ,
		UserID:     bank.UserID,
		CustomerID: bank.UserID,
		ServerURL:  serverURL,
		TanMethod:  bank.TanMethod,
		ProductID:  c.productID,
	}, nil
}

// FetchAll fetches accounts, balances and bookings for one bank access in a
// single dialog.
//
// Only this call may use a stored PIN. It belongs to the current login and the
// credential store still has it.
func (c *FinTsClient) FetchAll(
	ctx context.Context,
	database *db.Database,
	bank model.BankAccessItem,
	interaction BankingInteraction,
	since func(accountNumber string) *civil.Date,
	to civil.Date,
) ([]AccountSnapshot, error) {
	c.pinNotSaved.Store(false)

	stored := c.storedPinFor(ctx, database, bank)
	onStoredRejected := func() {
		c.ForgetPin(ctx, database, bank.ID)
	}
	remembering := newRememberingInteraction(interaction, stored, onStoredRejected)

	connection, err := c.ConnectionFor(ctx, database, bank)
	if err != nil {
		return nil, err
	}
	outcome, gatewayErr := c.gateway.FetchAll(connection, remembering, since, to)

	var mapped *BankingError
	if gatewayErr != nil {
		var gatewayValue GatewayError
		if errors.As(gatewayErr, &gatewayValue) {
			value := c.mapGatewayError(gatewayValue)
			mapped = &value
		} else {
			value := NewBankingError(CodeFintsError, gatewayErr.Error())
			mapped = &value
		}
	}

	c.finishFetchAll(ctx, database, bank.ID, remembering, mapped)

	if mapped != nil {
		return nil, *mapped
	}

	if outcome.TanMethod != nil &&
		(bank.TanMethod == nil || *bank.TanMethod != *outcome.TanMethod) {
		_, _ = bankconnections.UpdateConnection(ctx, database, bank.ID, model.BankConnectionUpdate{
			TanMethod: outcome.TanMethod,
		})
	}
	if outcome.Capabilities != nil {
		serialized := outcome.Capabilities.Serialize()
		_, _ = bankconnections.UpdateCapabilities(ctx, database, bank.ID, serialized)
	}
	return outcome.Snapshots, nil
}

// FinishFetchAll settles the PIN after a sync dialog: save an opted-in PIN
// after success, drop a stored PIN the bank rejected. Cancellation keeps the
// stored PIN (nothing changed at the bank), and a store failure never fails the
// sync.
func (c *FinTsClient) finishFetchAll(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	remembering *rememberingInteraction,
	bankingError *BankingError,
) {
	entered := remembering.takeLastEntry()
	if bankingError == nil {
		if entered != nil && entered.Remember() && !entered.IsEmpty() {
			if c.savePin(ctx, database, bankID, *entered) != nil {
				c.pinNotSaved.Store(true)
			}
		}
		return
	}
	if bankingError.Code != CodeCancelled && remembering.storedUsed() {
		c.ForgetPin(ctx, database, bankID)
	}
}

// StoredPinFor returns the stored PIN for the current login, or nil.
//
// A stale flag (another login) forgets it; a missing entry clears the flag. A
// store that cannot be read is not used and nothing is deleted, so a locked
// keychain never costs the user the entry.
func (c *FinTsClient) storedPinFor(
	ctx context.Context,
	database *db.Database,
	bank model.BankAccessItem,
) *secret.Text {
	flagKey := PinFlagKey(bank.ID)
	flag, ok, err := settings.Get(ctx, database, flagKey)
	if err != nil || !ok {
		return nil
	}
	if flag != LoginSignature(bank) {
		// The PIN belongs to another login: using it would only lock this one.
		c.ForgetPin(ctx, database, bank.ID)
		return nil
	}
	stored, found, err := c.credentials.Read(PinKey(c.dataDir, int64(bank.ID)))
	if err != nil {
		// Locked or denied store: ask the user, but don't touch the entry or
		// the flag.
		return nil
	}
	if found && len(stored) > 0 {
		// NewText copies; the store's buffer is wiped so the PIN is held once.
		text := secret.NewText(stored)
		clear(stored)
		return text
	}
	_ = deleteSetting(ctx, database, flagKey)
	return nil
}

// SavePin stores an opted-in PIN. The marker goes first: if it cannot be
// written, the PIN is not saved at all and the credential store stays
// untouched.
func (c *FinTsClient) savePin(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	entry PinEntry,
) error {
	bank, ok, err := bankconnections.GetItem(ctx, database, bankID)
	if err != nil {
		return internalError(err)
	}
	if !ok {
		return nil
	}

	marker := PinMarkerPath(c.dataDir, int64(bankID))
	if err := ensureMarker(marker); err != nil {
		return NewBankingError(CodePinNotSaved, err.Error())
	}

	key := PinKey(c.dataDir, int64(bankID))
	var writeErr error
	if entry.Pin() == nil {
		writeErr = c.credentials.Write(key, nil)
	} else {
		writeErr = entry.Pin().Use(func(value []byte) error {
			return c.credentials.Write(key, value)
		})
	}
	if writeErr != nil {
		return NewBankingError(CodePinNotSaved, writeErr.Error())
	}

	if err := settings.Set(ctx, database, PinFlagKey(bankID), LoginSignature(bank)); err != nil {
		return internalError(err)
	}
	_ = audit.Log(
		ctx,
		database,
		CodePinSaved,
		audit.ActorUser,
		fmt.Sprintf("Saved PIN for bank access %d", int64(bankID)),
	)
	return nil
}

// ensureMarker writes the empty marker file with 0600 permissions in a 0700
// directory. This is deliberately stricter than Rust's File::create, which
// takes the process umask.
func ensureMarker(marker string) error {
	directory := filepath.Dir(marker)
	if err := paths.EnsureDir(directory); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.OpenFile(filepath.Base(marker), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// ForgetPin deletes a stored PIN and its flag. The flag goes first, so a store
// failure can never keep a rejected PIN in use.
func (c *FinTsClient) ForgetPin(ctx context.Context, database *db.Database, bankID model.BankConnectionID) {
	flagKey := PinFlagKey(bankID)
	_, wasSaved, err := settings.Get(ctx, database, flagKey)
	if err != nil {
		wasSaved = false
	}
	_ = deleteSetting(ctx, database, flagKey)
	// Also without a flag: an entry whose flag write failed must not linger in
	// the store. The marker stays when the store delete fails, so a later
	// attempt still knows the entry exists.
	if c.credentials.Delete(PinKey(c.dataDir, int64(bankID))) == nil {
		_ = os.Remove(PinMarkerPath(c.dataDir, int64(bankID)))
	}
	// Logged only when a PIN was remembered, not on every bank removal or
	// user-id change.
	if wasSaved {
		_ = audit.Log(
			ctx,
			database,
			CodePinForgotten,
			audit.ActorUser,
			fmt.Sprintf("Removed stored PIN of bank access %d", int64(bankID)),
		)
	}
}

// HasSavedPin reports whether a saved PIN exists for bankID; it reads only the
// settings flag, never the credential store.
func (c *FinTsClient) HasSavedPin(ctx context.Context, database *db.Database, bankID model.BankConnectionID) bool {
	_, ok, err := settings.Get(ctx, database, PinFlagKey(bankID))
	return err == nil && ok
}

// DeleteStoredCredentials removes the bank's stored registration and any
// remembered PIN; call when the bank access is deleted.
func (c *FinTsClient) DeleteStoredCredentials(ctx context.Context, database *db.Database, bankID model.BankConnectionID) {
	// The connection row is deleted right after, so a failed clear changes
	// nothing; the PIN is what matters here.
	_ = c.ResetLogin(ctx, database, bankID, true)
}

// ResetLogin forgets everything tied to the old login after a login field
// (BLZ, user id or server URL) changed.
//
// The server state lives in the connection rows (TAN method, capabilities,
// server-state columns), so clearing it through the repository also clears
// whatever M5b stores there; there is no side file to remove.
//
// A failed clear is an error: saving new login fields over the old server
// state would make the edit silently ineffective. Forgetting the PIN stays
// best-effort (the marker keeps a failed store delete findable).
func (c *FinTsClient) ResetLogin(
	ctx context.Context,
	database *db.Database,
	bankID model.BankConnectionID,
	forgetPin bool,
) error {
	_, cleared := bankconnections.ClearLoginState(ctx, database, bankID)
	// The PIN goes even when the clear failed: a removed or changed login must
	// never keep a usable secret.
	if forgetPin {
		c.ForgetPin(ctx, database, bankID)
	}
	if cleared != nil {
		return internalError(cleared)
	}
	return nil
}

func (c *FinTsClient) mapGatewayError(gatewayError GatewayError) BankingError {
	if gatewayError.Kind() == GatewayErrorCancelled {
		return NewBankingError(CodeCancelled, i18n.Text(c.language, i18n.ErrorCancelledByUser))
	}
	return NewBankingError(CodeFintsError, gatewayError.Describe(c.language, nil))
}

// PinFlagKey is the settings flag key for a bank access's remembered PIN.
func PinFlagKey(bankID model.BankConnectionID) string {
	return "fints.pinSaved." + strconv.FormatInt(int64(bankID), 10)
}

// LoginSignature is the login signature stored in the flag: a stored PIN is
// never used for another login.
func LoginSignature(bank model.BankAccessItem) string {
	return bank.BLZ + "|" + bank.UserID
}

func deleteSetting(ctx context.Context, database *db.Database, key string) error {
	return database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "DELETE FROM settings WHERE key = ?1", key)
		return err
	})
}

func internalError(err error) BankingError {
	if err == nil {
		return BankingError{}
	}
	return NewBankingError(CodeSyncFailed, err.Error())
}

// rememberingInteraction wraps the caller's interaction for a sync: a stored
// PIN answers the first prompt, everything else is delegated, and after the
// bank rejects it the entry is dropped before the user is asked again.
type rememberingInteraction struct {
	inner            BankingInteraction
	stored           *secret.Text
	onStoredRejected func()

	mu        sync.Mutex
	used      bool
	dropped   bool
	lastEntry *PinEntry
}

func newRememberingInteraction(
	inner BankingInteraction,
	stored *secret.Text,
	onStoredRejected func(),
) *rememberingInteraction {
	return &rememberingInteraction{
		inner:            inner,
		stored:           stored,
		onStoredRejected: onStoredRejected,
	}
}

func (r *rememberingInteraction) storedUsed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.used
}

func (r *rememberingInteraction) takeLastEntry() *PinEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.lastEntry
	r.lastEntry = nil
	return entry
}

// Pin answers with the stored PIN once, then delegates. A later call means the
// bank rejected the PIN: it is forgotten before the user is asked again, so
// cancelling at the re-prompt (or a timeout) can never keep a PIN the bank
// refuses.
func (r *rememberingInteraction) Pin(bankName, userID string, offerRemember bool) *PinEntry {
	r.mu.Lock()
	if r.stored != nil && !r.used {
		r.used = true
		r.mu.Unlock()
		entry := NewPinEntry(r.stored, false)
		return &entry
	}
	dropStored := r.used && !r.dropped
	if dropStored {
		r.dropped = true
	}
	r.mu.Unlock()

	if dropStored {
		r.onStoredRejected()
	}

	entry := r.inner.Pin(bankName, userID, offerRemember)

	r.mu.Lock()
	if entry == nil {
		r.lastEntry = nil
	} else {
		copied := *entry
		r.lastEntry = &copied
	}
	r.mu.Unlock()
	return entry
}

func (r *rememberingInteraction) SelectTanMethod(options []TanMethod) *string {
	return r.inner.SelectTanMethod(options)
}

func (r *rememberingInteraction) SelectTanMedium(options []string) *string {
	return r.inner.SelectTanMedium(options)
}

func (r *rememberingInteraction) Tan(challenge TanChallenge) *secret.Text {
	return r.inner.Tan(challenge)
}

func (r *rememberingInteraction) ConfirmDecoupled(message string) bool {
	return r.inner.ConfirmDecoupled(message)
}

func (r *rememberingInteraction) Info(message string) {
	r.inner.Info(message)
}

// Ensure the wrapper satisfies the interaction port.
var _ BankingInteraction = (*rememberingInteraction)(nil)
