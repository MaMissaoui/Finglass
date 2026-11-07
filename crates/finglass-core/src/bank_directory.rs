//! Offline German bank directory from the Bundesbank bank code file.
//!
//! The file is fixed-width, ISO-8859-1 and keyed by BLZ. Users rarely know their bank's BLZ by heart,
//! so [`BankDirectory::search`] matches a name, BLZ, BIC or a German IBAN, as FinDesk's
//! `BankDirectory` does for the add-bank dialog.

use std::collections::HashMap;
use std::sync::OnceLock;

use crate::iban;

/// The bank code file shipped with Finglass, byte for byte as published.
const BUNDLED_FILE: &[u8] = include_bytes!("../data/bundesbank/blz-aktuell.txt");

/// A fixed-width record is 168 bytes long.
const RECORD_LENGTH: usize = 168;

/// One bank in the directory.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BankEntry {
    /// The German bank code (Bankleitzahl).
    pub blz: String,
    /// The bank's name.
    pub name: String,
    /// The postal code.
    pub postal_code: String,
    /// The city.
    pub city: String,
    /// The BIC; may be empty.
    pub bic: String,
}

/// Parses the Bundesbank bank code file.
///
/// Every line is sliced by byte offsets on the raw bytes and each field is then decoded as
/// ISO-8859-1. Decoding the whole line as UTF-8 first would shift every column after an umlaut.
/// Merkmal-2 records (extra branches) and records marked `D` (deleted) are dropped; lines shorter
/// than 168 bytes are skipped.
#[must_use]
pub fn parse(bytes: &[u8]) -> Vec<BankEntry> {
    let mut entries = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for raw in bytes.split(|byte| *byte == b'\n') {
        let line = raw.strip_suffix(b"\r").unwrap_or(raw);
        if line.len() < RECORD_LENGTH {
            continue;
        }
        if line[8] != b'1' || line[158] == b'D' {
            continue;
        }
        let blz = decode(&line[0..8]).trim().to_owned();
        if blz.is_empty() || !seen.insert(blz.clone()) {
            continue;
        }
        entries.push(BankEntry {
            blz,
            name: decode(&line[9..67]).trim_end().to_owned(),
            postal_code: decode(&line[67..72]).trim().to_owned(),
            city: decode(&line[72..107]).trim().to_owned(),
            bic: decode(&line[139..150]).trim().to_owned(),
        });
    }
    entries
}

/// ISO-8859-1: every byte is the Unicode code point of the same value.
fn decode(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| char::from(*byte)).collect()
}

/// A parsed bank directory with lookup indexes.
#[derive(Debug)]
pub struct BankDirectory {
    entries: Vec<BankEntry>,
    by_blz: HashMap<String, usize>,
    by_bic: HashMap<String, usize>,
}

