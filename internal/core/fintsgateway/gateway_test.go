package fintsgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func standardParameters(methods, allowed, operations string, accounts ...string) []string {
	parameters := make([]string, 0, 8)
	if methods != "" {
		parameters = append(parameters, methods)
	}
	parameters = append(parameters,
		hipins(operations),
		hisals(7),
		hikazs(7),
		hispas(3),
		hitabs(1),
	)
	parameters = append(parameters, accounts...)
	if allowed != "" {
		parameters = append(parameters, allowedSecurityFunctions(allowed))
	}
	return parameters
}

func syncBody(parameters []string) string {
	segments := []string{
		"HISYN:2:4:1+" + testSystemID + "'",
		"HIBPA:3:3+42+280+0+1+1+0'",
		"HIUPA:4:4+test-customer+7+55'",
	}
	segments = append(segments, parameters...)
	segments = append(segments, "HIRMG:2:2+0010::OK'")
	return responseMessage("dialog-sync", segments...)
}

func initBody(parameters []string) string {
	return executedResponse("dialog-init", parameters...)
}

func standardHandler(
	parameters []string,
	syncParameters []string,
	statementPayload func(account string) string,
	spaAccounts ...string,
) func(int, exchangeRecord) (string, error) {
	return func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(syncParameters), nil
		case request.has("HKSPA"):
			payload := make([]string, 0, len(spaAccounts))
			for index, iban := range spaAccounts {
				payload = append(payload, hispa(10+index, iban, testBIC, fmt.Sprintf("%08d", index+1)))
			}
			return executedResponse("dialog-1", payload...), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKKAZ"):
			account := ""
			if segment, ok := request.segment("HKKAZ"); ok {
				account = segment.DEG(1).Text(0)
			}
			return executedResponse("dialog-1", statementPayload(account)), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			return initBody(parameters), nil
		}
	}
}

