package pdf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

func TestMinimalPDFMatchesRust(t *testing.T) {
	builder := NewBuilder()
	builder.Text("Hello", Bold, 18.0)
	assertPDF(t, "testdata/minimal.pdf", builder.Finish())
}

func TestFlowPDFMatchesRust(t *testing.T) {
	builder := NewBuilder()
	builder.Heading("Steuerrelevante Buchungen 2026 – Größe · € …")
	builder.Text("Generated 15.10.2026 ✓ (tab\there, cr\rhere, nl\nhere) (parens) \\backslash", Regular, 9.0)
	builder.Gap(6.5)
	for i := 0; i < 140; i++ {
		builder.Row(fmt.Sprintf("Zeile %d – Bäckerei Schmidt", i), fmt.Sprintf("-%d.%02d €", i*7, i%100), Regular, 9.0)
		builder.EnsureSpace(14.0)
		builder.Advance(13.25)
		builder.DrawFitted("Beispielhaft lang benannte Handwerksfirma mit sehr ausführlichem Namen und Ort", Regular, 8.5, 50.0, 232.0)
		builder.DrawRightFitted("…3000 -1.234.567,89", Bold, 8.5, 545.0, 61.3)
		builder.Draw("\u0001\u007f😀 Ω", Bold, 7.0, 300.33)
	}
	for i, value := range []string{"(unbalanced", "close)", ")(", "a(b)c)d", "back\\slash(", "", " "} {
		builder.Text(value, Regular, float32(8.25)+float32(float32(i)*float32(0.1)))
		builder.Draw(value, Bold, float32(0.1)+float32(0.2), float32(float32(1.0)/float32(3.0))+float32(i))
		builder.Draw(value, Regular, 1e-7, 123456.79)
	}
	pages := builder.PageCount()
	for page := 0; page < pages; page++ {
		builder.AddFooter(page, fmt.Sprintf("Seite %d von %d", page+1, pages))
	}
	assertPDF(t, "testdata/flow.pdf", builder.Finish())
}

func assertPDF(t *testing.T, path string, got []byte) {
	t.Helper()

	want, err := os.ReadFile(path) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if bytes.Equal(got, want) {
		return
	}

	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	offset := 0
	for offset < limit && got[offset] == want[offset] {
		offset++
	}

	start := offset - 40
	if start < 0 {
		start = 0
	}
	gotEnd := offset + 40
	if gotEnd > len(got) {
		gotEnd = len(got)
	}
	wantEnd := offset + 40
	if wantEnd > len(want) {
		wantEnd = len(want)
	}

	t.Fatalf(
		"%s differs at offset %d (got %d bytes, want %d bytes)\n got[%d:%d] = %q\nwant[%d:%d] = %q",
		path, offset, len(got), len(want),
		start, gotEnd, got[start:gotEnd],
		start, wantEnd, want[start:wantEnd],
	)
}

type metricsCase struct {
	Text        []uint32 `json:"text"`
	Font        string   `json:"font"`
	SizeBits    uint32   `json:"size_bits"`
	WidthBits   uint32   `json:"width_bits"`
	Fit         []uint32 `json:"fit"`
	WidthOfBits uint32   `json:"width_of_bits"`
	Sanitize    []uint32 `json:"sanitize"`
	WinAnsi     []int    `json:"winansi"`
}

type metricsFile struct {
	Cases           []metricsCase `json:"cases"`
	TextFromWinAnsi []uint32      `json:"text_from_winansi"`
}

