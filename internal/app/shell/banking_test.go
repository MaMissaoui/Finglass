package shell

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// bankingTestGateway is a fake FinTS gateway: it never opens a socket and only
// interacts through the prompt bridge the shell gives it.
type bankingTestGateway struct {
	mu           sync.Mutex
	calls        int
	pinPrompts   int
	askTan       bool
	rejectStored bool
	failure      *banksync.GatewayError
	snapshots    []banksync.AccountSnapshot
	receivedPins []string
	receivedTans []string
	promptClosed chan struct{}
}

func newBankingTestGateway() *bankingTestGateway {
	return &bankingTestGateway{askTan: true, promptClosed: make(chan struct{}, 4)}
}

func (g *bankingTestGateway) setSnapshots(snapshots []banksync.AccountSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.snapshots = append([]banksync.AccountSnapshot(nil), snapshots...)
}

func (g *bankingTestGateway) setFailure(failure banksync.GatewayError) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failure = &failure
}

func (g *bankingTestGateway) setPinPrompts(count int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pinPrompts = count
}

func (g *bankingTestGateway) FetchAll(
	_ context.Context,
	connection banksync.FinTsConnection,
	interaction banksync.BankingInteraction,
	_ func(string) *civil.Date,
	_ civil.Date,
) (banksync.FetchOutcome, error) {
	g.mu.Lock()
	g.calls++
	pinPrompts := g.pinPrompts
	if pinPrompts == 0 {
		pinPrompts = 1
	}
	askTan := g.askTan
	failure := g.failure
	snapshots := append([]banksync.AccountSnapshot(nil), g.snapshots...)
	g.mu.Unlock()

	for index := 0; index < pinPrompts; index++ {
		entry := interaction.Pin(connection.BankName, connection.UserID, true)
		if entry == nil {
			g.signalClosed()
			return banksync.FetchOutcome{}, banksync.NewGatewayCancelledError()
		}
		if entry.Pin() != nil {
			var pin string
			_ = entry.Pin().Use(func(value []byte) error {
				pin = string(value)
				return nil
			})
			g.mu.Lock()
			g.receivedPins = append(g.receivedPins, pin)
			g.mu.Unlock()
		}
	}
	if g.rejectStored {
		return banksync.FetchOutcome{}, banksync.NewGatewayPinRejectedError()
	}
	if failure != nil {
		return banksync.FetchOutcome{}, *failure
	}
	if askTan {
		tan := interaction.Tan(banksync.TanChallenge{Message: "Bitte TAN eingeben"})
		if tan == nil {
			g.signalClosed()
			return banksync.FetchOutcome{}, banksync.NewGatewayCancelledError()
		}
		var value string
		_ = tan.Use(func(bytes []byte) error {
			value = string(bytes)
			return nil
		})
		g.mu.Lock()
		g.receivedTans = append(g.receivedTans, value)
		g.mu.Unlock()
	}
	return banksync.FetchOutcome{Snapshots: snapshots}, nil
}

func (g *bankingTestGateway) signalClosed() {
	select {
	case g.promptClosed <- struct{}{}:
	default:
	}
}

func (g *bankingTestGateway) counts() (int, int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls, len(g.receivedPins), len(g.receivedTans)
}

func bankingSnapshot() banksync.AccountSnapshot {
	account := banksync.FinTsAccount{
		IBAN:           "DE89370400440532013000",
		BIC:            "COLSDE33XXX",
		AccountNumber:  "0532013000",
		BankName:       "Sparkasse",
		BLZ:            "37040044",
		AccountType:    "checking",
		Currency:       "EUR",
		CurrencyStated: true,
	}
	partner := "Bakery"
	purpose := "Bread"
	valueDate := testToday()
	return banksync.AccountSnapshot{
		Account: account,
		Balance: &banksync.FinTsBalance{
			Account:  account,
			Balance:  decimal.New(123456, 2),
			Currency: "EUR",
			Date:     testNow,
		},
		Transactions: []banksync.FinTsTransaction{{
			Account:     account,
			Date:        testToday(),
			ValueDate:   valueDate,
			Amount:      decimal.New(-1234, 2),
			Currency:    "EUR",
			PartnerName: &partner,
			Purpose:     &purpose,
		}},
		Pending: []banksync.FinTsTransaction{},
	}
}