func TestFullSyncWithoutAStoredSystemID(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN,
	))
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	since := date(2026, time.January, 1)

	outcome, err := gateway.FetchAll(
		context.Background(),
		testConnection(),
		interaction,
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN {
				value := since
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	if outcome.SystemID == nil || *outcome.SystemID != testSystemID {
		t.Fatalf("system id = %v, want %s", outcome.SystemID, testSystemID)
	}
	if outcome.TanMethod == nil || *outcome.TanMethod != "942" {
		t.Fatalf("tan method = %v, want 942", outcome.TanMethod)
	}
	if outcome.HKTANVersion != 7 {
		t.Fatalf("hktan version = %d, want 7", outcome.HKTANVersion)
	}
	if outcome.Capabilities == nil {
		t.Fatal("capabilities are nil")
	}
	for _, job := range []string{
		banksync.JobBalance,
		banksync.JobStatementsMT940,
		banksync.JobAccounts,
		banksync.JobTANMedia,
	} {
		if !outcome.Capabilities.Supports(job) {
			t.Fatalf("capabilities do not support %s", job)
		}
	}
	if len(interaction.tanOptions) != 0 {
		t.Fatalf("SelectTanMethod was called for one usable function: %v", interaction.tanOptions)
	}

	if len(outcome.Snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(outcome.Snapshots))
	}
	snapshot := outcome.Snapshots[0]
	accountView := snapshot.Account
	if accountView.IBAN != testIBAN || accountView.BIC != testBIC {
		t.Fatalf("account = %+v", accountView)
	}
	if accountView.AccountNumber != "00000001" || accountView.BLZ != testBLZ {
		t.Fatalf("account number/BLZ = %q/%q", accountView.AccountNumber, accountView.BLZ)
	}
	if accountView.BankName != "Test Bank" {
		t.Fatalf("bank name = %q", accountView.BankName)
	}
	if accountView.AccountType != model.AccountTypeChecking.AsStr() {
		t.Fatalf("account type = %q", accountView.AccountType)
	}
	if accountView.Currency != "EUR" || !accountView.CurrencyStated {
		t.Fatalf("currency = %q stated=%t", accountView.Currency, accountView.CurrencyStated)
	}
	if accountView.ProductName == nil || *accountView.ProductName != "Girokonto" {
		t.Fatalf("product name = %v", accountView.ProductName)
	}

	balance := snapshot.Balance
	if balance == nil {
		t.Fatal("balance is nil")
	}
	assertDecimal(t, balance.Balance, "1234.56")
	if balance.AvailableBalance == nil {
		t.Fatal("available balance is nil")
	}
	assertDecimal(t, *balance.AvailableBalance, "900.00")
	if balance.Currency != "EUR" {
		t.Fatalf("balance currency = %q", balance.Currency)
	}
	wantDate := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)
	if !balance.Date.Equal(wantDate) {
		t.Fatalf("balance date = %v, want %v", balance.Date, wantDate)
	}

	if len(snapshot.Transactions) != 2 {
		t.Fatalf("transactions = %d, want 2", len(snapshot.Transactions))
	}
	first := snapshot.Transactions[0]
	if first.Date != date(2026, time.January, 1) || first.ValueDate != date(2026, time.January, 1) {
		t.Fatalf("first dates = %s/%s", first.Date, first.ValueDate)
	}
	assertDecimal(t, first.Amount, "-45.67")
	if first.Currency != "EUR" {
		t.Fatalf("first currency = %q", first.Currency)
	}
	if first.PartnerName == nil || *first.PartnerName != "Bakery GmbH" {
		t.Fatalf("first partner = %v", first.PartnerName)
	}
	if first.Purpose == nil || *first.Purpose != "Bakery" {
		t.Fatalf("first purpose = %v", first.Purpose)
	}
	if first.Reference != nil || first.BankID != nil {
		t.Fatalf("MT940 reference/bank id = %v/%v, want nil/nil", first.Reference, first.BankID)
	}

	second := snapshot.Transactions[1]
	if second.Date != date(2026, time.January, 2) {
		t.Fatalf("second date = %s", second.Date)
	}
	assertDecimal(t, second.Amount, "500.00")
	if second.PartnerName == nil || *second.PartnerName != "Employer AG" {
		t.Fatalf("second partner = %v", second.PartnerName)
	}
	if second.Purpose == nil || *second.Purpose != "Salary" {
		t.Fatalf("second purpose = %v", second.Purpose)
	}
	if second.Reference == nil || *second.Reference != "REF2" {
		t.Fatalf("second reference = %v", second.Reference)
	}
	if second.BankID != nil {
		t.Fatalf("MT940 bank id = %v, want nil", second.BankID)
	}

	// The dialog ran the expected one-dialog sequence.
	records := bank.snapshotRecords()
	wantSequence := [][]string{
		{"HKSYN"},
		{"HKEND"},
		{"HKIDN", "HKVVB", "HKTAN"},
		{"HKSPA"},
		{"HKSAL"},
		{"HKKAZ"},
		{"HKEND"},
	}
	if len(records) != len(wantSequence) {
		t.Fatalf("requests = %d, want %d", len(records), len(wantSequence))
	}
	for index, wanted := range wantSequence {
		for _, segmentType := range wanted {
			if !records[index].has(segmentType) {
				t.Fatalf("request %d = %v, want %s", index, records[index].types, segmentType)
			}
		}
	}

	// The statement request used the since date for the account.
	foundStart := false
	for _, record := range bank.snapshotRecords() {
		segment, ok := record.segment("HKKAZ")
		if !ok || segment.DEG(1).Text(0) != testIBAN {
			continue
		}
		foundStart = true
		if got := segment.DEG(3).Text(0); got != "20260101" {
			t.Fatalf("HKKAZ start = %q, want 20260101", got)
		}
	}
	if !foundStart {
		t.Fatal("no HKKAZ for the account")
	}

	// Init was signed with the selected method, not 999, and carried its version.
	initRecords := 0
	for _, record := range bank.snapshotRecords() {
		if !record.has("HKTAN") || record.has("HKSYN") || record.has("HKSPA") {
			continue
		}
		initRecords++
		if record.securityFunction != "942" {
			t.Fatalf("Init HNSHK security function = %q, want 942", record.securityFunction)
		}
		if len(record.hktanVersions) == 0 || record.hktanVersions[0] != 7 {
			t.Fatalf("Init HKTAN versions = %v, want 7", record.hktanVersions)
		}
		if hkvvb, ok := record.segment("HKVVB"); ok {
			if hkvvb.DEG(1).Text(0) != "0" || hkvvb.DEG(2).Text(0) != "0" {
				t.Fatalf("Init HKVVB versions = %q/%q, want 0/0",
					hkvvb.DEG(1).Text(0), hkvvb.DEG(2).Text(0))
			}
		}
	}
	if initRecords != 1 {
		t.Fatalf("Init messages = %d, want 1", initRecords)
	}
}

