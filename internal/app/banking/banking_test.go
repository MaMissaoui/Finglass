package banking

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), passphrase)
	passphrase.Wipe()
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func dataDir(t *testing.T) string {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	dir, err := resolved.ProfileDir("default")
	if err != nil {
		t.Fatalf("profile dir: %v", err)
	}
	return dir
}

func serviceWithStore(
	gateway *testGateway,
	store credentials.Store,
	dir string,
	language i18n.Language,
) *banksync.BankingService {
	return banksync.NewBankingService(
		banksync.NewFinTsClient(gateway, dir, "TEST-PRODUCT", store).WithLanguage(language),
	)
}

func serviceFor(t *testing.T, gateway *testGateway) (*banksync.BankingService, *banksync.MemoryCredentialStore) {
	t.Helper()
	store := banksync.NewMemoryCredentialStore()
	service := serviceWithStore(gateway, store, dataDir(t), i18n.En)
	return service, store
}

func onlineBank(t *testing.T, database *db.Database, name, userID string) model.BankConnectionID {
	t.Helper()
	bic := "COLSDE33XXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: name,
		BLZ:      "37040044",
		BIC:      &bic,
		UserID:   userID,
	})
	if err != nil {
		t.Fatalf("create online bank: %v", err)
	}
	serverURL := "https://fints.example/pintan"
	if _, err := bankconnections.UpdateConnection(
		context.Background(),
		database,
		created.ID,
		model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("update online bank: %v", err)
	}
	return created.ID
}

func offlineBank(t *testing.T, database *db.Database, name string) model.BankConnectionID {
	t.Helper()
	created, err := bankconnections.CreateOffline(context.Background(), database, name)
	if err != nil {
		t.Fatalf("create offline bank: %v", err)
	}
	return created.ID
}

func bridgeForTest(timeout time.Duration) *PromptBridge {
	return NewPromptBridge(timeout, func() {})
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if !time.Now().Before(deadline) {
			t.Fatal("condition did not become true in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func answerPinInBackground(bridge *PromptBridge, pin string, remember bool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(2 * time.Second)
		for {
			prompt := bridge.Prompt()
			if prompt != nil && prompt.Kind == Pin {
				answerPinPrompt(bridge, []byte(pin), remember)
				return
			}
			if !time.Now().Before(deadline) {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	return done
}

func cancelAtPinInBackground(bridge *PromptBridge) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(2 * time.Second)
		for {
			prompt := bridge.Prompt()
			if prompt != nil && prompt.Kind == Pin {
				bridge.CancelPending()
				return
			}
			if !time.Now().Before(deadline) {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	return done
}

// testGateway is a fake FinTS gateway: no network, no OS credential store.
type testGateway struct {
	mu           sync.Mutex
	snapshots    []banksync.AccountSnapshot
	failure      *banksync.GatewayError
	askPin       bool
	calls        int
	names        []string
	sinceSeen    map[string]*civil.Date
	cancelOnCall int
	busy         *accountsview.Busy
}

func newTestGateway() *testGateway {
	return &testGateway{sinceSeen: make(map[string]*civil.Date)}
}

func (g *testGateway) setSnapshots(snapshots []banksync.AccountSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.snapshots = append([]banksync.AccountSnapshot(nil), snapshots...)
}

func (g *testGateway) setFailure(failure *banksync.GatewayError) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if failure == nil {
		g.failure = nil
		return
	}
	value := *failure
	g.failure = &value
}

func (g *testGateway) setAskPin(ask bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.askPin = ask
}

func (g *testGateway) setCancelOnCall(call int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cancelOnCall = call
}

func (g *testGateway) setBusy(busy accountsview.Busy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.busy = &busy
}

func (g *testGateway) namesList() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.names...)
}

func (g *testGateway) since(iban string) *civil.Date {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sinceSeen[iban]
}

func (g *testGateway) FetchAll(
	_ context.Context,
	connection banksync.FinTsConnection,
	interaction banksync.BankingInteraction,
	since func(string) *civil.Date,
	_ civil.Date,
) (banksync.FetchOutcome, error) {
	g.mu.Lock()
	g.calls++
	call := g.calls
	g.names = append(g.names, connection.BankName)
	busy := g.busy
	snapshots := append([]banksync.AccountSnapshot(nil), g.snapshots...)
	failure := g.failure
	askPin := g.askPin
	cancelOnCall := g.cancelOnCall
	g.mu.Unlock()

	if busy != nil && !busy.Active() {
		return banksync.FetchOutcome{}, banksync.NewGatewayOtherError("profile Busy was not active")
	}

	seen := make(map[string]*civil.Date, len(snapshots))
	for _, snapshot := range snapshots {
		seen[snapshot.Account.IBAN] = since(snapshot.Account.IBAN)
	}
	g.mu.Lock()
	for iban, date := range seen {
		if date == nil {
			g.sinceSeen[iban] = nil
			continue
		}
		value := *date
		g.sinceSeen[iban] = &value
	}
	g.mu.Unlock()

	if askPin && interaction.Pin(connection.BankName, connection.UserID, true) == nil {
		return banksync.FetchOutcome{}, banksync.NewGatewayCancelledError()
	}
	if cancelOnCall == call {
		return banksync.FetchOutcome{}, banksync.NewGatewayCancelledError()
	}
	if failure != nil {
		return banksync.FetchOutcome{}, *failure
	}

	return banksync.FetchOutcome{Snapshots: snapshots}, nil
}

func mustDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return date
}

func mustDecimal(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	value, err := decimal.Parse(text)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", text, err)
	}
	return value
}

