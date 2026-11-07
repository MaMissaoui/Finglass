//! Read-only bank capabilities, ported from FinDesk's `BankCapabilities.kt` minus every order job.

use std::collections::BTreeSet;

/// The business transactions a bank login offers, detected from the bank's BPD/UPD during a dialog.
///
/// Only read jobs are represented. An unknown set (never synced, or a login that reports nothing) is treated
/// optimistically: the operation is attempted and the bank decides.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BankCapabilities {
    /// BPD/UPD job names the login supports, limited to [`Self::PROBED`].
    pub jobs: BTreeSet<String>,
}

impl BankCapabilities {
    /// The MT940 statement job.
    pub const JOB_STATEMENTS_MT940: &'static str = "KUmsAll";
    /// The CAMT statement job.
    pub const JOB_STATEMENTS_CAMT: &'static str = "KUmsAllCamt";
    /// The balance job.
    pub const JOB_BALANCE: &'static str = "SaldoReq";
    /// The account list job.
    pub const JOB_ACCOUNTS: &'static str = "HKKAU";
    /// The TAN medium list job.
    pub const JOB_TAN_MEDIA: &'static str = "HKTAN";

    /// Read jobs Finglass uses. Only these are probed and stored.
    pub const PROBED: &'static [&'static str] = &[
        Self::JOB_STATEMENTS_MT940,
        Self::JOB_STATEMENTS_CAMT,
        Self::JOB_BALANCE,
        Self::JOB_ACCOUNTS,
        Self::JOB_TAN_MEDIA,
    ];

    /// Whether the login supports `job`.
    pub fn supports(&self, job: &str) -> bool {
        self.jobs.contains(job)
    }

    /// Whether the login supports MT940 statements.
    pub fn supports_mt940(&self) -> bool {
        self.supports(Self::JOB_STATEMENTS_MT940)
    }

    /// Whether the login supports CAMT statements.
    pub fn supports_camt(&self) -> bool {
        self.supports(Self::JOB_STATEMENTS_CAMT)
    }

    /// Whether the login supports any statement format.
    pub fn supports_statements(&self) -> bool {
        self.supports_mt940() || self.supports_camt()
    }

    /// Whether the login supports balances.
    pub fn supports_balance(&self) -> bool {
        self.supports(Self::JOB_BALANCE)
    }

    /// A stable form for the `bank_connections.capabilities` column.
    pub fn serialize(&self) -> String {
        self.jobs.iter().cloned().collect::<Vec<_>>().join(",")
    }

    /// From a support predicate, keeping only jobs Finglass knows.
    pub fn detect(is_supported: impl Fn(&str) -> bool) -> Self {
        Self {
            jobs: Self::PROBED
                .iter()
                .filter(|job| is_supported(job))
                .map(|job| (*job).to_owned())
                .collect(),
        }
    }

    /// Parses a stored value. A missing, blank or empty set means unknown.
    pub fn parse(serialized: Option<&str>) -> Option<Self> {
        let serialized = serialized?.trim();
        if serialized.is_empty() {
            return None;
        }
        let jobs = serialized
            .split(',')
            .map(str::trim)
            .filter(|job| !job.is_empty())
            .map(str::to_owned)
            .collect::<BTreeSet<_>>();
        if jobs.is_empty() { None } else { Some(Self { jobs }) }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detect_keeps_only_read_jobs_finglass_knows() {
        let detected =
            BankCapabilities::detect(|job| job == BankCapabilities::JOB_BALANCE || job == "SOME_UNKNOWN_JOB");
        assert_eq!(
            detected.jobs,
            BTreeSet::from([BankCapabilities::JOB_BALANCE.to_owned()])
        );
        assert!(detected.supports_balance());
        assert!(!detected.supports_statements());
    }

    #[test]
    fn serialize_and_parse_round_trip() {
        let capabilities = BankCapabilities {
            jobs: BTreeSet::from([
                BankCapabilities::JOB_BALANCE.to_owned(),
                BankCapabilities::JOB_STATEMENTS_MT940.to_owned(),
            ]),
        };
        assert_eq!(
            BankCapabilities::parse(Some(&capabilities.serialize())),
            Some(capabilities)
        );
        assert_eq!(
            BankCapabilities::parse(Some("SaldoReq,KUmsAll")).unwrap().serialize(),
            "KUmsAll,SaldoReq"
        );
    }

    #[test]
    fn null_or_blank_means_unknown() {
        assert_eq!(BankCapabilities::parse(None), None);
        assert_eq!(BankCapabilities::parse(Some("")), None);
        assert_eq!(BankCapabilities::parse(Some("   ")), None);
        assert_eq!(
            BankCapabilities::parse(Some(&BankCapabilities::default().serialize())),
            None
        );
    }

    #[test]
    fn statement_support_combines_jobs() {
        let capabilities = BankCapabilities {
            jobs: BTreeSet::from([BankCapabilities::JOB_STATEMENTS_CAMT.to_owned()]),
        };
        assert!(capabilities.supports_statements());
        assert!(!capabilities.supports_mt940());
        assert!(capabilities.supports_camt());
    }

    #[test]
    fn only_read_jobs_are_probed() {
        let probed = BankCapabilities::PROBED.iter().copied().collect::<BTreeSet<_>>();
        assert_eq!(
            probed,
            BTreeSet::from([
                BankCapabilities::JOB_STATEMENTS_MT940,
                BankCapabilities::JOB_STATEMENTS_CAMT,
                BankCapabilities::JOB_BALANCE,
                BankCapabilities::JOB_ACCOUNTS,
                BankCapabilities::JOB_TAN_MEDIA,
            ])
        );
    }
}
