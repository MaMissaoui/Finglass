package fintsprobe

import (
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
	tan           func(ctx context.Context, challenge string) (*secret.Text, error)
	confirm       func(ctx context.Context, challenge string)
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
	wantTerminal := append(append([]string(nil), result.Lines...), "RESULT: GO")
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
