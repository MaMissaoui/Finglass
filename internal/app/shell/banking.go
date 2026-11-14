package shell

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/connectiontest"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/fintsgateway"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/fintsproduct"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
	"github.com/MaMissaoui/Finglass/internal/fints/trace"
)

// OnlineSyncAvailable says whether the real FinTS gateway is wired in. It is
// Rust's ONLINE_SYNC_AVAILABLE: false while banksync.UnavailableGateway stands
// in for the M5b adapter. The sync controls follow it. The adapter slice will
// also require fintsproduct.Present(), so a build without the product id keeps
// online sync off even then.
const OnlineSyncAvailable = false

// connectionTestProductVersion is the HKVVB product version the connection
// test and the sync adapter send.
const connectionTestProductVersion = "0.4"

// connectionTestTransportFactory is the shell's seam for the test transport.
type connectionTestTransportFactory = connectiontest.TransportFactory

// Trace kinds of the one in-memory trace slot.
const (
	traceKindSync = "sync"
	traceKindTest = "test"
)

// lastTraceState is the last sync's or connection test's redacted trace, kept
// in memory only.
type lastTraceState struct {
	trace  []byte
	blz    string
	kind   string
	bankID model.BankConnectionID
}

// syncTraceSink holds the recorder of the sync currently running. A new
// recorder replaces the previous one, so only the last transport's trace is
// kept.
type syncTraceSink struct {
	recorder atomic.Pointer[trace.Recorder]
}

// wrap records inner for one sync.
func (s *syncTraceSink) wrap(inner fints.Transport) fints.Transport {
	recorder := trace.NewRecorder(inner)
	s.recorder.Store(recorder)
	return recorder
}

// bytes writes the recorded trace, or returns nil when no exchange happened.
func (s *syncTraceSink) bytes() []byte {
	recorder := s.recorder.Load()
	if recorder == nil || recorder.Len() == 0 {
		return nil
	}
	var buffer bytes.Buffer
	_, _ = recorder.WriteTo(&buffer)
	return buffer.Bytes()
}

// selectGateway picks the real FinTS gateway only when the build enables online
// sync and links a product id. newReal is called only in that case.
func selectGateway(
	available, productPresent bool,
	language i18n.Language,
	newReal func() banksync.FinTsGateway,
) banksync.FinTsGateway {
	if available && productPresent && newReal != nil {
		if gateway := newReal(); gateway != nil {
			return gateway
		}
	}
	return banksync.NewUnavailableGateway(language)
}

// bankingJob is one blocking banking call prepared under the session mutex and
// run after it, as Rust's spawn_action and start_sync do.
type bankingJob struct {
	generation uint64
	database   *db.Database
	service    *banksync.BankingService
	bridge     *banking.PromptBridge
	busy       accountsview.Busy
	pending    *banking.BankingState
	accounts   *accountsview.AccountsState
	work       func(context.Context, *db.Database, *banksync.BankingService, *banking.BankingState)
	// keepLive keeps a sync claimed after this action's snapshot was taken, as
	// Rust's keep_live_sync_fields does for spawn_action.
	keepLive bool
	// refreshDashboard starts a Dashboard load after the job. A sync changes
	// balances and bookings, so the Dashboard reloads like the Accounts page.
	refreshDashboard bool
	// mergeOnly keeps the live sync fields and merges only the freshly loaded
	// bank list, as banking::load does.
	mergeOnly bool
	// setLastTrace replaces the session's in-memory trace with the job's trace,
	// even when it is empty (a new sync or test started).
	setLastTrace bool
	lastTrace    lastTraceState
	// captureSyncTrace makes the job record its sync's trace into traceSink.
	captureSyncTrace bool
	traceSink        *syncTraceSink
	traceKind        string
	traceBankID      model.BankConnectionID
	traceBLZ         string
}

// bankingServiceLocked builds the service exactly as Rust's
// ServiceInputs::service: the gateway selected by selectGateway, the session's
// one credential store, the profile folder and the product id linked into the
// build (empty when absent). The caller holds the session mutex.
func (s *Session) bankingServiceLocked(language i18n.Language) (*banksync.BankingService, bool) {
	return s.bankingServiceWithTraceLocked(language, nil)
}

