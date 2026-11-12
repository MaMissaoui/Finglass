package rules

import (
	"strings"
	"testing"
)

const testPaypalPartner = "PayPal Europe S.a.r.l. et Cie S.C.A"

func merchant(purpose string, partner string) *string {
	return MerchantOf(stringPointer(purpose), stringPointer(partner))
}

func assertShop(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("shop = nil, want %q", want)
	}
	if *got != want {
		t.Fatalf("shop = %q, want %q", *got, want)
	}
}

func assertNoShop(t *testing.T, got *string) {
	t.Helper()
	if got != nil {
		t.Fatalf("shop = %q, want nil", *got)
	}
}

func TestTheShopAfterTheMarkerIsExtractedFromAPurposeWithSepaTagsAndPadding(t *testing.T) {
	raw := "EREF+REF001          SVWZ+PP.0471.PPX .   Beispiel Shop GmbH,   Ihr Einkauf bei Beispiel Shop GmbH"
	assertShop(t, merchant(raw, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestAPaddedPartnerCountsAsPaypalWhenItsReadablePartDoes(t *testing.T) {
	partner := "PayPal Europe S.a.r.l.      Buchungscode 4711      Abrechnung 10/26"
	assertShop(t, merchant(paymentPaypalPurpose("Beispiel Shop GmbH"), partner), "Beispiel Shop GmbH")
}

func TestTheMarkerIsMatchedIgnoringCase(t *testing.T) {
	raw := "SVWZ+PP.0471.PPX . Beispiel Shop GmbH, IHR EINKAUF BEI Beispiel Shop GmbH"
	assertShop(t, merchant(raw, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestTrailingSeparatorsAreStrippedButATrailingDotIsKept(t *testing.T) {
	assertShop(
		t,
		merchant(paymentPaypalPurpose("Beispiel Shop S.a.r.l."), testPaypalPartner),
		"Beispiel Shop S.a.r.l.",
	)
	assertShop(
		t,
		merchant(paymentPaypalPurpose("Beispiel Shop GmbH,;:"), testPaypalPartner),
		"Beispiel Shop GmbH",
	)
}

func TestTheShopIsExtractedWithoutSvwzAndEndsAtTheNextTag(t *testing.T) {
	withoutSVWZ := "EREF+REF001 PP.0471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"
	assertShop(t, merchant(withoutSVWZ, testPaypalPartner), "Beispiel Shop GmbH")

	followedByTag := "EREF+REF001 Ihr Einkauf bei Beispiel Shop GmbH SVWZ+Rechnung 4711"
	assertShop(t, merchant(followedByTag, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestTitleOfCombinesTheProviderAndTheShop(t *testing.T) {
	title := TitleOf(stringPointer(paymentPaypalPurpose("Beispiel Shop GmbH")), stringPointer(testPaypalPartner))
	if title == nil || *title != "PayPal · Beispiel Shop GmbH" {
		t.Fatalf("title = %v, want %q", title, "PayPal · Beispiel Shop GmbH")
	}

	if title := TitleOf(
		stringPointer(paymentPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer("Beispiel Shop GmbH"),
	); title != nil {
		t.Fatalf("title = %q, want nil", *title)
	}
}

func TestAnOverLongShopNameIsCappedAt70Characters(t *testing.T) {
	long := strings.Repeat("BeispielShop", 12)
	shop := merchant(paymentPaypalPurpose(long), testPaypalPartner)
	if shop == nil {
		t.Fatal("shop = nil, want a capped name")
	}
	if count := len([]rune(*shop)); count != 70 {
		t.Fatalf("shop length = %d, want 70", count)
	}

	spaced := strings.TrimRight(strings.Repeat("Beispiel Shop ", 10), " ")
	assertShop(
		t,
		merchant(paymentPaypalPurpose(spaced), testPaypalPartner),
		strings.TrimRight(strings.Repeat("Beispiel Shop ", 5), " "),
	)
}

func TestANonPaypalPartnerOrNoneGivesNoneEvenWithAMarker(t *testing.T) {
	assertNoShop(t, merchant(paymentPaypalPurpose("Beispiel Shop GmbH"), "Beispiel Shop GmbH"))
	assertNoShop(t, merchant(paymentPaypalPurpose("Beispiel Shop GmbH"), "PP Muster GmbH"))
	assertNoShop(t, MerchantOf(stringPointer(paymentPaypalPurpose("Beispiel Shop GmbH")), nil))
	assertNoShop(t, merchant(paymentPaypalPurpose("Beispiel Shop GmbH"), "   "))
}

func TestAPurposeWithoutTheMarkerOrWithoutTextGivesNone(t *testing.T) {
	assertNoShop(t, merchant(
		"EREF+REF001 SVWZ+PP.0471.PPX . Beispiel Shop GmbH",
		testPaypalPartner,
	))
	assertNoShop(t, MerchantOf(nil, stringPointer(testPaypalPartner)))
	assertNoShop(t, merchant("   ", testPaypalPartner))
}

func TestAnEmptyTailAfterTheMarkerGivesNone(t *testing.T) {
	assertNoShop(t, merchant(paymentPaypalPurpose("   ., "), testPaypalPartner))
}

func TestTheMarkerIsFoundWithAnIDBeforeThePaypalID(t *testing.T) {
	raw := "1234567890123 PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"
	assertShop(t, merchant(raw, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestTheMarkerToleratesASpaceInsertedInsideAWord(t *testing.T) {
	inFirstWord := "PP.1234.PP . Beispiel Shop GmbH, Ihr Ein kauf bei Beispiel Shop GmbH"
	assertShop(t, merchant(inFirstWord, testPaypalPartner), "Beispiel Shop GmbH")

	inLastWord := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf b ei Beispiel Shop GmbH"
	assertShop(t, merchant(inLastWord, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestTheFullerOfThePreCommaNameAndTheMarkerShopWins(t *testing.T) {
	assertShop(
		t,
		merchant("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Sh", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
	assertShop(
		t,
		merchant("PP.1234.PP . Beispiel Sh, Ihr Einkauf bei Beispiel Shop GmbH", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
	assertShop(
		t,
		merchant("PP.1234.PP . Beispiel Sh op GmbH, Ihr Einkauf bei Beispiel Shop GmbH", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
	assertShop(
		t,
		merchant("PP.1234.PP . Anderer Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
}

func TestACodeTailAfterTheMarkerShopIsDropped(t *testing.T) {
	withShop := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH, AB12CD34"
	assertShop(t, merchant(withShop, testPaypalPartner), "Beispiel Shop GmbH")

	withoutShop := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei , AB12CD34"
	assertShop(t, merchant(withoutShop, testPaypalPartner), "Beispiel Shop GmbH")

	assertNoShop(t, merchant("Ihr Einkauf bei , AB12CD34", testPaypalPartner))
}

func TestAnUpperCaseTopUpNoteAfterTheMarkerShopIsCut(t *testing.T) {
	withAbbuchung := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH ABBUCHUNG VOM PAYPAL-KONTO"
	assertShop(t, merchant(withAbbuchung, testPaypalPartner), "Beispiel Shop GmbH")

	insertedSpace := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH ABBU CHUNG VOM PAYPAL-KONTO"
	assertShop(t, merchant(insertedSpace, testPaypalPartner), "Beispiel Shop GmbH")

	onlyVom := "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH VOM GIROKONTO"
	assertShop(t, merchant(onlyVom, testPaypalPartner), "Beispiel Shop GmbH")
}

func TestALowerCaseVomInsideTheShopNameIsKept(t *testing.T) {
	raw := "PP.1234.PP . Blumen vom Beispielhof, Ihr Einkauf bei Blumen vom Beispielhof"
	assertShop(t, merchant(raw, testPaypalPartner), "Blumen vom Beispielhof")
}

func TestAPreCommaNameWithoutTheMarkerIsTheShop(t *testing.T) {
	assertShop(t, merchant("PP.1234.PP . Beispiel Shop GmbH, Ihr Abo", testPaypalPartner), "Beispiel Shop GmbH")
}

func TestALeadingDotGivesThePreCommaNameWithoutAPPID(t *testing.T) {
	assertShop(
		t,
		merchant(". Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Sh", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
	assertShop(t, merchant(". Beispiel Shop GmbH, Ihr Abo", testPaypalPartner), "Beispiel Shop GmbH")
}

func TestCardDescriptorsGiveTheShopBeforeTheDigits(t *testing.T) {
	assertShop(
		t,
		MerchantOf(stringPointer("PAYPAL *BEISPIELSHOP"), stringPointer("PAYPAL *BEISPIELSHOP")),
		"BEISPIELSHOP",
	)
	assertShop(t, MerchantOf(stringPointer("PAYPAL *BEISPIELSHOP"), nil), "BEISPIELSHOP")
	assertShop(t, MerchantOf(stringPointer("PAYPAL*BEISPIELSHOP"), nil), "BEISPIELSHOP")
	assertShop(t, MerchantOf(stringPointer("PAYPAL *BEISPIEL SHOP 12345678"), nil), "BEISPIEL SHOP")
	assertShop(t, MerchantOf(stringPointer("PAYPAL *BEISPIEL SHOP, 4930123456"), nil), "BEISPIEL SHOP")
	assertShop(t, MerchantOf(stringPointer("PAYPAL *SHOP24"), nil), "SHOP24")
}

func TestACardPurposeWithANonPaypalPartnerGivesNoShop(t *testing.T) {
	assertNoShop(t, MerchantOf(stringPointer("PAYPAL *BEISPIELSHOP"), stringPointer("Beispiel Shop GmbH")))
}

func TestIDsWithdrawalsTransfersAndEmptyTailsGiveNoShop(t *testing.T) {
	assertNoShop(t, merchant("PP.1234.PP . , Ihr Einkauf bei", testPaypalPartner))
	assertNoShop(t, merchant("1234567890123 PP.1234.PP", testPaypalPartner))
	assertNoShop(t, merchant("1234567890", testPaypalPartner))
	assertNoShop(t, merchant("PP.1234.PP ABBUCHUNG VOM GIROKONTO", testPaypalPartner))
	assertNoShop(t, merchant("EREF+123 SVWZ+ABC TRANSFER", testPaypalPartner))
	assertNoShop(t, merchant(
		"PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
		"Beispiel Shop GmbH",
	))
}

func TestShopMatchExposesTheProviderViewForMarkerAndCardBookings(t *testing.T) {
	marker := ShopMatchOf(
		stringPointer("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		stringPointer(testPaypalPartner),
	)
	if marker == nil {
		t.Fatal("marker = nil, want a match")
	}
	if marker.Shop != "Beispiel Shop GmbH" {
		t.Errorf("marker.Shop = %q", marker.Shop)
	}
	if marker.ProviderPurpose != nil {
		t.Errorf("marker.ProviderPurpose = %q, want nil", *marker.ProviderPurpose)
	}
	if marker.ProviderPartner == nil || *marker.ProviderPartner != testPaypalPartner {
		t.Errorf("marker.ProviderPartner = %v, want %q", marker.ProviderPartner, testPaypalPartner)
	}

	card := ShopMatchOf(stringPointer("PAYPAL *BEISPIELSHOP"), nil)
	if card == nil {
		t.Fatal("card = nil, want a match")
	}
	if card.Shop != "BEISPIELSHOP" {
		t.Errorf("card.Shop = %q", card.Shop)
	}
	if card.ProviderPurpose == nil || *card.ProviderPurpose != "PAYPAL" {
		t.Errorf("card.ProviderPurpose = %v, want %q", card.ProviderPurpose, "PAYPAL")
	}
	if card.ProviderPartner != nil {
		t.Errorf("card.ProviderPartner = %q, want nil", *card.ProviderPartner)
	}

	cardWithPartner := ShopMatchOf(
		stringPointer("PAYPAL *BEISPIELSHOP"),
		stringPointer("PAYPAL *BEISPIELSHOP"),
	)
	if cardWithPartner == nil {
		t.Fatal("cardWithPartner = nil, want a match")
	}
	if cardWithPartner.ProviderPurpose == nil || *cardWithPartner.ProviderPurpose != "PAYPAL" {
		t.Errorf("cardWithPartner.ProviderPurpose = %v, want %q", cardWithPartner.ProviderPurpose, "PAYPAL")
	}
	if cardWithPartner.ProviderPartner == nil || *cardWithPartner.ProviderPartner != "PAYPAL" {
		t.Errorf("cardWithPartner.ProviderPartner = %v, want %q", cardWithPartner.ProviderPartner, "PAYPAL")
	}
}

// TestShopOffsetsAreRuneSafe proves a regexp2 rune offset is never used as a
// byte offset: non-ASCII text before the marker and before a PP id must still
// yield the shop. The expected values come from the Rust code by hand.
func TestShopOffsetsAreRuneSafe(t *testing.T) {
	assertShop(
		t,
		merchant("Ihr Einkauf bei Müller Café", testPaypalPartner),
		"Müller Café",
	)

	assertShop(
		t,
		merchant("Ähr PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH", testPaypalPartner),
		"Beispiel Shop GmbH",
	)
}

// Rust's sanitize trims with char::is_alphanumeric and requires
// char::is_alphabetic, which include letter numbers such as Ⅻ (U+216B, Nl).
func TestShopSanitizeUsesRustAlphabetic(t *testing.T) {
	purpose := "PAYPAL *Ⅻ Ⅻ"
	got := ShopMatchOf(&purpose, nil)
	if got == nil || got.Shop != "Ⅻ Ⅻ" {
		t.Fatalf("ShopMatchOf(%q) = %+v, want shop %q", purpose, got, "Ⅻ Ⅻ")
	}
}
