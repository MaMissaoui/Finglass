package imports

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
)

func testDate(t *testing.T, year int, month int, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, time.Month(month), day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return date
}

func TestStripTrailingZerosToPlainStringMatchesJava(t *testing.T) {
	tests := []struct {
		value    string
		expected string
	}{
		{"10.50", "10.5"},
		{"100.00", "100"},
		{"-0.10", "-0.1"},
		{"0.00", "0"},
		{"1E+2", "100"},
		{"12.345", "12.345"},
		{"-0.00", "0"},
		{"1000", "1000"},
		{"0.000", "0"},
		{"123456789.10", "123456789.1"},
	}
	for _, test := range tests {
		if got := PlainAmount(dec(t, test.value)); got != test.expected {
			t.Errorf("PlainAmount(%s) = %q, want %q", test.value, got, test.expected)
		}
	}
}

func TestKnownAnswerHashes(t *testing.T) {
	cases := []struct {
		date     civil.Date
		amount   string
		partner  *string
		purpose  *string
		expected string
	}{
		{testDate(t, 2026, 10, 3), "10.50", stringPointer("Bäcker"), stringPointer("Kaffee"), "def58a318d1e3580b1e1c6b56d9c4e12ceede08565a636e9195c8baf1e28318e"},
		{testDate(t, 2026, 10, 1), "3200.00", stringPointer("Müller & Söhne GmbH"), stringPointer("Gehalt Oktober"), "bbe5a24b9c15d00906d8519c4a18b3aa5685d643701e23eb6e53f337e00f4edb"},
		{testDate(t, 2026, 10, 3), "-0.10", nil, stringPointer(""), "229787067b4d87a6bdd9f7e3c7bb02d0967548b2c537b67423fed703f731c32f"},
		{testDate(t, 2026, 1, 1), "0.00", stringPointer("Zero"), stringPointer("Nothing"), "eedd6a1d00bbc92171407ef48ac6ae47fd4d58c48931ffb186dda4e0df3acaed"},
		{testDate(t, 2025, 12, 31), "-89.99", stringPointer("AMAZON EU"), stringPointer("Bestellung 303-123; Monitor"), "08c886b9765918612094084a60461a6b3bf2f20ebed58124f0a5ee4a3392761d"},
	}
	for _, test := range cases {
		if got := TransactionHash(test.date, dec(t, test.amount), test.partner, test.purpose); got != test.expected {
			t.Errorf("TransactionHash(%s|%s) = %q, want %q", test.date, test.amount, got, test.expected)
		}
	}
}

func TestTransactionHashIsStableAndChangesWithTheInput(t *testing.T) {
	day := testDate(t, 2026, 10, 1)
	amount := dec(t, "-49.99")
	first := TransactionHash(day, amount, stringPointer("REWE"), stringPointer("Shopping"))
	if first == "" {
		t.Fatal("the hash is empty")
	}
	if first != TransactionHash(day, amount, stringPointer("REWE"), stringPointer("Shopping")) {
		t.Error("same input, different hash")
	}
	if first == TransactionHash(testDate(t, 2026, 10, 2), amount, stringPointer("REWE"), stringPointer("Shopping")) {
		t.Error("a different date is the same hash")
	}
}

