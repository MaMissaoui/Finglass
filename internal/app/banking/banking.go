// Package banking ports crates/finglass-core/src/app/banking/: the bank-login
// part of the Accounts page and the bridge to the UI.
//
// This is core only: the state, the actions and the blocking prompt bridge.
// Every action takes a *db.Database and the banksync.BankingService; no method
// holds a lock while a gateway call runs, because a dialog may wait minutes for
// a PIN or a TAN.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
//
// Threading contract, as in Rust's UI:
//
//   - The UI calls BeginSync, BeginSyncAll or BeginHistoryFrom on the UI thread
//     under the banking lock.
//   - It then calls Clone, releases the lock and runs RunSync, RunSyncAll or
//     RunHistoryFrom on a worker with that deep copy.
//   - The worker's result is published back to the UI afterwards.
//
// PromptBridge is the one type here that is safe for concurrent use; see its
// documentation.
package banking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
)

const (
	auditRenameBankAccess     = "RENAME_BANK_ACCESS"
	auditUpdateBankConnection = "UPDATE_BANK_CONNECTION"
	auditSetSyncFrom          = "SET_SYNC_FROM"
	auditSyncAll              = "SYNC_ALL"

	codeInvalid = "INVALID"
)

// SyncAllProgress is the progress of a "Sync all" run.
type SyncAllProgress struct {
	// Position is the bank just started, 1-based.
	Position int
	// Total is how many banks the run includes.
	Total int
	// BankName is the bank's display name.
	BankName string
}

// BankItem is one bank connection as the bank-login dialogs need it.
type BankItem struct {
	// ID is the row id.
	ID model.BankConnectionID
	// BankName is the bank's display name.
	BankName string
	// BLZ is the German bank code; empty for an offline connection.
	BLZ string
	// UserID is the online-banking user id; empty for an offline connection.
	UserID string
	// ServerURL is the FinTS server URL.
	ServerURL *string
	// Offline says whether this connection never goes online.
	Offline bool
	// IncludeInSyncAll says whether "Sync all" includes this connection.
	IncludeInSyncAll bool
	// SyncFrom is the start date of the next history window.
	SyncFrom *civil.Date
	// LastSync is the last successful sync.
	LastSync *time.Time
	// HasSavedPin says whether a PIN is remembered for this login.
	HasSavedPin bool
	// CanTestConnection says whether the bank has a server URL, so the
	// read-only connection test can run.
	CanTestConnection bool
}

// ConnectionTestResult is the latest read-only connection test, as the Banking
// page shows it. It never carries bank text or trace bytes.
type ConnectionTestResult struct {
	// BankID is the bank the test ran for.
	BankID model.BankConnectionID
	// Running says whether the test is still running.
	Running bool
	// Go is the probe's GO/NO-GO decision.
	Go bool
	// Reason is the localized reason.
	Reason string
	// Codes are the bank's four-digit return codes.
	Codes []string
	// Probed is the number of accounts the probe attempted.
	Probed int
	// Skipped is the number of accounts the probe skipped.
	Skipped int
}

// LastTrace is the last sync's or connection test's redacted trace.
type LastTrace struct {
	// Kind is "sync" or "test".
	Kind string
	// BankID is the bank the trace belongs to.
	BankID model.BankConnectionID
	// HasTrace says whether a redacted trace is in memory.
	HasTrace bool
}

// BankingState is the bank-login state: validation, connection editing, PIN
// forgetting and sync orchestration.
//
// The state is single-caller. Clone deep-copies it, including its slices and
// pointers, so a worker's copy shares nothing mutable with the UI's copy.
type BankingState struct {
	// Language is the language for user-visible texts.
	Language i18n.Language
	// Error is the current error text, already localized.
	Error *string
	// Message is the current success text, already localized.
	Message *string
	// SyncingBank is the bank a single sync is running for.
	SyncingBank *model.BankConnectionID
	// SyncStep is the current step of a single sync, for example "Connecting…".
	SyncStep *string
	// SyncAll is the progress of a "Sync all" run.
	SyncAll *SyncAllProgress
	// Banks holds every bank connection, in bank-card order.
	Banks []BankItem
	// CanRememberPin says whether the system offers a credential store, so the
	// UI may offer to remember a PIN.
	CanRememberPin bool
	// ConnectionTest is the latest read-only connection test, if any.
	ConnectionTest *ConnectionTestResult
	// LastTrace is the last sync's or connection test's redacted trace.
	LastTrace *LastTrace
}