func TestStoredSystemIDAndTanMethodSkipsSyncAndUsesTheRememberedMethod(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN,
	))
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	outcome, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countSegments("HKSYN") != 0 {
		t.Fatal("a stored system id and TAN method still synchronised")
	}
	records := bank.snapshotRecords()
	if len(records) == 0 {
		t.Fatal("no requests recorded")
	}
	if records[0].securityFunction != "942" {
		t.Fatalf("first HNSHK security function = %q, want 942", records[0].securityFunction)
	}
	if len(records[0].hktanVersions) == 0 || records[0].hktanVersions[0] != 7 {
		t.Fatalf("first HKTAN versions = %v, want 7", records[0].hktanVersions)
	}
	if outcome.SystemID == nil || *outcome.SystemID != testSystemID {
		t.Fatalf("system id = %v", outcome.SystemID)
	}
}

func TestStoredSystemIDWithoutTanMethodSynchronises(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN,
	))
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = "stored-system"

	if _, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countSegments("HKSYN") != 1 {
		t.Fatalf("HKSYN = %d, want 1", bank.countSegments("HKSYN"))
	}
}

func TestInitBankErrorKeepsTheStoredSystemID(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return bankErrorResponse("dialog-1", "9010"), nil
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = "stored-system"
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	outcome, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if err == nil {
		t.Fatal("FetchAll succeeded")
	}
	var gatewayError banksync.GatewayError
	if !errors.As(err, &gatewayError) {
		t.Fatalf("error = %T, want GatewayError", err)
	}
	if gatewayError.Kind() != banksync.GatewayErrorBank {
		t.Fatalf("kind = %v, want bank", gatewayError.Kind())
	}
	if got := strings.Join(gatewayError.Codes(), ","); got != "9010" {
		t.Fatalf("codes = %q, want 9010", got)
	}
	if outcome.SystemID == nil || *outcome.SystemID != "stored-system" {
		t.Fatalf("system id = %v, want stored-system", outcome.SystemID)
	}
}

