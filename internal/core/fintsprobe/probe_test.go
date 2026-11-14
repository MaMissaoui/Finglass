package fintsprobe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

// probeInteraction is a scripted Interaction. Its callbacks let a test return a
// TAN, cancel a context or observe a notice.
type probeInteraction struct {
	tans          []string
	confirmations []string
	notices       []Notice
	retries       int
	tan           func(ctx context.Context, challenge string) (*secret.Text, error)
	confirm       func(ctx context.Context, challenge string)
	confirmRetry  func(ctx context.Context) bool
}

func (i *probeInteraction) TAN(ctx context.Context, challenge string) (*secret.Text, error) {
	i.tans = append(i.tans, challenge)
	if i.tan != nil {
		return i.tan(ctx, challenge)
	}
	return secret.NewText([]byte(testTAN)), nil
}

func (i *probeInteraction) ConfirmInApp(ctx context.Context, challenge string) {
	i.confirmations = append(i.confirmations, challenge)
	if i.confirm != nil {
		i.confirm(ctx, challenge)
	}
}

func (i *probeInteraction) ConfirmRetry(ctx context.Context) bool {
	i.retries++
	if i.confirmRetry != nil {
		return i.confirmRetry(ctx)
	}
	return true
}

func (i *probeInteraction) Notice(notice Notice) {
	i.notices = append(i.notices, notice)
}

func newProbeEnv(
	t *testing.T,
	transport fints.Transport,
	interaction Interaction,
	pin *secret.Text,
) ProbeEnv {
	t.Helper()
	return ProbeEnv{
		Inputs: Inputs{
			ProductID: testProductID,
			BLZ:       testBLZ,
			User:      testUser,
			URL:       "https://fints.example.invalid/",
			Days:      30,
		},
		PIN:            pin,
		Transport:      transport,
		Sleeper:        &fakeSleeper{},
		Interaction:    interaction,
		Today:          testToday(t),
		ProductVersion: "0.4",
	}
}

// depotGoBank scripts the depot-plus-current-account GO scenario.
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

// typedTANGoBank scripts a typed TAN at Init followed by a complete GO run.
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

// approvalRequiredResponse is the HVB answer to a new system id's login.
func approvalRequiredResponse(dialogID string) string {
	return responseMessage(
		dialogID,
		"HIRMG:2:2+9050::"+testMessage+"'",
		"HIRMS:3:2:4+9210::"+testMessage+"'",
	)
}

// exemptedInitResponse opens the dialog without strong authentication.
func exemptedInitResponse(dialogID string) string {
	return responseMessage(
		dialogID,
		"HIRMG:2:2+0020::"+testMessage+"'",
		"HIRMS:3:2:4+3076::"+testMessage+"'",
	)
}

// approvalRetryGoBank scripts a login that needs the approval retry and then
// completes the GO scenario.
func approvalRetryGoBank(t *testing.T) *fakeTransport {
	t.Helper()
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
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
	bank.reply(approvalRequiredResponse(testDialogID))
	bank.reply(exemptedInitResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))
	return bank
}

