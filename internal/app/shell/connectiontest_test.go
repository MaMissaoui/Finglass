package shell

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	connectionTestBLZ    = "37040044"
	connectionTestUser   = "user"
	connectionTestPIN    = "invented-pin-123"
	connectionTestTAN    = "invented-tan-456"
	connectionTestID     = "TESTPRODUCT0000000000000"
	connectionTestIBAN   = "DE89370400440532013000"
	connectionTestOwner  = "Invented Owner"
	connectionTestBooked = "Invented booking"
	connectionTestText   = "Invented bank text"
	connectionTestChal   = "Invented challenge"
	connectionTestDialog = "dialog-1"
)

type connectionTestReply struct {
	body string
	err  error
}

type connectionTestTransport struct {
	t        *testing.T
	replies  []connectionTestReply
	records  [][]string
	sent     [][]byte
	requests int
}

func (f *connectionTestTransport) reply(body string) {
	f.replies = append(f.replies, connectionTestReply{body: body})
}

func (f *connectionTestTransport) Exchange(_ context.Context, request []byte) ([]byte, error) {
	f.t.Helper()
	types, err := connectionRequestTypes(request)
	if err != nil {
		f.t.Fatalf("inspect request: %v", err)
	}
	f.sent = append(f.sent, append([]byte(nil), request...))
	f.records = append(f.records, types)
	call := len(f.records) - 1
	f.requests++
	if call >= len(f.replies) {
		f.t.Fatalf("exchange %d has no scripted reply: %v", call, types)
	}
	reply := f.replies[call]
	if reply.err != nil {
		return nil, reply.err
	}
	return []byte(reply.body), nil
}

func connectionRequestTypes(request []byte) ([]string, error) {
	outer, err := fints.ParseMessage(request)
	if err != nil {
		return nil, err
	}
	types := make([]string, 0, len(outer))
	for _, segment := range outer {
		if segment.Type() != "HNVSD" {
			types = append(types, segment.Type())
			continue
		}
		inner, err := fints.ParseMessage(segment.DEG(1).Binary(0))
		if err != nil {
			return nil, err
		}
		for _, segment := range inner {
			types = append(types, segment.Type())
		}
	}
	return types, nil
}

func connectionResponse(dialogID string, segments ...string) string {
	return "HNHBK:1:3+000000000000+300+" + dialogID + "+1'" +
		strings.Join(segments, "") +
		"HNHBS:99:1+1'"
}

func connectionSuccess(dialogID string) string {
	return connectionResponse(dialogID, "HIRMG:2:2+0010::"+connectionTestText+"'")
}

func connectionExecuted(dialogID string, payload ...string) string {
	segments := []string{"HIRMG:2:2+0020::" + connectionTestText + "'"}
	segments = append(segments, payload...)
	return connectionResponse(dialogID, segments...)
}

func connectionSync(extra ...string) string {
	segments := []string{
		"HISYN:2:4:1+test-system'",
		"HIBPA:3:3+42+280+0+1+1+0'",
		"HIUPA:4:4+test-customer+7+55'",
	}
	segments = append(segments, extra...)
	segments = append(segments, "HIRMG:90:2+0010::"+connectionTestText+"'")
	return connectionResponse("dialog-sync", segments...)
}

func connectionChipHitans(name string, firstWait, nextWait int) string {
	return fmt.Sprintf(
		"HITANS:5:7:4+1+1+1+N:N:0"+
			":942:2:TECH-CHIP:DKID-CHIP:1.0:%s:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1::%d:%d:J:J'",
		name, firstWait, nextWait,
	)
}

func connectionAllowed() string {
	return "HIRMS:6:2:4+3920::Erlaubt:942'"
}

func connectionHipins(operations string) string {
	return "HIPINS:7:1:4+1+1+0+" + operations + "'"
}

func connectionHisals(version int) string {
	return fmt.Sprintf("HISALS:8:%d:4+1+1+1+1'", version)
}

func connectionHikazs(version int) string {
	return fmt.Sprintf("HIKAZS:8:%d:4+1+1+1+1'", version)
}

func connectionHiupd(iban, accountNumber, currency string) string {
	return "HIUPD:7:6:4+" + accountNumber + "::280:" + connectionTestBLZ + "+" + iban + "+" + connectionTestUser +
		"+G+" + currency + "+" + connectionTestOwner + "++Girokonto+HKSAL:1+HKKAZ:1+TESTDEFFXXX'"
}

func connectionHispaAccount(iban, bic, accountNumber string) string {
	return "J:" + iban + ":" + bic + ":" + accountNumber + "::280:" + connectionTestBLZ
}

