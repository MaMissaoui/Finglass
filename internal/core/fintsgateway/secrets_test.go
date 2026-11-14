package fintsgateway

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestGatewayOutcomeAndErrorsNeverPrintSecrets(t *testing.T) {
	const (
		pin         = "12345"
		tan         = "135790"
		owner       = "Erika Musterfrau"
		challenge   = "Approve in the app"
		bankMessage = "Konto gesperrt"
		bookingText = "Bakery purchase"
	)
	iban := testIBAN
	product := testProduct
	user := testUser
	systemID := testSystemID
	purpose := bookingText
	partner := "Bakery GmbH"
	amount := decimal.New(12345, 2)

	outcome := banksync.FetchOutcome{
		Snapshots: []banksync.AccountSnapshot{{
			Account: banksync.FinTsAccount{
				IBAN:          iban,
				BIC:           testBIC,
				AccountNumber: "00000001",
				BankName:      "Test Bank",
				BLZ:           testBLZ,
				AccountType:   "checking",
				Currency:      "EUR",
			},
			Balance: &banksync.FinTsBalance{
				Balance:  amount,
				Currency: "EUR",
				Date:     time.Now(),
			},
			Transactions: []banksync.FinTsTransaction{{
				Date:        civil.FromTime(time.Now()),
				ValueDate:   civil.FromTime(time.Now()),
				Amount:      amount,
				Currency:    "EUR",
				PartnerName: &partner,
				Purpose:     &purpose,
			}},
		}},
		TanMethod:    stringPointer("942"),
		Capabilities: &banksync.BankCapabilities{Jobs: map[string]struct{}{banksync.JobBalance: {}}},
		SystemID:     &systemID,
		HKTANVersion: 7,
	}

	values := []any{
		*New(),
		New(),
		outcome,
		&outcome,
		banksync.NewPinEntry(secret.NewText([]byte(pin)), true),
		banksync.TanChallenge{Message: challenge},
		banksync.NewGatewayNetworkError(),
		banksync.NewGatewayHTTPStatusError(503),
		banksync.NewGatewayBankError([]string{"9942"}),
		banksync.NewGatewayPinRejectedError(),
		banksync.NewGatewayCancelledError(),
		banksync.NewGatewayOtherError(bankMessage),
		testConnectionWithSecrets(product, user, systemID),
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			rendered := fmt.Sprintf(format, value)
			for _, forbidden := range []string{
				pin, tan, product, user, systemID, iban, owner, challenge, bankMessage,
				bookingText, partner, amount.String(),
			} {
				if strings.Contains(rendered, forbidden) {
					t.Fatalf("%s of %T leaked %q: %q", format, value, forbidden, rendered)
				}
			}
		}
	}
}

func testConnectionWithSecrets(product, user, systemID string) banksync.FinTsConnection {
	connection := testConnection()
	connection.ProductID = product
	connection.UserID = user
	connection.CustomerID = user
	connection.SystemID = systemID
	return connection
}

func stringPointer(value string) *string {
	return &value
}
