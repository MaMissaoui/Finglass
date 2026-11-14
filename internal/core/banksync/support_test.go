package banksync

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	txrepo "github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

type fakeGateway struct {
	mu             sync.Mutex
	snapshots      []AccountSnapshot
	failWith       *GatewayError
	pinCalls       atomic.Int64
	sinceRequests  map[string]*civil.Date
	capabilities   *BankCapabilities
	requestTan     bool
	systemID       *string
	hktanVersion   uint16
	lastConnection FinTsConnection
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{sinceRequests: make(map[string]*civil.Date)}
}

func (f *fakeGateway) setSnapshots(snapshots []AccountSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append([]AccountSnapshot(nil), snapshots...)
}

func (f *fakeGateway) setFailure(failure *GatewayError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if failure == nil {
		f.failWith = nil
		return
	}
	value := *failure
	f.failWith = &value
}

func (f *fakeGateway) setPinCalls(calls int) {
	f.pinCalls.Store(int64(calls))
}

func (f *fakeGateway) setCapabilities(capabilities *BankCapabilities) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if capabilities == nil {
		f.capabilities = nil
		return
	}
	value := *capabilities
	f.capabilities = &value
}

func (f *fakeGateway) setRequestTan(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestTan = enabled
}

func (f *fakeGateway) setSystemID(systemID string, version uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := systemID
	f.systemID = &value
	f.hktanVersion = version
}

func (f *fakeGateway) connectionSeen() FinTsConnection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastConnection
}

func (f *fakeGateway) since(iban string) *civil.Date {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sinceRequests[iban]
}

func (f *fakeGateway) sinceRequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sinceRequests)
}

func (f *fakeGateway) FetchAll(
	_ context.Context,
	connection FinTsConnection,
	interaction BankingInteraction,
	since func(accountNumber string) *civil.Date,
	_ civil.Date,
) (FetchOutcome, error) {
	f.mu.Lock()
	f.lastConnection = connection
	f.mu.Unlock()
	calls := f.pinCalls.Load()
	if calls < 1 {
		calls = 1
	}
	for index := int64(0); index < calls; index++ {
		if interaction.Pin(connection.BankName, connection.UserID, true) == nil {
			return FetchOutcome{}, NewGatewayCancelledError()
		}
	}

	f.mu.Lock()
	requestTan := f.requestTan
	f.mu.Unlock()
	if requestTan {
		_ = interaction.Tan(TanChallenge{Message: "Bitte TAN"})
	}

	f.mu.Lock()
	failure := f.failWith
	snapshots := append([]AccountSnapshot(nil), f.snapshots...)
	capabilities := f.capabilities
	systemID := f.systemID
	hktanVersion := f.hktanVersion
	f.mu.Unlock()

	for _, snapshot := range snapshots {
		value := since(snapshot.Account.IBAN)
		f.mu.Lock()
		f.sinceRequests[snapshot.Account.IBAN] = value
		f.mu.Unlock()
	}
	tanMethod := "921"
	outcome := FetchOutcome{
		Snapshots:    snapshots,
		TanMethod:    &tanMethod,
		Capabilities: capabilities,
		SystemID:     systemID,
		HKTANVersion: hktanVersion,
	}
	if failure != nil {
		return outcome, *failure
	}
	return outcome, nil
}

type scriptedInteraction struct {
	pinValue        *secret.Text
	remember        bool
	pinAsked        atomic.Int64
	tanAsked        atomic.Int64
	mu              sync.Mutex
	offeredRemember []bool
	pinBankNames    []string
	tanValue        *secret.Text
}

func newScriptedInteraction(pin *string, remember bool) *scriptedInteraction {
	var value *secret.Text
	if pin != nil {
		value = secret.NewText([]byte(*pin))
	}
	return &scriptedInteraction{
		pinValue: value,
		remember: remember,
		tanValue: secret.NewText([]byte("999999")),
	}
}

func (s *scriptedInteraction) setTan(value string) {
	s.tanValue = secret.NewText([]byte(value))
}

func (s *scriptedInteraction) pinAskedCount() int {
	return int(s.pinAsked.Load())
}

func (s *scriptedInteraction) offeredRememberValues() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.offeredRemember...)
}

func (s *scriptedInteraction) pinBankNameValues() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pinBankNames...)
}

