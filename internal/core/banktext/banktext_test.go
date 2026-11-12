package banktext

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func txn(partner *string, purpose *string) model.TransactionItem {
	iban := "DE89370400440532013000"
	date, ok := civil.New(2026, 10, 24)
	if !ok {
		panic("invalid test date")
	}
	return model.TransactionItem{
		ID:          model.TransactionID(1),
		AccountID:   model.AccountID(1),
		AccountIBAN: &iban,
		Date:        date,
		Amount:      money.Money{Minor: -4250, Currency: money.EUR},
		Currency:    money.EUR,
		PartnerName: partner,
		Purpose:     purpose,
		TaxSource:   model.TaxSourceNone,
	}
}

func text(value string) *string {
	return &value
}

func TestAPurposeWithEREFMREFCREDAndSVWZShowsOnlyTheSVWZText(t *testing.T) {
	raw := "EREF+REF0001 MREF+M-77 CRED+DE00ZZZ00000000001 SVWZ+Rechnung 4711"
	if got := DisplayPurpose(text(raw)); got == nil || *got != "Rechnung 4711" {
		t.Errorf("display_purpose = %v, want %q", got, "Rechnung 4711")
	}
}

func TestTagsGluedToTextWithoutSpacesAreStillSeparated(t *testing.T) {
	if got := DisplayPurpose(text("EREF+X1SVWZ+Miete Oktober")); got == nil || *got != "Miete Oktober" {
		t.Errorf("display_purpose = %v, want %q", got, "Miete Oktober")
	}
}

func TestSVWZTextEndsAtTheNextTag(t *testing.T) {
	if got := DisplayPurpose(text("SVWZ+Taschengeld PURP+RINP")); got == nil || *got != "Taschengeld" {
		t.Errorf("display_purpose = %v, want %q", got, "Taschengeld")
	}
}

func TestAPurposeOfOnlyTagsWithoutSVWZShowsTheUntaggedRemainderOrNothing(t *testing.T) {
	// Nothing before the first tag and nothing after the last: no readable text
	// at all.
	if got := DisplayPurpose(text("EREF+B00520201172143239 MREF+P0202011731458239")); got != nil {
		t.Errorf("display_purpose = %v, want nil", got)
	}
	// Text left of the tags is outside the tagged fields.
	if got := DisplayPurpose(text("Kartenzahlung EREF+REF0001")); got == nil || *got != "Kartenzahlung" {
		t.Errorf("display_purpose = %v, want %q", got, "Kartenzahlung")
	}
}

func TestLowerCaseTagsAreRecognisedToo(t *testing.T) {
	if got := DisplayPurpose(text("svwz+abc")); got == nil || *got != "abc" {
		t.Errorf("display_purpose = %v, want %q", got, "abc")
	}
}

func TestAPlainPurposeWithoutTagsIsReturnedUnchangedExceptCollapsedWhitespace(t *testing.T) {
	if got := DisplayPurpose(text("Warenkauf  12.03.2026")); got == nil || *got != "Warenkauf 12.03.2026" {
		t.Errorf("display_purpose = %v, want %q", got, "Warenkauf 12.03.2026")
	}
}

func TestAPlusSignThatOpensNoTagIsLeftAlone(t *testing.T) {
	if got := DisplayPurpose(text("A+B Bau")); got == nil || *got != "A+B Bau" {
		t.Errorf("display_purpose = %v, want %q", got, "A+B Bau")
	}
}

func TestTheFirstNonBlankPartOfAPaddedPartnerIsShown(t *testing.T) {
	got := DisplayPartner(text("BEISPIEL KARTE GOLD      1234 56XX XXXX 7890      Abrechnung 10/26"))
	if got == nil || *got != "BEISPIEL KARTE GOLD" {
		t.Errorf("display_partner = %v, want %q", got, "BEISPIEL KARTE GOLD")
	}
}

func TestADoubleSpaceInAPartnerCollapsesToOne(t *testing.T) {
	if got := DisplayPartner(text("BEISPIEL  GMBH")); got == nil || *got != "BEISPIEL GMBH" {
		t.Errorf("display_partner = %v, want %q", got, "BEISPIEL GMBH")
	}
}

func TestNullAndBlankResolveToNil(t *testing.T) {
	if got := DisplayPartner(nil); got != nil {
		t.Errorf("display_partner(nil) = %v, want nil", got)
	}
	if got := DisplayPartner(text("   ")); got != nil {
		t.Errorf("display_partner(blank) = %v, want nil", got)
	}
	if got := DisplayPurpose(nil); got != nil {
		t.Errorf("display_purpose(nil) = %v, want nil", got)
	}
	if got := DisplayPurpose(text("\t  ")); got != nil {
		t.Errorf("display_purpose(blank) = %v, want nil", got)
	}
}