func connectionHispa(accounts ...string) string {
	return "HISPA:5:4:3+" + strings.Join(accounts, "+") + "'"
}

func connectionHisalPayload() string {
	return "HISAL:5:7:4+1::280:" + connectionTestBLZ +
		"+Girokonto+EUR+C:98765,43:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"
}

func connectionHikazPayload(booked, pending []byte) string {
	return "HIKAZ:5:7:4+@" + fmt.Sprint(len(booked)) + "@" + string(booked) +
		"+@" + fmt.Sprint(len(pending)) + "@" + string(pending) + "'"
}

func connectionMT940() []byte {
	return []byte(":20:STARTUMS\n" +
		":25:37040044/0000001\n" +
		":28C:1/1\n" +
		":60F:C260301EUR1000,00\n" +
		":61:2603010301C123,45NTRFNONREF//REF1\n" +
		":86:" + connectionTestBooked + "\n" +
		":62F:C260331EUR1123,45\n")
}

func connectionTanRequired(dialogID, taskReference, challenge string) string {
	return connectionResponse(
		dialogID,
		"HIRMS:3:2:4+0030::TAN erforderlich'",
		"HITAN:4:7:3+4+task-hash+"+taskReference+"+"+challenge+"+@0@'",
	)
}

func typedTANConnectionBank(t *testing.T) *connectionTestTransport {
	t.Helper()
	bank := &connectionTestTransport{t: t}
	bank.reply(connectionSync(
		connectionChipHitans("chipTAN manuell", 5, 2),
		connectionAllowed(),
		connectionHipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		connectionHisals(7),
		connectionHikazs(6),
		connectionHiupd(connectionTestIBAN, "00000001", "EUR"),
	))
	bank.reply(connectionSuccess("dialog-sync"))
	bank.reply(connectionTanRequired(connectionTestDialog, "task-3", connectionTestChal))
	bank.reply(connectionExecuted(connectionTestDialog))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHispa(
		connectionHispaAccount(connectionTestIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHisalPayload()))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHikazPayload(connectionMT940(), nil)))
	bank.reply(connectionSuccess(connectionTestDialog))
	return bank
}

func waitConnectionTestDone(t *testing.T, session *Session) banking.ConnectionTestResult {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := session.BankingState()
		if state.ConnectionTest != nil && !state.ConnectionTest.Running {
			return *state.ConnectionTest
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("connection test did not finish: %+v", state.ConnectionTest)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func answerProductID(t *testing.T, session *Session, value []byte) {
	t.Helper()
	prompt := waitForBankingPrompt(t, session, banking.ProductID)
	session.Bridge().AnswerProductID(prompt.ID, value)
}

func runTypedTANConnectionTest(t *testing.T, session *Session, bank *connectionTestTransport) banking.ConnectionTestResult {
	t.Helper()
	session.connectionTestFactory = func(string) (fints.Transport, error) { return bank, nil }
	session.BankingTestConnection(int(testBankID(t, session)))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	return waitConnectionTestDone(t, session)
}

func testBankID(t *testing.T, session *Session) int64 {
	t.Helper()
	state := session.BankingState()
	if len(state.Banks) == 0 {
		t.Fatal("no bank loaded")
	}
	return int64(state.Banks[0].ID)
}

func tableFingerprint(t *testing.T, database *db.Database, table string) (int, string) {
	t.Helper()
	count := 0
	hash := fnv.New64a()
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		// #nosec G202 -- table is one of the fixed names in bankingFingerprint.
		rows, err := conn.QueryContext(ctx, "SELECT * FROM "+table)
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
			count++
			_, _ = fmt.Fprint(hash, values...)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", table, err)
	}
	return count, fmt.Sprintf("%016x", hash.Sum64())
}

func bankingFingerprint(t *testing.T, database *db.Database) map[string]string {
	t.Helper()
	fingerprint := make(map[string]string)
	for _, table := range []string{"bank_connections", "accounts", "transactions", "settings", "audit_log"} {
		count, checksum := tableFingerprint(t, database, table)
		fingerprint[table] = fmt.Sprintf("%d/%s", count, checksum)
	}
	return fingerprint
}

func TestConnectionTestRunsThroughTheBridgeAndChangesNothing(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, _ := newBankingSession(t, store, nil)
	database := sessionDatabase(t, session)
	if err := settings.Delete(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil {
		t.Fatalf("clear unlock: %v", err)
	}
	before := bankingFingerprint(t, database)

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return typedTANConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))
	state := session.BankingState()
	if state.ConnectionTest == nil || !state.ConnectionTest.Running {
		t.Fatalf("running result = %+v", state.ConnectionTest)
	}
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	result := waitConnectionTestDone(t, session)

	if !result.Go || result.Reason != i18n.Text(i18n.En, i18n.TestConnectionGo) {
		t.Fatalf("result = %+v", result)
	}
	state = session.BankingState()
	if result.Probed != 1 || result.Skipped != 0 ||
		state.LastTrace == nil || state.LastTrace.Kind != traceKindTest ||
		state.LastTrace.BankID != bankID || !state.LastTrace.HasTrace {
		t.Fatalf("counts/trace = %+v %+v", result, state.LastTrace)
	}
	if !store.IsEmpty() {
		t.Fatalf("credential store has %d entries after a test", store.Len())
	}
	after := bankingFingerprint(t, database)
	assertOnlyTheSyncUnlockChanged(t, before, after, database, bankID)
}

