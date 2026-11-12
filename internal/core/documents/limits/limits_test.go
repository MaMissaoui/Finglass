package limits_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/testsupport/testpdf"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestImageDimensionsReadsACraftedHugePNGHeaderWithoutDecodingPixels(t *testing.T) {
	file := filepath.Join(t.TempDir(), "huge.png")
	testpdf.CraftedPNG(file, 60_000, 60_000)

	width, height, ok := limits.ImageDimensions(file)
	if !ok {
		t.Fatal("ImageDimensions failed")
	}
	if width != 60_000 || height != 60_000 {
		t.Fatalf("dimensions = %dx%d, want 60000x60000", width, height)
	}
}

func TestCheckImageRejectsACraftedHugePNG(t *testing.T) {
	file := filepath.Join(t.TempDir(), "huge.png")
	testpdf.CraftedPNG(file, 60_000, 60_000)

	err := limits.CheckImage(file)
	if err == nil {
		t.Fatal("CheckImage accepted a huge image")
	}
	if !strings.Contains(err.Error(), "huge.png") {
		t.Fatalf("error %q does not contain the file name", err)
	}
}

func TestCheckImageAcceptsASmallPNG(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tiny.png")
	testpdf.CraftedPNG(file, 10, 10)

	width, height, ok := limits.ImageDimensions(file)
	if !ok || width != 10 || height != 10 {
		t.Fatalf("dimensions = %dx%d, ok=%v, want 10x10", width, height, ok)
	}
	if err := limits.CheckImage(file); err != nil {
		t.Fatalf("CheckImage rejected a small image: %v", err)
	}
}

func TestCheckImageAcceptsANonImageFile(t *testing.T) {
	file := writeFile(t, "notes.txt", []byte("not an image"))

	if width, height, ok := limits.ImageDimensions(file); ok {
		t.Fatalf("ImageDimensions read %dx%d from a text file", width, height)
	}
	if err := limits.CheckImage(file); err != nil {
		t.Fatalf("CheckImage rejected a non-image: %v", err)
	}
}

func TestCappedScaleLeavesA4At300DPIUnchangedAndCapsHugePages(t *testing.T) {
	a4Scale := float32(300.0) / 72.0
	if got := limits.CappedScale(595.0, 842.0, a4Scale); got != a4Scale {
		t.Fatalf("A4 scale = %v, want %v", got, a4Scale)
	}

	hugeScale := limits.CappedScale(14_400.0, 14_400.0, a4Scale)
	if hugeScale <= 0 {
		t.Fatalf("huge scale = %v, want positive", hugeScale)
	}
	pixels := 14_400.0 * float64(hugeScale) * 14_400.0 * float64(hugeScale)
	if pixels > float64(limits.MaxPixels) {
		t.Fatalf("pixels = %f, want at most %d", pixels, limits.MaxPixels)
	}
}

func TestCappedScaleLeavesNonPositiveOrNonFinitePagesUnchanged(t *testing.T) {
	cases := []struct {
		width  float32
		height float32
	}{
		{0, 842},
		{595, 0},
		{float32(math.NaN()), 842},
		{595, float32(math.Inf(1))},
	}
	for _, testCase := range cases {
		if got := limits.CappedScale(testCase.width, testCase.height, 4.5); got != 4.5 {
			t.Fatalf("CappedScale(%v, %v) = %v, want 4.5", testCase.width, testCase.height, got)
		}
	}
}

func jpegHeader() []byte {
	return []byte{
		0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x11, 0x08,
		0x00, 0x10, 0x00, 0x20, 0x00,
	}
}

func gifHeader() []byte {
	return []byte{'G', 'I', 'F', '8', '9', 'a', 0x10, 0x00, 0x20, 0x00, 0x00, 0x00}
}

func tiffHeader() []byte {
	data := make([]byte, 0, 38)
	data = append(data, 'I', 'I', 0x2A, 0x00)
	data = binary.LittleEndian.AppendUint32(data, 8)
	data = binary.LittleEndian.AppendUint16(data, 2)
	data = binary.LittleEndian.AppendUint16(data, 0x0100)
	data = binary.LittleEndian.AppendUint16(data, 3)
	data = binary.LittleEndian.AppendUint32(data, 1)
	data = binary.LittleEndian.AppendUint16(data, 100)
	data = append(data, 0, 0)
	data = binary.LittleEndian.AppendUint16(data, 0x0101)
	data = binary.LittleEndian.AppendUint16(data, 3)
	data = binary.LittleEndian.AppendUint32(data, 1)
	data = binary.LittleEndian.AppendUint16(data, 50)
	data = append(data, 0, 0)
	data = binary.LittleEndian.AppendUint32(data, 0)
	return data
}

func webPHeader() []byte {
	data := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
	data = append(data, 'V', 'P', '8', 'X')
	data = binary.LittleEndian.AppendUint32(data, 10)
	data = append(data, 0, 0, 0, 0)
	data = append(data, 99, 0, 0)
	data = append(data, 49, 0, 0)
	return data
}