func (s *scriptedInteraction) Pin(bankName, _ string, offerRemember bool) *PinEntry {
	s.pinAsked.Add(1)
	s.mu.Lock()
	s.offeredRemember = append(s.offeredRemember, offerRemember)
	s.pinBankNames = append(s.pinBankNames, bankName)
	s.mu.Unlock()
	if s.pinValue == nil {
		return nil
	}
	entry := NewPinEntry(s.pinValue, s.remember)
	return &entry
}

func (s *scriptedInteraction) SelectTanMethod(options []TanMethod) *string {
	if len(options) == 0 {
		return nil
	}
	value := options[0].ID
	return &value
}

func (s *scriptedInteraction) SelectTanMedium(options []string) *string {
	if len(options) == 0 {
		return nil
	}
	value := options[len(options)-1]
	return &value
}

func (s *scriptedInteraction) Tan(_ TanChallenge) *secret.Text {
	s.tanAsked.Add(1)
	return s.tanValue
}

func (s *scriptedInteraction) ConfirmDecoupled(_ string) bool {
	return true
}

func (s *scriptedInteraction) Info(_ string) {}

type harness struct {
	db      *db.Database
	dataDir string
	gateway *fakeGateway
	store   *MemoryCredentialStore
	service *BankingService
	bank    model.BankConnectionID
	today   civil.Date
}

func newHarness(t *testing.T) *harness {
	return newHarnessWithToday(t, mustDate(t, 2026, time.October, 4))
}

func newHarnessWithToday(t *testing.T, today civil.Date) *harness {
	t.Helper()
	database := testDatabase(t)
	dataDir := filepath.Join(t.TempDir(), "profile")
	gateway := newFakeGateway()
	store := NewMemoryCredentialStore()
	service := NewBankingService(NewFinTsClient(gateway, dataDir, "TEST-PRODUCT", store))
	bank := createBank(t, database, "Sparkasse KölnBonn", "37050198", "demo-user", true)
	return &harness{
		db:      database,
		dataDir: dataDir,
		gateway: gateway,
		store:   store,
		service: service,
		bank:    bank,
		today:   today,
	}
}

func (h *harness) sync(t *testing.T, interaction BankingInteraction) (SyncReport, error) {
	t.Helper()
	return h.service.SyncBankConnection(context.Background(), h.db, h.bank, interaction, h.today)
}

func (h *harness) accounts(t *testing.T) []model.Account {
	t.Helper()
	loaded, err := accounts.List(context.Background(), h.db)
	if err != nil {
		t.Fatalf("accounts.List: %v", err)
	}
	return loaded
}

func (h *harness) assertAuditHasNo(t *testing.T, forbidden ...string) {
	t.Helper()
	for _, entry := range auditEntries(t, h.db, 10_000) {
		haystack := entry.At + " " + entry.Action + " " + entry.Details
		for _, value := range forbidden {
			if containsText(haystack, value) {
				t.Fatalf("audit entry leaked %q: %s", value, haystack)
			}
		}
	}
}

func createBank(
	t *testing.T,
	database *db.Database,
	name,
	blz,
	userID string,
	withServer bool,
) model.BankConnectionID {
	t.Helper()
	bic := "COLSDE33XXX"
	bank, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: name,
		BLZ:      blz,
		BIC:      &bic,
		UserID:   userID,
	})
	if err != nil {
		t.Fatalf("bankconnections.Create: %v", err)
	}
	if withServer {
		serverURL := "https://fints.example/pintan"
		if _, err := bankconnections.UpdateConnection(
			context.Background(),
			database,
			bank.ID,
			model.BankConnectionUpdate{ServerURL: &serverURL},
		); err != nil {
			t.Fatalf("bankconnections.UpdateConnection: %v", err)
		}
	}
	return bank.ID
}

func setLastSync(t *testing.T, database *db.Database, bank model.BankConnectionID, at time.Time) {
	t.Helper()
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"UPDATE bank_connections SET last_sync = ?1 WHERE id = ?2",
			at.UTC().Format(time.RFC3339Nano),
			int64(bank),
		)
		return err
	})
	if err != nil {
		t.Fatalf("set last sync: %v", err)
	}
}

func mustDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

