// Package fintsprobe is the read-only FinTS feasibility probe the user runs
// against their own bank.
//
// The package holds all probe logic without reading the environment, the
// terminal or the network. The command in cmd/finglass-probe supplies those.
package fintsprobe

import (
	"fmt"
	"strconv"
	"strings"
)

// The environment variables the probe reads.
const (
	// ProductIDVar holds the FinTS product registration id. It is optional in
	// the environment: when it is unset, the command asks for it without echo.
	ProductIDVar = "FINGLASS_FINTS_PRODUCT_ID"
	// BLZVar holds the German bank code.
	BLZVar = "FINGLASS_PROBE_BLZ"
	// UserVar holds the online-banking login.
	UserVar = "FINGLASS_PROBE_USER"
	// URLVar holds the FinTS server URL. It is required: the probe has no bank
	// registry.
	URLVar = "FINGLASS_PROBE_URL"
	// DaysVar holds the statement period in days.
	DaysVar = "FINGLASS_PROBE_DAYS"
)

const (
	defaultDays = 30
	minDays     = 1
	maxDays     = 90
)

// Inputs are the validated probe inputs. It deliberately contains no PIN and no
// TAN.
type Inputs struct {
	// ProductID is the DK registration id, or "" when the environment did not
	// provide one and the command still has to ask for it.
	ProductID string
	// BLZ is the bank code, exactly eight ASCII digits.
	BLZ string
	// User is the online-banking login.
	User string
	// URL is the FinTS server URL.
	URL string
	// Days is the statement period in days, from 1 to 90.
	Days int
}

// String prints the BLZ and the period and masks the product id, the user id
// and the URL path. It never prints a secret or a login.
func (i Inputs) String() string {
	return fmt.Sprintf(
		"fintsprobe.Inputs{productID:%s blz:%s user:%s url:%s days:%d}",
		maskedPresence(i.ProductID), i.BLZ, maskedPresence(i.User), maskedURL(i.URL), i.Days,
	)
}

// GoString prints the same masked form as String.
func (i Inputs) GoString() string {
	return i.String()
}

// Format writes the masked form for every fmt verb.
func (i Inputs) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(i.String()))
}

func maskedPresence(value string) string {
	if value == "" {
		return `""`
	}
	return "***"
}

// maskedURL keeps the scheme and host and drops the path, the query and any
// user information.
func maskedURL(raw string) string {
	const scheme = "https://"
	if raw == "" {
		return `""`
	}
	if len(raw) < len(scheme) || !strings.EqualFold(raw[:len(scheme)], scheme) {
		return "***"
	}
	rest := raw[len(scheme):]
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		rest = rest[:index]
	}
	if rest == "" || strings.Contains(rest, "@") {
		return "***"
	}
	return scheme + rest
}

// InputError is one invalid input. Its text names the variable and the problem
// and never the value.
type InputError struct {
	// Variable is the environment variable's name.
	Variable string
	// Message is a fixed description that contains no input value.
	Message string
}

// Error implements error. It never contains the invalid value.
func (e InputError) Error() string {
	return e.Variable + ": " + e.Message
}

// ParseInputs validates the probe inputs through lookup.
//
// lookup is the only source of environment values. A missing or empty product
// id is not an error: the command asks for it later. Every other error is
// reported without the value, and all of them are returned together.
func ParseInputs(lookup func(string) (string, bool)) (Inputs, []InputError) {
	inputs := Inputs{Days: defaultDays}
	errors := make([]InputError, 0)

	if value, ok := lookupTrimmed(lookup, ProductIDVar); ok {
		if !validProductID(value) {
			errors = append(errors, InputError{
				Variable: ProductIDVar,
				Message:  "must be 1 to 30 ASCII letters or digits",
			})
		} else {
			inputs.ProductID = value
		}
	}

	if value, ok := lookupTrimmed(lookup, BLZVar); ok {
		inputs.BLZ = value
		if !validBLZ(value) {
			errors = append(errors, InputError{
				Variable: BLZVar,
				Message:  "must be exactly 8 ASCII digits",
			})
		}
	} else {
		errors = append(errors, InputError{Variable: BLZVar, Message: "is required"})
	}

	if value, ok := lookupTrimmed(lookup, UserVar); ok {
		inputs.User = value
		if strings.ContainsAny(value, "?'+:@") {
			errors = append(errors, InputError{
				Variable: UserVar,
				Message:  "must not contain the FinTS separators ? ' + : @",
			})
		}
	} else {
		errors = append(errors, InputError{Variable: UserVar, Message: "is required"})
	}

	if value, ok := lookupTrimmed(lookup, URLVar); ok {
		inputs.URL = value
		if !validProbeURL(value) {
			errors = append(errors, InputError{
				Variable: URLVar,
				Message:  "must be an https URL with a host and no user information or fragment",
			})
		}
	} else {
		errors = append(errors, InputError{Variable: URLVar, Message: "is required"})
	}

	if value, ok := lookupTrimmed(lookup, DaysVar); ok {
		days, err := strconv.Atoi(value)
		if err != nil || days < minDays || days > maxDays {
			errors = append(errors, InputError{
				Variable: DaysVar,
				Message:  fmt.Sprintf("must be a whole number from %d to %d", minDays, maxDays),
			})
		} else {
			inputs.Days = days
		}
	}

	return inputs, errors
}

func lookupTrimmed(lookup func(string) (string, bool), name string) (string, bool) {
	value, ok := lookup(name)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// ValidProductID reports whether value is a valid DK registration id. It never
// reveals the value.
func ValidProductID(value string) bool {
	return validProductID(value)
}

// validProductID copies fintsproduct's rule: 1 to 30 ASCII letters or digits.
// The restricted set also keeps FinTS syntax separators out.
func validProductID(value string) bool {
	if len(value) < 1 || len(value) > 30 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validBLZ(value string) bool {
	if len(value) != 8 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

// validProbeURL mirrors httptransport's URL rule without importing net/url:
// https, a host, no user information and no fragment.
func validProbeURL(raw string) bool {
	const scheme = "https://"
	if raw == "" || len(raw) < len(scheme) || !strings.EqualFold(raw[:len(scheme)], scheme) {
		return false
	}
	for index := 0; index < len(raw); index++ {
		if raw[index] <= ' ' || raw[index] == 0x7f {
			return false
		}
	}
	rest := raw[len(scheme):]
	if strings.Contains(rest, "#") {
		return false
	}
	// url.Parse, which httptransport uses, rejects an invalid percent escape.
	for index := 0; index < len(rest); index++ {
		if rest[index] != '%' {
			continue
		}
		if index+2 >= len(rest) || !isHexDigit(rest[index+1]) || !isHexDigit(rest[index+2]) {
			return false
		}
	}
	authority := rest
	if index := strings.IndexAny(rest, "/?"); index >= 0 {
		authority = rest[:index]
	}
	if authority == "" || strings.ContainsAny(authority, "@\\") {
		return false
	}
	host := authority
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end <= 1 {
			return false
		}
		host = host[1:end]
	} else if index := strings.LastIndex(host, ":"); index >= 0 {
		port := host[index+1:]
		if port == "" || !allDigits(port) {
			return false
		}
		host = host[:index]
	}
	return host != ""
}

func isHexDigit(character byte) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F')
}

func allDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}
