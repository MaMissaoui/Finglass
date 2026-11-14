package fints

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const strongSyncSystemID = "strong-system"

// hisynResponse answers with a HISYN system id and a success code.
func hisynResponse(dialogID, systemID string) string {
	return responseMessage(
		dialogID,
		"HISYN:2:4:1+"+systemID+"'",
		"HIRMG:2:2+0010::OK'",
	)
}

// capturingBank parses each request before the dialog wipes it.
type capturingBank struct {
	*fakeBank
	segments [][]ParsedSegment
}

func newCapturingBank(t *testing.T) *capturingBank {
	t.Helper()
	return &capturingBank{fakeBank: newFakeBank(t)}
}

func (b *capturingBank) Exchange(ctx context.Context, request []byte) ([]byte, error) {
	copied := append([]byte(nil), request...)
	outer, err := ParseMessage(copied)
	Wipe(copied)
	if err != nil {
		b.t.Fatalf("ParseMessage: %v", err)
	}
	segments, err := flattenResponses(outer, 0)
	if err != nil {
		b.t.Fatalf("flattenResponses: %v", err)
	}
	b.segments = append(b.segments, segments)
	return b.fakeBank.Exchange(ctx, request)
}

func findRequestSegment(t *testing.T, segments []ParsedSegment, segmentType string) ParsedSegment {
	t.Helper()
	for _, segment := range segments {
		if segment.Type() == segmentType {
			return segment
		}
	}
	t.Fatalf("request has no %s", segmentType)
	return ParsedSegment{}
}

func segmentTypes(segments []ParsedSegment) []string {
	types := make([]string, 0, len(segments))
	for _, segment := range segments {
		types = append(types, segment.Type())
	}
	return types
}

func newStrongSyncDialog(t *testing.T, bank *fakeBank, extra ...string) *Dialog {
	t.Helper()
	body := syncResponse(append([]string{
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
	}, extra...)...)
	return newSyncedDialog(t, bank, body)
}

func TestDialogStrongSyncMessageShapeAndDirectFinish(t *testing.T) {
	bank := newCapturingBank(t)
	dialog := newStrongSyncDialog(t, bank.fakeBank)
	dialog.transport = bank
	// An earlier sync's id is carried in Params; the strong sync must not use it.
	dialog.params.SystemID = "old-system"
	dialog.systemID = "old-system"

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N"),
	))
	bank.reply(successResponse("dialog-strong"))
	bank.reply(executedResponse("dialog-init"))

	result, err := dialog.StrongSync(context.Background())
	if err != nil {
		t.Fatalf("StrongSync: %v", err)
	}
	if !result.Done {
		t.Fatalf("StrongSync result = %v, want Done", result)
	}
	if dialog.State() != DialogSynced {
		t.Fatalf("state = %s, want synced", dialog.State())
	}
	if dialog.SystemID() != testSystemID {
		t.Fatalf("SystemID = %q, want %q", dialog.SystemID(), testSystemID)
	}

	segments := bank.segments[0]
	wantShape := strings.Join(requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"), ",")
	if got := strings.Join(segmentTypes(segments), ","); got != wantShape {
		t.Fatalf("strong sync segments = %s, want %s", got, wantShape)
	}
	hkidn := findRequestSegment(t, segments, "HKIDN")
	if got := hkidn.DEG(3).Text(0); got != "0" {
		t.Fatalf("HKIDN system id = %q, want 0", got)
	}
	for _, header := range []struct {
		segmentType string
		index       int
	}{
		{"HNVSK", 4},
		{"HNSHK", 6},
	} {
		group := findRequestSegment(t, segments, header.segmentType).DEG(header.index)
		if group.Len() != 3 || group.Text(2) != "0" {
			t.Fatalf("%s security identification system id = %q, want 0",
				header.segmentType, group.Text(2))
		}
	}
	if got := findRequestSegment(t, segments, "HNSHK").DEG(1).Text(1); got != "2" {
		t.Fatalf("HNSHK security profile = %q, want 2", got)
	}
	hktan := findRequestSegment(t, segments, "HKTAN")
	if got := hktan.DEG(1).Text(0); got != "4" {
		t.Fatalf("HKTAN process = %q, want 4", got)
	}
	if got := hktan.DEG(2).Text(0); got != "HKIDN" {
		t.Fatalf("HKTAN reference = %q, want HKIDN", got)
	}
	if got := bank.records[0].securityFunction; got != "942" {
		t.Fatalf("security function = %q, want 942", got)
	}

	// Init follows on the same dialog and carries exactly the HISYN id.
	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init after StrongSync: %v", err)
	}
	initSegments := bank.segments[2]
	initHKIDN := findRequestSegment(t, initSegments, "HKIDN")
	if got := initHKIDN.DEG(3).Text(0); got != testSystemID {
		t.Fatalf("Init HKIDN system id = %q, want %q", got, testSystemID)
	}
	bank.assertRequestsWiped(t)
}

