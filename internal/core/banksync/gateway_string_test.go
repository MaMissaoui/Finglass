package banksync

import (
	"fmt"
	"strings"
	"testing"
)

func TestFinTsConnectionNeverPrintsTheProductID(t *testing.T) {
	const productID = "TESTPRODUCT0000000000000"
	connection := FinTsConnection{
		BankName:   "Bank",
		BLZ:        "99999999",
		UserID:     "user",
		CustomerID: "customer",
		ServerURL:  "https://fints.example",
		ProductID:  productID,
	}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		rendered := fmt.Sprintf(format, connection)
		if strings.Contains(rendered, productID) {
			t.Fatalf("format %s printed the product id", format)
		}
		if !strings.Contains(rendered, "***") {
			t.Fatalf("format %s did not mask the product id: %q", format, rendered)
		}
	}
}

func TestFinTsConnectionKeepsAnAbsentProductIDEmpty(t *testing.T) {
	rendered := (FinTsConnection{}).String()
	if !strings.Contains(rendered, `ProductID:""`) {
		t.Fatalf("String() = %q, want an empty product id", rendered)
	}
}

func TestFinTsConfigAndClientNeverPrintTheProductIDOrLogin(t *testing.T) {
	const productID = "TESTPRODUCT0000000000000"
	const userID = "test-user-4711"
	const customerID = "test-customer-4712"
	config := NewFinTsConfig("Bank", "99999999", "TESTDEFFXXX", userID, productID)
	customer := customerID
	config.CustomerID = &customer
	client := NewFinTsClient(nil, "/tmp/finglass-test-profile", productID, nil)

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		for name, value := range map[string]any{"config": config, "config pointer": &config, "client": client} {
			rendered := fmt.Sprintf(format, value)
			for _, secret := range []string{productID, userID, customerID, "/tmp/finglass-test-profile"} {
				if strings.Contains(rendered, secret) {
					t.Fatalf("format %s of the %s printed a protected value", format, name)
				}
			}
			if !strings.Contains(rendered, "***") {
				t.Fatalf("format %s of the %s did not mask the product id: %q", format, name, rendered)
			}
		}
	}
}
