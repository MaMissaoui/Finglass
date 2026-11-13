package fints

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

type fakePrompter struct {
	tan             *secret.Text
	challengeCalls  int
	decoupledCalls  int
	lastChallenge   TanChallenge
	lastWasDecouple bool
}

func (p *fakePrompter) Challenge(_ context.Context, challenge TanChallenge, decoupled bool) (*secret.Text, error) {
	p.challengeCalls++
	p.lastChallenge = challenge
	p.lastWasDecouple = decoupled
	return p.tan, nil
}

func (p *fakePrompter) Decoupled(_ context.Context, challenge TanChallenge) {
	p.decoupledCalls++
	p.lastChallenge = challenge
}

func TestWorkflowFetchBalance(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init", hisalPayload()))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	balance, ok, err := workflow.FetchBalance(context.Background(), Account{IBAN: testIBAN, BIC: testBIC})
	if err != nil {
		t.Fatalf("FetchBalance: %v", err)
	}
	if !ok {
		t.Fatal("FetchBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "1234.56" {
		t.Fatalf("amount = %s, want 1234.56", got)
	}
	if balance.Currency != "EUR" || !balance.CurrencyStated {
		t.Fatalf("currency = %q stated=%t", balance.Currency, balance.CurrencyStated)
	}
	if !balance.HasDate || balance.Date.String() != "20260315" {
		t.Fatalf("date = %v hasDate=%t", balance.Date, balance.HasDate)
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchBalanceWithTAN(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		chipHitans(5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:J"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody)
	prompter := &fakePrompter{tan: secret.NewText([]byte(testTAN))}
	workflow := NewWorkflow(dialog, prompter)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL", "HKTAN"),
		requestShape("HKTAN"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(tanRequiredResponse("dialog-init", "task-balance", "Bitte TAN eingeben"))
	bank.reply(executedResponse("dialog-init", hisalPayload()))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	balance, ok, err := workflow.FetchBalance(context.Background(), Account{IBAN: testIBAN, BIC: testBIC})
	if err != nil {
		t.Fatalf("FetchBalance: %v", err)
	}
	if !ok {
		t.Fatal("FetchBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "1234.56" {
		t.Fatalf("amount = %s, want 1234.56", got)
	}
	if prompter.challengeCalls != 1 {
		t.Fatalf("challenge calls = %d, want 1", prompter.challengeCalls)
	}
	if prompter.lastChallenge.TaskReference != "task-balance" {
		t.Fatalf("challenge reference = %q", prompter.lastChallenge.TaskReference)
	}
	if !prompter.tan.IsEmpty() {
		t.Fatal("the workflow did not hand the TAN to SubmitTAN, which wipes it")
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchBalanceDecoupled(t *testing.T) {
	bank := newFakeBank(t)
	sleeper := &fakeSleeper{}
	syncBody := syncResponse(
		decoupledHitans(3, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:J"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody, func(config *DialogConfig) {
		config.Sleeper = sleeper
	})
	prompter := &fakePrompter{}
	workflow := NewWorkflow(dialog, prompter)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(decoupledResponse("dialog-init", "task-app", "Bitte in der App bestaetigen"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init", hisalPayload()))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	balance, ok, err := workflow.FetchBalance(context.Background(), Account{IBAN: testIBAN, BIC: testBIC})
	if err != nil {
		t.Fatalf("FetchBalance: %v", err)
	}
	if !ok {
		t.Fatal("FetchBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "1234.56" {
		t.Fatalf("amount = %s, want 1234.56", got)
	}
	if prompter.decoupledCalls != 1 || prompter.challengeCalls != 0 {
		t.Fatalf("prompter calls = decoupled:%d challenge:%d", prompter.decoupledCalls, prompter.challengeCalls)
	}
	if got := sleeper.durations(); len(got) != 2 || got[0] != 5*time.Second || got[1] != 2*time.Second {
		t.Fatalf("poll waits = %v, want [5s 2s]", got)
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchStatementsViaCamtWithTwoTouchdowns(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		hipins("HKCAZ:N"),
		hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
	)
	dialog := newSyncedDialog(t, bank, syncBody)
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKCAZ"),
		requestShape("HKCAZ"),
		requestShape("HKCAZ"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(touchdownResponse("dialog-init", "td-1", hicazPayload("PAGE1", "PEND1")))
	bank.reply(touchdownResponse("dialog-init", "td-2", hicazPayload("PAGE2", "PEND2")))
	bank.reply(executedResponse("dialog-init", hicazPayload("PAGE3", "PEND3")))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	statements, err := workflow.FetchStatements(
		context.Background(),
		Account{IBAN: testIBAN, BIC: testBIC},
		Date{Year: 2026, Month: 1, Day: 1},
		Date{Year: 2026, Month: 3, Day: 31},
	)
	if err != nil {
		t.Fatalf("FetchStatements: %v", err)
	}
	if statements.Kind != StatementCamt {
		t.Fatalf("kind = %s, want camt", statements.Kind)
	}
	if len(statements.Camt.Booked) != 3 {
		t.Fatalf("booked documents = %d, want 3", len(statements.Camt.Booked))
	}
	for index, want := range []string{"PAGE1", "PAGE2", "PAGE3"} {
		if got := string(statements.Camt.Booked[index]); got != want {
			t.Fatalf("booked document %d = %q, want %q", index, got, want)
		}
	}
	for index, want := range []string{"PEND1", "PEND2", "PEND3"} {
		if got := string(statements.Camt.Pending[index]); got != want {
			t.Fatalf("pending document %d = %q, want %q", index, got, want)
		}
	}
	for exchangeIndex, record := range bank.records {
		for _, segmentType := range record.types {
			if segmentType == "HKKAZ" {
				t.Fatalf("exchange %d used HKKAZ although HICAZS offers camt", exchangeIndex)
			}
		}
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchStatementsViaHKKAZWithoutCamt(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		hipins("HKKAZ:N"),
		hikazs(6),
		hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.054.001.02"),
	)
	dialog := newSyncedDialog(t, bank, syncBody)
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKKAZ"),
		requestShape("HKKAZ"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(touchdownResponse("dialog-init", "td-mt940", hikazPayload("BOOK1", "PEND1")))
	bank.reply(executedResponse("dialog-init", hikazPayload("BOOK2", "PEND2")))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	statements, err := workflow.FetchStatements(
		context.Background(),
		Account{IBAN: testIBAN, BIC: testBIC},
		Date{Year: 2026, Month: 1, Day: 1},
		Date{Year: 2026, Month: 3, Day: 31},
	)
	if err != nil {
		t.Fatalf("FetchStatements: %v", err)
	}
	if statements.Kind != StatementMT940 {
		t.Fatalf("kind = %s, want mt940", statements.Kind)
	}
	if got := string(statements.MT940.Booked); got != "BOOK1BOOK2" {
		t.Fatalf("booked = %q, want BOOK1BOOK2", got)
	}
	if got := string(statements.MT940.Pending); got != "PEND1PEND2" {
		t.Fatalf("pending = %q, want PEND1PEND2", got)
	}
	for exchangeIndex, record := range bank.records {
		for _, segmentType := range record.types {
			if segmentType == "HKCAZ" {
				t.Fatalf("exchange %d used HKCAZ although no accepted camt format was offered", exchangeIndex)
			}
		}
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowTouchdownLimit(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKKAZ:N"), hikazs(6)))
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.handler = func(call int, request fakeExchange) (string, error) {
		for _, segmentType := range request.types {
			if segmentType == "HKEND" {
				return successResponse("dialog-init"), nil
			}
		}
		if call == 0 {
			return executedResponse("dialog-init"), nil
		}
		return touchdownResponse("dialog-init", "td-more", hikazPayload("PAGE", "PENDING")), nil
	}

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := workflow.FetchStatements(
		context.Background(),
		Account{IBAN: testIBAN, BIC: testBIC},
		Date{Year: 2026, Month: 1, Day: 1},
		Date{Year: 2026, Month: 3, Day: 31},
	)
	_ = assertDialogErrorKind(t, err, ErrorKindTouchdownLimit)

	statementRequests := 0
	hkendRequests := 0
	for _, record := range bank.records {
		for _, segmentType := range record.types {
			switch segmentType {
			case "HKKAZ":
				statementRequests++
			case "HKEND":
				hkendRequests++
			}
		}
	}
	if statementRequests != 100 {
		t.Fatalf("statement requests = %d, want 100", statementRequests)
	}
	if hkendRequests != 1 {
		t.Fatalf("HKEND requests = %d, want 1", hkendRequests)
	}
	if len(bank.records) != 102 {
		t.Fatalf("exchanges = %d, want 102", len(bank.records))
	}
	bank.assertRequestsWiped(t)
}

func TestDialogPollCancelledDuringWait(t *testing.T) {
	bank := newFakeBank(t)
	ctx, cancel := context.WithCancel(context.Background())
	sleeper := &fakeSleeper{}
	sleeper.onSleep = func(call int, _ context.Context) error {
		if call == 1 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	syncBody := syncResponse(decoupledHitans(4, 7, 3), allowedSecurityFunction("942"), hisals(7))
	dialog := newSyncedDialog(t, bank, syncBody, func(config *DialogConfig) {
		config.Sleeper = sleeper
	})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(decoupledResponse("dialog-init", "task-cancel", "Bitte in der App bestaetigen"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Poll(ctx)
	_ = assertDialogErrorKind(t, err, ErrorKindCancelled)
	if got := sleeper.durations(); len(got) != 2 || got[0] != 7*time.Second || got[1] != 3*time.Second {
		t.Fatalf("poll waits = %v, want [7s 3s]", got)
	}
	// HKEND still goes out although ctx is cancelled.
	bank.assertSegmentTypes(t,
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.assertRequestsWiped(t)
}

func TestWorkflowPromptCancellationEndsDialog(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		chipHitans(5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:J"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody)
	prompter := &cancellingPrompter{err: context.Canceled}
	workflow := NewWorkflow(dialog, prompter)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL", "HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(tanRequiredResponse("dialog-init", "task-cancel-prompt", "Bitte TAN eingeben"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, _, err := workflow.FetchBalance(context.Background(), Account{IBAN: testIBAN, BIC: testBIC})
	_ = assertDialogErrorKind(t, err, ErrorKindCancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	bank.assertRequestsWiped(t)
}

type cancellingPrompter struct {
	err error
}

func (p *cancellingPrompter) Challenge(context.Context, TanChallenge, bool) (*secret.Text, error) {
	return nil, p.err
}

func (p *cancellingPrompter) Decoupled(context.Context, TanChallenge) {}

func TestIsAcceptedCamtFormat(t *testing.T) {
	accepted := []string{
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.02",
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.08",
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.08",
	}
	for _, format := range accepted {
		if !isAcceptedCamtFormat(format) {
			t.Fatalf("%q was not accepted", format)
		}
	}
	rejected := []string{
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.04",
		"urn:iso:std:iso:20022:tech:xsd:camt.054.001.02",
		"",
	}
	for _, format := range rejected {
		if isAcceptedCamtFormat(format) {
			t.Fatalf("%q was accepted", format)
		}
	}
}

func TestStatementsFormattingHidesDocumentContent(t *testing.T) {
	statements := Statements{
		Kind: StatementCamt,
		MT940: MT940Statement{
			Booked:  []byte(testIBAN),
			Pending: []byte(testPIN),
		},
		Camt: CamtStatement{
			Booked:  [][]byte{[]byte(testIBAN)},
			Pending: [][]byte{[]byte(testTAN)},
		},
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, statements)
		for _, forbidden := range []string{testIBAN, testPIN, testTAN} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s leaked %q: %q", format, forbidden, got)
			}
		}
	}
}
