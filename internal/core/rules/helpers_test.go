package rules

import (
	"fmt"
	"strconv"
)

// itoa formats an integer the way the rule-file tests write it.
func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

// stringPointer returns a pointer to value.
func stringPointer(value string) *string {
	return &value
}

// paymentPaypalPurpose builds the purpose the payment-provider tests use.
func paymentPaypalPurpose(shop string) string {
	return fmt.Sprintf("EREF+REF001 SVWZ+PP.0471.PPX . %s, Ihr Einkauf bei %s", shop, shop)
}
