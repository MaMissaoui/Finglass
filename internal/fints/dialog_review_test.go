package fints

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The sync dialog is opened with security function 999, so its HKEND is
// signed with 999 too, even when the sync response selected another method.
// The business dialog then uses the selected method.
func TestDialogSyncEndUsesPINOnlySecurityFunction(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newTestDialog(t, bank)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKSYN"),
		requestShape("HKEND"),
		requestShape("HKIDN", "HKVVB", "HKTAN"),
	)
	bank.reply(syncResponse(decoupledHitans(60, 5, 2), allowedSecurityFunction("942")))
	bank.reply(successResponse("dialog-sync"))
	bank.reply(executedResponse("dialog-init"))

	if err := dialog.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for index, want := range []string{"999", "999", "942"} {
		if got := bank.records[index].securityFunction; got != want {
			t.Fatalf("exchange %d security function = %q, want %q", index, got, want)
		}
	}
	bank.assertRequestsWiped(t)
}

// HKTAN process 4 goes with the first statement page only; touchdown pages
// never ask for a second strong authentication (as in fints-rs).
func TestWorkflowStatementsSendHKTANOnFirstPageOnly(t *testing.T) {
	bank := newFakeBank(t)
	syncBody := syncResponse(
		hipins("HKCAZ:J"),
		hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
	)
	dialog := newSyncedDialog(t, bank, syncBody)
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKCAZ", "HKTAN"),
		requestShape("HKCAZ"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(touchdownResponse("dialog-init", "td-1", hicazPayload("PAGE1", "PEND1")))
	bank.reply(executedResponse("dialog-init", hicazPayload("PAGE2", "PEND2")))

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
	if len(statements.Camt.Booked) != 2 {
		t.Fatalf("booked documents = %d, want 2", len(statements.Camt.Booked))
	}
	bank.assertRequestsWiped(t)
}

// A successful HKSAL answer without HISAL is "no balance", not a protocol
// error, and the dialog stays open.
func TestWorkflowFetchBalanceAbsent(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, ok, err := workflow.FetchBalance(context.Background(), Account{IBAN: testIBAN, BIC: testBIC})
	if err != nil {
		t.Fatalf("FetchBalance: %v", err)
	}
	if ok {
		t.Fatal("FetchBalance reported a balance the bank did not send")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

// Ingesting the same parameters twice (sync, then init) and HIUPD plus HISPA
// for the same account never lists a TAN method or an account twice.
func TestBankParamsIngestDoesNotDuplicate(t *testing.T) {
	body := syncResponse(
		decoupledHitans(60, 5, 2),
		hisals(7),
		"HIUPD:9:6:4+1::280:99999999+"+testIBAN+"+test-customer++EUR+Erika Musterfrau++Girokonto+HKSAL:1'",
		"HISPA:10:4:4+J:"+testIBAN+":"+testBIC+":00000001::280:99999999'",
	)
	response, err := ParseResponse([]byte(body))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	params := NewBankParams()
	params.IngestResponse(response)
	params.IngestResponse(response)

	if len(params.TANMethods) != 1 {
		t.Fatalf("TAN methods = %d, want 1", len(params.TANMethods))
	}
	if len(params.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(params.Accounts))
	}
	account := params.Accounts[0]
	if account.IBAN != testIBAN || account.BIC != testBIC || account.Owner() != "Erika Musterfrau" {
		t.Fatal("merged account lost a field from HIUPD or HISPA")
	}
	if len(params.bpdSegments) != 2 {
		t.Fatalf("parameter segments = %d, want 2", len(params.bpdSegments))
	}
}

// Printing a BankParams or Dialog value, not only a pointer, stays masked;
// fmt would otherwise print the unexported raw segments.
func TestValueFormattingIsMasked(t *testing.T) {
	bank := newFakeBank(t)
	body := syncResponse(
		decoupledHitans(60, 5, 2),
		"HIUPD:9:6:4+1::280:99999999+"+testIBAN+"+test-customer++EUR+Erika Musterfrau++Girokonto+HKSAL:1'",
	)
	dialog := newSyncedDialog(t, bank, body, func(config *DialogConfig) {
		config.TANMedium = "Erikas Handy"
	})

	params := *dialog.Params()
	output := fmt.Sprintf("%v %+v %#v %s|%v %+v %#v %s", params, params, params, params,
		*dialog, *dialog, *dialog, *dialog)
	for _, secretValue := range []string{testIBAN, "Erika Musterfrau", "Erikas Handy", testPIN, "test-system"} {
		if strings.Contains(output, secretValue) {
			t.Fatalf("value formatting leaked %q", secretValue)
		}
	}
}

// The five PIN codes mean a rejected PIN anywhere, also after the dialog is
// open (where another 9xxx code is only a bank error).
func TestDialogPinRejectedCodesAfterOpen(t *testing.T) {
	for _, code := range []string{"9340", "9910", "9930", "9931", "9942"} {
		t.Run(code, func(t *testing.T) {
			bank := newFakeBank(t)
			dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

			bank.expect(
				requestShape("HKIDN", "HKVVB", "HKTAN"),
				requestShape("HKSAL"),
				requestShape("HKEND"),
			)
			bank.reply(executedResponse("dialog-init"))
			bank.reply(codeResponse("dialog-init", code))
			bank.reply(successResponse("dialog-init"))

			if _, err := dialog.Init(context.Background()); err != nil {
				t.Fatalf("Init: %v", err)
			}
			_, err := dialog.Send(context.Background(), NewHKSAL(7, testIBAN, testBIC, nil))
			dialogError := assertDialogErrorKind(t, err, ErrorKindPinRejected)
			if len(dialogError.Codes) != 1 || dialogError.Codes[0] != code {
				t.Fatalf("codes = %v, want [%s]", dialogError.Codes, code)
			}
			if dialog.State() != DialogEnded {
				t.Fatalf("state = %s, want ended", dialog.State())
			}
			bank.assertSegmentTypes(t,
				requestShape("HKIDN", "HKVVB", "HKTAN"),
				requestShape("HKSAL"),
				requestShape("HKEND"),
			)
			bank.assertRequestsWiped(t)
		})
	}
}

// HIUPD (sync/init) and HISPA (the later HKSPA answer) arrive in different
// responses; the HISPA answer must not drop what HIUPD said.
func TestBankParamsHISPAInLaterResponseKeepsHIUPDFields(t *testing.T) {
	first, err := ParseResponse([]byte(syncResponse(
		"HIUPD:9:6:4+1::280:99999999+" + testIBAN + "+test-customer++EUR+Erika Musterfrau++Girokonto+HKSAL:1+HKKAZ:1'",
	)))
	if err != nil {
		t.Fatalf("ParseResponse(first): %v", err)
	}
	second, err := ParseResponse([]byte(executedResponse("dialog-init",
		"HISPA:5:4:3+J:"+testIBAN+":"+testBIC+":00000001::280:99999999'",
	)))
	if err != nil {
		t.Fatalf("ParseResponse(second): %v", err)
	}

	params := NewBankParams()
	params.IngestResponse(first)
	params.IngestResponse(second)

	if len(params.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(params.Accounts))
	}
	account := params.Accounts[0]
	if account.Owner() != "Erika Musterfrau" || account.ProductName() == "" {
		t.Fatal("HISPA answer dropped the HIUPD owner or product name")
	}
	if len(account.AllowedBusinessTransactions) != 2 {
		t.Fatalf("allowed transactions = %d, want 2", len(account.AllowedBusinessTransactions))
	}
	if account.BIC != testBIC {
		t.Fatal("HISPA BIC was not merged into the HIUPD account")
	}
}

// A later response with only HITANS must not drop the HISALS versions; the
// bank offers 5, 6 and 8, so the highest usable version up to 7 is 6.
func TestBankParamsLaterResponseKeepsOtherParameterSegments(t *testing.T) {
	first, err := ParseResponse([]byte(syncResponse(hisals(5), hisals(6), hisals(8))))
	if err != nil {
		t.Fatalf("ParseResponse(first): %v", err)
	}
	second, err := ParseResponse([]byte(executedResponse("dialog-init", decoupledHitans(60, 5, 2))))
	if err != nil {
		t.Fatalf("ParseResponse(second): %v", err)
	}

	params := NewBankParams()
	params.IngestResponse(first)
	params.IngestResponse(second)

	if got := params.SupportedVersion(BusinessHKSAL, 7); got != 6 {
		t.Fatalf("SupportedVersion(HKSAL, 7) = %d, want 6", got)
	}
	if len(params.TANMethods) != 1 {
		t.Fatalf("TAN methods = %d, want 1", len(params.TANMethods))
	}
}

// 3076 (no strong authentication needed) wins over 0030 on a business
// request, as in fints-rs: the data is returned and no TAN is asked for.
func TestDialogSendScaExemption(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:J"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL", "HKTAN"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(responseMessage("dialog-init",
		"HIRMS:3:2:4+0030::TAN erforderlich+3076::Keine starke Authentifizierung noetig'",
		"HITAN:4:7:3+4+task-hash+noref+nochallenge'",
		hisalPayload(),
	))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := dialog.Send(context.Background(), NewHKSAL(7, testIBAN, testBIC, nil))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Kind != SendSuccess {
		t.Fatalf("kind = %s, want success", result.Kind)
	}
	if _, ok := result.Response.Balance(); !ok {
		t.Fatal("exempted response lost its balance")
	}
	if dialog.State() != DialogOpen {
		t.Fatalf("state = %s, want open", dialog.State())
	}
	bank.assertRequestsWiped(t)
}

// DialogConfig is printed masked: the product id (a CI secret), user id,
// customer id and TAN medium never appear.
func TestDialogConfigFormattingIsMasked(t *testing.T) {
	config := DialogConfig{
		BLZ:            testBLZ,
		UserID:         "user-distinct-4711",
		CustomerID:     "customer-distinct-4712",
		SystemID:       "system-distinct-4713",
		ProductID:      "PRODUCTDISTINCT4714",
		ProductVersion: "1.0",
		PIN:            secretTextForTest(testPIN),
		TANMedium:      "Erikas Handy",
	}
	output := fmt.Sprintf("%v %+v %#v %s|%v %+v %#v", config, config, config, config, &config, &config, &config)
	for _, secretValue := range []string{
		"user-distinct-4711", "customer-distinct-4712", "system-distinct-4713",
		"PRODUCTDISTINCT4714", "Erikas Handy", testPIN,
	} {
		if strings.Contains(output, secretValue) {
			t.Fatalf("DialogConfig formatting leaked %q", secretValue)
		}
	}
}

type statusTransportError struct{ body string }

func (e statusTransportError) Error() string   { return "status 503: " + e.body }
func (e statusTransportError) HTTPStatus() int { return 503 }

// A transport error that carries an HTTP status keeps only the number.
func TestDialogTransportErrorKeepsHTTPStatusOnly(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSAL:N"), hisals(7)))

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSAL"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.replyError(fmt.Errorf("wrapped: %w", statusTransportError{body: "Erika Musterfrau-Body"}))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := dialog.Send(context.Background(), NewHKSAL(7, testIBAN, testBIC, nil))
	dialogError := assertDialogErrorKind(t, err, ErrorKindTransport)
	if dialogError.HTTPStatus != 503 {
		t.Fatalf("HTTPStatus = %d, want 503", dialogError.HTTPStatus)
	}
	output := fmt.Sprintf("%s %v %+v %#v", dialogError.Error(), dialogError, dialogError, dialogError)
	if strings.Contains(output, "Erika Musterfrau-Body") {
		t.Fatal("transport error leaked the response body")
	}
	if !strings.Contains(dialogError.Error(), "503") {
		t.Fatalf("Error() = %q, want the status", dialogError.Error())
	}
	bank.assertRequestsWiped(t)
}
