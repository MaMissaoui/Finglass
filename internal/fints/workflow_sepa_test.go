package fints

import (
	"context"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// HIUPD lists the account without a BIC; the HKSPA answer supplies it. The
// merged Params().Accounts entry must keep what HIUPD said.
func TestWorkflowFetchSEPAAccountsMergesBICAndKeepsHIUPDFields(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(
		hipins("HKSPA:N"),
		"HIUPD:9:6:4+1::280:99999999+"+testIBAN+"+test-customer++EUR+Erika Musterfrau++Girokonto+HKSAL:1'",
	))
	workflow := NewWorkflow(dialog, &fakePrompter{})

	accounts := dialog.Params().Accounts
	if len(accounts) != 1 {
		t.Fatalf("HIUPD accounts = %d, want 1", len(accounts))
	}
	if accounts[0].BIC != "" {
		t.Fatalf("HIUPD BIC = %q, want empty before HKSPA", accounts[0].BIC)
	}

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(executedResponse("dialog-init",
		"HISPA:5:4:3+J:"+testIBAN+":"+testBIC+":00000001::280:99999999'"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	fetched, err := workflow.FetchSEPAAccounts(context.Background())
	if err != nil {
		t.Fatalf("FetchSEPAAccounts: %v", err)
	}
	if len(fetched) != 1 {
		t.Fatalf("HISPA accounts = %d, want 1", len(fetched))
	}
	if fetched[0].IBAN != testIBAN || fetched[0].BIC != testBIC {
		t.Fatalf("HISPA account = %#v, want IBAN and BIC", fetched[0])
	}

	merged := dialog.Params().Accounts
	if len(merged) != 1 {
		t.Fatalf("merged accounts = %d, want 1", len(merged))
	}
	if merged[0].IBAN != testIBAN || merged[0].BIC != testBIC {
		t.Fatalf("merged account = %#v, want IBAN and BIC", merged[0])
	}
	if merged[0].Owner() != "Erika Musterfrau" {
		t.Fatal("HKSPA answer dropped the HIUPD owner")
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchSEPAAccountsWithTAN(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(
		chipHitans(5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSPA:J"),
	))
	prompter := &fakePrompter{tan: secret.NewText([]byte(testTAN))}
	workflow := NewWorkflow(dialog, prompter)

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA", "HKTAN"),
		requestShape("HKTAN"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(tanRequiredResponse("dialog-init", "task-sepa", "Bitte TAN eingeben"))
	bank.reply(executedResponse("dialog-init",
		"HISPA:5:4:3+J:"+testIBAN+":"+testBIC+":00000001::280:99999999'"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	accounts, err := workflow.FetchSEPAAccounts(context.Background())
	if err != nil {
		t.Fatalf("FetchSEPAAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].IBAN != testIBAN || accounts[0].BIC != testBIC {
		t.Fatalf("accounts = %#v, want the HISPA account", accounts)
	}
	if prompter.challengeCalls != 1 {
		t.Fatalf("challenge calls = %d, want 1", prompter.challengeCalls)
	}
	if prompter.lastChallenge.TaskReference != "task-sepa" {
		t.Fatalf("challenge reference = %q", prompter.lastChallenge.TaskReference)
	}
	if !prompter.tan.IsEmpty() {
		t.Fatal("the workflow did not hand the TAN to SubmitTAN, which wipes it")
	}
	bank.assertRequestsWiped(t)
}

func TestWorkflowFetchSEPAAccountsBankErrorKeepsCodes(t *testing.T) {
	bank := newFakeBank(t)
	dialog := newSyncedDialog(t, bank, syncResponse(hipins("HKSPA:N")))
	workflow := NewWorkflow(dialog, &fakePrompter{})

	bank.expect(
		requestShape("HKIDN", "HKVVB", "HKTAN"),
		requestShape("HKSPA"),
		requestShape("HKEND"),
	)
	bank.reply(executedResponse("dialog-init"))
	bank.reply(codeResponse("dialog-init", "9050"))
	bank.reply(successResponse("dialog-init"))

	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := workflow.FetchSEPAAccounts(context.Background())
	dialogError := assertDialogErrorKind(t, err, ErrorKindBankError)
	if len(dialogError.Codes) != 1 || dialogError.Codes[0] != "9050" {
		t.Fatalf("codes = %v, want [9050]", dialogError.Codes)
	}
	if dialog.State() != DialogEnded {
		t.Fatalf("state = %s, want ended", dialog.State())
	}
	bank.assertRequestsWiped(t)
}
