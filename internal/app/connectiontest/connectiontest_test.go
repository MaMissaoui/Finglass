package connectiontest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

const (
	testBLZ       = "99999999"
	testUser      = "invented-user-4711"
	testProductID = "TESTPRODUCT0000000000000"
	testPIN       = "pin-123456"
	testTAN       = "tan-654321"
	testIBAN      = "DE00999999990000000001"
	testOwner     = "Invented Owner Name"
	testBooking   = "Invented booking text"
	testMessage   = "Invented bank message"
	testChallenge = "Invented challenge text"
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

// ---- scripted transport ----

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

func requestShape(business ...string) []string {
	types := []string{"HNHBK", "HNVSK", "HNSHK"}
	types = append(types, business...)
	types = append(types, "HNSHA", "HNHBS")
	return types
}

// ---- scripted interaction ----

type fakeInteraction struct {
	mu sync.Mutex

	pin           string
	remember      []bool
	pinBanks      []string
	pinUsers      []string
	tans          []string
	confirm       func(string) bool
	confirming    int
	dismissed     int
	dismissedHook func()
	infos         []string
}

func (f *fakeInteraction) Pin(bankName, userID string, offerRemember bool) *banksync.PinEntry {
	f.mu.Lock()
	f.pinBanks = append(f.pinBanks, bankName)
	f.pinUsers = append(f.pinUsers, userID)
	f.remember = append(f.remember, offerRemember)
	pin := f.pin
	f.mu.Unlock()
	if pin == "" {
		return nil
	}
	entry := banksync.NewPinEntry(secret.NewText([]byte(pin)), offerRemember)
	return &entry
}

func (f *fakeInteraction) SelectTanMethod([]banksync.TanMethod) *string { return nil }

func (f *fakeInteraction) SelectTanMedium([]string) *string { return nil }

func (f *fakeInteraction) Tan(challenge banksync.TanChallenge) *secret.Text {
	f.mu.Lock()
	f.tans = append(f.tans, challenge.Message)
	f.mu.Unlock()
	return secret.NewText([]byte(testTAN))
}

func (f *fakeInteraction) ConfirmDecoupled(message string) bool {
	f.mu.Lock()
	f.confirming++
	confirm := f.confirm
	f.mu.Unlock()

	confirmed := true
	if confirm != nil {
		confirmed = confirm(message)
	}

	f.mu.Lock()
	f.confirming--
	f.mu.Unlock()
	return confirmed
}

func (f *fakeInteraction) Info(message string) {
	f.mu.Lock()
	f.infos = append(f.infos, message)
	f.mu.Unlock()
}

// DismissDecoupled unblocks a confirmation that waits for the app to dismiss
// its prompt, like the bridge does.
func (f *fakeInteraction) DismissDecoupled() {
	f.mu.Lock()
	f.dismissed++
	confirm := f.confirm
	hook := f.dismissedHook
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if confirm != nil {
		confirm("dismiss")
	}
}

func (f *fakeInteraction) activeConfirmations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.confirming
}

// ---- fake sleeper ----

type fakeSleeper struct {
	waits []time.Duration
}

func (s *fakeSleeper) Sleep(_ context.Context, duration time.Duration) error {
	s.waits = append(s.waits, duration)
	return nil
}

type blockingSleeper struct{}