// bankingServiceWithTraceLocked is bankingServiceLocked with a trace sink for a
// sync. The caller holds the session mutex.
func (s *Session) bankingServiceWithTraceLocked(
	language i18n.Language,
	sink *syncTraceSink,
) (*banksync.BankingService, bool) {
	dir, err := s.paths.ProfileDir(startup.SlugKey(s.slug))
	if err != nil {
		return nil, false
	}
	gateway := selectGateway(OnlineSyncAvailable, fintsproduct.Present(), language, func() banksync.FinTsGateway {
		options := make([]fintsgateway.Option, 0, 1)
		if sink != nil {
			options = append(options, fintsgateway.WithTransportWrapper(sink.wrap))
		}
		return fintsgateway.New(options...)
	})
	if s.gatewayFactory != nil {
		gateway = s.gatewayFactory(language)
	} else if s.syncGatewayFactory != nil && sink != nil {
		gateway = s.syncGatewayFactory(language, sink)
	}
	productID, _ := fintsproduct.ID()
	client := banksync.NewFinTsClient(gateway, dir, productID, s.credentials).WithLanguage(language)
	return banksync.NewBankingService(client), true
}

// newLiveBridge builds a bridge whose onChange notifies the listeners only
// while it is the session's live bridge, as Rust's BridgeHandle token does: a
// torn-down profile's bridge never reaches the next profile's dialogs. The
// identity check reads s.bridge under the mutex; s.bridge is only swapped in the
// critical sections of newSession, Teardown and TeardownToUnlock. The caller
// need not hold the mutex.
func (s *Session) newLiveBridge() *banking.PromptBridge {
	var bridge *banking.PromptBridge
	bridge = banking.NewPromptBridge(PromptTimeout, func() {
		s.mu.Lock()
		live := s.bridge == bridge
		s.mu.Unlock()
		if live {
			s.notify(Change{Page: "banking-prompt"})
		}
	})
	return bridge
}

// BankingState returns a deep copy of the bank-login state.
func (s *Session) BankingState() banking.BankingState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.banking == nil {
		return banking.BankingState{}
	}
	return *s.banking.Clone()
}

// BankingLoad loads the bank list on a worker, as banking::load does.
func (s *Session) BankingLoad() {
	s.mu.Lock()
	job := s.beginBankingJobLocked(false, func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.Refresh(ctx, database, service)
	})
	if job != nil {
		job.mergeOnly = true
	}
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingRefresh reloads the bank list on a worker.
func (s *Session) BankingRefresh() {
	s.BankingLoad()
}

// beginBankingJobLocked prepares a banking job on a clone of the state. The
// caller holds the session mutex and starts the returned job after releasing
// it.
func (s *Session) beginBankingJobLocked(
	keepLive bool,
	work func(context.Context, *db.Database, *banksync.BankingService, *banking.BankingState),
) *bankingJob {
	if s.db == nil || s.banking == nil {
		return nil
	}
	service, ok := s.bankingServiceLocked(s.language)
	if !ok {
		return nil
	}
	generation, ok := s.beginJobLocked()
	if !ok {
		return nil
	}
	pending := s.banking.Clone()
	pending.Language = s.language
	accounts := s.accounts.Clone()
	return &bankingJob{
		generation: generation,
		database:   s.db,
		service:    service,
		bridge:     s.bridge,
		busy:       s.profileBusy,
		pending:    pending,
		accounts:   &accounts,
		work:       work,
		keepLive:   keepLive,
	}
}

// beginBankingActionLocked prepares Rust's spawn_action: the work runs on a
// clone, the account rows reload afterwards and a sync claimed meanwhile keeps
// its fields.
func (s *Session) beginBankingActionLocked(
	work func(context.Context, *db.Database, *banksync.BankingService, *banking.BankingState),
) *bankingJob {
	return s.beginBankingJobLocked(true, work)
}

// startBankingJob runs a prepared banking job.
func (s *Session) startBankingJob(job *bankingJob) {
	if job == nil {
		return
	}
	go s.runBankingJob(job)
}

func (s *Session) runBankingJob(job *bankingJob) {
	ctx := context.Background()
	job.work(ctx, job.database, job.service, job.pending)
	if job.captureSyncTrace {
		raw := job.traceSink.bytes()
		job.pending.LastTrace = &banking.LastTrace{
			Kind:     job.traceKind,
			BankID:   job.traceBankID,
			HasTrace: len(raw) > 0,
		}
		job.lastTrace = lastTraceState{
			trace:  append([]byte(nil), raw...),
			blz:    job.traceBLZ,
			kind:   job.traceKind,
			bankID: job.traceBankID,
		}
		job.setLastTrace = true
	}
	if job.accounts != nil {
		_ = job.accounts.LoadAccounts(ctx, job.database)
	}

	s.mu.Lock()
	current := s.jobs.IsCurrent(job.generation)
	var dashboardJob *pageJob
	if current {
		if job.setLastTrace {
			s.lastTrace = job.lastTrace
		}
		if job.mergeOnly && s.banking != nil {
			s.banking.Language = job.pending.Language
			s.banking.Banks = append([]banking.BankItem(nil), job.pending.Banks...)
			s.banking.CanRememberPin = job.pending.CanRememberPin
		} else {
			if job.keepLive {
				s.keepLiveSyncFieldsLocked(job.pending)
			}
			s.banking = job.pending
		}
		if job.accounts != nil {
			s.accounts = job.accounts
		}
		if job.refreshDashboard {
			dashboardJob = s.beginDashboardOpenLocked()
		}
	}
	s.jobs.EndGeneration(job.generation)
	s.mu.Unlock()

	if current {
		s.notifyBanking()
		s.startPageJob(dashboardJob)
	}
}

