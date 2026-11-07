//! Low-level PDF writing for the report exports.
//!
//! The layout mirrors FinDesk's PDFBox exporters: A4 pages, the Standard-14 Helvetica and
//! Helvetica-Bold fonts with WinAnsiEncoding, 50 pt margins, flowing text and per-page footers.
//! Content streams are left uncompressed so the tests can inspect them.

use pdf_writer::{Content, Finish, Name, Pdf, Rect, Ref, Str};

/// A4 width in points.
pub const PAGE_WIDTH: f32 = 595.0;
/// A4 height in points.
pub const PAGE_HEIGHT: f32 = 842.0;
/// The page margin in points.
pub const MARGIN: f32 = 50.0;
/// The footer baseline in points.
pub const FOOTER_Y: f32 = 28.0;
/// The footer font size.
pub const FOOTER_SIZE: f32 = 8.0;
/// The ellipsis used when fitted text is cut.
pub const ELLIPSIS: &str = "…";

/// One of the two Standard-14 fonts the exports use.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PdfFont {
    /// Helvetica.
    Regular,
    /// Helvetica-Bold.
    Bold,
}

impl PdfFont {
    fn resource(self) -> Name<'static> {
        match self {
            Self::Regular => Name(b"F1"),
            Self::Bold => Name(b"F2"),
        }
    }
}

/// Replaces characters Standard-14 WinAnsi fonts cannot encode, exactly like FinDesk's `sanitize`:
/// newlines, carriage returns and tabs become spaces, everything else unencodable becomes `?`.
pub fn sanitize(text: &str) -> String {
    text.encode_utf16()
        .map(|unit| {
            let Some(character) = char::from_u32(u32::from(unit)) else {
                return '?';
            };
            let character = match character {
                '\n' | '\r' | '\t' => ' ',
                other => other,
            };
            if encode_winansi(character).is_some() {
                character
            } else {
                '?'
            }
        })
        .collect()
}

/// Encodes sanitized text as WinAnsi bytes for a PDF string literal.
pub fn winansi_bytes(text: &str) -> Vec<u8> {
    text.chars()
        .map(|character| encode_winansi(character).unwrap_or(b'?'))
        .collect()
}

/// Decodes WinAnsi bytes back to text, used by the structural PDF tests.
pub fn text_from_winansi(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| decode_winansi(*byte)).collect()
}

fn decode_winansi(byte: u8) -> char {
    if (0x20..=0x7e).contains(&byte) || (0xa0..=0xff).contains(&byte) {
        return char::from(byte);
    }
    match byte {
        0x80 => '€',
        0x82 => '‚',
        0x83 => 'ƒ',
        0x84 => '„',
        0x85 => '…',
        0x86 => '†',
        0x87 => '‡',
        0x88 => 'ˆ',
        0x89 => '‰',
        0x8a => 'Š',
        0x8b => '‹',
        0x8c => 'Œ',
        0x8e => 'Ž',
        0x91 => '‘',
        0x92 => '’',
        0x93 => '“',
        0x94 => '”',
        0x95 => '•',
        0x96 => '–',
        0x97 => '—',
        0x98 => '˜',
        0x99 => '™',
        0x9a => 'š',
        0x9b => '›',
        0x9c => 'œ',
        0x9e => 'ž',
        0x9f => 'Ÿ',
        _ => '?',
    }
}

/// The WinAnsi byte for a character, when the encoding carries one.
fn encode_winansi(character: char) -> Option<u8> {
    let code = u32::from(character);
    if (0x20..=0x7e).contains(&code) {
        return Some(u8::try_from(code).expect("ASCII"));
    }
    if (0xa0..=0xff).contains(&code) {
        return Some(u8::try_from(code).expect("Latin-1"));
    }

    let byte = match character {
        '\u{20ac}' => 0x80, // €
        '\u{201a}' => 0x82, // ‚
        '\u{0192}' => 0x83, // ƒ
        '\u{201e}' => 0x84, // „
        '\u{2026}' => 0x85, // …
        '\u{2020}' => 0x86, // †
        '\u{2021}' => 0x87, // ‡
        '\u{02c6}' => 0x88, // ˆ
        '\u{2030}' => 0x89, // ‰
        '\u{0160}' => 0x8a, // Š
        '\u{2039}' => 0x8b, // ‹
        '\u{0152}' => 0x8c, // Œ
        '\u{017d}' => 0x8e, // Ž
        '\u{2018}' => 0x91, // ‘
        '\u{2019}' => 0x92, // ’
        '\u{201c}' => 0x93, // “
        '\u{201d}' => 0x94, // ”
        '\u{2022}' => 0x95, // •
        '\u{2013}' => 0x96, // –
        '\u{2014}' => 0x97, // —
        '\u{02dc}' => 0x98, // ˜
        '\u{2122}' => 0x99, // ™
        '\u{0161}' => 0x9a, // š
        '\u{203a}' => 0x9b, // ›
        '\u{0153}' => 0x9c, // œ
        '\u{017e}' => 0x9e, // ž
        '\u{0178}' => 0x9f, // Ÿ
        _ => return None,
    };
    Some(byte)
}