func accountFor(iban string) banksync.FinTsAccount {
	return banksync.FinTsAccount{
		IBAN:           iban,
		BIC:            "COLSDE33XXX",
		AccountNumber:  "1234567890",
		BankName:       "Sparkasse",
		BLZ:            "37040044",
		AccountType:    "checking",
		Currency:       "EUR",
		CurrencyStated: true,
	}
}

func snapshot(t *testing.T, iban, balance string) banksync.AccountSnapshot {
	t.Helper()
	bankAccount := accountFor(iban)
	return banksync.AccountSnapshot{
		Balance: &banksync.FinTsBalance{
			Account:  bankAccount,
			Balance:  mustDecimal(t, balance),
			Currency: "EUR",
			Date:     time.Now().UTC(),
		},
		Account:      bankAccount,
		Transactions: []banksync.FinTsTransaction{},
	}
}

func stringPointer(value string) *string {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestAddBankAccessValidatesNameBlzAndAnHTTPSURL(t *testing.T) {
	database := testDatabase(t)
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := state.AddBankAccess(context.Background(), database, service, "  ", "37040044", "", "user", "https://fints.example"); err == nil {
		t.Fatal("a blank name was accepted")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.BankNameRequired) {
		t.Fatalf("error = %q", got)
	}

	if err := state.AddBankAccess(context.Background(), database, service, "Bank", "1234", "", "user", "https://fints.example"); err == nil {
		t.Fatal("a short BLZ was accepted")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.BlzMustBe8Digits) {
		t.Fatalf("error = %q", got)
	}

	if err := state.AddBankAccess(context.Background(), database, service, "Bank", "37040044", "", "user", "  "); err == nil {
		t.Fatal("a blank URL was accepted")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorServerUrlRequired) {
		t.Fatalf("error = %q", got)
	}

	if err := state.AddBankAccess(context.Background(), database, service, "Bank", "37040044", "", "user", "http://fints.example"); err == nil {
		t.Fatal("an HTTP URL was accepted")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorServerUrlMustBeHttps) {
		t.Fatalf("error = %q", got)
	}

	if err := state.AddBankAccess(
		context.Background(),
		database,
		service,
		"Sparkasse",
		"37040044",
		"colsde33xxx",
		"user",
		"https://fints.example/pintan",
	); err != nil {
		t.Fatalf("add bank access: %v", err)
	}
	if len(state.Banks) != 1 {
		t.Fatalf("banks = %d, want 1", len(state.Banks))
	}
	if state.Banks[0].BankName != "Sparkasse" {
		t.Fatalf("bank name = %q", state.Banks[0].BankName)
	}
	if got := stringValue(state.Banks[0].ServerURL); got != "https://fints.example/pintan" {
		t.Fatalf("server URL = %q", got)
	}
	if !state.CanRememberPin {
		t.Fatal("CanRememberPin = false")
	}
	want := i18n.Format(i18n.En, i18n.MessageAdded, i18n.Str("Sparkasse"))
	if got := stringValue(state.Message); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestAnOfflineBankOnlyRenamesAndEveryEditIsRefusedWhileBusy(t *testing.T) {
	database := testDatabase(t)
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	bank := offlineBank(t, database, "Old card")
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	// The invalid BLZ and URL are deliberately ignored for an offline bank: only
	// the label changes.
	if err := state.UpdateConnection(
		context.Background(), database, service, bank, "New card", "not-a-blz", "x", "http://invalid",
	); err != nil {
		t.Fatalf("rename offline bank: %v", err)
	}
	stored, ok, err := bankconnections.GetItem(context.Background(), database, bank)
	if err != nil || !ok {
		t.Fatalf("get item: ok=%t err=%v", ok, err)
	}
	if stored.BankName != "New card" {
		t.Fatalf("bank name = %q", stored.BankName)
	}
	if stored.BLZ != "" {
		t.Fatalf("BLZ = %q", stored.BLZ)
	}
	if stored.ServerURL != nil {
		t.Fatalf("server URL = %q", stringValue(stored.ServerURL))
	}
	if got := stringValue(state.Message); got != i18n.Text(i18n.En, i18n.MessageConnectionSaved) {
		t.Fatalf("message = %q", got)
	}

	state.SyncAll = &SyncAllProgress{}
	updateErr := state.UpdateConnection(context.Background(), database, service, bank, "Newest card", "", "", "")
	var bankingErr banksync.BankingError
	if !asBankingError(updateErr, &bankingErr) {
		t.Fatalf("update error = %v, want a BankingError", updateErr)
	}
	if bankingErr.Message != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %q", bankingErr.Message)
	}
	stored, ok, err = bankconnections.GetItem(context.Background(), database, bank)
	if err != nil || !ok {
		t.Fatalf("get item: ok=%t err=%v", ok, err)
	}
	if stored.BankName != "New card" {
		t.Fatalf("bank name changed to %q while busy", stored.BankName)
	}

	state.SyncAll = nil
	state.SyncingBank = &bank
	if updateErr := state.UpdateConnection(
		context.Background(), database, service, bank, "Newest card", "", "", "",
	); updateErr == nil {
		t.Fatal("update succeeded while a sync ran")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %q", got)
	}
}

func TestSyncSuccessSetsTheSummaryAndTheProfileBusyIsActiveDuringTheDialog(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	busy := accountsview.NewBusy()
	gateway := newTestGateway()
	gateway.setBusy(busy)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	if !state.IsSyncing() {
		t.Fatal("IsSyncing = false")
	}
	if got := stringValue(state.SyncStep); got != i18n.Text(i18n.En, i18n.SyncConnecting) {
		t.Fatalf("sync step = %q", got)
	}
	if busy.Active() {
		t.Fatal("claiming the state started the busy section")
	}

	today := mustDate(t, 2026, time.November, 7)
	state.RunSync(context.Background(), database, service, bridge, &busy, bank, today)

	if state.SyncingBank != nil {
		t.Fatal("SyncingBank is set")
	}
	if state.SyncStep != nil {
		t.Fatal("SyncStep is set")
	}
	if state.Error != nil {
		t.Fatalf("error = %q", stringValue(state.Error))
	}
	message := stringValue(state.Message)
	if !strings.Contains(message, "Synced") {
		t.Fatalf("message = %q", message)
	}
	if busy.Active() {
		t.Fatal("the busy section did not end with the sync")
	}
	if got := gateway.namesList(); len(got) != 1 || got[0] != "Sparkasse" {
		t.Fatalf("gateway names = %v", got)
	}
	if state.Banks[0].LastSync == nil {
		t.Fatal("LastSync is nil")
	}
}

func TestCancellingAtThePinPromptClearsTheError(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	gateway := newTestGateway()
	gateway.setAskPin(true)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(30 * time.Second)
	canceller := cancelAtPinInBackground(bridge)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))
	<-canceller

	if state.Error != nil {
		t.Fatalf("error = %q", stringValue(state.Error))
	}
	if state.Message != nil {
		t.Fatalf("message = %q", stringValue(state.Message))
	}
	if state.SyncingBank != nil {
		t.Fatal("SyncingBank is set")
	}
}

