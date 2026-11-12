package accountids

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/iban"
)

const accountIban = "DE89370400440532013000"

func TestACardIdentifierIsNotAnIban(t *testing.T) {
	if got := Card("4321"); got != "CARD4321" {
		t.Errorf("Card = %q", got)
	}
	if !IsCard("CARD4321") {
		t.Error("CARD4321 is not a card")
	}
	if iban.IsValid("CARD4321") {
		t.Error("a card id has no IBAN length")
	}
	if got := iban.Normalize("CARD4321"); got != "CARD4321" {
		t.Errorf("normalize(card) = %q", got)
	}
	if IsCard(accountIban) {
		t.Error("an IBAN is a card")
	}
	for _, id := range []string{"CARD432", "CARD43219", "HEAD12"} {
		if IsCard(id) {
			t.Errorf("%q is a card", id)
		}
	}
}

func TestDisplayShowsACardsLast4DigitsAndFormatsAnIbanAsBefore(t *testing.T) {
	if got := Display("CARD4321"); got != "•••• 4321" {
		t.Errorf("Display(card) = %q", got)
	}
	if got := Display(accountIban); got != iban.Format(accountIban) {
		t.Errorf("Display(iban) = %q", got)
	}
}

func TestCardTrimsTheLastFourDigits(t *testing.T) {
	if got := Card(" 4321 "); got != "CARD4321" {
		t.Errorf("Card with spaces = %q", got)
	}
}