// New builds empty state for language.
func New(language i18n.Language) *BankingState {
	return &BankingState{
		Language: language,
		Banks:    make([]BankItem, 0),
	}
}

// Default builds empty state in English, like Rust's BankingState::default.
func Default() *BankingState {
	return New(i18n.En)
}

// Clone returns a deep copy: slices, maps and pointers included, so the copy
// shares nothing mutable with the original.
func (s *BankingState) Clone() *BankingState {
	if s == nil {
		return nil
	}

	cloned := *s
	cloned.Error = cloneString(s.Error)
	cloned.Message = cloneString(s.Message)
	cloned.SyncStep = cloneString(s.SyncStep)
	if s.SyncingBank != nil {
		value := *s.SyncingBank
		cloned.SyncingBank = &value
	}
	if s.SyncAll != nil {
		value := *s.SyncAll
		cloned.SyncAll = &value
	}
	if s.ConnectionTest != nil {
		value := *s.ConnectionTest
		value.Codes = append([]string(nil), s.ConnectionTest.Codes...)
		cloned.ConnectionTest = &value
	}
	if s.LastTrace != nil {
		value := *s.LastTrace
		cloned.LastTrace = &value
	}
	cloned.Banks = make([]BankItem, len(s.Banks))
	for index, bank := range s.Banks {
		copied := bank
		copied.ServerURL = cloneString(bank.ServerURL)
		copied.SyncFrom = cloneDate(bank.SyncFrom)
		copied.LastSync = cloneTime(bank.LastSync)
		cloned.Banks[index] = copied
	}
	return &cloned
}

// IsSyncing reports whether a single sync or "Sync all" is running.
func (s *BankingState) IsSyncing() bool {
	return s.SyncingBank != nil || s.SyncAll != nil
}

// IsTesting reports whether a read-only connection test is running.
func (s *BankingState) IsTesting() bool {
	return s.ConnectionTest != nil && s.ConnectionTest.Running
}

// IsBusy reports whether a sync or a connection test is running.
func (s *BankingState) IsBusy() bool {
	return s.IsSyncing() || s.IsTesting()
}

// Load reloads the bank list, the remembered-PIN flags and the credential-store
// availability.
func (s *BankingState) Load(ctx context.Context, database *db.Database, service *banksync.BankingService) error {
	return s.Refresh(ctx, database, service)
}

// Refresh reloads the bank list, the remembered-PIN flags and the
// credential-store availability.
func (s *BankingState) Refresh(ctx context.Context, database *db.Database, service *banksync.BankingService) error {
	banks, err := bankconnections.ListItems(ctx, database)
	if err != nil {
		return banksync.NewBankingError(
			banksync.CodeSyncFailed,
			i18n.Text(s.Language, i18n.ErrorUnexpected),
		)
	}
	s.CanRememberPin = service.CanRememberPin()
	items := make([]BankItem, 0, len(banks))
	for _, bank := range banks {
		items = append(items, BankItem{
			ID:               bank.ID,
			BankName:         bank.BankName,
			BLZ:              bank.BLZ,
			UserID:           bank.UserID,
			ServerURL:        cloneString(bank.ServerURL),
			Offline:          bank.Offline,
			IncludeInSyncAll: bank.IncludeInSyncAll,
			SyncFrom:         cloneDate(bank.SyncFrom),
			LastSync:         cloneTime(bank.LastSync),
			HasSavedPin:      !bank.Offline && service.HasSavedPin(ctx, database, bank.ID),
			CanTestConnection: bank.ServerURL != nil &&
				strings.TrimSpace(*bank.ServerURL) != "",
		})
	}
	s.Banks = items
	return nil
}

// ValidateBankAccess validates a bank's name and BLZ; it returns an error text
// or nil.
func (s *BankingState) ValidateBankAccess(name, blz string) *string {
	if strings.TrimSpace(name) == "" {
		message := i18n.Text(s.Language, i18n.BankNameRequired)
		return &message
	}
	if !isEightDigits(strings.TrimSpace(blz)) {
		message := i18n.Text(s.Language, i18n.BlzMustBe8Digits)
		return &message
	}
	return nil
}

