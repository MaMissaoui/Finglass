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

func assertDialogErrorKind(t *testing.T, err error, kind ErrorKind) *DialogError {
	t.Helper()
	var dialogError *DialogError
	if !errors.As(err, &dialogError) {
		t.Fatalf("error = %T %v, want *DialogError", err, err)
	}
	if dialogError.Kind != kind {
		t.Fatalf("error kind = %s (%v), want %s", dialogError.Kind, err, kind)
	}
	return dialogError
}

func TestDialogSync(t *testing.T) {
	bank := newFakeBank(t)
	sleeper := &fakeSleeper{}
	dialog := newTestDialog(t, bank, func(config *DialogConfig) { config.Sleeper = sleeper })

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hisals(7),
	))
	bank.reply(successResponse("dialog-sync"))

	if err := dialog.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if dialog.State() != DialogSynced {
		t.Fatalf("state = %s, want synced", dialog.State())
	}
	if dialog.SystemID() != testSystemID {
		t.Fatalf("SystemID = %q, want %q", dialog.SystemID(), testSystemID)
	}
	if dialog.DialogID() != "" {
		t.Fatalf("DialogID = %q after Sync, want empty", dialog.DialogID())
	}
	params := dialog.Params()
	if params.BPDVersion != testBPDVersion || params.UPDVersion != testUPDVersion {
		t.Fatalf("versions = %d/%d", params.BPDVersion, params.UPDVersion)
	}
	if params.SelectedSecurityFunction != "942" {
		t.Fatalf("SelectedSecurityFunction = %q, want 942", params.SelectedSecurityFunction)
	}
	if !params.IsDecoupled() {
		t.Fatal("selected method is not decoupled")
	}

	if len(bank.records) != 2 {
		t.Fatalf("exchanges = %d, want 2", len(bank.records))
	}
	if bank.records[0].messageNumber != 1 || bank.records[1].messageNumber != 2 {
		t.Fatalf("message numbers = %d/%d, want 1/2",
			bank.records[0].messageNumber, bank.records[1].messageNumber)
	}
	bank.assertRequestsWiped(t)

	// The dialog never owns the PIN and must not wipe the caller's value.
	if err := dialog.pin.Use(func(value []byte) error {
		if string(value) != testPIN {
			t.Fatalf("PIN = %q, want %q", value, testPIN)
		}
		return nil
	}); err != nil {
		t.Fatalf("caller PIN is unusable: %v", err)
	}
}