func TestDecoupledInitWithoutSyncPollsAndSucceeds(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "pushTAN", 3, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	sPolls := 0
	bank := newFakeBank(t, func(call int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSPA"):
			return executedResponse("dialog-1", hispa(10, testIBAN, testBIC, "00000001")), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKKAZ"):
			return executedResponse("dialog-1", hikazPayload(mt940Fixture)), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		case request.has("HKTAN") && containsProcess(request, "S"):
			sPolls++
			if sPolls == 1 {
				return decoupledPendingResponse("dialog-1"), nil
			}
			return executedResponse("dialog-1"), nil
		default:
			segments := []string{
				"HIRMS:3:2:4+3955::Im App bestaetigen'",
				"HITAN:4:7:3+4+task-hash+task-1+Approve in the app+@0@'",
			}
			segments = append(segments, parameters...)
			segments = append(segments, "HIRMG:2:2+0020::Ausgefuehrt'")
			return responseMessage("dialog-init", segments...), nil
		}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7
	interaction := newTestInteraction(testPIN)
	interaction.confirm = true

	outcome, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countHKTANProcess("S") < 2 {
		t.Fatalf("HKTAN process S = %d, want at least 2", bank.countHKTANProcess("S"))
	}
	if interaction.confirmAsked != 1 || interaction.confirmTexts[0] != "Approve in the app" {
		t.Fatalf("confirm = %d %v", interaction.confirmAsked, interaction.confirmTexts)
	}
	if len(outcome.Snapshots) != 1 {
		t.Fatalf("snapshots = %d", len(outcome.Snapshots))
	}
}

func TestDecoupledInitDeclinedIsCancelled(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return decoupledResponse("dialog-init", "task-1", "Approve in the app"), nil
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7
	interaction := newTestInteraction(testPIN)
	interaction.confirm = false

	_, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorCancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if bank.countHKTANProcess("S") != 0 {
		t.Fatal("a declined confirmation still polled")
	}
}

func TestDecoupledTanInsideStatementsDeclinedStopsThePoll(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "pushTAN", 3, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:J:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1", hispa(10, testIBAN, testBIC, "00000001")), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKKAZ"):
			return decoupledResponse("dialog-1", "task-statement", "Approve the statement"), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	interaction.confirm = false

	_, err := gateway.FetchAll(
		context.Background(), testConnection(), interaction,
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN {
				value := date(2026, time.January, 1)
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorCancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if bank.countHKTANProcess("S") != 0 {
		t.Fatal("a declined statement confirmation still polled")
	}
}

func TestSelectTanMethodNeverOffersPinOnlyOrUnknownFunctions(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(
			hitansMethod("942", "pushTAN", 3, 1, 1),
			hitansMethod("943", "chipTAN", -1, 1, 1),
		),
		"999:942:943:944",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN,
	))
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	interaction.answerTanMethod("942")

	if _, err := gateway.FetchAll(
		context.Background(), testConnection(), interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(interaction.tanOptions) != 1 {
		t.Fatalf("SelectTanMethod calls = %d, want 1", len(interaction.tanOptions))
	}
	options := interaction.tanOptions[0]
	if len(options) != 2 || options[0].ID != "942" || options[1].ID != "943" {
		t.Fatalf("options = %+v, want 942 and 943 only", options)
	}
	for _, option := range options {
		if option.ID == "999" || option.ID == "944" {
			t.Fatalf("offered forbidden function %q", option.ID)
		}
	}
}

func TestTwoAllowedFunctionsWithNoAnswerAreCancelled(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(
			hitansMethod("942", "pushTAN", 3, 1, 1),
			hitansMethod("943", "chipTAN", -1, 1, 1),
		),
		"942:943",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKSYN") {
			return syncBody(parameters), nil
		}
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return initBody(parameters), nil
	})
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	interaction.answerNil = true

	_, err := gateway.FetchAll(
		context.Background(), testConnection(), interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorCancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if len(interaction.tanOptions) != 1 || len(interaction.tanOptions[0]) != 2 {
		t.Fatalf("options = %+v, want both functions", interaction.tanOptions)
	}
	if bank.countSegments("HKEND") == 0 {
		t.Fatal("no HKEND was sent")
	}
}