func TestFileHashesUseReferenceThenBalanceThenOccurrence(t *testing.T) {
	withReference := ImportedBooking{
		Date:          testDate(t, 2026, 10, 3),
		Amount:        dec(t, "-4.50"),
		Currency:      stringPointer("EUR"),
		PartnerName:   stringPointer("Bäcker"),
		Purpose:       stringPointer("Kaffee"),
		BankReference: stringPointer("REF-1"),
	}
	withBalance := withReference
	withBalance.BankReference = nil
	withBalance.BalanceAfter = decimalPointer(dec(t, "100.00"))

	first := withReference
	first.BankReference = nil
	second := first

	hashes := Hashes("DE89370400440532013000", []ImportedBooking{withReference, withBalance, first, second})
	if hashes[0] != TransactionHash(testDate(t, 2026, 10, 3), dec(t, "-4.50"), stringPointer("Bäcker"), stringPointer("Kaffee#DE89370400440532013000#file:REF-1")) {
		t.Errorf("reference hash = %q", hashes[0])
	}
	if hashes[0] == hashes[1] {
		t.Error("reference and balance hashes are equal")
	}
	if hashes[2] == hashes[3] {
		t.Error("occurrence hashes are equal")
	}
	if hashes[2] != TransactionHash(testDate(t, 2026, 10, 3), dec(t, "-4.50"), stringPointer("Bäcker"), stringPointer("Kaffee#DE89370400440532013000#file:occ1")) {
		t.Errorf("first occurrence hash = %q", hashes[2])
	}
	if hashes[3] != TransactionHash(testDate(t, 2026, 10, 3), dec(t, "-4.50"), stringPointer("Bäcker"), stringPointer("Kaffee#DE89370400440532013000#file:occ2")) {
		t.Errorf("second occurrence hash = %q", hashes[3])
	}
	if hashes[1] != TransactionHash(testDate(t, 2026, 10, 3), dec(t, "-4.50"), stringPointer("Bäcker"), stringPointer("Kaffee#DE89370400440532013000#file:100")) {
		t.Errorf("balance hash = %q", hashes[1])
	}
}

func TestImportHashUsesTheBankIDFirst(t *testing.T) {
	expected := "cf4a97fe46f89bcde826419e4f4cd02a80351138ace839e137e006154e97987f"
	got := ImportHash(
		testDate(t, 2026, 10, 3),
		dec(t, "-45.67"),
		stringPointer("REWE Markt GmbH"),
		stringPointer("REWE SAGT DANKE 1234"),
		"DE89370400440532013000",
		stringPointer("booking-1"),
		decimalPointer(dec(t, "949.830")),
		stringPointer("PN-9"),
	)
	if got != expected {
		t.Errorf("ImportHash = %q, want %q", got, expected)
	}
}

func TestImportHashUsesTheRunningBalanceWhenThereIsNoBankID(t *testing.T) {
	expected := "46df355d0820dee9487a228a2a2f40e97c5e34a3f04017ac4e09a84255bb3ae0"
	got := ImportHash(
		testDate(t, 2026, 10, 3),
		dec(t, "-4.50"),
		stringPointer("BAECKEREI"),
		stringPointer("Kaffee"),
		"DE89370400440532013000",
		nil,
		decimalPointer(dec(t, "949.830")),
		stringPointer("PN-9"),
	)
	if got != expected {
		t.Errorf("ImportHash = %q, want %q", got, expected)
	}
}

func TestImportHashUsesThePrimaNotaWhenThereIsNeitherIDNorBalance(t *testing.T) {
	expected := "23b0646bbea98f9dbca26e24589f86095798bd717d21e3737abac40cb55cae91"
	got := ImportHash(
		testDate(t, 2026, 1, 2),
		dec(t, "100.00"),
		nil,
		nil,
		"DE44500105175407324931",
		nil,
		nil,
		stringPointer("PN-123"),
	)
	if got != expected {
		t.Errorf("ImportHash = %q, want %q", got, expected)
	}
}

func TestImportHashUsesAnEmptyDiscriminatorAsTheLastResort(t *testing.T) {
	expected := "6b805ba4fa4c0e531d9275235b0d0581b109d0548b094fc79059348d2cadd988"
	got := ImportHash(
		testDate(t, 2025, 12, 31),
		dec(t, "-0.10"),
		stringPointer("Zero"),
		stringPointer(""),
		"DE89370400440532013000",
		nil,
		nil,
		nil,
	)
	if got != expected {
		t.Errorf("ImportHash = %q, want %q", got, expected)
	}
}
