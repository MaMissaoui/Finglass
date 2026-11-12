package bankdirectory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const bundledSHA256 = "5872bf19298f55cc0594d22656b72e0f8ff277faedbf5b18f9b7f3bcae4c606b"

func put(line []byte, start int, value []byte) {
	copy(line[start:start+len(value)], value)
}

func recordBytes(
	merkmal byte,
	blz string,
	name []byte,
	postalCode string,
	city []byte,
	bic string,
	change byte,
) []byte {
	line := bytes.Repeat([]byte{' '}, recordLength)
	put(line, 0, []byte(blz))
	line[8] = merkmal
	put(line, 9, name)
	put(line, 67, []byte(postalCode))
	put(line, 72, city)
	put(line, 139, []byte(bic))
	line[158] = change
	return line
}

func record(blz, name, postalCode, city, bic string) []byte {
	return recordBytes('1', blz, []byte(name), postalCode, []byte(city), bic, ' ')
}

func TestParsesFixedWidthFieldsAndDropsExtraAndDeletedRecords(t *testing.T) {
	var data []byte
	data = append(data, record("12345678", "Example Bank", "10115", "Berlin", "EXAMPLEBICX")...)
	data = append(data, '\r', '\n')
	data = append(data, recordBytes(
		'2',
		"22222222",
		[]byte("Extra Branch"),
		"10115",
		[]byte("Berlin"),
		"EXTRAXXBICX",
		' ',
	)...)
	data = append(data, '\r', '\n')
	data = append(data, recordBytes(
		'1',
		"33333333",
		[]byte("Deleted Bank"),
		"10115",
		[]byte("Berlin"),
		"DELETEDBICX",
		'D',
	)...)
	data = append(data, '\n')
	data = append(data, record("44444444", "Second Bank", "20095", "Hamburg", "SECONDBICXX")...)

	entries := Parse(data)
	if len(entries) != 2 {
		t.Fatalf("Parse returned %d entries: %#v", len(entries), entries)
	}
	if entries[0].BLZ != "12345678" {
		t.Errorf("entries[0].BLZ = %q", entries[0].BLZ)
	}
	if entries[0].Name != "Example Bank" {
		t.Errorf("entries[0].Name = %q", entries[0].Name)
	}
	if entries[0].PostalCode != "10115" {
		t.Errorf("entries[0].PostalCode = %q", entries[0].PostalCode)
	}
	if entries[0].City != "Berlin" {
		t.Errorf("entries[0].City = %q", entries[0].City)
	}
	if entries[0].BIC != "EXAMPLEBICX" {
		t.Errorf("entries[0].BIC = %q", entries[0].BIC)
	}
	if entries[1].BLZ != "44444444" {
		t.Errorf("entries[1].BLZ = %q", entries[1].BLZ)
	}
}

func TestAnUmlautDecodesFromISO88591WithoutShiftingLaterFields(t *testing.T) {
	data := recordBytes(
		'1',
		"80331000",
		[]byte("Bank M\xfcnchen"),
		"80331",
		[]byte("M\xfcnchen"),
		"HYVEDEMMXXX",
		' ',
	)
	entries := Parse(data)
	if len(entries) != 1 {
		t.Fatalf("Parse returned %d entries", len(entries))
	}
	entry := entries[0]
	if entry.Name != "Bank München" {
		t.Errorf("Name = %q", entry.Name)
	}
	if entry.PostalCode != "80331" {
		t.Errorf("PostalCode = %q", entry.PostalCode)
	}
	if entry.City != "München" {
		t.Errorf("City = %q", entry.City)
	}
	if entry.BIC != "HYVEDEMMXXX" {
		t.Errorf("BIC = %q", entry.BIC)
	}
}

func TestAShortLineIsSkipped(t *testing.T) {
	data := record("12345678", "Example Bank", "10115", "Berlin", "EXAMPLEBICX")
	data = append(data, '\r', '\n')
	data = append(data, bytes.Repeat([]byte{' '}, recordLength-1)...)
	data = append(data, '\n')
	entries := Parse(data)
	if len(entries) != 1 {
		t.Fatalf("Parse returned %d entries", len(entries))
	}
}