func TestCamtBankFollowsTwoTouchdownPages(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKCAZ:1")
	parameters := []string{
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		hipins("HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N"),
		hisals(7),
		hikazs(7),
		hispas(3),
		hicazs(7, "urn:iso:std:iso:20022:tech:xsd:camt.053.001.02"),
		account,
		allowedSecurityFunctions("942"),
	}
	pageOne := icamtDocument(testIBAN, "10.00", "CRDT", "2026-01-05", "2026-01-06", "REF-A", "Alice", "Page one")
	pageTwo := icamtDocument(testIBAN, "20.00", "DBIT", "2026-01-07", "2026-01-07", "REF-B", "Bob", "Page two")
	camtPages := 0
	bank := newFakeBank(t, func(call int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1", hispa(10, testIBAN, testBIC, "00000001")), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKCAZ"):
			camtPages++
			if camtPages == 1 {
				return touchdownResponse("dialog-1", "page-2", hicazPayload(testIBAN, pageOne)), nil
			}
			return executedResponse("dialog-1", hicazPayload(testIBAN, pageTwo)), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)

	outcome, err := gateway.FetchAll(
		context.Background(), testConnection(), newTestInteraction(testPIN),
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN {
				value := date(2026, time.January, 1)
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countSegments("HKCAZ") != 2 {
		t.Fatalf("HKCAZ = %d, want 2 pages", bank.countSegments("HKCAZ"))
	}
	if len(outcome.Snapshots) != 1 || len(outcome.Snapshots[0].Transactions) != 2 {
		t.Fatalf("transactions = %+v", outcome.Snapshots)
	}
	for index, want := range []struct {
		reference string
		bankID    string
		partner   string
		purpose   string
	}{
		{"REF-A", "REF-A", "Alice", "Page one"},
		{"REF-B", "REF-B", "Bob", "Page two"},
	} {
		transaction := outcome.Snapshots[0].Transactions[index]
		if transaction.Reference == nil || *transaction.Reference != want.reference {
			t.Fatalf("transaction %d reference = %v", index, transaction.Reference)
		}
		if transaction.BankID == nil || *transaction.BankID != want.bankID {
			t.Fatalf("transaction %d bank id = %v", index, transaction.BankID)
		}
		if transaction.PartnerName == nil || *transaction.PartnerName != want.partner {
			t.Fatalf("transaction %d partner = %v", index, transaction.PartnerName)
		}
		if transaction.Purpose == nil || *transaction.Purpose != want.purpose {
			t.Fatalf("transaction %d purpose = %v", index, transaction.Purpose)
		}
	}
	if !outcome.Capabilities.Supports(banksync.JobStatementsCAMT) {
		t.Fatal("camt capability missing")
	}
}

func TestTypedTanOnStatementsSubmitsAndWipesTheTan(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:J:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1", hispa(10, testIBAN, testBIC, "00000001")), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKKAZ"):
			return tanRequiredResponse("dialog-1", "task-statement", "Enter the TAN"), nil
		case containsProcess(request, "2"):
			return executedResponse("dialog-1", hikazPayload(mt940Fixture)), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	tan := interaction.tan

	outcome, err := gateway.FetchAll(
		context.Background(), testConnection(), interaction,
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN {
				value := date(2026, time.January, 1)
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countHKTANProcess("2") != 1 {
		t.Fatalf("HKTAN process 2 = %d, want 1", bank.countHKTANProcess("2"))
	}
	if tan == nil || !tan.IsEmpty() {
		t.Fatal("the TAN was not wiped after SubmitTAN")
	}
	if len(outcome.Snapshots) != 1 || len(outcome.Snapshots[0].Transactions) != 2 {
		t.Fatalf("transactions = %+v", outcome.Snapshots)
	}
}

func TestDepotBeforeTheCheckingAccountIsSkipped(t *testing.T) {
	depot := hiuPd(9, "00000001", "30", testIBAN, "EUR", "Erika Musterfrau", "Depot", "HKWPD:1")
	checking := hiuPd(10, "00000002", "1", testIBAN2, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		depot, checking,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN, testIBAN2,
	))
	gateway := newTestGateway(t, bank)

	outcome, err := gateway.FetchAll(
		context.Background(), testConnection(), newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(outcome.Snapshots) != 1 {
		t.Fatalf("snapshots = %d, want only the checking account", len(outcome.Snapshots))
	}
	if outcome.Snapshots[0].Account.IBAN != testIBAN2 {
		t.Fatalf("fetched account = %s, want %s", outcome.Snapshots[0].Account.IBAN, testIBAN2)
	}
	for _, record := range bank.snapshotRecords() {
		for _, segmentType := range []string{"HKSAL", "HKKAZ"} {
			if segment, ok := record.segment(segmentType); ok && segment.DEG(1).Text(0) == testIBAN {
				t.Fatalf("%s fetched the depot", segmentType)
			}
		}
	}
}

func TestNilSinceSkipsOnlyThatAccountsStatements(t *testing.T) {
	first := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	second := hiuPd(10, "00000002", "1", testIBAN2, "EUR", "Erika Musterfrau", "Tagesgeld", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		first, second,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN, testIBAN2,
	))
	gateway := newTestGateway(t, bank)

	outcome, err := gateway.FetchAll(
		context.Background(), testConnection(), newTestInteraction(testPIN),
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN2 {
				value := date(2026, time.January, 1)
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(outcome.Snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(outcome.Snapshots))
	}
	for _, record := range bank.snapshotRecords() {
		if segment, ok := record.segment("HKKAZ"); ok && segment.DEG(1).Text(0) == testIBAN {
			t.Fatal("a nil since still requested statements")
		}
	}
	for _, snapshot := range outcome.Snapshots {
		if snapshot.Account.IBAN == testIBAN && len(snapshot.Transactions) != 0 {
			t.Fatal("the account with nil since got transactions")
		}
		if snapshot.Account.IBAN == testIBAN2 && len(snapshot.Transactions) != 2 {
			t.Fatalf("the account with a since got %d transactions", len(snapshot.Transactions))
		}
	}
}

func TestStatementParseFailureKeepsFetchingOtherAccounts(t *testing.T) {
	first := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	second := hiuPd(10, "00000002", "1", testIBAN2, "EUR", "Erika Musterfrau", "Tagesgeld", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		first, second,
	)
	bank := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(account string) string {
			if account == testIBAN {
				return hikazPayload("not an mt940 statement")
			}
			return hikazPayload(mt940Fixture)
		},
		testIBAN, testIBAN2,
	))
	gateway := newTestGateway(t, bank)

	outcome, err := gateway.FetchAll(
		context.Background(), testConnection(), newTestInteraction(testPIN),
		func(string) *civil.Date {
			value := date(2026, time.January, 1)
			return &value
		},
		date(2026, time.November, 14),
	)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(outcome.Snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(outcome.Snapshots))
	}
	for _, snapshot := range outcome.Snapshots {
		switch snapshot.Account.IBAN {
		case testIBAN:
			if snapshot.StatementError == nil || *snapshot.StatementError != statementParse {
				t.Fatalf("statement error = %v", snapshot.StatementError)
			}
		case testIBAN2:
			if snapshot.StatementError != nil {
				t.Fatalf("second account statement error = %v", *snapshot.StatementError)
			}
			if len(snapshot.Transactions) != 2 {
				t.Fatalf("second account transactions = %d", len(snapshot.Transactions))
			}
		}
	}
}

