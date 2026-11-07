//! Extracting text from PDFs and images.
//!
//! - PDFs with a text layer: text is read directly from the file (exact, confidence 100).
//! - Scanned PDFs and images: pages are rendered and passed to the [`OcrEngine`] if one is available.
//!
//! This ports FinDesk's `OcrService`; PDFBox is replaced by `pdf-extract` and `pdftoppm`, and tess4j by the
//! Tesseract command-line program.

use std::path::Path;
use std::sync::Arc;

use thiserror::Error;

use crate::db::Database;
use crate::repo::{self, RepoError};

use super::limits::{self, MAX_OCR_PAGES, MAX_TEXT_PAGES, MIN_TEXT_LAYER_CHARS, RENDER_DPI};
use super::pdf_text;
use super::programs;
use super::tesseract::{COMMAND_TIMEOUT, run_with_timeout};

/// One engine's recognition result.
#[derive(Debug, Clone, PartialEq)]
pub struct Recognition {
    /// The recognized text.
    pub text: String,
    /// The confidence from 0 to 100, as Tesseract reports it.
    pub confidence: f64,
}

/// Everything that can go wrong while recognizing an image.
#[derive(Debug, Error)]
pub enum OcrError {
    /// The engine could not be started.
    #[error(transparent)]
    Io(#[from] std::io::Error),
    /// The engine ran but failed.
    #[error("OCR command failed")]
    CommandFailed,
}

/// Recognizes text in a raster image.
///
/// Implementations must be safe to share; the real engine serializes access itself, and the fake engines in tests
/// use atomics.
pub trait OcrEngine: Send + Sync {
    /// Recognizes the image at `image`.
    fn recognize(&self, image: &Path) -> Result<Recognition, OcrError>;
}

/// Where an OCR result's text came from.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Source {
    /// The PDF's own text layer.
    TextLayer,
    /// The OCR engine.
    Ocr,
}

impl Source {
    /// The name used in audit details, matching FinDesk's enum names.
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::TextLayer => "TEXT_LAYER",
            Self::Ocr => "OCR",
        }
    }
}

/// A document's extracted text.
#[derive(Debug, Clone, PartialEq)]
pub struct OcrResult {
    /// The extracted text.
    pub text: String,
    /// The detected language, `de` or `en`.
    pub language: String,
    /// The confidence from 0 to 100.
    pub confidence: f64,
    /// The document's page count.
    pub page_count: usize,
    /// Where the text came from.
    pub source: Source,
}

/// A hit in stored OCR text.
#[derive(Debug, Clone, PartialEq)]
pub struct SearchResult {
    /// The document.
    pub document_id: i64,
    /// The document's file name.
    pub file_name: String,
    /// A snippet around the first match.
    pub matching_text: String,
    /// The document's OCR confidence, or zero.
    pub confidence: f64,
}

/// Image extensions that go through the OCR engine.
const IMAGE_EXTENSIONS: &[&str] = &["jpg", "jpeg", "png", "tiff", "tif"];

const GERMAN_WORDS: &[&str] = &[
    "der", "die", "das", "und", "ist", "von", "zu", "mit", "für", "rechnung", "summe", "betrag",
];

const ENGLISH_WORDS: &[&str] = &[
    "the", "and", "is", "of", "to", "for", "with", "invoice", "total", "amount",
];

/// Extracts text from PDFs and images.
pub struct OcrService {
    engine: Option<Arc<dyn OcrEngine>>,
}

impl OcrService {
    /// Builds a service with an optional engine.
    #[must_use]
    pub fn new(engine: Option<Arc<dyn OcrEngine>>) -> Self {
        Self { engine }
    }

    /// Builds a service without OCR, so only PDF text layers work.
    #[must_use]
    pub fn without_ocr() -> Self {
        Self { engine: None }
    }

    /// Whether an OCR engine is available.
    #[must_use]
    pub fn is_ocr_available(&self) -> bool {
        self.engine.is_some()
    }

    /// Processes a document.
    ///
    /// Returns `None` for an unsupported file type, or when the file needs OCR and no engine is available.
    /// Processing errors also yield `None`, as in FinDesk.
    #[must_use]
    pub fn process_document(&self, file: &Path) -> Option<OcrResult> {
        let extension = extension_of(file);
        match extension.as_str() {
            "pdf" => self.process_pdf(file),
            extension if IMAGE_EXTENSIONS.contains(&extension) => self.process_image(file),
            _ => None,
        }
    }

