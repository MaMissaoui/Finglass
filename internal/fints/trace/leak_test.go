package trace

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

// The leak test's distinct marker for every masked field.
const (
	leakPIN          = "PIN73?'+:@-1052846"
	leakTAN          = "TAN84?'+:@-02163957"
	leakUserID       = "USER9513274068"
	leakCustomerID   = "CUST0624385179"
	leakProductID    = "PROD1735496280"
	leakSystemID     = "SYST2846507391"
	leakDialogID     = "DIAL3957618402"
	leakIBAN         = "DE44556677889900110001"
	leakBIC          = "TESTDEFFXXX"
	leakBLZ          = "99999999"
	leakAccount      = "ACCT4068729513"
	leakSubAccount   = "SUB5179830624"
	leakOwner        = "OWNER6280941735"
	leakProductName  = "PNAME7392052846"
	leakAmount       = "9876543210,99"
	leakPurpose      = "PURPOSE8403163957"
	leakCounterparty = "COUNTER9514274068"
	leakChallenge    = "CHALLENGE0625385179"
	leakTaskRef      = "TASKREF1736496280"
	leakTouchdown    = "TOUCHDOWN2847507391"
	leakBankText     = "BANKTEXT3958618402"
	leakBankName     = "BANKNAME4069729513"
	leakMethodName   = "METHODNAME5170830624"
	leakCamtURN      = "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"
)

var leakMarkers = []string{
	leakPIN, leakTAN, leakUserID, leakCustomerID, leakProductID, leakSystemID,
	leakDialogID, leakIBAN, leakAccount, leakSubAccount, leakOwner, leakProductName,
	leakAmount, leakPurpose, leakCounterparty, leakChallenge, leakTaskRef,
	leakTouchdown, leakBankText, leakBankName, leakMethodName,
}

type scriptedBank struct {
	responses [][]byte
	errors    []error
}

func (b *scriptedBank) Exchange(_ context.Context, _ []byte) ([]byte, error) {
	if len(b.responses) == 0 {
		return nil, nil
	}
	response := b.responses[0]
	b.responses = b.responses[1:]
	if len(b.errors) > 0 {
		err := b.errors[0]
		b.errors = b.errors[1:]
		return response, err
	}
	return response, nil
}

func buildLeakRequest(t *testing.T, segments ...fints.Segment) []byte {
	t.Helper()
	pin := secret.NewText([]byte(leakPIN))
	defer pin.Wipe()
	tan := secret.NewText([]byte(leakTAN))
	defer tan.Wipe()

	message, err := fints.BuildMessage(fints.MessageInput{
		BLZ:              leakBLZ,
		UserID:           leakUserID,
		CustomerID:       leakCustomerID,
		SystemID:         leakSystemID,
		DialogID:         leakDialogID,
		MessageNumber:    1,
		SecurityFunction: "999",
		PIN:              pin,
		TAN:              tan,
		Source: fints.Source{
			Now: func() time.Time {
				return time.Date(2026, 3, 29, 12, 34, 56, 0, time.UTC)
			},
			SecurityReference: func() (uint32, error) { return 1_234_567, nil },
		},
	}, segments...)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	return message
}

func leakResponse(dialogID string, segments ...string) []byte {
	return []byte("HNHBK:1:3+000000000000+300+" + dialogID + "+1'" +
		strings.Join(segments, "") +
		"HNHBS:99:1+1'")
}

func binaryValue(data string) string {
	return "@" + strconv.Itoa(len(data)) + "@" + data
}

func escapedURN(urn string) string {
	return strings.ReplaceAll(urn, ":", "?:")
}

