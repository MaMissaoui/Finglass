//! One rule for two-digit years in every import format (CSV, MT940, QIF).

use time::Date;

use super::model::ImportError;

/// Resolves a two-digit year `yy` (0..=99) to a full year: `2000 + yy`, unless that is later than
/// `today`'s year + 1, in which case it is `1900 + yy`. With today in 2026, 28..99 become
/// 1928..1999 and 00..27 become 2000..2027.
pub fn resolve(yy: u8, today: Date) -> Result<i32, ImportError> {
    if yy > 99 {
        return Err(ImportError::invalid(format!("two-digit year out of range: {yy}")));
    }
    let year = 2000 + i32::from(yy);
    if year > today.year() + 1 {
        Ok(1900 + i32::from(yy))
    } else {
        Ok(year)
    }
}
