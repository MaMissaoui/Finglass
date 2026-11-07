//! The real OCR engine: the `tesseract` command-line program.
//!
//! FinDesk calls Tesseract through tess4j. Finglass runs the same program directly, with the `deu+eng` language
//! pair and TSV output, so the confidence is computed the same way: every word's confidence weighted by its
//! character count, grouped into the text lines Tesseract reports.

use std::ffi::OsString;
use std::io::Read;
use std::path::Path;
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use super::ocr::{OcrEngine, OcrError, Recognition};
use super::programs;

/// The language pair used for recognition.
pub const DEFAULT_LANGUAGE: &str = "deu+eng";

/// How long a spawned helper program may run before it is killed.
pub const COMMAND_TIMEOUT: Duration = Duration::from_secs(120);

/// The captured result of a helper command.
pub(crate) struct CommandOutput {
    /// Whether the command exited successfully.
    pub success: bool,
    /// Everything the command wrote to stdout.
    pub stdout: Vec<u8>,
}

/// Runs a program without a shell, with stdin closed and a hard timeout.
///
/// Arguments are passed separately, never as one shell string. The stdout and stderr pipes are drained on
/// threads while the child runs, so a chatty program cannot deadlock on a full pipe.
pub(crate) fn run_with_timeout(program: &Path, args: &[OsString], timeout: Duration) -> std::io::Result<CommandOutput> {
    let mut child = Command::new(program)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()?;

    let mut stdout = child.stdout.take().expect("stdout is piped");
    let mut stderr = child.stderr.take().expect("stderr is piped");
    let stdout_reader = thread::spawn(move || {
        let mut buffer = Vec::new();
        let _ = stdout.read_to_end(&mut buffer);
        buffer
    });
    let stderr_reader = thread::spawn(move || {
        let mut buffer = Vec::new();
        let _ = stderr.read_to_end(&mut buffer);
    });

    let started = Instant::now();
    let status = loop {
        if let Some(status) = child.try_wait()? {
            break status;
        }
        if started.elapsed() >= timeout {
            let _ = child.kill();
            let _ = child.wait();
            let _ = stdout_reader.join();
            let _ = stderr_reader.join();
            return Err(std::io::Error::new(
                std::io::ErrorKind::TimedOut,
                "helper command timed out",
            ));
        }
        thread::sleep(Duration::from_millis(50));
    };

    let stdout = stdout_reader.join().unwrap_or_default();
    let _ = stderr_reader.join();

    Ok(CommandOutput {
        success: status.success(),
        stdout,
    })
}

/// Tesseract through its command-line interface.
#[derive(Debug, Clone)]
pub struct TesseractCli {
    language: String,
}

impl TesseractCli {
    /// Returns a working engine, or `None` when Tesseract or the `deu`/`eng` traineddata is missing.
    ///
    /// This is the equivalent of FinDesk's `TesseractOcrEngine.createOrNull`: it probes
    /// `tesseract --list-langs` instead of loading a native library.
    #[must_use]
    pub fn create_or_none() -> Option<Self> {
        let tesseract = programs::resolve("tesseract")?;
        let output = run_with_timeout(&tesseract, &[OsString::from("--list-langs")], Duration::from_secs(10)).ok()?;
        if !output.success {
            return None;
        }

        let listed = String::from_utf8_lossy(&output.stdout);
        let has_language = |language: &str| listed.lines().any(|line| line.trim() == language);
        if !has_language("deu") || !has_language("eng") {
            return None;
        }

        Some(Self {
            language: DEFAULT_LANGUAGE.to_owned(),
        })
    }