func TestTheRowDescriptionPrefersThePartnerThenThePurposeThenAPlaceholder(t *testing.T) {
	withPartner := DisplayDescription(pointerItem(txn(text("Erika Mustermann"), text("EREF+R1 SVWZ+Rechnung 4711"))))
	withoutPartner := DisplayDescription(pointerItem(txn(nil, text("EREF+R1 SVWZ+Rechnung 4711"))))
	withNothing := DisplayDescription(pointerItem(txn(nil, nil)))
	if withPartner != "Erika Mustermann" {
		t.Errorf("with partner = %q, want %q", withPartner, "Erika Mustermann")
	}
	if withoutPartner != "Rechnung 4711" {
		t.Errorf("without partner = %q, want %q", withoutPartner, "Rechnung 4711")
	}
	if withNothing != "—" {
		t.Errorf("with nothing = %q, want %q", withNothing, "—")
	}
}

func TestTheSecondLineIsNilWhenAPaypalBookingNamesAShop(t *testing.T) {
	paypal := pointerItem(txn(text("PayPal Europe S.a.r.l. et Cie S.C.A"), text("Ihr Einkauf bei Beispiel Shop GmbH")))
	if got := SecondaryLine(paypal); got != nil {
		t.Errorf("secondary_line = %v, want nil (the title already names the provider and the shop)", got)
	}
}

func TestAShoplessPaypalBookingKeepsItsCleanedPurposeOnTheSecondLine(t *testing.T) {
	paypal := pointerItem(txn(text("PayPal Europe S.a.r.l. et Cie S.C.A"), text("PP.1234.PP ABBUCHUNG VOM GIROKONTO")))
	got := SecondaryLine(paypal)
	if got == nil || *got != "PP.1234.PP ABBUCHUNG VOM GIROKONTO" {
		t.Errorf("secondary_line = %v, want %q", got, "PP.1234.PP ABBUCHUNG VOM GIROKONTO")
	}
}

func TestAPlainBookingsSecondLineIsTheCleanedPurpose(t *testing.T) {
	plain := pointerItem(txn(text("Beispiel Inc."), text("EREF+R1 SVWZ+Rechnung 4711")))
	got := SecondaryLine(plain)
	if got == nil || *got != "Rechnung 4711" {
		t.Errorf("secondary_line = %v, want %q", got, "Rechnung 4711")
	}
}

func TestABookingWithoutAPartnerHasNoSecondLine(t *testing.T) {
	noPartner := pointerItem(txn(nil, text("SVWZ+Rechnung 4711")))
	if got := SecondaryLine(noPartner); got != nil {
		t.Errorf("secondary_line = %v, want nil", got)
	}
}

func TestAPaypalBookingIsTitledWithTheProviderAndTheShop(t *testing.T) {
	paypal := pointerItem(txn(
		text("PayPal Europe S.a.r.l. et Cie S.C.A"),
		text("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
	))
	if got := DisplayDescription(paypal); got != "PayPal · Beispiel Shop GmbH" {
		t.Errorf("display_description = %q, want %q", got, "PayPal · Beispiel Shop GmbH")
	}
}

func TestANonPaypalTitleIsUnchanged(t *testing.T) {
	plain := pointerItem(txn(
		text("Beispiel Shop GmbH"),
		text("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
	))
	if got := DisplayDescription(plain); got != "Beispiel Shop GmbH" {
		t.Errorf("display_description = %q, want %q (partner wins as before; no PayPal partner, no shop title)", got, "Beispiel Shop GmbH")
	}

	paypalPurposeOnly := pointerItem(txn(nil, text("SVWZ+Ihr Einkauf bei Beispiel Shop GmbH")))
	if got := DisplayDescription(paypalPurposeOnly); got != "Ihr Einkauf bei Beispiel Shop GmbH" {
		t.Errorf("display_description = %q, want %q (no PayPal partner: the purpose is shown as before)", got, "Ihr Einkauf bei Beispiel Shop GmbH")
	}
}

func TestNonASCIIBeforeAMatchDoesNotShiftRuneSlices(t *testing.T) {
	// regexp2 reports rune offsets. Slicing the string with those offsets would
	// corrupt this text because the non-ASCII prefix uses more bytes than runes.
	purpose := text("Überweisung für Miete EREF+X1 SVWZ+Miete Oktober")
	if got := DisplayPurpose(purpose); got == nil || *got != "Miete Oktober" {
		t.Errorf("display_purpose = %v, want %q", got, "Miete Oktober")
	}

	partner := text("Bäckerei Müller      1234 56XX XXXX 7890      Abrechnung 10/26")
	if got := DisplayPartner(partner); got == nil || *got != "Bäckerei Müller" {
		t.Errorf("display_partner = %v, want %q", got, "Bäckerei Müller")
	}
}

func pointerItem(item model.TransactionItem) *model.TransactionItem {
	return &item
}