impl BankDirectory {
    /// The directory parsed from the bundled bank code file, once per process.
    #[must_use]
    pub fn bundled() -> &'static BankDirectory {
        static BUNDLED: OnceLock<BankDirectory> = OnceLock::new();
        BUNDLED.get_or_init(|| BankDirectory::from_entries(parse(BUNDLED_FILE)))
    }

    /// Builds a directory from entries, for tests and callers that bring their own list.
    #[must_use]
    pub fn from_entries(entries: Vec<BankEntry>) -> Self {
        let mut by_blz = HashMap::new();
        let mut by_bic: HashMap<String, usize> = HashMap::new();
        for (index, entry) in entries.iter().enumerate() {
            by_blz.entry(entry.blz.clone()).or_insert(index);
            let bic = entry.bic.trim().to_uppercase();
            if bic.is_empty() {
                continue;
            }
            by_bic
                .entry(bic)
                .and_modify(|current| {
                    if entry.blz < entries[*current].blz {
                        *current = index;
                    }
                })
                .or_insert(index);
        }
        Self {
            entries,
            by_blz,
            by_bic,
        }
    }

    /// Looks a bank up by BLZ, ignoring surrounding whitespace.
    #[must_use]
    pub fn lookup(&self, blz: &str) -> Option<&BankEntry> {
        self.by_blz.get(blz.trim()).map(|index| &self.entries[*index])
    }

    /// Looks a bank up by BIC, case-insensitively. An 8-character BIC also matches `<bic>XXX`.
    /// Several entries with one BIC resolve to the one with the lowest BLZ.
    #[must_use]
    pub fn by_bic(&self, bic: &str) -> Option<&BankEntry> {
        let needle = bic.trim().to_uppercase();
        if needle.is_empty() {
            return None;
        }
        let mut found = self.by_bic.get(&needle).copied();
        if needle.len() == 8
            && let Some(index) = self.by_bic.get(&format!("{needle}XXX")).copied()
        {
            found = Some(match found {
                Some(current) if self.entries[current].blz <= self.entries[index].blz => current,
                _ => index,
            });
        }
        found.map(|index| &self.entries[index])
    }

    /// The bank behind a valid German IBAN, or `None` for an invalid or unknown one.
    #[must_use]
    pub fn by_iban(&self, iban: &str) -> Option<&BankEntry> {
        let normalised = iban::normalize(iban);
        if !normalised.starts_with("DE") || !iban::is_valid(&normalised) {
            return None;
        }
        let blz: String = normalised.chars().skip(4).take(8).collect();
        self.lookup(&blz)
    }

    /// Free-text bank search for the add-bank dialog.
    ///
    /// A complete German IBAN resolves straight to its bank. Shorter queries are ignored, and
    /// otherwise matches are ranked: BLZ prefix, BIC prefix, name prefix, name contains, city
    /// contains. The result is sorted by rank, name, city and BLZ and capped at `limit`.
    #[must_use]
    pub fn search(&self, query: &str, limit: usize) -> Vec<&BankEntry> {
        let trimmed = query.trim();
        if trimmed.is_empty() {
            return Vec::new();
        }
        if let Some(entry) = self.by_iban(trimmed) {
            return vec![entry];
        }
        if trimmed.chars().count() < 3 {
            return Vec::new();
        }
        let needle = trimmed.to_lowercase();
        let mut matches: Vec<(u8, &BankEntry)> = self
            .entries
            .iter()
            .filter_map(|entry| entry_rank(entry, &needle).map(|rank| (rank, entry)))
            .collect();
        matches.sort_by(|(left_rank, left), (right_rank, right)| {
            left_rank
                .cmp(right_rank)
                .then_with(|| left.name.cmp(&right.name))
                // Among same-name branches, the entry whose own BLZ is its FinTS code (the central
                // office) is the most useful suggestion. Branches then follow in city order.
                .then_with(|| central_rank(left).cmp(&central_rank(right)))
                .then_with(|| left.city.cmp(&right.city))
                .then_with(|| left.blz.cmp(&right.blz))
        });
        matches.into_iter().take(limit).map(|(_, entry)| entry).collect()
    }
}

/// `0` for a bank whose own BLZ is the one its FinTS server accepts, `1` for a branch mapped elsewhere.
fn central_rank(entry: &BankEntry) -> u8 {
    if entry.bic.starts_with("HYVEDEMM") && entry.blz != "70020270" {
        1
    } else {
        0
    }
}

/// Lower is better; `None` means no match.
fn entry_rank(entry: &BankEntry, needle: &str) -> Option<u8> {
    if entry.blz.starts_with(needle) {
        Some(0)
    } else if entry.bic.to_lowercase().starts_with(needle) {
        Some(1)
    } else if entry.name.to_lowercase().starts_with(needle) {
        Some(2)
    } else if entry.name.to_lowercase().contains(needle) {
        Some(3)
    } else if entry.city.to_lowercase().contains(needle) {
        Some(4)
    } else {
        None
    }
}

