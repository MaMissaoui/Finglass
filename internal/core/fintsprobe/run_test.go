package fintsprobe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	testBLZ       = "99999999"
	testUser      = "test-user"
	testProductID = "TESTPRODUCT0000000000000"
	testPIN       = "pin-123456"
	testTAN       = "tan-654321"
	testIBAN      = "DE00999999990000000001"
	testIBAN2     = "DE00999999990000000002"
	testOwner     = "Owner Secretname"
	testBooking   = "Booking secret text"
	testMessage   = "Bank message secret"
	testChallenge = "Challenge text from the bank"
	testDialogID  = "dialog-1"
	testSystemID  = "test-system"
)

func testToday(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, time.March, 31)
	if !ok {
		t.Fatal("invalid test date")
	}
	return date
}

// ---- fake transport ----

type fakeReply struct {
	body string
	err  error
}

type fakeTransport struct {
	t        *testing.T
	expected [][]string
	replies  []fakeReply
	records  [][]string
}

func newFakeTransport(t *testing.T) *fakeTransport {
	t.Helper()
	return &fakeTransport{t: t}
}

func (f *fakeTransport) expect(shapes ...[]string) {
	f.expected = shapes
}

func (f *fakeTransport) reply(body string) {
	f.replies = append(f.replies, fakeReply{body: body})
}

func (f *fakeTransport) replyError(err error) {
	f.replies = append(f.replies, fakeReply{err: err})
}

func (f *fakeTransport) Exchange(_ context.Context, request []byte) ([]byte, error) {
	f.t.Helper()
	types, err := requestTypes(request)
	if err != nil {
		f.t.Fatalf("inspect request: %v", err)
	}
	call := len(f.records)
	f.records = append(f.records, types)

	if f.expected != nil {
		if call >= len(f.expected) {
			f.t.Fatalf("unexpected exchange %d: %v", call, types)
		}
		if got, want := strings.Join(types, ","), strings.Join(f.expected[call], ","); got != want {
			f.t.Fatalf("exchange %d segments = %s, want %s", call, got, want)
		}
	}
	if call >= len(f.replies) {
		f.t.Fatalf("exchange %d has no scripted reply: %v", call, types)
	}
	reply := f.replies[call]
	if reply.err != nil {
		return nil, reply.err
	}
	return []byte(reply.body), nil
}

func requestTypes(request []byte) ([]string, error) {
	segments, err := flattenRequest(request)
	if err != nil {
		return nil, err
	}
	types := make([]string, 0, len(segments))
	for _, segment := range segments {
		types = append(types, segment.Type())
	}
	return types, nil
}

func flattenRequest(request []byte) ([]fints.ParsedSegment, error) {
	outer, err := fints.ParseMessage(request)
	if err != nil {
		return nil, err
	}
	flattened := make([]fints.ParsedSegment, 0, len(outer))
	for _, segment := range outer {
		if segment.Type() != "HNVSD" {
			flattened = append(flattened, segment)
			continue
		}
		payload := segment.DEG(1).Binary(0)
		inner, err := fints.ParseMessage(payload)
		if err != nil {
			return nil, err
		}
		flattened = append(flattened, inner...)
	}
	return flattened, nil
}

func requestShape(business ...string) []string {
	types := []string{"HNHBK", "HNVSK", "HNSHK"}
	types = append(types, business...)
	types = append(types, "HNSHA", "HNHBS")
	return types
}

// ---- fake terminal ----

type fakeTerminal struct {
	prompts []string
	lines   []string
	secrets [][]byte
	next    func(prompt string) ([]byte, error)
}

func (f *fakeTerminal) ReadSecret(prompt string) ([]byte, error) {
	f.prompts = append(f.prompts, prompt)
	if f.next != nil {
		return f.next(prompt)
	}
	value := []byte(testTAN)
	f.secrets = append(f.secrets, value)
	return value, nil
}

func (f *fakeTerminal) Println(line string) {
	f.lines = append(f.lines, line)
}

func (f *fakeTerminal) output() string {
	return strings.Join(f.lines, "\n")
}

// ---- fake sleeper ----

type fakeSleeper struct {
	waits []time.Duration
}

