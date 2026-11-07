//! The credential-store port, ported from FinDesk's `CredentialStore.kt` interface only.
//!
//! Core never talks to the operating system's keychain. The desktop integration supplies a real store in M5b; tests
//! use [`MemoryCredentialStore`] and builds without one use [`NoCredentialStore`].

use std::collections::HashMap;
use std::path::Path;
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, Ordering};

use secrecy::SecretString;
use thiserror::Error;

/// A failure of the credential store. Its message never contains the secret.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
#[error("{message}")]
pub struct CredentialError {
    message: String,
}

impl CredentialError {
    /// Builds a store error from a non-secret description.
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }
}

/// Secrets kept by the operating system, never in the database or a file.
pub trait CredentialStore: Send + Sync {
    /// Whether the system offers a protected store, so the UI may offer to remember a PIN.
    fn is_available(&self) -> bool;

    /// Reads a secret, or `None` when the key is absent.
    fn read(&self, key: &str) -> Result<Option<SecretString>, CredentialError>;

    /// Writes (or replaces) a secret.
    fn write(&self, key: &str, secret: &SecretString) -> Result<(), CredentialError>;

    /// Deletes a secret. Deleting a missing key succeeds.
    fn delete(&self, key: &str) -> Result<(), CredentialError>;
}

/// Used where no protected store is offered: the option to remember a PIN is not shown.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoCredentialStore;

impl CredentialStore for NoCredentialStore {
    fn is_available(&self) -> bool {
        false
    }

    fn read(&self, _key: &str) -> Result<Option<SecretString>, CredentialError> {
        Ok(None)
    }

    fn write(&self, _key: &str, _secret: &SecretString) -> Result<(), CredentialError> {
        Err(CredentialError::new("no credential store available"))
    }

    fn delete(&self, _key: &str) -> Result<(), CredentialError> {
        Ok(())
    }
}

/// An in-memory store for tests. It can simulate a locked or denied store without ever touching the system keychain.
#[derive(Debug, Default)]
pub struct MemoryCredentialStore {
    entries: Mutex<HashMap<String, SecretString>>,
    available: bool,
    fail_reads: AtomicBool,
    fail_writes: AtomicBool,
    fail_deletes: AtomicBool,
}

impl MemoryCredentialStore {
    /// An available, empty store.
    pub fn new() -> Self {
        Self {
            available: true,
            ..Self::default()
        }
    }

    /// A store that reports itself unavailable.
    pub fn unavailable() -> Self {
        Self::default()
    }

    /// Makes every read fail, like a locked keychain.
    pub fn failing_reads(self) -> Self {
        self.set_failing_reads(true);
        self
    }

    /// Makes every later read fail or work again.
    pub fn set_failing_reads(&self, value: bool) {
        self.fail_reads.store(value, Ordering::SeqCst);
    }

    /// Makes every write fail, like a denied keychain.
    pub fn failing_writes(self) -> Self {
        self.set_failing_writes(true);
        self
    }

    /// Makes every later write fail or work again.
    pub fn set_failing_writes(&self, value: bool) {
        self.fail_writes.store(value, Ordering::SeqCst);
    }

    /// Makes every delete fail, like a denied keychain.
    pub fn failing_deletes(self) -> Self {
        self.set_failing_deletes(true);
        self
    }

    /// Makes every later delete fail or work again.
    pub fn set_failing_deletes(&self, value: bool) {
        self.fail_deletes.store(value, Ordering::SeqCst);
    }

    /// Whether the store holds an entry for `key`.
    pub fn contains(&self, key: &str) -> bool {
        self.entries.lock().expect("credential store lock").contains_key(key)
    }

    /// The number of stored entries.
    pub fn len(&self) -> usize {
        self.entries.lock().expect("credential store lock").len()
    }

    /// Whether the store is empty.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

impl CredentialStore for MemoryCredentialStore {
    fn is_available(&self) -> bool {
        self.available
    }

    fn read(&self, key: &str) -> Result<Option<SecretString>, CredentialError> {
        if self.fail_reads.load(Ordering::SeqCst) {
            return Err(CredentialError::new("keychain locked"));
        }
        Ok(self.entries.lock().expect("credential store lock").get(key).cloned())
    }

    fn write(&self, key: &str, secret: &SecretString) -> Result<(), CredentialError> {
        if self.fail_writes.load(Ordering::SeqCst) {
            return Err(CredentialError::new("keychain denied"));
        }
        self.entries
            .lock()
            .expect("credential store lock")
            .insert(key.to_owned(), secret.clone());
        Ok(())
    }

