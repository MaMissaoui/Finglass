package bind

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/bankdirectory"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
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

type bankingFixture struct {
	paths    paths.AppPaths
	session  *shell.Session
	accounts *Accounts
	banking  *Banking
	router   *Router
	bankID   model.BankConnectionID
	store    credentials.Store

	mu             sync.Mutex
	bankingEvents  []BankingDTO
	promptEvents   []PromptDTO
	accountsEvents []AccountsDTO
	bankingChanged chan BankingDTO
	promptChanged  chan PromptDTO
}

func newBankingFixture(
	t *testing.T,
	store credentials.Store,
	seed func(*db.Database, model.BankConnectionID),
) *bankingFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	database, err := unlock.Create(appPaths, "default", []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	bic := "COLSDE33XXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: "Sparkasse",
		BLZ:      "37040044",
		BIC:      &bic,
		UserID:   "user",
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
	if seed != nil {
		seed(database, created.ID)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}

	session, _, refusal := shell.Start(
		appPaths,
		store,
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	fixture := &bankingFixture{
		paths:          appPaths,
		session:        session,
		bankID:         created.ID,
		store:          store,
		bankingChanged: make(chan BankingDTO, 64),
		promptChanged:  make(chan PromptDTO, 64),
	}
	fixture.accounts = NewAccounts(session, AccountsOptions{
		Emit: func(dto AccountsDTO) {
			fixture.mu.Lock()
			fixture.accountsEvents = append(fixture.accountsEvents, dto)
			fixture.mu.Unlock()
		},
	})
	fixture.banking = NewBanking(session, BankingOptions{
		Emit: func(dto BankingDTO) {
			fixture.mu.Lock()
			fixture.bankingEvents = append(fixture.bankingEvents, dto)
			fixture.mu.Unlock()
			fixture.bankingChanged <- dto
		},
		EmitPrompt: func(dto PromptDTO) {
			fixture.mu.Lock()
			fixture.promptEvents = append(fixture.promptEvents, dto)
			fixture.mu.Unlock()
			fixture.promptChanged <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("accounts", fixture.accounts)
	fixture.router.Register("banking", fixture.banking)
	fixture.router.RegisterPrompt("banking-prompt", fixture.banking)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.banking.ServiceShutdown()
		_ = fixture.accounts.ServiceShutdown()
		_ = session.Shutdown()
	})

	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := fixture.accounts.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return fixture
}

func (f *bankingFixture) waitBanking(t *testing.T, description string, predicate func(BankingDTO) bool) BankingDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.bankingChanged:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			current, _ := f.banking.State()
			if predicate(current) {
				return current
			}
			t.Fatalf("timed out waiting for %s: %+v", description, current)
		}
	}
}