func TestBundledFileHasThePublishedChecksumAndTheExpectedBanks(t *testing.T) {
	digest := sha256.Sum256(bundledFile)
	if got := hex.EncodeToString(digest[:]); got != bundledSHA256 {
		t.Fatalf("bundled checksum = %s, want %s", got, bundledSHA256)
	}

	directory := Bundled()
	if len(directory.entries) <= 3000 {
		t.Fatalf("the bundled directory has %d entries", len(directory.entries))
	}

	hypo, ok := directory.Lookup("70020270")
	if !ok {
		t.Fatal("HypoVereinsbank is missing")
	}
	if hypo.Name != "UniCredit Bank - HypoVereinsbank" {
		t.Errorf("Name = %q", hypo.Name)
	}
	if hypo.City != "München" {
		t.Errorf("City = %q", hypo.City)
	}
	if hypo.BIC != "HYVEDEMMXXX" {
		t.Errorf("BIC = %q", hypo.BIC)
	}

	foundHypo := false
	for _, entry := range directory.Search("hypo", 15) {
		if entry.BLZ == "70020270" {
			foundHypo = true
			break
		}
	}
	if !foundHypo {
		t.Error("searching for hypo does not return 70020270")
	}

	if bicEntry, ok := directory.ByBIC("hyvedemmxxx"); !ok || bicEntry.BLZ != "70020270" {
		t.Errorf("ByBIC(hyvedemmxxx) = %#v, %v", bicEntry, ok)
	}

	const account = "DE67700202701234567890"
	if ibanEntry, ok := directory.ByIBAN(account); !ok || ibanEntry.BLZ != "70020270" {
		t.Errorf("ByIBAN(%s) = %#v, %v", account, ibanEntry, ok)
	}
	if entry, ok := directory.ByIBAN("DE00700202701234567890"); ok {
		t.Errorf("ByIBAN accepted an invalid IBAN as %#v", entry)
	}
}

func TestFintsBLZMapsHypoVereinsbankBranchesButLeavesOtherBanksAlone(t *testing.T) {
	directory := Bundled()
	branch, ok := directory.Lookup("10020890")
	if !ok {
		t.Fatal("a HypoVereinsbank branch is missing")
	}
	if !bytes.HasPrefix([]byte(branch.BIC), []byte("HYVEDEMM")) {
		t.Errorf("branch BIC = %q", branch.BIC)
	}
	if branch.BLZ == "70020270" {
		t.Errorf("branch BLZ = %q", branch.BLZ)
	}
	if got := FintsBLZ(directory, branch.BLZ); got != "70020270" {
		t.Errorf("FintsBLZ(%q) = %q", branch.BLZ, got)
	}
	if got := FintsBLZ(directory, " 10010010 "); got != "10010010" {
		t.Errorf("FintsBLZ(padded) = %q", got)
	}
	if got := FintsBLZ(directory, "99999999"); got != "99999999" {
		t.Errorf("FintsBLZ(unknown) = %q", got)
	}
}

func entry(blz, name, city, bic string) BankEntry {
	return BankEntry{
		BLZ:        blz,
		Name:       name,
		PostalCode: "10115",
		City:       city,
		BIC:        bic,
	}
}

func TestSearchRanksABLZPrefixOverEveryOtherMatch(t *testing.T) {
	directory := FromEntries([]BankEntry{
		entry("99999999", "Bank with 123 inside", "Ort", "AAABICX"),
		entry("99999998", "Stadtbank", "123stadt", "AAABICX"),
		entry("99999997", "123 Name Bank", "Ort", "AAABICX"),
		entry("99999996", "Other Bank", "Ort", "123BICX"),
		entry("12345678", "Target Bank", "Ort", "AAABICX"),
	})
	found := make([]string, 0)
	for _, entry := range directory.Search("123", 15) {
		found = append(found, entry.BLZ)
	}
	want := []string{"12345678", "99999996", "99999997", "99999999", "99999998"}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("Search(123) = %#v, want %#v", found, want)
	}
}

func TestSearchNeedsThreeCharactersAndHonoursTheLimit(t *testing.T) {
	directory := FromEntries([]BankEntry{
		entry("11111111", "Alpha Bank", "Berlin", "ALPHABICXXX"),
		entry("22222222", "Alpha Zweig", "Berlin", "ALPHA2BICXX"),
		entry("33333333", "Alphabet", "Berlin", "ALPHA3BICXX"),
	})
	if found := directory.Search("Al", 15); len(found) != 0 {
		t.Errorf("Search(Al) = %#v", found)
	}
	if found := directory.Search("   ", 15); len(found) != 0 {
		t.Errorf("Search(spaces) = %#v", found)
	}
	if found := directory.Search("Alpha", 2); len(found) != 2 {
		t.Errorf("Search(Alpha, 2) has %d entries", len(found))
	}
	if found := directory.Search("Alpha", 15); len(found) != 3 {
		t.Errorf("Search(Alpha, 15) has %d entries", len(found))
	}
}