func seedBankingDatabase(t *testing.T, appPaths paths.AppPaths, userID string) model.BankConnectionID {
	t.Helper()
	database := createTestDatabase(t, appPaths, "default")
	bic := "COLSDE33XXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: "Sparkasse",
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
	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return created.ID
}

func newBankingSession(
	t *testing.T,
	store credentials.Store,
	gateway *bankingTestGateway,
) (*Session, model.BankConnectionID, paths.AppPaths) {
	t.Helper()
	appPaths := testPaths(t)
	bankID := seedBankingDatabase(t, appPaths, "user")
	session := newSession(
		appPaths,
		store,
		func() time.Time { return testNow },
		func() civil.Date { return testToday() },
		testGetenv,
		i18n.En,
	)
	if gateway != nil {
		session.gatewayFactory = func(i18n.Language) banksync.FinTsGateway {
			return gateway
		}
	}
	t.Cleanup(func() { _ = session.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	session.BankingLoad()
	waitForJobs(t, session)
	return session, bankID, appPaths
}

func waitForBankingPrompt(t *testing.T, session *Session, kind banking.PromptKind) banking.BankingPrompt {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if prompt := session.Bridge().Prompt(); prompt != nil && prompt.Kind == kind {
			return *prompt
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for prompt kind %d", kind)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitForBankingPromptAfter(
	t *testing.T,
	session *Session,
	kind banking.PromptKind,
	after uint64,
) banking.BankingPrompt {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if prompt := session.Bridge().Prompt(); prompt != nil && prompt.Kind == kind && prompt.ID != after {
			return *prompt
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for a new prompt kind %d after %d", kind, after)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitForBankingIdle(t *testing.T, session *Session) banking.BankingState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := session.BankingState()
		if !state.IsSyncing() {
			return state
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the banking state never became idle: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func answerBankingPin(t *testing.T, session *Session, pin string, remember bool) {
	t.Helper()
	prompt := waitForBankingPrompt(t, session, banking.Pin)
	session.Bridge().AnswerPin(prompt.ID, []byte(pin), remember)
}

func answerBankingTan(t *testing.T, session *Session, tan string) {
	t.Helper()
	prompt := waitForBankingPrompt(t, session, banking.Tan)
	session.Bridge().AnswerTan(prompt.ID, []byte(tan))
}

func bankingPinKey(appPaths paths.AppPaths, bankID model.BankConnectionID) string {
	dir, err := appPaths.ProfileDir("default")
	if err != nil {
		panic(err)
	}
	return banksync.PinKey(dir, int64(bankID))
}

func saveBankingPin(
	t *testing.T,
	session *Session,
	store *banksync.MemoryCredentialStore,
	appPaths paths.AppPaths,
	bankID model.BankConnectionID,
) {
	t.Helper()
	database := sessionDatabase(t, session)
	bank, ok, err := bankconnections.GetItem(context.Background(), database, bankID)
	if err != nil || !ok {
		t.Fatalf("get bank: ok=%t err=%v", ok, err)
	}
	if err := store.Write(bankingPinKey(appPaths, bankID), []byte("24680")); err != nil {
		t.Fatalf("write PIN: %v", err)
	}
	if err := settings.Set(
		context.Background(),
		database,
		banksync.PinFlagKey(bankID),
		banksync.LoginSignature(bank),
	); err != nil {
		t.Fatalf("write PIN flag: %v", err)
	}
	dir, err := appPaths.ProfileDir("default")
	if err != nil {
		t.Fatalf("profile dir: %v", err)
	}
	marker := banksync.PinMarkerPath(dir, int64(bankID))
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func hasBankingPin(
	t *testing.T,
	session *Session,
	store *banksync.MemoryCredentialStore,
	appPaths paths.AppPaths,
	bankID model.BankConnectionID,
) bool {
	t.Helper()
	database := sessionDatabase(t, session)
	if _, ok, err := bankconnections.GetItem(context.Background(), database, bankID); err != nil || !ok {
		return false
	}
	if _, ok, err := settings.Get(context.Background(), database, banksync.PinFlagKey(bankID)); err != nil || !ok {
		return false
	}
	return store.Contains(bankingPinKey(appPaths, bankID))
}

func TestABankingSyncAsksForPinAndTanThenReloadsAccountsAndDashboard(t *testing.T) {
	gateway := newBankingTestGateway()
	gateway.setSnapshots([]banksync.AccountSnapshot{bankingSnapshot()})
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)
	events, remove := listenForChanges(session)
	defer remove()

	session.BankingStartSync(int(bankID))
	prompt := waitForBankingPrompt(t, session, banking.Pin)
	if prompt.BankName != "Sparkasse" || prompt.UserID != "user" || !prompt.OfferRemember {
		t.Fatalf("pin prompt = %+v", prompt)
	}
	session.Bridge().AnswerPin(prompt.ID, []byte("12345"), false)
	answerBankingTan(t, session, "999999")

	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("sync error = %q", *state.Error)
	}
	if state.Message == nil || !strings.Contains(*state.Message, "Synced") {
		t.Fatalf("sync message = %v", state.Message)
	}

	deadline := time.Now().Add(10 * time.Second)
	for len(session.AccountsState().Accounts) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the synced account did not land")
		}
		time.Sleep(2 * time.Millisecond)
	}

	calls, pins, tans := gateway.counts()
	if calls != 1 || pins != 1 || tans != 1 {
		t.Fatalf("gateway calls/pins/tans = %d/%d/%d", calls, pins, tans)
	}

	saw := map[string]bool{}
	for len(events) > 0 {
		saw[(<-events).Page] = true
	}
	for _, page := range []string{"banking", "banking-prompt", "accounts", "dashboard"} {
		if !saw[page] {
			t.Fatalf("missing %s event: %v", page, saw)
		}
	}
}

func TestRememberPinIsKeptOnlyWhenTickedAndOnlyAfterSuccess(t *testing.T) {
	t.Run("ticked after success", func(t *testing.T) {
		store := banksync.NewMemoryCredentialStore()
		gateway := newBankingTestGateway()
		gateway.askTan = false
		gateway.setSnapshots([]banksync.AccountSnapshot{bankingSnapshot()})
		session, bankID, appPaths := newBankingSession(t, store, gateway)

		session.BankingStartSync(int(bankID))
		answerBankingPin(t, session, "13579", true)
		state := waitForBankingIdle(t, session)
		if state.Error != nil {
			t.Fatalf("sync error = %q", *state.Error)
		}
		if !hasBankingPin(t, session, store, appPaths, bankID) {
			t.Fatal("the opted-in PIN was not kept")
		}
	})

	t.Run("not ticked", func(t *testing.T) {
		store := banksync.NewMemoryCredentialStore()
		gateway := newBankingTestGateway()
		gateway.askTan = false
		session, bankID, appPaths := newBankingSession(t, store, gateway)

		session.BankingStartSync(int(bankID))
		answerBankingPin(t, session, "13579", false)
		waitForBankingIdle(t, session)
		if hasBankingPin(t, session, store, appPaths, bankID) || !store.IsEmpty() {
			t.Fatal("an unticked PIN was stored")
		}
	})

	t.Run("ticked after failure", func(t *testing.T) {
		store := banksync.NewMemoryCredentialStore()
		gateway := newBankingTestGateway()
		gateway.askTan = false
		gateway.setFailure(banksync.NewGatewayPinRejectedError())
		session, bankID, appPaths := newBankingSession(t, store, gateway)

		session.BankingStartSync(int(bankID))
		answerBankingPin(t, session, "13579", true)
		waitForBankingIdle(t, session)
		if hasBankingPin(t, session, store, appPaths, bankID) || !store.IsEmpty() {
			t.Fatal("a PIN was kept after a failed dialog")
		}
	})
}

func TestARejectedRememberedPinIsDeleted(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	gateway.askTan = false
	gateway.rejectStored = true
	session, bankID, appPaths := newBankingSession(t, store, gateway)
	saveBankingPin(t, session, store, appPaths, bankID)
	if !hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("test setup: no remembered PIN")
	}

	session.BankingStartSync(int(bankID))
	state := waitForBankingIdle(t, session)
	if state.Error == nil {
		t.Fatal("a rejected PIN did not publish an error")
	}
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the rejected PIN is still remembered")
	}
}

func TestChangingALoginFieldDeletesTheRememberedPin(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	session, bankID, appPaths := newBankingSession(t, store, gateway)
	saveBankingPin(t, session, store, appPaths, bankID)

	session.BankingSubmitConnection(
		int(bankID),
		"Sparkasse",
		"37040044",
		"other-user",
		"https://fints.example/pintan",
	)
	waitForJobs(t, session)
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the PIN survived a user-id change")
	}

	saveBankingPin(t, session, store, appPaths, bankID)
	session.BankingSubmitConnection(
		int(bankID),
		"Sparkasse",
		"37050198",
		"other-user",
		"https://fints.example/pintan",
	)
	waitForJobs(t, session)
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the PIN survived a bank-code change")
	}

	saveBankingPin(t, session, store, appPaths, bankID)
	session.BankingSubmitConnection(
		int(bankID),
		"Sparkasse",
		"37050198",
		"other-user",
		"https://other.example/pintan",
	)
	waitForJobs(t, session)
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the PIN survived a server-URL change")
	}
}

