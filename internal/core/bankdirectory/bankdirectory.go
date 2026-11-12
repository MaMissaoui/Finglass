// Package bankdirectory is the offline German bank directory from the
// Bundesbank bank code file, ported from
// crates/finglass-core/src/bank_directory.rs.
//
// The embedded data/blz-aktuell.txt is the Bundesbank's bank code file, byte
// for byte as published. It is not covered by Finglass's MIT licence; see
// data/NOTICE.md for the source, the validity period and the Bundesbank's
// terms.
package bankdirectory

import (
	"bytes"
	_ "embed"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/iban"
)

// bundledFile is the bank code file shipped with Finglass, byte for byte as
// published.
//
//go:embed data/blz-aktuell.txt
var bundledFile []byte

// recordLength is the length of a fixed-width record in bytes.
const recordLength = 168

// BankEntry is one bank in the directory.
type BankEntry struct {
	// BLZ is the German bank code (Bankleitzahl).
	BLZ string
	// Name is the bank's name.
	Name string
	// PostalCode is the postal code.
	PostalCode string
	// City is the city.
	City string
	// BIC is the BIC; it may be empty.
	BIC string
}

// Parse parses the Bundesbank bank code file.
//
// Every line is sliced by byte offsets on the raw bytes and each field is then
// decoded as ISO-8859-1. Decoding the whole line as UTF-8 first would shift
// every column after an umlaut. Merkmal-2 records (extra branches) and records
// marked D (deleted) are dropped; lines shorter than 168 bytes are skipped.
func Parse(data []byte) []BankEntry {
	entries := make([]BankEntry, 0)
	seen := make(map[string]struct{})
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := bytes.TrimSuffix(raw, []byte{'\r'})
		if len(line) < recordLength {
			continue
		}
		if line[8] != '1' || line[158] == 'D' {
			continue
		}
		blz := strings.TrimSpace(decode(line[0:8]))
		if blz == "" {
			continue
		}
		if _, exists := seen[blz]; exists {
			continue
		}
		seen[blz] = struct{}{}
		entries = append(entries, BankEntry{
			BLZ:        blz,
			Name:       strings.TrimRightFunc(decode(line[9:67]), unicode.IsSpace),
			PostalCode: strings.TrimSpace(decode(line[67:72])),
			City:       strings.TrimSpace(decode(line[72:107])),
			BIC:        strings.TrimSpace(decode(line[139:150])),
		})
	}
	return entries
}

// decode converts ISO-8859-1 bytes: every byte is the Unicode code point of
// the same value.
func decode(data []byte) string {
	characters := make([]rune, len(data))
	for index, value := range data {
		characters[index] = rune(value)
	}
	return string(characters)
}

// BankDirectory is a parsed bank directory with lookup indexes.
type BankDirectory struct {
	entries []BankEntry
	byBLZ   map[string]int
	byBIC   map[string]int
}

// Bundled returns the directory parsed from the bundled bank code file, once
// per process.
func Bundled() *BankDirectory {
	return bundled()
}

var bundled = sync.OnceValue(func() *BankDirectory {
	return FromEntries(Parse(bundledFile))
})

// FromEntries builds a directory from entries, for tests and callers that
// bring their own list.
func FromEntries(entries []BankEntry) *BankDirectory {
	byBLZ := make(map[string]int, len(entries))
	byBIC := make(map[string]int, len(entries))
	upper := cases.Upper(language.Und)
	for index, entry := range entries {
		if _, exists := byBLZ[entry.BLZ]; !exists {
			byBLZ[entry.BLZ] = index
		}
		bic := upper.String(strings.TrimSpace(entry.BIC))
		if bic == "" {
			continue
		}
		if current, exists := byBIC[bic]; exists {
			if entry.BLZ < entries[current].BLZ {
				byBIC[bic] = index
			}
		} else {
			byBIC[bic] = index
		}
	}
	return &BankDirectory{entries: entries, byBLZ: byBLZ, byBIC: byBIC}
}

// Lookup looks a bank up by BLZ, ignoring surrounding whitespace.
func (d *BankDirectory) Lookup(blz string) (BankEntry, bool) {
	index, ok := d.byBLZ[strings.TrimSpace(blz)]
	if !ok {
		return BankEntry{}, false
	}
	return d.entries[index], true
}

