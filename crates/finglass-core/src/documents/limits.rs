//! Size and resolution limits for attached documents.
//!
//! Attached files can come from anyone, so a crafted file must not be able to exhaust memory while it is being
//! copied, parsed, rendered or OCR-ed. This ports FinDesk's `DocumentLimits`.

use std::path::Path;

use thiserror::Error;

use crate::i18n::{self, Arg, Key, Language};

/// The largest document that may be uploaded.
pub const MAX_DOCUMENT_BYTES: u64 = 100 * 1024 * 1024;

/// The largest number of pixels an image may have before anything decodes it.
pub const MAX_PIXELS: u64 = 100_000_000;

/// How many PDF pages a text layer is read from.
pub const MAX_TEXT_PAGES: usize = 500;

/// How many scanned PDF pages are rendered for OCR.
pub const MAX_OCR_PAGES: usize = 10;

/// The resolution scanned pages are rendered at before OCR.
pub const RENDER_DPI: f32 = 300.0;

/// A PDF text layer with fewer characters than this is treated as a scan.
pub const MIN_TEXT_LAYER_CHARS: usize = 20;

/// A document limit that a file violates.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum LimitError {
    /// The image declares more pixels than [`MAX_PIXELS`]; holds the file name.
    #[error("{}", image_too_large(Language::En, .0))]
    ImageTooLarge(String),
}

impl LimitError {
    /// The user text in `language`.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        match self {
            Self::ImageTooLarge(name) => image_too_large(language, name),
        }
    }
}

fn image_too_large(language: Language, name: &str) -> String {
    i18n::format(language, Key::ErrorImageTooLarge, &[Arg::Str(name.to_owned())])
}

/// Reads the width and height from an image file's header without decoding the pixels.
///
/// Returns `None` when the format is unsupported or the header cannot be read. This is the Rust equivalent of
/// FinDesk's `DocumentLimits.imageDimensions`, using `imagesize` instead of `ImageIO`.
#[must_use]
pub fn image_dimensions(file: &Path) -> Option<(u32, u32)> {
    let size = imagesize::size(file).ok()?;
    let width = u32::try_from(size.width).ok()?;
    let height = u32::try_from(size.height).ok()?;
    Some((width, height))
}

/// Rejects an image whose pixel count exceeds [`MAX_PIXELS`].
///
/// Images whose dimensions cannot be read are left to the caller's decoder, as in FinDesk.
pub fn check_image(file: &Path) -> Result<(), LimitError> {
    let Some((width, height)) = image_dimensions(file) else {
        return Ok(());
    };
    if u64::from(width) * u64::from(height) > MAX_PIXELS {
        let name = file
            .file_name()
            .map_or_else(String::new, |name| name.to_string_lossy().into_owned());
        return Err(LimitError::ImageTooLarge(name));
    }
    Ok(())
}

/// Lowers `scale` so that `width_pt * scale` by `height_pt * scale` does not exceed [`MAX_PIXELS`].
///
/// Non-positive or non-finite page sizes leave `scale` unchanged. This mirrors `cappedScale` exactly.
#[must_use]
pub fn capped_scale(width_pt: f32, height_pt: f32, scale: f32) -> f32 {
    if width_pt <= 0.0 || height_pt <= 0.0 || !width_pt.is_finite() || !height_pt.is_finite() {
        return scale;
    }
    let max_scale = (MAX_PIXELS as f64 / (f64::from(width_pt) * f64::from(height_pt))).sqrt();
    scale.min(max_scale as f32)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn crafted_png(path: &Path, width: u32, height: u32) {
        super::super::test_support::crafted_png(path, width, height);
    }

    #[test]
    fn image_dimensions_reads_a_crafted_huge_png_header_without_decoding_pixels() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("huge.png");
        crafted_png(&file, 60_000, 60_000);

        assert_eq!(image_dimensions(&file), Some((60_000, 60_000)));
    }

    #[test]
    fn check_image_rejects_a_crafted_huge_png() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("huge.png");
        crafted_png(&file, 60_000, 60_000);

        let error = check_image(&file).unwrap_err();
        assert!(error.to_string().contains("huge.png"), "{error}");
    }

    #[test]
    fn check_image_accepts_a_small_png() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("tiny.png");
        crafted_png(&file, 10, 10);

        assert_eq!(image_dimensions(&file), Some((10, 10)));
        assert!(check_image(&file).is_ok());
    }

    #[test]
    fn check_image_accepts_a_non_image_file() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("notes.txt");
        std::fs::write(&file, "not an image").unwrap();

        assert_eq!(image_dimensions(&file), None);
        assert!(check_image(&file).is_ok());
    }

    #[test]
    fn capped_scale_leaves_a4_at_300_dpi_unchanged_and_caps_huge_pages() {
        let a4_scale = 300.0 / 72.0;
        assert_eq!(capped_scale(595.0, 842.0, a4_scale), a4_scale);

        let huge_scale = capped_scale(14_400.0, 14_400.0, a4_scale);
        assert!(huge_scale > 0.0);
        let pixels = 14_400.0 * f64::from(huge_scale) * 14_400.0 * f64::from(huge_scale);
        assert!(pixels <= MAX_PIXELS as f64);
    }

    #[test]
    fn capped_scale_leaves_non_positive_or_non_finite_pages_unchanged() {
        for (width, height) in [(0.0, 842.0), (595.0, 0.0), (f32::NAN, 842.0), (595.0, f32::INFINITY)] {
            assert_eq!(capped_scale(width, height, 4.5), 4.5);
        }
    }
}
