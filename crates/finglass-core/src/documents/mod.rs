//! Documents: storage, text extraction and OCR.
//!
//! These modules port FinDesk's document package. Files are stored in `<profile dir>/documents/`, PDF text layers
//! are read with a pure-Rust extractor, and OCR runs the external Tesseract command-line program.

pub mod limits;
pub mod ocr;
pub mod pdf_text;
mod programs;
pub mod service;
pub mod tesseract;

pub use limits::{MAX_DOCUMENT_BYTES, MAX_OCR_PAGES, MAX_PIXELS, MAX_TEXT_PAGES, MIN_TEXT_LAYER_CHARS, RENDER_DPI};
pub use ocr::{OcrEngine, OcrError, OcrResult, OcrService, Recognition, SearchResult, Source};
pub use service::{
    DocumentError, DocumentService, MatchSuggestion, SUPPORTED_EXTENSIONS, UploadResult, detect_mime_type,
    sanitize_file_name,
};
pub use tesseract::TesseractCli;

/// Test fixtures shared by the document modules.
#[cfg(test)]
pub(crate) mod test_support {
    use std::path::Path;

    use pdf_extract::{Dictionary, Document, Object, Stream};

    fn escape_text(text: &str) -> String {
        text.replace('\\', "\\\\").replace('(', "\\(").replace(')', "\\)")
    }

    fn save_document(mut document: Document, path: &Path) {
        document.save(path).expect("the test PDF can be written");
    }

    fn page_objects(
        document: &mut Document,
        content: Vec<u8>,
    ) -> (pdf_extract::ObjectId, pdf_extract::ObjectId, pdf_extract::ObjectId) {
        let pages_id = document.new_object_id();

        let mut font = Dictionary::new();
        font.set("Type", Object::Name(b"Font".to_vec()));
        font.set("Subtype", Object::Name(b"Type1".to_vec()));
        font.set("BaseFont", Object::Name(b"Helvetica".to_vec()));
        font.set("Encoding", Object::Name(b"WinAnsiEncoding".to_vec()));
        let font_id = document.add_object(font);

        let mut fonts = Dictionary::new();
        fonts.set("F1", Object::Reference(font_id));
        let mut resources = Dictionary::new();
        resources.set("Font", fonts);
        let resources_id = document.add_object(resources);

        let content_id = document.add_object(Stream::new(Dictionary::new(), content));

        let mut page = Dictionary::new();
        page.set("Type", Object::Name(b"Page".to_vec()));
        page.set("Parent", Object::Reference(pages_id));
        page.set(
            "MediaBox",
            Object::Array(vec![0.into(), 0.into(), 595.into(), 842.into()]),
        );
        page.set("Contents", Object::Reference(content_id));
        page.set("Resources", Object::Reference(resources_id));
        let page_id = document.add_object(page);

        let mut pages = Dictionary::new();
        pages.set("Type", Object::Name(b"Pages".to_vec()));
        pages.set("Kids", Object::Array(vec![Object::Reference(page_id)]));
        pages.set("Count", Object::Integer(1));
        document.objects.insert(pages_id, Object::Dictionary(pages));

        let mut catalog = Dictionary::new();
        catalog.set("Type", Object::Name(b"Catalog".to_vec()));
        catalog.set("Pages", Object::Reference(pages_id));
        let catalog_id = document.add_object(catalog);

        document.trailer.set("Root", Object::Reference(catalog_id));

        (pages_id, page_id, content_id)
    }

    /// Writes a single-page PDF whose text layer contains `lines`.
    pub fn text_pdf(path: &Path, lines: &[&str]) {
        let mut content = String::from("BT\n/F1 12 Tf\n50 700 Td\n");
        for line in lines {
            content.push('(');
            content.push_str(&escape_text(line));
            content.push_str(") Tj\n0 -16 Td\n");
        }
        content.push_str("ET\n");

        let mut document = Document::with_version("1.5");
        page_objects(&mut document, content.into_bytes());
        save_document(document, path);
    }

    /// Writes a single-page PDF without any text.
    pub fn empty_pdf(path: &Path) {
        let mut document = Document::with_version("1.5");
        page_objects(&mut document, Vec::new());
        save_document(document, path);
    }

    /// Writes a PNG that is only a header declaring `width` x `height`, with no pixel data.
    ///
    /// The header is enough for `imagesize`, which never decodes pixels.
    pub fn crafted_png(path: &Path, width: u32, height: u32) {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(b"\x89PNG\r\n\x1a\n");
        bytes.extend_from_slice(&13u32.to_be_bytes());
        bytes.extend_from_slice(b"IHDR");
        bytes.extend_from_slice(&width.to_be_bytes());
        bytes.extend_from_slice(&height.to_be_bytes());
        bytes.extend_from_slice(&[8, 2, 0, 0, 0]);
        bytes.extend_from_slice(&0u32.to_be_bytes());
        bytes.extend_from_slice(&0u32.to_be_bytes());
        bytes.extend_from_slice(b"IEND");
        std::fs::write(path, bytes).expect("the test PNG can be written");
    }
}