/// The BLZ whose FinTS server speaks for `blz`'s bank.
///
/// HypoVereinsbank's FinTS server accepts only the central bank code. The evidence is a real dialog:
/// a login with the branch BLZ got HTTP 500 on the first message, while the central BLZ initialised
/// the dialog. Don't add other banks without such evidence. The directory lookups (BIC, IBAN) keep
/// using the real branch entries.
#[must_use]
pub fn fints_blz(directory: &BankDirectory, blz: &str) -> String {
    let trimmed = blz.trim();
    let bic = directory.lookup(trimmed).map(|entry| entry.bic.as_str()).unwrap_or("");
    if bic.starts_with("HYVEDEMM") {
        "70020270".to_owned()
    } else {
        trimmed.to_owned()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use sha2::{Digest, Sha256};

    const BUNDLED_SHA256: &str = "5872bf19298f55cc0594d22656b72e0f8ff277faedbf5b18f9b7f3bcae4c606b";

    fn put(line: &mut [u8], start: usize, value: &[u8]) {
        line[start..start + value.len()].copy_from_slice(value);
    }

    fn record_bytes(
        merkmal: u8,
        blz: &str,
        name: &[u8],
        postal_code: &str,
        city: &[u8],
        bic: &str,
        change: u8,
    ) -> Vec<u8> {
        let mut line = vec![b' '; RECORD_LENGTH];
        put(&mut line, 0, blz.as_bytes());
        line[8] = merkmal;
        put(&mut line, 9, name);
        put(&mut line, 67, postal_code.as_bytes());
        put(&mut line, 72, city);
        put(&mut line, 139, bic.as_bytes());
        line[158] = change;
        line
    }

    fn record(blz: &str, name: &str, postal_code: &str, city: &str, bic: &str) -> Vec<u8> {
        record_bytes(b'1', blz, name.as_bytes(), postal_code, city.as_bytes(), bic, b' ')
    }

    #[test]
    fn parses_fixed_width_fields_and_drops_extra_and_deleted_records() {
        let mut bytes = Vec::new();
        bytes.extend(record("12345678", "Example Bank", "10115", "Berlin", "EXAMPLEBICX"));
        bytes.extend(b"\r\n");
        bytes.extend(record_bytes(
            b'2',
            "22222222",
            b"Extra Branch",
            "10115",
            b"Berlin",
            "EXTRAXXBICX",
            b' ',
        ));
        bytes.extend(b"\r\n");
        bytes.extend(record_bytes(
            b'1',
            "33333333",
            b"Deleted Bank",
            "10115",
            b"Berlin",
            "DELETEDBICX",
            b'D',
        ));
        bytes.extend(b"\n");
        bytes.extend(record("44444444", "Second Bank", "20095", "Hamburg", "SECONDBICXX"));

        let entries = parse(&bytes);
        assert_eq!(entries.len(), 2, "{entries:?}");
        assert_eq!(entries[0].blz, "12345678");
        assert_eq!(entries[0].name, "Example Bank");
        assert_eq!(entries[0].postal_code, "10115");
        assert_eq!(entries[0].city, "Berlin");
        assert_eq!(entries[0].bic, "EXAMPLEBICX");
        assert_eq!(entries[1].blz, "44444444");
    }

    #[test]
    fn an_umlaut_decodes_from_iso_8859_1_without_shifting_later_fields() {
        let bytes = record_bytes(
            b'1',
            "80331000",
            b"Bank M\xfcnchen",
            "80331",
            b"M\xfcnchen",
            "HYVEDEMMXXX",
            b' ',
        );
        let entries = parse(&bytes);
        assert_eq!(entries.len(), 1);
        let entry = &entries[0];
        assert_eq!(entry.name, "Bank München");
        assert_eq!(entry.postal_code, "80331");
        assert_eq!(entry.city, "München");
        assert_eq!(entry.bic, "HYVEDEMMXXX");
    }

    #[test]
    fn a_short_line_is_skipped() {
        let mut bytes = record("12345678", "Example Bank", "10115", "Berlin", "EXAMPLEBICX");
        bytes.push(b'\r');
        bytes.push(b'\n');
        bytes.extend(vec![b' '; RECORD_LENGTH - 1]);
        bytes.push(b'\n');
        let entries = parse(&bytes);
        assert_eq!(entries.len(), 1);
    }

    #[test]
    fn bundled_file_has_the_published_checksum_and_the_expected_banks() {
        let digest = Sha256::digest(BUNDLED_FILE);
        let mut hex = String::with_capacity(digest.len() * 2);
        for byte in digest {
            use std::fmt::Write as _;
            let _ = write!(hex, "{byte:02x}");
        }
        assert_eq!(hex, BUNDLED_SHA256);

        let directory = BankDirectory::bundled();
        assert!(directory.entries.len() > 3_000, "{} entries", directory.entries.len());

        let hypo = directory.lookup("70020270").expect("HypoVereinsbank");
        assert_eq!(hypo.name, "UniCredit Bank - HypoVereinsbank");
        assert_eq!(hypo.city, "München");
        assert_eq!(hypo.bic, "HYVEDEMMXXX");
        assert!(directory.search("hypo", 15).iter().any(|entry| entry.blz == "70020270"));
        assert_eq!(
            directory.by_bic("hyvedemmxxx").map(|entry| entry.blz.as_str()),
            Some("70020270")
        );

        let iban = "DE67700202701234567890";
        assert_eq!(
            directory.by_iban(iban).map(|entry| entry.blz.as_str()),
            Some("70020270")
        );
        assert!(directory.by_iban("DE00700202701234567890").is_none());
    }

    #[test]
    fn fints_blz_maps_hypovereinsbank_branches_but_leaves_other_banks_alone() {
        let directory = BankDirectory::bundled();
        let branch = directory.lookup("10020890").expect("a HypoVereinsbank branch");
        assert!(branch.bic.starts_with("HYVEDEMM"));
        assert_ne!(branch.blz, "70020270");
        assert_eq!(fints_blz(directory, &branch.blz), "70020270");
        assert_eq!(fints_blz(directory, " 10010010 "), "10010010");
        assert_eq!(fints_blz(directory, "99999999"), "99999999");
    }

    fn entry(blz: &str, name: &str, city: &str, bic: &str) -> BankEntry {
        BankEntry {
            blz: blz.to_owned(),
            name: name.to_owned(),
            postal_code: "10115".to_owned(),
            city: city.to_owned(),
            bic: bic.to_owned(),
        }
    }

    #[test]
    fn search_ranks_a_blz_prefix_over_every_other_match() {
        let directory = BankDirectory::from_entries(vec![
            entry("99999999", "Bank with 123 inside", "Ort", "AAABICX"),
            entry("99999998", "Stadtbank", "123stadt", "AAABICX"),
            entry("99999997", "123 Name Bank", "Ort", "AAABICX"),
            entry("99999996", "Other Bank", "Ort", "123BICX"),
            entry("12345678", "Target Bank", "Ort", "AAABICX"),
        ]);
        let found: Vec<&str> = directory
            .search("123", 15)
            .iter()
            .map(|entry| entry.blz.as_str())
            .collect();
        assert_eq!(found, vec!["12345678", "99999996", "99999997", "99999999", "99999998"]);
    }

    #[test]
    fn search_needs_three_characters_and_honours_the_limit() {
        let directory = BankDirectory::from_entries(vec![
            entry("11111111", "Alpha Bank", "Berlin", "ALPHABICXXX"),
            entry("22222222", "Alpha Zweig", "Berlin", "ALPHA2BICXX"),
            entry("33333333", "Alphabet", "Berlin", "ALPHA3BICXX"),
        ]);
        assert!(directory.search("Al", 15).is_empty());
        assert!(directory.search("   ", 15).is_empty());
        assert_eq!(directory.search("Alpha", 2).len(), 2);
        assert_eq!(directory.search("Alpha", 15).len(), 3);
    }

    #[test]
    fn search_resolves_a_valid_german_iban_to_its_bank() {
        let directory = BankDirectory::from_entries(vec![entry(
            "70020270",
            "UniCredit Bank - HypoVereinsbank",
            "München",
            "HYVEDEMMXXX",
        )]);
        let found = directory.search("DE67700202701234567890", 15);
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].blz, "70020270");
    }

    #[test]
    fn by_bic_is_case_insensitive_accepts_an_eight_character_bic_and_prefers_the_lowest_blz() {
        let directory = BankDirectory::from_entries(vec![
            entry("90000000", "High", "Ort", "SAME BICXXX"),
            entry("10000000", "Low", "Ort", "same bicxxx"),
            entry("20000000", "Eight", "Ort", "EIGHTBICXXX"),
        ]);
        assert_eq!(
            directory.by_bic(" Same BicXXX ").map(|entry| entry.blz.as_str()),
            Some("10000000")
        );
        assert_eq!(
            directory.by_bic("EIGHTBIC").map(|entry| entry.blz.as_str()),
            Some("20000000")
        );
        assert_eq!(
            directory.by_bic("EIGHTBICXXX").map(|entry| entry.blz.as_str()),
            Some("20000000")
        );
        assert!(directory.by_bic("").is_none());
    }

    #[test]
    fn lookup_trims_the_input() {
        let directory = BankDirectory::from_entries(vec![entry(
            "70020270",
            "UniCredit Bank - HypoVereinsbank",
            "München",
            "HYVEDEMMXXX",
        )]);
        assert_eq!(
            directory.lookup(" 70020270 ").map(|entry| entry.name.as_str()),
            Some("UniCredit Bank - HypoVereinsbank")
        );
        assert!(directory.lookup("00000000").is_none());
    }
}
