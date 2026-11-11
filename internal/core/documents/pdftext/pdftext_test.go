package pdftext

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
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

func words(text string) []string {
	return strings.Fields(text)
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
				if text.Text == fixture.Text.Text {
					t.Logf("%s: text is equal to Rust", fixture.File)
				} else if slices.Equal(words(text.Text), words(fixture.Text.Text)) {
					t.Logf("%s: text has the same words in the same order as Rust", fixture.File)
				} else {
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