func TestRemovingAProfileForgetsItsRememberedPins(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	dir := t.TempDir()
	key := banksync.PinKey(dir, 4)
	if err := store.Write(key, []byte("24680")); err != nil {
		t.Fatalf("write PIN: %v", err)
	}
	marker := banksync.PinMarkerPath(dir, 4)
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	banksync.ForgetPinsForProfile(store, dir)
	if store.Contains(key) {
		t.Fatal("the profile's PIN survived its removal")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the marker survived: %v", err)
	}
}

func TestPromptsAnsweredFromAnotherGoroutineWhileTheSyncRuns(t *testing.T) {
	gateway := newBankingTestGateway()
	gateway.setSnapshots([]banksync.AccountSnapshot{bankingSnapshot()})
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)

	answers := make(chan struct{}, 1)
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		answeredPin := false
		answeredTan := false
		for !answeredPin || !answeredTan {
			if !time.Now().Before(deadline) {
				close(answers)
				return
			}
			prompt := session.Bridge().Prompt()
			if prompt == nil {
				time.Sleep(time.Millisecond)
				continue
			}
			switch {
			case !answeredPin && prompt.Kind == banking.Pin:
				session.Bridge().AnswerPin(prompt.ID, []byte("12345"), false)
				answeredPin = true
			case answeredPin && !answeredTan && prompt.Kind == banking.Tan:
				session.Bridge().AnswerTan(prompt.ID, []byte("999999"))
				answeredTan = true
			}
			time.Sleep(time.Millisecond)
		}
		close(answers)
	}()

	session.BankingStartSync(int(bankID))
	select {
	case <-answers:
	case <-time.After(10 * time.Second):
		t.Fatal("the concurrent answers did not finish")
	}
	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("sync error = %q", *state.Error)
	}
	if _, pins, tans := gateway.counts(); pins != 1 || tans != 1 {
		t.Fatalf("gateway received %d PINs and %d TANs", pins, tans)
	}
}

