package banklogo

import "testing"

func ptr(value string) *string {
	return &value
}

func logo(name string, blz, bic *string) (Logo, bool) {
	return For(name, blz, bic)
}

func TestBrandExclusionsWinBeforeEveryLogoRule(t *testing.T) {
	if got, ok := logo("comdirect bank", ptr("37040044"), ptr("COBADEHDXXX")); ok {
		t.Errorf("comdirect got %q", got)
	}
	if got, ok := logo("Consorsbank", ptr("76026000"), ptr("CSDBDE71XXX")); ok {
		t.Errorf("Consorsbank got %q", got)
	}
	if got, ok := logo("Targobank", ptr("30020900"), ptr("CMCIDEDDXXX")); ok {
		t.Errorf("Targobank got %q", got)
	}
	if got, ok := logo("Sparda-Bank", ptr("50090500"), ptr("GENODEF1S12")); ok {
		t.Errorf("Sparda-Bank got %q", got)
	}
	if got, ok := logo("PSD Bank", nil, nil); ok {
		t.Errorf("PSD Bank got %q", got)
	}
	if got, ok := logo("APO Bank", nil, nil); ok {
		t.Errorf("APO Bank got %q", got)
	}
	if got, ok := logo("Apothekerbank", nil, nil); ok {
		t.Errorf("Apothekerbank got %q", got)
	}
	// The exclusion comes first even when a later rule would match.
	if got, ok := logo("Sparda-Bank", nil, ptr("HYVEDEMMXXX")); ok {
		t.Errorf("Sparda-Bank with a HypoVereinsbank BIC got %q", got)
	}
}