func TestAFailedSyncKeepsItsErrorMessage(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	gateway := newTestGateway()
	failure := banksync.NewGatewayOtherError("Dialog broke")
	gateway.setFailure(&failure)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))

	if got := stringValue(state.Error); got != "Dialog broke" {
		t.Fatalf("error = %q", got)
	}
	if state.Message != nil {
		t.Fatalf("message = %q", stringValue(state.Message))
	}
	if state.SyncingBank != nil {
		t.Fatal("SyncingBank is set")
	}
}

func TestBeginSyncRefusesDuringASyncAndForAnOfflineBank(t *testing.T) {
	database := testDatabase(t)
	online := onlineBank(t, database, "Sparkasse", "user")
	offline := offlineBank(t, database, "Card")
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(online) {
		t.Fatal("first begin sync refused")
	}
	if state.BeginSync(online) {
		t.Fatal("second begin sync succeeded")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %q", got)
	}

	state.SyncingBank = nil
	if state.BeginSync(offline) {
		t.Fatal("begin sync for an offline bank succeeded")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorBankOffline) {
		t.Fatalf("error = %q", got)
	}
}

func TestSyncAllRunsInCardOrderReportsProgressAndStopsOnCancel(t *testing.T) {
	database := testDatabase(t)
	onlineBank(t, database, "A Bank", "a")
	onlineBank(t, database, "B Bank", "b")
	onlineBank(t, database, "C Bank", "c")
	gateway := newTestGateway()
	gateway.setCancelOnCall(2)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	busy := accountsview.NewBusy()
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSyncAll() {
		t.Fatal("begin sync all refused")
	}
	progress := make([]SyncAllProgress, 0)
	state.RunSyncAll(
		context.Background(),
		database,
		service,
		bridge,
		&busy,
		mustDate(t, 2026, time.November, 7),
		func(current *BankingState) {
			if current.SyncAll != nil {
				progress = append(progress, *current.SyncAll)
			}
		},
	)

	if got := gateway.namesList(); len(got) != 2 || got[0] != "A Bank" || got[1] != "B Bank" {
		t.Fatalf("gateway names = %v", got)
	}
	if len(progress) != 2 {
		t.Fatalf("progress = %d entries, want 2", len(progress))
	}
	if progress[0].Position != 1 || progress[0].Total != 3 || progress[0].BankName != "A Bank" {
		t.Fatalf("first progress = %+v", progress[0])
	}
	if progress[1].Position != 2 || progress[1].Total != 3 || progress[1].BankName != "B Bank" {
		t.Fatalf("second progress = %+v", progress[1])
	}

	message := stringValue(state.Message)
	if !strings.Contains(message, "A Bank:") {
		t.Fatalf("message = %q", message)
	}
	stopped := i18n.Format(i18n.En, i18n.SyncAllStopped, i18n.Str("B Bank"))
	if !strings.Contains(message, stopped) {
		t.Fatalf("message = %q, want it to contain %q", message, stopped)
	}
	if state.Error != nil {
		t.Fatalf("error = %q", stringValue(state.Error))
	}
	if state.SyncAll != nil {
		t.Fatal("SyncAll is set")
	}
	if busy.Active() {
		t.Fatal("busy is still active")
	}
}