func (f *bankingFixture) waitAccounts(t *testing.T, description string, predicate func(AccountsDTO) bool) AccountsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-f.bankingChanged:
			// keep draining so a stale event cannot mask a timeout
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		default:
		}
		f.mu.Lock()
		events := append([]AccountsDTO(nil), f.accountsEvents...)
		f.mu.Unlock()
		for index := len(events) - 1; index >= 0; index-- {
			if predicate(events[index]) {
				return events[index]
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *bankingFixture) waitPrompt(t *testing.T, kind string) PromptDTO {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		dto, err := f.banking.Prompt()
		if err == nil && dto.Kind == kind {
			return dto
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for a %q prompt: %+v", kind, dto)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestBindBankingPromptFlowAnswersThePinAndTanTheBridgeAskedFor(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	fixture.waitAccounts(t, "the loaded bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0 && dto.BankCards[0].BankID == int(fixture.bankID)
	})
	fixture.waitBanking(t, "the first banking:changed event", func(BankingDTO) bool { return true })

	pinResult := make(chan *banksync.PinEntry, 1)
	go func() {
		pinResult <- fixture.session.Bridge().Pin("Sparkasse", "user", true)
	}()
	prompt := fixture.waitPrompt(t, "pin")
	select {
	case event := <-fixture.promptChanged:
		if event.Kind != "pin" || event.ID != prompt.ID {
			t.Fatalf("banking:prompt payload = %+v, want the open pin prompt", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no banking:prompt event")
	}
	if prompt.ID == 0 || prompt.Title != i18n.Format(i18n.En, i18n.PinFor, i18n.Str("Sparkasse")) {
		t.Fatalf("pin prompt DTO = %+v", prompt)
	}
	if prompt.BankName != "Sparkasse" || prompt.UserID != "user" || prompt.RememberVisible {
		t.Fatalf("pin prompt fields = %+v", prompt)
	}
	if err := fixture.banking.AnswerPin(prompt.ID, "12345", false); err != nil {
		t.Fatalf("AnswerPin: %v", err)
	}
	var pinEntry *banksync.PinEntry
	select {
	case pinEntry = <-pinResult:
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge did not receive the PIN")
	}
	if pinEntry == nil {
		t.Fatal("the bridge returned no PIN entry")
	}
	var gotPin string
	if err := pinEntry.Pin().Use(func(value []byte) error {
		gotPin = string(value)
		return nil
	}); err != nil {
		t.Fatalf("read PIN: %v", err)
	}
	if gotPin != "12345" {
		t.Fatalf("PIN = %q", gotPin)
	}
	pinEntry.Pin().Wipe()

	tanResult := make(chan string, 1)
	go func() {
		tan := fixture.session.Bridge().Tan(banksync.TanChallenge{Message: "Bitte TAN eingeben"})
		if tan == nil {
			tanResult <- ""
			return
		}
		var value string
		_ = tan.Use(func(bytes []byte) error {
			value = string(bytes)
			return nil
		})
		tanResult <- value
	}()
	tanPrompt := fixture.waitPrompt(t, "tan")
	if tanPrompt.Challenge != "Bitte TAN eingeben" || tanPrompt.Label != i18n.Text(i18n.En, i18n.TanLabel) {
		t.Fatalf("tan prompt DTO = %+v", tanPrompt)
	}
	if err := fixture.banking.AnswerTan(tanPrompt.ID, "999999"); err != nil {
		t.Fatalf("AnswerTan: %v", err)
	}
	select {
	case tan := <-tanResult:
		if tan != "999999" {
			t.Fatalf("TAN = %q", tan)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge did not receive the TAN")
	}
}

func TestBindAnswerPinWithoutACredentialStoreDropsRemember(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	fixture.waitAccounts(t, "the loaded bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0
	})
	result := make(chan *banksync.PinEntry, 1)
	go func() {
		result <- fixture.session.Bridge().Pin("Sparkasse", "user", true)
	}()
	prompt := fixture.waitPrompt(t, "pin")
	if err := fixture.banking.AnswerPin(prompt.ID, "12345", true); err != nil {
		t.Fatalf("AnswerPin: %v", err)
	}
	select {
	case entry := <-result:
		if entry == nil {
			t.Fatal("the bridge returned no entry")
		}
		if entry.Remember() {
			t.Fatal("remember survived without a credential store")
		}
		entry.Pin().Wipe()
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge did not answer")
	}
}

func TestEveryPinAndTanByteSliceTheServiceCreatesIsWiped(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	fixture.waitAccounts(t, "the loaded bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0
	})

	var (
		mu       sync.Mutex
		captured [][]byte
	)
	fixture.banking.answerSeam = func(raw []byte) {
		mu.Lock()
		captured = append(captured, raw)
		mu.Unlock()
	}

	pinResult := make(chan *banksync.PinEntry, 1)
	go func() {
		pinResult <- fixture.session.Bridge().Pin("Sparkasse", "user", true)
	}()
	prompt := fixture.waitPrompt(t, "pin")
	if err := fixture.banking.AnswerPin(prompt.ID, "12345", false); err != nil {
		t.Fatalf("AnswerPin: %v", err)
	}
	select {
	case entry := <-pinResult:
		if entry != nil {
			entry.Pin().Wipe()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge did not answer the PIN")
	}

	tanResult := make(chan *secret.Text, 1)
	go func() {
		tanResult <- fixture.session.Bridge().Tan(banksync.TanChallenge{Message: "TAN"})
	}()
	tanPrompt := fixture.waitPrompt(t, "tan")
	if err := fixture.banking.AnswerTan(tanPrompt.ID, "999999"); err != nil {
		t.Fatalf("AnswerTan: %v", err)
	}
	select {
	case entry := <-tanResult:
		if entry != nil {
			entry.Wipe()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge did not answer the TAN")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("captured %d answer slices, want 2", len(captured))
	}
	for index, raw := range captured {
		for offset, value := range raw {
			if value != 0 {
				t.Fatalf("answer slice %d byte %d is not wiped", index, offset)
			}
		}
	}
}

func TestNoDTOOrEventCarriesThePinOrTan(t *testing.T) {
	const (
		pin = "PIN-873465-SECRET"
		tan = "TAN-928374-SECRET"
	)
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	fixture.waitAccounts(t, "the loaded bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0
	})

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	pinResult := make(chan *banksync.PinEntry, 1)
	go func() {
		pinResult <- fixture.session.Bridge().Pin("Sparkasse", "user", true)
	}()
	prompt := fixture.waitPrompt(t, "pin")
	if err := fixture.banking.AnswerPin(prompt.ID, pin, true); err != nil {
		t.Fatalf("AnswerPin: %v", err)
	}
	if entry := <-pinResult; entry != nil {
		entry.Pin().Wipe()
	}
	tanResult := make(chan string, 1)
	go func() {
		value := fixture.session.Bridge().Tan(banksync.TanChallenge{Message: "TAN"})
		if value == nil {
			tanResult <- ""
			return
		}
		var text string
		_ = value.Use(func(bytes []byte) error {
			text = string(bytes)
			return nil
		})
		tanResult <- text
	}()
	tanPrompt := fixture.waitPrompt(t, "tan")
	if err := fixture.banking.AnswerTan(tanPrompt.ID, tan); err != nil {
		t.Fatalf("AnswerTan: %v", err)
	}
	<-tanResult

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	payloads := make([]any, 0)
	for _, dto := range fixture.bankingEvents {
		payloads = append(payloads, dto)
	}
	for _, dto := range fixture.promptEvents {
		payloads = append(payloads, dto)
	}
	for _, dto := range fixture.accountsEvents {
		payloads = append(payloads, dto)
	}
	for _, payload := range payloads {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if strings.Contains(string(encoded), pin) || strings.Contains(string(encoded), tan) {
			t.Fatalf("a DTO or event carried a secret: %s", encoded)
		}
	}
	if strings.Contains(logs.String(), pin) || strings.Contains(logs.String(), tan) {
		t.Fatalf("a log line carried a secret: %s", logs.String())
	}
}

func TestBankCardsCarryTheOnlineFieldsAndTheBankMenu(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, func(database *db.Database, id model.BankConnectionID) {
		day := civil.FromTime(testNow).AddDays(-30)
		if _, err := bankconnections.SetSyncFrom(context.Background(), database, id, &day); err != nil {
			t.Fatalf("set sync from: %v", err)
		}
		if err := settings.Set(context.Background(), database, banksync.PinFlagKey(id), "37040044|user"); err != nil {
			t.Fatalf("set pin flag: %v", err)
		}
	})
	dto := fixture.waitAccounts(t, "the bank card with online fields", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0 && dto.BankCards[0].BankID == int(fixture.bankID)
	})
	card := dto.BankCards[0]
	if card.Syncing || card.SyncEnabled {
		t.Fatalf("sync fields = syncing %v enabled %v", card.Syncing, card.SyncEnabled)
	}
	if card.SyncStep != i18n.Text(i18n.En, i18n.Syncing) {
		t.Fatalf("sync step = %q", card.SyncStep)
	}
	if card.SyncTooltip != i18n.Text(i18n.En, i18n.OnlineSyncNotYetAvailable) {
		t.Fatalf("sync tooltip = %q", card.SyncTooltip)
	}
	if card.PinText != i18n.Text(i18n.En, i18n.PinSavedForSyncs) {
		t.Fatalf("pin text = %q", card.PinText)
	}
	actions := make([]string, 0, len(card.Menu))
	for _, item := range card.Menu {
		actions = append(actions, item.Action)
	}
	want := []string{"history", "include", "rename", "connection", "clear-history", "forget-pin", "delete"}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Fatalf("menu actions = %v, want %v", actions, want)
	}
	history := card.Menu[0]
	if history.Enabled || history.Icon != "date_range" {
		t.Fatalf("history item = %+v", history)
	}
	include := card.Menu[1]
	if include.Enabled || !include.Checked {
		t.Fatalf("include item = %+v", include)
	}
	clear := card.Menu[4]
	if !clear.Enabled || clear.Icon != "history" {
		t.Fatalf("clear-history item = %+v", clear)
	}
	forget := card.Menu[5]
	if !forget.Enabled || forget.Icon != "lock" {
		t.Fatalf("forget-pin item = %+v", forget)
	}
}