// ValidateServerURL validates the FinTS server URL: it is required and must use
// HTTPS.
func (s *BankingState) ValidateServerURL(serverURL string) *string {
	url := strings.TrimSpace(serverURL)
	if url == "" {
		message := i18n.Text(s.Language, i18n.ErrorServerUrlRequired)
		return &message
	}
	lower := asciiLower(url)
	if !strings.HasPrefix(lower, "https://") || len(lower) == len("https://") {
		message := i18n.Text(s.Language, i18n.ErrorServerUrlMustBeHttps)
		return &message
	}
	return nil
}

// AddBankAccess adds an online bank login. The server URL is required because
// Finglass has no bank directory yet.
func (s *BankingState) AddBankAccess(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	name,
	blz,
	bic,
	userID,
	serverURL string,
) error {
	if message := s.ValidateBankAccess(name, blz); message != nil {
		return s.recordErrorMessage(*message)
	}
	if message := s.ValidateServerURL(serverURL); message != nil {
		return s.recordErrorMessage(*message)
	}

	cleanName := strings.TrimSpace(name)
	config := banksync.NewFinTsConfig(
		cleanName,
		strings.TrimSpace(blz),
		strings.ToUpper(strings.TrimSpace(bic)),
		strings.TrimSpace(userID),
		"",
	)
	cleanURL := strings.TrimSpace(serverURL)
	config.ServerURL = &cleanURL

	if _, err := service.AddBankConnection(ctx, database, config); err != nil {
		return s.recordServiceError(err)
	}
	_ = s.Refresh(ctx, database, service)
	s.Error = nil
	message := i18n.Format(s.Language, i18n.MessageAdded, i18n.Str(cleanName))
	s.Message = &message
	return nil
}

// UpdateConnection saves a connection's settings. An offline bank only gets a
// new name; any sync in progress refuses the edit.
//
// The service resets the login (and forgets its PIN) when the BLZ, user id or
// server URL changed.
func (s *BankingState) UpdateConnection(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	id model.BankConnectionID,
	name,
	blz,
	userID,
	serverURL string,
) error {
	if s.SyncAll != nil || (s.SyncingBank != nil && *s.SyncingBank == id) {
		return s.recordErrorKey(i18n.ErrorConnectionBusy)
	}

	bank, ok, err := bankconnections.GetItem(ctx, database, id)
	if err != nil {
		return s.recordErrorKey(i18n.ErrorUnexpected)
	}
	if !ok {
		return s.recordErrorKey(i18n.ErrorBankConnectionNotFound)
	}

	if bank.Offline {
		if strings.TrimSpace(name) == "" {
			return s.recordErrorKey(i18n.BankNameRequired)
		}
		serverURLText := ""
		if bank.ServerURL != nil {
			serverURLText = *bank.ServerURL
		}
		updated, updateErr := service.UpdateConnection(
			ctx, database, id, name, bank.BLZ, bank.UserID, serverURLText,
		)
		return s.finishUpdate(ctx, database, service, id, bank, updated, updateErr)
	}

	if message := s.ValidateBankAccess(name, blz); message != nil {
		return s.recordErrorMessage(*message)
	}
	if message := s.ValidateServerURL(serverURL); message != nil {
		return s.recordErrorMessage(*message)
	}

	updated, updateErr := service.UpdateConnection(ctx, database, id, name, blz, userID, serverURL)
	return s.finishUpdate(ctx, database, service, id, bank, updated, updateErr)
}

func (s *BankingState) finishUpdate(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	id model.BankConnectionID,
	previous model.BankAccessItem,
	updated model.BankAccessItem,
	updateErr error,
) error {
	if updateErr != nil {
		return s.recordServiceError(updateErr)
	}

	// Audit details carry ids only; the old and new label are the one
	// exception, as in FinDesk.
	if previous.Offline {
		if updated.BankName != previous.BankName {
			_ = audit.Log(
				ctx,
				database,
				auditRenameBankAccess,
				audit.ActorUser,
				fmt.Sprintf(
					"Renamed bank access %d from %s to %s",
					int64(id), previous.BankName, updated.BankName,
				),
			)
		}
	} else {
		_ = audit.Log(
			ctx,
			database,
			auditUpdateBankConnection,
			audit.ActorUser,
			fmt.Sprintf("Updated connection settings of bank access %d", int64(id)),
		)
		if updated.BankName != previous.BankName {
			_ = audit.Log(
				ctx,
				database,
				auditRenameBankAccess,
				audit.ActorUser,
				fmt.Sprintf(
					"Renamed bank access %d from %s to %s",
					int64(id), previous.BankName, updated.BankName,
				),
			)
		}
	}

	_ = s.Refresh(ctx, database, service)
	s.Error = nil
	message := i18n.Text(s.Language, i18n.MessageConnectionSaved)
	s.Message = &message
	return nil
}