func TestDialogInitWithoutTAN(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(hipins("HKSAL:N"), hisals(7))
	dialog := newSyncedDialog(t, bank, syncBody)

	bank.expect(requestShape("HKIDN", "HKVVB", "HKTAN"))
	bank.reply(executedResponse("dialog-init"))

	result, err := dialog.Init(context.Background())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !result.Opened {
		t.Fatalf("Init result = %v, want Opened", result)
	}
	if result.Decoupled {
		t.Fatal("Init reported a decoupled TAN without one")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

func TestDialogInitWithTANThenSubmitTAN(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		chipHitans(5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
	)
	bank.reply(tanRequiredResponse("dialog-init", "task-1", "Bitte TAN eingeben"))
	bank.reply(executedResponse("dialog-init"))

	result, err := dialog.Init(context.Background())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.Opened {
		t.Fatal("Init opened without the TAN the bank asked for")
	}
	if result.Decoupled {
		t.Fatal("a chipTAN challenge was reported as decoupled")
	}
	if result.Challenge.TaskReference != "task-1" {
		t.Fatalf("task reference = %q", result.Challenge.TaskReference)
	}
	if dialog.State() != DialogTANPending {
		t.Fatalf("state = %s, want tan-pending", dialog.State())
	}

	tan := secret.NewText([]byte(testTAN))
	if _, err := dialog.SubmitTAN(context.Background(), tan); err != nil {
		t.Fatalf("SubmitTAN: %v", err)
	}
	if !tan.IsEmpty() {
		t.Fatal("SubmitTAN did not wipe the TAN")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}

	if len(bank.records) != 2 {
		t.Fatalf("exchanges = %d, want 2", len(bank.records))
	}
	if bank.records[0].messageNumber != 1 || bank.records[1].messageNumber != 2 {
		t.Fatalf("message numbers = %d/%d, want 1/2",
			bank.records[0].messageNumber, bank.records[1].messageNumber)
	}
	bank.assertRequestsWiped(t)
}

// requestHKTAN returns the HKTAN segment of a request, before the dialog wipes
// the request bytes.
func requestHKTAN(t *testing.T, request []byte) ParsedSegment {
	t.Helper()
	outer, err := ParseMessage(request)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	segments, err := flattenResponses(outer, 0)
	if err != nil {
		t.Fatalf("flattenResponses: %v", err)
	}
	return findRequestSegment(t, segments, "HKTAN")
}

// A bank that offers only HITANS v5 must see HKTAN v5's field positions: the
// TAN medium in field 12 and, for process 2, the task reference in field 5 and
// "N" in field 7.
func TestDialogHKTANv5FieldsThroughLoginAndSubmit(t *testing.T) {
	bank := newFakeBank(t)
	const (
		medium        = "Handy"
		taskReference = "task-v5"
	)
	dialog := newSyncedDialog(
		t, bank,
		syncResponse(
			v5Hitans(),
			allowedSecurityFunction("912"),
			hipins("HKSAL:N"),
			hisals(7),
		),
		func(config *DialogConfig) { config.TANMedium = medium },
	)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
	)
	bank.handler = func(call int, record fakeExchange) (string, error) {
		hktan := requestHKTAN(t, record.request)
		reject := func(problem string) (string, error) {
			t.Errorf("exchange %d HKTAN v5: %s", call, problem)
			return codeResponse("dialog-init", "9050"), nil
		}
		if got := hktan.Version(); got != 5 {
			return reject(fmt.Sprintf("version = %d, want 5", got))
		}
		switch call {
		case 0:
			// HKTAN v5 process 4: field 1=4, field 2=HKIDN, fields 3-11
			// empty, field 12=TAN medium name.
			if got := hktan.DEG(1).Text(0); got != "4" {
				return reject("field 1 TAN process = " + got)
			}
			if got := hktan.DEG(2).Text(0); got != "HKIDN" {
				return reject("field 2 segment type = " + got)
			}
			for field := 3; field <= 11; field++ {
				if got := hktan.DEG(field).Text(0); got != "" {
					return reject(fmt.Sprintf("field %d = %q, want empty", field, got))
				}
			}
			if got := hktan.DEG(12).Text(0); got != medium {
				return reject(fmt.Sprintf("field 12 TAN medium = %q, want %q", got, medium))
			}
			return tanRequiredResponseV5("dialog-init", taskReference, "Bitte TAN eingeben"), nil
		case 1:
			// HKTAN v5 process 2: field 1=2, fields 2-4 empty, field 5=task
			// reference, field 6 empty, field 7=N, fields 8-11 empty,
			// field 12=TAN medium name.
			if got := hktan.DEG(1).Text(0); got != "2" {
				return reject("field 1 TAN process = " + got)
			}
			for field := 2; field <= 4; field++ {
				if got := hktan.DEG(field).Text(0); got != "" {
					return reject(fmt.Sprintf("field %d = %q, want empty", field, got))
				}
			}
			if got := hktan.DEG(5).Text(0); got != taskReference {
				return reject(fmt.Sprintf("field 5 task reference = %q, want %q", got, taskReference))
			}
			if got := hktan.DEG(6).Text(0); got != "" {
				return reject(fmt.Sprintf("field 6 = %q, want empty", got))
			}
			if got := hktan.DEG(7).Text(0); got != "N" {
				return reject(fmt.Sprintf("field 7 further TAN follows = %q, want N", got))
			}
			for field := 8; field <= 11; field++ {
				if got := hktan.DEG(field).Text(0); got != "" {
					return reject(fmt.Sprintf("field %d = %q, want empty", field, got))
				}
			}
			if got := hktan.DEG(12).Text(0); got != medium {
				return reject(fmt.Sprintf("field 12 TAN medium = %q, want %q", got, medium))
			}
			return executedResponse("dialog-init"), nil
		}
		return "", fmt.Errorf("unexpected exchange %d", call)
	}

	result, err := dialog.Init(context.Background())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.Opened || result.Decoupled {
		t.Fatalf("Init result = %v, want a waiting TAN", result)
	}
	if result.Challenge.TaskReference != taskReference {
		t.Fatalf("task reference = %q, want %q", result.Challenge.TaskReference, taskReference)
	}
	if dialog.State() != DialogTANPending {
		t.Fatalf("state = %s, want tan-pending", dialog.State())
	}

	tan := secret.NewText([]byte(testTAN))
	if _, err := dialog.SubmitTAN(context.Background(), tan); err != nil {
		t.Fatalf("SubmitTAN: %v", err)
	}
	if !tan.IsEmpty() {
		t.Fatal("SubmitTAN did not wipe the TAN")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

func TestDialogInitDecoupledThenPoll(t *testing.T) {
	bank := newFakeBank(t)
	sleeper := &fakeSleeper{}
	syncBody := syncResponse(
		decoupledHitans(4, 7, 3),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody, func(config *DialogConfig) {
		config.Sleeper = sleeper
	})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
	)
	bank.reply(decoupledResponse("dialog-init", "task-2", "Bitte in der App bestaetigen"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init"))

	result, err := dialog.Init(context.Background())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.Opened || !result.Decoupled {
		t.Fatalf("Init result = %v, want a decoupled TAN", result)
	}

	pollResult, err := dialog.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !pollResult.Confirmed {
		t.Fatal("Poll did not confirm")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}

	wantWaits := []time.Duration{7 * time.Second, 3 * time.Second, 3 * time.Second}
	gotWaits := sleeper.durations()
	if len(gotWaits) != len(wantWaits) {
		t.Fatalf("poll waits = %v, want %v", gotWaits, wantWaits)
	}
	for index := range wantWaits {
		if gotWaits[index] != wantWaits[index] {
			t.Fatalf("poll waits = %v, want %v", gotWaits, wantWaits)
		}
	}
	bank.assertRequestsWiped(t)
}

func TestDialogPollLimitReached(t *testing.T) {
	bank := newFakeBank(t)
	sleeper := &fakeSleeper{}
	syncBody := syncResponse(
		decoupledHitans(2, 7, 3),
		allowedSecurityFunction("942"),
		hisals(7),
	)
	dialog := newSyncedDialog(t, bank, syncBody, func(config *DialogConfig) {
		config.Sleeper = sleeper
	})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(decoupledResponse("dialog-init", "task-3", "Bitte in der App bestaetigen"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(decoupledPendingResponse("dialog-init"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Poll(context.Background())
	dialogError := assertDialogErrorKind(t, err, ErrorKindPollLimit)
	if len(dialogError.Codes) != 0 {
		t.Fatalf("poll-limit codes = %v, want none", dialogError.Codes)
	}

	if got := sleeper.durations(); len(got) != 2 || got[0] != 7*time.Second || got[1] != 3*time.Second {
		t.Fatalf("poll waits = %v, want [7s 3s]", got)
	}
	// HKEND is the last exchange, sent on the error path.
	bank.assertSegmentTypes(t,
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.assertRequestsWiped(t)
}

func TestDialogPinRejectedCodes(t *testing.T) {
	for _, code := range []string{"9340", "9910", "9930", "9931", "9942", "9010"} {
		t.Run(code, func(t *testing.T) {
			bank := newFakeBank(t)
			dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N")))

			bank.expect(
				requestShape("HKIDN", "HKVVB", "HKTAN"),
			)
			bank.reply(codeResponse("dialog-init", code))

			_, err := dialog.Init(context.Background())
			dialogError := assertDialogErrorKind(t, err, ErrorKindPinRejected)
			if len(dialogError.Codes) != 1 || dialogError.Codes[0] != code {
				t.Fatalf("codes = %v, want [%s]", dialogError.Codes, code)
			}
			if dialog.State() != DialogEnded {
				t.Fatalf("state = %s, want ended", dialog.State())
			}
			// No HKEND after a PIN rejection.
			bank.assertSegmentTypes(t, requestShape("HKIDN", "HKVVB", "HKTAN"))
			bank.assertRequestsWiped(t)
		})
	}
}

func TestDialogAccountLocked(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N")))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(codeResponse("dialog-init", "3938"))

	_, err := dialog.Init(context.Background())
	dialogError := assertDialogErrorKind(t, err, ErrorKindAccountLocked)
	if len(dialogError.Codes) != 1 || dialogError.Codes[0] != "3938" {
		t.Fatalf("codes = %v, want [3938]", dialogError.Codes)
	}
	bank.assertSegmentTypes(t, requestShape("HKIDN", "HKVVB", "HKTAN"))
	bank.assertRequestsWiped(t)
}

func TestDialogSendBeforeInitIsInvalid(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newTestDialog(t, bank)

	_, err := dialog.Send(context.Background(), NewHKSAL(7, Account{IBAN: testIBAN, BIC: testBIC}, nil))
	_ = assertDialogErrorKind(t, err, ErrorKindInvalidState)
	if len(bank.records) != 0 {
		t.Fatalf("an invalid transition sent %d exchanges", len(bank.records))
	}
}

func TestDialogOtherInvalidTransitionsDoNotSend(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N")))
	dialog.state = DialogOpen

	if _, err := dialog.SubmitTAN(context.Background(), secret.NewText([]byte(testTAN))); err == nil {
		t.Fatal("SubmitTAN in Open state succeeded")
	} else {
		_ = assertDialogErrorKind(t, err, ErrorKindInvalidState)
	}
	if _, err := dialog.Poll(context.Background()); err == nil {
		t.Fatal("Poll in Open state succeeded")
	} else {
		_ = assertDialogErrorKind(t, err, ErrorKindInvalidState)
	}
	if err := dialog.Sync(context.Background()); err == nil {
		t.Fatal("Sync in Open state succeeded")
	} else {
		_ = assertDialogErrorKind(t, err, ErrorKindInvalidState)
	}
	if len(bank.records) != 0 {
		t.Fatalf("invalid transitions sent %d exchanges", len(bank.records))
	}
}

func TestDialogTransportErrorSendsHKEND(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	transportFailure := errors.New(testPIN + " " + testIBAN + " Erika Musterfrau-Geheim")
	bank.replyError(transportFailure)
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Send(context.Background(), NewHKSAL(7, Account{IBAN: testIBAN, BIC: testBIC}, nil))
	dialogError := assertDialogErrorKind(t, err, ErrorKindTransport)
	for _, forbidden := range []string{testPIN, testIBAN, "Erika Musterfrau-Geheim"} {
		for _, text := range []string{
			dialogError.Error(),
			fmt.Sprintf("%v", dialogError),
			fmt.Sprintf("%+v", dialogError),
			fmt.Sprintf("%#v", dialogError),
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("transport error leaked %q: %q", forbidden, text)
			}
		}
	}
	if dialog.State() != DialogEnded {
		t.Fatalf("state = %s, want ended", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

func TestDialogMalformedResponse(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply("this is not a FinTS message")
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Send(context.Background(), NewHKSAL(7, Account{IBAN: testIBAN, BIC: testBIC}, nil))
	_ = assertDialogErrorKind(t, err, ErrorKindProtocol)
	bank.assertRequestsWiped(t)
}

func TestDialogSubmitTANWipesTANOnTransportFailure(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(
		t, bank,
		syncResponse(chipHitans(5, 2), allowedSecurityFunction("942"), hisals(7)),
	)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(tanRequiredResponse("dialog-init", "task-4", "Bitte TAN eingeben"))
	bank.replyError(errors.New("transport failed"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tan := secret.NewText([]byte(testTAN))
	_, err := dialog.SubmitTAN(context.Background(), tan)
	_ = assertDialogErrorKind(t, err, ErrorKindTransport)
	if !tan.IsEmpty() {
		t.Fatal("SubmitTAN did not wipe the TAN after a transport failure")
	}
	bank.assertRequestsWiped(t)
}

func TestDialogCancelSendsHKEND(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(
		t, bank,
		syncResponse(decoupledHitans(3, 5, 2), allowedSecurityFunction("942"), hisals(7)),
	)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(decoupledResponse("dialog-init", "task-5", "Bitte in der App bestaetigen"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := dialog.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if dialog.State() != DialogEnded {
		t.Fatalf("state = %s, want ended", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

func TestEveryRequestUsesOnlyAllowedSegmentTypes(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init", hisalPayload()))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := dialog.Send(context.Background(), NewHKSAL(7, Account{IBAN: testIBAN, BIC: testBIC}, nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(bank.records) == 0 {
		t.Fatal("no requests were recorded")
	}
	for exchangeIndex, record := range bank.records {
		for _, segmentType := range record.types {
			if err := checkOutgoingType(segmentType); err != nil {
				t.Fatalf("exchange %d sent %q", exchangeIndex, segmentType)
			}
		}
	}
}

func TestDialogSubmitTANRejectedWhenBankAsksAgain(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(
		t, bank,
		syncResponse(chipHitans(5, 2), allowedSecurityFunction("942"), hisals(7)),
	)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(tanRequiredResponse("dialog-init", "task-again", "Bitte TAN eingeben"))
	bank.reply(tanRequiredResponse("dialog-init", "task-again", "Bitte TAN erneut eingeben"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tan := secret.NewText([]byte(testTAN))
	_, err := dialog.SubmitTAN(context.Background(), tan)
	_ = assertDialogErrorKind(t, err, ErrorKindTanRejected)
	if !tan.IsEmpty() {
		t.Fatal("the rejected TAN was not wiped")
	}
	bank.assertRequestsWiped(t)
}

func TestDialogBankErrorCarriesOnlyCodes(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(codeResponse("dialog-init", "9010"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Send(context.Background(), NewHKSAL(7, Account{IBAN: testIBAN, BIC: testBIC}, nil))
	dialogError := assertDialogErrorKind(t, err, ErrorKindBankError)
	if len(dialogError.Codes) != 1 || dialogError.Codes[0] != "9010" {
		t.Fatalf("codes = %v, want [9010]", dialogError.Codes)
	}
	// Another bank error still closes the dialog with HKEND.
	bank.assertSegmentTypes(t,
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
		requestShape("HKEND"),
	)
	bank.assertRequestsWiped(t)
}

func TestDialogPollTimingDefaultsAndCaps(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newTestDialog(t, bank)

	first, next, maxPolls := dialog.pollTiming()
	if first != 5*time.Second || next != 2*time.Second || maxPolls != 60 {
		t.Fatalf("defaults = %v/%v/%d, want 5s/2s/60", first, next, maxPolls)
	}

	dialog.params.SelectedSecurityFunction = "942"
	dialog.params.TANMethods = []TanMethod{{
		SecurityFunction:    "942",
		DecoupledMaxPolls:   500,
		WaitBeforeFirstPoll: 0,
		WaitBeforeNextPoll:  0,
		HKTANVersion:        7,
	}}
	first, next, maxPolls = dialog.pollTiming()
	if first != 5*time.Second || next != 2*time.Second || maxPolls != 120 {
		t.Fatalf("capped = %v/%v/%d, want 5s/2s/120", first, next, maxPolls)
	}

	dialog.params.TANMethods[0].WaitBeforeFirstPoll = 1
	dialog.params.TANMethods[0].WaitBeforeNextPoll = 1
	first, next, maxPolls = dialog.pollTiming()
	if first != time.Second || next != time.Second || maxPolls != 120 {
		t.Fatalf("minimum = %v/%v/%d, want 1s/1s/120", first, next, maxPolls)
	}
}