func TestRecorderLeakTest(t *testing.T) {
	account := fints.Account{
		AccountNumber: leakAccount,
		SubAccount:    leakSubAccount,
		BLZ:           leakBLZ,
		IBAN:          leakIBAN,
		BIC:           leakBIC,
	}
	start := fints.Date{Year: 2026, Month: 1, Day: 1}
	end := fints.Date{Year: 2026, Month: 3, Day: 31}
	touchdown := leakTouchdown

	requests := [][]byte{
		buildLeakRequest(t, fints.NewHKSYN()),
		buildLeakRequest(t,
			fints.NewHKIDN(leakBLZ, leakCustomerID, leakSystemID),
			fints.NewHKVVB(42, 55, leakProductID, "1"),
			fints.NewHKTANProcess4(7, fints.TanRefHKIDN, nil),
		),
		buildLeakRequest(t, fints.NewHKSPA(3)),
		buildLeakRequest(t, fints.NewHKSAL(6, account, nil)),
		buildLeakRequest(t, fints.NewHKKAZ(6, account, start, end, &touchdown)),
		buildLeakRequest(t, fints.NewHKKAZ(6, account, start, end, &touchdown)),
		buildLeakRequest(t, fints.NewHKCAZ(1, account, []string{leakCamtURN}, false, start, end, nil, &touchdown)),
		buildLeakRequest(t, fints.NewHKTAB(6)),
		buildLeakRequest(t, fints.NewHKTANProcess2(6, leakTaskRef, nil)),
		buildLeakRequest(t, fints.NewHKEND(leakDialogID)),
	}

	mt940 := ":20:STARTUMS\n:25:99999999/" + leakAccount + "\n" +
		":61:2603150315C1234,56NTRF\n" +
		":86:" + leakPurpose + " " + leakCounterparty + "\n"
	camt := "<Document><BkToCstmrStmt><Stmt><Ntry><Amt>" + leakAmount +
		"</Amt><NtryDtls><TxDtls><RltdPties><Cdtr><Nm>" + leakCounterparty +
		"</Nm></Cdtr></RltdPties></TxDtls></NtryDtls></Ntry></Stmt></BkToCstmrStmt></Document>"

	hitans := "HITANS:7:7:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:" +
		leakMethodName + ":6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:1:1'"
	hirms := "HIRMS:9:2:4+0010::" + leakBankText + " ACCOUNT1234567890'"
	hibpa := "HIBPA:3:3+42+280:" + leakBLZ + "+" + leakBankName + "+1+1+0'"
	hiupa := "HIUPA:4:4+" + leakUserID + "+55+1'"
	hiupd := "HIUPD:5:6+" + leakAccount + ":" + leakSubAccount + ":280:" + leakBLZ +
		"+" + leakIBAN + "+" + leakCustomerID + "+1+EUR+" + leakOwner +
		"++" + leakProductName + "+1234,56+HKKAZ'"
	hispa := "HISPA:6:6+J:" + leakIBAN + ":" + leakBIC + ":" + leakAccount + ":" +
		leakSubAccount + ":280:" + leakBLZ + "'"
	hisal := "HISAL:10:7:4+" + leakAccount + ":" + leakSubAccount + ":280:" + leakBLZ +
		"+" + leakProductName + "+EUR+C:" + leakAmount + ":EUR:20260315+" +
		"D:25,50:EUR:20260314+5000,00+900,00'"
	hikaz := "HIKAZ:11:7:4+" + binaryValue(mt940) + "+" + binaryValue("") + "'"
	hicaz := "HICAZ:12:1:3+" + leakIBAN + ":" + leakBIC + "+" + escapedURN(leakCamtURN) +
		"+" + binaryValue(camt) + "+" + binaryValue("") + "'"
	hitan := "HITAN:13:7:3+4+task-hash+" + leakTaskRef + "+" + leakChallenge + "+@0@'"
	hitab := "HITAB:14:6:4+A+1:1:" + leakMethodName + "'"

	responses := [][]byte{
		leakResponse(leakDialogID, "HISYN:2:4+"+leakSystemID+"'", hibpa, hiupa, hitans,
			"HIRMG:8:2+0010::OK+3920::Allowed:942:962'"),
		leakResponse(leakDialogID, hirms, hitan),
		leakResponse(leakDialogID, hispa, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, hisal, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, hikaz, "HIRMS:9:2:4+3040::More:Next'"),
		leakResponse(leakDialogID, hikaz, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, hicaz, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, hitab, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, "HIRMG:8:2+0010::OK'"),
		leakResponse(leakDialogID, "HIRMG:8:2+0010::OK'"),
		// HIUPD arrives with one of the responses above in a real dialog; keep
		// it here so the trace carries the owner, product name and limit.
		leakResponse(leakDialogID, hiupd, "HIRMG:8:2+0010::OK'"),
	}

	bank := &scriptedBank{responses: responses}
	recorder := NewRecorder(bank, WithClock(func() time.Time { return time.Unix(0, 0) }))

	for _, request := range requests {
		if _, err := recorder.Exchange(context.Background(), request); err != nil {
			t.Fatalf("Exchange: %v", err)
		}
	}
	// Drain the last scripted response (the HIUPD one).
	if err := drainOne(context.Background(), recorder, requests[0]); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	var output strings.Builder
	if _, err := recorder.WriteTo(&output); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	traceText := output.String()

	for _, marker := range leakMarkers {
		assertMarkerAbsent(t, traceText, marker)
	}
	assertMarkerAbsent(t, traceText, leakBankText)
	assertMarkerAbsent(t, traceText, "ACCOUNT1234567890")

	for _, expected := range []string{
		"HNHBK:1:3", "HNVSK:998:3", "HNSHK:2:4", "HNVSD:999:1",
		"HISYN:2:4", "HIBPA:3:3", "HIUPA:4:4", "HITANS:7:7",
		"HIUPD:5:6", "HISPA:6:6", "HIRMG:8:2", "HIRMS:9:2:4",
		"HISAL:10:7", "HIKAZ:11:7", "HICAZ:12:1", "HITAN:13:7",
		"HITAB:14:6",
		"0010", "3920", "942", "962",
		leakBLZ, leakBIC, "…0001", leakCamtURN,
		"20260101", "20260331", "20260315",
		"<mt940 ", "bookings>", "<camt ", "entries>",
	} {
		if !strings.Contains(traceText, expected) {
			t.Fatalf("trace does not contain %q:\n%s", expected, traceText)
		}
	}

	// Segment numbers in requests grow with the number of business segments;
	// check the type and version with a pattern rather than an exact number.
	for _, pattern := range []string{
		`HKIDN:\d+:2`, `HKVVB:\d+:3`, `HKSYN:\d+:3`, `HKSPA:\d+:3`,
		`HKSAL:\d+:6`, `HKKAZ:\d+:6`, `HKCAZ:\d+:1`, `HKTAB:\d+:6`,
		`HKTAN:\d+:7`, `HKTAN:\d+:6`, `HKEND:\d+:1`, `HNSHA:\d+:2`,
		`HNHBS:\d+:1`,
	} {
		if !regexp.MustCompile(pattern).MatchString(traceText) {
			t.Fatalf("trace does not match %q:\n%s", pattern, traceText)
		}
	}
}

func drainOne(ctx context.Context, recorder *Recorder, request []byte) error {
	_, err := recorder.Exchange(ctx, request)
	return err
}

func assertMarkerAbsent(t *testing.T, output, marker string) {
	t.Helper()
	candidates := []string{marker, escapeFinTS(marker)}
	if len(marker) > 8 {
		candidates = append(candidates,
			marker[:5],
			marker[len(marker)/2-2:len(marker)/2+3],
		)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.Contains(output, candidate) {
			t.Fatalf("trace leaks %q (from marker %q):\n%s", candidate, marker, output)
		}
	}
}

func escapeFinTS(value string) string {
	replacer := strings.NewReplacer("?", "??", "'", "?'", "+", "?+", ":", "?:", "@", "?@")
	return replacer.Replace(value)
}
