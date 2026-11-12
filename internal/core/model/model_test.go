package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

const secretUserID = "secret-user"

func bankAccessItem() BankAccessItem {
	bic := "DEUTDEDBXXX"
	tan := "942"
	server := "https://example.test/fints"
	capabilities := "SaldoReq"
	syncFrom, _ := civil.New(2026, time.January, 1)
	lastSync := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	return BankAccessItem{
		ID:               7,
		BankName:         "Deutsche Bank",
		BLZ:              "37040044",
		BIC:              &bic,
		UserID:           secretUserID,
		TanMethod:        &tan,
		ServerURL:        &server,
		LastSync:         &lastSync,
		Capabilities:     &capabilities,
		SyncFrom:         &syncFrom,
		IncludeInSyncAll: true,
		Offline:          false,
	}
}

func bankConnection() BankConnection {
	item := bankAccessItem()
	return BankConnection{
		ID:               item.ID,
		BankName:         item.BankName,
		BLZ:              item.BLZ,
		BIC:              item.BIC,
		UserID:           item.UserID,
		ServerURL:        item.ServerURL,
		TanMethod:        item.TanMethod,
		SyncFrom:         item.SyncFrom,
		LastSync:         item.LastSync,
		Capabilities:     item.Capabilities,
		IncludeInSyncAll: item.IncludeInSyncAll,
		Offline:          item.Offline,
	}
}

func TestBankAccessItemNeverPrintsTheUserID(t *testing.T) {
	item := bankAccessItem()

	rendered := []string{
		item.String(),
		item.GoString(),
		fmt.Sprintf("%v", item),
		fmt.Sprintf("%+v", item),
		fmt.Sprintf("%#v", item),
		fmt.Sprintf("%s", item),
		fmt.Sprintf("%q", item),
	}
	for _, text := range rendered {
		if strings.Contains(text, secretUserID) {
			t.Errorf("rendered BankAccessItem leaked the user id: %s", text)
		}
		if !strings.Contains(text, "***") {
			t.Errorf("rendered BankAccessItem does not show a redacted user id: %s", text)
		}
	}
}

func TestBankConnectionNeverPrintsTheUserID(t *testing.T) {
	connection := bankConnection()

	rendered := []string{
		connection.String(),
		connection.GoString(),
		fmt.Sprintf("%v", connection),
		fmt.Sprintf("%+v", connection),
		fmt.Sprintf("%#v", connection),
		fmt.Sprintf("%s", connection),
		fmt.Sprintf("%q", connection),
	}
	for _, text := range rendered {
		if strings.Contains(text, secretUserID) {
			t.Errorf("rendered BankConnection leaked the user id: %s", text)
		}
		if !strings.Contains(text, "***") {
			t.Errorf("rendered BankConnection does not show a redacted user id: %s", text)
		}
	}
}

func TestTransactionItemHelpers(t *testing.T) {
	partner := "Shop"
	purpose := "Receipt"
	blank := "   "

	item := TransactionItem{
		Amount:      money.Money{Minor: -100, Currency: money.EUR},
		PartnerName: &partner,
		Purpose:     &purpose,
	}
	if got := item.RawDescription(); got != "Shop" {
		t.Errorf("RawDescription with a partner = %q", got)
	}
	if !item.IsExpense() || !item.CountsAsSpending() || item.CountsAsIncome() {
		t.Errorf("a negative normal booking must count as spending")
	}

	item.PartnerName = &blank
	if got := item.RawDescription(); got != "Receipt" {
		t.Errorf("RawDescription with a blank partner = %q", got)
	}
	item.Purpose = &blank
	if got := item.RawDescription(); got != "—" {
		t.Errorf("RawDescription with no text = %q", got)
	}

	item.Amount = money.Money{Minor: 100, Currency: money.EUR}
	item.IsTransfer = true
	if item.IsExpense() || item.CountsAsSpending() || item.CountsAsIncome() {
		t.Errorf("a transfer must count as neither income nor spending")
	}
	item.IsTransfer = false
	if !item.CountsAsIncome() || item.CountsAsSpending() {
		t.Errorf("a positive normal booking must count as income")
	}
}

func TestDocumentItemHelpers(t *testing.T) {
	pdf := DocumentItem{MIMEType: "application/pdf"}
	if !pdf.IsPDF() || pdf.IsImage() {
		t.Errorf("a PDF must be a PDF and not an image")
	}
	image := DocumentItem{MIMEType: "image/png"}
	if image.IsPDF() || !image.IsImage() {
		t.Errorf("a PNG must be an image and not a PDF")
	}
	text := DocumentItem{MIMEType: "text/plain"}
	if text.IsPDF() || text.IsImage() {
		t.Errorf("plain text is neither a PDF nor an image")
	}
}

func TestEnumStoredTextsRoundTrip(t *testing.T) {
	accountTypes := []AccountType{
		AccountTypeChecking,
		AccountTypeSavings,
		AccountTypeCreditCard,
		AccountTypeLoan,
		AccountTypeDepot,
	}
	for _, accountType := range accountTypes {
		got, ok := AccountTypeFromStored(accountType.AsStr())
		if !ok || got != accountType {
			t.Errorf("AccountTypeFromStored(%q) = %v, %v", accountType.AsStr(), got, ok)
		}
	}
	if _, ok := AccountTypeFromStored("bogus"); ok {
		t.Error("AccountTypeFromStored accepted bogus")
	}

	fields := []RuleField{RuleFieldAny, RuleFieldPartner, RuleFieldPurpose}
	for _, field := range fields {
		got, ok := RuleFieldFromStored(field.AsStr())
		if !ok || got != field {
			t.Errorf("RuleFieldFromStored(%q) = %v, %v", field.AsStr(), got, ok)
		}
	}
	if _, ok := RuleFieldFromStored("bogus"); ok {
		t.Error("RuleFieldFromStored accepted bogus")
	}

	directions := []RuleDirection{RuleDirectionAny, RuleDirectionExpense, RuleDirectionIncome}
	for _, direction := range directions {
		got, ok := RuleDirectionFromStored(direction.AsStr())
		if !ok || got != direction {
			t.Errorf("RuleDirectionFromStored(%q) = %v, %v", direction.AsStr(), got, ok)
		}
	}
	if _, ok := RuleDirectionFromStored("bogus"); ok {
		t.Error("RuleDirectionFromStored accepted bogus")
	}
}