func TestSyncAllWithNothingIncludedReportsTheErrorAndClearsTheProgress(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "A Bank", "a")
	if _, err := bankconnections.SetIncludeInSyncAll(context.Background(), database, bank, false); err != nil {
		t.Fatalf("set include: %v", err)
	}
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	busy := accountsview.NewBusy()
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSyncAll() {
		t.Fatal("begin sync all refused")
	}
	state.RunSyncAll(
		context.Background(),
		database,
		service,
		bridge,
		&busy,
		mustDate(t, 2026, time.November, 7),
		func(*BankingState) {},
	)

	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.SyncAllNothingIncluded) {
		t.Fatalf("error = %q", got)
	}
	if state.Message != nil {
		t.Fatalf("message = %q", stringValue(state.Message))
	}
	if state.SyncAll != nil {
		t.Fatal("SyncAll is set")
	}
	if names := gateway.namesList(); len(names) != 0 {
		t.Fatalf("gateway names = %v", names)
	}
}

func TestIncludeInSyncAllIsRefusedWhileASyncRuns(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "A Bank", "a")
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	state.SyncingBank = &bank
	if err := state.SetIncludeInSyncAll(context.Background(), database, service, bank, false); err == nil {
		t.Fatal("set include succeeded while a sync ran")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %q", got)
	}
	stored, ok, err := bankconnections.GetItem(context.Background(), database, bank)
	if err != nil || !ok {
		t.Fatalf("get item: ok=%t err=%v", ok, err)
	}
	if !stored.IncludeInSyncAll {
		t.Fatal("include changed while a sync ran")
	}

	state.SyncingBank = nil
	if err := state.SetIncludeInSyncAll(context.Background(), database, service, bank, false); err != nil {
		t.Fatalf("set include: %v", err)
	}
	if state.Banks[0].IncludeInSyncAll {
		t.Fatal("state did not reload the include flag")
	}
}