func (s *fakeSleeper) Sleep(_ context.Context, duration time.Duration) error {
	s.waits = append(s.waits, duration)
	return nil
}

// ---- wire response builders ----

func responseMessage(dialogID string, segments ...string) string {
	return "HNHBK:1:3+000000000000+300+" + dialogID + "+1'" +
		strings.Join(segments, "") +
		"HNHBS:99:1+1'"
}

func successResponse(dialogID string) string {
	return responseMessage(dialogID, "HIRMG:2:2+0010::"+testMessage+"'")
}

func executedResponse(dialogID string, payload ...string) string {
	segments := []string{"HIRMG:2:2+0020::" + testMessage + "'"}
	segments = append(segments, payload...)
	return responseMessage(dialogID, segments...)
}

func codeResponse(dialogID, code string) string {
	return responseMessage(dialogID, "HIRMG:2:2+"+code+"::"+testMessage+"'")
}

func tanRequiredResponse(dialogID, taskReference, challenge string) string {
	return responseMessage(
		dialogID,
		"HIRMS:3:2:4+0030::TAN erforderlich'",
		"HITAN:4:7:3+4+task-hash+"+taskReference+"+"+challenge+"+@0@'",
	)
}

func decoupledResponse(dialogID, taskReference, challenge string) string {
	return responseMessage(
		dialogID,
		"HIRMS:3:2:4+3955::Im App bestaetigen'",
		"HITAN:4:7:3+4+task-hash+"+taskReference+"+"+challenge+"+@0@'",
	)
}

func decoupledPendingResponse(dialogID string) string {
	return responseMessage(dialogID, "HIRMS:3:2:4+3956::Noch nicht bestaetigt'")
}

func syncResponse(extra ...string) string {
	segments := []string{
		"HISYN:2:4:1+" + testSystemID + "'",
		"HIBPA:3:3+42+280+0+1+1+0'",
		"HIUPA:4:4+test-customer+7+55'",
	}
	segments = append(segments, extra...)
	segments = append(segments, "HIRMG:90:2+0010::"+testMessage+"'")
	return responseMessage("dialog-sync", segments...)
}

func decoupledHitans(maxPolls, firstWait, nextWait int) string {
	return fmt.Sprintf(
		"HITANS:5:7:4+1+1+1+N:N:0"+
			":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:%d:%d:%d:J:J'",
		maxPolls, firstWait, nextWait,
	)
}

func chipHitans(name string, firstWait, nextWait int) string {
	return fmt.Sprintf(
		"HITANS:5:7:4+1+1+1+N:N:0"+
			":942:2:TECH-CHIP:DKID-CHIP:1.0:%s:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1::%d:%d:J:J'",
		name, firstWait, nextWait,
	)
}

func allowedSecurityFunction(securityFunction string) string {
	return "HIRMS:6:2:4+3920::Erlaubt:" + securityFunction + "'"
}

func hipins(operations string) string {
	return "HIPINS:7:1:4+1+1+0+" + operations + "'"
}

func hisals(version int) string {
	return fmt.Sprintf("HISALS:8:%d:4+1+1+1+1'", version)
}

func hikazs(version int) string {
	return fmt.Sprintf("HIKAZS:8:%d:4+1+1+1+1'", version)
}

func hicazs(version int, format string) string {
	return fmt.Sprintf("HICAZS:8:%d:4+1+1+1+%s'", version, escapeFixtureText(format))
}

func hiupd(iban, accountNumber, currency string) string {
	return "HIUPD:7:6:4+" + accountNumber + "::280:" + testBLZ + "+" + iban + "+" + testUser +
		"+G+" + currency + "+" + testOwner + "++Girokonto+HKSAL:1+HKKAZ:1+TESTDEFFXXX'"
}

func hiupdWithoutBIC(iban, accountNumber, currency string) string {
	return "HIUPD:7:6:4+" + accountNumber + "::280:" + testBLZ + "+" + iban + "+" + testUser +
		"+G+" + currency + "+" + testOwner + "++Girokonto+HKSAL:1+HKKAZ:1'"
}