func retryConnectionBank(t *testing.T) *connectionTestTransport {
	t.Helper()
	bank := &connectionTestTransport{t: t}
	bank.reply(connectionSync(
		connectionChipHitans("chipTAN manuell", 5, 2),
		connectionAllowed(),
		connectionHipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		connectionHisals(7),
		connectionHikazs(6),
		connectionHiupd(connectionTestIBAN, "00000001", "EUR"),
	))
	bank.reply(connectionSuccess("dialog-sync"))
	bank.reply(connectionResponse(
		connectionTestDialog,
		"HIRMG:2:2+9050::"+connectionTestText+"'",
		"HIRMS:3:2:4+9210::"+connectionTestText+"'",
	))
	bank.reply(connectionResponse(
		connectionTestDialog,
		"HIRMG:2:2+0020::"+connectionTestText+"'",
		"HIRMS:3:2:4+3076::"+connectionTestText+"'",
	))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHispa(
		connectionHispaAccount(connectionTestIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHisalPayload()))
	bank.reply(connectionExecuted(connectionTestDialog, connectionHikazPayload(connectionMT940(), nil)))
	bank.reply(connectionSuccess(connectionTestDialog))
	return bank
}

func TestConnectionTestRetriesAfterApprovalAndChangesNothing(t *testing.T) {
	enableApprovalRetry(t)
	store := banksync.NewMemoryCredentialStore()
	session, bankID, _ := newBankingSession(t, store, nil)
	database := sessionDatabase(t, session)
	if err := settings.Delete(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil {
		t.Fatalf("clear unlock: %v", err)
	}
	before := bankingFingerprint(t, database)

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return retryConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)

	prompt := waitForBankingPrompt(t, session, banking.Decoupled)
	wantPrompt := i18n.Text(i18n.En, i18n.TestConnectionApproveThenRetry)
	if prompt.Message != wantPrompt {
		t.Fatalf("approval prompt = %q, want %q", prompt.Message, wantPrompt)
	}
	session.Bridge().AnswerDecoupled(prompt.ID, true)

	result := waitConnectionTestDone(t, session)
	if !result.Go || result.Reason != i18n.Text(i18n.En, i18n.TestConnectionGo) {
		t.Fatalf("result = %+v", result)
	}
	if !store.IsEmpty() {
		t.Fatalf("credential store has %d entries after a test", store.Len())
	}
	after := bankingFingerprint(t, database)
	assertOnlyTheSyncUnlockChanged(t, before, after, database, bankID)
}

func assertOnlyTheSyncUnlockChanged(
	t *testing.T,
	before,
	after map[string]string,
	database *db.Database,
	bankID model.BankConnectionID,
) {
	t.Helper()
	for _, table := range []string{"bank_connections", "accounts", "transactions"} {
		if after[table] != before[table] {
			t.Fatalf("%s changed: %s -> %s", table, before[table], after[table])
		}
	}
	if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || !ok {
		t.Fatalf("the GO test did not store an unlock: ok=%t err=%v", ok, err)
	}
	entries, err := audit.Recent(context.Background(), database, 1000)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == banksync.CodeSyncUnlocked {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the GO test wrote no unlock audit entry")
	}
}

func TestConnectionTestIsRefusedWhileSyncing(t *testing.T) {
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	session.mu.Lock()
	id := bankID
	session.banking.SyncingBank = &id
	session.mu.Unlock()

	factoryCalls := 0
	session.connectionTestFactory = func(string) (fints.Transport, error) {
		factoryCalls++
		return typedTANConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))

	state := session.BankingState()
	if state.Error == nil || *state.Error != i18n.Text(i18n.En, i18n.ErrorConnectionBusy) {
		t.Fatalf("error = %v", state.Error)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls = %d, want 0", factoryCalls)
	}
}