func TestRemoveBankIsRefusedWhileSyncingAndWhileTheProfileIsBusy(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	gateway := newTestGateway()
	service, _ := serviceFor(t, gateway)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}
	busy := accountsview.NewBusy()

	state.SyncingBank = &bank
	if err := state.RemoveBank(context.Background(), database, service, &busy, bank); err == nil {
		t.Fatal("remove succeeded while a sync ran")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %q", got)
	}
	if _, ok, err := bankconnections.GetItem(context.Background(), database, bank); err != nil || !ok {
		t.Fatalf("bank is gone: ok=%t err=%v", ok, err)
	}

	state.SyncingBank = nil
	busy.Begin()
	if err := state.RemoveBank(context.Background(), database, service, &busy, bank); err == nil {
		t.Fatal("remove succeeded while the profile was busy")
	}
	if got := stringValue(state.Error); got != i18n.Text(i18n.En, i18n.ErrorDeleteWhileBusy) {
		t.Fatalf("error = %q", got)
	}
	if _, ok, err := bankconnections.GetItem(context.Background(), database, bank); err != nil || !ok {
		t.Fatalf("bank is gone: ok=%t err=%v", ok, err)
	}
	busy.End()

	if err := state.RemoveBank(context.Background(), database, service, &busy, bank); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok, err := bankconnections.GetItem(context.Background(), database, bank); err != nil || ok {
		t.Fatalf("bank still exists: ok=%t err=%v", ok, err)
	}
	if len(state.Banks) != 0 {
		t.Fatalf("banks = %d, want 0", len(state.Banks))
	}
	if got := stringValue(state.Message); got != i18n.Text(i18n.En, i18n.MessageBankRemoved) {
		t.Fatalf("message = %q", got)
	}
}

