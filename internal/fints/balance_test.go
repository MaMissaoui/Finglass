package fints

import "testing"

func TestParseBalanceCredit(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR:20260315'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "1234.56" {
		t.Fatalf("Amount = %q", got)
	}
	if balance.Currency != "EUR" {
		t.Fatalf("Currency = %q", balance.Currency)
	}
	if !balance.HasDate || balance.Date != (Date{Year: 2026, Month: 3, Day: 15}) {
		t.Fatalf("Date = %v, %v", balance.Date, balance.HasDate)
	}
	if balance.PendingAmount != nil || balance.CreditLine != nil || balance.Available != nil {
		t.Fatalf("unexpected optional values: %#v", balance)
	}
}

func TestParseBalanceDebit(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+D:1234,56:EUR:20260315'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "-1234.56" {
		t.Fatalf("Amount = %q", got)
	}
}

func TestParseBalanceMissingDate(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if balance.HasDate {
		t.Fatalf("HasDate = true with Date %v", balance.Date)
	}
	if balance.Date != (Date{}) {
		t.Fatalf("Date = %v, want zero", balance.Date)
	}
}

func TestParseBalancePendingAndLimits(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:100,00:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if balance.PendingAmount == nil || balance.PendingAmount.String() != "-25.50" {
		t.Fatalf("PendingAmount = %v", balance.PendingAmount)
	}
	if !balance.HasPendingDate || balance.PendingDate != (Date{Year: 2026, Month: 3, Day: 14}) {
		t.Fatalf("PendingDate = %v, %v", balance.PendingDate, balance.HasPendingDate)
	}
	if balance.CreditLine == nil || balance.CreditLine.String() != "5000.00" {
		t.Fatalf("CreditLine = %v", balance.CreditLine)
	}
	if balance.Available == nil || balance.Available.String() != "900.00" {
		t.Fatalf("Available = %v", balance.Available)
	}
}

func TestParseBalanceKeepsManyDecimals(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:0,123456789:EUR:20260315'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if got := balance.Amount.String(); got != "0.123456789" {
		t.Fatalf("Amount = %q", got)
	}
}

func TestParseBalanceCurrencyFallback(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   string
		stated bool
	}{
		{
			name:   "balance currency",
			input:  "HISAL:5:7:4+1::280:99999999+Girokonto+USD+C:10,00:CHF:20260315'",
			want:   "CHF",
			stated: true,
		},
		{
			name:   "header currency",
			input:  "HISAL:5:7:4+1::280:99999999+Girokonto+USD+C:10,00::20260315'",
			want:   "USD",
			stated: true,
		},
		{
			name:   "euro default",
			input:  "HISAL:5:7:4+1::280:99999999+Girokonto++C:10,00::20260315'",
			want:   "EUR",
			stated: false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			balance, ok := ParseBalance(parseOne(t, testCase.input))
			if !ok {
				t.Fatal("ParseBalance reported no balance")
			}
			if balance.Currency != testCase.want {
				t.Fatalf("Currency = %q, want %q", balance.Currency, testCase.want)
			}
			if balance.CurrencyStated != testCase.stated {
				t.Fatalf("CurrencyStated = %t, want %t", balance.CurrencyStated, testCase.stated)
			}
		})
	}
}

func TestParseBalanceMalformedAmount(t *testing.T) {
	for _, input := range []string{
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:not-a-number:EUR:20260315'",
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C::EUR:20260315'",
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C'",
		"HISAL:5:7:4'",
	} {
		if _, ok := ParseBalance(parseOne(t, input)); ok {
			t.Fatalf("ParseBalance(%q) reported a balance", input)
		}
	}
}

func TestParseBalanceInvalidDate(t *testing.T) {
	for _, date := range []string{"20261340", "20260230", "20260000"} {
		balance, ok := ParseBalance(parseOne(t,
			"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:10,00:EUR:"+date+"'"))
		if !ok {
			t.Fatalf("ParseBalance(%s) reported no balance", date)
		}
		if balance.HasDate {
			t.Fatalf("HasDate = true for an invalid date %s: %v", date, balance.Date)
		}
	}
}

func TestParseBalanceLeapDay(t *testing.T) {
	balance, ok := ParseBalance(parseOne(t,
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:10,00:EUR:20240229'"))
	if !ok {
		t.Fatal("ParseBalance reported no balance")
	}
	if !balance.HasDate || balance.Date != (Date{Year: 2024, Month: 2, Day: 29}) {
		t.Fatalf("Date = %v, %v", balance.Date, balance.HasDate)
	}
}

func TestResponseBalance(t *testing.T) {
	response, err := ParseResponse([]byte(
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR:20260315'"))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	balance, ok := response.Balance()
	if !ok || balance.Amount.String() != "1234.56" {
		t.Fatalf("Balance = %#v, %v", balance, ok)
	}
}