func mustDecimal(text string) decimal.Decimal {
	value, err := decimal.Parse(text)
	if err != nil {
		panic(err)
	}
	return value
}

func decimalPointer(text *string) *decimal.Decimal {
	if text == nil {
		return nil
	}
	value := mustDecimal(*text)
	return &value
}

func account(iban string) FinTsAccount {
	characters := []rune(iban)
	start := len(characters) - 10
	if start < 0 {
		start = 0
	}
	return FinTsAccount{
		IBAN:             iban,
		BIC:              "COLSDE33XXX",
		AccountNumber:    string(characters[start:]),
		SubAccountNumber: nil,
		BankName:         "Sparkasse",
		BLZ:              "37050198",
		AccountType:      "checking",
		Currency:         "EUR",
		ProductName:      nil,
		CurrencyStated:   true,
	}
}

func booking(
	iban string,
	day civil.Date,
	amount string,
	partner *string,
	purpose *string,
	balanceAfter *string,
) FinTsTransaction {
	return FinTsTransaction{
		Account:         account(iban),
		Date:            day,
		ValueDate:       day,
		Amount:          mustDecimal(amount),
		Currency:        "EUR",
		PartnerName:     partner,
		PartnerIBAN:     nil,
		PartnerBIC:      nil,
		Purpose:         purpose,
		TransactionCode: nil,
		Reference:       nil,
		PrimaNota:       nil,
		BalanceAfter:    decimalPointer(balanceAfter),
		BankID:          nil,
	}
}

func snapshot(iban, balance string, transactions []FinTsTransaction) AccountSnapshot {
	bankAccount := account(iban)
	return AccountSnapshot{
		Balance: &FinTsBalance{
			Account:          bankAccount,
			Balance:          mustDecimal(balance),
			AvailableBalance: nil,
			Currency:         "EUR",
			Date:             time.Now().UTC(),
		},
		Account:        bankAccount,
		Transactions:   transactions,
		StatementError: nil,
		BalanceCode:    nil,
		Pending:        nil,
	}
}

func foreignSnapshot(iban, balanceCurrency string) AccountSnapshot {
	bankAccount := account(iban)
	bankAccount.Currency = "CHF"
	bankAccount.CurrencyStated = false
	return AccountSnapshot{
		Account: bankAccount,
		Balance: &FinTsBalance{
			Account:          bankAccount,
			Balance:          mustDecimal("88.50"),
			AvailableBalance: nil,
			Currency:         balanceCurrency,
			Date:             time.Now().UTC(),
		},
		Transactions:   nil,
		StatementError: nil,
		BalanceCode:    nil,
		Pending:        nil,
	}
}

func (s AccountSnapshot) withAccount(account FinTsAccount) AccountSnapshot {
	if s.Balance != nil {
		s.Balance.Account = account
	}
	s.Account = account
	return s
}