func TestSearchResolvesAValidGermanIBANToItsBank(t *testing.T) {
	directory := FromEntries([]BankEntry{entry(
		"70020270",
		"UniCredit Bank - HypoVereinsbank",
		"München",
		"HYVEDEMMXXX",
	)})
	found := directory.Search("DE67700202701234567890", 15)
	if len(found) != 1 {
		t.Fatalf("Search(IBAN) has %d entries", len(found))
	}
	if found[0].BLZ != "70020270" {
		t.Errorf("Search(IBAN)[0].BLZ = %q", found[0].BLZ)
	}
}

func TestByBICIsCaseInsensitiveAcceptsAnEightCharacterBICAndPrefersTheLowestBLZ(t *testing.T) {
	directory := FromEntries([]BankEntry{
		entry("90000000", "High", "Ort", "SAME BICXXX"),
		entry("10000000", "Low", "Ort", "same bicxxx"),
		entry("20000000", "Eight", "Ort", "EIGHTBICXXX"),
	})
	if found, ok := directory.ByBIC(" Same BicXXX "); !ok || found.BLZ != "10000000" {
		t.Errorf("ByBIC(Same BicXXX) = %#v, %v", found, ok)
	}
	if found, ok := directory.ByBIC("EIGHTBIC"); !ok || found.BLZ != "20000000" {
		t.Errorf("ByBIC(EIGHTBIC) = %#v, %v", found, ok)
	}
	if found, ok := directory.ByBIC("EIGHTBICXXX"); !ok || found.BLZ != "20000000" {
		t.Errorf("ByBIC(EIGHTBICXXX) = %#v, %v", found, ok)
	}
	if found, ok := directory.ByBIC(""); ok {
		t.Errorf("ByBIC(empty) = %#v", found)
	}
}

func TestLookupTrimsTheInput(t *testing.T) {
	directory := FromEntries([]BankEntry{entry(
		"70020270",
		"UniCredit Bank - HypoVereinsbank",
		"München",
		"HYVEDEMMXXX",
	)})
	if found, ok := directory.Lookup(" 70020270 "); !ok || found.Name != "UniCredit Bank - HypoVereinsbank" {
		t.Errorf("Lookup(padded) = %#v, %v", found, ok)
	}
	if found, ok := directory.Lookup("00000000"); ok {
		t.Errorf("Lookup(unknown) = %#v", found)
	}
}

func TestBankDirectoryGolden(t *testing.T) {
	data, err := os.ReadFile(fixturePath("testdata", "golden", "bank_directory", "lookups.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}

	var golden struct {
		Entries []struct {
			BLZ        string `json:"blz"`
			Name       string `json:"name"`
			PostalCode string `json:"postal_code"`
			City       string `json:"city"`
			BIC        string `json:"bic"`
		} `json:"entries"`
		FintsBLZ []struct {
			Input string `json:"input"`
			BLZ   string `json:"blz"`
		} `json:"fints_blz"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Entries) == 0 {
		t.Fatal("the bank directory golden is empty")
	}

	input, err := os.ReadFile(fixturePath("testdata", "inputs", "bank_directory", "blz-test.txt")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	entries := Parse(input)
	if len(entries) != len(golden.Entries) {
		t.Fatalf("Parse returned %d entries, want %d", len(entries), len(golden.Entries))
	}
	for index, want := range golden.Entries {
		wantEntry := BankEntry{
			BLZ:        want.BLZ,
			Name:       want.Name,
			PostalCode: want.PostalCode,
			City:       want.City,
			BIC:        want.BIC,
		}
		if !reflect.DeepEqual(entries[index], wantEntry) {
			t.Errorf("entries[%d] = %#v, want %#v", index, entries[index], wantEntry)
		}
	}

	directory := FromEntries(entries)
	if len(golden.FintsBLZ) == 0 {
		t.Fatal("the fints_blz golden is empty")
	}
	for _, row := range golden.FintsBLZ {
		if got := FintsBLZ(directory, row.Input); got != row.BLZ {
			t.Errorf("FintsBLZ(%q) = %q, want %q", row.Input, got, row.BLZ)
		}
	}
}

// fixturePath locates a repository fixture from this package's directory.
func fixturePath(parts ...string) string {
	all := append([]string{"..", "..", ".."}, parts...)
	return filepath.Join(all...)
}