// ForgetPin removes the PIN remembered for id.
func (s *BankingState) ForgetPin(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	id model.BankConnectionID,
) error {
	service.ForgetPin(ctx, database, id)
	return s.Refresh(ctx, database, service)
}

// RemoveBank deletes an online bank login and its accounts.
//
// It is refused while this state syncs (the sync still inserts rows) or while
// the profile is busy (an import may do the same). The service forgets the
// remembered PIN and the FinTS login before it deletes the row.
func (s *BankingState) RemoveBank(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	busy *accountsview.Busy,
	id model.BankConnectionID,
) error {
	if s.IsSyncing() {
		return s.recordErrorKey(i18n.ErrorConnectionBusy)
	}
	if busy != nil && busy.Active() {
		return s.recordErrorKey(i18n.ErrorDeleteWhileBusy)
	}
	if _, err := service.RemoveBank(ctx, database, id); err != nil {
		return s.recordServiceError(err)
	}
	_ = s.Refresh(ctx, database, service)
	s.Error = nil
	message := i18n.Text(s.Language, i18n.MessageBankRemoved)
	s.Message = &message
	return nil
}

// ValidateSyncFrom validates a history start typed as "dd.MM.yyyy"; it returns
// an error text or nil.
func (s *BankingState) ValidateSyncFrom(input string, today civil.Date) *string {
	date, ok := format.ParseDate(input)
	if !ok {
		message := i18n.Text(s.Language, i18n.ErrorSyncFromDateInvalid)
		return &message
	}
	if date.After(today) {
		message := i18n.Text(s.Language, i18n.ErrorSyncFromDateFuture)
		return &message
	}
	return nil
}

// BeginHistoryFrom claims the state for a history sync of id from date. Call it
// on the UI thread before spawning RunHistoryFrom, like BeginSync, so a second
// click is refused at once.
func (s *BankingState) BeginHistoryFrom(id model.BankConnectionID, date, today civil.Date) bool {
	if s.IsSyncing() {
		s.setErrorKey(i18n.ErrorConnectionBusy)
		return false
	}
	if date.After(today) {
		s.setErrorKey(i18n.ErrorSyncFromDateFuture)
		return false
	}
	return s.BeginSync(id)
}

// RunHistoryFrom stores date as the start of the next sync's history window,
// then runs that sync for id under the profile's busy. It requires a successful
// BeginHistoryFrom.
func (s *BankingState) RunHistoryFrom(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	bridge *PromptBridge,
	busy *accountsview.Busy,
	id model.BankConnectionID,
	date,
	today civil.Date,
) {
	if _, err := bankconnections.SetSyncFrom(ctx, database, id, &date); err != nil {
		s.SyncingBank = nil
		s.SyncStep = nil
		s.setErrorKey(i18n.ErrorUnexpected)
		return
	}
	_ = audit.Log(
		ctx,
		database,
		auditSetSyncFrom,
		audit.ActorUser,
		fmt.Sprintf("Bank access %d: history from %s", int64(id), format.Date(date)),
	)
	s.RunSync(ctx, database, service, bridge, busy, id, today)
}

// ClearSyncFrom drops a stored history start so the next sync uses the normal
// delta window again.
func (s *BankingState) ClearSyncFrom(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	id model.BankConnectionID,
) error {
	if _, err := bankconnections.SetSyncFrom(ctx, database, id, nil); err != nil {
		return s.recordErrorKey(i18n.ErrorUnexpected)
	}
	return s.Refresh(ctx, database, service)
}