    fn delete(&self, key: &str) -> Result<(), CredentialError> {
        if self.fail_deletes.load(Ordering::SeqCst) {
            return Err(CredentialError::new("keychain denied"));
        }
        self.entries.lock().expect("credential store lock").remove(key);
        Ok(())
    }
}

/// The credential-store key of a bank access's remembered PIN.
///
/// The profile folder makes it profile-specific: two profiles with the same bank id never share a PIN. Callers must
/// pass the canonical folder from [`crate::paths::AppPaths::profile_dir`]; the default profile is the root and named
/// profiles live below it, so their keys never collide. The absolute form matches FinDesk's `pinKey(dataDir, id)` so
/// profile deletion can find the entries.
pub fn pin_key(profile_dir: &Path, bank_id: i64) -> String {
    let absolute = std::path::absolute(profile_dir).unwrap_or_else(|_| profile_dir.to_path_buf());
    format!("{}#bank-{bank_id}", absolute.display())
}

/// The empty marker file that records a remembered PIN for a profile and bank access.
pub fn pin_marker_path(profile_dir: &Path, bank_id: i64) -> std::path::PathBuf {
    profile_dir.join("credentials").join(format!("bank-{bank_id}"))
}

/// Forgets a profile's remembered PINs before its folder is moved.
///
/// Credentials live in the system store, keyed by the profile's data directory, so moving the folder alone would leave
/// them behind. The markers under `credentials/` name the banks even when the profile's database is locked. A stray
/// marker is harmless: deleting a missing entry succeeds and the marker is removed.
pub fn forget_pins_for_profile(store: &dyn CredentialStore, profile_dir: &Path) {
    if !store.is_available() {
        return;
    }
    let credentials = profile_dir.join("credentials");
    let Ok(entries) = std::fs::read_dir(credentials) else {
        return;
    };
    for entry in entries.flatten() {
        let name = entry.file_name();
        let Some(name) = name.to_str() else {
            continue;
        };
        let Some(id) = name.strip_prefix("bank-").and_then(|rest| rest.parse::<i64>().ok()) else {
            continue;
        };
        if store.delete(&pin_key(profile_dir, id)).is_ok() {
            let _ = std::fs::remove_file(entry.path());
        }
    }
}

#[cfg(test)]
mod tests {
    use secrecy::{ExposeSecret, SecretString};

    use super::*;

    #[test]
    fn a_memory_store_round_trips_and_deletes() {
        let store = MemoryCredentialStore::new();
        let secret = SecretString::from("12345");
        store.write("key", &secret).unwrap();
        assert!(store.contains("key"));
        assert_eq!(store.read("key").unwrap().expect("stored").expose_secret(), "12345");
        store.delete("key").unwrap();
        assert!(!store.contains("key"));
        assert!(store.read("key").unwrap().is_none());
    }

    #[test]
    fn the_no_op_store_never_offers_remembering() {
        let store = NoCredentialStore;
        assert!(!store.is_available());
        assert!(store.read("key").unwrap().is_none());
        assert!(store.write("key", &SecretString::from("x")).is_err());
        store.delete("key").unwrap();
    }

    #[test]
    fn a_read_failure_is_reported_without_the_secret() {
        let store = MemoryCredentialStore::new().failing_reads();
        let error = store.read("key").unwrap_err();
        assert!(!error.to_string().contains("12345"));
    }

    #[test]
    fn two_data_directories_never_share_a_key() {
        assert_ne!(
            pin_key(Path::new("/tmp/profile-a"), 7),
            pin_key(Path::new("/tmp/profile-b"), 7)
        );
    }

    #[test]
    fn forgetting_a_profile_finds_its_banks_by_marker_file() {
        let temp = tempfile::tempdir().unwrap();
        let profile = temp.path().join("business");
        let credentials = profile.join("credentials");
        std::fs::create_dir_all(&credentials).unwrap();
        std::fs::write(credentials.join("bank-7"), "").unwrap();
        std::fs::write(credentials.join("notes.txt"), "not a marker").unwrap();

        let store = MemoryCredentialStore::new();
        store
            .write(&pin_key(&profile, 7), &SecretString::from("12345"))
            .unwrap();
        store
            .write(&pin_key(&profile, 8), &SecretString::from("67890"))
            .unwrap();

        forget_pins_for_profile(&store, &profile);

        assert!(!store.contains(&pin_key(&profile, 7)));
        assert!(store.contains(&pin_key(&profile, 8)));
        assert!(!credentials.join("bank-7").exists());
        assert!(credentials.join("notes.txt").exists(), "other files stay");
    }

    #[test]
    fn forgetting_a_profile_without_a_credentials_folder_is_a_no_op() {
        let temp = tempfile::tempdir().unwrap();
        let store = MemoryCredentialStore::new();
        forget_pins_for_profile(&store, temp.path());
    }

    #[test]
    fn a_failed_store_delete_keeps_the_marker() {
        let temp = tempfile::tempdir().unwrap();
        let profile = temp.path().join("business");
        let credentials = profile.join("credentials");
        std::fs::create_dir_all(&credentials).unwrap();
        let marker = credentials.join("bank-7");
        std::fs::write(&marker, "").unwrap();

        let store = MemoryCredentialStore::new();
        store
            .write(&pin_key(&profile, 7), &SecretString::from("12345"))
            .unwrap();
        store.set_failing_deletes(true);

        forget_pins_for_profile(&store, &profile);

        assert!(store.contains(&pin_key(&profile, 7)));
        assert!(marker.exists());
    }
}