// refuseBankingIfSyncingLocked publishes Rust's refuse_if_syncing error. The
// caller holds the session mutex and notifies after releasing it.
func (s *Session) refuseBankingIfSyncingLocked() bool {
	if s.banking == nil || !s.banking.IsSyncing() {
		return false
	}
	message := i18n.Text(s.language, i18n.ErrorConnectionBusy)
	s.banking.Error = &message
	return true
}

// notifyBanking publishes a bank-login change. The Accounts page re-renders
// too, because Rust's publish applies the banking fields to the page.
func (s *Session) notifyBanking() {
	s.notify(Change{Page: "banking"})
	s.notify(Change{Page: "accounts"})
}

// notifyBankingAndAccounts publishes a change that touched both states. The
// notification is the same as notifyBanking; the name keeps the call sites
// readable.
func (s *Session) notifyBankingAndAccounts() {
	s.notifyBanking()
}

// keepLiveSyncFieldsLocked is Rust's keep_live_sync_fields. The caller holds the
// session mutex.
func (s *Session) keepLiveSyncFieldsLocked(result *banking.BankingState) {
	if result == nil || s.banking == nil {
		return
	}
	result.SyncingBank = copyBankConnectionID(s.banking.SyncingBank)
	result.SyncStep = copyString(s.banking.SyncStep)
	result.SyncAll = copySyncAll(s.banking.SyncAll)
}

// mergeBanksLocked is Rust's merge_banks: the bank list follows an Accounts
// action that added or removed a bank. The caller holds the session mutex.
func (s *Session) mergeBanksLocked(accounts *accountsview.AccountsState) {
	if accounts == nil || s.banking == nil {
		return
	}
	kept := make([]banking.BankItem, 0, len(accounts.BankAccesses))
	for _, item := range s.banking.Banks {
		if accountsHasBank(accounts, item.ID) {
			kept = append(kept, item)
		}
	}
	for _, access := range accounts.BankAccesses {
		if bankingHasBank(kept, access.ID) {
			continue
		}
		kept = append(kept, banking.BankItem{
			ID:               access.ID,
			BankName:         access.BankName,
			BLZ:              access.BLZ,
			UserID:           access.UserID,
			ServerURL:        cloneStringPointer(access.ServerURL),
			Offline:          access.Offline,
			IncludeInSyncAll: access.IncludeInSyncAll,
			SyncFrom:         cloneCivilDate(access.SyncFrom),
			LastSync:         cloneTimePointer(access.LastSync),
			HasSavedPin:      false,
			CanTestConnection: access.ServerURL != nil &&
				strings.TrimSpace(*access.ServerURL) != "",
		})
	}
	s.banking.Banks = kept
}

func accountsHasBank(accounts *accountsview.AccountsState, id model.BankConnectionID) bool {
	for _, access := range accounts.BankAccesses {
		if access.ID == id {
			return true
		}
	}
	return false
}

func bankingHasBank(banks []banking.BankItem, id model.BankConnectionID) bool {
	for _, item := range banks {
		if item.ID == id {
			return true
		}
	}
	return false
}

