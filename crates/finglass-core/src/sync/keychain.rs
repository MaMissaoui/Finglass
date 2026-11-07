//! The macOS Keychain credential store, ported from FinDesk's `MacKeychainStore`.
//!
//! Generic passwords are filed under the bundle identifier as the service and the profile-scoped
//! [`pin_key`](super::credentials::pin_key) as the account. A missing entry is not a failure: reads return `None` and
//! deletes succeed. Every other backend error becomes a [`CredentialError`] that names the operation and the backend's
//! error kind only, never the secret and never the account key.
//!
//! Other operating systems use [`NoCredentialStore`](super::credentials::NoCredentialStore), so "Remember PIN" is not
//! offered there.

use std::path::Path;
use std::sync::Arc;

use super::credentials::{CredentialStore, NoCredentialStore};

/// The Keychain service all entries are filed under: the app's bundle identifier.
pub const KEYCHAIN_SERVICE: &str = "io.github.mamissaoui.finglass";

/// The environment variable that can force [`NoCredentialStore`], e.g. for development runs.
pub const CREDENTIAL_STORE_ENV: &str = "FINGLASS_CREDENTIAL_STORE";

/// The account string of a bank access's remembered PIN, in the form the credential store uses.
pub fn account_for(profile_dir: &Path, bank_id: i64) -> String {
    super::credentials::pin_key(profile_dir, bank_id)
}

/// The credential store the running system offers.
///
/// The app calls this once at startup. `FINGLASS_CREDENTIAL_STORE=none` forces the no-op store; on every other
/// operating system the no-op store is used regardless.
pub fn system_credential_store() -> Arc<dyn CredentialStore> {
    system_credential_store_with(std::env::var(CREDENTIAL_STORE_ENV).ok().as_deref())
}

/// [`system_credential_store`] with the override value passed in, so tests never touch the environment.
pub fn system_credential_store_with(override_value: Option<&str>) -> Arc<dyn CredentialStore> {
    if override_value == Some("none") {
        return Arc::new(NoCredentialStore);
    }

    #[cfg(target_os = "macos")]
    {
        match KeychainCredentialStore::new() {
            Ok(store) => Arc::new(store),
            // A keychain that cannot be reached is no store, not a crash. The UI then stops offering "Remember PIN".
            Err(_) => Arc::new(NoCredentialStore),
        }
    }

    #[cfg(not(target_os = "macos"))]
    {
        Arc::new(NoCredentialStore)
    }
}

#[cfg(target_os = "macos")]
mod apple {
    use keyring_core::api::CredentialStoreApi;
    use secrecy::{ExposeSecret, SecretString};

    use super::KEYCHAIN_SERVICE;
    use crate::sync::credentials::{CredentialError, CredentialStore};

    /// Generic passwords in the user's login Keychain, through the Security framework.
    ///
    /// Constructing the store never unlocks or prompts the Keychain; only reading and writing an entry does.
    pub struct KeychainCredentialStore {
        store: std::sync::Arc<apple_native_keyring_store::keychain::Store>,
    }

    impl KeychainCredentialStore {
        /// Creates the store for the user's login Keychain.
        pub fn new() -> keyring_core::Result<Self> {
            Ok(Self {
                store: apple_native_keyring_store::keychain::Store::new()?,
            })
        }

        fn entry(&self, account: &str) -> keyring_core::Result<keyring_core::Entry> {
            self.store.build(KEYCHAIN_SERVICE, account, None)
        }
    }

    impl CredentialStore for KeychainCredentialStore {
        fn is_available(&self) -> bool {
            true
        }

        fn read(&self, key: &str) -> Result<Option<SecretString>, CredentialError> {
            let entry = self.entry(key).map_err(|error| map_error("read", error))?;
            read_result(entry.get_password())
        }

        fn write(&self, key: &str, secret: &SecretString) -> Result<(), CredentialError> {
            let entry = self.entry(key).map_err(|error| map_error("write", error))?;
            entry
                .set_password(secret.expose_secret())
                .map_err(|error| map_error("write", error))
        }

        fn delete(&self, key: &str) -> Result<(), CredentialError> {
            let entry = self.entry(key).map_err(|error| map_error("delete", error))?;
            delete_result(entry.delete_credential())
        }
    }

    /// Maps a backend read result: "no entry" is an absent secret, every other error is a failure.
    fn read_result(result: keyring_core::Result<String>) -> Result<Option<SecretString>, CredentialError> {
        match result {
            Ok(password) => Ok(Some(SecretString::from(password))),
            Err(keyring_core::Error::NoEntry) => Ok(None),
            Err(error) => Err(map_error("read", error)),
        }
    }

