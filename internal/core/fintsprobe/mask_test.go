package fintsprobe

import "testing"

const (
	germanIBAN   = "DE89 3704 0044 0532 0130 00"
	austrianIBAN = "AT61 1904 3002 3457 3201"
)

func TestMasksGermanIBANWithSpaces(t *testing.T) {
	if got := MaskIBAN(germanIBAN); got != "DE** **** **** **** **30 00" {
		t.Fatalf("MaskIBAN = %q", got)
	}
}

func TestMasksGermanIBANWithoutSpaces(t *testing.T) {
	if got := MaskIBAN("DE89370400440532013000"); got != "DE****************3000" {
		t.Fatalf("MaskIBAN = %q", got)
	}
}

func TestMasksAustrianIBANWithSpaces(t *testing.T) {
	if got := MaskIBAN(austrianIBAN); got != "AT** **** **** **** 3201" {
		t.Fatalf("MaskIBAN = %q", got)
	}
}

func TestMasksAustrianIBANWithoutSpaces(t *testing.T) {
	if got := MaskIBAN("AT611904300234573201"); got != "AT**************3201" {
		t.Fatalf("MaskIBAN = %q", got)
	}
}

func TestMasksShortValuesCompletely(t *testing.T) {
	cases := map[string]string{
		"ABC": "***",
		"AB":  "**",
		"":    "",
		"A B": "* *",
	}
	for input, want := range cases {
		if got := MaskIBAN(input); got != want {
			t.Fatalf("MaskIBAN(%q) = %q, want %q", input, got, want)
		}
	}
}