// BankingOpenAddBank opens the online add-bank dialog, as banking::open_add_bank
// does.
func (s *Session) BankingOpenAddBank() {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	if s.banking != nil {
		s.banking.ClearMessages()
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{Kind: "add-bank"}
	s.mu.Unlock()
	s.notifyBankingAndAccounts()
}

// BankingSubmitAddOnlineBank validates and stores an online bank login, as
// banking::submit_add_online_bank does. Validation errors stay in the dialog.
func (s *Session) BankingSubmitAddOnlineBank(
	name string,
	blz string,
	bic string,
	userID string,
	serverURL string,
) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	if s.banking == nil {
		s.mu.Unlock()
		return
	}
	if message := s.banking.ValidateBankAccess(name, blz); message != nil {
		s.accountsDialog.ErrorText = *message
		s.mu.Unlock()
		s.notifyBankingAndAccounts()
		return
	}
	if message := s.banking.ValidateServerURL(serverURL); message != nil {
		s.accountsDialog.ErrorText = *message
		s.mu.Unlock()
		s.notifyBankingAndAccounts()
		return
	}
	s.closeAccountsDialogLocked()
	job := s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.AddBankAccess(ctx, database, service, name, blz, bic, userID, serverURL)
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingSubmitConnection saves the connection dialog, as
// banking::submit_connection does. An offline bank only gets a new name.
func (s *Session) BankingSubmitConnection(
	bankID int,
	name string,
	blz string,
	userID string,
	serverURL string,
) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	offline := false
	if bank := s.accountsBankLocked(bankID); bank != nil {
		offline = bank.Offline
	}
	if s.banking == nil {
		s.mu.Unlock()
		return
	}
	if offline {
		if strings.TrimSpace(name) == "" {
			s.accountsDialog.ErrorText = i18n.Text(s.language, i18n.BankNameRequired)
			s.mu.Unlock()
			s.notifyBankingAndAccounts()
			return
		}
	} else {
		if message := s.banking.ValidateBankAccess(name, blz); message != nil {
			s.accountsDialog.ErrorText = *message
			s.mu.Unlock()
			s.notifyBankingAndAccounts()
			return
		}
		if message := s.banking.ValidateServerURL(serverURL); message != nil {
			s.accountsDialog.ErrorText = *message
			s.mu.Unlock()
			s.notifyBankingAndAccounts()
			return
		}
	}
	s.closeAccountsDialogLocked()
	id := model.BankConnectionID(bankID)
	job := s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.UpdateConnection(ctx, database, service, id, name, blz, userID, serverURL)
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingForgetPin forgets the remembered PIN of an online bank.
func (s *Session) BankingForgetPin(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	id := model.BankConnectionID(bankID)
	job := s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.ForgetPin(ctx, database, service, id)
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingSetIncludeInSyncAll toggles whether id takes part in "Sync all".
func (s *Session) BankingSetIncludeInSyncAll(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	id := model.BankConnectionID(bankID)
	include := false
	if s.banking != nil {
		for _, item := range s.banking.Banks {
			if item.ID == id {
				include = !item.IncludeInSyncAll
				break
			}
		}
	}
	job := s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.SetIncludeInSyncAll(ctx, database, service, id, include)
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingClearHistory drops a bank's stored history start.
func (s *Session) BankingClearHistory(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	s.closeAccountsDialogLocked()
	id := model.BankConnectionID(bankID)
	job := s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.ClearSyncFrom(ctx, database, service, id)
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// BankingSubmitHistory validates the date, closes the dialog and starts a
// history sync, as banking::submit_history does.
func (s *Session) BankingSubmitHistory(bankID int, input string) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	date, ok := format.ParseDate(input)
	if !ok {
		s.accountsDialog.ErrorText = i18n.Text(s.language, i18n.ErrorSyncFromDateInvalid)
		s.mu.Unlock()
		s.notifyBankingAndAccounts()
		return
	}
	today := s.today()
	if s.banking == nil {
		s.mu.Unlock()
		return
	}
	if message := s.banking.ValidateSyncFrom(input, today); message != nil {
		s.accountsDialog.ErrorText = *message
		s.mu.Unlock()
		s.notifyBankingAndAccounts()
		return
	}
	if s.db == nil || s.bridge == nil {
		s.mu.Unlock()
		return
	}
	id := model.BankConnectionID(bankID)
	traceBLZ := ""
	if bank := s.bankingBankLocked(bankID); bank != nil {
		traceBLZ = bank.BLZ
	}
	sink := &syncTraceSink{}
	service, serviceOK := s.bankingServiceWithTraceLocked(s.language, sink)
	generation, jobOK := s.beginJobLocked()
	if !serviceOK || !jobOK {
		if jobOK {
			s.jobs.EndGeneration(generation)
		}
		s.mu.Unlock()
		return
	}
	s.closeAccountsDialogLocked()
	if !s.banking.BeginHistoryFrom(id, date, today) {
		s.jobs.EndGeneration(generation)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	s.clearLastTraceLocked(traceKindSync, id)
	pending := s.banking.Clone()
	accounts := s.accounts.Clone()
	bridge := s.bridge
	busy := s.profileBusy
	database := s.db
	job := &bankingJob{
		generation:       generation,
		database:         database,
		service:          service,
		bridge:           bridge,
		busy:             busy,
		pending:          pending,
		accounts:         &accounts,
		keepLive:         false,
		refreshDashboard: true,
		captureSyncTrace: true,
		traceSink:        sink,
		traceKind:        traceKindSync,
		traceBankID:      id,
		traceBLZ:         traceBLZ,
		work: func(
			ctx context.Context,
			database *db.Database,
			service *banksync.BankingService,
			state *banking.BankingState,
		) {
			state.RunHistoryFrom(ctx, database, service, bridge, &busy, id, date, today)
		},
	}
	s.mu.Unlock()
	s.notifyBanking()
	s.startBankingJob(job)
}

// BankingStartSync starts one bank's sync on a worker; the claim happens on the
// calling side, as banking::start_sync does.
func (s *Session) BankingStartSync(bankID int) {
	s.mu.Lock()
	if s.db == nil || s.bridge == nil || s.banking == nil {
		s.mu.Unlock()
		return
	}
	id := model.BankConnectionID(bankID)
	traceBLZ := ""
	if bank := s.bankingBankLocked(bankID); bank != nil {
		traceBLZ = bank.BLZ
	}
	sink := &syncTraceSink{}
	service, serviceOK := s.bankingServiceWithTraceLocked(s.language, sink)
	generation, jobOK := s.beginJobLocked()
	if !serviceOK || !jobOK {
		if jobOK {
			s.jobs.EndGeneration(generation)
		}
		s.mu.Unlock()
		return
	}
	if !s.banking.BeginSync(id) {
		s.jobs.EndGeneration(generation)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	s.clearLastTraceLocked(traceKindSync, id)
	pending := s.banking.Clone()
	accounts := s.accounts.Clone()
	bridge := s.bridge
	busy := s.profileBusy
	database := s.db
	today := s.today()
	job := &bankingJob{
		generation:       generation,
		database:         database,
		service:          service,
		bridge:           bridge,
		busy:             busy,
		pending:          pending,
		accounts:         &accounts,
		refreshDashboard: true,
		captureSyncTrace: true,
		traceSink:        sink,
		traceKind:        traceKindSync,
		traceBankID:      id,
		traceBLZ:         traceBLZ,
		work: func(
			ctx context.Context,
			database *db.Database,
			service *banksync.BankingService,
			state *banking.BankingState,
		) {
			state.RunSync(ctx, database, service, bridge, &busy, id, today)
		},
	}
	s.mu.Unlock()
	s.notifyBanking()
	s.startBankingJob(job)
}

// BankingStartSyncAll starts "Sync all" on a worker, as banking::start_sync_all
// does.
func (s *Session) BankingStartSyncAll() {
	s.mu.Lock()
	if s.db == nil || s.bridge == nil || s.banking == nil {
		s.mu.Unlock()
		return
	}
	sink := &syncTraceSink{}
	service, serviceOK := s.bankingServiceWithTraceLocked(s.language, sink)
	generation, jobOK := s.beginJobLocked()
	if !serviceOK || !jobOK {
		if jobOK {
			s.jobs.EndGeneration(generation)
		}
		s.mu.Unlock()
		return
	}
	if !s.banking.BeginSyncAll() {
		s.jobs.EndGeneration(generation)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	s.clearLastTraceLocked(traceKindSync, 0)
	pending := s.banking.Clone()
	accounts := s.accounts.Clone()
	bridge := s.bridge
	busy := s.profileBusy
	database := s.db
	today := s.today()
	var job *bankingJob
	job = &bankingJob{
		generation:       generation,
		database:         database,
		service:          service,
		bridge:           bridge,
		busy:             busy,
		pending:          pending,
		accounts:         &accounts,
		refreshDashboard: true,
		captureSyncTrace: true,
		traceSink:        sink,
		traceKind:        traceKindSync,
		work: func(
			ctx context.Context,
			database *db.Database,
			service *banksync.BankingService,
			state *banking.BankingState,
		) {
			state.RunSyncAll(ctx, database, service, bridge, &busy, today, func(current *banking.BankingState) {
				if current.SyncAll != nil {
					job.traceBankID = current.SyncAll.BankID
					for _, bank := range pending.Banks {
						if bank.ID == current.SyncAll.BankID {
							job.traceBLZ = bank.BLZ
							break
						}
					}
				}
				s.mu.Lock()
				stillCurrent := s.jobs.IsCurrent(generation)
				if stillCurrent && s.banking != nil {
					s.banking.SyncAll = copySyncAll(current.SyncAll)
					s.banking.SyncingBank = copyBankConnectionID(current.SyncingBank)
					s.banking.SyncStep = copyString(current.SyncStep)
				}
				s.mu.Unlock()
				if stillCurrent {
					s.notifyBanking()
				}
			})
		},
	}
	s.mu.Unlock()
	s.notifyBanking()
	s.startBankingJob(job)
}

// BankingTestConnection runs the read-only connection test for one bank on a
// worker. It is refused while a sync or another test runs. The PIN and (when
// the build has no linked id) the product id are asked through the live bridge.
func (s *Session) BankingTestConnection(bankID int) {
	s.mu.Lock()
	if s.db == nil || s.bridge == nil || s.banking == nil {
		s.mu.Unlock()
		return
	}
	if s.banking.IsBusy() {
		s.setBankingErrorLocked(i18n.ErrorConnectionBusy)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	bank := s.bankingBankLocked(bankID)
	if bank == nil {
		s.setBankingErrorLocked(i18n.ErrorBankConnectionNotFound)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	if !bank.CanTestConnection {
		s.setBankingErrorLocked(i18n.ErrorNoFintsServer)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}

	id := model.BankConnectionID(bankID)
	bankName := bank.BankName
	blz := bank.BLZ
	userID := bank.UserID
	serverURL := ""
	if bank.ServerURL != nil {
		serverURL = *bank.ServerURL
	}
	bridge := s.bridge
	today := s.today()
	factory := s.connectionTestFactory
	if factory == nil {
		factory = connectiontest.DefaultTransportFactory
	}
	// The product id is a release secret. It is read here and wiped by
	// connectiontest.Run after this one test.
	var linkedProductID *secret.Text
	if value, ok := fintsproduct.ID(); ok {
		linkedProductID = secret.NewText([]byte(value))
	}

	running := &banking.ConnectionTestResult{BankID: id, Running: true}
	var job *bankingJob
	job = s.beginBankingActionLocked(func(
		ctx context.Context,
		_ *db.Database,
		_ *banksync.BankingService,
		state *banking.BankingState,
	) {
		productID := linkedProductID
		if productID == nil {
			productID = bridge.AskProductID()
			if productID == nil {
				state.ConnectionTest = &banking.ConnectionTestResult{
					BankID: id,
					Reason: connectionTestReason(state.Language, connectiontest.Outcome{
						Kind:      fintsprobe.NoGoOther,
						Other:     connectiontest.OtherCancelled,
						Cancelled: true,
					}),
				}
				state.LastTrace = &banking.LastTrace{Kind: traceKindTest, BankID: id}
				job.setLastTrace = true
				job.lastTrace = lastTraceState{kind: traceKindTest, bankID: id}
				return
			}
		}

		outcome := connectiontest.Run(ctx, connectiontest.Input{
			BankName:       bankName,
			BLZ:            blz,
			UserID:         userID,
			ServerURL:      serverURL,
			ProductID:      productID,
			Interaction:    bridge,
			Factory:        factory,
			Today:          today,
			ProductVersion: connectionTestProductVersion,
			Language:       state.Language,
		})
		job.setLastTrace = true
		job.lastTrace = lastTraceState{
			trace:  append([]byte(nil), outcome.Trace...),
			blz:    blz,
			kind:   traceKindTest,
			bankID: id,
		}
		state.LastTrace = &banking.LastTrace{
			Kind:     traceKindTest,
			BankID:   id,
			HasTrace: len(outcome.Trace) > 0,
		}
		state.ConnectionTest = &banking.ConnectionTestResult{
			BankID:  id,
			Go:      outcome.Go,
			Reason:  connectionTestReason(state.Language, outcome),
			Codes:   append([]string(nil), outcome.Codes...),
			Probed:  outcome.Probed,
			Skipped: outcome.Skipped,
		}
	})
	if job == nil {
		if linkedProductID != nil {
			linkedProductID.Wipe()
		}
		s.mu.Unlock()
		return
	}
	job.pending.ConnectionTest = &banking.ConnectionTestResult{BankID: id, Running: true}
	job.pending.LastTrace = &banking.LastTrace{Kind: traceKindTest, BankID: id}
	s.banking.ConnectionTest = running
	s.banking.LastTrace = &banking.LastTrace{Kind: traceKindTest, BankID: id}
	s.lastTrace = lastTraceState{kind: traceKindTest, bankID: id}
	s.mu.Unlock()
	s.notifyBanking()
	s.startBankingJob(job)
}

// BankingSaveConnectionTrace asks where to save the last redacted sync or
// connection test trace and writes it with mode 0600. The path never appears in
// an error.
func (s *Session) BankingSaveConnectionTrace() {
	s.mu.Lock()
	if s.db == nil || s.banking == nil {
		s.mu.Unlock()
		return
	}
	trace := append([]byte(nil), s.lastTrace.trace...)
	blz := s.lastTrace.blz
	if len(trace) == 0 {
		s.setBankingErrorLocked(i18n.ErrorNoConnectionTrace)
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	dialogs := s.fileDialogs
	language := s.language
	var now time.Time
	if s.clock != nil {
		now = s.clock()
	}
	defaultName := connectionTraceFileName(blz, now)

	job := s.beginBankingActionLocked(func(
		_ context.Context,
		_ *db.Database,
		_ *banksync.BankingService,
		state *banking.BankingState,
	) {
		if dialogs == nil {
			state.Message = nil
			message := i18n.Text(language, i18n.ErrorConnectionTrace)
			state.Error = &message
			return
		}
		path, ok := dialogs.SaveTrace(i18n.Text(language, i18n.SaveConnectionTrace), defaultName)
		if !ok {
			return
		}
		if err := writeConnectionTrace(path, trace); err != nil {
			state.Message = nil
			message := i18n.Text(language, i18n.ErrorConnectionTrace)
			state.Error = &message
			return
		}
		state.Error = nil
		message := i18n.Text(language, i18n.MessageConnectionTraceSaved)
		state.Message = &message
	})
	s.mu.Unlock()
	s.startBankingJob(job)
}

// clearLastTraceLocked drops the previous trace when a new sync or test starts.
// The caller holds the session mutex.
func (s *Session) clearLastTraceLocked(kind string, bankID model.BankConnectionID) {
	s.lastTrace = lastTraceState{kind: kind, bankID: bankID}
	if s.banking != nil {
		s.banking.LastTrace = &banking.LastTrace{Kind: kind, BankID: bankID}
	}
}

// setBankingErrorLocked publishes a localized banking error. The caller holds
// the session mutex.
func (s *Session) setBankingErrorLocked(key i18n.Key) {
	if s.banking == nil {
		return
	}
	message := i18n.Text(s.language, key)
	s.banking.Error = &message
}

// connectionTestReason localizes an outcome. It never includes bank text.
func connectionTestReason(language i18n.Language, outcome connectiontest.Outcome) string {
	if outcome.Cancelled || outcome.Other == connectiontest.OtherCancelled {
		return i18n.Text(language, i18n.TestConnectionCancelled)
	}
	if outcome.Go {
		return i18n.Text(language, i18n.TestConnectionGo)
	}
	switch outcome.Kind {
	case fintsprobe.NoGoLoginRejected:
		return i18n.Text(language, i18n.TestConnectionLoginRejected)
	case fintsprobe.NoGoTanMethodUnsupported:
		return i18n.Text(language, i18n.TestConnectionTanMethodUnsupported)
	case fintsprobe.NoGoTanTimeout:
		return i18n.Text(language, i18n.TestConnectionTanTimeout)
	case fintsprobe.NoGoStatementsUnavailable:
		return i18n.Text(language, i18n.TestConnectionStatementsUnavailable)
	}
	switch outcome.Other {
	case connectiontest.OtherInvalidLogin:
		return i18n.Text(language, i18n.TestConnectionInvalidLogin)
	case connectiontest.OtherProductIDMissing, connectiontest.OtherProductIDInvalid:
		return i18n.Text(language, i18n.TestConnectionProductID)
	}
	other := outcome.Other
	if other == "" {
		other = "Unknown"
	}
	return i18n.Format(language, i18n.TestConnectionOther, i18n.Str(other))
}

// connectionTraceFileName builds the dialog's default name.
func connectionTraceFileName(blz string, now time.Time) string {
	return "finglass-trace-" + blz + "-" + now.Format("20060102-1504") + ".txt"
}

// writeConnectionTrace creates the file with 0600, truncates an existing file
// and forces its mode to 0600 before writing.
func writeConnectionTrace(path string, trace []byte) error {
	// #nosec G304 -- the path comes only from the user's save dialog.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	_, err = file.Write(trace)
	return err
}

// BankingRemoveBank deletes an online bank with its remembered PIN and
// registration, as banking::delete_bank does.
func (s *Session) BankingRemoveBank(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	job := s.beginDeleteOnlineBankLocked(bankID)
	s.mu.Unlock()
	s.startBankingJob(job)
}

// beginDeleteOnlineBankLocked prepares banking::delete_bank. The caller holds
// the session mutex.
func (s *Session) beginDeleteOnlineBankLocked(bankID int) *bankingJob {
	id := model.BankConnectionID(bankID)
	busy := s.profileBusy
	return s.beginBankingActionLocked(func(
		ctx context.Context,
		database *db.Database,
		service *banksync.BankingService,
		state *banking.BankingState,
	) {
		_ = state.RemoveBank(ctx, database, service, &busy, id)
	})
}

// BankingClearMessages clears the bank-login error and success banner.
func (s *Session) BankingClearMessages() {
	s.mu.Lock()
	if s.banking != nil {
		s.banking.ClearMessages()
	}
	s.mu.Unlock()
	s.notifyBanking()
}

// BankingOpenConnectionDialog opens the connection-settings dialog.
func (s *Session) BankingOpenConnectionDialog(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	bank := s.bankingBankLocked(bankID)
	if bank == nil || s.banking == nil {
		s.mu.Unlock()
		return
	}
	pinText := ""
	if bank.Offline {
		pinText = ""
	} else if bank.HasSavedPin {
		pinText = i18n.Text(s.language, i18n.PinSavedForSyncs)
	} else if s.banking.CanRememberPin {
		pinText = i18n.Text(s.language, i18n.PinAskedEverySync)
	} else {
		pinText = i18n.Text(s.language, i18n.PinNeverStored)
	}
	title := i18n.Text(s.language, i18n.RenameConnection)
	if !bank.Offline {
		title = i18n.Format(s.language, i18n.OnlineBankingFor, i18n.Str(bank.BankName))
	}
	serverURL := ""
	if bank.ServerURL != nil {
		serverURL = *bank.ServerURL
	}
	syncing := s.banking.SyncingBank != nil && *s.banking.SyncingBank == bank.ID
	s.accountsDialog = AccountsDialogState{
		Kind:                "connection",
		Title:               title,
		AccountName:         bank.BankName,
		BankID:              bankID,
		Offline:             bank.Offline,
		ConnectionName:      bank.BankName,
		ConnectionBLZ:       bank.BLZ,
		ConnectionUserID:    bank.UserID,
		ConnectionServerURL: serverURL,
		ConnectionHasPin:    bank.HasSavedPin,
		ConnectionPinText:   pinText,
		ConnectionSyncing:   syncing,
	}
	s.mu.Unlock()
	s.notifyBankingAndAccounts()
}

// BankingOpenHistoryDialog opens the history-from dialog with the stored date or
// FinDesk's 90-day default.
func (s *Session) BankingOpenHistoryDialog(bankID int) {
	s.mu.Lock()
	if s.refuseBankingIfSyncingLocked() {
		s.mu.Unlock()
		s.notifyBanking()
		return
	}
	bank := s.bankingBankLocked(bankID)
	if bank == nil {
		s.mu.Unlock()
		return
	}
	today := s.today()
	start := today.AddDays(-banksync.InitialSyncDays)
	if bank.SyncFrom != nil {
		start = *bank.SyncFrom
	}
	s.accountsDialog = AccountsDialogState{
		Kind:            "history",
		Title:           i18n.Format(s.language, i18n.FetchHistoryFor, i18n.Str(bank.BankName)),
		BankID:          bankID,
		HistoryDate:     format.Date(start),
		HistoryHasStart: bank.SyncFrom != nil,
	}
	s.mu.Unlock()
	s.notifyBankingAndAccounts()
}

// bankingBankLocked finds one bank in the banking state. The caller holds the
// session mutex.
func (s *Session) bankingBankLocked(bankID int) *banking.BankItem {
	if s.banking == nil {
		return nil
	}
	for index := range s.banking.Banks {
		if int64(s.banking.Banks[index].ID) == int64(bankID) {
			return &s.banking.Banks[index]
		}
	}
	return nil
}

// BankingHandleMenu handles one entry of a bank card's menu, as
// banking::handle_bank_menu does.
func (s *Session) BankingHandleMenu(bankID int, action string) {
	switch action {
	case "sync":
		s.BankingStartSync(bankID)
	case "test-connection":
		s.BankingTestConnection(bankID)
	case "history":
		s.BankingOpenHistoryDialog(bankID)
	case "clear-history":
		s.BankingClearHistory(bankID)
	case "include":
		s.BankingSetIncludeInSyncAll(bankID)
	case "rename", "connection":
		s.BankingOpenConnectionDialog(bankID)
	case "forget-pin":
		s.BankingForgetPin(bankID)
	case "delete":
		s.mu.Lock()
		s.AccountsOpenDeleteBankLocked(bankID)
		s.mu.Unlock()
		s.notifyBankingAndAccounts()
	}
}

// AccountsOpenDeleteBankLocked opens the delete-bank confirmation while the
// caller holds the session mutex.
func (s *Session) AccountsOpenDeleteBankLocked(bankID int) {
	bank := s.accountsBankLocked(bankID)
	if bank == nil {
		return
	}
	s.accounts.ClearMessages()
	s.accountsDialog = AccountsDialogState{
		Kind:   "delete-bank",
		Title:  i18n.Format(s.language, i18n.RemoveBankTitle, i18n.Str(bank.BankName)),
		Text:   i18n.Text(s.language, i18n.RemoveBankText),
		BankID: bankID,
	}
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneCivilDate(value *civil.Date) *civil.Date {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