func TestOfflineBankCardsKeepTheRenameAndDeleteMenu(t *testing.T) {
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seed := seedBindAccounts(t, appPaths)
	session, _, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	t.Cleanup(func() { _ = session.Shutdown() })
	accounts := NewAccounts(session, AccountsOptions{})
	router := NewRouter(session)
	router.Register("accounts", accounts)
	t.Cleanup(func() { _ = router.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	dto, err := accounts.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		dto, err = accounts.State()
		if err == nil && len(dto.BankCards) > 0 && dto.BankCards[0].BankID == int(seed.bank) {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the offline bank card never loaded: %+v", dto.BankCards)
		}
		time.Sleep(2 * time.Millisecond)
	}
	card := dto.BankCards[0]
	if card.PinText != "" {
		t.Fatalf("offline pin text = %q", card.PinText)
	}
	if len(card.Menu) != 2 || card.Menu[0].Action != "connection" || card.Menu[1].Action != "delete" {
		t.Fatalf("offline menu = %+v", card.Menu)
	}
	if card.Menu[0].Label != i18n.Text(i18n.En, i18n.RenameConnection) {
		t.Fatalf("offline connection label = %q", card.Menu[0].Label)
	}
}

func TestWithTheUnavailableGatewaySyncControlsAreHiddenAndAnAttemptFails(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	dto := fixture.waitAccounts(t, "the loaded bank card", func(dto AccountsDTO) bool {
		return len(dto.BankCards) > 0
	})
	if dto.BankCards[0].SyncEnabled || dto.BankCards[0].SyncTooltip == "" {
		t.Fatalf("sync controls = enabled %v tooltip %q", dto.BankCards[0].SyncEnabled, dto.BankCards[0].SyncTooltip)
	}

	bankingDTO, err := fixture.banking.StartSync(int(fixture.bankID))
	if err != nil {
		t.Fatalf("StartSync: %v", err)
	}
	bankingDTO = fixture.waitBanking(t, "the unavailable gateway error", func(dto BankingDTO) bool {
		return dto.ErrorText != ""
	})
	if bankingDTO.SyncControlsVisible || bankingDTO.OnlineSyncAvailable || bankingDTO.SyncAllEnabled {
		t.Fatalf("sync controls follow the flag: %+v", bankingDTO)
	}
	if bankingDTO.OnlineSyncNote != i18n.Text(i18n.En, i18n.OnlineSyncNotYetAvailable) {
		t.Fatalf("online sync note = %q", bankingDTO.OnlineSyncNote)
	}
	if !strings.Contains(bankingDTO.ErrorText, i18n.Text(i18n.En, i18n.ErrorOnlineSyncUnavailable)) {
		t.Fatalf("error = %q", bankingDTO.ErrorText)
	}
	accountsDTO, err := fixture.accounts.State()
	if err != nil {
		t.Fatalf("Accounts State: %v", err)
	}
	if accountsDTO.ErrorText != bankingDTO.ErrorText {
		t.Fatalf("accounts banner = %q, want the banking error %q", accountsDTO.ErrorText, bankingDTO.ErrorText)
	}
}

func TestBankingDTOMapsEveryApplyPropsValue(t *testing.T) {
	state := bankingcore.BankingState{
		Language: i18n.En,
		Error:    stringPointerForTest("error"),
		Message:  stringPointerForTest("message"),
		SyncAll: &bankingcore.SyncAllProgress{
			Position: 2,
			Total:    3,
			BankName: "Sparkasse",
		},
		Banks: []bankingcore.BankItem{{
			ID:               7,
			BankName:         "Sparkasse",
			IncludeInSyncAll: true,
		}},
		CanRememberPin: true,
	}
	dto := bankingDTO(state, i18n.En)
	if !dto.Busy || dto.Syncing {
		t.Fatalf("busy/syncing = %v/%v", dto.Busy, dto.Syncing)
	}
	wantProgress := i18n.Format(i18n.En, i18n.SyncAllProgress, i18n.Int(2), i18n.Int(3), i18n.Str("Sparkasse"))
	if dto.SyncAllText != wantProgress {
		t.Fatalf("sync all text = %q, want %q", dto.SyncAllText, wantProgress)
	}
	if !dto.SyncAllVisible || dto.SyncAllEnabled || dto.SyncControlsVisible {
		t.Fatalf(
			"sync all visible/enabled/controls = %v/%v/%v",
			dto.SyncAllVisible,
			dto.SyncAllEnabled,
			dto.SyncControlsVisible,
		)
	}
	if dto.OnlineSyncAvailable || dto.OnlineSyncNote != i18n.Text(i18n.En, i18n.OnlineSyncNotYetAvailable) {
		t.Fatalf("online note = %q", dto.OnlineSyncNote)
	}
	if dto.ErrorText != "error" || dto.MessageText != "message" || !dto.CanRememberPin {
		t.Fatalf("messages = %q / %q can remember %v", dto.ErrorText, dto.MessageText, dto.CanRememberPin)
	}
	if len(dto.Banks) != 1 || dto.Banks[0].BankID != 7 || dto.Banks[0].SyncEnabled {
		t.Fatalf("bank fields = %+v", dto.Banks)
	}

	offline := bankingcore.BankingState{
		Language: i18n.En,
		Banks:    []bankingcore.BankItem{{ID: 8, Offline: true}},
	}
	offlineDTO := bankingDTO(offline, i18n.En)
	if offlineDTO.OnlineSyncNote != "" || offlineDTO.SyncAllVisible {
		t.Fatalf("offline note/visible = %q/%v", offlineDTO.OnlineSyncNote, offlineDTO.SyncAllVisible)
	}
}

func TestPromptDTOMapsEveryPromptKind(t *testing.T) {
	method := bankingcore.BankingPrompt{
		ID:   11,
		Kind: bankingcore.ChooseTanMethod,
		TanMethods: []banksync.TanMethod{{
			ID:   "942",
			Name: "chipTAN optisch",
		}},
	}
	dto := promptDTO(&method, i18n.En, true)
	if dto.ID != 11 || dto.Kind != "choice" || dto.Title != i18n.Text(i18n.En, i18n.ChooseTanProcedure) {
		t.Fatalf("method prompt = %+v", dto)
	}
	if len(dto.Choices) != 1 || dto.Choices[0].ID != "942" || dto.Choices[0].Label != "chipTAN optisch" {
		t.Fatalf("method choices = %+v", dto.Choices)
	}

	medium := bankingcore.BankingPrompt{
		ID:         12,
		Kind:       bankingcore.ChooseTanMedium,
		TanMediums: []string{"Handy"},
	}
	mediumDTO := promptDTO(&medium, i18n.En, true)
	if mediumDTO.Kind != "choice" || mediumDTO.Title != i18n.Text(i18n.En, i18n.ChooseTanMedium) {
		t.Fatalf("medium prompt = %+v", mediumDTO)
	}

	decoupled := bankingcore.BankingPrompt{
		ID:      13,
		Kind:    bankingcore.Decoupled,
		Message: "Bitte bestätigen",
	}
	decoupledDTO := promptDTO(&decoupled, i18n.En, true)
	if decoupledDTO.Kind != "decoupled" ||
		decoupledDTO.Title != i18n.Text(i18n.En, i18n.ConfirmInBankingApp) ||
		decoupledDTO.Text != "Bitte bestätigen" {
		t.Fatalf("decoupled prompt = %+v", decoupledDTO)
	}

	ticket := promptDTO(nil, i18n.En, true)
	if ticket.Kind != "" || ticket.Choices == nil {
		t.Fatalf("empty prompt = %+v", ticket)
	}
}

func stringPointerForTest(value string) *string {
	return &value
}

func TestTanChallengeImagesBecomeDataURLsOrFallBackToText(t *testing.T) {
	var encoded bytes.Buffer
	imageValue := image.NewRGBA(image.Rect(0, 0, 16, 16))
	imageValue.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&encoded, imageValue); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	url := tanImageDataURL(encoded.Bytes())
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("image URL = %q", url)
	}
	if tanImageDataURL([]byte("not a png")) != "" {
		t.Fatal("garbage bytes produced an image")
	}
	if tanImageDataURL(bytes.Repeat([]byte{0}, 1_000_001)) != "" {
		t.Fatal("oversized bytes produced an image")
	}
}