func TestDialogStrongSyncWithoutHISYNDoesNotReuseAnOldSystemID(t *testing.T) {
	bank := newCapturingBank(t)
	dialog := newStrongSyncDialog(t, bank.fakeBank)
	dialog.transport = bank
	dialog.params.SystemID = "old-system"
	dialog.systemID = "old-system"

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKEND"),
	)
	bank.reply(responseMessage("dialog-strong", "HIRMG:2:2+0010::OK'"))
	bank.reply(successResponse("dialog-strong"))

	_, err := dialog.StrongSync(context.Background())
	dialogError := assertDialogErrorKind(t, err, ErrorKindProtocol)
	if len(dialogError.Codes) != 0 {
		t.Fatalf("protocol error codes = %v, want none", dialogError.Codes)
	}
	if dialog.SystemID() == "old-system" {
		t.Fatal("StrongSync kept the old system id")
	}
	if dialog.State() != DialogEnded {
		t.Fatalf("state = %s, want ended", dialog.State())
	}

	segments := bank.segments[0]
	hkidn := findRequestSegment(t, segments, "HKIDN")
	if got := hkidn.DEG(3).Text(0); got != "0" {
		t.Fatalf("HKIDN system id = %q, want 0", got)
	}
	bank.assertRequestsWiped(t)
}

func TestDialogStrongSyncTypedTANThenSubmit(t *testing.T) {
	t.Run("HISYN after the TAN", func(t *testing.T) {
		bank := newFakeBank(t)
		dialog := newStrongSyncDialog(t, bank)

		bank.expect(
			requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
			requestShape("HKTAN"),
			requestShape("HKEND"),
		)
		bank.reply(tanRequiredResponse("dialog-strong", "task-strong", "strong challenge"))
		bank.reply(hisynResponse("dialog-strong", strongSyncSystemID))
		bank.reply(successResponse("dialog-strong"))

		result, err := dialog.StrongSync(context.Background())
		if err != nil {
			t.Fatalf("StrongSync: %v", err)
		}
		if result.Done || result.Decoupled {
			t.Fatalf("StrongSync result = %v, want a typed TAN", result)
		}
		if result.Challenge.TaskReference != "task-strong" {
			t.Fatalf("task reference = %q", result.Challenge.TaskReference)
		}
		if dialog.State() != DialogTANPending {
			t.Fatalf("state = %s, want tan-pending", dialog.State())
		}

		tan := secret.NewText([]byte(testTAN))
		response, err := dialog.SubmitTAN(context.Background(), tan)
		if err != nil {
			t.Fatalf("SubmitTAN: %v", err)
		}
		if response == nil {
			t.Fatal("SubmitTAN returned no response")
		}
		if !tan.IsEmpty() {
			t.Fatal("SubmitTAN did not wipe the TAN")
		}
		if dialog.State() != DialogSynced {
			t.Fatalf("state = %s, want synced", dialog.State())
		}
		if dialog.SystemID() != strongSyncSystemID {
			t.Fatalf("SystemID = %q, want %q", dialog.SystemID(), strongSyncSystemID)
		}
		bank.assertRequestsWiped(t)
	})

	t.Run("no HISYN after the TAN", func(t *testing.T) {
		bank := newFakeBank(t)
		dialog := newStrongSyncDialog(t, bank)

		bank.expect(
			requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
			requestShape("HKTAN"),
			requestShape("HKEND"),
		)
		bank.reply(tanRequiredResponse("dialog-strong", "task-strong", "strong challenge"))
		bank.reply(successResponse("dialog-strong"))
		bank.reply(successResponse("dialog-strong"))

		if _, err := dialog.StrongSync(context.Background()); err != nil {
			t.Fatalf("StrongSync: %v", err)
		}
		tan := secret.NewText([]byte(testTAN))
		_, err := dialog.SubmitTAN(context.Background(), tan)
		_ = assertDialogErrorKind(t, err, ErrorKindProtocol)
		if !tan.IsEmpty() {
			t.Fatal("SubmitTAN did not wipe the TAN")
		}
		if dialog.State() != DialogEnded {
			t.Fatalf("state = %s, want ended", dialog.State())
		}
		bank.assertRequestsWiped(t)
	})
}

