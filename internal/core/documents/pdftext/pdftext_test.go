package pdftext

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type goldenText struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

type goldenPage struct {
	WidthPt  float64 `json:"width_pt"`
	HeightPt float64 `json:"height_pt"`
}

type goldenInfo struct {
	PageCount int          `json:"page_count"`
	Pages     []goldenPage `json:"pages"`
}

type goldenPDF struct {
	File string      `json:"file"`
	Text *goldenText `json:"text"`
	Info *goldenInfo `json:"info"`
}

// repoRoot finds the checkout root from this package's test directory.
func repoRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above the test directory")
		}
		directory = parent
	}
}

func readGolden(t *testing.T) []goldenPDF {
	t.Helper()

	path := filepath.Join(repoRoot(t), "testdata", "golden", "pdf_text.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var golden []goldenPDF
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(golden) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return golden
}

func TestExtractAndInspectMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)

	for _, fixture := range readGolden(t) {
		t.Run(fixture.File, func(t *testing.T) {
			path := filepath.Join(root, "testdata", "pdf", fixture.File)

			text, ok := Extract(path, 5)
			if fixture.Text == nil {
				if ok {
					t.Fatalf("Extract(%s) succeeded, but Rust gives no text", fixture.File)
				}
			} else {
				if !ok {
					t.Fatalf("Extract(%s) failed, but Rust returns text", fixture.File)
				}
				if text.PageCount != fixture.Text.PageCount {
					t.Errorf("page count = %d, want %d", text.PageCount, fixture.Text.PageCount)
				}
				if text.Text != fixture.Text.Text {
					t.Errorf(
						"text differs from Rust:\n got: %q\nwant: %q",
						text.Text,
						fixture.Text.Text,
					)
				}
			}

			info, ok := Inspect(path)
			if fixture.Info == nil {
				if ok {
					t.Fatalf("Inspect(%s) succeeded, but Rust gives no info", fixture.File)
				}
				return
			}
			if !ok {
				t.Fatalf("Inspect(%s) failed, but Rust returns info", fixture.File)
			}
			if info.PageCount != fixture.Info.PageCount {
				t.Errorf("page count = %d, want %d", info.PageCount, fixture.Info.PageCount)
			}
			if len(info.Pages) != len(fixture.Info.Pages) {
				t.Fatalf("page sizes = %d pages, want %d", len(info.Pages), len(fixture.Info.Pages))
			}
			for index, want := range fixture.Info.Pages {
				got := info.Pages[index]
				if math.Abs(got.WidthPt-want.WidthPt) > 0.01 {
					t.Errorf("page %d width = %f, want %f", index+1, got.WidthPt, want.WidthPt)
				}
				if math.Abs(got.HeightPt-want.HeightPt) > 0.01 {
					t.Errorf("page %d height = %f, want %f", index+1, got.HeightPt, want.HeightPt)
				}
			}
		})
	}
}

func TestExtractHonoursMaxPagesAndKeepsTheTotalCount(t *testing.T) {
	path := filepath.Join(repoRoot(t), "testdata", "pdf", "multipage.pdf")

	text, ok := Extract(path, 2)
	if !ok {
		t.Fatal("Extract failed")
	}
	if text.PageCount != 3 {
		t.Errorf("page count = %d, want 3", text.PageCount)
	}
	if !strings.Contains(text.Text, "Seite Eins") || !strings.Contains(text.Text, "Page Two") {
		t.Errorf("first two pages missing from %q", text.Text)
	}
	if strings.Contains(text.Text, "Page Three") {
		t.Errorf("the third page was read despite maxPages=2: %q", text.Text)
	}
}

func TestMissingAndOversizedFilesAreRefused(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pdf")
	if _, ok := Extract(missing, 5); ok {
		t.Error("Extract accepted a missing file")
	}
	if _, ok := Inspect(missing); ok {
		t.Error("Inspect accepted a missing file")
	}

	oversized := filepath.Join(t.TempDir(), "oversized.pdf")
	file, err := os.Create(oversized) // #nosec G304 -- path is the test's temporary file
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := file.Truncate(maxFileBytes + 1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := Extract(oversized, 5); ok {
		t.Error("Extract accepted a file above the size cap")
	}
	if _, ok := Inspect(oversized); ok {
		t.Error("Inspect accepted a file above the size cap")
	}
}

type rustTextEntry struct {
	File      string  `json:"file"`
	MaxPages  int     `json:"max_pages"`
	Text      *string `json:"text"`
	PageCount *int    `json:"page_count"`
}

func readRustText(t *testing.T) []rustTextEntry {
	t.Helper()

	path := filepath.Join("testdata", "rust", "rust_text.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the package directory
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entries []rustTextEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return entries
}

func rustFixturePath(t *testing.T, file string) string {
	t.Helper()

	rustPath := filepath.Join("testdata", "rust", file)
	if _, err := os.Stat(rustPath); err == nil {
		return rustPath
	}
	return filepath.Join(repoRoot(t), "testdata", "pdf", file)
}

func TestExtractMatchesRustPdfExtract(t *testing.T) {
	for _, entry := range readRustText(t) {
		t.Run(fmt.Sprintf("%s/max_pages=%d", entry.File, entry.MaxPages), func(t *testing.T) {
			text, ok := Extract(rustFixturePath(t, entry.File), entry.MaxPages)
			if entry.Text == nil {
				if ok {
					t.Fatalf("Extract(%s) succeeded, but Rust gives no text", entry.File)
				}
				return
			}
			if !ok {
				t.Fatalf("Extract(%s) failed, but Rust returns text %q", entry.File, *entry.Text)
			}
			if text.Text != *entry.Text {
				t.Errorf("text differs from Rust:\n got: %q\nwant: %q", text.Text, *entry.Text)
			}
			if entry.PageCount != nil && text.PageCount != *entry.PageCount {
				t.Errorf("page count = %d, want %d", text.PageCount, *entry.PageCount)
			}
		})
	}
}

func TestExtractNeverPanicsOnTruncatedPrefixes(t *testing.T) {
	files := make([]string, 0)
	for _, directory := range []string{
		filepath.Join("testdata", "rust"),
		filepath.Join(repoRoot(t), "testdata", "pdf"),
	} {
		matches, err := filepath.Glob(filepath.Join(directory, "*.pdf"))
		if err != nil {
			t.Fatalf("glob %s: %v", directory, err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no corpus PDFs found")
	}

	target := filepath.Join(t.TempDir(), "prefix.pdf")
	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 -- fixed corpus path
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		lengths := make([]int, 0, 80)
		for cut := 0; cut < len(data); cut++ {
			if cut < 64 {
				lengths = append(lengths, cut)
			}
		}
		step := len(data) / 48
		if step < 1 {
			step = 1
		}
		for cut := 0; cut < len(data); cut += step {
			lengths = append(lengths, cut)
		}
		lengths = append(lengths, len(data)-1)

		for _, cut := range lengths {
			// #nosec G703 -- target is created inside t.TempDir()
			if err := os.WriteFile(target, data[:cut], 0o600); err != nil {
				t.Fatalf("write prefix: %v", err)
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("%s prefix %d panicked: %v", filepath.Base(file), cut, recovered)
					}
				}()
				_, _ = Extract(target, 500)
			}()
		}
	}
}