func webPVP8Header() []byte {
	data := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
	data = append(data, 'V', 'P', '8', ' ')
	data = binary.LittleEndian.AppendUint32(data, 10)
	data = append(data, 0, 0, 0, 0, 0, 0)
	data = binary.LittleEndian.AppendUint16(data, 320)
	data = binary.LittleEndian.AppendUint16(data, 240)
	return data
}

func webPVP8LHeader() []byte {
	data := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
	data = append(data, 'V', 'P', '8', 'L')
	data = binary.LittleEndian.AppendUint32(data, 10)
	data = append(data, 0x2F)
	dims := uint32(239)<<14 | uint32(319)
	data = binary.LittleEndian.AppendUint32(data, dims)
	return data
}

func tiffBigEndianHeader() []byte {
	data := []byte{'M', 'M', 0x00, 0x2A}
	data = binary.BigEndian.AppendUint32(data, 8)
	data = binary.BigEndian.AppendUint16(data, 2)
	data = binary.BigEndian.AppendUint16(data, 0x0100)
	data = binary.BigEndian.AppendUint16(data, 3)
	data = binary.BigEndian.AppendUint32(data, 1)
	data = binary.BigEndian.AppendUint16(data, 200)
	data = append(data, 0, 0)
	data = binary.BigEndian.AppendUint16(data, 0x0101)
	data = binary.BigEndian.AppendUint16(data, 3)
	data = binary.BigEndian.AppendUint32(data, 1)
	data = binary.BigEndian.AppendUint16(data, 100)
	data = append(data, 0, 0)
	data = binary.BigEndian.AppendUint32(data, 0)
	return data
}

func tiffBigTIFFHeader() []byte {
	data := []byte{'I', 'I', 0x2B, 0x00}
	data = binary.LittleEndian.AppendUint16(data, 8)
	data = binary.LittleEndian.AppendUint16(data, 0)
	data = binary.LittleEndian.AppendUint64(data, 16)
	data = binary.LittleEndian.AppendUint64(data, 2)
	data = binary.LittleEndian.AppendUint16(data, 0x0100)
	data = binary.LittleEndian.AppendUint16(data, 3)
	data = binary.LittleEndian.AppendUint64(data, 1)
	data = binary.LittleEndian.AppendUint16(data, 640)
	data = append(data, 0, 0, 0, 0, 0, 0)
	data = binary.LittleEndian.AppendUint16(data, 0x0101)
	data = binary.LittleEndian.AppendUint16(data, 3)
	data = binary.LittleEndian.AppendUint64(data, 1)
	data = binary.LittleEndian.AppendUint16(data, 480)
	data = append(data, 0, 0, 0, 0, 0, 0)
	return data
}

func bmpHeader() []byte {
	data := make([]byte, 0x1A)
	data[0] = 'B'
	data[1] = 'M'
	binary.LittleEndian.PutUint32(data[0x12:], 120)
	binary.LittleEndian.PutUint32(data[0x16:], 80)
	return data
}

func pnmHeader() []byte {
	return []byte("P5\n10 20\n255\n")
}

func TestImageDimensionsReadsEverySupportedFormatHeader(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		width  uint32
		height uint32
	}{
		{"jpeg", jpegHeader(), 32, 16},
		{"gif", gifHeader(), 16, 32},
		{"tiff", tiffHeader(), 100, 50},
		{"tiff-big-endian", tiffBigEndianHeader(), 200, 100},
		{"tiff-bigtiff", tiffBigTIFFHeader(), 640, 480},
		{"webp-vp8x", webPHeader(), 100, 50},
		{"webp-vp8", webPVP8Header(), 320, 240},
		{"webp-vp8l", webPVP8LHeader(), 320, 240},
		{"bmp", bmpHeader(), 120, 80},
		{"pnm", pnmHeader(), 10, 20},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFile(t, "image."+testCase.name, testCase.data)
			width, height, ok := limits.ImageDimensions(path)
			if !ok {
				t.Fatal("ImageDimensions failed")
			}
			if width != testCase.width || height != testCase.height {
				t.Fatalf("dimensions = %dx%d, want %dx%d", width, height, testCase.width, testCase.height)
			}
		})
	}
}

func TestTruncatedImageHeadersNeverPanic(t *testing.T) {
	png := filepath.Join(t.TempDir(), "tiny.png")
	testpdf.CraftedPNG(png, 10, 10)
	pngBytes, err := os.ReadFile(png) // #nosec G304 -- test temporary file
	if err != nil {
		t.Fatalf("read PNG: %v", err)
	}

	cases := map[string][]byte{
		"jpeg":         jpegHeader(),
		"png":          pngBytes,
		"gif":          gifHeader(),
		"tiff":         tiffHeader(),
		"tiff-be":      tiffBigEndianHeader(),
		"tiff-bigtiff": tiffBigTIFFHeader(),
		"webp-vp8x":    webPHeader(),
		"webp-vp8":     webPVP8Header(),
		"webp-vp8l":    webPVP8LHeader(),
		"bmp":          bmpHeader(),
		"pnm":          pnmHeader(),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			for length := 0; length <= len(data); length++ {
				path := writeFile(t, "truncated", data[:length])
				_, _, _ = limits.ImageDimensions(path)
				_ = limits.CheckImage(path)
			}
		})
	}
}