    fn process_pdf(&self, file: &Path) -> Option<OcrResult> {
        let extracted = pdf_text::extract_text(file, MAX_TEXT_PAGES);
        if let Some(extracted) = &extracted
            && extracted.text.chars().count() >= MIN_TEXT_LAYER_CHARS
        {
            return Some(OcrResult {
                text: extracted.text.clone(),
                language: detect_language(&extracted.text),
                confidence: 100.0,
                page_count: extracted.page_count,
                source: Source::TextLayer,
            });
        }

        let engine = self.engine.as_ref()?;
        let info = pdf_text::inspect(file)?;
        let temp = tempfile::tempdir().ok()?;
        let mut recognitions = Vec::new();

        for (index, page) in info.pages.iter().take(MAX_OCR_PAGES).enumerate() {
            let scale = limits::capped_scale(page.width_pt, page.height_pt, RENDER_DPI / 72.0);
            // Capped below 72 DPI the page is too coarse for OCR, so it is skipped.
            if scale < 1.0 {
                continue;
            }
            let dpi = (scale * 72.0).floor() as u32;
            if dpi == 0 {
                continue;
            }

            let prefix = temp.path().join(format!("page-{}", index + 1));
            let args = vec![
                "-f".into(),
                (index + 1).to_string().into(),
                "-l".into(),
                (index + 1).to_string().into(),
                "-r".into(),
                dpi.to_string().into(),
                "-gray".into(),
                "-png".into(),
                "-singlefile".into(),
                file.as_os_str().to_owned(),
                prefix.as_os_str().to_owned(),
            ];

            let pdftoppm = programs::resolve("pdftoppm")?;
            let output = match run_with_timeout(&pdftoppm, &args, COMMAND_TIMEOUT) {
                Ok(output) => output,
                // Without poppler a scanned PDF simply has no OCR, like FinDesk without Tesseract.
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => return None,
                Err(_) => continue,
            };
            if !output.success {
                continue;
            }

            let image = prefix.with_extension("png");
            if !image.exists() {
                continue;
            }
            match engine.recognize(&image) {
                Ok(recognition) => recognitions.push(recognition),
                Err(_) => return None,
            }
        }

        let combined = recognitions
            .iter()
            .map(|recognition| recognition.text.trim())
            .filter(|text| !text.is_empty())
            .collect::<Vec<_>>()
            .join("\n\n")
            .trim()
            .to_owned();

        Some(OcrResult {
            language: detect_language(&combined),
            confidence: average_confidence(&recognitions),
            text: combined,
            page_count: info.page_count,
            source: Source::Ocr,
        })
    }

    fn process_image(&self, file: &Path) -> Option<OcrResult> {
        let engine = self.engine.as_ref()?;
        limits::check_image(file).ok()?;
        let recognition = engine.recognize(file).ok()?;
        let text = recognition.text.trim().to_owned();
        let confidence = average_confidence(std::slice::from_ref(&recognition));

        Some(OcrResult {
            language: detect_language(&text),
            confidence,
            text,
            page_count: 1,
            source: Source::Ocr,
        })
    }

    /// Searches stored OCR text and returns a snippet around the first match per document.
    pub fn search_ocr_text(&self, db: &Database, query: &str) -> Result<Vec<SearchResult>, RepoError> {
        let query = query.trim();
        if query.is_empty() {
            return Ok(Vec::new());
        }

        let documents = repo::documents::find_items(db, Some(query), false, None)?;
        let mut results = Vec::new();

        for document in documents {
            let Some(content) = repo::documents::get_ocr_text(db, document.id)? else {
                continue;
            };
            if let Some(snippet) = snippet_around(&content, query) {
                results.push(SearchResult {
                    document_id: document.id,
                    file_name: document.file_name,
                    matching_text: snippet,
                    confidence: document.ocr_confidence.unwrap_or(0.0),
                });
            }
        }

        Ok(results)
    }
}

/// Detects `de` or `en` by counting common whole words, as FinDesk does.
#[must_use]
pub fn detect_language(text: &str) -> String {
    let lower = text.to_lowercase();
    let words = lower
        .split(|character: char| !character.is_alphabetic())
        .filter(|word| !word.is_empty());

    let mut german = 0usize;
    let mut english = 0usize;
    for word in words {
        if GERMAN_WORDS.contains(&word) {
            german += 1;
        }
        if ENGLISH_WORDS.contains(&word) {
            english += 1;
        }
    }

    if english > german { "en" } else { "de" }.to_owned()
}