func hiupdDepot(accountNumber, currency string) string {
	return "HIUPD:9:6:4+" + accountNumber + "::280:" + testBLZ + "+" + accountNumber + "+" + testUser +
		"+G+" + currency + "+" + testOwner + "++Depot+HKDVB:1'"
}

func hiupdWithTransactions(iban, accountNumber, currency, transactions string) string {
	return "HIUPD:7:6:4+" + accountNumber + "::280:" + testBLZ + "+" + iban + "+" + testUser +
		"+G+" + currency + "+" + testOwner + "++Girokonto+" + transactions + "+TESTDEFFXXX'"
}

func hispa(accounts ...string) string {
	return "HISPA:5:4:3+" + strings.Join(accounts, "+") + "'"
}

func hispaAccount(iban, bic, accountNumber string) string {
	return "J:" + iban + ":" + bic + ":" + accountNumber + "::280:" + testBLZ
}

func hisalPayload() string {
	return "HISAL:5:7:4+1::280:" + testBLZ +
		"+Girokonto+EUR+C:98765,43:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"
}

func hikazPayload(booked, pending []byte) string {
	return "HIKAZ:5:7:4+@" + fmt.Sprint(len(booked)) + "@" + string(booked) +
		"+@" + fmt.Sprint(len(pending)) + "@" + string(pending) + "'"
}

func hicazPayload(booked, pending []byte) string {
	return "HICAZ:5:1:3+" + testIBAN + ":TESTDEFFXXX" +
		"+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" +
		"+@" + fmt.Sprint(len(booked)) + "@" + string(booked) +
		"+@" + fmt.Sprint(len(pending)) + "@" + string(pending) + "'"
}

func escapeFixtureText(value string) string {
	replacer := strings.NewReplacer("?", "??", "'", "?'", "+", "?+", ":", "?:", "@", "?@")
	return replacer.Replace(value)
}

func mt940Booked() []byte {
	return []byte(":20:STARTUMS\n" +
		":25:99999999/0000001\n" +
		":28C:1/1\n" +
		":60F:C260301EUR1000,00\n" +
		":61:2603010301C123,45NTRFNONREF//REF1\n" +
		":86:" + testBooking + " one\n" +
		":61:2603020302D45,00NTRFNONREF//REF2\n" +
		":86:" + testBooking + " two\n" +
		":61:2603150315C200,00NTRFNONREF//REF3\n" +
		":86:" + testBooking + " three\n" +
		":62F:C260331EUR1278,45\n")
}

func mt940Pending() []byte {
	return []byte(":20:STARTUMS\n" +
		":25:99999999/0000001\n" +
		":28C:1/1\n" +
		":60F:C260301EUR1000,00\n" +
		":61:2603200320C9,99NTRFNONREF//PEND1\n" +
		":86:" + testBooking + " pending\n" +
		":62F:C260331EUR1009,99\n")
}

func camtBooked() []byte {
	entries := "<Ntry><Amt Ccy=\"EUR\">98765.43</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>BOOK</Sts>" +
		"<BookgDt><Dt>2026-02-05</Dt></BookgDt><ValDt><Dt>2026-02-05</Dt></ValDt>" +
		"<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>" + testOwner + "</Nm></Cdtr></RltdPties></TxDtls></NtryDtls></Ntry>" +
		"<Ntry><Amt Ccy=\"EUR\">12.34</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>" +
		"<BookgDt><Dt>2026-02-20</Dt></BookgDt><ValDt><Dt>2026-02-20</Dt></ValDt>" +
		"<NtryDtls><TxDtls><RltdPties><Dbtr><Nm>" + testOwner + "</Nm></Dbtr></RltdPties></TxDtls></NtryDtls></Ntry>"
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.053.001.02">` +
		`<BkToCstmrStmt><Stmt><Acct><Id><IBAN>` + testIBAN + `</IBAN></Id></Acct>` +
		entries + `</Stmt></BkToCstmrStmt></Document>`)
}

// ---- probe helpers ----