func TestNoExportedBankingAPIReferencesTheFinTsGateway(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the test source")
	}
	bindDir := filepath.Dir(filename)
	for _, dir := range []string{filepath.Join(bindDir, "..", "app", "shell"), bindDir} {
		files, err := parsePackageSource(dir)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}
		for _, file := range files {
			for _, declaration := range file.Decls {
				checkDeclaration(t, dir, declaration)
			}
		}
	}
}

func parsePackageSource(dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	set := token.NewFileSet()
	files := make([]*ast.File, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, dir+"/"+name, nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func checkDeclaration(t *testing.T, dir string, declaration ast.Decl) {
	t.Helper()
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		if value.Name == nil || !ast.IsExported(value.Name.Name) {
			return
		}
		if funcTypeMentionsGateway(value.Type) {
			t.Fatalf("%s: exported function %s accepts or returns a FinTsGateway", dir, value.Name.Name)
		}
	case *ast.GenDecl:
		for _, spec := range value.Specs {
			switch specValue := spec.(type) {
			case *ast.TypeSpec:
				if !ast.IsExported(specValue.Name.Name) {
					continue
				}
				structType, ok := specValue.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						if ast.IsExported(name.Name) && typeMentionsGateway(field.Type) {
							t.Fatalf("%s: exported field %s.%s is a FinTsGateway", dir, specValue.Name.Name, name.Name)
						}
					}
				}
			}
		}
	}
}