// ByBIC looks a bank up by BIC, case-insensitively. An 8-character BIC also
// matches <bic>XXX. Several entries with one BIC resolve to the one with the
// lowest BLZ.
func (d *BankDirectory) ByBIC(bic string) (BankEntry, bool) {
	needle := cases.Upper(language.Und).String(strings.TrimSpace(bic))
	if needle == "" {
		return BankEntry{}, false
	}
	index, found := d.byBIC[needle]
	if len(needle) == 8 {
		if extended, ok := d.byBIC[needle+"XXX"]; ok {
			if !found || d.entries[extended].BLZ < d.entries[index].BLZ {
				index = extended
				found = true
			}
		}
	}
	if !found {
		return BankEntry{}, false
	}
	return d.entries[index], true
}

// ByIBAN returns the bank behind a valid German IBAN, or false for an invalid
// or unknown one.
func (d *BankDirectory) ByIBAN(account string) (BankEntry, bool) {
	normalised := iban.Normalize(account)
	if !strings.HasPrefix(normalised, "DE") || !iban.IsValid(normalised) {
		return BankEntry{}, false
	}
	characters := []rune(normalised)
	return d.Lookup(string(characters[4:12]))
}

// Search is the free-text bank search for the add-bank dialog.
//
// A complete German IBAN resolves straight to its bank. Shorter queries are
// ignored, and otherwise matches are ranked: BLZ prefix, BIC prefix, name
// prefix, name contains, city contains. The result is sorted by rank, name,
// city and BLZ and capped at limit.
func (d *BankDirectory) Search(query string, limit int) []BankEntry {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []BankEntry{}
	}
	if entry, ok := d.ByIBAN(trimmed); ok {
		return []BankEntry{entry}
	}
	if len([]rune(trimmed)) < 3 || limit <= 0 {
		return []BankEntry{}
	}
	lower := cases.Lower(language.Und)
	needle := lower.String(trimmed)
	matches := make([]rankedEntry, 0)
	for _, entry := range d.entries {
		if rank, ok := entryRank(entry, needle, lower); ok {
			matches = append(matches, rankedEntry{rank: rank, entry: entry})
		}
	}
	sort.SliceStable(matches, func(left, right int) bool {
		leftMatch, rightMatch := matches[left], matches[right]
		if leftMatch.rank != rightMatch.rank {
			return leftMatch.rank < rightMatch.rank
		}
		if leftMatch.entry.Name != rightMatch.entry.Name {
			return leftMatch.entry.Name < rightMatch.entry.Name
		}
		leftCentral, rightCentral := centralRank(leftMatch.entry), centralRank(rightMatch.entry)
		if leftCentral != rightCentral {
			return leftCentral < rightCentral
		}
		if leftMatch.entry.City != rightMatch.entry.City {
			return leftMatch.entry.City < rightMatch.entry.City
		}
		return leftMatch.entry.BLZ < rightMatch.entry.BLZ
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	found := make([]BankEntry, len(matches))
	for index, match := range matches {
		found[index] = match.entry
	}
	return found
}

type rankedEntry struct {
	rank  uint8
	entry BankEntry
}

// centralRank is 0 for a bank whose own BLZ is the one its FinTS server
// accepts, and 1 for a branch mapped elsewhere.
func centralRank(entry BankEntry) uint8 {
	if strings.HasPrefix(entry.BIC, "HYVEDEMM") && entry.BLZ != "70020270" {
		return 1
	}
	return 0
}

// entryRank reports lower is better; false means no match.
func entryRank(entry BankEntry, needle string, lower cases.Caser) (uint8, bool) {
	switch {
	case strings.HasPrefix(entry.BLZ, needle):
		return 0, true
	case strings.HasPrefix(lower.String(entry.BIC), needle):
		return 1, true
	case strings.HasPrefix(lower.String(entry.Name), needle):
		return 2, true
	case strings.Contains(lower.String(entry.Name), needle):
		return 3, true
	case strings.Contains(lower.String(entry.City), needle):
		return 4, true
	default:
		return 0, false
	}
}

// FintsBLZ returns the BLZ whose FinTS server speaks for blz's bank.
//
// HypoVereinsbank's FinTS server accepts only the central bank code. The
// evidence is a real dialog: a login with the branch BLZ got HTTP 500 on the
// first message, while the central BLZ initialised the dialog. Don't add other
// banks without such evidence. The directory lookups (BIC, IBAN) keep using
// the real branch entries.
func FintsBLZ(directory *BankDirectory, blz string) string {
	trimmed := strings.TrimSpace(blz)
	bic := ""
	if entry, ok := directory.Lookup(trimmed); ok {
		bic = entry.BIC
	}
	if strings.HasPrefix(bic, "HYVEDEMM") {
		return "70020270"
	}
	return trimmed
}