// SetIncludeInSyncAll sets whether "Sync all" includes id; it is refused while
// any sync runs, because the list is read at run start.
func (s *BankingState) SetIncludeInSyncAll(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	id model.BankConnectionID,
	include bool,
) error {
	if s.IsSyncing() {
		return s.recordErrorKey(i18n.ErrorConnectionBusy)
	}
	if _, err := bankconnections.SetIncludeInSyncAll(ctx, database, id, include); err != nil {
		return s.recordErrorKey(i18n.ErrorUnexpected)
	}
	return s.Refresh(ctx, database, service)
}

// BeginSync claims the state for a single sync on the calling thread. It
// returns false when a sync already runs or the bank is offline, with the error
// text published in Error.
func (s *BankingState) BeginSync(id model.BankConnectionID) bool {
	if s.IsSyncing() {
		s.setErrorKey(i18n.ErrorConnectionBusy)
		return false
	}
	var found *BankItem
	for index := range s.Banks {
		if s.Banks[index].ID == id {
			found = &s.Banks[index]
			break
		}
	}
	if found == nil {
		s.setErrorKey(i18n.ErrorBankConnectionNotFound)
		return false
	}
	if found.Offline {
		s.setErrorKey(i18n.ErrorBankOffline)
		return false
	}

	claimed := id
	s.SyncingBank = &claimed
	step := i18n.Text(s.Language, i18n.SyncConnecting)
	s.SyncStep = &step
	s.Error = nil
	s.Message = nil
	return true
}

// RunSync runs a single sync on the worker, inside the profile's Busy section,
// and publishes the outcome.
func (s *BankingState) RunSync(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	bridge *PromptBridge,
	busy *accountsview.Busy,
	id model.BankConnectionID,
	today civil.Date,
) {
	switch outcome := s.syncOne(ctx, database, service, bridge, busy, id, today); outcome.kind {
	case syncSuccess:
		s.Message = &outcome.summary
		s.Error = outcome.warning
	case syncFailed:
		s.Message = nil
		message := outcome.failure
		s.Error = &message
	case syncCancelled:
		s.Message = nil
		s.Error = nil
	}
}

// BeginSyncAll claims the state for a "Sync all" run on the calling thread. It
// returns false while a sync already runs.
func (s *BankingState) BeginSyncAll() bool {
	if s.IsSyncing() {
		s.setErrorKey(i18n.ErrorConnectionBusy)
		return false
	}
	s.SyncAll = &SyncAllProgress{}
	s.Error = nil
	s.Message = nil
	return true
}

// RunSyncAll runs "Sync all": included, non-offline banks in bank-card order,
// one dialog after another.
//
// onProgress is called after each progress update so the caller can publish the
// state from the worker; the state itself is not shared. A cancelled bank stops
// the run with i18n.SyncAllStopped.
func (s *BankingState) RunSyncAll(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	bridge *PromptBridge,
	busy *accountsview.Busy,
	today civil.Date,
	onProgress func(*BankingState),
) {
	banks, err := bankconnections.ListItems(ctx, database)
	if err != nil {
		s.SyncAll = nil
		s.setErrorKey(i18n.ErrorUnexpected)
		return
	}
	included := make([]model.BankAccessItem, 0, len(banks))
	for _, bank := range banks {
		if bank.IncludeInSyncAll && !bank.Offline {
			included = append(included, bank)
		}
	}

	if len(included) == 0 {
		s.SyncAll = nil
		s.Message = nil
		s.setErrorKey(i18n.SyncAllNothingIncluded)
		return
	}

	ids := make([]string, 0, len(included))
	for _, bank := range included {
		ids = append(ids, fmt.Sprintf("%d", int64(bank.ID)))
	}
	_ = audit.Log(
		ctx,
		database,
		auditSyncAll,
		audit.ActorUser,
		fmt.Sprintf("Sync all: bank access ids %s", strings.Join(ids, ", ")),
	)

	total := len(included)
	messages := make([]string, 0)
	failures := make([]string, 0)
	var stoppedAt *string

	for index, bank := range included {
		s.SyncAll = &SyncAllProgress{
			Position: index + 1,
			Total:    total,
			BankName: bank.BankName,
		}
		if onProgress != nil {
			onProgress(s)
		}

		switch outcome := s.syncOne(ctx, database, service, bridge, busy, bank.ID, today); outcome.kind {
		case syncSuccess:
			messages = append(messages, fmt.Sprintf("%s: %s", bank.BankName, outcome.summary))
			if outcome.warning != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", bank.BankName, *outcome.warning))
			}
		case syncFailed:
			failures = append(failures, fmt.Sprintf("%s: %s", bank.BankName, outcome.failure))
		case syncCancelled:
			name := bank.BankName
			stoppedAt = &name
		}
		if stoppedAt != nil {
			break
		}
	}

	if stoppedAt != nil {
		messages = append(messages, i18n.Format(s.Language, i18n.SyncAllStopped, i18n.Str(*stoppedAt)))
	}
	if len(messages) > 0 {
		message := strings.Join(messages, "\n")
		s.Message = &message
	} else {
		s.Message = nil
	}
	if len(failures) > 0 {
		failure := strings.Join(failures, "\n")
		s.Error = &failure
	} else {
		s.Error = nil
	}
	s.SyncAll = nil
}