func TestTheHistoryDialogValidatesAndStartsAHistorySync(t *testing.T) {
	gateway := newBankingTestGateway()
	gateway.askTan = false
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)

	session.BankingOpenHistoryDialog(int(bankID))
	dialog := session.AccountsDialog()
	if dialog.Kind != "history" || dialog.HistoryDate == "" || dialog.HistoryHasStart {
		t.Fatalf("history dialog = %+v", dialog)
	}
	if dialog.Title != i18n.Format(i18n.En, i18n.FetchHistoryFor, i18n.Str("Sparkasse")) {
		t.Fatalf("history title = %q", dialog.Title)
	}

	session.BankingSubmitHistory(int(bankID), "not a date")
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.ErrorSyncFromDateInvalid) {
		t.Fatalf("invalid-date error = %q", dialog.ErrorText)
	}

	future := testToday().AddDays(3)
	session.BankingSubmitHistory(int(bankID), format.Date(future))
	if dialog := session.AccountsDialog(); dialog.ErrorText != i18n.Text(i18n.En, i18n.ErrorSyncFromDateFuture) {
		t.Fatalf("future-date error = %q", dialog.ErrorText)
	}

	session.BankingSubmitHistory(int(bankID), format.Date(testToday().AddDays(-120)))
	answerBankingPin(t, session, "12345", false)
	state := waitForBankingIdle(t, session)
	if state.Error != nil {
		t.Fatalf("history sync error = %q", *state.Error)
	}
	if _, pins, _ := gateway.counts(); pins != 1 {
		t.Fatalf("history sync asked for %d PINs", pins)
	}
}

func TestRemovingAnOnlineBankDeletesItsRememberedPin(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	session, bankID, appPaths := newBankingSession(t, store, gateway)
	saveBankingPin(t, session, store, appPaths, bankID)

	session.BankingRemoveBank(int(bankID))
	waitForJobs(t, session)
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the removed bank's PIN is still remembered")
	}
	if _, ok, err := bankconnections.GetItem(context.Background(), sessionDatabase(t, session), bankID); err != nil || ok {
		t.Fatalf("the bank still exists: ok=%t err=%v", ok, err)
	}
}