func funcTypeMentionsGateway(function *ast.FuncType) bool {
	if function.Params != nil {
		for _, field := range function.Params.List {
			if typeMentionsGateway(field.Type) {
				return true
			}
		}
	}
	if function.Results != nil {
		for _, field := range function.Results.List {
			if typeMentionsGateway(field.Type) {
				return true
			}
		}
	}
	return false
}

func typeMentionsGateway(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel != nil && selector.Sel.Name == "FinTsGateway" {
			found = true
		}
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == "FinTsGateway" {
			found = true
		}
		return !found
	})
	return found
}

func bindBankEntry(blz string, name string, city string, bic string) bankdirectory.BankEntry {
	return bankdirectory.BankEntry{
		BLZ:        blz,
		Name:       name,
		PostalCode: "80331",
		City:       city,
		BIC:        bic,
	}
}

func bindHypoDirectory() *bankdirectory.BankDirectory {
	return bankdirectory.FromEntries([]bankdirectory.BankEntry{
		bindBankEntry("10020890", "UniCredit Bank - HypoVereinsbank", "Berlin", "HYVEDEMM488"),
		bindBankEntry("10010010", "Postbank", "Berlin", "PBNKDEFFXXX"),
	})
}

func TestPickingASuggestionMapsTheBLZAndKeepsTheNameAndBIC(t *testing.T) {
	directory := bindHypoDirectory()
	fields := pickedBankFields(
		directory,
		" UniCredit Bank - HypoVereinsbank ",
		"10020890",
		" HYVEDEMM488 ",
	)
	if fields.Name != "UniCredit Bank - HypoVereinsbank" {
		t.Fatalf("name = %q", fields.Name)
	}
	if fields.BLZ != "70020270" {
		t.Fatalf("blz = %q", fields.BLZ)
	}
	if fields.BIC != "HYVEDEMM488" {
		t.Fatalf("bic = %q", fields.BIC)
	}
	if fields.OriginalBLZ == nil || *fields.OriginalBLZ != "10020890" {
		t.Fatalf("original blz = %v", fields.OriginalBLZ)
	}
	if note := centralNote(i18n.En, fields); note != i18n.Format(
		i18n.En,
		i18n.CentralBlzNote,
		i18n.Str("70020270"),
		i18n.Str("10020890"),
	) {
		t.Fatalf("central note = %q", note)
	}
}