func TestLastTraceClearsOnANewTestAndOnClose(t *testing.T) {
	session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	runTypedTANConnectionTest(t, session, typedTANConnectionBank(t))
	if state := session.BankingState(); state.LastTrace == nil || !state.LastTrace.HasTrace ||
		state.LastTrace.Kind != traceKindTest || state.LastTrace.BankID != bankID {
		t.Fatalf("last trace = %+v", state.LastTrace)
	}

	// A new test clears the previous trace at once.
	cancelled := &connectionTestTransport{t: t}
	session.connectionTestFactory = func(string) (fints.Transport, error) { return cancelled, nil }
	session.BankingTestConnection(int(bankID))
	session.mu.Lock()
	trace := append([]byte(nil), session.lastTrace.trace...)
	session.mu.Unlock()
	if len(trace) != 0 {
		t.Fatal("the old trace survived a new test")
	}
	answerProductID(t, session, nil)
	waitConnectionTestDone(t, session)
	state := session.BankingState()
	if state.LastTrace == nil || state.LastTrace.HasTrace {
		t.Fatalf("cancelled test reports a trace: %+v", state.LastTrace)
	}
	if state.LastTrace.Kind != traceKindTest {
		t.Fatalf("cancelled test trace kind = %q", state.LastTrace.Kind)
	}
	if session.lastTraceBLZForTest() != "" {
		t.Fatal("cancelled test kept a BLZ")
	}

	// Close the profile and start another test trace.
	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return typedTANConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	waitConnectionTestDone(t, session)
	session.Teardown(startup.Startup{})
	session.mu.Lock()
	trace = append([]byte(nil), session.lastTrace.trace...)
	session.mu.Unlock()
	if len(trace) != 0 {
		t.Fatal("the trace survived the profile close")
	}
}

func sessionLastTraceBLZForTest(session *Session) string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.lastTrace.blz
}

func (s *Session) lastTraceBLZForTest() string {
	return sessionLastTraceBLZForTest(s)
}

func TestSaveConnectionTraceWritesMode0600AndCancelWritesNothing(t *testing.T) {
	session, _, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	runTypedTANConnectionTest(t, session, typedTANConnectionBank(t))

	path := filepath.Join(t.TempDir(), "trace.txt")
	// #nosec G306 -- the test deliberately starts from a 0644 file.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	dialogs := &fakeFileDialogs{tracePath: path, traceResult: true}
	session.SetFileDialogs(dialogs)
	session.BankingSaveConnectionTrace()
	waitForJobs(t, session)

	// #nosec G304 -- path is a test temporary file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if len(data) == 0 || !strings.Contains(string(data), "Finglass FinTS trace") {
		t.Fatalf("trace content = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat trace: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("trace mode = %o, want 600", info.Mode().Perm())
	}
	if dialogs.traceTitle != i18n.Text(i18n.En, i18n.SaveConnectionTrace) {
		t.Fatalf("dialog title = %q", dialogs.traceTitle)
	}
	if !strings.HasPrefix(dialogs.traceName, "finglass-trace-"+connectionTestBLZ+"-") {
		t.Fatalf("default name = %q", dialogs.traceName)
	}

	cancelPath := filepath.Join(t.TempDir(), "cancelled.txt")
	session.SetFileDialogs(&fakeFileDialogs{tracePath: cancelPath, traceResult: false})
	session.BankingSaveConnectionTrace()
	waitForJobs(t, session)
	if _, err := os.Stat(cancelPath); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote %s", cancelPath)
	}
}

const connectionTestBankMarker = "MARKER-TEXT-1234567890"

func markerConnectionBank(t *testing.T, marker string) *connectionTestTransport {
	t.Helper()
	bank := &connectionTestTransport{t: t}
	bank.reply(connectionSync(
		connectionChipHitans("chipTAN manuell", 5, 2),
		connectionAllowed(),
		connectionHipins("HKSAL:N"),
		connectionHiupd(connectionTestIBAN, "00000001", "EUR"),
	))
	bank.reply(connectionSuccess("dialog-sync"))
	// The Init request numbers HKIDN 3, HKVVB 4 and HKTAN 5. 9942 is a PIN
	// code, so the approval retry does not run here.
	bank.reply(connectionResponse(
		connectionTestDialog,
		"HIRMG:2:2+3060::"+marker+"'",
		"HIRMS:3:2:5+9210::"+marker+"+9942::"+connectionTestText+"'",
		"HIRMS:4:2:5+0020::"+connectionTestText+"'",
	))
	return bank
}

