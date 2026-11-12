// Package limits holds the size and resolution limits for attached documents.
//
// Attached files can come from anyone, so a crafted file must not be able to
// exhaust memory while it is copied, parsed, rendered or OCR-ed. This ports
// FinDesk's DocumentLimits via `crates/finglass-core/src/documents/limits.rs`.
//
// ImageDimensions replaces the Rust `imagesize` 0.15 crate. It ports the
// detection order of `formats::image_type` and the size readers for JPEG, PNG,
// GIF, TIFF, WebP, BMP and PNM. Everything else the crate knows deliberately
// reports ok=false: Aseprite, ASTC, ATC, DDS, EAC/ETC2 (PKM), OpenEXR,
// Farbfeld, HEIF, Radiance HDR, ICO, ILBM, JPEG XL, KTX2, PSD, PVRTC, QOI,
// TGA and VTF. That is the one known deviation from Rust.
package limits

import (
	"math"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// MaxDocumentBytes is the largest document that may be uploaded.
const MaxDocumentBytes uint64 = 100 * 1024 * 1024

// MaxPixels is the largest number of pixels an image may have before anything
// decodes it.
const MaxPixels uint64 = 100_000_000

// MaxTextPages is how many PDF pages a text layer is read from.
const MaxTextPages = 500

// MaxOCRPages is how many scanned PDF pages are rendered for OCR.
const MaxOCRPages = 10

// RenderDPI is the resolution scanned pages are rendered at before OCR.
const RenderDPI float32 = 300.0

// MinTextLayerChars is the smallest PDF text layer that is not treated as a
// scan.
const MinTextLayerChars = 20

// LimitError is a document limit that a file violates.
//
// The zero value is not meaningful; ImageDimensions and CheckImage only build
// the ImageTooLarge variant.
type LimitError struct {
	// ImageTooLarge is the file name whose declared pixels exceed MaxPixels.
	ImageTooLarge string
}

// Error is the English text.
func (e *LimitError) Error() string {
	return e.Message(i18n.En)
}

// Message returns the user text in lang.
func (e *LimitError) Message(lang i18n.Language) string {
	return i18n.Format(lang, i18n.ErrorImageTooLarge, i18n.Str(e.ImageTooLarge))
}

// CheckImage rejects an image whose pixel count exceeds MaxPixels.
//
// Images whose dimensions cannot be read are left to the caller's decoder, as
// in FinDesk. A nil return means the image is acceptable.
func CheckImage(file string) *LimitError {
	width, height, ok := ImageDimensions(file)
	if !ok {
		return nil
	}
	if uint64(width)*uint64(height) > MaxPixels {
		return &LimitError{ImageTooLarge: fileName(file)}
	}
	return nil
}

// CappedScale lowers scale so that widthPt*scale by heightPt*scale does not
// exceed MaxPixels.
//
// Non-positive or non-finite page sizes leave scale unchanged. This mirrors
// Rust's capped_scale exactly: an f64 square root, then a comparison with the
// float32 scale.
func CappedScale(widthPt, heightPt, scale float32) float32 {
	if widthPt <= 0 || heightPt <= 0 || math.IsInf(float64(widthPt), 0) || math.IsNaN(float64(widthPt)) ||
		math.IsInf(float64(heightPt), 0) || math.IsNaN(float64(heightPt)) {
		return scale
	}
	maxScale := math.Sqrt(float64(MaxPixels) / (float64(widthPt) * float64(heightPt)))
	if maxScale < float64(scale) {
		return float32(maxScale)
	}
	return scale
}