func TestATanIsNeverStoredAndAPinOnlyInTheCredentialStore(t *testing.T) {
	const (
		pin = "PIN-24680-SECRET"
		tan = "TAN-13579-SECRET"
	)
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	gateway.setSnapshots([]banksync.AccountSnapshot{bankingSnapshot()})
	session, bankID, appPaths := newBankingSession(t, store, gateway)

	session.BankingStartSync(int(bankID))
	answerBankingPin(t, session, pin, true)
	answerBankingTan(t, session, tan)
	waitForBankingIdle(t, session)

	database := sessionDatabase(t, session)
	assertNoCellContains(t, database, pin, tan)

	if !store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatal("the opted-in PIN is not in the credential store")
	}
	if containsBytesInStore(t, store, bankingPinKey(appPaths, bankID), tan) {
		t.Fatal("the TAN is in the credential store")
	}
	if store.Len() != 1 {
		t.Fatalf("the credential store holds %d entries, want only the PIN", store.Len())
	}
}

func TestATanAndAnUnrememberedPinNeverReachTheDatabase(t *testing.T) {
	const (
		pin = "PIN-11111-SECRET"
		tan = "TAN-22222-SECRET"
	)
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	session, bankID, _ := newBankingSession(t, store, gateway)

	session.BankingStartSync(int(bankID))
	answerBankingPin(t, session, pin, false)
	answerBankingTan(t, session, tan)
	waitForBankingIdle(t, session)

	assertNoCellContains(t, sessionDatabase(t, session), pin, tan)
	if !store.IsEmpty() {
		t.Fatalf("the credential store holds %d entries", store.Len())
	}
}

func assertNoCellContains(t *testing.T, database *db.Database, needles ...string) {
	t.Helper()
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		tables, err := tableNames(ctx, conn)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if err := checkTable(ctx, conn, table, needles); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func tableNames(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tables := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func checkTable(ctx context.Context, conn *sql.Conn, table string, needles []string) error {
	var query strings.Builder
	query.WriteString("SELECT * FROM ")
	query.WriteByte('"')
	query.WriteString(strings.ReplaceAll(table, "\"", "\"\""))
	query.WriteByte('"')
	rows, err := conn.QueryContext(ctx, query.String())
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		for index, value := range values {
			text := fmt.Sprint(value)
			for _, needle := range needles {
				if strings.Contains(text, needle) {
					return fmt.Errorf("table %s column %s holds %q", table, columns[index], needle)
				}
			}
		}
	}
	return rows.Err()
}

func containsBytesInStore(t *testing.T, store *banksync.MemoryCredentialStore, key string, needle string) bool {
	t.Helper()
	value, found, err := store.Read(key)
	if err != nil {
		t.Fatalf("read credential store: %v", err)
	}
	return found && strings.Contains(string(value), needle)
}

func TestAStalePromptAnswerLeavesTheNextPromptOpen(t *testing.T) {
	gateway := newBankingTestGateway()
	gateway.askTan = false
	gateway.setPinPrompts(2)
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)

	session.BankingStartSync(int(bankID))
	first := waitForBankingPrompt(t, session, banking.Pin)
	session.Bridge().AnswerPin(first.ID, []byte("first"), false)

	second := waitForBankingPromptAfter(t, session, banking.Pin, first.ID)
	if second.ID == first.ID {
		t.Fatal("the second prompt reused the first prompt's id")
	}
	session.Bridge().AnswerPin(first.ID, []byte("late"), false)
	// A wrongly accepted answer is consumed by the worker asynchronously; give it
	// time to reach the fake bank before checking that nothing did.
	time.Sleep(300 * time.Millisecond)
	open := session.Bridge().Prompt()
	if open == nil || open.ID != second.ID || open.Kind != banking.Pin {
		t.Fatalf("the stale answer closed the second prompt: %+v", open)
	}
	_, pins, _ := gateway.counts()
	if pins != 1 {
		t.Fatalf("the fake bank received %d PINs before the second answer, want 1", pins)
	}
	session.Bridge().AnswerPin(second.ID, []byte("second"), false)
	waitForBankingIdle(t, session)
	_, pins, _ = gateway.counts()
	if pins != 2 {
		t.Fatalf("the fake bank received %d PINs, want 2", pins)
	}
	// The worker consumes an answer asynchronously, so the checks above can run
	// before a wrongly accepted stale answer arrives. The PINs the bank finally
	// received prove the late answer never reached it.
	gateway.mu.Lock()
	received := append([]string(nil), gateway.receivedPins...)
	gateway.mu.Unlock()
	if len(received) != 2 || received[0] != "first" || received[1] != "second" {
		t.Fatalf("the fake bank received the PINs %q, want first and second", received)
	}
}

