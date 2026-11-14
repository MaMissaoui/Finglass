// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "fmt"

// maskTail masks an IBAN or account number to its last four characters. It is
// used only by String, GoString and Format.
func maskTail(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return "…" + string(runes)
	}
	return "…" + string(runes[len(runes)-4:])
}

// Account is a SEPA account from HISPA or HIUPD.
type Account struct {
	// IBAN is the international account number.
	IBAN string
	// BIC is the bank identifier code.
	BIC string
	// AccountNumber is the national account number.
	AccountNumber string
	// SubAccount is the sub-account.
	SubAccount string
	// BLZ is the German bank code.
	BLZ string
	// Currency is the account currency.
	Currency string
	// AccountType is the account type code from HIUPD, when present.
	AccountType string
	// AllowedBusinessTransactions lists the business segment types the bank
	// allows for this account in HIUPD, when present.
	AllowedBusinessTransactions []string

	owner       string
	productName string
}

// Owner returns the account owner as sent by the bank.
//
// The owner may carry account numbers; never log or audit it.
func (a Account) Owner() string {
	return a.owner
}

// ProductName returns the bank's product name for the account.
//
// The product name may carry account numbers; never log or audit it.
func (a Account) ProductName() string {
	return a.productName
}

// String masks the IBAN and account numbers and never prints owner or product
// name text.
func (a Account) String() string {
	return fmt.Sprintf(
		"fints.Account{iban:%s bic:%s accountNumber:%s subAccount:%s blz:%s currency:%s accountType:%s transactions:%d hasOwner:%t hasProductName:%t}",
		maskTail(a.IBAN), a.BIC, maskTail(a.AccountNumber), maskTail(a.SubAccount),
		a.BLZ, a.Currency, a.AccountType, len(a.AllowedBusinessTransactions),
		a.owner != "", a.productName != "",
	)
}

// GoString masks the IBAN and account numbers and never prints bank text.
func (a Account) GoString() string {
	return a.String()
}

// Format writes the masked representation for every fmt verb.
func (a Account) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(a.String()))
}

// looksLikeIBAN ports fints-rs's looks_like_iban heuristic: 15 to 34
// characters, a two-letter country code, two check digits and an alphanumeric
// BBAN.
func looksLikeIBAN(value string) bool {
	if len(value) < 15 || len(value) > 34 {
		return false
	}
	bytes := []byte(value)
	if !isASCIIUpper(bytes[0]) || !isASCIIUpper(bytes[1]) {
		return false
	}
	if !isASCIIDigit(bytes[2]) || !isASCIIDigit(bytes[3]) {
		return false
	}
	for _, character := range bytes[4:] {
		if !isASCIIAlphanumeric(character) {
			return false
		}
	}
	return true
}

func isASCIIUpper(character byte) bool {
	return character >= 'A' && character <= 'Z'
}

func isASCIIAlpha(character byte) bool {
	return (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')
}

func isASCIIDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func isASCIIAlphanumeric(character byte) bool {
	return isASCIIAlpha(character) || isASCIIDigit(character)
}

// ParseHISPA ports fints-rs's parse_hispa: every DEG from index 1 on is one
// account connection, with or without the leading SEPA flag.
func ParseHISPA(segment ParsedSegment) []Account {
	accounts := make([]Account, 0, segment.DEGCount())
	for index := 1; index < segment.DEGCount(); index++ {
		group := segment.DEG(index)
		if group.Len() < 2 {
			continue
		}

		iban, bic, accountNumber, subAccount, blz := "", "", "", "", ""
		if group.Text(0) == "J" || group.Text(0) == "N" {
			iban = group.Text(1)
			bic = group.Text(2)
			accountNumber = group.Text(3)
			subAccount = group.Text(4)
			blz = group.Text(6)
		} else {
			iban = group.Text(0)
			bic = group.Text(1)
			accountNumber = group.Text(2)
			subAccount = group.Text(3)
			blz = group.Text(5)
		}
		if iban == "" {
			continue
		}

		accounts = append(accounts, Account{
			IBAN:          iban,
			BIC:           bic,
			AccountNumber: accountNumber,
			SubAccount:    subAccount,
			BLZ:           blz,
		})
	}
	return accounts
}

// ParseHIUPD ports fints-rs's parse_hiupd and adds the HIUPD account type and
// the allowed business transactions. It reports false for a segment too short
// to hold an account group.
func ParseHIUPD(segment ParsedSegment) (Account, bool) {
	if segment.DEGCount() < 3 {
		return Account{}, false
	}

	accountGroup := segment.DEG(1)
	account := Account{
		AccountNumber: accountGroup.Text(0),
		SubAccount:    accountGroup.Text(1),
		BLZ:           accountGroup.Text(3),
		Currency:      segment.DEG(5).Text(0),
		AccountType:   segment.DEG(4).Text(0),
	}

	for index := 1; index < segment.DEGCount(); index++ {
		value := segment.DEG(index).Text(0)
		// HIUPD has no BIC field: a guessed one took an 8-letter account
		// holder name as the BIC (Volksbank 9050/9110 on HKSAL). HISPA
		// supplies the BIC.
		if account.IBAN == "" && looksLikeIBAN(value) {
			account.IBAN = value
		}
	}

	account.owner = firstNonEmpty(segment.DEG(6).Text(0), segment.DEG(7).Text(0))
	account.productName = firstNonEmpty(segment.DEG(8).Text(0), segment.DEG(9).Text(0))
	account.AllowedBusinessTransactions = allowedBusinessTransactions(segment)
	return account, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func allowedBusinessTransactions(segment ParsedSegment) []string {
	transactions := make([]string, 0)
	seen := make(map[string]bool)
	for index := 1; index < segment.DEGCount(); index++ {
		value := segment.DEG(index).Text(0)
		if !looksLikeSegmentType(value) || seen[value] {
			continue
		}
		seen[value] = true
		transactions = append(transactions, value)
	}
	return transactions
}

// Accounts returns the accounts from every HISPA and HIUPD segment, in wire
// order.
func (r Response) Accounts() []Account {
	accounts := make([]Account, 0)
	for _, segment := range r.Segments {
		switch segment.Type() {
		case "HISPA":
			accounts = append(accounts, ParseHISPA(segment)...)
		case "HIUPD":
			if account, ok := ParseHIUPD(segment); ok {
				accounts = append(accounts, account)
			}
		}
	}
	return accounts
}