func TestPinRejectionStopsWithoutASecondMessage(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return bankErrorResponse("dialog-1", "9942"), nil
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorPinRejected) {
		t.Fatalf("error = %v, want PIN rejected", err)
	}
	if bank.countSegments("HKEND") != 0 {
		t.Fatal("HKEND was sent after a PIN rejection")
	}
	if records := bank.snapshotRecords(); len(records) != 1 {
		t.Fatalf("requests = %d, want only the rejecting one", len(records))
	}
}

func TestStaleRememberedStateRetriesOnceWithSyncAndSucceeds(t *testing.T) {
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
	)
	initCalls := 0
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1"), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			initCalls++
			if initCalls == 1 {
				return bankErrorResponse("dialog-1", "9010"), nil
			}
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7
	interaction := newTestInteraction(testPIN)

	if _, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if initCalls != 2 {
		t.Fatalf("Init calls = %d, want 2", initCalls)
	}
	if bank.countSegments("HKSYN") != 1 {
		t.Fatalf("HKSYN = %d, want 1", bank.countSegments("HKSYN"))
	}
	if interaction.pinAsked != 1 {
		t.Fatalf("PIN prompts = %d, want 1 (the retry reuses the entry)", interaction.pinAsked)
	}
}

func TestStaleRememberedStateAsksForANewMethodAfterTheRetrySync(t *testing.T) {
	parameters := standardParameters(
		hitans(
			hitansMethod("943", "pushTAN", 3, 1, 1),
			hitansMethod("944", "chipTAN", -1, 1, 1),
		),
		"943:944",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
	)
	initCalls := 0
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1"), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			initCalls++
			if initCalls == 1 {
				return bankErrorResponse("dialog-1", "9010"), nil
			}
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7
	interaction := newTestInteraction(testPIN)
	interaction.answerTanMethod("943")

	if _, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(interaction.tanOptions) != 1 {
		t.Fatalf("SelectTanMethod calls = %d, want 1", len(interaction.tanOptions))
	}
	options := interaction.tanOptions[0]
	if len(options) != 2 || options[0].ID != "943" || options[1].ID != "944" {
		t.Fatalf("options = %+v, want 943 and 944", options)
	}
}

func TestStaleRememberedStateNeverRetriesAfterAPinCode(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return bankErrorResponse("dialog-1", "9942"), nil
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorPinRejected) {
		t.Fatalf("error = %v, want PIN rejected", err)
	}
	if bank.countSegments("HKSYN") != 0 {
		t.Fatal("a PIN rejection retried with a sync")
	}
	if bank.countSegments("HKEND") != 0 {
		t.Fatal("HKEND was sent after a PIN rejection")
	}
}

