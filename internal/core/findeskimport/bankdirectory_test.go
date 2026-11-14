package findeskimport

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipEntry struct {
	name string
	data string
}

func writeJar(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(path) // #nosec G304 -- path is inside the test's temporary directory.
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		item, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("create jar entry %q: %v", entry.name, err)
		}
		if _, err := io.WriteString(item, entry.data); err != nil {
			t.Fatalf("write jar entry %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close jar writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close jar: %v", err)
	}
}

func appWithJar(t *testing.T, entries []zipEntry) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "FinDesk.app")
	appDir := filepath.Join(app, "Contents", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	writeJar(t, filepath.Join(appDir, "hbci4j-core-test.jar"), entries)
	return app
}

func TestFinDeskBankDirectoryReadsValidHTTPSURLs(t *testing.T) {
	overlong := strings.Repeat("x", maxBankDirectoryLineBytes+10)
	properties := strings.Join([]string{
		"# invented bank list",
		"",
		"11112222=Fixture Bank|Town|Bank|X|Extra|https://fints.example.test/pintan\r",
		"10000001=Blank URL|Town|Bank|X|Extra|   ",
		"10000002=Short line|Town",
		"10000003=Plain HTTP|Town|Bank|X|Extra|http://fints.example.test/pintan",
		"10000004=User info|Town|Bank|X|Extra|https://user:secret@fints.example.test/pintan",
		"10000005=Fragment|Town|Bank|X|Extra|https://fints.example.test/pintan#fragment",
		"10000006 No separator",
		"1234567=Seven digits|Town|Bank|X|Extra|https://fints.example.test/seven",
		"123456789=Nine digits|Town|Bank|X|Extra|https://fints.example.test/nine",
		"10000007=No URL field|Town|Bank|X|Extra",
		overlong,
		"10000008=Still valid|Town|Bank|X|Extra|HTTPS://fints.example.test/case",
	}, "\n")

	app := appWithJar(t, []zipEntry{{name: "blz.properties", data: properties}})
	lookup, err := FinDeskBankDirectory([]string{app})
	if err != nil {
		t.Fatalf("FinDeskBankDirectory: %v", err)
	}
	if lookup == nil {
		t.Fatal("no lookup")
	}

	for _, blz := range []string{"10000001", "10000002", "10000003", "10000004", "10000005", "10000006", "1234567", "123456789", "10000007"} {
		if _, ok := lookup(blz); ok {
			t.Errorf("%s was accepted", blz)
		}
	}
	if got, ok := lookup("11112222"); !ok || got != "https://fints.example.test/pintan" {
		t.Errorf("11112222 = %q, %v", got, ok)
	}
	if got, ok := lookup("10000008"); !ok || got != "HTTPS://fints.example.test/case" {
		t.Errorf("10000008 = %q, %v", got, ok)
	}
	if _, ok := lookup("99999999"); ok {
		t.Error("an unknown BLZ was found")
	}
}

func TestFinDeskBankDirectoryUsesTheFirstExactEntry(t *testing.T) {
	app := appWithJar(t, []zipEntry{
		{name: "folder/blz.properties", data: "11112222=Wrong|Town|Bank|X|Extra|https://fints.example.test/wrong"},
		{name: "blz.properties", data: "11112222=First|Town|Bank|X|Extra|https://fints.example.test/first"},
		{name: "blz.properties", data: "11112222=Second|Town|Bank|X|Extra|https://fints.example.test/second"},
	})

	lookup, err := FinDeskBankDirectory([]string{app})
	if err != nil {
		t.Fatalf("FinDeskBankDirectory: %v", err)
	}
	if lookup == nil {
		t.Fatal("no lookup")
	}
	if got, _ := lookup("11112222"); got != "https://fints.example.test/first" {
		t.Errorf("URL = %q, want the first exact entry", got)
	}
}

func TestFinDeskBankDirectoryNeedsExactlyOneJar(t *testing.T) {
	app := filepath.Join(t.TempDir(), "FinDesk.app")
	appDir := filepath.Join(app, "Contents", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	entries := []zipEntry{{name: "blz.properties", data: "11112222=Bank|Town|Bank|X|Extra|https://fints.example.test/pintan"}}
	writeJar(t, filepath.Join(appDir, "hbci4j-core-1.jar"), entries)
	writeJar(t, filepath.Join(appDir, "hbci4j-core-2.jar"), entries)

	lookup, err := FinDeskBankDirectory([]string{app})
	if err != nil {
		t.Fatalf("FinDeskBankDirectory: %v", err)
	}
	if lookup != nil {
		t.Error("two jars produced a lookup")
	}
}

func TestFinDeskBankDirectoryIgnoresMissingAppsAndJars(t *testing.T) {
	if lookup, err := FinDeskBankDirectory(nil); err != nil || lookup != nil {
		t.Errorf("no paths = %v, %v", lookup, err)
	}
	if lookup, err := FinDeskBankDirectory([]string{filepath.Join(t.TempDir(), "Missing.app")}); err != nil || lookup != nil {
		t.Errorf("missing app = %v, %v", lookup, err)
	}

	app := filepath.Join(t.TempDir(), "FinDesk.app")
	appDir := filepath.Join(app, "Contents", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	writeJar(t, filepath.Join(appDir, "hbci4j-core-test.jar"), []zipEntry{{name: "other.properties", data: "x"}})
	if lookup, err := FinDeskBankDirectory([]string{app}); err != nil || lookup != nil {
		t.Errorf("jar without a list = %v, %v", lookup, err)
	}
}

func TestFinDeskBankDirectoryRejectsAnOversizedList(t *testing.T) {
	app := appWithJar(t, []zipEntry{{
		name: "blz.properties",
		data: strings.Repeat("a", maxBankDirectoryDataBytes+1),
	}})
	lookup, err := FinDeskBankDirectory([]string{app})
	if err == nil {
		t.Fatal("an oversized list was accepted")
	}
	if lookup != nil {
		t.Error("an oversized list produced a lookup")
	}
	if !strings.HasPrefix(err.Error(), "bank directory: ") {
		t.Errorf("error = %q, want the bank directory prefix", err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Errorf("error contains list content: %q", err)
	}
}

func TestFinDeskBankDirectoryRejectsTooManyEntries(t *testing.T) {
	entries := make([]zipEntry, maxBankDirectoryEntries+1)
	for index := range entries {
		entries[index] = zipEntry{name: fmt.Sprintf("file-%05d.txt", index), data: "x"}
	}
	app := appWithJar(t, entries)

	lookup, err := FinDeskBankDirectory([]string{app})
	if err == nil {
		t.Fatal("a jar with too many entries was accepted")
	}
	if lookup != nil {
		t.Error("a jar with too many entries produced a lookup")
	}
	if !strings.HasPrefix(err.Error(), "bank directory: ") {
		t.Errorf("error = %q, want the bank directory prefix", err)
	}
}
