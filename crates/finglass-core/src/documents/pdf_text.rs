//! Reading PDF page counts, page sizes and text layers.
//!
//! Text comes from the pure-Rust `pdf-extract` crate. A malformed PDF must never take the app down, so every
//! extraction runs inside [`catch_unwind`]; a PDF that cannot be parsed simply has no text layer.

use std::panic::{AssertUnwindSafe, catch_unwind};
use std::path::Path;

use pdf_extract::{Document, Object, PlainTextOutput, output_doc_page};

/// The text layer of a PDF.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PdfText {
    /// The extracted text of the first [`crate::documents::limits::MAX_TEXT_PAGES`] pages, trimmed.
    pub text: String,
    /// The PDF's total page count.
    pub page_count: usize,
}

/// One PDF page's size in points.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct PdfPage {
    /// The page width in points.
    pub width_pt: f32,
    /// The page height in points.
    pub height_pt: f32,
}

/// Page geometry of a PDF.
#[derive(Debug, Clone, PartialEq)]
pub struct PdfInfo {
    /// The PDF's total page count.
    pub page_count: usize,
    /// The pages in order.
    pub pages: Vec<PdfPage>,
}

/// Extracts the text of at most `max_pages` pages.
///
/// Returns `None` when the PDF cannot be loaded or parsed. This function never panics, even when the underlying
/// crate does on malformed input.
#[must_use]
pub fn extract_text(path: &Path, max_pages: usize) -> Option<PdfText> {
    catch_unwind(AssertUnwindSafe(|| extract_text_inner(path, max_pages)))
        .ok()
        .flatten()
}

fn extract_text_inner(path: &Path, max_pages: usize) -> Option<PdfText> {
    let mut document = Document::load(path).ok()?;
    if document.is_encrypted() {
        document.decrypt("").ok()?;
    }

    let page_count = document.get_pages().len();
    let limit = page_count.min(max_pages);
    let mut text = String::new();

    for page in 1..=u32::try_from(limit).ok()? {
        let mut output = PlainTextOutput::new(&mut text);
        output_doc_page(&document, &mut output, page).ok()?;
    }

    Some(PdfText {
        text: text.trim().to_owned(),
        page_count,
    })
}

/// Reads the page count and page sizes without extracting any text.
///
/// Returns `None` when the PDF cannot be loaded or parsed. Never panics.
#[must_use]
pub fn inspect(path: &Path) -> Option<PdfInfo> {
    catch_unwind(AssertUnwindSafe(|| inspect_inner(path))).ok().flatten()
}

fn inspect_inner(path: &Path) -> Option<PdfInfo> {
    let document = Document::load(path).ok()?;
    let page_ids = document.get_pages();
    let page_count = page_ids.len();
    let mut pages = Vec::with_capacity(page_count);

    for page_id in page_ids.into_values() {
        let dictionary = document.get_dictionary(page_id).ok()?;
        let (width_pt, height_pt) = page_size(&document, dictionary).unwrap_or((595.0, 842.0));
        pages.push(PdfPage { width_pt, height_pt });
    }

    Some(PdfInfo { page_count, pages })
}

/// The page's crop box, falling back to its media box and then the inherited ones.
fn page_size(document: &Document, dictionary: &pdf_extract::Dictionary) -> Option<(f32, f32)> {
    for key in [b"CropBox".as_slice(), b"MediaBox".as_slice()] {
        if let Some((width, height)) = inherited_size(document, dictionary, key, 0) {
            return Some((width, height));
        }
    }
    None
}

fn inherited_size(
    document: &Document,
    dictionary: &pdf_extract::Dictionary,
    key: &[u8],
    depth: usize,
) -> Option<(f32, f32)> {
    if depth > 16 {
        return None;
    }

    if let Ok(object) = dictionary.get(key) {
        let (_, object) = document.dereference(object).ok()?;
        if let Ok(values) = object.as_array()
            && values.len() >= 4
        {
            let x0 = number(document, &values[0])?;
            let y0 = number(document, &values[1])?;
            let x1 = number(document, &values[2])?;
            let y1 = number(document, &values[3])?;
            let width = (x1 - x0).abs();
            let height = (y1 - y0).abs();
            if width > 0.0 && height > 0.0 {
                return Some((width, height));
            }
        }
    }

    let parent = dictionary.get(b"Parent").ok()?.as_reference().ok()?;
    let parent = document.get_dictionary(parent).ok()?;
    inherited_size(document, parent, key, depth + 1)
}

fn number(document: &Document, object: &Object) -> Option<f32> {
    let (_, object) = document.dereference(object).ok()?;
    object.as_float().ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::documents::test_support::{empty_pdf, text_pdf};

    #[test]
    fn reads_the_text_layer_and_page_count_of_a_real_pdf() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("invoice.pdf");
        text_pdf(&file, &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"]);

        let text = extract_text(&file, 500).unwrap();
        assert_eq!(text.page_count, 1);
        assert!(text.text.contains("Rechnung Nr. 4711"), "{:?}", text.text);
        assert!(text.text.contains("89,99"), "{:?}", text.text);

        let info = inspect(&file).unwrap();
        assert_eq!(info.page_count, 1);
        assert_eq!(info.pages.len(), 1);
        assert!(info.pages[0].width_pt > 0.0);
        assert!(info.pages[0].height_pt > 0.0);
    }

    #[test]
    fn a_truncated_pdf_gives_no_text_and_never_panics() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("truncated.pdf");
        text_pdf(&file, &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"]);
        let bytes = std::fs::read(&file).unwrap();
        std::fs::write(&file, &bytes[..bytes.len() / 2]).unwrap();

        assert_eq!(extract_text(&file, 500), None);
        assert_eq!(inspect(&file), None);
    }

    #[test]
    fn a_pdf_without_a_text_layer_has_empty_text() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("empty.pdf");
        empty_pdf(&file);

        let text = extract_text(&file, 500).unwrap();
        assert_eq!(text.page_count, 1);
        assert!(text.text.trim().is_empty());
    }
}