// ClearMessages clears the current error and success text.
func (s *BankingState) ClearMessages() {
	s.Error = nil
	s.Message = nil
}

type syncOutcomeKind int

const (
	syncSuccess syncOutcomeKind = iota
	syncFailed
	syncCancelled
)

type syncOutcome struct {
	kind    syncOutcomeKind
	summary string
	warning *string
	failure string
}

// syncOne runs one bank's dialog and reports how it ended. The caller turns
// that into screen messages.
func (s *BankingState) syncOne(
	ctx context.Context,
	database *db.Database,
	service *banksync.BankingService,
	bridge *PromptBridge,
	busy *accountsview.Busy,
	id model.BankConnectionID,
	today civil.Date,
) syncOutcome {
	claimed := id
	s.SyncingBank = &claimed
	step := i18n.Text(s.Language, i18n.SyncConnecting)
	s.SyncStep = &step

	var report banksync.SyncReport
	var syncErr error
	if busy != nil {
		busy.Run(func() {
			report, syncErr = service.SyncBankConnection(ctx, database, id, bridge, today)
		})
	} else {
		report, syncErr = service.SyncBankConnection(ctx, database, id, bridge, today)
	}
	s.SyncingBank = nil
	s.SyncStep = nil

	outcome := syncOutcome{}
	if syncErr == nil {
		outcome.kind = syncSuccess
		outcome.summary = report.SummaryIn(s.Language)
		if service.PinNotSaved() {
			outcome.summary += "\n" + i18n.Text(s.Language, i18n.PinNotSaved)
		}
		if len(report.Warnings) > 0 {
			warning := strings.Join(report.Warnings, "\n")
			outcome.warning = &warning
		}
	} else {
		bankingErr := bankingErrorFrom(syncErr)
		if bankingErr.Code == banksync.CodeCancelled {
			outcome.kind = syncCancelled
		} else {
			outcome.kind = syncFailed
			outcome.failure = bankingErr.Message
		}
	}

	_ = s.Refresh(ctx, database, service)
	return outcome
}

func (s *BankingState) setErrorKey(key i18n.Key) {
	message := i18n.Text(s.Language, key)
	s.Error = &message
}

func (s *BankingState) recordErrorKey(key i18n.Key) banksync.BankingError {
	return s.recordErrorMessage(i18n.Text(s.Language, key))
}

func (s *BankingState) recordErrorMessage(message string) banksync.BankingError {
	s.Error = &message
	return banksync.NewBankingError(codeInvalid, message)
}

func (s *BankingState) recordServiceError(err error) banksync.BankingError {
	bankingErr := bankingErrorFrom(err)
	s.Error = &bankingErr.Message
	return bankingErr
}

func bankingErrorFrom(err error) banksync.BankingError {
	var bankingErr banksync.BankingError
	if errors.As(err, &bankingErr) {
		return bankingErr
	}
	if err == nil {
		return banksync.BankingError{}
	}
	return banksync.NewBankingError(banksync.CodeSyncFailed, err.Error())
}

func isEightDigits(text string) bool {
	if len(text) != 8 {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

func asciiLower(text string) string {
	var lowered strings.Builder
	lowered.Grow(len(text))
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lowered.WriteByte(character)
	}
	return lowered.String()
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneDate(value *civil.Date) *civil.Date {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