func rejectedSavedPinBank(t *testing.T, code string) *connectionTestTransport {
	t.Helper()
	bank := &connectionTestTransport{t: t}
	bank.reply(connectionSync(
		connectionChipHitans("chipTAN manuell", 5, 2),
		connectionAllowed(),
		connectionHipins("HKSAL:N"),
		connectionHiupd(connectionTestIBAN, "00000001", "EUR"),
	))
	bank.reply(connectionSuccess("dialog-sync"))
	bank.reply(connectionResponse(
		connectionTestDialog,
		"HIRMG:2:2+3060::"+connectionTestText+"'",
		"HIRMS:3:2:5+9210::"+connectionTestText+"+"+code+"::"+connectionTestText+"'",
		"HIRMS:4:2:5+0020::"+connectionTestText+"'",
	))
	return bank
}

func tableContents(t *testing.T, database *db.Database, table string) string {
	t.Helper()
	var builder strings.Builder
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		// #nosec G202 -- table is one of the fixed names in the test.
		rows, err := conn.QueryContext(ctx, "SELECT * FROM "+table)
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
			_, _ = fmt.Fprint(&builder, values...)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return builder.String()
}

func TestBankMessagesReachTheScreenButAreNeverStored(t *testing.T) {
	session, _, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
	database := sessionDatabase(t, session)

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return markerConnectionBank(t, connectionTestBankMarker), nil
	}
	session.BankingTestConnection(int(testBankID(t, session)))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	result := waitConnectionTestDone(t, session)

	if len(result.Messages) == 0 {
		t.Fatalf("result has no bank messages: %+v", result)
	}
	found := false
	for _, message := range result.Messages {
		if message.Code == "9210" && message.Segment == "HKTAN" &&
			strings.Contains(message.Text, connectionTestBankMarker) {
			found = true
		}
	}
	if !found {
		t.Fatalf("9210/HKTAN message with the marker is missing: %+v", result.Messages)
	}

	for _, table := range []string{"audit_log", "settings"} {
		if text := tableContents(t, database, table); strings.Contains(text, connectionTestBankMarker) {
			t.Fatalf("%s contains the bank text", table)
		}
	}

	path := filepath.Join(t.TempDir(), "trace.txt")
	session.SetFileDialogs(&fakeFileDialogs{tracePath: path, traceResult: true})
	session.BankingSaveConnectionTrace()
	waitForJobs(t, session)
	// #nosec G304 -- path is a test temporary file.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved trace: %v", err)
	}
	if strings.Contains(string(saved), connectionTestBankMarker) {
		t.Fatal("the saved trace contains the bank text")
	}

	// Starting a sync clears the slot's messages. The NO-GO test locked the
	// bank, so mark it unlocked again first.
	bankID := model.BankConnectionID(testBankID(t, session))
	bank, ok, err := bankconnections.GetItem(context.Background(), database, bankID)
	if err != nil || !ok {
		t.Fatalf("get bank: ok=%t err=%v", ok, err)
	}
	serverURL := ""
	if bank.ServerURL != nil {
		serverURL = *bank.ServerURL
	}
	if err := settings.Set(
		context.Background(),
		database,
		banksync.SyncUnlockKey(bankID),
		banksync.SyncFingerprint(bank.BLZ, bank.UserID, serverURL),
	); err != nil {
		t.Fatalf("unlock bank: %v", err)
	}
	session.BankingLoad()
	waitForJobs(t, session)
	session.BankingStartSync(int(bankID))
	state := session.BankingState()
	if state.ConnectionTest == nil || len(state.ConnectionTest.Messages) != 0 {
		t.Fatalf("messages survived the sync start: %+v", state.ConnectionTest)
	}
	waitForBankingIdle(t, session)
}

// enableApprovalRetry switches the probe's approval retry on for one test.
func enableApprovalRetry(t *testing.T) {
	t.Helper()
	previous := fintsprobe.ApprovalRetryEnabled
	fintsprobe.ApprovalRetryEnabled = true
	t.Cleanup(func() { fintsprobe.ApprovalRetryEnabled = previous })
}