func TestStaleRememberedStateNeverRetriesALockedAccount(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return bankErrorResponse("dialog-1", "3938"), nil
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorBank) {
		t.Fatalf("error = %v, want bank", err)
	}
	if bank.countSegments("HKSYN") != 0 {
		t.Fatal("a locked account retried with a sync")
	}
	if bank.countSegments("HKEND") != 0 {
		t.Fatal("HKEND was sent after a locked account")
	}
}

func TestStaleRememberedStateReturnsTheSecondFailure(t *testing.T) {
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
	)
	initCalls := 0
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			initCalls++
			if initCalls == 1 {
				return bankErrorResponse("dialog-1", "9010"), nil
			}
			return bankErrorResponse("dialog-1", "9011"), nil
		}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	var gatewayError banksync.GatewayError
	if !errors.As(err, &gatewayError) {
		t.Fatalf("error = %v, want GatewayError", err)
	}
	if got := strings.Join(gatewayError.Codes(), ","); got != "9011" {
		t.Fatalf("codes = %q, want the second failure", got)
	}
	if initCalls != 2 {
		t.Fatalf("Init calls = %d, want 2 (at most one retry)", initCalls)
	}
	if bank.countSegments("HKSYN") != 1 {
		t.Fatalf("HKSYN = %d, want 1", bank.countSegments("HKSYN"))
	}
}

func TestDecoupledInitWithoutHitansUsesTheSeedAndPolls(t *testing.T) {
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	seed := paramsFor(connection, tanMethod)
	if len(seed.TANMethods) != 1 || seed.TANMethods[0].DecoupledMaxPolls != -1 {
		t.Fatalf("seed = %+v, want DecoupledMaxPolls -1", seed.TANMethods)
	}

	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSPA"):
			return executedResponse("dialog-1"), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		case request.has("HKTAN") && containsProcess(request, "S"):
			return executedResponse("dialog-1"), nil
		default:
			// The Init response is decoupled and carries no HITANS.
			return decoupledResponse("dialog-init", "task-1", "Approve in the app"), nil
		}
	})
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	interaction.confirm = true

	if _, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if bank.countSegments("HKSYN") != 0 {
		t.Fatal("the stored system id still synchronised")
	}
	if bank.countHKTANProcess("S") == 0 {
		t.Fatal("the seeded method did not poll")
	}
	if interaction.confirmAsked != 1 {
		t.Fatalf("confirm prompts = %d, want 1", interaction.confirmAsked)
	}
}

func TestDecoupledStatementDeclineSendsExactlyOneHKEND(t *testing.T) {
	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "pushTAN", 3, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:J:HKCAZ:N:HKSPA:N",
		account,
	)
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		switch {
		case request.has("HKSYN"):
			return syncBody(parameters), nil
		case request.has("HKSPA"):
			return executedResponse("dialog-1", hispa(10, testIBAN, testBIC, "00000001")), nil
		case request.has("HKSAL"):
			return executedResponse("dialog-1", hisalPayload()), nil
		case request.has("HKKAZ"):
			return decoupledResponse("dialog-1", "task-statement", "Approve the statement"), nil
		case request.has("HKEND"):
			return okResponse("dialog-1"), nil
		default:
			return initBody(parameters), nil
		}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7
	interaction := newTestInteraction(testPIN)
	interaction.confirm = false

	_, err := gateway.FetchAll(
		context.Background(), connection, interaction,
		func(accountNumber string) *civil.Date {
			if accountNumber == testIBAN {
				value := date(2026, time.January, 1)
				return &value
			}
			return nil
		},
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorCancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if bank.countHKTANProcess("S") != 0 {
		t.Fatal("a declined statement confirmation still polled")
	}
	if got := bank.countSegments("HKSYN"); got != 0 {
		t.Fatalf("HKSYN = %d, want 0", got)
	}
	if got := bank.countSegments("HKEND"); got != 1 {
		t.Fatalf("HKEND = %d, want exactly 1 after the cancel", got)
	}
}