/// The drawn width of text in a Standard-14 font, in points.
pub fn width_of(text: &str, font: PdfFont, size: f32) -> f32 {
    text.chars()
        .map(|character| f32::from(width_of_character(character, font)) / 1000.0)
        .sum::<f32>()
        * size
}

/// Cuts text to a column width with an ellipsis, measured with the same font.
pub fn fit(value: &str, font: PdfFont, size: f32, width: f32) -> String {
    let text = sanitize(value);
    if width_of(&text, font, size) <= width {
        return text;
    }

    let characters: Vec<char> = text.chars().collect();
    for end in (0..characters.len()).rev() {
        let prefix: String = characters[..end].iter().collect();
        let candidate = format!("{}…", prefix.trim_end());
        if width_of(&candidate, font, size) <= width {
            return candidate;
        }
    }
    ELLIPSIS.to_owned()
}

/// The Helvetica AFM width of a character in 1/1000 em; the euro uses its real WinAnsi width.
fn width_of_character(character: char, font: PdfFont) -> u16 {
    if character == '€' {
        return 556;
    }
    let byte = encode_winansi(character).unwrap_or(b'?');
    if (0x20..=0x7e).contains(&byte) {
        let index = usize::from(byte - 0x20);
        return match font {
            PdfFont::Regular => HELVETICA[index],
            PdfFont::Bold => HELVETICA_BOLD[index],
        };
    }

    match byte {
        0x85 | 0x97 => 1000, // ellipsis, em dash
        0x91 | 0x92 => {
            if font == PdfFont::Bold {
                278
            } else {
                222
            }
        }
        0x93 | 0x94 => {
            if font == PdfFont::Bold {
                500
            } else {
                333
            }
        }
        0x95 => 350,
        0x96 => 556,
        0x99 => 1000,
        0xb7 => 278,
        _ => 556,
    }
}

/// Widths for ASCII 0x20..=0x7E.
const HELVETICA: [u16; 95] = [
    278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
    556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833,
    722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556,
    556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334,
    260, 334, 584,
];

/// Widths for ASCII 0x20..=0x7E.
const HELVETICA_BOLD: [u16; 95] = [
    278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
    556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833,
    722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611,
    556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389,
    280, 389, 584,
];

/// A flowing multi-page PDF under construction.
pub struct PdfBuilder {
    pages: Vec<Content>,
    y: f32,
}

impl Default for PdfBuilder {
    fn default() -> Self {
        Self::new()
    }
}

impl PdfBuilder {
    /// Starts a document with its first page.
    pub fn new() -> Self {
        let mut builder = Self {
            pages: Vec::new(),
            y: 0.0,
        };
        builder.new_page();
        builder
    }

    /// How many pages have been started.
    pub fn page_count(&self) -> usize {
        self.pages.len()
    }

    /// The current text baseline.
    pub fn y(&self) -> f32 {
        self.y
    }

    fn new_page(&mut self) {
        self.pages.push(Content::new());
        self.y = PAGE_HEIGHT - MARGIN;
    }

    /// Makes room for `height` measured from the current line, turning the page when needed.
    pub fn ensure_space(&mut self, height: f32) {
        if self.y - height < MARGIN {
            self.new_page();
        }
    }

    /// Moves the baseline down without a page check.
    pub fn gap(&mut self, height: f32) {
        self.y -= height;
    }

    /// Moves the baseline down without a page check.
    pub fn advance(&mut self, height: f32) {
        self.y -= height;
    }

    /// Draws one flowing text line.
    pub fn text(&mut self, value: &str, font: PdfFont, size: f32) {
        self.ensure_space(size + 4.0);
        self.y -= size + 4.0;
        self.draw(value, font, size, MARGIN);
    }

    /// Draws a bold section heading.
    pub fn heading(&mut self, value: &str) {
        self.text(value, PdfFont::Bold, 12.0);
        self.gap(2.0);
    }

    /// Draws a label at the left margin and its value at the right margin.
    pub fn row(&mut self, label: &str, value: &str, font: PdfFont, size: f32) {
        self.ensure_space(size + 5.0);
        self.y -= size + 5.0;
        self.draw(label, font, size, MARGIN);
        let safe = sanitize(value);
        let width = width_of(&safe, font, size);
        self.draw(&safe, font, size, PAGE_WIDTH - MARGIN - width);
    }

    /// Draws text at the current baseline and a given x position.
    pub fn draw(&mut self, value: &str, font: PdfFont, size: f32, x: f32) {
        let y = self.y;
        let page = self.pages.last_mut().expect("the builder always has a page");
        draw_on(page, value, font, size, x, y);
    }