    /// Maps a backend delete result: deleting a missing entry succeeds, every other error is a failure.
    fn delete_result(result: keyring_core::Result<()>) -> Result<(), CredentialError> {
        match result {
            Ok(()) | Err(keyring_core::Error::NoEntry) => Ok(()),
            Err(error) => Err(map_error("delete", error)),
        }
    }

    /// Names the operation and the backend's error kind only. The account key and the secret never appear.
    fn map_error(operation: &str, error: keyring_core::Error) -> CredentialError {
        CredentialError::new(format!("credential store {operation} failed: {}", error_kind(&error)))
    }

    fn error_kind(error: &keyring_core::Error) -> &'static str {
        match error {
            keyring_core::Error::PlatformFailure(_) => "platform failure",
            keyring_core::Error::NoStorageAccess(_) => "storage access denied",
            keyring_core::Error::NoEntry => "no entry",
            keyring_core::Error::BadEncoding(_) => "bad encoding",
            keyring_core::Error::BadDataFormat(_, _) => "bad data format",
            keyring_core::Error::BadStoreFormat(_) => "bad store format",
            keyring_core::Error::TooLong(_, _) => "value too long",
            keyring_core::Error::Invalid(_, _) => "invalid parameter",
            keyring_core::Error::Ambiguous(_) => "ambiguous entry",
            keyring_core::Error::NoDefaultStore => "no default store",
            keyring_core::Error::NotSupportedByStore(_) => "unsupported by store",
            _ => "unknown error",
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn a_missing_entry_reads_as_absent_and_deletes_successfully() {
            assert!(read_result(Err(keyring_core::Error::NoEntry)).unwrap().is_none());
            assert!(delete_result(Err(keyring_core::Error::NoEntry)).is_ok());
        }

        #[test]
        fn other_errors_name_the_operation_and_kind_but_not_the_account_or_secret() {
            let account = crate::sync::keychain::account_for(std::path::Path::new("/tmp/profile-a"), 7);
            let secret = "12345";

            let locked = || keyring_core::Error::NoStorageAccess(Box::new(std::io::Error::other("keychain locked")));
            let broken = || keyring_core::Error::PlatformFailure(Box::new(std::io::Error::other("keychain broken")));

            let read_error = read_result(Err(locked())).unwrap_err();
            let delete_error = delete_result(Err(broken())).unwrap_err();
            let write_error = map_error("write", locked());

            for (operation, error) in [("read", read_error), ("delete", delete_error), ("write", write_error)] {
                let message = error.to_string();
                assert!(message.contains(operation), "{message}");
                assert!(!message.contains(&account), "{message}");
                assert!(!message.contains(secret), "{message}");
            }
        }

        #[test]
        fn bad_encoding_is_a_failure_without_the_secret() {
            let error = read_result(Err(keyring_core::Error::BadEncoding(b"secret".to_vec()))).unwrap_err();
            let message = error.to_string();
            assert!(message.contains("read"));
            assert!(message.contains("bad encoding"));
            assert!(!message.contains("secret"));
        }
    }
}

#[cfg(target_os = "macos")]
pub use apple::KeychainCredentialStore;

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::*;
    use crate::sync::credentials::pin_key;

    #[test]
    fn the_service_is_the_bundle_identifier() {
        assert_eq!(KEYCHAIN_SERVICE, "io.github.mamissaoui.finglass");
    }

    #[test]
    fn the_account_is_the_profile_scoped_pin_key() {
        let profile_a = Path::new("/tmp/profile-a");
        let profile_b = Path::new("/tmp/profile-b");

        for (profile, bank) in [(profile_a, 7), (profile_a, 8), (profile_b, 7), (profile_b, 8)] {
            assert_eq!(account_for(profile, bank), pin_key(profile, bank));
        }

        assert_ne!(account_for(profile_a, 7), account_for(profile_a, 8));
        assert_ne!(account_for(profile_a, 7), account_for(profile_b, 7));
        assert_ne!(account_for(profile_a, 8), account_for(profile_b, 8));
    }

    #[test]
    fn the_none_override_yields_an_unavailable_store() {
        let store = system_credential_store_with(Some("none"));
        assert!(!store.is_available());
        assert!(store.read("key").unwrap().is_none());
        store.delete("key").unwrap();
    }
}