func TestPickingACentralBLZLeavesItUnchangedWithoutANote(t *testing.T) {
	directory := bankdirectory.FromEntries([]bankdirectory.BankEntry{
		bindBankEntry("70020270", "UniCredit Bank - HypoVereinsbank", "München", "HYVEDEMMXXX"),
	})
	fields := pickedBankFields(directory, " UniCredit Bank ", " 70020270 ", " HYVEDEMMXXX ")
	if fields.BLZ != "70020270" || fields.OriginalBLZ != nil {
		t.Fatalf("central pick = %+v", fields)
	}
	if note := centralNote(i18n.En, fields); note != "" {
		t.Fatalf("central pick note = %q", note)
	}
}

func TestTypingAnEightDigitBLZFillsOnlyEmptyFields(t *testing.T) {
	directory := bindHypoDirectory()
	empty := typedBankBLZ(directory, "", "10020890", "")
	if empty.Name != "UniCredit Bank - HypoVereinsbank" {
		t.Fatalf("empty name = %q", empty.Name)
	}
	if empty.BLZ != "70020270" {
		t.Fatalf("empty blz = %q", empty.BLZ)
	}
	if empty.BIC != "HYVEDEMM488" {
		t.Fatalf("empty bic = %q", empty.BIC)
	}
	if empty.OriginalBLZ == nil || *empty.OriginalBLZ != "10020890" {
		t.Fatalf("empty original blz = %v", empty.OriginalBLZ)
	}

	filled := typedBankBLZ(directory, "My bank", "10020890", "MYBIC")
	if filled.Name != "My bank" || filled.BIC != "MYBIC" || filled.BLZ != "70020270" {
		t.Fatalf("filled fields = %+v", filled)
	}

	other := typedBankBLZ(directory, "", "10010010", "")
	if other.BLZ != "10010010" || other.Name != "Postbank" || other.BIC != "PBNKDEFFXXX" {
		t.Fatalf("other bank = %+v", other)
	}
	if other.OriginalBLZ != nil {
		t.Fatalf("other original blz = %v", other.OriginalBLZ)
	}

	partial := typedBankBLZ(directory, "", "1002", "")
	if partial.BLZ != "1002" || partial.Name != "" || partial.OriginalBLZ != nil {
		t.Fatalf("partial blz = %+v", partial)
	}

	unknown := typedBankBLZ(directory, "", "99999999", "")
	if unknown.BLZ != "99999999" || unknown.Name != "" || unknown.BIC != "" || unknown.OriginalBLZ != nil {
		t.Fatalf("unknown blz = %+v", unknown)
	}
}