func TestProbeRetriesLoginOnceAfterApproval(t *testing.T) {
	enableApprovalRetry(t)
	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	initRequests := make([][]fints.ParsedSegment, 0, 2)
	capture := &captureTransport{
		fakeTransport: approvalRetryGoBank(t),
		onExchange: func(call int, request []byte) {
			if call != 2 && call != 3 {
				return
			}
			segments, err := flattenRequest(request)
			if err != nil {
				t.Fatalf("inspect Init request %d: %v", call, err)
			}
			initRequests = append(initRequests, segments)
		},
	}

	result := Probe(context.Background(), newProbeEnv(t, capture, interaction, pin))
	if !result.Decision.Go {
		t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
	}
	if interaction.retries != 1 {
		t.Fatalf("ConfirmRetry calls = %d, want 1", interaction.retries)
	}
	if len(initRequests) != 2 {
		t.Fatalf("Init requests = %d, want 2", len(initRequests))
	}

	second := initRequests[1]
	hasHKIDN := false
	for _, segment := range second {
		if segment.Type() == "HKSYN" {
			t.Fatal("the retried Init sent HKSYN")
		}
		if segment.Type() != "HKIDN" {
			continue
		}
		hasHKIDN = true
		if got := segment.DEG(3).Text(0); got != testSystemID {
			t.Fatalf("retried HKIDN system id = %q, want %q", got, testSystemID)
		}
	}
	if !hasHKIDN {
		t.Fatal("the retried Init sent no HKIDN")
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeApprovalCancelStopsWithoutRetry(t *testing.T) {
	enableApprovalRetry(t)
	bank := approvalRetryGoBank(t)
	interaction := &probeInteraction{
		confirmRetry: func(context.Context) bool { return false },
	}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a cancelled approval retry must not be a GO")
	}
	if got := result.Decision.Reason.String(); got != "other: Cancelled" {
		t.Fatalf("reason = %q, want other: Cancelled", got)
	}
	if interaction.retries != 1 {
		t.Fatalf("ConfirmRetry calls = %d, want 1", interaction.retries)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want 3 with no request after the failed Init", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeDoesNotRetryOnPinCodeWithApprovalCode(t *testing.T) {
	enableApprovalRetry(t)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(responseMessage(
		testDialogID,
		"HIRMG:2:2+9210::"+testMessage+"'",
		"HIRMS:3:2:4+9942::"+testMessage+"'",
	))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a rejected PIN must not be a GO")
	}
	if interaction.retries != 0 {
		t.Fatalf("ConfirmRetry calls = %d, want 0", interaction.retries)
	}
	if got := result.Decision.Reason.String(); got != "login rejected: 9942" {
		t.Fatalf("reason = %q, want login rejected: 9942", got)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want 3", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeRetriesOnlyOnce(t *testing.T) {
	enableApprovalRetry(t)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(approvalRequiredResponse(testDialogID))
	bank.reply(approvalRequiredResponse(testDialogID))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a second rejected Init must not be a GO")
	}
	if interaction.retries != 1 {
		t.Fatalf("ConfirmRetry calls = %d, want exactly 1", interaction.retries)
	}
	if len(bank.records) != 4 {
		t.Fatalf("exchanges = %d, want 4 with no further retry", len(bank.records))
	}
	if reason := result.Decision.Reason.String(); !strings.Contains(reason, "9210") {
		t.Fatalf("reason = %q, want code 9210", reason)
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeDoesNotRetryOnNineThousandWithoutApprovalCode(t *testing.T) {
	enableApprovalRetry(t)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(hipins("HKSAL:N")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(codeResponse(testDialogID, "9050"))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("9050 without 9210 must not be a GO")
	}
	if interaction.retries != 0 {
		t.Fatalf("ConfirmRetry calls = %d, want 0", interaction.retries)
	}
	if got := result.Decision.Reason.String(); got != "login rejected: 9050" {
		t.Fatalf("reason = %q, want login rejected: 9050", got)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want 3", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeRetrySendsAndWipesThePIN(t *testing.T) {
	enableApprovalRetry(t)
	inits := 0
	capture := &captureTransport{
		fakeTransport: approvalRetryGoBank(t),
		onExchange: func(call int, request []byte) {
			if call != 2 && call != 3 {
				return
			}
			if !bytes.Contains(request, []byte(testPIN)) {
				t.Fatalf("Init request %d did not carry the PIN", call)
			}
			inits++
		},
	}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, capture, &probeInteraction{}, pin))

	if !result.Decision.Go {
		t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
	}
	if inits != 2 {
		t.Fatalf("Init requests with the PIN = %d, want 2", inits)
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestShouldRetryAfterApproval(t *testing.T) {
	type retryCase struct {
		name     string
		err      error
		systemID string
		want     bool
	}
	cases := []retryCase{
		{
			name:     "approval code",
			err:      &fints.DialogError{Kind: fints.ErrorKindPinRejected, Codes: []string{"9050", "9210"}},
			systemID: testSystemID,
			want:     true,
		},
		{
			name:     "empty system id",
			err:      &fints.DialogError{Codes: []string{"9210"}},
			systemID: "",
			want:     false,
		},
		{
			name:     "zero system id",
			err:      &fints.DialogError{Codes: []string{"9210"}},
			systemID: "0",
			want:     false,
		},
		{
			name:     "no approval code",
			err:      &fints.DialogError{Kind: fints.ErrorKindPinRejected, Codes: []string{"9050"}},
			systemID: testSystemID,
			want:     false,
		},
		{
			name:     "plain error",
			err:      errors.New("invented failure"),
			systemID: testSystemID,
			want:     false,
		},
		{
			name:     "nil error",
			err:      nil,
			systemID: testSystemID,
			want:     false,
		},
	}
	for _, blocked := range []string{"9340", "9910", "9930", "9931", "9942", "3938", "9301", "9399", "9950", "9999"} {
		cases = append(cases, retryCase{
			name:     "blocked " + blocked,
			err:      &fints.DialogError{Codes: []string{"9210", blocked}},
			systemID: testSystemID,
			want:     false,
		})
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRetryAfterApproval(test.err, test.systemID); got != test.want {
				t.Fatalf("shouldRetryAfterApproval() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestTerminalConfirmRetryReadsOneLine(t *testing.T) {
	cases := []struct {
		name   string
		answer []byte
		err    error
		want   bool
	}{
		{name: "enter", answer: nil, want: true},
		{name: "empty", answer: []byte{}, want: true},
		{name: "n", answer: []byte("n"), want: false},
		{name: "anything", answer: []byte("yes"), want: false},
		{name: "read error", err: errors.New("invented failure"), want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			answer := test.answer
			terminal := &fakeTerminal{
				next: func(string) ([]byte, error) { return answer, test.err },
			}
			interaction := terminalInteraction{terminal: terminal}
			if got := interaction.ConfirmRetry(context.Background()); got != test.want {
				t.Fatalf("ConfirmRetry() = %t, want %t", got, test.want)
			}
			if len(terminal.prompts) != 1 || terminal.prompts[0] != "" {
				t.Fatalf("prompts = %v, want one empty prompt", terminal.prompts)
			}
			if len(terminal.lines) != 1 || terminal.lines[0] != confirmRetryMessage {
				t.Fatalf("lines = %v, want [%q]", terminal.lines, confirmRetryMessage)
			}
			assertAllZero(t, answer)
		})
	}
}

func TestProbeGoLinesMatchRunOutput(t *testing.T) {
	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, depotGoBank(t), interaction, pin))

	if !result.Decision.Go {
		t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
	}
	if result.Probed != 1 || result.Skipped != 1 {
		t.Fatalf("probed/skipped = %d/%d, want 1/1", result.Probed, result.Skipped)
	}
	if len(result.Summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(result.Summaries))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}

	wantLines := []string{
		"IBAN - | skipped (no balance/statement access)",
		"IBAN DE****************0001 | currency EUR | balance present | statements 3 bookings (2026-03-01..2026-03-15) MT940",
	}
	if fmt.Sprint(result.Lines) != fmt.Sprint(wantLines) {
		t.Fatalf("lines = %v, want %v", result.Lines, wantLines)
	}

	terminal := &fakeTerminal{}
	code := runProbe(t, depotGoBank(t), terminal, &fakeSleeper{}, 30)
	if code != 0 {
		t.Fatalf("Run exit code = %d, want 0", code)
	}
	wantTerminal := append(append([]string(nil), result.Lines...), "Path: normal", "RESULT: GO")
	if fmt.Sprint(terminal.lines) != fmt.Sprint(wantTerminal) {
		t.Fatalf("terminal output = %v, want %v", terminal.lines, wantTerminal)
	}
}

func TestProbeTypedTANAtInit(t *testing.T) {
	t.Run("submitted and wiped", func(t *testing.T) {
		tan := secret.NewText([]byte(testTAN))
		interaction := &probeInteraction{
			tan: func(context.Context, string) (*secret.Text, error) {
				return tan, nil
			},
		}
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), newProbeEnv(t, typedTANGoBank(t), interaction, pin))

		if !result.Decision.Go {
			t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
		}
		if fmt.Sprint(interaction.tans) != fmt.Sprint([]string{testChallenge}) {
			t.Fatalf("TAN challenges = %v, want [%q]", interaction.tans, testChallenge)
		}
		if !tan.IsEmpty() {
			t.Fatal("Probe did not wipe the returned TAN")
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})

	t.Run("nil TAN cancels", func(t *testing.T) {
		bank := newFakeTransport(t)
		bank.expect(
			requestShape("HKIDN", "HKVVB", "HKSYN"),
			requestShape("HKEND"),
			requestShape("HKIDN", "HKVVB", "HKTAN"),
			requestShape("HKEND"),
		)
		bank.reply(syncResponse(
			chipHitans("chipTAN manuell", 5, 2),
			allowedSecurityFunction("942"),
			hipins("HKSAL:N"),
			hiupd(testIBAN, "00000001", "EUR"),
		))
		bank.reply(successResponse("dialog-sync"))
		bank.reply(tanRequiredResponse(testDialogID, "task-nil", testChallenge))
		bank.reply(successResponse(testDialogID))

		interaction := &probeInteraction{
			tan: func(context.Context, string) (*secret.Text, error) {
				return nil, nil
			},
		}
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

		if result.Decision.Go {
			t.Fatal("nil TAN must not be a GO")
		}
		if fmt.Sprint(interaction.tans) != fmt.Sprint([]string{testChallenge}) {
			t.Fatalf("TAN challenges = %v, want [%q]", interaction.tans, testChallenge)
		}
		if len(bank.records) != 4 {
			t.Fatalf("exchanges = %d, want 4 including the best-effort HKEND", len(bank.records))
		}
		if fmt.Sprint(bank.records[3]) != fmt.Sprint(requestShape("HKEND")) {
			t.Fatalf("last exchange = %v, want HKEND", bank.records[3])
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})
}

func TestProbeDecoupledCancelDuringPoll(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interaction := &probeInteraction{
		confirm: func(context.Context, string) {
			cancel()
		},
	}
	pin := secret.NewText([]byte(testPIN))
	env := newProbeEnv(t, bank, interaction, pin)
	env.Sleeper = blockingSleeper{}
	result := Probe(ctx, env)

	if result.Decision.Go {
		t.Fatal("a cancelled decoupled poll must not be a GO")
	}
	if fmt.Sprint(interaction.confirmations) != fmt.Sprint([]string{testChallenge}) {
		t.Fatalf("confirmations = %v, want [%q]", interaction.confirmations, testChallenge)
	}
	if got := result.Decision.Reason.String(); got != "other: Cancelled" {
		t.Fatalf("reason = %q, want %q", got, "other: Cancelled")
	}
	if len(bank.records) != 4 {
		t.Fatalf("exchanges = %d, want 4 including the best-effort HKEND", len(bank.records))
	}
	if fmt.Sprint(bank.records[3]) != fmt.Sprint(requestShape("HKEND")) {
		t.Fatalf("last exchange = %v, want HKEND", bank.records[3])
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeUnsupportedTANMethodNotice(t *testing.T) {
	bank := newFakeTransport(t)
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

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("an unsupported TAN method must not be a GO")
	}
	if len(interaction.notices) != 1 || interaction.notices[0] != NoticeTanMethodUnsupported {
		t.Fatalf("notices = %v, want [%v]", interaction.notices, NoticeTanMethodUnsupported)
	}
	if len(interaction.tans) != 0 {
		t.Fatalf("TAN called %d times, want 0", len(interaction.tans))
	}
	if got := result.Decision.Reason.String(); got != "TAN method unsupported" {
		t.Fatalf("reason = %q, want %q", got, "TAN method unsupported")
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeInvalidEnvironmentAndRunExit(t *testing.T) {
	t.Run("nil interaction", func(t *testing.T) {
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), ProbeEnv{
			Inputs:      Inputs{ProductID: testProductID, BLZ: testBLZ, User: testUser, Days: 30},
			PIN:         pin,
			Interaction: nil,
		})
		if !result.Invalid {
			t.Fatal("nil interaction must make the result invalid")
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})

	t.Run("nil PIN", func(t *testing.T) {
		result := Probe(context.Background(), ProbeEnv{
			Inputs:      Inputs{ProductID: testProductID, BLZ: testBLZ, User: testUser, Days: 30},
			PIN:         nil,
			Interaction: &probeInteraction{},
		})
		if !result.Invalid {
			t.Fatal("nil PIN must make the result invalid")
		}
	})

	t.Run("Run nil terminal", func(t *testing.T) {
		pin := secret.NewText([]byte(testPIN))
		code := Run(context.Background(), Env{
			Inputs:   Inputs{ProductID: testProductID, BLZ: testBLZ, User: testUser, Days: 30},
			PIN:      pin,
			Terminal: nil,
		})
		if code != 2 {
			t.Fatalf("Run exit code = %d, want 2", code)
		}
		if !pin.IsEmpty() {
			t.Fatal("Run did not wipe the PIN")
		}
	})

	t.Run("Run nil PIN", func(t *testing.T) {
		code := Run(context.Background(), Env{
			Inputs:   Inputs{ProductID: testProductID, BLZ: testBLZ, User: testUser, Days: 30},
			PIN:      nil,
			Terminal: &fakeTerminal{},
		})
		if code != 2 {
			t.Fatalf("Run exit code = %d, want 2", code)
		}
	})
}

func TestProbeFormattingHidesIdentifiers(t *testing.T) {
	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	env := newProbeEnv(t, depotGoBank(t), interaction, pin)

	formatted := []string{
		fmt.Sprintf("%v", env),
		fmt.Sprintf("%+v", env),
		fmt.Sprintf("%#v", env),
	}

	result := Probe(context.Background(), env)
	formatted = append(formatted,
		fmt.Sprintf("%v", result),
		fmt.Sprintf("%+v", result),
		fmt.Sprintf("%#v", result),
	)

	forbidden := []string{testPIN, testProductID, testUser, testIBAN, testIBAN2}
	for _, text := range formatted {
		for _, value := range forbidden {
			if strings.Contains(text, value) {
				t.Fatalf("formatted result leaked %q: %s", value, text)
			}
		}
	}
}

func TestProbeWipesPINOnEveryPath(t *testing.T) {
	t.Run("GO", func(t *testing.T) {
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), newProbeEnv(t, depotGoBank(t), &probeInteraction{}, pin))
		if !result.Decision.Go {
			t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})

	t.Run("NO-GO", func(t *testing.T) {
		bank := newFakeTransport(t)
		bank.expect(requestShape("HKIDN", "HKVVB", "HKSYN"))
		bank.replyError(errors.New("network down"))
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), newProbeEnv(t, bank, &probeInteraction{}, pin))
		if result.Decision.Go {
			t.Fatal("transport failure must not be a GO")
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})

	t.Run("invalid", func(t *testing.T) {
		pin := secret.NewText([]byte(testPIN))
		result := Probe(context.Background(), ProbeEnv{
			Inputs:      Inputs{ProductID: testProductID, BLZ: testBLZ, User: testUser, Days: 30},
			PIN:         pin,
			Interaction: nil,
		})
		if !result.Invalid {
			t.Fatal("nil interaction must make the result invalid")
		}
		if !pin.IsEmpty() {
			t.Fatal("Probe did not wipe the PIN")
		}
	})
}

// A TAN prompt inside the workflow that returns neither a TAN nor an error
// (a dismissed dialog in the app) ends the dialog without submitting a TAN.
func TestProbeDismissedTANInsideWorkflow(t *testing.T) {
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKSAL", "HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN manuell", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:J+HKKAZ:N+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(tanRequiredResponse(testDialogID, "task-sal", testChallenge))
	bank.reply(successResponse(testDialogID))

	interaction := &probeInteraction{
		tan: func(context.Context, string) (*secret.Text, error) {
			return nil, nil
		},
	}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a dismissed TAN must not be a GO")
	}
	if got := result.Decision.Reason.String(); got != "other: Cancelled" {
		t.Fatalf("reason = %q, want other: Cancelled", got)
	}
	if len(bank.records) != 6 {
		t.Fatalf("exchanges = %d, want 6 ending with HKEND", len(bank.records))
	}
	if fmt.Sprint(bank.records[5]) != fmt.Sprint(requestShape("HKEND")) {
		t.Fatalf("last exchange = %v, want HKEND (no TAN submitted)", bank.records[5])
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

// enableApprovalRetry switches the approval retry on for one test.
func enableApprovalRetry(t *testing.T) {
	t.Helper()
	previous := ApprovalRetryEnabled
	ApprovalRetryEnabled = true
	t.Cleanup(func() { ApprovalRetryEnabled = previous })
}

func TestProbeDoesNotRetryWhileTheApprovalRetryIsOff(t *testing.T) {
	if ApprovalRetryEnabled {
		t.Fatal("the approval retry must be off by default")
	}
}

// setStrongSyncFallback switches the strong-sync fallback for one test.
func setStrongSyncFallback(t *testing.T, enabled bool) {
	t.Helper()
	previous := StrongSyncFallbackEnabled
	StrongSyncFallbackEnabled = enabled
	t.Cleanup(func() { StrongSyncFallbackEnabled = previous })
}

// hisynSuccessResponse answers with a HISYN system id and a success code.
func hisynSuccessResponse(dialogID, systemID string) string {
	return responseMessage(
		dialogID,
		"HISYN:2:4:1+"+systemID+"'",
		"HIRMG:2:2+0010::"+testMessage+"'",
	)
}

// strongSyncTypedBank scripts the HVB-like 9210 followed by one strong sync
// with a typed TAN and a complete GO run.
func strongSyncTypedBank(t *testing.T) *fakeTransport {
	t.Helper()
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
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
	bank.reply(approvalRequiredResponse(testDialogID))
	bank.reply(tanRequiredResponse("dialog-strong", "task-strong", testChallenge))
	bank.reply(hisynSuccessResponse("dialog-strong", "strong-system"))
	bank.reply(successResponse("dialog-strong"))
	bank.reply(executedResponse(testDialogID))
	bank.reply(executedResponse(testDialogID, hispa(
		hispaAccount(testIBAN, "TESTDEFFXXX", "00000001"),
	)))
	bank.reply(executedResponse(testDialogID, hisalPayload()))
	bank.reply(executedResponse(testDialogID, hikazPayload(mt940Booked(), mt940Pending())))
	bank.reply(successResponse(testDialogID))
	return bank
}

func TestProbeNormalLoginDoesNotTryAStrongSync(t *testing.T) {
	setStrongSyncFallback(t, true)
	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, depotGoBank(t), interaction, pin))

	if !result.Decision.Go {
		t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
	}
	if result.Path != ProbePathNormal {
		t.Fatalf("path = %s, want normal", result.Path)
	}
	if result.StrongSyncTAN != "" {
		t.Fatalf("StrongSyncTAN = %q, want empty", result.StrongSyncTAN)
	}
	if len(interaction.notices) != 0 {
		t.Fatalf("notices = %v, want none", interaction.notices)
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeStrongSyncTypedTANFallback(t *testing.T) {
	setStrongSyncFallback(t, true)
	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	strongHKIDN := ""
	initHKIDN := ""
	capture := &captureTransport{
		fakeTransport: strongSyncTypedBank(t),
		onExchange: func(call int, request []byte) {
			if call != 3 && call != 6 {
				return
			}
			segments, err := flattenRequest(request)
			if err != nil {
				t.Fatalf("inspect request %d: %v", call, err)
			}
			for _, segment := range segments {
				if segment.Type() != "HKIDN" {
					continue
				}
				if call == 3 {
					strongHKIDN = segment.DEG(3).Text(0)
				} else {
					initHKIDN = segment.DEG(3).Text(0)
				}
			}
		},
	}

	result := Probe(context.Background(), newProbeEnv(t, capture, interaction, pin))
	if !result.Decision.Go {
		t.Fatalf("decision = %q, want GO", result.Decision.Reason.String())
	}
	if result.Path != ProbePathStrongSync {
		t.Fatalf("path = %s, want strong sync", result.Path)
	}
	if result.StrongSyncTAN != StrongSyncTANTyped {
		t.Fatalf("StrongSyncTAN = %q, want typed", result.StrongSyncTAN)
	}
	if len(interaction.notices) != 1 || interaction.notices[0] != NoticeStrongSync {
		t.Fatalf("notices = %v, want [%v]", interaction.notices, NoticeStrongSync)
	}
	if fmt.Sprint(interaction.tans) != fmt.Sprint([]string{testChallenge}) {
		t.Fatalf("TAN challenges = %v, want [%q]", interaction.tans, testChallenge)
	}
	if strongHKIDN != "0" {
		t.Fatalf("strong sync HKIDN system id = %q, want 0", strongHKIDN)
	}
	if initHKIDN != "strong-system" {
		t.Fatalf("Init after strong sync HKIDN system id = %q, want strong-system", initHKIDN)
	}
	if got := result.PathLine(); got != "Path: strong sync (TAN in the sync dialog), typed" {
		t.Fatalf("path line = %q", got)
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeStrongSyncIsBlockedByAPINOrLockCode(t *testing.T) {
	setStrongSyncFallback(t, true)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN manuell", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(responseMessage(
		testDialogID,
		"HIRMG:2:2+9050::"+testMessage+"'",
		"HIRMS:3:2:4+9210::"+testMessage+"+9942::"+testMessage+"'",
	))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a PIN code must not be a GO")
	}
	if result.Path != ProbePathNormal {
		t.Fatalf("path = %s, want normal", result.Path)
	}
	if len(interaction.notices) != 0 {
		t.Fatalf("notices = %v, want none", interaction.notices)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want 3", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeStrongSyncFailureIsANoGoWithTheBankCodes(t *testing.T) {
	setStrongSyncFallback(t, true)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN manuell", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(approvalRequiredResponse(testDialogID))
	bank.reply(approvalRequiredResponse("dialog-strong"))
	bank.reply(successResponse("dialog-strong"))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a failed strong sync must not be a GO")
	}
	if result.Path != ProbePathStrongSync {
		t.Fatalf("path = %s, want strong sync", result.Path)
	}
	if len(interaction.notices) != 1 || interaction.notices[0] != NoticeStrongSync {
		t.Fatalf("notices = %v, want [%v]", interaction.notices, NoticeStrongSync)
	}
	reason := result.Decision.Reason.String()
	if !strings.Contains(reason, "9050") || !strings.Contains(reason, "9210") {
		t.Fatalf("reason = %q, want the bank's 9050 and 9210", reason)
	}
	strongSyncs := 0
	for _, record := range bank.records {
		if strings.Join(record, ",") == strings.Join(requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"), ",") {
			strongSyncs++
		}
	}
	if strongSyncs != 1 {
		t.Fatalf("strong sync attempts = %d, want exactly 1", strongSyncs)
	}
	if len(bank.records) != 5 {
		t.Fatalf("exchanges = %d, want the strong sync and its best-effort HKEND", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestProbeStrongSyncFallbackOffKeepsTodaysBehaviour(t *testing.T) {
	setStrongSyncFallback(t, false)
	bank := newFakeTransport(t)
	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(
		chipHitans("chipTAN manuell", 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hiupd(testIBAN, "00000001", "EUR"),
	))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(approvalRequiredResponse(testDialogID))

	interaction := &probeInteraction{}
	pin := secret.NewText([]byte(testPIN))
	result := Probe(context.Background(), newProbeEnv(t, bank, interaction, pin))

	if result.Decision.Go {
		t.Fatal("a rejected login must not be a GO")
	}
	if result.Path != ProbePathNormal {
		t.Fatalf("path = %s, want normal", result.Path)
	}
	if len(interaction.notices) != 0 {
		t.Fatalf("notices = %v, want none", interaction.notices)
	}
	if len(bank.records) != 3 {
		t.Fatalf("exchanges = %d, want 3", len(bank.records))
	}
	if !pin.IsEmpty() {
		t.Fatal("Probe did not wipe the PIN")
	}
}

func TestStrongSyncFallbackIsOnByDefault(t *testing.T) {
	if !StrongSyncFallbackEnabled {
		t.Fatal("the strong-sync fallback must be on by default")
	}
}

func TestShouldTryStrongSyncNeverRepeatsThePINAfterASecurityCode(t *testing.T) {
	params := &fints.BankParams{SelectedSecurityFunction: "942"}
	if !shouldTryStrongSync(&fints.DialogError{Codes: []string{"9050", "9210"}}, params) {
		t.Fatal("9050 + 9210 must allow the strong sync")
	}
	for _, blocked := range []string{"9340", "9942", "3938", "9301", "9399", "9931", "9950", "9999"} {
		if shouldTryStrongSync(&fints.DialogError{Codes: []string{"9210", blocked}}, params) {
			t.Fatalf("9210 + %s must not start a strong sync", blocked)
		}
	}
}