func TestHistoryFromStoresTheDateAuditsItAndSyncsWithIt(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	const iban = "DE89370400440532013000"
	gateway := newTestGateway()
	gateway.setSnapshots([]banksync.AccountSnapshot{snapshot(t, iban, "12.34")})
	failure := banksync.NewGatewayOtherError("stop")
	gateway.setFailure(&failure)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}
	today := mustDate(t, 2026, time.November, 7)
	from := today.AddDays(-120)

	if !state.BeginHistoryFrom(bank, from, today) {
		t.Fatal("begin history from refused")
	}
	busy := accountsview.NewBusy()
	state.RunHistoryFrom(context.Background(), database, service, bridge, &busy, bank, from, today)

	if got := gateway.since(iban); got == nil || *got != from {
		t.Fatalf("since = %v, want %s", got, from)
	}
	stored, ok, err := bankconnections.GetItem(context.Background(), database, bank)
	if err != nil || !ok {
		t.Fatalf("get item: ok=%t err=%v", ok, err)
	}
	if stored.SyncFrom == nil || *stored.SyncFrom != from {
		t.Fatalf("sync_from = %v, want %s", stored.SyncFrom, from)
	}
	entries, err := audit.Recent(context.Background(), database, 1000)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "SET_SYNC_FROM" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no SET_SYNC_FROM audit entry")
	}
	if got := stringValue(state.Error); got != "stop" {
		t.Fatalf("error = %q", got)
	}
	if state.SyncingBank != nil {
		t.Fatal("SyncingBank is set")
	}
}

func TestAHistorySyncIsClaimedBeforeItRuns(t *testing.T) {
	state := New(i18n.En)
	today := mustDate(t, 2026, time.November, 7)
	state.Banks = append(state.Banks, BankItem{
		ID:               model.BankConnectionID(7),
		BankName:         "Sparkasse",
		BLZ:              "12345678",
		UserID:           "user",
		ServerURL:        stringPointer("https://bank.example"),
		Offline:          false,
		IncludeInSyncAll: true,
	})

	if state.BeginHistoryFrom(model.BankConnectionID(7), today.AddDays(1), today) {
		t.Fatal("a future history start was accepted")
	}
	if state.SyncingBank != nil {
		t.Fatal("SyncingBank is set")
	}
	if !state.BeginHistoryFrom(model.BankConnectionID(7), today.AddDays(-30), today) {
		t.Fatal("begin history from refused")
	}
	if state.SyncingBank == nil || *state.SyncingBank != model.BankConnectionID(7) {
		t.Fatalf("SyncingBank = %v", state.SyncingBank)
	}
	if state.BeginHistoryFrom(model.BankConnectionID(7), today.AddDays(-30), today) {
		t.Fatal("a second click while the first history sync runs was accepted")
	}
}

func TestARememberedPinIsOfferedOnlyWhenAStoreIsAvailable(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	dir := dataDir(t)
	gateway := newTestGateway()
	gateway.setAskPin(true)
	store := banksync.NewMemoryCredentialStore()
	service := serviceWithStore(gateway, store, dir, i18n.En)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !state.CanRememberPin {
		t.Fatal("CanRememberPin = false")
	}

	bridge := bridgeForTest(5 * time.Second)
	answerer := answerPinInBackground(bridge, "12345", true)
	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))
	<-answerer

	if !service.HasSavedPin(context.Background(), database, bank) {
		t.Fatal("HasSavedPin = false")
	}
	if !store.Contains(banksync.PinKey(dir, int64(bank))) {
		t.Fatal("credential store has no PIN entry")
	}
	if !state.Banks[0].HasSavedPin {
		t.Fatal("state did not reload the saved-PIN flag")
	}

	noStore := serviceWithStore(gateway, banksync.NoCredentialStore{}, dir, i18n.En)
	without := New(i18n.En)
	if err := without.Load(context.Background(), database, noStore); err != nil {
		t.Fatalf("load without store: %v", err)
	}
	if without.CanRememberPin {
		t.Fatal("CanRememberPin = true without a store")
	}
}