func TestInvalidInputsFailBeforeAnyDialog(t *testing.T) {
	tests := []struct {
		name       string
		connection banksync.FinTsConnection
		otherText  string
	}{
		{
			name:       "product id",
			connection: func() banksync.FinTsConnection { c := testConnection(); c.ProductID = "not valid!"; return c }(),
			otherText:  kindInvalidProductID,
		},
		{
			name:       "blz",
			connection: func() banksync.FinTsConnection { c := testConnection(); c.BLZ = "123"; return c }(),
			otherText:  kindInvalidBLZ,
		},
		{
			name:       "user id",
			connection: func() banksync.FinTsConnection { c := testConnection(); c.UserID = " "; return c }(),
			otherText:  kindMissingUserID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bank := newFakeBank(t, func(int, exchangeRecord) (string, error) {
				t.Fatal("the bank was contacted for invalid input")
				return "", nil
			})
			gateway := newTestGateway(t, bank)
			_, err := gateway.FetchAll(
				context.Background(), test.connection, newTestInteraction(testPIN),
				func(string) *civil.Date { return nil },
				date(2026, time.November, 14),
			)
			if !isGatewayKind(err, banksync.GatewayErrorOther) {
				t.Fatalf("error = %v, want other", err)
			}
			var gatewayError banksync.GatewayError
			_ = errors.As(err, &gatewayError)
			if got := gatewayError.Describe(i18n.En, nil); got != test.otherText {
				t.Fatalf("text = %q, want %q", got, test.otherText)
			}
		})
	}
}

func TestPlainHTTPURLIsRefusedByTheDefaultFactory(t *testing.T) {
	gateway := New(WithSleeper(noopSleeper{}))
	connection := testConnection()
	connection.ServerURL = "http://fints.example.invalid/"

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorOther) {
		t.Fatalf("error = %v, want other", err)
	}
	var gatewayError banksync.GatewayError
	_ = errors.As(err, &gatewayError)
	if got := gatewayError.Describe(i18n.En, nil); got != kindInvalidServerURL {
		t.Fatalf("text = %q, want %q", got, kindInvalidServerURL)
	}
}

func TestAnEmptyOrMissingPinCancelsBeforeTheBank(t *testing.T) {
	bank := newFakeBank(t, func(int, exchangeRecord) (string, error) {
		t.Fatal("the bank was contacted without a PIN")
		return "", nil
	})
	gateway := newTestGateway(t, bank)
	interaction := newTestInteraction(testPIN)
	interaction.pin = nil

	_, err := gateway.FetchAll(
		context.Background(), testConnection(), interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if !isGatewayKind(err, banksync.GatewayErrorCancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
}

func TestHTTPStatusErrorBecomesTheHTTPStatusKind(t *testing.T) {
	bank := newFakeBank(t, func(int, exchangeRecord) (string, error) {
		return "", statusError{code: 503}
	})
	gateway := newTestGateway(t, bank)
	connection := testConnection()
	connection.SystemID = testSystemID
	tanMethod := "942"
	connection.TanMethod = &tanMethod
	connection.HKTANVersion = 7

	_, err := gateway.FetchAll(
		context.Background(), connection, newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	var gatewayError banksync.GatewayError
	if !errors.As(err, &gatewayError) || gatewayError.Kind() != banksync.GatewayErrorHTTPStatus {
		t.Fatalf("error = %v, want HTTP status", err)
	}
	status, ok := gatewayError.HTTPStatus()
	if !ok || status != 503 {
		t.Fatalf("status = %d, %t", status, ok)
	}
}

type statusError struct {
	code int
}

func (e statusError) Error() string   { return "status" }
func (e statusError) HTTPStatus() int { return e.code }

func containsProcess(request exchangeRecord, process string) bool {
	for _, value := range request.hktanProcesses {
		if value == process {
			return true
		}
	}
	return false
}

func isGatewayKind(err error, kind banksync.GatewayErrorKind) bool {
	var gatewayError banksync.GatewayError
	return errors.As(err, &gatewayError) && gatewayError.Kind() == kind
}

func assertDecimal(t *testing.T, value decimal.Decimal, want string) {
	t.Helper()
	expected, err := decimal.Parse(want)
	if err != nil {
		t.Fatalf("parse %q: %v", want, err)
	}
	if value.Cmp(expected) != 0 {
		t.Fatalf("decimal = %s, want %s", value, want)
	}
}