func TestDialogStrongSyncDecoupledThenPoll(t *testing.T) {
	bank := newFakeBank(t)
	sleeper := &fakeSleeper{}
	dialog := newStrongSyncDialog(t, bank)
	dialog.sleeper = sleeper

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKTAN"),
		requestShape("HKEND"),
	)
	bank.reply(decoupledResponse("dialog-strong", "task-strong", "strong challenge"))
	bank.reply(hisynResponse("dialog-strong", strongSyncSystemID))
	bank.reply(successResponse("dialog-strong"))

	result, err := dialog.StrongSync(context.Background())
	if err != nil {
		t.Fatalf("StrongSync: %v", err)
	}
	if result.Done || !result.Decoupled {
		t.Fatalf("StrongSync result = %v, want a decoupled TAN", result)
	}
	if dialog.State() != DialogTANPending {
		t.Fatalf("state = %s, want tan-pending", dialog.State())
	}

	pollResult, err := dialog.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !pollResult.Confirmed {
		t.Fatal("Poll did not confirm")
	}
	if dialog.State() != DialogSynced {
		t.Fatalf("state = %s, want synced", dialog.State())
	}
	if dialog.SystemID() != strongSyncSystemID {
		t.Fatalf("SystemID = %q, want %q", dialog.SystemID(), strongSyncSystemID)
	}
	bank.assertRequestsWiped(t)
}

func TestDialogStrongSyncBankError(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newStrongSyncDialog(t, bank)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKEND"),
	)
	bank.reply(responseMessage(
		"dialog-strong",
		"HIRMG:2:2+9050::Kein Auftrag'",
		"HIRMS:3:2:4+9210::Kein Auftrag'",
	))
	bank.reply(successResponse("dialog-strong"))

	_, err := dialog.StrongSync(context.Background())
	dialogError := assertDialogErrorKind(t, err, ErrorKindPinRejected)
	if got := strings.Join(dialogError.Codes, ","); got != "9050,9210" {
		t.Fatalf("codes = %s, want 9050,9210", got)
	}
	if dialog.State() != DialogEnded {
		t.Fatalf("state = %s, want ended", dialog.State())
	}
	// 9050/9210 are not PIN codes, so HKEND still goes out best effort.
	bank.assertSegmentTypes(t,
		requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"),
		requestShape("HKEND"),
	)
	bank.assertRequestsWiped(t)
}

func TestDialogStrongSyncPINErrorSendsNoHKEND(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newStrongSyncDialog(t, bank)

	bank.expect(requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"))
	bank.reply(codeResponse("dialog-strong", "9942"))

	_, err := dialog.StrongSync(context.Background())
	_ = assertDialogErrorKind(t, err, ErrorKindPinRejected)
	// No HKEND: it would be one more message signed with the rejected PIN.
	bank.assertSegmentTypes(t, requestShape("HKIDN", "HKVVB", "HKTAN", "HKSYN"))
	bank.assertRequestsWiped(t)
}

func TestDialogStrongSyncRejectsAPINOnlySecurityFunctionWithoutSending(t *testing.T) {
	for _, securityFunction := range []string{"", SecurityFunctionPINOnly} {
		t.Run("selected "+securityFunction, func(t *testing.T) {
			bank := newFakeBank(t)
			dialog := newStrongSyncDialog(t, bank)
			dialog.params.SelectedSecurityFunction = securityFunction

			_, err := dialog.StrongSync(context.Background())
			_ = assertDialogErrorKind(t, err, ErrorKindInvalidState)
			if len(bank.records) != 0 {
				t.Fatalf("an invalid strong sync sent %d exchanges", len(bank.records))
			}
		})
	}
}

func TestSyncResultFormattingHidesTheChallenge(t *testing.T) {
	result := SyncResult{
		Challenge: TanChallenge{TaskReference: "task-123", challenge: "SECRETCHALLENGE"},
	}
	for _, formatted := range []string{
		result.String(),
		result.GoString(),
		fmt.Sprintf("%v", result),
		fmt.Sprintf("%+v", result),
		fmt.Sprintf("%#v", result),
		fmt.Sprintf("%s", result),
	} {
		if strings.Contains(formatted, "SECRETCHALLENGE") || strings.Contains(formatted, "task-123") {
			t.Fatalf("formatted SyncResult leaked the challenge: %s", formatted)
		}
	}
}
