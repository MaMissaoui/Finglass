package banksync

import "fmt"

// FinTsConfig is the configuration for a FinTS/HBCI banking connection.
//
// The product registration id is deliberately a value supplied by the caller
// (or read from FINGLASS_FINTS_PRODUCT_ID by the caller before constructing
// this), never a constant in core.
type FinTsConfig struct {
	// BankName is the bank's display name.
	BankName string
	// BLZ is the German bank code.
	BLZ string
	// BIC is the BIC, when known.
	BIC string
	// UserID is the online-banking login.
	UserID string
	// CustomerID is the customer id. nil means "the same as the user id", as in
	// FinDesk.
	CustomerID *string
	// HBCIVersion is the HBCI version.
	HBCIVersion string
	// ServerURL is the FinTS server URL, when the user entered one.
	ServerURL *string
	// TANMethod is a remembered TAN procedure id.
	TANMethod *string
	// ProductID is the FinTS product registration id.
	ProductID string
}

// NewFinTsConfig builds a configuration with FinDesk's defaults for the version
// and customer id.
func NewFinTsConfig(bankName, blz, bic, userID, productID string) FinTsConfig {
	return FinTsConfig{
		BankName:    bankName,
		BLZ:         blz,
		BIC:         bic,
		UserID:      userID,
		CustomerID:  nil,
		HBCIVersion: "300",
		ServerURL:   nil,
		TANMethod:   nil,
		ProductID:   productID,
	}
}

// CustomerIDOrDefault returns the customer id, defaulting to the user id when
// the bank does not distinguish them.
func (c FinTsConfig) CustomerIDOrDefault() string {
	if c.CustomerID != nil {
		return *c.CustomerID
	}
	return c.UserID
}

// String returns a representation that never contains the bank user id, the
// customer id or the product id.
func (c FinTsConfig) String() string {
	customer := "<nil>"
	if c.CustomerID != nil {
		customer = "***"
	}
	return fmt.Sprintf(
		"FinTsConfig{BankName:%q BLZ:%q BIC:%q UserID:%q CustomerID:%s HBCIVersion:%q ServerURL:%v TANMethod:%v ProductID:%q}",
		c.BankName, c.BLZ, c.BIC, "***", customer, c.HBCIVersion, stringOrNil(c.ServerURL), stringOrNil(c.TANMethod),
		maskedProductID(c.ProductID),
	)
}

// GoString returns a representation that never contains the bank user id, the
// customer id or the product id.
func (c FinTsConfig) GoString() string {
	return c.String()
}

// Format returns a representation that never contains the bank user id, the
// customer id or the product id, for every fmt verb.
func (c FinTsConfig) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(c.String()))
}

// stringOrNil quotes an optional value or prints <nil>.
func stringOrNil(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%q", *value)
}