func TestSearchBanksReturnsTheBundledRankingCappedAtFifteenAndTheDetailFormat(t *testing.T) {
	service := NewBanking(nil, BankingOptions{})
	rows, err := service.SearchBanks("bank")
	if err != nil {
		t.Fatalf("SearchBanks: %v", err)
	}
	entries := bankdirectory.Bundled().Search("bank", 15)
	if len(rows) != len(entries) || len(rows) > 15 {
		t.Fatalf("SearchBanks returned %d rows for %d entries", len(rows), len(entries))
	}
	for index, row := range rows {
		if row.BLZ != entries[index].BLZ {
			t.Fatalf("row %d blz = %q, want %q", index, row.BLZ, entries[index].BLZ)
		}
		if row.Name != entries[index].Name {
			t.Fatalf("row %d name = %q, want %q", index, row.Name, entries[index].Name)
		}
	}
	if len(rows) > 0 {
		entry := entries[0]
		want := strings.Join([]string{entry.BLZ, entry.City, entry.BIC}, " · ")
		if rows[0].Detail != want {
			t.Fatalf("detail = %q, want %q", rows[0].Detail, want)
		}
	}

	empty, err := service.SearchBanks("   ")
	if err != nil {
		t.Fatalf("SearchBanks empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty query returned %d rows", len(empty))
	}
}