func TestHypoVereinsbankMatchesByNameAndBIC(t *testing.T) {
	if got, ok := logo(
		"UniCredit Bank - HypoVereinsbank AG",
		ptr("70020270"),
		ptr("HYVEDEMMXXX"),
	); !ok || got != HypoVereinsbank {
		t.Errorf("UniCredit Bank - HypoVereinsbank AG = %q, %v", got, ok)
	}
	if got, ok := logo("HVB Mastercard", nil, nil); !ok || got != HypoVereinsbank {
		t.Errorf("HVB Mastercard = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("HYVEDEMMXXX")); !ok || got != HypoVereinsbank {
		t.Errorf("Some Bank by BIC = %q, %v", got, ok)
	}
}

func TestDKBMatchesByNameBICAndBLZ(t *testing.T) {
	if got, ok := logo("DKB Privat", ptr("12030000"), ptr("BYLADEM1001")); !ok || got != DKB {
		t.Errorf("DKB Privat = %q, %v", got, ok)
	}
	if got, ok := logo("Deutsche Kreditbank AG", nil, nil); !ok || got != DKB {
		t.Errorf("Deutsche Kreditbank AG = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("BYLADEM1001")); !ok || got != DKB {
		t.Errorf("Some Bank by DKB BIC = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", ptr("12030000"), nil); !ok || got != DKB {
		t.Errorf("Some Bank by DKB BLZ = %q, %v", got, ok)
	}
}

func TestTheRemainingLogoRulesMatchByNameOrBIC(t *testing.T) {
	if got, ok := logo("Postbank", nil, nil); !ok || got != Postbank {
		t.Errorf("Postbank = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("PBNKDEFFXXX")); !ok || got != Postbank {
		t.Errorf("PBNKDEFFXXX = %q, %v", got, ok)
	}

	if got, ok := logo("Deutsche Bank", nil, nil); !ok || got != DeutscheBank {
		t.Errorf("Deutsche Bank = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("DEUTDEFFXXX")); !ok || got != DeutscheBank {
		t.Errorf("DEUTDEFFXXX = %q, %v", got, ok)
	}

	if got, ok := logo("Commerzbank", nil, nil); !ok || got != Commerzbank {
		t.Errorf("Commerzbank = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("COBADEFFXXX")); !ok || got != Commerzbank {
		t.Errorf("COBADEFFXXX = %q, %v", got, ok)
	}

	if got, ok := logo("Santander", nil, nil); !ok || got != Santander {
		t.Errorf("Santander = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("SCFBDEFFXXX")); !ok || got != Santander {
		t.Errorf("SCFBDEFFXXX = %q, %v", got, ok)
	}

	if got, ok := logo("N26", nil, nil); !ok || got != N26 {
		t.Errorf("N26 = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("NTSBDEB1XXX")); !ok || got != N26 {
		t.Errorf("NTSBDEB1XXX = %q, %v", got, ok)
	}
}

func TestSparkasseMatchesByNameAndTheFourthBLZDigit(t *testing.T) {
	if got, ok := logo("Sparkasse KölnBonn", nil, nil); !ok || got != Sparkasse {
		t.Errorf("Sparkasse KölnBonn = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", ptr("45050001"), nil); !ok || got != Sparkasse {
		t.Errorf("45050001 = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", ptr("12355001"), nil); !ok || got != Sparkasse {
		t.Errorf("12355001 = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", ptr("12340001"), nil); ok {
		t.Errorf("12340001 = %q", got)
	}
}

func TestVolksbankMatchesByNameAndTheGENOBIC(t *testing.T) {
	if got, ok := logo(
		"Volksbank Mittelhessen",
		ptr("51390000"),
		ptr("VBMHDE5FXXX"),
	); !ok || got != Volksbank {
		t.Errorf("Volksbank Mittelhessen = %q, %v", got, ok)
	}
	if got, ok := logo("Raiffeisenbank", nil, nil); !ok || got != Volksbank {
		t.Errorf("Raiffeisenbank = %q, %v", got, ok)
	}
	if got, ok := logo("VR Bank Südpfalz", nil, nil); !ok || got != Volksbank {
		t.Errorf("VR Bank Südpfalz = %q, %v", got, ok)
	}
	if got, ok := logo("VR-Bank", nil, nil); !ok || got != Volksbank {
		t.Errorf("VR-Bank = %q, %v", got, ok)
	}
	if got, ok := logo("VRBank", nil, nil); !ok || got != Volksbank {
		t.Errorf("VRBank = %q, %v", got, ok)
	}
	if got, ok := logo("Some Bank", nil, ptr("GENODEF1S12")); !ok || got != Volksbank {
		t.Errorf("GENODEF1S12 = %q, %v", got, ok)
	}
}

func TestUnknownBanksGetNoLogo(t *testing.T) {
	if got, ok := logo("Bank Norwegian", nil, nil); ok {
		t.Errorf("Bank Norwegian = %q", got)
	}
	if got, ok := logo("BayernLB", nil, ptr("BYLADEMMXXX")); ok {
		t.Errorf("BayernLB = %q", got)
	}
	if got, ok := logo("", nil, nil); ok {
		t.Errorf("empty name = %q", got)
	}
}

func TestMatchingTrimsAndIgnoresCase(t *testing.T) {
	if got, ok := logo("  dkb  ", ptr(" 12030000 "), ptr(" byladm1001 ")); !ok || got != DKB {
		t.Errorf("padded dkb = %q, %v", got, ok)
	}
	if got, ok := logo("  SPARKASSE  ", nil, nil); !ok || got != Sparkasse {
		t.Errorf("padded SPARKASSE = %q, %v", got, ok)
	}
	if got, ok := logo("  volksbank  ", nil, nil); !ok || got != Volksbank {
		t.Errorf("padded volksbank = %q, %v", got, ok)
	}
}

func TestTheFirstMatchingRuleWins(t *testing.T) {
	if got, ok := logo("DKB", nil, ptr("HYVEDEMMXXX")); !ok || got != HypoVereinsbank {
		t.Errorf("DKB with a HypoVereinsbank BIC = %q, %v", got, ok)
	}
	if got, ok := logo("Commerzbank", ptr("12030000"), nil); !ok || got != DKB {
		t.Errorf("Commerzbank with the DKB BLZ = %q, %v", got, ok)
	}
}

func TestMonogramUsesAShortUppercaseWordFirst(t *testing.T) {
	if got := MonogramFor("HVB Mastercard").Letters; got != "HVB" {
		t.Errorf("HVB Mastercard letters = %q", got)
	}
	if got := MonogramFor("DKB Privat").Letters; got != "DKB" {
		t.Errorf("DKB Privat letters = %q", got)
	}
	if got := MonogramFor("ING").Letters; got != "ING" {
		t.Errorf("ING letters = %q", got)
	}
}

func TestMonogramOtherwiseUsesInitialsOrTheFirstTwoLetters(t *testing.T) {
	if got := MonogramFor("Bank Norwegian").Letters; got != "BN" {
		t.Errorf("Bank Norwegian letters = %q", got)
	}
	if got := MonogramFor("Sparkasse").Letters; got != "SP" {
		t.Errorf("Sparkasse letters = %q", got)
	}
	if got := MonogramFor("ING-DiBa").Letters; got != "IN" {
		t.Errorf("ING-DiBa letters = %q", got)
	}
	if got := MonogramFor("  ").Letters; got != "" {
		t.Errorf("blank name letters = %q", got)
	}
}

func TestMonogramColourIsStableAndInRange(t *testing.T) {
	first := MonogramFor("Bank Norwegian")
	second := MonogramFor("Bank Norwegian")
	if first != second {
		t.Errorf("the monogram is not stable: %#v != %#v", first, second)
	}
	if first.Colour != MonogramFor("bank norwegian").Colour {
		t.Errorf("the colour is not case-insensitive: %d != %d", first.Colour, MonogramFor("bank norwegian").Colour)
	}
	if first.Colour >= 8 {
		t.Errorf("the colour %d is out of range", first.Colour)
	}
	// The sum of the lower-cased UTF-8 bytes modulo 8, not a randomized hash.
	expected := 0
	for _, value := range []byte("bank norwegian") {
		expected += int(value)
	}
	if int(first.Colour) != expected%8 {
		t.Errorf("the colour = %d, want %d", first.Colour, expected%8)
	}
}

func TestMonogramColourUsesFullLowerCasing(t *testing.T) {
	// Rust lower-cases İ (U+0130) to "i" + U+0307 (combining dot above).
	// Those are UTF-8 bytes 0x69, 0xCC and 0x87:
	// 105 + 204 + 135 = 444, and 444 % 8 = 4.
	const want uint8 = 4
	if got := MonogramFor("İ").Colour; got != want {
		t.Errorf("MonogramFor(İ).Colour = %d, want %d", got, want)
	}
}
