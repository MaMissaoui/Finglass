package fints

import (
	"reflect"
	"testing"
)

func TestLooksLikeIBAN(t *testing.T) {
	valid := []string{
		testIBAN,
		testIBAN2,
		"GB00TEST00000000000001",
		"FR000000000000000000000001",
	}
	for _, value := range valid {
		if !looksLikeIBAN(value) {
			t.Errorf("looksLikeIBAN(%q) = false, want true", value)
		}
	}

	invalid := []string{
		"",
		"DE00",
		"1234567890123456",
		"de00999999990000000001",
		"DE00-999999990000000001",
	}
	for _, value := range invalid {
		if looksLikeIBAN(value) {
			t.Errorf("looksLikeIBAN(%q) = true, want false", value)
		}
	}
}

func TestLooksLikeBIC(t *testing.T) {
	valid := []string{"TESTDEFF", "TESTDEFFXXX", "TESTDEFF001"}
	for _, value := range valid {
		if !looksLikeBIC(value) {
			t.Errorf("looksLikeBIC(%q) = false, want true", value)
		}
	}

	invalid := []string{"", "TESTDE", "TESTDEFF1", "12STDEFFXXX"}
	for _, value := range invalid {
		if looksLikeBIC(value) {
			t.Errorf("looksLikeBIC(%q) = true, want false", value)
		}
	}
}

func TestParseHISPA(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Account
	}{
		{
			name:  "with SEPA flag",
			input: "HISPA:2:4:4+J:DE00999999990000000001:TESTDEFFXXX:00000001::280:99999999'",
			want: []Account{{
				IBAN:          testIBAN,
				BIC:           testBIC,
				AccountNumber: "00000001",
				BLZ:           testBLZ,
			}},
		},
		{
			name:  "without SEPA flag",
			input: "HISPA:3:4:4+DE00999999990000000002:TESTDEFFXXX:00000002::280:99999999'",
			want: []Account{{
				IBAN:          testIBAN2,
				BIC:           testBIC,
				AccountNumber: "00000002",
				BLZ:           testBLZ,
			}},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			accounts := ParseHISPA(parseOne(t, testCase.input))
			if !reflect.DeepEqual(accounts, testCase.want) {
				t.Fatalf("ParseHISPA = %#v, want %#v", accounts, testCase.want)
			}
		})
	}
}

func TestParseHISPARequiresIBAN(t *testing.T) {
	accounts := ParseHISPA(parseOne(t, "HISPA:4:4:4+J::TESTDEFFXXX:00000003::280:99999999'"))
	if len(accounts) != 0 {
		t.Fatalf("ParseHISPA = %#v, want none", accounts)
	}
}

func TestParseHIUPD(t *testing.T) {
	input := "HIUPD:7:6:4+1::280:99999999+DE00999999990000000001+test-customer+G+EUR+Erika Musterfrau++Girokonto+HKSAL:1+HKKAZ:1+TESTDEFFXXX'"
	account, ok := ParseHIUPD(parseOne(t, input))
	if !ok {
		t.Fatal("ParseHIUPD reported no account")
	}
	if account.IBAN != testIBAN {
		t.Fatalf("IBAN = %q", account.IBAN)
	}
	if account.BIC != testBIC {
		t.Fatalf("BIC = %q", account.BIC)
	}
	if account.AccountNumber != "1" {
		t.Fatalf("AccountNumber = %q", account.AccountNumber)
	}
	if account.SubAccount != "" {
		t.Fatalf("SubAccount = %q", account.SubAccount)
	}
	if account.BLZ != testBLZ {
		t.Fatalf("BLZ = %q", account.BLZ)
	}
	if account.Currency != "EUR" {
		t.Fatalf("Currency = %q", account.Currency)
	}
	if account.AccountType != "G" {
		t.Fatalf("AccountType = %q", account.AccountType)
	}
	if account.Owner() != "Erika Musterfrau" {
		t.Fatalf("Owner = %q", account.Owner())
	}
	if account.ProductName() != "Girokonto" {
		t.Fatalf("ProductName = %q", account.ProductName())
	}
	wantTransactions := []string{"HKSAL", "HKKAZ"}
	if !reflect.DeepEqual(account.AllowedBusinessTransactions, wantTransactions) {
		t.Fatalf("AllowedBusinessTransactions = %v", account.AllowedBusinessTransactions)
	}
}

func TestParseHIUPDOwnerAndProductFallbacks(t *testing.T) {
	input := "HIUPD:8:6:4+2::280:99999999+DE00999999990000000002+test-customer++EUR++Erika Musterfrau++Tagesgeld+HKSAL:1'"
	account, ok := ParseHIUPD(parseOne(t, input))
	if !ok {
		t.Fatal("ParseHIUPD reported no account")
	}
	if account.Owner() != "Erika Musterfrau" {
		t.Fatalf("Owner = %q", account.Owner())
	}
	if account.ProductName() != "Tagesgeld" {
		t.Fatalf("ProductName = %q", account.ProductName())
	}
	if !reflect.DeepEqual(account.AllowedBusinessTransactions, []string{"HKSAL"}) {
		t.Fatalf("AllowedBusinessTransactions = %v", account.AllowedBusinessTransactions)
	}
}

func TestParseHIUPDRejectsShortSegment(t *testing.T) {
	if _, ok := ParseHIUPD(parseOne(t, "HIUPD:9:6:4+1'")); ok {
		t.Fatal("ParseHIUPD accepted a segment without an account group")
	}
}

func TestResponseAccounts(t *testing.T) {
	input := "HISPA:1:4:4+J:DE00999999990000000001:TESTDEFFXXX:00000001::280:99999999'" +
		"HIUPD:2:6:4+2::280:99999999+DE00999999990000000002+test-customer++EUR+Erika Musterfrau++Tagesgeld+HKSAL:1'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	accounts := response.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("Accounts length = %d, want 2", len(accounts))
	}
	if accounts[0].IBAN != testIBAN || accounts[1].IBAN != testIBAN2 {
		t.Fatalf("Accounts = %#v", accounts)
	}
}