func runProbe(
	t *testing.T,
	transport fints.Transport,
	terminal *fakeTerminal,
	sleeper fints.Sleeper,
	days int,
) int {
	t.Helper()
	pin := secret.NewText([]byte(testPIN))
	code := Run(context.Background(), Env{
		Inputs: Inputs{
			ProductID: testProductID,
			BLZ:       testBLZ,
			User:      testUser,
			URL:       "https://fints.example.invalid/",
			Days:      days,
		},
		PIN:            pin,
		Transport:      transport,
		Sleeper:        sleeper,
		Terminal:       terminal,
		Today:          testToday(t),
		ProductVersion: "0.4",
	})
	if !pin.IsEmpty() {
		t.Fatal("Run did not wipe the PIN")
	}
	return code
}

func assertNoSecrets(t *testing.T, output string, allowedChallenge bool) {
	t.Helper()
	forbidden := []string{
		testPIN,
		testTAN,
		testProductID,
		testUser,
		testIBAN,
		testIBAN2,
		testOwner,
		testBooking,
		testMessage,
		"98765.43",
		"98765,43",
		"DE00999999990000000001",
		"DE00999999990000000002",
	}
	if !allowedChallenge {
		forbidden = append(forbidden, testChallenge)
	}
	for _, value := range forbidden {
		if strings.Contains(output, value) {
			t.Fatalf("output leaked %q:\n%s", value, output)
		}
	}
}

func assertAllZero(t *testing.T, value []byte) {
	t.Helper()
	for index, byteValue := range value {
		if byteValue != 0 {
			t.Fatalf("byte %d = %d after Run, want 0", index, byteValue)
		}
	}
}

// ---- scenarios ----