func (blockingSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

// ---- wire builders ----

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

// ---- scenarios ----

func depotGoBank(t *testing.T) *fakeTransport {
	t.Helper()
	bank := newFakeTransport(t)
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
	return bank
}

func typedTANGoBank(t *testing.T) *fakeTransport {
	t.Helper()
	bank := newFakeTransport(t)
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
	return bank
}

func newInput(t *testing.T, interaction *fakeInteraction, factory TransportFactory) Input {
	t.Helper()
	return Input{
		BankName:       "Invented Bank",
		BLZ:            testBLZ,
		UserID:         testUser,
		ServerURL:      "https://fints.example.invalid/",
		ProductID:      secret.NewText([]byte(testProductID)),
		Interaction:    interaction,
		Factory:        factory,
		Sleeper:        &fakeSleeper{},
		Today:          testToday(t),
		ProductVersion: "0.4",
	}
}

func factoryFor(bank *fakeTransport) TransportFactory {
	return func(string) (fints.Transport, error) { return bank, nil }
}

func TestRunGoProbesOneAccountAndSkipsTheDepot(t *testing.T) {
	interaction := &fakeInteraction{pin: testPIN}
	bank := depotGoBank(t)
	outcome := Run(context.Background(), newInput(t, interaction, factoryFor(bank)))

	if !outcome.Go {
		t.Fatalf("outcome = %s, want GO", outcome)
	}
	if outcome.Probed != 1 || outcome.Skipped != 1 {
		t.Fatalf("probed/skipped = %d/%d, want 1/1", outcome.Probed, outcome.Skipped)
	}
	if len(outcome.Trace) == 0 {
		t.Fatal("GO has no trace")
	}
	traceText := string(outcome.Trace)
	for _, marker := range []string{
		testPIN, testTAN, testProductID, testUser, testIBAN, testOwner,
		"98765", testBooking, testMessage, testChallenge,
	} {
		if strings.Contains(traceText, marker) {
			t.Fatalf("trace leaked %q", marker)
		}
	}
	if len(interaction.remember) != 1 || interaction.remember[0] {
		t.Fatalf("offerRemember = %v, want false", interaction.remember)
	}
	if interaction.pinBanks[0] != "Invented Bank" || interaction.pinUsers[0] != testUser {
		t.Fatalf("pin prompt bank/user = %q/%q", interaction.pinBanks[0], interaction.pinUsers[0])
	}
}

func TestRunLoginRejectedKeepsTheCode(t *testing.T) {
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(codeResponse(testDialogID, "9942"))

	outcome := Run(context.Background(), newInput(t, &fakeInteraction{pin: testPIN}, factoryFor(bank)))
	if outcome.Go {
		t.Fatalf("outcome = %s, want NO-GO", outcome)
	}
	if outcome.Kind != fintsprobe.NoGoLoginRejected {
		t.Fatalf("kind = %d, want login rejected", outcome.Kind)
	}
	if fmt.Sprint(outcome.Codes) != fmt.Sprint([]string{"9942"}) {
		t.Fatalf("codes = %v, want [9942]", outcome.Codes)
	}
}

func TestRunTypedTanIsAnswered(t *testing.T) {
	interaction := &fakeInteraction{pin: testPIN}
	bank := typedTANGoBank(t)
	outcome := Run(context.Background(), newInput(t, interaction, factoryFor(bank)))

	if !outcome.Go {
		t.Fatalf("outcome = %s, want GO", outcome)
	}
	if fmt.Sprint(interaction.tans) != fmt.Sprint([]string{testChallenge}) {
		t.Fatalf("TAN challenges = %v, want [%q]", interaction.tans, testChallenge)
	}
}

func TestRunDecoupledConfirmedByPolling(t *testing.T) {
	bank := newFakeTransport(t)
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

	sleeper := &fakeSleeper{}
	input := newInput(t, &fakeInteraction{pin: testPIN}, factoryFor(bank))
	input.Sleeper = sleeper
	outcome := Run(context.Background(), input)

	if !outcome.Go {
		t.Fatalf("outcome = %s, want GO", outcome)
	}
	if len(sleeper.waits) != 2 || sleeper.waits[0] != 7*time.Second || sleeper.waits[1] != 3*time.Second {
		t.Fatalf("poll waits = %v", sleeper.waits)
	}
}

func TestRunDecoupledCancelledByTheUserPollsNoFurther(t *testing.T) {
	bank := newFakeTransport(t)
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
	bank.reply(decoupledResponse(testDialogID, "task-cancel", testChallenge))
	bank.reply(successResponse(testDialogID))

	interaction := &fakeInteraction{pin: testPIN}
	interaction.confirm = func(string) bool { return false }
	input := newInput(t, interaction, factoryFor(bank))
	input.Sleeper = blockingSleeper{}
	outcome := Run(context.Background(), input)

	if !outcome.Cancelled || outcome.Go {
		t.Fatalf("outcome = %s, want cancelled", outcome)
	}
	if fmt.Sprint(outcome.Other) != OtherCancelled {
		t.Fatalf("other = %q, want %q", outcome.Other, OtherCancelled)
	}
	if len(bank.records) != 4 {
		t.Fatalf("exchanges = %d, want 4 with no poll", len(bank.records))
	}
	if fmt.Sprint(bank.records[3]) != fmt.Sprint(requestShape("HKEND")) {
		t.Fatalf("last exchange = %v, want HKEND", bank.records[3])
	}
}

func TestRunPinPromptNilCancelsWithoutAnyExchange(t *testing.T) {
	factoryCalls := 0
	factory := func(string) (fints.Transport, error) {
		factoryCalls++
		return newFakeTransport(t), nil
	}
	product := secret.NewText([]byte(testProductID))
	outcome := Run(context.Background(), Input{
		BankName:    "Invented Bank",
		BLZ:         testBLZ,
		UserID:      testUser,
		ServerURL:   "https://fints.example.invalid/",
		ProductID:   product,
		Interaction: &fakeInteraction{},
		Factory:     factory,
		Today:       testToday(t),
	})
	if !outcome.Cancelled || outcome.Go {
		t.Fatalf("outcome = %s, want cancelled", outcome)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls = %d, want 0", factoryCalls)
	}
	if len(outcome.Trace) != 0 {
		t.Fatal("cancelled test has a trace")
	}
	if !product.IsEmpty() {
		t.Fatal("product id was not wiped")
	}
}

func TestRunInvalidInputsNeverBuildOrExchange(t *testing.T) {
	cases := []struct {
		name      string
		product   *secret.Text
		blz       string
		url       string
		wantOther string
	}{
		{"blz", secret.NewText([]byte(testProductID)), "123", "https://fints.example.invalid/", OtherInvalidLogin},
		{"url", secret.NewText([]byte(testProductID)), testBLZ, "http://fints.example.invalid/", OtherInvalidLogin},
		{"product id", secret.NewText([]byte("bad?id")), testBLZ, "https://fints.example.invalid/", OtherProductIDInvalid},
		{"missing product id", nil, testBLZ, "https://fints.example.invalid/", OtherProductIDMissing},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			factoryCalls := 0
			outcome := Run(context.Background(), Input{
				BankName:    "Invented Bank",
				BLZ:         test.blz,
				UserID:      testUser,
				ServerURL:   test.url,
				ProductID:   test.product,
				Interaction: &fakeInteraction{pin: testPIN},
				Factory: func(string) (fints.Transport, error) {
					factoryCalls++
					return newFakeTransport(t), nil
				},
				Today: testToday(t),
			})
			if outcome.Go || outcome.Cancelled {
				t.Fatalf("outcome = %s, want invalid", outcome)
			}
			if outcome.Other != test.wantOther {
				t.Fatalf("other = %q, want %q", outcome.Other, test.wantOther)
			}
			if factoryCalls != 0 {
				t.Fatalf("factory calls = %d, want 0", factoryCalls)
			}
			if test.product != nil && !test.product.IsEmpty() {
				t.Fatal("product id was not wiped")
			}
		})
	}
}