    /// Draws text fitted to a column.
    pub fn draw_fitted(&mut self, value: &str, font: PdfFont, size: f32, x: f32, width: f32) {
        let value = fit(value, font, size, width);
        self.draw(&value, font, size, x);
    }

    /// Draws text right-aligned in a column.
    pub fn draw_right_fitted(&mut self, value: &str, font: PdfFont, size: f32, right_edge: f32, width: f32) {
        let value = fit(value, font, size, width);
        let x = right_edge - width_of(&value, font, size);
        self.draw(&value, font, size, x);
    }

    /// Draws the page footer at the bottom right of one page.
    pub fn add_footer(&mut self, page_index: usize, value: &str) {
        let safe = sanitize(value);
        let width = width_of(&safe, PdfFont::Regular, FOOTER_SIZE);
        let x = PAGE_WIDTH - MARGIN - width;
        let page = self.pages.get_mut(page_index).expect("the page exists");
        draw_on(page, &safe, PdfFont::Regular, FOOTER_SIZE, x, FOOTER_Y);
    }

    /// Finishes the document and returns its bytes.
    pub fn finish(self) -> Vec<u8> {
        let contents: Vec<Vec<u8>> = self.pages.into_iter().map(|page| page.finish().into_vec()).collect();
        let page_count = contents.len();
        let page_count_i32 = i32::try_from(page_count).expect("a report has fewer than 2^31 pages");

        let catalog_id = Ref::new(1);
        let pages_id = Ref::new(2);
        let regular_id = Ref::new(3);
        let bold_id = Ref::new(4);

        let page_ids: Vec<Ref> = (0..page_count)
            .map(|index| Ref::new(10 + i32::try_from(index).expect("page index") * 2))
            .collect();
        let content_ids: Vec<Ref> = (0..page_count)
            .map(|index| Ref::new(11 + i32::try_from(index).expect("page index") * 2))
            .collect();

        let mut pdf = Pdf::new();
        pdf.catalog(catalog_id).pages(pages_id);
        pdf.pages(pages_id).kids(page_ids.iter().copied()).count(page_count_i32);

        for ((page_id, content_id), bytes) in page_ids.iter().zip(&content_ids).zip(&contents) {
            let mut page = pdf.page(*page_id);
            page.parent(pages_id)
                .media_box(Rect::new(0.0, 0.0, PAGE_WIDTH, PAGE_HEIGHT))
                .contents(*content_id);
            page.resources()
                .fonts()
                .pair(Name(b"F1"), regular_id)
                .pair(Name(b"F2"), bold_id);
            page.finish();
            pdf.stream(*content_id, bytes);
        }

        pdf.type1_font(regular_id)
            .base_font(Name(b"Helvetica"))
            .encoding_predefined(Name(b"WinAnsiEncoding"))
            .finish();
        pdf.type1_font(bold_id)
            .base_font(Name(b"Helvetica-Bold"))
            .encoding_predefined(Name(b"WinAnsiEncoding"))
            .finish();

        pdf.finish()
    }
}

fn draw_on(page: &mut Content, value: &str, font: PdfFont, size: f32, x: f32, y: f32) {
    let bytes = winansi_bytes(&sanitize(value));
    page.begin_text();
    page.set_font(font.resource(), size);
    page.next_line(x, y);
    page.show(Str(&bytes));
    page.end_text();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_replaces_unencodable_characters_and_controls() {
        assert_eq!(sanitize("Beispielhilfe ✓ e. V."), "Beispielhilfe ? e. V.");
        assert_eq!(sanitize("a\nb\tc\rd"), "a b c d");
        assert_eq!(sanitize("Größe · € – …"), "Größe · € – …");
        assert_eq!(sanitize("\u{1}\u{7f}"), "??");
    }

    #[test]
    fn winansi_bytes_use_the_windows_code_page() {
        assert_eq!(winansi_bytes("€"), vec![0x80]);
        assert_eq!(winansi_bytes("·"), vec![0xb7]);
        assert_eq!(winansi_bytes("…"), vec![0x85]);
        assert_eq!(winansi_bytes("ö"), vec![0xf6]);
    }

    #[test]
    fn fit_adds_an_ellipsis_only_when_needed() {
        assert_eq!(fit("short", PdfFont::Regular, 9.0, 100.0), "short");
        let long = "Beispielhaft lang benannte Handwerksfirma mit sehr ausführlichem Namen und Ort";
        let fitted = fit(long, PdfFont::Regular, 9.0, 232.0);
        assert!(fitted.ends_with('…'), "{fitted}");
        assert!(width_of(&fitted, PdfFont::Regular, 9.0) <= 232.0);
    }

    #[test]
    fn a_document_starts_and_ends_like_a_pdf() {
        let mut builder = PdfBuilder::new();
        builder.text("Hello", PdfFont::Bold, 18.0);
        let bytes = builder.finish();
        assert!(bytes.starts_with(b"%PDF-"));
        assert!(bytes.ends_with(b"%%EOF\n") || bytes.ends_with(b"%%EOF"));
    }
}
