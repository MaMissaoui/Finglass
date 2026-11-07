//! Connection configuration, ported from FinDesk's `FinTSConfig.kt`.

/// Configuration for a FinTS/HBCI banking connection.
///
/// The product registration id is deliberately a value supplied by the caller (or read from
/// `FINGLASS_FINTS_PRODUCT_ID` by the caller before constructing this), never a constant in core.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinTsConfig {
    /// The bank's display name.
    pub bank_name: String,
    /// The German bank code.
    pub blz: String,
    /// The BIC, when known.
    pub bic: String,
    /// The online-banking login.
    pub user_id: String,
    /// The customer id. `None` means "the same as the user id", as in FinDesk.
    pub customer_id: Option<String>,
    /// The HBCI version.
    pub hbci_version: String,
    /// The FinTS server URL, when the user entered one.
    pub server_url: Option<String>,
    /// A remembered TAN procedure id.
    pub tan_method: Option<String>,
    /// The FinTS product registration id.
    pub product_id: String,
}

impl FinTsConfig {
    /// Builds a configuration with FinDesk's defaults for the version and customer id.
    pub fn new(
        bank_name: impl Into<String>,
        blz: impl Into<String>,
        bic: impl Into<String>,
        user_id: impl Into<String>,
        product_id: impl Into<String>,
    ) -> Self {
        Self {
            bank_name: bank_name.into(),
            blz: blz.into(),
            bic: bic.into(),
            user_id: user_id.into(),
            customer_id: None,
            hbci_version: "300".to_owned(),
            server_url: None,
            tan_method: None,
            product_id: product_id.into(),
        }
    }

    /// The customer id, defaulting to the user id when the bank does not distinguish them.
    pub fn customer_id(&self) -> &str {
        self.customer_id.as_deref().unwrap_or(&self.user_id)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn customer_id_defaults_to_the_user_id() {
        let config = FinTsConfig::new("Deutsche Bank", "37040044", "DEUTDEDBXXX", "testuser", "PRODUCT");
        assert_eq!(config.customer_id(), "testuser");
        assert_eq!(config.hbci_version, "300");
    }

    #[test]
    fn the_product_id_is_the_callers_value() {
        let config = FinTsConfig::new("Bank", "37050198", "", "user", "REGISTERED-ID");
        assert_eq!(config.product_id, "REGISTERED-ID");
    }
}