func TestAPinTheStoreRefusesIsReportedWithThePinNotSavedNote(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	gateway := newTestGateway()
	gateway.setAskPin(true)
	store := banksync.NewMemoryCredentialStore().FailingWrites()
	service := serviceWithStore(gateway, store, dataDir(t), i18n.En)
	bridge := bridgeForTest(5 * time.Second)
	answerer := answerPinInBackground(bridge, "12345", true)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))
	<-answerer

	if state.Error != nil {
		t.Fatalf("error = %q", stringValue(state.Error))
	}
	message := stringValue(state.Message)
	if !strings.Contains(message, i18n.Text(i18n.En, i18n.PinNotSaved)) {
		t.Fatalf("message = %q", message)
	}
}

func TestNoAuditEntryContainsBankMessageTextOrAPin(t *testing.T) {
	database := testDatabase(t)
	bank := onlineBank(t, database, "Sparkasse", "user")
	gateway := newTestGateway()
	gateway.setAskPin(true)
	failure := banksync.NewGatewayOtherError("BANKMESSAGE 1234567890 gesperrt")
	gateway.setFailure(&failure)
	service, _ := serviceFor(t, gateway)
	bridge := bridgeForTest(5 * time.Second)
	answerer := answerPinInBackground(bridge, "246810", false)
	state := New(i18n.En)
	if err := state.Load(context.Background(), database, service); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !state.BeginSync(bank) {
		t.Fatal("begin sync refused")
	}
	state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))
	<-answerer

	if got := stringValue(state.Error); !strings.Contains(got, "…7890") {
		t.Fatalf("error = %q", got)
	}
	entries, err := audit.Recent(context.Background(), database, 10_000)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, entry := range entries {
		haystack := fmt.Sprintf("%s %s %s", entry.At, entry.Action, entry.Details)
		for _, forbidden := range []string{"246810", "1234567890", "BANKMESSAGE", "gesperrt"} {
			if strings.Contains(haystack, forbidden) {
				t.Fatalf("an audit entry leaked %q: %s", forbidden, haystack)
			}
		}
	}
}

func TestTheUnavailableGatewayFailsWithTheNewTextInEveryLanguage(t *testing.T) {
	for _, language := range i18n.Languages {
		database := testDatabase(t)
		bank := onlineBank(t, database, "Sparkasse", "user")
		dir := dataDir(t)
		gateway := banksync.NewUnavailableGateway(language)
		service := banksync.NewBankingService(
			banksync.NewFinTsClient(gateway, dir, "TEST-PRODUCT", banksync.NewMemoryCredentialStore()).
				WithLanguage(language),
		)
		bridge := bridgeForTest(5 * time.Second)
		state := New(language)
		if err := state.Load(context.Background(), database, service); err != nil {
			t.Fatalf("load: %v", err)
		}

		if !state.BeginSync(bank) {
			t.Fatal("begin sync refused")
		}
		state.RunSync(context.Background(), database, service, bridge, &accountsview.Busy{}, bank, mustDate(t, 2026, time.November, 7))

		if got := stringValue(state.Error); got != i18n.Text(language, i18n.ErrorOnlineSyncUnavailable) {
			t.Fatalf("language %s: error = %q", language.Code(), got)
		}
	}
}

// asBankingError is the test-side errors.As for banksync.BankingError values.
func asBankingError(err error, target *banksync.BankingError) bool {
	return errors.As(err, target)
}