fn average_confidence(results: &[Recognition]) -> f64 {
    if results.is_empty() {
        return 0.0;
    }
    let average = results.iter().map(|result| result.confidence).sum::<f64>() / results.len() as f64;
    (average * 100.0).round() / 100.0
}

fn extension_of(file: &Path) -> String {
    file.extension()
        .map(|extension| extension.to_string_lossy().to_lowercase())
        .unwrap_or_default()
}

fn snippet_around(content: &str, query: &str) -> Option<String> {
    let characters: Vec<char> = content.chars().collect();
    let needle: Vec<char> = query.chars().collect();
    let index = find_ignore_case(&characters, &needle)?;
    let start = index.saturating_sub(40);
    let end = (index + needle.len() + 40).min(characters.len());
    let snippet: String = characters[start..end].iter().collect();
    Some(collapse_whitespace(&snippet))
}

fn find_ignore_case(haystack: &[char], needle: &[char]) -> Option<usize> {
    if needle.is_empty() {
        return Some(0);
    }
    haystack.windows(needle.len()).position(|window| {
        window
            .iter()
            .zip(needle)
            .all(|(left, right)| chars_equal(*left, *right))
    })
}

fn chars_equal(left: char, right: char) -> bool {
    left == right || left.to_lowercase().eq(right.to_lowercase())
}

fn collapse_whitespace(text: &str) -> String {
    let mut collapsed = String::with_capacity(text.len());
    let mut previous_whitespace = false;
    for character in text.chars() {
        if character.is_whitespace() {
            if !previous_whitespace {
                collapsed.push(' ');
            }
            previous_whitespace = true;
        } else {
            collapsed.push(character);
            previous_whitespace = false;
        }
    }
    collapsed
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use super::*;

    struct CountingEngine {
        calls: AtomicUsize,
    }

    impl OcrEngine for CountingEngine {
        fn recognize(&self, _image: &Path) -> Result<Recognition, OcrError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Ok(Recognition {
                text: String::new(),
                confidence: 0.0,
            })
        }
    }

    #[test]
    fn ocr_result_data_class() {
        let result = OcrResult {
            text: "Sample OCR text".to_owned(),
            language: "de".to_owned(),
            confidence: 85.5,
            page_count: 1,
            source: Source::Ocr,
        };

        assert_eq!(result.text, "Sample OCR text");
        assert_eq!(result.language, "de");
        assert_eq!(result.confidence, 85.5);
        assert_eq!(result.page_count, 1);
        assert_eq!(result.source, Source::Ocr);
    }

    #[test]
    fn search_result_data_class() {
        let result = SearchResult {
            document_id: 1,
            file_name: "test.pdf".to_owned(),
            matching_text: "matching text".to_owned(),
            confidence: 90.0,
        };

        assert_eq!(result.document_id, 1);
        assert_eq!(result.file_name, "test.pdf");
        assert_eq!(result.matching_text, "matching text");
        assert_eq!(result.confidence, 90.0);
    }

    #[test]
    fn process_document_returns_none_without_calling_the_engine_for_an_oversized_image() {
        let temp = tempfile::tempdir().unwrap();
        let file = temp.path().join("huge.png");
        super::super::test_support::crafted_png(&file, 60_000, 60_000);
        let engine = Arc::new(CountingEngine {
            calls: AtomicUsize::new(0),
        });
        let service = OcrService::new(Some(engine.clone()));

        assert_eq!(service.process_document(&file), None);
        assert_eq!(engine.calls.load(Ordering::SeqCst), 0);
    }

    #[test]
    fn detect_language_counts_common_whole_words() {
        assert_eq!(detect_language("Rechnung Summe Betrag und die"), "de");
        assert_eq!(detect_language("Invoice total amount for the and"), "en");
        assert_eq!(detect_language(""), "de", "the German default wins a tie");
    }

    #[test]
    fn snippets_surround_the_first_match_and_collapse_whitespace() {
        let content = format!("{} Rechnung   Nr. 4711 {}", "a".repeat(50), "b".repeat(50));
        let snippet = snippet_around(&content, "rechnung").unwrap();
        assert!(snippet.contains("Rechnung Nr. 4711"), "{snippet:?}");
        assert!(!snippet.contains("  "), "whitespace should be collapsed: {snippet:?}");
        assert_eq!(snippet_around("nothing", "4711"), None);
    }
}
