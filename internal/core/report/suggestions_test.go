package report

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func TestFindSuggestsKeywordMatchesOnPartnerOrPurposeAndSkipsDecidedAndTransferBookings(t *testing.T) {
	keywords := []string{"Spende", "Handwerker", "Kita"}
	transactions := itemsOf([]itemBuilder{
		item(1, -3_000).Purpose("Spende für Beispielprojekt"),
		item(2, -20_000).Date(testDate(t, 2026, time.June, 2)).Partner("Beispiel-Handwerker GmbH"),
		item(3, -3_000).Partner("Spendenaktion").TaxRelevant(),
		item(4, -3_000).Partner("Spendenaktion").Source(model.TaxSourceManual).OverrideTax(false),
		item(5, -3_000).Partner("Spendenaktion").Transfer(),
		item(6, -900).Partner("Bäckerei Beispiel"),
		item(7, -28_000).Date(testDate(t, 2026, time.April, 22)).Purpose("Handwerkerleistung Bad"),
	})
	suggestions := FindTaxSuggestions(transactions, keywords)

	ids := make([]int64, 0, len(suggestions))
	for _, suggestion := range suggestions {
		ids = append(ids, int64(suggestion.Booking.ID))
	}
	if !slices.Equal(ids, []int64{2, 1, 7}) {
		t.Fatalf("suggestions = %v, want [2 1 7]; date descending", ids)
	}
	for _, suggestion := range suggestions {
		id := int64(suggestion.Booking.ID)
		switch id {
		case 1:
			if suggestion.Keyword != "Spende" {
				t.Fatalf("suggestion 1 keyword = %q, want Spende; the first keyword (list order) wins", suggestion.Keyword)
			}
		case 2, 7:
			if suggestion.Keyword != "Handwerker" {
				t.Fatalf("suggestion %d keyword = %q, want Handwerker", id, suggestion.Keyword)
			}
		}
	}
}

func TestFindMatchesAKeywordOnlyAtTheStartOfAWord(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -3_000).Partner("Beispiel Kita-Beitrag"),
		item(2, -3_000).Date(testDate(t, 2026, time.June, 2)).Partner("Akita Import"),
		item(3, -3_000).Date(testDate(t, 2026, time.June, 3)).Purpose("SPENDEN Aktion"),
		item(4, -3_000).Date(testDate(t, 2026, time.June, 4)).Purpose("Gegenspende"),
	})
	suggestions := FindTaxSuggestions(transactions, []string{"Kita", "Spende"})
	ids := make([]int64, 0, len(suggestions))
	for _, suggestion := range suggestions {
		ids = append(ids, int64(suggestion.Booking.ID))
	}
	if !slices.Equal(ids, []int64{3, 1}) {
		t.Fatalf("suggestions = %v, want [3 1]; word starts match in any case, inside a word never", ids)
	}
}

func TestFindIgnoresBlankAndShortKeywordsAndNeedsAKeywordOfAtLeastThreeCharacters(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -3_000).Partner("a b Kitax t").Purpose("relevant later"),
	})
	suggestions := FindTaxSuggestions(transactions, []string{"  ", "", "ab", "Kitax"})
	ids := make([]int64, 0, len(suggestions))
	for _, suggestion := range suggestions {
		ids = append(ids, int64(suggestion.Booking.ID))
	}
	if !slices.Equal(ids, []int64{1}) {
		t.Fatalf("suggestions = %v, want [1]; 'Kitax' matches, blanks and sub-3-char words are ignored", ids)
	}

	none := FindTaxSuggestions(
		itemsOf([]itemBuilder{item(1, -3_000).Partner("a b c")}),
		[]string{"  ", "", "ab"},
	)
	if len(none) != 0 {
		t.Fatalf("suggestions = %v, want none; no usable keyword, no suggestion", none)
	}
}

func TestKeywordsReturnsTheDefaultsWithoutTheSettingAndTheStoredListWithIt(t *testing.T) {
	d := testDatabase(t)
	ctx := context.Background()
	if got, err := Keywords(ctx, d); err != nil || !slices.Equal(got, DefaultKeywords) {
		t.Fatalf("defaults = %v (%v), want %v", got, err, DefaultKeywords)
	}

	if err := settings.Set(ctx, d, KeyKeywords, "Steuerberater\n\n Küche \nKita"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	got, err := Keywords(ctx, d)
	if err != nil {
		t.Fatalf("Keywords: %v", err)
	}
	want := []string{"Steuerberater", "Küche", "Kita"}
	if !slices.Equal(got, want) {
		t.Fatalf("keywords = %v, want %v; trimmed, blank lines dropped, one per line", got, want)
	}
	if slices.Contains(got, "Handwerker") {
		t.Fatalf("keywords = %v, want no Handwerker", got)
	}

	stored, err := Keywords(ctx, d)
	if err != nil {
		t.Fatalf("Keywords: %v", err)
	}
	suggestions := FindTaxSuggestions(
		itemsOf([]itemBuilder{
			item(1, -3_000).Partner("Spendenbeispiel"),
			item(2, -900).Partner("Bäckerei Beispiel"),
		}),
		stored,
	)
	if len(suggestions) != 0 {
		t.Fatalf(
			"suggestions = %v, want none; no stored keyword matches the Spende partner any more",
			suggestions,
		)
	}
}