func TestAConnectionTestSetsOrKeepsTheSyncUnlock(t *testing.T) {
	t.Run("GO unlocks", func(t *testing.T) {
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
		database := sessionDatabase(t, session)
		if err := settings.Delete(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil {
			t.Fatalf("clear unlock: %v", err)
		}
		session.BankingLoad()
		waitForJobs(t, session)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return typedTANConnectionBank(t), nil
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		answerBankingPin(t, session, connectionTestPIN, false)
		answerBankingTan(t, session, connectionTestTAN)
		result := waitConnectionTestDone(t, session)
		if !result.Go {
			t.Fatalf("result = %+v", result)
		}
		if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || !ok {
			t.Fatalf("GO did not unlock the bank: ok=%t err=%v", ok, err)
		}
		if state := session.BankingState(); len(state.Banks) != 1 || !state.Banks[0].SyncUnlocked {
			t.Fatalf("state unlock = %+v", state.Banks)
		}
	})

	t.Run("NO-GO locks", func(t *testing.T) {
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
		database := sessionDatabase(t, session)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return markerConnectionBank(t, connectionTestBankMarker), nil
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		answerBankingPin(t, session, connectionTestPIN, false)
		result := waitConnectionTestDone(t, session)
		if result.Go {
			t.Fatalf("result = %+v", result)
		}
		if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || ok {
			t.Fatalf("NO-GO kept the unlock: ok=%t err=%v", ok, err)
		}
		if state := session.BankingState(); len(state.Banks) != 1 || state.Banks[0].SyncUnlocked {
			t.Fatalf("state unlock = %+v", state.Banks)
		}
	})

	t.Run("cancel keeps it", func(t *testing.T) {
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
		database := sessionDatabase(t, session)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return typedTANConnectionBank(t), nil
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		prompt := waitForBankingPrompt(t, session, banking.Pin)
		session.Bridge().AnswerPin(prompt.ID, nil, false)
		waitConnectionTestDone(t, session)
		if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || !ok {
			t.Fatalf("a cancel changed the unlock: ok=%t err=%v", ok, err)
		}
	})

	t.Run("transport keeps it", func(t *testing.T) {
		session, bankID, _ := newBankingSession(t, banksync.NoCredentialStore{}, nil)
		database := sessionDatabase(t, session)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return nil, errors.New("invented transport failure")
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		answerBankingPin(t, session, connectionTestPIN, false)
		waitConnectionTestDone(t, session)
		if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || !ok {
			t.Fatalf("a transport failure changed the unlock: ok=%t err=%v", ok, err)
		}
	})
}

func storedPinSnapshot(t *testing.T, store *banksync.MemoryCredentialStore, appPaths paths.AppPaths, bankID model.BankConnectionID) (int, string) {
	t.Helper()
	value, found, err := store.Read(bankingPinKey(appPaths, bankID))
	if err != nil {
		t.Fatalf("read saved PIN: %v", err)
	}
	if !found {
		return store.Len(), ""
	}
	return store.Len(), string(value)
}

func runTestConnectionWithSavedPin(
	t *testing.T,
	session *Session,
	bank *connectionTestTransport,
) banking.ConnectionTestResult {
	t.Helper()
	session.connectionTestFactory = func(string) (fints.Transport, error) { return bank, nil }
	session.BankingTestConnection(int(testBankID(t, session)))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingTan(t, session, connectionTestTAN)
	return waitConnectionTestDone(t, session)
}

func TestConnectionTestUsesTheSavedPinWithoutPromptingOrStoring(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	beforeLen, beforeValue := storedPinSnapshot(t, store, appPaths, bankID)
	bank := typedTANConnectionBank(t)

	result := runTestConnectionWithSavedPin(t, session, bank)

	if !result.Go || !result.UsedSavedPin {
		t.Fatalf("result = %+v, want GO with the saved PIN", result)
	}
	if result.SavedPinForgotten || result.SavedPinRejectedOnce {
		t.Fatalf("a GO changed the saved PIN state: %+v", result)
	}
	found := false
	for _, request := range bank.sent {
		if strings.Contains(string(request), "24680") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the fake bank never received the saved PIN")
	}
	afterLen, afterValue := storedPinSnapshot(t, store, appPaths, bankID)
	if afterLen != beforeLen || afterValue != beforeValue {
		t.Fatalf("credential store changed: %d/%q -> %d/%q", beforeLen, beforeValue, afterLen, afterValue)
	}
}

func TestConnectionTestSavedPinReadFailureAsksAndDeletesNothing(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	beforeLen, beforeValue := storedPinSnapshot(t, store, appPaths, bankID)
	store.SetFailingReads(true)

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return typedTANConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	result := waitConnectionTestDone(t, session)

	if result.UsedSavedPin {
		t.Fatal("a failing store read still reported the saved PIN")
	}
	store.SetFailingReads(false)
	afterLen, afterValue := storedPinSnapshot(t, store, appPaths, bankID)
	if afterLen != beforeLen || afterValue != beforeValue {
		t.Fatalf("credential store changed: %d/%q -> %d/%q", beforeLen, beforeValue, afterLen, afterValue)
	}
	if !hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the saved PIN or its flag was deleted after a read failure")
	}
}