func TestRunGoWithTwoAccountsMT940(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}
	sleeper := &fakeSleeper{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
		hiupd(testIBAN2, "00000002", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
		hispaAccount(testIBAN2, "TESTDEFFXXX", "00000002"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(nil, nil)))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, sleeper, 30)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output := terminal.output()
	wantFirst := "IBAN DE****************0001 | currency EUR | balance present | statements 3 bookings (2026-03-01..2026-03-15) MT940"
	wantSecond := "IBAN DE****************0002 | currency EUR | balance present | statements 0 bookings (no bookings) MT940"
	if !strings.Contains(output, wantFirst) {
		t.Fatalf("missing first line %q in:\n%s", wantFirst, output)
	}
	if !strings.Contains(output, wantSecond) {
		t.Fatalf("missing second line %q in:\n%s", wantSecond, output)
	}
	if !strings.HasSuffix(output, "RESULT: GO") {
		t.Fatalf("output does not end with GO:\n%s", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunGoViaCamt(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKCAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKCAZ:N+HKSPA:N"),
		hisals(7),
		hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	// The pending document repeats the booked entries; they must not count.
	bank.reply(executedResponse(testDialogID, hicazPayload(camtBooked(), camtBooked())))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output := terminal.output()
	want := "statements 2 bookings (2026-02-05..2026-02-20) camt"
	if !strings.Contains(output, want) {
		t.Fatalf("missing %q in:\n%s", want, output)
	}
	if !strings.HasSuffix(output, "RESULT: GO") {
		t.Fatalf("output does not end with GO:\n%s", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunInitDecoupledThenConfirmed(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}
	sleeper := &fakeSleeper{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(4, 7, 3),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(decoupledResponse(testDialogID, "task-1", testChallenge))
	bank.reply(decoupledPendingResponse(testDialogID))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, sleeper, 30)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(terminal.output(), confirmLogin) {
		t.Fatalf("missing confirmation line:\n%s", terminal.output())
	}
	if got := sleeper.waits; len(got) != 2 || got[0] != 7*time.Second || got[1] != 3*time.Second {
		t.Fatalf("poll waits = %v", got)
	}
	assertNoSecrets(t, terminal.output(), false)
}

func TestRunDecoupledPollLimitIsTANTimeout(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}
	sleeper := &fakeSleeper{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(2, 7, 3),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(decoupledResponse(testDialogID, "task-2", testChallenge))
	bank.reply(decoupledPendingResponse(testDialogID))
	bank.reply(decoupledPendingResponse(testDialogID))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, sleeper, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if output := terminal.output(); !strings.HasSuffix(output, "RESULT: NO-GO (TAN timeout)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, terminal.output(), false)
}

func TestRunTypedTANSubmitted(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN manuell", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(tanRequiredResponse(testDialogID, "task-3", testChallenge))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output := terminal.output()
	if !strings.Contains(output, testChallenge) {
		t.Fatalf("challenge not printed:\n%s", output)
	}
	if len(terminal.prompts) != 1 || terminal.prompts[0] != tanPrompt {
		t.Fatalf("prompts = %v", terminal.prompts)
	}
	if len(terminal.secrets) != 1 {
		t.Fatalf("secrets = %d", len(terminal.secrets))
	}
	assertAllZero(t, terminal.secrets[0])
	assertNoSecrets(t, output, true)
	if strings.Contains(output, testTAN) {
		t.Fatalf("output leaked the TAN:\n%s", output)
	}
}

func TestRunUnsupportedTANMethodEndsTheDialog(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN optisch", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(tanRequiredResponse(testDialogID, "task-4", testChallenge))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	if len(bank.records) != 4 {
		t.Fatalf("exchanges = %d, want 4 (the dialog must end with HKEND)", len(bank.records))
	}
	if !strings.Contains(output, unsupportedTanMessage) {
		t.Fatalf("missing unsupported message:\n%s", output)
	}
	if strings.Contains(output, "chipTAN optisch") {
		t.Fatalf("output leaked the method name:\n%s", output)
	}
	if !strings.HasSuffix(output, "RESULT: NO-GO (TAN method unsupported)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunPINRejectedStopsAndKeepsTheCode(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(codeResponse(testDialogID, "9942"))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want exactly 3 (no HKEND after rejection)", len(bank.records))
	}
	output := terminal.output()
	if !strings.HasSuffix(output, "RESULT: NO-GO (login rejected: 9942)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunLockedAccountStopsAndKeepsTheCode(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(codeResponse(testDialogID, "3938"))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want exactly 3 (no HKEND after a lock)", len(bank.records))
	}
	if output := terminal.output(); !strings.HasSuffix(output, "RESULT: NO-GO (login rejected: 3938)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, terminal.output(), false)
}

func TestRunInitNineThousandAtInitIsLoginRejected(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(codeResponse(testDialogID, "9078"))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want exactly 3", len(bank.records))
	}
	if output := terminal.output(); !strings.HasSuffix(output, "RESULT: NO-GO (login rejected: 9078)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, terminal.output(), false)
}

func TestRunBalanceAbsent(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hikazPayload(nil, nil)))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	if !strings.Contains(output, "balance absent") {
		t.Fatalf("missing absent balance:\n%s", output)
	}
	if !strings.HasSuffix(output, "RESULT: NO-GO (other: BalanceAbsent)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunStatementsErrorIsUnavailable(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(codeResponse(testDialogID, "9010"))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	if !strings.Contains(output, "statements unavailable") {
		t.Fatalf("missing unavailable statements:\n%s", output)
	}
	if !strings.HasSuffix(output, "RESULT: NO-GO (statements unavailable: 9010)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunStatementParseFailureIsUnavailable(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload([]byte("not an MT940 statement"), nil)))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	if !strings.Contains(output, "statements unavailable") {
		t.Fatalf("missing unavailable statements:\n%s", output)
	}
	if !strings.HasSuffix(output, "RESULT: NO-GO (statements unavailable)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunTransportError(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(requestShape("HKIDN", "HKVVB", "HKSYN"))
	bank.replyError(errors.New("network down"))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(bank.records) != 1 {
		t.Fatalf("exchanges = %d, want 1", len(bank.records))
	}
	if output := terminal.output(); !strings.HasSuffix(output, "RESULT: NO-GO (other: Transport)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, terminal.output(), false)
}

func TestRunSkipsDepotWithoutIBANThenProbesCurrentAccount(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL"),
		requestShape("HKKAZ"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hikazs(6),
		hiupdDepot("00000000", "EUR"),
		hiupdWithoutBIC(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output := terminal.output()
	skipped := "IBAN - | skipped (no balance/statement access)"
	if !strings.Contains(output, skipped) {
		t.Fatalf("missing skipped line %q in:\n%s", skipped, output)
	}
	current := "IBAN DE****************0001 | currency EUR | balance present | statements 3 bookings (2026-03-01..2026-03-15) MT940"
	if !strings.Contains(output, current) {
		t.Fatalf("missing current account line %q in:\n%s", current, output)
	}
	if !strings.HasSuffix(output, "RESULT: GO") {
		t.Fatalf("output does not end with GO:\n%s", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunSkipsAccountWithoutBalanceOrStatementAccess(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSPA:N"),
		hiupdWithTransactions(testIBAN, "00000001", "EUR", "HKKKU:1"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	skipped := "IBAN DE****************0001 | skipped (no balance/statement access)"
	if !strings.Contains(output, skipped) {
		t.Fatalf("missing skipped line %q in:\n%s", skipped, output)
	}
	if !strings.HasSuffix(output, "RESULT: NO-GO (other: NoAccounts)") {
		t.Fatalf("output = %q", output)
	}
	assertNoSecrets(t, output, false)
}

func TestRunSEPAAccountsBankErrorStopsBeforeBalance(t *testing.T) {
	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKSPA:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(codeResponse(testDialogID, "9050"))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, &fakeSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	output := terminal.output()
	if !strings.HasSuffix(output, "RESULT: NO-GO (other: BankError: 9050)") {
		t.Fatalf("output = %q", output)
	}
	for _, record := range bank.records {
		for _, segmentType := range record {
			if segmentType == "HKSAL" {
				t.Fatal("a balance request was sent after the HKSPA bank error")
			}
		}
	}
	assertNoSecrets(t, output, false)
}

func TestRunWipesPINOnEveryPath(t *testing.T) {
	cases := []struct {
		name    string
		replies func(*fakeTransport)
	}{
		{
			name: "success",
			replies: func(bank *fakeTransport) {
				bank.reply(syncResponse(hipins("HKSAL:N+HKSPA:N")))
				bank.reply(successResponse("dialog-sync"))
				bank.reply(executedResponse(testDialogID))
				bank.reply(executedResponse(testDialogID))
				bank.reply(successResponse(testDialogID))
			},
		},
		{
			name: "rejection",
			replies: func(bank *fakeTransport) {
				bank.reply(syncResponse(hipins("HKSAL:N")))
				bank.reply(successResponse("dialog-sync"))
				bank.reply(codeResponse(testDialogID, "9942"))
			},
		},
		{
			name: "transport failure",
			replies: func(bank *fakeTransport) {
				bank.replyError(errors.New("network down"))
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bank := newFakeTransport(t)
			test.replies(bank)
			pin := secret.NewText([]byte(testPIN))
			code := Run(context.Background(), Env{
				Inputs: Inputs{
					ProductID: testProductID,
					BLZ:       testBLZ,
					User:      testUser,
					URL:       "https://fints.example.invalid/",
					Days:      30,
				},
				PIN:            pin,
				Transport:      bank,
				Sleeper:        &fakeSleeper{},
				Terminal:       &fakeTerminal{},
				Today:          testToday(t),
				ProductVersion: "0.4",
			})
			if !pin.IsEmpty() {
				t.Fatalf("PIN not wiped; exit code %d", code)
			}
		})
	}
}

// blockingSleeper waits until the poll context ends, like a real wait that
// outlasts the probe's TAN deadline.
type blockingSleeper struct{}

func (blockingSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

// The probe's own TAN deadline (5 minutes in production) is a TAN timeout,
// not a cancellation, and the dialog still ends with HKEND.
func TestRunDecoupledDeadlineIsTANTimeout(t *testing.T) {
	previous := tanTimeout
	tanTimeout = 50 * time.Millisecond
	t.Cleanup(func() { tanTimeout = previous })

	bank := newFakeTransport(t)
	terminal := &fakeTerminal{}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(decoupledResponse(testDialogID, "task-9", testChallenge))
	bank.reply(successResponse(testDialogID))

	code := runProbe(t, bank, terminal, blockingSleeper{}, 30)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if output := terminal.output(); !strings.HasSuffix(output, "RESULT: NO-GO (TAN timeout)") {
		t.Fatalf("output = %q", output)
	}
	if len(bank.records) != 4 {
		t.Fatalf("exchanges = %d, want 4 (HKEND after the deadline)", len(bank.records))
	}
}