func TestCancellingThePinOrTanEndsTheSyncWithoutAnError(t *testing.T) {
	t.Run("pin", func(t *testing.T) {
		gateway := newBankingTestGateway()
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)
		session.BankingStartSync(int(bankID))
		prompt := waitForBankingPrompt(t, session, banking.Pin)
		session.Bridge().AnswerPin(prompt.ID, nil, false)
		state := waitForBankingIdle(t, session)
		if state.Error != nil || state.Message != nil {
			t.Fatalf("cancel published messages: %v / %v", state.Error, state.Message)
		}
	})

	t.Run("tan", func(t *testing.T) {
		gateway := newBankingTestGateway()
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)
		session.BankingStartSync(int(bankID))
		answerBankingPin(t, session, "12345", false)
		prompt := waitForBankingPrompt(t, session, banking.Tan)
		session.Bridge().AnswerTan(prompt.ID, nil)
		state := waitForBankingIdle(t, session)
		if state.Error != nil || state.Message != nil {
			t.Fatalf("cancel published messages: %v / %v", state.Error, state.Message)
		}
	})

	t.Run("teardown cancels and the old bridge never notifies", func(t *testing.T) {
		gateway := newBankingTestGateway()
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)
		events, remove := listenForChanges(session)
		defer remove()

		session.BankingStartSync(int(bankID))
		waitForBankingPrompt(t, session, banking.Pin)
		oldBridge := session.Bridge()
		session.Teardown(startup.Startup{})

		select {
		case <-gateway.promptClosed:
		case <-time.After(5 * time.Second):
			t.Fatal("the sync's PIN prompt was not cancelled")
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			state := session.BankingState()
			if !state.IsSyncing() && state.Error == nil && state.Message == nil {
				break
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("the torn-down sync published its result: %+v", state)
			}
			time.Sleep(2 * time.Millisecond)
		}
		for len(events) > 0 {
			<-events
		}
		oldBridge.Info("late bank message")
		time.Sleep(50 * time.Millisecond)
		for len(events) > 0 {
			if change := <-events; change.Page == "banking-prompt" {
				t.Fatal("the torn-down bridge notified the next profile")
			}
		}
	})
}

func TestBankMessageTextWithAnAccountNumberIsMaskedAndNeverAudited(t *testing.T) {
	gateway := newBankingTestGateway()
	gateway.askTan = false
	gateway.setFailure(banksync.NewGatewayOtherError("Konto DE89370400440532013000 gesperrt"))
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, gateway)

	session.BankingStartSync(int(bankID))
	answerBankingPin(t, session, "12345", false)
	state := waitForBankingIdle(t, session)
	if state.Error == nil {
		t.Fatal("the bank failure did not publish an error")
	}
	if strings.Contains(*state.Error, "DE89370400440532013000") {
		t.Fatalf("error text holds the full account number: %q", *state.Error)
	}
	if !strings.Contains(*state.Error, "…3000") {
		t.Fatalf("error text is not masked to the last four: %q", *state.Error)
	}

	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Details, "DE89370400440532013000") {
			t.Fatalf("audit entry holds the account number: %q", entry.Details)
		}
	}
}

func TestWithTheUnavailableGatewaySyncControlsFollowTheFlag(t *testing.T) {
	if OnlineSyncAvailable {
		t.Fatal("OnlineSyncAvailable must be false while the real gateway is not wired in")
	}
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	session.BankingStartSync(int(bankID))
	waitForJobs(t, session)
	state := waitForBankingIdle(t, session)
	if state.Error == nil {
		t.Fatal("the unavailable gateway did not publish an error")
	}
	if !strings.Contains(*state.Error, i18n.Text(i18n.En, i18n.ErrorOnlineSyncUnavailable)) {
		t.Fatalf("error = %q, want the unavailable-gateway text", *state.Error)
	}
}