func TestConnectionTestStaleSavedPinIsForgottenAndThePromptAsks(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	database := sessionDatabase(t, session)
	if err := settings.Set(
		context.Background(),
		database,
		banksync.PinFlagKey(bankID),
		"37040044|someone-else",
	); err != nil {
		t.Fatalf("stale flag: %v", err)
	}

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return typedTANConnectionBank(t), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	answerBankingPin(t, session, connectionTestPIN, false)
	answerBankingTan(t, session, connectionTestTAN)
	result := waitConnectionTestDone(t, session)

	if !result.Go || result.UsedSavedPin {
		t.Fatalf("result = %+v, want GO after the prompt", result)
	}
	if store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatal("the stale saved PIN survived")
	}
	if hasBankingPin(t, session, store, appPaths, bankID) {
		t.Fatal("the stale PIN flag survived")
	}
}

func TestConnectionTestSavedPinRejectionLockSyncAndForgetsWithOptionOne(t *testing.T) {
	for _, code := range []string{"9931", "9999"} {
		t.Run(code, func(t *testing.T) {
			store := banksync.NewMemoryCredentialStore()
			session, bankID, appPaths := newBankingSession(t, store, nil)
			saveBankingPin(t, session, store, appPaths, bankID)
			session.connectionTestFactory = func(string) (fints.Transport, error) {
				return rejectedSavedPinBank(t, code), nil
			}
			session.BankingTestConnection(int(bankID))
			answerProductID(t, session, []byte(connectionTestID))
			result := waitConnectionTestDone(t, session)

			if result.Go || !result.UsedSavedPin || !result.SavedPinForgotten {
				t.Fatalf("result = %+v, want NO-GO with the saved PIN removed", result)
			}
			if store.Contains(bankingPinKey(appPaths, bankID)) {
				t.Fatal("the rejected PIN is still stored")
			}
			database := sessionDatabase(t, session)
			if _, ok, err := settings.Get(context.Background(), database, banksync.SyncUnlockKey(bankID)); err != nil || ok {
				t.Fatalf("the NO-GO did not lock sync: ok=%t err=%v", ok, err)
			}
			if !auditHasActionForKey(t, database, banksync.CodeSyncLocked) {
				t.Fatal("the NO-GO wrote no sync-locked audit entry")
			}
		})
	}
}

func auditHasActionForKey(t *testing.T, database *db.Database, action string) bool {
	t.Helper()
	entries, err := audit.Recent(context.Background(), database, 1000)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, entry := range entries {
		if entry.Action == action {
			return true
		}
	}
	return false
}

func TestConnectionTestKeepsTheSavedPinAfterTransportAndCancel(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		store := banksync.NewMemoryCredentialStore()
		session, bankID, appPaths := newBankingSession(t, store, nil)
		saveBankingPin(t, session, store, appPaths, bankID)
		service, ok := session.bankingServiceLocked(i18n.En)
		if !ok {
			t.Fatal("no banking service")
		}
		if pin := service.StoredPinForTest(context.Background(), sessionDatabase(t, session), bankID); pin == nil {
			t.Fatal("test setup: the saved PIN is not readable")
		} else {
			pin.Wipe()
		}
		beforeLen, beforeValue := storedPinSnapshot(t, store, appPaths, bankID)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return nil, errors.New("invented transport failure")
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		result := waitConnectionTestDone(t, session)
		if result.Go || !result.UsedSavedPin || result.SavedPinForgotten {
			t.Fatalf("result = %+v", result)
		}
		afterLen, afterValue := storedPinSnapshot(t, store, appPaths, bankID)
		if afterLen != beforeLen || afterValue != beforeValue {
			t.Fatalf("credential store changed: %d/%q -> %d/%q", beforeLen, beforeValue, afterLen, afterValue)
		}
		if pinRejectionsSet(t, session, bankID) {
			t.Fatal("a transport failure changed the rejection count")
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store := banksync.NewMemoryCredentialStore()
		session, bankID, appPaths := newBankingSession(t, store, nil)
		saveBankingPin(t, session, store, appPaths, bankID)
		beforeLen, beforeValue := storedPinSnapshot(t, store, appPaths, bankID)
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return typedTANConnectionBank(t), nil
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		prompt := waitForBankingPrompt(t, session, banking.Tan)
		session.Bridge().AnswerTan(prompt.ID, nil)
		result := waitConnectionTestDone(t, session)
		if result.Go || !result.UsedSavedPin {
			t.Fatalf("result = %+v, want a non-GO with the saved PIN", result)
		}
		if result.Reason != i18n.Text(i18n.En, i18n.TestConnectionCancelled) &&
			result.Reason != i18n.Format(i18n.En, i18n.TestConnectionOther, i18n.Str("PromptFailed")) {
			t.Fatalf("result = %+v, want a cancel or prompt failure", result)
		}
		afterLen, afterValue := storedPinSnapshot(t, store, appPaths, bankID)
		if afterLen != beforeLen || afterValue != beforeValue {
			t.Fatalf("credential store changed: %d/%q -> %d/%q", beforeLen, beforeValue, afterLen, afterValue)
		}
		if pinRejectionsSet(t, session, bankID) {
			t.Fatal("a cancel changed the rejection count")
		}
	})
}