func TestMetricsMatchRust(t *testing.T) {
	data, err := os.ReadFile("testdata/metrics.json") // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	var metrics metricsFile
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatalf("parse metrics: %v", err)
	}
	if len(metrics.Cases) != 100 {
		t.Fatalf("got %d cases, want 100", len(metrics.Cases))
	}

	for index, testCase := range metrics.Cases {
		font := Regular
		switch testCase.Font {
		case "Regular":
		case "Bold":
			font = Bold
		default:
			t.Fatalf("case %d: unknown font %q", index, testCase.Font)
		}
		size := math.Float32frombits(testCase.SizeBits)
		width := math.Float32frombits(testCase.WidthBits)
		text := stringFromCodePoints(testCase.Text)

		gotFit := Fit(text, font, size, width)
		if want := stringFromCodePoints(testCase.Fit); gotFit != want {
			t.Errorf("case %d: Fit = %q, want %q", index, gotFit, want)
		}

		gotWidth := WidthOf(text, font, size)
		wantWidth := math.Float32frombits(testCase.WidthOfBits)
		if gotWidth != wantWidth {
			t.Errorf("case %d: WidthOf = %v, want %v", index, gotWidth, wantWidth)
		}
		if testCase.WidthOfBits != 0 && math.Float32bits(gotWidth) != testCase.WidthOfBits {
			t.Errorf("case %d: WidthOf bits = %#08x, want %#08x", index, math.Float32bits(gotWidth), testCase.WidthOfBits)
		}

		gotSanitize := Sanitize(text)
		if want := stringFromCodePoints(testCase.Sanitize); gotSanitize != want {
			t.Errorf("case %d: Sanitize = %q, want %q", index, gotSanitize, want)
		}

		gotWinAnsi := WinAnsiBytes(gotSanitize)
		wantWinAnsi := make([]byte, len(testCase.WinAnsi))
		for byteIndex, value := range testCase.WinAnsi {
			wantWinAnsi[byteIndex] = byte(value) // #nosec G115 -- metrics hold only WinAnsi bytes.
		}
		if !bytes.Equal(gotWinAnsi, wantWinAnsi) {
			t.Errorf("case %d: WinAnsiBytes = %v, want %v", index, gotWinAnsi, wantWinAnsi)
		}
	}

	all := make([]byte, 256)
	for index := range all {
		all[index] = byte(index)
	}
	gotText := TextFromWinAnsi(all)
	wantText := stringFromCodePoints(metrics.TextFromWinAnsi)
	if gotText != wantText {
		t.Errorf("TextFromWinAnsi differs: got %q, want %q", gotText, wantText)
	}
}

func stringFromCodePoints(codePoints []uint32) string {
	var result strings.Builder
	result.Grow(len(codePoints))
	for _, codePoint := range codePoints {
		result.WriteRune(rune(codePoint)) // #nosec G115 -- metrics hold only valid code points.
	}
	return result.String()
}

func TestSanitizeReplacesUnencodableCharactersAndControls(t *testing.T) {
	if got := Sanitize("Beispielhilfe ✓ e. V."); got != "Beispielhilfe ? e. V." {
		t.Errorf("Sanitize = %q", got)
	}
	if got := Sanitize("a\nb\tc\rd"); got != "a b c d" {
		t.Errorf("Sanitize = %q", got)
	}
	if got := Sanitize("Größe · € – …"); got != "Größe · € – …" {
		t.Errorf("Sanitize = %q", got)
	}
	if got := Sanitize("\u0001\u007f"); got != "??" {
		t.Errorf("Sanitize = %q", got)
	}
}

func TestWinAnsiBytesUseTheWindowsCodePage(t *testing.T) {
	cases := []struct {
		text string
		want []byte
	}{
		{"€", []byte{0x80}},
		{"·", []byte{0xb7}},
		{"…", []byte{0x85}},
		{"ö", []byte{0xf6}},
	}
	for _, testCase := range cases {
		if got := WinAnsiBytes(testCase.text); !bytes.Equal(got, testCase.want) {
			t.Errorf("WinAnsiBytes(%q) = %v, want %v", testCase.text, got, testCase.want)
		}
	}
}

func TestFitAddsAnEllipsisOnlyWhenNeeded(t *testing.T) {
	if got := Fit("short", Regular, 9.0, 100.0); got != "short" {
		t.Errorf("Fit = %q", got)
	}
	long := "Beispielhaft lang benannte Handwerksfirma mit sehr ausführlichem Namen und Ort"
	fitted := Fit(long, Regular, 9.0, 232.0)
	if !strings.HasSuffix(fitted, "…") {
		t.Errorf("Fit = %q, want an ellipsis", fitted)
	}
	if WidthOf(fitted, Regular, 9.0) > 232.0 {
		t.Errorf("Fit = %q is wider than 232", fitted)
	}
}

func TestADocumentStartsAndEndsLikeAPDF(t *testing.T) {
	builder := NewBuilder()
	builder.Text("Hello", Bold, 18.0)
	document := builder.Finish()
	if !bytes.HasPrefix(document, []byte("%PDF-")) {
		t.Errorf("document does not start with %%PDF-")
	}
	if !bytes.HasSuffix(document, []byte("%%EOF")) && !bytes.HasSuffix(document, []byte("%%EOF\n")) {
		t.Errorf("document does not end with %%%%EOF")
	}
}