func TestRunWipesPinAndProductIDOnEveryPath(t *testing.T) {
	cases := []struct {
		name    string
		replies func(*fakeTransport)
		factory TransportFactory
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
			name: "factory failure",
			factory: func(string) (fints.Transport, error) {
				return nil, errors.New("invented transport failure")
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bank := newFakeTransport(t)
			if test.replies != nil {
				test.replies(bank)
			}
			factory := test.factory
			if factory == nil {
				factory = factoryFor(bank)
			}
			interaction := &fakeInteraction{pin: testPIN}
			product := secret.NewText([]byte(testProductID))
			input := newInput(t, interaction, factory)
			input.ProductID = product
			Run(context.Background(), input)

			if !product.IsEmpty() {
				t.Fatal("product id was not wiped")
			}
			if interaction.remember[0] {
				t.Fatal("offerRemember was true")
			}
		})
	}
}

func TestRunNoConfirmationGoroutineOutlivesIt(t *testing.T) {
	bank := newFakeTransport(t)
	bank.reply(syncResponse(
		decoupledHitans(1, 0, 0),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(decoupledResponse(testDialogID, "task-open", testChallenge))
	bank.reply(decoupledPendingResponse(testDialogID))
	bank.reply(successResponse(testDialogID))

	dismissed := make(chan struct{}, 1)
	interaction := &fakeInteraction{pin: testPIN}
	interaction.confirm = func(message string) bool {
		if message == "dismiss" {
			return false
		}
		// The app prompt stays open until finish dismisses it, while the
		// probe reaches its poll limit.
		<-dismissed
		return false
	}
	interaction.dismissedHook = func() {
		select {
		case dismissed <- struct{}{}:
		default:
		}
	}
	input := newInput(t, interaction, factoryFor(bank))

	done := make(chan Outcome, 1)
	go func() { done <- Run(context.Background(), input) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if active := interaction.activeConfirmations(); active != 0 {
		t.Fatalf("confirmation goroutines still active: %d", active)
	}
}

// A pushTAN prompt that the app posts only after the probe has already finished
// polling is still dismissed: Run must not wait for the prompt timeout.
func TestRunDismissesAPromptPostedAfterThePollEnded(t *testing.T) {
	bank := newFakeTransport(t)
	bank.reply(syncResponse(
		decoupledHitans(4, 7, 3),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:N+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(decoupledResponse(testDialogID, "task-late", testChallenge))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))

	var postMu sync.Mutex
	posted := false
	answered := make(chan struct{}, 1)
	interaction := &fakeInteraction{pin: testPIN}
	interaction.confirm = func(message string) bool {
		if message == "dismiss" {
			postMu.Lock()
			isPosted := posted
			postMu.Unlock()
			if isPosted {
				select {
				case answered <- struct{}{}:
				default:
				}
			}
			return false
		}
		// The prompt is posted late; a dismiss before that finds nothing.
		time.Sleep(100 * time.Millisecond)
		postMu.Lock()
		posted = true
		postMu.Unlock()
		<-answered
		return false
	}
	input := newInput(t, interaction, factoryFor(bank))
	input.Sleeper = &fakeSleeper{}

	done := make(chan Outcome, 1)
	go func() { done <- Run(context.Background(), input) }()
	select {
	case outcome := <-done:
		if !outcome.Go {
			t.Fatalf("outcome = %s, want GO", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run waited for a prompt that was posted after the poll ended")
	}
	if active := interaction.activeConfirmations(); active != 0 {
		t.Fatalf("confirmation goroutines still active: %d", active)
	}
}