func pinRejectionsSet(t *testing.T, session *Session, bankID model.BankConnectionID) bool {
	t.Helper()
	database := sessionDatabase(t, session)
	_, ok, err := settings.Get(context.Background(), database, banksync.PinRejectionsKey(bankID))
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	return ok
}

func setPinForgetAfterForTest(t *testing.T, session *Session, bankID model.BankConnectionID, value string) {
	t.Helper()
	database := sessionDatabase(t, session)
	if err := settings.Set(
		context.Background(),
		database,
		banksync.PinForgetAfterKey(bankID),
		value,
	); err != nil {
		t.Fatalf("set pin-forget-after: %v", err)
	}
}

func TestConnectionTestOptionTwoKeepsAndThenRemovesTheSavedPin(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	setPinForgetAfterForTest(t, session, bankID, "2")

	run := func(code string) banking.ConnectionTestResult {
		session.connectionTestFactory = func(string) (fints.Transport, error) {
			return rejectedSavedPinBank(t, code), nil
		}
		session.BankingTestConnection(int(bankID))
		answerProductID(t, session, []byte(connectionTestID))
		return waitConnectionTestDone(t, session)
	}

	first := run("9999")
	if first.SavedPinForgotten || !first.SavedPinRejectedOnce {
		t.Fatalf("first rejection = %+v, want kept for one more try", first)
	}
	if !store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatal("the first rejection removed the saved PIN")
	}
	if !pinRejectionsSet(t, session, bankID) {
		t.Fatal("the first rejection wrote no count")
	}

	second := run("9999")
	if !second.SavedPinForgotten || second.SavedPinRejectedOnce {
		t.Fatalf("second rejection = %+v, want removed", second)
	}
	if store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatal("the second rejection kept the saved PIN")
	}
}

func TestConnectionTestOptionTwoResetsTheCountAfterAGo(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	setPinForgetAfterForTest(t, session, bankID, "2")

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return rejectedSavedPinBank(t, "9999"), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	first := waitConnectionTestDone(t, session)
	if !first.SavedPinRejectedOnce || !pinRejectionsSet(t, session, bankID) {
		t.Fatalf("first rejection = %+v", first)
	}

	result := runTestConnectionWithSavedPin(t, session, typedTANConnectionBank(t))
	if !result.Go {
		t.Fatalf("GO result = %+v", result)
	}
	if pinRejectionsSet(t, session, bankID) {
		t.Fatal("a GO did not reset the rejection count")
	}

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return rejectedSavedPinBank(t, "9999"), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	again := waitConnectionTestDone(t, session)
	if !again.SavedPinRejectedOnce || !store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatalf("the rejection after a GO = %+v, want kept for one more try", again)
	}
}

func TestConnectionTestOptionTwoStillRemovesAnExplicitWrongPinAtOnce(t *testing.T) {
	store := banksync.NewMemoryCredentialStore()
	session, bankID, appPaths := newBankingSession(t, store, nil)
	saveBankingPin(t, session, store, appPaths, bankID)
	setPinForgetAfterForTest(t, session, bankID, "2")

	session.connectionTestFactory = func(string) (fints.Transport, error) {
		return rejectedSavedPinBank(t, "9931"), nil
	}
	session.BankingTestConnection(int(bankID))
	answerProductID(t, session, []byte(connectionTestID))
	result := waitConnectionTestDone(t, session)
	if !result.SavedPinForgotten || result.SavedPinRejectedOnce {
		t.Fatalf("explicit rejection = %+v, want removed at once", result)
	}
	if store.Contains(bankingPinKey(appPaths, bankID)) {
		t.Fatal("the explicitly rejected PIN is still stored")
	}
}