func TestPickAndTypedBanksUseTheBundledDirectoryAndTheCentralNote(t *testing.T) {
	service := NewBanking(nil, BankingOptions{})
	picked, err := service.PickBankSuggestion("UniCredit Bank - HypoVereinsbank", "10020890", "HYVEDEMM488")
	if err != nil {
		t.Fatalf("PickBankSuggestion: %v", err)
	}
	if picked.BLZ != "70020270" || picked.BIC != "HYVEDEMM488" {
		t.Fatalf("picked fields = %+v", picked)
	}
	if picked.CentralNote == "" || !strings.Contains(picked.CentralNote, "10020890") {
		t.Fatalf("picked note = %q", picked.CentralNote)
	}

	central, err := service.PickBankSuggestion("UniCredit Bank - HypoVereinsbank", "70020270", "HYVEDEMMXXX")
	if err != nil {
		t.Fatalf("PickBankSuggestion central: %v", err)
	}
	if central.BLZ != "70020270" || central.CentralNote != "" {
		t.Fatalf("central fields = %+v", central)
	}

	typed, err := service.BankBLZChanged("", "10020890", "")
	if err != nil {
		t.Fatalf("BankBLZChanged: %v", err)
	}
	if typed.Name != "UniCredit Bank - HypoVereinsbank" || typed.BLZ != "70020270" ||
		typed.BIC != "HYVEDEMM488" || typed.CentralNote == "" {
		t.Fatalf("typed fields = %+v", typed)
	}

	untouched, err := service.BankBLZChanged("My bank", "1002", "MYBIC")
	if err != nil {
		t.Fatalf("BankBLZChanged partial: %v", err)
	}
	if untouched.Name != "My bank" || untouched.BLZ != "1002" || untouched.BIC != "MYBIC" ||
		untouched.CentralNote != "" {
		t.Fatalf("partial fields = %+v", untouched)
	}
}

func TestCentralNoteIsLocalized(t *testing.T) {
	fields := addBankFields{
		BLZ:         "70020270",
		OriginalBLZ: stringPointerForTest("10020890"),
	}
	for _, language := range []i18n.Language{i18n.En, i18n.De, i18n.Fr} {
		want := i18n.Format(
			language,
			i18n.CentralBlzNote,
			i18n.Str("70020270"),
			i18n.Str("10020890"),
		)
		if got := centralNote(language, fields); got != want {
			t.Fatalf("central note (%v) = %q, want %q", language, got, want)
		}
	}
}