func category(t *testing.T, database *db.Database, name string) model.CategoryID {
	t.Helper()
	id, err := categories.Create(context.Background(), database, model.NewCategory{
		Name:          name,
		Color:         "#3BC7B0",
		BudgetLimit:   nil,
		IsTaxRelevant: false,
		IsTransfer:    false,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func rule(t *testing.T, database *db.Database, pattern string, categoryID model.CategoryID) {
	t.Helper()
	if _, err := rulesrepo.Create(context.Background(), database, model.NewRule{
		Pattern:     pattern,
		CategoryID:  categoryID,
		Priority:    0,
		AutoApply:   true,
		MatchField:  model.RuleFieldAny,
		Direction:   model.RuleDirectionAny,
		TaxRelevant: nil,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
}

func transactionsForAccount(t *testing.T, database *db.Database, accountID model.AccountID) []model.Transaction {
	t.Helper()
	items, err := txrepo.ListForAccount(context.Background(), database, accountID, nil, nil)
	if err != nil {
		t.Fatalf("transactions.ListForAccount: %v", err)
	}
	return items
}

func manualAccount(t *testing.T, database *db.Database, iban string) model.AccountID {
	t.Helper()
	name := "Account"
	id, err := accounts.Create(context.Background(), database, model.NewAccount{
		BankConnectionID: nil,
		Institution:      nil,
		Name:             &name,
		IBAN:             &iban,
		AccountNumber:    nil,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Minor: 0, Currency: money.EUR},
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func bankAccount(
	t *testing.T,
	database *db.Database,
	bank model.BankConnectionID,
	iban string,
	balance money.Money,
	accountType model.AccountType,
) model.AccountID {
	t.Helper()
	institution := "Sparkasse"
	name := "Account"
	id, err := accounts.Create(context.Background(), database, model.NewAccount{
		BankConnectionID: &bank,
		Institution:      &institution,
		Name:             &name,
		IBAN:             &iban,
		AccountNumber:    nil,
		Currency:         balance.Currency,
		AccountType:      accountType,
		Balance:          balance,
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func csvStatement(t *testing.T, text string, today civil.Date) imports.ParsedStatement {
	t.Helper()
	statement, err := imports.Read("statement.csv", []byte(text), nil, today)
	if err != nil {
		t.Fatalf("imports.Read: %v", err)
	}
	return statement
}

func importStatement(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	statement imports.ParsedStatement,
	today civil.Date,
) {
	t.Helper()
	if _, err := imports.Import(context.Background(), database, accountID, statement, false, today); err != nil {
		t.Fatalf("imports.Import: %v", err)
	}
}

func fintsLine(
	iban string,
	day civil.Date,
	amount,
	partner,
	purpose,
	balance string,
) FinTsTransaction {
	return booking(iban, day, amount, &partner, &purpose, &balance)
}

func storePin(t *testing.T, h *harness) string {
	t.Helper()
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), true)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	key := PinKey(h.dataDir, int64(h.bank))
	if stored := mustReadStore(t, h.store, key); stored != "12345" {
		t.Fatalf("stored PIN = %q, want %q", stored, "12345")
	}
	return key
}

func mustReadStore(t *testing.T, store *MemoryCredentialStore, key string) string {
	t.Helper()
	value, found, err := store.Read(key)
	if err != nil {
		t.Fatalf("credential store read: %v", err)
	}
	if !found {
		t.Fatalf("credential store has no entry for %q", key)
	}
	return string(value)
}

func auditEntries(t *testing.T, database *db.Database, limit uint32) []audit.Entry {
	t.Helper()
	entries, err := audit.Recent(context.Background(), database, limit)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return entries
}

func containsText(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func gatewayFailure(gatewayError GatewayError) *GatewayError {
	return &gatewayError
}

func assertInt(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func assertString(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func assertDate(t *testing.T, got *civil.Date, want civil.Date) {
	t.Helper()
	if got == nil {
		t.Fatalf("date = nil, want %s", want)
	}
	if *got != want {
		t.Fatalf("date = %s, want %s", *got, want)
	}
}

func findAccount(t *testing.T, accounts []model.Account, iban string) model.Account {
	t.Helper()
	for _, account := range accounts {
		if account.IBAN != nil && *account.IBAN == iban {
			return account
		}
	}
	t.Fatalf("account %s not found in %v", iban, accounts)
	return model.Account{}
}

func loadBankItem(t *testing.T, database *db.Database, bank model.BankConnectionID) model.BankAccessItem {
	t.Helper()
	item, ok, err := bankconnections.GetItem(context.Background(), database, bank)
	if err != nil {
		t.Fatalf("bankconnections.GetItem: %v", err)
	}
	if !ok {
		t.Fatalf("bank %d not found", bank)
	}
	return item
}

func auditCount(t *testing.T, database *db.Database, action string) int {
	t.Helper()
	count := 0
	for _, entry := range auditEntries(t, database, 1000) {
		if entry.Action == action {
			count++
		}
	}
	return count
}

func auditHasAction(t *testing.T, database *db.Database, action string) bool {
	t.Helper()
	return auditCount(t, database, action) > 0
}

func bankingCode(err error) string {
	var bankingError BankingError
	if errors.As(err, &bankingError) {
		return bankingError.Code
	}
	return ""
}

func auditEntriesWithAction(t *testing.T, database *db.Database, action string) []audit.Entry {
	t.Helper()
	entries := make([]audit.Entry, 0)
	for _, entry := range auditEntries(t, database, 1000) {
		if entry.Action == action {
			entries = append(entries, entry)
		}
	}
	return entries
}

func decimalText(value int64) string {
	return strconv.FormatInt(value, 10)
}
