//! Rule matching, ported from FinDesk's `repository/RuleRepository.kt` (`RuleMatcher` and `RuleSpec`)
//! and `util/PaymentProviders.kt`.
//!
//! Matching uses [`fancy_regex`] with case-insensitive matching, so the Java-style lookarounds FinDesk
//! users have in their rules keep working. A pattern that does not compile falls back to a
//! case-insensitive substring test and never panics.

pub mod matcher;
pub mod payment_providers;

pub use matcher::{CompiledPattern, RuleMatcher, RuleSpec};
pub use payment_providers::{PaymentProviders, ShopMatch};

use crate::db::Database;
use crate::repo::{self, Result};

/// The auto-apply rules, loaded and compiled once, in priority order.
pub fn matcher(db: &Database) -> Result<RuleMatcher> {
    let specs = repo::rules::list(db)?
        .into_iter()
        .filter(|rule| rule.auto_apply)
        .map(|rule| RuleSpec {
            pattern: rule.pattern,
            category_id: rule.category_id,
            field: rule.match_field,
            direction: rule.direction,
        })
        .collect();
    Ok(RuleMatcher::new(specs))
}