    /// The text lines and their character-weighted confidence.
    fn recognition_from_tsv(tsv: &[u8]) -> Recognition {
        struct Line {
            key: (i64, i64, i64, i64),
            words: Vec<(String, f64)>,
        }

        let text = String::from_utf8_lossy(tsv);
        let mut lines: Vec<Line> = Vec::new();

        for row in text.lines() {
            let columns: Vec<&str> = row.split('\t').collect();
            if columns.len() < 12 {
                continue;
            }
            if columns[0].trim().parse::<i64>().ok() != Some(5) {
                continue;
            }

            let Ok(page) = columns[1].trim().parse::<i64>() else {
                continue;
            };
            let Ok(block) = columns[2].trim().parse::<i64>() else {
                continue;
            };
            let Ok(paragraph) = columns[3].trim().parse::<i64>() else {
                continue;
            };
            let Ok(line) = columns[4].trim().parse::<i64>() else {
                continue;
            };
            let Ok(confidence) = columns[10].trim().parse::<f64>() else {
                continue;
            };
            let word = columns[11].trim_end();
            if word.is_empty() {
                continue;
            }
            let key = (page, block, paragraph, line);

            let words = match lines.iter_mut().find(|line| line.key == key) {
                Some(existing) => &mut existing.words,
                None => {
                    lines.push(Line { key, words: Vec::new() });
                    &mut lines.last_mut().expect("just pushed").words
                }
            };
            words.push((word.to_owned(), confidence));
        }

        let mut joined = Vec::new();
        let mut total_chars = 0usize;
        let mut weighted = 0.0f64;

        for line in lines {
            let mut words = Vec::with_capacity(line.words.len());
            for (word, confidence) in line.words {
                let length = word.chars().count();
                total_chars += length;
                weighted += confidence * length as f64;
                words.push(word);
            }
            let line = words.join(" ").trim_end().to_owned();
            if !line.is_empty() {
                joined.push(line);
            }
        }

        Recognition {
            text: joined.join("\n"),
            confidence: if total_chars == 0 {
                0.0
            } else {
                weighted / total_chars as f64
            },
        }
    }
}

impl OcrEngine for TesseractCli {
    fn recognize(&self, image: &Path) -> Result<Recognition, OcrError> {
        let args = vec![
            image.as_os_str().to_owned(),
            OsString::from("stdout"),
            OsString::from("-l"),
            OsString::from(&self.language),
            OsString::from("--psm"),
            OsString::from("3"),
            OsString::from("tsv"),
        ];

        let tesseract = programs::resolve("tesseract")
            .ok_or_else(|| std::io::Error::new(std::io::ErrorKind::NotFound, "tesseract was not found"))?;
        let output = run_with_timeout(&tesseract, &args, COMMAND_TIMEOUT)?;
        if !output.success {
            return Err(OcrError::CommandFailed);
        }

        Ok(Self::recognition_from_tsv(&output.stdout))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::documents::test_support::text_pdf;
    use crate::money::Currency;
    use crate::receipt;
    use time::macros::date;

    fn tool_available(program: &str) -> bool {
        let Some(program) = programs::resolve(program) else {
            return false;
        };
        Command::new(program)
            .arg("-v")
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .is_ok()
    }

    fn render_first_page(pdf: &Path, png: &Path) {
        let pdftoppm = programs::resolve("pdftoppm").expect("pdftoppm resolves");
        let status = Command::new(pdftoppm)
            .args([
                "-r",
                "300",
                "-gray",
                "-png",
                "-singlefile",
                pdf.to_str().unwrap(),
                png.with_extension("").to_str().unwrap(),
            ])
            .status()
            .unwrap();
        assert!(status.success(), "pdftoppm failed");
    }

    #[test]
    fn recognizes_a_rendered_receipt_and_parser_finds_total_and_date() {
        let engine = TesseractCli::create_or_none();
        if engine.is_none() || !tool_available("pdftoppm") {
            println!("skipping: tesseract with deu+eng or pdftoppm is not installed");
            return;
        }

        let temp = tempfile::tempdir().unwrap();
        let pdf = temp.path().join("receipt.pdf");
        text_pdf(
            &pdf,
            &[
                "REWE Markt GmbH",
                "Datum: 03.10.2026",
                "Milch 1,29",
                "Brot 2,49",
                "SUMME EUR 3,78",
            ],
        );
        let png = temp.path().join("receipt.png");
        render_first_page(&pdf, &png);

        let result = engine.unwrap().recognize(&png).unwrap();

        assert!(
            result.text.to_lowercase().contains("rewe"),
            "recognized text: {:?}",
            result.text
        );
        assert!(result.confidence > 50.0, "confidence {}", result.confidence);
        let fields = receipt::parse(&result.text, Currency::EUR, date!(2026 - 11 - 07));
        assert_eq!(fields.total.map(|total| total.minor), Some(378), "{:?}", result.text);
        assert_eq!(fields.date, Some(date!(2026 - 10 - 03)), "{:?}", result.text);
    }
}
